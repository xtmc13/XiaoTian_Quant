package adapter

import (
	"testing"
)

func TestToString(t *testing.T) {
	if got := toString("hello"); got != "hello" {
		t.Errorf("toString(string) = %q, want hello", got)
	}
	if got := toString(42); got != "42" {
		t.Errorf("toString(int) = %q, want 42", got)
	}
	if got := toString(3.14); got != "3.14" {
		t.Errorf("toString(float) = %q, want 3.14", got)
	}
	if got := toString(nil); got != "" {
		t.Errorf("toString(nil) = %q, want empty", got)
	}
}

func TestToFloat(t *testing.T) {
	if got := toFloat(3.14); got != 3.14 {
		t.Errorf("toFloat(float64) = %f, want 3.14", got)
	}
	if got := toFloat("2.5"); got != 2.5 {
		t.Errorf("toFloat(string) = %f, want 2.5", got)
	}
	if got := toFloat(10); got != 10.0 {
		t.Errorf("toFloat(int) = %f, want 10.0", got)
	}
	if got := toFloat(nil); got != 0 {
		t.Errorf("toFloat(nil) = %f, want 0", got)
	}
}

func TestParseStockList(t *testing.T) {
	data := &TushareData{
		Fields: []string{"ts_code", "symbol", "name", "area", "industry", "market", "list_date", "is_hs300", "is_sz50", "is_st"},
		Items: [][]any{
			{"000001.SZ", "000001", "平安银行", "深圳", "银行", "主板", "19910403", "1", "0", "0"},
			{"600000.SH", "600000", "浦发银行", "上海", "银行", "主板", "19991110", "1", "1", "0"},
		},
	}

	stocks := parseStockList(data)
	if len(stocks) != 2 {
		t.Fatalf("expected 2 stocks, got %d", len(stocks))
	}

	if stocks[0].TSCode != "000001.SZ" {
		t.Errorf("expected 000001.SZ, got %s", stocks[0].TSCode)
	}
	if stocks[0].Name != "平安银行" {
		t.Errorf("expected 平安银行, got %s", stocks[0].Name)
	}
	if !stocks[0].IsHS300 {
		t.Error("expected HS300 constituent")
	}
	if stocks[0].IsSZ50 {
		t.Error("expected not SZ50")
	}
	if stocks[0].IsST {
		t.Error("expected not ST")
	}

	if stocks[1].TSCode != "600000.SH" {
		t.Errorf("expected 600000.SH, got %s", stocks[1].TSCode)
	}
	if !stocks[1].IsSZ50 {
		t.Error("expected SZ50 constituent")
	}
}

func TestParseDailyBars(t *testing.T) {
	data := &TushareData{
		Fields: []string{"ts_code", "trade_date", "open", "high", "low", "close", "pre_close", "change", "pct_chg", "vol", "amount"},
		Items: [][]any{
			{"000001.SZ", "20240102", 10.5, 11.0, 10.2, 10.8, 10.5, 0.3, 2.86, 150000, 1600000},
			{"000001.SZ", "20240103", 10.8, 11.2, 10.6, 11.1, 10.8, 0.3, 2.78, 180000, 2000000},
		},
	}

	bars := parseDailyBars(data)
	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}

	if bars[0].TSCode != "000001.SZ" {
		t.Errorf("expected 000001.SZ, got %s", bars[0].TSCode)
	}
	if bars[0].TradeDate != "20240102" {
		t.Errorf("expected 20240102, got %s", bars[0].TradeDate)
	}
	if bars[0].Open != 10.5 {
		t.Errorf("expected open 10.5, got %f", bars[0].Open)
	}
	if bars[0].Close != 10.8 {
		t.Errorf("expected close 10.8, got %f", bars[0].Close)
	}
	if bars[0].Volume != 150000 {
		t.Errorf("expected volume 150000, got %f", bars[0].Volume)
	}
	if bars[0].Amount != 1600000 {
		t.Errorf("expected amount 1600000, got %f", bars[0].Amount)
	}
}

func TestDailyBarToOHLCV(t *testing.T) {
	bars := []DailyBar{
		{TSCode: "000001.SZ", TradeDate: "20240102", Open: 10.5, High: 11.0, Low: 10.2, Close: 10.8, Volume: 150000, Amount: 1600000},
	}

	ohlcv := DailyBarToOHLCV(bars, "000001.SZ")
	if len(ohlcv) != 1 {
		t.Fatalf("expected 1 ohlcv, got %d", len(ohlcv))
	}

	if ohlcv[0].Symbol != "000001.SZ" {
		t.Errorf("expected symbol 000001.SZ, got %s", ohlcv[0].Symbol)
	}
	if ohlcv[0].Interval != "1d" {
		t.Errorf("expected interval 1d, got %s", ohlcv[0].Interval)
	}
	if ohlcv[0].Open != 10.5 {
		t.Errorf("expected open 10.5, got %f", ohlcv[0].Open)
	}
	if ohlcv[0].Volume != 15000000 { // 150000 * 100 (手→股)
		t.Errorf("expected volume 15000000 (手→股), got %f", ohlcv[0].Volume)
	}
}

func TestTushareAdapterName(t *testing.T) {
	ta := NewTushareAdapter("test-token")
	if ta.Name() != "tushare" {
		t.Errorf("expected name tushare, got %s", ta.Name())
	}
}

func TestParseIndexList(t *testing.T) {
	data := &TushareData{
		Fields: []string{"ts_code", "name", "market", "publisher", "category", "base_date", "base_point"},
		Items: [][]any{
			{"000001.SH", "上证指数", "SSE", "上海证券交易所", "综合指数", "19901219", 100.0},
			{"399001.SZ", "深证成指", "SZSE", "深圳证券交易所", "综合指数", "19910403", 1000.0},
		},
	}

	indices := parseIndexList(data)
	if len(indices) != 2 {
		t.Fatalf("expected 2 indices, got %d", len(indices))
	}

	if indices[0].TSCode != "000001.SH" {
		t.Errorf("expected 000001.SH, got %s", indices[0].TSCode)
	}
	if indices[0].Name != "上证指数" {
		t.Errorf("expected 上证指数, got %s", indices[0].Name)
	}
	if indices[0].BasePoint != 100.0 {
		t.Errorf("expected base point 100.0, got %f", indices[0].BasePoint)
	}
}
