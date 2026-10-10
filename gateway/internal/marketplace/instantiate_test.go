package marketplace

import (
	"errors"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// seedActiveSub 直接落库一条 active 市场条目订阅（绕过 Subscribe，测实例化用）。
func seedActiveSub(t *testing.T, userID int, listingID, feeType string, feePercent float64) *store.MarketSubscriptionRecord {
	t.Helper()
	rec := &store.MarketSubscriptionRecord{
		UserID: userID, ListingID: listingID, FeeType: feeType, FeePercent: feePercent,
		Status: store.MarketSubActive,
	}
	if _, err := store.CreateMarketSubscription(rec); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	return rec
}

func TestInstantiateAIBotClone(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-ml-inst-ai", 7, 5000) // seedListedListing 的 ObjectID 规则
	seedListedListing(t, "ml-inst-ai", 7, "profit_share", 20, 0)
	sub := seedActiveSub(t, 100, "ml-inst-ai", "profit_share", 20)
	svc := NewService()

	kind, id, created, err := svc.Instantiate(100, sub)
	if err != nil || !created {
		t.Fatalf("instantiate: created=%v err=%v", created, err)
	}
	if kind != ObjectKindAIBot || id == "" {
		t.Fatalf("kind=%s id=%s", kind, id)
	}
	inst := store.GetAIBotInstanceByID(id, 100)
	if inst == nil {
		t.Fatalf("cloned instance %s not found", id)
	}
	if getString(inst, "origin_listing_id", "") != "ml-inst-ai" {
		t.Fatalf("origin_listing_id: %+v", inst)
	}
	// 配置克隆：symbol/strategy/initial_balance 来自源实例；
	// 归属订阅者、stopped + paper、不继承作者交易所绑定；catalog 关联条目 id。
	if getString(inst, "symbol", "") != "BTCUSDT" || getString(inst, "strategy_type", "") != "ai_alpha" {
		t.Fatalf("config clone: %+v", inst)
	}
	if getFloat(inst, "initial_balance", 0) != 5000 {
		t.Fatalf("initial_balance clone: %+v", inst)
	}
	if getString(inst, "status", "") != "stopped" || getString(inst, "execution_mode", "") != "paper" {
		t.Fatalf("cloned instance must start stopped+paper: %+v", inst)
	}
	if getString(inst, "exchange_id", "x") != "" {
		t.Fatalf("exchange binding must not be copied: %+v", inst)
	}
	if getString(inst, "catalog_id", "") != "ml-inst-ai" {
		t.Fatalf("catalog_id should be listing id: %+v", inst)
	}

	// 幂等：同订阅重复 instantiate 返回既有实例
	kind2, id2, created2, err := svc.Instantiate(100, sub)
	if err != nil || created2 || id2 != id || kind2 != ObjectKindAIBot {
		t.Fatalf("idempotent instantiate: id=%s created=%v err=%v", id2, created2, err)
	}
	if n := len(store.GetAIBotInstances(100)); n != 1 {
		t.Fatalf("expected 1 cloned instance, got %d", n)
	}
}

func TestInstantiateGridBotClone(t *testing.T) {
	setupTestDB(t)
	grepo := store.NewGridRepo()
	src := &store.GridBotRecord{
		UserID: 7, Name: "作者网格", Symbol: "ETHUSDT", LowerPrice: 2000, UpperPrice: 3000,
		GridCount: 10, Investment: 2000, Mode: "long", InitialEquity: 2000, Exchange: "binance",
	}
	if err := grepo.Create(src); err != nil {
		t.Fatalf("seed grid bot: %v", err)
	}
	rec := &store.MarketListingRecord{
		ID: "ml-inst-grid", AuthorUserID: 7, ObjectKind: ObjectKindGridBot, ObjectID: src.ID,
		Kind: "robot", Name: "网格条目", FeeModel: "profit_share", FeePercent: 15, Status: StatusListed,
	}
	if err := store.NewMarketListingRepo().Create(rec); err != nil {
		t.Fatalf("seed listing: %v", err)
	}
	sub := seedActiveSub(t, 100, "ml-inst-grid", "profit_share", 15)
	svc := NewService()

	kind, id, created, err := svc.Instantiate(100, sub)
	if err != nil || !created || kind != ObjectKindGridBot {
		t.Fatalf("instantiate grid: kind=%s created=%v err=%v", kind, created, err)
	}
	bot, err := grepo.GetByID(id)
	if err != nil || bot == nil {
		t.Fatalf("cloned grid bot %s: %v", id, err)
	}
	if bot.UserID != 100 || bot.OriginListingID != "ml-inst-grid" {
		t.Fatalf("ownership/origin: %+v", bot)
	}
	// 网格配置克隆：区间/格数/投资额/模式
	if bot.Symbol != "ETHUSDT" || bot.LowerPrice != 2000 || bot.UpperPrice != 3000 ||
		bot.GridCount != 10 || bot.Investment != 2000 || bot.Mode != "long" {
		t.Fatalf("grid config clone: %+v", bot)
	}
	// 初始 stopped + paper，不带作者运行状态/引擎状态
	if bot.Status != "stopped" || bot.Exchange != "paper" || bot.StateJSON != "{}" || bot.RealizedPnL != 0 {
		t.Fatalf("cloned grid must be fresh stopped+paper: %+v", bot)
	}

	// 幂等
	_, id2, created2, err := svc.Instantiate(100, sub)
	if err != nil || created2 || id2 != id {
		t.Fatalf("idempotent grid instantiate: id=%s created=%v err=%v", id2, created2, err)
	}
}

func TestInstantiateGuards(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-src-2", 7, 10000)
	seedListedListing(t, "ml-inst-guard", 7, "free", 0, 0)
	svc := NewService()

	// 非 active 订阅（pending_payment / cancelled）→ ErrSubscriptionNotActive
	pending := &store.MarketSubscriptionRecord{
		UserID: 100, ListingID: "ml-inst-guard", FeeType: "monthly", MonthlyFee: 10,
		Status: store.MarketSubPendingPayment,
	}
	if _, err := store.CreateMarketSubscription(pending); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if _, _, _, err := svc.Instantiate(100, pending); !errors.Is(err, ErrSubscriptionNotActive) {
		t.Fatalf("pending_payment instantiate should be ErrSubscriptionNotActive, got %v", err)
	}

	// 条目非 listed → ErrListingNotListed
	draft := &store.MarketListingRecord{
		ID: "ml-inst-draft", AuthorUserID: 7, ObjectKind: ObjectKindAIBot, ObjectID: "aibot-src-2",
		Kind: "robot", Name: "草稿", FeeModel: "free", Status: StatusDraft,
	}
	if err := store.NewMarketListingRepo().Create(draft); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	subDraft := seedActiveSub(t, 100, "ml-inst-draft", "free", 0)
	if _, _, _, err := svc.Instantiate(100, subDraft); !errors.Is(err, ErrListingNotListed) {
		t.Fatalf("draft listing instantiate should be ErrListingNotListed, got %v", err)
	}

	// 条目不存在 → ErrListingNotFound
	ghost := &store.MarketSubscriptionRecord{
		UserID: 100, ListingID: "ml-inst-gone", FeeType: "free", Status: store.MarketSubActive,
	}
	if _, err := store.CreateMarketSubscription(ghost); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}
	if _, _, _, err := svc.Instantiate(100, ghost); !errors.Is(err, ErrListingNotFound) {
		t.Fatalf("missing listing should be ErrListingNotFound, got %v", err)
	}

	// 源实例已被作者删除 → ErrSourceObjectMissing
	seedListedListing(t, "ml-inst-nosrc", 7, "free", 0, 0)
	subNoSrc := seedActiveSub(t, 100, "ml-inst-nosrc", "free", 0)
	if _, _, _, err := svc.Instantiate(100, subNoSrc); !errors.Is(err, ErrSourceObjectMissing) {
		t.Fatalf("missing source should be ErrSourceObjectMissing, got %v", err)
	}
}

// TestInstantiateAfterSubscribeFlow 走真实订阅链路（Subscribe → Instantiate）。
func TestInstantiateAfterSubscribeFlow(t *testing.T) {
	setupTestDB(t)
	seedInstance(t, "aibot-ml-inst-flow", 7, 10000)
	seedListedListing(t, "ml-inst-flow", 7, "profit_share", 20, 0)
	svc := NewService()

	sub, created, err := svc.Subscribe(100, "ml-inst-flow")
	if err != nil || !created {
		t.Fatalf("subscribe: created=%v err=%v", created, err)
	}
	kind, id, created, err := svc.Instantiate(100, sub)
	if err != nil || !created || kind != ObjectKindAIBot || id == "" {
		t.Fatalf("instantiate after subscribe: kind=%s id=%s created=%v err=%v", kind, id, created, err)
	}
}
