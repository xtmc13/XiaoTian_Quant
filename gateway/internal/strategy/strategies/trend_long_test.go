package strategies

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func makeBars(prices []float64) []model.Bar {
	bars := make([]model.Bar, len(prices))
	for i, p := range prices {
		bars[i] = model.Bar{
			Symbol: "BTCUSDT",
			Open:   p,
			High:   p,
			Low:    p,
			Close:  p,
			Volume: 1,
			Time:   int64(i) * 60_000,
		}
	}
	return bars
}

func TestTrendLongStrategy_OnBar_InsufficientBars(t *testing.T) {
	s := NewTrendLongStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	bars := makeBars(make([]float64, 10))
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil {
			t.Fatalf("expected no signal with insufficient bars, got %v", sig)
		}
	}
}

func TestTrendLongStrategy_OnBar_GoldenCrossEntersLong(t *testing.T) {
	s := NewTrendLongStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Flat low, then a sustained much higher plateau to force EMA12 above EMA26.
	prices := make([]float64, 60)
	for i := 0; i < 30; i++ {
		prices[i] = 100
	}
	for i := 30; i < 60; i++ {
		prices[i] = 200
	}

	bars := makeBars(prices)
	var longSig *model.Signal
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil && sig.Direction == "LONG" {
			longSig = sig
		}
	}

	if longSig == nil {
		t.Fatal("expected LONG signal after golden cross, got none")
	}
	if longSig.Symbol != "BTCUSDT" {
		t.Errorf("expected symbol BTCUSDT, got %s", longSig.Symbol)
	}
	if longSig.Strategy != "trend_long" {
		t.Errorf("expected strategy trend_long, got %s", longSig.Strategy)
	}
}

func TestTrendLongStrategy_OnBar_DeathCrossClosesLong(t *testing.T) {
	s := NewTrendLongStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Flat low (enter), then flat high, then flat low again (exit).
	prices := make([]float64, 90)
	for i := 0; i < 30; i++ {
		prices[i] = 100
	}
	for i := 30; i < 60; i++ {
		prices[i] = 200
	}
	for i := 60; i < 90; i++ {
		prices[i] = 100
	}

	bars := makeBars(prices)
	var longSig, closeSig *model.Signal
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig == nil {
			continue
		}
		switch sig.Direction {
		case "LONG":
			longSig = sig
		case "CLOSE":
			closeSig = sig
		default:
			t.Fatalf("unexpected signal direction %s", sig.Direction)
		}
	}

	if longSig == nil {
		t.Fatal("expected LONG signal, got none")
	}
	if closeSig == nil {
		t.Fatal("expected CLOSE signal after death cross, got none")
	}
}

func TestTrendLongStrategy_OnBar_NoShortEntry(t *testing.T) {
	s := NewTrendLongStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Flat high then flat low: death cross occurs while flat, but we never entered long.
	prices := make([]float64, 60)
	for i := 0; i < 30; i++ {
		prices[i] = 200
	}
	for i := 30; i < 60; i++ {
		prices[i] = 100
	}

	bars := makeBars(prices)
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil {
			t.Fatalf("expected no signal without prior long position, got %v", sig)
		}
	}
}

func TestTrendLongStrategy_OnBar_NoDoubleEntry(t *testing.T) {
	s := NewTrendLongStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Flat low, step up to enter, then keep flat high (no exit, no re-entry).
	prices := make([]float64, 80)
	for i := 0; i < 30; i++ {
		prices[i] = 100
	}
	for i := 30; i < 80; i++ {
		prices[i] = 200
	}

	bars := makeBars(prices)
	longCount := 0
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil && sig.Direction == "LONG" {
			longCount++
		}
	}

	if longCount != 1 {
		t.Fatalf("expected exactly one LONG signal, got %d", longCount)
	}
}

func TestTrendLongStrategy_ApplyParams(t *testing.T) {
	s := NewTrendLongStrategy()
	if err := s.Start(map[string]any{
		"symbol":      "ETHUSDT",
		"fast_period": 10,
		"slow_period": 30,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	if s.Symbol() != "ETHUSDT" {
		t.Errorf("expected symbol ETHUSDT, got %s", s.Symbol())
	}
	if s.fastPeriod != 10 {
		t.Errorf("expected fast_period 10, got %d", s.fastPeriod)
	}
	if s.slowPeriod != 30 {
		t.Errorf("expected slow_period 30, got %d", s.slowPeriod)
	}
}

func TestTrendLongStrategy_InvalidParamsRejected(t *testing.T) {
	s := NewTrendLongStrategy()
	err := s.Start(map[string]any{
		"symbol":      "BTCUSDT",
		"fast_period": 50,
		"slow_period": 20,
	})
	if err == nil {
		t.Fatal("expected error when fast_period >= slow_period")
	}
}
