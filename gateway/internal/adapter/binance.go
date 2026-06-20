package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
		"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
)

const (
	BinanceRestURL   = "https://api.binance.com/api/v3"
	BinanceWsURL     = "wss://stream.binance.com:9443/ws"
	BinanceTestURL   = "https://testnet.binance.vision/api/v3"
	BinanceTestWsURL = "wss://testnet.binance.vision/ws"

	// Futures
	BinanceFuturesRestURL  = "https://fapi.binance.com"
	BinanceFuturesFapiPath = "/fapi/v2"
)

// BinanceAdapter provides full Binance exchange integration including WebSocket streams.
type BinanceAdapter struct {
	apiKey     string
	secretKey  string
	testnet    bool
	httpClient *http.Client
	mu         sync.RWMutex

	// WebSocket
	streamHub   *exchange.StreamHub
	wsConnected bool

	// Market data callbacks
	onTicker    func(tick model.Tick)
	onOrderBook func(ob model.OrderBookData)
	onTrade     func(trade model.TradeData)
	onKline     func(bar model.Bar)

	// User data stream
	listenKey     string
	listenKeyLock sync.Mutex

	// Orders and positions (in-memory, backed by exchange)
	orders    map[string]map[string]any
	positions map[string]map[string]any
}

func NewBinanceAdapter(apiKey, secret string, testnet bool) *BinanceAdapter {
	b := &BinanceAdapter{
		apiKey:     apiKey,
		secretKey:  secret,
		testnet:    testnet,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		streamHub:  exchange.NewStreamHub(),
		orders:     make(map[string]map[string]any),
		positions:  make(map[string]map[string]any),
	}
	return b
}

// ── Exchange Interface ──

func (b *BinanceAdapter) Name() string { return "binance" }

func (b *BinanceAdapter) Start() error {
	return nil
}

func (b *BinanceAdapter) Stop() error {
	b.streamHub.CloseAll()
	return nil
}

func (b *BinanceAdapter) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.wsConnected
}

// ── Callback Setters ──

func (b *BinanceAdapter) OnTicker(fn func(tick model.Tick))          { b.onTicker = fn }
func (b *BinanceAdapter) OnOrderBook(fn func(ob model.OrderBookData)) { b.onOrderBook = fn }
func (b *BinanceAdapter) OnTrade(fn func(trade model.TradeData))      { b.onTrade = fn }
func (b *BinanceAdapter) OnKline(fn func(bar model.Bar))              { b.onKline = fn }

// ── URL Helpers ──

func (b *BinanceAdapter) baseURL() string {
		if env := os.Getenv("BINANCE_REST_URL"); env != "" {
			return env
		}
	if b.testnet {
		return BinanceTestURL
	}
	return BinanceRestURL
}

func (b *BinanceAdapter) wsURL() string {
		if env := os.Getenv("BINANCE_WS_URL"); env != "" {
			return env
		}
	if b.testnet {
		return BinanceTestWsURL
	}
	return BinanceWsURL
}

// ── Signing ──

func (b *BinanceAdapter) sign(params url.Values) string {
	mac := hmac.New(sha256.New, []byte(b.secretKey))
	mac.Write([]byte(params.Encode()))
	return hex.EncodeToString(mac.Sum(nil))
}

// ── REST ──

