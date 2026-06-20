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
	MEXCRestURL = "https://api.mexc.com/api/v3"
	MEXCWsURL   = "wss://wbs.mexc.com/ws"
)

// MEXCAdapter provides MEXC exchange integration.
// NOTE: This is a stub adapter. Real trading is not yet implemented.
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

// -- MEXC Signing (HMAC-SHA256, same pattern as Binance) --

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

// -- Read-only Market Data (kept for basic use) --

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

// -- Read-only Account (kept for basic use) --

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

// -- Stub: Trading methods not yet implemented --

func (mx *MEXCAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return nil, stubError("mexc", "PlaceOrder")
}

func (mx *MEXCAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return nil, stubError("mexc", "CancelOrder")
}

func (mx *MEXCAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return nil, stubError("mexc", "GetOpenOrders")
}

func (mx *MEXCAdapter) GetPositions() ([]map[string]any, error) {
	return nil, stubError("mexc", "GetPositions")
}

func (mx *MEXCAdapter) StartMarketStream(symbols []string) error {
	return stubError("mexc", "StartMarketStream")
}

func (mx *MEXCAdapter) StartUserStream() error {
	return stubError("mexc", "StartUserStream")
}

func (mx *MEXCAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	return nil, stubError("mexc", "PlaceFuturesOrder")
}
