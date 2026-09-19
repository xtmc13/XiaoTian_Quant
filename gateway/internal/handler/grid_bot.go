package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/grid"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Grid bot HTTP API ──
//
// 网格机器人 REST 层：CRUD + start/stop，落库走 store.GridRepo，
// 运行控制走注入的 GridService（生产为 grid.Runner，测试为 fake）。
// 实时价来自 BinanceWS 内存缓存（gridPriceSource，可替换）。
// 成功响应直接返回裸数据对象，由 UnifiedResponseWrapper 统一包
// {success:true,data:...} 信封；错误统一返回 {"success":false,"message":...}。

// GridService 是 handler 对网格 Runner 的窄接口，便于测试注入 fake。
type GridService interface {
	StartBot(rec *store.GridBotRecord, currentPrice float64) error
	StopBot(botID string, status string) error
	IsRunning(botID string) bool
}

// GridSvc 是当前进程内驱动网格机器人的服务，main 启动时注入。
var GridSvc GridService

// SetGridService 注入网格机器人运行时（生产接 grid.Runner）。
func SetGridService(s GridService) { GridSvc = s }

// gridPriceSource 取 symbol 实时价；生产为 BinanceWS 内存价（零网络），
// 测试可用 SetGridPriceSource 换成假源。返回 <=0 表示行情未就绪。
var gridPriceSource = func(symbol string) float64 {
	if a := app.Get(); a != nil && a.BinanceWS != nil {
		return a.BinanceWS.GetPrice(symbol)
	}
	return 0
}

// SetGridPriceSource 覆盖实时价来源（测试用）。
func SetGridPriceSource(fn func(symbol string) float64) { gridPriceSource = fn }

// gridBotRepo 是无状态 typed CRUD（内部仅一把互斥锁），进程级复用即可。
var gridBotRepo = store.NewGridRepo()

// ── Helpers ──

func gridBotUserID(c *gin.Context) int {
	uid, exists := c.Get(middleware.UserIDKey)
	if !exists {
		return 0
	}
	if v, ok := uid.(int); ok {
		return v
	}
	return 0
}

func gridBotError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

// gridBotMustGet 按 :id 取记录；不存在返回 404，出错 500；
// 存在但属他人（且非 admin、已注入用户）返回 403（资源级越权防护）。
// 未注入用户（单用户模式/内部调用）保持原行为放行。
func gridBotMustGet(c *gin.Context) (*store.GridBotRecord, bool) {
	rec, err := gridBotRepo.GetByID(c.Param("id"))
	if err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if rec == nil {
		gridBotError(c, http.StatusNotFound, "grid bot not found")
		return nil, false
	}
	if !requireOwner(c, rec.UserID) {
		return nil, false
	}
	return rec, true
}

// gridBotRestricted 当前请求是否需按属主过滤（登录非 admin）。
func gridBotRestricted(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

func gridBotIsRunning(id string) bool {
	return GridSvc != nil && GridSvc.IsRunning(id)
}

// gridNormalizeSymbol 把 "BTC/USDT" 规范成 "BTCUSDT"（去斜杠、去空白、转大写）。
func gridNormalizeSymbol(symbol string) string {
	s := strings.ReplaceAll(strings.TrimSpace(symbol), "/", "")
	return strings.ToUpper(s)
}

// gridOpenOrders 从 state_json 解析当前挂单数（ExportState 的 orders 键数；
// A1.3 双腿格式跨腿累计，兼容裸单腿存量状态）。
func gridOpenOrders(stateJSON string) int {
	var state map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return 0
	}
	return grid.CountOpenOrders(state)
}

// gridBotLegViews 从 record + 实时价解析双腿格子状态与独立盈亏
// （详情接口展示用；不在运行的机器人也用持久化状态离线展示）。
func gridBotLegViews(rec *store.GridBotRecord, price float64) []grid.LegView {
	stateJSON := strings.TrimSpace(rec.StateJSON)
	if stateJSON == "" || stateJSON == "{}" {
		return nil
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil
	}
	cfg := grid.Config{
		Symbol:       rec.Symbol,
		Lower:        rec.LowerPrice,
		Upper:        rec.UpperPrice,
		GridCount:    rec.GridCount,
		Investment:   rec.Investment,
		FeeRate:      rec.FeeRate,
		CurrentPrice: price,
		Mode:         grid.ParseMode(rec.Mode),
	}
	views, err := grid.LegViewsFromState(grid.ParseMode(rec.Mode), state, cfg, price)
	if err != nil {
		return nil
	}
	return views
}

