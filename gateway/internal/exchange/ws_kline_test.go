package exchange

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

func klinePayload(openTime int64, closed bool) json.RawMessage {
	k := map[string]any{
		"e": "kline", "E": openTime + 30000, "s": "BTCUSDT",
		"k": map[string]any{
			"t": openTime, "T": openTime + 59999, "s": "BTCUSDT", "i": "1m",
			"o": "100.5", "c": "101.2", "h": "102", "l": "99", "v": "12.5", "x": closed,
		},
	}
	b, _ := json.Marshal(k)
	return b
}

func TestHandleKlinePublishesClosedBarOnce(t *testing.T) {
	bus := event.NewEventBus(16, 1)
	defer bus.Close()
	ch := make(chan model.Bar, 4)
	bus.Subscribe("BTCUSDT", event.PrioNormal, func(evt event.Event) {
		if bar, ok := evt.Data.(model.Bar); ok {
			ch <- bar
		}
	}, event.TypeBar)

	s := NewBinanceWSStream([]string{"btcusdt"}, bus)
	s.handleKline(klinePayload(1700000000000, false), "btcusdt@kline_1m", "spot")

	select {
	case bar := <-ch:
		t.Fatalf("forming bar was published: %+v", bar)
	case <-time.After(50 * time.Millisecond):
	}

	s.handleKline(klinePayload(1700000000000, true), "btcusdt@kline_1m", "spot")
	select {
	case bar := <-ch:
		if bar.Symbol != "BTCUSDT" || bar.Interval != "1m" || bar.Time != 1700000000000 {
			t.Fatalf("bar = %+v", bar)
		}
		if bar.Close != 101.2 || bar.Open != 100.5 || bar.High != 102 || bar.Low != 99 {
			t.Fatalf("OHLC = %+v", bar)
		}
	case <-time.After(time.Second):
		t.Fatal("closed bar not published")
	}

	// Same candle re-delivered (reconnect/replay) must not duplicate.
	s.handleKline(klinePayload(1700000000000, true), "btcusdt@kline_1m", "spot")
	select {
	case bar := <-ch:
		t.Fatalf("duplicate bar published: %+v", bar)
	case <-time.After(50 * time.Millisecond):
	}

	// A newer candle is accepted.
	s.handleKline(klinePayload(1700000060000, true), "btcusdt@kline_1m", "spot")
	select {
	case bar := <-ch:
		if bar.Time != 1700000060000 {
			t.Fatalf("new bar time = %d", bar.Time)
		}
	case <-time.After(time.Second):
		t.Fatal("new bar not published")
	}
}

func TestStreamURLIncludesKlineSubscriptions(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	s.EnsureKline("BTCUSDT", "1m")
	s.EnsureKline("BTCUSDT", "1m") // idempotent
	url := s.streamURL()
	if !strings.Contains(url, "btcusdt@kline_1m") {
		t.Fatalf("stream url missing kline: %s", url)
	}
	if strings.Count(url, "btcusdt@kline_1m") != 1 {
		t.Fatalf("duplicate kline subscription: %s", url)
	}
}

func TestReleaseKlineRemovesSubscription(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	s.EnsureKline("BTCUSDT", "1m")
	if !strings.Contains(s.streamURL(), "btcusdt@kline_1m") {
		t.Fatal("kline missing after EnsureKline")
	}
	s.ReleaseKline("BTCUSDT", "1m")
	if strings.Contains(s.streamURL(), "btcusdt@kline_1m") {
		t.Fatal("kline still subscribed after ReleaseKline")
	}
	s.ReleaseKline("BTCUSDT", "1m") // 幂等：重复释放不 panic
}

func TestReleaseSymbolRemovesSubscription(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	s.EnsureSymbol("ethusdt")

	has := func(sym string) bool {
		for _, e := range s.symbols {
			if e == sym {
				return true
			}
		}
		return false
	}
	if !has("ethusdt") {
		t.Fatal("ethusdt missing after EnsureSymbol")
	}
	s.ReleaseSymbol("ethusdt")
	if has("ethusdt") {
		t.Fatal("ethusdt still subscribed after ReleaseSymbol")
	}
	// 初始列表不受影响。
	if !has("btcusdt") {
		t.Fatal("initial btcusdt subscription was removed")
	}
}
