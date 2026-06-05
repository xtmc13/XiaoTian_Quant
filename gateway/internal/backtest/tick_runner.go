package backtest

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Slippage Model ─────────────────────────────────────────────

// SlippageModel defines how slippage is applied during tick backtests.
type SlippageModel string

const (
	SlippageFixed  SlippageModel = "fixed"
	SlippageVolume SlippageModel = "volume"
)

// ── Config & Result ──────────────────────────────────────────────

// TickBacktestConfig configures the tick backtest runner.
type TickBacktestConfig struct {
	InitialBalance float64       `json:"initial_balance"`
	Commission     float64       `json:"commission"`
	SlippageModel  SlippageModel `json:"slippage_model"`
	SlippageBps    float64       `json:"slippage_bps"`    // for fixed model (e.g. 5 = 5 bps)
	SlippageFactor float64       `json:"slippage_factor"` // for volume model
	StartTime      int64         `json:"start_time"`
	EndTime        int64         `json:"end_time"`
}

// TickBacktestResult holds the output of a tick backtest run.
type TickBacktestResult struct {
	TotalReturn    float64       `json:"total_return"`
	TotalReturnPct float64       `json:"total_return_pct"`
	MaxDrawdown    float64       `json:"max_drawdown"`
	MaxDrawdownPct float64       `json:"max_drawdown_pct"`
	SharpeRatio    float64       `json:"sharpe_ratio"`
	SortinoRatio   float64       `json:"sortino_ratio"`
	CalmarRatio    float64       `json:"calmar_ratio"`
	WinRate        float64       `json:"win_rate"`
	ProfitFactor   float64       `json:"profit_factor"`
	TotalTrades    int           `json:"total_trades"`
	WinningTrades  int           `json:"winning_trades"`
	LosingTrades   int           `json:"losing_trades"`
	AvgWin         float64       `json:"avg_win"`
	AvgLoss        float64       `json:"avg_loss"`
	BestTrade      float64       `json:"best_trade"`
	WorstTrade     float64       `json:"worst_trade"`
	EquityCurve    []EquityPoint `json:"equity_curve"`
	Trades         []Position    `json:"trades"`
	DurationMs     int64         `json:"duration_ms"`
	TicksProcessed int           `json:"ticks_processed"`
}

// ── Tick Backtest Runner ─────────────────────────────────────────

// TickBacktestRunner executes event-driven backtests on tick data.
type TickBacktestRunner struct {
	cfg        TickBacktestConfig
	ticks      map[string][]model.Tick
	trades     []Position
	equity     []EquityPoint
	equityPeak float64
	mu         sync.Mutex
}

// NewTickBacktestRunner creates a new tick backtest runner.
func NewTickBacktestRunner(cfg TickBacktestConfig) *TickBacktestRunner {
	if cfg.InitialBalance <= 0 {
		cfg.InitialBalance = 100000
	}
	if cfg.SlippageModel == "" {
		cfg.SlippageModel = SlippageFixed
	}
	if cfg.SlippageBps <= 0 {
		cfg.SlippageBps = 5 // 5 bps default
	}
	return &TickBacktestRunner{
		cfg:        cfg,
		ticks:      make(map[string][]model.Tick),
		equityPeak: cfg.InitialBalance,
	}
}

// LoadTicks loads tick data for a symbol.
func (r *TickBacktestRunner) LoadTicks(symbol string, ticks []model.Tick) {
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].Timestamp < ticks[j].Timestamp })
	r.ticks[symbol] = ticks
}

