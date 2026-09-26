package backtest

import (
	"fmt"
	"math"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 内置回测策略 ──
// 从 handler 包下沉（原 handler/backtest_strategies.go + handler/market.go 中的
// smaCross/breakout），供 agent MCP 工具等 handler 之外的调用方真实跑回测。
// 逻辑与 handler 侧保持一致，handler 未改动以避免扩大改动面。

// 内置策略名（与前端/AI 回测传参一致）。
const (
	BuiltinSmaCross       = "sma_cross"
	BuiltinBreakout       = "breakout"
	BuiltinMartinTrend    = "martin_trend"
	BuiltinWallstreet     = "wallstreet"
	BuiltinMACDGoldenLong = "macd_golden_long"
	BuiltinMACDDeathShort = "macd_death_short"
	BuiltinEMAFollowTrend = "ema_follow_trend"
	BuiltinEMACounter     = "ema_counter_trend"
	BuiltinDualBurn       = "dual_burn"
	BuiltinGlobalBurn     = "global_burn"
	BuiltinTrendLong      = "trend_long"
	BuiltinTrendShort     = "trend_short"
	BuiltinCounterStable  = "counter_stable"
	BuiltinHeadTailArb    = "head_tail_arb"
)

// BuiltinStrategyNames 返回全部内置策略名（供工具层列举）。
func BuiltinStrategyNames() []string {
	return []string{
		BuiltinSmaCross, BuiltinBreakout, BuiltinMartinTrend, BuiltinWallstreet,
		BuiltinMACDGoldenLong, BuiltinMACDDeathShort, BuiltinEMAFollowTrend,
		BuiltinEMACounter, BuiltinDualBurn, BuiltinGlobalBurn, BuiltinTrendLong,
		BuiltinTrendShort, BuiltinCounterStable, BuiltinHeadTailArb,
	}
}

// NewBuiltinStrategy 按名字创建内置回测策略；未知名字返回 nil。
func NewBuiltinStrategy(name, symbol string) BacktestStrategy {
	switch name {
	case BuiltinBreakout:
		return &breakoutStrategy{symbol: symbol, lookback: 20, bufferPct: 0.002, stopLossPct: 0.02, takeProfitPct: 0.04}
	case BuiltinMartinTrend:
		return &martinTrendStrategy{symbol: symbol}
	case BuiltinWallstreet:
		return &wallstreetStrategy{symbol: symbol}
	case BuiltinMACDGoldenLong:
		return &macdGoldenLongStrategy{symbol: symbol}
	case BuiltinMACDDeathShort:
		return &macdDeathShortStrategy{symbol: symbol}
	case BuiltinEMAFollowTrend:
		return &emaFollowTrendStrategy{symbol: symbol}
	case BuiltinEMACounter:
		return &emaCounterTrendStrategy{symbol: symbol}
	case BuiltinDualBurn:
		return &dualBurnStrategy{symbol: symbol}
	case BuiltinGlobalBurn:
		return &globalBurnStrategy{symbol: symbol}
	case BuiltinTrendLong:
		return &trendLongStrategy{symbol: symbol}
	case BuiltinTrendShort:
		return &trendShortStrategy{symbol: symbol}
	case BuiltinCounterStable:
		return &counterStableStrategy{symbol: symbol}
	case BuiltinHeadTailArb:
		return &headTailArbStrategy{symbol: symbol}
	case BuiltinSmaCross:
		fallthrough
	default:
		return &smaCrossStrategy{symbol: symbol, fastPeriod: 12, slowPeriod: 26}
	}
}

// ── 技术指标 ──

// btSMA 收盘价简单移动平均。
func btSMA(bars []model.Bar, period int) float64 {
	if len(bars) < period {
		period = len(bars)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

// btRangeHighLow 最近 N 根的最高/最低价。
func btRangeHighLow(bars []model.Bar, period int) (float64, float64) {
	if period > len(bars) {
		period = len(bars)
	}
	if period == 0 {
		return 0, 0
	}
	start := len(bars) - period
	if start < 0 {
		start = 0
	}
	highest := bars[start].High
	lowest := bars[start].Low
	for i := start + 1; i < len(bars); i++ {
		if bars[i].High > highest {
			highest = bars[i].High
		}
		if bars[i].Low < lowest {
			lowest = bars[i].Low
		}
	}
	return highest, lowest
}

// btEMA 指数移动平均。
func btEMA(bars []model.Bar, period int) float64 {
	if len(bars) == 0 {
		return 0
	}
	if len(bars) < period {
		return btSMA(bars, period)
	}
	k := 2.0 / (float64(period) + 1.0)
	val := bars[0].Close
	for i := 1; i < len(bars); i++ {
		val = (bars[i].Close-val)*k + val
	}
	return val
}

// btMACDValues 返回 MACD 线与信号线的当前值与前值。
func btMACDValues(bars []model.Bar) (macdCurr, signalCurr, macdPrev, signalPrev float64) {
	if len(bars) < 3 {
		return 0, 0, 0, 0
	}
	n := len(bars)
	e12 := make([]float64, n)
	e26 := make([]float64, n)
	m := make([]float64, n)
	s := make([]float64, n)
	k12 := 2.0 / 13.0
	k26 := 2.0 / 27.0
	k9 := 2.0 / 10.0

	e12[0] = bars[0].Close
	for i := 1; i < n; i++ {
		e12[i] = (bars[i].Close-e12[i-1])*k12 + e12[i-1]
	}
	e26[0] = bars[0].Close
	for i := 1; i < n; i++ {
		e26[i] = (bars[i].Close-e26[i-1])*k26 + e26[i-1]
	}
	for i := 0; i < n; i++ {
		m[i] = e12[i] - e26[i]
	}
	s[0] = m[0]
	for i := 1; i < n; i++ {
		s[i] = (m[i]-s[i-1])*k9 + s[i-1]
	}
	return m[n-1], s[n-1], m[n-2], s[n-2]
}

// btATR 平均真实波幅。
func btATR(bars []model.Bar, period int) float64 {
	if len(bars) < period+1 {
		return 0
	}
	sum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		tr1 := bars[i].High - bars[i].Low
		tr2 := math.Abs(bars[i].High - bars[i-1].Close)
		tr3 := math.Abs(bars[i].Low - bars[i-1].Close)
		tr := tr1
		if tr2 > tr {
			tr = tr2
		}
		if tr3 > tr {
			tr = tr3
		}
		sum += tr
	}
	return sum / float64(period)
}

// ── 小工具 ──

func btDefaultQty(state *StrategyState, price, pct float64) float64 {
	if price == 0 {
		return 0
	}
	return state.Equity * pct / price
}

func btCurrentPnLPct(pos *Position, price float64) float64 {
	if pos == nil || pos.Quantity == 0 || price == 0 {
		return 0
	}
	if pos.Side == model.SideBuy {
		return (price - pos.EntryPrice) / pos.EntryPrice
	}
	return (pos.EntryPrice - price) / pos.EntryPrice
}

// ── 1. SMA 金叉死叉 ──

type smaCrossStrategy struct {
	symbol     string
	fastPeriod int
	slowPeriod int
}

func (s *smaCrossStrategy) Name() string   { return BuiltinSmaCross }
func (s *smaCrossStrategy) Symbol() string { return s.symbol }
func (s *smaCrossStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < s.slowPeriod+1 {
		return nil, nil
	}
	fast := btSMA(bars, s.fastPeriod)
	slow := btSMA(bars, s.slowPeriod)
	prevFast := btSMA(bars[:len(bars)-1], s.fastPeriod)
	prevSlow := btSMA(bars[:len(bars)-1], s.slowPeriod)

	if prevFast <= prevSlow && fast > slow && state.Position == nil {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "sma golden cross"}, nil
	}
	if prevFast >= prevSlow && fast < slow && state.Position != nil {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "sma death cross"}, nil
	}
	return nil, nil
}
func (s *smaCrossStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}

