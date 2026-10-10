package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/marketplace"
	"github.com/xiaotian-quant/gateway/internal/store"
)

/* ── 市场条目订阅扣费链路 API 测试（迁移 0059）───────────────────
 * 覆盖：free/profit_share 直达 active；monthly 创建 pending_payment +
 * billing 订单（金额取条目快照而非请求体）；重复订阅幂等（含待支付订单
 * 重放）；作者自订阅 400；非 listed 409；未登录 401；取消与越权；
 * paid 到账激活（幂等 + 续费顺延走 store 层事务测试）。
 * ─────────────────────────────────────────────────────────────── */

func marketSubTestRouter(userID int, role string) *gin.Engine {
	r := setupRouter()
	withUser := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if userID > 0 {
				c.Set("user_id", userID)
				c.Set("role", role)
			}
			h(c)
		}
	}
	r.POST("/market/listings/:id/subscribe", withUser(MarketListingSubscribe))
	r.GET("/market/my-subscriptions", withUser(MarketMySubscriptions))
	r.POST("/market/my-subscriptions/:id/cancel", withUser(MarketSubscriptionCancel))
	r.POST("/market/my-subscriptions/:id/instantiate", withUser(MarketSubscriptionInstantiate))
	return r
}

// seedSubListing 直接落库一条指定状态/收费模式的条目（绕过状态机，测订阅链路）。
func seedSubListing(t *testing.T, id string, authorID int64, status, feeModel string, monthlyFee float64) {
	t.Helper()
	rec := &store.MarketListingRecord{
		ID: id, AuthorUserID: authorID, ObjectKind: marketplace.ObjectKindAIBot, ObjectID: "aibot-" + id,
		Kind: "robot", Name: "条目-" + id, FeeModel: feeModel, MonthlyFee: monthlyFee, Status: status,
	}
	if err := store.NewMarketListingRepo().Create(rec); err != nil {
		t.Fatalf("seed listing %s: %v", id, err)
	}
}

func TestMarketSubscribeFreeFlow(t *testing.T) {
	uid := billingSeedUser(t, "mkt_sub_free_user")
	author := billingSeedUser(t, "mkt_sub_free_author")
	seedSubListing(t, "ml-api-free", int64(author), marketplace.StatusListed, "free", 0)
	r := marketSubTestRouter(uid, "user")

	// free → 200 + active，无订单
	w, resp := marketDo(t, r, "POST", "/market/listings/ml-api-free/subscribe", "")
	assertEq(t, w.Code, http.StatusOK, "free subscribe")
	sub, ok := resp["subscription"].(map[string]any)
	assertTrue(t, ok, "subscription in response")
	assertTrue(t, sub["status"] == "active", "free subscription should be active")
	assertTrue(t, sub["listing_id"] == "ml-api-free", "listing_id")
	assertTrue(t, resp["already"] == false, "first subscribe already=false")
	assertTrue(t, resp["order"] == nil, "free subscription must not create order")
	subID := int(sub["id"].(float64))

	// 重复订阅 → 幂等返回既有订阅
	w, resp = marketDo(t, r, "POST", "/market/listings/ml-api-free/subscribe", "")
	assertEq(t, w.Code, http.StatusOK, "repeat subscribe")
	assertTrue(t, resp["already"] == true, "repeat subscribe already=true")
	sub2 := resp["subscription"].(map[string]any)
	assertTrue(t, int(sub2["id"].(float64)) == subID, "repeat subscribe returns same row")

	// 我的订阅：关联条目摘要
	w, resp = marketDo(t, r, "GET", "/market/my-subscriptions", "")
	assertEq(t, w.Code, http.StatusOK, "my-subscriptions")
	subs := resp["subscriptions"].([]any)
	assertTrue(t, len(subs) == 1, "one subscription")
	item := subs[0].(map[string]any)
	assertTrue(t, item["listing_name"] == "条目-ml-api-free", "listing summary name")
	assertTrue(t, item["fee_model"] == "free" && item["listing_status"] == "listed", "listing summary fields")

	// 取消 → cancelled
	w, resp = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/cancel", subID), "")
	assertEq(t, w.Code, http.StatusOK, "cancel")
	assertTrue(t, resp["status"] == "cancelled", "cancelled status")
	got := store.GetMarketSubscriptionByID(subID)
	assertTrue(t, got.Status == store.MarketSubCancelled, "store row cancelled")
}

