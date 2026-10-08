package store

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ── Order Repository ──

type OrderRecord struct {
	ID        string  `json:"id"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	OrderType string  `json:"order_type"`
	Price     float64 `json:"price"`
	StopPrice float64 `json:"stop_price"`
	Quantity  float64 `json:"quantity"`
	Filled    float64 `json:"filled"`
	Status    string  `json:"status"`
	Exchange  string  `json:"exchange"`
	UserID    uint64  `json:"user_id"`
	ClientOID string  `json:"client_oid"`
	// ExchangeOrderID 交易所侧订单号（如币安数字 orderId），下单ACK后回填，
	// 成交恢复(reconcile)按它查询交易所最新状态/成交明细。
	ExchangeOrderID string  `json:"exchange_order_id"`
	AvgFillPrice    float64 `json:"avg_fill_price"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`

	// Contract fields
	MarketType    string  `json:"market_type,omitempty"`
	PositionSide  string  `json:"position_side,omitempty"`
	Leverage      float64 `json:"leverage,omitempty"`
	MarginMode    string  `json:"margin_mode,omitempty"`
	TPPrice       float64 `json:"tp_price,omitempty"`
	SLPrice       float64 `json:"sl_price,omitempty"`
	ClosePosition bool    `json:"close_position,omitempty"`
}

type OrderRepo struct {
	mu sync.RWMutex
}

func NewOrderRepo() *OrderRepo { return &OrderRepo{} }

func (r *OrderRepo) Create(o *OrderRecord) error {
	if o.ID == "" {
		o.ID = fmt.Sprintf("ord-%d", time.Now().UnixMilli())
	}
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().UnixMilli()
	}
	if o.UpdatedAt == 0 {
		o.UpdatedAt = o.CreatedAt
	}
	if o.Status == "" {
		o.Status = "NEW"
	}
	if o.Exchange == "" {
		o.Exchange = "BINANCE"
	}
	_, err := db.Exec(
		`INSERT INTO xt_orders (id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.Symbol, o.Side, o.OrderType, o.Price, o.StopPrice, o.Quantity, o.Filled, o.Status, o.Exchange,
		o.UserID, o.ClientOID, o.ExchangeOrderID, o.AvgFillPrice, o.CreatedAt, o.UpdatedAt,
		o.MarketType, o.PositionSide, o.Leverage, o.MarginMode, o.TPPrice, o.SLPrice, o.ClosePosition,
	)
	return err
}

func (r *OrderRepo) GetByID(id string) (*OrderRecord, error) {
	row := db.QueryRow(`SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position FROM xt_orders WHERE id=?`, id)
	var o OrderRecord
	// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
	// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
	var createdF, updatedF float64
	err := row.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
		&o.UserID, &o.ClientOID, &o.ExchangeOrderID, &o.AvgFillPrice, &createdF, &updatedF,
		&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition)
	if err != nil {
		return nil, err
	}
	o.CreatedAt = int64(createdF)
	o.UpdatedAt = int64(updatedF)
	return &o, nil
}

func (r *OrderRepo) List(filter map[string]any, limit int) ([]*OrderRecord, error) {
	query := "SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position FROM xt_orders"
	allowedCols := map[string]bool{
		"id": true, "symbol": true, "side": true, "order_type": true, "status": true,
		"exchange": true, "user_id": true, "client_oid": true, "exchange_order_id": true, "market_type": true,
		"position_side": true, "created_at": true, "updated_at": true,
	}
	args, where := buildFilter(filter, allowedCols)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
		// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.ExchangeOrderID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
		result = append(result, &o)
	}
	return result, nil
}

