package strategy

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/metrics"
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

func (b *BaseStrategy) CustomStoploss(_ *Position, _ float64) float64                    { return 0 }
func (b *BaseStrategy) CustomStakeAmount(_ float64, _ *model.Signal) float64             { return 0 }
func (b *BaseStrategy) ConfirmTradeEntry(_ *model.Signal) bool                           { return true }
func (b *BaseStrategy) ConfirmTradeExit(_ *Position) bool                                { return true }
func (b *BaseStrategy) AdjustEntryPrice(_ *model.Signal, _ *model.OrderBookData) float64 { return 0 }
func (b *BaseStrategy) GetParameters() *ParamRegistry                                    { return nil }
func (b *BaseStrategy) InformativePairs() []InformativePair                              { return nil }

// ValidateParams default: no parameters to validate.
func (b *BaseStrategy) ValidateParams() error { return nil }

// ApplyParams default: no parameters to apply.
func (b *BaseStrategy) ApplyParams(_ map[string]any) error { return nil }

// ParamDefs default: no parameters.
func (b *BaseStrategy) ParamDefs() []map[string]any { return nil }

// Engine manages strategy registration, lifecycle, and event dispatch.
type Engine struct {
	strategies map[string]Strategy             // name -> strategy
	symbolMap  map[string][]string             // symbol -> strategy names
	subIDs     map[string]event.SubscriptionID // strategy name -> bus subscription
	bus        *event.EventBus
	mu         sync.RWMutex

	OnSignal func(signal model.Signal)

	// Protection manager — checks before emitting signals
	protectionMgr *protection.ProtectionManager

	// strategyProtections 策略级 protection manager（key=包装后策略名=配置 id），
	// 来自策略 config_json["protections"]（hyperopt epoch 回写，与全局
	// /api/protection/config 同 schema）。emitSignal 先过策略级再过引擎全局。
	// 由 handler 在策略启动/重载时注入，Unregister 时释放。
	strategyProtections map[string]*protection.ProtectionManager

	// Broadcaster sends notifications for trading events
	broadcaster *notify.Broadcaster

	// WSHub broadcasts events to WebSocket clients
	wsHub *ws.Hub

	// DCAManager handles dollar-cost averaging for positions
	dcaManager *order.DCAManager

	// ── A7 扩展（engine_ext.go） ──
	// feeder 供多周期/universe 动态订阅 K 线（引用计数，与 handler 侧独立记账）
	feeder     SymbolFeeder
	marketData *MarketData
	feedHolds  map[string][]feedHold

	uniMu     sync.Mutex
	universes map[string]*universeState

	scheduled map[string]*scheduledEntry
	schedMu   sync.Mutex
	schedOnce sync.Once

	// ── v1.2 契约钩子（hooks.go）──
	// positionLookup/balanceLookup 由 app 层注入持仓与余额视图，
	// 供 ConfirmTradeExit/AdjustTradePosition/CustomStakeAmount 决策。
	positionLookup func(strategyName, symbol string) *Position
	balanceLookup  func() float64
	// adjCounts 记录每策略当前持仓的加仓次数（持仓归零时清零，
	// MaxPositionAdjustmentsProvider 的上限按此判定；减仓不计入）。
	adjCounts map[string]int
}

var (
	engineInstance *Engine
	engineOnce     sync.Once
)

