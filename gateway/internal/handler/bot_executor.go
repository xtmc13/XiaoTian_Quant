// 机器人运行时共用的 OMS 下单执行器：DCA / 分层马丁 Runner 通过 Executor
// 窄接口调用本文件的生产实现，把现货买单/卖单打进现有 OMS 管线
// （RiskCheck → LockBalance → SubmitToExchange），paper 即时成交回报、
// live 走交易所适配器。实盘单子进单前过 canPlaceLiveOrder 安全闸
// （live_enabled 总闸/运行时锁定），confirmed=true——机器人是用户显式
// 配置+启动的自动化通道，不存在逐单人工确认环节，但不能绕过实盘总闸。
package handler

import (
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
)

// OMSBotExecutor 是 dca.Runner / lmartin.Runner 的 Executor 生产实现。
// kind 标记机器人类型（dca/lmartin/grid），写进订单 client_oid 前缀
// （"kind:botID"），供 A8.2 成交恢复把补录成交路由回对应引擎回填。
type OMSBotExecutor struct {
	kind string
}

// botClientOID 生成 "kind:botID" 形式的 client_oid。
func (e *OMSBotExecutor) botClientOID(botID string) string {
	if botID == "" || e.kind == "" {
		return ""
	}
	return e.kind + ":" + botID
}

// NewOMSBotExecutor 创建基于全局 OMS 的机器人下单执行器。
func NewOMSBotExecutor() *OMSBotExecutor { return &OMSBotExecutor{} }

// NewKindOMSBotExecutor 创建带类型标记的执行器（main 按 runner 类型创建）。
func NewKindOMSBotExecutor(kind string) *OMSBotExecutor { return &OMSBotExecutor{kind: kind} }

// BuySpot 按约 quoteAmount（计价币）买入现货：数量按参考价换算，
// 市价单进 OMS；返回确认成交量（0=未成交，调用方不得推进状态）。
func (e *OMSBotExecutor) BuySpot(botID, symbol, exchange string, userID int64, quoteAmount, refPrice float64) (float64, float64, error) {
	if err := canPlaceLiveOrder(exchange, true); err != nil {
		return 0, 0, err
	}
	qty := quoteAmount
	if refPrice > 0 {
		qty = quoteAmount / refPrice
	}
	req := &order.Request{
		Symbol:    symbol,
		Side:      model.SideBuy,
		OrderType: model.TypeMarket,
		Price:     refPrice,
		Quantity:  qty,
		Exchange:  exchange,
		UserID:    uint64(userID),
		ClientOID: e.botClientOID(botID),
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		return 0, 0, err
	}
	if ord.Status != model.StatusFilled {
		return 0, 0, nil
	}
	filled := ord.Filled
	avg := ord.AvgFillPrice
	if avg <= 0 {
		avg = refPrice
	}
	if filled <= 0 {
		filled = ord.Quantity
	}
	return filled, avg, nil
}

// SellSpot 卖出 baseQty 个现货（市价单），返回确认成交量与均价。
func (e *OMSBotExecutor) SellSpot(botID, symbol, exchange string, userID int64, baseQty, refPrice float64) (float64, float64, error) {
	if err := canPlaceLiveOrder(exchange, true); err != nil {
		return 0, 0, err
	}
	req := &order.Request{
		Symbol:    symbol,
		Side:      model.SideSell,
		OrderType: model.TypeMarket,
		Price:     refPrice,
		Quantity:  baseQty,
		Exchange:  exchange,
		UserID:    uint64(userID),
		ClientOID: e.botClientOID(botID),
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		return 0, 0, err
	}
	if ord.Status != model.StatusFilled {
		return 0, 0, nil
	}
	filled := ord.Filled
	avg := ord.AvgFillPrice
	if avg <= 0 {
		avg = refPrice
	}
	if filled <= 0 {
		filled = ord.Quantity
	}
	return filled, avg, nil
}

