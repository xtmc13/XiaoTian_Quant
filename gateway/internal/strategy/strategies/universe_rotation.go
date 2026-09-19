package strategies

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── UniverseRotationStrategy ─────────────────────────────────────
// 动态 universe 轮动示例（A7.3）：策略声明候选池 watchlist，引擎为候选池
// 全部供给 K 线（数据进引擎 MarketData）；每 N 根主周期 K 线引擎调
// OnUniverse，策略按动量排名返回 top N 标的，引擎自动增删对应 symbol 的
// 事件订阅与 K 线供给。OnBar 只在 universe 内的 symbol 上收到。
//
// 参数（config_json / params）：
//
//	symbols             候选池，逗号分隔（默认 BTCUSDT,ETHUSDT,SOLUSDT）
//	top_n               轮动持有数（默认 3）
//	momentum_bars       动量排名周期（主周期 K 线根数，默认 48）
//	min_momentum        最小动量阈值（小数，如 0.02 = 2%；默认 0）
//	timeframe           主周期（默认 15m）
//	universe_refresh_bars  universe 刷新频率（主周期根数，默认 12）
//	schedule            可选计划调度（A7.2），到点重排 universe

type UniverseRotationStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	timeframe    string
	watchlist    []string
	topN         int
	momentumBars int
	minMomentum  float64
	refreshBars  int
	schedule     string

	// 当前 universe（OnUniverse 返回后由引擎应用；策略侧也存一份用于 OnBar 过滤）
	current   map[string]bool
	inPosture map[string]bool        // symbol -> 是否已发过多单（简化仓位跟踪）
	barHist   map[string][]model.Bar // symbol -> 主周期历史（universe 内累积，移出即清）

	params *strategy.ParamRegistry
}

// NewUniverseRotationStrategy 创建默认轮动策略实例。
func NewUniverseRotationStrategy() *UniverseRotationStrategy {
	s := &UniverseRotationStrategy{
		name:         "universe_rotation",
		symbol:       "BTCUSDT",
		timeframe:    "15m",
		topN:         3,
		momentumBars: 48,
		minMomentum:  0,
		refreshBars:  12,
		current:      map[string]bool{},
		inPosture:    map[string]bool{},
		barHist:      map[string][]model.Bar{},
	}
	s.params = strategy.NewParamRegistry()
	return s
}

func (s *UniverseRotationStrategy) Name() string { return s.name }

func (s *UniverseRotationStrategy) Symbol() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.symbol
}

func (s *UniverseRotationStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol":    s.symbol,
		"timeframe": s.timeframe,
		"watchlist": strings.Join(s.watchlist, ","),
		"top_n":     s.topN,
	}
}

func (s *UniverseRotationStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *UniverseRotationStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyParamsLocked(params); err != nil {
		return fmt.Errorf("universe_rotation apply params: %w", err)
	}
	if sym, ok := params["symbol"].(string); ok && sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if tf, ok := params["timeframe"].(string); ok && tf != "" {
		s.timeframe = strings.ToLower(strings.TrimSpace(tf))
	}
	if v, ok := params["symbols"].(string); ok && strings.TrimSpace(v) != "" {
		s.watchlist = nil
		for _, part := range strings.Split(v, ",") {
			if part = strings.ToUpper(strings.TrimSpace(part)); part != "" {
				s.watchlist = append(s.watchlist, part)
			}
		}
	}
	if v, ok := params["watchlist"].(string); ok && strings.TrimSpace(v) != "" {
		s.watchlist = nil
		for _, part := range strings.Split(v, ",") {
			if part = strings.ToUpper(strings.TrimSpace(part)); part != "" {
				s.watchlist = append(s.watchlist, part)
			}
		}
	}
	if len(s.watchlist) == 0 {
		s.watchlist = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	}
	// 主 symbol 必须在候选池里（universe 至少保留它）
	found := false
	for _, w := range s.watchlist {
		if w == s.symbol {
			found = true
			break
		}
	}
	if !found {
		s.watchlist = append([]string{s.symbol}, s.watchlist...)
	}
	if n := intParam(params, "top_n", 3); n > 0 {
		s.topN = n
	}
	if n := intParam(params, "momentum_bars", 48); n > 0 {
		s.momentumBars = n
	}
	if f := floatParam(params, "min_momentum", 0); f >= 0 {
		s.minMomentum = f
	}
	if n := intParam(params, "universe_refresh_bars", 12); n > 0 {
		s.refreshBars = n
	}
	if sched, ok := params["schedule"].(string); ok {
		s.schedule = strings.TrimSpace(sched)
	}
	s.current[s.symbol] = true
	s.running = true
	return nil
}

