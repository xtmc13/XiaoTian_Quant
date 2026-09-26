package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// 测试基建：临时 sqlite（同 handler 包 TestMain 模式），
// 绝不触碰 ./runtime/gateway.db；strategy config 的 JSON 持久化
// 会写到包内 ./runtime，测试结束一并清理。

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agent_test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("DB_PATH", filepath.Join(dir, "gateway.db"))
	_ = os.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	if err := store.InitDB(); err != nil {
		panic(err)
	}
	// 策略引擎用真实事件总线（Register 需要非 nil bus）。
	strategy.GetEngine(event.NewEventBus(1000, 1))

	code := m.Run()
	store.CloseDB()
	_ = os.RemoveAll(dir)
	_ = os.RemoveAll("./runtime")
	os.Exit(code)
}

// ── Mock 依赖 ──

type mockMarket struct {
	ticker  map[string]any
	bid     float64
	ask     float64
	klines  []map[string]any
	symbols []string
	err     error
}

func (m *mockMarket) Ticker24h(_ context.Context, _ string) (map[string]any, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.ticker, nil
}

func (m *mockMarket) BookTicker(_ context.Context, _ string) (float64, float64, error) {
	if m.err != nil {
		return 0, 0, m.err
	}
	return m.bid, m.ask, nil
}

func (m *mockMarket) Klines(_ context.Context, _, _ string, _ int, _, _ int64) ([]map[string]any, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.klines, nil
}

func (m *mockMarket) ExchangeSymbols(_ context.Context) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.symbols, nil
}

type mockMatcher struct {
	placed    []map[string]any
	cancelled []string
	placeErr  error
}

func (m *mockMatcher) PlaceOrder(symbol, side, orderType string, price, quantity float64, userID uint64, storeOrderID string) (map[string]any, error) {
	if m.placeErr != nil {
		return nil, m.placeErr
	}
	m.placed = append(m.placed, map[string]any{
		"symbol": symbol, "side": side, "order_type": orderType,
		"price": price, "quantity": quantity, "user_id": userID,
	})
	return map[string]any{
		"store_order_id": fmt.Sprintf("ord-test-%d", len(m.placed)),
		"filled":         quantity,
		"status":         "FILLED",
	}, nil
}

func (m *mockMatcher) CancelOrder(_ string, storeOrderID string) error {
	m.cancelled = append(m.cancelled, storeOrderID)
	return nil
}

type mockPortfolio struct {
	balances  []*model.Balance
	positions []*model.PositionData
	equity    float64
	pnl       float64
}

func (m *mockPortfolio) TotalEquity() float64               { return m.equity }
func (m *mockPortfolio) TotalPnL() float64                  { return m.pnl }
func (m *mockPortfolio) Balances(_ string) []*model.Balance { return m.balances }
func (m *mockPortfolio) Positions() []*model.PositionData   { return m.positions }

func newTestContext(market MarketDataSource, matcher OrderMatcher, pf PortfolioReader) *ToolContext {
	return &ToolContext{
		UserID:    7,
		Market:    market,
		Matcher:   matcher,
		Portfolio: pf,
		Engine:    safeEngine{eng: strategy.GetEngine(nil)},
	}
}

// ── 工具列表完整性 ──

func TestAllToolsHasSixteen(t *testing.T) {
	tools := AllTools()
	if len(tools) != 16 {
		t.Fatalf("want 16 tools, got %d", len(tools))
	}
	want := map[string]bool{
		"get_market_data": true, "get_klines": true, "place_paper_order": true,
		"get_orders": true, "cancel_order": true, "get_positions": true,
		"get_balance": true, "list_strategies": true, "deploy_strategy": true,
		"start_strategy": true, "stop_strategy": true, "delete_strategy": true,
		"run_backtest": true, "list_backtests": true, "list_markets": true,
		"get_stats": true,
	}
	for _, tl := range tools {
		if !want[tl.Name] {
			t.Errorf("unexpected tool %q", tl.Name)
		}
		delete(want, tl.Name)
		if tl.Description == "" {
			t.Errorf("tool %q has empty description", tl.Name)
		}
		if tl.Schema == nil || tl.Schema["type"] != "object" {
			t.Errorf("tool %q schema missing type=object", tl.Name)
		}
	}
	for missing := range want {
		t.Errorf("missing tool %q", missing)
	}
}

