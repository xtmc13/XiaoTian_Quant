package handler

import (
	"fmt"
	"math"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Technical Indicators ──

func ema(bars []model.Bar, period int) float64 {
	if len(bars) == 0 {
		return 0
	}
	if len(bars) < period {
		return sma(bars, period)
	}
	k := 2.0 / (float64(period) + 1.0)
	val := bars[0].Close
	for i := 1; i < len(bars); i++ {
		val = (bars[i].Close-val)*k + val
	}
	return val
}

func macdValues(bars []model.Bar) (macdCurr, signalCurr, macdPrev, signalPrev float64) {
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

func rsi(bars []model.Bar, period int) float64 {
	if len(bars) < period+1 {
		return 50
	}
	gain, loss := 0.0, 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		ch := bars[i].Close - bars[i-1].Close
		if ch > 0 {
			gain += ch
		} else {
			loss += -ch
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	if avgLoss == 0 {
		return 100
	}
	return 100.0 - (100.0 / (1.0+avgGain/avgLoss))
}

func atr(bars []model.Bar, period int) float64 {
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

// ── Helpers ──

func defaultQty(state *backtest.StrategyState, price, pct float64) float64 {
	if price == 0 {
		return 0
	}
	return state.Equity * pct / price
}

func currentPnLPct(pos *backtest.Position, price float64) float64 {
	if pos == nil || pos.Quantity == 0 || price == 0 {
		return 0
	}
	if pos.Side == model.SideBuy {
		return (price - pos.EntryPrice) / pos.EntryPrice
	}
	return (pos.EntryPrice - price) / pos.EntryPrice
}

// ── 1. Martin Trend ──

type martinTrendStrategy struct {
	symbol string
	level  int
}

func (s *martinTrendStrategy) Name() string   { return "martin_trend" }
func (s *martinTrendStrategy) Symbol() string { return s.symbol }
func (s *martinTrendStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *martinTrendStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := ema(bars, 60)
	price := bar.Close

	if price <= ema60 {
		if state.Position != nil && !state.Position.IsClosed {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "price below ema60"}, nil
		}
		return nil, nil
	}

	if state.Position == nil || state.Position.IsClosed {
		s.level = 0
		qty := defaultQty(state, price, 0.02)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "martin initial long", Qty: qty}, nil
	}

	if state.Position.Side == model.SideBuy {
		pnlPct := currentPnLPct(state.Position, price)
		threshold := -0.02 * float64(s.level+1)
		if pnlPct <= threshold && s.level < 5 {
			s.level++
			multipliers := []float64{2, 4, 8, 16, 32, 64}
			pct := 0.01 * float64(multipliers[s.level])
			qty := defaultQty(state, price, pct)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: fmt.Sprintf("martin add level %d", s.level), Qty: qty}, nil
		}
		if pnlPct >= 0.02 {
			s.level = 0
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "martin take profit"}, nil
		}
	}
	return nil, nil
}

// ── 2. Wall Street ──

type wallstreetStrategy struct {
	symbol string
	level  int
}

func (s *wallstreetStrategy) Name() string   { return "wallstreet" }
func (s *wallstreetStrategy) Symbol() string { return s.symbol }
func (s *wallstreetStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *wallstreetStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := ema(bars, 60)
	price := bar.Close

	if price <= ema60 {
		if state.Position != nil && !state.Position.IsClosed {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "price below ema60"}, nil
		}
		return nil, nil
	}

	if state.Position == nil || state.Position.IsClosed {
		s.level = 0
		qty := defaultQty(state, price, 0.01)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "wallstreet initial", Qty: qty}, nil
	}

	if state.Position.Side == model.SideBuy {
		pnlPct := currentPnLPct(state.Position, price)
		threshold := -0.015 * float64(s.level+1)
		if pnlPct <= threshold && s.level < 8 {
			s.level++
			fibs := []float64{1, 2, 3, 5, 8, 13, 21, 34, 55}
			pct := 0.01 * fibs[s.level]
			qty := defaultQty(state, price, pct)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: fmt.Sprintf("wallstreet add %d", s.level), Qty: qty}, nil
		}
		if pnlPct >= 0.02 {
			s.level = 0
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "wallstreet take profit"}, nil
		}
	}
	return nil, nil
}

// ── 3. MACD Golden Long ──

type macdGoldenLongStrategy struct {
	symbol string
}

