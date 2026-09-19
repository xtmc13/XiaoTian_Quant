package grid

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// 测试用临时 sqlite（参照 handler 包 TestMain）：DB_PATH 指向临时目录，
// 绝不触碰 ./runtime/gateway.db；结束时关闭连接并删除整个目录。

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "grid_test")
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

// fakeSource 是可控的假价格源：测试用 set 逐 tick 喂价，Runner 每轮
// tick 读取当前值；0 模拟 WS 断连无有效行情。
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

func newTestRunner(src PriceSource, repo *store.GridRepo) *Runner {
	return newTestRunnerWithExec(src, repo, nil)
}

func newTestRunnerWithExec(src PriceSource, repo *store.GridRepo, exec ContractExecutor) *Runner {
	r := NewRunner(src, repo, exec)
	r.TickInterval = 5 * time.Millisecond
	r.SnapshotInterval = 25 * time.Millisecond
	return r
}

// longLegState 从持久化 state_json 解析 long 腿状态（BotEngine 双腿包装格式）。
func longLegState(t *testing.T, stateJSON string) map[string]any {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		t.Fatalf("state_json invalid: %v", err)
	}
	legs, ok := state["legs"].(map[string]any)
	if !ok {
		t.Fatalf("state missing legs wrapper: %v", state)
	}
	leg, ok := legs["long"].(map[string]any)
	if !ok {
		t.Fatalf("state missing long leg: %v", legs)
	}
	return leg
}

var botSeq int64

// newBotRecord 建一个测试机器人；ID 加序号后缀，支持 -count>1 重复运行。
func newBotRecord(t *testing.T, repo *store.GridRepo, id string) *store.GridBotRecord {
	t.Helper()
	rec := &store.GridBotRecord{
		ID:         fmt.Sprintf("%s-%d", id, atomic.AddInt64(&botSeq, 1)),
		Name:       "test-bot",
		Symbol:     "BTCUSDT",
		LowerPrice: testLower,
		UpperPrice: testUpper,
		GridCount:  testGrids,
		Investment: testInvest,
		FeeRate:    testFee,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create bot: %v", err)
	}
	return rec
}