// ── 读工具：行情 ──

func TestGetMarketDataReturnsRealShape(t *testing.T) {
	tc := newTestContext(&mockMarket{
		ticker: map[string]any{
			"lastPrice":          "67234.5",
			"priceChangePercent": "1.25",
			"highPrice":          "68100.0",
			"lowPrice":           "66000.0",
			"quoteVolume":        "123456789.0",
		},
		bid: 67230.1,
		ask: 67235.9,
	}, nil, nil)

	out, err := tc.GetMarketData(context.Background(), map[string]any{"symbol": "btcusdt"})
	if err != nil {
		t.Fatalf("GetMarketData: %v", err)
	}
	m := out.(map[string]any)
	if m["symbol"] != "BTCUSDT" {
		t.Errorf("symbol not uppercased: %v", m["symbol"])
	}
	if m["price"] != 67234.5 {
		t.Errorf("price: got %v", m["price"])
	}
	if math.Abs(m["spread"].(float64)-(67235.9-67230.1)) > 1e-6 {
		t.Errorf("spread: got %v", m["spread"])
	}
	if m["volume_24h"] != 123456789.0 {
		t.Errorf("volume_24h: got %v", m["volume_24h"])
	}
}

func TestGetMarketDataMissingSymbol(t *testing.T) {
	tc := newTestContext(&mockMarket{}, nil, nil)
	if _, err := tc.GetMarketData(context.Background(), map[string]any{}); err == nil {
		t.Fatal("want error for missing symbol")
	}
}

func TestGetKlinesValidation(t *testing.T) {
	klines := make([]map[string]any, 0, 3)
	for i := 0; i < 3; i++ {
		klines = append(klines, map[string]any{
			"timestamp": int64(1700000000000 + i*3600000),
			"open":      100.0, "high": 101.0, "low": 99.0, "close": 100.5, "volume": 12.0,
		})
	}
	tc := newTestContext(&mockMarket{klines: klines}, nil, nil)

	out, err := tc.GetKlines(context.Background(), map[string]any{
		"symbol": "BTCUSDT", "interval": "1h", "limit": 500,
	})
	if err != nil {
		t.Fatalf("GetKlines: %v", err)
	}
	m := out.(map[string]any)
	if m["count"] != 3 {
		t.Errorf("count: got %v", m["count"])
	}
	list := m["klines"].([]map[string]any)
	if list[0]["close"] != 100.5 {
		t.Errorf("kline close: got %v", list[0]["close"])
	}

	// 非法 interval
	if _, err := tc.GetKlines(context.Background(), map[string]any{
		"symbol": "BTCUSDT", "interval": "9h",
	}); err == nil {
		t.Fatal("want error for invalid interval")
	}
	// 缺 symbol
	if _, err := tc.GetKlines(context.Background(), map[string]any{"interval": "1h"}); err == nil {
		t.Fatal("want error for missing symbol")
	}
}

func TestListMarketsReturnsSymbols(t *testing.T) {
	tc := newTestContext(&mockMarket{symbols: []string{"BTCUSDT", "ETHUSDT"}}, nil, nil)
	out, err := tc.ListMarkets(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListMarkets: %v", err)
	}
	m := out.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("count: got %v", m["count"])
	}
}

// ── 读工具：余额/持仓/订单 ──