func (b *BinanceAdapter) request(method, path string, params url.Values, signed bool) (map[string]any, error) {
	if signed && b.apiKey != "" {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
		params.Set("recvWindow", "5000")
		params.Set("signature", b.sign(params))
	}

	var reqURL string
	var body io.Reader

	if method == "GET" || method == "DELETE" {
		u, _ := url.Parse(b.baseURL() + path)
		u.RawQuery = params.Encode()
		reqURL = u.String()
	} else {
		reqURL = b.baseURL() + path
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}

	if method != "GET" && method != "DELETE" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if b.apiKey != "" {
		req.Header.Set("X-MBX-APIKEY", b.apiKey)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

// ── Market Data REST ──

func (b *BinanceAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", fmt.Sprintf("%d", limit))

	u, _ := url.Parse(b.baseURL() + "/klines")
	u.RawQuery = params.Encode()

	resp, err := b.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw []any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	var result [][]any
	for _, r := range raw {
		if arr, ok := r.([]any); ok && len(arr) >= 6 {
			result = append(result, []any{arr[0], arr[1], arr[2], arr[3], arr[4], arr[5]})
		}
	}
	return result, nil
}

func (b *BinanceAdapter) GetTicker(symbol string) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	return b.request("GET", "/ticker/24hr", params, false)
}

// ── Trading REST ──

func (b *BinanceAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", orderType)
	params.Set("quantity", fmt.Sprintf("%.6f", quantity))
	if orderType == "LIMIT" {
		params.Set("price", fmt.Sprintf("%.2f", price))
		params.Set("timeInForce", "GTC")
	}
	return b.request("POST", "/order", params, true)
}

// PlaceFuturesOrder places a USDT-M futures order on Binance.
func (b *BinanceAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", orderType)
	params.Set("quantity", fmt.Sprintf("%.6f", quantity))
	if positionSide != "" {
		params.Set("positionSide", positionSide)
	}
	if orderType == "LIMIT" {
		params.Set("price", fmt.Sprintf("%.2f", price))
		params.Set("timeInForce", "GTC")
	}
	return b.futuresRequest("POST", "/fapi/v1/order", params)
}

func (b *BinanceAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)
	return b.request("DELETE", "/order", params, true)
}

func (b *BinanceAdapter) GetBalance() ([]map[string]any, error) {
	result, err := b.request("GET", "/account", url.Values{}, true)
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

func (b *BinanceAdapter) GetPositions() ([]map[string]any, error) {
	// Try futures positions first (most relevant for a quant platform)
	if b.apiKey != "" && b.secretKey != "" {
		positions, err := b.GetFuturesPositions()
		if err == nil && len(positions) > 0 {
			return positions, nil
		}
	}

	// Spot doesn't have "positions" in the traditional sense.
	// Return holdings from balance as position-like data.
	balances, err := b.GetBalance()
	if err != nil {
		return nil, err
	}

	var positions []map[string]any
	for _, bal := range balances {
		asset, _ := bal["asset"].(string)
		free := parseFloatStr(bal, "free")
		locked := parseFloatStr(bal, "locked")
		total := free + locked
		if total <= 0 || asset == "USDT" || asset == "BUSD" || asset == "USDC" {
			continue
		}
		// Get current price for this asset
		symbol := asset + "USDT"
		price := 0.0
		if ticker, err := b.GetTicker(symbol); err == nil {
			price = parseFloatStr(ticker, "lastPrice")
		}

		positions = append(positions, map[string]any{
			"symbol":       symbol,
			"positionAmt":  total,
			"entryPrice":   float64(0), // spot: no entry price tracking
			"markPrice":    price,
			"positionSide": "LONG",
			"leverage":     float64(1),
			"marketType":   "spot",
		})
	}
	return positions, nil
}

// ── Futures REST ──

func (b *BinanceAdapter) futuresRequest(method, path string, params url.Values) (map[string]any, error) {
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	params.Set("signature", b.sign(params))

	var reqURL string
	var body io.Reader

	if method == "GET" || method == "DELETE" {
		u, _ := url.Parse(BinanceFuturesRestURL + path)
		u.RawQuery = params.Encode()
		reqURL = u.String()
	} else {
		reqURL = BinanceFuturesRestURL + path
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}

	if method != "GET" && method != "DELETE" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

// GetFuturesAccount returns full futures account info (balance + positions).
func (b *BinanceAdapter) GetFuturesAccount() (map[string]any, error) {
	params := url.Values{}
	return b.futuresRequest("GET", "/fapi/v2/account", params)
}

// GetFuturesBalance extracts wallet balances from the futures account.
func (b *BinanceAdapter) GetFuturesBalance() ([]map[string]any, error) {
	acct, err := b.GetFuturesAccount()
	if err != nil {
		return nil, err
	}
	// Get assets array
	rawAssets, ok := acct["assets"]
	if !ok {
		return nil, fmt.Errorf("no assets in futures account response")
	}
	arr, ok := rawAssets.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected assets type: %T", rawAssets)
	}

	result := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		walletBalance := parseFloatStr(m, "walletBalance")
		unrealizedPnL := parseFloatStr(m, "unrealizedProfit")
		if walletBalance == 0 && unrealizedPnL == 0 {
			continue
		}
		result = append(result, map[string]any{
			"asset":            m["asset"],
			"walletBalance":    walletBalance,
			"unrealizedProfit": unrealizedPnL,
			"free":             walletBalance,
			"locked":           0.0,
		})
	}
	return result, nil
}