func (s *macdGoldenLongStrategy) Name() string   { return "macd_golden_long" }
func (s *macdGoldenLongStrategy) Symbol() string { return s.symbol }
func (s *macdGoldenLongStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *macdGoldenLongStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 35 {
		return nil, nil
	}
	macdLine, signalLine, prevMacd, prevSignal := macdValues(bars)
	goldenCross := prevMacd <= prevSignal && macdLine > signalLine
	deathCross := prevMacd >= prevSignal && macdLine < signalLine

	if goldenCross {
		if state.Position == nil || state.Position.IsClosed {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd golden cross", Qty: qty}, nil
		}
		if state.Position.Side == model.SideBuy {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd golden cross add", Qty: qty}, nil
		}
	}
	if deathCross && state.Position != nil && !state.Position.IsClosed {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd death cross clear"}, nil
	}
	return nil, nil
}

// ── 4. MACD Death Short ──

type macdDeathShortStrategy struct {
	symbol string
}

func (s *macdDeathShortStrategy) Name() string   { return "macd_death_short" }
func (s *macdDeathShortStrategy) Symbol() string { return s.symbol }
func (s *macdDeathShortStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *macdDeathShortStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 35 {
		return nil, nil
	}
	macdLine, signalLine, prevMacd, prevSignal := macdValues(bars)
	deathCross := prevMacd >= prevSignal && macdLine < signalLine
	goldenCross := prevMacd <= prevSignal && macdLine > signalLine

	if deathCross {
		if state.Position == nil || state.Position.IsClosed {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd death cross", Qty: qty}, nil
		}
		if state.Position.Side == model.SideSell {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd death cross add", Qty: qty}, nil
		}
	}
	if goldenCross && state.Position != nil && !state.Position.IsClosed {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "macd golden cross clear"}, nil
	}
	return nil, nil
}

// ── 5. EMA Follow Trend ──

type emaFollowTrendStrategy struct {
	symbol string
}

