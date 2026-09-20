package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// incomeTestRow 构造一条 /fapi/v1/income 的 REALIZED_PNL 流水响应行。
func incomeTestRow(symbol string, income float64, ts int64) map[string]any {
	return map[string]any{
		"symbol":     symbol,
		"incomeType": "REALIZED_PNL",
		"asset":      "USDT",
		"income":     income,
		"time":       ts,
		"tradeId":    "trade-" + strconv.FormatInt(ts, 10),
		"info":       "",
	}
}

// newIncomeServer 模拟 Binance fapi income 分页：按 startTime/endTime 过滤内存流水，
// 按 limit 截断（升序）。记录每次请求的 startTime 供翻页断言。
func newIncomeServer(t *testing.T, rows []map[string]any, startTimes *[]string, hits *int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		*hits++
		q := r.URL.Query()
		*startTimes = append(*startTimes, q.Get("startTime"))
		if q.Get("incomeType") != "REALIZED_PNL" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if q.Get("signature") == "" || r.Header.Get("X-MBX-APIKEY") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var start, end int64 = -1 << 62, 1 << 62
		if v := q.Get("startTime"); v != "" {
			start, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("endTime"); v != "" {
			end, _ = strconv.ParseInt(v, 10, 64)
		}
		limit, err := strconv.Atoi(q.Get("limit"))
		if err != nil || limit <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		out := []map[string]any{}
		for _, row := range rows {
			ts, _ := row["time"].(int64) // 内存行是原生 int64（响应侧才走 json.Number 宽松解析）
			if ts >= start && ts <= end {
				out = append(out, row)
			}
		}
		if len(out) > limit {
			out = out[:limit]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
}

// TestGetRealizedPnLIncomesPaginatesByStartTime 分页：limit=2、5 行流水应翻 3 页
// （2+2+1，短页终止），游标按末行 time+1 递增。
func TestGetRealizedPnLIncomesPaginatesByStartTime(t *testing.T) {
	rows := []map[string]any{
		incomeTestRow("BTCUSDT", 100, 1000),
		incomeTestRow("BTCUSDT", -40, 2000),
		incomeTestRow("ETHUSDT", 25, 3000),
		incomeTestRow("ETHUSDT", 5, 4000),
		incomeTestRow("BTCUSDT", 10, 5000),
	}
	var startTimes []string
	hits := 0
	srv := newIncomeServer(t, rows, &startTimes, &hits)
	defer srv.Close()

	got, err := getFapiIncomeRows(srv.Client(), srv.URL, "key", "secret", "REALIZED_PNL", "", 0, 0, 2)
	if err != nil {
		t.Fatalf("getFapiIncomeRows: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("应取回全部 5 行: %d", len(got))
	}
	if hits != 3 {
		t.Fatalf("应翻 3 页 (2+2+1 短页终止): %d", hits)
	}
	// 第一页不带 startTime，之后两页游标 = 上一页末行 time + 1。
	wantStarts := []string{"", "2001", "4001"}
	for i, w := range wantStarts {
		if startTimes[i] != w {
			t.Fatalf("第 %d 页 startTime=%q 期望 %q (all=%v)", i+1, startTimes[i], w, startTimes)
		}
	}
	sum := 0.0
	for _, m := range got {
		sum += floatOf(m["income"])
	}
	if sum != 100.0 {
		t.Fatalf("income 合计异常: %v", sum)
	}
}

// TestGetRealizedPnLIncomesStopsOnEmptyPage 空页终止：窗口内无流水时只请求 1 次即返回空。
func TestGetRealizedPnLIncomesStopsOnEmptyPage(t *testing.T) {
	rows := []map[string]any{incomeTestRow("BTCUSDT", 1, 500)}
	var startTimes []string
	hits := 0
	srv := newIncomeServer(t, rows, &startTimes, &hits)
	defer srv.Close()

	got, err := getFapiIncomeRows(srv.Client(), srv.URL, "key", "secret", "REALIZED_PNL", "", 600, 1000, 1000)
	if err != nil {
		t.Fatalf("getFapiIncomeRows: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("窗口外流水不应返回: %+v", got)
	}
	if hits != 1 {
		t.Fatalf("空页应 1 次请求即终止: %d", hits)
	}
}

// TestGetRealizedPnLIncomesViaAdapter 端到端：BINANCE_FAPI_URL 覆盖后走公开方法
// （含签名头、incomeType、解析与求和都由 reconcile 侧完成，这里验证方法本身）。
func TestGetRealizedPnLIncomesViaAdapter(t *testing.T) {
	rows := []map[string]any{
		incomeTestRow("BTCUSDT", 12.5, 10_000),
		incomeTestRow("BTCUSDT", -2.5, 20_000),
	}
	var startTimes []string
	hits := 0
	srv := newIncomeServer(t, rows, &startTimes, &hits)
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)

	b := NewBinanceAdapter("key", "secret", false)
	incomes, err := b.GetRealizedPnLIncomes("", 0, 0)
	if err != nil {
		t.Fatalf("GetRealizedPnLIncomes: %v", err)
	}
	if len(incomes) != 2 {
		t.Fatalf("应返回 2 条流水: %+v", incomes)
	}
	if incomes[0].Symbol != "BTCUSDT" || incomes[0].Income != 12.5 || incomes[0].Time != 10_000 || incomes[0].TradeID != "trade-10000" {
		t.Fatalf("流水解析异常: %+v", incomes[0])
	}
	if incomes[1].IncomeType != "REALIZED_PNL" || incomes[1].Asset != "USDT" {
		t.Fatalf("流水字段异常: %+v", incomes[1])
	}
	if hits != 1 {
		t.Fatalf("单页即可返回时不应翻页: %d", hits)
	}
}

// TestGetFuturesWalletBalance 解析 /fapi/v2/balance 的 USDT 钱包余额行。
func TestGetFuturesWalletBalance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/fapi/v2/balance" || q.Get("asset") == "" || q.Get("signature") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"accountAlias":"abc","asset":"USDT","balance":"1024.50","crossWalletBalance":"1024.50",
			 "crossUnPnl":"-12.25","availableBalance":"900.00","maxWithdrawAmount":"1024.50","marginAvailable":true,"updateTime":1720000000000},
			{"accountAlias":"abc","asset":"BNB","balance":"1.0","crossWalletBalance":"1.0",
			 "crossUnPnl":"0","availableBalance":"1.0","maxWithdrawAmount":"1.0","marginAvailable":true,"updateTime":1720000000000}
		]`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)

	b := NewBinanceAdapter("key", "secret", false)
	row, err := b.GetFuturesWalletBalance("USDT")
	if err != nil {
		t.Fatalf("GetFuturesWalletBalance: %v", err)
	}
	if row == nil {
		t.Fatal("应返回 USDT 行")
	}
	if row.Asset != "USDT" || row.Balance != 1024.50 || row.CrossUnPnl != -12.25 ||
		row.AvailableBalance != 900.00 || row.MaxWithdrawAmount != 1024.50 || row.UpdateTime != 1720000000000 {
		t.Fatalf("余额行解析异常: %+v", row)
	}

	// 账户中不存在的资产：nil, nil。
	missing, err := b.GetFuturesWalletBalance("USDC")
	if err != nil {
		t.Fatalf("GetFuturesWalletBalance(missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("不存在资产应返回 nil: %+v", missing)
	}
}
