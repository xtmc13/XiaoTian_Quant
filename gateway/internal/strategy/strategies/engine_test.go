package strategies

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func assert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func assertNoErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", msg, err)
	}
}

func assertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func newTestBus() *event.EventBus {
	return event.NewEventBus(100, 2)
}

/* ── Breakout Strategy Tests ─────────────────────────────────── */

func TestBreakoutNoSignalInsufficientBars(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{"lookback": float64(20)})

	// Only 10 bars — need lookback+2 = 22
	for i := 0; i < 10; i++ {
		bar := model.Bar{Symbol: "BTCUSDT", Close: 50000 + float64(i)*10, High: 50100, Low: 49900}
		sig, err := s.OnBar(bar, bus)
		assertNoErr(t, err, "OnBar should not error")
		if sig != nil {
			t.Fatalf("expected nil signal with insufficient bars, got %+v", sig)
		}
	}
}

// feedRangeBars feeds N bars in a tight range to fill the breakout strategy's buffer.
func feedRangeBars(s *BreakoutStrategy, bus *event.EventBus, n int, basePrice float64) {
	for i := 0; i < n; i++ {
		offset := float64(i) * 10.0
		s.OnBar(model.Bar{
			Symbol:   "BTCUSDT",
			Open:     basePrice + offset,
			High:     basePrice + offset + 60,
			Low:      basePrice + offset - 60,
			Close:    basePrice + offset + 30,
			Volume:   100,
			Time:     int64(i) * 3600000,
		}, bus)
	}
}

func TestBreakoutLongSignal(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{
		"lookback":   float64(5),
		"buffer_pct": float64(0.001),
	})

	// Feed 7 bars (need lookback+2=7 minimum) in a tight range around 49000-49100
	feedRangeBars(s, bus, 7, 49000)

	// Now a strong breakout bar above the range
	entrySig, err := s.OnBar(model.Bar{
		Symbol: "BTCUSDT", Open: 49500, High: 51200, Low: 49400, Close: 51000,
		Volume: 500, Time: 7 * 3600000,
	}, bus)
	assertNoErr(t, err, "OnBar should not error")
	if entrySig == nil {
		t.Fatal("expected LONG signal on breakout bar")
	}
	assertEq(t, entrySig.Direction, "LONG", "direction")
	assertEq(t, entrySig.Strategy, "breakout", "strategy name")
}

func TestBreakoutShortSignal(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{
		"lookback":   float64(5),
		"buffer_pct": float64(0.001),
	})

	feedRangeBars(s, bus, 7, 50000)

	// Breakdown below support
	entrySig, err := s.OnBar(model.Bar{
		Symbol: "BTCUSDT", Open: 49800, High: 49900, Low: 47500, Close: 47800,
		Volume: 500, Time: 7 * 3600000,
	}, bus)
	assertNoErr(t, err, "OnBar should not error")
	if entrySig == nil {
		t.Fatal("expected SHORT signal on breakdown bar")
	}
	assertEq(t, entrySig.Direction, "SHORT", "direction")
}

func TestBreakoutNoSignalInRange(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{
		"lookback":   float64(5),
		"buffer_pct": float64(0.02), // large buffer
	})

	// Feed 15 bars staying within tight range
	for i := 0; i < 15; i++ {
		bar := model.Bar{
			Symbol: "BTCUSDT",
			Open:   50000 + float64(i)*5,
			High:   50050 + float64(i)*5,
			Low:    49950 + float64(i)*5,
			Close:  50000 + float64(i)*5,
			Time:   int64(i) * 3600000,
		}
		sig, err := s.OnBar(bar, bus)
		assertNoErr(t, err, "OnBar should not error")
		if sig != nil {
			t.Fatalf("bar %d: expected nil signal, got %+v", i, sig)
		}
	}
}

