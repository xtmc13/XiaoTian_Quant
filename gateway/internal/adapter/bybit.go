package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)


const (
	BybitRestURL    = "https://api.bybit.com/v5"
	BybitWsPublicURL = "wss://stream.bybit.com/v5/public/spot"
	BybitTestURL    = "https://api-testnet.bybit.com/v5"
)

// BybitAdapter provides Bybit exchange integration (REST + WebSocket).
// Bybit uses HMAC-SHA256 signing similar to Binance but with different
// parameter ordering and timestamp format.
type BybitAdapter struct {
	apiKey    string
	secretKey string
	testnet   bool
	httpClient *http.Client
	mu        sync.RWMutex

	streamHub *exchange.StreamHub
	wsConnected bool

	onTicker    func(tick model.Tick)
	onOrderBook func(ob model.OrderBookData)
	onTrade     func(trade model.TradeData)
	onKline     func(bar model.Bar)

	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewBybitAdapter(apiKey, secret string, testnet bool) *BybitAdapter {
	return &BybitAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		testnet:    testnet,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (b *BybitAdapter) OnTicker(fn func(tick model.Tick))          { b.onTicker = fn }
func (b *BybitAdapter) OnOrderBook(fn func(ob model.OrderBookData)) { b.onOrderBook = fn }

func (b *BybitAdapter) Name() string { return "bybit" }

func (b *BybitAdapter) Start() error { return nil }

func (b *BybitAdapter) Stop() error {
	b.mu.Lock()
	b.wsConnected = false
	hub := b.streamHub
	b.mu.Unlock()
	if hub != nil {
		hub.CloseAll()
	}
	return nil
}

// ── REST Helpers ───────────────────────────────────────────────

func (b *BybitAdapter) baseURL() string {
	if b.testnet {
		return BybitTestURL
	}
	return BybitRestURL
}

// signBybit generates a Bybit V5 HMAC-SHA256 signature.
// Bybit expects: timestamp + apiKey + recvWindow + queryString (sorted)
func (b *BybitAdapter) signBybit(timestamp int64, params map[string]any) string {
	// Build sorted query string
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, params[k]))
	}
	queryStr := strings.Join(parts, "&")

	signStr := fmt.Sprintf("%d%s5000%s", timestamp, b.apiKey, queryStr)
	mac := hmac.New(sha256.New, []byte(b.secretKey))
	mac.Write([]byte(signStr))
	return hex.EncodeToString(mac.Sum(nil))
}

// signedGet performs an authenticated GET request.
func (b *BybitAdapter) signedGet(endpoint string, params map[string]any) (map[string]any, error) {
	params["api_key"] = b.apiKey
	params["timestamp"] = time.Now().UnixMilli()
	params["recv_window"] = "5000"
	params["sign"] = b.signBybit(params["timestamp"].(int64), params)

	u, _ := url.Parse(b.baseURL() + endpoint)
	q := u.Query()
	for k, v := range params {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bybit GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("bybit parse error: %w, body: %s", err, string(body))
	}

	// Bybit V5 response envelope
	if retCode, ok := result["retCode"].(float64); ok && retCode != 0 {
		retMsg, _ := result["retMsg"].(string)
		return nil, fmt.Errorf("bybit error [%v]: %s", retCode, retMsg)
	}

	return result, nil
}

// signedPost performs an authenticated POST request.
func (b *BybitAdapter) signedPost(endpoint string, body map[string]any) (map[string]any, error) {
	body["api_key"] = b.apiKey
	body["timestamp"] = time.Now().UnixMilli()
	body["recv_window"] = "5000"
	body["sign"] = b.signBybit(body["timestamp"].(int64), body)

	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", b.baseURL()+endpoint, strings.NewReader(string(jsonBody)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bybit POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("bybit parse error: %w", err)
	}

	if retCode, ok := result["retCode"].(float64); ok && retCode != 0 {
		retMsg, _ := result["retMsg"].(string)
		return nil, fmt.Errorf("bybit error [%v]: %s", retCode, retMsg)
	}

	return result, nil
}

// ── Market Data ────────────────────────────────────────────────

func (b *BybitAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	category := "spot"
	params := map[string]any{
		"category": category,
		"symbol":   symbol,
		"interval": toBybitInterval(interval),
		"limit":    fmt.Sprintf("%d", clampLimit(limit, 1, 1000)),
	}

	result, err := b.signedGet("/market/kline", params)
	if err != nil {
		return nil, err
	}

	list, ok := result["result"].(map[string]any)["list"].([]any)
	if !ok {
		return nil, fmt.Errorf("bybit klines: unexpected response format")
	}

	// Bybit returns newest first; reverse to chronological
	klines := make([][]any, len(list))
	for i, item := range list {
		arr, ok := item.([]any)
		if !ok || len(arr) < 6 {
			continue
		}
		// Map to Binance-compatible format: [time, open, high, low, close, volume]
		klines[len(list)-1-i] = []any{
			parseBybitInt(arr[0]), // timestamp
			parseBybitFloat(arr[1]), // open
			parseBybitFloat(arr[2]), // high
			parseBybitFloat(arr[3]), // low
			parseBybitFloat(arr[4]), // close
			parseBybitFloat(arr[5]), // volume
		}
	}
	return klines, nil
}

