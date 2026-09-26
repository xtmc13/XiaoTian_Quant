package adapter

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newIBKRTestEnv 组装指向 httptest.Server 的 *IBKRAdapter（同包内直接注入
// baseURL/httpClient，复用 server 的 cookie jar，模拟已认证会话）。
func newIBKRTestEnv(t *testing.T, handler http.Handler) *IBKRAdapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	jar, _ := cookiejar.New(nil)
	a := &IBKRAdapter{
		cfg:        IBKRConfig{Host: "localhost", Port: 5000, Timeout: 15 * time.Second},
		baseURL:    srv.URL,
		paperURL:   srv.URL,
		httpClient: &http.Client{Timeout: 15 * time.Second, Jar: jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}, //nolint:gosec // 测试
		conids:     make(map[string]int64),
		conidSym:   make(map[int64]string),
		stopCh:     make(chan struct{}),
	}
	t.Cleanup(srv.Close)
	return a
}

// ibkrBaseMux 注册认证/账户/合约搜索桩，测试按需追加业务端点。
func ibkrBaseMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/iserver/auth/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authenticated":true,"connected":true,"competing":false}`)
	})
	mux.HandleFunc("/iserver/reauthenticate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"NOT_AUTH"}`)
	})
	mux.HandleFunc("/portfolio/accounts", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"accounts":["DU123456"]}`)
	})
	mux.HandleFunc("/iserver/secdef/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") == "BTC" {
			fmt.Fprint(w, `[{"conid":479724371,"symbol":"BTC","companyName":"Bitcoin","securityType":"CRYPTO","listingExchange":"PAXOS","currency":"USD"}]`)
			return
		}
		fmt.Fprint(w, `[{"conid":265598,"symbol":"AAPL","companyName":"APPLE INC","securityType":"STK","listingExchange":"NASDAQ","currency":"USD"}]`)
	})
	return mux
}

// ── 基础行为 ─────────────────────────────────────────────────

func TestIBKRAdapterName(t *testing.T) {
	a := NewIBKRAdapter(IBKRConfig{})
	if a.Name() != "ibkr" {
		t.Fatalf("name should be ibkr, got %s", a.Name())
	}
}

func TestIBKRAdapterDefaultEndpoints(t *testing.T) {
	a := NewIBKRAdapter(IBKRConfig{})
	if a.baseURL != "https://localhost:5000/v1/api" {
		t.Fatalf("baseURL = %s", a.baseURL)
	}
	if a.paperURL != a.baseURL {
		t.Fatalf("paperURL 默认同 baseURL，got %s", a.paperURL)
	}
	a2 := NewIBKRAdapter(IBKRConfig{Host: "127.0.0.1", Port: 5001, PaperHost: "127.0.0.1", PaperPort: 5002, Paper: true})
	if a2.baseURL != "https://127.0.0.1:5001/v1/api" {
		t.Fatalf("baseURL = %s", a2.baseURL)
	}
	if a2.paperURL != "https://127.0.0.1:5002/v1/api" {
		t.Fatalf("paperURL = %s", a2.paperURL)
	}
}

func TestIBKRStartStopIsConnected(t *testing.T) {
	a := NewIBKRAdapter(IBKRConfig{})
	if a.IsConnected() {
		t.Fatal("初始不应 connected")
	}
	if err := a.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// ── 认证维持 / 重认证 ────────────────────────────────────────

// TestIBKRReauthenticateOnExpiredSession：auth/status 先报未认证，
// 私有端点调用应触发一次 POST /iserver/reauthenticate，之后请求成功。
func TestIBKRReauthenticateOnExpiredSession(t *testing.T) {
	var authed int32
	mux := http.NewServeMux()
	mux.HandleFunc("/iserver/auth/status", func(w http.ResponseWriter, r *http.Request) {
		ok := atomic.LoadInt32(&authed) == 1
		fmt.Fprintf(w, `{"authenticated":%v,"connected":true,"competing":false}`, ok)
	})
	var reauthCalls int32
	mux.HandleFunc("/iserver/reauthenticate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reauthCalls, 1)
		atomic.StoreInt32(&authed, 1)
		fmt.Fprint(w, `{"message":"reauthenticated"}`)
	})
	mux.HandleFunc("/portfolio/accounts", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"accounts":["DU123456"]}`)
	})
	mux.HandleFunc("/portfolio/DU123456/summary", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"netliquidation":{"value":"103220.12","asset":"USD"},"availablefunds":{"value":"50000.00","asset":"USD"}}`)
	})
	a := newIBKRTestEnv(t, mux)

	bal, err := a.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance after reauth: %v", err)
	}
	if atomic.LoadInt32(&reauthCalls) != 1 {
		t.Fatalf("reauthenticate 应调用 1 次，got %d", reauthCalls)
	}
	if len(bal) != 1 || bal[0]["asset"] != "USD" {
		t.Fatalf("balance asset = %v", bal)
	}
	if bal[0]["equity"] != 103220.12 {
		t.Fatalf("equity = %v", bal[0]["equity"])
	}
	if bal[0]["free"] != 50000.0 {
		t.Fatalf("free = %v", bal[0]["free"])
	}
	if !a.IsConnected() {
		t.Fatal("IsConnected 应为 true")
	}
}

// TestIBKRAuthStatusMaintainsSession：auth/status 返回已认证时不得触发 reauthenticate。
func TestIBKRAuthStatusMaintainsSession(t *testing.T) {
	var reauthCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/iserver/auth/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authenticated":true,"connected":true,"competing":false}`)
	})
	mux.HandleFunc("/iserver/reauthenticate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reauthCalls, 1)
	})
	mux.HandleFunc("/portfolio/accounts", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"accounts":["DU1","DU2"]}`)
	})
	mux.HandleFunc("/portfolio/DU1/positions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	a := newIBKRTestEnv(t, mux)

	pos, err := a.GetPositions()
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(pos) != 0 {
		t.Fatalf("positions = %v", pos)
	}
	if atomic.LoadInt32(&reauthCalls) != 0 {
		t.Fatalf("会话有效不应 reauthenticate，got %d", reauthCalls)
	}
	// account id 自动取第一个
	if acct, _ := a.account(); acct != "DU1" {
		t.Fatalf("account = %s", acct)
	}
}

// TestIBKRReauthenticateFailsGracefully：重认证也失败时返回可操作的错误提示。
func TestIBKRReauthenticateFailsGracefully(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/iserver/auth/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authenticated":false,"connected":false}`)
	})
	mux.HandleFunc("/iserver/reauthenticate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"NOT_AUTH"}`)
	})
	a := newIBKRTestEnv(t, mux)

	_, err := a.GetBalance()
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "IBKR Client Portal 会话未认证") {
		t.Fatalf("错误应提示重新登录: %v", err)
	}
}

