package reconcile

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupTestDB 初始化临时 SQLite（含全部迁移）。
func setupTestDB(t *testing.T) *store.ReconcileRepo {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(dir, "reconcile_test.db"))
	t.Setenv("SECRET_KEY", "reconcile-test-secret")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { store.CloseDB() })
	return store.NewReconcileRepo()
}

// fakePositionsAdapter 实现 PositionQuerier。
type fakePositionsAdapter struct {
	positions []map[string]any
	err       error
}

func (f *fakePositionsAdapter) GetPositions() ([]map[string]any, error) {
	return f.positions, f.err
}

// fakeQuerierAdapter 实现成交恢复/资金费三个接口。
type fakeQuerierAdapter struct {
	trades   []AccountTradeLike
	status   OrderStatusInfo
	statusOK bool
	incomes  []FundingIncome
}

func (f *fakeQuerierAdapter) GetOrderTrades(symbol, orderID string) ([]AccountTradeLike, error) {
	return f.trades, nil
}

func (f *fakeQuerierAdapter) QueryOrderStatus(symbol, orderID string) (OrderStatusInfo, error) {
	if !f.statusOK {
		return OrderStatusInfo{}, fmt.Errorf("order %s not found", orderID)
	}
	return f.status, nil
}

func (f *fakeQuerierAdapter) GetFundingIncomes(symbol string, startMs int64, limit int) ([]FundingIncome, error) {
	return f.incomes, nil
}

func TestPositionReconcileDetectsDrift(t *testing.T) {
	repo := setupTestDB(t)

	// 本地持仓：BTCUSDT 1.0 多（交易所 1.5 → 漂移）；SOLUSDT 仅本地（交易所缺失）。
	posRepo := store.NewPositionRepo()
	if err := posRepo.Create(&store.PositionRecord{
		ID: "pos-btc", UserID: 7, Symbol: "BTCUSDT", Side: "LONG",
		Quantity: 1.0, AvgEntryPrice: 50000, Exchange: "binance", Status: "OPEN",
	}); err != nil {
		t.Fatalf("create position: %v", err)
	}
	if err := posRepo.Create(&store.PositionRecord{
		ID: "pos-sol", UserID: 7, Symbol: "SOLUSDT", Side: "LONG",
		Quantity: 3.0, AvgEntryPrice: 150, Exchange: "binance", Status: "OPEN",
	}); err != nil {
		t.Fatalf("create position: %v", err)
	}

	fake := &fakePositionsAdapter{positions: []map[string]any{
		{"symbol": "BTCUSDT", "positionAmt": 1.5, "entryPrice": 51000},
		{"symbol": "ETHUSDT", "positionAmt": -2.0, "entryPrice": 3000},
	}}
	rec := NewPositionReconciler(repo, func(name string) any {
		if name == "binance" {
			return fake
		}
		return nil
	}, nil, nil)

	msg, err := rec.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("run: %s", msg)

	diffs, err := repo.ListDiffs(0, "", "", "binance", 50, 0)
	if err != nil {
		t.Fatalf("list diffs: %v", err)
	}
	types := map[string]bool{}
	for _, d := range diffs {
		types[d.DiffType] = true
	}
	if !types["position_quantity"] {
		t.Fatalf("应检出数量漂移: %+v", diffs)
	}
	if !types["position_missing_local"] {
		t.Fatalf("应检出交易所持仓本地缺失: %+v", diffs)
	}
	if !types["position_missing_exchange"] {
		t.Fatalf("应检出站内持仓交易所缺失: %+v", diffs)
	}

	// 幂等：再跑一轮不产生新的 open 差异。
	if _, err := rec.Run(); err != nil {
		t.Fatalf("run2: %v", err)
	}
	diffs2, _ := repo.ListDiffs(0, "", "", "binance", 50, 0)
	if len(diffs2) != len(diffs) {
		t.Fatalf("重复运行不得产生新差异: %d vs %d", len(diffs2), len(diffs))
	}

	// 人工解决。
	resolved, err := repo.ResolveDiff(diffs[0].ID, "accept_exchange", "tester")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != "resolved" || resolved.Resolution != "accept_exchange" {
		t.Fatalf("resolve 结果异常: %+v", resolved)
	}
}

