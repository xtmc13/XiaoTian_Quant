package exchange

import (
	"fmt"
	"sync"
)

// ExchangeRegistry manages multiple exchange instances.
type ExchangeRegistry struct {
	exchanges map[string]Exchange
	mu        sync.RWMutex
}

var (
	registry     *ExchangeRegistry
	registryOnce sync.Once
)

// GetRegistry returns the global exchange registry singleton.
func GetRegistry() *ExchangeRegistry {
	registryOnce.Do(func() {
		registry = &ExchangeRegistry{
			exchanges: make(map[string]Exchange),
		}
	})
	return registry
}

// Register adds an exchange to the registry.
func (r *ExchangeRegistry) Register(ex Exchange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := ex.Name()
	if _, exists := r.exchanges[name]; exists {
		return fmt.Errorf("exchange %s already registered", name)
	}
	r.exchanges[name] = ex
	return nil
}

// Unregister removes an exchange.
func (r *ExchangeRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ex, ok := r.exchanges[name]; ok {
		ex.Stop()
		delete(r.exchanges, name)
	}
}

// Get returns an exchange by name.
func (r *ExchangeRegistry) Get(name string) Exchange {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.exchanges[name]
}

// List returns all registered exchange names.
func (r *ExchangeRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.exchanges))
	for name := range r.exchanges {
		names = append(names, name)
	}
	return names
}

// PlaceOrder routes an order to the specified exchange.
func (r *ExchangeRegistry) PlaceOrder(exchangeName, symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	ex := r.Get(exchangeName)
	if ex == nil {
		return nil, fmt.Errorf("exchange %s not found", exchangeName)
	}
	return ex.PlaceOrder(symbol, side, orderType, price, quantity)
}

// CancelOrder routes a cancel to the specified exchange.
func (r *ExchangeRegistry) CancelOrder(exchangeName, symbol, orderID string) (map[string]any, error) {
	ex := r.Get(exchangeName)
	if ex == nil {
		return nil, fmt.Errorf("exchange %s not found", exchangeName)
	}
	return ex.CancelOrder(symbol, orderID)
}

// GetBalance fetches balance from an exchange.
func (r *ExchangeRegistry) GetBalance(exchangeName string) ([]map[string]any, error) {
	ex := r.Get(exchangeName)
	if ex == nil {
		return nil, fmt.Errorf("exchange %s not found", exchangeName)
	}
	return ex.GetBalance()
}

// StopAll stops all registered exchanges.
func (r *ExchangeRegistry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, ex := range r.exchanges {
		ex.Stop()
		delete(r.exchanges, name)
	}
}