func TestGetBalanceRealData(t *testing.T) {
	tc := newTestContext(nil, nil, &mockPortfolio{
		balances: []*model.Balance{{Currency: "USDT", Total: 100000, Free: 95000, Used: 5000}},
		equity:   102345.67,
		pnl:      2345.67,
	})
	out, err := tc.GetBalance(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	m := out.(map[string]any)
	if m["total_equity"] != 102345.67 {
		t.Errorf("equity: got %v", m["total_equity"])
	}
	bal := m["balances"].([]*model.Balance)
	if len(bal) != 1 || bal[0].Free != 95000 {
		t.Errorf("balances: got %+v", bal)
	}
}

func TestGetPositionsFilterBySymbol(t *testing.T) {
	tc := newTestContext(nil, nil, &mockPortfolio{
		positions: []*model.PositionData{
			{Symbol: "BTCUSDT", Quantity: 0.5, UnrealizedPnL: 100},
			{Symbol: "ETHUSDT", Quantity: 2, UnrealizedPnL: -50},
		},
	})
	out, err := tc.GetPositions(context.Background(), map[string]any{"symbol": "BTCUSDT"})
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	m := out.(map[string]any)
	if m["count"] != 1 {
		t.Fatalf("count: got %v", m["count"])
	}
}

func TestGetOrdersFromStore(t *testing.T) {
	store.PlaceOrder(map[string]any{
		"symbol": "BTCUSDT", "side": "buy", "order_type": "limit",
		"price": 67000.0, "quantity": 0.1, "status": "NEW",
		"user_id": uint64(7),
	})
	tc := newTestContext(nil, &mockMatcher{}, nil)
	out, err := tc.GetOrders(context.Background(), map[string]any{"symbol": "BTCUSDT"})
	if err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	m := out.(map[string]any)
	if m["count"].(int) < 1 {
		t.Fatalf("expected at least 1 order, got %v", m["count"])
	}
	orders := m["orders"].([]map[string]any)
	if orders[0]["symbol"] != "BTCUSDT" {
		t.Errorf("order symbol: got %v", orders[0]["symbol"])
	}
}

// ── 写工具：paper 下单 ──

func TestPlacePaperOrderSuccess(t *testing.T) {
	matcher := &mockMatcher{}
	tc := newTestContext(nil, matcher, nil)

	out, err := tc.PlacePaperOrder(context.Background(), map[string]any{
		"symbol": "BTCUSDT", "side": "buy", "order_type": "MARKET", "quantity": 0.2,
	})
	if err != nil {
		t.Fatalf("PlacePaperOrder: %v", err)
	}
	m := out.(map[string]any)
	if m["order_id"] != "ord-test-1" {
		t.Errorf("order_id: got %v", m["order_id"])
	}
	// 撮合引擎约定小写 side/order_type
	if matcher.placed[0]["side"] != "buy" || matcher.placed[0]["order_type"] != "market" {
		t.Errorf("engine args: got %+v", matcher.placed[0])
	}
	if matcher.placed[0]["user_id"] != uint64(7) {
		t.Errorf("user_id: got %+v", matcher.placed[0]["user_id"])
	}
}

func TestPlacePaperOrderValidation(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{}, nil)
	cases := []map[string]any{
		{"side": "BUY", "order_type": "MARKET", "quantity": 0.1},                       // 缺 symbol
		{"symbol": "BTCUSDT", "side": "HOLD", "order_type": "MARKET", "quantity": 0.1}, // 非法 side
		{"symbol": "BTCUSDT", "side": "BUY", "order_type": "STOP", "quantity": 0.1},    // 非法 order_type
		{"symbol": "BTCUSDT", "side": "BUY", "order_type": "MARKET", "quantity": -1},   // 数量非正
		{"symbol": "BTCUSDT", "side": "BUY", "order_type": "LIMIT", "quantity": 0.1},   // LIMIT 缺 price
	}
	for i, args := range cases {
		if _, err := tc.PlacePaperOrder(context.Background(), args); err == nil {
			t.Errorf("case %d: want error, got nil", i)
		}
	}
}

func TestPlacePaperOrderEngineError(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{placeErr: fmt.Errorf("insufficient balance")}, nil)
	_, err := tc.PlacePaperOrder(context.Background(), map[string]any{
		"symbol": "BTCUSDT", "side": "BUY", "order_type": "MARKET", "quantity": 999,
	})
	if err == nil || err.Error() != "insufficient balance" {
		t.Fatalf("want engine error, got %v", err)
	}
}

func TestCancelOrderNotFound(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{}, nil)
	if _, err := tc.CancelOrder(context.Background(), map[string]any{"order_id": "ord-none"}); err == nil {
		t.Fatal("want error for unknown order")
	}
}

