package reconcile

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// fakeReportedPnLAdapter 实现 ReportedPnLQuerier。
type fakeReportedPnLAdapter struct {
	incomes []ReportedPnL
	err     error
}

func (f *fakeReportedPnLAdapter) GetRealizedPnLIncomes(symbol string, startMs, endMs int64) ([]ReportedPnL, error) {
	return f.incomes, f.err
}

// newTestChecker 构造固定 now、可捕获告警的 checker。notifyCount 记录告警次数。
func newTestChecker(repo *store.ReconcileRepo, now time.Time, adapters map[string]any, notifyCount *int) *ReportedPnLChecker {
	c := NewReportedPnLChecker(repo, func(name string) any { return adapters[name] }, nil, nil)
	c.now = func() time.Time { return now }
	c.notify = func(title, content, level, channel, eventType string, data map[string]any) {
		*notifyCount++
	}
	return c
}

// seedWindowTrades 造窗口前建仓 + 窗口内平仓的成交：FIFO 已实现盈亏 = (51000-50000)*1 = +1000。
func seedWindowTrades(t *testing.T, now time.Time) {
	t.Helper()
	tradeRepo := store.NewTradeRepo()
	for _, tr := range []*store.TradeRecord{
		{ID: "t-open", Symbol: "BTCUSDT", Side: "BUY", Price: 50000, Quantity: 1,
			Exchange: "binance", UserID: 7, CreatedAt: now.Add(-30 * time.Hour).UnixMilli()},
		{ID: "t-close", Symbol: "BTCUSDT", Side: "SELL", Price: 51000, Quantity: 1,
			Exchange: "binance", UserID: 7, CreatedAt: now.Add(-12 * time.Hour).UnixMilli()},
	} {
		if err := tradeRepo.Create(tr); err != nil {
			t.Fatalf("create trade %s: %v", tr.ID, err)
		}
	}
}

func TestReportedPnLOK(t *testing.T) {
	repo := setupTestDB(t)
	now := time.UnixMilli(1_720_000_000_000)
	seedWindowTrades(t, now)

	notified := 0
	fake := &fakeReportedPnLAdapter{incomes: []ReportedPnL{
		{Symbol: "BTCUSDT", Asset: "USDT", Income: 600, Time: now.Add(-12 * time.Hour).UnixMilli(), TradeID: "x1"},
		{Symbol: "BTCUSDT", Asset: "USDT", Income: 400, Time: now.Add(-11 * time.Hour).UnixMilli(), TradeID: "x2"},
	}}
	c := newTestChecker(repo, now, map[string]any{"binance": fake}, &notified)

	msg, err := c.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("run: %s", msg)

	checks, err := repo.ListReportedPnLChecks(0, "binance", "", 0, 10, 0)
	if err != nil || len(checks) != 1 {
		t.Fatalf("应有 1 条对账记录: %v %+v", err, checks)
	}
	rec := checks[0]
	if rec.Status != "ok" {
		t.Fatalf("一致应为 ok: %+v", rec)
	}
	if rec.LocalPNL != 1000 || rec.ReportedPNL != 1000 || rec.Diff != 0 || rec.DiffPct != 0 {
		t.Fatalf("盈亏数值异常: %+v", rec)
	}
	if rec.UserID != 0 || rec.CredentialID != "binance" || rec.Symbol != "" {
		t.Fatalf("属主/凭证/合约口径异常: %+v", rec)
	}
	if notified != 0 {
		t.Fatalf("ok 不得告警: %d", notified)
	}
}

