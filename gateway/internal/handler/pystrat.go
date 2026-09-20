// ── 用户 Python 策略 HTTP API（契约化运行时 v1）──
//
// REST 层：CRUD + validate + start/stop + logs + status。落库走
// store.PyStrategyRepo，运行控制走注入的 PyStratService（生产为
// pystrat.Runner，测试为 fake）。所有权语义与 dca_bot.go 一致：
// 创建写 user_id，单个资源操作 requireOwner，列表按 user_id 过滤
// （admin/未注入用户看全部）。源码保存自动打版本快照到
// xt_strategy_versions（复用 0014 机制）。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/pystrat"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// PyStratService 是 handler 对 pystrat.Runner 的窄接口，便于测试注入 fake。
type PyStratService interface {
	Start(rec *store.PyStrategyRecord) error
	Stop(id string) error
	IsRunning(id string) bool
	Status(id string) pystrat.StrategyStatus
	Logs(id string) []pystrat.LogEntry
}

// PyStratSvc 是当前进程内驱动 Python 策略的服务，main 启动时注入。
var PyStratSvc PyStratService

// SetPyStratService 注入 Python 策略运行时（生产接 pystrat.Runner）。
func SetPyStratService(s PyStratService) { PyStratSvc = s }

// pyStratRepo 是无状态 typed CRUD（内部仅互斥锁），进程级复用即可。
var pyStratRepo = store.NewPyStrategyRepo()
var pyStratVersionRepo = store.NewStrategyVersionRepo()

const maxPyStratCodeBytes = 64 * 1024

// ── Helpers ──

func pyStratError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

