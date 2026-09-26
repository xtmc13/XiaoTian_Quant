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
	"os"
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
	BinanceFuturesRestURL     = "https://fapi.binance.com"
	BinanceFuturesTestRestURL = "https://testnet.binancefuture.com"
	BinanceSapiRestURL        = "https://api.binance.com"
	BinanceFuturesFapiPath    = "/fapi/v2"
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

func (b *BinanceAdapter) OnTicker(fn func(tick model.Tick))           { b.onTicker = fn }
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

// TransferFromFuturesToFunding 盈利保护划转：U 本位合约账户 → 资金账户
// （sapi POST /sapi/v1/asset/transfer, type=UMFUTURE_MAIN）。签名与 fapi 同法
// （HMAC-SHA256 + X-MBX-APIKEY）；检查响应 status=="OK"。
func (b *BinanceAdapter) TransferFromFuturesToFunding(amount float64) error {
	if amount <= 0 {
		return fmt.Errorf("transfer amount must be positive")
	}
	if b.apiKey == "" || b.secretKey == "" {
		return fmt.Errorf("binance credentials not configured")
	}
	params := url.Values{}
	params.Set("type", "UMFUTURE_MAIN")
	params.Set("asset", "USDT")
	params.Set("amount", strconv.FormatFloat(amount, 'f', 6, 64))
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	params.Set("signature", b.sign(params))

	req, err := http.NewRequest("POST", BinanceSapiRestURL+"/sapi/v1/asset/transfer", strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("transfer response parse: %v (%s)", err, string(body))
	}
	if result.Status != "OK" {
		return fmt.Errorf("transfer rejected: %s", string(body))
	}
	return nil
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
	// 币安 4xx/业务错误体为 {"code":-N,"msg":"..."}——必须作为错误返回，
	// 否则调用方把错误响应当成功解析，产生 id=<nil> 的"假成交"（status=NEW）。
	if resp.StatusCode >= 400 || negativeCode(result) {
		code, _ := result["code"].(float64)
		msg, _ := result["msg"].(string)
		if msg == "" {
			msg = strings.TrimSpace(string(respBody))
		}
		return result, fmt.Errorf("binance %s %s: HTTP %d code=%d %s", method, path, resp.StatusCode, int(code), msg)
	}
	return result, nil
}

// ── Market Data REST ──

func (b *BinanceAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	return b.GetKlinesRange(symbol, interval, 0, 0, limit)
}

// GetKlinesRange fetches Binance klines in a time range.
// startMs and endMs are unix milliseconds; pass 0 to ignore the bound.
func (b *BinanceAdapter) GetKlinesRange(symbol, interval string, startMs, endMs int64, limit int) ([][]any, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if startMs > 0 {
		params.Set("startTime", fmt.Sprintf("%d", startMs))
	}
	if endMs > 0 {
		params.Set("endTime", fmt.Sprintf("%d", endMs))
	}

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
	qtyStr, priceStr, err := b.normalizeBinanceOrder(symbol, orderType, price, quantity, false)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", orderType)
	params.Set("quantity", qtyStr)
	if orderType == "LIMIT" {
		params.Set("price", priceStr)
		params.Set("timeInForce", "GTC")
	}
	return b.request("POST", "/order", params, true)
}

