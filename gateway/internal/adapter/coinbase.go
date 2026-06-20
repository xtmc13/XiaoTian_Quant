package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
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
	CoinbaseRestURL = "https://api.coinbase.com/api/v3/brokerage"
	CoinbaseWsURL   = "wss://advanced-trade-ws.coinbase.com"
)

// CoinbaseAdapter provides Coinbase Advanced Trade API integration.
// NOTE: This is a stub adapter. Real trading is not yet implemented.
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

// -- Coinbase Signing (CB-ACCESS-SIGN = HMAC-SHA256(timestamp + method + path + body)) --

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

// -- Read-only Market Data (kept for basic use) --

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

// -- Read-only Account (kept for basic use) --

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

// -- Stub: Trading methods not yet implemented --

func (cb *CoinbaseAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return nil, stubError("coinbase", "PlaceOrder")
}

func (cb *CoinbaseAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return nil, stubError("coinbase", "CancelOrder")
}

func (cb *CoinbaseAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return nil, stubError("coinbase", "GetOpenOrders")
}

func (cb *CoinbaseAdapter) GetPositions() ([]map[string]any, error) {
	return nil, stubError("coinbase", "GetPositions")
}

func (cb *CoinbaseAdapter) StartMarketStream(symbols []string) error {
	return stubError("coinbase", "StartMarketStream")
}

func (cb *CoinbaseAdapter) StartUserStream() error {
	return stubError("coinbase", "StartUserStream")
}

func (cb *CoinbaseAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	return nil, stubError("coinbase", "PlaceFuturesOrder")
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
