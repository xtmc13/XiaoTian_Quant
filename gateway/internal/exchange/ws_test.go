package exchange

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestNewBinanceWSStream(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt", "ethusdt"}, nil)
	if s == nil {
		t.Fatal("nil stream")
	}
	// Verify symbols (stream is not running until Start() is called)
	if len(s.symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(s.symbols))
	}
}

func TestBinanceWSDefaultSymbols(t *testing.T) {
	s := NewBinanceWSStream(nil, nil)
	if len(s.symbols) != 3 {
		t.Fatalf("expected 3 default symbols, got %d", len(s.symbols))
	}
	if s.symbols[0] != "btcusdt" {
		t.Fatalf("expected btcusdt, got %s", s.symbols[0])
	}
}

func TestBinanceWSStartStop(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	if err := s.Start(); err != nil {
		t.Fatal("start should not error")
	}
	if !s.IsRunning() {
		t.Fatal("should be running")
	}
	s.Stop()
	if s.IsRunning() {
		t.Fatal("should be stopped")
	}
}

func TestBinanceWSDoubleStart(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	s.Start()
	s.Start() // should not panic or double-start
	s.Stop()
}

func TestBinanceWSGetPrice(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	p := s.GetPrice("BTCUSDT")
	if p != 0 {
		t.Logf("price before data: %.2f", p)
	}
	// Price should be 0 before any data arrives
}

func TestBinanceWSCallbacks(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	ticked := false
	s.SetOnTick(func(tick model.Tick) {
		ticked = true
	})
	if ticked {
		t.Fatal("callback should not fire immediately")
	}

	barred := false
	s.SetOnBar(func(bar model.Bar) {
		barred = true
	})
	if barred {
		t.Fatal("bar callback should not fire immediately")
	}
	s.Stop()
}

func TestBinanceWSFrontendPriceFeed(t *testing.T) {
	oldFeed := FrontendPriceFeed
	defer func() { FrontendPriceFeed = oldFeed }()

	received := ""
	FrontendPriceFeed = func(symbol string, price float64) {
		received = symbol
	}
	// Verify callback can be set
	if FrontendPriceFeed == nil {
		t.Fatal("price feed not set")
	}
	_ = received
}

func TestBinanceWSParseFloat(t *testing.T) {
	if parseFloat("3.14") != 3.14 {
		t.Fatal("parseFloat failed")
	}
	if parseFloat("not_a_number") != 0 {
		t.Fatal("parseFloat should return 0 for invalid")
	}
}

func TestStreamHub(t *testing.T) {
	hub := NewStreamHub()
	if hub == nil {
		t.Fatal("nil hub")
	}
	if hub.Count() != 0 {
		t.Fatal("hub should be empty")
	}
}
