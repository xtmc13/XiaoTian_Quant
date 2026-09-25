package marketplace

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupTestDB 每个用例独立临时 sqlite（对齐 store 包迁移测试模式）。
func setupTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-for-marketplace")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// seedInstance 写入一个作者持有的 AI 机器人实例（考核数据源）。
func seedInstance(t *testing.T, id string, userID int, initialBalance float64) {
	t.Helper()
	now := time.Now().Unix()
	store.SaveAIBotInstance(map[string]any{
		"id": id, "user_id": userID, "catalog_id": "", "name": "考核实例",
		"strategy_type": "ai_alpha", "symbol": "BTCUSDT", "market_type": "spot",
		"status": "running", "execution_mode": "paper", "config_json": "{}",
		"initial_balance": initialBalance,
		"created_at": now - 40*86400, "updated_at": now, "started_at": now - 40*86400,
	})
	if got := store.GetAIBotInstanceByID(id, userID); got == nil {
		t.Fatalf("seed instance %s failed", id)
	}
}

// ── 状态机迁移测试 ──

func TestCanTransition(t *testing.T) {
	legal := [][2]string{
		{StatusDraft, StatusProbation},
		{StatusProbation, StatusPendingReview},
		{StatusProbation, StatusDraft},
		{StatusPendingReview, StatusListed},
		{StatusPendingReview, StatusRejected},
		{StatusRejected, StatusProbation},
		{StatusListed, StatusDelisted},
		{StatusDelisted, StatusProbation},
	}
	for _, tr := range legal {
		if !CanTransition(tr[0], tr[1]) {
			t.Errorf("expected legal transition %s → %s", tr[0], tr[1])
		}
	}
	illegal := [][2]string{
		{StatusDraft, StatusListed},         // 未经考核/审核直接上架
		{StatusDraft, StatusPendingReview},  // 跳过考核期
		{StatusProbation, StatusListed},     // 达标也必须先过人工审核
		{StatusProbation, StatusRejected},
		{StatusPendingReview, StatusDraft},
		{StatusListed, StatusProbation},     // 须先下架再重新考核
		{StatusRejected, StatusListed},
		{StatusDelisted, StatusListed},
		{StatusListed, StatusDraft},
		{"", StatusProbation},
		{StatusDraft, "unknown"},
	}
	for _, tr := range illegal {
		if CanTransition(tr[0], tr[1]) {
			t.Errorf("expected illegal transition %s → %s to be rejected", tr[0], tr[1])
		}
	}
}

