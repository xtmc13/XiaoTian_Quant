package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// BillingOrderVerification GET /api/billing/orders/:id/verification
// 查询订单核验状态与最近一次链上核验详情（快照由后台核验器每轮 upsert）。
// 只读本人/admin 订单；快照不存在时返回 not_checked。
func BillingOrderVerification(c *gin.Context) {
	o, err := billingRepo.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if !billingOrderOwned(c, o) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: not the resource owner"})
		return
	}

	snap, err := billingRepo.GetVerification(o.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load verification failed"})
		return
	}

	// 核验阶段：与状态机对齐的展示语义。
	stage := "not_checked"
	switch {
	case o.Status == store.BillingStatusPaid:
		stage = "paid"
	case snap == nil:
		stage = "not_checked"
	case snap.Found && snap.Valid && snap.Confirmed:
		stage = "confirmed"
	case snap.Found && snap.Valid:
		stage = "confirming"
	case snap.Found:
		stage = "invalid"
	default:
		stage = "pending_onchain"
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":     o.ID,
		"status":       o.Status,
		"stage":        stage,
		"chain":        o.Chain,
		"tx_hash":      o.TxHash,
		"fail_reason":  o.FailReason,
		"attempts":     o.Attempts,
		"updated_at":   o.UpdatedAt,
		"verification": snap,
	})
}
