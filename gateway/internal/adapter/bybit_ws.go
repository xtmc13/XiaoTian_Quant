package adapter

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Bybit 公开行情 WebSocket ──
// 端点：wss://stream.bybit.com/v5/public/spot（现货；REST 另覆盖 linear 合约，
// 本次按任务范围只接现货公开行情，合约端点结构相同可后续平移）
// 协议要点：
//   - 订阅报文 {"op":"subscribe","args":["tickers.BTCUSDT","orderbook.50.BTCUSDT",...]}
//   - 确认帧 {"op":"subscribe","success":true,...}（不带逐条回执，按整批确认）
//   - 应用层心跳：每 20s 发送 {"op":"ping"}，交易所回复 {"op":"pong"}
//   - tickers/orderbook 的 data 是对象，publicTrade/kline 的 data 是数组

// 可调参数（包级变量，测试可缩短以加速用例）
var (
	bybitWsPingInterval   = 20 * time.Second // Bybit 要求 ≤20s 应用层 ping
	bybitWsPongTimeout    = 12 * time.Second // 读超时按 2*PongTimeout 计算，需大于 ping 间隔
	bybitWsReconnectDelay = 3 * time.Second  // 重连基础退避（WSClient 内部指数退避 + 抖动）
)

// wsSpotPublicURL 返回 Bybit 现货公开行情 WS 地址，支持环境变量覆盖（测试/代理用）。
func (b *BybitAdapter) wsSpotPublicURL() string {
	if env := os.Getenv("BYBIT_WS_URL"); env != "" {
		return env
	}
	if b.testnet {
		return "wss://stream-testnet.bybit.com/v5/public/spot"
	}
	return BybitWsPublicURL
}

// StartMarketStream 连接 Bybit 现货公开行情 WS 并订阅
// kline.1 / tickers / orderbook.50 / publicTrade。
// 断线由 WSClient 指数退避自动重连，重连成功后自动重新订阅。
func (b *BybitAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	var args []string
	for _, sym := range symbols {
		upper := strings.ToUpper(sym)
		args = append(args,
			fmt.Sprintf("tickers.%s", upper),
			fmt.Sprintf("orderbook.50.%s", upper),
			fmt.Sprintf("publicTrade.%s", upper),
			fmt.Sprintf("kline.1.%s", upper),
		)
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL:            b.wsSpotPublicURL(),
		PingInterval:   bybitWsPingInterval,
		PongTimeout:    bybitWsPongTimeout,
		ReconnectDelay: bybitWsReconnectDelay,
		AppPing:        []byte(`{"op":"ping"}`), // Bybit 应用层心跳
		OnMessage: func(msg []byte) {
			b.handleMarketMessage(msg)
		},
		OnConnected: func() {
			b.mu.Lock()
			b.wsConnected = true
			b.mu.Unlock()
			// 重连后确认会重发，清空旧状态再订阅
			b.resetWSSubscriptions()
			b.subscribeMarket(args)
			log.Printf("[Bybit] Market stream connected, subscribing %d topics", len(args))
		},
		OnDisconnected: func(err error) {
			b.mu.Lock()
			b.wsConnected = false
			b.mu.Unlock()
			if err != nil {
				log.Printf("[Bybit] Market stream disconnected: %v", err)
			}
		},
	})

	b.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

// subscribeMarket 发送一次 subscribe 报文订阅全部 topic。
func (b *BybitAdapter) subscribeMarket(args []string) {
	client := b.streamHub.Get("market")
	if client == nil {
		return
	}

	msg := map[string]any{
		"op":   "subscribe",
		"args": args,
	}
	if err := client.SendJSON(msg); err != nil {
		log.Printf("[Bybit] subscribe send failed: %v", err)
		return
	}
	// 先记入待确认集合，收到 op=subscribe success 后整批转正
	b.wsSubsMu.Lock()
	if b.wsSubs == nil {
		b.wsSubs = make(map[string]bool)
	}
	for _, topic := range args {
		b.wsSubs[topic] = false
	}
	b.wsSubsMu.Unlock()
}

// ── 订阅状态跟踪 ──

// resetWSSubscriptions 清空订阅确认状态（重连时调用）。
func (b *BybitAdapter) resetWSSubscriptions() {
	b.wsSubsMu.Lock()
	defer b.wsSubsMu.Unlock()
	b.wsSubs = make(map[string]bool)
}

// confirmAllWSSubscriptions 将待确认订阅整批标记为已确认。
func (b *BybitAdapter) confirmAllWSSubscriptions() {
	b.wsSubsMu.Lock()
	defer b.wsSubsMu.Unlock()
	for topic := range b.wsSubs {
		b.wsSubs[topic] = true
	}
}

// WSSubscriptions 返回已确认的订阅列表（topic 格式，如 "tickers.BTCUSDT"），
// 按字母序排序。
func (b *BybitAdapter) WSSubscriptions() []string {
	b.wsSubsMu.Lock()
	defer b.wsSubsMu.Unlock()
	var out []string
	for topic, confirmed := range b.wsSubs {
		if confirmed {
			out = append(out, topic)
		}
	}
	sort.Strings(out)
	return out
}

