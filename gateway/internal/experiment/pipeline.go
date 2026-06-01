package experiment

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Experiment Pipeline ───────────────────────────────────────────

// ExperimentRequest defines the input for an experiment run.
type ExperimentRequest struct {
	Code           string            `json:"code"`
	Symbol         string            `json:"symbol"`
	Interval       string            `json:"interval"`
	Klines         []map[string]any  `json:"klines,omitempty"`
	Optimizer      string            `json:"optimizer"` // "de" | "tpe"
	ParamSpace     ParamSpace        `json:"param_space,omitempty"`
	DEConfig       DEConfig          `json:"de_config,omitempty"`
	TPEConfig      TPEConfig         `json:"tpe_config,omitempty"`
	ScoreConfig    ScoreConfig       `json:"score_config,omitempty"`
	OOSRatio       float64           `json:"oos_ratio"`
	BacktestConfig backtest.RunnerConfig `json:"backtest_config,omitempty"`
}

// ExperimentResult holds the complete output of an experiment.
type ExperimentResult struct {
	ExperimentID   string                 `json:"experiment_id"`
	Status         string                 `json:"status"`
	DurationMs     int64                  `json:"duration_ms"`
	BestParams     map[string]any         `json:"best_params"`
	BestScore      float64                `json:"best_score"`
	BestVector     []float64              `json:"best_vector,omitempty"`
	ISResult       *backtest.RunResult    `json:"is_result,omitempty"`
	OOSResult      *backtest.RunResult    `json:"oos_result,omitempty"`
	ISScore        *ScoreResult           `json:"is_score,omitempty"`
	OOSScore       *ScoreResult           `json:"oos_score,omitempty"`
	ParetoFront    []ParetoPoint          `json:"pareto_front,omitempty"`
	Generations    []GenerationSnapshot   `json:"generations,omitempty"`
	Observations   []Observation          `json:"observations,omitempty"`
	AllResults     []ParamScore           `json:"all_results,omitempty"`
	OOSValidation  *OOSValidation         `json:"oos_validation,omitempty"`
}

// ParetoPoint represents a point on the Pareto front (return vs risk).
type ParetoPoint struct {
	Params         map[string]any `json:"params"`
	ReturnPct      float64        `json:"return_pct"`
	MaxDrawdownPct float64        `json:"max_drawdown_pct"`
	SharpeRatio    float64        `json:"sharpe_ratio"`
	Score          float64        `json:"score"`
}

// GenerationSnapshot captures the state of a DE generation.
type GenerationSnapshot struct {
	Generation int             `json:"generation"`
	BestScore  float64         `json:"best_score"`
	AvgScore   float64         `json:"avg_score"`
	BestParams map[string]any  `json:"best_params,omitempty"`
}

// ParamScore links params to a score for post-analysis.
type ParamScore struct {
	Params map[string]any `json:"params"`
	Score  float64        `json:"score"`
}

