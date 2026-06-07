package experiment

import (
	"encoding/json"
	"fmt"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Indicator Backtest Adapter ────────────────────────────────────

// IndicatorStrategy wraps indicator code into a backtest.BacktestStrategy.
type IndicatorStrategy struct {
	code   string
	symbol string
	params map[string]any
	client *indicator.SandboxConfig

	buySignals  []bool
	sellSignals []bool
}

// NewIndicatorStrategy creates a strategy from indicator code.
func NewIndicatorStrategy(code, symbol string, params map[string]any) *IndicatorStrategy {
	return &IndicatorStrategy{
		code:   code,
		symbol: symbol,
		params: params,
		client: indicator.DefaultSandboxConfig(),
	}
}

// Precompute executes the indicator in the sandbox and extracts signals.
func (s *IndicatorStrategy) Precompute(bars []model.Bar) error {
	// Convert bars to df_json
	dfJSON := make([]map[string]any, len(bars))
	for i, b := range bars {
		dfJSON[i] = map[string]any{
			"open":   b.Open,
			"high":   b.High,
			"low":    b.Low,
			"close":  b.Close,
			"volume": b.Volume,
		}
	}

	resp, err := s.client.Execute(s.code, s.params, dfJSON)
	if err != nil {
		return fmt.Errorf("sandbox execution failed: %w", err)
	}
	if resp == nil || !resp.Success {
		return fmt.Errorf("sandbox execution error: %s", resp.Msg)
	}

	// Extract signals from output
	outputJSON, _ := json.Marshal(resp.Output)
	output, hints := indicator.ValidateOutputJSON(string(outputJSON))
	_ = hints

	s.buySignals = make([]bool, len(bars))
	s.sellSignals = make([]bool, len(bars))

	for _, sig := range output.Signals {
		if len(sig.Data) != len(bars) {
			continue
		}
		for i, v := range sig.Data {
			if v == nil {
				continue
			}
			if sig.Type == "buy" {
				s.buySignals[i] = true
			} else if sig.Type == "sell" {
				s.sellSignals[i] = true
			}
		}
	}

	return nil
}

func (s *IndicatorStrategy) Name() string { return "indicator" }
func (s *IndicatorStrategy) Symbol() string { return s.symbol }

func (s *IndicatorStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	idx := state.BarIndex
	if idx < 0 || idx >= len(s.buySignals) {
		return nil, nil
	}

	// Edge-triggered signals
	if s.buySignals[idx] {
		// Check if not already in position
		if state.Position == nil || state.Position.IsClosed {
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "LONG",
				Reason:    "indicator_buy",
			}, nil
		}
	}

	if s.sellSignals[idx] {
		if state.Position != nil && !state.Position.IsClosed {
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Reason:    "indicator_sell",
			}, nil
		}
	}

	return nil, nil
}

func (s *IndicatorStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}

// ── Fitness Function ──────────────────────────────────────────────

// FitnessFromIndicator runs a full backtest for a parameter set and returns a score.
func FitnessFromIndicator(
	code, symbol string,
	params map[string]any,
	bars []model.Bar,
	cfg backtest.RunnerConfig,
	scoreCfg ScoreConfig,
) float64 {
	strategy := NewIndicatorStrategy(code, symbol, params)
	if err := strategy.Precompute(bars); err != nil {
		return -1e6 // invalid
	}

	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)
	result, err := runner.Run(strategy)
	if err != nil {
		return -1e6
	}

	score := Score(result, nil, scoreCfg)
	return score.TotalScore
}

// FitnessFromIndicatorOOS runs IS+OOS backtest and returns combined score.
func FitnessFromIndicatorOOS(
	code, symbol string,
	params map[string]any,
	isBars, oosBars []model.Bar,
	cfg backtest.RunnerConfig,
	scoreCfg ScoreConfig,
) float64 {
	strategy := NewIndicatorStrategy(code, symbol, params)
	if err := strategy.Precompute(append(isBars, oosBars...)); err != nil {
		return -1e6
	}

	// IS
	isRunner := backtest.NewRunner(cfg)
	isRunner.LoadBars(symbol, isBars)
	isResult, err := isRunner.Run(strategy)
	if err != nil {
		return -1e6
	}

	// OOS
	oosRunner := backtest.NewRunner(cfg)
	oosRunner.LoadBars(symbol, oosBars)
	oosResult, err := oosRunner.Run(strategy)
	if err != nil {
		return -1e6
	}

	score := Score(isResult, oosResult, scoreCfg)
	return score.TotalScore
}