func intParam(params map[string]any, key string, def int) int {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case int:
			return t
		case float64:
			return int(t)
		case string:
			var n int
			if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
				return n
			}
		}
	}
	return def
}

func floatParam(params map[string]any, key string, def float64) float64 {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case int:
			return float64(t)
		case float64:
			return t
		case string:
			var n float64
			if _, err := fmt.Sscanf(t, "%g", &n); err == nil {
				return n
			}
		}
	}
	return def
}

func (s *UniverseRotationStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosture = map[string]bool{}
	s.barHist = map[string][]model.Bar{}
	return nil
}

func (s *UniverseRotationStrategy) GetParameters() *strategy.ParamRegistry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.params
}

func (s *UniverseRotationStrategy) ValidateParams() error { return nil }

func (s *UniverseRotationStrategy) ApplyParams(m map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyParamsLocked(m)
}

func (s *UniverseRotationStrategy) applyParamsLocked(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	return s.params.FromMap(m)
}

func (s *UniverseRotationStrategy) ParamDefs() []map[string]any {
	return []map[string]any{
		{"name": "symbols", "type": "string", "required": true, "description": "候选池，逗号分隔，如 BTCUSDT,ETHUSDT,SOLUSDT"},
		{"name": "top_n", "type": "int", "default": 3, "min": 1, "max": 20, "description": "轮动持有数"},
		{"name": "momentum_bars", "type": "int", "default": 48, "min": 5, "max": 500, "description": "动量排名周期（主周期K线根数）"},
		{"name": "min_momentum", "type": "float", "default": 0, "min": 0, "max": 1, "description": "最小动量阈值（小数，如 0.02）"},
		{"name": "timeframe", "type": "interval", "default": "15m", "description": "主周期"},
		{"name": "universe_refresh_bars", "type": "int", "default": 12, "min": 1, "max": 500, "description": "universe 刷新频率（主周期K线根数）"},
		{"name": "schedule", "type": "string", "description": "计划调度，如 @every 4h / daily@00:00（可选）"},
	}
}

// ── A7.3 动态 universe 接口实现 ──

// Watchlist 返回候选池：引擎为全部候选供给 K 线（数据进 MarketData）。
func (s *UniverseRotationStrategy) Watchlist() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.watchlist...)
}

// UniverseRefreshBars universe 每 N 根主周期 K 线刷新一次。
func (s *UniverseRotationStrategy) UniverseRefreshBars() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.refreshBars
}

// PrimaryTimeframe 主周期（引擎按它过滤 OnBar 分发）。
func (s *UniverseRotationStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeframe
}

// Schedule 可选计划调度（到点经 OnSchedule 重排）。
func (s *UniverseRotationStrategy) Schedule() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schedule
}

