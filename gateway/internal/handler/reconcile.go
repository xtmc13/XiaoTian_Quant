package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/reconcile"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── A8 对账体系 REST ─────────────────────────────────────────
// 路由全部挂 AuthRequired（router.go），差异/偏差按属主过滤：
// 普通登录用户只见本人（user_id 匹配 + 历史无属主），admin 全量。

// reconcileSvc 对账调度服务（main 启动时注入，便于 status 读取运行时状态）。
var reconcileSvc *reconcile.Service

// SetReconcileService 注入对账服务（生产为 reconcile.Service）。
func SetReconcileService(svc *reconcile.Service) { reconcileSvc = svc }

func reconcileRepo() *store.ReconcileRepo { return store.NewReconcileRepo() }

// listOwnerFilter 从鉴权上下文取属主过滤：非 admin 登录用户 → 只看本人+无属主。
// 返回 0 表示无过滤（admin/未注入用户保持全量，与项目惯例一致）。
func listOwnerFilter(c *gin.Context) int64 {
	uid, injected := ctxUserID(c)
	if !injected || ctxIsAdmin(c) {
		return 0
	}
	return int64(uid)
}

// ReconcileDiffsList GET /api/reconcile/diffs?status=&type=&exchange=&limit=&offset=
func ReconcileDiffsList(c *gin.Context) {
	repo := reconcileRepo()
	limit := queryInt(c, "limit", 100)
	offset := queryInt(c, "offset", 0)
	diffs, err := repo.ListDiffs(listOwnerFilter(c), c.Query("status"), c.Query("type"), c.Query("exchange"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list diffs failed"})
		return
	}
	if diffs == nil {
		diffs = []*store.ReconcileDiff{}
	}
	c.JSON(http.StatusOK, gin.H{"diffs": diffs})
}

// ReconcileDiffResolve POST /api/reconcile/diffs/:id/resolve
// body: {"action":"accept_exchange"|"accept_local"|"ignore"}
//   - accept_exchange：认可交易所为准（拉平本地持仓，并写审计日志）
//   - accept_local：认可本地记录（忽略交易所差异）
//   - ignore：仅关闭差异
func ReconcileDiffResolve(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid diff id"})
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action is required"})
		return
	}
	switch body.Action {
	case "accept_exchange", "accept_local", "ignore":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be accept_exchange|accept_local|ignore"})
		return
	}

	repo := reconcileRepo()
	diff, err := repo.GetDiff(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "diff not found"})
		return
	}
	// 属主校验：本人或 admin（历史无属主差异所有人可见，与项目惯例一致）。
	if !ownsResource(c, diff.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "forbidden: not the resource owner"})
		return
	}

	resolvedBy := resolveActor(c)
	if body.Action == "accept_exchange" && isPositionDiff(diff) {
		// 认可交易所：本地持仓拉平到交易所值（留审计日志）。
		if err := applyExchangePosition(diff, resolvedBy); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
	}

	updated, err := repo.ResolveDiff(id, body.Action, resolvedBy)
	if err != nil {
		if err.Error() == "reconcile record not found" {
			c.JSON(http.StatusConflict, gin.H{"error": "diff already resolved"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "resolve failed"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// isPositionDiff 该差异是否关联持仓数量。
func isPositionDiff(d *store.ReconcileDiff) bool {
	return d.DiffType == "position_quantity" ||
		d.DiffType == "position_missing_exchange" ||
		d.DiffType == "position_missing_local"
}

// applyExchangePosition 把本地持仓修正为差异记录里的交易所值（人工确认版自动修正）。
func applyExchangePosition(d *store.ReconcileDiff, actor string) error {
	if d.ExchangeQty == 0 {
		return nil // 交易所已无持仓：关闭本地持仓
	}
	posRepo := store.NewPositionRepo()
	list, err := posRepo.List(map[string]any{"exchange": d.Exchange, "symbol": d.Symbol, "status": "OPEN"}, 1)
	if err != nil {
		return err
	}
	newSide := "LONG"
	if d.ExchangeQty < 0 {
		newSide = "SHORT"
	}
	newQty := d.ExchangeQty
	if newQty < 0 {
		newQty = -newQty
	}
	if len(list) == 0 {
		// 本地缺失 → 以交易所值重建持仓
		return posRepo.Create(&store.PositionRecord{
			UserID:        d.UserID,
			Symbol:        d.Symbol,
			Side:          newSide,
			Quantity:      newQty,
			AvgEntryPrice: d.ExchangeEntryPx,
			Exchange:      d.Exchange,
			Status:        "OPEN",
		})
	}
	p := list[0]
	p.Quantity = newQty
	p.Side = newSide
	if d.ExchangeEntryPx > 0 {
		p.AvgEntryPrice = d.ExchangeEntryPx
	}
	p.CostBasis = p.Quantity * p.AvgEntryPrice
	if err := posRepo.Update(p); err != nil {
		return err
	}
	return reconcileRepo().LogAudit("manual_fix_position", d.Exchange, d.Symbol, d.UserID,
		"actor="+actor+" qty="+strconv.FormatFloat(newQty, 'f', -1, 64))
}

// resolveActor 记录解决人（admin / user:<id> / anonymous）。
func resolveActor(c *gin.Context) string {
	uid, injected := ctxUserID(c)
	if !injected {
		return "anonymous"
	}
	if ctxIsAdmin(c) {
		return "admin"
	}
	return "user:" + strconv.Itoa(uid)
}

// ReconcileStatus GET /api/reconcile/status —— 任务/差异/配置总览。
func ReconcileStatus(c *gin.Context) {
	repo := reconcileRepo()
	openDiffs, _ := repo.CountOpenDiffs()
	audit, _ := repo.ListAudit(20)

	resp := map[string]any{
		"open_diffs": openDiffs,
		"audit":      audit,
	}
	if reconcileSvc != nil {
		for k, v := range reconcileSvc.Status() {
			resp[k] = v
		}
	} else {
		resp["config"] = reconcile.LoadConfig(repo).ConfigSnapshot()
	}
	c.JSON(http.StatusOK, resp)
}

// ReconcileDeviationsList GET /api/reconcile/deviations?kind=&status=&exchange=&limit=&offset=
func ReconcileDeviationsList(c *gin.Context) {
	repo := reconcileRepo()
	limit := queryInt(c, "limit", 100)
	offset := queryInt(c, "offset", 0)
	devs, err := repo.ListDeviations(listOwnerFilter(c), c.Query("kind"), c.Query("status"), c.Query("exchange"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list deviations failed"})
		return
	}
	if devs == nil {
		devs = []*store.ReconcileDeviation{}
	}
	c.JSON(http.StatusOK, gin.H{"deviations": devs})
}

// ReconcileDeviationResolve POST /api/reconcile/deviations/:id/resolve —— 人工确认偏差。
func ReconcileDeviationResolve(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deviation id"})
		return
	}
	repo := reconcileRepo()
	target, err := repo.GetDeviation(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deviation not found"})
		return
	}
	if !ownsResource(c, target.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"detail": "forbidden: not the resource owner"})
		return
	}
	updated, err := repo.ResolveDeviation(id, resolveActor(c))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "deviation already resolved"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// ReconcileReportedPnLList GET /api/reconcile/reported-pnl?exchange=&days=&status=&limit=&offset=
// 回报 PnL 对账记录：admin 全量，普通用户看全账户行（user_id=0）+ 本人行。
func ReconcileReportedPnLList(c *gin.Context) {
	repo := reconcileRepo()
	limit := queryInt(c, "limit", 100)
	offset := queryInt(c, "offset", 0)
	var sinceMs int64
	if days := queryInt(c, "days", 0); days > 0 {
		sinceMs = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	}
	checks, err := repo.ListReportedPnLChecks(listOwnerFilter(c), c.Query("exchange"), c.Query("status"), sinceMs, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list reported pnl checks failed"})
		return
	}
	if checks == nil {
		checks = []*store.ReportedPnLCheckRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"checks": checks})
}

// ReconcileReportedPnLRun POST /api/reconcile/reported-pnl/run?days= —— admin 手动触发一轮。
// days>0 时覆盖默认窗口（近 reported_pnl_window_h 小时）。
func ReconcileReportedPnLRun(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	if reconcileSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reconcile service not ready"})
		return
	}
	days := queryInt(c, "days", 0)
	msg, err := reconcileSvc.RunReportedPnL(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": msg})
}

// ReconcileConfigGet GET /api/reconcile/config —— 当前生效配置（登录可见）。
func ReconcileConfigGet(c *gin.Context) {
	if reconcileSvc != nil {
		c.JSON(http.StatusOK, reconcileSvc.CurrentConfig().ConfigSnapshot())
		return
	}
	c.JSON(http.StatusOK, reconcile.LoadConfig(reconcileRepo()).ConfigSnapshot())
}

// ReconcileConfigPut PUT /api/reconcile/config —— 更新阈值/周期（admin-only）。
// body 可含: interval_sec / slippage_pct / stuck_timeout_sec / auto_fix / enabled
//
//	min_drift / reported_pnl_window_h / reported_pnl_pct
func ReconcileConfigPut(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	// 白名单校验后转字符串存设置表。
	allowed := map[string]bool{
		"interval_sec": true, "slippage_pct": true, "stuck_timeout_sec": true,
		"auto_fix": true, "enabled": true, "min_drift": true,
		"reported_pnl_window_h": true, "reported_pnl_pct": true,
	}
	overrides := map[string]string{}
	for k, v := range body {
		if !allowed[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown config key: " + k})
			return
		}
		switch k {
		case "auto_fix", "enabled":
			if _, ok := v.(bool); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": k + " must be boolean"})
				return
			}
		default:
			if f, ok := v.(float64); !ok || f <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": k + " must be a positive number"})
				return
			}
		}
		overrides[k] = toSettingValue(v)
	}
	if reconcileSvc != nil {
		c.JSON(http.StatusOK, reconcileSvc.UpdateConfig(overrides).ConfigSnapshot())
		return
	}
	repo := reconcileRepo()
	for k, v := range overrides {
		_ = repo.SetSetting(k, v)
	}
	c.JSON(http.StatusOK, reconcile.LoadConfig(repo).ConfigSnapshot())
}

func toSettingValue(v any) string {
	switch n := v.(type) {
	case bool:
		if n {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case string:
		return n
	}
	return ""
}

func queryInt(c *gin.Context, name string, def int) int {
	if v := c.Query(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