// TestServiceIllegalTransitionsRejected 服务层同样拒绝非法迁移（不只是查表）。
func TestServiceIllegalTransitionsRejected(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-sm-1", 7, 10000)
	svc := NewService()
	now := time.Now()

	rec, err := svc.Create(7, "aibot-sm-1", "robot", "t", "", "free", 0, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// draft 状态直接 approve → 拒绝
	if err := svc.Approve(rec, 1, now); err == nil {
		t.Fatal("approve on draft should fail")
	}
	// draft 状态直接 reject → 拒绝
	if err := svc.Reject(rec, 1, "reason", now); err == nil {
		t.Fatal("reject on draft should fail")
	}
	// 正常提交考核 → probation
	if err := svc.Submit(rec, now); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if rec.Status != StatusProbation {
		t.Fatalf("status = %s, want probation", rec.Status)
	}
	// probation 重复提交 → 拒绝
	if err := svc.Submit(rec, now); err == nil {
		t.Fatal("re-submit on probation should fail")
	}
	// probation 状态 delist → 拒绝
	if err := svc.Delist(rec, 1, "reason", now); err == nil {
		t.Fatal("delist on probation should fail")
	}
	// reject 必须带原因
	if err := svc.Reject(rec, 1, "", now); err == nil {
		t.Fatal("reject without reason should fail")
	}
}

// TestServiceLifecycleHappyPath draft→probation→pending_review→listed→delisted→probation。
func TestServiceLifecycleHappyPath(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-lc-1", 8, 10000)
	svc := NewService()
	now := time.Now()

	rec, err := svc.Create(8, "aibot-lc-1", "robot", "t", "", "profit_share", 20, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	start := now.Add(-31 * 24 * time.Hour)
	if err := svc.Submit(rec, start); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if rec.ProbationStartedAt != start.Unix() {
		t.Fatal("probation start not recorded")
	}
	// 规则快照进条目
	if rec.RuleMinDays != 30 || rec.RuleMinTrades != 10 || rec.RuleMaxDrawdownPct != 50 {
		t.Fatalf("rules snapshot wrong: %+v", rec)
	}
	// 手动推进到 pending_review（cron 路径见 engine 测试）
	rec.Status = StatusPendingReview
	if err := svc.Approve(rec, 99, now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if rec.Status != StatusListed || rec.ListedAt == 0 {
		t.Fatalf("approve result wrong: %+v", rec)
	}
	// 上架后 catalog 可见（is_active=1）
	cat := store.GetAIBotCatalogByID(rec.ID)
	if cat == nil {
		t.Fatal("listed entry should be synced into ai_bot_catalog")
	}
	if active, _ := cat["is_active"].(int); active != 1 {
		t.Fatalf("catalog is_active = %v, want 1", cat["is_active"])
	}
	// 强制下架 → delisted + catalog 隐藏
	if err := svc.Delist(rec, 99, "违规策略", now); err != nil {
		t.Fatalf("delist: %v", err)
	}
	if rec.Status != StatusDelisted || rec.DelistReason != "违规策略" {
		t.Fatalf("delist result wrong: %+v", rec)
	}
	if cat := store.GetAIBotCatalogByID(rec.ID); cat != nil {
		if active, _ := cat["is_active"].(int); active != 0 {
			t.Fatalf("delisted entry catalog is_active = %v, want 0", cat["is_active"])
		}
	}
	// delisted 可重新提交考核
	if err := svc.Submit(rec, now); err != nil {
		t.Fatalf("resubmit from delisted: %v", err)
	}
	if rec.Status != StatusProbation {
		t.Fatalf("resubmit status = %s", rec.Status)
	}
}

// ── 统计聚合口径测试 ──

// TestAggregateStats 合成权益曲线 + 交易，验证与 backtest 口径一致的指标值。
func TestAggregateStats(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-ag-1", 9, 10000)
	svc := NewService()

	rec, err := svc.Create(9, "aibot-ag-1", "robot", "t", "", "free", 0, 0)
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

	// 权益曲线：10000 → 10500 → 9800 → 11000
	store.SaveAIBotSnapshotAt("aibot-ag-1", 10500, 0, 500, 5, start+3600)
	store.SaveAIBotSnapshotAt("aibot-ag-1", 9800, 0, -200, -2, start+2*3600)
	store.SaveAIBotSnapshotAt("aibot-ag-1", 11000, 0, 1000, 10, start+3*3600)
	// 3 笔已平仓交易：+500 / -300 / +200（胜率 2/3，盈亏比 700/300）
	for i, pnl := range []float64{500, -300, 200} {
		store.SaveAIBotTrade(map[string]any{
			"bot_instance_id": "aibot-ag-1", "symbol": "BTCUSDT", "side": "long",
			"entry_price": 100, "exit_price": 100 + pnl/10, "quantity": 10,
			"pnl": pnl, "opened_at": start + int64(i)*3600, "closed_at": start + int64(i)*3600 + 1800,
		})
	}
	// 窗口外数据不应计入
	store.SaveAIBotTrade(map[string]any{
		"bot_instance_id": "aibot-ag-1", "symbol": "BTCUSDT", "side": "long",
		"pnl": 99999, "opened_at": start - 7200, "closed_at": start - 3600,
	})
	store.SaveAIBotSnapshotAt("aibot-ag-1", 5000, 0, 0, -50, start-3600)

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
		t.Errorf("total_trades = %d, want 3", stats.TotalTrades)
	}
	if stats.RunningDays != 2 {
		t.Errorf("running_days = %d, want 2", stats.RunningDays)
	}
	// 月化/年化 = 总收益率按运行天数线性折算
	assertNear("monthly_return_pct", stats.MonthlyReturnPct, 10.0/2*30)
	assertNear("annualized_return_pct", stats.AnnualizedReturnPct, 10.0/2*365)
	if stats.Date != now.UTC().Format("2006-01-02") {
		t.Errorf("snapshot date = %s", stats.Date)
	}
}

// TestAggregateProfitFactorCapped 全盈利（无亏损）时盈亏比数学上为 +Inf，
// 落库用 maxProfitFactor 封顶（JSON 无法表达 Inf）。
func TestAggregateProfitFactorCapped(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-pf-1", 10, 10000)
	svc := NewService()
	rec, err := svc.Create(10, "aibot-pf-1", "robot", "t", "", "free", 0, 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now()
	rec.Status = StatusProbation
	rec.ProbationStartedAt = now.Add(-24 * time.Hour).Unix()
	if err := svc.repo.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}
	store.SaveAIBotTrade(map[string]any{
		"bot_instance_id": "aibot-pf-1", "symbol": "BTCUSDT", "side": "long",
		"pnl": 100, "opened_at": rec.ProbationStartedAt + 60, "closed_at": rec.ProbationStartedAt + 120,
	})
	stats, err := svc.Aggregate(rec, now)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if stats.ProfitFactor != maxProfitFactor {
		t.Errorf("profit_factor = %v, want capped %v", stats.ProfitFactor, maxProfitFactor)
	}
}

// ── 达标判定 + cron 测试 ──

func TestEvaluateProbation(t *testing.T) {
	l := &store.MarketListingRecord{RuleMinDays: 30, RuleMinTrades: 10, RuleMaxDrawdownPct: 50}
	base := &store.MarketListingStatsRecord{RunningDays: 30, TotalTrades: 10, MaxDrawdownPct: 49.9}
	if p := EvaluateProbation(l, base); !p.Passed {
		t.Fatalf("should pass: %+v", p)
	}
	cases := []struct {
		name  string
		stats store.MarketListingStatsRecord
	}{
		{"days not enough", store.MarketListingStatsRecord{RunningDays: 29, TotalTrades: 10, MaxDrawdownPct: 10}},
		{"trades not enough", store.MarketListingStatsRecord{RunningDays: 30, TotalTrades: 9, MaxDrawdownPct: 10}},
		{"catastrophic drawdown", store.MarketListingStatsRecord{RunningDays: 35, TotalTrades: 20, MaxDrawdownPct: 50}},
	}
	for _, tc := range cases {
		if p := EvaluateProbation(l, &tc.stats); p.Passed {
			t.Errorf("%s: should not pass: %+v", tc.name, p)
		}
	}
	// 进度字段
	p := EvaluateProbation(l, &store.MarketListingStatsRecord{RunningDays: 12, TotalTrades: 4, MaxDrawdownPct: 10})
	if p.RemainingDays != 18 || p.RemainingTrades != 6 {
		t.Errorf("progress remaining wrong: %+v", p)
	}
}

// TestEngineRunOnce 达标 cron：probation 满足规则 → pending_review；不满足保持 probation；
// 快照按日落库且幂等。
func TestEngineRunOnce(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-cron-1", 11, 10000)
	seedInstance(t, "aibot-cron-2", 12, 10000)
	svc := NewService()
	engine := NewEngine(svc)
	engine.SetLogf(func(string, ...any) {})

	now := time.Now()
	// 条目1：31 天前提交，10 笔盈利交易 → 达标
	rec1, _ := svc.Create(11, "aibot-cron-1", "robot", "pass", "", "free", 0, 0)
	if err := svc.Submit(rec1, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("submit rec1: %v", err)
	}
	for i := 0; i < 10; i++ {
		store.SaveAIBotTrade(map[string]any{
			"bot_instance_id": "aibot-cron-1", "symbol": "BTCUSDT", "side": "long",
			"pnl": 10, "opened_at": rec1.ProbationStartedAt + int64(i)*3600, "closed_at": rec1.ProbationStartedAt + int64(i)*3600 + 60,
		})
	}
	// 条目2：31 天前提交，但只有 3 笔交易 → 不达标
	rec2, _ := svc.Create(12, "aibot-cron-2", "robot", "fail", "", "free", 0, 0)
	if err := svc.Submit(rec2, now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("submit rec2: %v", err)
	}
	for i := 0; i < 3; i++ {
		store.SaveAIBotTrade(map[string]any{
			"bot_instance_id": "aibot-cron-2", "symbol": "BTCUSDT", "side": "long",
			"pnl": 10, "opened_at": rec2.ProbationStartedAt + int64(i)*3600, "closed_at": rec2.ProbationStartedAt + int64(i)*3600 + 60,
		})
	}

	if err := engine.RunOnce(now); err != nil {
		t.Fatalf("run once: %v", err)
	}
	got1 := svc.repo.GetByID(rec1.ID)
	if got1.Status != StatusPendingReview {
		t.Errorf("rec1 status = %s, want pending_review", got1.Status)
	}
	got2 := svc.repo.GetByID(rec2.ID)
	if got2.Status != StatusProbation {
		t.Errorf("rec2 status = %s, want probation", got2.Status)
	}
	// 两个条目都应有当日快照
	if s := svc.repo.LatestStats(rec1.ID); s == nil || s.TotalTrades != 10 {
		t.Errorf("rec1 latest stats wrong: %+v", s)
	}
	if s := svc.repo.LatestStats(rec2.ID); s == nil || s.TotalTrades != 3 {
		t.Errorf("rec2 latest stats wrong: %+v", s)
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

// TestRulesRoundTrip 规则保存/读取 + 范围校验（管理员缩短考核期做演示的路径）。
func TestRulesRoundTrip(t *testing.T) {
	setupTestDB(t)
	if r := LoadRules(); r.MinDays != 30 || r.MinTrades != 10 || r.MaxDrawdownPct != 50 {
		t.Fatalf("default rules wrong: %+v", r)
	}
	if err := SaveRules(Rules{MinDays: 7, MinTrades: 3, MaxDrawdownPct: 30}); err != nil {
		t.Fatalf("save rules: %v", err)
	}
	if r := LoadRules(); r.MinDays != 7 || r.MinTrades != 3 || r.MaxDrawdownPct != 30 {
		t.Fatalf("loaded rules wrong: %+v", r)
	}
	if err := SaveRules(Rules{MinDays: 0, MinTrades: 10, MaxDrawdownPct: 50}); err == nil {
		t.Fatal("min_days=0 should be rejected")
	}
	if err := SaveRules(Rules{MinDays: 30, MinTrades: 10, MaxDrawdownPct: 120}); err == nil {
		t.Fatal("max_drawdown_pct=120 should be rejected")
	}
	// 校验失败后旧值不被覆盖
	if r := LoadRules(); r.MinDays != 7 {
		t.Fatalf("rules clobbered by invalid save: %+v", r)
	}
}
