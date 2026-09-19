package order

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// A2.2 limit-then-market 执行：下单带 limit_timeout_ms（>0 启用）时，先挂限价单，
// 超时未完全成交则撤销剩余并市价补单。paper/live 共用本实现（差异在 OMS 的
// SubmitToExchange/CancelOnExchange 钩子里）。
//
// 成交明细区分：限价部分成交挂在父订单（client_oid 带 "ltm-" 前缀），市价补单
// 是独立订单（client_oid 带 "ltm-mkt:" 前缀），两部分通过 execution_parts 汇总，
// 落 trades 时带 exec_phase=limit/market 标记。

// lmOrderPlacer 是 tracker 对 OMS 的窄依赖（测试可注入 fake）。
type lmOrderPlacer interface {
	PlaceOrder(req *Request) (*model.OrderData, error)
	CancelOrder(orderID, symbol string) (*model.OrderData, error)
	GetOrder(orderID string) *model.OrderData
}

// ExecutionPart 一段执行明细（限价段 / 市价段）。
type ExecutionPart struct {
	Phase    string  `json:"phase"` // limit|market
	OrderID  string  `json:"order_id"`
	Filled   float64 `json:"filled"`
	AvgPrice float64 `json:"avg_price"`
	Status   string  `json:"status"`
}

// LMState limit-then-market 订单的运行状态。
type LMState struct {
	ParentID string          `json:"parent_id"`
	Symbol   string          `json:"symbol"`
	Side     model.OrderSide `json:"side"`
	Exchange string          `json:"exchange"`
	Quantity float64         `json:"quantity"`

	Status string `json:"status"` // active|completed|cancel_failed|market_failed

	LimitOrderID  string  `json:"limit_order_id"`
	LimitFilled   float64 `json:"limit_filled"`
	LimitAvgPrice float64 `json:"limit_avg_price"`

	MarketOrderID  string  `json:"market_order_id"`
	MarketFilled   float64 `json:"market_filled"`
	MarketAvgPrice float64 `json:"market_avg_price"`

	CreatedAt int64 `json:"created_at"`
	Deadline  int64 `json:"deadline"`
	UpdatedAt int64 `json:"updated_at"`
}

// IsTerminal 状态是否已终结（不再被 watcher 处理）。
func (s *LMState) IsTerminal() bool {
	return s.Status == "completed" || s.Status == "cancel_failed" || s.Status == "market_failed"
}

// Parts 汇总 limit/market 两段成交明细。
func (s *LMState) Parts() []ExecutionPart {
	parts := []ExecutionPart{{
		Phase: "limit", OrderID: s.LimitOrderID,
		Filled: s.LimitFilled, AvgPrice: s.LimitAvgPrice, Status: "done",
	}}
	if s.MarketOrderID != "" {
		parts = append(parts, ExecutionPart{
			Phase: "market", OrderID: s.MarketOrderID,
			Filled: s.MarketFilled, AvgPrice: s.MarketAvgPrice, Status: "done",
		})
	}
	return parts
}

// LimitMarketTracker 管理所有 limit-then-market 订单的超时与补单。
type LimitMarketTracker struct {
	placer lmOrderPlacer

	mu      sync.Mutex
	orders  map[string]*LMState
	started bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

var (
	lmTracker     *LimitMarketTracker
	lmTrackerOnce sync.Once
)

// GetLimitMarketTracker 返回全局 tracker（生产接全局 OrderManager）。
func GetLimitMarketTracker() *LimitMarketTracker {
	lmTrackerOnce.Do(func() {
		lmTracker = NewLimitMarketTracker(GetOrderManager())
	})
	return lmTracker
}

// NewLimitMarketTracker placer 通常为 *OrderManager（满足窄接口）。
func NewLimitMarketTracker(placer lmOrderPlacer) *LimitMarketTracker {
	return &LimitMarketTracker{
		placer: placer,
		orders: make(map[string]*LMState),
		stopCh: make(chan struct{}),
	}
}

// Start 拉起 watcher goroutine（幂等）。
func (t *LimitMarketTracker) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return
	}
	t.started = true
	t.doneCh = make(chan struct{})
	go t.watchLoop()
}

