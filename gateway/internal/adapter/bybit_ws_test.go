package adapter

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// shortenBybitWsTunables 把 Bybit WS 心跳/重连参数压缩到测试尺度，返回恢复函数。
func shortenBybitWsTunables() func() {
	oldPing, oldPong, oldRe := bybitWsPingInterval, bybitWsPongTimeout, bybitWsReconnectDelay
	bybitWsPingInterval = 100 * time.Millisecond
	bybitWsPongTimeout = 2 * time.Second // 读超时 2*PongTimeout=4s，大于 ping 间隔
	bybitWsReconnectDelay = 50 * time.Millisecond
	return func() {
		bybitWsPingInterval, bybitWsPongTimeout, bybitWsReconnectDelay = oldPing, oldPong, oldRe
	}
}

/* ── Bybit WS：订阅确认 + 行情推送 ────────────────────────────── */

func TestBybitWSMarketStreamSubscribeAndData(t *testing.T) {
	defer shortenBybitWsTunables()()

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
			return
		}
		for _, want := range []string{"tickers.BTCUSDT", "orderbook.50.BTCUSDT", "publicTrade.BTCUSDT", "kline.1.BTCUSDT"} {
			found := false
			for _, a := range args {
				if s, _ := a.(string); s == want {
					found = true
				}
			}
			if !found {
				t.Errorf("missing subscribe arg %q in %v", want, args)
			}
		}
		// 订阅确认（Bybit v5 回执不带逐条清单，整批确认）
		_ = conn.WriteJSON(map[string]any{"op": "subscribe", "success": true, "ret_msg": "", "conn_id": "test-1"})
		// ticker：data 是对象（旧实现按数组解析导致永远不触发）
		_ = conn.WriteJSON(map[string]any{
			"topic": "tickers.BTCUSDT", "type": "snapshot", "ts": 1700000000000,
			"data": map[string]any{"symbol": "BTCUSDT", "lastPrice": "42100.5", "bid1Price": "42100", "ask1Price": "42101", "volume24h": "888.8"},
		})
		// 盘口：data 是对象
		_ = conn.WriteJSON(map[string]any{
			"topic": "orderbook.50.BTCUSDT", "type": "snapshot", "ts": 1700000000001,
			"data": map[string]any{
				"s": "BTCUSDT", "u": 1001, "seq": 2002,
				"b": []any{[]any{"42099", "0.5"}, []any{"42098", "1.1"}},
				"a": []any{[]any{"42102", "0.7"}, []any{"42103", "0.9"}},
			},
		})
		// 逐笔成交：data 是数组
		_ = conn.WriteJSON(map[string]any{
			"topic": "publicTrade.BTCUSDT", "type": "snapshot", "ts": 1700000000002,
			"data": []map[string]any{{"T": 1700000000123, "s": "BTCUSDT", "S": "Sell", "v": "0.02", "p": "42100.5", "i": "abcd-1234"}},
		})
		// K线：data 是数组，topic 形如 kline.1.BTCUSDT
		_ = conn.WriteJSON(map[string]any{
			"topic": "kline.1.BTCUSDT", "type": "snapshot", "ts": 1700000000003,
			"data": []map[string]any{{"start": "1700000000000", "end": "1700000059999", "open": "100", "high": "110", "low": "95", "close": "105", "volume": "12.5", "confirm": "1"}},
		})
		// 保持连接，避免客户端因读超时重连干扰断言
		_, _, _ = conn.ReadMessage()
	})
	t.Setenv("BYBIT_WS_URL", ts.wsURL())

	b := NewBybitAdapter("", "", false)
	bars := make(chan model.Bar, 4)
	ticks := make(chan model.Tick, 4)
	obs := make(chan model.OrderBookData, 4)
	trades := make(chan model.TradeData, 4)
	b.OnKline(func(bar model.Bar) { bars <- bar })
	b.OnTicker(func(tk model.Tick) { ticks <- tk })
	b.OnOrderBook(func(o model.OrderBookData) { obs <- o })
	b.OnTrade(func(td model.TradeData) { trades <- td })

	if err := b.StartMarketStream([]string{"BTCUSDT"}); err != nil {
		t.Fatalf("StartMarketStream: %v", err)
	}
	defer b.Stop()

	waitFor(t, 3*time.Second, func() bool { return b.WSSubscribedCount() == 4 }, "4 confirmed subscriptions")
	subs := b.WSSubscriptions()
	wantSubs := map[string]bool{"tickers.BTCUSDT": false, "orderbook.50.BTCUSDT": false, "publicTrade.BTCUSDT": false, "kline.1.BTCUSDT": false}
	for _, s := range subs {
		if _, ok := wantSubs[s]; !ok {
			t.Fatalf("unexpected subscription %q", s)
		}
		wantSubs[s] = true
	}
	for s, found := range wantSubs {
		if !found {
			t.Fatalf("missing subscription %q in %v", s, subs)
		}
	}

	// ticker（回归：对象 data 必须解析成功）
	select {
	case tk := <-ticks:
		if tk.Symbol != "BTCUSDT" || tk.Last != 42100.5 || tk.Bid != 42100 || tk.Ask != 42101 || tk.Volume != 888.8 || tk.Timestamp != 1700000000000 {
			t.Fatalf("bad tick: %+v", tk)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no ticker received")
	}

	// 盘口 orderbook.50
	select {
	case ob := <-obs:
		if ob.Symbol != "BTCUSDT" || len(ob.Bids) != 2 || len(ob.Asks) != 2 {
			t.Fatalf("bad orderbook: %+v", ob)
		}
		if ob.Bids[0] != [2]float64{42099, 0.5} || ob.Asks[0] != [2]float64{42102, 0.7} {
			t.Fatalf("bad orderbook levels: %+v", ob)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no orderbook received")
	}

	// 逐笔成交：S=Sell → SELL，ID 取 i，时间戳取成交时间 T
	select {
	case td := <-trades:
		if td.Symbol != "BTCUSDT" || td.ID != "abcd-1234" || td.Price != 42100.5 || td.Quantity != 0.02 || td.Side != "SELL" || td.Timestamp != 1700000000123 {
			t.Fatalf("bad trade: %+v", td)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no trade received")
	}

	// K线（回归：symbol/interval 不能是 "1.BTCUSDT"）
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
}

/* ── Bybit WS：应用层心跳 ────────────────────────────────────── */

func TestBybitWSHeartbeat(t *testing.T) {
	defer shortenBybitWsTunables()()

	var appPings atomic.Int32
	ts := newWSTestServer(t, func(conn *websocket.Conn, connIdx int) {
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// 统计文本帧 {"op":"ping"}（应用层心跳），协议层 Ping 帧不应出现
			if mt == websocket.TextMessage && strings.Contains(string(msg), `"op":"ping"`) {
				if appPings.Add(1) <= 50 {
					_ = conn.WriteJSON(map[string]any{"op": "pong", "args": []any{}, "conn_id": "test-1"})
				}
			}
		}
	})
	t.Setenv("BYBIT_WS_URL", ts.wsURL())

	b := NewBybitAdapter("", "", false)
	if err := b.StartMarketStream([]string{"BTCUSDT"}); err != nil {
		t.Fatalf("StartMarketStream: %v", err)
	}
	defer b.Stop()

	waitFor(t, 3*time.Second, func() bool { return appPings.Load() >= 3 }, "≥3 app-level pings")
	if !b.IsConnected() {
		t.Fatal("client should stay connected while pong replies flow")
	}
}

/* ── Bybit WS：断线重连后自动重订阅 ───────────────────────────── */

func TestBybitWSReconnectResubscribes(t *testing.T) {
	defer shortenBybitWsTunables()()

	var subscribeCount atomic.Int32
	ts := newWSTestServer(t, func(conn *websocket.Conn, connIdx int) {
		m, err := readJSON(conn)
		if err != nil {
			return
		}
		if op, _ := m["op"].(string); op == "subscribe" {
			subscribeCount.Add(1)
			_ = conn.WriteJSON(map[string]any{"op": "subscribe", "success": true, "ret_msg": "", "conn_id": "test-1"})
		}
		// 粗暴断连，迫使客户端走重连路径
		_ = conn.Close()
	})
	t.Setenv("BYBIT_WS_URL", ts.wsURL())

	b := NewBybitAdapter("", "", false)
	if err := b.StartMarketStream([]string{"BTCUSDT"}); err != nil {
		t.Fatalf("StartMarketStream: %v", err)
	}
	defer b.Stop()

	waitFor(t, 5*time.Second, func() bool { return subscribeCount.Load() >= 2 && ts.connCount.Load() >= 2 },
		"reconnect with resubscribe")
	waitFor(t, 5*time.Second, func() bool { return b.WSSubscribedCount() == 4 }, "4 confirmed subscriptions after reconnect")
}

/* ── Bybit WS 辅助函数 ───────────────────────────────────────── */

func TestBybitWsInterval(t *testing.T) {
	btAssertEq(t, bybitWsInterval("1"), "1m", "1 minute")
	btAssertEq(t, bybitWsInterval("5"), "5m", "5 minutes")
	btAssertEq(t, bybitWsInterval("60"), "60", "hourly passthrough")
	btAssertEq(t, bybitWsInterval("D"), "D", "daily passthrough")
	btAssertEq(t, bybitTopicSymbol("orderbook.50.BTCUSDT"), "BTCUSDT", "orderbook topic symbol")
	btAssertEq(t, bybitTopicSymbol("kline.1.ETHUSDT"), "ETHUSDT", "kline topic symbol")
}
