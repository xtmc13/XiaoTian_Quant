package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// C2.2 口径回归：一张 paper LIMIT 单只能有一份事实记录——
// OMS 内存恰 1 张、xt_orders 恰 1 行、legacy 展示 store 无 MATCHING 镜像。
// 修复前：OMS 采用撮合镜像 ID 导致原 ID 的 PENDING 孤儿单残留（OMS/DB 各两条），
// 且镜像以 exchange=MATCHING 回写展示 store。
func TestPaperLimitOrderNotDuplicated(t *testing.T) {
	dir, err := os.MkdirTemp("", "app_dedup_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.CloseDB()
		_ = os.RemoveAll(dir)
	})
	t.Setenv("DB_PATH", filepath.Join(dir, "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	if err := store.InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	ctx := Get()
	ctx.Shutdown()
	if err := ctx.Init(config.Default()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	symbol := "DEDUPUSDT"
	req := &order.Request{
		Symbol: symbol, Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 0.5, Exchange: "paper", UserID: 42,
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	// OMS 内存：该 symbol 活动单恰为一张，且 ID 稳定不被镜像替换。
	open := order.GetOrderManager().GetOpenOrders(symbol)
	if len(open) != 1 {
		ids := make([]string, 0, len(open))
		for _, o := range open {
			ids = append(ids, o.ID+":"+string(o.Status))
		}
		t.Fatalf("OMS 活动单应恰为 1（修复前孤儿 PENDING + 镜像采纳单 = 2）: %v", ids)
	}
	if open[0].ID != ord.ID {
		t.Fatalf("OMS 订单 ID 应保持稳定: open=%s placed=%s", open[0].ID, ord.ID)
	}

	// xt_orders：恰为一行（修复前孤儿 PENDING 行 + 镜像采纳行 = 2 行）。
	recs, err := store.GetOrderRepo().List(map[string]any{"symbol": symbol}, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("xt_orders 应恰为 1 行, got %d: %+v", len(recs), recs)
	}

	// legacy 展示 store：无 MATCHING 镜像残留。
	for _, o := range store.GetOrders(symbol) {
		if o["exchange"] == "MATCHING" {
			t.Fatalf("撮合镜像不得回写展示层: %+v", o)
		}
	}
}
