package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

const (
	KrakenRestURL = "https://api.kraken.com"
	KrakenWsURL   = "wss://ws.kraken.com/v2"
)

// -- Pair Name Mapping --

// Kraken uses prefix notation: X for crypto base, Z for fiat quote.
func toKrakenPair(symbol string) string {
	s := strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
	basePrefix := map[string]string{
		"BTC": "XXBT", "ETH": "XETH", "LTC": "XLTC",
		"XRP": "XXRP", "ADA": "ADA", "DOT": "DOT",
		"SOL": "SOL", "DOGE": "XDG", "LINK": "LINK",
		"MATIC": "MATIC", "UNI": "UNI", "AVAX": "AVAX",
		"ATOM": "ATOM", "BNB": "BNB",
	}
	quoteMap := map[string]string{
		"USDT": "ZUSD", "USD": "ZUSD", "USDC": "USDC",
		"EUR": "ZEUR", "GBP": "ZGBP", "JPY": "ZJPY",
		"BUSD": "BUSD", "DAI": "DAI",
	}

	for std, kra := range basePrefix {
		if strings.HasPrefix(s, std) {
			rest := strings.TrimPrefix(s, std)
			if q, ok := quoteMap[rest]; ok {
				return kra + q
			}
			return kra + rest
		}
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		parts = []string{s[:len(s)/2], s[len(s)/2:]}
	}
	return "X" + parts[0] + "Z" + parts[1]
}

func fromKrakenPair(kp string) string {
	reverseBase := map[string]string{
		"XXBT": "BTC", "XETH": "ETH", "XLTC": "LTC",
		"XXRP": "XRP", "ADA": "ADA", "DOT": "DOT",
		"SOL": "SOL", "XDG": "DOGE", "LINK": "LINK",
		"MATIC": "MATIC", "UNI": "UNI", "AVAX": "AVAX",
		"ATOM": "ATOM", "BNB": "BNB",
	}
	reverseQuote := map[string]string{
		"USDT": "USDT", "ZUSD": "USD", "USDC": "USDC",
		"ZEUR": "EUR", "ZGBP": "GBP", "ZJPY": "JPY",
		"BUSD": "BUSD", "DAI": "DAI",
	}

	for kraQ, stdQ := range reverseQuote {
		if strings.HasSuffix(kp, kraQ) {
			base := strings.TrimSuffix(kp, kraQ)
			if std, ok := reverseBase[base]; ok {
				return std + "/" + stdQ
			}
			return strings.TrimPrefix(base, "X") + "/" + stdQ
		}
	}
	return kp
}

// -- KrakenAdapter --

// KrakenAdapter provides Kraken exchange integration.
// NOTE: This is a stub adapter. Real trading is not yet implemented.
type KrakenAdapter struct {
	apiKey     string
	secretKey  string
	httpClient *http.Client
	nonce      int64
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

func NewKrakenAdapter(apiKey, secret string) *KrakenAdapter {
	return &KrakenAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		nonce:      time.Now().UnixNano(),
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
}

func (k *KrakenAdapter) Name() string  { return "kraken" }
func (k *KrakenAdapter) Start() error  { return nil }

// -- Auth (kept for read-only market data) --

func (k *KrakenAdapter) getNonce() int64 {
	return atomic.AddInt64(&k.nonce, 1)
}

func (k *KrakenAdapter) signKraken(path string, postData url.Values) (string, int64) {
	nonce := k.getNonce()
	postData.Set("nonce", fmt.Sprintf("%d", nonce))

	sha := sha256.New()
	sha.Write([]byte(fmt.Sprintf("%d%s", nonce, postData.Encode())))
	hash := sha.Sum(nil)

	secretBytes, _ := base64.StdEncoding.DecodeString(k.secretKey)

	mac := hmac.New(sha512.New, secretBytes)
	mac.Write([]byte(path))
	mac.Write(hash)
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return sign, nonce
}

func (k *KrakenAdapter) privateRequest(path string, postData url.Values) (map[string]any, error) {
	sign, _ := k.signKraken(path, postData)

	req, _ := http.NewRequest("POST", KrakenRestURL+path, strings.NewReader(postData.Encode()))
	req.Header.Set("API-Key", k.apiKey)
	req.Header.Set("API-Sign", sign)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := k.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kraken request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("kraken parse: %w (body: %.200s)", err, string(body))
	}

	if errs, ok := result["error"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("kraken error: %v", errs)
	}

	return result, nil
}

