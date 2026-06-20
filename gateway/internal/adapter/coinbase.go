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
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

const (
	CoinbaseRestURL = "https://api.coinbase.com/api/v3/brokerage"
	CoinbaseWsURL   = "wss://advanced-trade-ws.coinbase.com"
)

// CoinbaseAdapter provides Coinbase Advanced Trade API integration.
type CoinbaseAdapter struct {
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

func NewCoinbaseAdapter(apiKey, secret string) *CoinbaseAdapter {
	return &CoinbaseAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (cb *CoinbaseAdapter) Name() string    { return "coinbase" }
func (cb *CoinbaseAdapter) Start() error     { return nil }
func (cb *CoinbaseAdapter) Stop() error      { cb.streamHub.CloseAll(); return nil }
func (cb *CoinbaseAdapter) IsConnected() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.wsConnected
}

func (cb *CoinbaseAdapter) OnTicker(fn func(tick model.Tick))          { cb.onTicker = fn }
func (cb *CoinbaseAdapter) OnOrderBook(fn func(ob model.OrderBookData)) { cb.onOrderBook = fn }
func (cb *CoinbaseAdapter) OnTrade(fn func(trade model.TradeData))      { cb.onTrade = fn }
func (cb *CoinbaseAdapter) OnKline(fn func(bar model.Bar))              { cb.onKline = fn }

// ── Coinbase Signing (CB-ACCESS-SIGN = HMAC-SHA256(timestamp + method + path + body)) ──

func (cb *CoinbaseAdapter) sign(timestamp, method, path, body string) string {
	mac := hmac.New(sha256.New, []byte(cb.secretKey))
	mac.Write([]byte(timestamp + method + path + body))
	return hex.EncodeToString(mac.Sum(nil))
}

func (cb *CoinbaseAdapter) request(method, path string, body map[string]any) (map[string]any, error) {
	var bodyStr string
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyStr = string(data)
		bodyReader = strings.NewReader(bodyStr)
	}

	reqURL := CoinbaseRestURL + path
	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	req.Header.Set("CB-ACCESS-KEY", cb.apiKey)
	req.Header.Set("CB-ACCESS-SIGN", cb.sign(timestamp, method, path, bodyStr))
	req.Header.Set("CB-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

// ── REST Market Data ──

func (cb *CoinbaseAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	u, _ := url.Parse(CoinbaseRestURL + "/products/" + symbol + "/candles")
	params := url.Values{}
	params.Set("granularity", coinbaseGranularity(interval))
	params.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = params.Encode()

	resp, err := cb.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)

	candles, _ := result["candles"].([]any)
	var output [][]any
	for _, c := range candles {
		if arr, ok := c.([]any); ok && len(arr) >= 6 {
			output = append(output, []any{arr[0], arr[3], arr[2], arr[1], arr[4], arr[5]})
		}
	}
	return output, nil
}

func (cb *CoinbaseAdapter) GetTicker(symbol string) (map[string]any, error) {
	u := CoinbaseRestURL + "/products/" + symbol
	resp, err := cb.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)
	return result, nil
}

// ── REST Trading ──

func (cb *CoinbaseAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	body := map[string]any{
		"product_id":    symbol,
		"side":          strings.ToUpper(side),
		"order_configuration": map[string]any{
			strings.ToLower(orderType) + "_gtc": map[string]any{
				"base_size":   fmt.Sprintf("%.6f", quantity),
				"limit_price": fmt.Sprintf("%.2f", price),
			},
		},
	}
	return cb.request("POST", "/orders", body)
}

func (cb *CoinbaseAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	body := map[string]any{"order_ids": []string{orderID}}
	return cb.request("POST", "/orders/batch_cancel", body)
}

func (cb *CoinbaseAdapter) GetBalance() ([]map[string]any, error) {
	result, err := cb.request("GET", "/accounts", nil)
	if err != nil {
		return nil, err
	}
	accounts, _ := result["accounts"].([]any)
	var balances []map[string]any
	for _, a := range accounts {
		if m, ok := a.(map[string]any); ok {
			balances = append(balances, m)
		}
	}
	return balances, nil
}