// GetFuturesPositions extracts open positions from the futures account.
func (b *BinanceAdapter) GetFuturesPositions() ([]map[string]any, error) {
	acct, err := b.GetFuturesAccount()
	if err != nil {
		return nil, err
	}
	rawPos, ok := acct["positions"]
	if !ok {
		return nil, nil
	}
	arr, ok := rawPos.([]any)
	if !ok {
		return nil, nil
	}

	result := make([]map[string]any, 0)
	for _, v := range arr {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		amt := parseFloatStr(m, "positionAmt")
		if amt == 0 {
			continue
		}
		result = append(result, map[string]any{
			"symbol":           m["symbol"],
			"positionAmt":      amt,
			"entryPrice":       parseFloatStr(m, "entryPrice"),
			"markPrice":        parseFloatStr(m, "markPrice"),
			"unrealizedProfit": parseFloatStr(m, "unrealizedProfit"),
			"positionSide":     m["positionSide"],
			"leverage":         parseFloatStr(m, "leverage"),
		})
	}
	return result, nil
}

func parseFloatStr(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
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

func (b *BinanceAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	result, err := b.request("GET", "/openOrders", params, true)
	if err != nil {
		return nil, err
	}
	var orders []map[string]any
	// Binance openOrders returns an array directly
	raw, _ := json.Marshal(result)
	json.Unmarshal(raw, &orders)
	if len(orders) == 0 {
		// Try nested "orders" key
		if arr, ok := result["orders"].([]any); ok {
			raw2, _ := json.Marshal(arr)
			json.Unmarshal(raw2, &orders)
		}
	}
	return orders, nil
}

// AccountTrade represents a single executed trade from the exchange.
type AccountTrade struct {
	ID               string  `json:"id"`
	OrderID          string  `json:"order_id"`
	Symbol          string  `json:"symbol"`
	Side            string  `json:"side"`
	Price           float64 `json:"price"`
	Quantity        float64 `json:"quantity"`
	QuoteQuantity   float64 `json:"quote_qty"`
	Commission      float64 `json:"commission"`
	CommissionAsset string  `json:"commission_asset"`
	Time            int64   `json:"time"`
	IsBuyer         bool    `json:"is_buyer"`
	IsMaker         bool    `json:"is_maker"`
	RealizedPnl     float64 `json:"realized_pnl"`
}

// GetAccountTradeHistory fetches executed trade history from Binance.
func (b *BinanceAdapter) GetAccountTradeHistory(symbol string, limit int) ([]AccountTrade, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}

	// Try futures first (more relevant for a quant platform)
	trades, err := b.getFuturesTradeHistory(symbol, limit)
	if err != nil {
		// Fall back to spot
		return b.getSpotTradeHistory(symbol, limit)
	}
	return trades, nil
}

func (b *BinanceAdapter) getSpotTradeHistory(symbol string, limit int) ([]AccountTrade, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	params.Set("limit", strconv.Itoa(limit))

	result, err := b.request("GET", "/myTrades", params, true)
	if err != nil {
		return nil, err
	}

	var trades []AccountTrade
	raw, _ := json.Marshal(result)
	if err := json.Unmarshal(raw, &trades); err != nil {
		// Try as array
		if arr, ok := result["trades"].([]any); ok {
			raw2, _ := json.Marshal(arr)
			json.Unmarshal(raw2, &trades)
		}
	}

	// Normalize side field from isBuyer
	for i := range trades {
		if trades[i].Side == "" {
			if trades[i].IsBuyer {
				trades[i].Side = "BUY"
			} else {
				trades[i].Side = "SELL"
			}
		}
		trades[i].ID = strconv.FormatInt(trades[i].Time, 10)
		trades[i].OrderID = strconv.FormatInt(trades[i].Time, 10)
	}

	return trades, nil
}