func TestMarketSubscribeMonthlyOrder(t *testing.T) {
	t.Setenv("USDT_TRC20_ADDRESS", tronTestB58())
	uid := billingSeedUser(t, "mkt_sub_monthly_user")
	author := billingSeedUser(t, "mkt_sub_monthly_author")
	seedSubListing(t, "ml-api-monthly", int64(author), marketplace.StatusListed, "monthly", 49.5)
	r := marketSubTestRouter(uid, "user")

	// monthly → pending_payment + billing 订单；请求体价格字段一律忽略，
	// 金额取条目快照 49.5 USDT = 49_500_000 微单位。
	w, resp := marketDo(t, r, "POST", "/market/listings/ml-api-monthly/subscribe",
		`{"chain":"trc20","monthly_fee":0.01,"fee_percent":99}`)
	assertEq(t, w.Code, http.StatusOK, "monthly subscribe")
	sub := resp["subscription"].(map[string]any)
	assertTrue(t, sub["status"] == "pending_payment", "monthly subscription pending_payment")
	order, ok := resp["order"].(map[string]any)
	assertTrue(t, ok, "order in response")
	assertTrue(t, order["amount_usdt"].(float64) == 49_500_000, "amount from listing snapshot, not request body")
	assertTrue(t, order["purpose"] == store.BillingPurposeMarketSubscription, "order purpose")
	assertTrue(t, order["plan_id"] == billingListingSubPlanID, "order plan_id marker")
	assertTrue(t, order["ref_id"] == fmt.Sprint(int(sub["id"].(float64))), "order ref_id = subscription id")
	assertTrue(t, order["status"] == "pending" && order["chain"] == "TRC20", "order pending on TRC20")
	assertTrue(t, order["address"] != "", "order address configured")
	assertTrue(t, resp["period_days"].(float64) == marketplace.SubscriptionPeriodDays, "period_days")
	orderID := order["order_id"].(string)

	// 重复订阅 → 幂等：既有 pending_payment 订阅 + 既有待支付订单重放
	w, resp = marketDo(t, r, "POST", "/market/listings/ml-api-monthly/subscribe", `{"chain":"TRC20"}`)
	assertEq(t, w.Code, http.StatusOK, "repeat monthly subscribe")
	assertTrue(t, resp["already"] == true, "already=true")
	assertTrue(t, resp["order"].(map[string]any)["order_id"] == orderID, "replay same pending order")

	// paid 到账联动：激活订阅并设 expires_at=+30 天（幂等）。
	repo := store.NewBillingRepo()
	o, err := repo.GetByID(orderID)
	assertTrue(t, err == nil, "load order")
	now := time.Now().Unix()
	assertTrue(t, billingGrantOrder(o), "first grant")
	subID, err := strconv.Atoi(o.RefID)
	assertTrue(t, err == nil && subID > 0, "order ref_id parses to subscription id")
	gotSub := store.GetMarketSubscriptionByID(subID)
	assertTrue(t, gotSub.Status == store.MarketSubActive, "subscription active after paid")
	assertTrue(t, gotSub.ExpiresAt >= now+29*86400 && gotSub.ExpiresAt <= now+31*86400,
		fmt.Sprintf("expires_at ≈ now+30d, got %d (now=%d)", gotSub.ExpiresAt, now))
	// 幂等：二次发放拒绝，到期时间不变
	assertTrue(t, !billingGrantOrder(o), "second grant idempotent")
	again := store.GetMarketSubscriptionByID(subID)
	assertTrue(t, again.ExpiresAt == gotSub.ExpiresAt, "expires_at unchanged after idempotent grant")
}

