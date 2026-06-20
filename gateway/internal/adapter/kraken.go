package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	KrakenRestURL  = "https://api.kraken.com"
	KrakenWsURL    = "wss://ws.kraken.com/v2"
)

// ── Pair Name Mapping ──────────────────────────────────────────

// Kraken uses prefix notation: X for crypto base, Z for fiat quote.
func toKrakenPair(symbol string) string {
	s := strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
	// Kraken uses prefix notation: X for crypto base, Z for fiat quote
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

	// Try to find base asset by checking known mappings
	for std, kra := range basePrefix {
		if strings.HasPrefix(s, std) {
			rest := strings.TrimPrefix(s, std)
			if q, ok := quoteMap[rest]; ok {
				return kra + q
			}
			// Fallback: no prefix for unknown quote
			return kra + rest
		}
	}
	// Fallback: prepend X to first part, Z to second
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		parts = []string{s[:len(s)/2], s[len(s)/2:]}
	}
	return "X" + parts[0] + "Z" + parts[1]
}

func fromKrakenPair(kp string) string {
	// Reverse mapping: XXBTZUSD → BTC/USD
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

	// Try to split by known quote suffixes
	for kraQ, stdQ := range reverseQuote {
		if strings.HasSuffix(kp, kraQ) {
			base := strings.TrimSuffix(kp, kraQ)
			if std, ok := reverseBase[base]; ok {
				return std + "/" + stdQ
			}
			// Fallback: strip X prefix
			return strings.TrimPrefix(base, "X") + "/" + stdQ
		}
	}
	return kp
}

// ── KrakenAdapter ──────────────────────────────────────────────

