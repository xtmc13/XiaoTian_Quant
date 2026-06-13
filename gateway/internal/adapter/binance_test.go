package adapter

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

/* ── Adapter Creation ────────────────────────────────────────── */

func TestBinanceAdapterName(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)
	btAssertEq(t, b.Name(), "binance", "adapter name")
}

func TestBinanceAdapterTestnet(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	btAssert(t, b.testnet, "should be testnet")
	btAssertEq(t, b.wsURL(), BinanceTestWsURL, "testnet WS URL")
}

func TestBinanceAdapterProduction(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)
	btAssert(t, !b.testnet, "should be production")
	btAssertEq(t, b.wsURL(), BinanceWsURL, "production WS URL")
}

func TestBinanceAdapterIsConnected(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)
	btAssert(t, !b.wsConnected, "should not be connected initially")
}

func TestBinanceAdapterStartStop(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	err := b.Start()
	btAssert(t, err == nil, "start should not error")

	err = b.Stop()
	btAssert(t, err == nil, "stop should not error")
}

func TestBinanceStreamHubInitialized(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)
	btAssert(t, b.streamHub != nil, "streamHub should be initialized")
	btAssertEq(t, b.streamHub.Count(), 0, "no streams initially")
}

/* ── Callback Registration ───────────────────────────────────── */

func TestBinanceCallbacks(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	b.OnTicker(func(tick model.Tick) {})
	b.OnOrderBook(func(ob model.OrderBookData) {})
	b.OnTrade(func(trade model.TradeData) {})
	b.OnKline(func(bar model.Bar) {})
	// Should not panic
}

/* ── REST — GetKlines ────────────────────────────────────────── */

func TestBinanceGetKlines(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	klines, err := b.GetKlines("BTCUSDT", "1h", 10)
	// May error due to no network; just assert no panic
	_ = klines
	_ = err
}

/* ── REST — GetTicker ────────────────────────────────────────── */

func TestBinanceGetTicker(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.GetTicker("BTCUSDT")
	_ = err
}

/* ── REST — GetBalance ───────────────────────────────────────── */

func TestBinanceGetBalance(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.GetBalance()
	_ = err
}

/* ── REST — PlaceOrder ───────────────────────────────────────── */

func TestBinancePlaceOrder(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	// Testnet may succeed or fail; just ensure no panic
	_ = err
}

/* ── REST — CancelOrder ──────────────────────────────────────── */

func TestBinanceCancelOrder(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.CancelOrder("BTCUSDT", "fake-id")
	// Testnet may succeed or fail; just ensure no panic
	_ = err
}

/* ── REST — GetOrders (not exposed by BinanceAdapter) ─────────── */
/* ── REST — GetOpenOrders ────────────────────────────────────── */

func TestBinanceGetOpenOrders(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.GetOpenOrders("BTCUSDT")
	_ = err
}

/* ── REST — GetPositions ─────────────────────────────────────── */

func TestBinanceGetPositions(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	pos, err := b.GetPositions()
	// GetPositions returns nil, nil — should not panic
	if err == nil {
		btAssert(t, pos == nil || len(pos) == 0, "no positions")
	}
}

/* ── REST — GetFuturesPositions ──────────────────────────────── */

func TestBinanceGetFuturesPositions(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.GetFuturesPositions()
	_ = err
}

/* ── REST — PlaceFuturesOrder ────────────────────────────────── */

func TestBinancePlaceFuturesOrder(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", true)
	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.01, 10, "LONG")
	_ = err
}

/* ── WebSocket — Ticker Stream Message ───────────────────────── */

func TestBinanceHandleStreamMessage_Ticker(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	var received model.Tick
	b.OnTicker(func(tick model.Tick) {
		received = tick
	})

	msg := `{
		"stream": "btcusdt@ticker",
		"data": {
			"s": "BTCUSDT",
			"c": "50000",
			"b": "49900",
			"a": "50100",
			"q": "250000000"
		}
	}`
	b.handleStreamMessage([]byte(msg))

	btAssert(t, received.Symbol == "BTCUSDT", "symbol should be BTCUSDT")
	btAssertEq(t, received.Bid, 49900.0, "bid price")
	btAssertEq(t, received.Ask, 50100.0, "ask price")
	btAssertEq(t, received.Last, 50000.0, "last price")
	btAssertEq(t, received.Volume, 250000000.0, "quote volume")
}

/* ── WebSocket — Trade Stream Message ─────────────────────────── */

