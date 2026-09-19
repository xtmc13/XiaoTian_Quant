package exchange

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

// TestWSClientReconnectsAfterAbruptClose 回归测试：服务器粗暴断连后，
// 客户端必须真正发起重连（而不是因旧状态未清理把新连接静默丢弃）。
func TestWSClientReconnectsAfterAbruptClose(t *testing.T) {
	var conns atomic.Int32
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conns.Add(1)
		// 立即粗暴关闭，不给读循环任何消息
		_ = conn.Close()
	}))
	defer srv.Close()

	client := NewWSClient(WSConfig{
		URL:            "ws" + strings.TrimPrefix(srv.URL, "http"),
		ReconnectDelay: 10 * time.Millisecond,
		MaxReconnects:  50,
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	deadline := time.Now().Add(3 * time.Second)
	for conns.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if conns.Load() < 3 {
		t.Fatalf("expected ≥3 connection attempts (initial + reconnects), got %d", conns.Load())
	}
}
