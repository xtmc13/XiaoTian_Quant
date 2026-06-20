package adapter

import (
	"encoding/json"
	"fmt"
	"io"
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
// NOTE: This is a stub adapter. Real trading is not yet implemented.
type AlpacaAdapter struct {
	apiKey     string
	secretKey  string
	paper      bool
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

func (a *AlpacaAdapter) Start() error { return nil }

func (a *AlpacaAdapter) baseURL() string {
	if a.paper {
		return AlpacaPaperURL
	}
	return AlpacaLiveURL
}

// -- HTTP Helpers --

func (a *AlpacaAdapter) request(method, urlStr string, body map[string]any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}

	req, _ := http.NewRequest(method, urlStr, bodyReader)
	req.Header.Set("APCA-API-KEY-ID", a.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", a.secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alpaca %s %s: %w", method, urlStr, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("alpaca parse: %w (body: %.200s)", err, string(respBody))
	}

	if msg, ok := result["message"].(string); ok && resp.StatusCode >= 400 {
		return nil, fmt.Errorf("alpaca error: %s", msg)
	}
	if code, ok := result["code"].(float64); ok && code >= 400000 {
		msg, _ := result["message"].(string)
		return nil, fmt.Errorf("alpaca error [%v]: %s", code, msg)
	}

	return result, nil
}

func (a *AlpacaAdapter) get(path string) (map[string]any, error) {
	return a.request("GET", a.baseURL()+path, nil)
}

func (a *AlpacaAdapter) dataGet(path string) (map[string]any, error) {
	urlStr := AlpacaDataURL + path
	req, _ := http.NewRequest("GET", urlStr, nil)
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

// -- Read-only Account (kept for basic use) --

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

// -- Read-only Market Data (kept for basic use) --

func (a *AlpacaAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
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
			parseTime(bar["t"]),
			bar["o"],
			bar["h"],
			bar["l"],
			bar["c"],
			bar["v"],
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
		"last":   quote["ap"],
	}, nil
}

// -- Stub: Trading methods not yet implemented --

func (a *AlpacaAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return nil, stubError("alpaca", "PlaceOrder")
}

func (a *AlpacaAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return nil, stubError("alpaca", "CancelOrder")
}

func (a *AlpacaAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return nil, stubError("alpaca", "GetOpenOrders")
}

func (a *AlpacaAdapter) GetPositions() ([]map[string]any, error) {
	return nil, stubError("alpaca", "GetPositions")
}

func (a *AlpacaAdapter) StartMarketStream(symbols []string) error {
	return stubError("alpaca", "StartMarketStream")
}

func (a *AlpacaAdapter) StartUserStream() error {
	return stubError("alpaca", "StartUserStream")
}

func (a *AlpacaAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	return nil, stubError("alpaca", "PlaceFuturesOrder")
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

// -- Helpers --

func parseTime(v any) int64 {
	switch val := v.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return 0
		}
		return t.UnixMilli()
	case float64:
		return int64(val) * 1000 // seconds -> ms
	}
	return 0
}