func TestMarketSubscribeGuards(t *testing.T) {
	uid := billingSeedUser(t, "mkt_sub_guard_user")
	author := billingSeedUser(t, "mkt_sub_guard_author")
	seedSubListing(t, "ml-api-self", int64(author), marketplace.StatusListed, "free", 0)
	seedSubListing(t, "ml-api-draft", int64(author), marketplace.StatusDraft, "free", 0)
	seedSubListing(t, "ml-api-monthly2", int64(author), marketplace.StatusListed, "monthly", 10)
	r := marketSubTestRouter(uid, "user")

	// 作者订阅自己的条目 → 400
	authorRouter := marketSubTestRouter(author, "user")
	w, _ := marketDo(t, authorRouter, "POST", "/market/listings/ml-api-self/subscribe", "")
	assertEq(t, w.Code, http.StatusBadRequest, "self subscribe must be 400")

	// 非 listed 条目 → 409
	w, _ = marketDo(t, r, "POST", "/market/listings/ml-api-draft/subscribe", "")
	assertEq(t, w.Code, http.StatusConflict, "draft subscribe must be 409")

	// 条目不存在 → 404
	w, _ = marketDo(t, r, "POST", "/market/listings/ml-nope/subscribe", "")
	assertEq(t, w.Code, http.StatusNotFound, "missing listing must be 404")

	// monthly 条目但链未配置 → 400（订阅保持 pending_payment，可换链重试）
	w, _ = marketDo(t, r, "POST", "/market/listings/ml-api-monthly2/subscribe", `{"chain":"BEP20"}`)
	assertEq(t, w.Code, http.StatusBadRequest, "unconfigured chain must be 400")
	assertTrue(t, store.FindMarketSubscription(uid, "ml-api-monthly2") != nil, "pending_payment subscription survives")

	// 未登录 → 401
	anon := marketSubTestRouter(0, "")
	w, _ = marketDo(t, anon, "POST", "/market/listings/ml-api-self/subscribe", "")
	assertEq(t, w.Code, http.StatusUnauthorized, "anonymous subscribe must be 401")
	w, _ = marketDo(t, anon, "GET", "/market/my-subscriptions", "")
	assertEq(t, w.Code, http.StatusUnauthorized, "anonymous my-subscriptions must be 401")
}

func TestMarketSubscriptionCancelGuards(t *testing.T) {
	uid := billingSeedUser(t, "mkt_sub_cancel_user")
	other := billingSeedUser(t, "mkt_sub_cancel_other")
	author := billingSeedUser(t, "mkt_sub_cancel_author")
	seedSubListing(t, "ml-api-cancel", int64(author), marketplace.StatusListed, "free", 0)

	r := marketSubTestRouter(uid, "user")
	w, resp := marketDo(t, r, "POST", "/market/listings/ml-api-cancel/subscribe", "")
	assertEq(t, w.Code, http.StatusOK, "subscribe")
	subID := int(resp["subscription"].(map[string]any)["id"].(float64))

	// 他人取消 → 403
	otherRouter := marketSubTestRouter(other, "user")
	w, _ = marketDo(t, otherRouter, "POST", fmt.Sprintf("/market/my-subscriptions/%d/cancel", subID), "")
	assertEq(t, w.Code, http.StatusForbidden, "cancel other's subscription must be 403")

	// 非法/不存在 id → 400/404
	w, _ = marketDo(t, r, "POST", "/market/my-subscriptions/abc/cancel", "")
	assertEq(t, w.Code, http.StatusBadRequest, "invalid id must be 400")
	w, _ = marketDo(t, r, "POST", "/market/my-subscriptions/999999/cancel", "")
	assertEq(t, w.Code, http.StatusNotFound, "missing subscription must be 404")

	// 本人取消 → cancelled
	w, _ = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/cancel", subID), "")
	assertEq(t, w.Code, http.StatusOK, "own cancel")
	assertTrue(t, store.GetMarketSubscriptionByID(subID).Status == store.MarketSubCancelled, "cancelled")
}

