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

// WrapStrategy creates a NamedStrategy with the given ID.
func WrapStrategy(id string, s Strategy) *NamedStrategy {
	return &NamedStrategy{Strategy: s, id: id}
}
