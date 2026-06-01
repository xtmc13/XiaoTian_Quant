package app

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/cache"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/factor"
	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/watchdog"
)

// Context holds all application services and manages their lifecycle.
type Context struct {
	Config   *config.Config
	EventBus *event.EventBus
	Logger   *logging.Logger

	RiskManager      *risk.Manager
	PortfolioManager *portfolio.Manager
	StrategyEngine   *strategy.Engine
	FactorPipeline   *factor.Pipeline
	BacktestRunner   *backtest.Runner
	Notifier         *notify.Manager
	Watchdog         *watchdog.Watchdog
	Cache            cache.Cache
	BinanceWS        *exchange.BinanceWSStream

	mu      sync.Mutex
	started bool
}

var instance *Context
var once sync.Once

// Get returns the global application context.
func Get() *Context {
	once.Do(func() {
		instance = &Context{}
	})
	return instance
}

// Init initializes all services in the proper order.
func (ctx *Context) Init(cfg *config.Config) error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.started {
		return nil
	}

	ctx.Config = cfg

	// 1. Logger
	ctx.Logger = logging.New("gateway")
	switch cfg.Server.LogLevel {
	case "DEBUG":
		ctx.Logger.SetLevel(logging.LevelDebug)
	case "WARN":
		ctx.Logger.SetLevel(logging.LevelWarn)
	case "ERROR":
		ctx.Logger.SetLevel(logging.LevelError)
	}
	ctx.Logger.Info("Initializing XiaoTianQuant Gateway", "version", "2.0")

	// 2. Database
	if err := store.InitDB(); err != nil {
		ctx.Logger.Warn("Database init skipped", "error", err.Error())
	} else {
		if err := store.RunMigrations(); err != nil {
			ctx.Logger.Warn("Migration skipped", "error", err.Error())
		}
		ctx.Logger.Info("Database initialized")
	}
	store.LoadConfig()
	store.LoadStrategyConfigs()

	// 3. Event Bus (10000 buffer, 4 workers)
	ctx.EventBus = event.NewEventBus(10000, 4)
	ctx.Logger.Info("Event bus started")

	// 4. Cache
	ctx.Cache = cache.GetCache()

	// 5. Risk Manager
	riskCfg := risk.DefaultManagerConfig()
	if cfg.Risk.MaxOrderSize > 0 {
		riskCfg.MaxOrderUSDT = cfg.Risk.MaxOrderSize
	}
	if cfg.Risk.PriceSanityPct > 0 {
		riskCfg.PriceDeviationPct = cfg.Risk.PriceSanityPct
	}
	ctx.RiskManager = risk.NewManager(riskCfg)
	ctx.Logger.Info("Risk manager initialized")

	// 6. Portfolio Manager
	ctx.PortfolioManager = portfolio.GetManager()
	ctx.Logger.Info("Portfolio manager initialized")

	// 6a. Sync portfolio from Binance (async, non-blocking)
	go func() {
		time.Sleep(2 * time.Second) // Wait for other components to settle
		ctx.PortfolioManager.SyncAllExchanges()
	}()

	// 7. Strategy Engine
	ctx.StrategyEngine = strategy.GetEngine(ctx.EventBus)
	ctx.Logger.Info("Strategy engine initialized")

	// 8. Factor Pipeline
	ctx.FactorPipeline = factor.NewPipeline(
		factor.NewPriceFactor(),
		factor.NewVolumeFactor(),
		factor.NewRSIFactor(14),
		factor.NewMACDFactor(12, 26, 9),
		factor.NewMomentumFactor(20),
		factor.NewVolatilityFactor(20),
	)

	// 9. Backtest Runner
	ctx.BacktestRunner = backtest.NewRunner(backtest.DefaultRunnerConfig())
	ctx.Logger.Info("Backtest runner initialized")

	// 10. Notifier
	ctx.Notifier = notify.GetManager()
	ctx.Logger.Info("Notifier initialized")

	// 11. AI Providers — register from config
	for _, p := range cfg.AI.Providers {
		if p.Enabled {
			ai.RegisterProvider(ai.Provider{
				Name:    p.Name,
				BaseURL: p.BaseURL,
				APIKey:  p.APIKey,
				Model:   p.Model,
			})
		}
	}
	ctx.Logger.Info("AI providers registered")

	// 12. Watchdog
	ctx.Watchdog = watchdog.New(3)
	ctx.Watchdog.Register("event_bus")
	ctx.Watchdog.Register("risk_manager")
	ctx.Watchdog.Register("portfolio")
	ctx.Logger.Info("Watchdog initialized")

	// 13. Binance WebSocket — real-time market data for strategies
	ctx.BinanceWS = exchange.NewBinanceWSStream(
		[]string{"btcusdt", "ethusdt", "solusdt", "bnbusdt", "dogeusdt"},
		ctx.EventBus,
	)
	// Feed real Binance prices to frontend WebSocket (avoids import cycle)
	ctx.BinanceWS.SetOnRealPrice(exchange.FrontendPriceFeed)
	if err := ctx.BinanceWS.Start(); err != nil {
		ctx.Logger.Warn("Binance WS start failed: " + err.Error())
	} else {
		ctx.Logger.Info("Binance WebSocket stream started")
	}

	// 14. Subscribe to risk alerts → notifier
	ctx.EventBus.Subscribe("risk_notify", event.PrioNormal, func(evt event.Event) {
		if alert, ok := evt.Data.(string); ok {
			ctx.Notifier.Send(notify.Message{
				Title:   "Risk Alert",
				Content: alert,
				Level:   "WARN",
				Tags:    map[string]string{"symbol": evt.Symbol},
			})
		}
	}, event.TypeRiskAlert)

	ctx.started = true
	ctx.Logger.Info("All components initialized successfully")
	return nil
}

// Shutdown gracefully stops all services.
func (ctx *Context) Shutdown() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.Logger.Info("Shutting down...")

	if ctx.BinanceWS != nil {
		ctx.BinanceWS.Stop()
	}
	if ctx.StrategyEngine != nil {
		ctx.StrategyEngine.StopAll()
	}
	if ctx.Watchdog != nil {
		ctx.Watchdog.Shutdown()
	}

	store.CloseDB()
	ctx.Logger.Info("Shutdown complete")
}

// WaitForShutdown blocks until SIGINT or SIGTERM is received.
func (ctx *Context) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx.Shutdown()
}