func TestPositionReconcileAutoFix(t *testing.T) {
	repo := setupTestDB(t)
	posRepo := store.NewPositionRepo()
	if err := posRepo.Create(&store.PositionRecord{
		ID: "pos-fix", UserID: 3, Symbol: "BTCUSDT", Side: "LONG",
		Quantity: 1.0, AvgEntryPrice: 50000, Exchange: "binance", Status: "OPEN",
	}); err != nil {
		t.Fatalf("create position: %v", err)
	}

	fake := &fakePositionsAdapter{positions: []map[string]any{
		{"symbol": "BTCUSDT", "positionAmt": 0.5, "entryPrice": 52000},
	}}
	r := NewPositionReconciler(repo, func(name string) any { return fake }, func() bool { return true }, nil)
	if _, err := r.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := posRepo.GetByID("pos-fix")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Quantity != 0.5 || got.AvgEntryPrice != 52000 {
		t.Fatalf("自动修正未生效: %+v", got)
	}
	audit, err := repo.ListAudit(10)
	if err != nil || len(audit) == 0 {
		t.Fatalf("自动修正必须留审计日志: %v %+v", err, audit)
	}
}

func TestFillRecoveryInsertsTradesIdempotently(t *testing.T) {
	repo := setupTestDB(t)
	orderRepo := store.GetOrderRepo()

	ord := &store.OrderRecord{
		ID: "ord-live-1", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 50000, Quantity: 1.0, Filled: 0.5, Status: "PARTIALLY_FILLED",
		Exchange: "binance", UserID: 9, ClientOID: "dca:bot-42", CreatedAt: 1, UpdatedAt: 1,
	}
	if err := orderRepo.Create(ord); err != nil {
		t.Fatalf("create order: %v", err)
	}

	fake := &fakeQuerierAdapter{
		trades: []AccountTradeLike{
			{TradeID: "1001", OrderID: "ord-live-1", Symbol: "BTCUSDT", Side: "BUY", Price: 50000, Quantity: 0.5, Timestamp: 111},
		},
		status:   OrderStatusInfo{Status: "FILLED", FilledQty: 1.0, AvgPrice: 50000},
		statusOK: true,
	}
	fr := NewFillRecoverer(repo, func(name string) any { return fake })

	applied := map[string][3]float64{}
	RegisterFillApplier("dca", func(botID, side string, qty, price float64) error {
		applied[botID] = [3]float64{qty, price, 1}
		return nil
	})

	msg, err := fr.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("run: %s", msg)

	trades, err := store.NewTradeRepo().List(map[string]any{"order_id": "ord-live-1"}, 10)
	if err != nil || len(trades) != 1 {
		t.Fatalf("应补录 1 笔成交: %v %+v", err, trades)
	}
	if trades[0].ExecPhase != "recovered" || trades[0].ID != "binance:1001" {
		t.Fatalf("成交记录异常: %+v", trades[0])
	}
	if applied["bot-42"][0] != 0.5 {
		t.Fatalf("bot 成交回填未触发: %+v", applied)
	}

	got, _ := orderRepo.GetByID("ord-live-1")
	if got.Status != "FILLED" || got.Filled != 1.0 || got.AvgFillPrice != 50000 {
		t.Fatalf("订单状态未推进: %+v", got)
	}

	// 幂等：第二轮不重复补成交、不重复回填。
	msg2, err := fr.Run()
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	delete(applied, "bot-42")
	// 注意：第二轮 trades 均已存在 → 无新成交 → 不再回填。
	trades2, _ := store.NewTradeRepo().List(map[string]any{"order_id": "ord-live-1"}, 10)
	if len(trades2) != 1 {
		t.Fatalf("重复运行不得重复补成交: %+v", trades2)
	}
	t.Logf("run2: %s", msg2)
}

func TestFundingReconcileReportsNewOnlyOnce(t *testing.T) {
	repo := setupTestDB(t)
	fake := &fakeQuerierAdapter{incomes: []FundingIncome{
		{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Asset: "USDT", Income: -1.25, Time: 1720000000000},
		{Symbol: "ETHUSDT", IncomeType: "FUNDING_FEE", Asset: "USDT", Income: 0.5, Time: 1720000001000},
	}}
	fr := NewFundingReconciler(repo, func(name string) any { return fake })

	if _, err := fr.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	diffs, _ := repo.ListDiffs(0, "", "funding", "binance", 50, 0)
	if len(diffs) != 2 {
		t.Fatalf("应有 2 条资金费差异: %+v", diffs)
	}

	// 第二轮：流水已镜像，不再报差异。
	if _, err := fr.Run(); err != nil {
		t.Fatalf("run2: %v", err)
	}
	diffs2, _ := repo.ListDiffs(0, "", "funding", "binance", 50, 0)
	if len(diffs2) != 2 {
		t.Fatalf("重复运行不得重复报资金费差异: %+v", diffs2)
	}
}

