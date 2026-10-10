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

// ── M6（2026-10-10）：动态订阅 ──

// TestBinanceWSEnsureSymbol：新 symbol 并入订阅表（去重、大小写归一），
// 未连接时不发帧；streamURL 含动态加入的 symbol（重连后订阅保持）。
func TestBinanceWSEnsureSymbol(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	defer s.Stop()

	s.EnsureSymbol("PEPEUSDT")
	if s.SymbolCount() != 2 {
		t.Fatalf("symbols = %d, want 2", s.SymbolCount())
	}
	// 去重（大小写不敏感）。
	s.EnsureSymbol("pepeusdt")
	s.EnsureSymbol("BTCUSDT")
	if s.SymbolCount() != 2 {
		t.Fatalf("dedup failed: symbols = %d, want 2", s.SymbolCount())
	}
	// 空值不订阅。
	s.EnsureSymbol("  ")
	if s.SymbolCount() != 2 {
		t.Fatalf("empty symbol must be ignored: %d", s.SymbolCount())
	}
	// 重连 URL 携带动态订阅的 symbol。
	url := s.streamURL()
	for _, want := range []string{"pepeusdt@trade", "pepeusdt@ticker", "pepeusdt@markPrice@1s", "btcusdt@trade"} {
		if !strings.Contains(url, want) {
			t.Fatalf("streamURL missing %s: %s", want, url)
		}
	}
}

// TestBinanceWSEnsureSymbolLiveSubscribe：连接中的会话立即收到 SUBSCRIBE 帧。
func TestBinanceWSEnsureSymbolLiveSubscribe(t *testing.T) {
	upgrader := websocket.Upgrader{}
	type frame struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
	}
	got := make(chan frame, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var f frame
		if err := conn.ReadJSON(&f); err == nil {
			got <- f
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial test server: %v", err)
	}
	defer client.Close()

	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	s.mu.Lock()
	s.conn = client // 模拟已连接会话（不启动 connect 拨binance）
	s.mu.Unlock()
	defer s.Stop()

	s.EnsureSymbol("DOGEUSDT")
	select {
	case f := <-got:
		if f.Method != "SUBSCRIBE" {
			t.Fatalf("method = %q, want SUBSCRIBE", f.Method)
		}
		want := map[string]bool{"dogeusdt@trade": true, "dogeusdt@ticker": true, "dogeusdt@markPrice@1s": true}
		if len(f.Params) != len(want) {
			t.Fatalf("params = %v", f.Params)
		}
		for _, p := range f.Params {
			if !want[p] {
				t.Fatalf("unexpected stream %q in %v", p, f.Params)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive SUBSCRIBE frame")
	}
}