// ── Request binding / validation ──

type gridBotParams struct {
	Name       string  `json:"name"`
	Symbol     string  `json:"symbol"`
	LowerPrice float64 `json:"lower_price"`
	UpperPrice float64 `json:"upper_price"`
	GridCount  int     `json:"grid_count"`
	Investment float64 `json:"investment"`
	FeeRate    float64 `json:"fee_rate"`
	Mode       string  `json:"mode"` // long / short / neutral（A1.3）
	Exchange   string  `json:"exchange"`
	Leverage   float64 `json:"leverage"`
	MarginMode string  `json:"margin_mode"`
}

// validate 校验创建/修改共用参数，返回错误消息（空串表示通过）。
func (p *gridBotParams) validate() string {
	if strings.TrimSpace(p.Symbol) == "" {
		return "symbol 不能为空"
	}
	if p.LowerPrice <= 0 || p.UpperPrice <= 0 || p.LowerPrice >= p.UpperPrice {
		return "lower_price 必须小于 upper_price 且均为正数"
	}
	if p.GridCount < 2 || p.GridCount > 200 {
		return "grid_count 必须在 2-200 之间"
	}
	if p.Investment <= 0 {
		return "investment 必须大于 0"
	}
	if p.FeeRate < 0 {
		return "fee_rate 不能为负数"
	}
	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	switch mode {
	case "", "long":
	case "short", "neutral":
		if p.Leverage < 1 || p.Leverage > 125 {
			return "合约网格 leverage 必须在 1-125 之间"
		}
		switch strings.ToLower(strings.TrimSpace(p.MarginMode)) {
		case "", "cross", "isolated":
		default:
			return "margin_mode 必须为 cross 或 isolated"
		}
	default:
		return "mode 必须为 long / short / neutral"
	}
	return ""
}

// gridContractMode 规范化模式字段：空→long；其余经校验后为 long/short/neutral。
func gridContractMode(p gridBotParams) string {
	m := strings.ToLower(strings.TrimSpace(p.Mode))
	if m == "" {
		return string(grid.ModeLong)
	}
	return string(grid.ParseMode(m))
}

// ── Handlers ──

// GridBotList: GET /api/grid/bots → 当前用户的机器人列表（附 is_running）。
// admin/未注入用户（单用户模式）看全部；普通用户仅看本人。
func GridBotList(c *gin.Context) {
	filter := map[string]any{}
	if uid, restricted := gridBotRestricted(c); restricted {
		filter["user_id"] = uid
	}
	recs, err := gridBotRepo.List(filter, 0)
	if err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	bots := make([]gin.H, 0, len(recs))
	for _, rec := range recs {
		bots = append(bots, gin.H{
			"id":             rec.ID,
			"name":           rec.Name,
			"symbol":         rec.Symbol,
			"lower_price":    rec.LowerPrice,
			"upper_price":    rec.UpperPrice,
			"grid_count":     rec.GridCount,
			"investment":     rec.Investment,
			"fee_rate":       rec.FeeRate,
			"status":         rec.Status,
			"exchange":       rec.Exchange,
			"mode":           rec.Mode,
			"leverage":       rec.Leverage,
			"margin_mode":    rec.MarginMode,
			"realized_pnl":   rec.RealizedPnL,
			"total_trades":   rec.TotalTrades,
			"base_qty":       rec.BaseQty,
			"quote_balance":  rec.QuoteBalance,
			"initial_equity": rec.InitialEquity,
			"created_at":     rec.CreatedAt,
			"updated_at":     rec.UpdatedAt,
			"started_at":     rec.StartedAt,
			"stopped_at":     rec.StoppedAt,
			"is_running":     gridBotIsRunning(rec.ID),
		})
	}
	c.JSON(http.StatusOK, gin.H{"bots": bots})
}

