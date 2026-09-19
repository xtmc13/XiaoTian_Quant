package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Billing Plans ──
// 金额一律用微单位（1 USDT/USD = 1_000_000）整数运算，禁止浮点比较。

// billingPlanDef 套餐定义（静态配置，与 QuantDinger 对齐三档）。
type billingPlanDef struct {
	ID            string
	Name          string
	NameEN        string
	PriceMicro    int64 // 微单位，避免浮点
	Credits       int64 // 发放积分（lifetime 档为 credits_per_30d 的每次发放量）
	PeriodDays    int64 // 会员有效期（-1 = 终身）
	LifetimeGrant bool
	// StripePriceID Stripe Price 映射（price_xxx）；空则 checkout 用内联
	// price_data 按 PriceMicro 计价，也可用 STRIPE_PRICE_ID_<PLAN> env 覆盖。
	StripePriceID string
}

var billingPlanDefs = []billingPlanDef{
	{ID: "monthly", Name: "月度会员", NameEN: "Monthly", PriceMicro: 19_900_000, Credits: 500, PeriodDays: 30},
	{ID: "yearly", Name: "年度会员", NameEN: "Yearly", PriceMicro: 199_000_000, Credits: 8000, PeriodDays: 365},
	{ID: "lifetime", Name: "终身会员", NameEN: "Lifetime", PriceMicro: 499_000_000, Credits: 800, PeriodDays: -1, LifetimeGrant: true},
}

// findBillingPlan 按 id 查套餐。
func findBillingPlan(id string) *billingPlanDef {
	for i := range billingPlanDefs {
		if billingPlanDefs[i].ID == id {
			return &billingPlanDefs[i]
		}
	}
	return nil
}

// usdtChainList 读取已配置的 USDT 收款链（env 在调用时读取，便于测试注入）。
func usdtChainList() []map[string]string {
	return []map[string]string{
		{"chain": "TRC20", "address": os.Getenv("USDT_TRC20_ADDRESS"), "memo": "TRON TRC20"},
		{"chain": "BEP20", "address": os.Getenv("USDT_BEP20_ADDRESS"), "memo": "BSC BEP20"},
		{"chain": "ERC20", "address": os.Getenv("USDT_ERC20_ADDRESS"), "memo": "Ethereum ERC20"},
		{"chain": "SOL", "address": os.Getenv("USDT_SOL_ADDRESS"), "memo": "Solana SPL"},
	}
}

// chainAddress 取链收款地址（未配置返回空串）。
func chainAddress(chain string) string {
	for _, ch := range usdtChainList() {
		if ch["chain"] == chain {
			return ch["address"]
		}
	}
	return ""
}

// billingOrderTTL 订单有效期：30 分钟未付款自动过期。
const billingOrderTTL = int64(1800)

// billingRepo 订单仓储（进程内单例）。
var billingRepo = store.NewBillingRepo()

// ── Endpoints ──

// BillingPlans 套餐列表（静态）。
func BillingPlans(c *gin.Context) {
	plans := make([]map[string]any, 0, len(billingPlanDefs))
	for _, p := range billingPlanDefs {
		m := map[string]any{
			"id": p.ID, "name": p.Name, "name_en": p.NameEN,
			"price":       float64(p.PriceMicro) / 1e6,
			"credits":     p.Credits,
			"period_days": p.PeriodDays,
		}
		if p.LifetimeGrant {
			// lifetime 档保持原语义：每 30 天发放
			m["credits_per_30d"] = p.Credits
			delete(m, "credits")
		}
		plans = append(plans, m)
	}
	c.JSON(http.StatusOK, plans)
}

// BillingChains 已配置收款地址的链列表（隐藏未配置的）。
func BillingChains(c *gin.Context) {
	chains := make([]map[string]string, 0, 4)
	for _, ch := range usdtChainList() {
		if ch["address"] != "" {
			chains = append(chains, ch)
		}
	}
	c.JSON(http.StatusOK, chains)
}

type billingCreateReq struct {
	PlanID string `json:"plan_id"`
	Chain  string `json:"chain"`
	TxHash string `json:"tx_hash"`
}

// newBillingOrderID 生成订单号：bill_ + 时间戳 + 4 字节随机后缀。
func newBillingOrderID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "bill_" + time.Now().Format("20060102150405")
	}
	return "bill_" + time.Now().Format("20060102150405") + hex.EncodeToString(buf)
}

// billingUID 从鉴权上下文取用户 id（billing 路由均已挂 AuthRequired）。
func billingUID(c *gin.Context) int64 {
	uid, ok := ctxUserID(c)
	if !ok {
		return 0
	}
	return int64(uid)
}

