package order

import (
	"sync"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// RecordFill 撮合回写（C2.3）：挂单成交后 OMS 状态/均价/数量翻转并持久化；
// 重复/乱序事件幂等收敛不回退；终态订单不再回写。
func TestRecordFillWritesBackAndIsIdempotent(t *testing.T) {
	om := GetOrderManager()
	ord, err := om.PlaceOrder(&Request{
		Symbol: "FILLWBUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 1.0, Exchange: "paper",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	// 首次部分成交：0.4 @ 100 → PARTIALLY_FILLED
	if err := om.RecordFill(ord.ID, 0.4, 100); err != nil {
		t.Fatalf("RecordFill partial: %v", err)
	}
	got := om.GetOrder(ord.ID)
	if got.Status != model.StatusPartiallyFilled || got.Filled != 0.4 || got.AvgFillPrice != 100 {
		t.Fatalf("partial fill wrong: status=%s filled=%v avg=%v", got.Status, got.Filled, got.AvgFillPrice)
	}

	// 重复同一累计事件：状态不变（幂等）
	if err := om.RecordFill(ord.ID, 0.4, 100); err != nil {
		t.Fatalf("RecordFill duplicate: %v", err)
	}
	if got := om.GetOrder(ord.ID); got.Status != model.StatusPartiallyFilled || got.Filled != 0.4 {
		t.Fatalf("duplicate event must be a no-op: status=%s filled=%v", got.Status, got.Filled)
	}

	// 全量成交：1.0 @ VWAP 99 → FILLED
	if err := om.RecordFill(ord.ID, 1.0, 99); err != nil {
		t.Fatalf("RecordFill full: %v", err)
	}
	got = om.GetOrder(ord.ID)
	if got.Status != model.StatusFilled || got.Filled != 1.0 || got.AvgFillPrice != 99 {
		t.Fatalf("full fill wrong: status=%s filled=%v avg=%v", got.Status, got.Filled, got.AvgFillPrice)
	}

	// 持久化核对：xt_orders 里也是 FILLED + 均价/数量
	rec, err := store.GetOrderRepo().GetByID(ord.ID)
	if err != nil || rec == nil {
		t.Fatalf("repo GetByID: %v", err)
	}
	if rec.Status != string(model.StatusFilled) || rec.Filled != 1.0 || rec.AvgFillPrice != 99 {
		t.Fatalf("persisted record wrong: %+v", rec)
	}

	// 乱序旧事件（更小的累计量）不得回退终态
	if err := om.RecordFill(ord.ID, 0.4, 100); err != nil {
		t.Fatalf("RecordFill stale: %v", err)
	}
	if got := om.GetOrder(ord.ID); got.Status != model.StatusFilled || got.Filled != 1.0 || got.AvgFillPrice != 99 {
		t.Fatalf("stale event must not regress: status=%s filled=%v avg=%v", got.Status, got.Filled, got.AvgFillPrice)
	}

	// 重复 FILLED 事件：no-op
	if err := om.RecordFill(ord.ID, 1.0, 99); err != nil {
		t.Fatalf("RecordFill repeat final: %v", err)
	}
	if got := om.GetOrder(ord.ID); got.Status != model.StatusFilled {
		t.Fatalf("repeat final must stay FILLED: %s", got.Status)
	}
}

// RecordFill 对已撤/拒/过期订单不回写（撤单与成交竞态：撤单赢）。
func TestRecordFillSkipsTerminalOrders(t *testing.T) {
	om := GetOrderManager()
	ord, err := om.PlaceOrder(&Request{
		Symbol: "FILLWBUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 1.0, Exchange: "paper",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	if _, err := om.CancelOrder(ord.ID, ord.Symbol); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if err := om.RecordFill(ord.ID, 1.0, 100); err != nil {
		t.Fatalf("RecordFill on cancelled: %v", err)
	}
	if got := om.GetOrder(ord.ID); got.Status != model.StatusCancelled || got.Filled != 0 {
		t.Fatalf("cancelled order must not be filled: status=%s filled=%v", got.Status, got.Filled)
	}

	if err := om.RecordFill("ord-not-exist", 1.0, 100); err == nil {
		t.Fatal("unknown order must error")
	}
	if err := om.RecordFill(ord.ID, 0, 100); err == nil {
		t.Fatal("non-positive qty must error")
	}
}

func TestConcurrentRecordFillAndStaleOrderWriteDoNotRegress(t *testing.T) {
	om := GetOrderManager()
	ord, err := om.PlaceOrder(&Request{
		Symbol: "FILLRACEUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 1, Exchange: "paper",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	om.orderMu.RLock()
	stale := *om.orders[ord.ID]
	om.orderMu.RUnlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := om.RecordFill(ord.ID, 0.4, 100); err != nil {
			t.Errorf("partial RecordFill: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := om.RecordFill(ord.ID, 1, 101); err != nil {
			t.Errorf("final RecordFill: %v", err)
		}
	}()
	wg.Wait()

	stale.Status = model.StatusNew
	om.storeOrder(&stale)
	got := om.GetOrder(ord.ID)
	if got.Status != model.StatusFilled || got.Filled != 1 || got.AvgFillPrice != 101 {
		t.Fatalf("stale update regressed concurrent fills: status=%s filled=%v avg=%v", got.Status, got.Filled, got.AvgFillPrice)
	}
}

func TestCancelPreservesFillArrivingDuringExchangeRequest(t *testing.T) {
	om := GetOrderManager()
	ord, err := om.PlaceOrder(&Request{
		Symbol: "FILLCANCELRACEUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 1, Exchange: "paper",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	originalCancel := om.CancelOnExchange
	defer func() { om.CancelOnExchange = originalCancel }()
	cancelEntered := make(chan struct{})
	continueCancel := make(chan struct{})
	om.CancelOnExchange = func(*model.OrderData) error {
		close(cancelEntered)
		<-continueCancel
		return nil
	}
	cancelResult := make(chan error, 1)
	go func() {
		_, err := om.CancelOrder(ord.ID, ord.Symbol)
		cancelResult <- err
	}()

	<-cancelEntered
	fillErr := om.RecordFill(ord.ID, 0.4, 100)
	close(continueCancel)
	if fillErr != nil {
		t.Fatalf("RecordFill during exchange cancel: %v", fillErr)
	}
	if err := <-cancelResult; err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	got := om.GetOrder(ord.ID)
	if got.Status != model.StatusCancelled || got.Filled != 0.4 || got.AvgFillPrice != 100 {
		t.Fatalf("cancel/fill race lost accumulated fill: status=%s filled=%v avg=%v", got.Status, got.Filled, got.AvgFillPrice)
	}
}
