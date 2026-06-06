package social

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers social trading HTTP endpoints.
func RegisterRoutes(r *gin.RouterGroup, engine *Engine) {
	h := &handler{engine: engine}

	r.GET("/providers", h.listProviders)
	r.POST("/providers/:id/follow", h.followProvider)
	r.POST("/providers/:id/unfollow", h.unfollowProvider)
	r.GET("/signals", h.listSignals)
	r.POST("/signals", h.publishSignal)
	r.GET("/followers/configs", h.getFollowerConfigs)
}

type handler struct {
	engine *Engine
}

func (h *handler) listProviders(c *gin.Context) {
	providers := h.engine.GetPublicProviders()
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

func (h *handler) followProvider(c *gin.Context) {
	providerID, _ := strconv.Atoi(c.Param("id"))
	followerID, _ := strconv.Atoi(c.Query("follower_id"))
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	cfg := DefaultCopyConfig(followerID, providerID)
	if err := h.engine.Follow(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *handler) unfollowProvider(c *gin.Context) {
	providerID, _ := strconv.Atoi(c.Param("id"))
	followerID, _ := strconv.Atoi(c.Query("follower_id"))
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	h.engine.Unfollow(followerID, providerID)
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

func (h *handler) getFollowerConfigs(c *gin.Context) {
	followerID, _ := strconv.Atoi(c.Query("follower_id"))
	if followerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "follower_id required"})
		return
	}
	configs := h.engine.GetFollowerConfigs(followerID)
	c.JSON(http.StatusOK, gin.H{"configs": configs})
}