// ── Account ────────────────────────────────────────────────────

func (b *BybitAdapter) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.wsConnected
}

func (b *BybitAdapter) GetBalance() ([]map[string]any, error) {
	params := map[string]any{
		"accountType": "UNIFIED",
	}

	result, err := b.signedGet("/account/wallet-balance", params)
	if err != nil {
		return nil, err
	}

	balances := make([]map[string]any, 0)
	list, ok := result["result"].(map[string]any)["list"].([]any)
	if !ok {
		return balances, nil
	}

	for _, item := range list {
		acc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		coinArr, ok := acc["coin"].([]any)
		if !ok {
			continue
		}
		for _, coinItem := range coinArr {
			coin, ok := coinItem.(map[string]any)
			if !ok {
				continue
			}
			name, _ := coin["coin"].(string)
			walletBal, _ := coin["walletBalance"].(string)
			balances = append(balances, map[string]any{
				"asset":  name,
				"free":   parseBybitFloat(walletBal),
				"locked": 0.0,
			})
		}
	}
	return balances, nil
}

// PlaceFuturesOrder places a USDT-M perpetual futures order on Bybit.
func (b *BybitAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	body := map[string]any{
		"category":  "linear",
		"symbol":    symbol,
		"side":      strings.ToUpper(side),
		"orderType": strings.ToUpper(orderType),
		"qty":       fmt.Sprintf("%.6f", quantity),
	}

	if strings.ToUpper(orderType) == "LIMIT" {
		body["price"] = fmt.Sprintf("%.2f", price)
	}

	// Position index for hedge mode
	// 0 = one-way mode, 1 = buy side of hedge mode, 2 = sell side of hedge mode
	if positionSide != "" {
		posIdx := 0
		if strings.ToUpper(positionSide) == "LONG" {
			posIdx = 1
		} else if strings.ToUpper(positionSide) == "SHORT" {
			posIdx = 2
		}
		body["positionIdx"] = posIdx
	}

	result, err := b.signedPost("/order/create", body)
	if err != nil {
		return nil, err
	}

	orderResult, _ := result["result"].(map[string]any)
	if orderResult == nil {
		return nil, fmt.Errorf("bybit futures order: empty result")
	}

	return map[string]any{
		"orderId": orderResult["orderId"],
		"symbol":  symbol,
		"side":    side,
		"type":    orderType,
		"status":  orderResult["orderStatus"],
	}, nil
}

func (b *BybitAdapter) GetPositions() ([]map[string]any, error) {
	// Try futures positions first (quant platforms primarily use derivatives)
	params := map[string]any{
		"category": "linear",
	}
	result, err := b.signedGet("/position/list", params)
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	list, _ := res["list"].([]any)

	positions := make([]map[string]any, 0)
	for _, item := range list {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		side := getString(p, "side", "")
		if side == "" {
			side = getString(p, "positionSide", "Buy")
		}
		posSide := "LONG"
		if side == "Sell" || side == "SHORT" {
			posSide = "SHORT"
		}
		positions = append(positions, map[string]any{
			"symbol":        getString(p, "symbol", ""),
			"positionAmt":   parseFloatStr(p, "size"),
			"entryPrice":    parseFloatStr(p, "avgPrice"),
			"markPrice":     parseFloatStr(p, "markPrice"),
			"positionSide":  posSide,
			"leverage":      parseFloatStr(p, "leverage"),
			"unrealizedPnl": parseFloatStr(p, "unrealisedPnl"),
			"liqPrice":      parseFloatStr(p, "liqPrice"),
		})
	}

	// Spot doesn't have traditional positions; return futures positions (or empty if none)
	return positions, nil
}

func (b *BybitAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return b.GetOrders(symbol)
}

