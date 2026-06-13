package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

const (
	BitgetRestURL = "https://api.bitget.com/api/v2"
)

// BitgetAdapter provides Bitget exchange integration.
// Bitget uses HMAC-SHA256 signature with base64 encoding.
type BitgetAdapter struct {
	apiKey     string
	secretKey  string
	passphrase string
	httpClient *http.Client
	mu         sync.RWMutex

	streamHub     *exchange.StreamHub
	wsConnected   bool
	onTicker      func(tick model.Tick)
	onOrderBook   func(ob model.OrderBookData)
	onTrade       func(trade model.TradeData)
	onKline       func(bar model.Bar)

	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewBitgetAdapter(apiKey, secretKey, passphrase string) *BitgetAdapter {
	return &BitgetAdapter{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (b *BitgetAdapter) Name() string     { return "bitget" }
func (b *BitgetAdapter) Start() error     { return nil }

// ── Auth ───────────────────────────────────────────────────────

func (b *BitgetAdapter) signBitget(timestamp, method, path, body string) string {
	preSign := fmt.Sprintf("%s%s%s%s", timestamp, method, path, body)
	mac := hmac.New(sha256.New, []byte(b.secretKey))
	mac.Write([]byte(preSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (b *BitgetAdapter) signedRequest(method, path string, bodyMap map[string]any) (map[string]any, error) {
	var bodyStr string
	if bodyMap != nil {
		data, _ := json.Marshal(bodyMap)
		bodyStr = string(data)
	}

	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	sign := b.signBitget(timestamp, method, path, bodyStr)

	var bodyReader io.Reader
	if bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	req, _ := http.NewRequest(method, BitgetRestURL+path, bodyReader)
	req.Header.Set("ACCESS-KEY", b.apiKey)
	req.Header.Set("ACCESS-SIGN", sign)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", b.passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitget %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("bitget parse: %w", err)
	}

	if code, _ := result["code"].(string); code != "00000" {
		msg, _ := result["msg"].(string)
		return nil, fmt.Errorf("bitget error [%s]: %s", code, msg)
	}

	return result, nil
}

// ── Market Data ────────────────────────────────────────────────

func (b *BitgetAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	result, err := b.signedRequest("GET", fmt.Sprintf(
		"/spot/market/candles?symbol=%s&granularity=%s&limit=%d",
		symbol, toBitgetInterval(interval), clampLimit(limit, 1, 200),
	), nil)
	if err != nil {
		return nil, err
	}

	data, _ := result["data"].([]any)
	klines := make([][]any, 0, len(data))
	for i := len(data) - 1; i >= 0; i-- { // reverse to chronological
		arr, ok := data[i].([]any)
		if !ok || len(arr) < 6 {
			continue
		}
		klines = append(klines, []any{
			int64(parseFloatSafe(arr[0])), // timestamp
			parseFloatSafe(arr[1]),         // open
			parseFloatSafe(arr[2]),         // high
			parseFloatSafe(arr[3]),         // low
			parseFloatSafe(arr[4]),         // close
			parseFloatSafe(arr[5]),         // volume
		})
	}
	return klines, nil
}

func (b *BitgetAdapter) GetTicker(symbol string) (map[string]any, error) {
	result, err := b.signedRequest("GET", "/spot/market/tickers?symbol="+symbol, nil)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].([]any)
	if len(data) == 0 {
		return nil, fmt.Errorf("bitget: no ticker for %s", symbol)
	}
	t, _ := data[0].(map[string]any)
	return map[string]any{
		"symbol": symbol,
		"last":   parseFloatSafe(t["close"]),
		"bid":    parseFloatSafe(t["buyOne"]),
		"ask":    parseFloatSafe(t["sellOne"]),
		"high":   parseFloatSafe(t["high24h"]),
		"low":    parseFloatSafe(t["low24h"]),
		"volume": parseFloatSafe(t["baseVol"]),
	}, nil
}

// ── Account ────────────────────────────────────────────────────

func (b *BitgetAdapter) GetBalance() ([]map[string]any, error) {
	result, err := b.signedRequest("GET", "/spot/account/assets", nil)
	if err != nil {
		return nil, err
	}

	data, _ := result["data"].([]any)
	balances := make([]map[string]any, 0, len(data))
	for _, item := range data {
		a, _ := item.(map[string]any)
		balances = append(balances, map[string]any{
			"asset":  a["coinName"],
			"free":   parseFloatSafe(a["available"]),
			"locked": parseFloatSafe(a["frozen"]),
		})
	}
	return balances, nil
}

// ── Orders ─────────────────────────────────────────────────────

func (b *BitgetAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	body := map[string]any{
		"symbol":    symbol,
		"side":      strings.ToLower(side),
		"orderType": strings.ToLower(orderType),
		"force":     "gtc",
		"quantity":  fmt.Sprintf("%.6f", quantity),
	}
	if strings.ToLower(orderType) == "limit" {
		body["price"] = fmt.Sprintf("%.2f", price)
	}

	result, err := b.signedRequest("POST", "/spot/trade/place-order", body)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].(map[string]any)
	return map[string]any{
		"order_id": data["orderId"],
		"symbol":   symbol, "side": side, "type": orderType,
		"price": price, "quantity": quantity, "status": "open",
	}, nil
}

func (b *BitgetAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return b.signedRequest("POST", "/spot/trade/cancel-order", map[string]any{
		"symbol":  symbol,
		"orderId": orderID,
	})
}

func (b *BitgetAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	result, err := b.signedRequest("GET", "/spot/trade/open-orders?symbol="+symbol, nil)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].([]any)
	orders := make([]map[string]any, 0, len(data))
	for _, item := range data {
		orders = append(orders, item.(map[string]any))
	}
	return orders, nil
}

func (b *BitgetAdapter) GetPositions() ([]map[string]any, error) {
	return []map[string]any{}, nil
}

func (b *BitgetAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: "wss://ws.bitget.com/v2/ws/",
		OnMessage: func(msg []byte) {
			b.handleMarketMessage(msg)
		},
		OnConnected: func() {
			b.mu.Lock()
			b.wsConnected = true
			b.mu.Unlock()
			log.Printf("[Bitget] Market stream connected, subscribing to %d symbols", len(symbols))
			// Send subscription
			for _, sym := range symbols {
				msg := map[string]any{
					"op": "subscribe",
					"args": []any{
						map[string]any{"channel": "ticker", "instType": "MC", "instId": sym},
						map[string]any{"channel": "book", "instType": "MC", "instId": sym, "size": "5"},
						map[string]any{"channel": "deal", "instType": "MC", "instId": sym},
						map[string]any{"channel": "candlestick", "instType": "MC", "instId": sym, "granularity": "1min"},
					},
				}
				b.streamHub.SendJSON("market", msg)
			}
		},
		OnDisconnected: func(err error) {
			b.mu.Lock()
			b.wsConnected = false
			b.mu.Unlock()
			if err != nil {
				log.Printf("[Bitget] Market stream disconnected: %v", err)
			}
		},
	})

	b.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

func (b *BitgetAdapter) StartUserStream() error {
	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: "wss://ws.bitget.com/v2/ws/",
		OnMessage: func(msg []byte) {
			var raw map[string]any
			if err := json.Unmarshal(msg, &raw); err == nil {
				log.Printf("[Bitget] User stream: %s", string(msg[:min(200, len(msg))]))
			}
		},
		OnConnected: func() {
			log.Printf("[Bitget] User stream connected")
		},
		OnDisconnected: func(err error) {
			if err != nil {
				log.Printf("[Bitget] User stream disconnected: %v", err)
			}
		},
	})

	b.streamHub.Add("user", wsClient)
	return wsClient.Connect()
}

