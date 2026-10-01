package order

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// 实盘 ACK 的 orderId 是 JSON 数字（float64），旧代码 .(string) 断言直接
// 丢弃 → reconcile 拿本地 ord-* ID 永远查不到交易所订单。修复后
// finalizeLiveSubmitResult 把它规整为字符串放进 exchange_order_id，
// OMS 必须捕获并持久化到 xt_orders.exchange_order_id。
func TestPlaceOrderCapturesExchangeOrderID(t *testing.T) {
	om := GetOrderManager()
	orig := om.SubmitToExchange
	t.Cleanup(func() { om.SubmitToExchange = orig })
	om.SubmitToExchange = func(order *model.OrderData) (map[string]any, error) {
		return map[string]any{
			"order_id":          float64(671122224734), // Binance ACK: JSON number
			"exchange_order_id": "671122224734",
			"status":            "NEW",
			"filled":            0.0,
			"exchange":          "binance",
		}, nil
	}

	ord, err := om.PlaceOrder(&Request{
		Symbol: "EXCHIDUSDT", Side: model.SideBuy, OrderType: model.TypeMarket,
		Price: 0, Quantity: 0.001, Exchange: "binance",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if ord.ExchangeOrderID != "671122224734" {
		t.Fatalf("内存态未捕获 exchange_order_id: %q", ord.ExchangeOrderID)
	}
	if ord.Status != model.StatusNew {
		t.Fatalf("status=%s 期望 NEW", ord.Status)
	}

	rec, err := store.GetOrderRepo().GetByID(ord.ID)
	if err != nil || rec == nil {
		t.Fatalf("repo GetByID: %v", err)
	}
	if rec.ExchangeOrderID != "671122224734" {
		t.Fatalf("落库缺少 exchange_order_id: %+v", rec)
	}
	// 本地 ID 必须保持 ord-* 形态（不被交易所 ID 覆盖）
	if rec.ID != ord.ID || len(rec.ID) < 4 || rec.ID[:4] != "ord-" {
		t.Fatalf("本地订单号被覆盖: %q", rec.ID)
	}
}
