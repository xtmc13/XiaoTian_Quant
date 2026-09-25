package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/hyperopt"
	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Hyperopt Epochs（每轮 trial 的持久化结果）────────────────────
//
// 表 xt_hyperopt_epochs 由迁移 0018_hyperopt_epochs.sql 建立，
// engine 每轮 trial 完成时经 OnTrialComplete 回调落库（见 hyperopt.go）。
// 这里提供组合过滤查询、单条详情与“最优超参一键回写策略”。

// hyperoptEpochView 是 epoch 的 API 视图：JSON 字段解析回 map 返回。
type hyperoptEpochView struct {
	ID         string             `json:"id"`
	UserID     int64              `json:"user_id"`
	JobID      string             `json:"job_id"`
	StrategyID string             `json:"strategy_id"`
	TrialID    int                `json:"trial_id"`
	Params     map[string]any     `json:"params"`
	Metrics    map[string]float64 `json:"metrics"`
	Loss       float64            `json:"loss"`
	LossName   string             `json:"loss_name"`
	Applied    bool               `json:"applied"`
	AppliedAt  int64              `json:"applied_at,omitempty"`
	CreatedAt  int64              `json:"created_at"`
}

func toHyperoptEpochView(rec *store.HyperoptEpochRecord) *hyperoptEpochView {
	v := &hyperoptEpochView{
		ID:         rec.ID,
		UserID:     rec.UserID,
		JobID:      rec.JobID,
		StrategyID: rec.StrategyID,
		TrialID:    rec.TrialID,
		Loss:       rec.Loss,
		LossName:   rec.LossName,
		Applied:    rec.Applied,
		AppliedAt:  rec.AppliedAt,
		CreatedAt:  rec.CreatedAt,
	}
	if rec.ParamsJSON != "" {
		_ = json.Unmarshal([]byte(rec.ParamsJSON), &v.Params)
	}
	if rec.MetricsJSON != "" {
		_ = json.Unmarshal([]byte(rec.MetricsJSON), &v.Metrics)
	}
	return v
}

// parseFloatQuery 解析可选 float 查询参数；空串返回 nil。
func parseFloatQuery(c *gin.Context, key string) *float64 {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}

// parseIntQuery 解析可选 int 查询参数；空串或非法返回 nil。
func parseIntQuery(c *gin.Context, key string) *int {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &v
}

// ListHyperoptEpochs 组合过滤查询：
// GET /api/hyperopt/epochs?job_id=&loss_max=&sharpe_min=&trade_count_min=&max_drawdown_max=&limit=
// 全部可选；普通登录用户只能看到本人 + 历史无属主(user_id=0)的记录。
func ListHyperoptEpochs(c *gin.Context) {
	if store.GetDB() == nil {
		c.JSON(http.StatusOK, gin.H{"epochs": []any{}, "count": 0})
		return
	}

	filter := store.HyperoptEpochFilter{
		JobID:          c.Query("job_id"),
		LossMax:        parseFloatQuery(c, "loss_max"),
		SharpeMin:      parseFloatQuery(c, "sharpe_min"),
		TradeCountMin:  parseIntQuery(c, "trade_count_min"),
		MaxDrawdownMax: parseFloatQuery(c, "max_drawdown_max"),
		Limit:          parseIntQueryDef(c, "limit", 200),
	}
	uid, injected := ctxUserID(c)
	if injected && !ctxIsAdmin(c) {
		filter.UserID = int64(uid)
	}

	epochs, err := store.NewHyperoptEpochRepo().List(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query epochs: " + err.Error()})
		return
	}

	views := make([]*hyperoptEpochView, 0, len(epochs))
	for _, rec := range epochs {
		views = append(views, toHyperoptEpochView(rec))
	}
	c.JSON(http.StatusOK, gin.H{"epochs": views, "count": len(views)})
}

func parseIntQueryDef(c *gin.Context, key string, def int) int {
	if v := parseIntQuery(c, key); v != nil {
		return *v
	}
	return def
}

