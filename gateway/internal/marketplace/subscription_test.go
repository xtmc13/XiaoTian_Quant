package marketplace

import (
	"errors"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// seedListedListing 直接落库一条 listed 条目（绕过状态机，测试订阅链路用）。
func seedListedListing(t *testing.T, id string, authorID int64, feeModel string, feePercent, monthlyFee float64) {
	t.Helper()
	rec := &store.MarketListingRecord{
		ID: id, AuthorUserID: authorID, ObjectKind: ObjectKindAIBot, ObjectID: "aibot-" + id,
		Kind: "robot", Name: "条目-" + id, FeeModel: feeModel, FeePercent: feePercent,
		MonthlyFee: monthlyFee, Status: StatusListed,
	}
	if err := store.NewMarketListingRepo().Create(rec); err != nil {
		t.Fatalf("seed listing %s: %v", id, err)
	}
}

func TestSubscribeFreeAndProfitShare(t *testing.T) {
	setupTestDB(t)
	seedListedListing(t, "ml-sub-free", 7, "free", 0, 0)
	seedListedListing(t, "ml-sub-ps", 7, "profit_share", 20, 0)
	svc := NewService()

	// free → 直接 active（无到期）
	sub, created, err := svc.Subscribe(100, "ml-sub-free")
	if err != nil || !created {
		t.Fatalf("free subscribe: created=%v err=%v", created, err)
	}
	if sub.Status != store.MarketSubActive || sub.ExpiresAt != 0 {
		t.Fatalf("free subscription should be active without expiry: %+v", sub)
	}
	if sub.FeeType != "free" || sub.FeePercent != 0 || sub.MonthlyFee != 0 {
		t.Fatalf("fee snapshot from listing: %+v", sub)
	}

	// profit_share → 直接 active（分成计提后续阶段接）
	sub, created, err = svc.Subscribe(100, "ml-sub-ps")
	if err != nil || !created {
		t.Fatalf("profit_share subscribe: created=%v err=%v", created, err)
	}
	if sub.Status != store.MarketSubActive || sub.FeeType != "profit_share" || sub.FeePercent != 20 {
		t.Fatalf("profit_share subscription: %+v", sub)
	}
}

func TestSubscribeMonthlyPendingPayment(t *testing.T) {
	setupTestDB(t)
	seedListedListing(t, "ml-sub-monthly", 7, "monthly", 0, 49.5)
	svc := NewService()

	sub, created, err := svc.Subscribe(100, "ml-sub-monthly")
	if err != nil || !created {
		t.Fatalf("monthly subscribe: created=%v err=%v", created, err)
	}
	if sub.Status != store.MarketSubPendingPayment {
		t.Fatalf("monthly subscription should be pending_payment, got %s", sub.Status)
	}
	if sub.MonthlyFee != 49.5 {
		t.Fatalf("monthly_fee snapshot = %v, want 49.5", sub.MonthlyFee)
	}
}

func TestSubscribeIdempotent(t *testing.T) {
	setupTestDB(t)
	seedListedListing(t, "ml-sub-idem", 7, "free", 0, 0)
	svc := NewService()

	first, created, err := svc.Subscribe(100, "ml-sub-idem")
	if err != nil || !created {
		t.Fatalf("first subscribe: created=%v err=%v", created, err)
	}
	second, created, err := svc.Subscribe(100, "ml-sub-idem")
	if err != nil {
		t.Fatalf("second subscribe: %v", err)
	}
	if created {
		t.Fatal("second subscribe must not create a new row")
	}
	if second.ID != first.ID {
		t.Fatalf("second subscribe should return existing row %d, got %d", first.ID, second.ID)
	}
	// 不重复落库
	if n := len(store.ListMarketSubscriptionsByUser(100)); n != 1 {
		t.Fatalf("expected 1 subscription row, got %d", n)
	}
}

func TestSubscribeGuards(t *testing.T) {
	setupTestDB(t)
	seedListedListing(t, "ml-sub-self", 7, "free", 0, 0)
	// draft 状态条目
	draft := &store.MarketListingRecord{
		ID: "ml-sub-draft", AuthorUserID: 7, ObjectKind: ObjectKindAIBot, ObjectID: "aibot-x",
		Kind: "robot", Name: "草稿", FeeModel: "free", Status: StatusDraft,
	}
	if err := store.NewMarketListingRepo().Create(draft); err != nil {
		t.Fatalf("seed draft listing: %v", err)
	}
	svc := NewService()

	// 作者订阅自己的条目 → ErrSelfSubscription
	if _, _, err := svc.Subscribe(7, "ml-sub-self"); !errors.Is(err, ErrSelfSubscription) {
		t.Fatalf("self subscribe should be ErrSelfSubscription, got %v", err)
	}
	// 非 listed → ErrListingNotSubscribable
	if _, _, err := svc.Subscribe(100, "ml-sub-draft"); !errors.Is(err, ErrListingNotSubscribable) {
		t.Fatalf("draft subscribe should be ErrListingNotSubscribable, got %v", err)
	}
	// 条目不存在 → ErrListingNotFound
	if _, _, err := svc.Subscribe(100, "ml-nope"); !errors.Is(err, ErrListingNotFound) {
		t.Fatalf("missing listing should be ErrListingNotFound, got %v", err)
	}
}

// TestEngineExpireSubscriptions RunOnce 的到期治理：expires_at 已过的 active
// 订阅置 expired；无到期（free）与非 active 不动。
func TestEngineExpireSubscriptions(t *testing.T) {
	setupTestDB(t)
	now := time.Now()

	expiredID, err := store.CreateMarketSubscription(&store.MarketSubscriptionRecord{
		UserID: 100, ListingID: "ml-exp-1", FeeType: "monthly", MonthlyFee: 10,
		Status: store.MarketSubActive, ExpiresAt: now.Unix() - 60,
	})
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}
	freeID, err := store.CreateMarketSubscription(&store.MarketSubscriptionRecord{
		UserID: 100, ListingID: "ml-exp-2", FeeType: "free", Status: store.MarketSubActive,
	})
	if err != nil {
		t.Fatalf("create free: %v", err)
	}
	pendingID, err := store.CreateMarketSubscription(&store.MarketSubscriptionRecord{
		UserID: 100, ListingID: "ml-exp-3", FeeType: "monthly", MonthlyFee: 10,
		Status: store.MarketSubPendingPayment,
	})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}

	svc := NewService()
	eng := NewEngine(svc)
	eng.SetLogf(func(string, ...any) {})
	if err := eng.RunOnce(now); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if got := store.GetMarketSubscriptionByID(expiredID); got.Status != store.MarketSubExpired {
		t.Fatalf("expired subscription should be expired, got %s", got.Status)
	}
	if got := store.GetMarketSubscriptionByID(freeID); got.Status != store.MarketSubActive {
		t.Fatalf("free subscription (expires_at=0) should stay active, got %s", got.Status)
	}
	if got := store.GetMarketSubscriptionByID(pendingID); got.Status != store.MarketSubPendingPayment {
		t.Fatalf("pending_payment subscription should be untouched, got %s", got.Status)
	}
}
