package social

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// marketHandler 开放信号市场 + 利润分成的 HTTP handler。
// 路由挂在 RegisterRoutes 内（api.go），/api/social 组已有 AuthRequired。
// 审核/提现管理动作叠加 middleware.AdminRequired（见 RegisterRoutes 注册处）。
type marketHandler struct {
	engine *Engine
	market *MarketService
}

// requireUserID 要求 JWT 已注入用户（C4：一切属主判定以 JWT 为准）。
func requireUserID(c *gin.Context) (int64, bool) {
	uid, ok := followerIDFromJWT(c)
	if !ok || uid == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return 0, false
	}
	return int64(uid), true
}

// applyProvider 开放入驻申请：POST /api/social/providers/apply。
// 收费模式由月费/分成比例推导（ DeriveFeeMode ），申请进入 pending 等待管理员审核。
// pricing_model 可选：subscription|profit_share|both（双轨 SKU，迁移 0028）；
// 传空则沿用 fee_mode 旧语义。
func (h *marketHandler) applyProvider(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		ProfitSharePct *float64 `json:"profit_share_pct"`
		MonthlyFee     float64  `json:"monthly_fee"`
		PricingModel   string   `json:"pricing_model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if req.MonthlyFee < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "monthly_fee must be >= 0"})
		return
	}
	if req.ProfitSharePct != nil && (*req.ProfitSharePct < 0 || *req.ProfitSharePct > MaxProfitSharePct) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profit_share_pct must be between 0 and 30"})
		return
	}
	pricingModel := strings.TrimSpace(req.PricingModel)
	if err := ValidatePricingModel(pricingModel, req.MonthlyFee, req.ProfitSharePct); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.market.ApplyProviderWithPricing(userID, strings.TrimSpace(req.Name), req.Description, req.MonthlyFee, req.ProfitSharePct, pricingModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": p})
}

// myProvider 当前用户最近一条入驻申请：GET /api/social/providers/my。
func (h *marketHandler) myProvider(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	p, err := h.market.LatestProviderByUser(userID)
	if err != nil {
		if IsNotFound(err) {
			c.JSON(http.StatusOK, gin.H{"provider": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": p})
}

// approveProvider 管理员审核通过：POST /api/social/providers/:id/approve（admin only）。
// 通过后 provider 以 offset id 注册进引擎，允许被订阅。
func (h *marketHandler) approveProvider(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	p, err := h.market.ApproveProvider(id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not in pending") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	h.engine.RegisterProvider(MarketEngineID(p.ID), p.MonthlyFee, p.IsPublic)
	c.JSON(http.StatusOK, gin.H{"provider": p})
}

// rejectProvider 管理员审核拒绝：POST /api/social/providers/:id/reject（admin only）。
func (h *marketHandler) rejectProvider(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	p, err := h.market.RejectProvider(id, req.Note)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not in pending") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": p})
}

// earnings 我的收益汇总：GET /api/social/earnings。
func (h *marketHandler) earnings(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	s, err := h.market.Earnings(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"earnings": s})
}

// withdrawChains 支持的提现链。
var withdrawChains = map[string]bool{"trc20": true, "bep20": true, "erc20": true, "sol": true}

// withdraw 申请提现：POST /api/social/earnings/withdraw。
// 校验 payable 可用余额足够（实时汇总，幂等：余额扣减以流水为准，重复提交余额自然不足）。
func (h *marketHandler) withdraw(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req struct {
		Amount  float64 `json:"amount"`
		Chain   string  `json:"chain"`
		Address string  `json:"address"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	chain := strings.ToLower(strings.TrimSpace(req.Chain))
	if !withdrawChains[chain] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chain must be one of trc20, bep20, erc20, sol"})
		return
	}
	if strings.TrimSpace(req.Address) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "address required"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
		return
	}
	s, err := h.market.Earnings(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Amount > s.Available+1e-9 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "insufficient payable balance",
			"available": s.Available,
		})
		return
	}
	w, err := h.market.CreateWithdrawal(userID, req.Amount, chain, strings.TrimSpace(req.Address))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawal": w})
}

// myWithdrawals 我的提现申请列表：GET /api/social/earnings/withdrawals。
func (h *marketHandler) myWithdrawals(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	list, err := h.market.ListWithdrawalsByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawals": list})
}