// ── 2. 突破/跌破 ──

type breakoutStrategy struct {
	symbol        string
	lookback      int
	bufferPct     float64
	stopLossPct   float64
	takeProfitPct float64
}

func (s *breakoutStrategy) Name() string   { return BuiltinBreakout }
func (s *breakoutStrategy) Symbol() string { return s.symbol }
func (s *breakoutStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < s.lookback+2 {
		return nil, nil
	}
	if state.Position != nil {
		return s.checkExit(bar, state), nil
	}
	highest, lowest := btRangeHighLow(bars[:len(bars)-1], s.lookback)
	if highest <= 0 || lowest <= 0 {
		return nil, nil
	}
	rangeSize := highest - lowest
	buffer := rangeSize * s.bufferPct
	if bar.Close > highest+buffer {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "breakout above resistance"}, nil
	}
	if bar.Close < lowest-buffer {
		return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "breakdown below support"}, nil
	}
	return nil, nil
}
func (s *breakoutStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *breakoutStrategy) checkExit(bar model.Bar, state *StrategyState) *model.Signal {
	if state.Position == nil {
		return nil
	}
	entryPrice := state.Position.EntryPrice
	if state.Position.Side == model.SideBuy {
		if bar.Close <= entryPrice*(1-s.stopLossPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "long stop loss"}
		}
		if bar.Close >= entryPrice*(1+s.takeProfitPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "long take profit"}
		}
	} else {
		if bar.Close >= entryPrice*(1+s.stopLossPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "short stop loss"}
		}
		if bar.Close <= entryPrice*(1-s.takeProfitPct) {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "short take profit"}
		}
	}
	return nil
}

