package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/aigate"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI 交易决策门 API（对标 QuantDinger JEV 决策门） ──
// GET  /api/ai/gate/decisions        决策时间线（分页+过滤，非 admin 只看本人）
// GET  /api/ai/gate/decisions/:id    单条详情（属主校验）
// GET  /api/ai/gate/stats            聚合统计（拦截率/fail-open 率卡片）
// GET  /api/ai/gate/config           当前配置（env 默认 + store 覆盖合并后）
// PUT  /api/ai/gate/config           更新配置（admin-only，写 store 配置 "ai_gate"）
//
// 安全口径：配置只含开关/阈值/provider 名，绝不含 API key；decision 记录
// 只存上下文摘要 hash 与截断 JSON，不存 prompt 全文与任何凭证。

// AIGateDecisionsList 分页列出决策时间线。
// 查询参数：page(默认1) page_size(默认20,<=100) decision symbol source
// fail_open(true|false) days(最近 N 天，默认全部)。
func AIGateDecisionsList(c *gin.Context) {
	page := queryInt(c, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize := queryInt(c, "page_size", 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	filter := store.AIGateDecisionFilter{
		Decision: strings.TrimSpace(c.Query("decision")),
		Symbol:   strings.TrimSpace(c.Query("symbol")),
		Source:   strings.TrimSpace(c.Query("source")),
		Limit:    pageSize,
		Offset:   (page - 1) * pageSize,
	}
	if v := strings.TrimSpace(c.Query("fail_open")); v == "true" || v == "false" {
		b := v == "true"
		filter.FailOpen = &b
	}
	if days := queryInt(c, "days", 0); days > 0 {
		filter.SinceMs = time.Now().UnixMilli() - int64(days)*24*3600*1000
	}
	// C1 口径：注入用户且非 admin → 只看本人（含历史无属主 0，与订单接口一致）。
	if uid, injected := ctxUserID(c); injected && !ctxIsAdmin(c) {
		filter.UserID = int64(uid)
	}

	repo := store.GetAIGateDecisionRepo()
	items, err := repo.List(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询决策记录失败: " + err.Error()})
		return
	}
	total, err := repo.Count(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计决策记录失败: " + err.Error()})
		return
	}
	if items == nil {
		items = []*store.AIGateDecisionRecord{}
	}
	c.JSON(http.StatusOK, gin.H{
		"decisions": items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// AIGateDecisionGet 返回单条决策详情（含上下文摘要），属主校验。
func AIGateDecisionGet(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "id 不能为空"})
		return
	}
	rec, err := store.GetAIGateDecisionRepo().GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "查询失败: " + err.Error()})
		return
	}
	if rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "决策记录不存在"})
		return
	}
	if !requireOwner(c, rec.UserID) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"decision": rec})
}

// AIGateStats 聚合统计：总量/评估数/拦截数/fail-open 数/绕过数（前端卡片）。
func AIGateStats(c *gin.Context) {
	var filter store.AIGateDecisionFilter
	if days := queryInt(c, "days", 0); days > 0 {
		filter.SinceMs = time.Now().UnixMilli() - int64(days)*24*3600*1000
	}
	if uid, injected := ctxUserID(c); injected && !ctxIsAdmin(c) {
		filter.UserID = int64(uid)
	}
	stats, err := store.GetAIGateDecisionRepo().Stats(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "统计失败: " + err.Error()})
		return
	}
	blockRate := 0.0
	if stats.Evaluated > 0 {
		blockRate = float64(stats.Blocked) / float64(stats.Evaluated)
	}
	failOpenRate := 0.0
	if stats.Total > 0 {
		failOpenRate = float64(stats.FailOpen) / float64(stats.Total)
	}
	c.JSON(http.StatusOK, gin.H{
		"stats":          stats,
		"block_rate":     blockRate,
		"fail_open_rate": failOpenRate,
	})
}

// AIGateConfigGet 返回合并后的当前配置（env 底 + store 覆盖）。
func AIGateConfigGet(c *gin.Context) {
	cfg := aigate.LoadConfig()
	c.JSON(http.StatusOK, gin.H{"config": cfg.ToMap()})
}

// AIGateConfigPut 更新配置（路由层 admin-only）。只接受白名单字段，
// 归一化后写回 store 配置 "ai_gate"（下次评估即生效，无需重启）。
func AIGateConfigPut(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json: " + err.Error()})
		return
	}
	// 白名单字段（防止往配置里塞无关/敏感键）。
	allowed := map[string]bool{
		"enabled": true, "min_confidence": true, "abstain_action": true,
		"paper_only": true, "timeout_seconds": true, "provider": true,
		"context_bars": true, "excluded_sources": true,
	}
	update := make(map[string]any, len(body))
	for k, v := range body {
		if allowed[k] {
			update[k] = v
		}
	}
	cfg := aigate.LoadConfig()
	cfg.ApplyMap(update) // ApplyMap 末尾已 normalize

	appCfg := store.GetConfig()
	appCfg["ai_gate"] = cfg.ToMap()
	if err := store.SaveConfig(appCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存配置失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "config": aigate.LoadConfig().ToMap()})
}
