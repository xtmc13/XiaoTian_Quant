package order

import (
	"fmt"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Order Status State Machine ──

// ValidateTransition checks if a status transition is valid.
func ValidateTransition(from, to model.OrderStatus) error {
	for _, valid := range model.StatusTransitions[from] {
		if valid == to {
			return nil
		}
	}
	return fmt.Errorf("invalid transition: %s -> %s", from, to)
}

// ── Order Request ──

// Request is the input payload for order placement.
type Request struct {
	Symbol    string          `json:"symbol"`
	Side      model.OrderSide `json:"side"`
	OrderType model.OrderType `json:"order_type"`
	Price     float64         `json:"price"`
	StopPrice float64         `json:"stop_price,omitempty"`
	Quantity  float64         `json:"quantity"`
	Exchange  string          `json:"exchange"`
	UserID    uint64          `json:"user_id"`
	ClientOID string          `json:"client_oid,omitempty"`

	// ── Contract fields ──
	MarketType    model.MarketType   `json:"market_type,omitempty"`
	PositionSide  model.PositionSide `json:"position_side,omitempty"`
	Leverage      float64            `json:"leverage,omitempty"`
	MarginMode    model.MarginMode   `json:"margin_mode,omitempty"`
	TPPrice       float64            `json:"tp_price,omitempty"`
	SLPrice       float64            `json:"sl_price,omitempty"`
	ClosePosition bool               `json:"close_position,omitempty"`

	// ── AI 决策门（aigate） ──
	// Source 标记订单来源（"signal:<strategy>" 等）；空时由决策门从
	// ClientOID 前缀（"dca:botID" 等）推断，再兜底 "manual"。
	Source string `json:"source,omitempty"`
	// AIGateBypass=true 表示本单是条件单触发/高级引擎子单（TWAP/冰山等），
	// 父单的入场决策已审批过，子单不再重复过 AI 门。
	AIGateBypass bool `json:"ai_gate_bypass,omitempty"`

	// ── A2.2 limit-then-market ──
	// LimitTimeoutMs > 0 时启用：限价单超时未完全成交则撤剩余并市价补单。
	LimitTimeoutMs int64 `json:"limit_timeout_ms,omitempty"`
}

func (r *Request) Validate() error {
	if r.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if r.Side != model.SideBuy && r.Side != model.SideSell {
		return fmt.Errorf("invalid side: %s", r.Side)
	}
	if r.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if r.OrderType == model.TypeLimit && r.Price <= 0 {
		return fmt.Errorf("limit order requires a positive price")
	}
	if r.OrderType == "" {
		r.OrderType = model.TypeLimit
	}
	if r.Exchange == "" {
		r.Exchange = "paper"
	}
	if r.LimitTimeoutMs < 0 {
		return fmt.Errorf("limit_timeout_ms must be >= 0")
	}
	if r.LimitTimeoutMs > 0 && r.OrderType != model.TypeLimit {
		return fmt.Errorf("limit_timeout_ms only applies to limit orders")
	}
	return nil
}

// ── Order Manager ──

// OrderManager coordinates the full order lifecycle.
type OrderManager struct {
	orders   map[string]*model.OrderData
	orderMu  sync.RWMutex
	orderSeq int64

	// Pipeline hooks (set during integration)
	RiskCheck        func(req *Request) error
	LockBalance      func(req *Request) error
	UnlockBalance    func(order *model.OrderData)
	SubmitToExchange func(order *model.OrderData) (map[string]any, error)
	CancelOnExchange func(order *model.OrderData) error
	OnOrderUpdate    func(order *model.OrderData)

	// ── AI 决策门钩子（aigate 包生产实现，见 app.Context.wireOrderManager）──
	// AIGateCheck 在 RiskCheck 之后、LockBalance 之前调用，仅评估入场单
	// （出场/止损/引擎子单在门内直接放行）。返回 decisionID（空=未评估），
	// error 非空表示 AI 拦截。nil 时完全跳过（零开销）。
	AIGateCheck func(req *Request) (string, error)
	// AIGateOutcome 在订单终态后回写决策记录的 order_id 与成交结果。nil 跳过。
	AIGateOutcome func(decisionID, orderID string, executed bool)

	// Rate limiting
	rateLimitBurst int
	rateWindowMs   int64
	lastOrderTime  int64
}

var (
	instance     *OrderManager
	instanceOnce sync.Once
)

// GetOrderManager returns the global order manager instance.
func GetOrderManager() *OrderManager {
	instanceOnce.Do(func() {
		instance = &OrderManager{
			orders:         make(map[string]*model.OrderData),
			rateLimitBurst: 10,
			rateWindowMs:   100,
		}
	})
	return instance
}

// PlaceOrder processes a new order request through the full pipeline.
func (om *OrderManager) PlaceOrder(req *Request) (*model.OrderData, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	// Rate check
	if !om.checkRateLimit() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Create order
	order := &model.OrderData{
		ID:        om.generateID(),
		Symbol:    req.Symbol,
		Side:      req.Side,
		OrderType: req.OrderType,
		Price:     req.Price,
		StopPrice: req.StopPrice,
		Quantity:  req.Quantity,
		Status:    model.StatusCreated,
		Exchange:  req.Exchange,
		UserID:    req.UserID,
		ClientOID: req.ClientOID,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),

		// ── Contract fields ──
		MarketType:    req.MarketType,
		PositionSide:  req.PositionSide,
		Leverage:      req.Leverage,
		MarginMode:    req.MarginMode,
		TPPrice:       req.TPPrice,
		SLPrice:       req.SLPrice,
		ClosePosition: req.ClosePosition,
	}

	// Risk check
	if om.RiskCheck != nil {
		if err := om.RiskCheck(req); err != nil {
			order.Status = model.StatusRejected
			om.storeOrder(order)
			return order, fmt.Errorf("risk check: %w", err)
		}
	}

	// AI 决策门（入场单强制审批；出场/止损/引擎子单在门内直接放行；
	// provider 故障 fail-open）。被拒按风控拒绝同口径：REJECTED 落库。
	var gateDecisionID string
	if om.AIGateCheck != nil {
		id, err := om.AIGateCheck(req)
		if err != nil {
			order.Status = model.StatusRejected
			om.storeOrder(order)
			if id != "" && om.AIGateOutcome != nil {
				om.AIGateOutcome(id, order.ID, false)
			}
			return order, fmt.Errorf("ai gate: %w", err)
		}
		gateDecisionID = id
	}

	// Lock balance
	if om.LockBalance != nil {
		if err := om.LockBalance(req); err != nil {
			order.Status = model.StatusRejected
			om.storeOrder(order)
			om.reportGateOutcome(gateDecisionID, order.ID, false)
			return order, fmt.Errorf("balance lock: %w", err)
		}
	}

	order.Status = model.StatusPending
	om.storeOrder(order)

	// Submit to exchange
	if om.SubmitToExchange != nil {
		result, err := om.SubmitToExchange(order)
		if err != nil {
			order.Status = model.StatusRejected
			order.UpdatedAt = time.Now().UnixMilli()
			om.storeOrder(order)
			if om.UnlockBalance != nil {
				om.UnlockBalance(order)
			}
			om.reportGateOutcome(gateDecisionID, order.ID, false)
			return order, fmt.Errorf("exchange submit: %w", err)
		}

		// Update from exchange response
		if id, ok := result["order_id"].(string); ok {
			order.ID = id
		}
		if status, ok := result["status"].(string); ok {
			order.Status = model.OrderStatus(status)
		}
		if filled, ok := result["filled"].(float64); ok {
			order.Filled = filled
		}
	}

	order.UpdatedAt = time.Now().UnixMilli()
	om.storeOrder(order)
	om.reportGateOutcome(gateDecisionID, order.ID, order.Status == model.StatusFilled)

	// Register conditional orders (TP/SL/Stop-Limit) with the conditional engine
	if order.TPPrice > 0 || order.SLPrice > 0 {
		GetConditionalEngine().RegisterTPSL(order, order.TPPrice, order.SLPrice)
	}
	if order.OrderType == model.TypeStopLossLimit || order.OrderType == model.TypeTakeProfitLimit {
		if order.StopPrice > 0 {
			GetConditionalEngine().RegisterStopLimit(order, order.StopPrice)
		}
	}

	// Handle advanced order types (Iceberg, TWAP, VWAP)
	if order.OrderType == model.TypeIceberg || order.OrderType == model.TypeTWAP || order.OrderType == model.TypeVWAP {
		return om.handleAdvancedOrder(order)
	}

	if om.OnOrderUpdate != nil {
		om.OnOrderUpdate(order)
	}

	return order, nil
}

