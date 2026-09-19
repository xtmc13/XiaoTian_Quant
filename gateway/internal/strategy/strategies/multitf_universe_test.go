package strategies

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// staticProvider 是 BarProvider 的测试替身：按 symbol+tf 返回固定序列。
type staticProvider struct {
	series map[string][]model.Bar
}

func (p *staticProvider) key(symbol, tf string) string { return symbol + "|" + tf }
func (p *staticProvider) GetBar(symbol, tf string) (model.Bar, bool) {
	s := p.series[p.key(symbol, tf)]
	if len(s) == 0 {
		return model.Bar{}, false
	}
	return s[len(s)-1], true
}
func (p *staticProvider) GetSeries(symbol, tf string) []model.Bar {
	return p.series[p.key(symbol, tf)]
}

// risingSeries 构造一段持续上涨的高周期序列（收盘在 EMA60 之上）。
func risingSeries(symbol, tf string, n int, startPrice, step float64) []model.Bar {
	bars := make([]model.Bar, n)
	price := startPrice
	for i := 0; i < n; i++ {
		price += step
		bars[i] = model.Bar{Symbol: symbol, Interval: tf, Open: price, High: price, Low: price, Close: price, Time: int64(i) * 3600_000}
	}
	return bars
}

func trendTriggerPrices() []float64 {
	prices := make([]float64, 60)
	for i := 0; i < 30; i++ {
		prices[i] = 100
	}
	for i := 30; i < 60; i++ {
		prices[i] = 200
	}
	return prices
}

func TestTrendLongMultiTimeframeBlocksCountertrendEntry(t *testing.T) {
	s := NewTrendLongStrategy()
	err := s.Start(map[string]any{
		"symbol":     "BTCUSDT",
		"timeframe":  "1h",
		"timeframes": "4h",
		"schedule":   "@every 4h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.PrimaryTimeframe(); got != "1h" {
		t.Fatalf("expected primary tf 1h, got %q", got)
	}
	tfs := s.Timeframes()
	if len(tfs) != 1 || tfs[0] != "4h" {
		t.Fatalf("expected extra tf [4h], got %v", tfs)
	}
	if s.Schedule() != "@every 4h" {
		t.Fatalf("expected schedule, got %q", s.Schedule())
	}

	// 高周期下跌序列（收盘在 EMA60 下方）→ higherTrendUp=false
	falling := make([]model.Bar, 80)
	price := 200.0
	for i := range falling {
		price -= 1
		falling[i] = model.Bar{Symbol: "BTCUSDT", Interval: "4h", Open: price, High: price, Low: price, Close: price, Time: int64(i) * 3600_000}
	}
	s.SetBarProvider(&staticProvider{series: map[string][]model.Bar{"BTCUSDT|4h": falling}})

	// 主周期金叉：被高周期趋势过滤，不应进场
	var sig *model.Signal
	for _, bar := range makeBars(trendTriggerPrices()) {
		sig, _ = s.OnBar(bar, nil)
	}
	if sig != nil && sig.Direction == "LONG" {
		t.Fatal("expected multi-timeframe filter to block countertrend long")
	}

	// 高周期上涨 → 允许进场
	s2 := NewTrendLongStrategy()
	if err := s2.Start(map[string]any{"symbol": "BTCUSDT", "timeframe": "1h", "timeframes": []any{"4h"}}); err != nil {
		t.Fatal(err)
	}
	s2.SetBarProvider(&staticProvider{series: map[string][]model.Bar{
		"BTCUSDT|4h": risingSeries("BTCUSDT", "4h", 80, 100, 1),
	}})
	var longSig *model.Signal
	for _, bar := range makeBars(trendTriggerPrices()) {
		if sig, _ := s2.OnBar(bar, nil); sig != nil && longSig == nil {
			longSig = sig
		}
	}
	if longSig == nil || longSig.Direction != "LONG" {
		t.Fatalf("expected long entry with higher-timeframe uptrend, got %v", longSig)
	}

	// OnSchedule 趋势守卫：大级别转跌时应平多
	falling2 := risingSeries("BTCUSDT", "4h", 80, 100, 1)
	for i := range falling2 {
		falling2[i].Close = 1 // 砸到 EMA60 之下
		falling2[i].Open = 1
		falling2[i].High = 1
		falling2[i].Low = 1
	}
	s2.SetBarProvider(&staticProvider{series: map[string][]model.Bar{"BTCUSDT|4h": falling2}})
	closeSig, err := s2.OnSchedule(time.Now(), &event.EventBus{})
	_ = err
	if closeSig == nil || closeSig.Direction != "CLOSE" {
		t.Fatalf("expected scheduled guard close, got %v", closeSig)
	}
}

func TestTrendShortMultiTimeframe(t *testing.T) {
	s := NewTrendShortStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "timeframe": "1h", "timeframes": "4h"}); err != nil {
		t.Fatal(err)
	}
	// 高周期上涨 → higherTrendDown=false → 死叉被过滤
	s.SetBarProvider(&staticProvider{series: map[string][]model.Bar{
		"BTCUSDT|4h": risingSeries("BTCUSDT", "4h", 80, 100, 1),
	}})
	// 主周期高位转跌制造死叉
	prices := make([]float64, 60)
	for i := 0; i < 30; i++ {
		prices[i] = 200
	}
	for i := 30; i < 60; i++ {
		prices[i] = 100
	}
	var sig *model.Signal
	for _, bar := range makeBars(prices) {
		sig, _ = s.OnBar(bar, nil)
	}
	if sig != nil && sig.Direction == "SHORT" {
		t.Fatal("expected multi-timeframe filter to block short against higher uptrend")
	}
}

