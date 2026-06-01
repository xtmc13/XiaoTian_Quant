package factor

import (
	"math"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Price Factor ──

// PriceFactor tracks the latest close price.
type PriceFactor struct{ BaseFactor }

func NewPriceFactor() *PriceFactor {
	return &PriceFactor{NewBaseFactor("price")}
}

func (f *PriceFactor) Update(bar model.Bar) {
	f.BaseFactor.push(bar.Close)
}

// ── Volume Factor ──

// VolumeFactor tracks trading volume.
type VolumeFactor struct {
	BaseFactor
	prevVolume float64
}

func NewVolumeFactor() *VolumeFactor {
	return &VolumeFactor{BaseFactor: NewBaseFactor("volume")}
}

func (f *VolumeFactor) Update(bar model.Bar) {
	ratio := 0.0
	if f.prevVolume > 0 {
		ratio = bar.Volume / f.prevVolume
	}
	f.prevVolume = bar.Volume
	f.BaseFactor.push(ratio)
}

// ── RSI Factor (14-period) ──

// RSIFactor computes the Relative Strength Index.
type RSIFactor struct {
	BaseFactor
	period int
	gains  []float64
	losses []float64
	prevClose float64
}

func NewRSIFactor(period int) *RSIFactor {
	if period <= 0 {
		period = 14
	}
	return &RSIFactor{
		BaseFactor: NewBaseFactor("rsi"),
		period:     period,
	}
}

func (f *RSIFactor) Update(bar model.Bar) {
	if f.prevClose == 0 {
		f.prevClose = bar.Close
		return
	}
	change := bar.Close - f.prevClose
	f.prevClose = bar.Close

	gain, loss := 0.0, 0.0
	if change >= 0 {
		gain = change
	} else {
		loss = -change
	}

	f.gains = append(f.gains, gain)
	f.losses = append(f.losses, loss)
	if len(f.gains) > f.period*2 {
		f.gains = f.gains[len(f.gains)-f.period*2:]
		f.losses = f.losses[len(f.losses)-f.period*2:]
	}

	if len(f.gains) < f.period {
		return
	}

	avgGain := average(f.gains[len(f.gains)-f.period:])
	avgLoss := average(f.losses[len(f.losses)-f.period:])

	if avgLoss == 0 {
		f.BaseFactor.push(100)
		return
	}
	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))
	f.BaseFactor.push(rsi)
}

// ── MACD Factor (12, 26, 9) ──

// MACDFactor computes MACD line, signal line, and histogram.
type MACDFactor struct {
	BaseFactor
	fastPeriod   int
	slowPeriod   int
	signalPeriod int
	prices       []float64
	signalLine   float64
	histogram    float64
}

func NewMACDFactor(fast, slow, signal int) *MACDFactor {
	if fast <= 0 {
		fast = 12
	}
	if slow <= 0 {
		slow = 26
	}
	if signal <= 0 {
		signal = 9
	}
	return &MACDFactor{
		BaseFactor:   NewBaseFactor("macd"),
		fastPeriod:   fast,
		slowPeriod:   slow,
		signalPeriod: signal,
	}
}

func (f *MACDFactor) Update(bar model.Bar) {
	f.prices = append(f.prices, bar.Close)
	if len(f.prices) > f.slowPeriod+f.signalPeriod {
		f.prices = f.prices[len(f.prices)-f.slowPeriod-f.signalPeriod:]
	}

	if len(f.prices) < f.slowPeriod {
		return
	}

	fastEMA := ema(f.prices, f.fastPeriod)
	slowEMA := ema(f.prices, f.slowPeriod)
	macdLine := fastEMA - slowEMA

	signalLine := macdLine
	if f.signalLine != 0 {
		signalLine = f.signalLine + (macdLine-f.signalLine)*(2.0/(float64(f.signalPeriod)+1))
	}
	f.signalLine = signalLine
	f.histogram = macdLine - signalLine
	f.BaseFactor.push(macdLine)
}

// Histogram returns the MACD histogram.
func (f *MACDFactor) Histogram() float64 { return f.histogram }

// Signal returns the MACD signal line.
func (f *MACDFactor) Signal() float64 { return f.signalLine }

// ── OrderBook Imbalance Factor ──

// OBImbalanceFactor tracks the smoothed order book imbalance.
type OBImbalanceFactor struct {
	BaseFactor
	depth int
}

func NewOBImbalanceFactor(depth int) *OBImbalanceFactor {
	if depth <= 0 {
		depth = 5
	}
	return &OBImbalanceFactor{
		BaseFactor: NewBaseFactor("ob_imbalance"),
		depth:      depth,
	}
}

func (f *OBImbalanceFactor) UpdateFromOB(ob model.OrderBookData) {
	imb := ob.Imbalance(f.depth)
	f.BaseFactor.push(imb)
}