// ── 下单 / 撤单 ────────────────────────────────────────────

// TestIBKRPlaceOrder：限价单全链路（secdef 搜索 → conid → POST orders），
// 校验请求体字段并解析返回 order_id。
func TestIBKRPlaceOrder(t *testing.T) {
	mux := ibkrBaseMux()
	var gotBody string
	mux.HandleFunc("/iserver/account/DU123456/orders", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		fmt.Fprint(w, `[{"id":"123456789","message":[]}]`)
	})
	a := newIBKRTestEnv(t, mux)

	res, err := a.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.5, 0.25)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if res["order_id"] != "123456789" {
		t.Fatalf("order_id = %v", res["order_id"])
	}
	if gotBody == "" {
		t.Fatal("应捕获请求体")
	}
	var parsed struct {
		Orders []map[string]any `json:"orders"`
	}
	if err := json.Unmarshal([]byte(gotBody), &parsed); err != nil {
		t.Fatalf("请求体非法 JSON: %v", err)
	}
	o := parsed.Orders[0]
	if o["conid"] != float64(479724371) {
		t.Fatalf("conid = %v", o["conid"])
	}
	if o["side"] != "BUY" || o["orderType"] != "LMT" || o["price"] != "50000.50" || o["tif"] != "DAY" {
		t.Fatalf("order 字段异常: %v", o)
	}
}

