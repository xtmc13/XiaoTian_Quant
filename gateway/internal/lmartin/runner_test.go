package lmartin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// 测试用临时 sqlite（参照 dca/grid 包 TestMain）。

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lmartin_test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("DB_PATH", filepath.Join(dir, "gateway.db"))
	_ = os.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	if err := store.InitDB(); err != nil {
		panic(err)
	}
	code := m.Run()
	store.CloseDB()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type fakeSource struct {
	mu sync.RWMutex
	v  float64
}

func (f *fakeSource) Price(symbol string) float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.v
}

func (f *fakeSource) set(v float64) {
	f.mu.Lock()
	f.v = v
	f.mu.Unlock()
}

// fakeExecutor 记录下单并即时全成成交（价格取参考价）。
type fakeExecutor struct {
	mu         sync.Mutex
	buyCalls   []fakeBuy
	sellCalls  []fakeSell
	buyFilled  bool
	sellFilled bool
}

type fakeBuy struct {
	symbol      string
	exchange    string
	userID      int64
	quoteAmount float64
	refPrice    float64
}

type fakeSell struct {
	symbol   string
	exchange string
	userID   int64
	baseQty  float64
	refPrice float64
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{buyFilled: true, sellFilled: true}
}

func (f *fakeExecutor) BuySpot(_ string, symbol, exchange string, userID int64, quoteAmount, refPrice float64) (float64, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buyCalls = append(f.buyCalls, fakeBuy{symbol, exchange, userID, quoteAmount, refPrice})
	if !f.buyFilled || refPrice <= 0 {
		return 0, 0, nil
	}
	return quoteAmount / refPrice, refPrice, nil
}

func (f *fakeExecutor) SellSpot(_ string, symbol, exchange string, userID int64, baseQty, refPrice float64) (float64, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sellCalls = append(f.sellCalls, fakeSell{symbol, exchange, userID, baseQty, refPrice})
	if !f.sellFilled || refPrice <= 0 {
		return 0, 0, nil
	}
	return baseQty, refPrice, nil
}

func (f *fakeExecutor) buyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.buyCalls)
}

func (f *fakeExecutor) sellCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sellCalls)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

func newTestRunner(src PriceSource, repo *store.LayeredMartinRepo, exec Executor) *Runner {
	r := NewRunner(src, repo, exec)
	r.TickInterval = 5 * time.Millisecond
	return r
}

var botSeq int64

// newBotWithGroups 建一个测试机器人 + 两个分组；ID 加序号后缀支持 -count>1。
func newBotWithGroups(t *testing.T, repo *store.LayeredMartinRepo, id string, mutate func(*store.LayeredMartinBotRecord)) (*store.LayeredMartinBotRecord, []*store.LayeredMartinGroupRecord) {
	t.Helper()
	rec := &store.LayeredMartinBotRecord{
		ID:                fmt.Sprintf("%s-%d", id, atomic.AddInt64(&botSeq, 1)),
		Name:              "test-lmartin",
		Symbol:            "BTCUSDT",
		Exchange:          "paper",
		PriceDeviationPct: 0.03,
		TakeProfitPct:     0.05,
		StopLossPct:       0.10,
		Status:            "stopped",
	}
	if mutate != nil {
		mutate(rec)
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create bot: %v", err)
	}
	groups := []*store.LayeredMartinGroupRecord{
		{QuoteAmount: 100, Multiplier: 2, MaxLayers: 3, BudgetCap: 0},
		{QuoteAmount: 50, Multiplier: 2, MaxLayers: 2, BudgetCap: 180},
	}
	if err := repo.ReplaceGroups(rec.ID, groups); err != nil {
		t.Fatalf("replace groups: %v", err)
	}
	return rec, groups
}

func mustGetBot(t *testing.T, repo *store.LayeredMartinRepo, id string) *store.LayeredMartinBotRecord {
	t.Helper()
	rec, err := repo.GetByID(id)
	if err != nil {
		t.Fatalf("get bot: %v", err)
	}
	if rec == nil {
		t.Fatalf("bot %s not found", id)
	}
	return rec
}

