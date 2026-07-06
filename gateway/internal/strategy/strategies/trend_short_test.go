package strategies

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestTrendShortStrategy_OnBar_InsufficientBars(t *testing.T) {
	s := NewTrendShortStrategy()
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

func TestTrendShortStrategy_OnBar_DeathCrossEntersShort(t *testing.T) {
	s := NewTrendShortStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	prices := make([]float64, 60)
	for i := 0; i < 30; i++ {
		prices[i] = 200
	}
	for i := 30; i < 60; i++ {
		prices[i] = 100
	}

	bars := makeBars(prices)
	var shortSig *model.Signal
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil && sig.Direction == "SHORT" {
			shortSig = sig
		}
	}

	if shortSig == nil {
		t.Fatal("expected SHORT signal after death cross, got none")
	}
	if shortSig.Symbol != "BTCUSDT" {
		t.Errorf("expected symbol BTCUSDT, got %s", shortSig.Symbol)
	}
	if shortSig.Strategy != "trend_short" {
		t.Errorf("expected strategy trend_short, got %s", shortSig.Strategy)
	}
}

func TestTrendShortStrategy_OnBar_GoldenCrossClosesShort(t *testing.T) {
	s := NewTrendShortStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	prices := make([]float64, 90)
	for i := 0; i < 30; i++ {
		prices[i] = 200
	}
	for i := 30; i < 60; i++ {
		prices[i] = 100
	}
	for i := 60; i < 90; i++ {
		prices[i] = 200
	}

	bars := makeBars(prices)
	var shortSig, closeSig *model.Signal
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig == nil {
			continue
		}
		switch sig.Direction {
		case "SHORT":
			shortSig = sig
		case "CLOSE":
			closeSig = sig
		default:
			t.Fatalf("unexpected signal direction %s", sig.Direction)
		}
	}

	if shortSig == nil {
		t.Fatal("expected SHORT signal, got none")
	}
	if closeSig == nil {
		t.Fatal("expected CLOSE signal after golden cross, got none")
	}
}

func TestTrendShortStrategy_OnBar_NoLongEntry(t *testing.T) {
	s := NewTrendShortStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	prices := make([]float64, 60)
	for i := 0; i < 30; i++ {
		prices[i] = 100
	}
	for i := 30; i < 60; i++ {
		prices[i] = 200
	}

	bars := makeBars(prices)
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil {
			t.Fatalf("expected no signal without prior short position, got %v", sig)
		}
	}
}

func TestTrendShortStrategy_OnBar_NoDoubleEntry(t *testing.T) {
	s := NewTrendShortStrategy()
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	prices := make([]float64, 80)
	for i := 0; i < 30; i++ {
		prices[i] = 200
	}
	for i := 30; i < 80; i++ {
		prices[i] = 100
	}

	bars := makeBars(prices)
	shortCount := 0
	for _, bar := range bars {
		sig, err := s.OnBar(bar, nil)
		if err != nil {
			t.Fatalf("on bar: %v", err)
		}
		if sig != nil && sig.Direction == "SHORT" {
			shortCount++
		}
	}

	if shortCount != 1 {
		t.Fatalf("expected exactly one SHORT signal, got %d", shortCount)
	}
}

func TestTrendShortStrategy_ApplyParams(t *testing.T) {
	s := NewTrendShortStrategy()
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

func TestTrendShortStrategy_InvalidParamsRejected(t *testing.T) {
	s := NewTrendShortStrategy()
	err := s.Start(map[string]any{
		"symbol":      "BTCUSDT",
		"fast_period": 50,
		"slow_period": 20,
	})
	if err == nil {
		t.Fatal("expected error when fast_period >= slow_period")
	}
}
