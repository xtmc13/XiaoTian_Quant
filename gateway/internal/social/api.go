package social

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// followerIDFromJWT 从 JWT context 取当前用户 id（C4：follower_id 一律以 JWT 为准）。
// 第二个返回值为 context 是否注入了用户；未注入（内部调用/单用户兼容）时
// 调用方可回退到旧参数来源。
func followerIDFromJWT(c *gin.Context) (int, bool) {
	v, exists := c.Get(middleware.UserIDKey)
	if !exists {
		return 0, false
	}
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	}
	return 0, false
}

// followerIDOrLegacy 优先取 JWT 用户；未注入时回退 query/body 里的旧参数。
func followerIDOrLegacy(c *gin.Context, legacy int) int {
	if uid, ok := followerIDFromJWT(c); ok {
		return uid
	}
	return legacy
}

// RegisterRoutes registers social trading HTTP endpoints.
func RegisterRoutes(r *gin.RouterGroup, engine *Engine) {
	market := NewMarketService()
	// 重启恢复：已上架的市场 provider 重新注册进引擎（offset id），否则 follow 找不到。
	if approved, err := market.ListApprovedProviders(); err == nil {
		for _, p := range approved {
			engine.RegisterProvider(MarketEngineID(p.ID), p.MonthlyFee, p.IsPublic)
		}
	}
	h := &handler{engine: engine, market: market}
	hm := &marketHandler{engine: engine, market: market}

	r.GET("/providers", h.listProviders)
	r.POST("/providers/apply", hm.applyProvider)
	r.GET("/providers/my", hm.myProvider)
	r.POST("/providers/:id/approve", middleware.AdminRequired(), hm.approveProvider)
	r.POST("/providers/:id/reject", middleware.AdminRequired(), hm.rejectProvider)
	r.POST("/providers/:id/follow", h.followProvider)
	r.POST("/providers/:id/unfollow", h.unfollowProvider)
	r.GET("/signals", h.listSignals)
	r.POST("/signals", h.publishSignal)
	r.GET("/followers/configs", h.getFollowerConfigs)
	r.POST("/followers/configs", h.saveFollowerConfig)

	// 利润分成与提现（开放信号市场）。
	r.GET("/earnings", hm.earnings)
	r.POST("/earnings/withdraw", hm.withdraw)
	r.GET("/earnings/withdrawals", hm.myWithdrawals)
	r.GET("/admin/withdrawals", middleware.AdminRequired(), hm.adminWithdrawals)
	r.POST("/admin/withdrawals/:id/pay", middleware.AdminRequired(), hm.adminPayWithdrawal)
	r.POST("/admin/withdrawals/:id/reject", middleware.AdminRequired(), hm.adminRejectWithdrawal)
}

type handler struct {
	engine *Engine
	market *MarketService
}

func (h *handler) listProviders(c *gin.Context) {
	providers := h.engine.GetPublicProviders()
	out := gin.H{"providers": providers}
	// 合并开放市场已上架 provider（offset id，与引擎内 catalog provider 区分）。
	if approved, err := h.market.ListApprovedProviders(); err == nil && len(approved) > 0 {
		marketProviders := make([]gin.H, 0, len(approved))
		for _, p := range approved {
			followers := 0
			if stats := h.engine.GetProviderStats(MarketEngineID(p.ID)); stats != nil {
				followers = stats.FollowerCount
			}
			item := gin.H{
				"id":               MarketEngineID(p.ID),
				"name":             p.Name,
				"description":      p.Description,
				"monthly_fee":      p.MonthlyFee,
				"fee_mode":         p.FeeMode,
				"profit_share_pct": p.ProfitSharePct,
				"follower_count":   followers,
			}
			marketProviders = append(marketProviders, item)
		}
		out["market_providers"] = marketProviders
	}
	c.JSON(http.StatusOK, out)
}

// resolveMarketProvider 解析市场 provider（offset id）：不存在或未上架返回错误。
func (h *handler) resolveMarketProvider(c *gin.Context, engineID int) (*ProviderApply, error) {
	p, err := h.market.GetProvider(MarketDBID(engineID))
	if err != nil {
		if IsNotFound(err) {
			return nil, fmt.Errorf("provider %d not found", engineID)
		}
		return nil, err
	}
	if p.ApplyStatus != ApplyApproved {
		return nil, fmt.Errorf("provider %d is not approved", engineID)
	}
	return p, nil
}