// Run executes the backtest using tick data for the given strategy.
func (r *TickBacktestRunner) Run(strategy BacktestStrategy) (*TickBacktestResult, error) {
	symbol := strategy.Symbol()
	ticks, ok := r.ticks[symbol]
	if !ok || len(ticks) == 0 {
		return nil, fmt.Errorf("no tick data for symbol %s", symbol)
	}

	if r.cfg.StartTime > 0 {
		startIdx := sort.Search(len(ticks), func(i int) bool { return ticks[i].Timestamp >= r.cfg.StartTime })
		ticks = ticks[startIdx:]
	}
	if r.cfg.EndTime > 0 {
		endIdx := sort.Search(len(ticks), func(i int) bool { return ticks[i].Timestamp >= r.cfg.EndTime })
		if endIdx < len(ticks) {
			ticks = ticks[:endIdx+1]
		}
	}

	if len(ticks) < 2 {
		return nil, fmt.Errorf("insufficient ticks (%d)", len(ticks))
	}

	r.mu.Lock()
	r.trades = nil
	r.equity = nil
	r.equityPeak = r.cfg.InitialBalance
	r.mu.Unlock()

	cash := r.cfg.InitialBalance
	var position *Position
	equityPoints := []EquityPoint{{Timestamp: ticks[0].Timestamp, Equity: cash, AvailableCash: cash}}

	startTime := time.Now()

	state := &StrategyState{
		Cash:       cash,
		Equity:     cash,
		Bars:       nil,
		Indicators: make(map[string]float64),
	}

	for i := 0; i < len(ticks); i++ {
		tick := ticks[i]
		state.Equity = cash
		if position != nil && !position.IsClosed {
			state.Equity += position.Quantity * tick.Last
		}
		state.Cash = cash
		state.Position = position

		signal, err := strategy.OnTick(tick, state)
		if err != nil {
			continue
		}
		if signal == nil {
			equityPoints = append(equityPoints, EquityPoint{
				Timestamp: tick.Timestamp, Equity: state.Equity, AvailableCash: cash,
			})
			continue
		}

		execPrice := r.applySlippage(tick, signal.Direction)
		if execPrice == 0 {
			continue
		}

		commissionCost := execPrice * r.cfg.Commission
		if position != nil && !position.IsClosed {
			// closing existing position incurs commission on both sides
			commissionCost *= 2
		}

		switch signal.Direction {
		case "LONG":
			if position != nil && !position.IsClosed {
				continue
			}
			position = &Position{
				Symbol:     symbol,
				Side:       model.SideBuy,
				Quantity:   r.cfg.InitialBalance * 0.02 / execPrice,
				EntryPrice: execPrice,
				EntryTime:  tick.Timestamp,
			}
			cash -= position.Quantity*execPrice + commissionCost
			state.TradeCount++

		case "SHORT":
			if position != nil && !position.IsClosed {
				continue
			}
			position = &Position{
				Symbol:     symbol,
				Side:       model.SideSell,
				Quantity:   r.cfg.InitialBalance * 0.02 / execPrice,
				EntryPrice: execPrice,
				EntryTime:  tick.Timestamp,
			}
			cash -= position.Quantity*execPrice + commissionCost
			state.TradeCount++

		case "CLOSE":
			if position == nil || position.IsClosed {
				continue
			}
			position.ExitPrice = execPrice
			position.ExitTime = tick.Timestamp
			position.IsClosed = true
			position.ExitReason = signal.Reason

			if position.Side == model.SideBuy {
				position.RealizedPnL = position.Quantity * (execPrice - position.EntryPrice)
			} else {
				position.RealizedPnL = position.Quantity * (position.EntryPrice - execPrice)
			}
			position.RealizedPnL -= commissionCost
			cash += position.Quantity*execPrice + position.RealizedPnL

			r.mu.Lock()
			r.trades = append(r.trades, *position)
			r.mu.Unlock()
		}

		equity := cash
		if position != nil && !position.IsClosed {
			equity += position.Quantity * tick.Last
		}
		equityPoints = append(equityPoints, EquityPoint{
			Timestamp: tick.Timestamp, Equity: equity, AvailableCash: cash,
		})

		r.mu.Lock()
		if equity > r.equityPeak {
			r.equityPeak = equity
		}
		r.mu.Unlock()
	}

	// Close any open position at the last tick
	if position != nil && !position.IsClosed {
		lastTick := ticks[len(ticks)-1]
		position.ExitPrice = lastTick.Last
		position.ExitTime = lastTick.Timestamp
		position.IsClosed = true
		position.ExitReason = "end_of_test"

		if position.Side == model.SideBuy {
			position.RealizedPnL = position.Quantity * (lastTick.Last - position.EntryPrice)
		} else {
			position.RealizedPnL = position.Quantity * (position.EntryPrice - lastTick.Last)
		}
		cash += position.Quantity*lastTick.Last + position.RealizedPnL

		r.mu.Lock()
		r.trades = append(r.trades, *position)
		r.mu.Unlock()
	}

	r.mu.Lock()
	r.equity = equityPoints
	positions := make([]Position, len(r.trades))
	copy(positions, r.trades)
	r.mu.Unlock()

	return r.buildResult(positions, equityPoints, time.Since(startTime).Milliseconds(), len(ticks)), nil
}

