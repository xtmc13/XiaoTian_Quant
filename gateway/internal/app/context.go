package app

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/cache"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/factor"
	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/service"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/watchdog"
	"github.com/xiaotian-quant/gateway/internal/ws"
)

// Context holds all application services and manages their lifecycle.
type Context struct {
	Config   *config.Config
	EventBus *event.EventBus
	Logger   *logging.Logger

	RiskManager      *risk.Manager
	PortfolioManager *portfolio.Manager
	StrategyEngine   *strategy.Engine
	DCAManager       *order.DCAManager
	FactorPipeline   *factor.Pipeline
	BacktestRunner   *backtest.Runner
	Notifier         *notify.Manager
	Watchdog         *watchdog.Watchdog
	Cache            cache.Cache
	BinanceWS        *exchange.BinanceWSStream
	MatchingService  *service.MatchingService
	FundingTracker   *risk.FundingFeeTracker

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
	ctx.Logger.Info("Initializing XiaoTianQuant Gateway", "version", "3.0")

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
	// position_limit_pct 等风控配置此前从未接线（恒用默认 50%），paper 时期
	// 被 10 万假权益掩盖；权益改真实后暴露为 paper 单全被误杀。2026-09-16。
	if cfg.Risk.PositionLimit > 0 {
		riskCfg.MaxPositionPct = cfg.Risk.PositionLimit
	}
	if cfg.Risk.NetExposureLimit > 0 {
		riskCfg.MaxExposurePct = cfg.Risk.NetExposureLimit
	}
	if cfg.Risk.MaxDrawdown > 0 {
		riskCfg.MaxDrawdownPct = cfg.Risk.MaxDrawdown
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

	// 7a. DCA Manager — integrated with strategy engine and order pipeline
	ctx.DCAManager = order.NewDCAManager()
	ctx.StrategyEngine.SetDCAManager(ctx.DCAManager)
	ctx.Logger.Info("DCA manager initialized")

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

	// 15. Wire OrderManager pipeline — risk → balance lock → exchange submit → portfolio update
	ctx.wireOrderManager()

	// 15a. Start Conditional Order Engine (TP/SL/Stop-Limit monitoring)
	ctx.wireConditionalEngine()

	// 15b. Start Advanced Order Engine (Iceberg/TWAP/VWAP)
	ctx.wireAdvancedOrderEngine()

	// 16. Wire StrategyEngine — signal → order placement + protection + broadcast
	ctx.wireStrategyEngine()

	// 17. Matching Service (internal order book simulation)
	ctx.MatchingService = service.GetMatchingService()
	ctx.Logger.Info("Matching service initialized")

	// 18. Funding Fee Tracker — connects to position lifecycle
	ctx.FundingTracker = risk.NewFundingFeeTracker()
	ctx.PortfolioManager.OnPositionUpdate = func(pos model.PositionData) {
		if pos.Quantity > 0 {
			ctx.FundingTracker.TrackPosition(pos.Symbol, pos.Side, pos.Quantity, pos.AvgEntryPrice)
		} else {
			ctx.FundingTracker.UntrackPosition(pos.Symbol)
		}
	}
	// Background funding settlement (every 8 hours)
	go ctx.runFundingSettlement()
	ctx.Logger.Info("Funding fee tracker initialized")

	// 19. Metrics — register core gauges and start background updater
	ctx.initMetrics()

	ctx.started = true
	ctx.Logger.Info("All components initialized successfully")
	return nil
}

// Shutdown gracefully stops all services.
func (ctx *Context) Shutdown() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if !ctx.started {
		return
	}
	ctx.started = false

	ctx.Logger.Info("Shutting down...")

	// Stop accepting new data first
	if ctx.BinanceWS != nil {
		ctx.BinanceWS.Stop()
		ctx.Logger.Info("BinanceWS stopped")
	}

	// Stop strategy engine (stops all running strategies)
	if ctx.StrategyEngine != nil {
		ctx.StrategyEngine.StopAll()
		ctx.Logger.Info("StrategyEngine stopped")
	}

	// Stop watchdog
	if ctx.Watchdog != nil {
		ctx.Watchdog.Shutdown()
		ctx.Logger.Info("Watchdog stopped")
	}

	// Close database last
	store.CloseDB()
	ctx.Logger.Info("Database closed")

	ctx.Logger.Info("Shutdown complete")
}

