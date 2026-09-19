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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

const (
	OKXRestURL   = "https://www.okx.com"
	OKXWsPubURL  = "wss://ws.okx.com:8443/ws/v5/public"
	OKXWsPrivURL = "wss://ws.okx.com:8443/ws/v5/private"
)

// OKXAdapter provides full OKX exchange integration including WebSocket streams.
type OKXAdapter struct {
	apiKey     string
	secretKey  string
	passphrase string
	testnet    bool
	httpClient *http.Client
	mu         sync.RWMutex

	streamHub   *exchange.StreamHub
	wsConnected bool

	// 公开行情 WS 订阅确认状态（key: channel:instId，value: 是否已确认）
	wsSubs   map[string]bool
	wsSubsMu sync.Mutex

	onTicker    func(tick model.Tick)
	onOrderBook func(ob model.OrderBookData)
	onTrade     func(trade model.TradeData)
	onKline     func(bar model.Bar)

	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewOKXAdapter(apiKey, secret, passphrase string, testnet bool) *OKXAdapter {
	return &OKXAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		passphrase: passphrase,
		testnet:    testnet,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (o *OKXAdapter) Name() string    { return "okx" }
func (o *OKXAdapter) Start() error    { return nil }
func (o *OKXAdapter) Stop() error     { o.streamHub.CloseAll(); return nil }
func (o *OKXAdapter) IsConnected() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.wsConnected
}

func (o *OKXAdapter) OnTicker(fn func(tick model.Tick))          { o.onTicker = fn }
func (o *OKXAdapter) OnOrderBook(fn func(ob model.OrderBookData)) { o.onOrderBook = fn }
func (o *OKXAdapter) OnTrade(fn func(trade model.TradeData))      { o.onTrade = fn }
func (o *OKXAdapter) OnKline(fn func(bar model.Bar))              { o.onKline = fn }

// ── OKX Signing ──

func (o *OKXAdapter) sign(timestamp, method, path, body string) string {
	preHash := timestamp + method + path + body
	mac := hmac.New(sha256.New, []byte(o.secretKey))
	mac.Write([]byte(preHash))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (o *OKXAdapter) request(method, path string, body map[string]any) (map[string]any, error) {
	u, _ := url.Parse(OKXRestURL + path)
	var bodyStr string
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyStr = string(data)
		bodyReader = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequest(method, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	req.Header.Set("OK-ACCESS-KEY", o.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", o.sign(timestamp, method, path, bodyStr))
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", o.passphrase)
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)

	// Check OKX error code
	if code, ok := result["code"].(string); ok && code != "0" {
		return result, fmt.Errorf("okx %s: %s", code, result["msg"])
	}
	if code, ok := result["code"].(float64); ok && code != 0 {
		return result, fmt.Errorf("okx %.0f: %v", code, result["msg"])
	}

	return result, nil
}

// ── REST Market Data ──

func (o *OKXAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	instID := toOKXInstID(symbol)
	u, _ := url.Parse(OKXRestURL + "/api/v5/market/candles")
	params := url.Values{}
	params.Set("instId", instID)
	params.Set("bar", interval)
	params.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = params.Encode()

	resp, err := o.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)

	data, _ := result["data"].([]any)
	var output [][]any
	for _, d := range data {
		if arr, ok := d.([]any); ok && len(arr) >= 6 {
			output = append(output, []any{arr[0], arr[1], arr[2], arr[3], arr[4], arr[5]})
		}
	}
	return output, nil
}

func (o *OKXAdapter) GetTicker(symbol string) (map[string]any, error) {
	instID := toOKXInstID(symbol)
	result, err := o.request("GET", "/api/v5/market/ticker?instId="+instID, nil)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].([]any)
	if len(data) > 0 {
		if m, ok := data[0].(map[string]any); ok {
			return m, nil
		}
	}
	return result, nil
}

// ── REST Trading ──

func (o *OKXAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	instID := toOKXInstID(symbol)
	body := map[string]any{
		"instId":  instID,
		"tdMode":  "cash",
		"side":    strings.ToLower(side),
		"ordType": strings.ToLower(orderType),
		"sz":      fmt.Sprintf("%.6f", quantity),
	}
	if strings.ToLower(orderType) == "limit" {
		body["px"] = fmt.Sprintf("%.2f", price)
	} else {
		body["ordType"] = "market"
	}
	return o.request("POST", "/api/v5/trade/order", body)
}

// PlaceFuturesOrder places a futures (swap) order on OKX.
func (o *OKXAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	instID := toOKXInstID(symbol)
	tdMode := "cross" // default cross margin
	if positionSide == "isolated" {
		tdMode = "isolated"
	}

	body := map[string]any{
		"instId":   instID,
		"instType": "SWAP",
		"tdMode":   tdMode,
		"side":     strings.ToLower(side),
		"ordType":  strings.ToLower(orderType),
		"sz":       fmt.Sprintf("%.6f", quantity),
		"lever":    fmt.Sprintf("%.0f", leverage),
	}

	// Position side for hedge mode
	if positionSide != "" {
		body["posSide"] = strings.ToLower(positionSide)
	}

	if strings.ToLower(orderType) == "limit" {
		body["px"] = fmt.Sprintf("%.2f", price)
	} else {
		body["ordType"] = "market"
	}

	return o.request("POST", "/api/v5/trade/order", body)
}

func (o *OKXAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	instID := toOKXInstID(symbol)
	body := map[string]any{
		"instId": instID,
		"ordId":  orderID,
	}
	return o.request("POST", "/api/v5/trade/cancel-order", body)
}

func (o *OKXAdapter) GetBalance() ([]map[string]any, error) {
	result, err := o.request("GET", "/api/v5/account/balance", nil)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].([]any)
	var balances []map[string]any
	for _, d := range data {
		if m, ok := d.(map[string]any); ok {
			details, _ := m["details"].([]any)
			for _, det := range details {
				if dm, ok := det.(map[string]any); ok {
					balances = append(balances, dm)
				}
			}
		}
	}
	return balances, nil
}

