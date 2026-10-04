package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── 组合配置持久化（xt_combo_configs，migration 0056）──
// 内存 registry（strategy 包）仍是运行期读取路径；创建/更新/删除/启停在
// 改内存的同时落库，启动时 LoadComboConfigsFromStore 从库重建 registry。

func comboRecordFromConfig(cfg *strategy.ComboConfig) (*store.ComboConfigRecord, error) {
	members, err := json.Marshal(cfg.Members)
	if err != nil {
		return nil, err
	}
	return &store.ComboConfigRecord{
		ID:              cfg.ID,
		UserID:          cfg.UserID,
		Name:            cfg.Name,
		Symbol:          cfg.Symbol,
		MembersJSON:     string(members),
		AggregationMode: cfg.AggregationMode,
		Status:          cfg.Status,
		CreatedAt:       cfg.CreatedAt,
		UpdatedAt:       cfg.UpdatedAt,
	}, nil
}

func comboConfigFromRecord(rec *store.ComboConfigRecord) (*strategy.ComboConfig, error) {
	cfg := &strategy.ComboConfig{
		ID:              rec.ID,
		UserID:          rec.UserID,
		Name:            rec.Name,
		Symbol:          rec.Symbol,
		AggregationMode: rec.AggregationMode,
		Status:          rec.Status,
		CreatedAt:       rec.CreatedAt,
		UpdatedAt:       rec.UpdatedAt,
	}
	if rec.MembersJSON != "" {
		if err := json.Unmarshal([]byte(rec.MembersJSON), &cfg.Members); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// persistCombo 落库；失败仅记日志（运行态变更已成功时不阻断请求，由
// 调用方决定硬失败场景——创建/更新/删除走硬失败）。
func persistCombo(cfg *strategy.ComboConfig) error {
	rec, err := comboRecordFromConfig(cfg)
	if err != nil {
		return err
	}
	return store.NewComboConfigRepo().Update(rec)
}

// LoadComboConfigsFromStore 启动挂载点：从库把组合配置重建进内存 registry。
// 库中 status 如实保留（运行态在引擎内存，重启后不自动拉起）。
func LoadComboConfigsFromStore() {
	if store.GetDB() == nil {
		return
	}
	recs, err := store.NewComboConfigRepo().List()
	if err != nil {
		log.Printf("[WARN] combo configs DB 读取失败（内存 registry 为空）: %v", err)
		return
	}
	for _, rec := range recs {
		cfg, err := comboConfigFromRecord(rec)
		if err != nil {
			log.Printf("[WARN] combo config %s members JSON 损坏，跳过: %v", rec.ID, err)
			continue
		}
		strategy.RegisterComboConfig(cfg)
	}
	if len(recs) > 0 {
		log.Printf("[combo] 从 DB 恢复 %d 个组合配置", len(recs))
	}
}

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

	rec, err := comboRecordFromConfig(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if err := store.NewComboConfigRepo().Create(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
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

	if err := persistCombo(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
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

	// 先删库再删内存：库删除失败时保留内存态并报错，
	// 避免"内存没了但重启后复活"的分裂状态。
	if err := store.NewComboConfigRepo().Delete(cfg.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
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
		if err := persistCombo(cfg); err != nil {
			log.Printf("[WARN] combo %s 状态落库失败: %v", cfg.ID, err)
		}
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
	if err := persistCombo(cfg); err != nil {
		log.Printf("[WARN] combo %s 状态落库失败: %v", cfg.ID, err)
	}
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
	if err := persistCombo(cfg); err != nil {
		log.Printf("[WARN] combo %s 状态落库失败: %v", cfg.ID, err)
	}
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
