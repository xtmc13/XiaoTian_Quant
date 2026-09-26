package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 信号源订阅定价（对齐 CryptoRobotics + ai_bot_catalog fee_model）──
//
// POST /executor/sources                    创建信号源（含定价）
// POST /executor/sources/:id/subscribe      订阅（月费扣 credits / 记录账单流水）
// GET  /executor/sources/:id/subscribers    作者视角订阅列表
//
// fee_model: free | fixed_monthly | profit_share
//   - fixed_monthly: 订阅时按 monthly_fee 扣 xt_users.credits（余额体系），
//     余额不足生成 pending 账单流水，next_billing_at 记一个月后的到期时间；
//   - profit_share: 不预扣，信号执行平仓落库时按 fee_percent 累计
//     订阅的 pending_share（见 executor_tpsl.go finalizeExecution）。

// validFeeModel 校验定价模型。
func validFeeModel(m string) bool {
	switch m {
	case "free", "fixed_monthly", "profit_share":
		return true
	}
	return false
}

// ExecutorCreateSignalSource godoc
// POST /executor/sources
func ExecutorCreateSignalSource(c *gin.Context) {
	var body struct {
		Name       string      `json:"name"`
		Type       string      `json:"type"`
		FeeModel   string      `json:"fee_model"`
		FeePercent float64     `json:"fee_percent"`
		MonthlyFee float64     `json:"monthly_fee"`
		TPSL       *tpslConfig `json:"tp_sl"`
		Enabled    *bool       `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	feeModel := body.FeeModel
	if feeModel == "" {
		feeModel = "free"
	}
	if !validFeeModel(feeModel) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fee_model must be free|fixed_monthly|profit_share"})
		return
	}
	if feeModel == "profit_share" && body.FeePercent <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profit_share requires fee_percent > 0"})
		return
	}
	if feeModel == "fixed_monthly" && body.MonthlyFee <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fixed_monthly requires monthly_fee > 0"})
		return
	}
	typ := body.Type
	if typ == "" {
		typ = "webhook"
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	tpslJSON := "{}"
	if body.TPSL != nil {
		b, _ := json.Marshal(body.TPSL)
		tpslJSON = string(b)
	}
	uid, _ := ctxUserID(c)
	src := &store.SignalSource{
		Name:        body.Name,
		Type:        typ,
		OwnerUserID: uid,
		Enabled:     enabled,
		FeeModel:    feeModel,
		FeePercent:  body.FeePercent,
		MonthlyFee:  body.MonthlyFee,
		TPSLJSON:    tpslJSON,
	}
	if err := store.NewSignalSourceRepo().Create(src); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create source failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "source_id": src.ID})
}

// ExecutorSubscribeSignalSource godoc
// POST /executor/sources/:id/subscribe
func ExecutorSubscribeSignalSource(c *gin.Context) {
	id := c.Param("id")
	uid, injected := ctxUserID(c)
	if !injected || uid <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required to subscribe"})
		return
	}
	repo := store.NewSignalSourceRepo()
	src, err := repo.GetByID(id)
	if err != nil || src == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signal source not found"})
		return
	}
	if !src.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "signal source is disabled"})
		return
	}
	if existing, err := repo.GetSubscription(id, uid); err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "already subscribed"})
		return
	}

	now := time.Now()
	sub := &store.SignalSourceSubscription{
		SourceID:      src.ID,
		UserID:        uid,
		FeeModel:      src.FeeModel,
		FeePercent:    src.FeePercent,
		MonthlyFee:    src.MonthlyFee,
		NextBillingAt: 0,
		Status:        "active",
		CreatedAt:     now.UnixMilli(),
	}
	if src.FeeModel == "fixed_monthly" {
		// 接 billing 余额体系：有余额直接扣并结清账单，不足生成 pending 流水
		paid := store.DeductUserCredits(uid, src.MonthlyFee)
		bill := &store.SignalSourceBill{
			SourceID:  src.ID,
			UserID:    uid,
			BillType:  "monthly_fee",
			Amount:    src.MonthlyFee,
			Status:    "pending",
			CreatedAt: now.UnixMilli(),
		}
		if paid {
			bill.Status = "settled"
			bill.SettledAt = now.UnixMilli()
		}
		if err := repo.CreateBill(bill); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create bill failed: " + err.Error()})
			return
		}
		sub.NextBillingAt = now.AddDate(0, 1, 0).Unix()
	}
	if err := repo.CreateSubscription(sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create subscription failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"subscription_id": sub.ID,
		"fee_model":       sub.FeeModel,
		"next_billing_at": sub.NextBillingAt,
	})
}

// ExecutorSourceSubscribers godoc
// GET /executor/sources/:id/subscribers
// 作者视角：仅信号源属主或 admin 可见。
func ExecutorSourceSubscribers(c *gin.Context) {
	id := c.Param("id")
	repo := store.NewSignalSourceRepo()
	src, err := repo.GetByID(id)
	if err != nil || src == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signal source not found"})
		return
	}
	uid, injected := ctxUserID(c)
	if injected && ctxUserRole(c) != "admin" && src.OwnerUserID != 0 && src.OwnerUserID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner only"})
		return
	}
	subs, err := repo.ListSubscriptionsBySource(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if subs == nil {
		subs = []*store.SignalSourceSubscription{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "subscribers": subs})
}
