package market

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// defaultBinanceRestURL matches adapter.BinanceRestURL. Like the adapter,
// BINANCE_REST_URL env overrides it (TUN/proxy deployments).
const defaultBinanceRestURL = "https://api.binance.com/api/v3"

// DefaultKlineFeedInterval is how often a symbol+interval pair is polled.
const DefaultKlineFeedInterval = 20 * time.Second

// KlineFeeder polls Binance REST klines and publishes a model.Bar event onto
// the event bus each time a new fully-closed kline appears. Binance WS pushes
// no real kline stream to the strategy bus, so bar-consuming strategies
// (OnBar) would starve without this feeder.
//
// Publish shape matches the rest of the codebase (see exchange.BinanceWSStream
// and strategy.Engine.dispatch): Event{Type: TypeBar, Symbol: <UPPERCASE>,
// Data: model.Bar} — engine subscribers are keyed by exact symbol string.
type KlineFeeder struct {
	// Bus is the event bus to publish on. Required.
	Bus *event.EventBus
	// BaseURL is the Binance REST base, e.g. https://api.binance.com/api/v3.
	// Defaults to BINANCE_REST_URL env or the mainnet URL.
	BaseURL string
	// Interval is the poll period per symbol+interval. Defaults to
	// DefaultKlineFeedInterval.
	Interval time.Duration

	httpClient *http.Client

	mu      sync.Mutex
	running map[string]*feedEntry // key: SYMBOL|interval -> entry
}

type feedEntry struct {
	stop     chan struct{}
	done     chan struct{}
	refs     int
	lastOpen int64 // openTime (ms) of the newest closed bar already published
}

// NewKlineFeeder creates a feeder publishing to bus.
func NewKlineFeeder(bus *event.EventBus) *KlineFeeder {
	return &KlineFeeder{
		Bus:        bus,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		running:    make(map[string]*feedEntry),
	}
}

func (f *KlineFeeder) baseURL() string {
	if f.BaseURL != "" {
		return f.BaseURL
	}
	if env := os.Getenv("BINANCE_REST_URL"); env != "" {
		return env
	}
	return defaultBinanceRestURL
}

func (f *KlineFeeder) interval() time.Duration {
	if f.Interval > 0 {
		return f.Interval
	}
	return DefaultKlineFeedInterval
}

func feedKey(symbol, interval string) string {
	return symbol + "|" + interval
}

// EnsureSymbol starts (or refs) a poll loop for symbol+interval. Idempotent:
// repeated calls for the same pair share one goroutine; each call must be
// paired with one ReleaseSymbol. The first successful poll back-publishes up
// to 100 recently closed bars (oldest→newest) for indicator warmup, then
// only newly closed bars are published.
func (f *KlineFeeder) EnsureSymbol(symbol, interval string) {
	symbol = normalizeFeedSymbol(symbol)
	if symbol == "" {
		return
	}
	interval = strings.ToLower(strings.TrimSpace(interval))
	if interval == "" {
		interval = "15m"
	}

	key := feedKey(symbol, interval)

	f.mu.Lock()
	if e, ok := f.running[key]; ok {
		e.refs++
		f.mu.Unlock()
		return
	}
	e := &feedEntry{stop: make(chan struct{}), done: make(chan struct{}), refs: 1}
	f.running[key] = e
	f.mu.Unlock()

	go f.run(symbol, interval, e)
}

// ReleaseSymbol drops one ref for symbol+interval; the poll loop stops when
// the last ref is released. Blocks until the goroutine has fully exited, so
// no further events for the pair are published once it returns.
func (f *KlineFeeder) ReleaseSymbol(symbol, interval string) {
	symbol = normalizeFeedSymbol(symbol)
	interval = strings.ToLower(strings.TrimSpace(interval))
	if interval == "" {
		interval = "15m"
	}
	key := feedKey(symbol, interval)

	f.mu.Lock()
	e, ok := f.running[key]
	if !ok {
		f.mu.Unlock()
		return
	}
	e.refs--
	if e.refs > 0 {
		f.mu.Unlock()
		return
	}
	delete(f.running, key)
	close(e.stop)
	f.mu.Unlock()

	<-e.done
}

// Stop releases every active feed. Safe to call multiple times.
func (f *KlineFeeder) Stop() {
	f.mu.Lock()
	entries := make([]*feedEntry, 0, len(f.running))
	for key, e := range f.running {
		entries = append(entries, e)
		delete(f.running, key)
	}
	f.mu.Unlock()

	for _, e := range entries {
		close(e.stop)
	}
	for _, e := range entries {
		<-e.done
	}
}

// ActiveFeeds returns the number of running symbol+interval poll loops.
func (f *KlineFeeder) ActiveFeeds() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.running)
}