func mustGetGroups(t *testing.T, repo *store.LayeredMartinRepo, id string) []*store.LayeredMartinGroupRecord {
	t.Helper()
	groups, err := repo.GetGroups(id)
	if err != nil {
		t.Fatalf("get groups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("bot %s has no groups", id)
	}
	return groups
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-6
}

// TestRunnerStartBuysFirstLayerAllGroups: 启动后所有组立即开首层并落库。
func TestRunnerStartBuysFirstLayerAllGroups(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec, _ := newBotWithGroups(t, repo, "lm-first", nil)
	if err := runner.StartBot(rec, mustGetGroups(t, repo, rec.ID), 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 两组首层各成交一笔。
	waitFor(t, 3*time.Second, func() bool {
		orders, _ := repo.GetOrders(rec.ID, 0)
		return len(orders) == 2
	}, "first-layer buys for both groups persisted")
	if exec.buyCount() != 2 {
		t.Fatalf("buys = %d, want 2", exec.buyCount())
	}

	// 组状态落库（persist 在 insert 之后单行执行，轮询等其落库）。
	waitFor(t, 3*time.Second, func() bool {
		groups := mustGetGroups(t, repo, rec.ID)
		return groups[0].Layer == 1 && groups[1].Layer == 1
	}, "group states persisted")
	groups := mustGetGroups(t, repo, rec.ID)
	if !approx(groups[0].TotalInvested, 100) || groups[0].BaseQty <= 0 {
		t.Fatalf("group 0 state mismatch: %+v", groups[0])
	}
	if !approx(groups[1].TotalInvested, 50) {
		t.Fatalf("group 1 state mismatch: %+v", groups[1])
	}

	// 主表 running。
	if got := mustGetBot(t, repo, rec.ID); got.Status != "running" {
		t.Fatalf("status = %q, want running", got.Status)
	}
}

// TestRunnerLayerAddOnDrop: 价格下跌触发第 2 层加仓（倍投金额），层数推进。
func TestRunnerLayerAddOnDrop(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec, _ := newBotWithGroups(t, repo, "lm-add", nil)
	if err := runner.StartBot(rec, mustGetGroups(t, repo, rec.ID), 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	waitFor(t, 3*time.Second, func() bool {
		return exec.buyCount() == 2
	}, "first layers filled")

	// 组 1 预算 180：第 2 层 100U → 50+100=150 ≤ 180 允许；组 0 第 2 层 200U 无预算限制。
	src.set(97) // -3%：触发位到达
	waitFor(t, 3*time.Second, func() bool {
		groups := mustGetGroups(t, repo, rec.ID)
		return groups[0].Layer == 2 && groups[1].Layer == 2
	}, "layer 2 entries on -3% drop")

	groups := mustGetGroups(t, repo, rec.ID)
	if !approx(groups[0].TotalInvested, 300) { // 100 + 200
		t.Fatalf("group 0 invested = %v, want 300", groups[0].TotalInvested)
	}
	if !approx(groups[1].TotalInvested, 150) { // 50 + 100
		t.Fatalf("group 1 invested = %v, want 150", groups[1].TotalInvested)
	}

	// 继续深跌至 -6%（触发位 94）：组 0 允许第 3 层（400U）；组 1 层数封顶且预算将超限。
	src.set(94)
	waitFor(t, 3*time.Second, func() bool {
		groups := mustGetGroups(t, repo, rec.ID)
		return groups[0].Layer == 3
	}, "group 0 layer 3 on deeper drop")
	// 组 1 停在 2 层（max_layers=2）。
	waitFor(t, 3*time.Second, func() bool {
		return mustGetGroups(t, repo, rec.ID)[1].Layer == 2
	}, "group 1 stays at layer cap")
}

// TestRunnerBudgetCapBlocksAdd: 组预算硬限阻止加仓（组 1：150 已投 + 第 3 层 200 > 180）。
func TestRunnerBudgetCapBlocksAdd(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec, groups := newBotWithGroups(t, repo, "lm-budget", nil)
	// 组 1 改为 max_layers=3：层数不挡，只有预算挡（50+100+200=350>180）。
	groups[1].MaxLayers = 3
	if err := repo.ReplaceGroups(rec.ID, groups); err != nil {
		t.Fatalf("replace groups: %v", err)
	}
	if err := runner.StartBot(rec, mustGetGroups(t, repo, rec.ID), 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	waitFor(t, 3*time.Second, func() bool {
		return exec.buyCount() == 2
	}, "first layers filled")
	src.set(97)
	waitFor(t, 3*time.Second, func() bool {
		return mustGetGroups(t, repo, rec.ID)[0].Layer == 2
	}, "group 0 layer 2")
	src.set(94)
	waitFor(t, 3*time.Second, func() bool {
		return mustGetGroups(t, repo, rec.ID)[0].Layer == 3
	}, "group 0 layer 3")
	// 组 1 预算 180：第 3 层 200U 被拒，停在 2 层。
	waitFor(t, 3*time.Second, func() bool {
		gs := mustGetGroups(t, repo, rec.ID)
		return gs[1].Layer == 2 && approx(gs[1].TotalInvested, 150)
	}, "group 1 budget-capped at layer 2")
}

// TestRunnerTakeProfitExitsGroup: 组整体止盈卖出 → 组 finished；
// 两组全部退出后 bot finished 并离开运行表。
func TestRunnerTakeProfitExitsGroup(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec, groups := newBotWithGroups(t, repo, "lm-tp", func(r *store.LayeredMartinBotRecord) {
		r.TakeProfitPct = 0.02
	})
	// 两组都只允许 1 层：首层后不再加仓，只等 TP/SL。
	groups[0].MaxLayers = 1
	groups[1].MaxLayers = 1
	if err := repo.ReplaceGroups(rec.ID, groups); err != nil {
		t.Fatalf("replace groups: %v", err)
	}
	if err := runner.StartBot(rec, mustGetGroups(t, repo, rec.ID), 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		return exec.buyCount() == 2
	}, "first layers filled")

	// +2% 触发两组整体止盈。
	src.set(102)
	waitFor(t, 3*time.Second, func() bool {
		return mustGetBot(t, repo, rec.ID).Status == "finished"
	}, "bot finished after all groups TP")
	if runner.IsRunning(rec.ID) {
		t.Fatal("bot should leave running set after finish")
	}

	gs := mustGetGroups(t, repo, rec.ID)
	for i, g := range gs {
		if g.Status != "finished" || g.BaseQty != 0 {
			t.Fatalf("group %d should be finished & flat: %+v", i, g)
		}
	}
	bot := mustGetBot(t, repo, rec.ID)
	// 组 0：(102-100)*1 = 2；组 1：(102-100)*0.5 = 1 → 合计 3。
	if !approx(bot.RealizedPnL, 3) {
		t.Fatalf("realized_pnl = %v, want 3", bot.RealizedPnL)
	}
	orders, _ := repo.GetOrders(rec.ID, 0)
	if len(orders) != 4 { // 2 buys + 2 sells
		t.Fatalf("orders = %d, want 4", len(orders))
	}
}

// TestRunnerNoFillNoAdvance: executor 回报未成交时不推进层数、不落单。
func TestRunnerNoFillNoAdvance(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	exec.buyFilled = false
	runner := newTestRunner(src.Price, repo, exec)

	rec, _ := newBotWithGroups(t, repo, "lm-nofill", nil)
	if err := runner.StartBot(rec, mustGetGroups(t, repo, rec.ID), 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 多次 tick 未成交：无订单落库、组停在 0 层，但买入意图持续尝试。
	time.Sleep(120 * time.Millisecond)
	orders, _ := repo.GetOrders(rec.ID, 0)
	if len(orders) != 0 {
		t.Fatalf("no fills expected, got %d orders", len(orders))
	}
	groups := mustGetGroups(t, repo, rec.ID)
	for i, g := range groups {
		if g.Layer != 0 || g.TotalInvested != 0 {
			t.Fatalf("group %d advanced without fill: %+v", i, g)
		}
	}
	if exec.buyCount() == 0 {
		t.Fatal("buy intent should have been attempted")
	}
}

// TestRunnerStopAndResume: StopBot 停止驱动；ResumeRunningBots 按落库组状态恢复续跑。
func TestRunnerStopAndResume(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec, _ := newBotWithGroups(t, repo, "lm-resume", nil)
	if err := runner.StartBot(rec, mustGetGroups(t, repo, rec.ID), 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		return exec.buyCount() == 2
	}, "first layers filled")

	if err := runner.StopBot(rec.ID, "stopped"); err != nil {
		t.Fatalf("StopBot: %v", err)
	}
	layerBefore := mustGetGroups(t, repo, rec.ID)[0].Layer

	// 模拟进程重启后从库恢复（深跌后的价格 → 恢复后立即触发第 2 层）。
	if err := repo.UpdateStatus(rec.ID, "running"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	src.set(97)
	list, err := repo.List(map[string]any{"status": "running"}, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var mine []*store.LayeredMartinBotRecord
	for _, b := range list {
		if b.ID == rec.ID {
			mine = append(mine, b)
		}
	}
	runner.ResumeRunningBots(mine, func(botID string) ([]*store.LayeredMartinGroupRecord, error) {
		return repo.GetGroups(botID)
	})
	if !runner.IsRunning(rec.ID) {
		t.Fatal("bot not resumed")
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 恢复后组 1 层数不回退（仍是 1），组 0 在深跌价下补第 2 层。
	waitFor(t, 3*time.Second, func() bool {
		groups := mustGetGroups(t, repo, rec.ID)
		return groups[0].Layer == layerBefore+1
	}, "layer advances from restored state after resume")
}

// TestRunnerDupStartRejected: 重复启动同一机器人被拒绝。
func TestRunnerDupStartRejected(t *testing.T) {
	repo := store.NewLayeredMartinRepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec, groups := newBotWithGroups(t, repo, "lm-dup", nil)
	if err := runner.StartBot(rec, groups, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")
	if err := runner.StartBot(rec, groups, 100); err == nil {
		t.Fatal("second StartBot should fail")
	}
}
