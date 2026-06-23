package arbitrage

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

func initTriangularTestBus() {
	app.Get().EventBus = event.NewEventBus(1000, 2)
}

func TestNewTriangularEngine(t *testing.T) {
	cfg := DefaultTriangularEngineConfig()
	engine := NewTriangularEngine(cfg)
	if engine == nil {
		t.Fatal("expected engine")
	}
	if engine.IsRunning() {
		t.Error("expected not running")
	}
}

func TestTriangularEngineStartStop(t *testing.T) {
	initTriangularTestBus()
	cfg := DefaultTriangularEngineConfig()
	engine := NewTriangularEngine(cfg)

	if err := engine.Start(); err == nil {
		t.Error("expected error with no client")
	}

	engine.RegisterClient(&mockClient{name: "binance", price: 10000})
	if err := engine.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !engine.IsRunning() {
		t.Error("expected running")
	}

	engine.Stop()
	if engine.IsRunning() {
		t.Error("expected stopped")
	}
}

func TestWalkDepthBuy(t *testing.T) {
	ob := model.OrderBookData{
		Symbol: "BTCUSDT",
		Asks:   [][2]float64{{10000, 1}, {10100, 2}},
		Bids:   [][2]float64{{9990, 1}},
	}

	baseQty, avgPrice, slippage, filled := WalkDepth(ob, "BUY", 20000)
	if !filled {
		t.Fatal("expected filled")
	}
	if baseQty <= 0 {
		t.Errorf("expected positive base qty, got %f", baseQty)
	}
	if avgPrice <= 10000 || avgPrice >= 10100 {
		t.Errorf("expected avg price between 10000 and 10100, got %f", avgPrice)
	}
	if slippage < 0 {
		t.Errorf("expected non-negative slippage, got %f", slippage)
	}
}

func TestWalkDepthSell(t *testing.T) {
	ob := model.OrderBookData{
		Symbol: "ETHUSDT",
		Asks:   [][2]float64{{510, 1}},
		Bids:   [][2]float64{{509, 1}, {508, 2}},
	}

	quoteValue, avgPrice, slippage, filled := WalkDepth(ob, "SELL", 2)
	if !filled {
		t.Fatal("expected filled")
	}
	if quoteValue <= 0 {
		t.Errorf("expected positive quote value, got %f", quoteValue)
	}
	if avgPrice > 509 || avgPrice < 508 {
		t.Errorf("expected avg price between 508 and 509, got %f", avgPrice)
	}
	if slippage < 0 {
		t.Errorf("expected non-negative slippage, got %f", slippage)
	}
}

func TestTriangularCycleDetection(t *testing.T) {
	cfg := DefaultTriangularEngineConfig()
	cfg.Symbols = []string{"BTCUSDT", "ETHUSDT", "ETHBTC"}
	cfg.QuoteAsset = "USDT"
	cfg.MinProfitPct = 0.1
	engine := NewTriangularEngine(cfg)
	engine.RegisterClient(&mockClient{name: "binance", price: 10000})

	// Set up order books for a profitable cycle:
	// USDT -> BTC (buy BTCUSDT at 10000)
	// BTC -> ETH (buy ETHBTC at 0.049)
	// ETH -> USDT (sell ETHUSDT at 509)
	engine.stateMu.Lock()
	engine.marketState["BTCUSDT"] = &triangularMarketState{
		orderBook: model.OrderBookData{
			Symbol: "BTCUSDT",
			Asks:   [][2]float64{{10000, 10}},
			Bids:   [][2]float64{{9990, 10}},
		},
		obTime: time.Now().UnixMilli(),
	}
	engine.marketState["ETHUSDT"] = &triangularMarketState{
		orderBook: model.OrderBookData{
			Symbol: "ETHUSDT",
			Asks:   [][2]float64{{510, 100}},
			Bids:   [][2]float64{{509, 100}},
		},
		obTime: time.Now().UnixMilli(),
	}
	engine.marketState["ETHBTC"] = &triangularMarketState{
		orderBook: model.OrderBookData{
			Symbol: "ETHBTC",
			Asks:   [][2]float64{{0.049, 1000}},
			Bids:   [][2]float64{{0.048, 1000}},
		},
		obTime: time.Now().UnixMilli(),
	}
	engine.stateMu.Unlock()

	opp := engine.findBestOpportunityLocked()
	if opp == nil {
		t.Fatal("expected opportunity")
	}
	if !opp.Viable {
		t.Errorf("expected viable opportunity, got net profit %f%%", opp.NetProfitPct)
	}
	if opp.NetProfitPct <= 0 {
		t.Errorf("expected positive net profit, got %f%%", opp.NetProfitPct)
	}
	if len(opp.Cycle) != 4 {
		t.Errorf("expected cycle length 4 (including close), got %d", len(opp.Cycle))
	}
	if len(opp.Legs) != 3 {
		t.Errorf("expected 3 legs, got %d", len(opp.Legs))
	}
}

