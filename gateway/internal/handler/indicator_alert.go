// ── 指标信号告警任务 HTTP API（对标 QuantDinger indicator_signal_alerts）──
//
// REST 层：CRUD + enable/disable + run（立即执行一次）+ history（最近触发）。
// 落库走 store.IndicatorAlertRepo，立即执行/触发历史走注入的 AlertScanService
// （生产为 alerts.Service，测试可注入 fake）。所有权语义与 pystrat 一致：
// 创建写 user_id，单个资源操作 requireOwner，列表按 user_id 过滤（admin 看全部）。
package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/alertexpr"
	"github.com/xiaotian-quant/gateway/internal/alerts"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// AlertScanService 是 handler 对 alerts.Service 的窄接口，便于测试注入 fake。
type AlertScanService interface {
	RunAlert(id string, respectCooldown bool) (*alerts.RunResult, error)
	History(id string, limit int) []alerts.TriggerRecord
}

// AlertSvc 是进程内扫描服务，main 启动时注入；为 nil 时 run/history 返回 503。
var AlertSvc AlertScanService

// SetAlertScanService 注入告警扫描服务（生产接 alerts.Service）。
func SetAlertScanService(s AlertScanService) { AlertSvc = s }

var indicatorAlertRepo = store.NewIndicatorAlertRepo()

const (
	maxAlertNameLen     = 80
	maxAlertMsgLen      = 500
	maxAlertCooldownMin = 10080 // 7 天
)

// alertIntervals 允许的 K 线周期（Binance 公开 K 线支持集合）。
var alertIntervals = map[string]bool{
	"1m": true, "3m": true, "5m": true, "15m": true, "30m": true,
	"1h": true, "2h": true, "4h": true, "6h": true, "8h": true, "12h": true,
	"1d": true, "3d": true, "1w": true,
}

var alertSymbolRe = regexp.MustCompile(`^[A-Z0-9]{2,20}$`)

func alertError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

// alertRestricted 当前请求是否需按属主过滤（登录非 admin）。
func alertRestricted(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

// alertMustGet 按 :id 取任务并做属主校验；失败时已写响应。
func alertMustGet(c *gin.Context) (*store.IndicatorAlertRecord, bool) {
	rec, err := indicatorAlertRepo.GetByID(c.Param("id"))
	if err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if rec == nil {
		alertError(c, http.StatusNotFound, "alert not found")
		return nil, false
	}
	if !requireOwner(c, rec.UserID) {
		return nil, false
	}
	return rec, true
}

func alertToJSON(rec *store.IndicatorAlertRecord) gin.H {
	return gin.H{
		"id":                rec.ID,
		"user_id":           rec.UserID,
		"name":              rec.Name,
		"symbol":            rec.Symbol,
		"interval":          rec.Interval,
		"condition_expr":    rec.ConditionExpr,
		"message":           rec.Message,
		"cooldown_minutes":  rec.CooldownMinutes,
		"last_triggered_at": rec.LastTriggeredAt,
		"last_value":        rec.LastValue,
		"active":            rec.Active,
		"created_at":        rec.CreatedAt,
		"updated_at":        rec.UpdatedAt,
	}
}

// ── Request binding / validation ──

type alertParams struct {
	Name            string `json:"name"`
	Symbol          string `json:"symbol"`
	Interval        string `json:"interval"`
	ConditionExpr   string `json:"condition_expr"`
	Message         string `json:"message"`
	CooldownMinutes *int   `json:"cooldown_minutes"`
	Active          *bool  `json:"active"`
}

// normalize 清洗 + 校验；返回错误消息（空串 = 通过）。
func (p *alertParams) normalize() string {
	p.Name = strings.TrimSpace(p.Name)
	p.Symbol = normalizeAlertSymbol(p.Symbol)
	p.Interval = strings.ToLower(strings.TrimSpace(p.Interval))
	p.ConditionExpr = strings.TrimSpace(p.ConditionExpr)
	p.Message = strings.TrimSpace(p.Message)

	if p.Name == "" || len(p.Name) > maxAlertNameLen {
		return "name is required (≤ 80 chars)"
	}
	if !alertSymbolRe.MatchString(p.Symbol) {
		return "invalid symbol (expect e.g. BTCUSDT)"
	}
	if !alertIntervals[p.Interval] {
		return "invalid interval (1m/3m/5m/15m/30m/1h/2h/4h/6h/8h/12h/1d/3d/1w)"
	}
	if err := ValidateAlertExpr(p.ConditionExpr); err != nil {
		return "invalid condition_expr: " + err.Error()
	}
	if len(p.Message) > maxAlertMsgLen {
		return "message too long (≤ 500 chars)"
	}
	if p.CooldownMinutes != nil && (*p.CooldownMinutes < 0 || *p.CooldownMinutes > maxAlertCooldownMin) {
		return "cooldown_minutes out of range (0..10080)"
	}
	return ""
}

func normalizeAlertSymbol(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// ValidateAlertExpr 校验表达式语法。
func ValidateAlertExpr(expr string) error {
	_, err := alertexpr.Parse(expr)
	return err
}

// ── Handlers ──

// IndicatorAlertList GET /api/alerts/
func IndicatorAlertList(c *gin.Context) {
	filter := map[string]any{}
	if uid, restricted := alertRestricted(c); restricted {
		filter["user_id"] = uid
	}
	recs, err := indicatorAlertRepo.List(filter, 0)
	if err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]gin.H, 0, len(recs))
	for _, rec := range recs {
		items = append(items, alertToJSON(rec))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// IndicatorAlertCreate POST /api/alerts/
func IndicatorAlertCreate(c *gin.Context) {
	var req alertParams
	if err := c.ShouldBindJSON(&req); err != nil {
		alertError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.normalize(); msg != "" {
		alertError(c, http.StatusBadRequest, msg)
		return
	}
	uid, _ := ctxUserID(c)
	cooldown := 60
	if req.CooldownMinutes != nil {
		cooldown = *req.CooldownMinutes
	}
	rec := &store.IndicatorAlertRecord{
		UserID:          int64(uid),
		Name:            req.Name,
		Symbol:          req.Symbol,
		Interval:        req.Interval,
		ConditionExpr:   req.ConditionExpr,
		Message:         req.Message,
		CooldownMinutes: cooldown,
		Active:          req.Active == nil || *req.Active,
	}
	if err := indicatorAlertRepo.Create(rec); err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, alertToJSON(rec))
}

// IndicatorAlertGet GET /api/alerts/:id
func IndicatorAlertGet(c *gin.Context) {
	rec, ok := alertMustGet(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, alertToJSON(rec))
}

// IndicatorAlertUpdate PUT /api/alerts/:id
func IndicatorAlertUpdate(c *gin.Context) {
	rec, ok := alertMustGet(c)
	if !ok {
		return
	}
	var req alertParams
	if err := c.ShouldBindJSON(&req); err != nil {
		alertError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.normalize(); msg != "" {
		alertError(c, http.StatusBadRequest, msg)
		return
	}
	rec.Name = req.Name
	rec.Symbol = req.Symbol
	rec.Interval = req.Interval
	rec.ConditionExpr = req.ConditionExpr
	rec.Message = req.Message
	if req.CooldownMinutes != nil {
		rec.CooldownMinutes = *req.CooldownMinutes
	}
	if req.Active != nil {
		rec.Active = *req.Active
	}
	if err := indicatorAlertRepo.Update(rec); err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, alertToJSON(rec))
}

// IndicatorAlertDelete DELETE /api/alerts/:id
func IndicatorAlertDelete(c *gin.Context) {
	rec, ok := alertMustGet(c)
	if !ok {
		return
	}
	if err := indicatorAlertRepo.Delete(rec.ID); err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "id": rec.ID})
}

