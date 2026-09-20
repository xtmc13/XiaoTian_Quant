package pystrat

import (
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ClientOIDPrefix 是 pystrat 订单 client_oid 前缀（runner 注释契约：
// "pystrat:<id>"，生产由 handler.NewKindOMSBotExecutor("pystrat") 打进订单）。
const ClientOIDPrefix = "pystrat:"

// OrderSource 是策略自有订单回报订阅源（v1.1 on_order 落地）。生产接事件总线
// TypeOrderUpdate（OMS OnOrderUpdate 钩子发布，见 app.Context.wireOrderManager），
// 按 client_oid 前缀 "pystrat:<botID>" 过滤后转成 OrderEvent；测试用 channel fake。
type OrderSource interface {
	// Subscribe 订阅 botID 的订单回报；返回接收 channel 与取消函数。
	Subscribe(botID, symbol string) (<-chan OrderEvent, func(), error)
}

// EventOrderSource 生产用 OrderSource：事件总线 TypeOrderUpdate → client_oid
// 前缀过滤 → OrderEvent。与 EventBarSource 同一模式（非阻塞发送，满则丢新，
// 不阻塞总线 worker；订单事件容忍度高，丢单仅影响 on_order 触发）。
type EventOrderSource struct {
	Bus *event.EventBus
}

// compile-time check: EventOrderSource 实现 OrderSource。
var _ OrderSource = (*EventOrderSource)(nil)

// Subscribe 见 OrderSource 接口。
func (s *EventOrderSource) Subscribe(botID, symbol string) (<-chan OrderEvent, func(), error) {
	if s.Bus == nil {
		return nil, nil, fmt.Errorf("pystrat: EventOrderSource not fully configured")
	}
	symbol = normalizeSymbol(symbol)
	prefix := ClientOIDPrefix + botID
	ch := make(chan OrderEvent, 256)
	subID := s.Bus.Subscribe(symbol, event.PrioNormal, func(evt event.Event) {
		ord, ok := evt.Data.(model.OrderData)
		if !ok {
			return
		}
		if !strings.HasPrefix(ord.ClientOID, prefix) {
			return
		}
		select {
		case ch <- OrderEvent{
			ID:        ord.ID,
			Symbol:    ord.Symbol,
			Side:      strings.ToLower(string(ord.Side)),
			Type:      strings.ToLower(string(ord.OrderType)),
			Qty:       ord.Quantity,
			Price:     ord.Price,
			Filled:    ord.Filled,
			AvgPrice:  ord.AvgFillPrice,
			Status:    strings.ToLower(string(ord.Status)),
			PnL:       ord.RealizedPnL,
			ClientOID: ord.ClientOID,
		}:
		default: // 策略处理慢于回报时丢订单事件，不阻塞总线 worker
		}
	}, event.TypeOrderUpdate)
	unsubscribe := func() { s.Bus.Unsubscribe(subID) }
	return ch, unsubscribe, nil
}