// GetHyperoptEpoch 单条详情：GET /api/hyperopt/epochs/:id
func GetHyperoptEpoch(c *gin.Context) {
	if store.GetDB() == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "epoch not found"})
		return
	}
	rec, err := store.NewHyperoptEpochRepo().GetByID(c.Param("id"))
	if err == sql.ErrNoRows || rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "epoch not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get epoch: " + err.Error()})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	c.JSON(http.StatusOK, toHyperoptEpochView(rec))
}

// ApplyHyperoptEpoch 一键回写：POST /api/hyperopt/epochs/:id/apply
//
// 把该 epoch 的超参合并进对应 strategy_configs 记录的 config_json，
// 写前的旧值以 diff 字段（key → {old, new}）随响应返回；成功后将 epoch
// 标记 applied=1。仅 epoch 属主可 apply（策略配置另做属主复核）。
func ApplyHyperoptEpoch(c *gin.Context) {
	if store.GetDB() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store not initialized"})
		return
	}

	epochRepo := store.NewHyperoptEpochRepo()
	rec, err := epochRepo.GetByID(c.Param("id"))
	if err == sql.ErrNoRows || rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "epoch not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get epoch: " + err.Error()})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	if rec.Applied {
		c.JSON(http.StatusConflict, gin.H{"error": "epoch already applied", "epoch_id": rec.ID})
		return
	}
	if rec.StrategyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "epoch has no strategy_id; start the job with strategy_id to enable apply"})
		return
	}

	cfgRepo := store.NewStrategyConfigRepo()
	cfgRec, err := cfgRepo.GetByID(rec.StrategyID)
	if err == sql.ErrNoRows || cfgRec == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "strategy config not found: " + rec.StrategyID})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get strategy config: " + err.Error()})
		return
	}
	if !requireOwner(c, cfgRec.UserID) {
		return
	}

	// 合并超参进 config_json，写前留旧值生成 diff。
	cfgMap := map[string]any{}
	if cfgRec.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(cfgRec.ConfigJSON), &cfgMap); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "parse strategy config_json: " + err.Error()})
			return
		}
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(rec.ParamsJSON), &params); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "parse epoch params: " + err.Error()})
		return
	}

	// protection 空间参数（protection__<Name>__<param>）不进策略顶层字段，
	// 而是写回 config_json 的 protections 数组（与 protection.BuildManagerFromConfig
	// 的配置结构一致），回测/实盘经该配置生效。
	stratParams, protParams := hyperopt.SplitProtectionParams(params)

	diff := make(map[string]gin.H, len(params))
	for k, v := range stratParams {
		oldVal, existed := cfgMap[k]
		if !existed {
			diff[k] = gin.H{"old": nil, "new": v}
		} else {
			diff[k] = gin.H{"old": oldVal, "new": v}
		}
		cfgMap[k] = v
	}

	if len(protParams) > 0 {
		base := parseProtectionConfigs(cfgMap["protections"])
		oldProtections := cfgMap["protections"]
		merged := hyperopt.ApplyProtectionParams(base, protParams)
		cfgMap["protections"] = protectionConfigsToJSON(merged)
		diff["protections"] = gin.H{"old": oldProtections, "new": cfgMap["protections"]}
	}

	payload, err := json.Marshal(cfgMap)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal strategy config_json: " + err.Error()})
		return
	}
	cfgRec.ConfigJSON = string(payload)
	if err := cfgRepo.Update(cfgRec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update strategy config: " + err.Error()})
		return
	}

	if err := epochRepo.MarkApplied(rec.ID, 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mark epoch applied: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "applied",
		"epoch_id":    rec.ID,
		"strategy_id": rec.StrategyID,
		"diff":        diff,
	})
}

// parseProtectionConfigs 把 config_json 里的 protections 字段（[]any）
// 解析为 protection.ProtectionConfig 切片；无法解析时返回空。
func parseProtectionConfigs(raw any) []protection.ProtectionConfig {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []protection.ProtectionConfig
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// protectionConfigsToJSON 把 protection 配置转成可写入 config_json 的形式。
func protectionConfigsToJSON(cfgs []protection.ProtectionConfig) []map[string]any {
	out := make([]map[string]any, 0, len(cfgs))
	for _, pc := range cfgs {
		out = append(out, map[string]any{
			"name":   pc.Name,
			"params": pc.Params,
		})
	}
	return out
}
