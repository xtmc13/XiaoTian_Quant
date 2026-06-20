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
	MEXCRestURL = "https://api.mexc.com/api/v3"
	MEXCWsURL   = "wss://wbs.mexc.com/ws"
)

// MEXCAdapter provides MEXC exchange integration (Binance-compatible API).
type MEXCAdapter struct {
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

	listenKey     string
	listenKeyLock sync.Mutex

	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewMEXCAdapter(apiKey, secret string) *MEXCAdapter {
	return &MEXCAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (mx *MEXCAdapter) Name() string    { return "mexc" }
func (mx *MEXCAdapter) Start() error     { return nil }
func (mx *MEXCAdapter) Stop() error      { mx.streamHub.CloseAll(); return nil }
func (mx *MEXCAdapter) IsConnected() bool {
	mx.mu.RLock()
	defer mx.mu.RUnlock()
	return mx.wsConnected
}

func (mx *MEXCAdapter) OnTicker(fn func(tick model.Tick))          { mx.onTicker = fn }
func (mx *MEXCAdapter) OnOrderBook(fn func(ob model.OrderBookData)) { mx.onOrderBook = fn }
func (mx *MEXCAdapter) OnTrade(fn func(trade model.TradeData))      { mx.onTrade = fn }
func (mx *MEXCAdapter) OnKline(fn func(bar model.Bar))              { mx.onKline = fn }

// ── MEXC Signing (HMAC-SHA256, same pattern as Binance) ──

func (mx *MEXCAdapter) sign(params url.Values) string {
	mac := hmac.New(sha256.New, []byte(mx.secretKey))
	mac.Write([]byte(params.Encode()))
	return hex.EncodeToString(mac.Sum(nil))
}

func (mx *MEXCAdapter) request(method, path string, params url.Values, signed bool) (map[string]any, error) {
	if signed && mx.apiKey != "" {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
		params.Set("recvWindow", "5000")
		params.Set("signature", mx.sign(params))
	}

	var reqURL string
	var body io.Reader

	if method == "GET" || method == "DELETE" {
		u, _ := url.Parse(MEXCRestURL + path)
		u.RawQuery = params.Encode()
		reqURL = u.String()
	} else {
		reqURL = MEXCRestURL + path
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}

	if method != "GET" && method != "DELETE" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-MEXC-APIKEY", mx.apiKey)

	resp, err := mx.httpClient.Do(req)
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

func (mx *MEXCAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", fmt.Sprintf("%d", limit))

	u, _ := url.Parse(MEXCRestURL + "/klines")
	u.RawQuery = params.Encode()

	resp, err := mx.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw []any
	json.Unmarshal(body, &raw)

	var result [][]any
	for _, r := range raw {
		if arr, ok := r.([]any); ok && len(arr) >= 6 {
			result = append(result, []any{arr[0], arr[1], arr[2], arr[3], arr[4], arr[5]})
		}
	}
	return result, nil
}

func (mx *MEXCAdapter) GetTicker(symbol string) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	return mx.request("GET", "/ticker/24hr", params, false)
}

// ── REST Trading ──

func (mx *MEXCAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", strings.ToUpper(side))
	params.Set("type", strings.ToUpper(orderType))
	params.Set("quantity", fmt.Sprintf("%.6f", quantity))
	if strings.ToUpper(orderType) == "LIMIT" {
		params.Set("price", fmt.Sprintf("%.2f", price))
	}
	return mx.request("POST", "/order", params, true)
}

func (mx *MEXCAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)
	return mx.request("DELETE", "/order", params, true)
}

func (mx *MEXCAdapter) GetBalance() ([]map[string]any, error) {
	result, err := mx.request("GET", "/account", url.Values{}, true)
	if err != nil {
		return nil, err
	}
	balances, _ := result["balances"].([]any)
	var out []map[string]any
	for _, bal := range balances {
		if bm, ok := bal.(map[string]any); ok {
			out = append(out, bm)
		}
	}
	return out, nil
}

func (mx *MEXCAdapter) GetPositions() ([]map[string]any, error) {
	// Call MEXC futures position API
	result, err := mx.request("GET", "/position", url.Values{}, true)
	if err != nil {
		return nil, err
	}
	positions := make([]map[string]any, 0)
	if arr, ok := result["data"].([]any); ok {
		for _, p := range arr {
			if pm, ok := p.(map[string]any); ok {
				positions = append(positions, pm)
			}
		}
	}
	return positions, nil
}

// MEXC futures base URL
const MEXCFuturesURL = "https://contract.mexc.com/api/v1/private"

func (mx *MEXCAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	// MEXC futures contract API uses a different endpoint structure than spot.
	// Attempt the futures endpoint; fall back to spot if unavailable.
	params := url.Values{}
	params.Set("symbol", strings.Replace(symbol, "USDT", "_USDT", 1))
	params.Set("side", fmt.Sprintf("%d", mapSideToMEXCInt(side)))
	params.Set("openType", fmt.Sprintf("%d", mapOrderTypeToMEXCInt(orderType)))
	params.Set("vol", fmt.Sprintf("%.4f", quantity))
	if orderType == "LIMIT" && price > 0 {
		params.Set("price", fmt.Sprintf("%.2f", price))
	}
	if leverage > 0 {
		params.Set("leverage", fmt.Sprintf("%.0f", leverage))
	}
	if positionSide != "" {
		params.Set("positionType", fmt.Sprintf("%d", mapPosSideToMEXCInt(positionSide)))
	}

	// Build signed request using MEXC auth
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	signature := mx.sign(params)
	params.Set("signature", signature)

	req, err := http.NewRequest("POST", MEXCFuturesURL+"/order/submit", strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MEXC-APIKEY", mx.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MEXC futures order failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return mx.PlaceOrder(symbol, side, orderType, price, quantity)
	}
	if code, ok := result["code"].(float64); ok && code != 200 {
		return mx.PlaceOrder(symbol, side, orderType, price, quantity)
	}
	return result, nil
}

// MEXC internal mapping helpers
func mapSideToMEXCInt(side string) int {
	switch strings.ToUpper(side) {
	case "BUY":
		return 1
	case "SELL":
		return 2
	default:
		return 1
	}
}

func mapOrderTypeToMEXCInt(orderType string) int {
	switch strings.ToUpper(orderType) {
	case "LIMIT":
		return 1
	case "MARKET":
		return 2
	default:
		return 2
	}
}

func mapPosSideToMEXCInt(posSide string) int {
	switch strings.ToUpper(posSide) {
	case "LONG":
		return 1
	case "SHORT":
		return 2
	default:
		return 1
	}
}

func (mx *MEXCAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	result, err := mx.request("GET", "/openOrders", params, true)
	if err != nil {
		return nil, err
	}
	var orders []map[string]any
	raw, _ := json.Marshal(result)
	json.Unmarshal(raw, &orders)
	return orders, nil
}

// ── WebSocket Market Streams ──

func (mx *MEXCAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: MEXCWsURL,
		OnMessage: func(msg []byte) {
			mx.handleStreamMessage(msg)
		},
		OnConnected: func() {
			mx.mu.Lock()
			mx.wsConnected = true
			mx.mu.Unlock()
			mx.subscribe(symbols)
		},
		OnDisconnected: func(err error) {
			mx.mu.Lock()
			mx.wsConnected = false
			mx.mu.Unlock()
			log.Printf("[MEXC] Stream disconnected: %v", err)
		},
	})

	mx.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

