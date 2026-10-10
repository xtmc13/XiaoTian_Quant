package backtest

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ocBars 构造开/收盘价不同的确定性 K 线（opens/closes 等长，间隔 1 分钟），
// 用于区分"当根收盘价成交"与"T+1 开盘价成交"。
func ocBars(opens, closes []float64) []model.Bar {
	bars := make([]model.Bar, len(opens))
	for i := range opens {
		bars[i] = model.Bar{
			Symbol: "BTCUSDT", Time: int64(i+1) * 60000,
			Open: opens[i], High: math.Max(opens[i], closes[i]), Low: math.Min(opens[i], closes[i]),
			Close: closes[i], Volume: 1,
		}
	}
	return bars
}

// TestNextOpenFill：信号在 bar T 产生、T+1 开盘价成交（QuantDinger/freqtrade
// pending 订单口径），成交明细记录 signal_time 与成交价的来源。
func TestNextOpenFill(t *testing.T) {
	s := &hookBTStrategy{name: "bt_next_open"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		switch state.BarIndex {
		case 0:
			return &model.Signal{Direction: "LONG", Qty: 1, Reason: "entry"}, nil
		case 2:
			return &model.Signal{Direction: "CLOSE", Reason: "tp"}, nil
		}
		return nil, nil
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", ocBars(
		[]float64{100, 110, 120, 130},
		[]float64{105, 115, 125, 135},
	))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 {
		t.Fatalf("want 1 trade, got %d", res.TotalTrades)
	}
	pos := res.Trades[0]
	// bar0 信号 → bar1 开盘 110 成交；bar2 信号 → bar3 开盘 130 成交
	if !almostEq(pos.EntryPrice, 110) || pos.EntryTime != 120000 {
		t.Fatalf("entry = %v @ %d, want 110 @ bar1 open", pos.EntryPrice, pos.EntryTime)
	}
	if !almostEq(pos.ExitPrice, 130) || pos.ExitTime != 240000 {
		t.Fatalf("exit = %v @ %d, want 130 @ bar3 open", pos.ExitPrice, pos.ExitTime)
	}
	if !almostEq(pos.RealizedPnL, 20) {
		t.Fatalf("RealizedPnL = %v, want 20", pos.RealizedPnL)
	}
	if res.ResultStatus != "completed_trades" {
		t.Fatalf("resultStatus = %q, want completed_trades", res.ResultStatus)
	}
	if len(res.Executions) != 2 {
		t.Fatalf("executions = %d, want 2", len(res.Executions))
	}
	buy, sell := res.Executions[0], res.Executions[1]
	if buy.Side != "buy" || buy.SignalTime != 60000 || buy.Time != 120000 || !almostEq(buy.Price, 110) {
		t.Fatalf("buy execution = %+v", buy)
	}
	if sell.Side != "sell" || sell.SignalTime != 180000 || sell.Time != 240000 || !almostEq(sell.Price, 130) {
		t.Fatalf("sell execution = %+v", sell)
	}
	if len(res.OrderLedger) != 2 {
		t.Fatalf("ledger = %d, want 2", len(res.OrderLedger))
	}
	for _, e := range res.OrderLedger {
		if e.Status != "filled" || e.FillTime == 0 {
			t.Fatalf("ledger entry = %+v, want filled with fill_time", e)
		}
	}
}

// TestLastBarSignalExpires：末根 K 线的信号无下一根可成交，记台账 expired。
func TestLastBarSignalExpires(t *testing.T) {
	s := &hookBTStrategy{name: "bt_expire"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 2 { // 末根
			return &model.Signal{Direction: "LONG"}, nil
		}
		return nil, nil
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 100, 100))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 0 || len(res.Executions) != 0 {
		t.Fatalf("expired signal must not fill, trades=%d execs=%d", res.TotalTrades, len(res.Executions))
	}
	if res.ResultStatus != "no_signals" {
		t.Fatalf("resultStatus = %q, want no_signals", res.ResultStatus)
	}
	if len(res.OrderLedger) != 1 || res.OrderLedger[0].Status != "expired" {
		t.Fatalf("ledger = %+v, want 1 expired entry", res.OrderLedger)
	}
}

// TestResultStatusOpenPositionOnly：只有入场、持仓被 end_of_test 强平时为
// open_position_only（无信号驱动的完整回合）。
func TestResultStatusOpenPositionOnly(t *testing.T) {
	s := &hookBTStrategy{name: "bt_open_only"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG", Qty: 1}, nil
		}
		return nil, nil
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 110, 120))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 1 || res.Trades[0].ExitReason != "end_of_test" {
		t.Fatalf("want end_of_test close, got %+v", res.Trades)
	}
	if res.ResultStatus != "open_position_only" {
		t.Fatalf("resultStatus = %q, want open_position_only", res.ResultStatus)
	}
	// 入场 + end_of_test 强平各一条成交
	if len(res.Executions) != 2 {
		t.Fatalf("executions = %d, want 2", len(res.Executions))
	}
	if res.Executions[1].Reason != "end_of_test" {
		t.Fatalf("final execution reason = %q", res.Executions[1].Reason)
	}
}