func (k *KrakenAdapter) publicRequest(path string) (map[string]any, error) {
	resp, err := k.httpClient.Get(KrakenRestURL + path)
	if err != nil {
		return nil, fmt.Errorf("kraken public: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("kraken parse: %w", err)
	}

	if errs, ok := result["error"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("kraken error: %v", errs)
	}

	return result, nil
}

// -- Read-only Market Data (kept for basic use) --

func (k *KrakenAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	krakenPair := toKrakenPair(symbol)
	krakenInterval := toKrakenInterval(interval)
	minutes := krakenIntervalMinutes(krakenInterval)

	result, err := k.publicRequest(fmt.Sprintf(
		"/0/public/OHLC?pair=%s&interval=%d", krakenPair, minutes,
	))
	if err != nil {
		return nil, err
	}

	res, ok := result["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("kraken OHLC: unexpected response")
	}

	pairData, ok := res[krakenPair].([]any)
	if !ok {
		for k2, v := range res {
			if strings.EqualFold(k2, krakenPair) {
				pairData, _ = v.([]any)
				break
			}
		}
	}
	if pairData == nil {
		return nil, fmt.Errorf("kraken: no data for %s (%s)", symbol, krakenPair)
	}

	klines := make([][]any, 0, len(pairData))
	for i, item := range pairData {
		if limit > 0 && i >= limit {
			break
		}
		arr, ok := item.([]any)
		if !ok || len(arr) < 7 {
			continue
		}
		klines = append(klines, []any{
			int64(arr[0].(float64)),
			parseFloatSafe(arr[1]),
			parseFloatSafe(arr[2]),
			parseFloatSafe(arr[3]),
			parseFloatSafe(arr[4]),
			parseFloatSafe(arr[6]),
		})
	}

	return klines, nil
}

func (k *KrakenAdapter) GetTicker(symbol string) (map[string]any, error) {
	krakenPair := toKrakenPair(symbol)

	result, err := k.publicRequest(fmt.Sprintf("/0/public/Ticker?pair=%s", krakenPair))
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	pairData, _ := res[krakenPair].(map[string]any)
	if pairData == nil {
		return nil, fmt.Errorf("kraken: ticker not found for %s", symbol)
	}

	c, _ := pairData["c"].([]any)
	a, _ := pairData["a"].([]any)
	b, _ := pairData["b"].([]any)
	h, _ := pairData["h"].([]any)
	l, _ := pairData["l"].([]any)
	v, _ := pairData["v"].([]any)

	return map[string]any{
		"symbol": symbol,
		"last":   parseFloatArr(c, 0),
		"ask":    parseFloatArr(a, 0),
		"bid":    parseFloatArr(b, 0),
		"high":   parseFloatArr(h, 0),
		"low":    parseFloatArr(l, 0),
		"volume": parseFloatArr(v, 0),
	}, nil
}

// -- Read-only Account (kept for basic use) --

func (k *KrakenAdapter) GetBalance() ([]map[string]any, error) {
	result, err := k.privateRequest("/0/private/Balance", url.Values{})
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	if res == nil {
		return nil, fmt.Errorf("kraken: no balance data")
	}

	balances := make([]map[string]any, 0, len(res))
	for asset, amount := range res {
		vol := parseFloatSafe(amount)
		if vol <= 0 {
			continue
		}
		balances = append(balances, map[string]any{
			"asset":  asset,
			"free":   vol,
			"locked": 0.0,
		})
	}
	return balances, nil
}

// -- Stub: Trading methods not yet implemented --

func (k *KrakenAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return nil, stubError("kraken", "PlaceOrder")
}

func (k *KrakenAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return nil, stubError("kraken", "CancelOrder")
}

func (k *KrakenAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	return nil, stubError("kraken", "GetOpenOrders")
}

func (k *KrakenAdapter) GetPositions() ([]map[string]any, error) {
	return nil, stubError("kraken", "GetPositions")
}

func (k *KrakenAdapter) StartMarketStream(symbols []string) error {
	return stubError("kraken", "StartMarketStream")
}

func (k *KrakenAdapter) StartUserStream() error {
	return stubError("kraken", "StartUserStream")
}

func (k *KrakenAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	return nil, stubError("kraken", "PlaceFuturesOrder")
}

func (k *KrakenAdapter) Stop() error {
	k.mu.Lock()
	k.wsConnected = false
	hub := k.streamHub
	k.mu.Unlock()
	if hub != nil {
		hub.CloseAll()
	}
	return nil
}

func (k *KrakenAdapter) IsConnected() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.wsConnected
}

// -- Helpers --

func toKrakenInterval(interval string) string {
	m := map[string]string{
		"1m": "1", "5m": "5", "15m": "15", "30m": "30",
		"1h": "60", "4h": "240", "1d": "1440", "1w": "10080",
	}
	if v, ok := m[interval]; ok {
		return v
	}
	return "60"
}

func krakenIntervalMinutes(interval string) int {
	switch interval {
	case "1":
		return 1
	case "5":
		return 5
	case "15":
		return 15
	case "30":
		return 30
	case "60":
		return 60
	case "240":
		return 240
	case "1440":
		return 1440
	case "10080":
		return 10080
	default:
		return 60
	}
}

func parseFloatArr(arr []any, idx int) float64 {
	if arr == nil || idx >= len(arr) {
		return 0
	}
	return parseFloatSafe(arr[idx])
}
