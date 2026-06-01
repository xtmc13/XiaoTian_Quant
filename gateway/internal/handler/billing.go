package handler

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// ── Billing Plans ──

var billingPlans = []map[string]any{
	{"id": "monthly", "name": "月度会员", "name_en": "Monthly", "price": 19.90, "credits": 500, "period_days": 30},
	{"id": "yearly", "name": "年度会员", "name_en": "Yearly", "price": 199.00, "credits": 8000, "period_days": 365},
	{"id": "lifetime", "name": "终身会员", "name_en": "Lifetime", "price": 499.00, "credits_per_30d": 800, "period_days": -1},
}

var usdtChains = []map[string]string{
	{"chain": "TRC20", "address": os.Getenv("USDT_TRC20_ADDRESS"), "memo": "TRON TRC20"},
	{"chain": "BEP20", "address": os.Getenv("USDT_BEP20_ADDRESS"), "memo": "BSC BEP20"},
	{"chain": "ERC20", "address": os.Getenv("USDT_ERC20_ADDRESS"), "memo": "Ethereum ERC20"},
	{"chain": "SOL", "address": os.Getenv("USDT_SOL_ADDRESS"), "memo": "Solana SPL"},
}

// ── Endpoints ──

func BillingPlans(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"plans": billingPlans})
}

func BillingChains(c *gin.Context) {
	// Hide empty addresses
	var chains []map[string]string
	for _, ch := range usdtChains {
		if ch["address"] != "" {
			chains = append(chains, ch)
		}
	}
	if chains == nil {
		chains = []map[string]string{}
	}
	c.JSON(http.StatusOK, gin.H{"chains": chains})
}

type billingCreateReq struct {
	PlanID string `json:"plan_id"`
	Chain  string `json:"chain"`
	TxHash string `json:"tx_hash"`
}

func BillingCreateOrder(c *gin.Context) {
	var req billingCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	orderID := "bill_" + time.Now().Format("20060102150405")
	c.JSON(http.StatusOK, gin.H{
		"order_id":  orderID,
		"plan_id":   req.PlanID,
		"chain":     req.Chain,
		"status":    "pending",
		"expires_in": 1800, // 30 minutes
	})
}

func BillingOrderStatus(c *gin.Context) {
	orderID := c.Param("id")
	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"status":   "pending",
		"message":  "等待链上确认...",
	})
}
