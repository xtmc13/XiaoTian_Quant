package adapter

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

const (
	GateIORestURL = "https://api.gateio.ws/api/v4"
	GateIOWsURL   = "wss://api.gateio.ws/ws/v4/"
)

// GateIOAdapter provides Gate.io REST and WebSocket integration.
type GateIOAdapter struct {
	apiKey     string
	secretKey  string
	httpClient *http.Client
	mu         sync.RWMutex

	streamHub   *exchange.StreamHub
	wsConnected bool

	onTicker    func(tick model.Tick)
	onOrderBook func(ob model.OrderBookData)
	onTrade     func(trade model.TradeData)
	onKline     func(bar model.Bar)

	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewGateIOAdapter(apiKey, secret string) *GateIOAdapter {
	return &GateIOAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (g *GateIOAdapter) Name() string    { return "gateio" }
func (g *GateIOAdapter) Start() error     { return nil }
func (g *GateIOAdapter) Stop() error      { g.streamHub.CloseAll(); return nil }
func (g *GateIOAdapter) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.wsConnected
}

func (g *GateIOAdapter) OnTicker(fn func(tick model.Tick))          { g.onTicker = fn }
func (g *GateIOAdapter) OnOrderBook(fn func(ob model.OrderBookData)) { g.onOrderBook = fn }
func (g *GateIOAdapter) OnTrade(fn func(trade model.TradeData))      { g.onTrade = fn }
func (g *GateIOAdapter) OnKline(fn func(bar model.Bar))              { g.onKline = fn }

// ── Gate.io Signing (HMAC-SHA512 of body hashed to hex) ──

func (g *GateIOAdapter) sign(method, path, query, body string, timestamp string) string {
	payload := strings.ToUpper(method) + "\n" + path + "\n" + query + "\n" + body + "\n" + timestamp
	mac := hmac.New(sha512.New, []byte(g.secretKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (g *GateIOAdapter) request(method, path string, params url.Values, body map[string]any) (map[string]any, error) {
	var bodyStr string
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyStr = string(data)
		bodyReader = strings.NewReader(bodyStr)
	}

	queryStr := ""
	if params != nil {
		queryStr = params.Encode()
	}

	reqURL := GateIORestURL + path
	if queryStr != "" {
		reqURL += "?" + queryStr
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	payloadHash := sha512Hash(bodyStr)
	sig := g.sign(method, path, queryStr, payloadHash, timestamp)

	req.Header.Set("KEY", g.apiKey)
	req.Header.Set("SIGN", sig)
	req.Header.Set("Timestamp", timestamp)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

func sha512Hash(s string) string {
	h := sha512.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// ── REST Market Data ──

func (g *GateIOAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	params := url.Values{}
	params.Set("currency_pair", symbol)
	params.Set("interval", interval)
	params.Set("limit", fmt.Sprintf("%d", limit))

	u, _ := url.Parse(GateIORestURL + "/spot/candlesticks")
	u.RawQuery = params.Encode()

	resp, err := g.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw []any
	json.Unmarshal(body, &raw)

	var output [][]any
	for _, r := range raw {
		if arr, ok := r.([]any); ok && len(arr) >= 6 {
			output = append(output, []any{arr[0], arr[2], arr[3], arr[4], arr[5], arr[1]})
		}
	}
	return output, nil
}

func (g *GateIOAdapter) GetTicker(symbol string) (map[string]any, error) {
	params := url.Values{}
	params.Set("currency_pair", symbol)

	u, _ := url.Parse(GateIORestURL + "/spot/tickers")
	u.RawQuery = params.Encode()

	resp, err := g.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw []any
	json.Unmarshal(body, &raw)

	if len(raw) > 0 {
		if m, ok := raw[0].(map[string]any); ok {
			return m, nil
		}
	}
	return nil, nil
}

// ── REST Trading ──

func (g *GateIOAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	body := map[string]any{
		"currency_pair": symbol,
		"side":          strings.ToLower(side),
		"type":          strings.ToLower(orderType),
		"amount":        fmt.Sprintf("%.6f", quantity),
	}
	if strings.ToLower(orderType) == "limit" {
		body["price"] = fmt.Sprintf("%.2f", price)
		body["time_in_force"] = "gtc"
	}
	return g.request("POST", "/spot/orders", nil, body)
}

func (g *GateIOAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	params := url.Values{}
	params.Set("currency_pair", symbol)
	return g.request("DELETE", "/spot/orders/"+orderID, params, nil)
}

func (g *GateIOAdapter) GetBalance() ([]map[string]any, error) {
	result, err := g.request("GET", "/spot/accounts", nil, nil)
	if err != nil {
		return nil, err
	}

	var accounts []map[string]any
	if arr, ok := result["detail"]; ok {
		// Error response
		return nil, fmt.Errorf("balance error: %v", arr)
	}

	// Gate.io returns array directly inside result for some endpoints
	if arr, ok := result["accounts"]; ok {
		if list, ok2 := arr.([]any); ok2 {
			for _, a := range list {
				if m, ok3 := a.(map[string]any); ok3 {
					accounts = append(accounts, m)
				}
			}
		}
	} else {
		// Try direct array
		raw, _ := json.Marshal(result)
		var list []map[string]any
		json.Unmarshal(raw, &list)
		for _, m := range list {
			accounts = append(accounts, m)
		}
	}
	return accounts, nil
}

func (g *GateIOAdapter) GetPositions() ([]map[string]any, error) {
	// Spot account doesn't have positions in the same sense
	result, err := g.request("GET", "/futures/usdt/positions", nil, nil)
	if err != nil {
		return nil, nil
	}
	var positions []map[string]any
	if arr, ok := result["positions"]; ok {
		if list, ok2 := arr.([]any); ok2 {
			for _, p := range list {
				if m, ok3 := p.(map[string]any); ok3 {
					positions = append(positions, m)
				}
			}
		}
	}
	return positions, nil
}

func (g *GateIOAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("currency_pair", symbol)
	}
	result, err := g.request("GET", "/spot/open_orders", params, nil)
	if err != nil {
		return nil, err
	}

	var orders []map[string]any
	if arr, ok := result["orders"]; ok {
		if list, ok2 := arr.([]any); ok2 {
			for _, o := range list {
				if m, ok3 := o.(map[string]any); ok3 {
					orders = append(orders, m)
				}
			}
		}
	}
	return orders, nil
}

// ── WebSocket Market Streams ──

func (g *GateIOAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: GateIOWsURL,
		OnMessage: func(msg []byte) {
			g.handleStreamMessage(msg)
		},
		OnConnected: func() {
			g.mu.Lock()
			g.wsConnected = true
			g.mu.Unlock()
			g.subscribe(symbols)
		},
		OnDisconnected: func(err error) {
			g.mu.Lock()
			g.wsConnected = false
			g.mu.Unlock()
			log.Printf("[GateIO] Stream disconnected: %v", err)
		},
	})

	g.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

func (g *GateIOAdapter) subscribe(symbols []string) {
	client := g.streamHub.Get("market")
	if client == nil {
		return
	}

	var payload []string
	for _, sym := range symbols {
		payload = append(payload,
			"spot.tickers:"+sym,
			"spot.order_book_update:"+sym,
			"spot.trades:"+sym,
			"spot.candlesticks:"+sym+":1m",
		)
	}

	msg := map[string]any{
		"time":    time.Now().Unix(),
		"channel": "spot.tickers",
		"event":   "subscribe",
		"payload": payload,
	}
	client.SendJSON(msg)
}

func (g *GateIOAdapter) StartUserStream() error {
	log.Printf("[GateIO] User stream not separately implemented; use private WS channel")
	return nil
}

func (g *GateIOAdapter) handleStreamMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	channel, _ := raw["channel"].(string)
	result, _ := raw["result"].(map[string]any)
	if result == nil {
		return
	}

	switch {
	case strings.HasPrefix(channel, "spot.tickers"):
		if g.onTicker != nil {
			currencyPair, _ := result["currency_pair"].(string)
			g.onTicker(model.Tick{
				Symbol:    currencyPair,
				Last:      parseFloat(result["last"]),
				Bid:       parseFloat(result["highest_bid"]),
				Ask:       parseFloat(result["lowest_ask"]),
				Volume:    parseFloat(result["quote_volume"]),
				Timestamp: time.Now().UnixMilli(),
			})
		}
	case strings.HasPrefix(channel, "spot.order_book"):
		if g.onOrderBook != nil {
			currencyPair, _ := result["currency_pair"].(string)
			ob := model.OrderBookData{Symbol: currencyPair, Timestamp: time.Now().UnixMilli()}
			if bids, ok := result["bids"].([]any); ok {
				for _, b := range bids {
					if arr, ok2 := b.([]any); ok2 && len(arr) >= 2 {
						ob.Bids = append(ob.Bids, [2]float64{parseFloat(arr[0]), parseFloat(arr[1])})
					}
				}
			}
			if asks, ok := result["asks"].([]any); ok {
				for _, a := range asks {
					if arr, ok2 := a.([]any); ok2 && len(arr) >= 2 {
						ob.Asks = append(ob.Asks, [2]float64{parseFloat(arr[0]), parseFloat(arr[1])})
					}
				}
			}
			g.onOrderBook(ob)
		}
	case strings.HasPrefix(channel, "spot.trades"):
		if g.onTrade != nil {
			currencyPair, _ := result["currency_pair"].(string)
			side := "BUY"
			if s, _ := result["side"].(string); s == "sell" {
				side = "SELL"
			}
			g.onTrade(model.TradeData{
				Symbol:    currencyPair,
				ID:        fmt.Sprint(result["id"]),
				Price:     parseFloat(result["price"]),
				Quantity:  parseFloat(result["amount"]),
				Side:      side,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	case strings.Contains(channel, "candlestick"):
		if g.onKline != nil {
			currencyPair, _ := result["currency_pair"].(string)
			g.onKline(model.Bar{
				Symbol:   currencyPair,
				Open:     parseFloat(result["o"]),
				High:     parseFloat(result["h"]),
				Low:      parseFloat(result["l"]),
				Close:    parseFloat(result["c"]),
				Volume:   parseFloat(result["v"]),
				Interval: "1m",
				Time:     time.Now().UnixMilli(),
			})
		}
	}
}
