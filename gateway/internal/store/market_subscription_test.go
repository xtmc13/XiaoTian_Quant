package store

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestListingSubscriptionsMigration 冒烟验证 0059 迁移：ai_bot_subscriptions 增加
// listing_id/expires_at 列与索引，存量行默认空串/0（status 列建表已有，不重复加），
// 幂等重跑。
func TestListingSubscriptionsMigration(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-listing-subscriptions")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, col := range []string{"listing_id", "expires_at", "status"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('ai_bot_subscriptions') WHERE name=?`, col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("ai_bot_subscriptions missing %s column: n=%d err=%v", col, n, err)
		}
	}
	var idx string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_aibot_sub_listing'`).Scan(&idx); err != nil {
		t.Fatalf("index idx_aibot_sub_listing missing: %v", err)
	}

	// 存量形态行（不带新列的 INSERT）：listing_id 默认空串、expires_at 默认 0、status 默认 active。
	if _, err := db.Exec(`INSERT INTO ai_bot_subscriptions (user_id, bot_instance_id, fee_type, created_at)
		VALUES (1, 'aibot-legacy', 'profit_share', 1)`); err != nil {
		t.Fatalf("insert legacy subscription: %v", err)
	}
	var listingID, status string
	var expiresAt int64
	if err := db.QueryRow(`SELECT listing_id, expires_at, status FROM ai_bot_subscriptions WHERE bot_instance_id='aibot-legacy'`).
		Scan(&listingID, &expiresAt, &status); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if listingID != "" || expiresAt != 0 || status != "active" {
		t.Fatalf("legacy defaults: listing_id=%q expires_at=%d status=%q, want ''/0/active", listingID, expiresAt, status)
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}

// TestMarketSubscriptionRepo 订阅行 CRUD + 幂等查找 + 到期治理。
func TestMarketSubscriptionRepo(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().Unix()
	rec := &MarketSubscriptionRecord{
		UserID: 42, ListingID: "ml-1", FeeType: "monthly", MonthlyFee: 50,
		Status: MarketSubPendingPayment,
	}
	id, err := CreateMarketSubscription(rec)
	if err != nil || id <= 0 {
		t.Fatalf("create: id=%d err=%v", id, err)
	}

	got := GetMarketSubscriptionByID(id)
	if got == nil || got.ListingID != "ml-1" || got.Status != MarketSubPendingPayment || got.ExpiresAt != 0 {
		t.Fatalf("get: %+v", got)
	}

	// 幂等查找：pending_payment 命中；同 listing 的 active 也命中。
	if FindMarketSubscription(42, "ml-1") == nil {
		t.Fatal("find pending_payment should hit")
	}
	found := &MarketSubscriptionRecord{
		UserID: 42, ListingID: "ml-2", FeeType: "free", Status: MarketSubActive,
	}
	if _, err := CreateMarketSubscription(found); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if f := FindMarketSubscription(42, "ml-2"); f == nil || f.Status != MarketSubActive {
		t.Fatalf("find active: %+v", f)
	}
	// cancelled/expired 不命中（允许重新订阅）。
	dead := &MarketSubscriptionRecord{
		UserID: 42, ListingID: "ml-3", FeeType: "free", Status: MarketSubCancelled,
	}
	if _, err := CreateMarketSubscription(dead); err != nil {
		t.Fatalf("create third: %v", err)
	}
	if FindMarketSubscription(42, "ml-3") != nil {
		t.Fatal("cancelled subscription must not block re-subscribe")
	}
	// 他人/其他 listing 不命中。
	if FindMarketSubscription(43, "ml-1") != nil || FindMarketSubscription(42, "ml-x") != nil {
		t.Fatal("find must be scoped by user+listing")
	}

	// 列表：只含 listing 维度行（不含实例直连旧订阅）。
	if _, err := db.Exec(`INSERT INTO ai_bot_subscriptions (user_id, bot_instance_id, fee_type, status, created_at)
		VALUES (42, 'aibot-direct', 'profit_share', 'active', ?)`, now); err != nil {
		t.Fatalf("insert direct subscription: %v", err)
	}
	list := ListMarketSubscriptionsByUser(42)
	if len(list) != 3 {
		t.Fatalf("list should contain 3 listing subscriptions, got %d", len(list))
	}
	for _, s := range list {
		if s.ListingID == "" {
			t.Fatalf("direct subscription leaked into listing list: %+v", s)
		}
	}

	// 到期治理：expires_at 已过的 active 置 expired；未到期/无到期/非 active 不动。
	future := &MarketSubscriptionRecord{
		UserID: 42, ListingID: "ml-4", FeeType: "monthly", MonthlyFee: 10,
		Status: MarketSubActive, ExpiresAt: now + 3600,
	}
	if _, err := CreateMarketSubscription(future); err != nil {
		t.Fatalf("create future: %v", err)
	}
	past := &MarketSubscriptionRecord{
		UserID: 42, ListingID: "ml-5", FeeType: "monthly", MonthlyFee: 10,
		Status: MarketSubActive, ExpiresAt: now - 1,
	}
	pastID, err := CreateMarketSubscription(past)
	if err != nil {
		t.Fatalf("create past: %v", err)
	}
	n, err := ExpireMarketSubscriptions(now)
	if err != nil || n != 1 {
		t.Fatalf("expire: n=%d err=%v, want 1", n, err)
	}
	if got := GetMarketSubscriptionByID(pastID); got.Status != MarketSubExpired {
		t.Fatalf("past subscription should be expired, got %s", got.Status)
	}
	if got := GetMarketSubscriptionByID(future.ID); got.Status != MarketSubActive {
		t.Fatalf("future subscription should stay active, got %s", got.Status)
	}
	if got := GetMarketSubscriptionByID(found.ID); got.Status != MarketSubActive {
		t.Fatalf("expires_at=0 (free) subscription should stay active, got %s", got.Status)
	}
	// 幂等：再过一轮无新增。
	if n, err := ExpireMarketSubscriptions(now); err != nil || n != 0 {
		t.Fatalf("re-expire: n=%d err=%v, want 0", n, err)
	}
}