// TestInsufficientBalanceRejected：订单名义价值 + 手续费超过可用现金时拒绝
// 成交并记台账 insufficient_balance。
func TestInsufficientBalanceRejected(t *testing.T) {
	s := &hookBTStrategy{name: "bt_insufficient"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		if state.BarIndex == 0 {
			return &model.Signal{Direction: "LONG", Qty: 10000}, nil // 远超余额
		}
		return nil, nil
	}

	r := hookRunner(t, nil)
	r.LoadBars("BTCUSDT", btBars(100, 100, 100))
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TotalTrades != 0 || len(res.Executions) != 0 {
		t.Fatalf("rejected order must not fill, trades=%d execs=%d", res.TotalTrades, len(res.Executions))
	}
	if len(res.OrderLedger) != 1 || res.OrderLedger[0].Status != "rejected" ||
		res.OrderLedger[0].Reason != "insufficient_balance" {
		t.Fatalf("ledger = %+v, want rejected/insufficient_balance", res.OrderLedger)
	}
}

// TestEquityDrawdownAndAnnualized：equityCurve 每点带回撤（<=0），最大回撤
// 记录峰值/谷值时间；年化收益/波动率与手续费合计正确回填。
func TestEquityDrawdownAndAnnualized(t *testing.T) {
	s := &hookBTStrategy{name: "bt_metrics"}
	s.onBar = func(bar model.Bar, state *StrategyState) (*model.Signal, error) {
		switch state.BarIndex {
		case 0:
			return &model.Signal{Direction: "LONG", Qty: 10, Reason: "entry"}, nil
		case 3:
			return &model.Signal{Direction: "CLOSE", Reason: "exit"}, nil
		}
		return nil, nil
	}

	r := hookRunner(t, func(cfg *RunnerConfig) { cfg.Commission = 0.001 })
	// 日频 K 线，5 根（区间 4 天）；权益 10000 → 9799（谷底）→ 回升创新高
	dayMs := int64(86400000)
	bars := []model.Bar{
		{Symbol: "BTCUSDT", Time: dayMs, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1},
		{Symbol: "BTCUSDT", Time: 2 * dayMs, Open: 100, High: 100, Low: 80, Close: 80, Volume: 1},
		{Symbol: "BTCUSDT", Time: 3 * dayMs, Open: 80, High: 90, Low: 80, Close: 90, Volume: 1},
		{Symbol: "BTCUSDT", Time: 4 * dayMs, Open: 90, High: 110, Low: 90, Close: 110, Volume: 1},
		{Symbol: "BTCUSDT", Time: 5 * dayMs, Open: 120, High: 120, Low: 120, Close: 120, Volume: 1},
	}
	r.LoadBars("BTCUSDT", bars)
	res, err := r.Run(s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// 每点回撤 <= 0，且 bar1（权益谷底 9799）回撤最小
	for _, pt := range res.EquityCurve {
		if pt.Drawdown > 0 {
			t.Fatalf("drawdown must be <= 0, got %+v", pt)
		}
	}
	if res.MaxDrawdownPct <= 0 {
		t.Fatal("expected positive max drawdown")
	}
	if res.MaxDrawdownPeakTime != dayMs || res.MaxDrawdownTroughTime != 2*dayMs {
		t.Fatalf("dd peak/trough time = %d/%d, want %d/%d",
			res.MaxDrawdownPeakTime, res.MaxDrawdownTroughTime, dayMs, 2*dayMs)
	}
	// 区间 4 天，期末权益 > 初值 → 年化收益显著为正；日频波动率年化 > 0
	if res.AnnualizedReturn <= 0 {
		t.Fatalf("annualizedReturn = %v, want > 0", res.AnnualizedReturn)
	}
	if res.AnnualizedVolatility <= 0 {
		t.Fatalf("annualizedVolatility = %v, want > 0", res.AnnualizedVolatility)
	}
	// 手续费 = 各成交手续费合计（入场 10×100×0.001=1，平仓 10×120×0.001=1.2）
	if !almostEq(res.TotalCommission, 2.2) {
		t.Fatalf("totalCommission = %v, want 2.2", res.TotalCommission)
	}
	var execSum float64
	for _, e := range res.Executions {
		execSum += e.Commission
	}
	if !almostEq(execSum, res.TotalCommission) {
		t.Fatalf("totalCommission %v != executions sum %v", res.TotalCommission, execSum)
	}
}