// ── 3. 马丁趋势 ──

type martinTrendStrategy struct {
	symbol string
	level  int
}

func (s *martinTrendStrategy) Name() string   { return BuiltinMartinTrend }
func (s *martinTrendStrategy) Symbol() string { return s.symbol }
func (s *martinTrendStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *martinTrendStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := btEMA(bars, 60)
	price := bar.Close

	if price <= ema60 {
		if state.Position != nil && !state.Position.IsClosed {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "price below ema60"}, nil
		}
		return nil, nil
	}

	if state.Position == nil || state.Position.IsClosed {
		s.level = 0
		qty := btDefaultQty(state, price, 0.02)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "martin initial long", Qty: qty}, nil
	}

	if state.Position.Side == model.SideBuy {
		pnlPct := btCurrentPnLPct(state.Position, price)
		threshold := -0.02 * float64(s.level+1)
		if pnlPct <= threshold && s.level < 5 {
			s.level++
			multipliers := []float64{2, 4, 8, 16, 32, 64}
			pct := 0.01 * float64(multipliers[s.level])
			qty := btDefaultQty(state, price, pct)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: fmt.Sprintf("martin add level %d", s.level), Qty: qty}, nil
		}
		if pnlPct >= 0.02 {
			s.level = 0
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "martin take profit"}, nil
		}
	}
	return nil, nil
}

// ── 4. 华尔街（斐波那契加仓）──

type wallstreetStrategy struct {
	symbol string
	level  int
}

func (s *wallstreetStrategy) Name() string   { return BuiltinWallstreet }
func (s *wallstreetStrategy) Symbol() string { return s.symbol }
func (s *wallstreetStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *wallstreetStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := btEMA(bars, 60)
	price := bar.Close

	if price <= ema60 {
		if state.Position != nil && !state.Position.IsClosed {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "price below ema60"}, nil
		}
		return nil, nil
	}

	if state.Position == nil || state.Position.IsClosed {
		s.level = 0
		qty := btDefaultQty(state, price, 0.01)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "wallstreet initial", Qty: qty}, nil
	}

	if state.Position.Side == model.SideBuy {
		pnlPct := btCurrentPnLPct(state.Position, price)
		threshold := -0.015 * float64(s.level+1)
		if pnlPct <= threshold && s.level < 8 {
			s.level++
			fibs := []float64{1, 2, 3, 5, 8, 13, 21, 34, 55}
			pct := 0.01 * fibs[s.level]
			qty := btDefaultQty(state, price, pct)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: fmt.Sprintf("wallstreet add %d", s.level), Qty: qty}, nil
		}
		if pnlPct >= 0.02 {
			s.level = 0
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "wallstreet take profit"}, nil
		}
	}
	return nil, nil
}

