package backtest

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/data"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Backtest Runner ──

// Runner executes event-driven backtests on historical data.
type Runner struct {
	initialBalance  float64
	commission      float64
	slippage        float64
	latencyMs       int64
	startTime       int64
	endTime         int64
	positionSizePct float64
	riskFreeRate    float64

	rng         *rand.Rand
	bars        map[string][]model.Bar  // symbol -> bars
	ticks       map[string][]model.Tick // symbol -> ticks
	trades      []model.TradeData
	orders      []model.OrderData
	positions   []Position
	equity      []EquityPoint
	executions  []Execution
	orderLedger []OrderLedgerEntry
	equityPeak  float64

	maxPositionAdjustments int // v1.2: 单笔持仓加/减仓次数上限（<=0 不限）

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
	// Adjustments 是持仓期间的加仓/减仓总次数（v1.2：含 PositionAdjuster
	// 钩子产生的调整单与信号驱动的同向加仓），回测报告据此统计平均加仓次数。
	Adjustments int `json:"adjustments,omitempty"`
	// partialPnL 是减仓调整已落袋的盈亏（最终平仓时并入 RealizedPnL 展示，
	// 现金在减仓当时已入账，避免重复计）。
	partialPnL float64
	// addCount 是钩子/信号驱动的加仓次数（max_position_adjustments 上限按
	// 此判定；减仓是降风险动作，不计入——freqtrade 同口径）。
	addCount int
}

// EquityPoint is a snapshot of portfolio value at a point in time.
type EquityPoint struct {
	Timestamp     int64   `json:"timestamp"`
	Equity        float64 `json:"equity"`
	AvailableCash float64 `json:"available_cash"`
	PositionValue float64 `json:"position_value"`
	// Drawdown 是相对历史峰值的回撤百分比（<=0，峰值处为 0；QuantDinger
	// equityCurve[].drawdown 同口径），由 buildResult 统一回填。
	Drawdown float64 `json:"drawdown,omitempty"`
}

// Execution 单次成交明细（对标 QuantDinger executions[]：每次实际成交一条，
// 含信号时间与成交时间，前端据此在 K 线图上叠加买卖点）。
type Execution struct {
	Time       int64   `json:"time"`                  // 成交时间（unix ms，T+1 开盘价成交）
	SignalTime int64   `json:"signal_time,omitempty"` // 信号产生的 K 线时间（钩子调整单无）
	Side       string  `json:"side"`                  // buy | sell（按持仓量增量方向）
	Quantity   float64 `json:"quantity"`
	Price      float64 `json:"price"`
	Notional   float64 `json:"notional"`
	Commission float64 `json:"commission"`
	Balance    float64 `json:"balance"` // 成交后的账户权益
	Reason     string  `json:"reason,omitempty"`
}

// OrderLedgerEntry 订单台账（对标 QuantDinger orderLedger）：策略信号产生的
// 订单从产生到成交/否决/拒绝/过期的全生命周期记录；vetoed=确认钩子否决，
// rejected=成交时不可执行（余额不足/无持仓/方向冲突），expired=末根 K 线
// 信号无下一根可成交。
type OrderLedgerEntry struct {
	SignalTime int64   `json:"signal_time"`
	Direction  string  `json:"direction"`           // LONG | SHORT | CLOSE
	Status     string  `json:"status"`              // filled | vetoed | rejected | expired
	Reason     string  `json:"reason,omitempty"`    // 信号原因或拒绝原因
	FillTime   int64   `json:"fill_time,omitempty"` // status=filled 时的成交时间
	Quantity   float64 `json:"quantity,omitempty"`
	Price      float64 `json:"price,omitempty"`
}