// ListActiveByClientOIDPrefix 返回 client_oid 带指定前缀且仍在活动状态
// （NEW/PENDING/PARTIALLY_FILLED）的订单，按创建时间升序。
// ltm 重启恢复扫描用：父单前缀 "ltm-"，市价补单前缀 "ltm-mkt:<parentID>"。
func (r *OrderRepo) ListActiveByClientOIDPrefix(prefix string) ([]*OrderRecord, error) {
	rows, err := db.Query(`SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position
		FROM xt_orders WHERE client_oid LIKE ? AND status IN ('NEW','PENDING','PARTIALLY_FILLED') ORDER BY created_at ASC`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
		// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.ExchangeOrderID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
		result = append(result, &o)
	}
	return result, nil
}

func (r *OrderRepo) Update(o *OrderRecord) error {
	o.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE xt_orders SET symbol=?, side=?, order_type=?, price=?, stop_price=?, quantity=?, filled=?, status=?, exchange=?, user_id=?, client_oid=?, exchange_order_id=?, avg_fill_price=?, market_type=?, position_side=?, leverage=?, margin_mode=?, tp_price=?, sl_price=?, close_position=? WHERE id=?`,
		o.Symbol, o.Side, o.OrderType, o.Price, o.StopPrice, o.Quantity, o.Filled, o.Status, o.Exchange, o.UserID,
		o.ClientOID, o.ExchangeOrderID, o.AvgFillPrice, o.MarketType, o.PositionSide, o.Leverage, o.MarginMode, o.TPPrice, o.SLPrice, o.ClosePosition, o.ID,
	)
	return err
}

// Upsert 按主键 id 存在即更新、不存在即插入。
// 修正原 "Update 失败再 Create" 的伪 upsert：UPDATE 命中 0 行并不报错，
// 导致首写永远落不了库（OMS 订单只有经 handler/镜像绕行才有 DB 行）。
func (r *OrderRepo) Upsert(o *OrderRecord) error {
	if o.ID == "" {
		o.ID = fmt.Sprintf("ord-%d", time.Now().UnixMilli())
	}
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().UnixMilli()
	}
	o.UpdatedAt = time.Now().UnixMilli()
	if o.Status == "" {
		o.Status = "NEW"
	}
	if o.Exchange == "" {
		o.Exchange = "BINANCE"
	}
	_, err := db.Exec(
		`INSERT INTO xt_orders (id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET symbol=excluded.symbol, side=excluded.side, order_type=excluded.order_type,
		 price=excluded.price, stop_price=excluded.stop_price, quantity=excluded.quantity, filled=excluded.filled,
		 status=excluded.status, exchange=excluded.exchange, user_id=excluded.user_id, client_oid=excluded.client_oid,
		 exchange_order_id=excluded.exchange_order_id, avg_fill_price=excluded.avg_fill_price, updated_at=excluded.updated_at, market_type=excluded.market_type,
		 position_side=excluded.position_side, leverage=excluded.leverage, margin_mode=excluded.margin_mode,
		 tp_price=excluded.tp_price, sl_price=excluded.sl_price, close_position=excluded.close_position`,
		o.ID, o.Symbol, o.Side, o.OrderType, o.Price, o.StopPrice, o.Quantity, o.Filled, o.Status, o.Exchange,
		o.UserID, o.ClientOID, o.ExchangeOrderID, o.AvgFillPrice, o.CreatedAt, o.UpdatedAt,
		o.MarketType, o.PositionSide, o.Leverage, o.MarginMode, o.TPPrice, o.SLPrice, o.ClosePosition,
	)
	return err
}

func (r *OrderRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_orders WHERE id=?", id)
	return err
}

// ListRecent 返回 updated_at >= sinceMs 的订单（新→旧），偏差监控等扫描用。
func (r *OrderRepo) ListRecent(sinceMs int64, limit int) ([]*OrderRecord, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := db.Query(`SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position
		FROM xt_orders WHERE updated_at >= ? ORDER BY updated_at DESC LIMIT ?`, sinceMs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
		// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.ExchangeOrderID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
		result = append(result, &o)
	}
	return result, nil
}

// ListFilledByClientOIDPrefix 返回 client_oid 带指定前缀的已成交订单（新→旧），
// 成交记录页按策略过滤用（sig:<策略id>:% 打标归属）。
func (r *OrderRepo) ListFilledByClientOIDPrefix(prefix, symbol string, limit int) ([]*OrderRecord, error) {
	q := `SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, exchange_order_id, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position
		FROM xt_orders WHERE client_oid LIKE ? AND status='FILLED' AND filled>0 AND avg_fill_price>0`
	args := []any{prefix + "%"}
	if symbol != "" {
		q += " AND UPPER(symbol)=UPPER(?)"
		args = append(args, symbol)
	}
	q += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.ExchangeOrderID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
		result = append(result, &o)
	}
	return result, nil
}

// NetFilledByStrategy 汇总某策略的已成交净买量与买入 VWAP：
// signal 下单链路给订单打标 client_oid "sig:<策略配置id>:<nonce>"，
// 据此把成交归属到策略（orders 表无 strategy 列，Source 不落库）。
// 重启仓位重建用：净买量 = Σ买入filled − Σ卖出filled；VWAP 按各买单
// avg_fill_price 加权。无记录返回 0。
func NetFilledByStrategy(strategyID, symbol string) (netQty, buyVWAP float64, err error) {
	prefix := "sig:" + strategyID + ":%"
	rows, err := db.Query(
		`SELECT side, COALESCE(SUM(filled),0), COALESCE(SUM(filled*avg_fill_price),0)
		 FROM xt_orders WHERE client_oid LIKE ? AND UPPER(symbol)=UPPER(?)
		 AND status='FILLED' AND filled>0 AND avg_fill_price>0 GROUP BY side`,
		prefix, symbol)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var buyQty, sellQty, buyCost float64
	for rows.Next() {
		var side string
		var qty, cost float64
		if err := rows.Scan(&side, &qty, &cost); err != nil {
			return 0, 0, err
		}
		switch strings.ToUpper(side) {
		case "BUY":
			buyQty, buyCost = qty, cost
		case "SELL":
			sellQty = qty
		}
	}
	netQty = buyQty - sellQty
	if buyQty > 0 {
		buyVWAP = buyCost / buyQty
	}
	if netQty < 0 {
		netQty = 0 // 净卖出/无持仓不恢复
	}
	return netQty, buyVWAP, nil
}

// OrderFill 一笔已成交订单的重建视图（重启分档重建输入）。
// ClientOID 保留归属打标：G1 手动补仓单带 ":manual:" 中缀，策略侧重放时
// 据此恢复 Manual 档（手动补仓重启后依然不推自动阶梯）。
type OrderFill struct {
	Side         string  `json:"side"` // BUY | SELL
	Filled       float64 `json:"filled"`
	AvgFillPrice float64 `json:"avg_fill_price"`
	ClientOID    string  `json:"client_oid"`
}

// FilledOrdersByStrategy 按成交时间升序返回策略的已成交明细（归属口径同
// NetFilledByStrategy：client_oid "sig:<id>:" 打标 + FILLED + filled>0 +
// avg_fill_price>0）。CRA 分档持仓重启重建（尾单/首尾止盈的各档成本）用，
// 由策略侧重放（cra.RebuildLotsFromFills）。
func FilledOrdersByStrategy(strategyID, symbol string) ([]OrderFill, error) {
	prefix := "sig:" + strategyID + ":%"
	rows, err := db.Query(
		`SELECT side, filled, avg_fill_price, COALESCE(client_oid,'') FROM xt_orders
		 WHERE client_oid LIKE ? AND UPPER(symbol)=UPPER(?)
		 AND status='FILLED' AND filled>0 AND avg_fill_price>0
		 ORDER BY updated_at ASC, created_at ASC, rowid ASC`,
		prefix, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrderFill
	for rows.Next() {
		var f OrderFill
		if err := rows.Scan(&f.Side, &f.Filled, &f.AvgFillPrice, &f.ClientOID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// StrategyPaperPnL 按成交账本（client_oid "sig:<id>:" 打标）用平均成本法逐笔
// 匹配，计算策略实例的模拟盘绩效：已实现盈亏 + 现价×净持仓的浮动盈亏 −
// 双边手续费（feeRate，与 paper 账户结算同口径，2026-10-04 补扣——此前
// 显示盈亏高于账户实际，交易越多偏差越大）。
// allowShort=true（合约）时支持双向：SELL 开/加空、BUY 平空，盈亏按
// (开空价−价)×量计；现货策略保持净多口径，超出持仓的 SELL 被保护性忽略
// （对账/重建行防卖穿）。
// ok=false 表示无任何成交记录（调用方不应覆盖既有展示值）。
func StrategyPaperPnL(strategyID, symbol string, markPrice, feeRate float64, allowShort bool) (netQty, avgCost, realized, unrealized, totalPnl float64, ok bool) {
	prefix := "sig:" + strategyID + ":%"
	rows, err := db.Query(`SELECT side, filled, avg_fill_price FROM xt_orders
		WHERE client_oid LIKE ? AND UPPER(symbol)=UPPER(?) AND status='FILLED' AND filled>0 AND avg_fill_price>0
		ORDER BY updated_at ASC, created_at ASC, rowid ASC`, prefix, symbol)
	if err != nil {
		return 0, 0, 0, 0, 0, false
	}
	defer rows.Close()
	totalFee := 0.0
	for rows.Next() {
		var side string
		var qty, price float64
		if rows.Scan(&side, &qty, &price) != nil {
			return 0, 0, 0, 0, 0, false
		}
		ok = true
		switch strings.ToUpper(side) {
		case "BUY":
			totalFee += price * qty * feeRate
			if netQty < 0 && allowShort {
				// 平空：低价买回盈利。
				closeQty := qty
				if closeQty > -netQty {
					closeQty = -netQty
				}
				realized += (avgCost - price) * closeQty
				netQty += closeQty
				if rest := qty - closeQty; rest > 0 {
					// 超出部分转为开多。
					newQty := netQty + rest
					avgCost = (avgCost*netQty + price*rest) / newQty
					netQty = newQty
				}
			} else {
				newQty := netQty + qty
				if newQty > 0 {
					avgCost = (avgCost*netQty + price*qty) / newQty
				}
				netQty = newQty
			}
		case "SELL":
			if netQty > 0 {
				sellQty := qty
				if !allowShort && sellQty > netQty {
					sellQty = netQty // 现货超卖保护（对账/重建行不卖穿）
				}
				if sellQty > 0 {
					totalFee += price * sellQty * feeRate
					realized += (price - avgCost) * sellQty
					netQty -= sellQty
				}
				if allowShort {
					if rest := qty - sellQty; rest > 0 {
						// 多余部分开空。
						totalFee += price * rest * feeRate
						newShort := -netQty + rest
						avgCost = (avgCost*(-netQty) + price*rest) / newShort
						netQty = -newShort
					}
				}
			} else if allowShort {
				// 开空/加空。
				totalFee += price * qty * feeRate
				newShort := -netQty + qty
				avgCost = (avgCost*(-netQty) + price*qty) / newShort
				netQty = -newShort
			}
			// 现货且无多仓：SELL 整笔忽略（无费、无仓位变化）。
		}
	}
	switch {
	case netQty > 1e-12 && markPrice > 0:
		unrealized = (markPrice - avgCost) * netQty
	case netQty < -1e-12 && markPrice > 0:
		unrealized = (avgCost - markPrice) * (-netQty) // 空单：价跌盈利
	}
	totalPnl = realized + unrealized - totalFee
	return netQty, avgCost, realized, unrealized, totalPnl, ok
}