func (b *BinanceAdapter) getFuturesTradeHistory(symbol string, limit int) ([]AccountTrade, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	params.Set("limit", strconv.Itoa(limit))

	result, err := b.futuresRequest("GET", "/fapi/v1/userTrades", params)
	if err != nil {
		return nil, err
	}

	var trades []AccountTrade
	raw, _ := json.Marshal(result)
	if err := json.Unmarshal(raw, &trades); err != nil {
		if arr, ok := result["trades"].([]any); ok {
			raw2, _ := json.Marshal(arr)
			json.Unmarshal(raw2, &trades)
		}
	}

	for i := range trades {
		if trades[i].Side == "" {
			if trades[i].IsBuyer {
				trades[i].Side = "BUY"
			} else {
				trades[i].Side = "SELL"
			}
		}
	}

	return trades, nil
}

// GetFundingRate fetches the current funding rate for a futures symbol.
func (b *BinanceAdapter) GetFundingRate(symbol string) (float64, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	result, err := b.futuresRequest("GET", "/fapi/v1/premiumIndex", params)
	if err != nil {
		return 0, err
	}
	if rate, ok := result["lastFundingRate"].(string); ok {
		f, _ := strconv.ParseFloat(rate, 64)
		return f, nil
	}
	if rate, ok := result["lastFundingRate"].(float64); ok {
		return rate, nil
	}
	return 0, fmt.Errorf("funding rate not found in response")
}

// GetMarkPrice fetches the current mark price for a futures symbol.
func (b *BinanceAdapter) GetMarkPrice(symbol string) (float64, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	result, err := b.futuresRequest("GET", "/fapi/v1/premiumIndex", params)
	if err != nil {
		return 0, err
	}
	if price, ok := result["markPrice"].(string); ok {
		f, _ := strconv.ParseFloat(price, 64)
		return f, nil
	}
	if price, ok := result["markPrice"].(float64); ok {
		return price, nil
	}
	return 0, fmt.Errorf("mark price not found in response")
}

// GetAllFundingRates fetches funding rates for all futures symbols.
func (b *BinanceAdapter) GetAllFundingRates() (map[string]float64, error) {
	u, _ := url.Parse(BinanceFuturesRestURL + "/fapi/v1/premiumIndex")
	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	params.Set("signature", b.sign(params))
	u.RawQuery = params.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Try array response first (all symbols)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		rates := make(map[string]float64, len(arr))
		for _, m := range arr {
			symbol, _ := m["symbol"].(string)
			if symbol == "" {
				continue
			}
			if rate, ok := m["lastFundingRate"].(string); ok {
				f, _ := strconv.ParseFloat(rate, 64)
				rates[symbol] = f
			} else if rate, ok := m["lastFundingRate"].(float64); ok {
				rates[symbol] = rate
			}
		}
		return rates, nil
	}

	// Try single object response
	var single map[string]any
	if err := json.Unmarshal(body, &single); err == nil {
		rates := make(map[string]float64, 1)
		symbol, _ := single["symbol"].(string)
		if symbol != "" {
			if rate, ok := single["lastFundingRate"].(string); ok {
				f, _ := strconv.ParseFloat(rate, 64)
				rates[symbol] = f
			} else if rate, ok := single["lastFundingRate"].(float64); ok {
				rates[symbol] = rate
			}
		}
		return rates, nil
	}

	return nil, fmt.Errorf("failed to parse funding rates response")
}

// ── WebSocket Market Streams ──

// StartMarketStream connects to Binance WebSocket for real-time market data.
func (b *BinanceAdapter) StartMarketStream(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	// Build combined stream URL
	var streams []string
	for _, sym := range symbols {
		lower := strings.ToLower(sym)
		streams = append(streams,
			fmt.Sprintf("%s@ticker", lower),
			fmt.Sprintf("%s@depth20@100ms", lower),
			fmt.Sprintf("%s@trade", lower),
			fmt.Sprintf("%s@kline_1m", lower),
		)
	}

	streamURL := b.wsURL() + "/stream?streams=" + strings.Join(streams, "/")
	log.Printf("[Binance] Connecting market stream: %d symbols", len(symbols))

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL:     streamURL,
		OnMessage: func(msg []byte) {
			b.handleStreamMessage(msg)
		},
		OnConnected: func() {
			b.mu.Lock()
			b.wsConnected = true
			b.mu.Unlock()
			log.Printf("[Binance] Market stream connected")
		},
		OnDisconnected: func(err error) {
			b.mu.Lock()
			b.wsConnected = false
			b.mu.Unlock()
			log.Printf("[Binance] Market stream disconnected: %v", err)
		},
	})

	b.streamHub.Add("market", wsClient)
	return wsClient.Connect()
}

