package dca

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

// 测试用临时 sqlite（参照 grid 包 TestMain）：DB_PATH 指向临时目录，
// 绝不触碰 ./runtime/gateway.db；结束时关闭连接并删除整个目录。

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dca_test")
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

// fakeSource 是可控的假价格源（同 grid 测试惯例）。
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

func newTestRunner(src PriceSource, repo *store.DCARepo, exec Executor) *Runner {
	r := NewRunner(src, repo, exec)
	r.TickInterval = 5 * time.Millisecond
	return r
}

var botSeq int64

// newBotRecord 建一个测试机器人；ID 加序号后缀，支持 -count>1 重复运行。
func newBotRecord(t *testing.T, repo *store.DCARepo, id string, mutate func(*store.DCABotRecord)) *store.DCABotRecord {
	t.Helper()
	rec := &store.DCABotRecord{
		ID:              fmt.Sprintf("%s-%d", id, atomic.AddInt64(&botSeq, 1)),
		Name:            "test-dca",
		Symbol:          "BTCUSDT",
		Exchange:        "paper",
		QuoteAmount:     100,
		IntervalMinutes: 60,
		TakeProfitPct:   0.05,
		StopLossPct:     0.10,
		Status:          "stopped",
	}
	if mutate != nil {
		mutate(rec)
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create bot: %v", err)
	}
	return rec
}

func mustGetBot(t *testing.T, repo *store.DCARepo, id string) *store.DCABotRecord {
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

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-6
}

// TestRunnerStartBuysOnSchedule: 启动后按间隔定时买入并落库（首单立即）。
func TestRunnerStartBuysOnSchedule(t *testing.T) {
	repo := store.NewDCARepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec := newBotRecord(t, repo, "dca-schedule", func(r *store.DCABotRecord) {
		r.IntervalMinutes = 1
		r.MaxOrders = 2
	})
	if err := runner.StartBot(rec, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 首单立即成交落库。
	waitFor(t, 3*time.Second, func() bool {
		orders, _ := repo.GetOrders(rec.ID, 0)
		return len(orders) == 1
	}, "first scheduled buy persisted")
	if exec.buyCount() < 1 {
		t.Fatalf("executor should have been called for first buy")
	}

	// 1 分钟未到：不重复买入（喂多轮价确认 tick 在跑）。
	src.set(101)
	time.Sleep(80 * time.Millisecond)
	if exec.buyCount() != 1 {
		t.Fatalf("interval not elapsed: buys = %d, want 1", exec.buyCount())
	}

	// 缩短间隔到已流逝 → 第 2 单成交；max_orders=2 后停止。
	rec2 := mustGetBot(t, repo, rec.ID)
	rec2.IntervalMinutes = 0 // 间隔已流逝
	if err := repo.Update(rec2); err != nil {
		t.Fatalf("update interval: %v", err)
	}
	runner.StopBot(rec.ID, "stopped")
	if err := runner.StartBot(mustGetBot(t, repo, rec.ID), 101); err != nil {
		t.Fatalf("re-StartBot: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		b := mustGetBot(t, repo, rec.ID)
		return b.FilledOrders == 2
	}, "second buy after interval elapsed")

	// 达到 max_orders=2 且仍持仓 → 继续监控 TP/SL，不再买入。
	src.set(101)
	time.Sleep(80 * time.Millisecond)
	bot := mustGetBot(t, repo, rec.ID)
	if bot.FilledOrders != 2 {
		t.Fatalf("filled_orders = %d, want 2", bot.FilledOrders)
	}
	if exec.buyCount() != 2 {
		t.Fatalf("buys = %d, want exactly 2 (max_orders)", exec.buyCount())
	}

	// 成交明细：两笔买入，每笔固定 100U（quote_amount 固定金额定投）。
	orders, err := repo.GetOrders(rec.ID, 0)
	if err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(orders))
	}
	for _, o := range orders {
		if o.Side != "buy" {
			t.Fatalf("order side = %s, want buy", o.Side)
		}
		if !approx(o.QuoteQty, 100) {
			t.Fatalf("order quote = %v, want 100 (fixed quote amount)", o.QuoteQty)
		}
	}
	// 持仓均价 = 总投入/总数量 = 200/(1+100/101)，累计投入 200。
	if !approx(bot.TotalInvested, 200) {
		t.Fatalf("total_invested = %v, want 200", bot.TotalInvested)
	}
	wantAvg := 200 / (1 + 100.0/101)
	if !approx(bot.AvgPrice, wantAvg) {
		t.Fatalf("avg_price = %v, want %v", bot.AvgPrice, wantAvg)
	}
}

// TestRunnerTakeProfitSellsAll: 均价之上 TP 触发整体卖出，落库 realized_pnl，
// bot 进入 finished 并退出运行表。
func TestRunnerTakeProfitSellsAll(t *testing.T) {
	repo := store.NewDCARepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec := newBotRecord(t, repo, "dca-tp", func(r *store.DCABotRecord) {
		r.TakeProfitPct = 0.05
		r.MaxOrders = 1
	})
	if err := runner.StartBot(rec, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		b := mustGetBot(t, repo, rec.ID)
		return b.FilledOrders == 1 && b.BaseQty > 0
	}, "initial buy filled")

	// 涨过 TP：+5% → 整体卖出。
	src.set(106)
	waitFor(t, 3*time.Second, func() bool {
		b := mustGetBot(t, repo, rec.ID)
		return b.Status == "finished"
	}, "bot finished after TP sell")
	if runner.IsRunning(rec.ID) {
		t.Fatal("bot should leave running set after finish")
	}

	bot := mustGetBot(t, repo, rec.ID)
	if !approx(bot.RealizedPnL, 6) { // (106-100) * 1 qty
		t.Fatalf("realized_pnl = %v, want 6", bot.RealizedPnL)
	}
	if bot.BaseQty != 0 || bot.AvgPrice != 0 {
		t.Fatalf("position should be flat: qty=%v avg=%v", bot.BaseQty, bot.AvgPrice)
	}
	orders, _ := repo.GetOrders(rec.ID, 0)
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2 (buy+sell)", len(orders))
	}
	if orders[0].Side != "sell" || orders[0].Reason != "take_profit" {
		t.Fatalf("newest order should be take_profit sell, got %+v", orders[0])
	}
}

