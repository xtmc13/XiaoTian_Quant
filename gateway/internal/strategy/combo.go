package strategy

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Strategy Factory ──

var strategyFactory = make(map[string]func() Strategy)

// RegisterStrategyFactory registers a factory function for a strategy type.
func RegisterStrategyFactory(name string, factory func() Strategy) {
	strategyFactory[name] = factory
}

// StrategyFactory creates a strategy instance by name.
func StrategyFactory(name string) Strategy {
	if f, ok := strategyFactory[name]; ok {
		return f()
	}
	return nil
}

// ── StrategyCombo ──

type comboMemberInstance struct {
	member   ComboMember
	strategy Strategy
}

// StrategyCombo wraps multiple strategies and aggregates their signals.
type StrategyCombo struct {
	BaseStrategy
	id      string
	name    string
	symbol  string
	config  *ComboConfig
	members []comboMemberInstance
	running bool
	mu      sync.RWMutex

	// recent signals history (limited to last 100)
	signals []model.Signal
}

// NewStrategyCombo creates a new combo from a config.
func NewStrategyCombo(config *ComboConfig) (*StrategyCombo, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	sc := &StrategyCombo{
		id:      config.ID,
		name:    config.Name,
		symbol:  config.Symbol,
		config:  config,
		members: make([]comboMemberInstance, 0, len(config.Members)),
		signals: make([]model.Signal, 0, 100),
	}

	for _, m := range config.Members {
		if !m.Enabled {
			sc.members = append(sc.members, comboMemberInstance{member: m})
			continue
		}
		s := StrategyFactory(m.StrategyName)
		if s == nil {
			return nil, fmt.Errorf("unknown strategy: %s", m.StrategyName)
		}
		sc.members = append(sc.members, comboMemberInstance{member: m, strategy: s})
	}

	return sc, nil
}

// Name returns the unique ID of the combo (used by the engine).
func (sc *StrategyCombo) Name() string { return sc.id }

// Symbol returns the trading symbol.
func (sc *StrategyCombo) Symbol() string { return sc.symbol }

// Params returns the combo configuration as a map.
func (sc *StrategyCombo) Params() map[string]any {
	return sc.config.ToMap()
}

// IsRunning reports whether the combo is active.
func (sc *StrategyCombo) IsRunning() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.running
}

// Start initializes all member strategies and marks the combo as running.
func (sc *StrategyCombo) Start(params map[string]any) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.running {
		return nil
	}
	for _, m := range sc.members {
		if m.strategy == nil {
			continue
		}
		if err := m.strategy.Start(params); err != nil {
			return fmt.Errorf("start member %s: %w", m.member.StrategyName, err)
		}
	}
	sc.running = true
	sc.config.Status = "running"
	return nil
}

// Stop halts all member strategies and marks the combo as stopped.
func (sc *StrategyCombo) Stop() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if !sc.running {
		return nil
	}
	for _, m := range sc.members {
		if m.strategy == nil {
			continue
		}
		_ = m.strategy.Stop()
	}
	sc.running = false
	sc.config.Status = "stopped"
	return nil
}

// OnTick delegates to members or returns nil.
func (sc *StrategyCombo) OnTick(_ model.Tick, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderBook delegates to members or returns nil.
func (sc *StrategyCombo) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderUpdate delegates to members or returns nil.
func (sc *StrategyCombo) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar runs all enabled member strategies, collects their signals, and aggregates them.
func (sc *StrategyCombo) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	sc.mu.RLock()
	if !sc.running {
		sc.mu.RUnlock()
		return nil, nil
	}
	members := make([]comboMemberInstance, len(sc.members))
	copy(members, sc.members)
	sc.mu.RUnlock()

	var signals []*model.Signal
	for _, m := range members {
		if !m.member.Enabled || m.strategy == nil {
			continue
		}
		sig, err := m.strategy.OnBar(bar, bus)
		if err != nil {
			continue
		}
		if sig != nil {
			signals = append(signals, sig)
		}
	}

	if len(signals) == 0 {
		return nil, nil
	}

	aggregated := sc.aggregate(signals)
	if aggregated != nil {
		sc.mu.Lock()
		sc.signals = append(sc.signals, *aggregated)
		if len(sc.signals) > 100 {
			sc.signals = sc.signals[len(sc.signals)-100:]
		}
		sc.mu.Unlock()
	}
	return aggregated, nil
}

func (sc *StrategyCombo) aggregate(signals []*model.Signal) *model.Signal {
	switch sc.config.AggregationMode {
	case "vote":
		return sc.voteAggregate(signals)
	case "weighted":
		return sc.weightedAggregate(signals)
	case "unanimous":
		return sc.unanimousAggregate(signals)
	default:
		return sc.voteAggregate(signals)
	}
}