func TestTriangularCycleBelowThreshold(t *testing.T) {
	cfg := DefaultTriangularEngineConfig()
	cfg.Symbols = []string{"BTCUSDT", "ETHUSDT", "ETHBTC"}
	cfg.QuoteAsset = "USDT"
	cfg.MinProfitPct = 5.0 // high threshold
	engine := NewTriangularEngine(cfg)
	engine.RegisterClient(&mockClient{name: "binance", price: 10000})

	engine.stateMu.Lock()
	engine.marketState["BTCUSDT"] = &triangularMarketState{
		orderBook: model.OrderBookData{
			Symbol: "BTCUSDT",
			Asks:   [][2]float64{{10000, 10}},
			Bids:   [][2]float64{{9990, 10}},
		},
		obTime: time.Now().UnixMilli(),
	}
	engine.marketState["ETHUSDT"] = &triangularMarketState{
		orderBook: model.OrderBookData{
			Symbol: "ETHUSDT",
			Asks:   [][2]float64{{510, 100}},
			Bids:   [][2]float64{{509, 100}},
		},
		obTime: time.Now().UnixMilli(),
	}
	engine.marketState["ETHBTC"] = &triangularMarketState{
		orderBook: model.OrderBookData{
			Symbol: "ETHBTC",
			Asks:   [][2]float64{{0.049, 1000}},
			Bids:   [][2]float64{{0.048, 1000}},
		},
		obTime: time.Now().UnixMilli(),
	}
	engine.stateMu.Unlock()

	opp := engine.findBestOpportunityLocked()
	if opp != nil && opp.Viable {
		t.Errorf("expected no viable opportunity with high threshold, got profit %f%%", opp.NetProfitPct)
	}
}

func TestTriangularExecuteDryRun(t *testing.T) {
	cfg := DefaultTriangularEngineConfig()
	cfg.DryRun = true
	cfg.OrderSize = 10000
	engine := NewTriangularEngine(cfg)
	engine.RegisterClient(&mockClient{name: "binance", price: 10000})

	opp := arbitrageTriangularOpportunity()
	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	trade := history[0]
	if trade.Status != "dry_run" {
		t.Errorf("expected dry_run, got %s", trade.Status)
	}
	if len(trade.Legs) != 3 {
		t.Errorf("expected 3 legs, got %d", len(trade.Legs))
	}
	if trade.NetProfit <= 0 {
		t.Errorf("expected positive net profit, got %f", trade.NetProfit)
	}
}

func TestTriangularBalanceCheckInsufficient(t *testing.T) {
	cfg := DefaultTriangularEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000000 // way more than mock balance
	engine := NewTriangularEngine(cfg)
	engine.RegisterClient(&mockClient{
		name:     "binance",
		price:    10000,
		balances: []map[string]any{{"asset": "USDT", "free": "10000"}},
	})

	opp := arbitrageTriangularOpportunity()
	opp.StartQty = 1000000
	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Status != "failed" {
		t.Errorf("expected failed due to insufficient balance, got %s", history[0].Status)
	}
}

func TestTriangularGetStats(t *testing.T) {
	cfg := DefaultTriangularEngineConfig()
	cfg.DryRun = true
	engine := NewTriangularEngine(cfg)
	engine.RegisterClient(&mockClient{name: "binance", price: 10000})

	opp := arbitrageTriangularOpportunity()
	engine.Execute(opp)

	stats := engine.GetStats()
	if stats["total_trades"] != 1 {
		t.Errorf("expected 1 trade, got %v", stats["total_trades"])
	}
}

func arbitrageTriangularOpportunity() TriangularOpportunity {
	return TriangularOpportunity{
		ID:           "tri-test",
		Exchange:     "binance",
		Cycle:        []string{"USDT", "BTC", "ETH", "USDT"},
		StartAsset:   "USDT",
		StartQty:     10000,
		EndQty:       10376,
		GrossProfit:  376,
		NetProfit:    350,
		NetProfitPct: 3.5,
		TotalFees:    26,
		Viable:       true,
		Timestamp:    time.Now().UnixMilli(),
		Legs: []TriangularLeg{
			{Symbol: "BTCUSDT", Side: "BUY", OrderType: "MARKET", Price: 10000, ExecutablePrice: 10000, Quantity: 10000, FilledQty: 10000, Status: "pending"},
			{Symbol: "ETHBTC", Side: "BUY", OrderType: "MARKET", Price: 0.049, ExecutablePrice: 0.049, Quantity: 1, FilledQty: 1, Status: "pending"},
			{Symbol: "ETHUSDT", Side: "SELL", OrderType: "MARKET", Price: 509, ExecutablePrice: 509, Quantity: 20.4, FilledQty: 20.4, Status: "pending"},
		},
	}
}