func (r *TickBacktestRunner) applySlippage(tick model.Tick, direction string) float64 {
	basePrice := tick.Last
	if basePrice == 0 {
		basePrice = (tick.Bid + tick.Ask) / 2
	}
	if basePrice == 0 {
		return 0
	}

	switch r.cfg.SlippageModel {
	case SlippageVolume:
		// Volume-based: larger volume = less slippage
		volSlip := r.cfg.SlippageFactor / (tick.Volume + 1.0)
		if direction == "LONG" {
			return basePrice * (1.0 + volSlip)
		} else if direction == "SHORT" || direction == "CLOSE" {
			return basePrice * (1.0 - volSlip)
		}
	default: // SlippageFixed
		bps := r.cfg.SlippageBps / 10000.0
		noise := rand.Float64() * bps // 0-1x bps random noise
		if direction == "LONG" {
			return basePrice * (1.0 + bps + noise)
		} else if direction == "SHORT" || direction == "CLOSE" {
			return basePrice * (1.0 - bps - noise)
		}
	}
	return basePrice
}

func (r *TickBacktestRunner) buildResult(positions []Position, equity []EquityPoint, durationMs int64, ticksProcessed int) *TickBacktestResult {
	result := &TickBacktestResult{
		EquityCurve:    equity,
		Trades:         positions,
		TotalTrades:    len(positions),
		DurationMs:     durationMs,
		TicksProcessed: ticksProcessed,
	}

	if len(equity) == 0 {
		return result
	}

	finalEquity := equity[len(equity)-1].Equity
	result.TotalReturn = finalEquity - r.cfg.InitialBalance
	result.TotalReturnPct = (finalEquity - r.cfg.InitialBalance) / r.cfg.InitialBalance * 100

	var wins, losses []float64
	for _, pos := range positions {
		if pos.RealizedPnL > 0 {
			wins = append(wins, pos.RealizedPnL)
		} else if pos.RealizedPnL < 0 {
			losses = append(losses, pos.RealizedPnL)
		}
	}
	result.WinningTrades = len(wins)
	result.LosingTrades = len(losses)

	if result.TotalTrades > 0 {
		result.WinRate = float64(len(wins)) / float64(result.TotalTrades) * 100
	}

	if len(wins) > 0 {
		result.AvgWin = sum(wins) / float64(len(wins))
		result.BestTrade = max(wins)
	}
	if len(losses) > 0 {
		result.AvgLoss = sum(losses) / float64(len(losses))
		result.WorstTrade = min(losses)
	}

	grossProfit := sum(wins)
	grossLoss := math.Abs(sum(losses))
	if grossLoss > 0 {
		result.ProfitFactor = grossProfit / grossLoss
	} else if grossProfit > 0 {
		result.ProfitFactor = math.Inf(1)
	}

	peak := r.cfg.InitialBalance
	maxDD := 0.0
	for _, pt := range equity {
		if pt.Equity > peak {
			peak = pt.Equity
		}
		dd := (peak - pt.Equity) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
	}
	result.MaxDrawdownPct = maxDD
	result.MaxDrawdown = peak * maxDD / 100

	returns := make([]float64, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Equity > 0 {
			returns[i-1] = (equity[i].Equity - equity[i-1].Equity) / equity[i-1].Equity
		}
	}

	result.SharpeRatio = sharpeRatio(returns, 0.02)
	result.SortinoRatio = sortinoRatio(returns, 0.02)
	if maxDD > 0 {
		result.CalmarRatio = result.TotalReturnPct / maxDD
	}

	return result
}