func TestCancelOrderSuccess(t *testing.T) {
	id := store.PlaceOrder(map[string]any{
		"symbol": "BTCUSDT", "side": "buy", "order_type": "limit",
		"price": 67000.0, "quantity": 0.1, "status": "NEW", "user_id": uint64(7),
	})
	matcher := &mockMatcher{}
	tc := newTestContext(nil, matcher, nil)
	out, err := tc.CancelOrder(context.Background(), map[string]any{"order_id": id})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if out.(map[string]any)["status"] != "cancelled" {
		t.Errorf("status: got %+v", out)
	}
	if len(matcher.cancelled) != 1 || matcher.cancelled[0] != id {
		t.Errorf("matcher cancelled: %+v", matcher.cancelled)
	}
}

func TestCancelOrderOtherUsers(t *testing.T) {
	id := store.PlaceOrder(map[string]any{
		"symbol": "BTCUSDT", "side": "buy", "order_type": "limit",
		"price": 67000.0, "quantity": 0.1, "status": "NEW", "user_id": uint64(99),
	})
	tc := newTestContext(nil, &mockMatcher{}, nil)
	if _, err := tc.CancelOrder(context.Background(), map[string]any{"order_id": id}); err == nil {
		t.Fatal("want ownership error")
	}
}

// ── 策略：部署/启动/停止/删除 ──

func TestDeployAndDeleteStrategy(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{}, nil)
	cfg := `{"strategy_type":"sma_cross","symbol":"BTCUSDT","timeframe":"1h"}`
	out, err := tc.DeployStrategy(context.Background(), map[string]any{"config_json": cfg})
	if err != nil {
		t.Fatalf("DeployStrategy: %v", err)
	}
	id := out.(map[string]any)["id"].(string)
	if id == "" {
		t.Fatal("deploy returned empty id")
	}
	if store.GetStrategyConfig(id) == nil {
		t.Fatal("config not persisted to store")
	}
	defer func() {
		store.DeleteStrategyConfig(id)
		store.PersistStrategyConfigs()
	}()

	// 非法 JSON
	if _, err := tc.DeployStrategy(context.Background(), map[string]any{"config_json": "{bad"}); err == nil {
		t.Fatal("want error for invalid JSON")
	}
	// 缺 strategy_type
	if _, err := tc.DeployStrategy(context.Background(), map[string]any{"config_json": `{"symbol":"BTCUSDT"}`}); err == nil {
		t.Fatal("want error for missing strategy_type")
	}

	// 删除
	if _, err := tc.DeleteStrategy(context.Background(), map[string]any{"name": id}); err != nil {
		t.Fatalf("DeleteStrategy: %v", err)
	}
	if store.GetStrategyConfig(id) != nil {
		t.Fatal("config still present after delete")
	}
}

func TestStartStopStrategyLifecycle(t *testing.T) {
	strategy.RegisterStrategyFactory("test_agent_strat", func() strategy.Strategy {
		return &testStrategy{name: "test_agent_strat", symbol: "BTCUSDT"}
	})

	tc := newTestContext(nil, &mockMatcher{}, nil)
	cfg := `{"strategy_type":"test_agent_strat","symbol":"BTCUSDT"}`
	deployed, err := tc.DeployStrategy(context.Background(), map[string]any{"config_json": cfg})
	if err != nil {
		t.Fatalf("DeployStrategy: %v", err)
	}
	id := deployed.(map[string]any)["id"].(string)
	defer func() {
		tc.DeleteStrategy(context.Background(), map[string]any{"name": id})
	}()

	started, err := tc.StartStrategy(context.Background(), map[string]any{"name": id})
	if err != nil {
		t.Fatalf("StartStrategy: %v", err)
	}
	if started.(map[string]any)["status"] != "started" {
		t.Errorf("start status: %+v", started)
	}
	if store.GetStrategyConfig(id)["status"] != "running" {
		t.Errorf("config status not updated: %+v", store.GetStrategyConfig(id))
	}

	stopped, err := tc.StopStrategy(context.Background(), map[string]any{"name": id})
	if err != nil {
		t.Fatalf("StopStrategy: %v", err)
	}
	if stopped.(map[string]any)["status"] != "stopped" {
		t.Errorf("stop status: %+v", stopped)
	}
}

