package adapter

import (
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
	AlpacaPaperURL = "https://paper-api.alpaca.markets/v2"
	AlpacaLiveURL  = "https://api.alpaca.markets/v2"
	AlpacaDataURL  = "https://data.alpaca.markets/v2"
)

// AlpacaAdapter provides US stock/ETF trading via Alpaca Markets.
// Supports: stocks, ETFs, fractional shares, paper trading.
type AlpacaAdapter struct {
	apiKey    string
	secretKey string
	paper     bool
	httpClient *http.Client
	mu        sync.RWMutex

	streamHub     *exchange.StreamHub
	wsConnected   bool
	onTicker      func(tick model.Tick)
	onOrderBook   func(ob model.OrderBookData)
	onTrade       func(trade model.TradeData)
	onKline       func(bar model.Bar)

	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewAlpacaAdapter(apiKey, secretKey string, paper bool) *AlpacaAdapter {
	return &AlpacaAdapter{
		apiKey:     apiKey,
		secretKey:  secretKey,
		paper:      paper,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (a *AlpacaAdapter) Name() string {
	if a.paper {
		return "alpaca_paper"
	}
	return "alpaca"
}

func (a *AlpacaAdapter) Start() error  { return nil }

func (a *AlpacaAdapter) baseURL() string {
	if a.paper {
		return AlpacaPaperURL
	}
	return AlpacaLiveURL
}

// ── HTTP Helpers ───────────────────────────────────────────────

func (a *AlpacaAdapter) get(path string) (map[string]any, error) {
	return a.request("GET", a.baseURL()+path, nil)
}

func (a *AlpacaAdapter) post(path string, body map[string]any) (map[string]any, error) {
	return a.request("POST", a.baseURL()+path, body)
}

func (a *AlpacaAdapter) del(path string) (map[string]any, error) {
	return a.request("DELETE", a.baseURL()+path, nil)
}

func (a *AlpacaAdapter) dataGet(path string) (map[string]any, error) {
	url := AlpacaDataURL + path
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("APCA-API-KEY-ID", a.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", a.secretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)
	return result, nil
}

func (a *AlpacaAdapter) request(method, url string, body map[string]any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}

	req, _ := http.NewRequest(method, url, bodyReader)
	req.Header.Set("APCA-API-KEY-ID", a.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", a.secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alpaca %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("alpaca parse: %w (body: %.200s)", err, string(respBody))
	}

	// Check for API errors
	if msg, ok := result["message"].(string); ok && resp.StatusCode >= 400 {
		return nil, fmt.Errorf("alpaca error: %s", msg)
	}
	if code, ok := result["code"].(float64); ok && code >= 400000 {
		msg, _ := result["message"].(string)
		return nil, fmt.Errorf("alpaca error [%v]: %s", code, msg)
	}

	return result, nil
}

// ── Account ────────────────────────────────────────────────────

func (a *AlpacaAdapter) GetAccount() (map[string]any, error) {
	return a.get("/account")
}

func (a *AlpacaAdapter) GetBalance() ([]map[string]any, error) {
	account, err := a.GetAccount()
	if err != nil {
		return nil, err
	}

	cash := parseFloatSafe(account["cash"])
	buyingPower := parseFloatSafe(account["buying_power"])
	equity := parseFloatSafe(account["equity"])
	currency, _ := account["currency"].(string)

	return []map[string]any{
		{"asset": currency, "free": cash, "locked": buyingPower - cash, "equity": equity},
	}, nil
}

// ── Positions ──────────────────────────────────────────────────

func (a *AlpacaAdapter) GetPositions() ([]map[string]any, error) {
	result, err := a.get("/positions")
	if err != nil {
		return nil, err
	}

	positions := make([]map[string]any, 0)
	for _, item := range result {
		if pos, ok := item.(map[string]any); ok {
			positions = append(positions, map[string]any{
				"symbol":          pos["symbol"],
				"qty":             pos["qty"],
				"entry_price":     pos["avg_entry_price"],
				"current_price":   pos["current_price"],
				"market_value":    pos["market_value"],
				"unrealized_pl":   pos["unrealized_pl"],
				"unrealized_plpc": pos["unrealized_plpc"],
				"side":            pos["side"],
			})
		}
	}
	return positions, nil
}

// ── Orders ─────────────────────────────────────────────────────

func (a *AlpacaAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	body := map[string]any{
		"symbol":        strings.ToUpper(symbol),
		"side":          strings.ToLower(side),
		"type":          strings.ToLower(orderType),
		"qty":           fmt.Sprintf("%.6f", quantity),
		"time_in_force": "day",
	}

	if strings.ToLower(orderType) == "limit" {
		body["limit_price"] = fmt.Sprintf("%.2f", price)
	}

	result, err := a.post("/orders", body)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"order_id": result["id"],
		"symbol":   result["symbol"],
		"side":     result["side"],
		"type":     result["type"],
		"qty":      result["qty"],
		"price":    result["limit_price"],
		"status":   result["status"],
	}, nil
}

func (a *AlpacaAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return a.del("/orders/" + orderID)
}

func (a *AlpacaAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	path := "/orders?status=open"
	if symbol != "" {
		path += "&symbol=" + strings.ToUpper(symbol)
	}

	result, err := a.get(path)
	if err != nil {
		return nil, err
	}

	orders := make([]map[string]any, 0)
	for _, item := range result {
		if ord, ok := item.(map[string]any); ok {
			orders = append(orders, map[string]any{
				"order_id": ord["id"],
				"symbol":   ord["symbol"],
				"side":     ord["side"],
				"type":     ord["type"],
				"qty":      ord["qty"],
				"price":    ord["limit_price"],
				"status":   ord["status"],
			})
		}
	}
	return orders, nil
}

// ── Market Data ────────────────────────────────────────────────

func (a *AlpacaAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	// Map our interval to Alpaca timeframe
	tf := map[string]string{
		"1m": "1Min", "5m": "5Min", "15m": "15Min",
		"1h": "1Hour", "4h": "4Hour", "1d": "1Day",
	}[interval]
	if tf == "" {
		tf = "1Hour"
	}

	path := fmt.Sprintf("/stocks/%s/bars?timeframe=%s&limit=%d&adjustment=raw",
		strings.ToUpper(symbol), tf, clampLimit(limit, 1, 1000))

	result, err := a.dataGet(path)
	if err != nil {
		return nil, err
	}

	bars, ok := result["bars"].([]any)
	if !ok {
		return nil, fmt.Errorf("alpaca: unexpected bars response")
	}

	klines := make([][]any, 0, len(bars))
	for _, b := range bars {
		bar, _ := b.(map[string]any)
		klines = append(klines, []any{
			parseTime(bar["t"]), // timestamp
			bar["o"],             // open
			bar["h"],             // high
			bar["l"],             // low
			bar["c"],             // close
			bar["v"],             // volume
		})
	}
	return klines, nil
}

func (a *AlpacaAdapter) GetTicker(symbol string) (map[string]any, error) {
	result, err := a.dataGet(fmt.Sprintf("/stocks/%s/quotes/latest", strings.ToUpper(symbol)))
	if err != nil {
		return nil, err
	}

	quote, ok := result["quote"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("alpaca: ticker not found for %s", symbol)
	}

	return map[string]any{
		"symbol": symbol,
		"bid":    quote["bp"],
		"ask":    quote["ap"],
		"last":   quote["ap"], // use ask as last price proxy
	}, nil
}

func (a *AlpacaAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: "wss://stream.alpaca.markets/v1/iex/market",
		OnMessage: func(msg []byte) {
			a.handleMarketMessage(msg)
		},
		OnConnected: func() {
			a.mu.Lock()
			a.wsConnected = true
			a.mu.Unlock()
			log.Printf("[Alpaca] Market stream connected, subscribing to %d symbols", len(symbols))
			// Alpaca uses array of symbols for subscription
			for _, sym := range symbols {
				subMsg := []string{"L." + strings.ToUpper(sym)}
				a.streamHub.SendJSON("market", subMsg)
			}
		},
		OnDisconnected: func(err error) {
			a.mu.Lock()
			a.wsConnected = false
			a.mu.Unlock()
			if err != nil {
				log.Printf("[Alpaca] Market stream disconnected: %v", err)
			}
		},
	})

	a.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

func (a *AlpacaAdapter) StartUserStream() error {
	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: "wss://stream.alpaca.markets/v1/iex/books",
		OnMessage: func(msg []byte) {
			var raw map[string]any
			if err := json.Unmarshal(msg, &raw); err == nil {
				log.Printf("[Alpaca] User stream: %s", string(msg[:min(200, len(msg))]))
			}
		},
		OnConnected: func() {
			log.Printf("[Alpaca] User stream connected")
		},
		OnDisconnected: func(err error) {
			if err != nil {
				log.Printf("[Alpaca] User stream disconnected: %v", err)
			}
		},
	})

	a.streamHub.Add("user", wsClient)
	return wsClient.Connect()
}