func (h *handler) followProvider(c *gin.Context) {
	providerID, _ := strconv.Atoi(c.Param("id"))
	followerID, _ := strconv.Atoi(c.Query("follower_id"))
	followerID = followerIDOrLegacy(c, followerID)
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	var marketProvider *ProviderApply
	if dbID := MarketDBID(providerID); dbID > 0 {
		p, err := h.resolveMarketProvider(c, providerID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		marketProvider = p
	}
	cfg := DefaultCopyConfig(followerID, providerID)
	if err := h.engine.Follow(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 市场 provider：建立订阅关系（profit_share/hybrid 免费订阅，月费走既有订阅流程）。
	if marketProvider != nil {
		if err := h.market.UpsertSubscription(marketProvider.ID, int64(followerID), marketProvider.FeeMode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *handler) unfollowProvider(c *gin.Context) {
	providerID, _ := strconv.Atoi(c.Param("id"))
	followerID, _ := strconv.Atoi(c.Query("follower_id"))
	followerID = followerIDOrLegacy(c, followerID)
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	h.engine.Unfollow(followerID, providerID)
	if dbID := MarketDBID(providerID); dbID > 0 {
		if err := h.market.CancelSubscription(dbID, int64(followerID)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *handler) listSignals(c *gin.Context) {
	providerID, _ := strconv.Atoi(c.Query("provider_id"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if providerID > 0 {
		signals := h.engine.GetProviderSignals(providerID, limit)
		c.JSON(http.StatusOK, gin.H{"signals": signals})
		return
	}
	c.JSON(http.StatusOK, gin.H{"signals": []Signal{}})
}

func (h *handler) publishSignal(c *gin.Context) {
	var req struct {
		ProviderID   int     `json:"provider_id"`
		ProviderName string  `json:"provider_name"`
		Symbol       string  `json:"symbol"`
		Direction    string  `json:"direction"`
		Price        float64 `json:"price"`
		StopLoss     float64 `json:"stop_loss"`
		TakeProfit   float64 `json:"take_profit"`
		Size         float64 `json:"size"`
		Confidence   float64 `json:"confidence"`
		Strategy     string  `json:"strategy"`
		Reason       string  `json:"reason"`
		ExpiresAt    int64   `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// C4: provider_id 必须属于当前登录用户（前端以 user id 作为自营信号 provider）
	if uid, ok := followerIDFromJWT(c); ok && req.ProviderID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "provider_id does not belong to current user"})
		return
	}
	sig := Signal{
		ID:           strconv.FormatInt(time.Now().UnixNano(), 10),
		ProviderID:   req.ProviderID,
		ProviderName: req.ProviderName,
		Symbol:       req.Symbol,
		Direction:    req.Direction,
		Price:        req.Price,
		StopLoss:     req.StopLoss,
		TakeProfit:   req.TakeProfit,
		Size:         req.Size,
		Confidence:   req.Confidence,
		Strategy:     req.Strategy,
		Reason:       req.Reason,
		Timestamp:    time.Now().UnixMilli(),
		ExpiresAt:    req.ExpiresAt,
	}
	h.engine.PublishSignal(sig)
	c.JSON(http.StatusOK, gin.H{"signal": sig})
}

func (h *handler) saveFollowerConfig(c *gin.Context) {
	var req struct {
		ProviderID   int      `json:"provider_id"`
		FollowerID   int      `json:"follower_id"`
		Enabled      bool     `json:"enabled"`
		Multiplier   float64  `json:"multiplier"`
		MaxPosition  float64  `json:"max_position"`
		MaxDailyLoss float64  `json:"max_daily_loss"`
		SlippagePct  float64  `json:"slippage_pct"`
		AutoExecute  bool     `json:"auto_execute"`
		Symbols      []string `json:"symbols"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ProviderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider_id required"})
		return
	}
	// C4: follower_id 一律取自 JWT，忽略请求体
	followerID := followerIDOrLegacy(c, req.FollowerID)
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	update := CopyConfig{
		FollowerID:   followerID,
		ProviderID:   req.ProviderID,
		Enabled:      req.Enabled,
		Multiplier:   req.Multiplier,
		MaxPosition:  req.MaxPosition,
		MaxDailyLoss: req.MaxDailyLoss,
		SlippagePct:  req.SlippagePct,
		AutoExecute:  req.AutoExecute,
		Symbols:      req.Symbols,
	}
	if err := h.engine.UpdateFollowConfig(followerID, req.ProviderID, update); err != nil {
		cfg := DefaultCopyConfig(followerID, req.ProviderID)
		cfg.AutoExecute = req.AutoExecute
		if err := h.engine.Follow(cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_ = h.engine.UpdateFollowConfig(followerID, req.ProviderID, update)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *handler) getFollowerConfigs(c *gin.Context) {
	followerID, _ := strconv.Atoi(c.Query("follower_id"))
	followerID = followerIDOrLegacy(c, followerID)
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	configs := h.engine.GetFollowerConfigs(followerID)
	c.JSON(http.StatusOK, gin.H{"configs": configs})
}
