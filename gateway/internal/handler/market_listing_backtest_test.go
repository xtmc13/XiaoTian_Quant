package handler

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/data"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

/* ── 条目 stats 回测对照 API 测试 ──────────────────────────────
 * 覆盖：grid_bot 条目含 backtest_series + deviation（真实小数据引擎重放）、
 * 结果缓存命中、ai_bot 无对照降级（null + note）、K 线不足降级（不 500）。
 * ─────────────────────────────────────────────────────────────── */

// seedGridCompareBars 落本地 1h K 线（等幅正弦震荡，价格始终在 [90,110] 内），
// 并隔离替换 DataDownloader（同 analysis_test 口径）。
func seedGridCompareBars(t *testing.T, symbol string, fromMs, toMs int64) {
	t.Helper()
	old := DataDownloader
	DataDownloader = data.NewDownloader(data.NewStorage())
	t.Cleanup(func() { DataDownloader = old })

	var bars []model.Bar
	for ts, i := fromMs, 0; ts < toMs; ts += 3600_000 {
		c := 100 + 8*math.Sin(float64(i)/3.0)
		o := 100 + 8*math.Sin(float64(i-1)/3.0)
		bars = append(bars, model.Bar{
			Symbol: symbol, Interval: "1h", Time: ts,
			Open: o, High: math.Max(o, c) + 0.4, Low: math.Min(o, c) - 0.4,
			Close: c, Volume: 1000,
		})
		i++
	}
	if err := DataDownloader.SaveBars(bars); err != nil {
		t.Fatalf("seed bars: %v", err)
	}
}

// createListedGridListing 造 listed 网格条目：属主创建并提交考核后
// 直接落库推进 listed，probation_started_at 回拨到 daysAgo 天前。
func createListedGridListing(t *testing.T, r *gin.Engine, botID string, daysAgo int) string {
	t.Helper()
	w, resp := marketDo(t, r, "POST", "/market/listings",
		fmt.Sprintf(`{"object_kind":"grid_bot","object_id":%q,"kind":"robot","fee_model":"free","submit":true}`, botID))
	assertEq(t, w.Code, http.StatusOK, "create grid listing")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "listing id")
	repo := store.NewMarketListingRepo()
	rec := repo.GetByID(id)
	rec.Status = "listed"
	rec.ProbationStartedAt = time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour).Unix()
	if err := repo.Update(rec); err != nil {
		t.Fatalf("force listed: %v", err)
	}
	t.Cleanup(func() {
		rec = repo.GetByID(id)
		rec.Status = "delisted"
		_ = repo.Update(rec)
	})
	return id
}