// Stop 优雅停止 watcher（幂等）。
func (t *LimitMarketTracker) Stop() {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return
	}
	t.started = false
	close(t.stopCh)
	done := t.doneCh
	t.mu.Unlock()
	<-done
}

// watchLoop 周期扫描超时订单。100ms 粒度对默认秒级超时足够。
func (t *LimitMarketTracker) watchLoop() {
	defer close(t.doneCh)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.scanOnce()
		}
	}
}

func (t *LimitMarketTracker) scanOnce() {
	now := time.Now().UnixMilli()
	t.mu.Lock()
	var due []*LMState
	for _, s := range t.orders {
		if !s.IsTerminal() && s.Deadline <= now {
			due = append(due, s)
		}
	}
	t.mu.Unlock()
	for _, s := range due {
		t.completeWithMarket(s)
	}
}

// PlaceLimitThenMarket 下 limit-then-market 订单：
// 先下限价单；未完全成交的部分在 timeout 后撤单并市价补齐。
// 返回父订单与初始状态。限价单立即全成交时状态直接 completed。
func (t *LimitMarketTracker) PlaceLimitThenMarket(req *Request, timeout time.Duration) (*model.OrderData, *LMState, error) {
	if timeout <= 0 {
		return nil, nil, fmt.Errorf("limit timeout must be positive")
	}
	if req.OrderType != model.TypeLimit {
		return nil, nil, fmt.Errorf("limit-then-market requires a LIMIT order")
	}
	if req.ClientOID == "" {
		req.ClientOID = "ltm-" + newLMID()
	}

	limitReq := *req // 拷贝，避免污染调用方
	ord, err := t.placer.PlaceOrder(&limitReq)
	if err != nil {
		return ord, nil, err
	}

	now := time.Now().UnixMilli()
	state := &LMState{
		ParentID:      ord.ID,
		Symbol:        ord.Symbol,
		Side:          ord.Side,
		Exchange:      ord.Exchange,
		Quantity:      ord.Quantity,
		LimitOrderID:  ord.ID,
		LimitFilled:   ord.Filled,
		LimitAvgPrice: ord.AvgFillPrice,
		CreatedAt:     now,
		Deadline:      now + timeout.Milliseconds(),
		UpdatedAt:     now,
	}

	if ord.Filled >= ord.Quantity {
		state.Status = "completed"
		if state.LimitAvgPrice <= 0 {
			state.LimitAvgPrice = ord.Price
		}
	} else {
		state.Status = "active"
		t.mu.Lock()
		t.orders[ord.ID] = state
		t.mu.Unlock()
	}
	recordLMTrade(ord, "limit")
	return ord, state, nil
}

// recordLMTrade 落 limit-then-market 成交明细（exec_phase 区分 limit/market 两段；
// id 按订单维度幂等，重复调用/重复恢复不会双记）。store 未初始化（单测）时跳过。
func recordLMTrade(ord *model.OrderData, phase string) {
	if ord == nil || ord.Filled <= 0 || store.GetDB() == nil {
		return
	}
	id := fmt.Sprintf("%s:%s:%s", strings.ToLower(ord.Exchange), ord.ID, phase)
	tradeRepo := store.NewTradeRepo()
	if tradeRepo.Exists(id) {
		return
	}
	avg := ord.AvgFillPrice
	if avg <= 0 {
		avg = ord.Price
	}
	_ = tradeRepo.Create(&store.TradeRecord{
		ID:        id,
		UserID:    int64(ord.UserID),
		OrderID:   ord.ID,
		Symbol:    ord.Symbol,
		Side:      strings.ToUpper(string(ord.Side)),
		Price:     avg,
		Quantity:  ord.Filled,
		Exchange:  ord.Exchange,
		ExecPhase: phase,
		CreatedAt: ord.UpdatedAt,
	})
}