// GridBotCreate: POST /api/grid/bots → 校验参数、status=stopped 落库。
func GridBotCreate(c *gin.Context) {
	var body gridBotParams
	if err := c.ShouldBindJSON(&body); err != nil {
		gridBotError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		gridBotError(c, http.StatusBadRequest, msg)
		return
	}
	feeRate := body.FeeRate
	if feeRate == 0 {
		feeRate = 0.001
	}
	rec := &store.GridBotRecord{
		UserID:     int64(gridBotUserID(c)),
		Name:       body.Name,
		Symbol:     gridNormalizeSymbol(body.Symbol),
		LowerPrice: body.LowerPrice,
		UpperPrice: body.UpperPrice,
		GridCount:  body.GridCount,
		Investment: body.Investment,
		FeeRate:    feeRate,
		Status:     "stopped",
	}
	rec.Mode = gridContractMode(body)
	if strings.TrimSpace(body.Exchange) != "" {
		rec.Exchange = strings.TrimSpace(body.Exchange)
	}
	rec.Leverage = body.Leverage
	if rec.Leverage == 0 {
		rec.Leverage = 1
	}
	rec.MarginMode = strings.ToLower(strings.TrimSpace(body.MarginMode))
	if rec.MarginMode == "" {
		rec.MarginMode = "cross"
	}
	if err := gridBotRepo.Create(rec); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":             rec.ID,
		"name":           rec.Name,
		"symbol":         rec.Symbol,
		"lower_price":    rec.LowerPrice,
		"upper_price":    rec.UpperPrice,
		"grid_count":     rec.GridCount,
		"investment":     rec.Investment,
		"fee_rate":       rec.FeeRate,
		"status":         rec.Status,
		"exchange":       rec.Exchange,
		"mode":           rec.Mode,
		"leverage":       rec.Leverage,
		"margin_mode":    rec.MarginMode,
		"is_running":     false,
		"initial_equity": rec.InitialEquity,
		"created_at":     rec.CreatedAt,
		"updated_at":     rec.UpdatedAt,
	})
}

// GridBotGet: GET /api/grid/bots/:id → 记录 + is_running + 运行状态
// （open_orders）+ 最近 100 笔 trades + 最近 200 条 snapshots。
func GridBotGet(c *gin.Context) {
	rec, ok := gridBotMustGet(c)
	if !ok {
		return
	}
	trades, err := gridBotRepo.GetTrades(rec.ID, 100)
	if err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	snapshots, err := gridBotRepo.GetSnapshots(rec.ID, 200, 0)
	if err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if trades == nil {
		trades = []*store.GridTradeRecord{}
	}
	if snapshots == nil {
		snapshots = []*store.GridSnapshotRecord{}
	}
	running := gridBotIsRunning(rec.ID)
	openOrders := 0
	if running {
		openOrders = gridOpenOrders(rec.StateJSON)
	}
	price := gridPriceSource(rec.Symbol)
	legs := gridBotLegViews(rec, price)
	if legs == nil {
		legs = []grid.LegView{}
	}
	netPosition := rec.BaseQty
	if running && legs != nil {
		netPosition = 0
		for _, lv := range legs {
			netPosition += lv.BaseQty
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"id":             rec.ID,
		"name":           rec.Name,
		"symbol":         rec.Symbol,
		"lower_price":    rec.LowerPrice,
		"upper_price":    rec.UpperPrice,
		"grid_count":     rec.GridCount,
		"investment":     rec.Investment,
		"fee_rate":       rec.FeeRate,
		"status":         rec.Status,
		"exchange":       rec.Exchange,
		"mode":           rec.Mode,
		"leverage":       rec.Leverage,
		"margin_mode":    rec.MarginMode,
		"realized_pnl":   rec.RealizedPnL,
		"total_trades":   rec.TotalTrades,
		"base_qty":       rec.BaseQty,
		"quote_balance":  rec.QuoteBalance,
		"state_json":     rec.StateJSON,
		"initial_equity": rec.InitialEquity,
		"created_at":     rec.CreatedAt,
		"updated_at":     rec.UpdatedAt,
		"started_at":     rec.StartedAt,
		"stopped_at":     rec.StoppedAt,
		"is_running":     running,
		"open_orders":    openOrders,
		"legs":           legs,
		"net_position":   netPosition,
		"price":          price,
		"trades":         trades,
		"snapshots":      snapshots,
	})
}