// handleAdvancedOrder delegates iceberg/TWAP/VWAP orders to the advanced order engine.
func (om *OrderManager) handleAdvancedOrder(order *model.OrderData) (*model.OrderData, error) {
	ae := GetAdvancedOrderEngine()
	var advOrd *AdvancedOrder
	var err error

	switch order.OrderType {
	case model.TypeIceberg:
		// Default: 10% visible, slice = visible amount
		visibleQty := order.Quantity * 0.1
		sliceSize := visibleQty
		advOrd, err = ae.SubmitIceberg(order, visibleQty, sliceSize)
	case model.TypeTWAP:
		advOrd, err = ae.SubmitTWAP(order, 60000, 10) // 1 min, 10 slices
	case model.TypeVWAP:
		advOrd, err = ae.SubmitVWAP(order, 60000, 10) // 1 min, 10 slices
	}

	if err != nil {
		order.Status = model.StatusRejected
		om.storeOrder(order)
		return order, fmt.Errorf("advanced order: %w", err)
	}

	order.Status = model.StatusNew
	om.storeOrder(order)

	// Link the advanced order ID
	order.ClientOID = advOrd.ID

	return order, nil
}

// CancelOrder cancels an active order.
func (om *OrderManager) CancelOrder(orderID, symbol string) (*model.OrderData, error) {
	om.orderMu.Lock()
	order, ok := om.orders[orderID]
	om.orderMu.Unlock()

	if !ok {
		return nil, fmt.Errorf("order %s not found", orderID)
	}
	if order.IsDone() {
		return nil, fmt.Errorf("order %s already %s", orderID, order.Status)
	}

	// Try to cancel on exchange if applicable
	if om.CancelOnExchange != nil {
		if err := om.CancelOnExchange(order); err != nil {
			return nil, fmt.Errorf("exchange cancel: %w", err)
		}
	}

	order.Status = model.StatusCancelled
	order.UpdatedAt = time.Now().UnixMilli()
	om.storeOrder(order)

	if om.UnlockBalance != nil {
		om.UnlockBalance(order)
	}
	if om.OnOrderUpdate != nil {
		om.OnOrderUpdate(order)
	}

	return order, nil
}

