package marketplace

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/social"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// seedPendingReviewListing 落库一条 pending_review 条目（approve 入口形态）。
func seedPendingReviewListing(t *testing.T, id string, authorID int64, feeModel string, feePercent, monthlyFee float64) *store.MarketListingRecord {
	t.Helper()
	rec := &store.MarketListingRecord{
		ID: id, AuthorUserID: authorID, ObjectKind: ObjectKindAIBot, ObjectID: "aibot-" + id,
		Kind: "robot", Name: "条目-" + id, FeeModel: feeModel, FeePercent: feePercent,
		MonthlyFee: monthlyFee, Status: StatusPendingReview,
	}
	if err := store.NewMarketListingRepo().Create(rec); err != nil {
		t.Fatalf("seed listing %s: %v", id, err)
	}
	return rec
}

func countProviders(t *testing.T, userID int64) int {
	t.Helper()
	mkt := social.NewMarketService()
	n := 0
	// LatestProviderByUser 只给最近一条；数总量走 approved 列表按 user_id 过滤。
	for _, p := range mustListApproved(t, mkt) {
		if p.UserID == userID {
			n++
		}
	}
	return n
}

func mustListApproved(t *testing.T, mkt *social.MarketService) []*social.ProviderApply {
	t.Helper()
	list, err := mkt.ListApprovedProviders()
	if err != nil {
		t.Fatalf("list approved providers: %v", err)
	}
	return list
}

