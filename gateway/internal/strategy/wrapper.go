package strategy

// NamedStrategy wraps a Strategy and overrides its Name() with a custom ID.
// This allows multiple configurations of the same strategy type to coexist
// in the engine, each identified by their unique config ID.
type NamedStrategy struct {
	Strategy
	id string
}

// Name returns the configured ID instead of the underlying strategy's fixed name.
func (n *NamedStrategy) Name() string { return n.id }

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