// completeWithMarket 超时处理：撤掉限价剩余 → 市价补单。
// 撤单失败不补单（避免双边暴露），状态置 cancel_failed 并告警。
func (t *LimitMarketTracker) completeWithMarket(s *LMState) {
	t.mu.Lock()
	if s.IsTerminal() || s.Status == "cancelling" {
		t.mu.Unlock()
		return
	}
	if s.MarketOrderID != "" {
		// 市价补单已下：只确认其结果，不再重复撤限价单。
		t.mu.Unlock()
		t.confirmMarket(s)
		return
	}
	s.Status = "cancelling"
	t.mu.Unlock()

	limitOrd := t.placer.GetOrder(s.LimitOrderID)
	if limitOrd == nil {
		t.finish(s, "cancel_failed", fmt.Sprintf("limit order %s missing", s.LimitOrderID))
		return
	}
	limitFilled := limitOrd.Filled
	limitAvg := limitOrd.AvgFillPrice

	remaining := s.Quantity - limitFilled
	if remaining <= 1e-12 {
		// 超时前已全部成交。
		t.mu.Lock()
		s.LimitFilled = limitFilled
		s.LimitAvgPrice = avgOr(limitAvg, limitOrd.Price)
		t.mu.Unlock()
		t.finish(s, "completed", "")
		return
	}

	if _, err := t.placer.CancelOrder(s.LimitOrderID, s.Symbol); err != nil {
		// 撤单失败：先复查订单实际状态——超时窗口内可能已完全成交。
		if fresh := t.placer.GetOrder(s.LimitOrderID); fresh != nil &&
			(fresh.Status == model.StatusFilled || fresh.Filled >= s.Quantity) {
			t.mu.Lock()
			s.LimitFilled = fresh.Filled
			s.LimitAvgPrice = avgOr(fresh.AvgFillPrice, fresh.Price)
			t.mu.Unlock()
			t.finish(s, "completed", "")
			return
		}
		// 确实撤不掉：不补单（可能实际已成交/已撤，补单会造成重复敞口），交由人工/对账兜底。
		t.finish(s, "cancel_failed", fmt.Sprintf("cancel limit order failed: %v", err))
		return
	}

	// 撤单途中可能又有成交，重取剩余。
	if fresh := t.placer.GetOrder(s.LimitOrderID); fresh != nil && fresh.Filled > limitFilled {
		limitFilled = fresh.Filled
		remaining = s.Quantity - limitFilled
	}
	t.mu.Lock()
	s.LimitFilled = limitFilled
	s.LimitAvgPrice = avgOr(limitAvg, 0)
	t.mu.Unlock()

	if remaining <= 1e-12 {
		t.finish(s, "completed", "")
		return
	}

	// 市价补单：继承父单字段（合约参数/用户/交易所），数量=剩余。
	mktReq := &Request{
		Symbol:        s.Symbol,
		Side:          s.Side,
		OrderType:     model.TypeMarket,
		Quantity:      remaining,
		Exchange:      s.Exchange,
		UserID:        limitOrd.UserID,
		ClientOID:     "ltm-mkt:" + s.LimitOrderID,
		MarketType:    limitOrd.MarketType,
		PositionSide:  limitOrd.PositionSide,
		Leverage:      limitOrd.Leverage,
		MarginMode:    limitOrd.MarginMode,
		ClosePosition: limitOrd.ClosePosition,
	}

	mktOrd, err := t.placer.PlaceOrder(mktReq)
	if err != nil {
		t.finish(s, "market_failed", fmt.Sprintf("market fallback failed: %v", err))
		return
	}
	t.mu.Lock()
	s.MarketOrderID = mktOrd.ID
	s.MarketFilled = mktOrd.Filled
	s.MarketAvgPrice = avgOr(mktOrd.AvgFillPrice, mktOrd.Price)
	t.mu.Unlock()
	recordLMTrade(mktOrd, "market")
	syncLMOrderToLegacyStore(mktOrd)

	if mktOrd.Filled >= remaining || mktOrd.Status == model.StatusFilled {
		t.finish(s, "completed", "")
	} else {
		// 市价单未即时成交（live 交易所常见）：watcher 下轮再确认。
		t.mu.Lock()
		s.Status = "active"
		s.Deadline = time.Now().Add(5 * time.Second).UnixMilli() // 给市价单 5s 确认窗口
		t.mu.Unlock()
	}
}