/* ── 订阅实例化 API（迁移 0060）─────────────────────────────────
 * 覆盖：ai_bot/grid_bot 双类型克隆（属主切换 + origin 溯源 + stopped/paper）、
 * 幂等返回既有实例、属主/状态守卫、my-subscriptions 带出 instance_id。
 * ─────────────────────────────────────────────────────────────── */

// seedInstantiableListing 落库 listed 条目 + 作者源实例（ai_bot）。
func seedInstantiableListing(t *testing.T, listingID string, authorID int, feeModel string, feePercent float64) {
	t.Helper()
	now := time.Now().Unix()
	store.SaveAIBotInstance(map[string]any{
		"id": "aibot-" + listingID, "user_id": authorID, "name": "源实例-" + listingID,
		"strategy_type": "ai_alpha", "symbol": "BTCUSDT", "market_type": "spot",
		"status": "running", "execution_mode": "paper", "config_json": `{"confidence_threshold":65}`,
		"initial_balance": 5000.0, "created_at": now, "updated_at": now,
	})
	seedSubListing(t, listingID, int64(authorID), marketplace.StatusListed, feeModel, 0)
	rec := store.NewMarketListingRepo().GetByID(listingID)
	rec.FeePercent = feePercent
	if err := store.NewMarketListingRepo().Update(rec); err != nil {
		t.Fatalf("set fee_percent: %v", err)
	}
}

func TestMarketSubscriptionInstantiateAIBot(t *testing.T) {
	uid := billingSeedUser(t, "mkt_inst_ai_user")
	author := billingSeedUser(t, "mkt_inst_ai_author")
	seedInstantiableListing(t, "ml-api-inst-ai", author, "profit_share", 20)
	r := marketSubTestRouter(uid, "user")

	// 先订阅（profit_share 直达 active）
	w, resp := marketDo(t, r, "POST", "/market/listings/ml-api-inst-ai/subscribe", "")
	assertEq(t, w.Code, http.StatusOK, "subscribe")
	subID := int(resp["subscription"].(map[string]any)["id"].(float64))

	// 实例化 → 克隆新实例
	w, resp = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/instantiate", subID), "")
	assertEq(t, w.Code, http.StatusOK, "instantiate")
	assertTrue(t, resp["kind"] == "ai_bot", "kind ai_bot")
	assertTrue(t, resp["already"] == false, "first instantiate already=false")
	instID, _ := resp["instance_id"].(string)
	assertTrue(t, instID != "", "instance_id present")

	inst := store.GetAIBotInstanceByID(instID, uid)
	assertTrue(t, inst != nil, "cloned instance persisted")
	assertTrue(t, getString(inst, "origin_listing_id", "") == "ml-api-inst-ai", "origin_listing_id")
	assertTrue(t, getString(inst, "status", "") == "stopped", "cloned instance stopped")
	assertTrue(t, getString(inst, "config_json", "") == `{"confidence_threshold":65}`, "config cloned")

	// 幂等：重复 instantiate 返回同一实例
	w, resp = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/instantiate", subID), "")
	assertEq(t, w.Code, http.StatusOK, "repeat instantiate")
	assertTrue(t, resp["already"] == true, "repeat already=true")
	assertTrue(t, resp["instance_id"] == instID, "same instance id")

	// my-subscriptions 带出 instance_id（前端三态用）
	w, resp = marketDo(t, r, "GET", "/market/my-subscriptions", "")
	assertEq(t, w.Code, http.StatusOK, "my-subscriptions")
	item := resp["subscriptions"].([]any)[0].(map[string]any)
	assertTrue(t, item["instance_id"] == instID, "instance_id in my-subscriptions")
}

