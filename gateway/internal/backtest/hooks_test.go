package backtest

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// hookBTStrategy 是 v1.2 契约钩子测试用的回测策略假身：fn 非 nil 的钩子
// 生效，nil 即默认行为（不放任何钩子逻辑）。
type hookBTStrategy struct {
	name         string
	onBar        func(bar model.Bar, state *StrategyState) (*model.Signal, error)
	confirmEntry func(sig *model.Signal, state *StrategyState) bool
	confirmExit  func(pos *Position, state *StrategyState) bool
	customStake  func(proposed float64, sig *model.Signal, state *StrategyState) float64
	adjust       func(pos *Position, bar model.Bar, state *StrategyState) float64
}

func (s *hookBTStrategy) Name() string   { return s.name }
func (s *hookBTStrategy) Symbol() string { return "BTCUSDT" }
func (s *hookBTStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	if s.onBar != nil {
		return s.onBar(bar, state)
	}
	return nil, nil
}
func (s *hookBTStrategy) OnTick(model.Tick, *StrategyState) (*model.Signal, error) { return nil, nil }

func (s *hookBTStrategy) ConfirmTradeEntry(sig *model.Signal, state *StrategyState) bool {
	if s.confirmEntry == nil {
		return true
	}
	return s.confirmEntry(sig, state)
}

func (s *hookBTStrategy) ConfirmTradeExit(pos *Position, state *StrategyState) bool {
	if s.confirmExit == nil {
		return true
	}
	return s.confirmExit(pos, state)
}

func (s *hookBTStrategy) CustomStakeAmount(proposed float64, sig *model.Signal, state *StrategyState) float64 {
	if s.customStake == nil {
		return 0
	}
	return s.customStake(proposed, sig, state)
}

func (s *hookBTStrategy) AdjustTradePosition(pos *Position, bar model.Bar, state *StrategyState) float64 {
	if s.adjust == nil {
		return 0
	}
	return s.adjust(pos, bar, state)
}

// maxAdjBTStrategy 额外实现 MaxPositionAdjustmentsProvider（策略级次数上限）。
type maxAdjBTStrategy struct {
	hookBTStrategy
	max int
}

func (s *maxAdjBTStrategy) MaxPositionAdjustments() int { return s.max }

// btBars 构造确定性的 K 线序列（ closes 依次，间隔 1 分钟）。
func btBars(closes ...float64) []model.Bar {
	bars := make([]model.Bar, len(closes))
	for i, c := range closes {
		bars[i] = model.Bar{Symbol: "BTCUSDT", Time: int64(i+1) * 60000, Open: c, High: c, Low: c, Close: c, Volume: 1}
	}
	return bars
}

// hookRunner 零手续费零滑点的确定性 runner。
func hookRunner(t *testing.T, mutate func(*RunnerConfig)) *Runner {
	t.Helper()
	cfg := RunnerConfig{
		InitialBalance: 10000, Commission: 0, Slippage: 0,
		PositionSizePct: 0.1, RiskFreeRate: 0.02, SlippageSeed: 1,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	r := NewRunner(cfg)
	return r
}

func almostEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestBacktestConfirmEntryVeto：confirm_trade_entry 对标——否决时不开仓，
// 放行时正常成交。
func TestBacktestConfirmEntryVeto(t *testing.T) {
	veto := true
	s := &hookBTStrategy{name: "bt_entry"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG"}, nil
		}
		return nil, nil
	}
	s.confirmEntry = func(sig *model.Signal, state *StrategyState) bool { return !veto }

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 101, 102))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 0 {
		t.Fatalf("vetoed entry must yield no trade, got %d", res.TotalTrades)
	}

	veto = false
	r2 := hookRunner(t, nil)
	r2.LoadBars("BTCUSDT", btBars(100, 101, 102))
	res2, err := r2.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res2.TotalTrades != 1 {
		t.Fatalf("allowed entry should yield 1 trade, got %d", res2.TotalTrades)
	}
}

