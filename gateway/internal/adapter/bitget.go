package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	BitgetRestURL = "https://api.bitget.com/api/v2"
)

// BitgetAdapter provides Bitget exchange integration.
// NOTE: This is a stub adapter. Real trading is not yet implemented.
type BitgetAdapter struct {
	apiKey     string
	secretKey  string
	passphrase string
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

func NewBitgetAdapter(apiKey, secretKey, passphrase string) *BitgetAdapter {
	return &BitgetAdapter{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (b *BitgetAdapter) Name() string { return "bitget" }
func (b *BitgetAdapter) Start() error { return nil }

// -- Auth (kept for read-only market data) --

func (b *BitgetAdapter) signBitget(timestamp, method, path, body string) string {
	preSign := fmt.Sprintf("%s%s%s%s", timestamp, method, path, body)
	mac := hmac.New(sha256.New, []byte(b.secretKey))
	mac.Write([]byte(preSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (b *BitgetAdapter) signedRequest(method, path string, bodyMap map[string]any) (map[string]any, error) {
	var bodyStr string
	if bodyMap != nil {
		data, _ := json.Marshal(bodyMap)
		bodyStr = string(data)
	}

	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	sign := b.signBitget(timestamp, method, path, bodyStr)

	var bodyReader io.Reader
	if bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	req, _ := http.NewRequest(method, BitgetRestURL+path, bodyReader)
	req.Header.Set("ACCESS-KEY", b.apiKey)
	req.Header.Set("ACCESS-SIGN", sign)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", b.passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitget %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("bitget parse: %w", err)
	}

	if code, _ := result["code"].(string); code != "00000" {
		msg, _ := result["msg"].(string)
		return nil, fmt.Errorf("bitget error [%s]: %s", code, msg)
	}

	return result, nil
}

// -- Read-only Market Data (kept for basic use) --

func (b *BitgetAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	result, err := b.signedRequest("GET", fmt.Sprintf(
		"/spot/market/candles?symbol=%s&granularity=%s&limit=%d",
		symbol, toBitgetInterval(interval), clampLimit(limit, 1, 200),
	), nil)
	if err != nil {
		return nil, err
	}

	data, _ := result["data"].([]any)
	klines := make([][]any, 0, len(data))
	for i := len(data) - 1; i >= 0; i-- {
		arr, ok := data[i].([]any)
		if !ok || len(arr) < 6 {
			continue
		}
		klines = append(klines, []any{
			int64(parseFloatSafe(arr[0])),
			parseFloatSafe(arr[1]),
			parseFloatSafe(arr[2]),
			parseFloatSafe(arr[3]),
			parseFloatSafe(arr[4]),
			parseFloatSafe(arr[5]),
		})
	}
	return klines, nil
}

func (b *BitgetAdapter) GetTicker(symbol string) (map[string]any, error) {
	result, err := b.signedRequest("GET", "/spot/market/tickers?symbol="+symbol, nil)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].([]any)
	if len(data) == 0 {
		return nil, fmt.Errorf("bitget: no ticker for %s", symbol)
	}
	t, _ := data[0].(map[string]any)
	return map[string]any{
		"symbol": symbol,
		"last":   parseFloatSafe(t["close"]),
		"bid":    parseFloatSafe(t["buyOne"]),
		"ask":    parseFloatSafe(t["sellOne"]),
		"high":   parseFloatSafe(t["high24h"]),
		"low":    parseFloatSafe(t["low24h"]),
		"volume": parseFloatSafe(t["baseVol"]),
	}, nil
}

// -- Read-only Account (kept for basic use) --

func (b *BitgetAdapter) GetBalance() ([]map[string]any, error) {
	result, err := b.signedRequest("GET", "/spot/account/assets", nil)
	if err != nil {
		return nil, err
	}

	data, _ := result["data"].([]any)
	balances := make([]map[string]any, 0, len(data))
	for _, item := range data {
		a, _ := item.(map[string]any)
		balances = append(balances, map[string]any{
			"asset":  a["coinName"],
			"free":   parseFloatSafe(a["available"]),
			"locked": parseFloatSafe(a["frozen"]),
		})
	}
	return balances, nil
}

// -- Stub: Trading methods not yet implemented --

func (b *BitgetAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return nil, stubError("bitget", "PlaceOrder")
}

func (b *BitgetAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return nil, stubError("bitget", "CancelOrder")
}

func (b *BitgetAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return nil, stubError("bitget", "GetOpenOrders")
}

func (b *BitgetAdapter) GetPositions() ([]map[string]any, error) {
	result, err := b.signedRequest("GET", "/mix/position/allPosition?productType=USDT-FUTURES", nil)
	if err != nil {
		return nil, err
	}
	data, ok := result["data"].([]any)
	if !ok {
		return nil, fmt.Errorf("bitget positions: unexpected format")
	}
	out := make([]map[string]any, 0, len(data))
	for _, item := range data {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (b *BitgetAdapter) StartMarketStream(symbols []string) error {
	return stubError("bitget", "StartMarketStream")
}

func (b *BitgetAdapter) StartUserStream() error {
	return stubError("bitget", "StartUserStream")
}

func (b *BitgetAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	return nil, stubError("bitget", "PlaceFuturesOrder")
}

func (b *BitgetAdapter) Stop() error {
	b.mu.Lock()
	b.wsConnected = false
	hub := b.streamHub
	b.mu.Unlock()
	if hub != nil {
		hub.CloseAll()
	}
	return nil
}

func (b *BitgetAdapter) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.wsConnected
}

// -- Helpers --

func toBitgetInterval(interval string) string {
	m := map[string]string{
		"1m": "1min", "5m": "5min", "15m": "15min", "30m": "30min",
		"1h": "1H", "4h": "4H", "1d": "1D", "1w": "1W", "1M": "1M",
	}
	if v, ok := m[interval]; ok {
		return v
	}
	return "1H"
}