// wireOrderManager connects the order lifecycle hooks to real services.
func (ctx *Context) wireOrderManager() {
	om := order.GetOrderManager()
	if om == nil {
		ctx.Logger.Warn("OrderManager not available, skipping wiring")
		return
	}

	// ── Risk Check ──
	om.RiskCheck = func(req *order.Request) error {
		riskCtx := ctx.buildRiskContext(req)
		return ctx.RiskManager.Check(riskCtx)
	}

	// ── Balance Lock (paper trading default account) ──
	om.LockBalance = func(req *order.Request) error {
		acct := ctx.PortfolioManager.GetAccount("default")
		if acct == nil {
			return fmt.Errorf("default account not found")
		}

		// ── CONTRACT (SWAP) ──
		if req.MarketType == model.MarketSwap {
			price := req.Price
			if req.OrderType == model.TypeMarket || price <= 0 {
				price = getLastPrice(req.Symbol)
			}
			notional := price * req.Quantity
			leverage := req.Leverage
			if leverage <= 0 {
				leverage = 1
			}
			margin := notional / leverage

			qb := acct.Balances["USDT"]
			if qb == nil {
				if acct.Exchange != "paper" {
					return fmt.Errorf("insufficient USDT margin")
				}
				// paper 账户兜底建余额条目，避免 nil deref（真实崩溃案例）
				qb = &model.Balance{Currency: "USDT"}
				acct.Balances["USDT"] = qb
			}
			if qb.Free < margin {
				return fmt.Errorf("insufficient margin: %.4f < %.4f USDT", qb.Free, margin)
			}
			qb.Free -= margin
			qb.Used += margin
			return nil
		}

		// ── SPOT ──
		base, quote := parseSymbolPair(req.Symbol)
		cost := req.Price * req.Quantity
		if req.OrderType == model.TypeMarket {
			cost = getLastPrice(req.Symbol) * req.Quantity
		}

		if req.Side == model.SideBuy {
			qb := acct.Balances[quote]
			if qb == nil {
				if acct.Exchange != "paper" {
					return fmt.Errorf("insufficient %s balance", quote)
				}
				qb = &model.Balance{Currency: quote}
				acct.Balances[quote] = qb
			}
			if qb.Free < cost {
				return fmt.Errorf("insufficient %s balance: %.2f < %.2f", quote, qb.Free, cost)
			}
			qb.Free -= cost
			qb.Used += cost
		} else {
			bb := acct.Balances[base]
			if bb == nil {
				if acct.Exchange != "paper" {
					return fmt.Errorf("insufficient %s balance", base)
				}
				bb = &model.Balance{Currency: base}
				acct.Balances[base] = bb
			}
			if bb.Free < req.Quantity {
				return fmt.Errorf("insufficient %s balance: %.2f < %.2f", base, bb.Free, req.Quantity)
			}
			bb.Free -= req.Quantity
			bb.Used += req.Quantity
		}
		return nil
	}

	// ── Balance Unlock ──
	om.UnlockBalance = func(ord *model.OrderData) {
		acct := ctx.PortfolioManager.GetAccount("default")
		if acct == nil {
			return
		}

		// ── CONTRACT (SWAP) ──
		if ord.MarketType == model.MarketSwap {
			price := ord.Price
			if ord.OrderType == model.TypeMarket || price <= 0 {
				price = getLastPrice(ord.Symbol)
			}
			notional := price * ord.Quantity
			leverage := ord.Leverage
			if leverage <= 0 {
				leverage = 1
			}
			margin := notional / leverage

			quote := "USDT"
			qb := acct.Balances[quote]
			if qb != nil {
				qb.Free += margin
				qb.Used -= margin
				if qb.Used < 0 {
					qb.Used = 0
				}
			}
			return
		}

		// ── SPOT ──
		base, quote := parseSymbolPair(ord.Symbol)
		cost := ord.Price * ord.Quantity
		if ord.OrderType == model.TypeMarket {
			cost = getLastPrice(ord.Symbol) * ord.Quantity
		}

		if ord.Side == model.SideBuy {
			qb := acct.Balances[quote]
			if qb != nil {
				qb.Free += cost
				qb.Used -= cost
				if qb.Used < 0 {
					qb.Used = 0
				}
			}
		} else {
			bb := acct.Balances[base]
			if bb != nil {
				bb.Free += ord.Quantity
				bb.Used -= ord.Quantity
				if bb.Used < 0 {
					bb.Used = 0
				}
			}
		}
	}

	// ── Submit to Exchange (real or paper) ──
	om.SubmitToExchange = func(ord *model.OrderData) (map[string]any, error) {
		if ord.Exchange != "paper" {
			var result map[string]any
			var err error

			switch ord.Exchange {
			case "binance":
				apiKey, secret, _ := adapter.GetCredential("binance")
				if apiKey != "" && secret != "" {
					binance := adapter.NewBinanceAdapter(apiKey, secret, false)
					if ord.MarketType == model.MarketSwap {
						result, err = binance.PlaceFuturesOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity, ord.Leverage, string(ord.PositionSide))
					} else {
						result, err = binance.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
					}
				}
			case "okx":
				apiKey, secret, passphrase := adapter.GetCredential("okx")
				if apiKey != "" && secret != "" {
					okx := adapter.NewOKXAdapter(apiKey, secret, passphrase, false)
					if ord.MarketType == model.MarketSwap {
						result, err = okx.PlaceFuturesOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity, ord.Leverage, string(ord.PositionSide))
					} else {
						result, err = okx.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
					}
				}
			case "bybit":
				apiKey, secret, _ := adapter.GetCredential("bybit")
				if apiKey != "" && secret != "" {
					bybit := adapter.NewBybitAdapter(apiKey, secret, false)
					if ord.MarketType == model.MarketSwap {
						result, err = bybit.PlaceFuturesOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity, ord.Leverage, string(ord.PositionSide))
					} else {
						result, err = bybit.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
					}
				}
			case "gate", "gateio":
				apiKey, secret, _ := adapter.GetCredential("gate")
				if apiKey != "" && secret != "" {
					gateio := adapter.NewGateIOAdapter(apiKey, secret)
					result, err = gateio.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "mexc":
				apiKey, secret, _ := adapter.GetCredential("mexc")
				if apiKey != "" && secret != "" {
					mexc := adapter.NewMEXCAdapter(apiKey, secret)
					result, err = mexc.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "bitget":
				apiKey, secret, passphrase := adapter.GetCredential("bitget")
				if apiKey != "" && secret != "" {
					bitget := adapter.NewBitgetAdapter(apiKey, secret, passphrase)
					result, err = bitget.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "coinbase":
				apiKey, secret, _ := adapter.GetCredential("coinbase")
				if apiKey != "" && secret != "" {
					coinbase := adapter.NewCoinbaseAdapter(apiKey, secret)
					result, err = coinbase.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "kraken":
				apiKey, secret, _ := adapter.GetCredential("kraken")
				if apiKey != "" && secret != "" {
					kraken := adapter.NewKrakenAdapter(apiKey, secret)
					result, err = kraken.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "alpaca":
				apiKey, secret, _ := adapter.GetCredential("alpaca")
				if apiKey != "" && secret != "" {
					alpaca := adapter.NewAlpacaAdapter(apiKey, secret, false)
					result, err = alpaca.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			}

			if err == nil && result != nil {
				ctx.Logger.Info("Order submitted to exchange", "symbol", ord.Symbol, "side", ord.Side, "exchange", ord.Exchange, "id", result["orderId"])
				return map[string]any{
					"order_id": result["orderId"],
					"status":   "NEW",
					"filled":   0.0,
					"exchange": ord.Exchange,
				}, nil
			}

			if err != nil {
				ctx.Logger.Warn("Exchange order failed, falling back to paper", "exchange", ord.Exchange, "error", err.Error())
			}
		}

		// Paper trading: simulate immediate fill
		return ctx.simulatePaperFill(ord)
	}

	// ── Cancel on Exchange ──
	om.CancelOnExchange = func(ord *model.OrderData) error {
		if ord.Exchange == "paper" {
			// For paper limit orders routed through the matching engine, cancel there too.
			if ord.OrderType == model.TypeLimit {
				return ctx.MatchingService.CancelOrder(ord.Symbol, ord.ID)
			}
			return nil
		}
		var err error
		switch ord.Exchange {
		case "binance":
			apiKey, secret, _ := adapter.GetCredential("binance")
			if apiKey != "" && secret != "" {
				binance := adapter.NewBinanceAdapter(apiKey, secret, false)
				_, err = binance.CancelOrder(ord.Symbol, ord.ID)
			}
		case "okx":
			apiKey, secret, passphrase := adapter.GetCredential("okx")
			if apiKey != "" && secret != "" {
				okx := adapter.NewOKXAdapter(apiKey, secret, passphrase, false)
				_, err = okx.CancelOrder(ord.Symbol, ord.ID)
			}
		case "bybit":
			apiKey, secret, _ := adapter.GetCredential("bybit")
			if apiKey != "" && secret != "" {
				bybit := adapter.NewBybitAdapter(apiKey, secret, false)
				_, err = bybit.CancelOrder(ord.Symbol, ord.ID)
			}
		case "gate", "gateio":
			apiKey, secret, _ := adapter.GetCredential("gate")
			if apiKey != "" && secret != "" {
				gateio := adapter.NewGateIOAdapter(apiKey, secret)
				_, err = gateio.CancelOrder(ord.Symbol, ord.ID)
			}
		case "mexc":
			apiKey, secret, _ := adapter.GetCredential("mexc")
			if apiKey != "" && secret != "" {
				mexc := adapter.NewMEXCAdapter(apiKey, secret)
				_, err = mexc.CancelOrder(ord.Symbol, ord.ID)
			}
		case "bitget":
			apiKey, secret, passphrase := adapter.GetCredential("bitget")
			if apiKey != "" && secret != "" {
				bitget := adapter.NewBitgetAdapter(apiKey, secret, passphrase)
				_, err = bitget.CancelOrder(ord.Symbol, ord.ID)
			}
		case "coinbase":
			apiKey, secret, _ := adapter.GetCredential("coinbase")
			if apiKey != "" && secret != "" {
				coinbase := adapter.NewCoinbaseAdapter(apiKey, secret)
				_, err = coinbase.CancelOrder(ord.Symbol, ord.ID)
			}
		case "kraken":
			apiKey, secret, _ := adapter.GetCredential("kraken")
			if apiKey != "" && secret != "" {
				kraken := adapter.NewKrakenAdapter(apiKey, secret)
				_, err = kraken.CancelOrder(ord.Symbol, ord.ID)
			}
		case "alpaca":
			apiKey, secret, _ := adapter.GetCredential("alpaca")
			if apiKey != "" && secret != "" {
				alpaca := adapter.NewAlpacaAdapter(apiKey, secret, false)
				_, err = alpaca.CancelOrder(ord.Symbol, ord.ID)
			}
		}
		if err != nil {
			ctx.Logger.Warn("Exchange cancel failed", "exchange", ord.Exchange, "order_id", ord.ID, "error", err.Error())
		}
		return err
	}

		// ── On Order Update ──
		om.OnOrderUpdate = func(ord *model.OrderData) {
			if ord.Status == model.StatusFilled {
				ctx.updatePortfolioFromFill(ord)
				// Record DCA entry if applicable
				if ctx.DCAManager != nil && ord.Side == model.SideBuy {
					ctx.DCAManager.RecordEntry(ord.Symbol, ord.AvgFillPrice, 0)
				}
			}
			// Publish to event bus for strategies
			ctx.EventBus.Publish(event.Event{
				Type:     event.TypeOrderUpdate,
				Symbol:   ord.Symbol,
				Data:     *ord,
				Priority: event.PrioNormal,
			})
		}

		ctx.Logger.Info("OrderManager pipeline wired")
	}

// wireConditionalEngine connects the conditional order engine to price feeds and order execution.
func (ctx *Context) wireConditionalEngine() {
	ce := order.GetConditionalEngine()

	// Price feed: Binance WS → conditional engine
	ctx.BinanceWS.SetOnPrice(func(symbol string, price float64) {
		ce.UpdatePrice(symbol, price)
	})

	// Trigger callback: log and notify
	ce.OnTrigger = func(co *order.ConditionalOrder) {
		ctx.Logger.Info("Conditional order triggered",
			"type", co.TriggerType,
			"symbol", co.Symbol,
			"trigger_price", co.TriggerPrice,
			"order_id", co.OrderData.ID)
		ctx.EventBus.Publish(event.Event{
			Type:     event.TypeRiskAlert,
			Symbol:   co.Symbol,
			Data:     fmt.Sprintf("%s triggered for %s @ %.2f", co.TriggerType, co.Symbol, co.TriggerPrice),
			Priority: event.PrioHigh,
		})
	}

	// Order placement callback: use OrderManager
	ce.OnPlaceOrder = func(ord *model.OrderData) (*model.OrderData, error) {
		req := &order.Request{
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			OrderType:     ord.OrderType,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Exchange:      ord.Exchange,
			MarketType:    ord.MarketType,
			PositionSide:  ord.PositionSide,
			Leverage:      ord.Leverage,
			MarginMode:    ord.MarginMode,
			ClosePosition: ord.ClosePosition,
		}
		return order.GetOrderManager().PlaceOrder(req)
	}

	ce.Start()
	ctx.Logger.Info("Conditional order engine started")
}

// wireAdvancedOrderEngine connects the advanced order engine to order execution.
func (ctx *Context) wireAdvancedOrderEngine() {
	ae := order.GetAdvancedOrderEngine()

	// Child order placement: use OrderManager
	ae.OnPlaceChild = func(ord *model.OrderData) (*model.OrderData, error) {
		req := &order.Request{
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			OrderType:     ord.OrderType,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Exchange:      ord.Exchange,
			MarketType:    ord.MarketType,
			PositionSide:  ord.PositionSide,
			Leverage:      ord.Leverage,
			MarginMode:    ord.MarginMode,
			ClosePosition: ord.ClosePosition,
		}
		return order.GetOrderManager().PlaceOrder(req)
	}

	// Child order cancellation
	ae.OnCancelChild = func(orderID string) error {
		_, err := order.GetOrderManager().CancelOrder(orderID, "")
		return err
	}

	ae.Start()
	ctx.Logger.Info("Advanced order engine started")
}

// simulatePaperFill simulates execution for paper trading.
// Limit orders are routed through the internal matching engine so they rest on
// the book and can match with other paper orders; market orders fall back to
// immediate fill at the last known price to preserve expected paper UX.
func (ctx *Context) simulatePaperFill(ord *model.OrderData) (map[string]any, error) {
	price := ord.Price
	if ord.OrderType == model.TypeMarket || price <= 0 {
		price = getLastPrice(ord.Symbol)
	}
	qty := ord.Quantity
	if price <= 0 || qty <= 0 {
		return nil, fmt.Errorf("invalid price/qty for paper fill")
	}

	// Route limit orders through the matching engine for realistic order-book behavior.
	if ord.OrderType == model.TypeLimit {
		result, err := ctx.MatchingService.PlaceOrder(
			ord.Symbol,
			strings.ToLower(string(ord.Side)),
			"limit",
			ord.Price,
			ord.Quantity,
			uint64(ord.UserID),
		)
		if err != nil {
			return nil, fmt.Errorf("matching engine: %w", err)
		}

		engineOrderID, _ := result["order_id"].(uint64)
		storeOrderID, _ := result["store_order_id"].(string)
		trades, _ := result["trades"].([]interface{})
		filled, _ := result["filled"].(float64)

		status := "NEW"
		if filled > 0 {
			if filled >= ord.Quantity {
				status = "FILLED"
			} else {
				status = "PARTIALLY_FILLED"
			}
		}

		return map[string]any{
			"order_id":        storeOrderID,
			"engine_order_id": engineOrderID,
			"status":          status,
			"filled":          filled,
			"price":           ord.Price,
			"trades":          trades,
			"exchange":        "paper",
		}, nil
	}

	// Market orders: immediate fill at last price (preserves legacy paper behavior).
	ord.Status = model.StatusFilled
	ord.Filled = qty
	ord.AvgFillPrice = price
	ord.UpdatedAt = time.Now().UnixMilli()

	return map[string]any{
		"order_id": ord.ID,
		"status":   "FILLED",
		"filled":   qty,
		"price":    price,
		"exchange": "paper",
	}, nil
}

// updatePortfolioFromFill updates balances and positions after a fill.
// Supports both spot and contract (swap) trading.
func (ctx *Context) updatePortfolioFromFill(ord *model.OrderData) {
	price := ord.AvgFillPrice
	if price <= 0 {
		price = ord.Price
	}
	if price <= 0 {
		price = getLastPrice(ord.Symbol)
	}
	qty := ord.Filled
	if qty <= 0 {
		qty = ord.Quantity
	}

	acct := ctx.PortfolioManager.GetAccount("default")
	if acct == nil {
		return
	}

	if ord.MarketType == model.MarketSwap {
		// ── CONTRACT (SWAP) ──
		ctx.updatePortfolioFromContractFill(ord, acct, price, qty)
	} else {
		// ── SPOT ──
		ctx.updatePortfolioFromSpotFill(ord, acct, price, qty)
	}

	// Record snapshot
	ctx.PortfolioManager.Snapshot()

	// Notify
	if ord.RealizedPnL != 0 {
		ctx.EventBus.Publish(event.Event{
			Type:     event.TypeRiskAlert,
			Symbol:   ord.Symbol,
			Data:     fmt.Sprintf("Fill: %s %s %.4f @ %.2f PnL=%.2f", ord.Symbol, ord.Side, qty, price, ord.RealizedPnL),
			Priority: event.PrioNormal,
		})
	}
}

// updatePortfolioFromSpotFill handles spot trading fill updates.
func (ctx *Context) updatePortfolioFromSpotFill(ord *model.OrderData, acct *model.AccountData, price, qty float64) {
	base, quote := parseSymbolPair(ord.Symbol)
	cost := price * qty

	// Calculate realized PnL if closing an opposite position
	var realizedPnL float64
	oppositeSide := "SELL"
	if ord.Side == model.SideSell {
		oppositeSide = "BUY"
	}
	oppositePos := acct.Positions[ord.Symbol+"-"+oppositeSide]
	if oppositePos != nil && oppositePos.Quantity > 0 {
		closeQty := qty
		if closeQty > oppositePos.Quantity {
			closeQty = oppositePos.Quantity
		}
		if ord.Side == model.SideSell {
			realizedPnL = (price - oppositePos.AvgEntryPrice) * closeQty
		} else {
			realizedPnL = (oppositePos.AvgEntryPrice - price) * closeQty
		}
		oppositePos.Quantity -= closeQty
		if oppositePos.Quantity <= 0 {
			delete(acct.Positions, ord.Symbol+"-"+oppositeSide)
			ctx.PortfolioManager.RemovePosition(ord.Symbol + "-" + oppositeSide)
		} else {
			ctx.PortfolioManager.UpdatePosition(*oppositePos)
		}
	}

	// Update balances
	if ord.Side == model.SideBuy {
		ctx.adjustBalance("default", quote, -cost)
		ctx.adjustBalance("default", base, qty)
	} else {
		ctx.adjustBalance("default", base, -qty)
		ctx.adjustBalance("default", quote, cost)
	}

	// Update or create position
	posID := ord.Symbol + "-" + string(ord.Side)
	existing := acct.Positions[posID]
	if existing != nil {
		totalQty := existing.Quantity + qty
		totalCost := existing.AvgEntryPrice*existing.Quantity + price*qty
		avgPrice := totalCost / totalQty
		existing.Quantity = totalQty
		existing.AvgEntryPrice = avgPrice
		existing.CurrentPrice = price
		existing.UnrealizedPnL = (existing.CurrentPrice - existing.AvgEntryPrice) * existing.Quantity
		if ord.Side == model.SideSell {
			existing.UnrealizedPnL = (existing.AvgEntryPrice - existing.CurrentPrice) * existing.Quantity
		}
		ctx.PortfolioManager.UpdatePosition(*existing)
	} else {
		newPos := model.PositionData{
			ID:            posID,
			Symbol:        ord.Symbol,
			Side:          string(ord.Side),
			Quantity:      qty,
			AvgEntryPrice: price,
			CurrentPrice:  price,
			UnrealizedPnL: 0,
			RealizedPnL:   realizedPnL,
			OpenedAt:      time.Now().UnixMilli(),
		}
		ctx.PortfolioManager.UpdatePosition(newPos)
	}
}

// updatePortfolioFromContractFill handles contract (swap) trading fill updates.
func (ctx *Context) updatePortfolioFromContractFill(ord *model.OrderData, acct *model.AccountData, price, qty float64) {
	notional := price * qty
	leverage := ord.Leverage
	if leverage <= 0 {
		leverage = 1
	}
	margin := notional / leverage

	positionSide := ord.PositionSide
	if positionSide == "" {
		if ord.Side == model.SideBuy {
			positionSide = model.PositionLong
		} else {
			positionSide = model.PositionShort
		}
	}

	posID := ord.Symbol + "-" + string(positionSide)
	existing := acct.Positions[posID]

	if ord.ClosePosition || (existing != nil && existing.Quantity > 0) {
		// ── CLOSE / REDUCE position ──
		if existing == nil || existing.Quantity <= 0 {
			return
		}
		closeQty := qty
		if closeQty > existing.Quantity {
			closeQty = existing.Quantity
		}

		// Realized PnL
		var realizedPnL float64
		if positionSide == model.PositionLong {
			realizedPnL = (price - existing.AvgEntryPrice) * closeQty
		} else {
			realizedPnL = (existing.AvgEntryPrice - price) * closeQty
		}
		ord.RealizedPnL = realizedPnL

		// Release margin proportionally
		releasedMargin := margin
		if closeQty < existing.Quantity {
			releasedMargin = (existing.Margin * closeQty) / existing.Quantity
		}

		// Update balance (release margin + realized PnL)
		ctx.adjustBalance("default", "USDT", releasedMargin+realizedPnL)

		// Update position
		existing.Quantity -= closeQty
		existing.RealizedPnL += realizedPnL
		existing.Margin -= releasedMargin
		if existing.Quantity <= 0 {
			existing.Quantity = 0
			existing.Margin = 0
			existing.UnrealizedPnL = 0
			delete(acct.Positions, posID)
			ctx.PortfolioManager.RemovePosition(posID)
		} else {
			existing.CurrentPrice = price
			if positionSide == model.PositionLong {
				existing.UnrealizedPnL = (existing.CurrentPrice - existing.AvgEntryPrice) * existing.Quantity
			} else {
				existing.UnrealizedPnL = (existing.AvgEntryPrice - existing.CurrentPrice) * existing.Quantity
			}
			ctx.PortfolioManager.UpdatePosition(*existing)
		}
	} else {
		// ── OPEN / INCREASE position ──
		// Deduct margin
		ctx.adjustBalance("default", "USDT", -margin)

		if existing != nil && existing.Quantity > 0 {
			// Average down/up
			totalQty := existing.Quantity + qty
			totalCost := existing.AvgEntryPrice*existing.Quantity + price*qty
			avgPrice := totalCost / totalQty
			existing.Quantity = totalQty
			existing.AvgEntryPrice = avgPrice
			existing.CurrentPrice = price
			existing.Margin += margin
			existing.Leverage = leverage
			existing.MarginMode = ord.MarginMode
			if positionSide == model.PositionLong {
				existing.UnrealizedPnL = (existing.CurrentPrice - existing.AvgEntryPrice) * existing.Quantity
			} else {
				existing.UnrealizedPnL = (existing.AvgEntryPrice - existing.CurrentPrice) * existing.Quantity
			}
			ctx.PortfolioManager.UpdatePosition(*existing)
		} else {
			// New position
			newPos := model.PositionData{
				ID:               posID,
				Symbol:           ord.Symbol,
				Side:             string(ord.Side),
				Quantity:         qty,
				AvgEntryPrice:    price,
				CurrentPrice:     price,
				UnrealizedPnL:    0,
				RealizedPnL:      0,
				OpenedAt:         time.Now().UnixMilli(),
				PositionSide:     positionSide,
				Leverage:         leverage,
				MarginMode:       ord.MarginMode,
				Margin:           margin,
				MarketType:       ord.MarketType,
				LiquidationPrice: calcLiquidationPrice(price, leverage, positionSide),
			}
			ctx.PortfolioManager.UpdatePosition(newPos)
		}
	}
}

// adjustBalance adjusts a currency balance in the default account.
func (ctx *Context) adjustBalance(accountID, currency string, delta float64) {
	acct := ctx.PortfolioManager.GetAccount(accountID)
	if acct == nil {
		return
	}
	bal := acct.Balances[currency]
	if bal == nil {
		bal = &model.Balance{Currency: currency, Total: 0, Free: 0, Used: 0}
		acct.Balances[currency] = bal
	}
	bal.Total += delta
	bal.Free += delta
	if bal.Free < 0 {
		bal.Free = 0
	}
	if bal.Total < 0 {
		bal.Total = 0
	}
}

// parseSymbolPair extracts base and quote from a symbol like "BTCUSDT".
func parseSymbolPair(symbol string) (base, quote string) {
	symbol = strings.ToUpper(symbol)
	for _, q := range []string{"USDT", "USDC", "USD", "BTC", "ETH", "BNB", "SOL"} {
		if strings.HasSuffix(symbol, q) && len(symbol) > len(q) {
			return strings.TrimSuffix(symbol, q), q
		}
	}
	return symbol, "USDT"
}

// ── Market-data liveness & offline fallbacks ──
//
// The order pipeline must stay responsive even when every external market
// data source is unreachable (dead proxy, blocked direct connection). These
// helpers resolve prices in three tiers — in-memory WS cache, short-timeout
// REST probe, synthetic anchor — and track liveness so that expensive market
// enrichment (bid/ask, volatility, funding) is skipped entirely while offline
// instead of blocking the pipeline with multi-second TCP timeouts.

var marketDataGate struct {
	okUntil   int64 // unix nano until which market data is considered fresh
	failUntil int64 // unix nano until which market data is considered unreachable
}

const (
	marketDataOKWindow   = 60 * time.Second
	marketDataFailWindow = 30 * time.Second
)

// shortTimeoutHTTPClient is used for market probes on the order path; a dead
// network must fail in ~2s, not hang on TCP retransmits for minutes.
var shortTimeoutHTTPClient = &http.Client{Timeout: 2 * time.Second}

func markMarketDataOK() {
	atomic.StoreInt64(&marketDataGate.okUntil, time.Now().Add(marketDataOKWindow).UnixNano())
}

func markMarketDataFailed() {
	now := time.Now()
	atomic.StoreInt64(&marketDataGate.failUntil, now.Add(marketDataFailWindow).UnixNano())
	atomic.StoreInt64(&marketDataGate.okUntil, now.UnixNano())
}

// marketDataOnline reports whether live market data is flowing. While offline,
// order-path enrichment skips network calls completely.
func marketDataOnline() bool {
	now := time.Now().UnixNano()
	if now < atomic.LoadInt64(&marketDataGate.failUntil) {
		return false
	}
	return now < atomic.LoadInt64(&marketDataGate.okUntil)
}

// syntheticAnchorPrices are deterministic last-resort prices per symbol, used
// only when no live market data is reachable. They keep the paper pipeline
// functional offline — same idea as the paper exchange's synthetic klines and
// the AI bot simulator's seeded random walk.
var syntheticAnchorPrices = map[string]float64{
	"BTCUSDT":  50000,
	"ETHUSDT":  3000,
	"SOLUSDT":  150,
	"BNBUSDT":  500,
	"DOGEUSDT": 0.12,
	"XRPUSDT":  0.5,
	"ADAUSDT":  0.4,
}

// normalizeMarketSymbol converts "BTC/USDT" → "BTCUSDT" for cache/HTTP lookups.
func normalizeMarketSymbol(symbol string) string {
	return strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
}

// syntheticPriceFor returns a stable fallback price for a symbol with no live
// data: a well-known anchor when available, otherwise a deterministic
// hash-derived value (stable across restarts).
func syntheticPriceFor(symbol string) float64 {
	norm := normalizeMarketSymbol(symbol)
	if p, ok := syntheticAnchorPrices[norm]; ok {
		return p
	}
	var h int64 = 5381
	for _, c := range norm {
		h = ((h << 5) + h) + int64(c)
	}
	if h < 0 {
		h = -h
	}
	return 10000.0 + float64(h%90000)
}

// getLastPrice fetches the latest price for a symbol. Resolution order:
// in-memory WS price cache (zero network), short-timeout REST probe, then a
// synthetic anchor so paper trading stays fully offline-capable. The old
// implementation used http.Get (default client, no timeout), which hung for
// minutes on a dead route and blocked paper order placement entirely.
func getLastPrice(symbol string) float64 {
	norm := normalizeMarketSymbol(symbol)

	// Tier 1: in-memory WS price cache.
	if appCtx := Get(); appCtx != nil && appCtx.BinanceWS != nil {
		if p := appCtx.BinanceWS.GetPrice(norm); p > 0 {
			markMarketDataOK()
			return p
		}
	}

	// Tier 2: short-timeout REST probe.
	resp, err := shortTimeoutHTTPClient.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + norm)
	if err == nil {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var result map[string]any
		if json.Unmarshal(raw, &result) == nil {
			if priceStr, ok := result["price"].(string); ok {
				if f, err := strconv.ParseFloat(priceStr, 64); err == nil && f > 0 {
					markMarketDataOK()
					return f
				}
			}
		}
	}
	markMarketDataFailed()

	// Tier 3: synthetic anchor.
	return syntheticPriceFor(symbol)
}

// buildRiskContext assembles a complete risk context for an order request.
// It enriches the request with live market data (bid/ask, volatility, funding
// rate) so that all risk checks can be evaluated.
func (ctx *Context) buildRiskContext(req *order.Request) *risk.Context {
	price := req.Price
	if req.OrderType == model.TypeMarket || price <= 0 {
		price = getLastPrice(req.Symbol)
	}

	// Enrich with live market data only while market data is actually
	// flowing. Offline, each of these calls burns a TCP timeout (previously
	// 30s each, run sequentially — the root cause of multi-minute paper order
	// hangs); the paper path then falls back to synthetic quotes derived from
	// the resolved price (same 0.9999/1.0001 spread as the paper exchange).
	bid, ask := 0.0, 0.0
	volatility := 0.0
	fundingRate := 0.0
	if marketDataOnline() {
		bid, ask = getBidAsk(req.Symbol)
		volatility = getVolatility(req.Symbol)
		if req.MarketType == model.MarketSwap {
			fundingRate = getFundingRate(req.Symbol)
		}
	} else if price > 0 {
		bid, ask = price*0.9999, price*1.0001
	}

	// 权益/曝险基准按订单执行目标区分：paper 单用模拟账本权益做分母（否则
	// 0.2U 真实权益会让 150U 名义的 paper 单曝险 70000%+ 被风控误杀）；
	// 实盘单用真实权益。
	equity := ctx.PortfolioManager.TotalEquity()
	netExposure := ctx.PortfolioManager.NetExposure()
	if req.Exchange == "paper" {
		if pe := ctx.PortfolioManager.PaperEquity(); pe > 0 {
			equity = pe
			netExposure = ctx.PortfolioManager.NetExposureAgainst(pe)
		}
	}

	return &risk.Context{
		Symbol:           req.Symbol,
		CurrentPrice:     price,
		OrderPrice:       price,
		OrderQuantity:    req.Quantity,
		OrderSide:        req.Side,
		TotalEquity:      equity,
		AvailableBalance: ctx.PortfolioManager.AvailableBalance(),
		PositionCount:    len(ctx.PortfolioManager.GetPositions()),
		NetExposure:      netExposure,
		MaxDrawdownPct:   ctx.PortfolioManager.Drawdown(),
		BidPrice:         bid,
		AskPrice:         ask,
		Volatility:       volatility,
		FundingRate:      fundingRate,
		Blacklist:        make(map[string]bool),

		// ── Contract fields ──
		MarketType:    req.MarketType,
		PositionSide:  req.PositionSide,
		Leverage:      req.Leverage,
		MarginMode:    req.MarginMode,
		ClosePosition: req.ClosePosition,
	}
}

// getBidAsk fetches the best bid/ask prices for a symbol from Binance public API.
func getBidAsk(symbol string) (bid, ask float64) {
	ticker, err := service.GetMarketService().GetTicker(symbol)
	if err != nil {
		markMarketDataFailed()
		return 0, 0
	}
	markMarketDataOK()
	bid = parseAnyFloat(ticker["bidPrice"])
	ask = parseAnyFloat(ticker["askPrice"])
	return
}

// getVolatility estimates recent volatility from 1-minute klines.
func getVolatility(symbol string) float64 {
	klines, err := service.GetMarketService().FetchKlines(symbol, "1m", 20)
	if err != nil || len(klines) < 2 {
		if err != nil {
			markMarketDataFailed()
		}
		return 0
	}
	markMarketDataOK()

	returns := make([]float64, len(klines)-1)
	for i := 1; i < len(klines); i++ {
		prev := parseAnyFloat(klines[i-1]["close"])
		cur := parseAnyFloat(klines[i]["close"])
		if prev > 0 {
			returns[i-1] = (cur - prev) / prev
		}
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	var sumSq float64
	for _, r := range returns {
		diff := r - mean
		sumSq += diff * diff
	}
	stddev := math.Sqrt(sumSq / float64(len(returns)))
	return stddev * 100 // percent
}

// getFundingRate fetches the current funding rate for a swap symbol.
func getFundingRate(symbol string) float64 {
	binance := adapter.NewBinanceAdapter("", "", false)
	rate, err := binance.GetFundingRate(symbol)
	if err != nil {
		markMarketDataFailed()
		return 0
	}
	markMarketDataOK()
	return rate
}

// parseAnyFloat converts string/float64 values safely.
func parseAnyFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	}
	return 0
}

// wireStrategyEngine connects strategy signals to order execution and notifications.
func (ctx *Context) wireStrategyEngine() {
	eng := ctx.StrategyEngine
	if eng == nil {
		ctx.Logger.Warn("StrategyEngine not available, skipping wiring")
		return
	}

	// ── Protection Manager ──
	eng.SetProtectionManager(protection.NewProtectionManager())

	// ── Broadcaster ──
	eng.SetBroadcaster(notify.NewBroadcaster())

	// ── WebSocket Hub ──
	eng.SetWSHub(ws.GetHub())

	// ── Signal → Order Pipeline ──
	eng.OnSignal = func(signal model.Signal) {
		// Map signal direction to order side
		var side model.OrderSide
		switch signal.Direction {
		case "LONG", "BUY", "long", "buy":
			side = model.SideBuy
		case "SHORT", "SELL", "short", "sell":
			side = model.SideSell
		case "CLOSE", "close", "EXIT", "exit":
			// Close opposite position
			ctx.closePositionFromSignal(signal)
			return
		default:
			ctx.Logger.Warn("Unknown signal direction", "direction", signal.Direction)
			return
		}

		// Determine quantity: try strategy config, then default to 10% of available USDT
		qty := ctx.resolveSignalQuantity(signal)
		if qty <= 0 {
			ctx.Logger.Warn("Signal quantity resolved to zero, skipping order", "symbol", signal.Symbol)
			return
		}

		// Use market order for signal-driven execution
		req := &order.Request{
			Symbol:    signal.Symbol,
			Side:      side,
			OrderType: model.TypeMarket,
			Price:     0,
			Quantity:  qty,
			Exchange:  ctx.resolveExchange(signal.Symbol),
		}

		// ── Contract support: load strategy config for leverage/TP/SL ──
		// 注意必须用 findStrategyConfigForSignal：signal.Strategy 是策略内部名
		// （如 "macd"）而非配置 id，GetStrategyConfig 按 id 查永远落空——这曾导致
		// execution_mode=paper 与杠杆/TP/SL 配置被静默跳过，信号单直连真实交易所。
		if cfg := findStrategyConfigForSignal(signal); cfg != nil {
			// 策略配置 execution_mode=paper 时强制 paper 撮合：dry_run 下
			// 真实交易所路径不会自动成交，信号单会永远停在 NEW。
			// 安全红线：execution_mode 为空（历史残废记录）视为 paper——
			// 空值会走凭证直连真实交易所。
			if em, _ := cfg["execution_mode"].(string); em == "" || em == "paper" {
				req.Exchange = "paper"
			}
			if cj, ok := cfg["config_json"].(string); ok && cj != "" {
				if craParams, err := cra.ParseCRAParams(cj); err == nil && craParams.IsContract() {
					req.MarketType = model.MarketSwap
					if craParams.Leverage > 0 {
						req.Leverage = craParams.Leverage
					} else {
						req.Leverage = 10
					}
					if craParams.MarginMode == "isolated" {
						req.MarginMode = model.MarginIsolated
					} else {
						req.MarginMode = model.MarginCross
					}
					switch craParams.PositionSide {
					case "SHORT":
						req.PositionSide = model.PositionShort
					case "BOTH":
						if signal.Direction == "SHORT" || signal.Direction == "short" || signal.Direction == "SELL" {
							req.PositionSide = model.PositionShort
						} else {
							req.PositionSide = model.PositionLong
						}
					default:
						req.PositionSide = model.PositionLong
					}
					// Static TP/SL: pass to order manager for conditional order tracking.
					if craParams.TPMode == "static" {
						if craParams.TakeProfitRatio > 0 {
							if req.PositionSide == model.PositionShort {
								req.TPPrice = req.Price * (1 - craParams.TakeProfitRatio)
							} else {
								req.TPPrice = req.Price * (1 + craParams.TakeProfitRatio)
							}
						}
						if craParams.StopLossEnabled {
							switch craParams.StopLossType {
							case "ratio":
								if craParams.StopLossRatio > 0 {
									if req.PositionSide == model.PositionShort {
										req.SLPrice = req.Price * (1 + craParams.StopLossRatio)
									} else {
										req.SLPrice = req.Price * (1 - craParams.StopLossRatio)
									}
								}
							case "price":
								req.SLPrice = craParams.StopLossPrice
							}
						}
					}
				}
			}
		}

		ord, err := order.GetOrderManager().PlaceOrder(req)
		if err != nil {
			ctx.Logger.Warn("Signal order failed",
				"strategy", signal.Strategy,
				"symbol", signal.Symbol,
				"side", side,
				"error", err.Error())
			return
		}

		ctx.Logger.Info("Signal order placed",
			"strategy", signal.Strategy,
			"symbol", signal.Symbol,
			"side", side,
			"qty", qty,
			"order_id", ord.ID,
			"status", ord.Status)

		// Track capital allocated by this strategy for display.
		ctx.updateStrategyCapitalFromOrder(signal.Strategy, req)

		// Publish signal to event bus for other subscribers
		ctx.EventBus.Publish(event.Event{
			Type:     event.TypeSignal,
			Symbol:   signal.Symbol,
			Data:     signal,
			Priority: event.PrioHigh,
		})
	}

	ctx.Logger.Info("StrategyEngine wired")
}

// findStrategyConfigForSignal resolves the strategy config for a signal.
// signal.Strategy carries the strategy's internal name (e.g. "macd"), not the
// config id, so the direct GetStrategyConfig lookup misses; fall back to
// matching by strategy_type (first match wins when several share a type).
func findStrategyConfigForSignal(signal model.Signal) map[string]any {
	if cfg := store.GetStrategyConfig(signal.Strategy); cfg != nil {
		return cfg
	}
	for _, cfg := range store.GetStrategyConfigs() {
		if st, _ := cfg["strategy_type"].(string); st == signal.Strategy {
			return cfg
		}
	}
	return nil
}

// resolveSignalQuantity determines the order quantity from strategy config or defaults.
func (ctx *Context) resolveSignalQuantity(signal model.Signal) float64 {
	if signal.Qty > 0 {
		return signal.Qty
	}
	// Try to find strategy config by id (signal.Strategy is the wrapped strategy id)
	configs := store.GetStrategyConfigs()
	for _, cfg := range configs {
		id, _ := cfg["id"].(string)
		if id != signal.Strategy {
			// signal.Strategy 通常是策略内部名（如 "macd"）而非配置 id，
			// 退而按 strategy_type 匹配，否则配置里的 quantity 永远不生效。
			if st, _ := cfg["strategy_type"].(string); st != signal.Strategy {
				continue
			}
		}
		// Check config_json for quantity/stake_amount
		if cj, ok := cfg["config_json"].(string); ok && cj != "" {
			var parsed map[string]any
			if json.Unmarshal([]byte(cj), &parsed) == nil {
				if v, ok := parsed["stake_amount"].(float64); ok && v > 0 {
					return v
				}
				if v, ok := parsed["quantity"].(float64); ok && v > 0 {
					return v
				}
				if v, ok := parsed["first_order_amount"].(float64); ok && v > 0 {
					return v
				}
			}
		}
	}

	// Default: 10% of available USDT balance at market price
	acct := ctx.PortfolioManager.GetAccount("default")
	if acct == nil {
		return 0.01 // absolute fallback
	}
	usdtBal := acct.Balances["USDT"]
	if usdtBal == nil || usdtBal.Free <= 0 {
		return 0.01
	}
	price := getLastPrice(signal.Symbol)
	if price <= 0 {
		return 0.01
	}
	stake := usdtBal.Free * 0.1 // 10% of free USDT
	return stake / price
}

// resolveExchange determines which exchange to use for a symbol.
func (ctx *Context) resolveExchange(symbol string) string {
	// Check if Binance credentials exist
	apiKey, secret, _ := adapter.GetCredential("binance")
	if apiKey != "" && secret != "" {
		return "binance"
	}
	return "paper"
}

// closePositionFromSignal closes the opposite position when a CLOSE signal is received.
func (ctx *Context) closePositionFromSignal(signal model.Signal) {
	acct := ctx.PortfolioManager.GetAccount("default")
	if acct == nil {
		return
	}
	// Try both LONG and SHORT positions for this symbol
	for _, side := range []model.PositionSide{model.PositionLong, model.PositionShort} {
		posID := signal.Symbol + "-" + string(side)
		pos := acct.Positions[posID]
		if pos == nil || pos.Quantity <= 0 {
			continue
		}
		// Place opposite order to close
		closeSide := model.SideSell
		if side == model.PositionShort {
			closeSide = model.SideBuy
		}
		req := &order.Request{
			Symbol:        signal.Symbol,
			Side:          closeSide,
			OrderType:     model.TypeMarket,
			Price:         0,
			Quantity:      pos.Quantity,
			Exchange:      ctx.resolveExchange(signal.Symbol),
			MarketType:    pos.MarketType,
			PositionSide:  side,
			Leverage:      pos.Leverage,
			MarginMode:    pos.MarginMode,
			ClosePosition: true,
		}
		ord, err := order.GetOrderManager().PlaceOrder(req)
		if err != nil {
			ctx.Logger.Warn("Close position order failed", "symbol", signal.Symbol, "side", side, "error", err.Error())
			continue
		}
		ctx.Logger.Info("Position closed from signal", "symbol", signal.Symbol, "side", side, "qty", pos.Quantity, "order_id", ord.ID)

		// Release displayed capital for this strategy since position is closed.
		ctx.releaseStrategyCapital(signal.Strategy)
	}
}

// updateStrategyCapitalFromOrder adds the estimated margin/cost of an opening order
// to the strategy's displayed initial_capital. This reflects real capital exposure
// once a position is opened.
func (ctx *Context) updateStrategyCapitalFromOrder(strategyID string, req *order.Request) {
	if strategyID == "" {
		return
	}
	cfg := store.GetStrategyConfig(strategyID)
	if cfg == nil {
		ctx.Logger.Warn("updateStrategyCapital: config not found", "strategy", strategyID)
		return
	}

	price := req.Price
	if price <= 0 {
		price = getLastPrice(req.Symbol)
	}
	if price <= 0 {
		ctx.Logger.Warn("updateStrategyCapital: no price", "strategy", strategyID,
			"symbol", req.Symbol, "reqPrice", req.Price)
		return
	}

	// 投入资金按名义价值口径、SET 语义维护（用户口径：1U 保证金×150 杠杆
	// =150U 投入，2026-09-16 用户明确）。不能用累加：CRA 重启后状态清零会
	// 重放首单，累加会让每次重启都虚增 +150；且持仓不归属策略，无法精确
	// 累计。平仓时 releaseStrategyCapital 归零。
	invested := price * req.Quantity

	cfg["initial_capital"] = invested
	cfg["current_equity"] = invested
	cfg["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(strategyID, cfg)
	store.PersistStrategyConfigs()
}

// releaseStrategyCapital resets displayed capital to zero when a strategy closes its position.
func (ctx *Context) releaseStrategyCapital(strategyID string) {
	if strategyID == "" {
		return
	}
	cfg := store.GetStrategyConfig(strategyID)
	if cfg == nil {
		return
	}
	cfg["initial_capital"] = 0.0
	cfg["current_equity"] = 0.0
	cfg["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(strategyID, cfg)
	store.PersistStrategyConfigs()
}

// runFundingSettlement periodically settles funding fees for tracked positions.
// Uses real funding rates from Binance futures API.
func (ctx *Context) runFundingSettlement() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if !risk.IsSettlementTime() {
			continue
		}

		positions := ctx.PortfolioManager.GetPositions()
		if len(positions) == 0 {
			continue
		}

		// Try to get real funding rates from Binance
		apiKey, secret, _ := adapter.GetCredential("binance")
		var fundingRates map[string]float64
		if apiKey != "" && secret != "" {
			binance := adapter.NewBinanceAdapter(apiKey, secret, false)
			if rates, err := binance.GetAllFundingRates(); err == nil {
				fundingRates = rates
			}
		}

		for _, pos := range positions {
			if pos.Quantity <= 0 {
				continue
			}
			currentPrice := getLastPrice(pos.Symbol)
			if currentPrice <= 0 {
				continue
			}

			// Use real funding rate if available, else fallback
			fundingRate := 0.0001 // fallback: 0.01% per 8h
			if fundingRates != nil {
				if rate, ok := fundingRates[pos.Symbol]; ok {
					fundingRate = rate
				}
			}

			ctx.FundingTracker.SettleFunding(pos.Symbol, currentPrice, fundingRate)
		}
	}
}

// initMetrics registers core application gauges and starts a background updater.
func (ctx *Context) initMetrics() {
	equityGauge := metrics.NewGauge("portfolio_equity_usdt", "Current portfolio equity in USDT")
	posGauge := metrics.NewGauge("portfolio_positions", "Number of open positions")
	stratGauge := metrics.NewGauge("strategies_active", "Number of active strategies")
	orderGauge := metrics.NewGauge("orders_pending", "Number of pending orders")

	reg := metrics.GetRegistry()
	reg.RegisterGauge(equityGauge)
	reg.RegisterGauge(posGauge)
	reg.RegisterGauge(stratGauge)
	reg.RegisterGauge(orderGauge)

	// Background updater (every 30s)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			equityGauge.Set(ctx.PortfolioManager.TotalEquity())
			posGauge.Set(float64(len(ctx.PortfolioManager.GetPositions())))
			if ctx.StrategyEngine != nil {
				stratGauge.Set(float64(len(ctx.StrategyEngine.List())))
			}
			orderGauge.Set(float64(order.GetOrderManager().ActiveOrderCount()))
		}
	}()

	ctx.Logger.Info("Metrics initialized")
}

// calcLiquidationPrice estimates the liquidation price for a contract position.
// Uses Binance-style formula: LiqPrice = EntryPrice * (1 ± 1/Leverage ± MMRate)
// where MMRate is the maintenance margin rate (varies by position size).
func calcLiquidationPrice(entryPrice, leverage float64, side model.PositionSide) float64 {
	// Binance maintenance margin rate tiers (simplified)
	// Tier 1 (0-50K): 0.4%
	// Tier 2 (50K-250K): 0.5%
	// Tier 3 (250K+): 0.65%
	mmr := 0.004 // Tier 1 default

	if side == model.PositionLong {
		return entryPrice * (1 - 1/leverage + mmr)
	}
	return entryPrice * (1 + 1/leverage - mmr)
}

// WaitForShutdown blocks until SIGINT or SIGTERM is received.
func (ctx *Context) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx.Shutdown()
}
