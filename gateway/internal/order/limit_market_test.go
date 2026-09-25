package order

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// fakeLMPlacer 模拟交易所：限价单挂出后按脚本成交；市价单即时成交。
type fakeLMPlacer struct {
	mu           sync.Mutex
	orders       map[string]*model.OrderData
	seq          int
	limitFilled  float64 // 限价单已成交量（模拟异步成交）
	limitAvg     float64
	cancelErr    error
	marketReject bool
}

func newFakeLMPlacer() *fakeLMPlacer {
	return &fakeLMPlacer{orders: map[string]*model.OrderData{}, limitAvg: 50000}
}

func (f *fakeLMPlacer) PlaceOrder(req *Request) (*model.OrderData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	ord := &model.OrderData{
		ID:        fmt.Sprintf("ord-%d", f.seq),
		Symbol:    req.Symbol,
		Side:      req.Side,
		OrderType: req.OrderType,
		Price:     req.Price,
		Quantity:  req.Quantity,
		Exchange:  req.Exchange,
		UserID:    req.UserID,
		ClientOID: req.ClientOID,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}
	if req.OrderType == model.TypeMarket {
		if f.marketReject {
			ord.Status = model.StatusRejected
			return ord, fmt.Errorf("market order rejected")
		}
		ord.Status = model.StatusFilled
		ord.Filled = req.Quantity
		ord.AvgFillPrice = 50100
	} else {
		ord.Status = model.StatusNew
		ord.Filled = f.limitFilled
		if f.limitFilled > 0 {
			ord.AvgFillPrice = f.limitAvg
			if f.limitFilled >= req.Quantity {
				ord.Status = model.StatusFilled
			} else {
				ord.Status = model.StatusPartiallyFilled
			}
		}
	}
	f.orders[ord.ID] = ord
	return ord, nil
}

func (f *fakeLMPlacer) CancelOrder(orderID, symbol string) (*model.OrderData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ord, ok := f.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("order not found")
	}
	if f.cancelErr != nil {
		return nil, f.cancelErr
	}
	if ord.Status == model.StatusCancelled {
		return nil, fmt.Errorf("order already cancelled")
	}
	ord.Status = model.StatusCancelled
	ord.UpdatedAt = time.Now().UnixMilli()
	return ord, nil
}

func (f *fakeLMPlacer) GetOrder(orderID string) *model.OrderData {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.orders[orderID]
}

// HandleOrderUpdate 模拟 OMS 的内存回填接缝：重启恢复时把 DB 里的活动订单
// 重新放进内存，否则恢复后的撤单/查单全部 miss。
func (f *fakeLMPlacer) HandleOrderUpdate(o *model.OrderData) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *o
	f.orders[o.ID] = &cp
}

func (f *fakeLMPlacer) marketOrders() []*model.OrderData {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.OrderData
	for _, o := range f.orders {
		if o.OrderType == model.TypeMarket {
			out = append(out, o)
		}
	}
	return out
}

