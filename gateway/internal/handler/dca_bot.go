package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── DCA 定投机器人 HTTP API（A1.2）──
//
// REST 层：CRUD + start/stop，落库走 store.DCARepo，运行控制走注入的
// DCAService（生产为 dca.Runner，测试为 fake）。实时价来自 BinanceWS
// 内存缓存（dcaPriceSource，可替换）。所有权语义与 grid_bot.go 一致：
// 创建写 user_id，单个资源操作 requireOwner，列表按 user_id 过滤
// （admin/未注入用户看全部）。

// DCAService 是 handler 对 DCA Runner 的窄接口，便于测试注入 fake。
type DCAService interface {
	StartBot(rec *store.DCABotRecord, currentPrice float64) error
	StopBot(botID string, status string) error
	IsRunning(botID string) bool
}

// DCASvc 是当前进程内驱动 DCA 机器人的服务，main 启动时注入。
var DCASvc DCAService

// SetDCAService 注入 DCA 机器人运行时（生产接 dca.Runner）。
func SetDCAService(s DCAService) { DCASvc = s }

// dcaPriceSource 取 symbol 实时价；生产为 BinanceWS 内存价（零网络），
// 测试可用 SetDCAPriceSource 换成假源。返回 <=0 表示行情未就绪。
var dcaPriceSource = func(symbol string) float64 {
	if a := app.Get(); a != nil && a.BinanceWS != nil {
		return a.BinanceWS.GetPrice(symbol)
	}
	return 0
}

// SetDCAPriceSource 覆盖实时价来源（测试用）。
func SetDCAPriceSource(fn func(symbol string) float64) { dcaPriceSource = fn }

// dcaBotRepo 是无状态 typed CRUD（内部仅一把互斥锁），进程级复用即可。
var dcaBotRepo = store.NewDCARepo()

// ── Helpers ──

func dcaBotError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

// dcaBotMustGet 按 :id 取记录；不存在返回 404，出错 500；
// 存在但属他人（且非 admin、已注入用户）返回 403（资源级越权防护）。
func dcaBotMustGet(c *gin.Context) (*store.DCABotRecord, bool) {
	rec, err := dcaBotRepo.GetByID(c.Param("id"))
	if err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if rec == nil {
		dcaBotError(c, http.StatusNotFound, "dca bot not found")
		return nil, false
	}
	if !requireOwner(c, rec.UserID) {
		return nil, false
	}
	return rec, true
}

