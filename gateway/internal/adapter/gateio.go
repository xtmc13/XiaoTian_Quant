package adapter

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
// NOTE: This is a stub adapter. Real trading is not yet implemented.
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

// -- Gate.io Signing (HMAC-SHA512 of body hashed to hex) --

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

// -- Read-only Market Data (kept for basic use) --

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
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gateio ticker parse: %w", err)
	}

	if len(raw) > 0 {
		if m, ok := raw[0].(map[string]any); ok {
			return m, nil
		}
	}
	return nil, fmt.Errorf("gateio ticker: empty response for %s", symbol)
}

// -- Read-only Account (kept for basic use) --

func (g *GateIOAdapter) GetBalance() ([]map[string]any, error) {
	result, err := g.request("GET", "/spot/accounts", nil, nil)
	if err != nil {
		return nil, err
	}

	var accounts []map[string]any
	if arr, ok := result["detail"]; ok {
		return nil, fmt.Errorf("balance error: %v", arr)
	}

	if arr, ok := result["accounts"]; ok {
		if list, ok2 := arr.([]any); ok2 {
			for _, a := range list {
				if m, ok3 := a.(map[string]any); ok3 {
					accounts = append(accounts, m)
				}
			}
		}
	} else {
		raw, _ := json.Marshal(result)
		var list []map[string]any
		json.Unmarshal(raw, &list)
		for _, m := range list {
			accounts = append(accounts, m)
		}
	}
	return accounts, nil
}

// -- Stub: Trading methods not yet implemented --

func (g *GateIOAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return nil, stubError("gateio", "PlaceOrder")
}

func (g *GateIOAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return nil, stubError("gateio", "CancelOrder")
}

func (g *GateIOAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return nil, stubError("gateio", "GetOpenOrders")
}

func (g *GateIOAdapter) GetPositions() ([]map[string]any, error) {
	result, err := g.request("GET", "/futures/positions", nil, nil)
	if err != nil {
		return nil, err
	}
	var positions []map[string]any
	if arr, ok := result["positions"].([]any); ok {
		for _, p := range arr {
			if m, ok := p.(map[string]any); ok {
				positions = append(positions, m)
			}
		}
	} else {
		raw, _ := json.Marshal(result)
		json.Unmarshal(raw, &positions)
	}
	return positions, nil
}

func (g *GateIOAdapter) StartMarketStream(symbols []string) error {
	return stubError("gateio", "StartMarketStream")
}

func (g *GateIOAdapter) StartUserStream() error {
	return stubError("gateio", "StartUserStream")
}

func (g *GateIOAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	return nil, stubError("gateio", "PlaceFuturesOrder")
}
