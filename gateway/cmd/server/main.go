package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/grid"
	"github.com/xiaotian-quant/gateway/internal/handler"
	"github.com/xiaotian-quant/gateway/internal/market"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

func main() {
	// ── Load configuration ──
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Printf("WARNING: Config load failed, using defaults: %v", err)
		cfg = config.Default()
	}

	// ── Initialize application context (all services) ──
	appCtx := app.Get()
	if err := appCtx.Init(cfg); err != nil {
		log.Printf("WARNING: App init partial: %v", err)
	}
	defer appCtx.Shutdown()

	// ── Ensure legacy store is initialized (handlers depend on it directly) ──
	if err := store.InitDB(); err != nil {
		if isFatalInitErr(err) {
			log.Fatalf("FATAL: %v", err)
		}
		log.Printf("WARNING: SQLite init skipped: %v", err)
	}
	store.LoadConfig()
	store.LoadStrategyConfigs()

	// ── 凭证安全（P0-4）：启动期主密钥检查（production 未设 → fatal，与
	// SECRET_KEY 同级）+ 明文真实密钥迁移进加密保险库并抹除 config.yaml 明文。 ──
	if err := store.EnsureVaultReady(); err != nil {
		if isFatalInitErr(err) {
			log.Fatalf("FATAL: %v", err)
		}
		log.Printf("WARNING: vault init: %v", err)
	}
	if migrated, err := store.MigratePlaintextCredentialsToVault(); err != nil {
		log.Printf("WARNING: credential migration: %v", err)
	} else if len(migrated) > 0 {
		log.Printf("[vault] migrated plaintext credentials for: %v", migrated)
	}

	// ── Grid bot runner: 7×24 real-market grid trading bots ──
	// 价格源直接用 BinanceWS 内存价（零网络）；返回 0 时由 runner 跳过本轮。
	gridRepo := store.NewGridRepo()
	priceSource := func(symbol string) float64 {
		if appCtx.BinanceWS != nil {
			return appCtx.BinanceWS.GetPrice(symbol)
		}
		return 0
	}
	gridRunner := grid.NewRunner(priceSource, gridRepo)
	// HTTP 层通过窄接口 GridService 操控 runner（创建/启动/停止机器人）。
	handler.SetGridService(gridRunner)
	// 启动即恢复 grid_bots 中 status='running' 的机器人，之后每 60s 复查，
	// 防御进程重启漏恢复或行情无效导致的跳过。
	go gridRunner.RetryResume(func() ([]*store.GridBotRecord, error) {
		return gridRepo.List(map[string]any{"status": "running"}, 0)
	}, 60*time.Second)

	// ── Register strategy factories for combo engine ──
	registerStrategyFactories()

	// ── K线供给管：轮询币安 REST，新闭合 K 线发布 model.Bar 事件到总线，
	// 让吃 K 线（OnBar）的策略在实盘能收到真实 K 线。 ──
	handler.SetKlineFeeder(market.NewKlineFeeder(appCtx.EventBus))

	// ── 启动即恢复 status=running 的策略（断点续跑），每 60s 复查 ──
	go handler.ResumeRunningStrategiesLoop()

	// ── Setup Gin ──
	setupGinMode(cfg)
	r := setupGinEngine(appCtx)

	// ── Register all routes ──
	setupRoutes(r, &serverConfig{ServerMode: cfg.Server.Mode})

	// ── Start background tasks ──
	go handler.StartBackgroundTasks()

	// ── Market data cache purger (every 30 seconds) ──
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			market.GetCache().Purge()
		}
	}()

	// ── Start server with graceful shutdown ──
	port := cfg.Server.Port
	if port == "" {
		port = os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
	}

	appCtx.Logger.Info("XiaoTianQuant Gateway starting", "port", port)

	srv := &http.Server{
		Addr:    "0.0.0.0:" + port,
		Handler: metrics.HTTPMiddleware(r),
	}

	go func() {
		// Grid bots must stop BEFORE appCtx.WaitForShutdown returns: that
		// call runs Shutdown(), which closes the store — a StopAll after it
		// would persist nothing. Both signal channels receive the same
		// SIGINT/SIGTERM, so WaitForShutdown proceeds immediately after.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		gridRunner.StopAll()
		appCtx.WaitForShutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

// registerStrategyFactories registers all built-in strategy factories
// and their frontend-friendly aliases for the combo engine.
func registerStrategyFactories() {
	strategy.RegisterStrategyFactory("breakout", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })
	strategy.RegisterStrategyFactory("ema_cross", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("rsi", func() strategy.Strategy { return strategies.NewRSIStrategy() })
	strategy.RegisterStrategyFactory("bollinger_bands", func() strategy.Strategy { return strategies.NewBollingerBandsStrategy() })
	strategy.RegisterStrategyFactory("atr_trailing_stop", func() strategy.Strategy { return strategies.NewATRTrailingStopStrategy() })
	strategy.RegisterStrategyFactory("dual_thrust", func() strategy.Strategy { return strategies.NewDualThrustStrategy() })
	strategy.RegisterStrategyFactory("renko", func() strategy.Strategy { return strategies.NewRenkoStrategy() })
	strategy.RegisterStrategyFactory("grid_trading", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
	strategy.RegisterStrategyFactory("arbitrage", func() strategy.Strategy { return strategies.NewArbitrageStrategy() })
	strategy.RegisterStrategyFactory("market_making", func() strategy.Strategy { return strategies.NewMarketMakingStrategy() })
	strategy.RegisterStrategyFactory("martingale", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("wallstreet", func() strategy.Strategy { return strategies.NewWallstreetStrategy() })
	strategy.RegisterStrategyFactory("wallstreet_v2", func() strategy.Strategy { return strategy.NewWallStreetStrategy() })

	// Aliases for frontend bot_type names
	strategy.RegisterStrategyFactory("grid", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
	strategy.RegisterStrategyFactory("dca", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("trend", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })
	strategy.RegisterStrategyFactory("martin_trend", func() strategy.Strategy { return strategy.NewMartinStrategy() })
	strategy.RegisterStrategyFactory("martin_trend_v2", func() strategy.Strategy { return strategy.NewMartinStrategy() })
	strategy.RegisterStrategyFactory("macd_golden", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("macd_death", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("dual_burn", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("ema_follow", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("ema_counter", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("custom", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })

	// AI Bots marketplace aliases
	strategy.RegisterStrategyFactory("optimus", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
	strategy.RegisterStrategyFactory("cyberbot", func() strategy.Strategy { return strategies.NewRSIStrategy() })
	strategy.RegisterStrategyFactory("mono_optimus", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
	strategy.RegisterStrategyFactory("mono_cyberbot", func() strategy.Strategy { return strategies.NewRSIStrategy() })
	strategy.RegisterStrategyFactory("crypto_future", func() strategy.Strategy { return strategies.NewDualThrustStrategy() })
	strategy.RegisterStrategyFactory("ai_alpha", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("ai_alpha_futures", func() strategy.Strategy { return strategies.NewDualThrustStrategy() })
	strategy.RegisterStrategyFactory("terminator_volatility", func() strategy.Strategy { return strategies.NewATRTrailingStopStrategy() })
	strategy.RegisterStrategyFactory("alt_volatility", func() strategy.Strategy { return strategies.NewDualThrustStrategy() })
	strategy.RegisterStrategyFactory("trade_holder", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("noah", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })

	// CRA-style frontend aliases
	strategy.RegisterStrategyFactory("aggressive", func() strategy.Strategy { return strategy.NewMartinStrategy() })
	strategy.RegisterStrategyFactory("conservative", func() strategy.Strategy { return strategy.NewMartinStrategy() })
	strategy.RegisterStrategyFactory("high_flat", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("macd_golden_long", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("macd_death_short", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("ema_follow_trend", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("ema_counter_trend", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("macd_spot_long", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("ema_spot", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("trend_long", func() strategy.Strategy { return strategies.NewTrendLongStrategy() })
	strategy.RegisterStrategyFactory("trend_short", func() strategy.Strategy { return strategies.NewTrendShortStrategy() })
	strategy.RegisterStrategyFactory("counter_stable", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("counter_safe", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("head_tail_arb", func() strategy.Strategy { return strategies.NewArbitrageStrategy() })
	// CRA unified factories (frontend types are mapped to these in handler/strategy.go).
	strategy.RegisterStrategyFactory("cra_spot", func() strategy.Strategy { return cra.NewCRASpotStrategy("cra_spot", "BTCUSDT") })
	strategy.RegisterStrategyFactory("cra_contract", func() strategy.Strategy { return cra.NewCRAContractStrategy("cra_contract", "BTCUSDT") })
}

// setupGinMode configures Gin's mode based on server config.
func setupGinMode(cfg *config.Config) {
	if cfg.Server.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
}

// setupGinEngine creates the Gin engine with middleware and health routes.
func setupGinEngine(appCtx *app.Context) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestLogger(appCtx.Logger), gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.CORS())
	r.Use(middleware.UnifiedResponseWrapper())

	return r
}

// isFatalInitErr determines whether a store initialization error should stop the server.
func isFatalInitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return (strings.Contains(msg, "SECRET_KEY") || strings.Contains(msg, "VAULT_MASTER_KEY")) && strings.Contains(msg, "required in production")
}

// isLocalhost checks if an IP address is loopback.
func isLocalhost(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}