// dcaBotRestricted 当前请求是否需按属主过滤（登录非 admin）。
func dcaBotRestricted(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

func dcaBotIsRunning(id string) bool {
	return DCASvc != nil && DCASvc.IsRunning(id)
}

// ── Request binding / validation ──

type dcaBotParams struct {
	Name            string  `json:"name"`
	Symbol          string  `json:"symbol"`
	Exchange        string  `json:"exchange"`
	QuoteAmount     float64 `json:"quote_amount"`
	IntervalMinutes int     `json:"interval_minutes"`
	MaxOrders       int     `json:"max_orders"`
	PeriodBudget    float64 `json:"period_budget"`
	TakeProfitPct   float64 `json:"take_profit_pct"`
	StopLossPct     float64 `json:"stop_loss_pct"`
	TrailingEnabled bool    `json:"trailing_enabled"`
}

// validate 校验创建/修改共用参数，返回错误消息（空串表示通过）。
func (p *dcaBotParams) validate() string {
	if strings.TrimSpace(p.Symbol) == "" {
		return "symbol 不能为空"
	}
	if p.QuoteAmount <= 0 {
		return "quote_amount 必须大于 0"
	}
	if p.IntervalMinutes < 1 {
		return "interval_minutes 至少为 1"
	}
	if p.MaxOrders < 0 {
		return "max_orders 不能为负数"
	}
	if p.PeriodBudget < 0 {
		return "period_budget 不能为负数"
	}
	if p.TakeProfitPct < 0 || p.StopLossPct < 0 {
		return "take_profit_pct / stop_loss_pct 不能为负数"
	}
	if p.TakeProfitPct >= 1 || p.StopLossPct >= 1 {
		return "take_profit_pct / stop_loss_pct 必须小于 1（如 0.05 表示 5%）"
	}
	return ""
}

func dcaBotToJSON(rec *store.DCABotRecord, running bool) gin.H {
	return gin.H{
		"id":               rec.ID,
		"name":             rec.Name,
		"symbol":           rec.Symbol,
		"exchange":         rec.Exchange,
		"quote_amount":     rec.QuoteAmount,
		"interval_minutes": rec.IntervalMinutes,
		"max_orders":       rec.MaxOrders,
		"period_budget":    rec.PeriodBudget,
		"take_profit_pct":  rec.TakeProfitPct,
		"stop_loss_pct":    rec.StopLossPct,
		"trailing_enabled": rec.TrailingEnabled,
		"status":           rec.Status,
		"filled_orders":    rec.FilledOrders,
		"total_invested":   rec.TotalInvested,
		"base_qty":         rec.BaseQty,
		"avg_price":        rec.AvgPrice,
		"realized_pnl":     rec.RealizedPnL,
		"last_buy_at":      rec.LastBuyAt,
		"highest_price":    rec.HighestPrice,
		"created_at":       rec.CreatedAt,
		"updated_at":       rec.UpdatedAt,
		"started_at":       rec.StartedAt,
		"stopped_at":       rec.StoppedAt,
		"is_running":       running,
	}
}

// ── Handlers ──

// DCABotList: GET /api/dca-bots/ → 当前用户的机器人列表（附 is_running）。
// admin/未注入用户（单用户模式）看全部；普通用户仅看本人。
func DCABotList(c *gin.Context) {
	filter := map[string]any{}
	if uid, restricted := dcaBotRestricted(c); restricted {
		filter["user_id"] = uid
	}
	recs, err := dcaBotRepo.List(filter, 0)
	if err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	bots := make([]gin.H, 0, len(recs))
	for _, rec := range recs {
		bots = append(bots, dcaBotToJSON(rec, dcaBotIsRunning(rec.ID)))
	}
	c.JSON(http.StatusOK, gin.H{"bots": bots})
}

// DCABotCreate: POST /api/dca-bots/ → 校验参数、status=stopped 落库。
func DCABotCreate(c *gin.Context) {
	var body dcaBotParams
	if err := c.ShouldBindJSON(&body); err != nil {
		dcaBotError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		dcaBotError(c, http.StatusBadRequest, msg)
		return
	}
	exchange := strings.TrimSpace(body.Exchange)
	if exchange == "" {
		exchange = "paper"
	}
	uid, _ := ctxUserID(c)
	rec := &store.DCABotRecord{
		UserID:          int64(uid),
		Name:            body.Name,
		Symbol:          gridNormalizeSymbol(body.Symbol),
		Exchange:        exchange,
		QuoteAmount:     body.QuoteAmount,
		IntervalMinutes: body.IntervalMinutes,
		MaxOrders:       body.MaxOrders,
		PeriodBudget:    body.PeriodBudget,
		TakeProfitPct:   body.TakeProfitPct,
		StopLossPct:     body.StopLossPct,
		TrailingEnabled: body.TrailingEnabled,
		Status:          "stopped",
	}
	if err := dcaBotRepo.Create(rec); err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, dcaBotToJSON(rec, false))
}

// DCABotGet: GET /api/dca-bots/:id → 记录 + is_running + 最近 200 笔 orders。
func DCABotGet(c *gin.Context) {
	rec, ok := dcaBotMustGet(c)
	if !ok {
		return
	}
	orders, err := dcaBotRepo.GetOrders(rec.ID, 200)
	if err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if orders == nil {
		orders = []*store.DCAOrderRecord{}
	}
	resp := dcaBotToJSON(rec, dcaBotIsRunning(rec.ID))
	resp["orders"] = orders
	c.JSON(http.StatusOK, resp)
}