func (b *BitgetAdapter) handleMarketMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	channelType, _ := raw["cardType"].(string)
	_ = channelType
	data, ok := raw["data"].([]any)
	if !ok || len(data) == 0 {
		return
	}
	d0, ok := data[0].(map[string]any)
	if !ok {
		return
	}

	// Bitget WS messages: detect channel by field presence
	if instID, _ := d0["instId"].(string); instID != "" {
		if _, ok := d0["open"]; ok {
			// Candlestick data
			if b.onKline != nil {
				b.handleBitgetCandlestick(instID, d0)
			}
		} else if _, ok := d0["close"]; ok {
			// Ticker
			if b.onTicker != nil {
				b.onTicker(model.Tick{
					Symbol:    instID,
					Bid:       parseFloatSafe(d0["buyOne"]),
					Ask:       parseFloatSafe(d0["sellOne"]),
					Last:      parseFloatSafe(d0["close"]),
					Volume:    parseFloatSafe(d0["baseVol"]),
					Timestamp: int64(parseFloatSafe(d0["ts"])),
				})
			}
		} else if _, ok := d0["asks"]; ok {
			// Order book
			bids := bitgetDepth(d0["bids"], nil)
			asks := bitgetDepth(d0["asks"], nil)
			if b.onOrderBook != nil {
				b.onOrderBook(model.OrderBookData{
					Symbol:    instID,
					Bids:      bids,
					Asks:      asks,
					Timestamp: int64(parseFloatSafe(d0["ts"])),
				})
			}
		} else if _, ok := d0["price"].(string); ok {
			// Trade
			if b.onTrade != nil {
				side := "BUY"
				if s, _ := d0["side"].(string); s == "sell" {
					side = "SELL"
				}
				b.onTrade(model.TradeData{
					Symbol:    instID,
					ID:        getString(d0, "tradeId", ""),
					Price:     parseFloatSafe(d0["price"]),
					Quantity:  parseFloatSafe(d0["baseVol"]),
					Side:      side,
					Timestamp: int64(parseFloatSafe(d0["ts"])),
				})
			}
		}
	}
}

