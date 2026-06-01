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

	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	req.Header.Set("OK-ACCESS-KEY", o.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", o.sign(timestamp, method, path, bodyStr))
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", o.passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
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

func (o *OKXAdapter) StartMarketStream(symbols []string) error {
	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: OKXWsPubURL,
		OnMessage: func(msg []byte) {
			o.handlePublicMessage(msg)
		},
		OnConnected: func() {
			o.mu.Lock()
			o.wsConnected = true
			o.mu.Unlock()
			o.subscribePublic(symbols)
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

func (o *OKXAdapter) subscribePublic(symbols []string) {
	client := o.streamHub.Get("public")
	if client == nil {
		return
	}

	var args []map[string]string
	for _, sym := range symbols {
		instID := toOKXInstID(sym)
		args = append(args, map[string]string{"channel": "tickers", "instId": instID})
		args = append(args, map[string]string{"channel": "books5", "instId": instID})
		args = append(args, map[string]string{"channel": "trades", "instId": instID})
		args = append(args, map[string]string{"channel": "candle1m", "instId": instID})
	}

	msg := map[string]any{
		"op":   "subscribe",
		"args": args,
	}
	client.SendJSON(msg)
}

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

func (o *OKXAdapter) handlePublicMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
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
					Last:      parseFloat(fmt.Sprint(d["last"])),
					Bid:       parseFloat(fmt.Sprint(d["bidPx"])),
					Ask:       parseFloat(fmt.Sprint(d["askPx"])),
					Volume:    parseFloat(fmt.Sprint(d["vol24h"])),
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
								parseFloat(fmt.Sprint(arr[0])),
								parseFloat(fmt.Sprint(arr[1])),
							})
						}
					}
				}
				if asks, ok := d["asks"].([]any); ok {
					for _, a := range asks {
						if arr, ok2 := a.([]any); ok2 && len(arr) >= 2 {
							ob.Asks = append(ob.Asks, [2]float64{
								parseFloat(fmt.Sprint(arr[0])),
								parseFloat(fmt.Sprint(arr[1])),
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
				o.onTrade(model.TradeData{
					Symbol:    symbol,
					ID:        fmt.Sprint(d["tradeId"]),
					Price:     parseFloat(fmt.Sprint(d["px"])),
					Quantity:  parseFloat(fmt.Sprint(d["sz"])),
					Side:      side,
					Timestamp: time.Now().UnixMilli(),
				})
			}
		}
	case "candle1m":
		if len(data) > 0 && o.onKline != nil {
			if d, ok := data[0].(map[string]any); ok {
				o.onKline(model.Bar{
					Symbol:   symbol,
					Open:     parseFloat(fmt.Sprint(d["o"])),
					High:     parseFloat(fmt.Sprint(d["h"])),
					Low:      parseFloat(fmt.Sprint(d["l"])),
					Close:    parseFloat(fmt.Sprint(d["c"])),
					Volume:   parseFloat(fmt.Sprint(d["vol"])),
					Interval: "1m",
					Time:     int64(parseFloat(fmt.Sprint(d["ts"]))),
				})
			}
		}
	}
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