func (f *OBImbalanceFactor) Update(bar model.Bar) {
	// OB imbalance can only be updated from OrderBook events, not bars
}

// ── Spread Factor (bps) ──

// SpreadFactor tracks the bid-ask spread in basis points.
type SpreadFactor struct{ BaseFactor }

func NewSpreadFactor() *SpreadFactor {
	return &SpreadFactor{NewBaseFactor("spread_bps")}
}

func (f *SpreadFactor) UpdateFromOB(ob model.OrderBookData) {
	f.BaseFactor.push(ob.SpreadBps())
}

func (f *SpreadFactor) Update(bar model.Bar) {
	// Spread can only be updated from OrderBook events
}

// ── VWAP Factor (20-period) ──

// VWAPFactor computes the Volume-Weighted Average Price.
type VWAPFactor struct {
	BaseFactor
	period int
	prices []float64
	volumes []float64
}

func NewVWAPFactor(period int) *VWAPFactor {
	if period <= 0 {
		period = 20
	}
	return &VWAPFactor{
		BaseFactor: NewBaseFactor("vwap"),
		period:     period,
	}
}

func (f *VWAPFactor) Update(bar model.Bar) {
	f.prices = append(f.prices, bar.Close*bar.Volume)
	f.volumes = append(f.volumes, bar.Volume)

	if len(f.prices) > f.period {
		f.prices = f.prices[len(f.prices)-f.period:]
		f.volumes = f.volumes[len(f.volumes)-f.period:]
	}

	if len(f.prices) < f.period {
		return
	}

	priceVolSum := 0.0
	volSum := 0.0
	for i := 0; i < len(f.prices); i++ {
		priceVolSum += f.prices[i]
		volSum += f.volumes[i]
	}

	if volSum == 0 {
		return
	}
	vwap := priceVolSum / volSum

	// Store ratio of price to VWAP
	ratio := bar.Close / vwap
	f.BaseFactor.push(ratio)
}

// ── Momentum Factor (20-period) ──

// MomentumFactor computes price rate of change.
type MomentumFactor struct {
	BaseFactor
	period int
	prices []float64
}

func NewMomentumFactor(period int) *MomentumFactor {
	if period <= 0 {
		period = 20
	}
	return &MomentumFactor{
		BaseFactor: NewBaseFactor("momentum"),
		period:     period,
	}
}

func (f *MomentumFactor) Update(bar model.Bar) {
	f.prices = append(f.prices, bar.Close)
	if len(f.prices) > f.period+1 {
		f.prices = f.prices[len(f.prices)-f.period-1:]
	}

	if len(f.prices) < f.period+1 {
		return
	}

	prevPrice := f.prices[len(f.prices)-f.period-1]
	if prevPrice == 0 {
		return
	}
	momentum := (bar.Close - prevPrice) / prevPrice
	f.BaseFactor.push(momentum)
}

// ── Volatility Factor (20-period) ──

// VolatilityFactor computes annualized volatility.
type VolatilityFactor struct {
	BaseFactor
	period  int
	returns []float64
	prevPrice float64
}

func NewVolatilityFactor(period int) *VolatilityFactor {
	if period <= 0 {
		period = 20
	}
	return &VolatilityFactor{
		BaseFactor: NewBaseFactor("volatility"),
		period:     period,
	}
}

func (f *VolatilityFactor) Update(bar model.Bar) {
	if f.prevPrice > 0 {
		ret := (bar.Close - f.prevPrice) / f.prevPrice
		f.returns = append(f.returns, ret)
		if len(f.returns) > f.period {
			f.returns = f.returns[len(f.returns)-f.period:]
		}
	}
	f.prevPrice = bar.Close

	if len(f.returns) < 2 {
		return
	}

	avg := average(f.returns)
	variance := 0.0
	for _, r := range f.returns {
		variance += (r - avg) * (r - avg)
	}
	std := math.Sqrt(variance / float64(len(f.returns)-1))
	annualVol := std * math.Sqrt(252)
	f.BaseFactor.push(annualVol)
}

// ── Helpers ──

func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func ema(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}
	k := 2.0 / (float64(period) + 1)
	result := prices[len(prices)-period]
	for i := len(prices) - period + 1; i < len(prices); i++ {
		result = prices[i]*k + result*(1-k)
	}
	return result
}

// ── OBSpread Events ──

// UpdaterFromOB allows factors to be updated from order book data.
type UpdaterFromOB interface {
	UpdateFromOB(ob model.OrderBookData)
}

// UpdateAllFromOB calls UpdateFromOB on all factors that implement it.
func UpdateAllFromOB(pipeline *Pipeline, ob model.OrderBookData) {
	for _, f := range pipeline.factors {
		if updater, ok := f.(UpdaterFromOB); ok {
			updater.UpdateFromOB(ob)
		}
	}
}