func TestMarketListingStatsGridBacktestCompare(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	gridRepo := store.NewGridRepo()
	if err := gridRepo.Create(&store.GridBotRecord{
		ID: "grid-bt-cmp", UserID: 791, Name: "回测对照网格", Symbol: "GRIDBTCUSDT",
		LowerPrice: 90, UpperPrice: 110, GridCount: 10, Investment: 10000, InitialEquity: 10000,
	}); err != nil {
		t.Fatalf("seed grid bot: %v", err)
	}
	now := time.Now()
	seedGridCompareBars(t, "GRIDBTCUSDT", now.Add(-6*24*time.Hour).UnixMilli(), now.UnixMilli())

	author := marketTestRouter(791, "user")
	id := createListedGridListing(t, author, "grid-bt-cmp", 5)

	// 实盘侧平台快照：最新累计收益 12.5%（deviation 的实盘端）
	repo := store.NewMarketListingRepo()
	if err := repo.UpsertStats(&store.MarketListingStatsRecord{
		ListingID: id, Date: now.UTC().Format("2006-01-02"),
		TotalReturnPct: 12.5, MaxDrawdownPct: 3, WinRate: 60, ProfitFactor: 1.8,
		SharpeRatio: 1.2, TotalTrades: 20, MonthlyReturnPct: 6, RunningDays: 5,
	}); err != nil {
		t.Fatalf("upsert stats: %v", err)
	}

	other := marketTestRouter(792, "user")
	cacheSizeBefore := backtest.GetGlobalCache().Size()
	w, resp := marketDo(t, other, "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusOK, "grid stats with backtest compare")

	// 回测曲线：非空、点含 time/return_pct
	btSeries, ok := resp["backtest_series"].([]any)
	assertTrue(t, ok && len(btSeries) > 24, "backtest_series should be a non-empty series")
	pt := btSeries[0].(map[string]any)
	assertTrue(t, pt["time"].(float64) > 1_000_000_000_000, "point time should be unix ms")
	if _, ok := pt["return_pct"].(float64); !ok {
		t.Fatal("point should carry return_pct")
	}
	assertTrue(t, resp["backtest_error"] == nil, "no backtest_error on success")

	// 口径假设随响应披露
	assumptions, ok := resp["backtest_assumptions"].(map[string]any)
	assertTrue(t, ok, "backtest_assumptions present")
	assertTrue(t, assumptions["interval"] == "1h", "interval 1h")
	assertTrue(t, assumptions["fee_rate"].(float64) == 0.001, "uniform fee 0.1%%")
	assertTrue(t, assumptions["slippage"].(float64) == 0, "limit-fill no slippage")
	assertTrue(t, assumptions["truncated"] == false, "5-day window not truncated")
	assertTrue(t, assumptions["window_days"].(float64) >= 4, "window days ~5")
	note, _ := resp["backtest_note"].(string)
	assertTrue(t, strings.Contains(note, "回测对照"), "note discloses replay assumptions")

	// 偏差指标：live=12.5，diff = live - backtest（方向与符号一致）
	dev, ok := resp["deviation"].(map[string]any)
	assertTrue(t, ok, "deviation present when both sides have data")
	assertTrue(t, dev["live_return_pct"].(float64) == 12.5, "live side from latest snapshot")
	btRet := dev["backtest_return_pct"].(float64)
	diff := dev["diff_pct"].(float64)
	assertTrue(t, math.Abs(diff-(12.5-btRet)) < 0.001, "diff = live - backtest")
	wantDir := "flat"
	if diff > 0 {
		wantDir = "higher"
	} else if diff < 0 {
		wantDir = "lower"
	}
	assertTrue(t, dev["direction"] == wantDir, "direction matches diff sign")

	// 缓存：首次重放后写入，二次请求命中不再增长
	assertEq(t, backtest.GetGlobalCache().Size(), cacheSizeBefore+1, "first request caches result")
	w, resp = marketDo(t, other, "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusOK, "second stats request")
	assertEq(t, backtest.GetGlobalCache().Size(), cacheSizeBefore+1, "second request should hit cache")
	if _, ok := resp["backtest_series"].([]any); !ok {
		t.Fatal("cached response should still carry backtest_series")
	}
}

func TestMarketListingStatsAIBotBacktestNote(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	seedMarketInstance(t, "aibot-mkt-bt", 793)
	author := marketTestRouter(793, "user")
	id := createProbationListing(t, author, "aibot-mkt-bt")
	repo := store.NewMarketListingRepo()
	rec := repo.GetByID(id)
	rec.Status = "listed"
	if err := repo.Update(rec); err != nil {
		t.Fatalf("force listed: %v", err)
	}

	other := marketTestRouter(794, "user")
	w, resp := marketDo(t, other, "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusOK, "ai_bot stats")
	// AI 信号驱动：无回测对照（显式 null + 如实说明），不下发偏差
	v, exists := resp["backtest_series"]
	assertTrue(t, exists && v == nil, "backtest_series should be explicit null")
	note, ok := resp["backtest_note"].(string)
	assertTrue(t, ok && strings.Contains(note, "AI 信号驱动策略无回测对照"), "ai_bot note")
	if _, exists := resp["deviation"]; exists {
		t.Fatal("ai_bot should not carry deviation")
	}
	if _, exists := resp["backtest_error"]; exists {
		t.Fatal("ai_bot should not carry backtest_error")
	}

	rec = repo.GetByID(id)
	rec.Status = "delisted"
	if err := repo.Update(rec); err != nil {
		t.Fatalf("cleanup delist: %v", err)
	}
}

func TestMarketListingStatsGridBacktestNoData(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	// 空本地库（该 symbol 无 K 线）
	old := DataDownloader
	DataDownloader = data.NewDownloader(data.NewStorage())
	t.Cleanup(func() { DataDownloader = old })

	gridRepo := store.NewGridRepo()
	if err := gridRepo.Create(&store.GridBotRecord{
		ID: "grid-bt-nodata", UserID: 795, Name: "无数据网格", Symbol: "NOBARSUSDT",
		LowerPrice: 90, UpperPrice: 110, GridCount: 10, Investment: 10000, InitialEquity: 10000,
	}); err != nil {
		t.Fatalf("seed grid bot: %v", err)
	}
	author := marketTestRouter(795, "user")
	id := createListedGridListing(t, author, "grid-bt-nodata", 5)

	other := marketTestRouter(796, "user")
	w, resp := marketDo(t, other, "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusOK, "no-data degrade should not 500")
	assertTrue(t, resp["backtest_series"] == nil, "no series without bars")
	errMsg, ok := resp["backtest_error"].(string)
	assertTrue(t, ok && strings.Contains(errMsg, "K线数据不足"), "backtest_error explains insufficiency")
	if _, exists := resp["deviation"]; exists {
		t.Fatal("no deviation without backtest series")
	}
}

func TestMarketListingStatsGridBacktestShortWindow(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	gridRepo := store.NewGridRepo()
	if err := gridRepo.Create(&store.GridBotRecord{
		ID: "grid-bt-fresh", UserID: 797, Name: "新考核网格", Symbol: "GRIDBTCUSDT",
		LowerPrice: 90, UpperPrice: 110, GridCount: 10, Investment: 10000, InitialEquity: 10000,
	}); err != nil {
		t.Fatalf("seed grid bot: %v", err)
	}
	// 考核今天才开始：窗口 [now, 今日UTC零点) 为空 → 如实降级
	author := marketTestRouter(797, "user")
	w, resp := marketDo(t, author, "POST", "/market/listings",
		`{"object_kind":"grid_bot","object_id":"grid-bt-fresh","kind":"robot","fee_model":"free","submit":true}`)
	assertEq(t, w.Code, http.StatusOK, "create grid listing")
	id, _ := resp["id"].(string)

	w, resp = marketDo(t, author, "GET", "/market/listings/"+id+"/stats", "")
	assertEq(t, w.Code, http.StatusOK, "fresh probation stats")
	assertTrue(t, resp["backtest_series"] == nil, "no series for sub-day window")
	errMsg, ok := resp["backtest_error"].(string)
	assertTrue(t, ok && strings.Contains(errMsg, "考核窗口不足"), "short window error message")
}
