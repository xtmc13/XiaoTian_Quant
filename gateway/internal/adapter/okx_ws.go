package adapter

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── OKX 公开行情 WebSocket ──
// 端点：wss://ws.okx.com:8443/ws/v5/public
// 协议要点：
//   - 订阅报文 {"op":"subscribe","args":[{"channel":"candle1m","instId":"BTC-USDT"}]}
//   - 每个 arg 收到 {"event":"subscribe","arg":{...}} 确认后才算订阅成功
//   - 应用层心跳：每 25s 发送文本帧 "ping"，交易所回复 "pong"
//   - instId 格式：现货 BTC-USDT；合约（如现有 REST 下单走 SWAP）传
//     BTC-USDT-SWAP，toOKXInstID 对含 "-" 的 symbol 原样透传

// 可调参数（包级变量，测试可缩短以加速用例）
var (
	okxWsPingInterval   = 25 * time.Second // OKX 要求 25s 内应用层 ping
	okxWsPongTimeout    = 15 * time.Second // 读超时按 2*PongTimeout 计算，需大于 ping 间隔
	okxWsReconnectDelay = 3 * time.Second  // 重连基础退避（WSClient 内部指数退避 + 抖动）
)

// wsPublicURL 返回 OKX 公开行情 WS 地址，支持环境变量覆盖（测试/代理用）。
func (o *OKXAdapter) wsPublicURL() string {
	if env := os.Getenv("OKX_WS_URL"); env != "" {
		return env
	}
	return OKXWsPubURL
}

// StartMarketStream 连接 OKX 公开行情 WS 并订阅 candle1m/tickers/books5/trades。
// 断线由 WSClient 指数退避自动重连，重连成功后自动重新订阅。
func (o *OKXAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	var args []map[string]string
	for _, sym := range symbols {
		instID := toOKXInstID(sym)
		args = append(args, map[string]string{"channel": "tickers", "instId": instID})
		args = append(args, map[string]string{"channel": "books5", "instId": instID})
		args = append(args, map[string]string{"channel": "trades", "instId": instID})
		args = append(args, map[string]string{"channel": "candle1m", "instId": instID})
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL:            o.wsPublicURL(),
		PingInterval:   okxWsPingInterval,
		PongTimeout:    okxWsPongTimeout,
		ReconnectDelay: okxWsReconnectDelay,
		AppPing:        []byte("ping"), // OKX 应用层心跳：文本帧 "ping" → "pong"
		OnMessage: func(msg []byte) {
			o.handlePublicMessage(msg)
		},
		OnConnected: func() {
			o.mu.Lock()
			o.wsConnected = true
			o.mu.Unlock()
			// 重连后订阅确认会重发，清空旧状态再订阅
			o.resetWSSubscriptions()
			o.subscribePublic(args)
			log.Printf("[OKX] Public stream connected, subscribing %d args", len(args))
		},
		OnDisconnected: func(err error) {
			o.mu.Lock()
			o.wsConnected = false
			o.mu.Unlock()
			log.Printf("[OKX] Public stream disconnected: %v", err)
		},
	})

	o.streamHub.Add("public", wsClient)
	return wsClient.Connect()
}

// subscribePublic 发送一次 subscribe 报文订阅全部频道。
func (o *OKXAdapter) subscribePublic(args []map[string]string) {
	client := o.streamHub.Get("public")
	if client == nil {
		return
	}

	msg := map[string]any{
		"op":   "subscribe",
		"args": args,
	}
	if err := client.SendJSON(msg); err != nil {
		log.Printf("[OKX] subscribe send failed: %v", err)
		return
	}
	// 先记入待确认集合，收到 event=subscribe 后转正
	o.wsSubsMu.Lock()
	if o.wsSubs == nil {
		o.wsSubs = make(map[string]bool)
	}
	for _, a := range args {
		o.wsSubs[a["channel"]+":"+a["instId"]] = false
	}
	o.wsSubsMu.Unlock()
}

// ── 订阅状态跟踪 ──

// resetWSSubscriptions 清空订阅确认状态（重连时调用）。
func (o *OKXAdapter) resetWSSubscriptions() {
	o.wsSubsMu.Lock()
	defer o.wsSubsMu.Unlock()
	o.wsSubs = make(map[string]bool)
}

// confirmWSSubscription 标记某个 channel:instId 已收到交易所确认。
func (o *OKXAdapter) confirmWSSubscription(key string) {
	o.wsSubsMu.Lock()
	defer o.wsSubsMu.Unlock()
	if o.wsSubs != nil {
		o.wsSubs[key] = true
	}
}

