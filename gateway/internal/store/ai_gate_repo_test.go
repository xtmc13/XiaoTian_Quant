package store

import (
	"path/filepath"
	"testing"
)

// TestAIGateMigrationAndRepo 冒烟验证 0027 迁移：表创建、Create/MarkOutcome/
// GetByID/List/Count/Stats 与幂等重跑。
func TestAIGateMigrationAndRepo(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-for-ai-gate")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='xt_ai_gate_decisions'`).Scan(&name); err != nil {
		t.Fatalf("table xt_ai_gate_decisions missing: %v", err)
	}

	repo := NewAIGateDecisionRepo()
	rec := &AIGateDecisionRecord{
		ID: "aig-test-1", UserID: 7, Source: "manual", Symbol: "BTCUSDT", Side: "BUY",
		OrderType: "MARKET", MarketType: "spot", Quantity: 0.1, RefPrice: 50000, Notional: 5000,
		Decision: "approve", Allowed: true, Confidence: 0.9, Reasons: []string{"趋势一致", "风险可控"},
		Provider: "deepseek", Model: "deepseek-chat", LatencyMs: 800, RequestHash: "abc123",
		ContextJSON: `{"version":1}`,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.CreatedAt == 0 || rec.UpdatedAt == 0 {
		t.Fatalf("create should fill timestamps: %+v", rec)
	}
	// 一条 fail-open + 一条被拦截记录，供 Stats 验证。
	if err := repo.Create(&AIGateDecisionRecord{
		ID: "aig-test-2", UserID: 7, Source: "signal:macd", Symbol: "ETHUSDT", Side: "BUY",
		Decision: "fail_open", Allowed: true, FailOpen: true, DegradeReason: "provider_unavailable",
	}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if err := repo.Create(&AIGateDecisionRecord{
		ID: "aig-test-3", UserID: 8, Source: "manual", Symbol: "BTCUSDT", Side: "BUY",
		Decision: "reject", Allowed: false, Confidence: 0.95, Reasons: []string{"账户风险过高"},
	}); err != nil {
		t.Fatalf("create 3: %v", err)
	}
	if err := repo.Create(&AIGateDecisionRecord{
		ID: "aig-test-4", UserID: 7, Source: "manual", Symbol: "BTCUSDT", Side: "SELL",
		Decision: "bypassed_exit", Allowed: true,
	}); err != nil {
		t.Fatalf("create 4: %v", err)
	}

	// 成交回写
	if err := repo.MarkOutcome("aig-test-1", "ord-1", true); err != nil {
		t.Fatalf("mark outcome: %v", err)
	}

	got, err := repo.GetByID("aig-test-1")
	if err != nil || got == nil {
		t.Fatalf("get by id: %v rec=%v", err, got)
	}
	if !got.Executed || got.OrderID != "ord-1" {
		t.Fatalf("outcome not written back: %+v", got)
	}
	if len(got.Reasons) != 2 || got.Reasons[0] != "趋势一致" {
		t.Fatalf("reasons roundtrip failed: %+v", got.Reasons)
	}
	if !got.Allowed || got.FailOpen {
		t.Fatalf("bool fields roundtrip failed: %+v", got)
	}

	// 属主过滤：user 7 应见 3 条（含本人，不含 user 8 的）。
	list, err := repo.List(AIGateDecisionFilter{UserID: 7})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("user 7 should see 3 decisions, got %d", len(list))
	}
	total, err := repo.Count(AIGateDecisionFilter{Decision: "approve"})
	if err != nil || total != 1 {
		t.Fatalf("count approve: %d err=%v", total, err)
	}
	// fail_open 过滤
	failTrue := true
	foList, err := repo.List(AIGateDecisionFilter{FailOpen: &failTrue})
	if err != nil || len(foList) != 1 {
		t.Fatalf("fail_open filter: %d err=%v", len(foList), err)
	}

	stats, err := repo.Stats(AIGateDecisionFilter{})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Total != 4 || stats.Blocked != 1 || stats.FailOpen != 1 || stats.BypassedExit != 1 {
		t.Fatalf("stats mismatch: %+v", stats)
	}
	if stats.Evaluated != 2 { // approve + reject（fail_open/bypassed_exit 不计入评估数）
		t.Fatalf("evaluated should be 2: %+v", stats)
	}

	// 来源当日订单统计（xt_orders client_oid 前缀）
	if _, err := db.Exec(`INSERT INTO xt_orders (id, symbol, side, order_type, status, client_oid, created_at, updated_at)
		VALUES ('o1','BTCUSDT','BUY','MARKET','FILLED','dca:42', strftime('%s','now')*1000, strftime('%s','now')*1000)`); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	tot, filled, rejected, err := CountOrdersByClientOIDPrefix("dca:42", 0)
	if err != nil || tot != 1 || filled != 1 || rejected != 0 {
		t.Fatalf("source order stats: %d/%d/%d err=%v", tot, filled, rejected, err)
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
