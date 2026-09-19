package backtest

import (
	"fmt"
	"sort"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 组合回测（A6.2） ──
// 单策略回测的组合级封装：每个 leg 独立跑事件驱动回测（复用 Runner），
// 再按目标权重把各 leg 权益曲线合成为组合权益。定期再平衡
// （daily/weekly/monthly/none）在组合层面调整各 leg 的份额单位，
// 并记录权重漂移。

// PortfolioLeg 是组合中的一个策略腿。
type PortfolioLeg struct {
	StrategyType string         `json:"strategy_type"`
	Symbol       string         `json:"symbol"`
	Weight       float64        `json:"weight"`
	Params       map[string]any `json:"params,omitempty"`
}

// PortfolioConfig 配置组合回测。
type PortfolioConfig struct {
	Name           string         `json:"name"`
	Timeframe      string         `json:"timeframe"`
	Start          int64          `json:"start"` // unix ms，0=不限
	End            int64          `json:"end"`
	InitialCapital float64        `json:"initial_capital"`
	Rebalance      string         `json:"rebalance"` // none|daily|weekly|monthly
	Legs           []PortfolioLeg `json:"legs"`

	Commission   float64 `json:"commission,omitempty"`
	Slippage     float64 `json:"slippage,omitempty"`
	RiskFreeRate float64 `json:"risk_free_rate,omitempty"`
}

// StrategyFactoryFn 由调用方（handler）注入：按 leg 构造单策略回测实例。
type StrategyFactoryFn func(leg PortfolioLeg) (BacktestStrategy, error)

// LoadBarsFn 由调用方注入：加载 symbol+interval 在 [fromMs,toMs] 的 K 线。
type LoadBarsFn func(symbol, interval string, fromMs, toMs int64) ([]model.Bar, error)

// LegResult 是每个 leg 的输出。
type LegResult struct {
	StrategyType      string  `json:"strategy_type"`
	Symbol            string  `json:"symbol"`
	Weight            float64 `json:"weight"`
	InitialAllocation float64 `json:"initial_allocation"`
	FinalValue        float64 `json:"final_value"`
	TotalReturnPct    float64 `json:"total_return_pct"`
	ContributionPct   float64 `json:"contribution_pct"` // 对组合总收益的贡献（百分点）
	Trades            int     `json:"trades"`
	WinRate           float64 `json:"win_rate"`
	SharpeRatio       float64 `json:"sharpe_ratio"`
	MaxDrawdownPct    float64 `json:"max_drawdown_pct"`
}

// WeightDriftRecord 记录一次再平衡前后的权重。
type WeightDriftRecord struct {
	Time          int64              `json:"time"`
	Trigger       string             `json:"trigger"` // rebalance|end
	WeightsBefore map[string]float64 `json:"weights_before"`
	WeightsAfter  map[string]float64 `json:"weights_after"`
}

// PortfolioResult 是组合回测输出。
type PortfolioResult struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Metrics     *RunResult          `json:"metrics"`
	EquityCurve []EquityPoint       `json:"equity_curve"`
	Legs        []LegResult         `json:"legs"`
	Drift       []WeightDriftRecord `json:"drift"`
	DurationMs  int64               `json:"duration_ms"`
}