// TestIBKRPlaceMarketAndStopOrder：市价单（MKT 无 price）与止损单（STP 用 auxPrice）。
func TestIBKRPlaceMarketAndStopOrder(t *testing.T) {
	mux := ibkrBaseMux()
	var bodies []string
	mux.HandleFunc("/iserver/account/DU123456/orders", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		fmt.Fprint(w, `[{"id":"1","message":[]}]`)
	})
	a := newIBKRTestEnv(t, mux)

	if _, err := a.PlaceOrder("AAPL", "buy", "MARKET", 0, 10); err != nil {
		t.Fatalf("market order: %v", err)
	}
	if _, err := a.PlaceOrder("AAPL", "SELL", "STOP", 180.25, 10); err != nil {
		t.Fatalf("stop order: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies = %d", len(bodies))
	}
	var mkt struct {
		Orders []map[string]any `json:"orders"`
	}
	json.Unmarshal([]byte(bodies[0]), &mkt)
	if mkt.Orders[0]["orderType"] != "MKT" {
		t.Fatalf("市价单类型 = %v", mkt.Orders[0]["orderType"])
	}
	if _, hasPrice := mkt.Orders[0]["price"]; hasPrice {
		t.Fatal("市价单不应带 price")
	}
	var stp struct {
		Orders []map[string]any `json:"orders"`
	}
	json.Unmarshal([]byte(bodies[1]), &stp)
	if stp.Orders[0]["orderType"] != "STP" || stp.Orders[0]["auxPrice"] != "180.25" {
		t.Fatalf("止损单字段异常: %v", stp.Orders[0])
	}
}

// TestIBKRPlaceOrderErrorParsing：IBKR 下单错误（数组内 message / 顶层 error）正确解析。
func TestIBKRPlaceOrderErrorParsing(t *testing.T) {
	cases := []struct {
		name string
		code int
		body string
		want string
	}{
		{"array_error", 200, `[{"error":"Order rejected: price exceeds limit"}]`, "price exceeds limit"},
		{"top_error", 400, `{"error":"Insufficient funds"}`, "Insufficient funds"},
		{"message_no_id", 200, `[{"message":"Order confirmation required: ..."}]`, "rejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := ibkrBaseMux()
			mux.HandleFunc("/iserver/account/DU123456/orders", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			})
			a := newIBKRTestEnv(t, mux)
			_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 1)
			if err == nil {
				t.Fatal("应返回错误")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误应包含 %q: %v", tc.want, err)
			}
		})
	}
}

// TestIBKRCancelOrder：DELETE /order/{orderId} 撤单。
func TestIBKRCancelOrder(t *testing.T) {
	mux := ibkrBaseMux()
	var gotPath string
	mux.HandleFunc("/iserver/account/DU123456/order/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"order_id":"999888","msg":"Order cancelled"}`)
	})
	a := newIBKRTestEnv(t, mux)

	res, err := a.CancelOrder("AAPL", "999888")
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/iserver/account/DU123456/order/999888") {
		t.Fatalf("path = %s", gotPath)
	}
	if res["status"] != "CANCELLED" {
		t.Fatalf("status = %v", res["status"])
	}
}

// ── 订单查询 / 成交 ──────────────────────────────────────────

