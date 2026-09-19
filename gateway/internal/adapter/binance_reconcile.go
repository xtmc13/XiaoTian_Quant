package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A8 对账体系所需的 Binance 查询接口：订单状态/订单成交明细/资金费流水。
// 全部真实 REST 调用（fapi 优先，spot 兜底），供 reconcile 包通过窄接口使用。

// futuresRawRequest 发起已签名 fapi 请求并返回原始响应体（数组响应也能解析）。
func (b *BinanceAdapter) futuresRawRequest(method, path string, params url.Values) ([]byte, error) {
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	params.Set("signature", b.sign(params))

	u, _ := url.Parse(BinanceFuturesRestURL + path)
	u.RawQuery = params.Encode()

	var body io.Reader
	reqURL := u.String()
	if method != "GET" && method != "DELETE" {
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance fapi %s %s: status %d: %s", method, path, resp.StatusCode, truncateStr(string(raw), 200))
	}
	return raw, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ── Order Status Query（A8.2 成交恢复）──

// OrderStatusInfo 单个订单的最新状态。
type OrderStatusInfo struct {
	Status    string  // NEW|PARTIALLY_FILLED|FILLED|CANCELED|REJECTED|EXPIRED
	FilledQty float64 // 累计成交量
	AvgPrice  float64 // 累计成交均价（futures 直接给，spot 用成交额/成交量算）
}

// QueryOrderStatus 查询订单最新状态：U 本位合约优先，现货兜底。
func (b *BinanceAdapter) QueryOrderStatus(symbol, orderID string) (OrderStatusInfo, error) {
	if info, err := b.futuresOrderStatus(symbol, orderID); err == nil {
		return info, nil
	}
	return b.spotOrderStatus(symbol, orderID)
}

func (b *BinanceAdapter) futuresOrderStatus(symbol, orderID string) (OrderStatusInfo, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)
	raw, err := b.futuresRawRequest("GET", "/fapi/v1/order", params)
	if err != nil {
		return OrderStatusInfo{}, err
	}
	var r struct {
		Status      string `json:"status"`
		ExecutedQty string `json:"executedQty"`
		AvgPrice    string `json:"avgPrice"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return OrderStatusInfo{}, fmt.Errorf("parse futures order: %w", err)
	}
	if r.Status == "" {
		return OrderStatusInfo{}, fmt.Errorf("empty futures order status")
	}
	return OrderStatusInfo{
		Status:    r.Status,
		FilledQty: parseQty(r.ExecutedQty),
		AvgPrice:  parseQty(r.AvgPrice),
	}, nil
}

func (b *BinanceAdapter) spotOrderStatus(symbol, orderID string) (OrderStatusInfo, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", "5000")
	params.Set("signature", b.sign(params))

	u, _ := url.Parse(b.baseURL() + "/api/v3/order")
	u.RawQuery = params.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return OrderStatusInfo{}, err
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return OrderStatusInfo{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return OrderStatusInfo{}, fmt.Errorf("binance spot order status: status %d: %s", resp.StatusCode, truncateStr(string(raw), 200))
	}
	var r struct {
		Status              string `json:"status"`
		ExecutedQty         string `json:"executedQty"`
		CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return OrderStatusInfo{}, fmt.Errorf("parse spot order: %w", err)
	}
	info := OrderStatusInfo{Status: r.Status, FilledQty: parseQty(r.ExecutedQty)}
	if info.FilledQty > 0 {
		info.AvgPrice = parseQty(r.CummulativeQuoteQty) / info.FilledQty
	}
	return info, nil
}

func parseQty(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// ── Order Trades Query（A8.2 成交恢复，按订单查成交明细）──

// GetOrderTrades 拉取指定订单的成交明细：合约 userTrades(orderId) 优先，现货 myTrades 兜底。
// Binance 各字段数字/字符串混排，统一走 json.Number 宽松解析。
func (b *BinanceAdapter) GetOrderTrades(symbol, orderID string) ([]AccountTrade, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)
	params.Set("limit", "1000")
	raw, err := b.futuresRawRequest("GET", "/fapi/v1/userTrades", params)
	if err == nil {
		if trades := parseAccountTrades(raw, orderID); len(trades) > 0 {
			return trades, nil
		}
	}
	// 现货兜底
	sp, err := url.Parse(b.baseURL() + "/api/v3/myTrades")
	if err != nil {
		return nil, err
	}
	q := sp.Query()
	q.Set("symbol", symbol)
	q.Set("orderId", orderID)
	q.Set("limit", "1000")
	q.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	q.Set("recvWindow", "5000")
	q.Set("signature", b.sign(q))
	sp.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", sp.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance myTrades: status %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return parseAccountTrades(body, orderID), nil
}

// parseAccountTrades 宽松解析 userTrades/myTrades 响应（字段可能是数字或字符串）。
func parseAccountTrades(raw []byte, fallbackOrderID string) []AccountTrade {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var arr []map[string]any
	if err := dec.Decode(&arr); err != nil {
		return nil
	}
	trades := make([]AccountTrade, 0, len(arr))
	for _, m := range arr {
		t := AccountTrade{
			ID:              numberStrOf(m["id"]),
			OrderID:         firstStrOf(numberStrOf(m["orderId"]), fallbackOrderID),
			Symbol:          strOf(m["symbol"]),
			Side:            strOf(m["side"]),
			Price:           floatOf(m["price"]),
			Quantity:        floatOf(m["qty"]),
			QuoteQuantity:   floatOf(m["quoteQty"]),
			Commission:      floatOf(m["commission"]),
			CommissionAsset: strOf(m["commissionAsset"]),
			Time:            int64Of(m["time"]),
			RealizedPnl:     floatOf(m["realizedPnl"]),
		}
		if t.ID == "" {
			t.ID = fmt.Sprintf("%d", t.Time)
		}
		if t.Side == "" {
			if isBuyer, ok := m["isBuyer"].(bool); ok && isBuyer {
				t.Side = "BUY"
			} else {
				t.Side = "SELL"
			}
		}
		trades = append(trades, t)
	}
	return trades
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

func numberStrOf(v any) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case string:
		return n
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return ""
}

func firstStrOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func floatOf(v any) float64 {
	switch n := v.(type) {
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case float64:
		return n
	}
	return 0
}

func int64Of(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case float64:
		return int64(n)
	}
	return 0
}

// ── Funding Income（A8.3 资金费对账）──

// FundingIncome 一条资金费（或其他类型）收支流水。
type FundingIncome struct {
	Symbol     string  `json:"symbol"`
	IncomeType string  `json:"incomeType"`
	Asset      string  `json:"asset"`
	Income     float64 `json:"income"`
	Time       int64   `json:"time"`
	TradeID    string  `json:"tradeId"`
	Info       string  `json:"info"`
}

// GetFundingIncomes 查询 U 本位合约账户收支流水（默认资金费）。
// symbol 为空表示全部合约；startMs 为起始时间（毫秒）。
// Binance income 字段为字符串数字，走 json.Number 宽松解析。
func (b *BinanceAdapter) GetFundingIncomes(symbol string, startMs int64, limit int) ([]FundingIncome, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	params.Set("incomeType", "FUNDING_FEE")
	if startMs > 0 {
		params.Set("startTime", strconv.FormatInt(startMs, 10))
	}
	params.Set("limit", strconv.Itoa(limit))
	raw, err := b.futuresRawRequest("GET", "/fapi/v1/income", params)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var arr []map[string]any
	if err := dec.Decode(&arr); err != nil {
		return nil, fmt.Errorf("parse funding income: %w", err)
	}
	incomes := make([]FundingIncome, 0, len(arr))
	for _, m := range arr {
		incomes = append(incomes, FundingIncome{
			Symbol:     strOf(m["symbol"]),
			IncomeType: strOf(m["incomeType"]),
			Asset:      strOf(m["asset"]),
			Income:     floatOf(m["income"]),
			Time:       int64Of(m["time"]),
			TradeID:    strOf(m["tradeId"]),
			Info:       strOf(m["info"]),
		})
	}
	return incomes, nil
}