func (sc *StrategyCombo) voteAggregate(signals []*model.Signal) *model.Signal {
	counts := make(map[string]int)
	strengths := make(map[string]float64)
	for _, sig := range signals {
		counts[sig.Direction]++
		strengths[sig.Direction] += sig.Strength
	}

	var winner string
	maxCount := 0
	for dir, count := range counts {
		if count > maxCount {
			maxCount = count
			winner = dir
		}
	}

	// Check for tie (no clear majority)
	tie := false
	for dir, count := range counts {
		if dir != winner && count == maxCount {
			tie = true
			break
		}
	}
	if tie || winner == "" {
		return nil
	}

	avgStrength := strengths[winner] / float64(counts[winner])
	reasons := make([]string, 0, len(signals))
	for _, sig := range signals {
		if sig.Direction == winner {
			reasons = append(reasons, fmt.Sprintf("%s:%.2f", sig.Strategy, sig.Strength))
		}
	}

	return &model.Signal{
		Symbol:    sc.symbol,
		Direction: winner,
		Strength:  avgStrength,
		Strategy:  sc.name,
		Reason:    fmt.Sprintf("vote majority %s (%s)", winner, joinReasons(reasons)),
		Timestamp: time.Now().UnixMilli(),
	}
}

func (sc *StrategyCombo) weightedAggregate(signals []*model.Signal) *model.Signal {
	weightedStrengths := make(map[string]float64)
	weightSums := make(map[string]float64)

	for _, sig := range signals {
		var weight float64
		for _, m := range sc.members {
			if m.member.StrategyName == sig.Strategy {
				weight = m.member.Weight
				break
			}
		}
		if weight == 0 {
			// fallback: equal weight among received signals
			weight = 1.0 / float64(len(signals))
		}
		weightedStrengths[sig.Direction] += sig.Strength * weight
		weightSums[sig.Direction] += weight
	}

	var winner string
	maxStrength := 0.0
	for dir, strength := range weightedStrengths {
		if strength > maxStrength {
			maxStrength = strength
			winner = dir
		}
	}

	if winner == "" || maxStrength < 0.35 {
		return nil
	}

	// Check for tie
	tie := false
	for dir, strength := range weightedStrengths {
		if dir != winner && math.Abs(strength-maxStrength) < 0.0001 {
			tie = true
			break
		}
	}
	if tie {
		return nil
	}

	normalizedStrength := maxStrength
	if weightSums[winner] > 0 {
		normalizedStrength = weightedStrengths[winner] / weightSums[winner]
	}

	reasons := make([]string, 0, len(signals))
	for _, sig := range signals {
		reasons = append(reasons, fmt.Sprintf("%s:%.2f", sig.Strategy, sig.Strength))
	}

	return &model.Signal{
		Symbol:    sc.symbol,
		Direction: winner,
		Strength:  math.Min(normalizedStrength, 1.0),
		Strategy:  sc.name,
		Reason:    fmt.Sprintf("weighted %s (%.2f) [%s]", winner, maxStrength, joinReasons(reasons)),
		Timestamp: time.Now().UnixMilli(),
	}
}

func (sc *StrategyCombo) unanimousAggregate(signals []*model.Signal) *model.Signal {
	if len(signals) == 0 {
		return nil
	}

	firstDir := signals[0].Direction
	for _, sig := range signals[1:] {
		if sig.Direction != firstDir {
			return nil
		}
	}

	var totalStrength float64
	for _, sig := range signals {
		totalStrength += sig.Strength
	}
	avgStrength := totalStrength / float64(len(signals))

	reasons := make([]string, 0, len(signals))
	for _, sig := range signals {
		reasons = append(reasons, fmt.Sprintf("%s:%.2f", sig.Strategy, sig.Strength))
	}

	return &model.Signal{
		Symbol:    sc.symbol,
		Direction: firstDir,
		Strength:  avgStrength,
		Strategy:  sc.name,
		Reason:    fmt.Sprintf("unanimous %s (%s)", firstDir, joinReasons(reasons)),
		Timestamp: time.Now().UnixMilli(),
	}
}

// RecentSignals returns the last N aggregated signals.
func (sc *StrategyCombo) RecentSignals(limit int) []model.Signal {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if limit <= 0 || limit > len(sc.signals) {
		limit = len(sc.signals)
	}
	if limit == 0 {
		return nil
	}
	result := make([]model.Signal, limit)
	copy(result, sc.signals[len(sc.signals)-limit:])
	return result
}

func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "none"
	}
	result := reasons[0]
	for i := 1; i < len(reasons); i++ {
		result += ", " + reasons[i]
	}
	return result
}