func TestReportedPnLMismatchNotifies(t *testing.T) {
	repo := setupTestDB(t)
	now := time.UnixMilli(1_720_000_000_000)
	seedWindowTrades(t, now)

	notified := 0
	// 本地 FIFO +1000，交易所回报 950：diff=50 → 50/950≈5.26% > 1% → mismatch。
	fake := &fakeReportedPnLAdapter{incomes: []ReportedPnL{
		{Symbol: "BTCUSDT", Asset: "USDT", Income: 950, Time: now.Add(-12 * time.Hour).UnixMilli()},
	}}
	c := newTestChecker(repo, now, map[string]any{"binance": fake}, &notified)

	if _, err := c.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	checks, _ := repo.ListReportedPnLChecks(0, "binance", "", 0, 10, 0)
	if len(checks) != 1 {
		t.Fatalf("应有 1 条对账记录: %+v", checks)
	}
	rec := checks[0]
	if rec.Status != "mismatch" {
		t.Fatalf("超阈应为 mismatch: %+v", rec)
	}
	if rec.Diff != 50 || rec.DiffPct < 5.0 {
		t.Fatalf("diff/diff_pct 异常: %+v", rec)
	}
	if notified != 1 {
		t.Fatalf("mismatch 必须告警一次: %d", notified)
	}
	if !strings.Contains(rec.Detail, "local=1000") {
		t.Fatalf("detail 应含本地盈亏: %s", rec.Detail)
	}

	// status 过滤：mismatch 能查到，error 查不到。
	mm, _ := repo.ListReportedPnLChecks(0, "", "mismatch", 0, 10, 0)
	if len(mm) != 1 {
		t.Fatalf("status 过滤异常: %+v", mm)
	}
	er, _ := repo.ListReportedPnLChecks(0, "", "error", 0, 10, 0)
	if len(er) != 0 {
		t.Fatalf("不应有 error 记录: %+v", er)
	}
}

func TestReportedPnLErrorDoesNotBreakRun(t *testing.T) {
	repo := setupTestDB(t)
	now := time.UnixMilli(1_720_000_000_000)

	notified := 0
	adapters := map[string]any{
		"binance": &fakeReportedPnLAdapter{err: fmt.Errorf("api key invalid")},
		"bybit":   &fakeReportedPnLAdapter{incomes: nil}, // 无成交无流水 → ok
	}
	c := newTestChecker(repo, now, adapters, &notified)

	msg, err := c.Run()
	if err != nil {
		t.Fatalf("单所失败不得中断整体: %v", err)
	}
	t.Logf("run: %s", msg)

	checks, _ := repo.ListReportedPnLChecks(0, "", "", 0, 10, 0)
	if len(checks) != 2 {
		t.Fatalf("应落 binance(error)+bybit(ok) 两条: %+v", checks)
	}
	byStatus := map[string]*store.ReportedPnLCheckRecord{}
	for _, c := range checks {
		byStatus[c.Exchange] = c
	}
	if byStatus["binance"].Status != "error" || !strings.Contains(byStatus["binance"].Detail, "api key invalid") {
		t.Fatalf("binance 应为 error 行: %+v", byStatus["binance"])
	}
	if byStatus["bybit"].Status != "ok" {
		t.Fatalf("bybit 应为 ok 行: %+v", byStatus["bybit"])
	}
	if notified != 0 {
		t.Fatalf("error/ok 都不得发 mismatch 告警: %d", notified)
	}
}

func TestReportedPnLConfigOverride(t *testing.T) {
	repo := setupTestDB(t)
	if err := repo.SetSetting("reported_pnl_pct", "2.5"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if err := repo.SetSetting("reported_pnl_window_h", "12"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	cfg := LoadConfig(repo)
	if cfg.ReportedPnLThresholdPct != 2.5 || cfg.ReportedPnLWindowH != 12 {
		t.Fatalf("设置表覆盖未生效: %+v", cfg)
	}
	snap := cfg.ConfigSnapshot()
	if snap["reported_pnl_pct"] != 2.5 || snap["reported_pnl_window_h"] != 12 {
		t.Fatalf("snapshot 缺回报 PnL 配置: %+v", snap)
	}
}

func TestServiceRunReportedPnLManual(t *testing.T) {
	repo := setupTestDB(t)
	svc := NewService(repo, func(name string) any { return nil })
	defer svc.Stop()

	msg, err := svc.RunReportedPnL(7)
	if err != nil {
		t.Fatalf("manual run: %v", err)
	}
	if msg != "checked=0 mismatch=0 error=0" {
		t.Fatalf("无凭证时应空跑: %q", msg)
	}
	status := svc.Status()
	run, ok := status["last_runs"].(map[string]TaskRun)["reported_pnl"]
	if !ok || !run.OK {
		t.Fatalf("手动触发应进 last_runs: %+v", status["last_runs"])
	}
}
