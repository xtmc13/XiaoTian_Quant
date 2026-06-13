package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/handler"
	"github.com/xiaotian-quant/gateway/internal/market"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

func main() {
	// ── Load configuration ──
	cfg, err := config.Load("config.yaml")
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

	// ── Register strategy factories for combo engine ──
	registerStrategyFactories()

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

	// Aliases for frontend bot_type names
	strategy.RegisterStrategyFactory("grid", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
	strategy.RegisterStrategyFactory("dca", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("trend", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })
	strategy.RegisterStrategyFactory("martin_trend", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("macd_golden", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("macd_death", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	strategy.RegisterStrategyFactory("dual_burn", func() strategy.Strategy { return strategies.NewMartingaleStrategy() })
	strategy.RegisterStrategyFactory("ema_follow", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("ema_counter", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	strategy.RegisterStrategyFactory("custom", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })
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
	return strings.Contains(msg, "SECRET_KEY") && strings.Contains(msg, "required in production")
}

// isLocalhost checks if an IP address is loopback.
func isLocalhost(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}