// StartUserStream creates a user data stream listenKey and starts listening.
func (b *BinanceAdapter) StartUserStream() error {
	// Create listenKey
	result, err := b.request("POST", "/userDataStream", url.Values{}, false)
	if err != nil {
		return fmt.Errorf("create listenKey: %w", err)
	}

	key, ok := result["listenKey"].(string)
	if !ok {
		return fmt.Errorf("no listenKey in response")
	}

	b.listenKeyLock.Lock()
	b.listenKey = key
	b.listenKeyLock.Unlock()

	// Start keep-alive (every 30 minutes)
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			b.listenKeyLock.Lock()
			key := b.listenKey
			b.listenKeyLock.Unlock()
			if key == "" {
				return
			}
			params := url.Values{}
			params.Set("listenKey", key)
			b.request("PUT", "/userDataStream", params, false)
		}
	}()

	// Connect user data stream
	wsURL := b.wsURL() + "/" + key
	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: wsURL,
		OnMessage: func(msg []byte) {
			b.handleUserStreamMessage(msg)
		},
		OnConnected: func() {
			log.Printf("[Binance] User data stream connected")
		},
		OnDisconnected: func(err error) {
			log.Printf("[Binance] User data stream disconnected: %v", err)
		},
	})

	b.streamHub.Add("user", wsClient)
	return wsClient.Connect()
}

// ── Stream Message Handling ──

type binanceStreamMsg struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

func (b *BinanceAdapter) handleStreamMessage(msg []byte) {
	// Try combined stream format first
	var combined binanceStreamMsg
	if err := json.Unmarshal(msg, &combined); err == nil && combined.Stream != "" {
		b.dispatchStreamData(combined.Stream, combined.Data)
		return
	}

	// Try raw format (single stream)
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err == nil {
		if e, ok := raw["e"].(string); ok {
			b.dispatchRawData(e, msg)
		}
	}
}

func (b *BinanceAdapter) dispatchStreamData(stream string, data json.RawMessage) {
	lower := strings.ToLower(stream)
	switch {
	case strings.HasSuffix(lower, "@ticker"):
		var ticker struct {
			S string `json:"s"` // symbol
			C string `json:"c"` // last price
			B string `json:"b"` // bid
			A string `json:"a"` // ask
			Q string `json:"q"` // quote volume
		}
		if err := json.Unmarshal(data, &ticker); err == nil && b.onTicker != nil {
			b.onTicker(model.Tick{
				Symbol:    ticker.S,
				Bid:       parseFloat(ticker.B),
				Ask:       parseFloat(ticker.A),
				Last:      parseFloat(ticker.C),
				Volume:    parseFloat(ticker.Q),
				Timestamp: time.Now().UnixMilli(),
			})
		}
	case strings.HasSuffix(lower, "@depth20@100ms"):
		var depth struct {
			S    string     `json:"s"`
			Bids [][]string `json:"bids"`
			Asks [][]string `json:"asks"`
		}
		if err := json.Unmarshal(data, &depth); err == nil && b.onOrderBook != nil {
			ob := model.OrderBookData{
				Symbol:    depth.S,
				Timestamp: time.Now().UnixMilli(),
			}
			for _, b := range depth.Bids {
				if len(b) >= 2 {
					ob.Bids = append(ob.Bids, [2]float64{parseFloat(b[0]), parseFloat(b[1])})
				}
			}
			for _, a := range depth.Asks {
				if len(a) >= 2 {
					ob.Asks = append(ob.Asks, [2]float64{parseFloat(a[0]), parseFloat(a[1])})
				}
			}
			b.onOrderBook(ob)
		}
	case strings.HasSuffix(lower, "@trade"):
		var trade struct {
			S string `json:"s"`
			T int64  `json:"t"`
			P string `json:"p"`
			Q string `json:"q"`
			M bool   `json:"m"` // true = seller is maker
		}
		if err := json.Unmarshal(data, &trade); err == nil && b.onTrade != nil {
			side := "BUY"
			if trade.M {
				side = "SELL"
			}
			b.onTrade(model.TradeData{
				Symbol:    trade.S,
				ID:        fmt.Sprintf("%d", trade.T),
				Price:     parseFloat(trade.P),
				Quantity:  parseFloat(trade.Q),
				Side:      side,
				Timestamp: trade.T,
			})
		}
	case strings.HasSuffix(lower, "@kline_1m"):
		var kline struct {
			S string `json:"s"`
			K struct {
				T int64  `json:"t"`
				O string `json:"o"`
				H string `json:"h"`
				L string `json:"l"`
				C string `json:"c"`
				V string `json:"v"`
			} `json:"k"`
		}
		if err := json.Unmarshal(data, &kline); err == nil && b.onKline != nil {
			b.onKline(model.Bar{
				Symbol:   kline.S,
				Open:     parseFloat(kline.K.O),
				High:     parseFloat(kline.K.H),
				Low:      parseFloat(kline.K.L),
				Close:    parseFloat(kline.K.C),
				Volume:   parseFloat(kline.K.V),
				Interval: "1m",
				Time:     kline.K.T,
			})
		}
	}
}