func TestLimitThenMarketTimeoutTriggersFallback(t *testing.T) {
	placer := newFakeLMPlacer()
	tr := NewLimitMarketTracker(placer)
	tr.Start()
	defer tr.Stop()

	req := &Request{
		Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 50000, Quantity: 1.0, Exchange: "paper", UserID: 1,
	}
	ord, state, err := tr.PlaceLimitThenMarket(req, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if state.Status != "active" {
		t.Fatalf("未全成交应进入 active: %+v", state)
	}

	// 等 watcher 超时撤单 + 市价补单。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := tr.Get(ord.ID)
		if s != nil && s.IsTerminal() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s := tr.Get(ord.ID)
	if s == nil || s.Status != "completed" {
		t.Fatalf("超时后应 completed: %+v", s)
	}
	if s.MarketOrderID == "" {
		t.Fatalf("应有市价补单: %+v", s)
	}

	mkts := placer.marketOrders()
	if len(mkts) != 1 {
		t.Fatalf("应恰好一笔市价补单: %+v", mkts)
	}
	if mkts[0].Quantity != 1.0 {
		t.Fatalf("补单数量应等于剩余 1.0: %+v", mkts[0])
	}
	// 成交明细区分两段。
	parts := s.Parts()
	if len(parts) != 2 || parts[0].Phase != "limit" || parts[1].Phase != "market" {
		t.Fatalf("execution parts 应含 limit+market 两段: %+v", parts)
	}
	if parts[1].Filled != 1.0 {
		t.Fatalf("market 段成交量异常: %+v", parts[1])
	}
	// 补单 client_oid 与父单联动。
	if mkts[0].ClientOID != "ltm-mkt:"+ord.ID {
		t.Fatalf("补单 client_oid 应回链父单: %s", mkts[0].ClientOID)
	}
}

func TestLimitThenMarketFullFillNoFallback(t *testing.T) {
	placer := newFakeLMPlacer()
	placer.limitFilled = 1.0 // 限价单立即全成交
	tr := NewLimitMarketTracker(placer)
	tr.Start()
	defer tr.Stop()

	req := &Request{
		Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 50000, Quantity: 1.0, Exchange: "paper", UserID: 1,
	}
	ord, state, err := tr.PlaceLimitThenMarket(req, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if state.Status != "completed" {
		t.Fatalf("立即全成交应直接 completed: %+v", state)
	}
	time.Sleep(500 * time.Millisecond) // 超过超时时间也不应补单
	if mkts := placer.marketOrders(); len(mkts) != 0 {
		t.Fatalf("全成交不得触发市价补单: %+v", mkts)
	}
	_ = ord
}

func TestLimitThenMarketPartialFillFallbackQty(t *testing.T) {
	placer := newFakeLMPlacer()
	placer.limitFilled = 0.4 // 限价段先成交 0.4
	tr := NewLimitMarketTracker(placer)
	tr.Start()
	defer tr.Stop()

	req := &Request{
		Symbol: "BTCUSDT", Side: model.SideSell, OrderType: model.TypeLimit,
		Price: 50000, Quantity: 1.0, Exchange: "paper", UserID: 1,
	}
	ord, _, err := tr.PlaceLimitThenMarket(req, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("place: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := tr.Get(ord.ID)
		if s != nil && s.IsTerminal() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s := tr.Get(ord.ID)
	if s == nil || s.Status != "completed" {
		t.Fatalf("应 completed: %+v", s)
	}
	if s.LimitFilled != 0.4 {
		t.Fatalf("限价段成交量应保留: %+v", s)
	}
	mkts := placer.marketOrders()
	if len(mkts) != 1 || mkts[0].Quantity != 0.6 {
		t.Fatalf("补单应只补剩余 0.6: %+v", mkts)
	}
}

func TestLimitThenMarketCancelFailure(t *testing.T) {
	placer := newFakeLMPlacer()
	placer.cancelErr = fmt.Errorf("exchange reject cancel")
	tr := NewLimitMarketTracker(placer)
	tr.Start()
	defer tr.Stop()

	var notified *LMState
	tr.SetNotifyHook(func(s *LMState, msg string) { notified = s })

	req := &Request{
		Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 50000, Quantity: 1.0, Exchange: "live", UserID: 1,
	}
	ord, _, err := tr.PlaceLimitThenMarket(req, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("place: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := tr.Get(ord.ID)
		if s != nil && s.IsTerminal() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s := tr.Get(ord.ID)
	if s == nil || s.Status != "cancel_failed" {
		t.Fatalf("撤单失败应置 cancel_failed: %+v", s)
	}
	if notified == nil {
		t.Fatal("撤单失败必须触发告警回调")
	}
	// 撤单失败不得补单（避免重复敞口）。
	if mkts := placer.marketOrders(); len(mkts) != 0 {
		t.Fatalf("撤单失败不得触发市价补单: %+v", mkts)
	}
}

func TestLimitThenMarketMarketFailure(t *testing.T) {
	placer := newFakeLMPlacer()
	placer.marketReject = true
	tr := NewLimitMarketTracker(placer)
	tr.Start()
	defer tr.Stop()

	req := &Request{
		Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 50000, Quantity: 1.0, Exchange: "live", UserID: 1,
	}
	ord, _, err := tr.PlaceLimitThenMarket(req, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := tr.Get(ord.ID)
		if s != nil && s.IsTerminal() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s := tr.Get(ord.ID)
	if s == nil || s.Status != "market_failed" {
		t.Fatalf("补单失败应置 market_failed: %+v", s)
	}
}

func TestLimitThenMarketValidation(t *testing.T) {
	tr := NewLimitMarketTracker(newFakeLMPlacer())
	req := &Request{Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeMarket, Quantity: 1}
	if _, _, err := tr.PlaceLimitThenMarket(req, time.Second); err == nil {
		t.Fatal("市价单不允许 limit-then-market")
	}
	req2 := &Request{Symbol: "BTCUSDT", Side: model.SideBuy, OrderType: model.TypeLimit, Price: 1, Quantity: 1}
	if _, _, err := tr.PlaceLimitThenMarket(req2, 0); err == nil {
		t.Fatal("timeout 必须为正")
	}
}