// RunnerConfig configures the backtest runner.
type RunnerConfig struct {
	InitialBalance  float64 `json:"initial_balance"`
	Commission      float64 `json:"commission"`       // e.g., 0.001 for 0.1%
	Slippage        float64 `json:"slippage"`         // e.g., 0.0005 for 0.05%
	LatencyMs       int64   `json:"latency_ms"`
	StartTime       int64   `json:"start_time"`       // unix ms, 0 = all data
	EndTime         int64   `json:"end_time"`
	PositionSizePct float64 `json:"position_size_pct"` // position size as % of balance, e.g. 0.02 = 2%
	RiskFreeRate    float64 `json:"risk_free_rate"`    // annual risk-free rate, e.g. 0.02 = 2%
	SlippageSeed    int64   `json:"slippage_seed"`     // seed for reproducible slippage; 0 = use time
	// MaxPositionAdjustments 限制单笔持仓的加仓次数（v1.2，对标
	// freqtrade max_entry_position_adjustment；减仓是降风险动作不受此限）；
	// <=0 表示不限。策略实现 MaxPositionAdjustmentsProvider 时以策略值优先。
	MaxPositionAdjustments int `json:"max_position_adjustments"`
}

func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		InitialBalance:  100000,
		Commission:      0.001,
		Slippage:        0.0005,
		LatencyMs:       100,
		PositionSizePct: 0.02,
		RiskFreeRate:    0.02,
	}
}

func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.InitialBalance <= 0 {
		cfg.InitialBalance = 100000
	}
	if cfg.PositionSizePct <= 0 {
		cfg.PositionSizePct = 0.02
	}
	if cfg.RiskFreeRate <= 0 {
		cfg.RiskFreeRate = 0.02
	}
	seed := cfg.SlippageSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &Runner{
		initialBalance:  cfg.InitialBalance,
		commission:      cfg.Commission,
		slippage:        cfg.Slippage,
		latencyMs:       cfg.LatencyMs,
		startTime:       cfg.StartTime,
		endTime:         cfg.EndTime,
		positionSizePct: cfg.PositionSizePct,
		riskFreeRate:    cfg.RiskFreeRate,
		maxPositionAdjustments: cfg.MaxPositionAdjustments,
		rng:             rand.New(rand.NewSource(seed)),
		bars:            make(map[string][]model.Bar),
		ticks:           make(map[string][]model.Tick),
		equityPeak:      cfg.InitialBalance,
	}
}

// LoadBars loads OHLCV bar data for a symbol.
func (r *Runner) LoadBars(symbol string, bars []model.Bar) {
	// Sort by time
	sort.Slice(bars, func(i, j int) bool { return bars[i].Time < bars[j].Time })
	r.bars[symbol] = bars
}