// WSSubscriptions 返回已收到交易所确认（event=subscribe）的订阅列表，
// 格式 "channel:instId"（如 "candle1m:BTC-USDT"），按字母序排序。
func (o *OKXAdapter) WSSubscriptions() []string {
	o.wsSubsMu.Lock()
	defer o.wsSubsMu.Unlock()
	var out []string
	for key, confirmed := range o.wsSubs {
		if confirmed {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// WSSubscribedCount 返回已确认的订阅数量。
func (o *OKXAdapter) WSSubscribedCount() int {
	return len(o.WSSubscriptions())
}

// ── 公开流消息处理 ──

func (o *OKXAdapter) handlePublicMessage(msg []byte) {
	// 心跳回复：交易所对文本 "ping" 的应答是文本 "pong"，无需处理
	if string(msg) == "pong" {
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	// 事件帧：subscribe 确认 / error / ping
	if event, ok := raw["event"].(string); ok {
		switch event {
		case "subscribe":
			if arg, ok := raw["arg"].(map[string]any); ok {
				channel, _ := arg["channel"].(string)
				instID, _ := arg["instId"].(string)
				if channel != "" && instID != "" {
					o.confirmWSSubscription(channel + ":" + instID)
				}
			}
		case "error":
			log.Printf("[OKX] Public stream error: code=%v msg=%v", raw["code"], raw["msg"])
		}
		return
	}

	arg, _ := raw["arg"].(map[string]any)
	if arg == nil {
		return
	}

	channel, _ := arg["channel"].(string)
	instID, _ := arg["instId"].(string)
	symbol := fromOKXInstID(instID)

	data, _ := raw["data"].([]any)
	if data == nil {
		return
	}

	switch channel {
	case "tickers":
		if len(data) > 0 && o.onTicker != nil {
			if d, ok := data[0].(map[string]any); ok {
				o.onTicker(model.Tick{
					Symbol:    symbol,
					Last:      parseFloat(d["last"]),
					Bid:       parseFloat(d["bidPx"]),
					Ask:       parseFloat(d["askPx"]),
					Volume:    parseFloat(d["vol24h"]),
					Timestamp: time.Now().UnixMilli(),
				})
			}
		}
	case "books5":
		if len(data) > 0 && o.onOrderBook != nil {
			if d, ok := data[0].(map[string]any); ok {
				ob := model.OrderBookData{Symbol: symbol, Timestamp: time.Now().UnixMilli()}
				if bids, ok := d["bids"].([]any); ok {
					for _, b := range bids {
						if arr, ok2 := b.([]any); ok2 && len(arr) >= 2 {
							ob.Bids = append(ob.Bids, [2]float64{
								parseFloat(arr[0]),
								parseFloat(arr[1]),
							})
						}
					}
				}
				if asks, ok := d["asks"].([]any); ok {
					for _, a := range asks {
						if arr, ok2 := a.([]any); ok2 && len(arr) >= 2 {
							ob.Asks = append(ob.Asks, [2]float64{
								parseFloat(arr[0]),
								parseFloat(arr[1]),
							})
						}
					}
				}
				o.onOrderBook(ob)
			}
		}
	case "trades":
		for _, t := range data {
			if d, ok := t.(map[string]any); ok && o.onTrade != nil {
				side := "BUY"
				if s, _ := d["side"].(string); s == "sell" {
					side = "SELL"
				}
				ts := time.Now().UnixMilli()
				if tsv := parseFloat(d["ts"]); tsv > 0 {
					ts = int64(tsv)
				}
				o.onTrade(model.TradeData{
					Symbol:    symbol,
					ID:        fmt.Sprint(d["tradeId"]),
					Price:     parseFloat(d["px"]),
					Quantity:  parseFloat(d["sz"]),
					Side:      side,
					Timestamp: ts,
				})
			}
		}
	case "candle1m":
		if len(data) > 0 && o.onKline != nil {
			if d, ok := data[0].(map[string]any); ok {
				o.onKline(model.Bar{
					Symbol:   symbol,
					Open:     parseFloat(d["o"]),
					High:     parseFloat(d["h"]),
					Low:      parseFloat(d["l"]),
					Close:    parseFloat(d["c"]),
					Volume:   parseFloat(d["vol"]),
					Interval: "1m",
					Time:     int64(parseFloat(d["ts"])),
				})
			}
		}
	}
}