// GetOrder returns an order by ID.
func (om *OrderManager) GetOrder(orderID string) *model.OrderData {
	om.orderMu.RLock()
	defer om.orderMu.RUnlock()
	return om.orders[orderID]
}

// GetOpenOrders returns all active orders, optionally filtered by symbol.
func (om *OrderManager) GetOpenOrders(symbol string) []*model.OrderData {
	om.orderMu.RLock()
	defer om.orderMu.RUnlock()
	var result []*model.OrderData
	for _, o := range om.orders {
		if o.IsActive() && (symbol == "" || o.Symbol == symbol) {
			result = append(result, o)
		}
	}
	return result
}

// GetOrderHistory returns terminal orders.
func (om *OrderManager) GetOrderHistory(symbol string, limit int) []*model.OrderData {
	om.orderMu.RLock()
	defer om.orderMu.RUnlock()
	var result []*model.OrderData
	for _, o := range om.orders {
		if o.IsDone() && (symbol == "" || o.Symbol == symbol) {
			result = append(result, o)
		}
	}
	// Sort by updated_at descending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].UpdatedAt > result[i].UpdatedAt {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

// ── Event Handling ──

// HandleOrderUpdate processes an order update from external sources (e.g., WebSocket).
func (om *OrderManager) HandleOrderUpdate(orderData *model.OrderData) {
	om.orderMu.Lock()
	existing, ok := om.orders[orderData.ID]
	if ok {
		*existing = *orderData
	} else {
		om.orders[orderData.ID] = orderData
	}
	om.orderMu.Unlock()

	if om.OnOrderUpdate != nil {
		om.OnOrderUpdate(orderData)
	}
}

// ── Internal ──

// reportGateOutcome 回写 AI 决策的成交结果（decisionID 为空或钩子未设置时跳过）。
func (om *OrderManager) reportGateOutcome(decisionID, orderID string, executed bool) {
	if decisionID == "" || om.AIGateOutcome == nil {
		return
	}
	om.AIGateOutcome(decisionID, orderID, executed)
}

func (om *OrderManager) storeOrder(order *model.OrderData) {
	om.orderMu.Lock()
	defer om.orderMu.Unlock()
	// Make a copy
	copy_ := *order
	om.orders[order.ID] = &copy_

	// Persist to database via OrderRepo (upsert pattern)
	rec := &store.OrderRecord{
		ID:            copy_.ID,
		Symbol:        copy_.Symbol,
		Side:          string(copy_.Side),
		OrderType:     string(copy_.OrderType),
		Price:         copy_.Price,
		StopPrice:     copy_.StopPrice,
		Quantity:      copy_.Quantity,
		Filled:        copy_.Filled,
		Status:        string(copy_.Status),
		Exchange:      copy_.Exchange,
		UserID:        copy_.UserID,
		ClientOID:     copy_.ClientOID,
		AvgFillPrice:  copy_.AvgFillPrice,
		CreatedAt:     copy_.CreatedAt,
		UpdatedAt:     copy_.UpdatedAt,
		MarketType:    string(copy_.MarketType),
		PositionSide:  string(copy_.PositionSide),
		Leverage:      copy_.Leverage,
		MarginMode:    string(copy_.MarginMode),
		TPPrice:       copy_.TPPrice,
		SLPrice:       copy_.SLPrice,
		ClosePosition: copy_.ClosePosition,
	}

	// 首写插入、状态翻转更新，一次调用完成（原 Update 命中 0 行不报错，
	// 首写永远落不了库）。
	_ = store.GetOrderRepo().Upsert(rec)
}

func (om *OrderManager) generateID() string {
	om.orderMu.Lock()
	defer om.orderMu.Unlock()
	om.orderSeq++
	return fmt.Sprintf("ord-%d-%d", time.Now().UnixMilli(), om.orderSeq)
}

func (om *OrderManager) checkRateLimit() bool {
	now := time.Now().UnixMilli()
	om.orderMu.Lock()
	defer om.orderMu.Unlock()

	if now-om.lastOrderTime < om.rateWindowMs {
		om.orderSeq++
		if om.orderSeq > int64(om.rateLimitBurst) {
			return false
		}
	} else {
		om.orderSeq = 0
	}
	om.lastOrderTime = now
	return true
}

// ActiveOrderCount returns the number of active orders.
func (om *OrderManager) ActiveOrderCount() int {
	om.orderMu.RLock()
	defer om.orderMu.RUnlock()
	count := 0
	for _, o := range om.orders {
		if o.IsActive() {
			count++
		}
	}
	return count
}
