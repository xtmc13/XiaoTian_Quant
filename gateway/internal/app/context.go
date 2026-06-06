package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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
		price := req.Price
		if req.OrderType == model.TypeMarket || price <= 0 {
			price = getLastPrice(req.Symbol)
		}
		riskCtx := &risk.Context{
			Symbol:           req.Symbol,
			CurrentPrice:     price,
			OrderPrice:       price,
			OrderQuantity:    req.Quantity,
			OrderSide:        req.Side,
			TotalEquity:      ctx.PortfolioManager.TotalEquity(),
			AvailableBalance: ctx.PortfolioManager.AvailableBalance(),
			PositionCount:    len(ctx.PortfolioManager.GetPositions()),
			NetExposure:      ctx.PortfolioManager.NetExposure(),
			MaxDrawdownPct:   ctx.PortfolioManager.Drawdown(),
			Blacklist:        make(map[string]bool),
		}
		return ctx.RiskManager.Check(riskCtx)
	}

	// ── Balance Lock (paper trading default account) ──
	om.LockBalance = func(req *order.Request) error {
		acct := ctx.PortfolioManager.GetAccount("default")
		if acct == nil {
			return fmt.Errorf("default account not found")
		}
		base, quote := parseSymbolPair(req.Symbol)
		cost := req.Price * req.Quantity
		if req.OrderType == model.TypeMarket {
			cost = getLastPrice(req.Symbol) * req.Quantity
		}

		if req.Side == model.SideBuy {
			qb := acct.Balances[quote]
			if qb == nil || qb.Free < cost {
				return fmt.Errorf("insufficient %s balance: %.2f < %.2f", quote, qb.Free, cost)
			}
			qb.Free -= cost
			qb.Used += cost
		} else {
			bb := acct.Balances[base]
			if bb == nil || bb.Free < req.Quantity {
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
					result, err = binance.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "okx":
				apiKey, secret, passphrase := adapter.GetCredential("okx")
				if apiKey != "" && secret != "" {
					okx := adapter.NewOKXAdapter(apiKey, secret, passphrase, false)
					result, err = okx.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "bybit":
				apiKey, secret, _ := adapter.GetCredential("bybit")
				if apiKey != "" && secret != "" {
					bybit := adapter.NewBybitAdapter(apiKey, secret, false)
					result, err = bybit.PlaceOrder(ord.Symbol, string(ord.Side), string(ord.OrderType), ord.Price, ord.Quantity)
				}
			case "gateio":
				apiKey, secret, _ := adapter.GetCredential("gateio")
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
		case "gateio":
			apiKey, secret, _ := adapter.GetCredential("gateio")
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

// simulatePaperFill simulates immediate execution for paper trading.
func (ctx *Context) simulatePaperFill(ord *model.OrderData) (map[string]any, error) {
	price := ord.Price
	if ord.OrderType == model.TypeMarket || price <= 0 {
		price = getLastPrice(ord.Symbol)
	}
	qty := ord.Quantity
	if price <= 0 || qty <= 0 {
		return nil, fmt.Errorf("invalid price/qty for paper fill")
	}

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
func (ctx *Context) updatePortfolioFromFill(ord *model.OrderData) {
	base, quote := parseSymbolPair(ord.Symbol)
	price := ord.AvgFillPrice
	if price <= 0 {
		price = ord.Price
	}
	if price <= 0 {
		price = getLastPrice(ord.Symbol)
	}
	qty := ord.Filled
	cost := price * qty

	acct := ctx.PortfolioManager.GetAccount("default")
	if acct == nil {
		return
	}

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

	// Record snapshot
	ctx.PortfolioManager.Snapshot()

	// Notify
	if realizedPnL != 0 {
		ctx.EventBus.Publish(event.Event{
			Type:     event.TypeRiskAlert,
			Symbol:   ord.Symbol,
			Data:     fmt.Sprintf("Paper fill: %s %s %.4f @ %.2f PnL=%.2f", ord.Symbol, ord.Side, qty, price, realizedPnL),
			Priority: event.PrioNormal,
		})
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

// getLastPrice fetches the latest price for a symbol from Binance public API.
func getLastPrice(symbol string) float64 {
	var result map[string]any
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	json.Unmarshal(raw, &result)
	if priceStr, ok := result["price"].(string); ok {
		f, _ := strconv.ParseFloat(priceStr, 64)
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

// resolveSignalQuantity determines the order quantity from strategy config or defaults.
func (ctx *Context) resolveSignalQuantity(signal model.Signal) float64 {
	// Try to find strategy config
	configs := store.GetStrategyConfigs()
	for _, cfg := range configs {
		name, _ := cfg["name"].(string)
		if name != signal.Strategy {
			continue
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
	// Try both BUY and SELL positions for this symbol
	for _, side := range []string{"BUY", "SELL"} {
		posID := signal.Symbol + "-" + side
		pos := acct.Positions[posID]
		if pos == nil || pos.Quantity <= 0 {
			continue
		}
		// Place opposite order to close
		closeSide := model.SideSell
		if side == "SELL" {
			closeSide = model.SideBuy
		}
		req := &order.Request{
			Symbol:    signal.Symbol,
			Side:      closeSide,
			OrderType: model.TypeMarket,
			Price:     0,
			Quantity:  pos.Quantity,
			Exchange:  ctx.resolveExchange(signal.Symbol),
		}
		ord, err := order.GetOrderManager().PlaceOrder(req)
		if err != nil {
			ctx.Logger.Warn("Close position order failed", "symbol", signal.Symbol, "side", side, "error", err.Error())
			continue
		}
		ctx.Logger.Info("Position closed from signal", "symbol", signal.Symbol, "side", side, "qty", pos.Quantity, "order_id", ord.ID)
	}
}

// runFundingSettlement periodically settles funding fees for tracked positions.
func (ctx *Context) runFundingSettlement() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if !risk.IsSettlementTime() {
			continue
		}
		positions := ctx.PortfolioManager.GetPositions()
		for _, pos := range positions {
			if pos.Quantity <= 0 {
				continue
			}
			// Fetch funding rate from Binance (simplified: use 0 for now)
			// In production, this should call the exchange's funding rate API
			currentPrice := getLastPrice(pos.Symbol)
			if currentPrice <= 0 {
				continue
			}
			fundingRate := 0.0001 // placeholder: 0.01% per 8h
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

// WaitForShutdown blocks until SIGINT or SIGTERM is received.
func (ctx *Context) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx.Shutdown()
}
