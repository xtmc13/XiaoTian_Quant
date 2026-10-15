package market

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// fakeBinanceKlines serves GET /api/v3/klines with 3 rows. The second-to-last
// row (the newest *closed* bar) has the openTime currently set via setClosed.
type fakeBinanceKlines struct {
	mu       sync.Mutex
	closedAt int64
	srv      *httptest.Server
}

func newFakeBinanceKlines(closedOpenTime int64) *fakeBinanceKlines {
	f := &fakeBinanceKlines{closedAt: closedOpenTime}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/klines" {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		closedAt := f.closedAt
		f.mu.Unlock()
		// Rows: [openTime, open, high, low, close, volume, closeTime, ...]
		rows := fmt.Sprintf(`[
			[%d, "100.0", "110.0", "95.0", "105.0", "10.0", %d, 0, 0, 0, 0, 0],
			[%d, "200.0", "220.0", "190.0", "210.0", "20.0", %d, 0, 0, 0, 0, 0],
			[%d, "300.0", "310.0", "290.0", "305.0", "5.0",  0, 0, 0, 0, 0, 0]
		]`, closedAt-900000, closedAt-1, closedAt, closedAt+899999, closedAt+900000)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, rows)
	}))
	return f
}

func (f *fakeBinanceKlines) setClosed(openTime int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedAt = openTime
}

func (f *fakeBinanceKlines) close() { f.srv.Close() }

// barCollector gathers Bar events published on the bus.
type barCollector struct {
	mu   sync.Mutex
	bars []model.Bar
}

func (c *barCollector) handler(evt event.Event) {
	bar, ok := evt.Data.(model.Bar)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bars = append(c.bars, bar)
}

func (c *barCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bars)
}

func (c *barCollector) snapshot() []model.Bar {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Bar, len(c.bars))
	copy(out, c.bars)
	return out
}

// waitFor polls cond until it holds or the deadline passes; returns cond().
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestKlineFeederPublishesOnlyNewClosedBars(t *testing.T) {
	const t1 = int64(1700000000000)
	const t2 = t1 + 900000 // next 15m bar

	fake := newFakeBinanceKlines(t1)
	defer fake.close()

	bus := event.NewEventBus(1000, 2)
	defer bus.Close()

	col := &barCollector{}
	bus.Subscribe("BTCUSDT", event.PrioNormal, col.handler, event.TypeBar)

	feeder := NewKlineFeeder(bus)
	feeder.BaseURL = fake.srv.URL + "/api/v3"
	feeder.Interval = 30 * time.Millisecond

	feeder.EnsureSymbol("BTCUSDT", "15m")
	defer feeder.Stop()

	// First poll back-fills the closed history (2 closed rows served by the
	// fake) for indicator warmup: chronological, baseline = newest.
	if !waitFor(func() bool { return col.count() == 2 }, 2*time.Second) {
		t.Fatalf("expected 2 backfill bars, got %d", col.count())
	}
	backfill := col.snapshot()
	if backfill[0].Time != t1-900000 || backfill[1].Time != t1 {
		t.Fatalf("backfill times = %d,%d, want %d,%d", backfill[0].Time, backfill[1].Time, t1-900000, t1)
	}
	if backfill[1].Open != 200.0 || backfill[1].High != 220.0 || backfill[1].Low != 190.0 || backfill[1].Close != 210.0 || backfill[1].Volume != 20.0 {
		t.Errorf("newest backfill OHLCV = %v/%v/%v/%v/%v, want 200/220/190/210/20",
			backfill[1].Open, backfill[1].High, backfill[1].Low, backfill[1].Close, backfill[1].Volume)
	}

	// Kline T1 closes → feeder publishes exactly one NEW bar for it.
	fake.setClosed(t2)
	if !waitFor(func() bool { return col.count() == 3 }, 2*time.Second) {
		t.Fatalf("expected 3 bar events after kline close, got %d", col.count())
	}

	bars := col.snapshot()
	b := bars[len(bars)-1]
	if b.Symbol != "BTCUSDT" {
		t.Errorf("bar.Symbol = %q, want BTCUSDT", b.Symbol)
	}
	if b.Interval != "15m" {
		t.Errorf("bar.Interval = %q, want 15m", b.Interval)
	}
	if b.Time != t2 {
		t.Errorf("bar.Time = %d, want %d (closed bar openTime)", b.Time, t2)
	}
	if b.Open != 200.0 || b.High != 220.0 || b.Low != 190.0 || b.Close != 210.0 || b.Volume != 20.0 {
		t.Errorf("bar OHLCV = %v/%v/%v/%v/%v, want 200/220/190/210/20",
			b.Open, b.High, b.Low, b.Close, b.Volume)
	}

	// Same closed bar again (no new kline): no duplicate publish.
	time.Sleep(150 * time.Millisecond)
	if got := col.count(); got != 3 {
		t.Fatalf("duplicate bar published: got %d events, want 3", got)
	}
}