// TestIBKRGetOpenOrders：/iserver/account/orders 数组响应解析 + symbol 过滤。
func TestIBKRGetOpenOrders(t *testing.T) {
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/account/orders", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"orderId":"111","conid":265598,"side":"BUY","quantity":"10","price":"190.5","status":"Submitted","orderType":"LMT"},
			{"orderId":"222","conid":479724371,"side":"SELL","quantity":"0.1","price":"51000","status":"Filled","orderType":"LMT"}
		]`)
	})
	a := newIBKRTestEnv(t, mux)
	// 预热 conid 缓存以便 symbol 反解
	if _, err := a.conid("AAPL"); err != nil {
		t.Fatalf("conid AAPL: %v", err)
	}
	if _, err := a.conid("BTCUSDT"); err != nil {
		t.Fatalf("conid BTCUSDT: %v", err)
	}

	orders, err := a.GetOpenOrders("")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("orders = %d", len(orders))
	}
	if orders[0]["order_id"] != "111" || orders[0]["status"] != "NEW" || orders[0]["symbol"] != "AAPL" {
		t.Fatalf("order[0] = %v", orders[0])
	}
	if orders[1]["status"] != "FILLED" || orders[1]["symbol"] != "BTCUSDT" {
		t.Fatalf("order[1] = %v", orders[1])
	}

	filtered, err := a.GetOpenOrders("BTCUSDT")
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(filtered) != 1 || filtered[0]["order_id"] != "222" {
		t.Fatalf("filtered = %v", filtered)
	}
}

// TestIBKRGetTrades：成交查询（BOT/SLD → BUY/SELL，trade_time_r 毫秒时间戳）。
func TestIBKRGetTrades(t *testing.T) {
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/account/trades", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"execution_id":"0000e0d5.abc","symbol":"AAPL","side":"BOT","order_ref":"111","conid":265598,"quantity":"10","price":"190.5","trade_time_r":1700000000000},
			{"execution_id":"0000e0d5.def","symbol":"AAPL","side":"SLD","order_ref":"222","conid":265598,"quantity":"5","price":"195.0","trade_time_r":1700000001000}
		]`)
	})
	a := newIBKRTestEnv(t, mux)

	trades, err := a.GetTrades()
	if err != nil {
		t.Fatalf("GetTrades: %v", err)
	}
	if len(trades) != 2 {
		t.Fatalf("trades = %d", len(trades))
	}
	if trades[0]["side"] != "BUY" || trades[1]["side"] != "SELL" {
		t.Fatalf("side 归一化失败: %v %v", trades[0]["side"], trades[1]["side"])
	}
	if trades[0]["trade_id"] != "0000e0d5.abc" || trades[0]["price"] != 190.5 || trades[0]["qty"] != float64(10) {
		t.Fatalf("trade[0] = %v", trades[0])
	}
	if trades[0]["time"] != int64(1700000000000) {
		t.Fatalf("time = %v", trades[0]["time"])
	}

	// GetOrderTrades 按订单过滤（对账窄接口形状）
	ot, err := a.GetOrderTrades("AAPL", "222")
	if err != nil {
		t.Fatalf("GetOrderTrades: %v", err)
	}
	if len(ot) != 1 || ot[0].ID != "0000e0d5.def" || ot[0].Side != "SELL" || ot[0].Price != 195.0 {
		t.Fatalf("order trades = %+v", ot)
	}
}

// TestIBKRQueryOrderStatus：/iserver/account/order/status/{id} 状态归一化。
func TestIBKRQueryOrderStatus(t *testing.T) {
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/account/order/status/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/111") {
			fmt.Fprint(w, `{"order_id":"111","status":"Filled","filled_quantity":"10","avg_price":"190.5"}`)
			return
		}
		fmt.Fprint(w, `{"order_id":"333","status":"Submitted","filled_quantity":"3","remaining_quantity":"7","avg_price":"190.4"}`)
	})
	a := newIBKRTestEnv(t, mux)

	info, err := a.QueryOrderStatus("AAPL", "111")
	if err != nil {
		t.Fatalf("QueryOrderStatus: %v", err)
	}
	if info.Status != "FILLED" || info.FilledQty != 10 || info.AvgPrice != 190.5 {
		t.Fatalf("info = %+v", info)
	}

	partial, err := a.QueryOrderStatus("AAPL", "333")
	if err != nil {
		t.Fatalf("partial: %v", err)
	}
	if partial.Status != "PARTIALLY_FILLED" || partial.FilledQty != 3 {
		t.Fatalf("partial = %+v", partial)
	}
}