// confirmMarket 确认已下市价补单的成交结果。
func (t *LimitMarketTracker) confirmMarket(s *LMState) {
	mkt := t.placer.GetOrder(s.MarketOrderID)
	if mkt == nil {
		t.finish(s, "market_failed", "market order missing")
		return
	}
	t.mu.Lock()
	s.MarketFilled = mkt.Filled
	s.MarketAvgPrice = avgOr(mkt.AvgFillPrice, mkt.Price)
	done := mkt.Filled >= s.Quantity-s.LimitFilled || mkt.Status == model.StatusFilled
	if !done && (mkt.Status == model.StatusRejected || mkt.Status == model.StatusCancelled || mkt.Status == model.StatusExpired) {
		t.mu.Unlock()
		t.finish(s, "market_failed", fmt.Sprintf("market order ended %s filled=%.8f", mkt.Status, mkt.Filled))
		return
	}
	t.mu.Unlock()
	if done {
		t.finish(s, "completed", "")
		return
	}
	// 仍在成交中：继续等待，下一轮再确认。
	t.mu.Lock()
	s.Status = "active"
	s.Deadline = time.Now().Add(5 * time.Second).UnixMilli()
	t.mu.Unlock()
}

// finish 置终态并清理。
func (t *LimitMarketTracker) finish(s *LMState, status, msg string) {
	t.mu.Lock()
	s.Status = status
	s.UpdatedAt = time.Now().UnixMilli()
	t.mu.Unlock()
	if msg != "" || status != "completed" {
		log.Printf("[ltm] %s %s %s: %s", s.Symbol, s.LimitOrderID, status, msg)
	}
	if status == "cancel_failed" || status == "market_failed" {
		t.notifyFailure(s, msg)
	}
}

func (t *LimitMarketTracker) notifyFailure(s *LMState, msg string) {
	if lmNotifyHook != nil {
		lmNotifyHook(s, msg)
	}
}

// lmNotifyHook 失败告警钩子（main 接线时注入 notify 模块；nil 时仅记日志）。
var lmNotifyHook func(s *LMState, msg string)

// SetNotifyHook 注入失败告警回调。
func (t *LimitMarketTracker) SetNotifyHook(fn func(*LMState, string)) { lmNotifyHook = fn }

// syncLMOrderToLegacyStore 把 limit-then-market 的市价补单同步进 legacy 订单存储，
// 让 GET /orders、CancelAllOrders 等存量接口能看到补单（store 未初始化时跳过）。
func syncLMOrderToLegacyStore(ord *model.OrderData) {
	if ord == nil || store.GetDB() == nil {
		return
	}
	store.PlaceOrder(map[string]any{
		"id":             ord.ID,
		"order_id":       ord.ID,
		"symbol":         ord.Symbol,
		"side":           string(ord.Side),
		"order_type":     string(ord.OrderType),
		"price":          ord.Price,
		"quantity":       ord.Quantity,
		"filled":         ord.Filled,
		"status":         string(ord.Status),
		"exchange":       ord.Exchange,
		"user_id":        ord.UserID,
		"client_oid":     ord.ClientOID,
		"avg_fill_price": ord.AvgFillPrice,
		"created_at":     ord.CreatedAt,
		"updated_at":     ord.UpdatedAt,
		"market_type":    string(ord.MarketType),
		"position_side":  string(ord.PositionSide),
		"leverage":       ord.Leverage,
		"margin_mode":    string(ord.MarginMode),
		"close_position": ord.ClosePosition,
	})
}

// Get 查询状态（REST 展示 execution_parts 用）。
func (t *LimitMarketTracker) Get(parentID string) *LMState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.orders[parentID]
}

// Snapshot 全部状态（调试/状态接口）。
func (t *LimitMarketTracker) Snapshot() []*LMState {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*LMState, 0, len(t.orders))
	for _, s := range t.orders {
		out = append(out, s)
	}
	return out
}

func avgOr(avg, fallback float64) float64 {
	if avg > 0 {
		return avg
	}
	return fallback
}

var lmSeq atomic.Int64

func newLMID() string {
	n := lmSeq.Add(1)
	return fmt.Sprintf("%d-%d", time.Now().UnixMilli(), n)
}
