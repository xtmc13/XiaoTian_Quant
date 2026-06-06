package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ── Tushare Adapter ────────────────────────────────────────────

// TushareAdapter provides A-share (Chinese stock market) data via Tushare Pro API.
// Supports: stock list, daily/weekly bars, fundamental data, financial reports.
type TushareAdapter struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

// NewTushareAdapter creates a Tushare adapter.
func NewTushareAdapter(token string) *TushareAdapter {
	return &TushareAdapter{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "http://api.tushare.pro",
	}
}

func (t *TushareAdapter) Name() string { return "tushare" }

// ── API Request ────────────────────────────────────────────────

// TushareRequest is the standard request format for Tushare API.
type TushareRequest struct {
	APIName string         `json:"api_name"`
	Token   string         `json:"token"`
	Params  map[string]any `json:"params"`
	Fields  string         `json:"fields,omitempty"`
}

// TushareResponse is the standard response format.
type TushareResponse struct {
	Code    int      `json:"code"`
	Msg     string   `json:"msg"`
	Data    *TushareData `json:"data"`
}

// TushareData holds the tabular response data.
type TushareData struct {
	Fields []string   `json:"fields"`
	Items  [][]any    `json:"items"`
}

func (t *TushareAdapter) request(apiName string, params map[string]any, fields string) (*TushareResponse, error) {
	reqBody := TushareRequest{
		APIName: apiName,
		Token:   t.token,
		Params:  params,
		Fields:  fields,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := t.httpClient.Post(t.baseURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tushare request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result TushareResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("tushare parse error: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("tushare API error %d: %s", result.Code, result.Msg)
	}
	return &result, nil
}

// ── Stock List ─────────────────────────────────────────────────

// StockBasic holds basic stock information.
type StockBasic struct {
	TSCode     string `json:"ts_code"`     // e.g. 000001.SZ
	Symbol     string `json:"symbol"`      // e.g. 000001
	Name       string `json:"name"`        // e.g. 平安银行
	Area       string `json:"area"`
	Industry   string `json:"industry"`
	Market     string `json:"market"`      // 主板/创业板/科创板
	ListDate   string `json:"list_date"`
	IsHS300    bool   `json:"is_hs300"`    // CSI 300 constituent
	IsSZ50     bool   `json:"is_sz50"`     // SZSE 50 constituent
	IsST       bool   `json:"is_st"`       // Special treatment
}

// GetStockList returns the list of A-share stocks.
func (t *TushareAdapter) GetStockList(exchange, market string) ([]StockBasic, error) {
	params := map[string]any{}
	if exchange != "" {
		params["exchange"] = exchange // SSE, SZSE
	}
	if market != "" {
		params["market"] = market // 主板, 创业板, 科创板, 北交所
	}

	resp, err := t.request("stock_basic", params, "")
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("no data returned")
	}

	return parseStockList(resp.Data), nil
}

func parseStockList(data *TushareData) []StockBasic {
	fieldIdx := make(map[string]int)
	for i, f := range data.Fields {
		fieldIdx[f] = i
	}

	var result []StockBasic
	for _, item := range data.Items {
		s := StockBasic{}
		if idx, ok := fieldIdx["ts_code"]; ok && idx < len(item) {
			s.TSCode = toString(item[idx])
		}
		if idx, ok := fieldIdx["symbol"]; ok && idx < len(item) {
			s.Symbol = toString(item[idx])
		}
		if idx, ok := fieldIdx["name"]; ok && idx < len(item) {
			s.Name = toString(item[idx])
		}
		if idx, ok := fieldIdx["area"]; ok && idx < len(item) {
			s.Area = toString(item[idx])
		}
		if idx, ok := fieldIdx["industry"]; ok && idx < len(item) {
			s.Industry = toString(item[idx])
		}
		if idx, ok := fieldIdx["market"]; ok && idx < len(item) {
			s.Market = toString(item[idx])
		}
		if idx, ok := fieldIdx["list_date"]; ok && idx < len(item) {
			s.ListDate = toString(item[idx])
		}
		if idx, ok := fieldIdx["is_hs300"]; ok && idx < len(item) {
			s.IsHS300 = toString(item[idx]) == "1"
		}
		if idx, ok := fieldIdx["is_sz50"]; ok && idx < len(item) {
			s.IsSZ50 = toString(item[idx]) == "1"
		}
		if idx, ok := fieldIdx["is_st"]; ok && idx < len(item) {
			s.IsST = toString(item[idx]) == "1"
		}
		result = append(result, s)
	}
	return result
}

// ── Daily Bars ───────────────────────────────────────────────────

// DailyBar holds a single day's OHLCV data for A-shares.
type DailyBar struct {
	TSCode    string  `json:"ts_code"`
	TradeDate string  `json:"trade_date"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	PreClose  float64 `json:"pre_close"`
	Change    float64 `json:"change"`
	PctChg    float64 `json:"pct_chg"`
	Volume    float64 `json:"vol"`      // 手 (100 shares)
	Amount    float64 `json:"amount"`   // 千元
}

// GetDailyBars returns daily OHLCV bars for a stock.
func (t *TushareAdapter) GetDailyBars(tsCode, startDate, endDate string) ([]DailyBar, error) {
	params := map[string]any{
		"ts_code":    tsCode,
		"start_date": startDate,
		"end_date":   endDate,
	}

	resp, err := t.request("daily", params, "")
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("no data returned")
	}

	return parseDailyBars(resp.Data), nil
}

func parseDailyBars(data *TushareData) []DailyBar {
	fieldIdx := make(map[string]int)
	for i, f := range data.Fields {
		fieldIdx[f] = i
	}

	var result []DailyBar
	for _, item := range data.Items {
		b := DailyBar{}
		if idx, ok := fieldIdx["ts_code"]; ok && idx < len(item) {
			b.TSCode = toString(item[idx])
		}
		if idx, ok := fieldIdx["trade_date"]; ok && idx < len(item) {
			b.TradeDate = toString(item[idx])
		}
		if idx, ok := fieldIdx["open"]; ok && idx < len(item) {
			b.Open = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["high"]; ok && idx < len(item) {
			b.High = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["low"]; ok && idx < len(item) {
			b.Low = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["close"]; ok && idx < len(item) {
			b.Close = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["pre_close"]; ok && idx < len(item) {
			b.PreClose = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["change"]; ok && idx < len(item) {
			b.Change = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["pct_chg"]; ok && idx < len(item) {
			b.PctChg = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["vol"]; ok && idx < len(item) {
			b.Volume = toFloat(item[idx])
		}
		if idx, ok := fieldIdx["amount"]; ok && idx < len(item) {
			b.Amount = toFloat(item[idx])
		}
		result = append(result, b)
	}
	return result
}

// ── Index List ─────────────────────────────────────────────────

// IndexBasic holds index information.
type IndexBasic struct {
	TSCode   string `json:"ts_code"`
	Name     string `json:"name"`
	Market   string `json:"market"`
	Publisher string `json:"publisher"`
	Category string `json:"category"`
	BaseDate string `json:"base_date"`
	BasePoint float64 `json:"base_point"`
}

// GetIndexList returns major A-share indices.
func (t *TushareAdapter) GetIndexList(market string) ([]IndexBasic, error) {
	params := map[string]any{}
	if market != "" {
		params["market"] = market // SW, MS, CSI, SSE, SZSE, CICC, OTH
	}

	resp, err := t.request("index_basic", params, "")
	if err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("no data returned")
	}

	return parseIndexList(resp.Data), nil
}

func parseIndexList(data *TushareData) []IndexBasic {
	fieldIdx := make(map[string]int)
	for i, f := range data.Fields {
		fieldIdx[f] = i
	}

	var result []IndexBasic
	for _, item := range data.Items {
		idx := IndexBasic{}
		if i, ok := fieldIdx["ts_code"]; ok && i < len(item) {
			idx.TSCode = toString(item[i])
		}
		if i, ok := fieldIdx["name"]; ok && i < len(item) {
			idx.Name = toString(item[i])
		}
		if i, ok := fieldIdx["market"]; ok && i < len(item) {
			idx.Market = toString(item[i])
		}
		if i, ok := fieldIdx["publisher"]; ok && i < len(item) {
			idx.Publisher = toString(item[i])
		}
		if i, ok := fieldIdx["category"]; ok && i < len(item) {
			idx.Category = toString(item[i])
		}
		if i, ok := fieldIdx["base_date"]; ok && i < len(item) {
			idx.BaseDate = toString(item[i])
		}
		if i, ok := fieldIdx["base_point"]; ok && i < len(item) {
			idx.BasePoint = toFloat(item[i])
		}
		result = append(result, idx)
	}
	return result
}

// ── Helpers ────────────────────────────────────────────────────

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case int:
		return float64(val)
	default:
		return 0
	}
}

// DailyBarToOHLCV converts Tushare daily bars to standard OHLCV format.
func DailyBarToOHLCV(bars []DailyBar, symbol string) []OHLCV {
	result := make([]OHLCV, len(bars))
	for i, b := range bars {
		// Parse trade_date (YYYYMMDD) to timestamp
		t, _ := time.Parse("20060102", b.TradeDate)
		result[i] = OHLCV{
			Symbol:   symbol,
			Interval: "1d",
			Time:     t.UnixMilli(),
			Open:     b.Open,
			High:     b.High,
			Low:      b.Low,
			Close:    b.Close,
			Volume:   b.Volume * 100, // 手 → 股
		}
	}
	return result
}

// OHLCV is the standard candlestick format (re-export from data package).
type OHLCV struct {
	Symbol   string  `json:"symbol"`
	Interval string  `json:"interval"`
	Time     int64   `json:"time"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
}
