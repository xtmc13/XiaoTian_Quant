// Package hyperopt implements strategy hyperparameter optimization.
// Supports grid search with configurable loss functions.
package hyperopt

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ── Types ──────────────────────────────────────────────────────

// ParamType defines the type of a hyperparameter.
type ParamType int

const (
	ParamInt         ParamType = iota // integer parameter
	ParamFloat                        // float parameter
	ParamCategorical                  // categorical (string) parameter
)

func (p ParamType) String() string {
	switch p {
	case ParamInt:
		return "INT"
	case ParamFloat:
		return "FLOAT"
	case ParamCategorical:
		return "CATEGORICAL"
	default:
		return "UNKNOWN"
	}
}

// ParamSpace defines the search space for a single parameter.
type ParamSpace struct {
	Name    string
	Type    ParamType
	Min     float64 // for Int/Float
	Max     float64 // for Int/Float
	Step    float64 // for Int/Float
	Options []string // for Categorical
}

// TrialResult holds the result of a single optimization trial.
type TrialResult struct {
	ID      int                `json:"id"`
	Params  map[string]any     `json:"params"`
	Metrics *BacktestMetrics   `json:"metrics"`
	Score   float64            `json:"score"` // lower = better (loss)
	Elapsed time.Duration      `json:"elapsed"`
}