// TestRunnerStopLossSellsAll: 均价之下 SL 硬止损整体卖出 → finished。
func TestRunnerStopLossSellsAll(t *testing.T) {
	repo := store.NewDCARepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec := newBotRecord(t, repo, "dca-sl", func(r *store.DCABotRecord) {
		r.StopLossPct = 0.10
		r.TakeProfitPct = 0
		r.MaxOrders = 1
	})
	if err := runner.StartBot(rec, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		b := mustGetBot(t, repo, rec.ID)
		return b.FilledOrders == 1
	}, "initial buy filled")

	src.set(89) // -11% < -10% SL
	waitFor(t, 3*time.Second, func() bool {
		b := mustGetBot(t, repo, rec.ID)
		return b.Status == "finished" && b.RealizedPnL < 0
	}, "bot finished after SL sell")

	bot := mustGetBot(t, repo, rec.ID)
	if !approx(bot.RealizedPnL, -11) { // (89-100) * 1 qty
		t.Fatalf("realized_pnl = %v, want -11", bot.RealizedPnL)
	}
	orders, _ := repo.GetOrders(rec.ID, 0)
	if len(orders) != 2 || orders[0].Reason != "stop_loss" {
		t.Fatalf("orders mismatch: %+v", orders)
	}
}

// TestRunnerNoFillNoAdvance: executor 回报未成交（filled=0）时状态不推进。
func TestRunnerNoFillNoAdvance(t *testing.T) {
	repo := store.NewDCARepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	exec.buyFilled = false
	runner := newTestRunner(src.Price, repo, exec)

	rec := newBotRecord(t, repo, "dca-nofill", nil)
	if err := runner.StartBot(rec, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 多次 tick 都未成交：订单不落库、状态不推进，但会持续重试（下单意图保留）。
	time.Sleep(120 * time.Millisecond)
	orders, _ := repo.GetOrders(rec.ID, 0)
	if len(orders) != 0 {
		t.Fatalf("no fills expected, got %d orders", len(orders))
	}
	bot := mustGetBot(t, repo, rec.ID)
	if bot.FilledOrders != 0 || bot.TotalInvested != 0 || bot.BaseQty != 0 {
		t.Fatalf("state advanced without fill: %+v", bot)
	}
	if exec.buyCount() == 0 {
		t.Fatal("buy intent should have been attempted")
	}
}

// TestRunnerStopAndResume: StopBot 停止驱动；ResumeRunningBots 从库恢复续跑
// （恢复后按新预算继续定投，不重置已投金额）。
func TestRunnerStopAndResume(t *testing.T) {
	repo := store.NewDCARepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	// 预算 150：首单 100 后，再加 100 将超额 → 只有 1 单。
	rec := newBotRecord(t, repo, "dca-resume", func(r *store.DCABotRecord) {
		r.IntervalMinutes = 0 // 间隔即刻流逝
		r.PeriodBudget = 150
	})
	if err := runner.StartBot(rec, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		return mustGetBot(t, repo, rec.ID).FilledOrders >= 1
	}, "first buy")
	waitFor(t, 3*time.Second, func() bool {
		orders, _ := repo.GetOrders(rec.ID, 0)
		return len(orders) == 1 // 预算阻止第 2 单，状态稳定
	}, "budget blocks second buy")

	if err := runner.StopBot(rec.ID, "stopped"); err != nil {
		t.Fatalf("StopBot: %v", err)
	}
	filledBefore := mustGetBot(t, repo, rec.ID).FilledOrders

	// 放宽预算到 300 → 模拟进程重启后从库恢复并续投。
	bot := mustGetBot(t, repo, rec.ID)
	bot.PeriodBudget = 300
	if err := repo.Update(bot); err != nil {
		t.Fatalf("Update budget: %v", err)
	}
	if err := repo.UpdateStatus(rec.ID, "running"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	list, err := repo.List(map[string]any{"status": "running"}, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var mine []*store.DCABotRecord
	for _, b := range list {
		if b.ID == rec.ID {
			mine = append(mine, b)
		}
	}
	runner.ResumeRunningBots(mine)
	if !runner.IsRunning(rec.ID) {
		t.Fatal("bot not resumed")
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 恢复后基于已投 100 继续：预算 300 → 第 2 单成交。
	waitFor(t, 3*time.Second, func() bool {
		return mustGetBot(t, repo, rec.ID).FilledOrders > filledBefore
	}, "buys continue after resume")
}

// TestRunnerDupStartRejected: 重复启动同一机器人被拒绝。
func TestRunnerDupStartRejected(t *testing.T) {
	repo := store.NewDCARepo()
	src := &fakeSource{}
	src.set(100)
	exec := newFakeExecutor()
	runner := newTestRunner(src.Price, repo, exec)

	rec := newBotRecord(t, repo, "dca-dup", nil)
	if err := runner.StartBot(rec, 100); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")
	if err := runner.StartBot(rec, 100); err == nil {
		t.Fatal("second StartBot should fail")
	}
}