// LoadBarsFromDownloader loads historical bars from the data downloader for backtesting.
func (r *Runner) LoadBarsFromDownloader(symbol, interval string, downloader *data.Downloader) error {
	if downloader == nil {
		return fmt.Errorf("downloader is nil")
	}
	bars := downloader.LoadBarsForBacktest(symbol, interval, r.startTime, r.endTime)
	if len(bars) == 0 {
		return fmt.Errorf("no historical data found for %s %s", symbol, interval)
	}
	r.LoadBars(symbol, bars)
	return nil
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

// ── v1.2 可选契约钩子（对标 freqtrade IStrategy / strategy 包同名能力）──
// 与实盘引擎"回测实盘同源"：回测策略实现哪个接口就启用哪个钩子，未实现
// 走默认行为，存量回测策略完全不受影响。钩子目前只在 K 线路径（Run）生效；
// tick 路径（RunWithTicks）不支持（回测差异见 docs/PYTHON_STRATEGY_API.md 同节说明）。

// EntryConfirmer 对标 confirm_trade_entry：开新仓前最后一刻确认，
// 返回 false 否决该笔入场（加仓信号不询问——策略自身信号决策）。
type EntryConfirmer interface {
	ConfirmTradeEntry(signal *model.Signal, state *StrategyState) bool
}

// ExitConfirmer 对标 confirm_trade_exit：平仓前最后一刻确认，
// 返回 false 否决本次平仓（持仓保留）。
type ExitConfirmer interface {
	ConfirmTradeExit(pos *Position, state *StrategyState) bool
}

// StakeCustomizer 对标 custom_stake_amount：信号未指定数量时自定义入场金额
// （计价币 USDT）。返回 <=0 用默认（initial_balance × position_size_pct）。
type StakeCustomizer interface {
	CustomStakeAmount(proposedStake float64, signal *model.Signal, state *StrategyState) float64
}

// PositionAdjuster 对标 adjust_trade_position（DCA 动态加减仓）：持仓期间
// 每根 K 线（含无信号 K 线）询问是否调整仓位。返回计价币金额：
//
//	>0 加仓（按当根收盘价折算数量，均价重算，计入 Adjustments）
//	<0 减仓（|金额| 折算部分平仓，盈亏即时落袋）
//	0  不调整
//
// 次数受 RunnerConfig.MaxPositionAdjustments / MaxPositionAdjustmentsProvider 限制。
type PositionAdjuster interface {
	AdjustTradePosition(pos *Position, bar model.Bar, state *StrategyState) float64
}

// MaxPositionAdjustmentsProvider 对标 max_entry_position_adjustment：
// 限制单笔持仓的**加仓**次数（减仓不受此限）；<=0 表示不限。实现后优先于
// RunnerConfig.MaxPositionAdjustments。
type MaxPositionAdjustmentsProvider interface {
	MaxPositionAdjustments() int
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
	// v1.2 加仓统计：TotalAdjustments 是所有已平仓持仓的加/减仓总次数，
	// AvgAdjustmentsPerTrade 是平均每笔交易的调整次数（DCA 密集度）。
	TotalAdjustments       int     `json:"total_adjustments"`
	AvgAdjustmentsPerTrade float64 `json:"avg_adjustments_per_trade"`

	// ── QuantDinger 对齐字段（v1.3）──
	AnnualizedReturn     float64 `json:"annualized_return,omitempty"`     // 年化收益 %（复合，区间 <1 天为 0）
	AnnualizedVolatility float64 `json:"annualized_volatility,omitempty"` // 年化波动率 %（按 K 线间隔推断年化因子）
	TotalCommission      float64 `json:"total_commission,omitempty"`      // 全部成交的手续费合计
	MaxDrawdownPeakTime  int64   `json:"max_drawdown_peak_time,omitempty"`
	MaxDrawdownTroughTime int64  `json:"max_drawdown_trough_time,omitempty"`
	Executions           []Execution        `json:"executions,omitempty"`
	OrderLedger          []OrderLedgerEntry `json:"order_ledger,omitempty"`
	// ResultStatus 三态（QuantDinger resultStatus）：no_signals=无任何成交；
	// open_position_only=有成交但无信号驱动的完整回合（仅 end_of_test 强制平仓）；
	// completed_trades=至少一笔信号驱动的完整回合。
	ResultStatus string `json:"result_status,omitempty"`
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
	r.executions = nil
	r.orderLedger = nil
	r.equityPeak = r.initialBalance
	r.mu.Unlock()

	cash := r.initialBalance
	var position *Position
	var pending *model.Signal
	var pendingTime int64
	var equityPoints []EquityPoint

	startTime := time.Now()

	state := &StrategyState{
		Cash:       cash,
		Equity:     cash,
		Bars:       make([]model.Bar, 0, len(bars)),
		Indicators: make(map[string]float64),
	}

	for i := 0; i < len(bars); i++ {
		bar := bars[i]

		// 上一根 K 线（T）产生的信号在本根（T+1）开盘价成交——
		// QuantDinger/freqtrade pending 订单口径，消除当根收盘价成交的前视偏差。
		if pending != nil {
			r.fillPending(strategy, pending, pendingTime, bar, symbol, &position, &cash, state)
			pending = nil
		}

		state.Bars = append(state.Bars, bar)
		state.BarIndex = i
		state.Equity = cash
		if position != nil && !position.IsClosed {
			state.Equity += position.Quantity * bar.Close * (1 - r.commission)
		}
		state.Cash = cash
		state.Position = position

		signal, err := strategy.OnBar(bar, state)
		vetoed := false
		if err == nil && signal != nil {
			switch {
			case r.hookVetoes(strategy, signal, position, state):
				vetoed = true
				r.appendLedger(OrderLedgerEntry{
					SignalTime: bar.Time, Direction: signal.Direction,
					Status: "vetoed", Reason: "hook_veto",
				})
			case i == len(bars)-1:
				// 末根 K 线的信号没有下一根可成交，挂起即过期（freqtrade 同口径丢弃）。
				r.appendLedger(OrderLedgerEntry{
					SignalTime: bar.Time, Direction: signal.Direction,
					Status: "expired", Reason: "end_of_data",
				})
			default:
				pending = signal
				pendingTime = bar.Time
			}
		}

		// v1.2 adjust_trade_position：持仓期间每根 K 线（含无信号 K 线）询问
		// 加/减仓；被确认钩子否决的信号 K 线不询问（信号未成交，视同无信号）。
		if err == nil && !vetoed {
			r.maybeAdjustPosition(strategy, position, bar, state, &cash)
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
		r.closePosition(position, lastBar.Close, lastBar.Time, 0, "end_of_test", false, &cash)
	}

	r.mu.Lock()
	r.equity = equityPoints
	positions := make([]Position, len(r.positions))
	copy(positions, r.positions)
	executions := make([]Execution, len(r.executions))
	copy(executions, r.executions)
	ledger := make([]OrderLedgerEntry, len(r.orderLedger))
	copy(ledger, r.orderLedger)
	r.mu.Unlock()

	return r.buildResult(positions, equityPoints, executions, ledger, time.Since(startTime).Milliseconds()), nil
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
				if position.Side == model.SideBuy && signal.Qty > 0 {
					addQty := signal.Qty
					addNotional := addQty * execPrice
					cash -= addNotional
					totalCost := position.Quantity*position.EntryPrice + addQty*execPrice
					position.Quantity += addQty
					position.EntryPrice = totalCost / position.Quantity
					state.TradeCount++
				}
				continue
			}
			qty := signal.Qty
			if qty <= 0 {
				qty = r.initialBalance * r.positionSizePct / execPrice
			}
			position = &Position{
				Symbol:     symbol,
				Side:       model.SideBuy,
				Quantity:   qty,
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

	return r.buildResult(positions, equityPoints, nil, nil, time.Since(startTime).Milliseconds()), nil
}

// hookVetoes 应用 v1.2 确认钩子：新仓入场问 EntryConfirmer，平仓问
// ExitConfirmer；返回 true = 策略否决，该信号不下单（持仓保留）。
func (r *Runner) hookVetoes(strategy BacktestStrategy, signal *model.Signal, position *Position, state *StrategyState) bool {
	switch signal.Direction {
	case "LONG", "SHORT":
		if position != nil && !position.IsClosed {
			return false // 信号驱动的同向加仓不询问（策略自身信号决策）
		}
		if ec, ok := strategy.(EntryConfirmer); ok {
			return !ec.ConfirmTradeEntry(signal, state)
		}
	case "CLOSE":
		if position == nil || position.IsClosed {
			return false
		}
		if xc, ok := strategy.(ExitConfirmer); ok {
			return !xc.ConfirmTradeExit(position, state)
		}
	}
	return false
}

// appendExecution / appendLedger 记录成交明细与订单台账。
func (r *Runner) appendExecution(exec Execution) {
	r.mu.Lock()
	r.executions = append(r.executions, exec)
	r.mu.Unlock()
}

func (r *Runner) appendLedger(entry OrderLedgerEntry) {
	r.mu.Lock()
	r.orderLedger = append(r.orderLedger, entry)
	r.mu.Unlock()
}

// fillPending 在本根 K 线开盘价成交上一根挂起的信号（QuantDinger/freqtrade
// T+1 开盘成交口径）。成交方向/加减仓按成交时的持仓状态判定；现金不足、
// 无持仓可平或方向冲突的订单记入台账（rejected）且不成交。
func (r *Runner) fillPending(strategy BacktestStrategy, signal *model.Signal, signalTime int64, bar model.Bar, symbol string, position **Position, cash *float64, state *StrategyState) {
	execPrice := r.applySlippage(bar.Open, signal.Direction)
	if execPrice <= 0 {
		r.appendLedger(OrderLedgerEntry{SignalTime: signalTime, Direction: signal.Direction, Status: "rejected", Reason: "invalid_price"})
		return
	}
	ledger := OrderLedgerEntry{
		SignalTime: signalTime, Direction: signal.Direction,
		Status: "filled", Reason: signal.Reason, FillTime: bar.Time, Price: execPrice,
	}

	switch signal.Direction {
	case "LONG", "SHORT":
		side := model.SideBuy
		execSide := "buy"
		if signal.Direction == "SHORT" {
			side = model.SideSell
			execSide = "sell"
		}
		if *position != nil && !(*position).IsClosed {
			pos := *position
			if pos.Side == side && signal.Qty > 0 {
				// Add to existing position
				addNotional := signal.Qty * execPrice
				commissionCost := addNotional * r.commission
				if addNotional+commissionCost > *cash {
					r.appendLedger(OrderLedgerEntry{
						SignalTime: signalTime, Direction: signal.Direction,
						Status: "rejected", Reason: "insufficient_balance",
					})
					return
				}
				*cash -= addNotional + commissionCost
				totalCost := pos.Quantity*pos.EntryPrice + signal.Qty*execPrice
				pos.Quantity += signal.Qty
				pos.EntryPrice = totalCost / pos.Quantity
				pos.Adjustments++ // v1.2: 信号驱动加仓计入统计
				pos.addCount++
				state.TradeCount++
				ledger.Quantity = signal.Qty
				r.appendLedger(ledger)
				r.appendExecution(Execution{
					Time: bar.Time, SignalTime: signalTime, Side: execSide,
					Quantity: signal.Qty, Price: execPrice, Notional: addNotional,
					Commission: commissionCost, Balance: *cash + pos.Quantity*execPrice,
					Reason: signal.Reason,
				})
				return
			}
			r.appendLedger(OrderLedgerEntry{
				SignalTime: signalTime, Direction: signal.Direction,
				Status: "rejected", Reason: "conflicting_position",
			})
			return
		}
		qty := signal.Qty
		if qty <= 0 {
			// v1.2 custom_stake_amount：策略自定义入场金额（<=0 用默认）
			stake := r.initialBalance * r.positionSizePct
			if sc, ok := strategy.(StakeCustomizer); ok {
				if custom := sc.CustomStakeAmount(stake, signal, state); custom > 0 {
					stake = custom
				}
			}
			qty = stake / execPrice
		}
		notional := qty * execPrice
		commissionCost := notional * r.commission
		if notional+commissionCost > *cash {
			r.appendLedger(OrderLedgerEntry{
				SignalTime: signalTime, Direction: signal.Direction,
				Status: "rejected", Reason: "insufficient_balance",
			})
			return
		}
		*position = &Position{
			Symbol:     symbol,
			Side:       side,
			Quantity:   qty,
			EntryPrice: execPrice,
			EntryTime:  bar.Time,
		}
		*cash -= notional + commissionCost
		state.TradeCount++
		ledger.Quantity = qty
		r.appendLedger(ledger)
		r.appendExecution(Execution{
			Time: bar.Time, SignalTime: signalTime, Side: execSide,
			Quantity: qty, Price: execPrice, Notional: notional,
			Commission: commissionCost, Balance: *cash + qty*execPrice,
			Reason: signal.Reason,
		})

	case "CLOSE":
		if *position == nil || (*position).IsClosed {
			r.appendLedger(OrderLedgerEntry{
				SignalTime: signalTime, Direction: signal.Direction,
				Status: "rejected", Reason: "no_open_position",
			})
			return
		}
		pos := *position
		ledger.Quantity = pos.Quantity
		r.appendLedger(ledger)
		r.closePosition(pos, execPrice, bar.Time, signalTime, signal.Reason, true, cash)
	}
}

// closePosition 平仓结算：盈亏落袋（chargeCommission 区分信号平仓收手续费 /
// end_of_test 强平不收费的既有口径），记录已平仓持仓与成交明细。
func (r *Runner) closePosition(position *Position, execPrice float64, t int64, signalTime int64, reason string, chargeCommission bool, cash *float64) {
	position.ExitPrice = execPrice
	position.ExitTime = t
	position.IsClosed = true
	position.ExitReason = reason

	var closeLegPnL float64
	if position.Side == model.SideBuy {
		closeLegPnL = position.Quantity * (execPrice - position.EntryPrice)
	} else {
		closeLegPnL = position.Quantity * (position.EntryPrice - execPrice)
	}
	closeNotional := position.Quantity * execPrice
	closeCommission := 0.0
	if chargeCommission {
		closeCommission = closeNotional * r.commission
		closeLegPnL -= closeCommission
	}
	// v1.2：减仓调整已落袋的盈亏并入展示口径；现金只记平仓腿
	// （减仓腿的现金在减仓当时已入账，不能重复计）。
	position.RealizedPnL = closeLegPnL + position.partialPnL
	*cash += closeNotional + closeLegPnL

	execSide := "sell"
	if position.Side == model.SideSell {
		execSide = "buy"
	}
	execReason := reason
	if execReason == "" {
		execReason = "close"
	}
	r.appendExecution(Execution{
		Time: t, SignalTime: signalTime, Side: execSide,
		Quantity: position.Quantity, Price: execPrice, Notional: closeNotional,
		Commission: closeCommission, Balance: *cash,
		Reason: execReason,
	})

	r.mu.Lock()
	r.positions = append(r.positions, *position)
	r.mu.Unlock()
}

// maybeAdjustPosition 驱动 PositionAdjuster（v1.2，对标 adjust_trade_position）：
// 持仓期间每根 K 线询问加/减仓；次数受 RunnerConfig.MaxPositionAdjustments
// 限制（策略实现 MaxPositionAdjustmentsProvider 时以策略值优先）。
func (r *Runner) maybeAdjustPosition(strategy BacktestStrategy, position *Position, bar model.Bar, state *StrategyState, cash *float64) {
	if position == nil || position.IsClosed {
		return
	}
	adj, ok := strategy.(PositionAdjuster)
	if !ok {
		return
	}
	amt := adj.AdjustTradePosition(position, bar, state)
	if amt == 0 {
		return
	}
	if amt > 0 {
		// 上限只约束加仓（freqtrade max_entry_position_adjustment 同口径：
		// 减仓永不被拦截）。
		maxAdj := r.maxPositionAdjustments
		if mp, ok2 := strategy.(MaxPositionAdjustmentsProvider); ok2 {
			maxAdj = mp.MaxPositionAdjustments()
		}
		if maxAdj > 0 && position.addCount >= maxAdj {
			return
		}
	}
	r.applyPositionAdjustment(position, amt, bar, state, cash)
}

// applyPositionAdjustment 执行一次加/减仓：加仓重算均价，减仓盈亏即时落袋
// （现金当场入账、计入 partialPnL，最终平仓时并入 RealizedPnL 展示口径）；
// 减到 0 视为平仓（ExitReason=position_reduce）记入已平仓持仓。
func (r *Runner) applyPositionAdjustment(position *Position, amt float64, bar model.Bar, state *StrategyState, cash *float64) {
	if amt > 0 {
		dir := "LONG"
		execSide := "buy"
		if position.Side == model.SideSell {
			dir = "SHORT"
			execSide = "sell"
		}
		execPrice := r.applySlippage(bar.Close, dir)
		if execPrice <= 0 {
			return
		}
		addQty := amt / execPrice
		addNotional := addQty * execPrice
		commissionCost := addNotional * r.commission
		*cash -= addNotional + commissionCost
		totalCost := position.Quantity*position.EntryPrice + addQty*execPrice
		position.Quantity += addQty
		position.EntryPrice = totalCost / position.Quantity
		position.Adjustments++
		position.addCount++
		state.TradeCount++
		r.appendExecution(Execution{
			Time: bar.Time, Side: execSide,
			Quantity: addQty, Price: execPrice, Notional: addNotional,
			Commission: commissionCost, Balance: *cash + position.Quantity*execPrice,
			Reason: "position_adjust",
		})
		return
	}
	execPrice := r.applySlippage(bar.Close, "CLOSE")
	if execPrice <= 0 {
		return
	}
	reduceQty := (-amt) / execPrice
	if reduceQty > position.Quantity {
		reduceQty = position.Quantity
	}
	reduceNotional := reduceQty * execPrice
	var legPnL float64
	execSide := "sell"
	if position.Side == model.SideBuy {
		legPnL = reduceQty * (execPrice - position.EntryPrice)
	} else {
		legPnL = reduceQty * (position.EntryPrice - execPrice)
		execSide = "buy"
	}
	reduceCommission := reduceNotional * r.commission
	legPnL -= reduceCommission
	*cash += reduceNotional + legPnL
	position.partialPnL += legPnL
	position.Quantity -= reduceQty
	position.Adjustments++
	state.TradeCount++
	r.appendExecution(Execution{
		Time: bar.Time, Side: execSide,
		Quantity: reduceQty, Price: execPrice, Notional: reduceNotional,
		Commission: reduceCommission, Balance: *cash + position.Quantity*execPrice,
		Reason: "position_adjust",
	})
	if position.Quantity <= 0 {
		position.IsClosed = true
		position.ExitPrice = execPrice
		position.ExitTime = bar.Time
		position.ExitReason = "position_reduce"
		position.RealizedPnL = position.partialPnL
		r.mu.Lock()
		r.positions = append(r.positions, *position)
		r.mu.Unlock()
	}
}

func (r *Runner) applySlippage(price float64, direction string) float64 {
	slipFactor := 1.0
	if direction == "LONG" {
		slipFactor = 1.0 + r.slippage + r.rng.Float64()*r.slippage*2
	} else if direction == "SHORT" || direction == "CLOSE" {
		slipFactor = 1.0 - r.slippage - r.rng.Float64()*r.slippage*2
	}
	return price * slipFactor
}

// MetricsFromEquity 从权益曲线 + 成交明细构建完整指标（不依赖 Runner 实例，
// 供组合回测等复用 stats.go 的 Sharpe/回撤/胜率等口径）。
func MetricsFromEquity(equity []EquityPoint, initialBalance, riskFreeRate float64, positions []Position, durationMs int64) *RunResult {
	if initialBalance <= 0 {
		initialBalance = 100000
	}
	if riskFreeRate <= 0 {
		riskFreeRate = 0.02
	}
	r := &Runner{initialBalance: initialBalance, riskFreeRate: riskFreeRate, commission: 0.001}
	return r.buildResult(positions, equity, nil, nil, durationMs)
}

func (r *Runner) buildResult(positions []Position, equity []EquityPoint, executions []Execution, ledger []OrderLedgerEntry, durationMs int64) *RunResult {
	result := &RunResult{
		EquityCurve: equity,
		Trades:      positions,
		TotalTrades: len(positions),
		Executions:  executions,
		OrderLedger: ledger,
		DurationMs:  durationMs,
	}

	// resultStatus 三态（QuantDinger 口径）：无任何成交与平仓 → no_signals；
	// 有成交但没有信号驱动的完整回合（仅 end_of_test 强平）→ open_position_only。
	completedTrades := 0
	for _, pos := range positions {
		if pos.ExitReason != "end_of_test" {
			completedTrades++
		}
	}
	switch {
	case len(executions) == 0 && len(positions) == 0:
		result.ResultStatus = "no_signals"
	case completedTrades == 0:
		result.ResultStatus = "open_position_only"
	default:
		result.ResultStatus = "completed_trades"
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

	// v1.2 加仓统计：调整总次数 + 平均每笔交易调整次数（DCA 密集度）
	totalAdj := 0
	for _, pos := range positions {
		totalAdj += pos.Adjustments
	}
	result.TotalAdjustments = totalAdj

	if result.TotalTrades > 0 {
		result.WinRate = float64(len(wins)) / float64(result.TotalTrades) * 100
		result.AvgAdjustmentsPerTrade = float64(totalAdj) / float64(result.TotalTrades)
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

	// Drawdown：每点回填相对峰值的回撤（<=0，QuantDinger equityCurve[].drawdown
	// 口径），并记录最大回撤的峰值/谷值时间。
	peak := r.initialBalance
	peakTime := equity[0].Timestamp
	maxDD := 0.0
	for i := range equity {
		if equity[i].Equity > peak {
			peak = equity[i].Equity
			peakTime = equity[i].Timestamp
		}
		dd := 0.0
		if peak > 0 {
			dd = (equity[i].Equity/peak - 1) * 100 // <=0
		}
		equity[i].Drawdown = dd
		if -dd > maxDD {
			maxDD = -dd
			result.MaxDrawdownPeakTime = peakTime
			result.MaxDrawdownTroughTime = equity[i].Timestamp
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

	result.SharpeRatio = sharpeRatio(returns, r.riskFreeRate)
	result.SortinoRatio = sortinoRatio(returns, r.riskFreeRate)
	if maxDD > 0 {
		result.CalmarRatio = result.TotalReturnPct / maxDD
	}

	// ── QuantDinger 对齐指标（v1.3）──
	// totalCommission：全部成交的手续费合计。
	for _, exec := range executions {
		result.TotalCommission += exec.Commission
	}
	// annualizedReturn：复合年化 %（区间 <1 天或初值 <=0 时为 0；上限对齐
	// QuantDinger 的 1e6% 截断）。
	if elapsedDays := float64(equity[len(equity)-1].Timestamp-equity[0].Timestamp) / 86400000.0; elapsedDays >= 1 && r.initialBalance > 0 {
		if finalEquity <= 0 {
			result.AnnualizedReturn = -100
		} else {
			result.AnnualizedReturn = math.Expm1(math.Min(
				math.Log(finalEquity/r.initialBalance)*(365.25/elapsedDays),
				math.Log(10001.0),
			)) * 100
		}
	}
	// annualizedVolatility：bar 收益 std(ddof=0) × √periodsPerYear × 100，
	// periodsPerYear 按权益点时间间隔中位数推断（无法推断时按日频 252）。
	if len(returns) >= 2 {
		result.AnnualizedVolatility = stddev(returns) * math.Sqrt(periodsPerYear(equity)) * 100
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

// stddev 总体标准差（ddof=0，QuantDinger returns.std(ddof=0) 口径）。
func stddev(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	avg := average(vals)
	variance := 0.0
	for _, v := range vals {
		variance += (v - avg) * (v - avg)
	}
	return math.Sqrt(variance / float64(len(vals)))
}

// periodsPerYear 按权益点时间间隔中位数推断每年的 bar 数；无法推断时
// 回退日频 252。
func periodsPerYear(equity []EquityPoint) float64 {
	if len(equity) < 2 {
		return 252
	}
	diffs := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if d := equity[i].Timestamp - equity[i-1].Timestamp; d > 0 {
			diffs = append(diffs, float64(d))
		}
	}
	if len(diffs) == 0 {
		return 252
	}
	sort.Float64s(diffs)
	median := diffs[len(diffs)/2]
	return 365.25 * 86400 * 1000 / median
}