// GetEngine returns the global strategy engine.
func GetEngine(bus *event.EventBus) *Engine {
	engineOnce.Do(func() {
		engineInstance = &Engine{
			strategies:          make(map[string]Strategy),
			symbolMap:           make(map[string][]string),
			subIDs:              make(map[string]event.SubscriptionID),
			bus:                 bus,
			feedHolds:           make(map[string][]feedHold),
			universes:           make(map[string]*universeState),
			scheduled:           make(map[string]*scheduledEntry),
			adjCounts:           make(map[string]int),
			strategyProtections: make(map[string]*protection.ProtectionManager),
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

// SetStrategyProtectionManager 注入/替换某策略的策略级 protection manager；
// mgr 为 nil 时清除（策略 config_json 不再含 protections 的重载场景）。
func (e *Engine) SetStrategyProtectionManager(name string, mgr *protection.ProtectionManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.strategyProtections == nil { // 防御：struct 字面量构造的引擎（测试）
		e.strategyProtections = make(map[string]*protection.ProtectionManager)
	}
	if mgr == nil {
		delete(e.strategyProtections, name)
		return
	}
	e.strategyProtections[name] = mgr
}

// StrategyProtectionCount 策略级 protection 数量（状态/测试观测用；无 manager 返回 0）。
func (e *Engine) StrategyProtectionCount(name string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	mgr := e.strategyProtections[name]
	if mgr == nil {
		return 0
	}
	return len(mgr.Protections())
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

	// Subscribe to all relevant event types for this strategy's symbol.
	// 订阅回调带 recover：单个策略/信号处理 panic 不得拖垮整个事件总线
	// 与网关进程（真实崩溃案例：信号下单 nil deref 在回补重放时炸掉主进程）。
	// 订阅 id 必须登记，Unregister 时退订——否则停止的策略会变成僵尸订阅，
	// 继续白收事件（每次重启策略累积一个）。
	e.subIDs[name] = e.bus.Subscribe(s.Symbol(), event.PrioNormal, func(evt event.Event) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[StrategyEngine] panic in strategy %s dispatch (recovered): %v", s.Name(), r)
			}
		}()
		e.dispatch(s, evt)
	}, event.TypeTick, event.TypeOrderBook, event.TypeBar, event.TypeOrderUpdate)

	return nil
}

// Unregister removes a strategy and its subscriptions.
func (e *Engine) Unregister(name string) error {
	// 扩展资源（feeder 引用 / universe 订阅 / 调度注册）的释放可能阻塞
	// 等待 feeder goroutine 退出，而 feeder 正在 dispatch 里等引擎读锁
	// （emitSignal/maybeRefreshUniverse）——必须先于引擎写锁完成，否则自死锁。
	e.teardownExtensions(name)

	e.mu.Lock()
	s, ok := e.strategies[name]
	if !ok {
		e.mu.Unlock()
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

	if subID, ok := e.subIDs[name]; ok {
		e.bus.Unsubscribe(subID)
		delete(e.subIDs, name)
	}
	delete(e.strategies, name)
	delete(e.strategyProtections, name) // 策略级 protection 随注销释放
	e.mu.Unlock()

	s.Stop()
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
	if err := s.Start(params); err != nil {
		return err
	}
	// 多周期供给 / 计划调度 / 动态 universe 的启用与资源记账
	e.setupExtensions(name, s)
	return nil
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

// RuntimeStatus returns runtime state for a registered strategy. The second
// return value reports whether the strategy exists and exposes runtime state
// (via RuntimeStatus() map[string]any, implemented by MACD/CRA strategies).
func (e *Engine) RuntimeStatus(name string) (map[string]any, bool) {
	e.mu.RLock()
	s, ok := e.strategies[name]
	e.mu.RUnlock()
	if !ok {
		return nil, false
	}
	// 注册进引擎的是 WrapStrategy 包装（NamedStrategy），断言前解包到
	// 真实策略实例，否则接口方法集不会穿透包装。
	if ns, isNamed := s.(*NamedStrategy); isNamed {
		s = ns.Strategy
	}
	rs, ok := s.(interface{ RuntimeStatus() map[string]any })
	if !ok {
		return nil, false
	}
	return rs.RuntimeStatus(), true
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
			// 声明了主周期的策略只接收主周期 K 线：同 symbol 的其它周期
			// 供给（多周期 feeder/universe 订阅）不会污染策略状态。
			// 注意：注册进引擎的是 NamedStrategy 包装，可选接口断言必须先解包。
			core := UnwrapStrategy(s)
			if p, ok2 := core.(PrimaryTimeframer); ok2 {
				if tf := strings.ToLower(strings.TrimSpace(p.PrimaryTimeframe())); tf != "" &&
					bar.Interval != "" && !strings.EqualFold(bar.Interval, tf) {
					break
				}
			}
			LogFirstBar(s.Name(), bar)
			signal, err = s.OnBar(bar, e.bus)
			// 动态 universe：按主周期 K 线计数驱动刷新（OnBar 之后，拿到的
			// 是最新状态；panic 已由订阅回调 recover）
			e.maybeRefreshUniverse(s, core, bar)
			// v1.2 adjust_trade_position：持仓期间每根主周期 K 线询问加/减仓；
			// OnBar 报错的 K 线跳过（与信号出口的错误处理同口径）。
			if err == nil {
				e.maybeAdjustPosition(s, core, bar)
			}
		}
	case event.TypeOrderUpdate:
		if order, ok := evt.Data.(model.OrderData); ok {
			signal, err = s.OnOrderUpdate(order, e.bus)
		}
	}

	if err != nil {
		return
	}
	if signal != nil {
		// 用包装后的策略名（=配置 id）覆盖策略内部名（见 emitSignal 同样口径）
		signal.Strategy = s.Name()
	}
	e.emitSignal(s, signal, err)
}

// PublishSignal publishes a signal to the event bus.
func PublishSignal(bus *event.EventBus, signal model.Signal) {
	metrics.RecordSignal(signal.Strategy, signal.Direction)
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