// GridBotUpdate: PUT /api/grid/bots/:id → 仅 stopped 可改参数，运行中 409。
func GridBotUpdate(c *gin.Context) {
	rec, ok := gridBotMustGet(c)
	if !ok {
		return
	}
	if rec.Status != "stopped" || gridBotIsRunning(rec.ID) {
		gridBotError(c, http.StatusConflict, "运行中的机器人不能修改，请先停止")
		return
	}
	var body gridBotParams
	if err := c.ShouldBindJSON(&body); err != nil {
		gridBotError(c, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		gridBotError(c, http.StatusBadRequest, msg)
		return
	}
	if body.Name != "" {
		rec.Name = body.Name
	}
	rec.Symbol = gridNormalizeSymbol(body.Symbol)
	rec.LowerPrice = body.LowerPrice
	rec.UpperPrice = body.UpperPrice
	rec.GridCount = body.GridCount
	rec.Investment = body.Investment
	if body.FeeRate > 0 {
		rec.FeeRate = body.FeeRate
	}
	rec.Mode = gridContractMode(body)
	if strings.TrimSpace(body.Exchange) != "" {
		rec.Exchange = strings.TrimSpace(body.Exchange)
	}
	rec.Leverage = body.Leverage
	if rec.Leverage == 0 {
		rec.Leverage = 1
	}
	rec.MarginMode = strings.ToLower(strings.TrimSpace(body.MarginMode))
	if rec.MarginMode == "" {
		rec.MarginMode = "cross"
	}
	if err := gridBotRepo.Update(rec); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":          rec.ID,
		"name":        rec.Name,
		"symbol":      rec.Symbol,
		"lower_price": rec.LowerPrice,
		"upper_price": rec.UpperPrice,
		"grid_count":  rec.GridCount,
		"investment":  rec.Investment,
		"fee_rate":    rec.FeeRate,
		"status":      rec.Status,
		"mode":        rec.Mode,
		"leverage":    rec.Leverage,
		"margin_mode": rec.MarginMode,
		"is_running":  false,
		"updated_at":  rec.UpdatedAt,
	})
}

// GridBotDelete: DELETE /api/grid/bots/:id → 运行中先停再删。
func GridBotDelete(c *gin.Context) {
	rec, ok := gridBotMustGet(c)
	if !ok {
		return
	}
	if gridBotIsRunning(rec.ID) {
		if err := GridSvc.StopBot(rec.ID, "stopped"); err != nil {
			gridBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if uid, restricted := gridBotRestricted(c); restricted {
		// H1: 删除也带属主条件（mustGet 已校验过，双保险）
		if err := gridBotRepo.DeleteForUser(rec.ID, uid); err != nil {
			gridBotError(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := gridBotRepo.Delete(rec.ID); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": rec.ID})
}

// GridBotStart: POST /api/grid/bots/:id/start → 仅 stopped 可启动；
// 实时价无效 503，超出网格区间 400；合约模式（short/neutral）先过
// canPlaceLiveOrder 实盘安全闸（paper 直通，live 未开闸 403）；
// initial_equity=investment 后交 Runner。
func GridBotStart(c *gin.Context) {
	rec, ok := gridBotMustGet(c)
	if !ok {
		return
	}
	if rec.Status != "stopped" || gridBotIsRunning(rec.ID) {
		gridBotError(c, http.StatusConflict, "机器人不是 stopped 状态，无法启动")
		return
	}
	if GridSvc == nil {
		gridBotError(c, http.StatusInternalServerError, "grid runner 未初始化")
		return
	}
	mode := grid.ParseMode(rec.Mode)
	if mode != grid.ModeLong {
		if err := canPlaceLiveOrder(rec.Exchange, true); err != nil {
			gridBotError(c, http.StatusForbidden, err.Error())
			return
		}
	}
	price := gridPriceSource(rec.Symbol)
	if price <= 0 {
		gridBotError(c, http.StatusServiceUnavailable, "行情未就绪")
		return
	}
	if price < rec.LowerPrice || price > rec.UpperPrice {
		gridBotError(c, http.StatusBadRequest,
			fmt.Sprintf("当前价 %v 超出网格区间 [%v, %v]", price, rec.LowerPrice, rec.UpperPrice))
		return
	}
	rec.InitialEquity = rec.Investment
	if err := gridBotRepo.Update(rec); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := GridSvc.StartBot(rec, price); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": true, "id": rec.ID, "price": price})
}

// GridBotStop: POST /api/grid/bots/:id/stop → 运行中才停。
func GridBotStop(c *gin.Context) {
	rec, ok := gridBotMustGet(c)
	if !ok {
		return
	}
	if !gridBotIsRunning(rec.ID) {
		gridBotError(c, http.StatusConflict, "机器人未在运行")
		return
	}
	if err := GridSvc.StopBot(rec.ID, "stopped"); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"stopped": true, "id": rec.ID})
}
