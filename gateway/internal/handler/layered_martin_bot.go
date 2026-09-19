package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 分层马丁格尔机器人 HTTP API（A1.3）──
//
// REST 层：CRUD + start/stop，落库走 store.LayeredMartinRepo（主表 + 每组
// 状态 groups + 成交 orders），运行控制走注入的 LayeredMartinService
// （生产为 lmartin.Runner，测试为 fake）。所有权语义与 grid/dca 一致：
// 创建写 user_id，单个资源操作 requireOwner，列表按 user_id 过滤
// （admin/未注入用户看全部）。

// LayeredMartinService 是 handler 对分层马丁 Runner 的窄接口，便于测试注入 fake。
type LayeredMartinService interface {
	StartBot(rec *store.LayeredMartinBotRecord, groups []*store.LayeredMartinGroupRecord, currentPrice float64) error
	StopBot(botID string, status string) error
	IsRunning(botID string) bool
}

// LayeredMartinSvc 是当前进程内驱动分层马丁机器人的服务，main 启动时注入。
var LayeredMartinSvc LayeredMartinService

// SetLayeredMartinService 注入分层马丁机器人运行时（生产接 lmartin.Runner）。
func SetLayeredMartinService(s LayeredMartinService) { LayeredMartinSvc = s }

// lmPriceSource 取 symbol 实时价；生产为 BinanceWS 内存价（零网络），
// 测试可用 SetLMPriceSource 换成假源。返回 <=0 表示行情未就绪。
var lmPriceSource = func(symbol string) float64 {
	if a := app.Get(); a != nil && a.BinanceWS != nil {
		return a.BinanceWS.GetPrice(symbol)
	}
	return 0
}

// SetLMPriceSource 覆盖实时价来源（测试用）。
func SetLMPriceSource(fn func(symbol string) float64) { lmPriceSource = fn }

// lmBotRepo 是无状态 typed CRUD（内部仅一把互斥锁），进程级复用即可。
var lmBotRepo = store.NewLayeredMartinRepo()

// ── Helpers ──

func lmBotError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

// lmBotMustGet 按 :id 取记录；不存在返回 404，出错 500；
// 存在但属他人（且非 admin、已注入用户）返回 403（资源级越权防护）。
func lmBotMustGet(c *gin.Context) (*store.LayeredMartinBotRecord, bool) {
	rec, err := lmBotRepo.GetByID(c.Param("id"))
	if err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if rec == nil {
		lmBotError(c, http.StatusNotFound, "layered martin bot not found")
		return nil, false
	}
	if !requireOwner(c, rec.UserID) {
		return nil, false
	}
	return rec, true
}