// ── 持仓 / 余额 ────────────────────────────────────────────

// TestIBKRPositionsAndBalance：持仓数组 + summary 净值解析。
func TestIBKRPositionsAndBalance(t *testing.T) {
	mux := ibkrBaseMux()
	mux.HandleFunc("/portfolio/DU123456/positions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"conid":265598,"contractDesc":"AAPL","position":"100","avgCost":"181.2","marketPrice":"185.5","marketValue":"18550","unrealizedPnl":"430","realizedPnl":"0","currency":"USD","assetClass":"STK"},
			{"conid":479724371,"contractDesc":"BTC","position":"-0.5","avgCost":"51000","marketPrice":"50000","marketValue":"-25000","unrealizedPnl":"500","currency":"USD","assetClass":"CRYPTO"}
		]`)
	})
	mux.HandleFunc("/portfolio/DU123456/summary", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"netliquidation":{"value":"103220.12","asset":"USD"},"availablefunds":{"value":"50000.00","asset":"USD"}}`)
	})
	a := newIBKRTestEnv(t, mux)

	positions, err := a.GetPositions()
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("positions = %d", len(positions))
	}
	p := positions[0]
	if p["symbol"] != "AAPL" || p["qty"] != float64(100) || p["entry_price"] != 181.2 || p["side"] != "LONG" {
		t.Fatalf("position[0] = %v", p)
	}
	if positions[1]["side"] != "SHORT" {
		t.Fatalf("position[1] side = %v", positions[1]["side"])
	}

	bal, err := a.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal[0]["equity"] != 103220.12 || bal[0]["free"] != 50000.0 || bal[0]["asset"] != "USD" {
		t.Fatalf("balance = %v", bal)
	}
	if locked := bal[0]["locked"].(float64); locked < 53220.11 || locked > 53220.13 {
		t.Fatalf("locked = %v", bal[0]["locked"])
	}
}

// ── 行情 / 合约搜索 ──────────────────────────────────────────