func TestBinanceHandleStreamMessage_Trade(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	var received model.TradeData
	b.OnTrade(func(trade model.TradeData) {
		received = trade
	})

	msg := `{
		"stream": "btcusdt@trade",
		"data": {
			"s": "BTCUSDT",
			"t": 123456,
			"p": "50000",
			"q": "0.01",
			"m": false
		}
	}`
	b.handleStreamMessage([]byte(msg))

	btAssert(t, received.Symbol == "BTCUSDT", "symbol should be BTCUSDT")
	btAssertEq(t, received.Price, 50000.0, "price")
	btAssertEq(t, received.Quantity, 0.01, "quantity")
	btAssertEq(t, received.Side, "BUY", "side should be BUY")
}

func TestBinanceHandleStreamMessage_TradeSell(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	var received model.TradeData
	b.OnTrade(func(trade model.TradeData) {
		received = trade
	})

	msg := `{
		"stream": "btcusdt@trade",
		"data": {
			"t": 123456,
			"p": "50000",
			"q": "0.01",
			"m": true
		}
	}`
	b.handleStreamMessage([]byte(msg))

	btAssertEq(t, received.Side, "SELL", "side should be SELL when m=true")
}

/* ── WebSocket — Depth Stream Message ────────────────────────── */

func TestBinanceHandleStreamMessage_Depth(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	var received model.OrderBookData
	b.OnOrderBook(func(ob model.OrderBookData) {
		received = ob
	})

	msg := `{
		"stream": "btcusdt@depth20@100ms",
		"data": {
			"s": "BTCUSDT",
			"bids": [["50000", "1.5"], ["49999", "2.0"]],
			"asks": [["50001", "1.0"], ["50002", "0.5"]]
		}
	}`
	b.handleStreamMessage([]byte(msg))

	btAssert(t, received.Symbol == "BTCUSDT", "symbol should be BTCUSDT")
	btAssertEq(t, len(received.Bids), 2, "2 bid levels")
	btAssertEq(t, len(received.Asks), 2, "2 ask levels")
	btAssertEq(t, received.Bids[0][0], 50000.0, "first bid price")
	btAssertEq(t, received.Bids[0][1], 1.5, "first bid qty")
	btAssertEq(t, received.Asks[0][0], 50001.0, "first ask price")
}

/* ── WebSocket — Kline Stream Message ────────────────────────── */

func TestBinanceHandleStreamMessage_Kline(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	var received model.Bar
	b.OnKline(func(bar model.Bar) {
		received = bar
	})

	msg := `{
		"stream": "btcusdt@kline_1m",
		"data": {
			"s": "BTCUSDT",
			"k": {
				"t": 1690000000000,
				"o": "50000",
				"h": "50100",
				"l": "49900",
				"c": "50050",
				"v": "100"
			}
		}
	}`
	b.handleStreamMessage([]byte(msg))

	btAssert(t, received.Symbol == "BTCUSDT", "symbol should be BTCUSDT")
	btAssertEq(t, received.Open, 50000.0, "open")
	btAssertEq(t, received.High, 50100.0, "high")
	btAssertEq(t, received.Low, 49900.0, "low")
	btAssertEq(t, received.Close, 50050.0, "close")
	btAssertEq(t, received.Volume, 100.0, "volume")
}

/* ── WebSocket — User Stream ─────────────────────────────────── */

func TestBinanceHandleUserStreamMessage_ExecutionReport(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)

	msg := `{
		"e": "executionReport",
		"s": "BTCUSDT",
		"i": 1,
		"p": "50000",
		"q": "0.01",
		"x": "TRADE",
		"X": "FILLED",
		"r": "NONE",
		"o": "LIMIT",
		"z": "0.01"
	}`
	b.handleUserStreamMessage([]byte(msg))

	// Verify order was stored in internal map
	btAssert(t, len(b.orders) == 1, "order should be stored in orders map")
}

/* ── WebSocket — Error Handling ──────────────────────────────── */

func TestBinanceHandleStreamMessage_InvalidJSON(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)
	b.handleStreamMessage([]byte(`{invalid`))
	b.handleUserStreamMessage([]byte(`{invalid`))
}

func TestBinanceHandleStreamMessage_EmptyMessage(t *testing.T) {
	b := NewBinanceAdapter("key", "secret", false)
	b.handleStreamMessage([]byte(`{}`))
}

/* ── REST — GetFuturesAccount ────────────────────────────────── */
