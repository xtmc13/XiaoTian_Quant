package strategies

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// maxBarsHistory limits the number of bars kept in memory for indicator strategies.
const maxBarsHistory = 200

// ── TrendLongStrategy ───────────────────────────────────────────
// Follows the trend on the long side only.
// Enters LONG when fast EMA crosses above slow EMA (golden cross).
// Exits when fast EMA crosses below slow EMA (death cross).

type TrendLongStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	bars       []model.Bar
	inPosition bool

	// ── A7.1 多周期 / A7.2 调度 ──
	// timeframe 是主周期（params["timeframe"]，引擎按它过滤 OnBar 分发）；
	// extraTF 是策略额外需要的周期（params["timeframes"]，逗号分隔或数组），
	// 引擎为其供给 K 线，策略经 provider 读取高周期指标做方向过滤；
	// schedule 声明后引擎到点触发 OnSchedule（本策略用它做高周期趋势重检）。
	timeframe string
	extraTF   []string
	schedule  string
	provider  strategy.BarProvider

	params *strategy.ParamRegistry
}

// NewTrendLongStrategy creates a default trend-long strategy instance.
func NewTrendLongStrategy() *TrendLongStrategy {
	s := &TrendLongStrategy{
		name:   "trend_long",
		symbol: "BTCUSDT",
	}
	s.params = strategy.NewParamRegistry()
	// Dynamic parameters are intentionally not exposed for trend_long.
	// Entry/add-position indicators are configured via CRA params instead.
	return s
}

func (s *TrendLongStrategy) Name() string { return s.name }

func (s *TrendLongStrategy) Symbol() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.symbol
}

func (s *TrendLongStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol": s.symbol,
	}
}

func (s *TrendLongStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *TrendLongStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyParamsLocked(params); err != nil {
		return fmt.Errorf("trend_long apply params: %w", err)
	}
	if sym, ok := params["symbol"].(string); ok && sym != "" {
		s.symbol = sym
	}
	s.applyEngineKeysLocked(params)
	s.running = true
	return nil
}

// applyEngineKeysLocked 读取引擎级键（A7.1 多周期 / A7.2 调度）。
func (s *TrendLongStrategy) applyEngineKeysLocked(params map[string]any) {
	if tf, ok := params["timeframe"].(string); ok && tf != "" {
		s.timeframe = strings.ToLower(strings.TrimSpace(tf))
	}
	if v, ok := params["timeframes"]; ok {
		s.extraTF = nil
		switch t := v.(type) {
		case string:
			for _, part := range strings.Split(t, ",") {
				if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
					s.extraTF = append(s.extraTF, part)
				}
			}
		case []any:
			for _, item := range t {
				if str, ok2 := item.(string); ok2 {
					if str = strings.ToLower(strings.TrimSpace(str)); str != "" {
						s.extraTF = append(s.extraTF, str)
					}
				}
			}
		case []string:
			for _, str := range t {
				if str = strings.ToLower(strings.TrimSpace(str)); str != "" {
					s.extraTF = append(s.extraTF, str)
				}
			}
		}
	}
	if sched, ok := params["schedule"].(string); ok {
		s.schedule = strings.TrimSpace(sched)
	}
}

// Timeframes 声明额外周期（引擎为其供给 K 线，经 BarProvider 读取）。
func (s *TrendLongStrategy) Timeframes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.extraTF...)
}

// PrimaryTimeframe 声明主周期：引擎只把该周期的 K 线分发进 OnBar。
func (s *TrendLongStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeframe
}

// Schedule 声明计划调度（params["schedule"]，如 "@every 4h" / "daily@00:00"）。
func (s *TrendLongStrategy) Schedule() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schedule
}

// SetBarProvider 注入多周期数据访问器（引擎 Start 后调用）。
func (s *TrendLongStrategy) SetBarProvider(p strategy.BarProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = p
}

// higherTrendUp 用最高声明周期判断大级别趋势：最新收盘 > 该周期 EMA60。
// 未声明多周期或数据不足时返回 true（不约束）。
func (s *TrendLongStrategy) higherTrendUp() bool {
	if s.provider == nil || len(s.extraTF) == 0 {
		return true
	}
	tf := s.extraTF[len(s.extraTF)-1] // 周期列表假定升序，取最大周期
	series := s.provider.GetSeries(s.symbol, tf)
	if len(series) < 61 {
		return true // 高周期数据未暖机完成前不约束
	}
	closes := make([]float64, len(series))
	for i, b := range series {
		closes[i] = b.Close
	}
	ema60 := ema(closes, 60)
	return closes[len(closes)-1] > ema60
}

func (s *TrendLongStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *TrendLongStrategy) GetParameters() *strategy.ParamRegistry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.params
}

func (s *TrendLongStrategy) ValidateParams() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *TrendLongStrategy) ApplyParams(m map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyParamsLocked(m)
}

func (s *TrendLongStrategy) ParamDefs() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// applyParamsLocked updates parameters. Caller must hold s.mu.
func (s *TrendLongStrategy) applyParamsLocked(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	// Unknown keys are ignored so CRA-style configs can safely include
	// frontend fields that this strategy does not declare.
	return s.params.FromMap(m)
}

// OnTick delegates to bar-based logic.
func (s *TrendLongStrategy) OnTick(_ model.Tick, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderBook is not used for this indicator strategy.
func (s *TrendLongStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderUpdate is not used for this indicator strategy.
func (s *TrendLongStrategy) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar receives a new bar, updates internal history, and emits signals.
func (s *TrendLongStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}

	s.bars = append(s.bars, bar)
	if len(s.bars) > maxBarsHistory {
		s.bars = s.bars[len(s.bars)-maxBarsHistory:]
	}
	if len(s.bars) < trendSlowPeriod+1 {
		return nil, nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	fastEMA := ema(closes, trendFastPeriod)
	slowEMA := ema(closes, trendSlowPeriod)
	prevFast := ema(closes[:len(closes)-1], trendFastPeriod)
	prevSlow := ema(closes[:len(closes)-1], trendSlowPeriod)

	goldenCross := prevFast <= prevSlow && fastEMA > slowEMA
	deathCross := prevFast >= prevSlow && fastEMA < slowEMA

	// 多周期过滤：金叉出现在大级别（声明的最高周期）下跌趋势中时不进场，
	// 只等顺大级别趋势的金叉（GetBar(symbol,"4h")/GetSeries 的示例用法）。
	if goldenCross && !s.inPosition && !s.higherTrendUp() {
		return nil, nil
	}

	if goldenCross && !s.inPosition {
		s.inPosition = true
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  0.7,
			Strategy:  s.name,
			Reason:    "ema golden cross long",
			Timestamp: bar.Time,
		}, nil
	}
	if deathCross && s.inPosition {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  0.7,
			Strategy:  s.name,
			Reason:    "ema death cross close",
			Timestamp: bar.Time,
		}, nil
	}
	return nil, nil
}

// OnSchedule 计划调度回调（A7.2）：到点用最近一根 K 线重检持仓逻辑——
// 大级别趋势转空时平掉多单（比等死亡交叉更及时的保护）。
func (s *TrendLongStrategy) OnSchedule(_ time.Time, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || !s.inPosition {
		return nil, nil
	}
	if s.provider == nil || len(s.extraTF) == 0 {
		return nil, nil
	}
	if !s.higherTrendUp() {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  0.6,
			Strategy:  s.name,
			Reason:    "scheduled higher-timeframe trend guard exit",
		}, nil
	}
	return nil, nil
}