func (cb *CoinbaseAdapter) GetPositions() ([]map[string]any, error) {
	result, err := cb.request("GET", "/orders/fills", nil)
	if err != nil {
		return nil, err
	}
	fills, _ := result["fills"].([]any)
	var positions []map[string]any
	for _, f := range fills {
		if m, ok := f.(map[string]any); ok {
			positions = append(positions, m)
		}
	}
	return positions, nil
}

func (cb *CoinbaseAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	path := "/orders/historical/batch"
	if symbol != "" {
		path += "?product_id=" + symbol
	}
	result, err := cb.request("GET", path, nil)
	if err != nil {
		return nil, err
	}
	orders, _ := result["orders"].([]any)
	var out []map[string]any
	for _, o := range orders {
		if m, ok := o.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// ── WebSocket Market Streams ──

func (cb *CoinbaseAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: CoinbaseWsURL,
		OnMessage: func(msg []byte) {
			cb.handleStreamMessage(msg)
		},
		OnConnected: func() {
			cb.mu.Lock()
			cb.wsConnected = true
			cb.mu.Unlock()
			cb.subscribe(symbols)
		},
		OnDisconnected: func(err error) {
			cb.mu.Lock()
			cb.wsConnected = false
			cb.mu.Unlock()
			log.Printf("[Coinbase] Stream disconnected: %v", err)
		},
	})

	cb.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

func (cb *CoinbaseAdapter) subscribe(symbols []string) {
	client := cb.streamHub.Get("market")
	if client == nil {
		return
	}

	var productIDs []string
	for _, sym := range symbols {
		productIDs = append(productIDs, sym)
	}

	msg := map[string]any{
		"type":        "subscribe",
		"product_ids": productIDs,
		"channels":    []string{"ticker", "level2", "matches", "candles"},
	}
	client.SendJSON(msg)
}

func (cb *CoinbaseAdapter) StartUserStream() error {
	// Coinbase doesn't have a separate user data stream like Binance;
	// it uses the same WebSocket with authenticated channel subscriptions.
	log.Printf("[Coinbase] User stream: Coinbase uses authenticated channels on the same WS connection — user data is available via market stream subscription")
	return nil
}

func (cb *CoinbaseAdapter) handleStreamMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	msgType, _ := raw["type"].(string)
	switch msgType {
	case "ticker":
		if cb.onTicker != nil {
			productID, _ := raw["product_id"].(string)
			price, _ := raw["price"].(string)
			cb.onTicker(model.Tick{
				Symbol:    productID,
				Last:      parseFloat(price),
				Bid:       parseFloat(fmt.Sprint(raw["best_bid"])),
				Ask:       parseFloat(fmt.Sprint(raw["best_ask"])),
				Volume:    parseFloat(fmt.Sprint(raw["volume_24h"])),
				Timestamp: time.Now().UnixMilli(),
			})
		}
	case "l2update":
		if cb.onOrderBook != nil {
			productID, _ := raw["product_id"].(string)
			ob := model.OrderBookData{Symbol: productID, Timestamp: time.Now().UnixMilli()}
			if changes, ok := raw["changes"].([]any); ok {
				for _, ch := range changes {
					if arr, ok2 := ch.([]any); ok2 && len(arr) >= 3 {
						side, _ := arr[0].(string)
						price := parseFloat(arr[1])
						qty := parseFloat(arr[2])
						if side == "buy" {
							ob.Bids = append(ob.Bids, [2]float64{price, qty})
						} else {
							ob.Asks = append(ob.Asks, [2]float64{price, qty})
						}
					}
				}
			}
			cb.onOrderBook(ob)
		}
	case "match":
		if cb.onTrade != nil {
			productID, _ := raw["product_id"].(string)
			tradeID, _ := raw["trade_id"].(float64)
			side := "BUY"
			if s, _ := raw["side"].(string); s == "sell" {
				side = "SELL"
			}
			cb.onTrade(model.TradeData{
				Symbol:    productID,
				ID:        fmt.Sprintf("%.0f", tradeID),
				Price:     parseFloat(raw["price"]),
				Quantity:  parseFloat(raw["size"]),
				Side:      side,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	}
}

func coinbaseGranularity(interval string) string {
	switch interval {
	case "1m":
		return "60"
	case "5m":
		return "300"
	case "15m":
		return "900"
	case "1h":
		return "3600"
	case "6h":
		return "21600"
	case "1d":
		return "86400"
	default:
		return "3600"
	}
}