func ibkrMarketMux() *http.ServeMux {
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/marketdata/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("conids") == "479724371" {
			fmt.Fprint(w, `[{"conid":479724371,"31":"50001.5C","84":"50000","85":"50002","86":"123.5","88":"49500","7295":"50001.5"}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	})
	return mux
}

// TestIBKRSearchSymbols：合约搜索归一化（股票 + 加密货币）。
func TestIBKRSearchSymbols(t *testing.T) {
	a := newIBKRTestEnv(t, ibkrMarketMux())

	rows, err := a.SearchSymbols("AAPL", "")
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(rows) != 1 || rows[0]["conid"] != float64(265598) || rows[0]["sec_type"] != "STK" {
		t.Fatalf("rows = %v", rows)
	}
}

// TestIBKRSymbolNormalization：上层 symbol → 搜索词映射。
func TestIBKRSymbolNormalization(t *testing.T) {
	cases := map[string]string{
		"BTCUSDT": "BTC",
		"BTCUSD":  "BTC",
		"aapl":    "AAPL",
		"EURUSD":  "EUR.USD",
		"BRK/B":   "BRKB",
	}
	for in, want := range cases {
		if got := ibkrSearchSymbol(in); got != want {
			t.Fatalf("ibkrSearchSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIBKRGetTicker：snapshot 字符串字段（含交易所状态后缀）解析。
func TestIBKRGetTicker(t *testing.T) {
	a := newIBKRTestEnv(t, ibkrMarketMux())

	tk, err := a.GetTicker("BTCUSDT")
	if err != nil {
		t.Fatalf("GetTicker: %v", err)
	}
	if tk["last"] != 50001.5 || tk["bid"] != 50000.0 || tk["ask"] != 50002.0 || tk["volume"] != 123.5 {
		t.Fatalf("ticker = %v", tk)
	}
	// conid 缓存应命中（不再重复搜索）
	c1, _ := a.conid("BTCUSDT")
	c2, _ := a.conid("BTCUSDT")
	if c1 != c2 || c1 != 479724371 {
		t.Fatalf("conid cache = %d/%d", c1, c2)
	}
}

// TestIBKRGetKlines：marketdata/history 解析 + limit 截断。
func TestIBKRGetKlines(t *testing.T) {
	mux := ibkrMarketMux()
	mux.HandleFunc("/iserver/marketdata/history", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bar") != "1h" {
			t.Errorf("bar = %s", r.URL.Query().Get("bar"))
		}
		fmt.Fprint(w, `{"serverId":"s1","symbol":"BTC","data":[
			{"t":1700000000,"o":"49900","h":"50100","l":"49800","c":"50050","v":"12.5"},
			{"t":1700003600,"o":"50050","h":"50200","l":"50000","c":"50150","v":"10.0"},
			{"t":1700007200,"o":"50150","h":"50300","l":"50100","c":"50250","v":"8.25"}
		]}`)
	})
	a := newIBKRTestEnv(t, mux)

	rows, err := a.GetKlines("BTCUSDT", "1h", 2)
	if err != nil {
		t.Fatalf("GetKlines: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	last := rows[1]
	if last[1] != 50150.0 || last[4] != 50250.0 || last[5] != 8.25 {
		t.Fatalf("last bar = %v", last)
	}
	if last[0] != int64(1700007200000) {
		t.Fatalf("ts = %v (应为毫秒)", last[0])
	}

	// GetKlinesRange 区间过滤
	rangeRows, err := a.GetKlinesRange("BTCUSDT", "1h", 1700000000000, 1700003600000, 10)
	if err != nil {
		t.Fatalf("GetKlinesRange: %v", err)
	}
	if len(rangeRows) != 2 {
		t.Fatalf("rangeRows = %d", len(rangeRows))
	}
}

// ── 限流 / 错误处理 ──────────────────────────────────────────

// TestIBKR429Retry：429 限流最多重试 3 次（指数退避）后成功。
func TestIBKR429Retry(t *testing.T) {
	var calls int32
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/marketdata/snapshot", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"throttled"}`)
			return
		}
		fmt.Fprint(w, `[{"conid":479724371,"31":"50001.5","84":"50000","85":"50002","86":"1"}]`)
	})
	a := newIBKRTestEnv(t, mux)

	start := time.Now()
	tk, err := a.GetTicker("BTCUSDT")
	if err != nil {
		t.Fatalf("GetTicker after 429s: %v", err)
	}
	if tk["last"] != 50001.5 {
		t.Fatalf("last = %v", tk["last"])
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("calls = %d", calls)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("应有指数退避耗时，got %v", elapsed)
	}
}

// TestIBKR429Exhausted：连续 429 超过 3 次后失败并报限流错误。
func TestIBKR429Exhausted(t *testing.T) {
	var calls int32
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/marketdata/snapshot", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"throttled"}`)
	})
	a := newIBKRTestEnv(t, mux)

	_, err := a.GetTicker("BTCUSDT")
	if err == nil {
		t.Fatal("应返回限流错误")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("错误应含 429: %v", err)
	}
	if atomic.LoadInt32(&calls) != 4 { // 1 次原始 + 3 次重试
		t.Fatalf("calls = %d", calls)
	}
}

// TestIBKRErrorPropagation：非 2xx 且无 {"error"} 字段时保留状态码与响应摘要。
func TestIBKRErrorPropagation(t *testing.T) {
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/marketdata/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `<html>gateway down</html>`)
	})
	a := newIBKRTestEnv(t, mux)

	_, err := a.GetTicker("BTCUSDT")
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("错误应含状态码: %v", err)
	}
}

