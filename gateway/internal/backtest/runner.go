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

// ── Backtest Runner ──

// Runner executes event-driven backtests on historical data.
type Runner struct {
	initialBalance float64
	commission     float64
	slippage       float64
	latencyMs      int64
	startTime      int64
	endTime        int64

	bars       map[string][]model.Bar     // symbol -> bars
	ticks      map[string][]model.Tick    // symbol -> ticks
	trades     []model.TradeData
	orders     []model.OrderData
	positions  []Position
	equity     []EquityPoint
	equityPeak float64

	mu sync.Mutex
}

// Position during backtest.
type Position struct {
	Symbol        string
	Side          model.OrderSide
	Quantity      float64
	EntryPrice    float64
	ExitPrice     float64
	EntryTime     int64
	ExitTime      int64
	RealizedPnL   float64
	IsClosed      bool
	ExitReason    string
}

// EquityPoint is a snapshot of portfolio value at a point in time.
type EquityPoint struct {
	Timestamp     int64   `json:"timestamp"`
	Equity        float64 `json:"equity"`
	AvailableCash float64 `json:"available_cash"`
	PositionValue float64 `json:"position_value"`
}

// RunnerConfig configures the backtest runner.
type RunnerConfig struct {
	InitialBalance float64 `json:"initial_balance"`
	Commission     float64 `json:"commission"`      // e.g., 0.001 for 0.1%
	Slippage       float64 `json:"slippage"`        // e.g., 0.0005 for 0.05%
	LatencyMs      int64   `json:"latency_ms"`
	StartTime      int64   `json:"start_time"`      // unix ms, 0 = all data
	EndTime        int64   `json:"end_time"`
}

func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		InitialBalance: 100000,
		Commission:     0.001,
		Slippage:       0.0005,
		LatencyMs:      100,
	}
}

func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.InitialBalance <= 0 {
		cfg.InitialBalance = 100000
	}
	return &Runner{
		initialBalance: cfg.InitialBalance,
		commission:     cfg.Commission,
		slippage:       cfg.Slippage,
		latencyMs:      cfg.LatencyMs,
		startTime:      cfg.StartTime,
		endTime:        cfg.EndTime,
		bars:           make(map[string][]model.Bar),
		ticks:          make(map[string][]model.Tick),
		equityPeak:     cfg.InitialBalance,
	}
}

// LoadBars loads OHLCV bar data for a symbol.
func (r *Runner) LoadBars(symbol string, bars []model.Bar) {
	// Sort by time
	sort.Slice(bars, func(i, j int) bool { return bars[i].Time < bars[j].Time })
	r.bars[symbol] = bars
}

// LoadTicks loads tick data for a symbol.
func (r *Runner) LoadTicks(symbol string, ticks []model.Tick) {
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].Timestamp < ticks[j].Timestamp })
	r.ticks[symbol] = ticks
}

// ── Strategy for Backtesting ──

// BacktestStrategy is the interface strategies must implement for backtesting.
type BacktestStrategy interface {
	Name() string
	Symbol() string
	OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error)
	OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error)
}

// StrategyState provides current state to the strategy during backtest.
type StrategyState struct {
	Cash            float64
	Position        *Position
	Equity          float64
	BarIndex        int
	Bars            []model.Bar
	Indicators      map[string]float64
	TradeCount      int
}

// ── Run ──

// RunResult holds the output of a backtest run.
type RunResult struct {
	TotalReturn    float64         `json:"total_return"`
	TotalReturnPct float64         `json:"total_return_pct"`
	MaxDrawdown    float64         `json:"max_drawdown"`
	MaxDrawdownPct float64         `json:"max_drawdown_pct"`
	SharpeRatio    float64         `json:"sharpe_ratio"`
	SortinoRatio   float64         `json:"sortino_ratio"`
	CalmarRatio    float64         `json:"calmar_ratio"`
	WinRate        float64         `json:"win_rate"`
	ProfitFactor   float64         `json:"profit_factor"`
	TotalTrades    int             `json:"total_trades"`
	WinningTrades  int             `json:"winning_trades"`
	LosingTrades   int             `json:"losing_trades"`
	AvgWin         float64         `json:"avg_win"`
	AvgLoss        float64         `json:"avg_loss"`
	BestTrade      float64         `json:"best_trade"`
	WorstTrade     float64         `json:"worst_trade"`
	EquityCurve    []EquityPoint   `json:"equity_curve"`
	Trades         []Position      `json:"trades"`
	Orders         []model.OrderData `json:"orders"`
	DurationMs     int64           `json:"duration_ms"`
}