// setAlertActive 启停共用逻辑。
func setAlertActive(c *gin.Context, active bool) {
	rec, ok := alertMustGet(c)
	if !ok {
		return
	}
	if err := indicatorAlertRepo.SetActive(rec.ID, active); err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return
	}
	rec.Active = active
	c.JSON(http.StatusOK, alertToJSON(rec))
}

// IndicatorAlertEnable POST /api/alerts/:id/enable
func IndicatorAlertEnable(c *gin.Context) { setAlertActive(c, true) }

// IndicatorAlertDisable POST /api/alerts/:id/disable
func IndicatorAlertDisable(c *gin.Context) { setAlertActive(c, false) }

// IndicatorAlertRun POST /api/alerts/:id/run?force=1
// 立即执行一次：返回求值结果与是否触发；force=1 忽略冷却（仍通知并刷新冷却起点）。
func IndicatorAlertRun(c *gin.Context) {
	rec, ok := alertMustGet(c)
	if !ok {
		return
	}
	if AlertSvc == nil {
		alertError(c, http.StatusServiceUnavailable, "alert scan service not initialized")
		return
	}
	force := c.Query("force") == "1" || strings.EqualFold(c.Query("force"), "true")
	res, err := AlertSvc.RunAlert(rec.ID, !force)
	if err != nil {
		alertError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// IndicatorAlertHistory GET /api/alerts/:id/history?limit=20
// 最近触发记录：优先内存环形（进程内），为空时用 last_triggered_at 兜底。
func IndicatorAlertHistory(c *gin.Context) {
	rec, ok := alertMustGet(c)
	if !ok {
		return
	}
	limit := 20
	if n, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && n > 0 {
		limit = n
		if limit > 100 {
			limit = 100
		}
	}
	var entries []alerts.TriggerRecord
	if AlertSvc != nil {
		entries = AlertSvc.History(rec.ID, limit)
	}
	if len(entries) == 0 && rec.LastTriggeredAt > 0 {
		entries = []alerts.TriggerRecord{{
			At:       rec.LastTriggeredAt,
			Symbol:   rec.Symbol,
			Interval: rec.Interval,
			Expr:     rec.ConditionExpr,
			Value:    rec.LastValue,
		}}
	}
	c.JSON(http.StatusOK, gin.H{"items": entries, "total": len(entries)})
}