func (b *BinanceAdapter) dispatchRawData(eventType string, data json.RawMessage) {
	switch eventType {
	case "executionReport":
		b.handleExecutionReport(data)
	case "outboundAccountPosition":
		b.handleAccountUpdate(data)
	}
}

func (b *BinanceAdapter) handleUserStreamMessage(msg []byte) {
	var raw map[string]any
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}
	if e, ok := raw["e"].(string); ok {
		b.dispatchRawData(e, msg)
	}
}

func (b *BinanceAdapter) handleExecutionReport(data json.RawMessage) {
	var report map[string]any
	json.Unmarshal(data, &report)
	// Store order update
	if id, ok := report["i"].(float64); ok {
		orderID := fmt.Sprintf("%.0f", id)
		b.mu.Lock()
		b.orders[orderID] = report
		b.mu.Unlock()
	}
}

func (b *BinanceAdapter) handleAccountUpdate(data json.RawMessage) {
	var update map[string]any
	json.Unmarshal(data, &update)
	if balances, ok := update["B"].([]any); ok {
		for _, b := range balances {
			// Update local balance cache
			_ = b
		}
	}
}

// ── OrderBook ──

type OrderBook struct {
	Symbol string
	Bids   [][2]float64
	Asks   [][2]float64
	mu     sync.RWMutex
}

func NewOrderBook(symbol string) *OrderBook {
	return &OrderBook{
		Symbol: symbol,
		Bids:   make([][2]float64, 0),
		Asks:   make([][2]float64, 0),
	}
}

func (ob *OrderBook) Update(bids, asks [][2]float64) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.Bids = bids
	ob.Asks = asks
}

func (ob *OrderBook) BestBid() float64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	if len(ob.Bids) == 0 {
		return 0
	}
	return ob.Bids[0][0]
}

func (ob *OrderBook) BestAsk() float64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	if len(ob.Asks) == 0 {
		return 0
	}
	return ob.Asks[0][0]
}

func (ob *OrderBook) Spread() float64 {
	bid := ob.BestBid()
	ask := ob.BestAsk()
	if bid == 0 || ask == 0 {
		return 0
	}
	return ask - bid
}

// ── Funding Wallet ────────────────────────────────────────────────────────

// FundingBalance holds a single funding wallet asset balance.
type FundingBalance struct {
	Asset         string  `json:"asset"`
	Free          float64 `json:"free,string"`
	Locked        float64 `json:"locked,string"`
	Freeze        float64 `json:"freeze,string"`
	Withdrawing   float64 `json:"withdrawing,string"`
	BtcValuation  string  `json:"btcValuation"` // Scientific notation string like "9.990322679103085E-5"
}

// GetFundingWallet queries the Binance funding wallet (Earn / Staking / Liquid Swap).
// POST /sapi/v1/asset/get-funding-asset
func (b *BinanceAdapter) GetFundingWallet() ([]FundingBalance, error) {
	ts := time.Now().UnixMilli()
	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", ts))

	sig := b.sign(params)
	params.Set("signature", sig)

	apiURL := "https://api.binance.com/sapi/v1/asset/get-funding-asset?" + params.Encode()

	req, err := http.NewRequest("POST", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("funding wallet: create request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("funding wallet: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("funding wallet: %d %s", resp.StatusCode, string(body))
	}

	var result []FundingBalance
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("funding wallet: parse: %w", err)
	}

	return result, nil
}