// pyStratMustGet 按 :id 取记录并做属主校验；失败时已写响应。
func pyStratMustGet(c *gin.Context) (*store.PyStrategyRecord, bool) {
	rec, err := pyStratRepo.GetByID(c.Param("id"))
	if err != nil {
		pyStratError(c, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if rec == nil {
		pyStratError(c, http.StatusNotFound, "python strategy not found")
		return nil, false
	}
	if !requireOwner(c, rec.UserID) {
		return nil, false
	}
	return rec, true
}

// pyStratRestricted 当前请求是否需按属主过滤（登录非 admin）。
func pyStratRestricted(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

func pyStratIsRunning(id string) bool {
	return PyStratSvc != nil && PyStratSvc.IsRunning(id)
}

func pyStratToJSON(rec *store.PyStrategyRecord, running bool) gin.H {
	return gin.H{
		"id":          rec.ID,
		"user_id":     rec.UserID,
		"name":        rec.Name,
		"symbol":      rec.Symbol,
		"interval":    rec.Interval,
		"direction":   rec.Direction,
		"params_json": rec.ParamsJSON,
		"code":        rec.Code,
		"version":     rec.Version,
		"status":      rec.Status,
		"error":       rec.Error,
		"paper":       rec.Paper,
		"bot_id":      rec.BotID,
		"market":      rec.Market,
		"leverage":    rec.Leverage,
		"margin_mode": rec.MarginMode,
		"created_at":  rec.CreatedAt,
		"updated_at":  rec.UpdatedAt,
		"is_running":  running,
	}
}

// snapshotVersion 把当前记录打一条版本快照到 xt_strategy_versions，
// 并把分配的 version 写回记录（与 0014 策略版本同一机制）。
func snapshotVersion(rec *store.PyStrategyRecord, note string) {
	payload, err := json.Marshal(pyStratToJSON(rec, false))
	if err != nil {
		return
	}
	ver := &store.StrategyVersionRecord{
		UserID:     rec.UserID,
		StrategyID: rec.ID,
		Payload:    string(payload),
		Note:       note,
	}
	if err := pyStratVersionRepo.Create(ver); err != nil {
		return
	}
	rec.Version = ver.Version
}

// ── Request binding / validation ──

type pyStratParams struct {
	Name       string          `json:"name"`
	Symbol     string          `json:"symbol"`
	Interval   string          `json:"interval"`
	Direction  string          `json:"direction"`
	ParamsJSON json.RawMessage `json:"params_json"`
	Code       string          `json:"code"`
	Paper      *bool           `json:"paper"`
	Market     string          `json:"market"`
	Leverage   int             `json:"leverage"`
	MarginMode string          `json:"margin_mode"`
}

func (p *pyStratParams) validate() string {
	if strings.TrimSpace(p.Name) == "" {
		return "name 不能为空"
	}
	if strings.TrimSpace(p.Symbol) == "" {
		return "symbol 不能为空"
	}
	if len(p.Code) == 0 {
		return "code 不能为空"
	}
	if len(p.Code) > maxPyStratCodeBytes {
		return "code 超过 64KB 上限"
	}
	if len(p.ParamsJSON) > 0 {
		if _, err := p.paramsObject(); err != nil {
			return "params_json 必须是 JSON 对象（如 {} 或 {\"fast\": 9}），字符串内容也须是 JSON 对象"
		}
	}
	switch p.Direction {
	case "", "long", "short", "both":
	default:
		return "direction 必须是 long|short|both"
	}
	switch p.Market {
	case "", store.PyStratMarketSpot, store.PyStratMarketFutures:
	default:
		return "market 必须是 spot|futures"
	}
	if p.Leverage < 0 || p.Leverage > store.PyStratMaxLeverage {
		return fmt.Sprintf("leverage 必须在 [1,%d] 区间（0 表示用默认值 1）", store.PyStratMaxLeverage)
	}
	switch p.MarginMode {
	case "", store.PyStratMarginCross, store.PyStratMarginIsolated:
	default:
		return "margin_mode 必须是 cross|isolated"
	}
	return ""
}

// paramsObject 把 params_json（JSON 对象或 JSON 字符串包裹的对象）解析为 map。
func (p *pyStratParams) paramsObject() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(p.ParamsJSON, &m); err == nil {
		return m, nil
	}
	var s string
	if err := json.Unmarshal(p.ParamsJSON, &s); err == nil {
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return nil, err
		}
		return m, nil
	}
	return nil, errors.New("params_json must be a JSON object or a JSON string of an object")
}

func (p *pyStratParams) paramsJSONOrDefault() string {
	if len(p.ParamsJSON) == 0 {
		return "{}"
	}
	m, err := p.paramsObject()
	if err != nil {
		return "{}"
	}
	normalized, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(normalized)
}

// ── Handlers ──

// PyStratList: GET /api/pystrategies → 当前用户策略列表（附 is_running）。
func PyStratList(c *gin.Context) {
	filter := map[string]any{}
	if uid, restricted := pyStratRestricted(c); restricted {
		filter["user_id"] = uid
	}
	recs, err := pyStratRepo.List(filter, 0)
	if err != nil {
		pyStratError(c, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]gin.H, 0, len(recs))
	for _, rec := range recs {
		items = append(items, pyStratToJSON(rec, pyStratIsRunning(rec.ID)))
	}
	c.JSON(http.StatusOK, gin.H{"strategies": items})
}

// PyStratCreate: POST /api/pystrategies → 校验落库（draft）+ 版本快照 v1。
func PyStratCreate(c *gin.Context) {
	var body pyStratParams
	if err := c.ShouldBindJSON(&body); err != nil {
		pyStratError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		pyStratError(c, http.StatusBadRequest, msg)
		return
	}
	paper := true
	if body.Paper != nil {
		paper = *body.Paper
	}
	uid, _ := ctxUserID(c)
	rec := &store.PyStrategyRecord{
		UserID:     int64(uid),
		Name:       strings.TrimSpace(body.Name),
		Symbol:     gridNormalizeSymbol(body.Symbol),
		Interval:   body.Interval,
		Direction:  body.Direction,
		ParamsJSON: body.paramsJSONOrDefault(),
		Code:       body.Code,
		Status:     store.PyStratStatusDraft,
		Paper:      paper,
		Market:     body.Market,
		Leverage:   body.Leverage,
		MarginMode: body.MarginMode,
	}
	if rec.Direction == "" {
		rec.Direction = "long"
	}
	if err := pyStratRepo.Create(rec); err != nil {
		pyStratError(c, http.StatusInternalServerError, err.Error())
		return
	}
	snapshotVersion(rec, "create")
	_ = pyStratRepo.Update(rec) // 回写 version 列
	c.JSON(http.StatusOK, pyStratToJSON(rec, false))
}

// PyStratGet: GET /api/pystrategies/:id → 记录 + is_running。
func PyStratGet(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, pyStratToJSON(rec, pyStratIsRunning(rec.ID)))
}

// PyStratUpdate: PUT /api/pystrategies/:id → 运行中 409；改代码打新快照。
func PyStratUpdate(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	if pyStratIsRunning(rec.ID) {
		pyStratError(c, http.StatusConflict, "运行中的策略不能修改，请先停止")
		return
	}
	var body pyStratParams
	if err := c.ShouldBindJSON(&body); err != nil {
		pyStratError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		pyStratError(c, http.StatusBadRequest, msg)
		return
	}
	rec.Name = strings.TrimSpace(body.Name)
	rec.Symbol = gridNormalizeSymbol(body.Symbol)
	rec.Interval = body.Interval
	rec.Direction = body.Direction
	rec.ParamsJSON = body.paramsJSONOrDefault()
	rec.Code = body.Code
	rec.Market = body.Market
	rec.Leverage = body.Leverage
	rec.MarginMode = body.MarginMode
	if body.Paper != nil {
		rec.Paper = *body.Paper
	}
	if err := pyStratRepo.Update(rec); err != nil {
		pyStratError(c, http.StatusInternalServerError, err.Error())
		return
	}
	snapshotVersion(rec, "update")
	_ = pyStratRepo.Update(rec)
	c.JSON(http.StatusOK, pyStratToJSON(rec, false))
}

