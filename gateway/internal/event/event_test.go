package event

import (
	"sync"
	"testing"
	"time"
)

// 回归测试：2026-09-16 真实死锁。dispatch 在持有 bus RLock 的情况下调用
// handler，handler 里（合法地）再 Publish（信号→下单→order-update），
// 与并发的 Subscribe（启动策略）撞车即成环：
//
//	K线供给管: dispatch[持RLock] → 信号 → Publish[等RLock，被写者挡住]
//	启动策略:  Register[持Engine锁] → Subscribe[等bus写锁，等RLock释放]
//
// Go RWMutex 不可重入，二者互相等待。修复 = dispatch 快照订阅后放锁再调 handler。
func TestReentrantPublishWithConcurrentSubscribeNoDeadlock(t *testing.T) {
	bus := NewEventBus(4096, 4)
	defer bus.Close()

	// 模拟策略回调：收到 tick 后在 dispatch 上下文里再 Publish（重入）。
	bus.Subscribe("BTCUSDT", PrioNormal, func(evt Event) {
		if evt.Type == TypeTick {
			bus.Publish(Event{Type: TypeOrderUpdate, Symbol: "BTCUSDT"})
		}
	}, TypeTick, TypeOrderUpdate)

	stop := make(chan struct{})
	var churn sync.WaitGroup
	churn.Add(1)
	go func() {
		defer churn.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			id := bus.Subscribe("BTCUSDT", PrioLow, func(Event) {}, TypeTick)
			bus.Unsubscribe(id)
		}
	}()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			bus.PublishSync(Event{Type: TypeTick, Symbol: "BTCUSDT"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: re-entrant publish with concurrent subscribe/Unsubscribe")
	}
	close(stop)
	churn.Wait()
}