func (a *AlpacaAdapter) handleMarketMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	typ, _ := raw["T"].(string)
	if typ == "" {
		return
	}

	switch typ {
	case "L":
		// Last trade
		if price, _ := raw["p"].(float64); price > 0 {
			if a.onTrade != nil {
				a.onTrade(model.TradeData{
					Symbol:    getString(raw, "s", ""),
					ID:        getString(raw, "i", ""),
					Price:     price,
					Quantity:  getFloat(raw, "s", 0),
					Side:      "BUY",
					Timestamp: parseTime(raw["t"]),
				})
			}
		}
	case "Q":
		// Quote update
		if bp, _ := raw["bp"].(float64); bp > 0 {
			if a.onTicker != nil {
				last := getFloat(raw, "lp", 0)
				if last == 0 {
					last = getFloat(raw, "ap", 0)
				}
				a.onTicker(model.Tick{
					Symbol:    getString(raw, "s", ""),
					Bid:       bp,
					Ask:       getFloat(raw, "ap", 0),
					Last:      last,
					Volume:    getFloat(raw, "vp", 0),
					Timestamp: parseTime(raw["t"]),
				})
			}
		}
	}
}

func (a *AlpacaAdapter) Stop() error {
	a.mu.Lock()
	a.wsConnected = false
	hub := a.streamHub
	a.mu.Unlock()
	if hub != nil {
		hub.CloseAll()
	}
	return nil
}

func (a *AlpacaAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.wsConnected
}

// ── Helpers ────────────────────────────────────────────────────

	}
	return def
}

func getFloat(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			var f float64
			fmt.Sscanf(val, "%f", &f)
			return f
		}
	}
	return def
}

func parseTime(v any) int64 {
	switch val := v.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return 0
		}
		return t.UnixMilli()
	case float64:
		return int64(val) * 1000 // seconds → ms
	}
	return 0
}