func (mx *MEXCAdapter) subscribe(symbols []string) {
	client := mx.streamHub.Get("market")
	if client == nil {
		return
	}

	var args []string
	for _, sym := range symbols {
		lower := strings.ToLower(sym)
		args = append(args,
			"spot@public.ticker.v3.api@"+lower,
			"spot@public.limit.depth.v3.api@"+lower+"@20",
			"spot@public.deals.v3.api@"+lower,
			"spot@public.kline.v3.api@"+lower+"@Min1",
		)
	}

	msg := map[string]any{
		"method": "SUBSCRIPTION",
		"params": args,
		"id":     time.Now().UnixMilli(),
	}
	client.SendJSON(msg)
}

func (mx *MEXCAdapter) StartUserStream() error {
	result, err := mx.request("POST", "/userDataStream", url.Values{}, false)
	if err != nil {
		return fmt.Errorf("create listenKey: %w", err)
	}

	key, ok := result["listenKey"].(string)
	if !ok {
		return fmt.Errorf("no listenKey in response")
	}

	mx.listenKeyLock.Lock()
	mx.listenKey = key
	mx.listenKeyLock.Unlock()

	// Keep-alive every 30 minutes
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			mx.listenKeyLock.Lock()
			k := mx.listenKey
			mx.listenKeyLock.Unlock()
			if k == "" {
				return
			}
			params := url.Values{}
			params.Set("listenKey", k)
			mx.request("PUT", "/userDataStream", params, false)
		}
	}()

	wsURL := MEXCWsURL + "?listenKey=" + key
	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: wsURL,
		OnMessage: func(msg []byte) {
			mx.handleUserStreamMessage(msg)
		},
		OnConnected: func() {
			log.Printf("[MEXC] User data stream connected")
		},
		OnDisconnected: func(err error) {
			log.Printf("[MEXC] User data stream disconnected: %v", err)
		},
	})

	mx.streamHub.Add("user", wsClient)
	return wsClient.Connect()
}