// OnUniverse 按候选池动量排名返回新的 universe（top N）。
// 引擎每 N 根主周期 K 线调用一次，据此增删 symbol 订阅。
func (s *UniverseRotationStrategy) OnUniverse(ctx strategy.UniverseContext) []string {
	s.mu.Lock()
	watchlist := append([]string(nil), s.watchlist...)
	topN := s.topN
	momentumBars := s.momentumBars
	minMomentum := s.minMomentum
	symbol := s.symbol
	tf := s.timeframe
	s.mu.Unlock()

	type scored struct {
		sym string
		mom float64
	}
	scoredList := make([]scored, 0, len(watchlist))
	for _, sym := range watchlist {
		bars := ctx.Bars(sym, tf)
		if len(bars) < momentumBars+1 {
			continue
		}
		base := bars[len(bars)-1-momentumBars].Close
		if base <= 0 {
			continue
		}
		mom := bars[len(bars)-1].Close/base - 1
		scoredList = append(scoredList, scored{sym: sym, mom: mom})
	}
	// 动量降序
	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].mom > scoredList[j].mom })

	next := make([]string, 0, topN)
	for i, sc := range scoredList {
		if i >= topN {
			break
		}
		if sc.mom < minMomentum {
			break
		}
		next = append(next, sc.sym)
	}
	// 主 symbol 保底（引擎层也不允许移除，双保险）
	hasPrimary := false
	for _, sym := range next {
		if sym == symbol {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		next = append([]string{symbol}, next...)
	}

	s.mu.Lock()
	s.current = map[string]bool{}
	for _, sym := range next {
		s.current[sym] = true
	}
	s.mu.Unlock()
	return next
}

// OnSchedule 到点重排 universe：引擎随后会调用 OnUniverse 并应用变更。
// 返回 nil 信号（换仓信号在 OnUniverse 应用的下一根 K 线由 OnBar 产生）。
func (s *UniverseRotationStrategy) OnSchedule(_ time.Time, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// ── 事件处理 ──

func (s *UniverseRotationStrategy) OnTick(_ model.Tick, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *UniverseRotationStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *UniverseRotationStrategy) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar 对 universe 内 symbol 做简化动量姿态：已在 universe 且动量为正则持多，
// 离开 universe 或动量转负则平仓（真实仓位由执行层跟踪，这里发信号）。
func (s *UniverseRotationStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	sym := strings.ToUpper(bar.Symbol)
	inUniverse := s.current[sym]

	if !inUniverse {
		wasIn := s.inPosture[sym]
		delete(s.inPosture, sym)
		delete(s.barHist, sym)
		if wasIn {
			return &model.Signal{
				Symbol: sym, Direction: "CLOSE", Strength: 0.6,
				Strategy: s.name, Reason: "rotated out of universe",
				Timestamp: bar.Time,
			}, nil
		}
		return nil, nil
	}

	// 维护逐 symbol 主周期历史，计算可用窗口内的动量姿态
	hist := s.barHist[sym]
	if n := len(hist); n == 0 || hist[n-1].Time != bar.Time {
		hist = append(hist, bar)
		if len(hist) > s.momentumBars+1 {
			hist = hist[len(hist)-s.momentumBars-1:]
		}
		s.barHist[sym] = hist
	}
	if len(hist) < 3 {
		return nil, nil
	}
	lookback := s.momentumBars
	if lookback > len(hist)-1 {
		lookback = len(hist) - 1
	}
	base := hist[len(hist)-1-lookback].Close
	if base <= 0 {
		return nil, nil
	}
	momentum := hist[len(hist)-1].Close/base - 1

	if !s.inPosture[sym] && momentum > s.minMomentum && momentum > 0 {
		s.inPosture[sym] = true
		return &model.Signal{
			Symbol: sym, Direction: "LONG", Strength: 0.6,
			Strategy: s.name, Reason: "momentum rotation entry",
			Timestamp: bar.Time,
		}, nil
	}
	if s.inPosture[sym] && momentum < 0 {
		s.inPosture[sym] = false
		return &model.Signal{
			Symbol: sym, Direction: "CLOSE", Strength: 0.5,
			Strategy: s.name, Reason: "momentum rotation exit",
			Timestamp: bar.Time,
		}, nil
	}
	return nil, nil
}