// adminWithdrawals 提现申请管理列表：GET /api/social/admin/withdrawals?status=pending（admin only）。
func (h *marketHandler) adminWithdrawals(c *gin.Context) {
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	switch status {
	case "", WithdrawStatusPending, WithdrawStatusPaid, WithdrawStatusRejected:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status filter"})
		return
	}
	list, err := h.market.ListWithdrawals(status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawals": list})
}

// adminPayWithdrawal 标记打款：POST /api/social/admin/withdrawals/:id/pay {tx_hash}（admin only）。
// 幂等：仅 pending 可流转，重复打款返回 409。
func (h *marketHandler) adminPayWithdrawal(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		TxHash string `json:"tx_hash"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.TxHash) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tx_hash required"})
		return
	}
	changed, err := h.market.MarkWithdrawalPaid(id, strings.TrimSpace(req.TxHash))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !changed {
		c.JSON(http.StatusConflict, gin.H{"error": "withdrawal is not pending"})
		return
	}
	w, err := h.market.GetWithdrawal(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawal": w})
}

// adminRejectWithdrawal 拒绝提现：POST /api/social/admin/withdrawals/:id/reject {note}（admin only）。
// 幂等：仅 pending 可流转。
func (h *marketHandler) adminRejectWithdrawal(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	changed, err := h.market.MarkWithdrawalRejected(id, req.Note)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !changed {
		c.JSON(http.StatusConflict, gin.H{"error": "withdrawal is not pending"})
		return
	}
	w, err := h.market.GetWithdrawal(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawal": w})
}

// ── 双轨订阅（迁移 0028，对标 CryptoRobotics 双 SKU）──────────────────
//
// 订阅轨选择：POST /providers/:id/subscribe {track, chain?}
//   - track=profit_share：免费开通，立即挂到利润分成日窗跑批（T+3 锁定）；
//   - track=subscription：创建 market_subscription 用途的 USDT 计费订单，
//     链上核验 paid 后由 billing 发放路径激活订阅轨（30 天/期，顺延）。
// 切轨规则：POST /providers/:id/switch_track {track, chain?}
//   - 分成轨 → 订阅轨：随时（返回计费订单，paid 后切换）；
//   - 订阅轨 → 分成轨：仅订阅到期后允许（409 携带到期时间）。

// marketOrderChains 市场订阅订单可用的 USDT 链 → 收款地址 env。
var marketOrderChains = map[string][2]string{
	"TRC20": {"USDT_TRC20_ADDRESS", "TRON TRC20"},
	"BEP20": {"USDT_BEP20_ADDRESS", "BSC BEP20"},
	"ERC20": {"USDT_ERC20_ADDRESS", "Ethereum ERC20"},
	"SOL":   {"USDT_SOL_ADDRESS", "Solana SPL"},
}

// marketChainAddress 取链收款地址；链不可用（地址未配置或核验通道未就绪）返回空串。
// 与 handler.chainAdapterAvailable 口径一致（social 不能反向 import handler，保持小份镜像）。
func marketChainAddress(chain string) string {
	e, ok := marketOrderChains[chain]
	if !ok {
		return ""
	}
	if os.Getenv(e[0]) == "" {
		return ""
	}
	switch chain {
	case "BEP20":
		if os.Getenv("BSC_RPC_URL") == "" && os.Getenv("BSCSCAN_API_KEY") == "" {
			return ""
		}
	case "ERC20":
		if os.Getenv("ETH_RPC_URL") == "" && os.Getenv("ETHERSCAN_API_KEY") == "" {
			return ""
		}
	}
	return os.Getenv(e[0])
}

// newMarketOrderID 市场订阅订单号：mkt_ + 时间戳 + 4 字节随机后缀。
func newMarketOrderID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "mkt_" + time.Now().Format("20060102150405")
	}
	return "mkt_" + time.Now().Format("20060102150405") + hex.EncodeToString(buf)
}

// resolveApprovedProvider 解析 :id（库 id）并校验已上架。
func (h *marketHandler) resolveApprovedProvider(c *gin.Context) (*ProviderApply, bool) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	p, err := h.market.GetProvider(id)
	if err != nil {
		if IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	if p.ApplyStatus != ApplyApproved {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is not approved"})
		return nil, false
	}
	return p, true
}

// subscriptionView 订阅状态的统一输出。
func subscriptionView(sub *Subscription, p *ProviderApply) gin.H {
	out := gin.H{
		"provider_id":      sub.ProviderID,
		"track":            sub.EffectiveTrack(),
		"status":           sub.Status,
		"track_expires_at": sub.TrackExpiresAt,
		"created_at":       sub.CreatedAt,
	}
	if p != nil {
		out["provider_name"] = p.Name
		out["available_tracks"] = AvailableTracks(p)
	}
	return out
}

// createSubscriptionOrder 创建订阅轨计费订单（用途 market_subscription）。
func (h *marketHandler) createSubscriptionOrder(c *gin.Context, p *ProviderApply, userID int64, chain string) {
	chain = strings.ToUpper(strings.TrimSpace(chain))
	if chain == "" {
		chain = "TRC20"
	}
	address := marketChainAddress(chain)
	if address == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported chain or address not configured"})
		return
	}
	amountMicro := int64(math.Round(p.MonthlyFee * 1e6))
	if amountMicro <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider monthly_fee is not configured"})
		return
	}
	now := time.Now().Unix()
	o := &store.BillingOrder{
		ID:          newMarketOrderID(),
		UserID:      userID,
		PlanID:      "market_sub",
		Purpose:     store.BillingPurposeMarketSubscription,
		RefID:       strconv.FormatInt(p.ID, 10),
		Chain:       chain,
		Address:     address,
		AmountMicro: amountMicro,
		Status:      store.BillingStatusPending,
		CreatedAt:   now,
	}
	if err := store.NewBillingRepo().Create(o); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create order failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"order":        o,
		"expires_at":   o.CreatedAt + 1800,
		"period_days":  MarketSubscriptionPeriodDays,
		"provider_id":  p.ID,
		"track":        TrackSubscription,
		"available_tracks": AvailableTracks(p),
	})
}

// subscribeTrack 双轨订阅入口：POST /api/social/providers/:id/subscribe。
// 同一用户同一条目二选一：已有 active 订阅且轨不同 → 409 引导走 switch_track。
func (h *marketHandler) subscribeTrack(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	p, ok := h.resolveApprovedProvider(c)
	if !ok {
		return
	}
	var req struct {
		Track string `json:"track"`
		Chain string `json:"chain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	track := strings.TrimSpace(req.Track)
	if track != TrackSubscription && track != TrackProfitShare {
		c.JSON(http.StatusBadRequest, gin.H{"error": "track must be subscription or profit_share"})
		return
	}
	if !trackAvailable(p, track) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "track not available for this provider", "available_tracks": AvailableTracks(p)})
		return
	}

	nowMs := time.Now().UnixMilli()
	if cur, err := h.market.GetSubscription(p.ID, userID); err == nil {
		curTrack := cur.EffectiveTrack()
		if curTrack == track {
			if track == TrackSubscription && cur.TrackExpired(nowMs) {
				// 订阅轨已到期：续费 → 新订单
				h.createSubscriptionOrder(c, p, userID, req.Chain)
				return
			}
			// 同轨且有效：幂等返回当前订阅
			c.JSON(http.StatusOK, gin.H{"subscription": subscriptionView(cur, p), "already": true})
			return
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":          "已占用另一轨，请使用 switch_track 切轨",
			"current_track":  curTrack,
			"track_expires_at": cur.TrackExpiresAt,
		})
		return
	} else if !IsNotFound(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if track == TrackProfitShare {
		// 分成轨：免费开通，立即进结算作用范围，并开始跟单。
		if err := h.market.UpsertSubscriptionTrack(p.ID, userID, TrackProfitShare, 0); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = h.engine.Follow(DefaultCopyConfig(int(userID), MarketEngineID(p.ID)))
		sub, err := h.market.GetSubscription(p.ID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"subscription": subscriptionView(sub, p)})
		return
	}
	// 订阅轨：创建计费订单，paid 后由 billing 发放路径激活。
	h.createSubscriptionOrder(c, p, userID, req.Chain)
}

