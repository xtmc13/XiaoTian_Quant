package handler

import (
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Affiliate 推荐码体系（0020 迁移）────────────────────────
// 注册挂接见 auth.go Register（referral_code 字段）；
// 佣金触发挂点：billing_verify.go 链上核验通过 default 分支、
// billing_stripe.go webhook 成功处，两处均在 billingGrantOrder 返回
// granted=true 后调用 billingGrantReferralCommission。

const (
	referralDefaultCommissionPct = 10
	referralMaxCommissionPct     = 50 // API 层上限（DB 允许 0-100，防滥用收紧到 0-50）
	referralCodeLen              = 8
)

// referralCodePattern 推荐码格式：XT + 8 位大写字母数字（注册入参校验）。
var referralCodePattern = regexp.MustCompile(`^XT[A-Z0-9]{8}$`)

var referralRepo = store.NewReferralRepo()

// normalizeReferralCode 归一化用户输入的推荐码（去空白、转大写）。
func normalizeReferralCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// referralWebBase 推荐链接的前端 base：FRONTEND_BASE_URL 未配置时返回空串，
// 前端应优先用 window.location.origin 拼 referral_path。
func referralWebBase() string {
	return strings.TrimRight(envOrDefault("FRONTEND_BASE_URL", ""), "/")
}

func writeReferralCode(c *gin.Context, rc *store.ReferralCode) {
	c.JSON(http.StatusOK, gin.H{
		"code":           rc.Code,
		"commission_pct": rc.CommissionPct,
		"active":         rc.Active,
		"created_at":     rc.CreatedAt,
		"referral_path":  "/register?ref=" + rc.Code,
		"referral_link":  referralWebBase() + "/register?ref=" + rc.Code,
	})
}

// BillingReferralCode GET /billing/referral/code —— 我的推荐码，没有则自动生成。
func BillingReferralCode(c *gin.Context) {
	uid := billingUID(c)
	rc, err := referralRepo.EnsureCode(uid, referralDefaultCommissionPct)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get or create referral code failed"})
		return
	}
	writeReferralCode(c, rc)
}

type referralCodeUpdateReq struct {
	// CommissionPct 仅允许 0-50（缺省保持不变）。
	CommissionPct *int `json:"commission_pct"`
}

// BillingReferralCodeUpdate POST /billing/referral/code —— 更新佣金比例（取或建后更新）。
func BillingReferralCodeUpdate(c *gin.Context) {
	uid := billingUID(c)
	var req referralCodeUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.CommissionPct != nil {
		p := *req.CommissionPct
		if p < 0 || p > referralMaxCommissionPct {
			c.JSON(http.StatusBadRequest, gin.H{"error": "commission_pct must be between 0 and 50"})
			return
		}
	}
	rc, err := referralRepo.EnsureCode(uid, referralDefaultCommissionPct)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get or create referral code failed"})
		return
	}
	if req.CommissionPct != nil {
		if err := referralRepo.SetCommission(uid, *req.CommissionPct); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "update commission_pct failed"})
			return
		}
		rc, _ = referralRepo.GetCodeByUser(uid)
	}
	writeReferralCode(c, rc)
}

// BillingReferralSummary GET /billing/referral/summary —— 我的推荐汇总。
func BillingReferralSummary(c *gin.Context) {
	uid := billingUID(c)
	stats, err := referralRepo.ReferralStats(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "referral stats failed"})
		return
	}
	rc, rcErr := referralRepo.GetCodeByUser(uid)
	resp := gin.H{
		"referral_count":  stats.ReferralCount,
		"total_credits":   stats.TotalCredits,
		"pending_credits": 0, // 佣金随支付成功同事务入账，无延迟
	}
	if rcErr == nil && rc != nil {
		resp["code"] = rc.Code
		resp["commission_pct"] = rc.CommissionPct
		resp["active"] = rc.Active
	} else {
		resp["code"] = nil
		resp["commission_pct"] = referralDefaultCommissionPct
		resp["active"] = false
	}
	c.JSON(http.StatusOK, resp)
}

// BillingReferralEarnings GET /billing/referral/earnings —— 我的佣金明细（仅本人数据）。
func BillingReferralEarnings(c *gin.Context) {
	uid := billingUID(c)
	list, err := referralRepo.ListEarnings(uid, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list earnings failed"})
		return
	}
	if list == nil {
		list = []*store.ReferralEarning{}
	}
	c.JSON(http.StatusOK, gin.H{"earnings": list})
}

// AdminListReferrals GET /admin/referrals —— 全员推荐关系+收益汇总（仅 admin）。
func AdminListReferrals(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	list, err := referralRepo.AdminList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list referrals failed"})
		return
	}
	if list == nil {
		list = []*store.ReferralAdminRow{}
	}
	c.JSON(http.StatusOK, gin.H{"referrals": list})
}

// ── 佣金触发（支付成功挂点调用，失败只记日志不阻断主支付流）──

// billingGrantReferralCommission 订单置 paid 后为推荐人发佣金：
// 查订单用户 referred_by → 取推荐人推荐码的 commission_pct →
// credits = 订单金额(微单位)/10000 * pct / 100（先转美元分再按比例，1 积分=$0.01）
// → xt_referral_earnings 插一行（order_id 唯一保证幂等）→ 推荐人 credits 增加，
// 两步在同一事务（CreditEarningTx）。推荐码停用/比例 0/自推荐等异常只记日志。
func billingGrantReferralCommission(o *store.BillingOrder) {
	referredBy, ok := store.GetReferredBy(o.UserID)
	if !ok || referredBy <= 0 {
		return // 非推荐注册用户，无佣金
	}
	if referredBy == o.UserID {
		log.Printf("[referral] order %s: skip self-referral commission (user=%d)", o.ID, o.UserID)
		return
	}
	rc, err := referralRepo.GetCodeByUser(referredBy)
	if err != nil || rc == nil {
		log.Printf("[referral] order %s: referrer %d has no referral code, skip", o.ID, referredBy)
		return
	}
	if !rc.Active {
		log.Printf("[referral] order %s: referrer %d code inactive, skip", o.ID, referredBy)
		return
	}
	if rc.CommissionPct <= 0 {
		return
	}
	credits := o.AmountMicro / 10000 * int64(rc.CommissionPct) / 100
	if credits <= 0 {
		log.Printf("[referral] order %s: computed credits 0 (amount=%d pct=%d), skip", o.ID, o.AmountMicro, rc.CommissionPct)
		return
	}
	inserted, err := referralRepo.CreditEarningTx(&store.ReferralEarning{
		ReferrerUserID: referredBy,
		ReferredUserID: o.UserID,
		OrderID:        o.ID,
		OrderAmount:    o.AmountMicro,
		Credits:        credits,
		Status:         "credited",
		CreatedAt:      time.Now().Unix(),
	})
	if err != nil {
		log.Printf("[referral] order %s: credit referrer %d failed: %v", o.ID, referredBy, err)
		return
	}
	if inserted {
		log.Printf("[referral] order %s paid: referrer=%d +%d credits (pct=%d%%, amount=%d)",
			o.ID, referredBy, credits, rc.CommissionPct, o.AmountMicro)
	}
}
