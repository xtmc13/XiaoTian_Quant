package order

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// persistRestoreRecord 把一张订单写进临时测试库，模拟重启前已持久化的挂单。
func persistRestoreRecord(t *testing.T, rec *store.OrderRecord) {
	t.Helper()
	if err := store.GetOrderRepo().Upsert(rec); err != nil {
		t.Fatalf("persist order %s: %v", rec.ID, err)
	}
}

// TestRestoreActiveOrdersRebuildsPaperOnly 校验 OMS 重启恢复：
//   - paper 中间态挂单（NEW / PARTIALLY_FILLED）回填进内存；
//   - live 交易所挂单不在本地盲目重建；
//   - 已终结订单（FILLED）不回填；
//   - 内存已存在的订单不被覆盖。
func TestRestoreActiveOrdersRebuildsPaperOnly(t *testing.T) {
	now := time.Now().UnixMilli()
	base := func(id, exchange, status string, filled float64) *store.OrderRecord {
		return &store.OrderRecord{
			ID:        id,
			Symbol:    "BTCUSDT",
			Side:      "BUY",
			OrderType: "LIMIT",
			Price:     50000,
			Quantity:  0.1,
			Filled:    filled,
			Status:    status,
			Exchange:  exchange,
			UserID:    7,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	paperNew := base("ord-restore-paper-new", "paper", "NEW", 0)
	paperPartial := base("ord-restore-paper-partial", "paper", "PARTIALLY_FILLED", 0.04)
	liveNew := base("ord-restore-live-new", "binance", "NEW", 0)
	paperFilled := base("ord-restore-paper-filled", "paper", "FILLED", 0.1)
	for _, rec := range []*store.OrderRecord{paperNew, paperPartial, liveNew, paperFilled} {
		persistRestoreRecord(t, rec)
	}

	om := &OrderManager{orders: map[string]*model.OrderData{}}
	n := om.RestoreActiveOrders()

	// 同库可能残留其他恢复用例的 active 单，这里只核验本用例订单的归属，
	// 不要求总数精确。
	if n < 2 {
		t.Fatalf("rebuilt %d paper active orders, want >=2", n)
	}
	if om.GetOrder(paperNew.ID) == nil {
		t.Fatal("paper NEW order was not restored")
	}
	if om.GetOrder(paperPartial.ID) == nil {
		t.Fatal("paper PARTIALLY_FILLED order was not restored")
	}
	if got := om.GetOrder(liveNew.ID); got != nil {
		t.Fatalf("live exchange order must not be rebuilt locally, got %+v", got)
	}
	if got := om.GetOrder(paperFilled.ID); got != nil {
		t.Fatalf("terminal FILLED order must not be rebuilt, got %+v", got)
	}

	// 内存已存在的订单不被恢复覆盖。
	existing := &model.OrderData{ID: "ord-restore-existing", Symbol: "ETHUSDT", Status: model.StatusNew}
	om.orders[existing.ID] = existing
	if n2 := om.RestoreActiveOrders(); n2 != 0 {
		t.Fatalf("second restore should add 0 (existing not overwritten), got %d", n2)
	}
	if om.GetOrder(existing.ID) == nil {
		t.Fatal("pre-existing order was removed/overwritten")
	}
}
