package handler

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// fmtSscanInt 解析正整数字符串（billingVerifyTimeoutSec 用）。
func fmtSscanInt(s string, n *int64) (int, error) {
	return fmt.Sscanf(s, "%d", n)
}

// 后台核验器：每 30s 轮询待核验订单，驱动状态机
// pending/confirming/failed → paid / failed / confirming。
// 轮询间隔可用 BILLING_VERIFY_INTERVAL（秒）调整，测试时调小。

const billingMaxAttempts = 30 // 连续核验不到交易的次数上限，超过转 failed

// billingVerifyTimeoutSec 已提交 tx_hash 订单的核验总时限（默认 24h，超时转 failed
// 并可重新提交 tx_hash 重试）；未提交哈希的订单仍按 30 分钟订单有效期过期。
func billingVerifyTimeoutSec() int64 {
	if v := os.Getenv("BILLING_VERIFY_TIMEOUT_SEC"); v != "" {
		var n int64
		if _, err := fmtSscanInt(v, &n); err == nil && n > 0 {
			return n
		}
	}
	return 24 * 3600
}

// StartBillingVerifier 启动订单核验后台任务（与 StartBackgroundTasks 同模式，
// 生命周期跟随进程；进程退出即终止）。
func StartBillingVerifier() {
	interval := 30 * time.Second
	if v := os.Getenv("BILLING_VERIFY_INTERVAL"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			interval = time.Duration(sec) * time.Second
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		runBillingVerifySweep()
	}
}

// runBillingVerifySweep 一轮核验：先过期清理，再逐单核验。
func runBillingVerifySweep() {
	now := time.Now().Unix()

	// 30 分钟未提交 tx_hash 的 pending 订单自动过期
	if n, err := billingRepo.ExpireStalePending(now - billingOrderTTL); err != nil {
		log.Printf("[billing] expire stale orders failed: %v", err)
	} else if n > 0 {
		log.Printf("[billing] expired %d stale pending order(s)", n)
	}

	candidates, err := billingRepo.ListVerifyCandidates(50, billingMaxAttempts)
	if err != nil {
		log.Printf("[billing] list verify candidates failed: %v", err)
		return
	}
	for _, o := range candidates {
		verifyOneBillingOrder(o, now)
	}
}

// verifyOneBillingOrder 核验单个订单并按结果驱动状态机。
func verifyOneBillingOrder(o *store.BillingOrder, now int64) {
	// 已提交 tx_hash 的订单：超过核验总时限（默认 24h）仍无定论 → failed，
	// 用户可重新提交 tx_hash 重试（expired 则只能重新下单，语义不同）。
	if o.TxHash != "" && o.CreatedAt+billingVerifyTimeoutSec() < now {
		billingMarkFailed(o.ID, "链上核验超时（24h），请重新提交交易哈希或联系客服")
		return
	}
	// 未提交 tx_hash 且已过订单有效期（30 分钟）→ expired（不再核验）。
	if o.TxHash == "" && o.CreatedAt+billingOrderTTL < now {
		if err := billingRepo.MarkExpired(o.ID); err != nil {
			log.Printf("[billing] mark expired %s: %v", o.ID, err)
		}
		return
	}

	res := verifyChainTx(o.Chain, o.TxHash, o.Address, o.AmountMicro)
	billingSaveVerification(o, &res)
	switch {
	case res.Err != nil:
		// 网络/接口错误：只累加尝试次数，不动状态
		attempts, err := billingRepo.IncAttempts(o.ID)
		if err != nil {
			log.Printf("[billing] inc attempts %s failed: %v", o.ID, err)
			return
		}
		if attempts >= billingMaxAttempts {
			billingMarkFailed(o.ID, "链上核验失败次数过多，请稍后重新提交交易哈希")
		}

	case !res.Found:
		// 链上暂未查到该交易
		attempts, err := billingRepo.IncAttempts(o.ID)
		if err != nil {
			log.Printf("[billing] inc attempts %s failed: %v", o.ID, err)
			return
		}
		if attempts >= billingMaxAttempts {
			billingMarkFailed(o.ID, "链上未查询到该交易，请确认哈希正确或稍后重新提交")
		}

	case !res.Valid:
		billingMarkFailed(o.ID, res.Reason)

	case !res.Confirmed:
		if err := billingRepo.MarkConfirming(o.ID); err != nil {
			log.Printf("[billing] mark confirming %s failed: %v", o.ID, err)
		}

	default:
		if billingGrantOrder(o) {
			billingGrantReferralCommission(o)
		}
	}
}

// billingSaveVerification 持久化本轮核验快照（GET /verification 数据源）。
// 网络错误也记录（found=valid=confirmed=false + 原因），便于排查"为何还在 pending"。
func billingSaveVerification(o *store.BillingOrder, res *chainVerifyResult) {
	snap := &store.BillingVerification{
		OrderID:               o.ID,
		Chain:                 o.Chain,
		TxHash:                o.TxHash,
		Found:                 res.Found,
		Valid:                 res.Valid,
		Confirmed:             res.Confirmed,
		Confirmations:         res.Confirmations,
		RequiredConfirmations: res.RequiredConfirmations,
		BlockNumber:           res.BlockNumber,
		ReceivedMicro:         res.ReceivedMicro,
		ExpectedMicro:         o.AmountMicro,
		FailReason:            res.Reason,
	}
	if res.Err != nil {
		snap.FailReason = "核验请求失败: " + res.Err.Error()
	}
	if err := billingRepo.UpsertVerification(snap); err != nil {
		log.Printf("[billing] save verification %s: %v", o.ID, err)
	}
}

// billingMarkFailed 置 failed（带并发保护：已 paid 的订单不会被覆盖）。
func billingMarkFailed(orderID, reason string) {
	if err := billingRepo.MarkFailed(orderID, reason); err != nil {
		log.Printf("[billing] mark failed %s: %v", orderID, err)
	}
}

// billingGrantOrder 核验通过：单事务内订单置 paid + 发放套餐/积分。
// 返回是否本轮真正发放（granted=false = 已发放过的重复触发）。
func billingGrantOrder(o *store.BillingOrder) bool {
	plan := findBillingPlan(o.PlanID)
	if plan == nil {
		billingMarkFailed(o.ID, "套餐不存在，无法发放")
		return false
	}
	granted, err := billingRepo.GrantPlanTx(o.ID, o.UserID, plan.ID, plan.Credits, plan.PeriodDays, time.Now().Unix())
	if err != nil {
		log.Printf("[billing] grant %s failed: %v", o.ID, err)
		return false
	}
	if granted {
		log.Printf("[billing] order %s paid: user=%d plan=%s credits=%d", o.ID, o.UserID, plan.ID, plan.Credits)
	}
	return granted
}