// ── 5. MACD 金叉做多 ──

type macdGoldenLongStrategy struct {
	symbol string
}

func (s *macdGoldenLongStrategy) Name() string   { return BuiltinMACDGoldenLong }
func (s *macdGoldenLongStrategy) Symbol() string { return s.symbol }
func (s *macdGoldenLongStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *macdGoldenLongStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 35 {
		return nil, nil
	}
	macdLine, signalLine, prevMacd, prevSignal := btMACDValues(bars)
	goldenCross := prevMacd <= prevSignal && macdLine > signalLine
	deathCross := prevMacd >= prevSignal && macdLine < signalLine

	if goldenCross {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd golden cross", Qty: qty}, nil
		}
		if state.Position.Side == model.SideBuy {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd golden cross add", Qty: qty}, nil
		}
	}
	if deathCross && state.Position != nil && !state.Position.IsClosed {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd death cross clear"}, nil
	}
	return nil, nil
}

// ── 6. MACD 死叉做空 ──

type macdDeathShortStrategy struct {
	symbol string
}

func (s *macdDeathShortStrategy) Name() string   { return BuiltinMACDDeathShort }
func (s *macdDeathShortStrategy) Symbol() string { return s.symbol }
func (s *macdDeathShortStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *macdDeathShortStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 35 {
		return nil, nil
	}
	macdLine, signalLine, prevMacd, prevSignal := btMACDValues(bars)
	deathCross := prevMacd >= prevSignal && macdLine < signalLine
	goldenCross := prevMacd <= prevSignal && macdLine > signalLine

	if deathCross {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd death cross", Qty: qty}, nil
		}
		if state.Position.Side == model.SideSell {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd death cross add", Qty: qty}, nil
		}
	}
	if goldenCross && state.Position != nil && !state.Position.IsClosed {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd golden cross clear"}, nil
	}
	return nil, nil
}

// ── 7. EMA 趋势跟随 ──

type emaFollowTrendStrategy struct {
	symbol string
}

func (s *emaFollowTrendStrategy) Name() string   { return BuiltinEMAFollowTrend }
func (s *emaFollowTrendStrategy) Symbol() string { return s.symbol }
func (s *emaFollowTrendStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *emaFollowTrendStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema10 := btEMA(bars, 10)
	ema60 := btEMA(bars, 60)
	prevEma10 := btEMA(bars[:len(bars)-1], 10)
	price := bar.Close

	aboveEma60 := price > ema60
	ema10Rising := ema10 > prevEma10

	if aboveEma60 && ema10Rising {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, price, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema follow trend long", Qty: qty}, nil
		}
	}

	if state.Position != nil && !state.Position.IsClosed && state.Position.Side == model.SideBuy {
		if !aboveEma60 || !ema10Rising {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema trend broken"}, nil
		}
	}
	return nil, nil
}

// ── 8. EMA 反向 ──

type emaCounterTrendStrategy struct {
	symbol string
}

func (s *emaCounterTrendStrategy) Name() string   { return BuiltinEMACounter }
func (s *emaCounterTrendStrategy) Symbol() string { return s.symbol }
func (s *emaCounterTrendStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *emaCounterTrendStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := btEMA(bars, 60)
	atrVal := btATR(bars, 14)
	price := bar.Close
	atrPct := 0.0
	if price > 0 {
		atrPct = atrVal / price
	}
	if atrPct < 0.005 {
		return nil, nil
	}

	if price > ema60 {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, price, 0.01)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema counter short", Qty: qty}, nil
		}
	} else if price < ema60 {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, price, 0.01)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema counter long", Qty: qty}, nil
		}
	}

	if state.Position != nil && !state.Position.IsClosed {
		if state.Position.Side == model.SideBuy && price >= ema60 {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter revert to ema60"}, nil
		}
		if state.Position.Side == model.SideSell && price <= ema60 {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter revert to ema60"}, nil
		}
	}
	return nil, nil
}