// BacktestMetrics holds the performance metrics for a backtest run.
type BacktestMetrics struct {
	TotalReturnPct float64 `json:"total_return_pct"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	SortinoRatio   float64 `json:"sortino_ratio"`
	CalmarRatio    float64 `json:"calmar_ratio"`
	WinRate        float64 `json:"win_rate"`
	ProfitFactor   float64 `json:"profit_factor"`
	TotalTrades    int     `json:"total_trades"`
	AvgTradePct    float64 `json:"avg_trade_pct"`
	// Extended metrics for advanced loss functions
	AvgWinPct   float64 `json:"avg_win_pct,omitempty"`   // average winning trade return
	AvgLossPct  float64 `json:"avg_loss_pct,omitempty"`  // average losing trade return (negative)
	TradeStdDev float64 `json:"trade_stddev,omitempty"`  // std dev of individual trade returns
	GrossProfit float64 `json:"gross_profit,omitempty"`  // sum of all winning trades
	GrossLoss   float64 `json:"gross_loss,omitempty"`    // sum of all losing trades (negative)
}

// LossFunc computes a loss score from backtest metrics.
// Lower score = better. Returns +Inf for invalid results.
type LossFunc func(m *BacktestMetrics) float64

// Evaluator runs a backtest with the given parameters and returns metrics.
type Evaluator func(params map[string]any) (*BacktestMetrics, error)

// ── Loss Functions ─────────────────────────────────────────────

// LossSharpe maximizes Sharpe ratio (negate since lower = better).
func LossSharpe(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.SharpeRatio
}

// LossSortino maximizes Sortino ratio.
func LossSortino(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.SortinoRatio
}

// LossCalmar maximizes Calmar ratio.
func LossCalmar(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.CalmarRatio
}

// LossMaxDrawdown minimizes maximum drawdown.
func LossMaxDrawdown(m *BacktestMetrics) float64 {
	if m == nil {
		return math.Inf(1)
	}
	return m.MaxDrawdownPct
}

// LossProfit maximizes total return (negate).
func LossProfit(m *BacktestMetrics) float64 {
	if m == nil {
		return math.Inf(1)
	}
	return -m.TotalReturnPct
}

// LossWinRate maximizes win rate (negate).
func LossWinRate(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.WinRate
}

// LossExpectancy maximizes average trade return (negate).
func LossExpectancy(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.AvgTradePct
}

// LossMultiMetric combines profit and drawdown into a single score.
// Score = -profit + 2*drawdown (penalize drawdown more).
func LossMultiMetric(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.TotalReturnPct + 2.0*m.MaxDrawdownPct
}

// LossProfitDrawdown maximizes profit/drawdown ratio (negate).
func LossProfitDrawdown(m *BacktestMetrics) float64 {
	if m == nil || m.MaxDrawdownPct == 0 || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	return -m.TotalReturnPct / m.MaxDrawdownPct
}

// ── New Loss Functions (Step 1) ──────────────────────────────

// LossProfitFactor maximizes the profit factor (gross profit / |gross loss|).
// PF > 1.0 means profitable; PF > 2.0 is considered good.
func LossProfitFactor(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	// Use exact gross profit/loss if available
	grossProfit := m.GrossProfit
	grossLoss := m.GrossLoss
	if grossProfit == 0 && grossLoss == 0 {
		// Fallback: derive from avg win/loss and win rate
		if m.AvgWinPct > 0 && m.AvgLossPct < 0 && m.WinRate > 0 && m.WinRate < 100 {
			winCount := float64(m.TotalTrades) * m.WinRate / 100.0
			lossCount := float64(m.TotalTrades) * (100.0 - m.WinRate) / 100.0
			grossProfit = winCount * m.AvgWinPct
			grossLoss = lossCount * m.AvgLossPct // already negative
		} else if m.ProfitFactor > 0 {
			// Last resort: use the profit_factor field directly
			return -m.ProfitFactor
		} else {
			return math.Inf(1)
		}
	}
	absGrossLoss := grossLoss
	if absGrossLoss < 0 {
		absGrossLoss = -absGrossLoss
	}
	if absGrossLoss == 0 {
		return math.Inf(1) // avoid division by zero
	}
	return -(grossProfit / absGrossLoss)
}

// LossRiskReward maximizes the risk-reward ratio (avg win / |avg loss|).
// R:R > 1.0 means wins are larger than losses.
func LossRiskReward(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	avgWin := m.AvgWinPct
	avgLoss := m.AvgLossPct
	// Fallback derivation if exact values not provided
	if avgWin == 0 && avgLoss == 0 && m.WinRate > 0 && m.WinRate < 100 {
		// From expectancy: E = W*avgWin + (1-W)*avgLoss, where W = winRate
		// Also: avgTrade = W*avgWin + (1-W)*avgLoss
		// We need another equation. Assume R:R = r, so avgWin = r*|avgLoss|
		// Then: avgTrade = W*r*|avgLoss| - (1-W)*|avgLoss| = |avgLoss|(W*r - (1-W))
		// => |avgLoss| = avgTrade / (W*r - (1-W))
		// This has two unknowns. Use profit factor as additional constraint.
		if m.ProfitFactor > 0 && m.AvgTradePct != 0 {
			W := m.WinRate / 100.0
			PF := m.ProfitFactor
			// PF = (W*avgWin) / ((1-W)*|avgLoss|)
			// Let r = avgWin/|avgLoss|
			// PF = W*r / (1-W)  =>  r = PF*(1-W)/W
			r := PF * (1.0 - W) / W
			return -r
		}
	}
	if avgWin <= 0 {
		return math.Inf(1) // no winning trades or invalid
	}
	absAvgLoss := avgLoss
	if absAvgLoss < 0 {
		absAvgLoss = -absAvgLoss
	}
	if absAvgLoss == 0 {
		return math.Inf(1)
	}
	return -(avgWin / absAvgLoss)
}

// LossSQN maximizes the System Quality Number (Van Tharp).
// SQN = sqrt(N) * (expectancy / stdDev)  where expectancy = avg trade return.
// SQN > 2.0 is tradable; > 5.0 is excellent.
func LossSQN(m *BacktestMetrics) float64 {
	if m == nil || m.TotalTrades == 0 {
		return math.Inf(1)
	}
	stdDev := m.TradeStdDev
	if stdDev == 0 {
		// Fallback: approximate from Sharpe ratio
		// Sharpe ≈ avgReturn / returnStdDev; SQN uses trade-level stdDev
		// Rough approximation: if we assume trade returns ≈ bar returns
		if m.SharpeRatio != 0 && m.TotalTrades > 1 {
			stdDev = math.Abs(m.AvgTradePct / m.SharpeRatio)
		} else {
			return math.Inf(1)
		}
	}
	if stdDev == 0 {
		return math.Inf(1)
	}
	sqn := math.Sqrt(float64(m.TotalTrades)) * (m.AvgTradePct / stdDev)
	return -sqn
}

// GetLossFunc returns a loss function by name.
func GetLossFunc(name string) LossFunc {
	switch name {
	case "sharpe":
		return LossSharpe
	case "sortino":
		return LossSortino
	case "calmar":
		return LossCalmar
	case "max_drawdown":
		return LossMaxDrawdown
	case "profit":
		return LossProfit
	case "win_rate":
		return LossWinRate
	case "expectancy":
		return LossExpectancy
	case "multi_metric":
		return LossMultiMetric
	case "profit_drawdown":
		return LossProfitDrawdown
	case "profit_factor":
		return LossProfitFactor
	case "risk_reward":
		return LossRiskReward
	case "sqn":
		return LossSQN
	default:
		return LossSharpe
	}
}

// LossFuncNames returns all available loss function names.
func LossFuncNames() []string {
	return []string{
		"sharpe", "sortino", "calmar", "max_drawdown",
		"profit", "win_rate", "expectancy", "multi_metric",
		"profit_drawdown", "profit_factor", "risk_reward", "sqn",
	}
}

// ── Grid Optimizer ─────────────────────────────────────────────

// OptimizerConfig configures the optimizer.
type OptimizerConfig struct {
	MaxTrials  int           `json:"max_trials"`  // 0 = unlimited
	Timeout    time.Duration `json:"timeout"`      // 0 = no timeout
	LossFunc   LossFunc      `json:"-"`            // loss function
	LossName   string        `json:"loss_name"`    // name of the loss function
	Concurrent int           `json:"concurrent"`    // number of parallel trials (1 = sequential)
}

// DefaultOptimizerConfig returns sensible defaults.
func DefaultOptimizerConfig() OptimizerConfig {
	return OptimizerConfig{
		MaxTrials:  0,
		Timeout:    10 * time.Minute,
		LossFunc:   LossSharpe,
		LossName:   "sharpe",
		Concurrent: 1,
	}
}

// GridOptimizer performs grid search over a parameter space.
type GridOptimizer struct {
	cfg       OptimizerConfig
	spaces    []ParamSpace
	evaluator Evaluator
	results   []TrialResult
	mu        sync.RWMutex
}

// NewGridOptimizer creates a new grid search optimizer.
func NewGridOptimizer(cfg OptimizerConfig, spaces []ParamSpace, evaluator Evaluator) *GridOptimizer {
	if cfg.Concurrent <= 0 {
		cfg.Concurrent = 1
	}
	if cfg.LossFunc == nil {
		cfg.LossFunc = LossSharpe
		cfg.LossName = "sharpe"
	}
	return &GridOptimizer{
		cfg:       cfg,
		spaces:    spaces,
		evaluator: evaluator,
		results:   make([]TrialResult, 0),
	}
}

// Run executes the grid search and returns results sorted by score (best first).
func (o *GridOptimizer) Run() ([]TrialResult, error) {
	if o.evaluator == nil {
		return nil, fmt.Errorf("evaluator is required")
	}
	if len(o.spaces) == 0 {
		return nil, fmt.Errorf("no parameters to optimize")
	}

	// Generate all parameter combinations
	grid := o.generateGrid()
	if len(grid) == 0 {
		return nil, fmt.Errorf("empty parameter grid")
	}

	// Limit trials
	if o.cfg.MaxTrials > 0 && len(grid) > o.cfg.MaxTrials {
		grid = grid[:o.cfg.MaxTrials]
	}

	// Evaluate each combination
	startTime := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, o.cfg.Concurrent)

	for i, params := range grid {
		// Check timeout before dispatching each trial
		if o.cfg.Timeout > 0 && time.Since(startTime) > o.cfg.Timeout {
			break
		}

		wg.Add(1)
		go func(id int, p map[string]any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Also check timeout inside goroutine before expensive work
			if o.cfg.Timeout > 0 && time.Since(startTime) > o.cfg.Timeout {
				return
			}

			trialStart := time.Now()
			metrics, err := o.evaluator(p)
			elapsed := time.Since(trialStart)

			var score float64
			if err != nil {
				score = math.Inf(1)
			} else {
				score = o.cfg.LossFunc(metrics)
			}

			tr := TrialResult{
				ID:      id,
				Params:  p,
				Metrics: metrics,
				Score:   score,
				Elapsed: elapsed,
			}

			o.mu.Lock()
			o.results = append(o.results, tr)
			o.mu.Unlock()
		}(i, params)
	}

	wg.Wait()

	// Sort by score ascending (lower = better)
	o.mu.RLock()
	sorted := make([]TrialResult, len(o.results))
	copy(sorted, o.results)
	o.mu.RUnlock()

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score < sorted[j].Score
	})

	return sorted, nil
}

// Best returns the best trial result, or nil if no trials completed.
func (o *GridOptimizer) Best() *TrialResult {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if len(o.results) == 0 {
		return nil
	}

	best := o.results[0]
	for _, r := range o.results[1:] {
		if r.Score < best.Score {
			best = r
		}
	}
	return &best
}

// Results returns all trial results (unsorted).
func (o *GridOptimizer) Results() []TrialResult {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]TrialResult, len(o.results))
	copy(result, o.results)
	return result
}

// generateGrid creates all parameter combinations from the search spaces.
func (o *GridOptimizer) generateGrid() []map[string]any {
	if len(o.spaces) == 0 {
		return nil
	}

	// Generate value lists for each parameter
	values := make([][]any, len(o.spaces))
	for i, space := range o.spaces {
		values[i] = o.expandParam(space)
	}

	// Cartesian product
	return cartesianProduct(values, o.spaces)
}

func (o *GridOptimizer) expandParam(space ParamSpace) []any {
	var vals []any
	switch space.Type {
	case ParamInt:
		for v := space.Min; v <= space.Max; v += space.Step {
			vals = append(vals, int(v))
		}
	case ParamFloat:
		for v := space.Min; v <= space.Max+space.Step/2; v += space.Step {
			vals = append(vals, math.Round(v*1e8)/1e8) // round to avoid floating errors
		}
	case ParamCategorical:
		for _, opt := range space.Options {
			vals = append(vals, opt)
		}
	}
	return vals
}

func cartesianProduct(values [][]any, spaces []ParamSpace) []map[string]any {
	if len(values) == 0 {
		return nil
	}

	// Calculate total combinations
	total := 1
	for _, v := range values {
		total *= len(v)
	}

	result := make([]map[string]any, 0, total)

	// Recursive helper
	var helper func(level int, current map[string]any)
	helper = func(level int, current map[string]any) {
		if level == len(values) {
			// Make a copy
			entry := make(map[string]any, len(current))
			for k, v := range current {
				entry[k] = v
			}
			result = append(result, entry)
			return
		}
		for _, val := range values[level] {
			current[spaces[level].Name] = val
			helper(level+1, current)
		}
	}

	helper(0, make(map[string]any))
	return result
}

// GridSize returns the number of combinations in the search space.
func (o *GridOptimizer) GridSize() int {
	values := make([][]any, len(o.spaces))
	for i, space := range o.spaces {
		values[i] = o.expandParam(space)
	}
	total := 1
	for _, v := range values {
		total *= len(v)
	}
	return total
}
