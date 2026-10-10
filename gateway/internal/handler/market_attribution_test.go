package handler

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/marketplace"
	"github.com/xiaotian-quant/gateway/internal/social"
	"github.com/xiaotian-quant/gateway/internal/store"
)

/* ── 分成归因接线测试（迁移 0060）────────────────────────────────
 * ai_bot 模拟器平仓盈利 → xt_social_copy_pnl（克隆实例带 origin_listing_id）；
 * 归因异常（条目不存在/未建档）不影响平仓落库主流程；
 * executor 平仓计提 pending_share 处同步落钱轨（属主已建档时）。
 * ─────────────────────────────────────────────────────────────── */

// seedAttributionListing 落库 listed profit_share 条目 + 作者 provider 建档 +
// 订阅者 active 订阅，返回 provider id。
// 注意 handler 包共享测试库：listed 条目会影响 market_listing_test.go 的
// 公开列表全局计数断言，调用方测试结束须 delistCleanup 清理。
func seedAttributionListing(t *testing.T, listingID string, authorID, subscriberID int, feePercent float64) int64 {
	t.Helper()
	rec := &store.MarketListingRecord{
		ID: listingID, AuthorUserID: int64(authorID), ObjectKind: marketplace.ObjectKindAIBot,
		ObjectID: "aibot-src-" + listingID, Kind: "robot", Name: "归因条目-" + listingID,
		FeeModel: "profit_share", FeePercent: feePercent, Status: marketplace.StatusListed,
	}
	if err := store.NewMarketListingRepo().Create(rec); err != nil {
		t.Fatalf("seed listing: %v", err)
	}
	pid, err := marketplace.EnsureListingProvider(rec)
	if err != nil || pid <= 0 {
		t.Fatalf("ensure provider: pid=%d err=%v", pid, err)
	}
	if _, err := store.CreateMarketSubscription(&store.MarketSubscriptionRecord{
		UserID: subscriberID, ListingID: listingID, FeeType: "profit_share",
		FeePercent: feePercent, Status: store.MarketSubActive,
	}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	return pid
}

// delistCleanup 测试收尾下架条目（保持共享库公开列表计数干净）。
func delistCleanup(t *testing.T, listingID string) {
	t.Helper()
	repo := store.NewMarketListingRepo()
	if l := repo.GetByID(listingID); l != nil {
		l.Status = marketplace.StatusDelisted
		if err := repo.Update(l); err != nil {
			t.Fatalf("delist cleanup: %v", err)
		}
	}
}

func copyPnLCount(t *testing.T, providerID int64, followerID int) int64 {
	t.Helper()
	_, cnt, err := social.NewMarketService().SumCopyPnL(providerID, int64(followerID), 0, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("sum copy pnl: %v", err)
	}
	return cnt
}

// TestSimulatorCloseAttributesProfit 克隆实例 TP 平仓盈利 → 归因落钱轨。
func TestSimulatorCloseAttributesProfit(t *testing.T) {
	subscriber := 771
	author := 772
	providerID := seedAttributionListing(t, "ml-sim-attr", author, subscriber, 20)
	defer delistCleanup(t, "ml-sim-attr")

	// 固定行情：60000 开仓，61800 触发 +3% TP（确定性盈利）。
	price := 60000.0
	orig := aiBotPriceFn
	aiBotPriceFn = func(symbol string) float64 { return price }
	defer func() { aiBotPriceFn = orig }()

	botID := "aibot-test-attr-close"
	resetPaperState(botID)
	defer resetPaperState(botID)

	inst := map[string]any{
		"id": botID, "user_id": subscriber, "symbol": "BTCUSDT",
		"initial_balance": 10000.0, "config_json": `{"confidence_threshold":60}`,
		"origin_listing_id": "ml-sim-attr",
	}

	// 开仓：long 信号驱动
	insertTestSignal(t, int64(subscriber), "BTCUSDT", "long", 80, time.Now().Unix())
	if _, _, _, _, _, _, _, _, err := simulateAIBotStep(inst); err != nil {
		t.Fatalf("open step: %v", err)
	}
	// 涨价到 TP → 平仓盈利 → 归因
	price = 61800
	_, realized, _, _, _, _, _, totalTrades, err := simulateAIBotStep(inst)
	if err != nil {
		t.Fatalf("close step: %v", err)
	}
	if totalTrades != 1 || realized <= 0 {
		t.Fatalf("expected 1 profitable close, trades=%d realized=%.4f", totalTrades, realized)
	}
	if cnt := copyPnLCount(t, providerID, subscriber); cnt != 1 {
		t.Fatalf("expected 1 copy pnl row, got %d", cnt)
	}
}

// TestSimulatorCloseAttributionIsolation 归因失败（条目不存在）不影响平仓落库。
func TestSimulatorCloseAttributionIsolation(t *testing.T) {
	price := 60000.0
	orig := aiBotPriceFn
	aiBotPriceFn = func(symbol string) float64 { return price }
	defer func() { aiBotPriceFn = orig }()

	botID := "aibot-test-attr-gone"
	resetPaperState(botID)
	defer resetPaperState(botID)

	subscriber := 773
	inst := map[string]any{
		"id": botID, "user_id": subscriber, "symbol": "BTCUSDT",
		"initial_balance": 10000.0, "config_json": `{"confidence_threshold":60}`,
		"origin_listing_id": "ml-sim-gone", // 条目不存在：归因静默跳过
	}
	insertTestSignal(t, int64(subscriber), "BTCUSDT", "long", 80, time.Now().Unix())
	if _, _, _, _, _, _, _, _, err := simulateAIBotStep(inst); err != nil {
		t.Fatalf("open step: %v", err)
	}
	price = 61800
	_, realized, _, _, _, _, _, totalTrades, err := simulateAIBotStep(inst)
	if err != nil {
		t.Fatalf("close step must not fail on attribution: %v", err)
	}
	if totalTrades != 1 || realized <= 0 {
		t.Fatalf("close bookkeeping must survive attribution skip, trades=%d realized=%.4f", totalTrades, realized)
	}
	trades := store.GetAIBotTrades(botID, 10)
	if len(trades) != 1 {
		t.Fatalf("trade must be persisted, got %d", len(trades))
	}
}

// TestExecutorCloseAttributesToProvider executor 平仓计提 pending_share 时，
// 属主已建档的信号源同步落 xt_social_copy_pnl（active profit_share 订阅者）。
func TestExecutorCloseAttributesToProvider(t *testing.T) {
	mkt := social.NewMarketService()
	owner := billingSeedUser(t, "mkt_attr_exec_owner")
	follower := billingSeedUser(t, "mkt_attr_exec_follower")

	pct := 20.0
	p, err := mkt.ApplyProvider(int64(owner), "执行源作者", "", 0, &pct)
	if err != nil {
		t.Fatalf("apply provider: %v", err)
	}
	if _, err := mkt.ApproveProvider(p.ID); err != nil {
		t.Fatalf("approve provider: %v", err)
	}
	srcRepo := store.NewSignalSourceRepo()
	src := &store.SignalSource{
		Name: "归因源", Type: "webhook", OwnerUserID: owner, Enabled: true,
		FeeModel: "profit_share", FeePercent: 20,
	}
	if err := srcRepo.Create(src); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := srcRepo.CreateSubscription(&store.SignalSourceSubscription{
		SourceID: src.ID, UserID: follower, FeeModel: "profit_share", FeePercent: 20, Status: "active",
	}); err != nil {
		t.Fatalf("create source subscription: %v", err)
	}

	// 信号 + 执行记录（source 归属新源），走阶梯 TP 全平
	sig := &store.SignalRecord{Symbol: "ATTRTESTUSDT", Direction: "LONG", Strategy: "test", Status: "EXECUTED"}
	if err := store.NewSignalRepo().Create(sig); err != nil {
		t.Fatal(err)
	}
	repo := store.NewSignalExecutionRepo()
	e := newTestExecution(sig.ID, 0, 0)
	e.SourceID = src.ID
	e.Symbol = "ATTRTESTUSDT"
	e.PositionID = "ATTRTESTUSDT-LONG"
	if err := repo.Create(e); err != nil {
		t.Fatal(err)
	}
	sigTPSLManager.attach(e)
	defer sigTPSLManager.detach(e.ID)
	fillOrderAndUpdatePortfolio(map[string]any{
		"symbol": "ATTRTESTUSDT", "side": "BUY", "type": "MARKET", "price": 100.0, "quantity": 1.0,
		"market_type": "swap", "position_side": "LONG", "margin_mode": "cross", "leverage": 1.0,
	})

	TickExecutorTPSL("ATTRTESTUSDT", 121) // 越过 TP1/2/3 逐档推进到全平
	reload, _ := repo.GetByID(e.ID)
	if reload == nil || reload.Status != "closed" {
		t.Fatalf("execution should be closed: %+v", reload)
	}
	if reload.RealizedPnL <= 0 {
		t.Fatalf("expected profitable close, pnl=%.4f", reload.RealizedPnL)
	}
	// 钱轨：订阅者归因行落库；pending_share 兼容字段照旧累计
	if cnt := copyPnLCount(t, p.ID, follower); cnt != 1 {
		t.Fatalf("expected 1 copy pnl row for follower, got %d", cnt)
	}
	sub, err := srcRepo.GetSubscription(src.ID, follower)
	if err != nil || sub.PendingShare <= 0 {
		t.Fatalf("pending_share legacy accrual must stay: %+v err=%v", sub, err)
	}
}
