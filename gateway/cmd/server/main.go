package main

import (
	"context"
	"log"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/alerting"
	"github.com/xiaotian-quant/gateway/internal/alerts"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/dataprovider"
	"github.com/xiaotian-quant/gateway/internal/dca"
	"github.com/xiaotian-quant/gateway/internal/grid"
	"github.com/xiaotian-quant/gateway/internal/handler"
	"github.com/xiaotian-quant/gateway/internal/lmartin"
	"github.com/xiaotian-quant/gateway/internal/market"
	"github.com/xiaotian-quant/gateway/internal/marketplace"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/reconcile"
	"github.com/xiaotian-quant/gateway/internal/social"
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
	// Grid bot runner 的合约腿（short/neutral）下单与 DCA 共用 OMS 执行器
	// （market_type=swap + position_side + leverage/margin_mode，paper 即时
	// 成交，live 进单前过 canPlaceLiveOrder 实盘安全闸）。执行器按机器人类型
	// 打 client_oid 前缀（A8.2 成交恢复按前缀把补录成交路由回引擎回填）。
	gridRunner := grid.NewRunner(priceSource, gridRepo, handler.NewKindOMSBotExecutor("grid"))
	// HTTP 层通过窄接口 GridService 操控 runner（创建/启动/停止机器人）。
	handler.SetGridService(gridRunner)
	// 启动即恢复 grid_bots 中 status='running' 的机器人，之后每 60s 复查，
	// 防御进程重启漏恢复或行情无效导致的跳过。
	go gridRunner.RetryResume(func() ([]*store.GridBotRecord, error) {
		return gridRepo.List(map[string]any{"status": "running"}, 0)
	}, 60*time.Second)

	// ── DCA 定投机器人 runner（A1.2）：与 grid 同一调度/持久化模式，
	// 下单走 OMS（paper 即时成交，live 过现有实盘安全闸）。 ──
	dcaRepo := store.NewDCARepo()
	dcaRunner := dca.NewRunner(priceSource, dcaRepo, handler.NewKindOMSBotExecutor("dca"))
	handler.SetDCAService(dcaRunner)
	go dcaRunner.RetryResume(func() ([]*store.DCABotRecord, error) {
		return dcaRepo.List(map[string]any{"status": "running"}, 0)
	}, 60*time.Second)

	// ── 分层马丁格尔机器人 runner（A1.3）：多组独立马丁循环，
	// 层数/组预算硬限 + 成交确认推进，下单同走 OMS。 ──
	lmRepo := store.NewLayeredMartinRepo()
	lmRunner := lmartin.NewRunner(priceSource, lmRepo, handler.NewKindOMSBotExecutor("lmartin"))
	handler.SetLayeredMartinService(lmRunner)
	go lmRunner.RetryResume(func() ([]*store.LayeredMartinBotRecord, error) {
		return lmRepo.List(map[string]any{"status": "running"}, 0)
	}, func(botID string) ([]*store.LayeredMartinGroupRecord, error) {
		return lmRepo.GetGroups(botID)
	}, 60*time.Second)

	// ── 用户 Python 策略契约化运行时（v1）：策略代码在独立 python3 沙箱子
	// 进程中长驻执行，K 线走 KlineFeeder→事件总线，信号经 OMSBotExecutor
	// 进 OMS；paper=1 强制模拟盘，paper=0 启动前过实盘闸。接线细节见
	// handler/pystrat_wiring.go。 ──
	pyStratRunner := handler.NewPyStratRunner()
	handler.SetPyStratService(pyStratRunner)
	go pyStratRunner.RetryResume(func() ([]*store.PyStrategyRecord, error) {
		return store.NewPyStrategyRepo().List(map[string]any{"status": "active"}, 0)
	}, 60*time.Second)

	// ── A8 对账体系（持仓对账/成交恢复/资金费对账/实盘偏差监控/回报PnL对账）──
	// 交易所访问工厂：有默认凭证的交易所构造适配器，供 reconcile 通过窄接口
	// （PositionQuerier/OrderStatusQuerier/OrderTradesQuerier/FundingQuerier/ReportedPnLQuerier）
	// 取用；未配置的交易所返回 nil，对应任务自动跳过。
	reconcileExchangeFactory := func(name string) any {
		// IBKR 走本地 Client Portal 网关 + 会话 cookie 认证，无 API key；
		// 启用即可构造（adapter 内部处理认证维持/重认证）。
		if name == "ibkr" {
			if adapter.IBKRConfigured() {
				return adapter.NewIBKRAdapter(adapter.LoadIBKRConfig())
			}
			return nil
		}
		apiKey, secret, passphrase := adapter.GetCredential(name)
		if apiKey == "" || secret == "" {
			return nil
		}
		switch name {
		case "binance":
			// 桥接：reconcile 窄接口要求 reconcile.ReportedPnL 返回类型，
			// 避免 reconcile 反向依赖 adapter（嵌入保留 GetPositions 等提升方法）。
			return binanceReportedPnLBridge{adapter.NewBinanceAdapter(apiKey, secret, false)}
		case "bybit":
			return adapter.NewBybitAdapter(apiKey, secret, false)
		case "okx":
			return adapter.NewOKXAdapter(apiKey, secret, passphrase, false)
		case "mexc":
			return adapter.NewMEXCAdapter(apiKey, secret)
		case "gateio", "gate":
			return adapter.NewGateIOAdapter(apiKey, secret)
		case "kraken":
			return adapter.NewKrakenAdapter(apiKey, secret)
		case "bitget":
			return adapter.NewBitgetAdapter(apiKey, secret, passphrase)
		case "coinbase":
			return adapter.NewCoinbaseAdapter(apiKey, secret)
		case "alpaca":
			return adapter.NewAlpacaAdapter(apiKey, secret, false)
		}
		return nil
	}
	reconcileSvc := reconcile.NewService(store.NewReconcileRepo(), reconcileExchangeFactory)
	handler.SetReconcileService(reconcileSvc)
	// A8.2 成交恢复 → 各引擎 ApplyFill 回填路由。
	reconcile.RegisterFillApplier("dca", func(botID, side string, qty, price float64) error {
		return dcaRunner.ApplyRecoveredFill(botID, side, qty, price)
	})
	reconcile.RegisterFillApplier("lmartin", func(botID, side string, qty, price float64) error {
		return lmRunner.ApplyRecoveredFill(botID, side, qty, price)
	})
	reconcile.RegisterFillApplier("grid", func(botID, side string, qty, price float64) error {
		return gridRunner.ApplyRecoveredFill(botID, side, qty, price)
	})
	reconcileSvc.Start()

	// ── 指标信号告警扫描（对标 QuantDinger indicator_signal_alerts）──
	alertSvc := alerts.NewService(store.NewIndicatorAlertRepo(), alerts.MarketKlineSource{}, notify.GetManager(), 0, 0)
	handler.SetAlertScanService(alertSvc)
	alertSvc.Start()

	// ── Alertmanager 告警接入（/api/alerts/webhook → fingerprint 去重 → notify 路由）──
	handler.SetAlertIngestService(alerting.NewService(store.NewAlertEventRepo(), notify.NewBroadcaster()))

	// ── 外部数据生态采集（对标 QuantDinger data_providers）──
	// 情绪/宏观/新闻/热力图/经济日历：每源限流+熔断+TTL 缓存，周期刷新落库
	// xt_dataprovider_cache（迁移 0031）；密钥全走环境变量，未配置的源自动降级
	// 为 not_configured，API 照常 200 返回降级结构。
	dpCfg := dataprovider.LoadEnvConfig()
	dpSvc := dataprovider.NewService(dataprovider.BuildSources(dpCfg, nil), store.NewDataProviderRepo(), dpCfg.Secrets())
	dataprovider.SetDefault(dpSvc)
	dpSvc.Start()

	// ── ML 训练闭环（对标 FreqAI live_retrain_hours 全链路）────────────────
	// 链路：retrainer 调度（含漂移触发）→ TrainingLoop 闭环执行
	//   数据准备（本地 K 线）→ ml_server 训练（不可达时按 ML_TRAIN_FALLBACK
	//   降级 Go 原生训练或跳过并告警，不 panic）→ 导出模型 JSON → 原子热加载
	//   进 ModelRegistry（Go 原生推理）→ 运行档案落库 xt_ml_training_runs。
	// ml_server 生命周期：ML_SERVER_MANAGED=true 时由 gateway 拉起/守护
	// sandbox/ml_server/server.py（健康检查、崩溃重启、优雅退出）。
	mlServerMgr := ml.NewServerManager(ml.LoadServerManagerConfig(), notify.GetManager())
	mlServerMgr.Start()
	if mlServerMgr.Status().Managed {
		// 托管模式下 MLClient 跟随管理器端口（env ML_SERVER_PORT 可改）
		handler.MLClient = ml.NewClient(mlServerMgr.URL())
	}
	mlModelDir := os.Getenv("ML_MODEL_EXPORT_DIR")
	if mlModelDir == "" {
		mlModelDir = "data/ml_models" // 导出模型 JSON 落盘目录（热加载 + 重启恢复）
	}
	mlModelRegistry := ml.NewModelRegistry(mlModelDir)
	mlModelRegistry.LoadFromDir() // 重启恢复已导出模型
	mlTrainingLoop := ml.NewTrainingLoop(
		ml.NewTrainingPipeline(handler.MLClient, handler.DataDownloader),
		handler.MLClient,
		mlModelRegistry,
		store.NewMLTrainingRunRepo(),
	)
	mlRetrainer := ml.NewRetrainer(
		store.NewMLRetrainRepo(),
		mlTrainingLoop,
		notify.GetManager(),
		ml.DefaultPredictionCache(),
	)
	handler.SetMLRetrainer(mlRetrainer)
	handler.SetMLLoopDeps(mlRetrainer, mlTrainingLoop, mlModelRegistry, mlServerMgr, store.NewMLTrainingRunRepo())
	mlRetrainer.Start()

	// ── P1-4 开放信号市场：利润分成每日结算引擎（T+锁定后可提现）──
	settleEngine := social.NewSettlementEngine(social.NewMarketService())
	settleEngine.Start()

	// ── 市场上架准入：标准化统计日聚合 + 考核达标自动转 pending_review ──
	marketEngine := marketplace.NewEngine(marketplace.NewService())
	marketEngine.Start()

	// ── A2.2 limit-then-market 跟踪器：超时撤剩余 + 市价补单（paper/live 均支持）──
	ltmTracker := order.GetLimitMarketTracker()
	ltmTracker.SetNotifyHook(func(s *order.LMState, msg string) {
		notify.GetManager().Send(notify.Message{
			Title:   "limit-then-market 执行异常: " + s.Symbol,
			Content: msg,
			Level:   "WARN",
			Tags:    map[string]string{"source": "ltm", "order_id": s.LimitOrderID},
		})
		notify.GetNotificationStore().Add("limit-then-market 执行异常", msg, "WARN", "order")
	})
	ltmTracker.Start()
	// 重启恢复：扫描 xt_orders 中挂在中间态的 ltm 订单，重建状态机继续执行
	// （原 limit_timeout_ms 未持久化，统一给一个宽限窗口后进入超时→市价补单）。
	if n := ltmTracker.RestoreFromStore(order.DefaultLMRecoveryGrace); n > 0 {
		log.Printf("[ltm] 重启恢复 %d 个未完成 limit-then-market 订单", n)
	}

	// ── 阶梯智能单引擎：分档止盈 + 保本/追踪止损 + 一键全平（paper/live 同路径）──
	// 价格源复用条件单引擎的 WS 喂价缓存；子单全部走 OMS。
	ladderEng := order.GetLadderEngine()
	ladderEng.SetPriceSource(order.GetConditionalEngine().GetPrice)
	ladderEng.SetNotifyHook(func(l *order.LadderOrder, msg string) {
		notify.GetManager().Send(notify.Message{
			Title:   "阶梯单执行异常: " + l.Symbol,
			Content: msg,
			Level:   "WARN",
			Tags:    map[string]string{"source": "ladder", "ladder_id": l.ID},
		})
		notify.GetNotificationStore().Add("阶梯单执行异常", msg, "WARN", "order")
	})
	ladderEng.Start()
	// 重启恢复：扫描 xt_ladder_orders 未终结的阶梯单，回填子单进 OMS 后继续状态机。
	if n := ladderEng.RestoreFromStore(); n > 0 {
		log.Printf("[ladder] 重启恢复 %d 个未终结阶梯单", n)
	}

	// ── C2.2 撮合引擎资金校验：接组合账本（paper 账户）可用余额 ──
	// 模拟做市单（userID=0）与未配置组合账本时自动豁免（+Inf）。
	if appCtx.MatchingService != nil {
		appCtx.MatchingService.SetBalanceProvider(portfolioBalanceProvider{})
	}

	// ── Register strategy factories for combo engine ──
	registerStrategyFactories()

	// ── K线供给管：轮询币安 REST，新闭合 K 线发布 model.Bar 事件到总线，
	// 让吃 K 线（OnBar）的策略在实盘能收到真实 K 线。 ──
	handler.SetKlineFeeder(market.NewKlineFeeder(appCtx.EventBus))

	// ── 策略引擎接线：同一供给管注入引擎，供多周期（A7.1）/动态 universe（A7.3）
	// 动态增删 symbol 的 K 线订阅（feeder 引用计数，与 handler 侧独立记账）。 ──
	if eng := strategy.GetEngine(appCtx.EventBus); eng != nil {
		eng.SetKlineFeeder(handler.KlineFeederForEngine())
	}

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

	// PROMETHEUS_ENABLED=false 时整个 HTTP 指标中间件跳过（/metrics 也不挂载）。
	var httpHandler http.Handler = r
	if metrics.Enabled() {
		httpHandler = metrics.HTTPMiddleware(r)
	}

	srv := &http.Server{
		Addr:    "0.0.0.0:" + port,
		Handler: httpHandler,
	}

	go func() {
		// Grid/DCA/分层马丁 bots must stop BEFORE appCtx.WaitForShutdown returns:
		// that call runs Shutdown(), which closes the store — a StopAll after it
		// would persist nothing. Both signal channels receive the same
		// SIGINT/SIGTERM, so WaitForShutdown proceeds immediately after.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		gridRunner.StopAll()
		dcaRunner.StopAll()
		lmRunner.StopAll()
		pyStratRunner.StopAll() // 杀沙箱子进程 + 退订 K 线，须在关库前落库状态
		// 对账/limit-then-market 后台任务先停（它们依赖 store 与 OMS），
		// 与上方 bots 同理：必须在 appCtx.WaitForShutdown 关库之前完成。
		reconcileSvc.Stop()
		mlRetrainer.Stop()
		mlServerMgr.Stop() // 托管模式才终止 ml_server 子进程（收养的实例不杀）
		ltmTracker.Stop()
		settleEngine.Stop()
		marketEngine.Stop()
		alertSvc.Stop()
		dpSvc.Stop()
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

// registerStrategyFactories registers all built-in strategy factories// and their frontend-friendly aliases for the combo engine.
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
	strategy.RegisterStrategyFactory("universe_rotation", func() strategy.Strategy { return strategies.NewUniverseRotationStrategy() })
	strategy.RegisterStrategyFactory("trend_long_mt", func() strategy.Strategy { return strategies.NewTrendLongStrategy() })
	strategy.RegisterStrategyFactory("trend_short_mt", func() strategy.Strategy { return strategies.NewTrendShortStrategy() })
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

// portfolioBalanceProvider 把组合账本（paper 默认账户）的可用余额喂给撮合引擎
// 做资金校验（C2.2）。组合账本未初始化时返回 +Inf（不校验，保持旧行为）。
type portfolioBalanceProvider struct{}

// binanceReportedPnLBridge 把 BinanceAdapter 的回报 PnL 查询桥接为 reconcile 窄接口
// ReportedPnLQuerier 要求的 reconcile.ReportedPnL 返回类型（reconcile 不反向依赖
// adapter）；内嵌 *adapter.BinanceAdapter，GetPositions 等提升方法不受影响。
type binanceReportedPnLBridge struct{ *adapter.BinanceAdapter }

func (b binanceReportedPnLBridge) GetRealizedPnLIncomes(symbol string, startMs, endMs int64) ([]reconcile.ReportedPnL, error) {
	rows, err := b.BinanceAdapter.GetRealizedPnLIncomes(symbol, startMs, endMs)
	if err != nil {
		return nil, err
	}
	out := make([]reconcile.ReportedPnL, 0, len(rows))
	for _, r := range rows {
		out = append(out, reconcile.ReportedPnL{
			Symbol:  r.Symbol,
			Asset:   r.Asset,
			Income:  r.Income,
			Time:    r.Time,
			TradeID: r.TradeID,
			Info:    r.Info,
		})
	}
	return out, nil
}

func (portfolioBalanceProvider) Available(userID uint64, asset string) float64 {
	mgr := portfolio.GetManager()
	if mgr == nil {
		return math.Inf(1)
	}
	acct := mgr.GetAccount("default")
	if acct == nil {
		return math.Inf(1)
	}
	bal, ok := acct.Balances[strings.ToUpper(asset)]
	if !ok || bal == nil {
		return 0
	}
	return bal.Free
}