func (o *OKXAdapter) GetPositions() ([]map[string]any, error) {
	result, err := o.request("GET", "/api/v5/account/positions", nil)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].([]any)
	var positions []map[string]any
	for _, d := range data {
		if m, ok := d.(map[string]any); ok {
			positions = append(positions, m)
		}
	}
	return positions, nil
}

func (o *OKXAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	path := "/api/v5/trade/orders-pending"
	if symbol != "" {
		path += "?instId=" + toOKXInstID(symbol)
	}
	result, err := o.request("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var orders []map[string]any
	data, _ := result["data"].([]any)
	for _, d := range data {
		if m, ok := d.(map[string]any); ok {
			orders = append(orders, m)
		}
	}
	return orders, nil
}

// ── WebSocket Market Streams ──
// 公开行情 WS 实现见 okx_ws.go（订阅/心跳/重连/订阅状态查询）。

func (o *OKXAdapter) StartUserStream() error {
	ts := fmt.Sprintf("%.0f", float64(time.Now().Unix()))
	signature := o.sign(ts, "GET", "/users/self/verify", "")

	var wsClient *exchange.WSClient
	wsClient = exchange.NewWSClient(exchange.WSConfig{
		URL: OKXWsPrivURL,
		OnMessage: func(msg []byte) {
			o.handlePrivateMessage(msg)
		},
		OnConnected: func() {
			loginMsg := map[string]any{
				"op": "login",
				"args": []map[string]string{{
					"apiKey":     o.apiKey,
					"passphrase": o.passphrase,
					"timestamp":  ts,
					"sign":       signature,
				}},
			}
			if wsClient != nil {
				wsClient.SendJSON(loginMsg)
			}
		},
	})

	o.streamHub.Add("private", wsClient)
	return wsClient.Connect()
}

func (o *OKXAdapter) handlePrivateMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	if event, ok := raw["event"].(string); ok {
		switch event {
		case "login":
			log.Printf("[OKX] Private stream login: %v", raw["msg"])
			client := o.streamHub.Get("private")
			if client != nil {
				subMsg := map[string]any{
					"op": "subscribe",
					"args": []map[string]string{
						{"channel": "orders", "instType": "SPOT"},
						{"channel": "account"},
					},
				}
				client.SendJSON(subMsg)
			}
		case "error":
			log.Printf("[OKX] Private stream error: %v", raw)
		}
	}
}