// lmBotRestricted 当前请求是否需按属主过滤（登录非 admin）。
func lmBotRestricted(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

func lmBotIsRunning(id string) bool {
	return LayeredMartinSvc != nil && LayeredMartinSvc.IsRunning(id)
}

// ── Request binding / validation ──

type lmGroupParams struct {
	QuoteAmount float64 `json:"quote_amount"`
	Multiplier  float64 `json:"multiplier"`
	MaxLayers   int     `json:"max_layers"`
	BudgetCap   float64 `json:"budget_cap"`
}

type lmBotParams struct {
	Name              string          `json:"name"`
	Symbol            string          `json:"symbol"`
	Exchange          string          `json:"exchange"`
	PriceDeviationPct float64         `json:"price_deviation_pct"`
	TakeProfitPct     float64         `json:"take_profit_pct"`
	StopLossPct       float64         `json:"stop_loss_pct"`
	TrailingEnabled   bool            `json:"trailing_enabled"`
	Groups            []lmGroupParams `json:"groups"`
}

// validate 校验创建/修改共用参数，返回错误消息（空串表示通过）。
func (p *lmBotParams) validate() string {
	if strings.TrimSpace(p.Symbol) == "" {
		return "symbol 不能为空"
	}
	if p.PriceDeviationPct <= 0 || p.PriceDeviationPct >= 1 {
		return "price_deviation_pct 必须在 (0, 1) 区间（如 0.03 表示 3%）"
	}
	if p.TakeProfitPct < 0 || p.StopLossPct < 0 {
		return "take_profit_pct / stop_loss_pct 不能为负数"
	}
	if p.TakeProfitPct >= 1 || p.StopLossPct >= 1 {
		return "take_profit_pct / stop_loss_pct 必须小于 1（如 0.05 表示 5%）"
	}
	if len(p.Groups) == 0 {
		return "至少需要一个分组"
	}
	if len(p.Groups) > 20 {
		return "分组数量最多 20 个"
	}
	for i, g := range p.Groups {
		if g.QuoteAmount <= 0 {
			return sprintfGroupErr(i, "quote_amount 必须大于 0")
		}
		if g.Multiplier < 1 {
			return sprintfGroupErr(i, "multiplier 至少为 1")
		}
		if g.MaxLayers < 1 {
			return sprintfGroupErr(i, "max_layers 至少为 1")
		}
		if g.BudgetCap < 0 {
			return sprintfGroupErr(i, "budget_cap 不能为负数")
		}
	}
	return ""
}

func sprintfGroupErr(i int, msg string) string {
	return fmt.Sprintf("第 %d 组 %s", i+1, msg)
}

func lmBotToJSON(rec *store.LayeredMartinBotRecord, groups []*store.LayeredMartinGroupRecord, running bool) gin.H {
	if groups == nil {
		groups = []*store.LayeredMartinGroupRecord{}
	}
	return gin.H{
		"id":                  rec.ID,
		"name":                rec.Name,
		"symbol":              rec.Symbol,
		"exchange":            rec.Exchange,
		"price_deviation_pct": rec.PriceDeviationPct,
		"take_profit_pct":     rec.TakeProfitPct,
		"stop_loss_pct":       rec.StopLossPct,
		"trailing_enabled":    rec.TrailingEnabled,
		"status":              rec.Status,
		"realized_pnl":        rec.RealizedPnL,
		"total_trades":        rec.TotalTrades,
		"groups":              groups,
		"created_at":          rec.CreatedAt,
		"updated_at":          rec.UpdatedAt,
		"started_at":          rec.StartedAt,
		"stopped_at":          rec.StoppedAt,
		"is_running":          running,
	}
}

func lmGroupsFromParams(botID string, params []lmGroupParams) []*store.LayeredMartinGroupRecord {
	groups := make([]*store.LayeredMartinGroupRecord, 0, len(params))
	for i, g := range params {
		groups = append(groups, &store.LayeredMartinGroupRecord{
			BotID:       botID,
			GroupIndex:  i,
			QuoteAmount: g.QuoteAmount,
			Multiplier:  g.Multiplier,
			MaxLayers:   g.MaxLayers,
			BudgetCap:   g.BudgetCap,
			Status:      "idle",
		})
	}
	return groups
}

// ── Handlers ──

// LayeredMartinBotList: GET /api/layered-martin-bots/ → 当前用户的机器人列表。
// admin/未注入用户（单用户模式）看全部；普通用户仅看本人。
func LayeredMartinBotList(c *gin.Context) {
	filter := map[string]any{}
	if uid, restricted := lmBotRestricted(c); restricted {
		filter["user_id"] = uid
	}
	recs, err := lmBotRepo.List(filter, 0)
	if err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	bots := make([]gin.H, 0, len(recs))
	for _, rec := range recs {
		groups, err := lmBotRepo.GetGroups(rec.ID)
		if err != nil {
			lmBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
		bots = append(bots, lmBotToJSON(rec, groups, lmBotIsRunning(rec.ID)))
	}
	c.JSON(http.StatusOK, gin.H{"bots": bots})
}

// LayeredMartinBotCreate: POST /api/layered-martin-bots/ → 校验参数、
// 主表 status=stopped 落库 + 全量写入分组。
func LayeredMartinBotCreate(c *gin.Context) {
	var body lmBotParams
	if err := c.ShouldBindJSON(&body); err != nil {
		lmBotError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		lmBotError(c, http.StatusBadRequest, msg)
		return
	}
	exchange := strings.TrimSpace(body.Exchange)
	if exchange == "" {
		exchange = "paper"
	}
	uid, _ := ctxUserID(c)
	rec := &store.LayeredMartinBotRecord{
		UserID:            int64(uid),
		Name:              body.Name,
		Symbol:            gridNormalizeSymbol(body.Symbol),
		Exchange:          exchange,
		PriceDeviationPct: body.PriceDeviationPct,
		TakeProfitPct:     body.TakeProfitPct,
		StopLossPct:       body.StopLossPct,
		TrailingEnabled:   body.TrailingEnabled,
		Status:            "stopped",
	}
	if err := lmBotRepo.Create(rec); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	groups := lmGroupsFromParams(rec.ID, body.Groups)
	if err := lmBotRepo.ReplaceGroups(rec.ID, groups); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, lmBotToJSON(rec, groups, false))
}

// LayeredMartinBotGet: GET /api/layered-martin-bots/:id → 记录 + 分组状态
// + is_running + 最近 200 笔 orders。
func LayeredMartinBotGet(c *gin.Context) {
	rec, ok := lmBotMustGet(c)
	if !ok {
		return
	}
	groups, err := lmBotRepo.GetGroups(rec.ID)
	if err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	orders, err := lmBotRepo.GetOrders(rec.ID, 200)
	if err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if orders == nil {
		orders = []*store.LayeredMartinOrderRecord{}
	}
	resp := lmBotToJSON(rec, groups, lmBotIsRunning(rec.ID))
	resp["orders"] = orders
	c.JSON(http.StatusOK, resp)
}

// LayeredMartinBotUpdate: PUT /api/layered-martin-bots/:id → 仅 stopped/finished
// 可改参数（含全量替换分组），运行中 409。
func LayeredMartinBotUpdate(c *gin.Context) {
	rec, ok := lmBotMustGet(c)
	if !ok {
		return
	}
	if rec.Status == "running" || lmBotIsRunning(rec.ID) {
		lmBotError(c, http.StatusConflict, "运行中的机器人不能修改，请先停止")
		return
	}
	var body lmBotParams
	if err := c.ShouldBindJSON(&body); err != nil {
		lmBotError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		lmBotError(c, http.StatusBadRequest, msg)
		return
	}
	if body.Name != "" {
		rec.Name = body.Name
	}
	rec.Symbol = gridNormalizeSymbol(body.Symbol)
	if strings.TrimSpace(body.Exchange) != "" {
		rec.Exchange = strings.TrimSpace(body.Exchange)
	}
	rec.PriceDeviationPct = body.PriceDeviationPct
	rec.TakeProfitPct = body.TakeProfitPct
	rec.StopLossPct = body.StopLossPct
	rec.TrailingEnabled = body.TrailingEnabled
	if err := lmBotRepo.Update(rec); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	groups := lmGroupsFromParams(rec.ID, body.Groups)
	if err := lmBotRepo.ReplaceGroups(rec.ID, groups); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, lmBotToJSON(rec, groups, false))
}