func (b *BitgetAdapter) handleBitgetCandlestick(instID string, d0 map[string]any) {
	// Bitget candlestick: data[data][0] = [timestamp, open, close, high, low, volume]
	if dataRaw, ok := d0["data"].([]any); ok && len(dataRaw) > 0 {
		arr, ok := dataRaw[0].([]any)
		if !ok || len(arr) < 6 {
			return
		}
		b.mu.RLock()
		fn := b.onKline
		b.mu.RUnlock()
		if fn != nil {
			fn(model.Bar{
				Symbol:   instID,
				Open:     parseFloatSafe(arr[1]),
				High:     parseFloatSafe(arr[2]),
				Low:      parseFloatSafe(arr[3]),
				Close:    parseFloatSafe(arr[4]),
				Volume:   parseFloatSafe(arr[5]),
				Interval: "1m",
				Time:     int64(parseFloatSafe(arr[0])),
			})
		}
	}
}

func bitgetDepth(rawData any, _ any) [][2]float64 {
	arr, ok := rawData.([]any)
	if !ok {
		return nil
	}
	depth := make([][2]float64, 0, len(arr))
	for _, item := range arr {
		pair, ok := item.([]any)
		if !ok || len(pair) < 2 {
			continue
		}
		depth = append(depth, [2]float64{
			floatPair(pair[0]),
			floatPair(pair[1]),
		})
	}
	return depth
}

func floatPair(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func (b *BitgetAdapter) Stop() error {
	b.mu.Lock()
	b.wsConnected = false
	hub := b.streamHub
	b.mu.Unlock()
	if hub != nil {
		hub.CloseAll()
	}
	return nil
}

func (b *BitgetAdapter) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.wsConnected
}

// ── Helpers ────────────────────────────────────────────────────

func toBitgetInterval(interval string) string {
	m := map[string]string{
		"1m": "1min", "5m": "5min", "15m": "15min", "30m": "30min",
		"1h": "1H", "4h": "4H", "1d": "1D", "1w": "1W", "1M": "1M",
	}
	if v, ok := m[interval]; ok {
		return v
	}
	return "1H"
}