func TestMarketSubscriptionInstantiateGridBot(t *testing.T) {
	uid := billingSeedUser(t, "mkt_inst_grid_user")
	author := billingSeedUser(t, "mkt_inst_grid_author")
	grepo := store.NewGridRepo()
	src := &store.GridBotRecord{
		UserID: int64(author), Name: "源网格", Symbol: "ETHUSDT", LowerPrice: 2000, UpperPrice: 3000,
		GridCount: 10, Investment: 2000,
	}
	assertTrue(t, grepo.Create(src) == nil, "seed source grid bot")
	rec := &store.MarketListingRecord{
		ID: "ml-api-inst-grid", AuthorUserID: int64(author), ObjectKind: marketplace.ObjectKindGridBot,
		ObjectID: src.ID, Kind: "robot", Name: "网格条目", FeeModel: "profit_share",
		FeePercent: 15, Status: marketplace.StatusListed,
	}
	assertTrue(t, store.NewMarketListingRepo().Create(rec) == nil, "seed grid listing")
	r := marketSubTestRouter(uid, "user")

	w, resp := marketDo(t, r, "POST", "/market/listings/ml-api-inst-grid/subscribe", "")
	assertEq(t, w.Code, http.StatusOK, "subscribe")
	subID := int(resp["subscription"].(map[string]any)["id"].(float64))

	w, resp = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/instantiate", subID), "")
	assertEq(t, w.Code, http.StatusOK, "instantiate grid")
	assertTrue(t, resp["kind"] == "grid_bot" && resp["already"] == false, "grid clone")
	botID, _ := resp["instance_id"].(string)
	bot, err := grepo.GetByID(botID)
	assertTrue(t, err == nil && bot != nil, "cloned grid bot persisted")
	assertTrue(t, bot.UserID == int64(uid) && bot.OriginListingID == "ml-api-inst-grid", "ownership/origin")
	assertTrue(t, bot.Status == "stopped" && bot.Exchange == "paper", "stopped+paper")
	assertTrue(t, bot.GridCount == 10 && bot.Investment == 2000, "grid config cloned")

	// 幂等
	w, resp = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/instantiate", subID), "")
	assertTrue(t, w.Code == http.StatusOK && resp["already"] == true && resp["instance_id"] == botID, "idempotent")
}

func TestMarketSubscriptionInstantiateGuards(t *testing.T) {
	uid := billingSeedUser(t, "mkt_inst_guard_user")
	other := billingSeedUser(t, "mkt_inst_guard_other")
	author := billingSeedUser(t, "mkt_inst_guard_author")
	seedInstantiableListing(t, "ml-api-inst-guard", author, "free", 0)
	r := marketSubTestRouter(uid, "user")

	// 未登录 → 401
	anon := marketSubTestRouter(0, "")
	w, _ := marketDo(t, anon, "POST", "/market/my-subscriptions/1/instantiate", "")
	assertEq(t, w.Code, http.StatusUnauthorized, "anonymous must be 401")

	// 不存在/非法 id → 404/400
	w, _ = marketDo(t, r, "POST", "/market/my-subscriptions/999999/instantiate", "")
	assertEq(t, w.Code, http.StatusNotFound, "missing subscription 404")
	w, _ = marketDo(t, r, "POST", "/market/my-subscriptions/abc/instantiate", "")
	assertEq(t, w.Code, http.StatusBadRequest, "invalid id 400")

	// 订阅 → active
	w, resp := marketDo(t, r, "POST", "/market/listings/ml-api-inst-guard/subscribe", "")
	assertEq(t, w.Code, http.StatusOK, "subscribe")
	subID := int(resp["subscription"].(map[string]any)["id"].(float64))

	// 他人实例化 → 403
	otherRouter := marketSubTestRouter(other, "user")
	w, _ = marketDo(t, otherRouter, "POST", fmt.Sprintf("/market/my-subscriptions/%d/instantiate", subID), "")
	assertEq(t, w.Code, http.StatusForbidden, "instantiate other's subscription must be 403")

	// 取消后实例化 → 409
	w, _ = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/cancel", subID), "")
	assertEq(t, w.Code, http.StatusOK, "cancel")
	w, _ = marketDo(t, r, "POST", fmt.Sprintf("/market/my-subscriptions/%d/instantiate", subID), "")
	assertEq(t, w.Code, http.StatusConflict, "cancelled subscription instantiate must be 409")
}