// TestBacktestCustomStakeAmount：custom_stake_amount 对标——信号未指定数量时
// 按策略返回金额折算数量；返回 0 用默认（initial × size_pct）。
func TestBacktestCustomStakeAmount(t *testing.T) {
	var seenProposed float64
	s := &hookBTStrategy{name: "bt_stake"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG"}, nil
		}
		return nil, nil
	}
	s.customStake = func(proposed float64, sig *model.Signal, state *StrategyState) float64 {
		seenProposed = proposed
		return 500 // 覆盖默认 1000
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 100, 100))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 {
		t.Fatalf("want 1 trade, got %d", res.TotalTrades)
	}
	if !almostEq(seenProposed, 1000) { // 10000 × 0.1
		t.Fatalf("proposed stake = %v, want 1000", seenProposed)
	}
	if !almostEq(res.Trades[0].Quantity, 5) { // 500 / 100
		t.Fatalf("qty = %v, want 5 (stake 500 / close 100)", res.Trades[0].Quantity)
	}

	// 返回 0 → 默认金额 1000 → qty 10
	s.customStake = func(float64, *model.Signal, *StrategyState) float64 { return 0 }
	r2 := hookRunner(t, nil)
	r2.LoadBars("BTCUSDT", btBars(100, 100, 100))
	res2, _ := r2.Run(s)
	if !almostEq(res2.Trades[0].Quantity, 10) {
		t.Fatalf("stake=0 must use default sizing, qty = %v want 10", res2.Trades[0].Quantity)
	}
}

// TestBacktestConfirmExitVeto：confirm_trade_exit 对标——否决平仓信号时持仓
// 保留到回测结束（end_of_test），放行时按信号平仓。
func TestBacktestConfirmExitVeto(t *testing.T) {
	veto := true
	s := &hookBTStrategy{name: "bt_exit"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		switch state.BarIndex {
		case 0:
			return &model.Signal{Direction: "LONG"}, nil
		case 1:
			return &model.Signal{Direction: "CLOSE", Reason: "tp"}, nil
		}
		return nil, nil
	}
	s.confirmExit = func(pos *Position, state *StrategyState) bool { return !veto }

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 110, 120))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 || res.Trades[0].ExitReason != "end_of_test" {
		t.Fatalf("vetoed exit must hold to end_of_test, got %+v", res.Trades)
	}

	veto = false
	r2 := hookRunner(t, nil)
	r2.LoadBars("BTCUSDT", btBars(100, 110, 120))
	res2, _ := r2.Run(s)
	if res2.TotalTrades != 1 || res2.Trades[0].ExitReason != "tp" {
		t.Fatalf("allowed exit should close by signal, got %+v", res2.Trades)
	}
}

// TestBacktestAdjustTradePositionAdd：adjust_trade_position 对标——持仓期间
// 无信号 K 线同样询问；>0 加仓重算均价，次数与报表字段正确。
func TestBacktestAdjustTradePositionAdd(t *testing.T) {
	s := &hookBTStrategy{name: "bt_adj_add"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG", Qty: 1}, nil
		}
		return nil, nil
	}
	var calls int
	s.adjust = func(pos *Position, bar model.Bar, state *StrategyState) float64 {
		calls++
		if bar.Time == 120000 { // 第 2 根 K 线加仓 100U
			return 100
		}
		return 0
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 100, 100, 100))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 {
		t.Fatalf("want 1 trade, got %d", res.TotalTrades)
	}
	pos := res.Trades[0]
	if !almostEq(pos.Quantity, 2) { // 1 + 100/100
		t.Fatalf("qty after adjustment = %v, want 2", pos.Quantity)
	}
	if pos.Adjustments != 1 {
		t.Fatalf("position adjustments = %d, want 1", pos.Adjustments)
	}
	if calls < 3 {
		t.Fatalf("adjust hook should be asked on quiet bars too, got %d calls", calls)
	}
	if res.TotalAdjustments != 1 || !almostEq(res.AvgAdjustmentsPerTrade, 1) {
		t.Fatalf("report stats = %d / %v, want 1 / 1", res.TotalAdjustments, res.AvgAdjustmentsPerTrade)
	}
}