func (f *KlineFeeder) run(symbol, interval string, e *feedEntry) {
	defer close(e.done)

	ticker := time.NewTicker(f.interval())
	defer ticker.Stop()

	first := true
	for {
		select {
		case <-e.stop:
			return
		default:
		}

		bar, err := f.fetchClosedBar(symbol, interval)
		if err != nil {
			log.Printf("[KlineFeeder] fetch %s %s failed: %v", symbol, interval, err)
		} else {
			// Entry state (lastOpen/first) is single-goroutine; no lock needed.
			if first {
				first = false
				// 指标暖机回补：启动时发布最近最多 100 根已闭合历史 K 线
				// （旧→新），让 MACD 等指标策略立刻有完整计算窗口，不必
				// 等数小时收齐；基线设为最新一根，之后只发新闭合 K 线。
				bars, berr := f.fetchClosedBars(symbol, interval, 100)
				if berr != nil {
					log.Printf("[KlineFeeder] backfill %s %s failed: %v", symbol, interval, berr)
					e.lastOpen = bar.Time
				} else if len(bars) == 0 {
					e.lastOpen = bar.Time
				} else {
					for _, b := range bars {
						select {
						case <-e.stop:
							return
						default:
						}
						if f.Bus != nil {
							// PublishSync 保证回补序列按旧→新顺序送达
							// （Publish 的多 worker 队列会并发乱序完成）。
							f.Bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: symbol, Data: b})
						}
					}
					e.lastOpen = bars[len(bars)-1].Time
					log.Printf("[KlineFeeder] backfilled %d bars for %s %s (baseline openTime=%d)",
						len(bars), symbol, interval, e.lastOpen)
				}
			} else if bar.Time > e.lastOpen {
				e.lastOpen = bar.Time
				select {
				case <-e.stop:
					return
				default:
				}
				if f.Bus != nil {
					f.Bus.PublishSync(event.Event{
						Type:   event.TypeBar,
						Symbol: symbol,
						Data:   *bar,
					})
				}
			}
		}

		select {
		case <-e.stop:
			return
		case <-ticker.C:
		}
	}
}

// fetchClosedBar fetches the last 3 klines and converts the second-to-last
// one (the newest fully closed bar; the last element is still forming).
func (f *KlineFeeder) fetchClosedBar(symbol, interval string) (*model.Bar, error) {
	bars, err := f.fetchClosedBars(symbol, interval, 3)
	if err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("binance klines: no closed rows")
	}
	return &bars[len(bars)-1], nil
}

// fetchClosedBars fetches up to limit klines and returns all fully closed
// ones in chronological order; the newest fetched row is still forming and
// is excluded.
func (f *KlineFeeder) fetchClosedBars(symbol, interval string, limit int) ([]model.Bar, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", strconv.Itoa(limit))

	u, _ := url.Parse(f.baseURL() + "/klines")
	u.RawQuery = params.Encode()

	resp, err := f.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance klines status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("binance klines: only %d rows", len(raw))
	}

	closed := raw[:len(raw)-1] // newest row is still forming
	bars := make([]model.Bar, 0, len(closed))
	for _, k := range closed {
		b, err := parseKlineRow(k, symbol, interval)
		if err != nil {
			return nil, err
		}
		bars = append(bars, b)
	}
	return bars, nil
}

func parseKlineRow(k []any, symbol, interval string) (model.Bar, error) {
	if len(k) < 6 {
		return model.Bar{}, fmt.Errorf("binance klines: short row %v", k)
	}

	openTime, err := toInt64(k[0])
	if err != nil {
		return model.Bar{}, err
	}
	open, err := toFloat(k[1])
	if err != nil {
		return model.Bar{}, err
	}
	high, err := toFloat(k[2])
	if err != nil {
		return model.Bar{}, err
	}
	low, err := toFloat(k[3])
	if err != nil {
		return model.Bar{}, err
	}
	closePrice, err := toFloat(k[4])
	if err != nil {
		return model.Bar{}, err
	}
	volume, err := toFloat(k[5])
	if err != nil {
		return model.Bar{}, err
	}

	return model.Bar{
		Symbol:   symbol,
		Open:     open,
		High:     high,
		Low:      low,
		Close:    closePrice,
		Volume:   volume,
		Interval: interval,
		Time:     openTime, // kline open time, ms (model.Bar has no closeTime field)
	}, nil
}

// normalizeFeedSymbol uppercases and trims, and converts common separator
// forms ("BTC/USDT") to the bare Binance form ("BTCUSDT").
func normalizeFeedSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func toFloat(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case string:
		return strconv.ParseFloat(t, 64)
	case json.Number:
		return t.Float64()
	default:
		return 0, fmt.Errorf("unexpected numeric type %T (%v)", v, v)
	}
}

func toInt64(v any) (int64, error) {
	switch t := v.(type) {
	case float64:
		return int64(t), nil
	case string:
		return strconv.ParseInt(t, 10, 64)
	case json.Number:
		return t.Int64()
	default:
		return 0, fmt.Errorf("unexpected int type %T (%v)", v, v)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