// Run executes the backtest using bar data for the given strategy.
func (r *Runner) Run(strategy BacktestStrategy) (*RunResult, error) {
	symbol := strategy.Symbol()
	bars, ok := r.bars[symbol]
	if !ok || len(bars) == 0 {
		return nil, fmt.Errorf("no bar data for symbol %s", symbol)
	}

	if r.startTime > 0 {
		startIdx := sort.Search(len(bars), func(i int) bool { return bars[i].Time >= r.startTime })
		bars = bars[startIdx:]
	}
	if r.endTime > 0 {
		endIdx := sort.Search(len(bars), func(i int) bool { return bars[i].Time >= r.endTime })
		if endIdx < len(bars) {
			bars = bars[:endIdx+1]
		}
	}

	if len(bars) < 2 {
		return nil, fmt.Errorf("insufficient bars (%d)", len(bars))
	}

	r.mu.Lock()
	r.trades = nil
	r.orders = nil
	r.positions = nil
	r.equity = nil
	r.equityPeak = r.initialBalance
	r.mu.Unlock()

	cash := r.initialBalance
	var position *Position
	equityPoints := []EquityPoint{{Timestamp: bars[0].Time, Equity: cash, AvailableCash: cash}}

	startTime := time.Now()

	state := &StrategyState{
		Cash:       cash,
		Equity:     cash,
		Bars:       make([]model.Bar, 0, len(bars)),
		Indicators: make(map[string]float64),
	}

	for i := 0; i < len(bars); i++ {
		bar := bars[i]
		state.Bars = append(state.Bars, bar)
		state.BarIndex = i
		state.Equity = cash
		if position != nil && !position.IsClosed {
			state.Equity += position.Quantity * bar.Close * (1 - r.commission)
		}
		state.Cash = cash
		state.Position = position

		signal, err := strategy.OnBar(bar, state)
		if err != nil {
			continue
		}
		if signal == nil {
			equityPoints = append(equityPoints, EquityPoint{
				Timestamp: bar.Time, Equity: state.Equity, AvailableCash: cash,
			})
			continue
		}

		execPrice := r.applySlippage(bar.Close, signal.Direction)
		commissionCost := execPrice * r.initialBalance * r.commission / 10000

		switch signal.Direction {
		case "LONG":
			if position != nil && !position.IsClosed {
				continue // already in a position
			}
			position = &Position{
				Symbol:     symbol,
				Side:       model.SideBuy,
				Quantity:   r.initialBalance * 0.02 / execPrice, // 2% risk per trade
				EntryPrice: execPrice,
				EntryTime:  bar.Time,
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
				Quantity:   r.initialBalance * 0.02 / execPrice,
				EntryPrice: execPrice,
				EntryTime:  bar.Time,
			}
			cash -= position.Quantity*execPrice + commissionCost
			state.TradeCount++

		case "CLOSE":
			if position == nil || position.IsClosed {
				continue
			}
			position.ExitPrice = execPrice
			position.ExitTime = bar.Time
			position.IsClosed = true
			position.ExitReason = signal.Reason

			if position.Side == model.SideBuy {
				position.RealizedPnL = position.Quantity * (execPrice - position.EntryPrice)
			} else {
				position.RealizedPnL = position.Quantity * (position.EntryPrice - execPrice)
			}
			position.RealizedPnL -= commissionCost * 2
			cash += position.Quantity*execPrice + position.RealizedPnL

			r.mu.Lock()
			r.positions = append(r.positions, *position)
			r.mu.Unlock()
		}

		equity := cash
		if position != nil && !position.IsClosed {
			equity += position.Quantity * bar.Close
		}
		equityPoints = append(equityPoints, EquityPoint{
			Timestamp: bar.Time, Equity: equity, AvailableCash: cash,
		})

		r.mu.Lock()
		if equity > r.equityPeak {
			r.equityPeak = equity
		}
		r.mu.Unlock()
	}

	// Close any open position at the last bar
	if position != nil && !position.IsClosed {
		lastBar := bars[len(bars)-1]
		position.ExitPrice = lastBar.Close
		position.ExitTime = lastBar.Time
		position.IsClosed = true
		position.ExitReason = "end_of_test"

		if position.Side == model.SideBuy {
			position.RealizedPnL = position.Quantity * (lastBar.Close - position.EntryPrice)
		} else {
			position.RealizedPnL = position.Quantity * (position.EntryPrice - lastBar.Close)
		}
		cash += position.Quantity*lastBar.Close + position.RealizedPnL

		r.mu.Lock()
		r.positions = append(r.positions, *position)
		r.mu.Unlock()
	}

	r.mu.Lock()
	r.equity = equityPoints
	positions := make([]Position, len(r.positions))
	copy(positions, r.positions)
	r.mu.Unlock()

	return r.buildResult(positions, equityPoints, time.Since(startTime).Milliseconds()), nil
}

