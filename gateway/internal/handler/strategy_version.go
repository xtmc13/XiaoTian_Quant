package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 策略配置版本快照（对标 QuantDinger strategy_v2 版本快照能力） ──
//
// POST   /api/strategies/configs/:id/versions               手动打快照（body 可带 note）
// GET    /api/strategies/configs/:id/versions               版本列表
// GET    /api/strategies/configs/:id/versions/:vid          版本详情（payload 为完整配置快照）
// POST   /api/strategies/configs/:id/versions/:vid/restore  恢复该版本（写回 strategy_configs，
//                                                           恢复前自动打一条"恢复前"快照）
//
// 快照不可变：没有更新/删除入口，恢复只追加新版本。
// 全部端点做属主校验（ownsResource，require_owner.go 的现有口径）；
// 错误统一 {"error": "..."} 中文。

// strategyVersionOwner 解析策略属主：内存+DB 双查（store.GetStrategyConfig
// 在 db 可用时本身以 DB 为准并回灌内存），保证 JSON-file-only 的遗留部署
// 也能解析到属主。第二个返回值为策略是否存在。
func strategyVersionOwner(strategyID string) (int64, bool) {
	if item := store.GetStrategyConfig(strategyID); item != nil {
		return getInt64Of(item, "user_id"), true
	}
	if rec, err := store.NewStrategyConfigRepo().GetByID(strategyID); err == nil && rec != nil {
		return rec.UserID, true
	}
	return 0, false
}

// requireStrategyVersionAccess 统一入口：策略不存在 → 404 {"error"}；
// 非属主（且非 admin/未注入用户）→ 403 {"error"}。通过返回 true。
func requireStrategyVersionAccess(c *gin.Context, strategyID string) bool {
	ownerID, exists := strategyVersionOwner(strategyID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
		return false
	}
	if !ownsResource(c, ownerID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该策略"})
		return false
	}
	return true
}

// strategyVersionPayloadOf 把 strategy_configs 记录打成快照 payload：
// 用 ToMap() 的 JSON 视图（与内存/JSON-file 表示一致），恢复时经
// StrategyConfigRecordFromMap 还原，字段不丢。
func strategyVersionPayloadOf(rec *store.StrategyConfigRecord) string {
	data, err := json.Marshal(rec.ToMap())
	if err != nil {
		return "{}"
	}
	return string(data)
}

// autoSnapshotStrategyVersion 写库前自动打"更新前"快照：
// 读 DB 当前记录（写库前调用，即为旧状态）；失败仅记日志，不影响更新。
func autoSnapshotStrategyVersion(strategyID, note string) {
	rec, err := store.NewStrategyConfigRepo().GetByID(strategyID)
	if err != nil || rec == nil {
		// 策略只在内存/JSON-file 的遗留场景没有 DB 旧状态可拍，静默跳过。
		return
	}
	ver := &store.StrategyVersionRecord{
		UserID:     rec.UserID,
		StrategyID: rec.ID,
		Payload:    strategyVersionPayloadOf(rec),
		Note:       note,
	}
	if err := store.NewStrategyVersionRepo().Create(ver); err != nil {
		log.Printf("[strategy-version] auto snapshot failed (strategy=%s): %v", strategyID, err)
	}
}

// CreateStrategyVersion godoc
// POST /strategies/configs/:id/versions
func CreateStrategyVersion(c *gin.Context) {
	strategyID := c.Param("id")
	if !requireStrategyVersionAccess(c, strategyID) {
		return
	}
	// body 可空（note 可选）；解析失败按无 note 处理。
	var body struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&body)

	rec, err := store.NewStrategyConfigRepo().GetByID(strategyID)
	if err != nil || rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
		return
	}
	ver := &store.StrategyVersionRecord{
		UserID:     rec.UserID,
		StrategyID: rec.ID,
		Payload:    strategyVersionPayloadOf(rec),
		Note:       strings.TrimSpace(body.Note),
	}
	if err := store.NewStrategyVersionRepo().Create(ver); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建版本快照失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":          ver.ID,
		"strategy_id": ver.StrategyID,
		"version":     ver.Version,
		"note":        ver.Note,
		"created_at":  ver.CreatedAt,
	})
}

// ListStrategyVersions godoc
// GET /strategies/configs/:id/versions
func ListStrategyVersions(c *gin.Context) {
	strategyID := c.Param("id")
	if !requireStrategyVersionAccess(c, strategyID) {
		return
	}
	recs, err := store.NewStrategyVersionRepo().ListByStrategy(strategyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取版本列表失败"})
		return
	}
	type versionItem struct {
		ID        string `json:"id"`
		Version   int    `json:"version"`
		Note      string `json:"note"`
		CreatedAt int64  `json:"created_at"`
	}
	items := make([]versionItem, 0, len(recs))
	for _, rec := range recs {
		items = append(items, versionItem{ID: rec.ID, Version: rec.Version, Note: rec.Note, CreatedAt: rec.CreatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"versions": items})
}

// GetStrategyVersion godoc
// GET /strategies/configs/:id/versions/:vid
func GetStrategyVersion(c *gin.Context) {
	strategyID := c.Param("id")
	if !requireStrategyVersionAccess(c, strategyID) {
		return
	}
	vid, err := strconv.Atoi(c.Param("vid"))
	if err != nil || vid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "版本号无效"})
		return
	}
	ver, err := store.NewStrategyVersionRepo().Get(strategyID, vid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取版本失败"})
		return
	}
	if ver == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "版本不存在"})
		return
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(ver.Payload), &payload); err != nil || payload == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "版本内容损坏"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":          ver.ID,
		"strategy_id": ver.StrategyID,
		"version":     ver.Version,
		"note":        ver.Note,
		"created_at":  ver.CreatedAt,
		"payload":     payload,
	})
}

// RestoreStrategyVersion godoc
// POST /strategies/configs/:id/versions/:vid/restore
func RestoreStrategyVersion(c *gin.Context) {
	strategyID := c.Param("id")
	if !requireStrategyVersionAccess(c, strategyID) {
		return
	}
	vid, err := strconv.Atoi(c.Param("vid"))
	if err != nil || vid <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "版本号无效"})
		return
	}
	verRepo := store.NewStrategyVersionRepo()
	ver, err := verRepo.Get(strategyID, vid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取版本失败"})
		return
	}
	if ver == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "版本不存在"})
		return
	}

	// 恢复前自动打一条"恢复前"快照（失败仅记日志，不阻断恢复）。
	autoSnapshotStrategyVersion(strategyID, "恢复前自动快照")

	var payload map[string]any
	if err := json.Unmarshal([]byte(ver.Payload), &payload); err != nil || payload == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "版本内容损坏，无法恢复"})
		return
	}
	restored := store.StrategyConfigRecordFromMap(payload)
	// id/属主以现库记录为准：防止篡改 payload 改写身份或转移属主。
	cur, err := store.NewStrategyConfigRepo().GetByID(strategyID)
	if err != nil || cur == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "策略不存在"})
		return
	}
	restored.ID = strategyID
	restored.UserID = cur.UserID
	if restored.CreatedAt == 0 {
		restored.CreatedAt = cur.CreatedAt
	}
	if restored.ConfigJSON == "" {
		restored.ConfigJSON = "{}"
	}
	if err := store.NewStrategyConfigRepo().Update(restored); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢复失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "restored_version": vid})
}