// TestGrantListingSubscriptionTx 到账激活：pending_payment→active+30 天；
// 幂等（二次发放拒绝）；续费顺延；订阅缺失整体回滚。
func TestGrantListingSubscriptionTx(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewBillingRepo()
	now := time.Now().Unix()
	subID, err := CreateMarketSubscription(&MarketSubscriptionRecord{
		UserID: 7, ListingID: "ml-1", FeeType: "monthly", MonthlyFee: 50,
		Status: MarketSubPendingPayment,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	o := &BillingOrder{
		ID: "bill_listing_grant", UserID: 7, PlanID: "market_listing_sub",
		Purpose: BillingPurposeMarketSubscription, RefID: "1", Chain: "TRC20",
		Address: "addr", AmountMicro: 50_000_000, Status: BillingStatusPending, CreatedAt: now,
	}
	o.RefID = strconv.Itoa(subID)
	if err := repo.Create(o); err != nil {
		t.Fatalf("create order: %v", err)
	}

	granted, err := repo.GrantListingSubscriptionTx(o.ID, 7, int64(subID), 30, now)
	if err != nil || !granted {
		t.Fatalf("first grant: granted=%v err=%v", granted, err)
	}
	got := GetMarketSubscriptionByID(subID)
	if got.Status != MarketSubActive {
		t.Fatalf("subscription should be active, got %s", got.Status)
	}
	if got.ExpiresAt != now+30*86400 {
		t.Fatalf("expires_at = %d, want %d (now+30d)", got.ExpiresAt, now+30*86400)
	}
	ord, err := repo.GetByID(o.ID)
	if err != nil || ord.Status != BillingStatusPaid {
		t.Fatalf("order should be paid: %+v err=%v", ord, err)
	}

	// 幂等：二次发放拒绝，到期时间不变。
	granted, err = repo.GrantListingSubscriptionTx(o.ID, 7, int64(subID), 30, now)
	if err != nil || granted {
		t.Fatalf("second grant must be idempotent: granted=%v err=%v", granted, err)
	}
	if got := GetMarketSubscriptionByID(subID); got.ExpiresAt != now+30*86400 {
		t.Fatalf("expires_at changed after idempotent grant: %d", got.ExpiresAt)
	}

	// 续费顺延：新订单 paid 后在既有到期时间上叠加 30 天。
	o2 := &BillingOrder{
		ID: "bill_listing_renew", UserID: 7, PlanID: "market_listing_sub",
		Purpose: BillingPurposeMarketSubscription, RefID: strconv.Itoa(subID), Chain: "TRC20",
		Address: "addr", AmountMicro: 50_000_000, Status: BillingStatusPending, CreatedAt: now,
	}
	if err := repo.Create(o2); err != nil {
		t.Fatalf("create renew order: %v", err)
	}
	granted, err = repo.GrantListingSubscriptionTx(o2.ID, 7, int64(subID), 30, now)
	if err != nil || !granted {
		t.Fatalf("renew grant: granted=%v err=%v", granted, err)
	}
	if got := GetMarketSubscriptionByID(subID); got.ExpiresAt != now+60*86400 {
		t.Fatalf("renewal expires_at = %d, want %d (now+60d)", got.ExpiresAt, now+60*86400)
	}

	// 订阅缺失/归属不符：整体回滚，订单不置 paid。
	o3 := &BillingOrder{
		ID: "bill_listing_missing", UserID: 7, PlanID: "market_listing_sub",
		Purpose: BillingPurposeMarketSubscription, RefID: "99999", Chain: "TRC20",
		Address: "addr", AmountMicro: 50_000_000, Status: BillingStatusPending, CreatedAt: now,
	}
	if err := repo.Create(o3); err != nil {
		t.Fatalf("create missing-ref order: %v", err)
	}
	if granted, err = repo.GrantListingSubscriptionTx(o3.ID, 7, 99999, 30, now); err == nil || granted {
		t.Fatalf("missing subscription grant must fail: granted=%v err=%v", granted, err)
	}
	if ord, _ := repo.GetByID(o3.ID); ord.Status != BillingStatusPending {
		t.Fatalf("failed grant must roll back order paid, got %s", ord.Status)
	}
}