// ── Earn (理财) ────────────────────────────────────────────────────────────

// EarnPosition holds a single earn position (flexible or locked).
type EarnPosition struct {
	Asset  string  `json:"asset"`
	Amount float64 `json:"amount,string"`
	Type   string  `json:"type"` // "flexible" or "locked"
}

// sapiSignedGET performs a signed GET request to Binance SAPI (https://api.binance.com/sapi/...).
func (b *BinanceAdapter) sapiSignedGET(path string) ([]byte, error) {
	ts := time.Now().UnixMilli()
	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", ts))
	params.Set("recvWindow", "5000")

	sig := b.sign(params)
	params.Set("signature", sig)

	apiURL := "https://api.binance.com" + path + "?" + params.Encode()

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("sapi %s: create request: %w", path, err)
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sapi %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sapi %s: %d %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

// GetFlexibleEarn queries Binance Flexible Earn positions.
// GET /sapi/v1/simple-earn/flexible/position
func (b *BinanceAdapter) GetFlexibleEarn() ([]EarnPosition, error) {
	body, err := b.sapiSignedGET("/sapi/v1/simple-earn/flexible/position")
	if err != nil {
		return nil, err
	}

	// Binance may return {"code":...,"msg":"..."} on error or permission denial
	type flexRow struct {
		Asset       string `json:"asset"`
		TotalAmount string `json:"totalAmount"`
	}
	type flexResponse struct {
		Rows []flexRow `json:"rows"`
		Code int       `json:"code"`
		Msg  string    `json:"msg"`
	}

	// Try wrapped response first
	var wrapped flexResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Rows) > 0 {
		var result []EarnPosition
		for _, r := range wrapped.Rows {
			amt, _ := strconv.ParseFloat(r.TotalAmount, 64)
			if amt > 0 {
				result = append(result, EarnPosition{Asset: r.Asset, Amount: amt, Type: "flexible"})
			}
		}
		return result, nil
	}

	// Try flat array response
	var rows []flexRow
	if err := json.Unmarshal(body, &rows); err != nil {
		// Permission issue or empty — treat as no positions
		log.Printf("[Adapter] Flexible earn: %s", string(body))
		return nil, nil
	}

	var result []EarnPosition
	for _, r := range rows {
		amt, _ := strconv.ParseFloat(r.TotalAmount, 64)
		if amt > 0 {
			result = append(result, EarnPosition{Asset: r.Asset, Amount: amt, Type: "flexible"})
		}
	}
	return result, nil
}

// GetLockedEarn queries Binance Locked Earn positions.
// GET /sapi/v1/simple-earn/locked/position
func (b *BinanceAdapter) GetLockedEarn() ([]EarnPosition, error) {
	body, err := b.sapiSignedGET("/sapi/v1/simple-earn/locked/position")
	if err != nil {
		return nil, err
	}

	type lockedRow struct {
		Asset  string `json:"asset"`
		Amount string `json:"amount"`
	}
	type lockedResponse struct {
		Rows []lockedRow `json:"rows"`
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
	}

	// Try wrapped response first
	var wrapped lockedResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Rows) > 0 {
		var result []EarnPosition
		for _, r := range wrapped.Rows {
			amt, _ := strconv.ParseFloat(r.Amount, 64)
			if amt > 0 {
				result = append(result, EarnPosition{Asset: r.Asset, Amount: amt, Type: "locked"})
			}
		}
		return result, nil
	}

	// Try flat array response
	var rows []lockedRow
	if err := json.Unmarshal(body, &rows); err != nil {
		// Permission issue or empty — treat as no positions
		log.Printf("[Adapter] Locked earn: %s", string(body))
		return nil, nil
	}

	var result []EarnPosition
	for _, r := range rows {
		amt, _ := strconv.ParseFloat(r.Amount, 64)
		if amt > 0 {
			result = append(result, EarnPosition{Asset: r.Asset, Amount: amt, Type: "locked"})
		}
	}
	return result, nil
}
