package adapter

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// shortenOKXWsTunables 把 OKX WS 心跳/重连参数压缩到测试尺度，返回恢复函数。
// 注意：这些包级变量不能在并行测试中改动，本包测试均未使用 t.Parallel。
func shortenOKXWsTunables() func() {
	oldPing, oldPong, oldRe := okxWsPingInterval, okxWsPongTimeout, okxWsReconnectDelay
	okxWsPingInterval = 100 * time.Millisecond
	okxWsPongTimeout = 2 * time.Second // 读超时 2*PongTimeout=4s，大于 ping 间隔
	okxWsReconnectDelay = 50 * time.Millisecond
	return func() {
		okxWsPingInterval, okxWsPongTimeout, okxWsReconnectDelay = oldPing, oldPong, oldRe
	}
}

/* ── OKX WS：订阅确认 + 行情推送 ──────────────────────────────── */

func TestOKXWSMarketStreamSubscribeAndData(t *testing.T) {
	defer shortenOKXWsTunables()()

	// 服务器：收到 subscribe 后逐条回确认，再推四类行情数据和一个 error 事件
	ts := newWSTestServer(t, func(conn *websocket.Conn, connIdx int) {
		m, err := readJSON(conn)
		if err != nil {
			return
		}
		if op, _ := m["op"].(string); op != "subscribe" {
			t.Errorf("expected subscribe op, got %v", m)
			return
		}
		args, _ := m["args"].([]any)
		if len(args) != 4 {
			t.Errorf("expected 4 subscribe args, got %d", len(args))
		}
		// 回复确认前先丢一个 error 事件，验证客户端能容错并继续收数据
		_ = conn.WriteJSON(map[string]any{"event": "error", "code": "60018", "msg": "test error ignored"})
		for _, a := range args {
			arg, ok := a.(map[string]any)
			if !ok {
				t.Errorf("bad arg: %v", a)
				return
			}
			_ = conn.WriteJSON(map[string]any{
				"event": "subscribe",
				"arg":   map[string]any{"channel": arg["channel"], "instId": arg["instId"]},
			})
		}
		// K线（candle1m）
		_ = conn.WriteJSON(map[string]any{
			"arg":  map[string]any{"channel": "candle1m", "instId": "BTC-USDT"},
			"data": []map[string]string{{"ts": "1700000000000", "o": "100", "h": "110", "l": "95", "c": "105", "vol": "12.5"}},
		})
		// 盘口（books5）
		_ = conn.WriteJSON(map[string]any{
			"arg": map[string]any{"channel": "books5", "instId": "BTC-USDT"},
			"data": []map[string]any{{
				"bids": []any{[]any{"104", "1.5", "0", "1"}, []any{"103", "2", "0", "1"}},
				"asks": []any{[]any{"106", "0.8", "0", "1"}, []any{"107", "1.2", "0", "1"}},
			}},
		})
		// 成交（trades）
		_ = conn.WriteJSON(map[string]any{
			"arg":  map[string]any{"channel": "trades", "instId": "BTC-USDT"},
			"data": []map[string]string{{"tradeId": "998877", "px": "105.5", "sz": "0.3", "side": "sell", "ts": "1700000000123"}},
		})
		// ticker（tickers）
		_ = conn.WriteJSON(map[string]any{
			"arg":  map[string]any{"channel": "tickers", "instId": "BTC-USDT"},
			"data": []map[string]string{{"last": "105.2", "bidPx": "105.1", "askPx": "105.3", "vol24h": "12345"}},
		})
		// 保持连接，避免客户端因读超时重连干扰断言
		_, _, _ = conn.ReadMessage()
	})
	t.Setenv("OKX_WS_URL", ts.wsURL())

	a := NewOKXAdapter("", "", "", false)
	bars := make(chan model.Bar, 4)
	ticks := make(chan model.Tick, 4)
	obs := make(chan model.OrderBookData, 4)
	trades := make(chan model.TradeData, 4)
	a.OnKline(func(b model.Bar) { bars <- b })
	a.OnTicker(func(tk model.Tick) { ticks <- tk })
	a.OnOrderBook(func(o model.OrderBookData) { obs <- o })
	a.OnTrade(func(td model.TradeData) { trades <- td })

	if err := a.StartMarketStream([]string{"BTCUSDT"}); err != nil {
		t.Fatalf("StartMarketStream: %v", err)
	}
	defer a.Stop()

	// 订阅确认可查询：4 个 channel:instId 全部确认
	waitFor(t, 3*time.Second, func() bool { return a.WSSubscribedCount() == 4 }, "4 confirmed subscriptions")
	subs := a.WSSubscriptions()
	want := map[string]bool{"candle1m:BTC-USDT": false, "tickers:BTC-USDT": false, "books5:BTC-USDT": false, "trades:BTC-USDT": false}
	for _, s := range subs {
		if _, ok := want[s]; !ok {
			t.Fatalf("unexpected subscription %q", s)
		}
		want[s] = true
	}
	for s, found := range want {
		if !found {
			t.Fatalf("missing subscription %q in %v", s, subs)
		}
	}

	// K线：symbol 归一化为 BTCUSDT，interval 1m
	select {
	case bar := <-bars:
		if bar.Symbol != "BTCUSDT" || bar.Interval != "1m" || bar.Time != 1700000000000 {
			t.Fatalf("bad bar: %+v", bar)
		}
		if bar.Open != 100 || bar.High != 110 || bar.Low != 95 || bar.Close != 105 || bar.Volume != 12.5 {
			t.Fatalf("bad bar OHLCV: %+v", bar)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no kline bar received")
	}

	// 盘口：books5 快照，档位取 [px, sz] 前两列
	select {
	case ob := <-obs:
		if ob.Symbol != "BTCUSDT" || len(ob.Bids) != 2 || len(ob.Asks) != 2 {
			t.Fatalf("bad orderbook: %+v", ob)
		}
		if ob.Bids[0] != [2]float64{104, 1.5} || ob.Asks[0] != [2]float64{106, 0.8} {
			t.Fatalf("bad orderbook levels: %+v", ob)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no orderbook received")
	}

	// 成交：side sell → SELL，时间戳用报文 ts
	select {
	case td := <-trades:
		if td.Symbol != "BTCUSDT" || td.ID != "998877" || td.Price != 105.5 || td.Quantity != 0.3 || td.Side != "SELL" || td.Timestamp != 1700000000123 {
			t.Fatalf("bad trade: %+v", td)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no trade received")
	}

	// ticker
	select {
	case tk := <-ticks:
		if tk.Symbol != "BTCUSDT" || tk.Last != 105.2 || tk.Bid != 105.1 || tk.Ask != 105.3 || tk.Volume != 12345 {
			t.Fatalf("bad tick: %+v", tk)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no ticker received")
	}
}

/* ── OKX WS：应用层心跳 ──────────────────────────────────────── */

func TestOKXWSHeartbeat(t *testing.T) {
	defer shortenOKXWsTunables()()

	var textPings atomic.Int32
	ts := newWSTestServer(t, func(conn *websocket.Conn, connIdx int) {
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// 只统计文本帧 "ping"（应用层心跳），协议层 Ping 帧不应出现
			if mt == websocket.TextMessage && string(msg) == "ping" {
				if textPings.Add(1) <= 50 {
					_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
				}
			}
		}
	})
	t.Setenv("OKX_WS_URL", ts.wsURL())

	a := NewOKXAdapter("", "", "", false)
	if err := a.StartMarketStream([]string{"BTCUSDT"}); err != nil {
		t.Fatalf("StartMarketStream: %v", err)
	}
	defer a.Stop()

	waitFor(t, 3*time.Second, func() bool { return textPings.Load() >= 3 }, "≥3 app-level pings")
	if !a.IsConnected() {
		t.Fatal("client should stay connected while pong replies flow")
	}
}

/* ── OKX WS：断线重连后自动重订阅 ────────────────────────────── */

func TestOKXWSReconnectResubscribes(t *testing.T) {
	defer shortenOKXWsTunables()()

	var subscribeCount atomic.Int32
	ts := newWSTestServer(t, func(conn *websocket.Conn, connIdx int) {
		m, err := readJSON(conn)
		if err != nil {
			return
		}
		args, _ := m["args"].([]any)
		subscribeCount.Add(1)
		for _, arg := range args {
			a2, ok := arg.(map[string]any)
			if !ok {
				return
			}
			_ = conn.WriteJSON(map[string]any{
				"event": "subscribe",
				"arg":   map[string]any{"channel": a2["channel"], "instId": a2["instId"]},
			})
		}
		// 立即粗暴断连，迫使客户端走重连路径
		_ = conn.Close()
	})
	t.Setenv("OKX_WS_URL", ts.wsURL())

	a := NewOKXAdapter("", "", "", false)
	if err := a.StartMarketStream([]string{"BTCUSDT"}); err != nil {
		t.Fatalf("StartMarketStream: %v", err)
	}
	defer a.Stop()

	waitFor(t, 5*time.Second, func() bool { return subscribeCount.Load() >= 2 && ts.connCount.Load() >= 2 },
		"reconnect with resubscribe")
	// 重连后订阅确认应重新建立
	waitFor(t, 5*time.Second, func() bool { return a.WSSubscribedCount() == 4 }, "4 confirmed subscriptions after reconnect")
}

/* ── OKX symbol 格式 ─────────────────────────────────────────── */

func TestOKXWSContractInstIDPassthrough(t *testing.T) {
	// 现货：BTCUSDT → BTC-USDT；合约：含 "-" 原样透传（REST 走 SWAP 时传 BTC-USDT-SWAP）
	if got := toOKXInstID("BTCUSDT"); got != "BTC-USDT" {
		t.Fatalf("spot instId: got %s", got)
	}
	if got := toOKXInstID("BTC-USDT-SWAP"); got != "BTC-USDT-SWAP" {
		t.Fatalf("swap instId should pass through: got %s", got)
	}
	if got := fromOKXInstID("BTC-USDT"); got != "BTCUSDT" {
		t.Fatalf("symbol normalize: got %s", got)
	}
	if !strings.Contains(OKXWsPubURL, "ws.okx.com") {
		t.Fatalf("unexpected OKX WS url %s", OKXWsPubURL)
	}
}