// RunPortfolio 执行组合回测。
func RunPortfolio(cfg PortfolioConfig, factory StrategyFactoryFn, loadBars LoadBarsFn) (*PortfolioResult, error) {
	if factory == nil {
		return nil, fmt.Errorf("strategy factory is nil")
	}
	if loadBars == nil {
		return nil, fmt.Errorf("load bars func is nil")
	}
	if len(cfg.Legs) == 0 {
		return nil, fmt.Errorf("portfolio needs at least one leg")
	}
	if cfg.InitialCapital <= 0 {
		cfg.InitialCapital = 100000
	}
	if cfg.Timeframe == "" {
		cfg.Timeframe = "1h"
	}

	// 权重归一化
	var wsum float64
	for _, l := range cfg.Legs {
		if l.Weight <= 0 {
			l.Weight = 1
		}
		wsum += l.Weight
	}
	weights := make([]float64, len(cfg.Legs))
	for i, l := range cfg.Legs {
		weights[i] = l.Weight / wsum
	}

	start := time.Now()
	type legRun struct {
		leg    PortfolioLeg
		weight float64
		alloc  float64
		result *RunResult
		idx    []EquityPoint // 归一化净值（起点=1）
	}
	runs := make([]legRun, len(cfg.Legs))
	for i, leg := range cfg.Legs {
		if leg.Symbol == "" {
			return nil, fmt.Errorf("leg %d: symbol required", i)
		}
		strategy, err := factory(leg)
		if err != nil {
			return nil, fmt.Errorf("leg %d (%s %s): %w", i, leg.StrategyType, leg.Symbol, err)
		}
		bars, err := loadBars(leg.Symbol, cfg.Timeframe, cfg.Start, cfg.End)
		if err != nil {
			return nil, fmt.Errorf("leg %d (%s %s): load bars: %w", i, leg.StrategyType, leg.Symbol, err)
		}
		if len(bars) < 50 {
			return nil, fmt.Errorf("leg %d (%s %s): 仅 %d 根K线，至少需要 50 根", i, leg.StrategyType, leg.Symbol, len(bars))
		}
		alloc := cfg.InitialCapital * weights[i]
		rcfg := DefaultRunnerConfig()
		rcfg.InitialBalance = alloc
		rcfg.StartTime = cfg.Start
		rcfg.EndTime = cfg.End
		rcfg.Commission = cfg.Commission
		rcfg.Slippage = cfg.Slippage
		if cfg.RiskFreeRate > 0 {
			rcfg.RiskFreeRate = cfg.RiskFreeRate
		}
		runner := NewRunner(rcfg)
		runner.LoadBars(leg.Symbol, bars)
		result, err := runner.Run(strategy)
		if err != nil {
			return nil, fmt.Errorf("leg %d (%s %s): run: %w", i, leg.StrategyType, leg.Symbol, err)
		}
		// 归一化净值曲线
		idx := make([]EquityPoint, len(result.EquityCurve))
		base := result.EquityCurve[0].Equity
		if base <= 0 {
			base = alloc
		}
		for j, pt := range result.EquityCurve {
			idx[j] = EquityPoint{Timestamp: pt.Timestamp, Equity: pt.Equity / base}
		}
		runs[i] = legRun{leg: leg, weight: weights[i], alloc: alloc, result: result, idx: idx}
	}

	// 组合时间轴 = 全部 leg 净值时间戳的并集（升序）
	tsSet := map[int64]struct{}{}
	for _, r := range runs {
		for _, pt := range r.idx {
			tsSet[pt.Timestamp] = struct{}{}
		}
	}
	timeline := make([]int64, 0, len(tsSet))
	for ts := range tsSet {
		timeline = append(timeline, ts)
	}
	sort.Slice(timeline, func(i, j int) bool { return timeline[i] < timeline[j] })

	// units_i = 各 leg 份额（净值=1 起步 → units=初始资金×权重）
	units := make([]float64, len(runs))
	for i := range runs {
		units[i] = runs[i].alloc
	}
	idxAt := func(i int, t int64) float64 {
		idx := runs[i].idx
		// 二分：最后一个 Timestamp <= t 的点
		k := sort.Search(len(idx), func(j int) bool { return idx[j].Timestamp > t }) - 1
		if k < 0 {
			k = 0
		}
		return idx[k].Equity
	}

	equity := make([]EquityPoint, 0, len(timeline))
	drift := make([]WeightDriftRecord, 0)
	portfolioAt := func(t int64) float64 {
		total := 0.0
		for i := range runs {
			total += units[i] * idxAt(i, t)
		}
		return total
	}
	weightsSnapshot := func(t int64) map[string]float64 {
		m := make(map[string]float64, len(runs))
		total := portfolioAt(t)
		for i, r := range runs {
			if total > 0 {
				m[r.leg.Symbol+"|"+r.leg.StrategyType] = units[i] * idxAt(i, t) / total
			} else {
				m[r.leg.Symbol+"|"+r.leg.StrategyType] = 0
			}
		}
		return m
	}

	rebalanceKind := parseRebalance(cfg.Rebalance)
	var lastBoundary string
	for _, t := range timeline {
		if rebalanceKind != "" && len(equity) > 0 {
			if b := rebalanceBoundary(t, rebalanceKind); b != "" && b != lastBoundary {
				// 到再平衡点：先记漂移，再按目标权重重置份额
				before := weightsSnapshot(t)
				total := portfolioAt(t)
				after := make(map[string]float64, len(runs))
				for i := range runs {
					target := runs[i].weight
					iv := idxAt(i, t)
					if iv > 0 {
						units[i] = total * target / iv
					}
					after[runs[i].leg.Symbol+"|"+runs[i].leg.StrategyType] = target
				}
				drift = append(drift, WeightDriftRecord{Time: t, Trigger: "rebalance", WeightsBefore: before, WeightsAfter: after})
				lastBoundary = b
			}
		}
		equity = append(equity, EquityPoint{Timestamp: t, Equity: portfolioAt(t)})
	}
	// 期末漂移记录（none 模式下让用户看到自然漂移）
	if len(equity) > 0 {
		endT := equity[len(equity)-1].Timestamp
		before := weightsSnapshot(endT)
		after := make(map[string]float64, len(runs))
		for _, r := range runs {
			after[r.leg.Symbol+"|"+r.leg.StrategyType] = r.weight
		}
		drift = append(drift, WeightDriftRecord{Time: endT, Trigger: "end", WeightsBefore: before, WeightsAfter: after})
	}

	// 组合级指标：合并所有 leg 的成交 + 组合权益曲线，复用 stats.go 口径
	var allTrades []Position
	legs := make([]LegResult, len(runs))
	for i, r := range runs {
		allTrades = append(allTrades, r.result.Trades...)
		finalValue := r.alloc
		if len(r.idx) > 0 {
			finalValue = r.alloc * r.idx[len(r.idx)-1].Equity
		}
		legs[i] = LegResult{
			StrategyType:      r.leg.StrategyType,
			Symbol:            r.leg.Symbol,
			Weight:            r.weight,
			InitialAllocation: r.alloc,
			FinalValue:        finalValue,
			TotalReturnPct:    r.result.TotalReturnPct,
			ContributionPct:   (finalValue - r.alloc) / cfg.InitialCapital * 100,
			Trades:            r.result.TotalTrades,
			WinRate:           r.result.WinRate,
			SharpeRatio:       r.result.SharpeRatio,
			MaxDrawdownPct:    r.result.MaxDrawdownPct,
		}
	}
	metrics := MetricsFromEquity(equity, cfg.InitialCapital, cfg.RiskFreeRate, allTrades, time.Since(start).Milliseconds())

	return &PortfolioResult{
		ID:          fmt.Sprintf("pb-%d", time.Now().UnixMilli()),
		Name:        cfg.Name,
		Metrics:     metrics,
		EquityCurve: equity,
		Legs:        legs,
		Drift:       drift,
		DurationMs:  time.Since(start).Milliseconds(),
	}, nil
}

// parseRebalance 校验再平衡周期。
func parseRebalance(s string) string {
	switch s {
	case "daily", "weekly", "monthly":
		return s
	default:
		return ""
	}
}

// rebalanceBoundary 返回时间所在的再平衡周期标识（变化即触发再平衡）。
// 用 UTC 日历边界；weekly 按 ISO 周（周一为界）。
func rebalanceBoundary(t int64, kind string) string {
	tm := time.UnixMilli(t).UTC()
	switch kind {
	case "daily":
		return tm.Format("2006-01-02")
	case "weekly":
		y, w := tm.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	case "monthly":
		return tm.Format("2006-01")
	}
	return ""
}