func TestStartStrategyUnknownType(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{}, nil)
	cfg := `{"strategy_type":"no_such_strat_ever"}`
	deployed, err := tc.DeployStrategy(context.Background(), map[string]any{"config_json": cfg})
	if err != nil {
		t.Fatalf("DeployStrategy: %v", err)
	}
	id := deployed.(map[string]any)["id"].(string)
	defer func() {
		store.DeleteStrategyConfig(id)
		store.PersistStrategyConfigs()
	}()
	if _, err := tc.StartStrategy(context.Background(), map[string]any{"name": id}); err == nil {
		t.Fatal("want error for unknown strategy type")
	}
}

func TestStartStrategyNotFound(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{}, nil)
	if _, err := tc.StartStrategy(context.Background(), map[string]any{"name": "ghost"}); err == nil {
		t.Fatal("want error for unknown strategy")
	}
}

// ── 回测 ──

func makeSyntheticKlines(n int) []map[string]any {
	klines := make([]map[string]any, 0, n)
	price := 100.0
	base := time.Now().UnixMilli() - int64(n)*3600000
	for i := 0; i < n; i++ {
		// 先跌后涨再回落：让 fast/slow SMA 先死叉再金叉再死叉，
		// sma_cross 至少完成一次开平仓。
		switch {
		case i < n/3:
			price *= 0.999
		case i < 2*n/3:
			price *= 1.002
		default:
			price *= 0.998
		}
		klines = append(klines, map[string]any{
			"timestamp": base + int64(i)*3600000,
			"open":      price / 1.0005, "high": price * 1.001,
			"low": price / 1.001, "close": price,
			"volume": 1000.0,
		})
	}
	return klines
}

func TestRunBacktestRealMetrics(t *testing.T) {
	tc := newTestContext(&mockMarket{klines: makeSyntheticKlines(300)}, &mockMatcher{}, nil)
	out, err := tc.RunBacktest(context.Background(), map[string]any{
		"strategy_name": "sma_cross", "symbol": "BTCUSDT", "interval": "1h",
		"initial_balance": 10000.0,
	})
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}
	m := out.(map[string]any)
	if m["total_trades"].(int) < 1 {
		t.Errorf("expected trades on trending data, got %+v", m["total_trades"])
	}
	if m["final_equity"].(float64) <= 0 {
		t.Errorf("final_equity: got %v", m["final_equity"])
	}
	if math.IsNaN(m["sharpe_ratio"].(float64)) {
		t.Error("sharpe_ratio is NaN")
	}
	if m["id"] == "" {
		t.Error("backtest id missing (not persisted?)")
	}

	// list_backtests 应能读到刚落库的记录
	list, err := tc.ListBacktests(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListBacktests: %v", err)
	}
	found := false
	for _, bt := range list.(map[string]any)["backtests"].([]map[string]any) {
		if bt["id"] == m["id"] {
			found = true
			if bt["report"] == nil {
				t.Error("report missing from list output")
			}
		}
	}
	if !found {
		t.Error("run backtest record not in list_backtests")
	}
}

func TestRunBacktestValidation(t *testing.T) {
	tc := newTestContext(&mockMarket{}, &mockMatcher{}, nil)
	// 未知策略
	if _, err := tc.RunBacktest(context.Background(), map[string]any{
		"strategy_name": "nope", "symbol": "BTCUSDT",
	}); err == nil {
		t.Fatal("want error for unknown strategy")
	}
	// K 线不足
	if _, err := tc.RunBacktest(context.Background(), map[string]any{
		"strategy_name": "sma_cross", "symbol": "BTCUSDT",
	}); err == nil {
		t.Fatal("want error for insufficient klines")
	}
}