func TestDeviationMonitorSlippageAndStuck(t *testing.T) {
	repo := setupTestDB(t)
	orderRepo := store.GetOrderRepo()

	now := nowMilli()
	// 滑点单：限价 50000 成交 51000（2% > 0.5%）
	if err := orderRepo.Create(&store.OrderRecord{
		ID: "ord-slip", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 50000, Quantity: 1, Filled: 1, Status: "FILLED", AvgFillPrice: 51000,
		Exchange: "binance", UserID: 5, CreatedAt: now - 1000, UpdatedAt: now - 500,
	}); err != nil {
		t.Fatalf("create slip order: %v", err)
	}
	// 卡单：挂单超过超时时间未成交。
	if err := orderRepo.Create(&store.OrderRecord{
		ID: "ord-stuck", Symbol: "ETHUSDT", Side: "SELL", OrderType: "LIMIT",
		Price: 3000, Quantity: 2, Filled: 0, Status: "NEW",
		Exchange: "binance", UserID: 5, CreatedAt: now - 3600_000, UpdatedAt: now - 3600_000,
	}); err != nil {
		t.Fatalf("create stuck order: %v", err)
	}
	// paper 单不监控。
	if err := orderRepo.Create(&store.OrderRecord{
		ID: "ord-paper", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT",
		Price: 50000, Quantity: 1, Filled: 0, Status: "NEW",
		Exchange: "paper", UserID: 5, CreatedAt: now - 7200_000, UpdatedAt: now - 7200_000,
	}); err != nil {
		t.Fatalf("create paper order: %v", err)
	}

	cfg := &Config{SlippagePct: 0.5, StuckTimeout: 15 * time.Minute}
	m := NewDeviationMonitor(repo, func() *Config { return cfg })

	msg, err := m.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("run: %s", msg)

	devs, err := repo.ListDeviations(0, "", "", "binance", 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	kinds := map[string]bool{}
	for _, d := range devs {
		kinds[d.Kind] = true
	}
	if !kinds["slippage"] || !kinds["stuck"] {
		t.Fatalf("应同时检出滑点与卡单: %+v", devs)
	}
	// paper 订单不得产生偏差。
	for _, d := range devs {
		if d.OrderID == "ord-paper" {
			t.Fatalf("paper 单不得进入偏差监控: %+v", d)
		}
	}
	// risk_events 已写入。
	events, err := store.NewRiskEventRepo().List(map[string]any{}, 50)
	if err != nil || len(events) < 2 {
		t.Fatalf("偏差必须写 risk_events: %v %+v", err, events)
	}

	// 幂等：再跑一轮不重复。
	if _, err := m.Run(); err != nil {
		t.Fatalf("run2: %v", err)
	}
	devs2, _ := repo.ListDeviations(0, "", "", "binance", 50, 0)
	if len(devs2) != len(devs) {
		t.Fatalf("重复扫描不得重复落偏差: %d vs %d", len(devs2), len(devs))
	}
}

func TestServiceStartStopLifecycle(t *testing.T) {
	repo := setupTestDB(t)
	svc := NewService(repo, func(name string) any { return nil })
	svc.Start()
	if !svc.IsRunning() {
		t.Fatal("service should be running after Start")
	}
	svc.Stop()
	if svc.IsRunning() {
		t.Fatal("service should be stopped after Stop")
	}
	// 幂等停止。
	svc.Stop()

	status := svc.Status()
	if status["running"] != false {
		t.Fatalf("status.running 应为 false: %+v", status)
	}
	if _, ok := status["config"].(map[string]any); !ok {
		t.Fatalf("status 应含 config: %+v", status)
	}
}

func TestServiceConfigUpdate(t *testing.T) {
	repo := setupTestDB(t)
	svc := NewService(repo, func(name string) any { return nil })
	defer svc.Stop()

	cfg := svc.UpdateConfig(map[string]string{"slippage_pct": "1.5", "interval_sec": "30"})
	if cfg.SlippagePct != 1.5 || int64(cfg.Interval/time.Second) != 30 {
		t.Fatalf("配置更新未生效: %+v", cfg)
	}
	// 持久化：新 Service 实例也能读到设置表覆盖。
	svc2 := NewService(repo, func(name string) any { return nil })
	if svc2.CurrentConfig().SlippagePct != 1.5 {
		t.Fatalf("设置表覆盖未持久化: %+v", svc2.CurrentConfig())
	}
}