func (mx *MEXCAdapter) handleStreamMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	// MEXC uses "c" channel field and "d" data field
	channel, _ := raw["c"].(string)
	data, _ := raw["d"].(map[string]any)
	if data == nil {
		return
	}

	switch {
	case strings.Contains(channel, "ticker"):
		if mx.onTicker != nil {
			symbol, _ := data["s"].(string)
			mx.onTicker(model.Tick{
				Symbol:    symbol,
				Last:      parseFloat(data["p"]),
				Bid:       parseFloat(data["b"]),
				Ask:       parseFloat(data["a"]),
				Volume:    parseFloat(data["v"]),
				Timestamp: time.Now().UnixMilli(),
			})
		}
	case strings.Contains(channel, "depth"):
		if mx.onOrderBook != nil {
			symbol, _ := data["s"].(string)
			ob := model.OrderBookData{Symbol: symbol, Timestamp: time.Now().UnixMilli()}
			if bids, ok := data["bids"].([]any); ok {
				for _, b := range bids {
					if arr, ok2 := b.([]any); ok2 && len(arr) >= 2 {
						ob.Bids = append(ob.Bids, [2]float64{parseFloat(arr[0]), parseFloat(arr[1])})
					}
				}
			}
			if asks, ok := data["asks"].([]any); ok {
				for _, a := range asks {
					if arr, ok2 := a.([]any); ok2 && len(arr) >= 2 {
						ob.Asks = append(ob.Asks, [2]float64{parseFloat(arr[0]), parseFloat(arr[1])})
					}
				}
			}
			mx.onOrderBook(ob)
		}
	case strings.Contains(channel, "deals"):
		if mx.onTrade != nil {
			symbol, _ := data["s"].(string)
			if deals, ok := data["deals"].([]any); ok && len(deals) > 0 {
				for _, d := range deals {
					if deal, ok2 := d.(map[string]any); ok2 {
						side := "BUY"
						if s, _ := deal["S"].(float64); s == 2 {
							side = "SELL"
						}
						mx.onTrade(model.TradeData{
							Symbol:    symbol,
							ID:        fmt.Sprint(deal["t"]),
							Price:     parseFloat(deal["p"]),
							Quantity:  parseFloat(deal["q"]),
							Side:      side,
							Timestamp: time.Now().UnixMilli(),
						})
					}
				}
			}
		}
	case strings.Contains(channel, "kline"):
		if mx.onKline != nil {
			symbol, _ := data["s"].(string)
			if k, ok := data["k"].(map[string]any); ok {
				mx.onKline(model.Bar{
					Symbol:   symbol,
					Open:     parseFloat(k["o"]),
					High:     parseFloat(k["h"]),
					Low:      parseFloat(k["l"]),
					Close:    parseFloat(k["c"]),
					Volume:   parseFloat(k["v"]),
					Interval: "1m",
					Time:     time.Now().UnixMilli(),
				})
			}
		}
	}
}

func (mx *MEXCAdapter) handleUserStreamMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}
	// MEXC user data events: executionReport, outboundAccountInfo
	if e, ok := raw["e"].(string); ok {
		log.Printf("[MEXC] User stream event: %s", e)
	}
}