// LayeredMartinBotDelete: DELETE /api/layered-martin-bots/:id → 运行中先停再删。
func LayeredMartinBotDelete(c *gin.Context) {
	rec, ok := lmBotMustGet(c)
	if !ok {
		return
	}
	if lmBotIsRunning(rec.ID) {
		if err := LayeredMartinSvc.StopBot(rec.ID, "stopped"); err != nil {
			lmBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if uid, restricted := lmBotRestricted(c); restricted {
		if err := lmBotRepo.DeleteForUser(rec.ID, uid); err != nil {
			lmBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := lmBotRepo.Delete(rec.ID); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": rec.ID})
}

// LayeredMartinBotStart: POST /api/layered-martin-bots/:id/start → 仅 stopped
// 可启动；实时价无效 503；实盘交易所先过 canPlaceLiveOrder 安全闸（403）。
func LayeredMartinBotStart(c *gin.Context) {
	rec, ok := lmBotMustGet(c)
	if !ok {
		return
	}
	if rec.Status != "stopped" || lmBotIsRunning(rec.ID) {
		lmBotError(c, http.StatusConflict, "机器人不是 stopped 状态，无法启动")
		return
	}
	if LayeredMartinSvc == nil {
		lmBotError(c, http.StatusInternalServerError, "layered martin runner 未初始化")
		return
	}
	if err := canPlaceLiveOrder(rec.Exchange, true); err != nil {
		lmBotError(c, http.StatusForbidden, err.Error())
		return
	}
	price := lmPriceSource(rec.Symbol)
	if price <= 0 {
		lmBotError(c, http.StatusServiceUnavailable, "行情未就绪")
		return
	}
	groups, err := lmBotRepo.GetGroups(rec.ID)
	if err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if len(groups) == 0 {
		lmBotError(c, http.StatusBadRequest, "机器人没有分组，无法启动")
		return
	}
	if err := LayeredMartinSvc.StartBot(rec, groups, price); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": true, "id": rec.ID, "price": price})
}

// LayeredMartinBotStop: POST /api/layered-martin-bots/:id/stop → 运行中才停。
func LayeredMartinBotStop(c *gin.Context) {
	rec, ok := lmBotMustGet(c)
	if !ok {
		return
	}
	if !lmBotIsRunning(rec.ID) {
		lmBotError(c, http.StatusConflict, "机器人未在运行")
		return
	}
	if err := LayeredMartinSvc.StopBot(rec.ID, "stopped"); err != nil {
		lmBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"stopped": true, "id": rec.ID})
}