// RunWithTicks executes the backtest using tick data.
func (r *Runner) RunWithTicks(strategy BacktestStrategy) (*RunResult, error) {
	symbol := strategy.Symbol()
	ticks, ok := r.ticks[symbol]
	if !ok || len(ticks) == 0 {
		return nil, fmt.Errorf("no tick data for symbol %s", symbol)
	}

	cash := r.initialBalance
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

		execPrice := tick.Last
		switch signal.Direction {
		case "LONG":
			if position != nil && !position.IsClosed {
				continue
			}
			position = &Position{
				Symbol:     symbol,
				Side:       model.SideBuy,
				Quantity:   r.initialBalance * 0.02 / execPrice,
				EntryPrice: execPrice,
				EntryTime:  tick.Timestamp,
			}
			cash -= position.Quantity * execPrice
			state.TradeCount++
		case "CLOSE":
			if position == nil || position.IsClosed {
				continue
			}
			position.ExitPrice = execPrice
			position.ExitTime = tick.Timestamp
			position.IsClosed = true

			if position.Side == model.SideBuy {
				position.RealizedPnL = position.Quantity * (execPrice - position.EntryPrice)
			} else {
				position.RealizedPnL = position.Quantity * (position.EntryPrice - execPrice)
			}
			cash += position.Quantity*execPrice + position.RealizedPnL

			r.mu.Lock()
			r.positions = append(r.positions, *position)
			r.mu.Unlock()
		}

		equity := cash
		if position != nil && !position.IsClosed {
			equity += position.Quantity * tick.Last
		}
		equityPoints = append(equityPoints, EquityPoint{
			Timestamp: tick.Timestamp, Equity: equity, AvailableCash: cash,
		})
	}

	r.mu.Lock()
	r.equity = equityPoints
	positions := make([]Position, len(r.positions))
	copy(positions, r.positions)
	r.mu.Unlock()

	return r.buildResult(positions, equityPoints, time.Since(startTime).Milliseconds()), nil
}

func (r *Runner) applySlippage(price float64, direction string) float64 {
	slipFactor := 1.0
	if direction == "LONG" {
		slipFactor = 1.0 + r.slippage + rand.Float64()*r.slippage*2
	} else if direction == "SHORT" || direction == "CLOSE" {
		slipFactor = 1.0 - r.slippage - rand.Float64()*r.slippage*2
	}
	return price * slipFactor
}

func (r *Runner) buildResult(positions []Position, equity []EquityPoint, durationMs int64) *RunResult {
	result := &RunResult{
		EquityCurve: equity,
		Trades:      positions,
		TotalTrades: len(positions),
		DurationMs:  durationMs,
	}

	if len(equity) == 0 {
		return result
	}

	finalEquity := equity[len(equity)-1].Equity
	result.TotalReturn = finalEquity - r.initialBalance
	result.TotalReturnPct = (finalEquity - r.initialBalance) / r.initialBalance * 100

	// Win/Loss stats
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
	} else {
		result.BestTrade = 0
	}
	if len(losses) > 0 {
		result.AvgLoss = sum(losses) / float64(len(losses))
		result.WorstTrade = min(losses)
	} else {
		result.WorstTrade = 0
	}

	// Profit factor
	grossProfit := sum(wins)
	grossLoss := math.Abs(sum(losses))
	if grossLoss > 0 {
		result.ProfitFactor = grossProfit / grossLoss
	} else if grossProfit > 0 {
		result.ProfitFactor = math.Inf(1)
	}

	// Drawdown
	peak := r.initialBalance
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

	// Returns series for Sharpe/Sortino
	returns := make([]float64, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Equity > 0 {
			returns[i-1] = (equity[i].Equity - equity[i-1].Equity) / equity[i-1].Equity
		}
	}

	result.SharpeRatio = sharpeRatio(returns, 0.02) // assume 2% risk-free
	result.SortinoRatio = sortinoRatio(returns, 0.02)
	if maxDD > 0 {
		result.CalmarRatio = result.TotalReturnPct / maxDD
	}

	return result
}

// GetEquityCurve returns the equity curve.
func (r *Runner) GetEquityCurve() []EquityPoint {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]EquityPoint, len(r.equity))
	copy(result, r.equity)
	return result
}

// ── Helpers ──

func sum(vals []float64) float64 {
	s := 0.0
	for _, v := range vals {
		s += v
	}
	return s
}

func max(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func min(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func sharpeRatio(returns []float64, riskFree float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := average(returns)
	variance := 0.0
	for _, r := range returns {
		variance += (r - avg) * (r - avg)
	}
	std := math.Sqrt(variance / float64(len(returns)-1))
	if std == 0 {
		return 0
	}
	return (avg - riskFree/252) / std * math.Sqrt(252)
}

func sortinoRatio(returns []float64, riskFree float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := average(returns)
	downVar := 0.0
	count := 0
	for _, r := range returns {
		if r < 0 {
			downVar += r * r
			count++
		}
	}
	if count <= 1 || downVar == 0 {
		return 0
	}
	downStd := math.Sqrt(downVar / float64(count-1))
	return (avg - riskFree/252) / downStd * math.Sqrt(252)
}

func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	return sum(vals) / float64(len(vals))
}