func TestGetStatsShape(t *testing.T) {
	tc := newTestContext(&mockMarket{klines: makeSyntheticKlines(300)}, &mockMatcher{}, &mockPortfolio{equity: 100000, pnl: 42.5})
	if _, err := tc.RunBacktest(context.Background(), map[string]any{
		"strategy_name": "sma_cross", "symbol": "BTCUSDT",
	}); err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}
	out, err := tc.GetStats(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	m := out.(map[string]any)
	for _, k := range []string{"total_orders", "total_trades", "total_pnl", "sharpe_ratio", "win_rate", "max_drawdown"} {
		if _, ok := m[k]; !ok {
			t.Errorf("stats missing key %q", k)
		}
	}
	if m["backtests_completed"].(int) < 1 {
		t.Errorf("backtests_completed: got %v", m["backtests_completed"])
	}
	if m["total_pnl"] != 42.5 {
		t.Errorf("total_pnl: got %v", m["total_pnl"])
	}
}

// ── 策略列表 ──

func TestListStrategiesIncludesDeployed(t *testing.T) {
	tc := newTestContext(nil, &mockMatcher{}, nil)
	cfg := `{"strategy_type":"sma_cross","symbol":"ETHUSDT"}`
	deployed, err := tc.DeployStrategy(context.Background(), map[string]any{"config_json": cfg})
	if err != nil {
		t.Fatalf("DeployStrategy: %v", err)
	}
	id := deployed.(map[string]any)["id"].(string)
	defer func() {
		store.DeleteStrategyConfig(id)
		store.PersistStrategyConfigs()
	}()

	out, err := tc.ListStrategies(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListStrategies: %v", err)
	}
	m := out.(map[string]any)
	found := false
	for _, s := range m["strategies"].([]map[string]any) {
		if s["id"] == id {
			found = true
			if s["strategy_type"] != "sma_cross" {
				t.Errorf("strategy_type: got %v", s["strategy_type"])
			}
		}
	}
	if !found {
		t.Error("deployed strategy not listed")
	}
	if _, ok := m["bots"].([]map[string]any); !ok {
		t.Error("bots missing in list output")
	}
}

// ── Token scope 过滤 ──

func TestFilterToolsByScope(t *testing.T) {
	tm := &TokenManager{}
	tools := AllTools()

	readOnly := tm.FilterTools(tools, []string{"R"})
	if len(readOnly) != 8 { // get_market_data/get_klines/get_orders/get_positions/get_balance/list_strategies/list_markets/get_stats
		t.Errorf("read-only tools: want 8, got %d", len(readOnly))
	}
	for _, tl := range readOnly {
		if tl.Scope == ScopeWrite || tl.Scope == ScopeBacktest {
			t.Errorf("write/backtest tool %q leaked into read-only set", tl.Name)
		}
	}

	writeTools := tm.FilterTools(tools, []string{"R", "W"})
	hasWrite := false
	for _, tl := range writeTools {
		if tl.Scope == ScopeWrite {
			hasWrite = true
		}
		if tl.Scope == ScopeBacktest {
			t.Errorf("backtest tool %q leaked into R+W set", tl.Name)
		}
	}
	if !hasWrite {
		t.Error("write tools missing with R+W scopes")
	}

	// 全 scope 拿满 16 个
	all := tm.FilterTools(tools, []string{"R", "W", "B", "N", "C", "T"})
	if len(all) != 16 {
		t.Errorf("all scopes: want 16 tools, got %d", len(all))
	}

	// 兼容单词式 scope（历史 token 存过 "read"/"trade"）
	aliased := tm.FilterTools(tools, []string{"read", "write"})
	if len(aliased) != len(writeTools) {
		t.Errorf("alias scopes: got %d, want %d", len(aliased), len(writeTools))
	}
}

// ── MCP 协议层 ──