// RunExperiment executes the full experiment pipeline.
func RunExperiment(req ExperimentRequest) (*ExperimentResult, error) {
	start := time.Now()
	result := &ExperimentResult{
		ExperimentID: fmt.Sprintf("exp_%d", time.Now().UnixMilli()),
		Status:       "running",
	}

	// Parse param space from code if not provided
	space := req.ParamSpace
	if len(space) == 0 && req.Code != "" {
		parsed := indicator.ParseSource(req.Code)
		space = FromIndicatorParseResult(parsed)
	}
	if len(space) == 0 {
		return nil, fmt.Errorf("no tunable parameters found")
	}

	// Parse klines to bars
	bars := klinesToBars(req.Klines, req.Symbol, req.Interval)
	if len(bars) == 0 {
		return nil, fmt.Errorf("no bar data provided")
	}

	// OOS split
	isBars, oosBars := OOSSplit(bars, req.OOSRatio)
	if len(oosBars) == 0 {
		isBars = bars
	}

	// Backtest config
	btCfg := req.BacktestConfig
	if btCfg.InitialBalance <= 0 {
		btCfg.InitialBalance = 10000
	}

	// Score config
	scoreCfg := req.ScoreConfig
	if scoreCfg.Weights == nil {
		scoreCfg = DefaultScoreConfig()
	}

	// Fitness function with caching
	fitnessCache := make(map[string]float64)
	fitnessFn := func(params map[string]any) float64 {
		key := paramsKey(params)
		if cached, ok := fitnessCache[key]; ok {
			return cached
		}

		var score float64
		if len(oosBars) > 0 {
			score = FitnessFromIndicatorOOS(req.Code, req.Symbol, params, isBars, oosBars, btCfg, scoreCfg)
		} else {
			score = FitnessFromIndicator(req.Code, req.Symbol, params, isBars, btCfg, scoreCfg)
		}
		fitnessCache[key] = score
		return score
	}

	// Run optimizer
	var bestParams map[string]any
	var bestVector []float64
	var bestScore float64

	switch req.Optimizer {
	case "de", "":
		deCfg := req.DEConfig
		if deCfg.PopulationSize <= 0 {
			deCfg = DefaultDEConfig()
		}
		pop, best := DifferentialEvolution(space, deCfg, fitnessFn)
		if best != nil {
			bestParams = best.Params
			bestVector = best.Vector
			bestScore = best.Score
		}
		result.Generations = captureDEGenerations(pop, space)
		result.ParetoFront = buildParetoFront(pop)

	case "tpe":
		tpeCfg := req.TPEConfig
		if tpeCfg.MaxTrials <= 0 {
			tpeCfg = DefaultTPEConfig()
		}
		obs, best := TPE(space, tpeCfg, fitnessFn)
		if best != nil {
			bestParams = best.Params
			bestVector = best.Vector
			bestScore = best.Score
		}
		result.Observations = obs
		result.ParetoFront = buildParetoFrontFromObs(obs)

	default:
		return nil, fmt.Errorf("unknown optimizer: %s", req.Optimizer)
	}

	result.BestParams = bestParams
	result.BestVector = bestVector
	result.BestScore = bestScore
	result.Status = "completed"
	result.DurationMs = time.Since(start).Milliseconds()

	// Final validation run with best params
	if bestParams != nil {
		strategy := NewIndicatorStrategy(req.Code, req.Symbol, bestParams)
		if err := strategy.Precompute(bars); err == nil {
			if len(oosBars) > 0 {
				result.OOSValidation = ValidateOOS(isBars, oosBars, strategy, btCfg, scoreCfg)
				result.ISResult = result.OOSValidation.ISResult
				result.OOSResult = result.OOSValidation.OOSResult
				result.ISScore = result.OOSValidation.ISScore
				result.OOSScore = result.OOSValidation.OOSScore
			} else {
				runner := backtest.NewRunner(btCfg)
				runner.LoadBars(req.Symbol, bars)
				if r, err := runner.Run(strategy); err == nil {
					result.ISResult = r
					result.ISScore = Score(r, nil, scoreCfg)
				}
			}
		}
	}

	// Collect all results for analysis
	for key, score := range fitnessCache {
		var params map[string]any
		json.Unmarshal([]byte(key), &params)
		result.AllResults = append(result.AllResults, ParamScore{Params: params, Score: score})
	}

	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────

func klinesToBars(klines []map[string]any, symbol, interval string) []model.Bar {
	var bars []model.Bar
	for _, k := range klines {
		b := model.Bar{Symbol: symbol, Interval: interval}
		if v, ok := k["open"].(float64); ok {
			b.Open = v
		} else if v, ok := k["open"].(json.Number); ok {
			b.Open, _ = v.Float64()
		}
		if v, ok := k["high"].(float64); ok {
			b.High = v
		} else if v, ok := k["high"].(json.Number); ok {
			b.High, _ = v.Float64()
		}
		if v, ok := k["low"].(float64); ok {
			b.Low = v
		} else if v, ok := k["low"].(json.Number); ok {
			b.Low, _ = v.Float64()
		}
		if v, ok := k["close"].(float64); ok {
			b.Close = v
		} else if v, ok := k["close"].(json.Number); ok {
			b.Close, _ = v.Float64()
		}
		if v, ok := k["volume"].(float64); ok {
			b.Volume = v
		} else if v, ok := k["volume"].(json.Number); ok {
			b.Volume, _ = v.Float64()
		}
		if v, ok := k["time"].(float64); ok {
			b.Time = int64(v)
		} else if v, ok := k["timestamp"].(float64); ok {
			b.Time = int64(v)
		} else if v, ok := k["time"].(int64); ok {
			b.Time = v
		} else if v, ok := k["timestamp"].(int64); ok {
			b.Time = v
		}
		bars = append(bars, b)
	}
	return bars
}

func paramsKey(params map[string]any) string {
	b, _ := json.Marshal(params)
	return string(b)
}

func captureDEGenerations(pop []Individual, space ParamSpace) []GenerationSnapshot {
	if len(pop) == 0 {
		return nil
	}
	var sum float64
	var best *Individual
	for _, ind := range pop {
		if ind.Valid {
			sum += ind.Score
			if best == nil || ind.Score > best.Score {
				cp := ind
				best = &cp
			}
		}
	}
	gs := GenerationSnapshot{
		Generation: 0,
		BestScore:  0,
		AvgScore:   0,
	}
	if best != nil {
		gs.BestScore = best.Score
		gs.BestParams = space.ToMap(best.Vector)
	}
	if len(pop) > 0 {
		gs.AvgScore = sum / float64(len(pop))
	}
	return []GenerationSnapshot{gs}
}

func buildParetoFront(pop []Individual) []ParetoPoint {
	var points []ParetoPoint
	for _, ind := range pop {
		if !ind.Valid {
			continue
		}
		// We need actual backtest results for Pareto, but we only have scores here.
		// For now, approximate with score as a proxy.
		points = append(points, ParetoPoint{
			Params: ind.Params,
			Score:  ind.Score,
		})
	}
	return points
}

func buildParetoFrontFromObs(obs []Observation) []ParetoPoint {
	var points []ParetoPoint
	for _, o := range obs {
		points = append(points, ParetoPoint{
			Params: o.Params,
			Score:  o.Score,
		})
	}
	return points
}

// IsParetoDominated checks if a is dominated by b (for minimization of risk, maximization of return).
func IsParetoDominated(a, b ParetoPoint) bool {
	// a is dominated by b if b is better or equal in all objectives and strictly better in at least one.
	// Objectives: maximize return, minimize drawdown, maximize sharpe
	return (b.ReturnPct >= a.ReturnPct && b.MaxDrawdownPct <= a.MaxDrawdownPct && b.SharpeRatio >= a.SharpeRatio) &&
		(b.ReturnPct > a.ReturnPct || b.MaxDrawdownPct < a.MaxDrawdownPct || b.SharpeRatio > a.SharpeRatio)
}

// FilterParetoFront returns non-dominated points.
func FilterParetoFront(points []ParetoPoint) []ParetoPoint {
	if len(points) <= 1 {
		return points
	}
	var front []ParetoPoint
	for i, a := range points {
		dominated := false
		for j, b := range points {
			if i == j {
				continue
			}
			if IsParetoDominated(a, b) {
				dominated = true
				break
			}
		}
		if !dominated {
			front = append(front, a)
		}
	}
	return front
}

// SensitivityAnalysis performs one-at-a-time sensitivity on the best params.
func SensitivityAnalysis(
	code, symbol string,
	baseParams map[string]any,
	space ParamSpace,
	bars []model.Bar,
	btCfg backtest.RunnerConfig,
	scoreCfg ScoreConfig,
) map[string][]ParamScore {
	result := make(map[string][]ParamScore)

	for _, bound := range space {
		if bound.Type != ParamInt && bound.Type != ParamFloat {
			continue
		}

		var scores []ParamScore
		steps := 10
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			v := bound.Min + t*(bound.Max-bound.Min)

			params := make(map[string]any)
			for k, val := range baseParams {
				params[k] = val
			}
			if bound.Type == ParamInt {
				params[bound.Name] = int(round(v))
			} else {
				params[bound.Name] = roundToStep(v, bound.Step)
			}

			score := FitnessFromIndicator(code, symbol, params, bars, btCfg, scoreCfg)
			scores = append(scores, ParamScore{Params: params, Score: score})
		}
		result[bound.Name] = scores
	}

	return result
}