func mustGetBot(t *testing.T, repo *store.GridRepo, id string) *store.GridBotRecord {
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

// feedPrices 依次喂价并给 ticker 留出驱动窗口。
func feedPrices(src *fakeSource, prices ...float64) {
	for _, p := range prices {
		src.set(p)
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunnerStartBotFillsAndPersists: 启动后喂单边上涨序列 → 成交落库、
// 主表累计字段与 state_json 更新、快照表有记录。
func TestRunnerStartBotFillsAndPersists(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(testStartPrice)
	runner := newTestRunner(src.Price, repo)

	rec := newBotRecord(t, repo, "bot-fill")
	if err := runner.StartBot(rec, testStartPrice); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	got := mustGetBot(t, repo, rec.ID)
	if got.Status != "running" || got.StartedAt == 0 {
		t.Fatalf("status=%q started_at=%d, want running/>0", got.Status, got.StartedAt)
	}

	feedPrices(src, 106, 107, 108, 109, 110)

	// 启动价 105 上方 106..110 五档 SELL 各成交一次。
	waitFor(t, 3*time.Second, func() bool {
		trades, _ := repo.GetTrades(rec.ID, 0)
		return len(trades) == 5
	}, "5 grid trades persisted")
	trades, err := repo.GetTrades(rec.ID, 0)
	if err != nil {
		t.Fatalf("GetTrades: %v", err)
	}
	levels := map[int]bool{}
	for _, tr := range trades {
		if tr.Side != "SELL" {
			t.Errorf("trade side = %s, want SELL", tr.Side)
		}
		levels[tr.Level] = true
	}
	for lv := 6; lv <= 10; lv++ {
		if !levels[lv] {
			t.Errorf("missing SELL fill at level %d", lv)
		}
	}

	// 主表累计字段。SELL@i 成交价为档位价 100+i。
	wantPnL := 0.0
	for lv := 6; lv <= 10; lv++ {
		gross := basePerSlot() * (100 + float64(lv))
		wantPnL += gross - gross*testFee - testInvest/testGrids
	}
	got = mustGetBot(t, repo, rec.ID)
	if got.TotalTrades != 5 {
		t.Errorf("total_trades = %d, want 5", got.TotalTrades)
	}
	if !approx(got.RealizedPnL, wantPnL) {
		t.Errorf("realized_pnl = %v, want %v", got.RealizedPnL, wantPnL)
	}
	if !approx(got.BaseQty, 5*basePerSlot()) {
		t.Errorf("base_qty = %v, want %v", got.BaseQty, 5*basePerSlot())
	}
	if !approx(got.QuoteBalance, basePerSlot()*(1-testFee)*(106+107+108+109+110)) {
		t.Errorf("quote_balance = %v", got.QuoteBalance)
	}

	// state_json 是可恢复的完整状态（双腿包装格式，long 腿为唯一腿）：
	// 初始 BUY(100..104) 未成交仍在册，SELL 成交后在 105..109 重挂 BUY，
	// 共 10 个未成交挂单。
	state := longLegState(t, got.StateJSON)
	orders, ok := state["orders"].(map[string]any)
	if !ok || len(orders) != 10 {
		t.Errorf("state orders = %v, want 10 open orders", state["orders"])
	}
	for lv, raw := range orders {
		om, _ := raw.(map[string]any)
		if side := om["side"]; side != "BUY" {
			t.Errorf("open order at level %s side = %v, want BUY", lv, side)
		}
	}
	if tt, _ := state["total_trades"].(float64); int(tt) != 5 {
		t.Errorf("state total_trades = %v, want 5", state["total_trades"])
	}

	// 快照表有记录，且字段来自引擎实时状态。
	waitFor(t, 3*time.Second, func() bool {
		snaps, _ := repo.GetSnapshots(rec.ID, 0, 0)
		for _, s := range snaps {
			if s.OpenOrders == 10 {
				return true
			}
		}
		return false
	}, "snapshot with 10 open orders")
	snaps, err := repo.GetSnapshots(rec.ID, 0, 0)
	if err != nil {
		t.Fatalf("GetSnapshots: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("no snapshots persisted")
	}
	for _, s := range snaps {
		if s.Equity <= 0 || s.Price <= 0 || s.Ts == 0 {
			t.Errorf("bad snapshot: %+v", s)
		}
	}
}

// TestRunnerStopBotStopsFills: StopBot 后引擎不再被驱动、status 落库 stopped。
func TestRunnerStopBotStopsFills(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(testStartPrice)
	runner := newTestRunner(src.Price, repo)

	rec := newBotRecord(t, repo, "bot-stop")
	if err := runner.StartBot(rec, testStartPrice); err != nil {
		t.Fatalf("StartBot: %v", err)
	}

	feedPrices(src, 106, 107)
	waitFor(t, 3*time.Second, func() bool {
		trades, _ := repo.GetTrades(rec.ID, 0)
		return len(trades) == 2
	}, "2 trades before stop")

	if err := runner.StopBot(rec.ID, "stopped"); err != nil {
		t.Fatalf("StopBot: %v", err)
	}
	if runner.IsRunning(rec.ID) {
		t.Fatal("bot still running after StopBot")
	}

	got := mustGetBot(t, repo, rec.ID)
	if got.Status != "stopped" || got.StoppedAt == 0 {
		t.Errorf("status=%q stopped_at=%d, want stopped/>0", got.Status, got.StoppedAt)
	}
	state := longLegState(t, got.StateJSON)
	if tt, _ := state["total_trades"].(float64); int(tt) != 2 {
		t.Errorf("final state total_trades = %v, want 2", state["total_trades"])
	}

	// 停后继续大幅喂价：不再产生任何成交。
	feedPrices(src, 108, 109, 110, 104, 103, 102)
	trades, _ := repo.GetTrades(rec.ID, 0)
	if len(trades) != 2 {
		t.Errorf("trades after stop = %d, want 2", len(trades))
	}
}

// TestRunnerResumeRunningBots: 跑一段 → 停 → 改库 status=running →
// ResumeRunningBots 用 state_json 恢复 → 续喂价格成交继续累加、不重复建仓。
func TestRunnerResumeRunningBots(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(testStartPrice)
	runner := newTestRunner(src.Price, repo)

	rec := newBotRecord(t, repo, "bot-resume")
	if err := runner.StartBot(rec, testStartPrice); err != nil {
		t.Fatalf("StartBot: %v", err)
	}

	feedPrices(src, 106, 107)
	waitFor(t, 3*time.Second, func() bool {
		trades, _ := repo.GetTrades(rec.ID, 0)
		return len(trades) == 2
	}, "2 trades before stop")

	if err := runner.StopBot(rec.ID, "stopped"); err != nil {
		t.Fatalf("StopBot: %v", err)
	}
	baseBefore := mustGetBot(t, repo, rec.ID).BaseQty
	if approx(baseBefore, 0) {
		t.Fatalf("base_qty before resume = %v, want > 0", baseBefore)
	}

	// 模拟进程重启后从库恢复：status 改回 running，内存无运行实例。
	if err := repo.UpdateStatus(rec.ID, "running"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	list, err := repo.List(map[string]any{"status": "running"}, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 只恢复本测试的 bot：列表里可能还有其他测试/迭代遗留的 running 记录。
	var mine []*store.GridBotRecord
	for _, b := range list {
		if b.ID == rec.ID {
			mine = append(mine, b)
		}
	}
	runner.ResumeRunningBots(mine)
	if !runner.IsRunning(rec.ID) {
		t.Fatal("bot not resumed")
	}

	// 恢复后无价格变化：不得重复建仓或自成交。
	time.Sleep(60 * time.Millisecond)
	trades, _ := repo.GetTrades(rec.ID, 0)
	if len(trades) != 2 {
		t.Fatalf("trades after resume without price move = %d, want 2", len(trades))
	}
	if got := mustGetBot(t, repo, rec.ID).BaseQty; !approx(got, baseBefore) {
		t.Errorf("base_qty after resume = %v, want %v (no re-entry)", got, baseBefore)
	}

	// 续喂价格：基于恢复状态继续累加（SELL@108 一档）。
	feedPrices(src, 108)
	waitFor(t, 3*time.Second, func() bool {
		trades, _ := repo.GetTrades(rec.ID, 0)
		return len(trades) == 3
	}, "3rd trade after resume")

	got := mustGetBot(t, repo, rec.ID)
	if got.TotalTrades != 3 {
		t.Errorf("total_trades = %d, want 3", got.TotalTrades)
	}
	if !approx(got.BaseQty, baseBefore-basePerSlot()) {
		t.Errorf("base_qty = %v, want %v", got.BaseQty, baseBefore-basePerSlot())
	}
	state := longLegState(t, got.StateJSON)
	if tt, _ := state["total_trades"].(float64); int(tt) != 3 {
		t.Errorf("state total_trades = %v, want 3", state["total_trades"])
	}

	runner.StopBot(rec.ID, "stopped")
}

// TestRunnerZeroPriceSkips: 价格源返回 0 时引擎不被驱动、不 panic；
// 行情恢复后循环继续工作。
func TestRunnerZeroPriceSkips(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(0) // WS 断连：无有效行情
	runner := newTestRunner(src.Price, repo)

	rec := newBotRecord(t, repo, "bot-zerop")
	if err := runner.StartBot(rec, testStartPrice); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 持续喂 0：若干 tick 与快照周期后仍无成交、无快照、无 panic。
	time.Sleep(100 * time.Millisecond)
	trades, _ := repo.GetTrades(rec.ID, 0)
	if len(trades) != 0 {
		t.Errorf("trades with zero price = %d, want 0", len(trades))
	}
	snaps, _ := repo.GetSnapshots(rec.ID, 0, 0)
	if len(snaps) != 0 {
		t.Errorf("snapshots with zero price = %d, want 0", len(snaps))
	}
	got := mustGetBot(t, repo, rec.ID)
	if got.TotalTrades != 0 {
		t.Errorf("total_trades = %d, want 0", got.TotalTrades)
	}

	// 行情恢复：同一 Runner 继续正常驱动。
	feedPrices(src, 106)
	waitFor(t, 3*time.Second, func() bool {
		trades, _ := repo.GetTrades(rec.ID, 0)
		return len(trades) == 1
	}, "trade after price recovers")
}

// TestRunnerStartBotRejectsDup 防止重复启动同一机器人。
func TestRunnerStartBotRejectsDup(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(testStartPrice)
	runner := newTestRunner(src.Price, repo)

	rec := newBotRecord(t, repo, "bot-dup")
	if err := runner.StartBot(rec, testStartPrice); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	if err := runner.StartBot(rec, testStartPrice); err == nil {
		t.Fatal("second StartBot should fail")
	}
}

// TestRunnerResumeSkipsInvalidPrice: 恢复时行情源无报价 → 记录并跳过，
// 不创建运行实例（交由 RetryResume 下轮再试）。
func TestRunnerResumeSkipsInvalidPrice(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(0)
	runner := newTestRunner(src.Price, repo)

	rec := newBotRecord(t, repo, "bot-noprice")
	if err := repo.UpdateStatus(rec.ID, "running"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	rec.Status = "running"

	list, err := repo.List(map[string]any{"status": "running"}, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	runner.ResumeRunningBots(list)
	if runner.IsRunning(rec.ID) {
		t.Fatal("bot should be skipped when no live price")
	}
}

// fakeContractExecutor 记录合约下单调用（ContractExecutor 的测试实现）。
type fakeContractExecutor struct {
	mu     sync.Mutex
	calls  []contractCall
	filled float64
}

type contractCall struct {
	symbol       string
	side         string
	qty          float64
	price        float64
	leverage     float64
	marginMode   string
	positionSide string
}

func (f *fakeContractExecutor) PlaceContract(_ string, symbol, exchange string, userID int64, side string, qty, price, leverage float64, marginMode, positionSide string) (float64, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, contractCall{
		symbol: symbol, side: side, qty: qty, price: price,
		leverage: leverage, marginMode: marginMode, positionSide: positionSide,
	})
	filled := f.filled
	if filled <= 0 {
		filled = qty
	}
	return filled, price, nil
}

// TestRunnerNeutralPlacesContractOrders: neutral 模式双腿成交打进合约执行器——
// long 腿成交 → positionSide LONG，short 腿成交 → positionSide SHORT，
// 杠杆/保证金模式随机器人配置，成交落库带腿标识。
func TestRunnerNeutralPlacesContractOrders(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(testStartPrice)
	exec := &fakeContractExecutor{}
	runner := newTestRunnerWithExec(src.Price, repo, exec)

	rec := newBotRecord(t, repo, "bot-neutral")
	rec.Mode = string(ModeNeutral)
	rec.Leverage = 5
	rec.MarginMode = "isolated"
	if err := repo.Update(rec); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := runner.StartBot(rec, testStartPrice); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	defer runner.StopBot(rec.ID, "stopped")

	// 上行穿越 106：两腿各一档 SELL 成交 → 两次合约下单。
	feedPrices(src, 106)
	waitFor(t, 3*time.Second, func() bool {
		trades, _ := repo.GetTrades(rec.ID, 0)
		return len(trades) == 2
	}, "2 leg fills persisted")

	trades, err := repo.GetTrades(rec.ID, 0)
	if err != nil {
		t.Fatalf("GetTrades: %v", err)
	}
	legs := map[string]bool{}
	for _, tr := range trades {
		if tr.Quantity <= 0 || tr.Price != 106 {
			t.Errorf("bad trade: %+v", tr)
		}
		legs[tr.Leg] = true
	}
	if !legs[LegLong] || !legs[LegShort] {
		t.Errorf("trades legs = %v, want both long and short", legs)
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.calls) != 2 {
		t.Fatalf("contract calls = %d, want 2", len(exec.calls))
	}
	seen := map[string]string{}
	for _, c := range exec.calls {
		if c.leverage != 5 || c.marginMode != "isolated" {
			t.Errorf("contract call params not propagated: %+v", c)
		}
		seen[c.positionSide] = c.side
	}
	if seen["LONG"] != "SELL" || seen["SHORT"] != "SELL" {
		t.Errorf("position sides = %v, want LONG+SHORT both SELL on up-tick", seen)
	}

	// 聚合主表：base_qty 列为净头寸（short 腿为负）。
	got := mustGetBot(t, repo, rec.ID)
	if got.TotalTrades != 2 {
		t.Errorf("total_trades = %d, want 2", got.TotalTrades)
	}
	if got.BaseQty >= 0 {
		t.Errorf("base_qty(net) = %v, want < 0 after up-tick", got.BaseQty)
	}
}

// TestRunnerContractModeRequiresExecutor: short/neutral 模式无合约执行器时
// StartBot 必须报错（不允许静默降级绕过合约下单链路）。
func TestRunnerContractModeRequiresExecutor(t *testing.T) {
	repo := store.NewGridRepo()
	src := &fakeSource{}
	src.set(testStartPrice)
	runner := newTestRunner(src.Price, repo)

	for _, mode := range []Mode{ModeShort, ModeNeutral} {
		rec := newBotRecord(t, repo, "bot-noexec-"+string(mode))
		rec.Mode = string(mode)
		if err := repo.Update(rec); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if err := runner.StartBot(rec, testStartPrice); err == nil {
			t.Errorf("mode %s without executor should fail", mode)
			runner.StopBot(rec.ID, "stopped")
		}
	}
}