// BuySpotLimit 是 pystrat v1.1 的限价买入：按 price 挂现货 LIMIT 单，
// 数量按 quoteAmount/price 换算（与 BuySpot 同一口径，只是限价而非市价）。
// 返回 OMS 订单 ID——限价单可能未成交，调用方不得按成交推进状态；未成交
// 委托对前端经现有订单查询/WS 可见，成交回报经订单事件（client_oid
// "pystrat:<id>"）回推策略 on_order。live 进单前过 canPlaceLiveOrder 安全闸。
func (e *OMSBotExecutor) BuySpotLimit(botID, symbol, exchange string, userID int64, quoteAmount, price float64) (string, error) {
	if err := canPlaceLiveOrder(exchange, true); err != nil {
		return "", err
	}
	if price <= 0 {
		return "", fmt.Errorf("limit buy requires a positive price")
	}
	qty := quoteAmount / price
	req := &order.Request{
		Symbol:    symbol,
		Side:      model.SideBuy,
		OrderType: model.TypeLimit,
		Price:     price,
		Quantity:  qty,
		Exchange:  exchange,
		UserID:    uint64(userID),
		ClientOID: e.botClientOID(botID),
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		return "", err
	}
	return ord.ID, nil
}

// SellSpotLimit 是 pystrat v1.1 的限价卖出：挂现货 LIMIT 单，返回订单 ID。
// 语义同 BuySpotLimit（未成交不等于错误，成交回报走订单事件）。
func (e *OMSBotExecutor) SellSpotLimit(botID, symbol, exchange string, userID int64, baseQty, price float64) (string, error) {
	if err := canPlaceLiveOrder(exchange, true); err != nil {
		return "", err
	}
	if price <= 0 {
		return "", fmt.Errorf("limit sell requires a positive price")
	}
	req := &order.Request{
		Symbol:    symbol,
		Side:      model.SideSell,
		OrderType: model.TypeLimit,
		Price:     price,
		Quantity:  baseQty,
		Exchange:  exchange,
		UserID:    uint64(userID),
		ClientOID: e.botClientOID(botID),
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		return "", err
	}
	return ord.ID, nil
}

// PlaceContract 执行合约腿下单（grid.Runner 的 ContractExecutor 生产实现）：
// 合约市价单走现有 OMS 合约链路（market_type=swap + position_side +
// leverage/margin_mode）。paper 即时成交回报；live 进单前过
// canPlaceLiveOrder 安全闸（confirmed=true——机器人是用户显式配置+启动的
// 自动化通道，不逐单人工确认，但绝不绕过实盘总闸）。返回确认成交量。
func (e *OMSBotExecutor) PlaceContract(botID, symbol, exchange string, userID int64, side string, qty, price, leverage float64, marginMode, positionSide string) (float64, float64, error) {
	if err := canPlaceLiveOrder(exchange, true); err != nil {
		return 0, 0, err
	}
	orderSide := model.SideBuy
	if side == string(model.SideSell) {
		orderSide = model.SideSell
	}
	mm := model.MarginMode(marginMode)
	if mm != model.MarginIsolated {
		mm = model.MarginCross
	}
	ps := model.PositionSide(positionSide)
	if ps != model.PositionShort {
		ps = model.PositionLong
	}
	req := &order.Request{
		Symbol:       symbol,
		Side:         orderSide,
		OrderType:    model.TypeMarket,
		Price:        price,
		Quantity:     qty,
		Exchange:     exchange,
		UserID:       uint64(userID),
		MarketType:   model.MarketSwap,
		PositionSide: ps,
		Leverage:     leverage,
		MarginMode:   mm,
		ClientOID:    e.botClientOID(botID),
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		return 0, 0, err
	}
	if ord.Status != model.StatusFilled {
		return 0, 0, nil
	}
	filled := ord.Filled
	avg := ord.AvgFillPrice
	if avg <= 0 {
		avg = price
	}
	if filled <= 0 {
		filled = ord.Quantity
	}
	return filled, avg, nil
}