// BillingCreateOrder 创建订单：校验套餐与链，金额=套餐价，30 分钟过期。
// chain 传 "stripe" 表示信用卡通道订单（需 Stripe 已启用）。
func BillingCreateOrder(c *gin.Context) {
	var req billingCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	plan := findBillingPlan(req.PlanID)
	if plan == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan_id"})
		return
	}

	address := ""
	if req.Chain == "stripe" {
		if !stripeEnabled() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "stripe 支付未启用"})
			return
		}
	} else {
		address = chainAddress(req.Chain)
		if address == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported chain or address not configured"})
			return
		}
	}

	// tx_hash 幂等占用检查：同一哈希不能服务多个订单（已在别的订单上则拒绝，
	// 即使是本人也应回到原订单重新提交，避免一笔转账触发两次发放）。
	txHash := trimTxHash(req.TxHash)
	if txHash != "" {
		if exist, err := billingRepo.FindByTxHash(txHash); err == nil && exist.ID != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "该交易哈希已被其他订单使用"})
			return
		}
	}

	now := time.Now().Unix()
	o := &store.BillingOrder{
		ID:          newBillingOrderID(),
		UserID:      billingUID(c),
		PlanID:      plan.ID,
		Chain:       req.Chain,
		Address:     address,
		AmountMicro: plan.PriceMicro,
		TxHash:      txHash,
		Status:      store.BillingStatusPending,
		CreatedAt:   now,
	}
	if err := billingRepo.Create(o); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create order failed"})
		return
	}
	writeBillingOrder(c, o)
}

// trimTxHash 去掉首尾空白（前端常从浏览器/钱包复制出带空格的哈希）。
func trimTxHash(h string) string {
	for len(h) > 0 && (h[0] == ' ' || h[0] == '\n' || h[0] == '\t') {
		h = h[1:]
	}
	for len(h) > 0 && (h[len(h)-1] == ' ' || h[len(h)-1] == '\n' || h[len(h)-1] == '\t') {
		h = h[:len(h)-1]
	}
	return h
}

// writeBillingOrder 输出订单（附加 expires_at 便于前端倒计时）。
func writeBillingOrder(c *gin.Context, o *store.BillingOrder) {
	c.JSON(http.StatusOK, gin.H{
		"order_id":     o.ID,
		"user_id":      o.UserID,
		"plan_id":      o.PlanID,
		"chain":        o.Chain,
		"address":      o.Address,
		"amount_usdt":  o.AmountMicro,
		"tx_hash":      o.TxHash,
		"status":       o.Status,
		"fail_reason":  o.FailReason,
		"attempts":     o.Attempts,
		"created_at":   o.CreatedAt,
		"updated_at":   o.UpdatedAt,
		"confirmed_at": o.ConfirmedAt,
		"expires_at":   o.CreatedAt + billingOrderTTL,
	})
}

// billingOrderOwned 订单属主校验：本人或 admin 可见，否则 403。
func billingOrderOwned(c *gin.Context, o *store.BillingOrder) bool {
	uid, injected := ctxUserID(c)
	if !injected {
		return true // 未走鉴权中间件的内部调用放行（保持项目惯例）
	}
	if ctxIsAdmin(c) {
		return true
	}
	return int64(uid) == o.UserID
}

// BillingListOrders 当前用户订单列表（新→旧）；admin 带 ?all=1 看全部。
func BillingListOrders(c *gin.Context) {
	uid := billingUID(c)
	var (
		orders []*store.BillingOrder
		err    error
	)
	if ctxIsAdmin(c) && c.Query("all") == "1" {
		orders, err = billingRepo.ListAll(200)
	} else {
		orders, err = billingRepo.ListByUser(uid, 100)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list orders failed"})
		return
	}
	if orders == nil {
		orders = []*store.BillingOrder{}
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

// BillingOrderStatus 单订单查询（仅本人/admin）。
func BillingOrderStatus(c *gin.Context) {
	o, err := billingRepo.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if !billingOrderOwned(c, o) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: not the resource owner"})
		return
	}
	writeBillingOrder(c, o)
}

type billingSubmitTxReq struct {
	TxHash string `json:"tx_hash"`
}

// BillingSubmitTx 提交/重提交易哈希：pending/failed 可提交（failed 重新核验），
// 幂等占用同一哈希的其他订单。
func BillingSubmitTx(c *gin.Context) {
	var req billingSubmitTxReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	txHash := trimTxHash(req.TxHash)
	if txHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tx_hash 不能为空"})
		return
	}

	o, err := billingRepo.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if !billingOrderOwned(c, o) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: not the resource owner"})
		return
	}
	if o.Status == store.BillingStatusPaid {
		writeBillingOrder(c, o)
		return
	}
	if o.Status == store.BillingStatusExpired {
		c.JSON(http.StatusConflict, gin.H{"error": "订单已过期，请重新创建订单"})
		return
	}
	if o.Status != store.BillingStatusPending && o.Status != store.BillingStatusFailed {
		c.JSON(http.StatusConflict, gin.H{"error": "当前状态不允许提交交易哈希"})
		return
	}
	if o.Chain == "stripe" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stripe 订单无需交易哈希"})
		return
	}

	// 占用检查：同哈希已被别的订单使用则拒绝（同一订单重复提交自身允许）。
	if exist, err := billingRepo.FindByTxHash(txHash); err == nil && exist.ID != "" && exist.ID != o.ID {
		c.JSON(http.StatusConflict, gin.H{"error": "该交易哈希已被其他订单使用"})
		return
	}

	updated, err := billingRepo.SubmitTxHash(o.ID, txHash)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "提交失败，订单状态已变化"})
		return
	}
	writeBillingOrder(c, updated)
}

// BillingSubscription 当前用户套餐/积分（不从 /auth/me 取，避免触碰 auth.go）。
func BillingSubscription(c *gin.Context) {
	s, err := billingRepo.GetSubscription(billingUID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}