// TestApproveAutoProvisionsProvider approve 自动建档：无记录则按条目快照建档
// （approved + 钳制比例 + 昵称），provider_id 回写条目；重复 approve/补建幂等；
// 同作者第二条目复用既有建档。
func TestApproveAutoProvisionsProvider(t *testing.T) {
	setupTestDB(t)
	// 作者昵称建档（store.CreateUser 落 xt_users）
	authorID, err := store.CreateUser("attr_author", "pw", "小天作者", "", "user")
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	seedInstance(t, "aibot-attr-1", authorID, 10000)
	svc := NewService()
	now := time.Now()

	l := seedPendingReviewListing(t, "ml-attr-approve", int64(authorID), "profit_share", 20, 0)
	l.ObjectID = "aibot-attr-1"
	l.BotInstanceID = "aibot-attr-1"
	if err := svc.Approve(l, 1, now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if l.ProviderID <= 0 {
		t.Fatalf("provider_id should be written back, got %+v", l)
	}
	// 落库回读确认
	got := store.NewMarketListingRepo().GetByID(l.ID)
	if got == nil || got.ProviderID != l.ProviderID {
		t.Fatalf("provider_id persisted: %+v", got)
	}
	mkt := social.NewMarketService()
	p, err := mkt.GetProvider(l.ProviderID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if p.UserID != int64(authorID) || p.Name != "小天作者" || p.ApplyStatus != social.ApplyApproved {
		t.Fatalf("provider profile: %+v", p)
	}
	if p.ProfitSharePct == nil || *p.ProfitSharePct != 20 || p.FeeMode != social.FeeModeProfitShare {
		t.Fatalf("provider pricing from listing snapshot: %+v", p)
	}

	// 幂等：再次 ensure 不新建
	pid, err := EnsureListingProvider(got)
	if err != nil || pid != l.ProviderID {
		t.Fatalf("ensure idempotent: pid=%d err=%v", pid, err)
	}
	if n := countProviders(t, int64(authorID)); n != 1 {
		t.Fatalf("expected 1 provider row, got %d", n)
	}

	// 同作者第二条目复用既有建档（比例仍按条目快照建档时的值，不改写）
	l2 := seedPendingReviewListing(t, "ml-attr-approve-2", int64(authorID), "profit_share", 30, 0)
	l2.ObjectID = "aibot-attr-1"
	l2.BotInstanceID = "aibot-attr-1"
	if err := svc.Approve(l2, 1, now); err != nil {
		t.Fatalf("approve second listing: %v", err)
	}
	if l2.ProviderID != l.ProviderID {
		t.Fatalf("second listing should reuse provider %d, got %d", l.ProviderID, l2.ProviderID)
	}
}

// TestEnsureListingProviderClamp 分成比例钳制沿用 B 栈双轨区间 10-35；
// monthly 条目建档带月费；free 条目建档比例为空（结算空转）。
func TestEnsureListingProviderClamp(t *testing.T) {
	setupTestDB(t)
	mkt := social.NewMarketService()

	low := seedPendingReviewListing(t, "ml-attr-low", 11, "profit_share", 5, 0)
	if _, err := EnsureListingProvider(low); err != nil {
		t.Fatalf("ensure low: %v", err)
	}
	p, err := mkt.GetProvider(low.ProviderID)
	if err != nil || p.ProfitSharePct == nil || *p.ProfitSharePct != social.DualTrackMinSharePct {
		t.Fatalf("clamp low to %.0f: %+v err=%v", social.DualTrackMinSharePct, p, err)
	}

	high := seedPendingReviewListing(t, "ml-attr-high", 12, "profit_share", 50, 0)
	if _, err := EnsureListingProvider(high); err != nil {
		t.Fatalf("ensure high: %v", err)
	}
	p, err = mkt.GetProvider(high.ProviderID)
	if err != nil || p.ProfitSharePct == nil || *p.ProfitSharePct != social.DualTrackMaxSharePct {
		t.Fatalf("clamp high to %.0f: %+v err=%v", social.DualTrackMaxSharePct, p, err)
	}

	monthly := seedPendingReviewListing(t, "ml-attr-monthly", 13, "monthly", 0, 49.5)
	if _, err := EnsureListingProvider(monthly); err != nil {
		t.Fatalf("ensure monthly: %v", err)
	}
	p, err = mkt.GetProvider(monthly.ProviderID)
	if err != nil || p.MonthlyFee != 49.5 || p.FeeMode != social.FeeModeMonthly {
		t.Fatalf("monthly provider: %+v err=%v", p, err)
	}

	free := seedPendingReviewListing(t, "ml-attr-free", 14, "free", 0, 0)
	if _, err := EnsureListingProvider(free); err != nil {
		t.Fatalf("ensure free: %v", err)
	}
	p, err = mkt.GetProvider(free.ProviderID)
	if err != nil || p.ProfitSharePct != nil {
		t.Fatalf("free provider must carry no share pct: %+v err=%v", p, err)
	}
}

// seedAttributionFixture 组装归因闭环：作者建档条目（listed+provider）+
// 订阅者 active profit_share 订阅。返回条目与 provider。
func seedAttributionFixture(t *testing.T, listingID string, authorID, subscriberID int64, feePercent float64) (*store.MarketListingRecord, *social.ProviderApply) {
	t.Helper()
	l := seedPendingReviewListing(t, listingID, authorID, "profit_share", feePercent, 0)
	l.Status = StatusListed
	if err := store.NewMarketListingRepo().Update(l); err != nil {
		t.Fatalf("list listing: %v", err)
	}
	if _, err := EnsureListingProvider(l); err != nil {
		t.Fatalf("ensure provider: %v", err)
	}
	if _, err := store.CreateMarketSubscription(&store.MarketSubscriptionRecord{
		UserID: int(subscriberID), ListingID: listingID, FeeType: "profit_share",
		FeePercent: feePercent, Status: store.MarketSubActive,
	}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	p, err := social.NewMarketService().GetProvider(l.ProviderID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	return l, p
}

func countCopyPnL(t *testing.T, providerID, followerID int64) int64 {
	t.Helper()
	_, cnt, err := social.NewMarketService().SumCopyPnL(providerID, followerID, 0, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("sum copy pnl: %v", err)
	}
	return cnt
}

// TestAttributionClosedLoop 端到端闭环：克隆实例平仓盈利归因 → xt_social_copy_pnl
// 落行 → SettleWindow 产出 pending 快照（金额=pnl×pct，引擎算比例）→
// Sweep 拨时间 pending→locked→payable → 提现可用额 = payable − 在途提现。
func TestAttributionClosedLoop(t *testing.T) {
	setupTestDB(t)
	l, p := seedAttributionFixture(t, "ml-loop", 7, 100, 20)
	mkt := social.NewMarketService()

	// 平仓盈利 1000 → 归因落行（同时补建分成轨订阅行）
	if err := AttributeListingPnL(l.ID, 100, 1000, "ai_bot:aibot-clone-x"); err != nil {
		t.Fatalf("attribute pnl: %v", err)
	}
	if cnt := countCopyPnL(t, p.ID, 100); cnt != 1 {
		t.Fatalf("copy pnl rows: %d, want 1", cnt)
	}

	// 结算当日窗口：快照金额由引擎按 provider 比例计算（1000 × 20% = 200）
	day := time.Now().Format("2006-01-02")
	eng := social.NewSettlementEngine(mkt)
	eng.SetLogf(func(string, ...any) {})
	if err := eng.SettleWindow(day); err != nil {
		t.Fatalf("settle window: %v", err)
	}
	snaps, err := mkt.PendingSnapshots(p.ID, 100)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("pending snapshots: len=%d err=%v", len(snaps), err)
	}
	snap := snaps[0]
	if snap.CopiedPnl != 1000 || snap.SharePct != 20 || snap.ShareAmount != 200 {
		t.Fatalf("snapshot amounts: %+v", snap)
	}
	if snap.Status != social.SnapStatusPending {
		t.Fatalf("snapshot status: %s", snap.Status)
	}

	// T+3 锁定到期 → locked
	dayStart, _ := time.ParseInLocation("2006-01-02", day, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)
	toLocked, toPayable, err := eng.Sweep(dayEnd.Add(72*time.Hour + time.Minute))
	if err != nil || toLocked != 1 || toPayable != 0 {
		t.Fatalf("sweep to locked: locked=%d payable=%d err=%v", toLocked, toPayable, err)
	}
	// locked → payable（+24h 放款准备期）
	_, toPayable, err = eng.Sweep(dayEnd.Add(72*time.Hour + 24*time.Hour + time.Minute))
	if err != nil || toPayable != 1 {
		t.Fatalf("sweep to payable: payable=%d err=%v", toPayable, err)
	}

	// 提现余额：payable 200 全额可用；申请 200 后在途占额，可用归零
	earnings, err := mkt.Earnings(7)
	if err != nil {
		t.Fatalf("earnings: %v", err)
	}
	if earnings.Payable != 200 || earnings.Available != 200 {
		t.Fatalf("earnings before withdraw: %+v", earnings)
	}
	if _, err := mkt.CreateWithdrawal(7, 200, "TRC20", "TAddr"); err != nil {
		t.Fatalf("create withdrawal: %v", err)
	}
	earnings, err = mkt.Earnings(7)
	if err != nil {
		t.Fatalf("earnings after withdraw: %v", err)
	}
	if earnings.Available != 0 || earnings.PendingWithdrawals != 200 {
		t.Fatalf("available must drop to 0 with pending withdrawal: %+v", earnings)
	}
}

// TestAttributionSkips 归因守卫：非盈利/无溯源/未建档/无有效订阅均静默跳过。
func TestAttributionSkips(t *testing.T) {
	setupTestDB(t)
	l, p := seedAttributionFixture(t, "ml-skip", 7, 100, 20)

	// pnl <= 0 不归因（亏损冲抵语义留在结算引擎，不归因层不产生亏损记录）
	if err := AttributeListingPnL(l.ID, 100, 0, ""); err != nil {
		t.Fatalf("zero pnl: %v", err)
	}
	if err := AttributeListingPnL(l.ID, 100, -50, ""); err != nil {
		t.Fatalf("negative pnl: %v", err)
	}
	// 无溯源标记 / 条目不存在 / 条目未建档
	if err := AttributeListingPnL("", 100, 100, ""); err != nil {
		t.Fatalf("empty listing: %v", err)
	}
	if err := AttributeListingPnL("ml-gone", 100, 100, ""); err != nil {
		t.Fatalf("missing listing: %v", err)
	}
	noProvider := seedPendingReviewListing(t, "ml-skip-noprovider", 8, "profit_share", 20, 0)
	if err := AttributeListingPnL(noProvider.ID, 100, 100, ""); err != nil {
		t.Fatalf("listing without provider: %v", err)
	}
	// 无订阅用户 / free 订阅用户
	if err := AttributeListingPnL(l.ID, 999, 100, ""); err != nil {
		t.Fatalf("no subscription: %v", err)
	}
	seedListedListing(t, "ml-skip-free", 7, "free", 0, 0)
	freeSub := seedActiveSub(t, 101, "ml-skip-free", "free", 0)
	_ = freeSub
	if err := AttributeListingPnL("ml-skip-free", 101, 100, ""); err != nil {
		t.Fatalf("free subscription: %v", err)
	}
	// 取消订阅后不再归因
	sub := store.FindMarketSubscription(100, l.ID)
	if sub == nil {
		t.Fatal("subscription missing")
	}
	store.CancelAIBotSubscription(sub.ID, 100)
	if err := AttributeListingPnL(l.ID, 100, 100, ""); err != nil {
		t.Fatalf("cancelled subscription: %v", err)
	}

	if cnt := countCopyPnL(t, p.ID, 100); cnt != 0 {
		t.Fatalf("no copy pnl rows expected, got %d", cnt)
	}
}

// TestAttributeSignalSourcePnL executor 孤岛收口：信号源属主已 approved 建档时，
// 平仓盈利镜像到钱轨（覆盖全部 active profit_share 订阅者）；未建档不动。
func TestAttributeSignalSourcePnL(t *testing.T) {
	setupTestDB(t)
	mkt := social.NewMarketService()

	// 属主已建档（approved，比例 20%）
	provider, err := mkt.ApplyProvider(9, "信号作者", "", 0, ptrFloat(20))
	if err != nil {
		t.Fatalf("apply provider: %v", err)
	}
	if _, err := mkt.ApproveProvider(provider.ID); err != nil {
		t.Fatalf("approve provider: %v", err)
	}

	srcRepo := store.NewSignalSourceRepo()
	src := &store.SignalSource{
		Name: "来源A", Type: "webhook", OwnerUserID: 9, Enabled: true,
		FeeModel: "profit_share", FeePercent: 20,
	}
	if err := srcRepo.Create(src); err != nil {
		t.Fatalf("create source: %v", err)
	}
	// 两个 active profit_share 订阅者 + 一个 cancelled + 一个 monthly
	for _, sub := range []*store.SignalSourceSubscription{
		{SourceID: src.ID, UserID: 201, FeeModel: "profit_share", FeePercent: 20, Status: "active"},
		{SourceID: src.ID, UserID: 202, FeeModel: "profit_share", FeePercent: 20, Status: "active"},
		{SourceID: src.ID, UserID: 203, FeeModel: "profit_share", FeePercent: 20, Status: "cancelled"},
		{SourceID: src.ID, UserID: 204, FeeModel: "fixed_monthly", MonthlyFee: 10, Status: "active"},
	} {
		if err := srcRepo.CreateSubscription(sub); err != nil {
			t.Fatalf("create source subscription: %v", err)
		}
	}

	if err := AttributeSignalSourcePnL(src, 500, "signal_executor:exec-1"); err != nil {
		t.Fatalf("attribute source pnl: %v", err)
	}
	if cnt := countCopyPnL(t, provider.ID, 201); cnt != 1 {
		t.Fatalf("follower 201 rows: %d", cnt)
	}
	if cnt := countCopyPnL(t, provider.ID, 202); cnt != 1 {
		t.Fatalf("follower 202 rows: %d", cnt)
	}
	if cnt := countCopyPnL(t, provider.ID, 203); cnt != 0 {
		t.Fatalf("cancelled follower must not be attributed, got %d", cnt)
	}
	if cnt := countCopyPnL(t, provider.ID, 204); cnt != 0 {
		t.Fatalf("monthly follower must not be attributed, got %d", cnt)
	}

	// 未建档属主 → 静默跳过
	src2 := &store.SignalSource{
		Name: "来源B", Type: "webhook", OwnerUserID: 55, Enabled: true,
		FeeModel: "profit_share", FeePercent: 20,
	}
	if err := srcRepo.Create(src2); err != nil {
		t.Fatalf("create source2: %v", err)
	}
	if err := AttributeSignalSourcePnL(src2, 500, ""); err != nil {
		t.Fatalf("unprovisioned owner must skip: %v", err)
	}
	// 非 profit_share 源 / 非盈利 → 跳过
	if err := AttributeSignalSourcePnL(&store.SignalSource{FeeModel: "fixed_monthly", FeePercent: 0}, 100, ""); err != nil {
		t.Fatalf("monthly source: %v", err)
	}
	if err := AttributeSignalSourcePnL(src, -10, ""); err != nil {
		t.Fatalf("negative pnl: %v", err)
	}
}

func ptrFloat(v float64) *float64 { return &v }
