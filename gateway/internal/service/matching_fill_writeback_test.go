package service

import (
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// C2.3：paper LIMIT 挂单（maker）被后续对手单撮合成交后，必须回写 OMS 事实源
// （FILLED + 成交均价/数量），不得继续挂 NEW；重复/乱序 fill 事件幂等收敛。
func TestMatchingFillWritesBackToOMS(t *testing.T) {
	om := order.GetOrderManager()
	ms := GetMatchingService()
	symbol := "FILLWB2USDT"
	t.Cleanup(func() { ms.GetEngine(symbol).Destroy() })

	// 统计该单的 FILLED 级 OnOrderUpdate 触发次数（幂等断言用）。
	var mu sync.Mutex
	filledUpdates := 0
	origUpdate := om.OnOrderUpdate
	om.OnOrderUpdate = func(o *model.OrderData) {
		if o.Symbol == symbol && o.Status == model.StatusFilled {
			mu.Lock()
			filledUpdates++
			mu.Unlock()
		}
	}
	t.Cleanup(func() { om.OnOrderUpdate = origUpdate })

	// 1. OMS 下单（无 pipeline hooks：挂单保持 PENDING），并挂进撮合引擎。
	ord, err := om.PlaceOrder(&order.Request{
		Symbol: symbol, Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 1.0, Exchange: "paper",
	})
	if err != nil {
		t.Fatalf("OMS PlaceOrder: %v", err)
	}
	res, err := ms.PlaceOrder(symbol, "buy", "limit", 100, 1.0, 1, ord.ID)
	if err != nil {
		t.Fatalf("matching PlaceOrder: %v", err)
	}
	if filled, _ := res["filled"].(float64); filled != 0 {
		t.Fatalf("resting order should be unfilled at submit, got %v", filled)
	}
	if got := om.GetOrder(ord.ID); got.Status == model.StatusFilled {
		t.Fatal("order must not be filled before a crossing trade")
	}

	// 2. 对手卖单部分成交 0.4 → maker 应回写 PARTIALLY_FILLED(0.4 @ 100)。
	if _, err := ms.PlaceOrder(symbol, "sell", "limit", 100, 0.4, 2, ""); err != nil {
		t.Fatalf("crossing sell 1: %v", err)
	}
	waitOrderStatus(t, om, ord.ID, model.StatusPartiallyFilled)
	got := om.GetOrder(ord.ID)
	if got.Filled != 0.4 || got.AvgFillPrice != 100 {
		t.Fatalf("partial write-back wrong: filled=%v avg=%v", got.Filled, got.AvgFillPrice)
	}

	// 3. 剩余 0.6 成交 → FILLED(1.0 @ VWAP 100)。
	if _, err := ms.PlaceOrder(symbol, "sell", "limit", 100, 0.6, 2, ""); err != nil {
		t.Fatalf("crossing sell 2: %v", err)
	}
	waitOrderStatus(t, om, ord.ID, model.StatusFilled)
	got = om.GetOrder(ord.ID)
	if got.Filled != 1.0 || got.AvgFillPrice != 100 {
		t.Fatalf("full write-back wrong: filled=%v avg=%v", got.Filled, got.AvgFillPrice)
	}

	// 4. 持久化事实源核对（xt_orders 同口径）。
	rec, err := store.GetOrderRepo().GetByID(ord.ID)
	if err != nil || rec == nil {
		t.Fatalf("repo GetByID: %v", err)
	}
	if rec.Status != string(model.StatusFilled) || rec.Filled != 1.0 || rec.AvgFillPrice != 100 {
		t.Fatalf("persisted record wrong: %+v", rec)
	}

	// 5. 幂等：重复/乱序 fill 事件重放不得回退、不得重复触发 FILLED 更新。
	engineID := res["order_id"].(uint64)
	ms.handleEngineFill(symbol, engineID, 1.0, 100) // 重复最终事件
	ms.handleEngineFill(symbol, engineID, 0.4, 100) // 乱序旧事件
	time.Sleep(200 * time.Millisecond)              // 等异步 goroutine 落定
	got = om.GetOrder(ord.ID)
	if got.Status != model.StatusFilled || got.Filled != 1.0 || got.AvgFillPrice != 100 {
		t.Fatalf("duplicate/stale events must not regress: %+v", got)
	}
	mu.Lock()
	if filledUpdates != 1 {
		t.Fatalf("FILLED OnOrderUpdate must fire exactly once, got %d", filledUpdates)
	}
	mu.Unlock()
}

// 非 OMS 单（内部铸造 mord- 号）与未登记引擎单：fill 事件静默跳过，不报错不炸。
func TestMatchingFillSkipsUnknownOrders(t *testing.T) {
	ms := GetMatchingService()
	symbol := "FILLWB3USDT"
	t.Cleanup(func() { ms.GetEngine(symbol).Destroy() })

	// 双方都是内部铸造号（storeOrderID=""）：成交不应触发任何 OMS 回写。
	if _, err := ms.PlaceOrder(symbol, "buy", "limit", 100, 1.0, 1, ""); err != nil {
		t.Fatalf("resting buy: %v", err)
	}
	res, err := ms.PlaceOrder(symbol, "sell", "limit", 100, 1.0, 2, "")
	if err != nil {
		t.Fatalf("crossing sell: %v", err)
	}
	if filled, _ := res["filled"].(float64); filled != 1.0 {
		t.Fatalf("taker should fill: %v", res)
	}
	// 未登记的引擎单号直接喂回调：必须静默返回。
	ms.handleEngineFill(symbol, 999999, 1.0, 100)
}

func waitOrderStatus(t *testing.T, om *order.OrderManager, id string, want model.OrderStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := om.GetOrder(id); got != nil && got.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := om.GetOrder(id)
	t.Fatalf("order %s did not reach %s (current: %+v)", id, want, got)
}