// TestBacktestAdjustTradePositionReduce：<0 减仓盈亏即时落袋，最终平仓
// RealizedPnL 含减仓腿且不重复计现金。
func TestBacktestAdjustTradePositionReduce(t *testing.T) {
	s := &hookBTStrategy{name: "bt_adj_reduce"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG", Qty: 2}, nil
		}
		return nil, nil
	}
	s.adjust = func(pos *Position, bar model.Bar, state *StrategyState) float64 {
		if bar.Time == 120000 { // 第 2 根（close=110）减仓 55U = 0.5 币
			return -55
		}
		return 0
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 110, 120, 130))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 {
		t.Fatalf("want 1 trade, got %d", res.TotalTrades)
	}
	pos := res.Trades[0]
	// 减仓腿：0.5 × (110-100) = 5；平仓腿：1.5 × (130-100) = 45 → 50
	if !almostEq(pos.RealizedPnL, 50) {
		t.Fatalf("RealizedPnL = %v, want 50 (5 reduce leg + 45 close leg)", pos.RealizedPnL)
	}
	if pos.Adjustments != 1 {
		t.Fatalf("adjustments = %d, want 1", pos.Adjustments)
	}
	// 现金账（既有口径：平仓/减仓腿现金 = 名义价值 + 腿盈亏，与 CLOSE 信号
	// 同约定）：-200（开仓） +60（减仓 55 + 5 盈）；权益曲线按市价重估：
	// bar1=9800+2×110=10020、bar2=9860+1.5×120=10040、bar3=9860+1.5×130=10055，
	// end_of_test 平仓不再追加分 → TotalReturn=55（50 盈亏 + 既有口径加成）。
	if !almostEq(res.TotalReturn, 55) {
		t.Fatalf("TotalReturn = %v, want 55 (existing close-leg cash convention)", res.TotalReturn)
	}
}

// TestBacktestAdjustReduceToZero：减仓金额 ≥ 持仓价值时按全平处理，
// ExitReason=position_reduce。
func TestBacktestAdjustReduceToZero(t *testing.T) {
	s := &hookBTStrategy{name: "bt_adj_zero"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG", Qty: 1}, nil
		}
		return nil, nil
	}
	s.adjust = func(pos *Position, bar model.Bar, state *StrategyState) float64 {
		if bar.Time == 120000 {
			return -10000 // 远超持仓价值 → 全平
		}
		return 0
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 110, 120))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 || res.Trades[0].ExitReason != "position_reduce" {
		t.Fatalf("reduce-to-zero must close as position_reduce, got %+v", res.Trades)
	}
	// 1 × (110-100) = 10
	if !almostEq(res.Trades[0].RealizedPnL, 10) {
		t.Fatalf("RealizedPnL = %v, want 10", res.Trades[0].RealizedPnL)
	}
}

// TestBacktestMaxPositionAdjustments：max_position_adjustments 限制参数——
// RunnerConfig 上限生效；策略实现 provider 时以策略值优先。
func TestBacktestMaxPositionAdjustments(t *testing.T) {
	mkStrat := func() *hookBTStrategy {
		s := &hookBTStrategy{name: "bt_maxadj"}
		s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
			if state.BarIndex == 0 {
				return &model.Signal{Direction: "LONG", Qty: 1}, nil
			}
			return nil, nil
		}
		s.adjust = func(*Position, model.Bar, *StrategyState) float64 { return 100 }
		return s
	}

	// RunnerConfig 上限 1：5 根 K 线只调整 1 次
	r := hookRunner(t, func(cfg *RunnerConfig) { cfg.MaxPositionAdjustments = 1 })
	r.LoadBars("BTCUSDT", btBars(100, 100, 100, 100, 100))
	res, err := r.Run(mkStrat())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalAdjustments != 1 {
		t.Fatalf("config max=1 must cap adjustments, got %d", res.TotalAdjustments)
	}

	// 策略 provider=2 覆盖 config=1 → 调整 2 次
	r2 := hookRunner(t, func(cfg *RunnerConfig) { cfg.MaxPositionAdjustments = 1 })
	r2.LoadBars("BTCUSDT", btBars(100, 100, 100, 100, 100))
	res2, err := r2.Run(&maxAdjBTStrategy{hookBTStrategy: *mkStrat(), max: 2})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res2.TotalAdjustments != 2 {
		t.Fatalf("strategy provider must override config, got %d", res2.TotalAdjustments)
	}
}

// TestBacktestStatsCarryAdjustments：PerformanceReport 携带加仓统计字段。
func TestBacktestStatsCarryAdjustments(t *testing.T) {
	s := &hookBTStrategy{name: "bt_report"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG", Qty: 1}, nil
		}
		return nil, nil
	}
	s.adjust = func(*Position, model.Bar, *StrategyState) float64 { return 100 }

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 100, 100))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	report := GenerateReport(res, s.Name(), "BTCUSDT")
	if report.TotalAdjustments != res.TotalAdjustments ||
		!almostEq(report.AvgAdjustmentsPerTrade, res.AvgAdjustmentsPerTrade) {
		t.Fatalf("report must carry adjustment stats: %+v vs %+v", report, res)
	}
	if report.TotalAdjustments == 0 {
		t.Fatal("expected non-zero adjustments in report")
	}
}
