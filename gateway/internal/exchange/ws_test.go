package exchange

import (
	"encoding/json"
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

// ── 价格缓存新鲜度（2026-10-10 运行面板陈旧价根修）──
// 生产实证：运行面板"现价"经 GetPrice 读 WS 缓存，SOLUSDT 策略引擎按
// K 线收盘价 120.1 开出首单时，面板显示数小时前的残留值 108.72——
// GetPrice 无任何陈旧度概念（WS 断连/未订阅的 symbol 缓存永久冻结）。
// GetPriceFresh 带本地写入时间戳判定，超阈值视为不可用，供调用方回落。

func TestBinanceWSGetPriceFresh(t *testing.T) {
	s := NewBinanceWSStream([]string{"btcusdt"}, nil)
	nowMs := time.Now().UnixMilli()

	s.pricesMu.Lock()
	s.prices["BTCUSDT"] = 67000.5
	s.pricesTs["BTCUSDT"] = nowMs
	s.pricesMu.Unlock()

	// 新鲜缓存 → 返回值（大小写归一）。
	if p := s.GetPriceFresh("btcusdt", 2*time.Minute); p != 67000.5 {
		t.Fatalf("fresh price: got %v, want 67000.5", p)
	}

	// 冻结超过阈值（WS 断连后的残留值）→ 0。
	s.pricesMu.Lock()
	s.pricesTs["BTCUSDT"] = nowMs - int64((10 * time.Minute).Milliseconds())
	s.pricesMu.Unlock()
	if p := s.GetPriceFresh("BTCUSDT", 2*time.Minute); p != 0 {
		t.Fatalf("stale price must be treated as unavailable: got %v", p)
	}
	// 原始 GetPrice 口径不变：陈旧值仍如实返回（兼容既有消费方）。
	if p := s.GetPrice("BTCUSDT"); p != 67000.5 {
		t.Fatalf("legacy GetPrice must remain raw: got %v", p)
	}

	// 从未收到行情的 symbol（未订阅）→ 0。
	if p := s.GetPriceFresh("SOLUSDT", 2*time.Minute); p != 0 {
		t.Fatalf("never-seen symbol: got %v", p)
	}
	// 有价格但无写入时间戳（不经正常写入路径）→ 0。
	s.pricesMu.Lock()
	s.prices["ETHUSDT"] = 3000
	s.pricesMu.Unlock()
	if p := s.GetPriceFresh("ETHUSDT", 2*time.Minute); p != 0 {
		t.Fatalf("price without timestamp must be unavailable: got %v", p)
	}
	// 非正价格即使新鲜 → 0。
	s.pricesMu.Lock()
	s.prices["XRPUSDT"] = -1
	s.pricesTs["XRPUSDT"] = nowMs
	s.pricesMu.Unlock()
	if p := s.GetPriceFresh("XRPUSDT", 2*time.Minute); p != 0 {
		t.Fatalf("non-positive price: got %v", p)
	}
}

// 三条写入路径（trade/ticker/markPrice）都必须给缓存打时间戳。
func TestBinanceWSHandlersStampFreshness(t *testing.T) {
	s := NewBinanceWSStream([]string{"solusdt"}, nil)

	s.handleTicker(json.RawMessage(`{"s":"SOLUSDT","c":"120.1","o":"119","h":"121","l":"118","v":"123","b":"120","a":"120.2","p":"1","P":"0.8"}`), "solusdt@ticker")
	if p := s.GetPriceFresh("SOLUSDT", time.Minute); p != 120.1 {
		t.Fatalf("ticker write must be fresh: got %v", p)
	}

	s.handleTrade(json.RawMessage(`{"s":"ETHUSDT","p":"3000.5","q":"0.1","T":1720000000000,"m":true}`), "ethusdt@trade")
	if p := s.GetPriceFresh("ETHUSDT", time.Minute); p != 3000.5 {
		t.Fatalf("trade write must be fresh: got %v", p)
	}

	s.handleMarkPrice(json.RawMessage(`{"s":"BTCUSDT","p":"67001","r":"0.0001","T":1720000000000}`), "btcusdt@markPrice@1s")
	if p := s.GetPriceFresh("BTCUSDT", time.Minute); p != 67001 {
		t.Fatalf("markPrice write must be fresh: got %v", p)
	}
}