func (s *emaFollowTrendStrategy) Name() string   { return "ema_follow_trend" }
func (s *emaFollowTrendStrategy) Symbol() string { return s.symbol }
func (s *emaFollowTrendStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *emaFollowTrendStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema10 := ema(bars, 10)
	ema60 := ema(bars, 60)
	prevEma10 := ema(bars[:len(bars)-1], 10)
	price := bar.Close

	aboveEma60 := price > ema60
	ema10Rising := ema10 > prevEma10

	if aboveEma60 && ema10Rising {
		if state.Position == nil || state.Position.IsClosed {
			qty := defaultQty(state, price, 0.02)
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

// ── 6. EMA Counter Trend ──

type emaCounterTrendStrategy struct {
	symbol string
}

func (s *emaCounterTrendStrategy) Name() string   { return "ema_counter_trend" }
func (s *emaCounterTrendStrategy) Symbol() string { return s.symbol }
func (s *emaCounterTrendStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *emaCounterTrendStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := ema(bars, 60)
	atrVal := atr(bars, 14)
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
			qty := defaultQty(state, price, 0.01)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema counter short", Qty: qty}, nil
		}
	} else if price < ema60 {
		if state.Position == nil || state.Position.IsClosed {
			qty := defaultQty(state, price, 0.01)
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

// ── 7. Dual Burn ──

type dualBurnStrategy struct {
	symbol       string
	pendingClose bool
	reverseDir   string
}

func (s *dualBurnStrategy) Name() string   { return "dual_burn" }
func (s *dualBurnStrategy) Symbol() string { return s.symbol }
func (s *dualBurnStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *dualBurnStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 20 {
		return nil, nil
	}

	if s.pendingClose {
		s.pendingClose = false
		if s.reverseDir == "LONG" {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn reverse long", Qty: qty}, nil
		} else if s.reverseDir == "SHORT" {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn reverse short", Qty: qty}, nil
		}
	}

	if state.Position == nil || state.Position.IsClosed {
		ema20 := ema(bars, 20)
		prevEma20 := ema(bars[:len(bars)-1], 20)
		if bar.Close > ema20 && ema20 > prevEma20 {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn initial long", Qty: qty}, nil
		}
		if bar.Close < ema20 && ema20 < prevEma20 {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "dual burn initial short", Qty: qty}, nil
		}
		return nil, nil
	}

	pnlPct := currentPnLPct(state.Position, bar.Close)
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

// ── 8. Global Burn ──

type globalBurnStrategy struct {
	symbol       string
	pendingClose bool
	reverseDir   string
}

func (s *globalBurnStrategy) Name() string   { return "global_burn" }
func (s *globalBurnStrategy) Symbol() string { return s.symbol }
func (s *globalBurnStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *globalBurnStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 20 {
		return nil, nil
	}

	if s.pendingClose {
		s.pendingClose = false
		if s.reverseDir == "LONG" {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn reverse long", Qty: qty}, nil
		} else if s.reverseDir == "SHORT" {
			qty := defaultQty(state, bar.Close, 0.02)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn reverse short", Qty: qty}, nil
		}
	}

	if state.Position == nil || state.Position.IsClosed {
		ema20 := ema(bars, 20)
		prevEma20 := ema(bars[:len(bars)-1], 20)
		if bar.Close > ema20 && ema20 > prevEma20 {
			qty := defaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn initial long", Qty: qty}, nil
		}
		if bar.Close < ema20 && ema20 < prevEma20 {
			qty := defaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "global burn initial short", Qty: qty}, nil
		}
		return nil, nil
	}

	pnlPct := currentPnLPct(state.Position, bar.Close)
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

// ── 9. Trend Long ──

type trendLongStrategy struct {
	symbol string
}

func (s *trendLongStrategy) Name() string   { return "trend_long" }
func (s *trendLongStrategy) Symbol() string { return s.symbol }
func (s *trendLongStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *trendLongStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 26 {
		return nil, nil
	}
	ema12 := ema(bars, 12)
	ema26 := ema(bars, 26)
	prevEma12 := ema(bars[:len(bars)-1], 12)
	prevEma26 := ema(bars[:len(bars)-1], 26)

	goldenCross := prevEma12 <= prevEma26 && ema12 > ema26
	deathCross := prevEma12 >= prevEma26 && ema12 < ema26

	if goldenCross && (state.Position == nil || state.Position.IsClosed) {
		qty := defaultQty(state, bar.Close, 0.02)
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema golden cross long", Qty: qty}, nil
	}
	if deathCross && state.Position != nil && !state.Position.IsClosed && state.Position.Side == model.SideBuy {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema death cross close"}, nil
	}
	return nil, nil
}

// ── 10. Trend Short ──

type trendShortStrategy struct {
	symbol string
}

func (s *trendShortStrategy) Name() string   { return "trend_short" }
func (s *trendShortStrategy) Symbol() string { return s.symbol }
func (s *trendShortStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *trendShortStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 26 {
		return nil, nil
	}
	ema12 := ema(bars, 12)
	ema26 := ema(bars, 26)
	prevEma12 := ema(bars[:len(bars)-1], 12)
	prevEma26 := ema(bars[:len(bars)-1], 26)

	deathCross := prevEma12 >= prevEma26 && ema12 < ema26
	goldenCross := prevEma12 <= prevEma26 && ema12 > ema26

	if deathCross && (state.Position == nil || state.Position.IsClosed) {
		qty := defaultQty(state, bar.Close, 0.02)
		return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema death cross short", Qty: qty}, nil
	}
	if goldenCross && state.Position != nil && !state.Position.IsClosed && state.Position.Side == model.SideSell {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "ema golden cross close"}, nil
	}
	return nil, nil
}

// ── 11. Counter Stable ──

type counterStableStrategy struct {
	symbol string
}

func (s *counterStableStrategy) Name() string   { return "counter_stable" }
func (s *counterStableStrategy) Symbol() string { return s.symbol }
func (s *counterStableStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *counterStableStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	bars := state.Bars
	if len(bars) < 60 {
		return nil, nil
	}
	ema60 := ema(bars, 60)
	atrVal := atr(bars, 14)
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
			qty := defaultQty(state, price, basePct)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter stable short", Qty: qty}, nil
		}
	} else if price < ema60*0.995 {
		if state.Position == nil || state.Position.IsClosed {
			qty := defaultQty(state, price, basePct)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter stable long", Qty: qty}, nil
		}
	}

	if state.Position != nil && !state.Position.IsClosed {
		pnlPct := currentPnLPct(state.Position, price)
		if pnlPct >= 0.01 || pnlPct <= -0.01 {
			return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "counter stable exit"}, nil
		}
	}
	return nil, nil
}

// ── 12. Head Tail Arb ──

type headTailArbStrategy struct {
	symbol    string
	confirmed bool
}

func (s *headTailArbStrategy) Name() string   { return "head_tail_arb" }
func (s *headTailArbStrategy) Symbol() string { return s.symbol }
func (s *headTailArbStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *headTailArbStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
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
			qty := defaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail probe long", Qty: qty}, nil
		}
		if bearish {
			qty := defaultQty(state, bar.Close, 0.01)
			return &model.Signal{Direction: "SHORT", Symbol: s.symbol, Strategy: s.Name(), Reason: "head tail probe short", Qty: qty}, nil
		}
		return nil, nil
	}

	pnlPct := currentPnLPct(state.Position, bar.Close)

	if !s.confirmed && pnlPct > 0.005 {
		s.confirmed = true
		addQty := defaultQty(state, bar.Close, 0.02)
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