// PyStratDelete: DELETE /api/pystrategies/:id → 运行中先停再删。
func PyStratDelete(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	if pyStratIsRunning(rec.ID) {
		if err := PyStratSvc.Stop(rec.ID); err != nil {
			pyStratError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := pyStratRepo.Delete(rec.ID); err != nil {
		pyStratError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": rec.ID})
}

// PyStratValidate: POST /api/pystrategies/:id/validate → 静态校验（带行号）
// + 沙箱加载校验（import 白名单 AST 终审 + manifest 校验 + initialize 试跑）。
func PyStratValidate(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	resp := gin.H{"valid": true, "issues": []pystrat.ValidationIssue{}, "checked_at": time.Now().UnixMilli()}

	issues := pystrat.ValidateStatic(rec.Code)
	if len(issues) > 0 {
		resp["valid"] = false
		resp["issues"] = issues
	}

	// 沙箱终审：AST 安全校验 + manifest 解析 + initialize(context) 试跑。
	// 静态层已过时不阻塞，但沙箱拒绝必须呈现。
	sandbox := pyStratSandboxFactory()
	defer sandbox.Close()
	var params map[string]any
	_ = json.Unmarshal([]byte(rec.ParamsJSON), &params)
	if _, err := sandbox.Load(c.Request.Context(), rec.Code, params,
		normalizePyStratSymbol(rec.Symbol), rec.Interval); err != nil {
		resp["valid"] = false
		resp["sandbox_error"] = err.Error()
		// 静态层没报出行号时，把沙箱错误作为整份代码级 issue 一并返回
		if len(issues) == 0 {
			resp["issues"] = []pystrat.ValidationIssue{{Line: 0, Code: "SANDBOX", Message: err.Error()}}
		}
	}

	if err := pyStratRepo.UpdateStatus(rec.ID, rec.Status, sandboxErrorText(resp)); err == nil {
		rec.Error = sandboxErrorText(resp)
	}
	c.JSON(http.StatusOK, resp)
}

func sandboxErrorText(resp gin.H) string {
	if v, ok := resp["valid"].(bool); ok && v {
		return ""
	}
	if msg, ok := resp["sandbox_error"].(string); ok {
		return msg
	}
	if arr, ok := resp["issues"].([]pystrat.ValidationIssue); ok && len(arr) > 0 {
		return arr[0].String()
	}
	return "校验未通过"
}

// PyStratStart: POST /api/pystrategies/:id/start → 仅非 active 可启动。
// paper=0 的实盘闸校验在 runner 内（依赖注入的 gate）。
func PyStratStart(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	if rec.Status == store.PyStratStatusActive || pyStratIsRunning(rec.ID) {
		pyStratError(c, http.StatusConflict, "策略已在运行")
		return
	}
	if PyStratSvc == nil {
		pyStratError(c, http.StatusInternalServerError, "pystrat runner 未初始化")
		return
	}
	if err := PyStratSvc.Start(rec); err != nil {
		// 启动失败把原因落 error 列，便于前端徽标展示
		_ = pyStratRepo.UpdateStatus(rec.ID, rec.Status, err.Error())
		rec.Error = err.Error()
		pyStratError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": true, "id": rec.ID, "status": store.PyStratStatusActive})
}

// PyStratStop: POST /api/pystrategies/:id/stop → 运行中才停。
func PyStratStop(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	if !pyStratIsRunning(rec.ID) {
		pyStratError(c, http.StatusConflict, "策略未在运行")
		return
	}
	if err := PyStratSvc.Stop(rec.ID); err != nil {
		pyStratError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"stopped": true, "id": rec.ID})
}

// PyStratLogs: GET /api/pystrategies/:id/logs?limit=200 → 内存环形缓冲日志。
func PyStratLogs(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	logs := PyStratSvc.Logs(rec.ID)
	if logs == nil {
		logs = []pystrat.LogEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "is_running": pyStratIsRunning(rec.ID)})
}

// PyStratStatus: GET /api/pystrategies/:id/status → 落库状态 + 运行时细节。
func PyStratStatus(c *gin.Context) {
	rec, ok := pyStratMustGet(c)
	if !ok {
		return
	}
	runtime := pystrat.StrategyStatus{}
	if PyStratSvc != nil {
		runtime = PyStratSvc.Status(rec.ID)
	}
	c.JSON(http.StatusOK, gin.H{
		"id":         rec.ID,
		"status":     rec.Status,
		"error":      rec.Error,
		"paper":      rec.Paper,
		"version":    rec.Version,
		"is_running": runtime.Running,
		"runtime":    runtime,
	})
}