// switchTrack 切轨：POST /api/social/providers/:id/switch_track。
// 分成轨 → 订阅轨随时；订阅轨 → 分成轨仅到期后。
func (h *marketHandler) switchTrack(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	p, ok := h.resolveApprovedProvider(c)
	if !ok {
		return
	}
	var req struct {
		Track string `json:"track"`
		Chain string `json:"chain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	track := strings.TrimSpace(req.Track)
	if track != TrackSubscription && track != TrackProfitShare {
		c.JSON(http.StatusBadRequest, gin.H{"error": "track must be subscription or profit_share"})
		return
	}
	if !trackAvailable(p, track) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "track not available for this provider", "available_tracks": AvailableTracks(p)})
		return
	}

	cur, err := h.market.GetSubscription(p.ID, userID)
	if err != nil {
		if IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no active subscription, use subscribe instead"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	curTrack := cur.EffectiveTrack()
	if curTrack == track {
		c.JSON(http.StatusOK, gin.H{"subscription": subscriptionView(cur, p), "already": true})
		return
	}

	if track == TrackSubscription {
		// 分成轨 → 订阅轨：随时，创建订单，paid 后切换到订阅轨。
		h.createSubscriptionOrder(c, p, userID, req.Chain)
		return
	}

	// 订阅轨 → 分成轨：仅到期后允许。
	nowMs := time.Now().UnixMilli()
	if !cur.TrackExpired(nowMs) {
		c.JSON(http.StatusConflict, gin.H{
			"error":            "订阅轨未到期，到期后方可切换到分成轨",
			"track_expires_at": cur.TrackExpiresAt,
		})
		return
	}
	if err := h.market.UpsertSubscriptionTrack(p.ID, userID, TrackProfitShare, 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sub, err := h.market.GetSubscription(p.ID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscription": subscriptionView(sub, p)})
}

// mySubscription 当前用户对该 provider 的订阅轨状态：GET /api/social/providers/:id/subscription。
func (h *marketHandler) mySubscription(c *gin.Context) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	p, ok := h.resolveApprovedProvider(c)
	if !ok {
		return
	}
	sub, err := h.market.GetSubscription(p.ID, userID)
	if err != nil {
		if IsNotFound(err) {
			c.JSON(http.StatusOK, gin.H{"subscription": nil, "available_tracks": AvailableTracks(p)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscription": subscriptionView(sub, p)})
}
