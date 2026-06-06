package strategy

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/ws"
)

// Strategy defines the interface all trading strategies must implement.
type Strategy interface {
	Name() string
	Symbol() string
	Params() map[string]any

	// Lifecycle
	Start(params map[string]any) error
	Stop() error
	IsRunning() bool

	// Event handlers — each receives the event bus for signal publishing
	OnTick(evt model.Tick, bus *event.EventBus) (*model.Signal, error)
	OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error)
	OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error)
	OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error)

	// ── Enhanced callbacks (optional, default implementations in BaseStrategy) ──

	// CustomStoploss returns a custom stoploss price for a position.
	// Return 0 to use the default stoploss.
	CustomStoploss(position *Position, currentPrice float64) float64

	// CustomStakeAmount returns a custom stake amount for a trade.
	// Return 0 to use the default position sizing.
	CustomStakeAmount(availableBalance float64, signal *model.Signal) float64

	// ConfirmTradeEntry is called before entering a trade. Return false to skip.
	ConfirmTradeEntry(signal *model.Signal) bool

	// ConfirmTradeExit is called before exiting a position. Return false to skip.
	ConfirmTradeExit(position *Position) bool

	// AdjustEntryPrice allows the strategy to adjust the entry limit price.
	AdjustEntryPrice(signal *model.Signal, orderbook *model.OrderBookData) float64

	// ── Parameters (optional) ──

	// GetParameters returns the strategy's hyperparameter registry (may be nil).
	GetParameters() *ParamRegistry

	// ValidateParams validates all parameter values are within constraints.
	// Returns nil if the strategy has no parameters.
	ValidateParams() error

	// ApplyParams applies parameter values from a map, updating the strategy state.
	// Called automatically by Start() if the strategy has a ParamRegistry.
	ApplyParams(m map[string]any) error

	// ParamDefs returns parameter definitions for frontend rendering.
	// Returns nil if the strategy has no parameters.
	ParamDefs() []map[string]any

	// ── Informative pairs (optional) ──

	// InformativePairs returns additional pairs/timeframes the strategy needs.
	InformativePairs() []InformativePair
}

