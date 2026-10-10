package marketplace

import (
	"math"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// seedGridBot 写入一个作者持有的网格机器人（考核数据源；时间戳毫秒级，
// 与 grid.Runner 落库口径一致）。
func seedGridBot(t *testing.T, id string, userID int64, initialEquity float64) {
	t.Helper()
	repo := store.NewGridRepo()
	bot := &store.GridBotRecord{
		ID: id, UserID: userID, Name: "考核网格-" + id, Symbol: "BTCUSDT",
		LowerPrice: 90, UpperPrice: 110, GridCount: 10, Investment: initialEquity,
		InitialEquity: initialEquity,
		CreatedAt:     time.Now().UnixMilli() - 40*86400*1000,
	}
	if err := repo.Create(bot); err != nil {
		t.Fatalf("seed grid bot %s: %v", id, err)
	}
	if got, err := repo.GetByIDForUser(id, userID); err != nil || got == nil {
		t.Fatalf("seed grid bot %s failed: got=%v err=%v", id, got, err)
	}
}

// TestCreateGridBotOwnership 创建分派：grid_bot 校验 grid_bots 属主，
// 非属主/不存在拒绝；非法 object_kind 返回明确错误；空 kind 兼容 ai_bot。
func TestCreateGridBotOwnership(t *testing.T) {
	setupTestDB(t)
	seedGridBot(t, "grid-own-1", 21, 10000)
	svc := NewService()

	// 属主创建成功：名称默认取机器人名，object 字段落库，bot_instance_id 留空。
	rec, err := svc.Create(21, ObjectKindGridBot, "grid-own-1", "robot", "", "", "free", 0, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ObjectKind != ObjectKindGridBot || rec.ObjectID != "grid-own-1" {
		t.Fatalf("object fields wrong: %+v", rec)
	}
	if rec.Name != "考核网格-grid-own-1" {
		t.Fatalf("default name = %q, want grid bot name", rec.Name)
	}
	if rec.BotInstanceID != "" {
		t.Fatalf("grid listing should not set bot_instance_id, got %q", rec.BotInstanceID)
	}
	persisted := svc.repo.GetByID(rec.ID)
	if persisted == nil || persisted.ObjectKind != ObjectKindGridBot || persisted.ObjectID != "grid-own-1" {
		t.Fatalf("persisted object fields wrong: %+v", persisted)
	}

	// 非属主 → 拒绝
	if _, err := svc.Create(22, ObjectKindGridBot, "grid-own-1", "robot", "", "", "free", 0, 0); err == nil {
		t.Fatal("create with others' grid bot should fail")
	}
	// 不存在的网格机器人 → 拒绝
	if _, err := svc.Create(21, ObjectKindGridBot, "grid-no-such", "robot", "", "", "free", 0, 0); err == nil {
		t.Fatal("create with unknown grid bot should fail")
	}
	// 非法 object_kind → 明确错误
	if _, err := svc.Create(21, "dca_bot", "grid-own-1", "robot", "", "", "free", 0, 0); err == nil {
		t.Fatal("unsupported object_kind should fail")
	}
}

// TestAggregateGridBotStats 合成网格权益曲线 + 成交（毫秒时间戳），验证与
// backtest 口径一致的指标值；pnl=0 的建仓腿不计入平仓口径；窗口外数据不计入。
func TestAggregateGridBotStats(t *testing.T) {
	setupTestDB(t)
	seedGridBot(t, "grid-ag-1", 23, 10000)
	svc := NewService()
	repo := store.NewGridRepo()

	rec, err := svc.Create(23, ObjectKindGridBot, "grid-ag-1", "robot", "t", "", "free", 0, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now()
	start := now.Add(-48 * time.Hour).Unix()
	rec.Status = StatusProbation
	rec.ProbationStartedAt = start
	if err := svc.repo.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}

	// 权益曲线（毫秒）：10000 → 10500 → 9800 → 11000
	startMs := start * 1000
	for i, eq := range []float64{10500, 9800, 11000} {
		if err := repo.InsertSnapshot("grid-ag-1", eq, 100, eq-10000, 0, startMs+int64(i+1)*3600*1000); err != nil {
			t.Fatalf("insert snapshot %d: %v", i, err)
		}
	}
	// 平仓腿成交：+500 / -300 / +200（胜率 2/3，盈亏比 700/300）
	for i, pnl := range []float64{500, -300, 200} {
		if err := repo.InsertTrade("grid-ag-1", i, "SELL", 100, 10, 1000, 0, pnl, "long", startMs+int64(i)*3600*1000); err != nil {
			t.Fatalf("insert trade %d: %v", i, err)
		}
	}
	// pnl=0 的建仓腿（BUY）不计入平仓口径
	if err := repo.InsertTrade("grid-ag-1", 3, "BUY", 99, 10, 990, 0, 0, "long", startMs+4*3600*1000); err != nil {
		t.Fatalf("insert buy fill: %v", err)
	}
	// 窗口外数据不应计入
	if err := repo.InsertTrade("grid-ag-1", 4, "SELL", 100, 10, 1000, 0, 99999, "long", startMs-3600*1000); err != nil {
		t.Fatalf("insert pre-window trade: %v", err)
	}
	if err := repo.InsertSnapshot("grid-ag-1", 5000, 100, -5000, 0, startMs-3600*1000); err != nil {
		t.Fatalf("insert pre-window snapshot: %v", err)
	}

	stats, err := svc.Aggregate(rec, now)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	assertNear := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 0.01 {
			t.Errorf("%s = %.4f, want %.4f", name, got, want)
		}
	}
	assertNear("total_return_pct", stats.TotalReturnPct, 10.0)
	// 回撤：峰值 10500 → 谷值 9800 ⇒ 700/10500 = 6.6667%
	assertNear("max_drawdown_pct", stats.MaxDrawdownPct, 700.0/105.0)
	assertNear("win_rate", stats.WinRate, 200.0/3.0)
	assertNear("profit_factor", stats.ProfitFactor, 700.0/300.0)
	if stats.TotalTrades != 3 {
		t.Errorf("total_trades = %d, want 3 (zero-pnl fill excluded)", stats.TotalTrades)
	}
	if stats.RunningDays != 2 {
		t.Errorf("running_days = %d, want 2", stats.RunningDays)
	}
	assertNear("monthly_return_pct", stats.MonthlyReturnPct, 10.0/2*30)
	assertNear("annualized_return_pct", stats.AnnualizedReturnPct, 10.0/2*365)

	// 非法 object_kind → 明确错误
	bad := &store.MarketListingRecord{ID: "ml-bad", ObjectKind: "dca_bot"}
	if _, err := svc.Aggregate(bad, now); err == nil {
		t.Fatal("aggregate with unsupported object_kind should fail")
	}
}

// TestEngineRunOnceGridBot 网格条目考核 cron：probation 达标（天数/平仓数/回撤）
// 自动转 pending_review；不达标保持 probation；快照按日幂等。
func TestEngineRunOnceGridBot(t *testing.T) {
	setupTestDB(t)
	seedGridBot(t, "grid-cron-1", 24, 10000)
	seedGridBot(t, "grid-cron-2", 25, 10000)
	svc := NewService()
	repo := store.NewGridRepo()
	engine := NewEngine(svc)
	engine.SetLogf(func(string, ...any) {})

	now := time.Now()
	// 条目1：31 天前提交，10 笔落袋成交 → 达标
	rec1, err := svc.Create(24, ObjectKindGridBot, "grid-cron-1", "robot", "pass", "", "free", 0, 0)
	if err != nil {
		t.Fatalf("create rec1: %v", err)
	}
	if err := svc.Submit(rec1, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("submit rec1: %v", err)
	}
	for i := 0; i < 10; i++ {
		ts := rec1.ProbationStartedAt*1000 + int64(i)*3600*1000
		if err := repo.InsertTrade("grid-cron-1", i, "SELL", 100, 10, 1000, 0, 10, "long", ts); err != nil {
			t.Fatalf("insert trade %d: %v", i, err)
		}
		if err := repo.InsertSnapshot("grid-cron-1", 10000+float64(i+1)*10, 100, float64(i+1)*10, 0, ts); err != nil {
			t.Fatalf("insert snapshot %d: %v", i, err)
		}
	}
	// 条目2：31 天前提交，但只有 3 笔落袋成交 → 不达标
	rec2, err := svc.Create(25, ObjectKindGridBot, "grid-cron-2", "robot", "fail", "", "free", 0, 0)
	if err != nil {
		t.Fatalf("create rec2: %v", err)
	}
	if err := svc.Submit(rec2, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("submit rec2: %v", err)
	}
	for i := 0; i < 3; i++ {
		ts := rec2.ProbationStartedAt*1000 + int64(i)*3600*1000
		if err := repo.InsertTrade("grid-cron-2", i, "SELL", 100, 10, 1000, 0, 10, "long", ts); err != nil {
			t.Fatalf("insert trade %d: %v", i, err)
		}
	}

	if err := engine.RunOnce(now); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if got := svc.repo.GetByID(rec1.ID); got.Status != StatusPendingReview {
		t.Errorf("rec1 status = %s, want pending_review", got.Status)
	}
	if got := svc.repo.GetByID(rec2.ID); got.Status != StatusProbation {
		t.Errorf("rec2 status = %s, want probation", got.Status)
	}
	if s := svc.repo.LatestStats(rec1.ID); s == nil || s.TotalTrades != 10 {
		t.Errorf("rec1 latest stats wrong: %+v", s)
	}
	// 幂等：当天重跑不重复迁移、不重复插行
	if err := engine.RunOnce(now); err != nil {
		t.Fatalf("run once again: %v", err)
	}
	if got := svc.repo.GetByID(rec1.ID); got.Status != StatusPendingReview {
		t.Errorf("rec1 re-run status = %s", got.Status)
	}
	if n := len(svc.repo.StatsSeries(rec1.ID, 0)); n != 1 {
		t.Errorf("rec1 stats rows = %d, want 1 (same-day idempotent)", n)
	}
}
