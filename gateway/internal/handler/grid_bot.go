package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
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

// gridBotMustGet 按 :id 取记录；不存在返回 404，出错 500。
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
	return rec, true
}

func gridBotIsRunning(id string) bool {
	return GridSvc != nil && GridSvc.IsRunning(id)
}

// gridNormalizeSymbol 把 "BTC/USDT" 规范成 "BTCUSDT"（去斜杠、去空白、转大写）。
func gridNormalizeSymbol(symbol string) string {
	s := strings.ReplaceAll(strings.TrimSpace(symbol), "/", "")
	return strings.ToUpper(s)
}

// gridOpenOrders 从 state_json 解析当前挂单数（ExportState 的 orders 键数）。
func gridOpenOrders(stateJSON string) int {
	var state map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return 0
	}
	orders, ok := state["orders"].(map[string]any)
	if !ok {
		return 0
	}
	return len(orders)
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
	return ""
}

// ── Handlers ──

// GridBotList: GET /api/grid/bots → 当前用户的机器人列表（附 is_running）。
func GridBotList(c *gin.Context) {
	recs, err := gridBotRepo.List(map[string]any{"user_id": gridBotUserID(c)}, 0)
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
	if err := gridBotRepo.Delete(rec.ID); err != nil {
		gridBotError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": rec.ID})
}

// GridBotStart: POST /api/grid/bots/:id/start → 仅 stopped 可启动；
// 实时价无效 503，超出网格区间 400；initial_equity=investment 后交 Runner。
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