// TestMCPProtocolEndToEnd 走 JSON-RPC 层（handleRequest + 写出），
// 验证 tools/list 与 tools/call 的完整报文。
func TestMCPProtocolEndToEnd(t *testing.T) {
	tc := newTestContext(&mockMarket{
		ticker: map[string]any{"lastPrice": "100.0"},
		bid:    99.9, ask: 100.1,
	}, &mockMatcher{}, &mockPortfolio{equity: 100000})
	server := NewMCPServer("test", "1.0.0", tc)

	var buf bytes.Buffer
	server.writer = &buf

	server.handleRequest(MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	server.handleRequest(MCPRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"})

	callParams, _ := json.Marshal(map[string]any{
		"name":      "get_balance",
		"arguments": map[string]any{},
	})
	server.handleRequest(MCPRequest{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: callParams})

	// 未知方法 / 未知工具 / 未配置上下文
	server.handleRequest(MCPRequest{JSONRPC: "2.0", ID: 4, Method: "bogus/method"})
	server.handleRequest(MCPRequest{JSONRPC: "2.0", ID: 5, Method: "tools/call",
		Params: json.RawMessage(`{"name":"nope","arguments":{}}`)})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 responses, got %d: %s", len(lines), buf.String())
	}

	var initResp, listResp, callResp, errResp, unknownToolResp map[string]any
	for i, dst := range []any{&initResp, &listResp, &callResp, &errResp, &unknownToolResp} {
		if err := json.Unmarshal([]byte(lines[i]), dst); err != nil {
			t.Fatalf("response %d not valid JSON: %v", i, err)
		}
	}
	if errObj, ok := initResp["error"]; ok && errObj != nil {
		t.Fatalf("initialize failed: %v", errObj)
	}
	tools := listResp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 16 {
		t.Errorf("tools/list: want 16, got %d", len(tools))
	}
	content := callResp["result"].(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "100000") {
		t.Errorf("tools/call get_balance text missing equity: %s", text)
	}
	if errObj := errResp["error"].(map[string]any); errObj["code"].(float64) != -32601 {
		t.Errorf("unknown method error code: %v", errObj)
	}
	if errObj := unknownToolResp["error"].(map[string]any); errObj["code"].(float64) != -32602 {
		t.Errorf("unknown tool error code: %v", errObj)
	}
}

func TestMCPServerToolsListAndCall(t *testing.T) {
	tc := newTestContext(&mockMarket{
		ticker: map[string]any{"lastPrice": "100.0"},
		bid:    99.9, ask: 100.1,
	}, &mockMatcher{}, &mockPortfolio{equity: 100000})
	server := NewMCPServer("test", "1.0.0", tc)

	if len(server.tools) != 16 {
		t.Fatalf("server registered %d tools, want 16", len(server.tools))
	}
	if _, ok := server.handlers["get_market_data"]; !ok {
		t.Fatal("get_market_data handler missing")
	}

	// 未注入上下文时报错而非 panic
	bare := NewMCPServer("test", "1.0.0", nil)
	if _, err := bare.handlers["get_balance"](context.Background(), nil); err == nil {
		t.Fatal("want error when tool context not configured")
	}

	// 真实调用走 ToolContext
	out, err := server.handlers["get_balance"](context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("get_balance handler: %v", err)
	}
	if out.(map[string]any)["total_equity"] != 100000.0 {
		t.Errorf("equity: got %+v", out)
	}
}

func TestMCPToolSchemaConversion(t *testing.T) {
	mcp := toolToMCP(AllTools()[0]) // get_market_data
	if mcp.Name != "get_market_data" {
		t.Errorf("name: %s", mcp.Name)
	}
	if _, ok := mcp.InputSchema.Properties["symbol"]; !ok {
		t.Error("symbol property missing")
	}
	if len(mcp.InputSchema.Required) != 1 || mcp.InputSchema.Required[0] != "symbol" {
		t.Errorf("required: %+v", mcp.InputSchema.Required)
	}
	data, err := json.Marshal(mcp)
	if err != nil {
		t.Fatalf("marshal MCPTool: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("MCPTool not serializable: %v", err)
	}
}

// ── 测试用策略 ──

type testStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
}

func (s *testStrategy) Name() string               { return s.name }
func (s *testStrategy) Symbol() string             { return s.symbol }
func (s *testStrategy) Params() map[string]any     { return map[string]any{} }
func (s *testStrategy) Start(map[string]any) error { s.running = true; return nil }
func (s *testStrategy) Stop() error                { s.running = false; return nil }
func (s *testStrategy) IsRunning() bool            { return s.running }
func (s *testStrategy) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *testStrategy) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *testStrategy) OnBar(model.Bar, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *testStrategy) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
