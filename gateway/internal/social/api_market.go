package social

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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
	p, err := h.market.ApplyProvider(userID, strings.TrimSpace(req.Name), req.Description, req.MonthlyFee, req.ProfitSharePct)
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