func TestKlineFeederReleaseStopsFeed(t *testing.T) {
	const t1 = int64(1700000000000)

	fake := newFakeBinanceKlines(t1)
	defer fake.close()

	bus := event.NewEventBus(1000, 2)
	defer bus.Close()

	col := &barCollector{}
	bus.Subscribe("ETHUSDT", event.PrioNormal, col.handler, event.TypeBar)

	feeder := NewKlineFeeder(bus)
	feeder.BaseURL = fake.srv.URL + "/api/v3"
	feeder.Interval = 30 * time.Millisecond

	feeder.EnsureSymbol("ethusdt", "15m") // normalization: lowercase in
	time.Sleep(120 * time.Millisecond)
	if got := feeder.ActiveFeeds(); got != 1 {
		t.Fatalf("ActiveFeeds = %d, want 1", got)
	}
	if got := col.count(); got != 2 {
		t.Fatalf("backfill bars = %d, want 2", got)
	}

	feeder.ReleaseSymbol("ETHUSDT", "15m")
	if got := feeder.ActiveFeeds(); got != 0 {
		t.Fatalf("ActiveFeeds after release = %d, want 0", got)
	}

	// A closed kline appearing after release must not be published.
	fake.setClosed(t1 + 900000)
	time.Sleep(150 * time.Millisecond)
	if got := col.count(); got != 2 {
		t.Fatalf("bar published after ReleaseSymbol: got %d events, want 2", got)
	}
}

func TestKlineFeederMarketRouting(t *testing.T) {
	tests := []struct {
		name       string
		marketType string
		wantPath   string
	}{
		{name: "default spot", wantPath: "/api/v3/klines"},
		{name: "explicit spot", marketType: "spot", wantPath: "/api/v3/klines"},
		{name: "swap", marketType: "swap", wantPath: "/fapi/v1/klines"},
		{name: "futures alias", marketType: "futures", wantPath: "/fapi/v1/klines"},
		{name: "margin alias", marketType: "margin", wantPath: "/fapi/v1/klines"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `[
					[1700000000000, "100", "110", "90", "105", "10"],
					[1700000900000, "200", "220", "190", "210", "20"],
					[1700001800000, "300", "310", "290", "305", "5"]
				]`)
			}))
			defer srv.Close()

			feeder := NewKlineFeeder(nil)
			feeder.BaseURL = srv.URL + "/api/v3"
			feeder.FuturesBaseURL = srv.URL
			feeder.Interval = time.Hour
			args := []string{}
			if tc.marketType != "" {
				args = append(args, tc.marketType)
			}
			if !feeder.EnsureSymbol("BTCUSDT", "15m", args...) {
				t.Fatal("feed did not start")
			}
			select {
			case got := <-requests:
				if got != tc.wantPath {
					t.Fatalf("request path = %q, want %q", got, tc.wantPath)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no kline request")
			}
			feeder.Stop()
		})
	}
}

func TestKlineFeederSurvivesHTTPErrors(t *testing.T) {
	const t1 = int64(1700000000000)

	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, `[
			[%d, "1", "1", "1", "1", "1", %d, 0,0,0,0,0],
			[%d, "200.0", "220.0", "190.0", "210.0", "20.0", %d, 0,0,0,0,0],
			[%d, "3", "3", "3", "3", "3", 0, 0,0,0,0,0]
		]`, t1-900000, t1-1, t1, t1+899999, t1+900000)
	}))
	defer srv.Close()

	bus := event.NewEventBus(1000, 2)
	defer bus.Close()

	col := &barCollector{}
	bus.Subscribe("BTCUSDT", event.PrioNormal, col.handler, event.TypeBar)

	feeder := NewKlineFeeder(bus)
	feeder.BaseURL = srv.URL
	feeder.Interval = 30 * time.Millisecond

	feeder.EnsureSymbol("BTCUSDT", "15m")
	defer feeder.Stop()

	// Errors during baseline: feeder must keep polling, not exit.
	time.Sleep(120 * time.Millisecond)
	if got := feeder.ActiveFeeds(); got != 1 {
		t.Fatalf("feeder exited on HTTP errors: ActiveFeeds = %d, want 1", got)
	}

	// Recovery: point the feeder at a healthy fake. The first successful
	// poll triggers the backfill (2 closed bars) and sets the baseline; a
	// subsequently closed kline publishes exactly one more.
	fake := newFakeBinanceKlines(t1 + 900000)
	defer fake.close()
	feeder.BaseURL = fake.srv.URL + "/api/v3"

	if !waitFor(func() bool { return col.count() == 2 }, 2*time.Second) {
		t.Fatalf("backfill after recovery: got %d events, want 2", col.count())
	}

	fake.setClosed(t1 + 1800000)
	if !waitFor(func() bool { return col.count() == 3 }, 2*time.Second) {
		t.Fatalf("feeder did not recover/publish after errors: got %d events", col.count())
	}
	last := col.snapshot()[2]
	if last.Time != t1+1800000 {
		t.Fatalf("last bar Time = %d, want %d", last.Time, t1+1800000)
	}
}