func TestBreakoutLongTakeProfit(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{
		"lookback":        float64(5),
		"buffer_pct":      float64(0.001),
		"stop_loss_pct":   float64(0.10),  // far away
		"take_profit_pct": float64(0.03),  // 3% profit target
	})

	// Fill buffer and trigger LONG entry at ~49200
	feedRangeBars(s, bus, 7, 49000)
	entryBar := model.Bar{
		Symbol: "BTCUSDT", Open: 49200, High: 53000, Low: 49100, Close: 52800,
		Volume: 1000, Time: 7 * 3600000,
	}
	entrySig, _ := s.OnBar(entryBar, bus)
	if entrySig == nil {
		t.Fatal("entry signal should fire")
	}

	// Price rises to 3% above the entry closing price, take profit triggers
	// entry close was 52800, 3% above = 54384
	exitSig, err := s.OnBar(model.Bar{
		Symbol: "BTCUSDT", Open: 53000, High: 54600, Low: 52900, Close: 54500,
		Volume: 800, Time: 8 * 3600000,
	}, bus)
	assertNoErr(t, err, "OnBar should not error")
	if exitSig == nil {
		t.Fatal("expected CLOSE signal on take profit")
	}
	assertEq(t, exitSig.Direction, "CLOSE", "exit direction")
	assertEq(t, exitSig.Reason, "long take profit", "exit reason")
}

func TestBreakoutLongStopLoss(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{
		"lookback":        float64(5),
		"buffer_pct":      float64(0.001),
		"stop_loss_pct":   float64(0.02),   // 2% stop
		"take_profit_pct": float64(0.20),   // far away
	})

	// Trigger LONG entry
	feedRangeBars(s, bus, 7, 49000)
	entryBar := model.Bar{
		Symbol: "BTCUSDT", Open: 49200, High: 51500, Low: 49100, Close: 51000,
		Volume: 1000, Time: 7 * 3600000,
	}
	s.OnBar(entryBar, bus)

	// Stop loss: price drops 5% below entry (entry close=51000, 2% below = 49980)
	exitSig, err := s.OnBar(model.Bar{
		Symbol: "BTCUSDT", Open: 50500, High: 50600, Low: 49600, Close: 49700,
		Volume: 800, Time: 8 * 3600000,
	}, bus)
	assertNoErr(t, err, "OnBar should not error")
	if exitSig == nil {
		t.Fatal("expected CLOSE signal on stop loss")
	}
	assertEq(t, exitSig.Direction, "CLOSE", "exit direction")
	assertEq(t, exitSig.Reason, "long stop loss", "exit reason")
}