// PlaceFuturesOrder places a USDT-M futures order on Binance.
func (b *BinanceAdapter) PlaceFuturesOrder(symbol, side, orderType string, price, quantity, leverage float64, positionSide string) (map[string]any, error) {
	qtyStr, priceStr, err := b.normalizeBinanceOrder(symbol, orderType, price, quantity, true)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", orderType)
	params.Set("quantity", qtyStr)
	if positionSide != "" {
		params.Set("positionSide", positionSide)
	}
	if orderType == "LIMIT" {
		params.Set("price", priceStr)
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
		u, _ := url.Parse(b.fapiBaseURL() + path)
		u.RawQuery = params.Encode()
		reqURL = u.String()
	} else {
		reqURL = b.fapiBaseURL() + path
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
	// 币安 4xx/业务错误体为 {"code":-N,"msg":"..."}——必须作为错误返回，
	// 否则调用方把错误响应当成功解析，产生 id=<nil> 的"假成交"（status=NEW）。
	if resp.StatusCode >= 400 || negativeCode(result) {
		code, _ := result["code"].(float64)
		msg, _ := result["msg"].(string)
		if msg == "" {
			msg = strings.TrimSpace(string(respBody))
		}
		return result, fmt.Errorf("binance %s %s: HTTP %d code=%d %s", method, path, resp.StatusCode, int(code), msg)
	}
	return result, nil
}

// negativeCode 报告币安业务错误码（负数即错误）。
func negativeCode(result map[string]any) bool {
	if result == nil {
		return false
	}
	c, ok := result["code"].(float64)
	return ok && c < 0
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
	ID              string  `json:"id"`
	OrderID         string  `json:"order_id"`
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

	// Combined stream endpoint uses /stream, single streams use /ws/<stream>.
	base := strings.TrimSuffix(b.wsURL(), "/ws")
	streamURL := base + "/stream?streams=" + strings.Join(streams, "/")
	log.Printf("[Binance] Connecting market stream: %d symbols, url=%s", len(symbols), streamURL)

	wsClient := exchange.NewWSClient(exchange.WSConfig{
		URL: streamURL,
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
	Asset        string  `json:"asset"`
	Free         float64 `json:"free,string"`
	Locked       float64 `json:"locked,string"`
	Freeze       float64 `json:"freeze,string"`
	Withdrawing  float64 `json:"withdrawing,string"`
	BtcValuation string  `json:"btcValuation"` // Scientific notation string like "9.990322679103085E-5"
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

// ── Reported PnL Reconciliation（交易所回报 PnL 对账，A8.5）────────────────
//
// 交易所结算口径的已实现盈亏查询，供 reconcile 包通过窄接口独立核对本地记账。
// 依据 Binance U 本位合约文档：
//   - GET /fapi/v1/income 支持 incomeType=REALIZED_PNL，返回账户结算口径已实现盈亏
//     流水（逐笔平仓盈亏，不含手续费——手续费是独立的 COMMISSION 流水），
//     按 time 升序返回，limit 最大 1000；翻页用 startTime 递增直至空页/短页。
//   - GET /fapi/v2/balance 返回逐资产钱包余额口径（balance/crossUnPnl），
//     用作净值对账辅助（balance + crossUnPnl ≈ 保证金净值）。

// maxIncomePages 分页防御上限：正常远不会到达，防止异常服务端导致死循环。
const maxIncomePages = 50

// ReportedPnLIncome 交易所结算口径的单条已实现盈亏流水
// （GET /fapi/v1/income incomeType=REALIZED_PNL，字段与资金费流水同构）。
type ReportedPnLIncome struct {
	Symbol     string  `json:"symbol"`
	IncomeType string  `json:"incomeType"`
	Asset      string  `json:"asset"`
	Income     float64 `json:"income"`
	Time       int64   `json:"time"`
	TradeID    string  `json:"tradeId"`
	Info       string  `json:"info"`
}

// fapiBaseURL returns the USDT-M futures REST base URL. Mirrors baseURL()'s
// testnet switch (spot testnet → testnet.binance.vision, futures testnet →
// testnet.binancefuture.com) and the BINANCE_FAPI_URL escape hatch:
// tests/self-hosted gateways may override so the reported-PnL methods can be
// aimed at an httptest server. 修复前 testnet=true 时合约仍指向生产
// fapi.binance.com，testnet 凭证会被生产网关拒签。
func (b *BinanceAdapter) fapiBaseURL() string {
	if env := os.Getenv("BINANCE_FAPI_URL"); env != "" {
		return env
	}
	if b.testnet {
		return BinanceFuturesTestRestURL
	}
	return BinanceFuturesRestURL
}

// fapiSignedGet performs a signed GET against an fapi base and returns the raw
// body, failing on non-200. Same wire format as futuresRawRequest
// (binance_reconcile.go) but takes an explicit base URL + credentials so it can
// run against an httptest server; the HMAC-SHA256 signing mirrors BinanceAdapter.sign.
func fapiSignedGet(client *http.Client, baseURL, apiKey, secret, path string, params url.Values) ([]byte, error) {
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(params.Encode()))
	params.Set("signature", hex.EncodeToString(mac.Sum(nil)))

	u, _ := url.Parse(baseURL + path)
	u.RawQuery = params.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance fapi GET %s: status %d: %s", path, resp.StatusCode, truncateStr(string(raw), 200))
	}
	return raw, nil
}

// getFapiIncomeRows pages through GET /fapi/v1/income: fixed page size, advancing
// startTime just past the newest row of each page until a short or empty page ends
// the scan. Standalone (not method-bound) on purpose so httptest servers can
// exercise pagination and termination.
//
// 已知口径 caveat：翻页游标取末行 time+1，同一毫秒内的后续流水行会被跳过；
// Binance 该接口同一 (symbol,incomeType) 下 time 实际唯一，可接受。
func getFapiIncomeRows(client *http.Client, baseURL, apiKey, secret, incomeType, symbol string, startMs, endMs int64, pageLimit int) ([]map[string]any, error) {
	if pageLimit <= 0 || pageLimit > 1000 {
		pageLimit = 1000
	}
	var rows []map[string]any
	cursor := startMs
	for page := 0; page < maxIncomePages; page++ {
		params := url.Values{}
		if symbol != "" {
			params.Set("symbol", symbol)
		}
		params.Set("incomeType", incomeType)
		if cursor > 0 {
			params.Set("startTime", strconv.FormatInt(cursor, 10))
		}
		if endMs > 0 {
			params.Set("endTime", strconv.FormatInt(endMs, 10))
		}
		params.Set("limit", strconv.Itoa(pageLimit))

		raw, err := fapiSignedGet(client, baseURL, apiKey, secret, "/fapi/v1/income", params)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		var batch []map[string]any
		if err := dec.Decode(&batch); err != nil {
			return nil, fmt.Errorf("parse fapi income: %w", err)
		}
		if len(batch) == 0 {
			break // 空页：窗口内无更多流水
		}
		rows = append(rows, batch...)
		if len(batch) < pageLimit {
			break // 短页：区间内已无更多流水（再翻一页必为空，提前终止）
		}
		next := int64Of(batch[len(batch)-1]["time"]) + 1
		if next <= cursor {
			break // 防御：游标未推进
		}
		cursor = next
	}
	return rows, nil
}

// GetRealizedPnLIncomes fetches exchange-reported realized PnL (settlement view)
// for the USDT-M futures account over [startMs, endMs]. symbol empty = all
// contracts. Paginates /fapi/v1/income?incomeType=REALIZED_PNL at limit 1000,
// advancing startTime until an empty/short page. Fee-neutral by definition of the
// REALIZED_PNL income type (fees are separate COMMISSION entries).
func (b *BinanceAdapter) GetRealizedPnLIncomes(symbol string, startMs, endMs int64) ([]ReportedPnLIncome, error) {
	rows, err := getFapiIncomeRows(b.httpClient, b.fapiBaseURL(), b.apiKey, b.secretKey, "REALIZED_PNL", symbol, startMs, endMs, 1000)
	if err != nil {
		return nil, err
	}
	out := make([]ReportedPnLIncome, 0, len(rows))
	for _, m := range rows {
		out = append(out, ReportedPnLIncome{
			Symbol:     strOf(m["symbol"]),
			IncomeType: strOf(m["incomeType"]),
			Asset:      strOf(m["asset"]),
			Income:     floatOf(m["income"]),
			Time:       int64Of(m["time"]),
			TradeID:    strOf(m["tradeId"]),
			Info:       strOf(m["info"]),
		})
	}
	return out, nil
}

// FuturesWalletBalanceRow is one asset row of GET /fapi/v2/balance (wallet-balance
// accounting view of the USDT-M futures account).
type FuturesWalletBalanceRow struct {
	Asset              string  `json:"asset"`
	Balance            float64 `json:"balance"` // 钱包余额（含已结算盈亏，不含未实现）
	CrossWalletBalance float64 `json:"crossWalletBalance"`
	CrossUnPnl         float64 `json:"crossUnPnl"`
	AvailableBalance   float64 `json:"availableBalance"`
	MaxWithdrawAmount  float64 `json:"maxWithdrawAmount"`
	UpdateTime         int64   `json:"updateTime"`
}

// GetFuturesWalletBalance queries GET /fapi/v2/balance for a single asset (USDT
// when asset is empty). Wallet balance + crossUnPnl gives the equity-style net
// value used as a secondary balance reconciliation reference. Returns nil, nil
// when the asset is absent from the account.
func (b *BinanceAdapter) GetFuturesWalletBalance(asset string) (*FuturesWalletBalanceRow, error) {
	if asset == "" {
		asset = "USDT"
	}
	params := url.Values{}
	params.Set("asset", asset)
	raw, err := fapiSignedGet(b.httpClient, b.fapiBaseURL(), b.apiKey, b.secretKey, "/fapi/v2/balance", params)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var arr []map[string]any
	if err := dec.Decode(&arr); err != nil {
		return nil, fmt.Errorf("parse fapi v2 balance: %w", err)
	}
	for _, m := range arr {
		if strOf(m["asset"]) != asset {
			continue
		}
		return &FuturesWalletBalanceRow{
			Asset:              asset,
			Balance:            floatOf(m["balance"]),
			CrossWalletBalance: floatOf(m["crossWalletBalance"]),
			CrossUnPnl:         floatOf(m["crossUnPnl"]),
			AvailableBalance:   floatOf(m["availableBalance"]),
			MaxWithdrawAmount:  floatOf(m["maxWithdrawAmount"]),
			UpdateTime:         int64Of(m["updateTime"]),
		}, nil
	}
	return nil, nil
}
