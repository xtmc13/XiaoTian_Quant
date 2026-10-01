package strategy

import (
	"strings"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// NamedStrategy wraps a Strategy and overrides its Name() with a custom ID.
// This allows multiple configurations of the same strategy type to coexist
// in the engine, each identified by their unique config ID.
type NamedStrategy struct {
	Strategy
	id string
}

// Name returns the configured ID instead of the underlying strategy's fixed name.
func (n *NamedStrategy) Name() string { return n.id }

// OnOrderUpdate 按订单归属过滤后透传：signal 链路打标 client_oid
// "sig:<配置id>:<nonce>"，只把本策略的订单更新（或未打标的存量/手工单，
// 保持旧行为）交给内层策略——防止同交易对其他策略的拒单/成交误触发
// 本策略状态机回滚或改价（2026-10-01 虚持仓修复的精确性前提）。
func (n *NamedStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	if oid := order.ClientOID; oid != "" && strings.HasPrefix(oid, "sig:") &&
		!strings.HasPrefix(oid, "sig:"+n.id+":") {
		return nil, nil
	}
	return n.Strategy.OnOrderUpdate(order, bus)
}

// RestorePosition 透传 PositionRestorer：内层策略实现时生效（重启仓位重建）。
func (n *NamedStrategy) RestorePosition(qty, avgPrice float64) error {
	if pr, ok := n.Strategy.(PositionRestorer); ok {
		return pr.RestorePosition(qty, avgPrice)
	}
	return nil
}

// Unwrap 返回被包装的真实策略实例。引擎注册进 map 的是 NamedStrategy，
// 可选接口（Timeframer/PrimaryTimeframer/ScheduleProvider/UniverseProvider/
// BarProviderSetter 等）不会穿透包装层的方法提升——所有接口断言前必须先解包。
func (n *NamedStrategy) Unwrap() Strategy { return n.Strategy }

// UnwrapStrategy 解包 NamedStrategy；非包装实例原样返回。
func UnwrapStrategy(s Strategy) Strategy {
	if ns, ok := s.(*NamedStrategy); ok {
		return ns.Strategy
	}
	return s
}

// WrapStrategy creates a NamedStrategy with the given ID.
func WrapStrategy(id string, s Strategy) *NamedStrategy {
	return &NamedStrategy{Strategy: s, id: id}
}