func TestBreakoutShortTakeProfit(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	err := s.Start(map[string]any{
		"lookback":        float64(5),
		"buffer_pct":      float64(0.001),
		"stop_loss_pct":   float64(0.20),   // far away
		"take_profit_pct": float64(0.03),   // 3% profit
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Trigger SHORT entry (breakdown)
	feedRangeBars(s, bus, 7, 50000)
	entryBar := model.Bar{
		Symbol: "BTCUSDT", Open: 49800, High: 49900, Low: 47500, Close: 47800,
		Volume: 1000, Time: 7 * 3600000,
	}
	entrySig, err := s.OnBar(entryBar, bus)
	if err != nil {
		t.Fatalf("OnBar error: %v", err)
	}
	if entrySig == nil {
		t.Fatalf("entry signal should fire for short. running=%v lookback=%d bufferPct=%f bars=%d",
			s.IsRunning(), s.lookback, s.bufferPct, len(s.bars))
	}

	// Take profit: price drops 3% below entry (entry close=47800, 3% below = 46366)
	exitSig, err := s.OnBar(model.Bar{
		Symbol: "BTCUSDT", Open: 46700, High: 46800, Low: 46100, Close: 46200,
		Volume: 800, Time: 8 * 3600000,
	}, bus)
	assertNoErr(t, err, "OnBar should not error")
	if exitSig == nil {
		t.Fatal("expected CLOSE signal on short take profit")
	}
	assertEq(t, exitSig.Direction, "CLOSE", "exit direction")
	assertEq(t, exitSig.Reason, "short take profit", "exit reason")
}

func TestBreakoutShortStopLoss(t *testing.T) {
	s := NewBreakoutStrategy()
	bus := newTestBus()

	s.Start(map[string]any{
		"lookback":        float64(5),
		"buffer_pct":      float64(0.001),
		"stop_loss_pct":   float64(0.02),    // 2% stop
		"take_profit_pct": float64(0.20),    // far away
	})

	// Trigger SHORT entry (breakdown)
	feedRangeBars(s, bus, 7, 50000)
	entryBar := model.Bar{
		Symbol: "BTCUSDT", Open: 49800, High: 49900, Low: 47500, Close: 47800,
		Volume: 1000, Time: 7 * 3600000,
	}
	s.OnBar(entryBar, bus)

	// Stop loss: price rises 2% above entry (entry close=47800, 2% above = 48756)
	exitSig, err := s.OnBar(model.Bar{
		Symbol: "BTCUSDT", Open: 48500, High: 49100, Low: 48400, Close: 48900,
		Volume: 800, Time: 8 * 3600000,
	}, bus)
	assertNoErr(t, err, "OnBar should not error")
	if exitSig == nil {
		t.Fatal("expected CLOSE signal on short stop loss")
	}
	assertEq(t, exitSig.Reason, "short stop loss", "exit reason")
}

func TestBreakoutStartStopLifecycle(t *testing.T) {
	s := NewBreakoutStrategy()

	assert(t, !s.IsRunning(), "should not be running initially")

	err := s.Start(map[string]any{"lookback": float64(10)})
	assertNoErr(t, err, "start should succeed")
	assert(t, s.IsRunning(), "should be running after start")

	err = s.Stop()
	assertNoErr(t, err, "stop should succeed")
	assert(t, !s.IsRunning(), "should not be running after stop")
}

func TestBreakoutParams(t *testing.T) {
	s := NewBreakoutStrategy()

	params := s.Params()
	assertEq(t, params["symbol"].(string), "BTCUSDT", "default symbol")
	assertEq(t, params["lookback"].(int), 20, "default lookback")

	s.Start(map[string]any{
		"lookback":        float64(30),
		"buffer_pct":      float64(0.005),
		"stop_loss_pct":   float64(0.03),
		"take_profit_pct": float64(0.06),
		"position_size":   float64(1000),
	})

	params = s.Params()
	assertEq(t, params["lookback"].(int), 30, "updated lookback")
	assertEq(t, params["buffer_pct"].(float64), float64(0.005), "updated buffer")
}

/* ── Arbitrage Strategy Tests ────────────────────────────────── */

func TestArbitrageNoSignalNormalSpread(t *testing.T) {
	s := NewArbitrageStrategy()
	bus := newTestBus()

	s.Start(map[string]any{"min_spread_pct": float64(0.5)})

	tick := model.Tick{Symbol: "BTCUSDT", Bid: 50000.0, Ask: 50050.0, Last: 50025.0}
	sig, err := s.OnTick(tick, bus)
	assertNoErr(t, err, "OnTick should not error")
	if sig != nil {
		t.Fatalf("expected nil signal for 0.1%% spread, got %+v", sig)
	}
}

func TestArbitrageSignalOnLargeSpread(t *testing.T) {
	s := NewArbitrageStrategy()
	bus := newTestBus()

	s.Start(map[string]any{"min_spread_pct": float64(0.5)})

	tick := model.Tick{Symbol: "BTCUSDT", Bid: 49500.0, Ask: 50000.0, Last: 49750.0} // ~1% spread
	sig, err := s.OnTick(tick, bus)
	assertNoErr(t, err, "OnTick should not error")
	if sig == nil {
		t.Fatal("expected arbitrage signal for large spread")
	}
	assertEq(t, sig.Direction, "LONG", "arbitrage direction")
	assertEq(t, sig.Strategy, "arbitrage", "strategy name")
}

func TestArbitrageNoSignalWhenStopped(t *testing.T) {
	s := NewArbitrageStrategy()
	bus := newTestBus()

	s.Start(map[string]any{"min_spread_pct": float64(0.1)})
	tick := model.Tick{Symbol: "BTCUSDT", Bid: 49000.0, Ask: 50000.0, Last: 49500.0}
	sig, _ := s.OnTick(tick, bus)
	assert(t, sig != nil, "should get signal when running")

	s.Stop()
	sig2, err := s.OnTick(tick, bus)
	assertNoErr(t, err, "OnTick should not error when stopped")
	if sig2 != nil {
		t.Fatalf("expected nil signal when stopped, got %+v", sig2)
	}
}

func TestArbitrageZeroPrices(t *testing.T) {
	s := NewArbitrageStrategy()
	bus := newTestBus()

	s.Start(map[string]any{"min_spread_pct": float64(0.1)})
	tick := model.Tick{Symbol: "BTCUSDT", Bid: 0, Ask: 0, Last: 0}
	sig, err := s.OnTick(tick, bus)
	assertNoErr(t, err, "OnTick should not error")
	if sig != nil {
		t.Fatalf("expected nil signal for zero prices, got %+v", sig)
	}
}

/* ── Event Bus Tests ─────────────────────────────────────────── */

func TestEventBusSubscribeAndDispatch(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	// Use a channel with buffer to avoid blocking the worker
	received := make(chan model.Bar, 1)
	bus.Subscribe("BTCUSDT", event.PrioNormal, func(evt event.Event) {
		if bar, ok := evt.Data.(model.Bar); ok {
			select {
			case received <- bar:
			default:
			}
		}
	}, event.TypeBar)

	bar := model.Bar{Symbol: "BTCUSDT", Close: 50000.0, Time: 12345}
	bus.PublishBlocking(event.Event{
		Type:   event.TypeBar,
		Symbol: "BTCUSDT",
		Data:   bar,
	})

	// Event bus processes asynchronously — wait briefly
	time.Sleep(20 * time.Millisecond)

	select {
	case got := <-received:
		assertEq(t, got.Close, 50000.0, "bar close price")
	default:
		t.Fatal("expected to receive bar event within timeout")
	}
}

func TestEventBusSymbolFiltering(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	btcCh := make(chan struct{}, 1)
	ethCh := make(chan struct{}, 1)

	bus.Subscribe("BTCUSDT", event.PrioNormal, func(evt event.Event) {
		select {
		case btcCh <- struct{}{}:
		default:
		}
	}, event.TypeBar)

	bus.Subscribe("ETHUSDT", event.PrioNormal, func(evt event.Event) {
		select {
		case ethCh <- struct{}{}:
		default:
		}
	}, event.TypeBar)

	bus.PublishBlocking(event.Event{
		Type:   event.TypeBar,
		Symbol: "BTCUSDT",
		Data:   model.Bar{Symbol: "BTCUSDT"},
	})

	time.Sleep(20 * time.Millisecond)

	select {
	case <-btcCh:
		// expected
	default:
		t.Fatal("BTC subscriber should have received event")
	}
	select {
	case <-ethCh:
		t.Fatal("ETH subscriber should NOT have received BTC event")
	default:
		// expected
	}
}

func TestEventBusSubscriberCount(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	assertEq(t, bus.SubscriberCount(), 0, "should have 0 subscribers")

	id := bus.Subscribe("BTCUSDT", event.PrioNormal, func(evt event.Event) {}, event.TypeBar)
	assertEq(t, bus.SubscriberCount(), 1, "should have 1 subscriber")

	bus.Unsubscribe(id)
	assertEq(t, bus.SubscriberCount(), 0, "should have 0 after unsubscribe")
}