// ctxProvider 构造 UniverseContext 用的 BarProvider 替身。
type ctxProvider map[string][]model.Bar

func (p ctxProvider) GetBar(symbol, tf string) (model.Bar, bool) {
	s := p[symbol+"|"+tf]
	if len(s) == 0 {
		return model.Bar{}, false
	}
	return s[len(s)-1], true
}
func (p ctxProvider) GetSeries(symbol, tf string) []model.Bar { return p[symbol+"|"+tf] }

func TestUniverseRotationOnUniverseRanking(t *testing.T) {
	s := NewUniverseRotationStrategy()
	err := s.Start(map[string]any{
		"symbol":        "BTCUSDT",
		"timeframe":     "1h",
		"symbols":       "BTCUSDT,ETHUSDT,SOLUSDT",
		"top_n":         2,
		"momentum_bars": 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.UniverseRefreshBars(); got != 12 {
		t.Fatalf("expected default refresh 12, got %d", got)
	}
	wl := s.Watchlist()
	if len(wl) != 3 {
		t.Fatalf("expected watchlist of 3, got %v", wl)
	}

	// ETH 动量最强、SOL 其次、BTC 最弱 → universe 应为 [BTC(保底), ETH]
	mk := func(sym string, gain float64) []model.Bar {
		bars := make([]model.Bar, 11)
		base := 100.0
		for i := range bars {
			p := base
			if i == 10 {
				p = base * (1 + gain)
			}
			bars[i] = model.Bar{Symbol: sym, Interval: "1h", Open: p, High: p, Low: p, Close: p, Time: int64(i)}
		}
		return bars
	}
	provider := ctxProvider{
		"BTCUSDT|1h": mk("BTCUSDT", 0.01),
		"ETHUSDT|1h": mk("ETHUSDT", 0.08),
		"SOLUSDT|1h": mk("SOLUSDT", 0.05),
	}
	ctx := strategy.NewUniverseContext(time.Now(), []string{"BTCUSDT"}, 24, provider)
	next := s.OnUniverse(ctx)
	// top_n=2（ETH/SOL）+ 主 symbol 保底 → 3 个
	if len(next) != 3 {
		t.Fatalf("expected top2 + primary fallback universe, got %v", next)
	}
	// 主 symbol 保底在列
	if next[0] != "BTCUSDT" && next[1] != "BTCUSDT" {
		t.Fatalf("expected primary symbol in universe, got %v", next)
	}
	foundETH := false
	for _, sym := range next {
		if sym == "ETHUSDT" {
			foundETH = true
		}
	}
	if !foundETH {
		t.Fatalf("expected ETH (best momentum) selected, got %v", next)
	}

	s.mu.Lock()
	s.current = map[string]bool{"BTCUSDT": true, "ETHUSDT": true}
	s.barHist["ETHUSDT"] = mk("ETHUSDT", 0.05)
	s.mu.Unlock()

	// ETH 在 universe 且动量为正 → 进场信号
	bars := s.barHist["ETHUSDT"]
	entryBar := bars[len(bars)-1]
	sig, err := s.OnBar(entryBar, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Direction != "LONG" || sig.Symbol != "ETHUSDT" {
		t.Fatalf("expected LONG on ETHUSDT, got %v", sig)
	}

	// 动量转负 → 平仓信号
	s.mu.Lock()
	hist := s.barHist["ETHUSDT"]
	down := append([]model.Bar(nil), hist...)
	down[len(down)-1].Close = down[0].Close * 0.9
	s.barHist["ETHUSDT"] = down
	s.mu.Unlock()
	sig, _ = s.OnBar(down[len(down)-1], nil)
	if sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("expected CLOSE on momentum flip, got %v", sig)
	}

	// 移出 universe → 平仓并清理历史（仓位在仓时）
	s.mu.Lock()
	delete(s.current, "ETHUSDT")
	s.inPosture["ETHUSDT"] = true
	s.inPosture["SOLUSDT"] = true
	s.barHist["SOLUSDT"] = mk("SOLUSDT", 0.01)
	s.mu.Unlock()
	sig, _ = s.OnBar(model.Bar{Symbol: "ETHUSDT", Interval: "1h", Close: 100, Time: 999}, nil)
	if sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("expected CLOSE when rotated out, got %v", sig)
	}
	if len(s.barHist["ETHUSDT"]) != 0 {
		t.Fatal("expected ETH history cleared after removal")
	}
}

var _ = event.EventBus{}
