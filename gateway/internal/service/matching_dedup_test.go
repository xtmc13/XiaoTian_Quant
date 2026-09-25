package service

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// C2.2 口径回归：OMS 是订单唯一事实源，撮合引擎镜像不得回写展示层
// （legacy store）。镜像回写会让一张 paper LIMIT 单在订单列表出现两条
// （OMS 一条 + MATCHING 镜像一条）。
func TestPlaceOrderDoesNotMirrorIntoDisplayStore(t *testing.T) {
	ms := GetMatchingService()
	symbol := "MIRRORDEDUP"
	t.Cleanup(func() { ms.GetEngine(symbol).Destroy() })

	before := len(store.GetOrders(symbol))
	result, err := ms.PlaceOrder(symbol, "buy", "limit", 100.0, 1.0, 42, "")
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if got := len(store.GetOrders(symbol)); got != before {
		t.Fatalf("撮合镜像不得写入展示 store: before=%d after=%d", before, got)
	}

	// 撮合功能不受去重影响：引擎仍持有挂单。
	snap, err := ms.GetOrderBook(symbol, 10)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}
	if bids := snap["bids"].([][]float64); len(bids) != 1 {
		t.Fatalf("引擎应仍持有挂单: %+v", snap)
	}

	// 撤单链路仍工作：按 store_order_id 撤掉引擎单。
	storeOrderID, _ := result["store_order_id"].(string)
	if storeOrderID == "" {
		t.Fatal("expected store_order_id in result")
	}
	if err := ms.CancelOrder(symbol, storeOrderID); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	snap2, _ := ms.GetOrderBook(symbol, 10)
	if bids := snap2["bids"].([][]float64); len(bids) != 0 {
		t.Fatalf("撤单后引擎盘口应为空: %+v", snap2)
	}
}

// 重启后内部登记簿丢失：撤销一张引擎/内存里都没有、只在 DB 里的 paper 单
// 不得硬报错（ltm 重启恢复路径依赖：撤单成功才能进入市价补单）。
func TestCancelOrderToleratesUnknownEngineOrder(t *testing.T) {
	ms := GetMatchingService()
	symbol := "MIRRORDEDUP"
	// 只写 xt_orders（模拟重启前 OMS 持久化的记录），legacy 内存与引擎都没有它。
	recID := "restart-only-ltm-order"
	if err := store.GetOrderRepo().Create(&store.OrderRecord{
		ID: recID, Symbol: symbol, Side: "BUY", OrderType: "LIMIT",
		Price: 100.0, Quantity: 1.0, Status: "NEW", Exchange: "paper",
	}); err != nil {
		t.Fatalf("seed db order: %v", err)
	}
	if err := ms.CancelOrder(symbol, recID); err != nil {
		t.Fatalf("重启后撤销引擎外订单不得报错: %v", err)
	}
	var status string
	row := store.GetDB().QueryRow(`SELECT status FROM xt_orders WHERE id=?`, recID)
	if err := row.Scan(&status); err != nil || status != "CANCELLED" {
		t.Fatalf("DB 订单应置 CANCELLED: status=%q err=%v", status, err)
	}
}