func (b *BybitAdapter) GetTicker(symbol string) (map[string]any, error) {
	params := map[string]any{
		"category": "spot",
		"symbol":   symbol,
	}
	result, err := b.signedGet("/market/tickers", params)
	if err != nil {
		return nil, err
	}

	list, ok := result["result"].(map[string]any)["list"].([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("bybit ticker: no data for %s", symbol)
	}

	ticker := list[0].(map[string]any)
	return map[string]any{
		"symbol": symbol,
		"last":   parseBybitFloat(ticker["lastPrice"]),
		"bid":    parseBybitFloat(ticker["bid1Price"]),
		"ask":    parseBybitFloat(ticker["ask1Price"]),
		"high":   parseBybitFloat(ticker["highPrice24h"]),
		"low":    parseBybitFloat(ticker["lowPrice24h"]),
		"volume": parseBybitFloat(ticker["volume24h"]),
	}, nil
}

func (b *BybitAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	var args []string
	for _, sym := range symbols {
		upper := strings.ToUpper(sym)
		args = append(args,
			fmt.Sprintf("tickers.%s", upper),
			fmt.Sprintf("books5.%s", upper),
			fmt.Sprintf("trades.%s", upper),
			fmt.Sprintf("kline.1.%s", upper),
		)
	}

	wsURL := BybitWsPublicURL
	if b.testnet {
		wsURL = "wss://stream-testnet.bybit.com/v5/public/spot"
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL:   wsURL,
		PingInterval: 30 * time.Second,
		PongTimeout:  10 * time.Second,
		ReconnectDelay: 5 * time.Second,
		OnMessage: func(msg []byte) {
			b.handleMarketMessage(msg)
		},
		OnConnected: func() {
			b.mu.Lock()
			b.wsConnected = true
			b.mu.Unlock()
			log.Printf("[Bybit] Market stream connected, subscribing to %d symbols", len(symbols))
			// Send subscription via JSON-RPC
			b.mu.RLock()
			streamHub := b.streamHub
			b.mu.RUnlock()
			if streamHub != nil {
				for _, sym := range symbols {
					msg := map[string]any{
						"op": "subscribe",
						"args": []any{
							fmt.Sprintf("tickers.%s", strings.ToUpper(sym)),
							fmt.Sprintf("books5.%s", strings.ToUpper(sym)),
							fmt.Sprintf("trades.%s", strings.ToUpper(sym)),
							fmt.Sprintf("kline.1.%s", strings.ToUpper(sym)),
						},
					}
					streamHub.SendJSON("market", msg)
				}
			}
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

func (b *BybitAdapter) StartUserStream() error {
	wsURL := "wss://stream.bybit.com/v5/public/linear"
	if b.testnet {
		wsURL = "wss://stream-testnet.bybit.com/v5/public/linear"
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL:          wsURL,
		PingInterval: 30 * time.Second,
		PongTimeout:  10 * time.Second,
		ReconnectDelay: 5 * time.Second,
		OnMessage: func(msg []byte) {
			// Parse user stream events (execution, position, etc.)
			var raw map[string]any
			if err := json.Unmarshal(msg, &raw); err == nil {
				log.Printf("[Bybit] User stream: %s", string(msg[:min(200, len(msg))]))
			}
		},
		OnConnected: func() {
			log.Printf("[Bybit] User stream connected")
		},
		OnDisconnected: func(err error) {
			if err != nil {
				log.Printf("[Bybit] User stream disconnected: %v", err)
			}
		},
	})

	b.streamHub.Add("user", wsClient)
	return wsClient.Connect()
}

func (b *BybitAdapter) handleMarketMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	// Bybit V5 WS messages have "topic" and "data" for pub channels
	topic, _ := raw["topic"].(string)
	data, ok := raw["data"].([]any)
	if !ok || len(data) == 0 {
		return
	}

	ts, _ := raw["ts"].(float64)

	switch {
	case strings.HasPrefix(topic, "tickers."):
		ticker, ok := data[0].(map[string]any)
		if !ok {
			return
		}
		if b.onTicker != nil {
			symbol := strings.TrimPrefix(topic, "tickers.")
			b.onTicker(model.Tick{
				Symbol:    symbol,
				Bid:       parseBybitFloat(ticker["bid1Price"]),
				Ask:       parseBybitFloat(ticker["ask1Price"]),
				Last:      parseBybitFloat(ticker["lastPrice"]),
				Volume:    parseBybitFloat(ticker["volume24h"]),
				Timestamp: int64(ts),
			})
		}

	case strings.HasPrefix(topic, "books5."):
		obData, ok := data[0].(map[string]any)
		if !ok {
			return
		}
		bidsRaw, _ := obData["b"].([]any)
		asksRaw, _ := obData["a"].([]any)
		bids := parseBybitDepth(bidsRaw)
		asks := parseBybitDepth(asksRaw)
		if b.onOrderBook != nil {
			symbol := strings.TrimPrefix(topic, "books5.")
			b.onOrderBook(model.OrderBookData{
				Symbol:    symbol,
				Bids:      bids,
				Asks:      asks,
				Timestamp: int64(ts),
			})
		}

	case strings.HasPrefix(topic, "trades."):
		tradeData, ok := data[0].(map[string]any)
		if !ok {
			return
		}
		if b.onTrade != nil {
			symbol := strings.TrimPrefix(topic, "trades.")
			side := "BUY"
			if s, _ := tradeData["S"].(string); s == "S" {
				side = "SELL"
			}
			b.onTrade(model.TradeData{
				Symbol:    symbol,
				ID:        getString(tradeData, "id", ""),
				Price:     parseBybitFloat(tradeData["p"]),
				Quantity:  parseBybitFloat(tradeData["v"]),
				Side:      side,
				Timestamp: int64(ts),
			})
		}

	case strings.HasPrefix(topic, "kline."):
		klineData, ok := data[0].(map[string]any)
		if !ok {
			return
		}
		if b.onKline != nil {
			symbol := strings.TrimPrefix(topic, "kline.")
			b.onKline(model.Bar{
				Symbol:   symbol,
				Open:     parseBybitFloat(klineData["open"]),
				High:     parseBybitFloat(klineData["high"]),
				Low:      parseBybitFloat(klineData["low"]),
				Close:    parseBybitFloat(klineData["close"]),
				Volume:   parseBybitFloat(klineData["volume"]),
				Interval: strings.TrimPrefix(topic, "kline."),
				Time:     int64(ts),
			})
		}
	}
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

func parseFloatSafe2(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		var f float64
		_, err := fmt.Sscanf(val, "%f", &f)
		return f, err
	}
	return 0, fmt.Errorf("not a number")
}

func (b *BybitAdapter) SubscribeKline(symbol, interval string, callback func(bar model.Bar)) error {
	b.mu.Lock()
	if callback != nil {
		b.onKline = callback
	}
	b.mu.Unlock()
	return nil
}

// ── Orders ─────────────────────────────────────────────────────

func (b *BybitAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	body := map[string]any{
		"category": "spot",
		"symbol":   symbol,
		"side":     strings.ToUpper(side),
		"orderType": strings.ToUpper(orderType),
		"qty":      fmt.Sprintf("%.6f", quantity),
	}

	if strings.ToUpper(orderType) == "LIMIT" {
		body["price"] = fmt.Sprintf("%.2f", price)
	}

	result, err := b.signedPost("/order/create", body)
	if err != nil {
		return nil, err
	}

	orderResult, _ := result["result"].(map[string]any)
	if orderResult == nil {
		return nil, fmt.Errorf("bybit order: empty result")
	}

	order := map[string]any{
		"order_id":  orderResult["orderId"],
		"symbol":    symbol,
		"side":      side,
		"type":      orderType,
		"price":     orderResult["price"],
		"quantity":  orderResult["qty"],
		"status":    orderResult["orderStatus"],
	}

	return order, nil
}

func (b *BybitAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	body := map[string]any{
		"category": "spot",
		"symbol":   symbol,
		"orderId":  orderID,
	}

	result, err := b.signedPost("/order/cancel", body)
	if err != nil {
		return nil, err
	}
	return result["result"].(map[string]any), nil
}

func (b *BybitAdapter) GetOrders(symbol string) ([]map[string]any, error) {
	params := map[string]any{
		"category": "spot",
		"symbol":   symbol,
		"limit":    "50",
	}

	result, err := b.signedGet("/order/realtime", params)
	if err != nil {
		return nil, err
	}

	orders := make([]map[string]any, 0)
	list, ok := result["result"].(map[string]any)["list"].([]any)
	if !ok {
		return orders, nil
	}

	for _, item := range list {
		if ord, ok := item.(map[string]any); ok {
			orders = append(orders, map[string]any{
				"order_id": ord["orderId"],
				"symbol":   ord["symbol"],
				"side":     ord["side"],
				"type":     ord["orderType"],
				"price":    ord["price"],
				"quantity": ord["qty"],
				"status":   ord["orderStatus"],
			})
		}
	}
	return orders, nil
}

// ── Set Callbacks ──────────────────────────────────────────────

func (b *BybitAdapter) SetOnKline(fn func(bar model.Bar)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onKline = fn
}

// ── Helpers ────────────────────────────────────────────────────

func toBybitInterval(interval string) string {
	m := map[string]string{
		"1m": "1", "3m": "3", "5m": "5", "15m": "15", "30m": "30",
		"1h": "60", "4h": "240", "1d": "D", "1w": "W", "1M": "M",
	}
	if v, ok := m[interval]; ok {
		return v
	}
	return "60"
}

func parseBybitFloat(v any) float64 {
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

func parseBybitInt(v any) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case string:
		var n int64
		fmt.Sscanf(val, "%d", &n)
		return n
	}
	return 0
}

func clampLimit(limit, min, max int) int {
	if limit < min {
		return min
	}
	if limit > max {
		return max
	}
	return limit
}