// Position is a simplified position state for callback decisions.
type Position struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"` // "LONG" or "SHORT"
	EntryPrice    float64 `json:"entry_price"`
	Quantity      float64 `json:"quantity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	StoplossPrice float64 `json:"stoploss_price"`
	OpenTime      int64   `json:"open_time"`
}

// InformativePair declares an additional data feed the strategy needs.
type InformativePair struct {
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	Asset     string `json:"asset,omitempty"` // related asset for correlation
}

// BaseStrategy provides default implementations of optional callbacks.
// Strategies can embed this to only override what they need.
type BaseStrategy struct{}

func (b *BaseStrategy) CustomStoploss(_ *Position, _ float64) float64               { return 0 }
func (b *BaseStrategy) CustomStakeAmount(_ float64, _ *model.Signal) float64        { return 0 }
func (b *BaseStrategy) ConfirmTradeEntry(_ *model.Signal) bool                       { return true }
func (b *BaseStrategy) ConfirmTradeExit(_ *Position) bool                            { return true }
func (b *BaseStrategy) AdjustEntryPrice(_ *model.Signal, _ *model.OrderBookData) float64 { return 0 }
func (b *BaseStrategy) GetParameters() *ParamRegistry                                 { return nil }
func (b *BaseStrategy) InformativePairs() []InformativePair                           { return nil }

// ValidateParams default: no parameters to validate.
func (b *BaseStrategy) ValidateParams() error { return nil }

// ApplyParams default: no parameters to apply.
func (b *BaseStrategy) ApplyParams(_ map[string]any) error { return nil }

// ParamDefs default: no parameters.
func (b *BaseStrategy) ParamDefs() []map[string]any { return nil }

// Engine manages strategy registration, lifecycle, and event dispatch.
type Engine struct {
	strategies map[string]Strategy // name -> strategy
	symbolMap  map[string][]string // symbol -> strategy names
	bus        *event.EventBus
	mu         sync.RWMutex

	OnSignal func(signal model.Signal)

	// Protection manager — checks before emitting signals
	protectionMgr *protection.ProtectionManager

	// Broadcaster sends notifications for trading events
	broadcaster *notify.Broadcaster

	// WSHub broadcasts events to WebSocket clients
	wsHub *ws.Hub

	// DCAManager handles dollar-cost averaging for positions
	dcaManager *order.DCAManager
}

var (
	engineInstance *Engine
	engineOnce     sync.Once
)

// GetEngine returns the global strategy engine.
func GetEngine(bus *event.EventBus) *Engine {
	engineOnce.Do(func() {
		engineInstance = &Engine{
			strategies: make(map[string]Strategy),
			symbolMap:  make(map[string][]string),
			bus:        bus,
		}
	})
	return engineInstance
}

// SetProtectionManager sets the protection manager for the engine.
func (e *Engine) SetProtectionManager(mgr *protection.ProtectionManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.protectionMgr = mgr
}

// Register adds a strategy to the engine and subscribes it to events.
func (e *Engine) Register(s Strategy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := s.Name()
	if _, exists := e.strategies[name]; exists {
		return fmt.Errorf("strategy %s already registered", name)
	}

	e.strategies[name] = s
	e.symbolMap[s.Symbol()] = append(e.symbolMap[s.Symbol()], name)

	// Subscribe to all relevant event types for this strategy's symbol
	e.bus.Subscribe(s.Symbol(), event.PrioNormal, func(evt event.Event) {
		e.dispatch(s, evt)
	}, event.TypeTick, event.TypeOrderBook, event.TypeBar, event.TypeOrderUpdate)

	return nil
}

// Unregister removes a strategy and its subscriptions.
func (e *Engine) Unregister(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	s, ok := e.strategies[name]
	if !ok {
		return fmt.Errorf("strategy %s not found", name)
	}

	symbol := s.Symbol()
	names := e.symbolMap[symbol]
	for i, n := range names {
		if n == name {
			e.symbolMap[symbol] = append(names[:i], names[i+1:]...)
			break
		}
	}
	if len(e.symbolMap[symbol]) == 0 {
		delete(e.symbolMap, symbol)
	}

	s.Stop()
	delete(e.strategies, name)
	return nil
}

// Start starts a registered strategy.
func (e *Engine) Start(name string, params map[string]any) error {
	e.mu.RLock()
	s, ok := e.strategies[name]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("strategy %s not found", name)
	}
	return s.Start(params)
}

// Stop stops a registered strategy.
func (e *Engine) Stop(name string) error {
	e.mu.RLock()
	s, ok := e.strategies[name]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("strategy %s not found", name)
	}
	return s.Stop()
}

// List returns all registered strategy names.
func (e *Engine) List() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.strategies))
	for name := range e.strategies {
		names = append(names, name)
	}
	return names
}

// Get returns a strategy by name.
func (e *Engine) Get(name string) Strategy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.strategies[name]
}

// StrategiesForSymbol returns all strategies watching a symbol.
func (e *Engine) StrategiesForSymbol(symbol string) []Strategy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := e.symbolMap[symbol]
	result := make([]Strategy, 0, len(names))
	for _, name := range names {
		if s, ok := e.strategies[name]; ok {
			result = append(result, s)
		}
	}
	return result
}

func (e *Engine) SetBroadcaster(b *notify.Broadcaster) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.broadcaster = b
}

func (e *Engine) SetWSHub(h *ws.Hub) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.wsHub = h
}

// SetDCAManager sets the DCA manager for the engine.
func (e *Engine) SetDCAManager(mgr *order.DCAManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dcaManager = mgr
}

func (e *Engine) dispatch(s Strategy, evt event.Event) {
	if !s.IsRunning() {
		return
	}

	var signal *model.Signal
	var err error

	switch evt.Type {
	case event.TypeTick:
		if tick, ok := evt.Data.(model.Tick); ok {
			signal, err = s.OnTick(tick, e.bus)
		}
	case event.TypeOrderBook:
		if ob, ok := evt.Data.(model.OrderBookData); ok {
			signal, err = s.OnOrderBook(ob, e.bus)
		}
	case event.TypeBar:
		if bar, ok := evt.Data.(model.Bar); ok {
			signal, err = s.OnBar(bar, e.bus)
		}
	case event.TypeOrderUpdate:
		if order, ok := evt.Data.(model.OrderData); ok {
			signal, err = s.OnOrderUpdate(order, e.bus)
		}
	}

	if err != nil {
		return
	}
	if signal != nil && e.OnSignal != nil {
		// Check DCA for existing positions before emitting signal
		if e.dcaManager != nil && (signal.Direction == "LONG" || signal.Direction == "BUY" || signal.Direction == "long" || signal.Direction == "buy") {
			if dcaPos := e.dcaManager.GetPosition(signal.Symbol); dcaPos != nil && dcaPos.Active {
				// DCA position exists — the strategy's own DCA logic handles add-position signals
				// This hook ensures generic strategies (breakout, grid, etc.) can also use DCA
				// when configured via the DCAConfig parameter.
			}
		}

		// Check protection before emitting signal
		if e.protectionMgr != nil {
			ctx := protection.ProtectionContext{
				Symbol:      signal.Symbol,
				CurrentTime: time.Now(),
			}
			result := e.protectionMgr.CheckAll(ctx)
			if result.Blocked {
				log.Printf("[protection] signal blocked for %s: %s (resume: %v)",
					signal.Symbol, result.Reason, result.ResumeTime)
				// Notify protection trigger
				if e.broadcaster != nil {
					e.broadcaster.Protection("protection", signal.Symbol, "block", result.Reason, 0)
					// WS broadcast protection
					if e.wsHub != nil {
						e.wsHub.BroadcastProtection("protection", signal.Symbol, "block", result.Reason)
					}
				}
				return
			}
		}
		e.OnSignal(*signal)
		// Notify signal
		if e.broadcaster != nil {
			params := s.GetParameters()
			var paramMap map[string]any
			if params != nil {
				paramMap = params.ToMap()
			}
			e.broadcaster.Signal(signal.Symbol, signal.Direction, s.Name(), 0, paramMap)
			// WS broadcast signal
			if e.wsHub != nil {
				e.wsHub.BroadcastSignal(*signal)
			}
		}
	}
}

// PublishSignal publishes a signal to the event bus.
func PublishSignal(bus *event.EventBus, signal model.Signal) {
	bus.Publish(event.Event{
		Type:     event.TypeSignal,
		Symbol:   signal.Symbol,
		Data:     signal,
		Priority: event.PrioHigh,
	})
}

// StopAll stops all registered strategies.
func (e *Engine) StopAll() {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, s := range e.strategies {
		s.Stop()
	}
}
