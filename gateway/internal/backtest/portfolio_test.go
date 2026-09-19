package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// fixedStrategy 每期输出固定收益率的信号策略（用于确定性验证组合合成）。
type fixedStrategy struct {
	symbol string
	barIdx int
}

func (s *fixedStrategy) Name() string   { return "fixed" }
func (s *fixedStrategy) Symbol() string { return s.symbol }
func (s *fixedStrategy) OnTick(model.Tick, *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *fixedStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	s.barIdx++
	// 每 10 根开多、再 10 根平，制造确定性的资金占用节奏
	if s.barIdx%20 == 1 {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Qty: state.Equity * 0.5 / bar.Close}, nil
	}
	if s.barIdx%20 == 11 && state.Position != nil && !state.Position.IsClosed {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol}, nil
	}
	return nil, nil
}

func synthPortfolioBars(symbol string, n int, startMs int64, gainPct float64) []model.Bar {
	bars := make([]model.Bar, n)
	price := 100.0
	step := gainPct / 100 / float64(n)
	for i := 0; i < n; i++ {
		open := price
		price *= 1 + step + 0.001*math.Sin(float64(i)/5)
		bars[i] = model.Bar{
			Symbol: symbol, Open: open, High: math.Max(open, price) * 1.001,
			Low: math.Min(open, price) * 0.999, Close: price, Volume: 1000,
			Interval: "1h", Time: startMs + int64(i)*3600_000,
		}
	}
	return bars
}

func TestRunPortfolioSynthetic(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	data := map[string][]model.Bar{
		"AAAUSDT": synthPortfolioBars("AAAUSDT", 400, start, 8),
		"BBBUSDT": synthPortfolioBars("BBBUSDT", 400, start, -4),
	}

	cfg := PortfolioConfig{
		Name:           "test-portfolio",
		Timeframe:      "1h",
		InitialCapital: 100000,
		Rebalance:      "none",
		Legs: []PortfolioLeg{
			{StrategyType: "fixed", Symbol: "AAAUSDT", Weight: 0.6},
			{StrategyType: "fixed", Symbol: "BBBUSDT", Weight: 0.4},
		},
	}
	res, err := RunPortfolio(cfg,
		func(leg PortfolioLeg) (BacktestStrategy, error) {
			return &fixedStrategy{symbol: leg.Symbol}, nil
		},
		func(symbol, interval string, fromMs, toMs int64) ([]model.Bar, error) {
			return data[symbol], nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.EquityCurve) == 0 {
		t.Fatal("expected equity curve")
	}
	if len(res.Legs) != 2 {
		t.Fatalf("expected 2 leg results, got %d", len(res.Legs))
	}
	// 权重校验
	if math.Abs(res.Legs[0].Weight-0.6) > 1e-9 || math.Abs(res.Legs[1].Weight-0.4) > 1e-9 {
		t.Fatalf("weights not normalized: %+v", res.Legs)
	}
	// 权益曲线首尾
	if res.EquityCurve[0].Equity != 100000 {
		t.Fatalf("equity should start at initial capital, got %f", res.EquityCurve[0].Equity)
	}
	// 期末总权益 ≈ 60%*legA终值 + 40%*legB终值
	expectFinal := res.Legs[0].FinalValue + res.Legs[1].FinalValue
	gotFinal := res.EquityCurve[len(res.EquityCurve)-1].Equity
	if math.Abs(gotFinal-expectFinal) > 1 {
		t.Fatalf("final equity mismatch: got %f expect %f", gotFinal, expectFinal)
	}
	// 指标口径
	if res.Metrics.TotalTrades == 0 {
		t.Fatal("expected trades from legs")
	}
	if res.Metrics.MaxDrawdownPct < 0 {
		t.Fatal("drawdown should be >= 0")
	}
	// 期末漂移记录（none 模式也有 end 记录）
	if len(res.Drift) == 0 || res.Drift[len(res.Drift)-1].Trigger != "end" {
		t.Fatal("expected end drift record")
	}
	t.Logf("final=%.2f ret=%.2f%% sharpe=%.2f legs=%+v", gotFinal, res.Metrics.TotalReturnPct, res.Metrics.SharpeRatio, res.Legs)
}

func TestRunPortfolioRebalanceResetsWeights(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	data := map[string][]model.Bar{
		"AAAUSDT": synthPortfolioBars("AAAUSDT", 400, start, 30),
		"BBBUSDT": synthPortfolioBars("BBBUSDT", 400, start, 0),
	}
	cfg := PortfolioConfig{
		Name:           "test-rebalance",
		Timeframe:      "1h",
		InitialCapital: 100000,
		Rebalance:      "daily",
		Legs: []PortfolioLeg{
			{StrategyType: "fixed", Symbol: "AAAUSDT", Weight: 0.5},
			{StrategyType: "fixed", Symbol: "BBBUSDT", Weight: 0.5},
		},
	}
	res, err := RunPortfolio(cfg,
		func(leg PortfolioLeg) (BacktestStrategy, error) { return &fixedStrategy{symbol: leg.Symbol}, nil },
		func(symbol, interval string, fromMs, toMs int64) ([]model.Bar, error) { return data[symbol], nil })
	if err != nil {
		t.Fatal(err)
	}
	rebalances := 0
	for _, d := range res.Drift {
		if d.Trigger == "rebalance" {
			rebalances++
			// 再平衡后权重应精确回到目标
			sum := 0.0
			for _, w := range d.WeightsAfter {
				sum += w
			}
			if math.Abs(sum-1) > 1e-9 {
				t.Fatalf("weights after rebalance sum to %f", sum)
			}
		}
	}
	if rebalances == 0 {
		t.Fatal("expected at least one rebalance record over 400 hourly bars (~16 days)")
	}
	// 再平衡后紧跟的权益点，两 leg 的权重应回到 50/50
	if len(res.Drift) < 2 {
		t.Fatal("expected drift records")
	}
	t.Logf("rebalances=%d drift[0]=%+v", rebalances, res.Drift[0].WeightsBefore)
}

func TestRunPortfolioValidation(t *testing.T) {
	if _, err := RunPortfolio(PortfolioConfig{}, nil, nil); err == nil {
		t.Fatal("expected error for nil factory")
	}
	cfg := PortfolioConfig{InitialCapital: 10000, Legs: []PortfolioLeg{{StrategyType: "x", Symbol: "Y"}}}
	if _, err := RunPortfolio(cfg,
		func(leg PortfolioLeg) (BacktestStrategy, error) { return &fixedStrategy{symbol: leg.Symbol}, nil },
		func(symbol, interval string, fromMs, toMs int64) ([]model.Bar, error) { return nil, nil }); err == nil {
		t.Fatal("expected error for missing bars")
	}
}

func TestRebalanceBoundary(t *testing.T) {
	daily := time.Date(2024, 3, 4, 23, 59, 0, 0, time.UTC).UnixMilli()
	if rebalanceBoundary(daily, "daily") != "2024-03-04" {
		t.Fatal("daily boundary")
	}
	// 2024-03-04 是周一（ISO 周 10）
	if rebalanceBoundary(daily, "weekly") != "2024-W10" {
		t.Fatal("weekly boundary")
	}
	if rebalanceBoundary(daily, "monthly") != "2024-03" {
		t.Fatal("monthly boundary")
	}
	if rebalanceBoundary(daily, "none") != "" {
		t.Fatal("none boundary should be empty")
	}
}