// ── 9. Dual Burn ──

type dualBurnStrategy struct {
	symbol       string
	pendingClose bool
	reverseDir   string
}

func (s *dualBurnStrategy) Name() string   { return BuiltinDualBurn }
func (s *dualBurnStrategy) Symbol() string { return s.symbol }
func (s *dualBurnStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *dualBurnStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 20 {
		return nil, nil
	}

	if s.pendingClose {
		s.pendingClose = false
		if s.reverseDir == "LONG" {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn reverse long", Qty: qty}, nil
		} else if s.reverseDir == "SHORT" {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn reverse short", Qty: qty}, nil
		}
	}

	if state.Position == nil || state.Position.IsClosed {
		ema20 := btEMA(bars, 20)
		prevEma20 := btEMA(bars[:len(bars)-1], 20)
		if bar.Close > ema20 && ema20 > prevEma20 {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn initial long", Qty: qty}, nil
		}
		if bar.Close < ema20 && ema20 < prevEma20 {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn initial short", Qty: qty}, nil
		}
		return nil, nil
	}

	pnlPct := btCurrentPnLPct(state.Position, bar.Close)
	if pnlPct <= -0.03 {
		s.pendingClose = true
		if state.Position.Side == model.SideBuy {
			s.reverseDir = "SHORT"
		} else {
			s.reverseDir = "LONG"
		}
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn stop and reverse"}, nil
	}
	if pnlPct >= 0.03 {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn take profit"}, nil
	}
	return nil, nil
}

// ── 10. Global Burn ──

type globalBurnStrategy struct {
	symbol       string
	pendingClose bool
	reverseDir   string
}

func (s *globalBurnStrategy) Name() string   { return BuiltinGlobalBurn }
func (s *globalBurnStrategy) Symbol() string { return s.symbol }
func (s *globalBurnStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *globalBurnStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 20 {
		return nil, nil
	}

	if s.pendingClose {
		s.pendingClose = false
		if s.reverseDir == "LONG" {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn reverse long", Qty: qty}, nil
		} else if s.reverseDir == "SHORT" {
			qty := btDefaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn reverse short", Qty: qty}, nil
		}
	}

	if state.Position == nil || state.Position.IsClosed {
		ema20 := btEMA(bars, 20)
		prevEma20 := btEMA(bars[:len(bars)-1], 20)
		if bar.Close > ema20 && ema20 > prevEma20 {
			qty := btDefaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn initial long", Qty: qty}, nil
		}
		if bar.Close < ema20 && ema20 < prevEma20 {
			qty := btDefaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn initial short", Qty: qty}, nil
		}
		return nil, nil
	}

	pnlPct := btCurrentPnLPct(state.Position, bar.Close)
	if pnlPct <= -0.05 {
		s.pendingClose = true
		if state.Position.Side == model.SideBuy {
			s.reverseDir = "SHORT"
		} else {
			s.reverseDir = "LONG"
		}
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn stop and reverse"}, nil
	}
	if pnlPct >= 0.05 {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn take profit"}, nil
	}
	return nil, nil
}

// ── 11. 趋势做多 ──

type trendLongStrategy struct {
	symbol string
}

func (s *trendLongStrategy) Name() string   { return BuiltinTrendLong }
func (s *trendLongStrategy) Symbol() string { return s.symbol }
func (s *trendLongStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *trendLongStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 26 {
		return nil, nil
	}
	ema12 := btEMA(bars, 12)
	ema26 := btEMA(bars, 26)
	prevEma12 := btEMA(bars[:len(bars)-1], 12)
	prevEma26 := btEMA(bars[:len(bars)-1], 26)

	goldenCross := prevEma12 <= prevEma26 && ema12 > ema26
	deathCross := prevEma12 >= prevEma26 && ema12 < ema26

	if goldenCross && (state.Position == nil || state.Position.IsClosed) {
		qty := btDefaultQty(state, bar.Close, 0.02)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema golden cross long", Qty: qty}, nil
	}
	if deathCross && state.Position != nil && !state.Position.IsClosed && state.Position.Side == model.SideBuy {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema death cross close"}, nil
	}
	return nil, nil
}