// WSSubscribedCount 返回已确认的订阅数量。
func (b *BybitAdapter) WSSubscribedCount() int {
	return len(b.WSSubscriptions())
}

// ── 行情消息处理 ──

func (b *BybitAdapter) handleMarketMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	// 控制帧：订阅确认 / pong / 错误
	if op, ok := raw["op"].(string); ok {
		switch op {
		case "subscribe":
			if success, _ := raw["success"].(bool); success {
				b.confirmAllWSSubscriptions()
			} else {
				log.Printf("[Bybit] subscribe rejected: %v", raw["ret_msg"])
			}
		case "pong":
			// 心跳应答，无需处理
		}
		return
	}

	// 行情帧：topic + data
	topic, _ := raw["topic"].(string)
	if topic == "" {
		return
	}

	ts, _ := raw["ts"].(float64)

	switch {
	case strings.HasPrefix(topic, "tickers."):
		// v5 tickers 的 data 是单个对象
		ticker, ok := raw["data"].(map[string]any)
		if !ok || b.onTicker == nil {
			return
		}
		symbol := strings.TrimPrefix(topic, "tickers.")
		b.onTicker(model.Tick{
			Symbol:    symbol,
			Bid:       parseBybitFloat(ticker["bid1Price"]),
			Ask:       parseBybitFloat(ticker["ask1Price"]),
			Last:      parseBybitFloat(ticker["lastPrice"]),
			Volume:    parseBybitFloat(ticker["volume24h"]),
			Timestamp: int64(ts),
		})

	case strings.HasPrefix(topic, "orderbook."):
		// v5 orderbook 的 data 是单个对象（snapshot/delta 同构，50 档为全量快照）
		obData, ok := raw["data"].(map[string]any)
		if !ok || b.onOrderBook == nil {
			return
		}
		bidsRaw, _ := obData["b"].([]any)
		asksRaw, _ := obData["a"].([]any)
		bids := parseBybitDepth(bidsRaw)
		asks := parseBybitDepth(asksRaw)
		symbol := bybitTopicSymbol(topic)
		if symbol == "" {
			symbol = getString(obData, "s", "")
		}
		b.onOrderBook(model.OrderBookData{
			Symbol:    symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: int64(ts),
		})

	case strings.HasPrefix(topic, "publicTrade."):
		// v5 publicTrade 的 data 是成交数组
		data, ok := raw["data"].([]any)
		if !ok {
			return
		}
		symbol := strings.TrimPrefix(topic, "publicTrade.")
		for _, item := range data {
			tradeData, ok := item.(map[string]any)
			if !ok || b.onTrade == nil {
				continue
			}
			side := "BUY"
			if s, _ := tradeData["S"].(string); s == "Sell" {
				side = "SELL"
			}
			tsTrade := int64(ts)
			if tv := parseBybitFloat(tradeData["T"]); tv > 0 {
				tsTrade = int64(tv)
			}
			b.onTrade(model.TradeData{
				Symbol:    symbol,
				ID:        getString(tradeData, "i", ""),
				Price:     parseBybitFloat(tradeData["p"]),
				Quantity:  parseBybitFloat(tradeData["v"]),
				Side:      side,
				Timestamp: tsTrade,
			})
		}

	case strings.HasPrefix(topic, "kline."):
		// v5 kline 的 data 是数组（通常一条），topic 形如 kline.1.BTCUSDT
		data, ok := raw["data"].([]any)
		if !ok || len(data) == 0 {
			return
		}
		klineData, ok := data[0].(map[string]any)
		if !ok || b.onKline == nil {
			return
		}
		parts := strings.Split(topic, ".")
		if len(parts) != 3 {
			return
		}
		b.onKline(model.Bar{
			Symbol:   parts[2],
			Open:     parseBybitFloat(klineData["open"]),
			High:     parseBybitFloat(klineData["high"]),
			Low:      parseBybitFloat(klineData["low"]),
			Close:    parseBybitFloat(klineData["close"]),
			Volume:   parseBybitFloat(klineData["volume"]),
			Interval: bybitWsInterval(parts[1]),
			Time:     parseBybitInt(klineData["start"]),
		})
	}
}

// bybitTopicSymbol 从 orderbook.50.BTCUSDT 这类 topic 提取 symbol。
func bybitTopicSymbol(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// bybitWsInterval 把 topic 里的 kline 周期（"1".."59" 分钟，其余原样）转成
// 内部统一格式（分钟数 + "m"，如 "1" → "1m"）。
func bybitWsInterval(v string) string {
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 59 {
		return fmt.Sprintf("%dm", n)
	}
	return v
}

func parseBybitDepth(raw []any) [][2]float64 {
	depth := make([][2]float64, 0, len(raw))
	for _, item := range raw {
		arr, ok := item.([]any)
		if !ok || len(arr) < 2 {
			continue
		}
		depth = append(depth, [2]float64{
			parseBybitFloat(arr[0]),
			parseBybitFloat(arr[1]),
		})
	}
	return depth
}