type KrakenAdapter struct {
	apiKey    string
	secretKey string
	httpClient *http.Client
	nonce     int64
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

// ── Auth ───────────────────────────────────────────────────────

func (k *KrakenAdapter) getNonce() int64 {
	return atomic.AddInt64(&k.nonce, 1)
}

// signKraken generates Kraken's API-Sign header.
// API-Sign = HMAC-SHA512 of (nonce + postData) using base64-decoded secret
func (k *KrakenAdapter) signKraken(path string, postData url.Values) (string, int64) {
	nonce := k.getNonce()
	postData.Set("nonce", fmt.Sprintf("%d", nonce))

	// SHA256 of nonce + postData
	sha := sha256.New()
	sha.Write([]byte(fmt.Sprintf("%d%s", nonce, postData.Encode())))
	hash := sha.Sum(nil)

	// Decode base64 secret
	secretBytes, _ := base64.StdEncoding.DecodeString(k.secretKey)

	// HMAC-SHA512 of path + sha256 hash
	mac := hmac.New(sha512.New, secretBytes)
	mac.Write([]byte(path))
	mac.Write(hash)
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return sign, nonce
}

// privateRequest makes an authenticated request to Kraken.
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

// publicRequest makes a public (unauthenticated) request.
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

// ── Market Data ────────────────────────────────────────────────

func (k *KrakenAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	krakenPair := toKrakenPair(symbol)
	krakenInterval := toKrakenInterval(interval)

	// Kraken since param — convert interval to minutes
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
		// Try case-insensitive
		for k, v := range res {
			if strings.EqualFold(k, krakenPair) {
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
		// Kraken format: [time, open, high, low, close, vwap, volume, count]
		klines = append(klines, []any{
			int64(arr[0].(float64)), // timestamp
			parseFloatSafe(arr[1]),   // open
			parseFloatSafe(arr[2]),   // high
			parseFloatSafe(arr[3]),   // low
			parseFloatSafe(arr[4]),   // close
			parseFloatSafe(arr[6]),   // volume
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

	c, _ := pairData["c"].([]any) // last trade closed [price, lot]
	a, _ := pairData["a"].([]any) // ask [price, whole lot, lot]
	b, _ := pairData["b"].([]any) // bid
	h, _ := pairData["h"].([]any) // high
	l, _ := pairData["l"].([]any) // low
	v, _ := pairData["v"].([]any) // volume

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

// ── Account ────────────────────────────────────────────────────

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

// ── Orders ─────────────────────────────────────────────────────

func (k *KrakenAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	krakenPair := toKrakenPair(symbol)
	krakenSide := strings.ToLower(side)  // "buy" or "sell"
	krakenType := strings.ToLower(orderType) // "limit" or "market"

	postData := url.Values{}
	postData.Set("pair", krakenPair)
	postData.Set("type", krakenSide)
	postData.Set("ordertype", krakenType)
	postData.Set("volume", fmt.Sprintf("%.6f", quantity))

	if krakenType == "limit" {
		postData.Set("price", fmt.Sprintf("%.2f", price))
	}

	result, err := k.privateRequest("/0/private/AddOrder", postData)
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	txid, _ := res["txid"].([]any)

	order := map[string]any{
		"order_id": "",
		"txid":     txid,
		"symbol":   symbol,
		"side":     side,
		"type":     orderType,
		"price":    price,
		"quantity": quantity,
		"status":   "open",
	}
	if len(txid) > 0 {
		order["order_id"] = txid[0]
	}
	return order, nil
}

func (k *KrakenAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	postData := url.Values{}
	postData.Set("txid", orderID)

	result, err := k.privateRequest("/0/private/CancelOrder", postData)
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	return res, nil
}

func (k *KrakenAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	postData := url.Values{}
	if symbol != "" {
		postData.Set("pair", toKrakenPair(symbol))
	}

	result, err := k.privateRequest("/0/private/OpenOrders", postData)
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	open, _ := res["open"].(map[string]any)

	orders := make([]map[string]any, 0)
	for txid, orderData := range open {
		o, _ := orderData.(map[string]any)
		if o == nil {
			continue
		}
		descr, _ := o["descr"].(map[string]any)
		orders = append(orders, map[string]any{
			"order_id": txid,
			"symbol":   fromKrakenPair(descr["pair"].(string)),
			"side":     descr["type"],
			"type":     descr["ordertype"],
			"price":    descr["price"],
			"quantity": o["vol"],
			"status":   o["status"],
		})
	}
	return orders, nil
}

func (k *KrakenAdapter) GetPositions() ([]map[string]any, error) {
	postData := url.Values{}
	postData.Set("docalcs", "true")

	result, err := k.privateRequest("/0/private/OpenPositions", postData)
	if err != nil {
		return nil, err
	}

	res, _ := result["result"].(map[string]any)
	positions := make([]map[string]any, 0)
	for posID, posData := range res {
		p, _ := posData.(map[string]any)
		if p == nil {
			continue
		}
		symbol := fromKrakenPair(getString(p, "pair", ""))
		positions = append(positions, map[string]any{
			"symbol":       symbol,
			"positionAmt":  parseFloatStr(p, "vol"),
			"entryPrice":   parseFloatStr(p, "cost"),
			"markPrice":    parseFloatStr(p, "value"),
			"positionSide": "LONG",
			"leverage":     parseFloatStr(p, "margin"),
			"unrealizedPnl": parseFloatStr(p, "net"),
			"orderId":      posID,
		})
	}
	return positions, nil
}

func (k *KrakenAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	// Convert symbols to Kraken pair format
	var pairs []string
	for _, sym := range symbols {
		pairs = append(pairs, toKrakenPair(sym))
	}

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: KrakenWsURL,
		OnMessage: func(msg []byte) {
			k.handleMarketMessage(msg)
		},
		OnConnected: func() {
			k.mu.Lock()
			k.wsConnected = true
			k.mu.Unlock()
			log.Printf("[Kraken] Market stream connected, subscribing to %d pairs", len(pairs))
			// Send subscription for ticker, book, trade, ohlc
			for _, pair := range pairs {
				for _, channel := range []string{"ticker", "book", "trade", "ohlc"} {
					msg := map[string]any{
						"event": "subscribe",
						"pair":  []string{pair},
						"subscription": map[string]any{
							"name": channel,
						},
					}
					k.streamHub.SendJSON("market", msg)
				}
			}
		},
		OnDisconnected: func(err error) {
			k.mu.Lock()
			k.wsConnected = false
			k.mu.Unlock()
			if err != nil {
				log.Printf("[Kraken] Market stream disconnected: %v", err)
			}
		},
	})

	k.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

func (k *KrakenAdapter) StartUserStream() error {
	return nil // Kraken private streams require auth, skip for now
}

func (k *KrakenAdapter) handleMarketMessage(msg []byte) {
	// Kraken WS messages are:
	// [channelID, data] for data feeds
	// [channelID, pair, [data], event] for subscription confirmations
	// We try to parse as an array first.
	var arr []json.RawMessage
	if err := json.Unmarshal(msg, &arr); err != nil {
		return
	}
	if len(arr) < 2 {
		return
	}

	// Check if first element is a string (channel name) or array (channelID)
	var channelID string
	if err := json.Unmarshal(arr[0], &channelID); err != nil {
		// Not a simple channel name, could be an array
		channelID = string(arr[0])
	}

	// Check if it's a subscription status event
	if strings.Contains(string(arr[1]), `"event":"subscriptionStatus"`) {
		return
	}

	dataRaw := arr[1]
	var data any
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return
	}

	dataArray, ok := data.([]any)
	if !ok || len(dataArray) == 0 {
		return
	}

	if len(dataArray) < 5 {
		return
	}

	// Kraken ticker format: [close, volume, high, low, open, bids, asks, ...]
	if k.onTicker != nil {
		k.onTicker(model.Tick{
			Last:     dataArray[0].(float64),
			Volume:   dataArray[1].(float64),
			Bid:      dataArray[5].(float64),
			Ask:      dataArray[6].(float64),
			Timestamp: time.Now().UnixMilli(),
		})
	}
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

// ── Helpers ────────────────────────────────────────────────────

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
	case "1": return 1
	case "5": return 5
	case "15": return 15
	case "30": return 30
	case "60": return 60
	case "240": return 240
	case "1440": return 1440
	case "10080": return 10080
	default: return 60
	}
}

func parseFloatSafe(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func parseFloatArr(arr []any, idx int) float64 {
	if arr == nil || idx >= len(arr) {
		return 0
	}
	return parseFloatSafe(arr[idx])
}
