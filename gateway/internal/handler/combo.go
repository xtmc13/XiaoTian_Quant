package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// GetCombos lists strategy combos visible to the current user
// （本人的 + 历史无属主；admin/未注入用户看全部）。
func GetCombos(c *gin.Context) {
	uid, injected := ctxUserID(c)
	configs := strategy.ListComboConfigsForUser(int64(uid), !injected || ctxIsAdmin(c))
	items := make([]map[string]any, 0, len(configs))
	for _, cfg := range configs {
		items = append(items, cfg.ToMap())
	}
	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, items)
}

// comboMustGet 按 :id 取组合；不存在 404，属他人（且非 admin）403。
func comboMustGet(c *gin.Context) (*strategy.ComboConfig, bool) {
	cfg := strategy.GetComboConfig(c.Param("id"))
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return nil, false
	}
	if !requireOwner(c, cfg.UserID) {
		return nil, false
	}
	return cfg, true
}

// GetCombo returns a single combo config by ID.
func GetCombo(c *gin.Context) {
	cfg, ok := comboMustGet(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, cfg.ToMap())
}

// CreateCombo creates a new strategy combo.
func CreateCombo(c *gin.Context) {
	var body struct {
		Name            string                `json:"name"`
		Symbol          string                `json:"symbol"`
		Members         []strategy.ComboMember `json:"members"`
		AggregationMode string                `json:"aggregation_mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}

	if body.AggregationMode == "" {
		body.AggregationMode = "vote"
	}

	cfg := &strategy.ComboConfig{
		ID:              shortUUID(),
		UserID:          getUserID(c),
		Name:            body.Name,
		Symbol:          body.Symbol,
		Members:         body.Members,
		AggregationMode: body.AggregationMode,
		Status:          "stopped",
		CreatedAt:       time.Now().UnixMilli(),
		UpdatedAt:       time.Now().UnixMilli(),
	}

	if err := cfg.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	strategy.RegisterComboConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": cfg.ID})
}

// UpdateCombo modifies an existing combo config.
func UpdateCombo(c *gin.Context) {
	cfg, ok := comboMustGet(c)
	if !ok {
		return
	}

	var body struct {
		Name            string                `json:"name"`
		Symbol          string                `json:"symbol"`
		Members         []strategy.ComboMember `json:"members"`
		AggregationMode string                `json:"aggregation_mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}

	if body.Name != "" {
		cfg.Name = body.Name
	}
	if body.Symbol != "" {
		cfg.Symbol = body.Symbol
	}
	if body.AggregationMode != "" {
		cfg.AggregationMode = body.AggregationMode
	}
	if body.Members != nil {
		cfg.Members = body.Members
	}
	cfg.UpdatedAt = time.Now().UnixMilli()

	if err := cfg.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteCombo removes a combo config and stops it if running.
func DeleteCombo(c *gin.Context) {
	cfg, ok := comboMustGet(c)
	if !ok {
		return
	}

	if cfg.Status == "running" {
		eng := app.Get().StrategyEngine
		if eng != nil {
			_ = eng.Stop(cfg.ID)
			_ = eng.Unregister(cfg.ID)
		}
	}

	strategy.DeleteComboConfig(cfg.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// StartCombo registers (if needed) and starts a combo in the strategy engine.
func StartCombo(c *gin.Context) {
	cfg, ok := comboMustGet(c)
	if !ok {
		return
	}

	eng := app.Get().StrategyEngine
	if eng == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "strategy engine not available"})
		return
	}

	if eng.Get(cfg.ID) != nil {
		if err := eng.Start(cfg.ID, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
		}
		cfg.Status = "running"
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	combo, err := strategy.NewStrategyCombo(cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if err := eng.Register(combo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	if err := eng.Start(cfg.ID, nil); err != nil {
		_ = eng.Unregister(cfg.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	cfg.Status = "running"
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// StopCombo stops a running combo.
func StopCombo(c *gin.Context) {
	cfg, ok := comboMustGet(c)
	if !ok {
		return
	}

	eng := app.Get().StrategyEngine
	if eng == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "strategy engine not available"})
		return
	}

	if err := eng.Stop(cfg.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	cfg.Status = "stopped"
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetComboSignals returns recent aggregated signals for a combo.
func GetComboSignals(c *gin.Context) {
	cfg, ok := comboMustGet(c)
	if !ok {
		return
	}

	eng := app.Get().StrategyEngine
	if eng == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "strategy engine not available"})
		return
	}

	s := eng.Get(cfg.ID)
	if s == nil {
		c.JSON(http.StatusOK, []map[string]any{})
		return
	}

	combo, ok := s.(*strategy.StrategyCombo)
	if !ok {
		c.JSON(http.StatusOK, []map[string]any{})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	signals := combo.RecentSignals(limit)
	items := make([]map[string]any, 0, len(signals))
	for _, sig := range signals {
		items = append(items, map[string]any{
			"symbol":    sig.Symbol,
			"direction": sig.Direction,
			"strength":  sig.Strength,
			"strategy":  sig.Strategy,
			"reason":    sig.Reason,
			"timestamp": sig.Timestamp,
		})
	}
	c.JSON(http.StatusOK, items)
}