// ── 12. 趋势做空 ──

type trendShortStrategy struct {
	symbol string
}

func (s *trendShortStrategy) Name() string   { return BuiltinTrendShort }
func (s *trendShortStrategy) Symbol() string { return s.symbol }
func (s *trendShortStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *trendShortStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 26 {
		return nil, nil
	}
	ema12 := btEMA(bars, 12)
	ema26 := btEMA(bars, 26)
	prevEma12 := btEMA(bars[:len(bars)-1], 12)
	prevEma26 := btEMA(bars[:len(bars)-1], 26)

	deathCross := prevEma12 >= prevEma26 && ema12 < ema26
	goldenCross := prevEma12 <= prevEma26 && ema12 > ema26

	if deathCross && (state.Position == nil || state.Position.IsClosed) {
		qty := btDefaultQty(state, bar.Close, 0.02)
		return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema death cross short", Qty: qty}, nil
	}
	if goldenCross && state.Position != nil && !state.Position.IsClosed && state.Position.Side == model.SideSell {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema golden cross close"}, nil
	}
	return nil, nil
}

// ── 13. 逆势稳健 ──

type counterStableStrategy struct {
	symbol string
}

func (s *counterStableStrategy) Name() string   { return BuiltinCounterStable }
func (s *counterStableStrategy) Symbol() string { return s.symbol }
func (s *counterStableStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *counterStableStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := btEMA(bars, 60)
	atrVal := btATR(bars, 14)
	price := bar.Close

	atrPct := 0.0
	if price > 0 {
		atrPct = atrVal / price
	}
	basePct := 0.01
	if atrPct > 0.02 {
		basePct = 0.005
	} else if atrPct < 0.01 {
		basePct = 0.015
	}

	if price > ema60*1.005 {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, price, basePct)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter stable short", Qty: qty}, nil
		}
	} else if price < ema60*0.995 {
		if state.Position == nil || state.Position.IsClosed {
			qty := btDefaultQty(state, price, basePct)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter stable long", Qty: qty}, nil
		}
	}

	if state.Position != nil && !state.Position.IsClosed {
		pnlPct := btCurrentPnLPct(state.Position, price)
		if pnlPct >= 0.01 || pnlPct <= -0.01 {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter stable exit"}, nil
		}
	}
	return nil, nil
}

// ── 14. 首尾套利 ──

type headTailArbStrategy struct {
	symbol    string
	confirmed bool
}

func (s *headTailArbStrategy) Name() string   { return BuiltinHeadTailArb }
func (s *headTailArbStrategy) Symbol() string { return s.symbol }
func (s *headTailArbStrategy) OnTick(tick model.Tick, state *StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *headTailArbStrategy) OnBar(bar model.Bar, state *StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 3 {
		return nil, nil
	}

	prev1 := bars[len(bars)-2]
	prev2 := bars[len(bars)-3]
	bullish := prev1.Close > prev1.Open && prev2.Close > prev2.Open && bar.Close > bar.Open
	bearish := prev1.Close < prev1.Open && prev2.Close < prev2.Open && bar.Close < bar.Open

	if state.Position == nil || state.Position.IsClosed {
		s.confirmed = false
		if bullish {
			qty := btDefaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail probe long", Qty: qty}, nil
		}
		if bearish {
			qty := btDefaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail probe short", Qty: qty}, nil
		}
		return nil, nil
	}

	pnlPct := btCurrentPnLPct(state.Position, bar.Close)

	if !s.confirmed && pnlPct > 0.005 {
		s.confirmed = true
		addQty := btDefaultQty(state, bar.Close, 0.02)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail confirm add", Qty: addQty}, nil
	}

	if pnlPct < -0.01 {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail stop loss"}, nil
	}
	if pnlPct > 0.02 {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail take profit"}, nil
	}
	return nil, nil
}