// DCABotUpdate: PUT /api/dca-bots/:id → 仅 stopped/finished 可改参数，运行中 409。
func DCABotUpdate(c *gin.Context) {
	rec, ok := dcaBotMustGet(c)
	if !ok {
		return
	}
	if rec.Status == "running" || dcaBotIsRunning(rec.ID) {
		dcaBotError(c, http.StatusConflict, "运行中的机器人不能修改，请先停止")
		return
	}
	var body dcaBotParams
	if err := c.ShouldBindJSON(&body); err != nil {
		dcaBotError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		dcaBotError(c, http.StatusBadRequest, msg)
		return
	}
	if body.Name != "" {
		rec.Name = body.Name
	}
	rec.Symbol = gridNormalizeSymbol(body.Symbol)
	if strings.TrimSpace(body.Exchange) != "" {
		rec.Exchange = strings.TrimSpace(body.Exchange)
	}
	rec.QuoteAmount = body.QuoteAmount
	rec.IntervalMinutes = body.IntervalMinutes
	rec.MaxOrders = body.MaxOrders
	rec.PeriodBudget = body.PeriodBudget
	rec.TakeProfitPct = body.TakeProfitPct
	rec.StopLossPct = body.StopLossPct
	rec.TrailingEnabled = body.TrailingEnabled
	if err := dcaBotRepo.Update(rec); err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, dcaBotToJSON(rec, false))
}

// DCABotDelete: DELETE /api/dca-bots/:id → 运行中先停再删。
func DCABotDelete(c *gin.Context) {
	rec, ok := dcaBotMustGet(c)
	if !ok {
		return
	}
	if dcaBotIsRunning(rec.ID) {
		if err := DCASvc.StopBot(rec.ID, "stopped"); err != nil {
			dcaBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if uid, restricted := dcaBotRestricted(c); restricted {
		// 删除也带属主条件（mustGet 已校验过，双保险）
		if err := dcaBotRepo.DeleteForUser(rec.ID, uid); err != nil {
			dcaBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := dcaBotRepo.Delete(rec.ID); err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": rec.ID})
}

// DCABotStart: POST /api/dca-bots/:id/start → 仅 stopped 可启动；
// 实时价无效 503；实盘交易所先过 canPlaceLiveOrder 安全闸（403）。
func DCABotStart(c *gin.Context) {
	rec, ok := dcaBotMustGet(c)
	if !ok {
		return
	}
	if rec.Status != "stopped" || dcaBotIsRunning(rec.ID) {
		dcaBotError(c, http.StatusConflict, "机器人不是 stopped 状态，无法启动")
		return
	}
	if DCASvc == nil {
		dcaBotError(c, http.StatusInternalServerError, "dca runner 未初始化")
		return
	}
	if err := canPlaceLiveOrder(rec.Exchange, true); err != nil {
		dcaBotError(c, http.StatusForbidden, err.Error())
		return
	}
	price := dcaPriceSource(rec.Symbol)
	if price <= 0 {
		dcaBotError(c, http.StatusServiceUnavailable, "行情未就绪")
		return
	}
	if err := DCASvc.StartBot(rec, price); err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": true, "id": rec.ID, "price": price})
}

// DCABotStop: POST /api/dca-bots/:id/stop → 运行中才停。
func DCABotStop(c *gin.Context) {
	rec, ok := dcaBotMustGet(c)
	if !ok {
		return
	}
	if !dcaBotIsRunning(rec.ID) {
		dcaBotError(c, http.StatusConflict, "机器人未在运行")
		return
	}
	if err := DCASvc.StopBot(rec.ID, "stopped"); err != nil {
		dcaBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"stopped": true, "id": rec.ID})
}
