package handler

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/marketplace"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 市场条目订阅扣费链路（迁移 0059）──
// 订阅关系存 ai_bot_subscriptions（listing_id 非空行），收费模式快照以
// xt_market_listings 记录为准（请求体的价格字段一律忽略）。
// monthly 条目：订阅置 pending_payment + billing 订单（purpose=market_subscription、
// ref_id=订阅 id、plan_id=market_listing_sub），链上核验 paid 后由
// billingGrantListingSubscription 单事务激活并顺延 30 天。

// billingListingSubPlanID 市场条目订阅订单的 plan_id 标识（与社交 provider
// 订阅订单的 market_sub 区分，billingGrantOrder 据此分派发放路径）。
const billingListingSubPlanID = "market_listing_sub"

// MarketListingSubscribe 订阅市场条目。
// POST /api/market/listings/:id/subscribe  body: {"chain":"TRC20"}（仅 monthly 用）
func MarketListingSubscribe(c *gin.Context) {
	uid, ok := marketUserID(c)
	if !ok {
		return
	}
	var req struct {
		Chain string `json:"chain"`
	}
	_ = c.ShouldBindJSON(&req) // 允许空 body（free/profit_share 不需要链）

	sub, created, err := marketSvc.Subscribe(int64(uid), c.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, marketplace.ErrListingNotFound):
			marketError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, marketplace.ErrSelfSubscription):
			marketError(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, marketplace.ErrListingNotSubscribable):
			marketError(c, http.StatusConflict, err.Error())
		default:
			marketError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	resp := gin.H{"subscription": sub, "already": !created}
	if sub.Status == store.MarketSubPendingPayment {
		order, err := marketListingOrder(c, int64(uid), sub, req.Chain, created)
		if err != nil {
			return // 响应已写出；订阅保持 pending_payment，重试 subscribe 幂等续单
		}
		resp["order"] = order
		resp["expires_at"] = order.CreatedAt + billingOrderTTL
		resp["period_days"] = marketplace.SubscriptionPeriodDays
	}
	c.JSON(http.StatusOK, resp)
}

// marketListingOrder 为 pending_payment 订阅取/建计费订单：幂等重放返回既有
// 待支付订单；否则按条目月费快照金额（微单位整数）创建新订单。
func marketListingOrder(c *gin.Context, userID int64, sub *store.MarketSubscriptionRecord, chain string, created bool) (*store.BillingOrder, error) {
	refID := strconv.Itoa(sub.ID)
	if !created {
		if exist, err := billingRepo.FindPendingByRef(userID, store.BillingPurposeMarketSubscription, refID); err == nil && exist.ID != "" {
			return exist, nil
		}
	}
	chain = strings.ToUpper(strings.TrimSpace(chain))
	if chain == "" {
		chain = "TRC20"
	}
	if !chainAdapterAvailable(chain) {
		marketError(c, http.StatusBadRequest, "unsupported chain or address not configured")
		return nil, errors.New("chain unavailable")
	}
	amountMicro := int64(math.Round(sub.MonthlyFee * 1e6))
	if amountMicro <= 0 {
		marketError(c, http.StatusBadRequest, "listing monthly_fee is not configured")
		return nil, errors.New("monthly_fee not configured")
	}
	o := &store.BillingOrder{
		ID:          newBillingOrderID(),
		UserID:      userID,
		PlanID:      billingListingSubPlanID,
		Purpose:     store.BillingPurposeMarketSubscription,
		RefID:       refID,
		Chain:       chain,
		Address:     chainAddress(chain),
		AmountMicro: amountMicro,
		Status:      store.BillingStatusPending,
		CreatedAt:   time.Now().Unix(),
	}
	if err := billingRepo.Create(o); err != nil {
		marketError(c, http.StatusInternalServerError, "create order failed")
		return nil, err
	}
	return o, nil
}

// MarketMySubscriptions 当前用户全部市场条目订阅 + 关联条目摘要。
// GET /api/market/my-subscriptions
func MarketMySubscriptions(c *gin.Context) {
	uid, ok := marketUserID(c)
	if !ok {
		return
	}
	out := []gin.H{}
	for _, sub := range store.ListMarketSubscriptionsByUser(uid) {
		item := gin.H{
			"id":           sub.ID,
			"user_id":      sub.UserID,
			"listing_id":   sub.ListingID,
			"fee_type":     sub.FeeType,
			"fee_percent":  sub.FeePercent,
			"monthly_fee":  sub.MonthlyFee,
			"status":       sub.Status,
			"expires_at":   sub.ExpiresAt,
			"created_at":   sub.CreatedAt,
			"listing_name": "",
		}
		if l := marketSvc.Repo().GetByID(sub.ListingID); l != nil {
			item["listing_name"] = l.Name
			item["object_kind"] = l.ObjectKind
			item["fee_model"] = l.FeeModel
			item["listing_status"] = l.Status
			// 实例化溯源：该订阅克隆出的实例 id（详情页"创建实例"按钮三态用）。
			switch l.ObjectKind {
			case "", marketplace.ObjectKindAIBot:
				if inst := store.FindAIBotInstanceByOrigin(uid, l.ID); inst != nil {
					item["instance_id"] = getString(inst, "id", "")
				}
			case marketplace.ObjectKindGridBot:
				if bot, err := gridBotRepo.GetByOrigin(int64(uid), l.ID); err == nil && bot != nil {
					item["instance_id"] = bot.ID
				}
			}
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": out})
}

// MarketSubscriptionCancel 取消市场条目订阅（cancelled 语义；已付不退，到期失效）。
// 复用 ai_bot_subscriptions 的取消写路径。
// POST /api/market/my-subscriptions/:id/cancel
func MarketSubscriptionCancel(c *gin.Context) {
	uid, ok := marketUserID(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		marketError(c, http.StatusBadRequest, "invalid subscription id")
		return
	}
	sub := store.GetMarketSubscriptionByID(id)
	if sub == nil || sub.ListingID == "" {
		marketError(c, http.StatusNotFound, "subscription not found")
		return
	}
	if sub.UserID != uid {
		marketError(c, http.StatusForbidden, "forbidden: not the resource owner")
		return
	}
	store.CancelAIBotSubscription(id, uid)
	c.JSON(http.StatusOK, gin.H{"id": id, "status": store.MarketSubCancelled})
}

// MarketSubscriptionInstantiate 订阅实例化：克隆条目源对象配置为当前用户名下
// 的新实例（origin_listing_id 溯源，平仓盈利按条目分成比例进归因钱轨）。
// 幂等：重复调用返回已创建的实例。
// POST /api/market/my-subscriptions/:id/instantiate
func MarketSubscriptionInstantiate(c *gin.Context) {
	uid, ok := marketUserID(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		marketError(c, http.StatusBadRequest, "invalid subscription id")
		return
	}
	sub := store.GetMarketSubscriptionByID(id)
	if sub == nil || sub.ListingID == "" {
		marketError(c, http.StatusNotFound, "subscription not found")
		return
	}
	if sub.UserID != uid {
		marketError(c, http.StatusForbidden, "forbidden: not the resource owner")
		return
	}
	kind, instanceID, created, err := marketSvc.Instantiate(int64(uid), sub)
	if err != nil {
		switch {
		case errors.Is(err, marketplace.ErrListingNotFound):
			marketError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, marketplace.ErrSubscriptionNotActive),
			errors.Is(err, marketplace.ErrListingNotListed),
			errors.Is(err, marketplace.ErrSourceObjectMissing):
			marketError(c, http.StatusConflict, err.Error())
		default:
			marketError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"kind": kind, "instance_id": instanceID, "already": !created})
}