// TestIBKR429RetryPostBody：429 重试后 POST 请求体必须完整重发（reader 复用缺陷回归）。
func TestIBKR429RetryPostBody(t *testing.T) {
	var calls int32
	var bodies []string
	mux := ibkrBaseMux()
	mux.HandleFunc("/iserver/account/DU123456/orders", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"throttled"}`)
			return
		}
		fmt.Fprint(w, `[{"id":"777","message":[]}]`)
	})
	a := newIBKRTestEnv(t, mux)

	res, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190.5, 2)
	if err != nil {
		t.Fatalf("PlaceOrder after 429: %v", err)
	}
	if res["order_id"] != "777" {
		t.Fatalf("order_id = %v", res["order_id"])
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || !strings.Contains(bodies[1], `"conid":265598`) {
		t.Fatalf("重试请求体不完整: %v", bodies)
	}
}

// ── paper 路由 ─────────────────────────────────────────────

// TestIBKRPaperRouting：Paper=true 时下单走 paper 网关，行情仍走主网关。
func TestIBKRPaperRouting(t *testing.T) {
	var paperOrderCalls, mainOrderCalls int32
	paperSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&paperOrderCalls, 1)
		fmt.Fprint(w, `[{"id":"555","message":[]}]`)
	}))
	defer paperSrv.Close()

	mainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/iserver/account/"):
			atomic.AddInt32(&mainOrderCalls, 1)
			fmt.Fprint(w, `[{"id":"444","message":[]}]`)
		case strings.HasPrefix(r.URL.Path, "/iserver/secdef/search"):
			fmt.Fprint(w, `[{"conid":265598,"symbol":"AAPL","securityType":"STK","listingExchange":"NASDAQ"}]`)
		case strings.HasPrefix(r.URL.Path, "/iserver/auth/status"):
			fmt.Fprint(w, `{"authenticated":true,"connected":true}`)
		case strings.HasPrefix(r.URL.Path, "/portfolio/accounts"):
			fmt.Fprint(w, `{"accounts":["DU999"]}`)
		}
	}))
	defer mainSrv.Close()

	a := &IBKRAdapter{
		cfg:        IBKRConfig{Host: "localhost", Port: 5000, Paper: true, Timeout: 15 * time.Second},
		baseURL:    mainSrv.URL,
		paperURL:   paperSrv.URL,
		httpClient: &http.Client{Timeout: 15 * time.Second, Jar: mustJar(t)},
		conids:     make(map[string]int64),
		conidSym:   make(map[int64]string),
		stopCh:     make(chan struct{}),
	}

	res, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 1)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if res["order_id"] != "555" {
		t.Fatalf("paper order_id = %v", res["order_id"])
	}
	if atomic.LoadInt32(&paperOrderCalls) != 1 || atomic.LoadInt32(&mainOrderCalls) != 0 {
		t.Fatalf("paper=%d main=%d", paperOrderCalls, mainOrderCalls)
	}
}

func mustJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

// ── 配置加载 / 打分 ──────────────────────────────────────────

func TestIBKRLoadConfigDefaults(t *testing.T) {
	cfg := LoadIBKRConfig()
	if cfg.Host != "localhost" || cfg.Port != 5000 {
		t.Fatalf("defaults = %s:%d", cfg.Host, cfg.Port)
	}
	if cfg.Timeout != 15*time.Second {
		t.Fatalf("timeout = %v", cfg.Timeout)
	}
}

// TestIBKRConidScoring：多结果时优先精确 symbol + 偏好 secType/交易所。
func TestIBKRConidScoring(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/iserver/secdef/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"conid":1,"symbol":"BTC","securityType":"FUT","listingExchange":"CME"},
			{"conid":2,"symbol":"BTC","securityType":"CRYPTO","listingExchange":"PAXOS"}
		]`)
	})
	a := newIBKRTestEnv(t, mux)

	c, err := a.conid("BTCUSDT") // 搜索词 BTC，两条都精确匹配 → CRYPTO/PAXOS 优先于 FUT/CME
	if err != nil {
		t.Fatalf("conid: %v", err)
	}
	if c != 2 {
		t.Fatalf("conid = %d, want 2 (CRYPTO 优先)", c)
	}
}
