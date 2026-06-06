package ml

import (
	"encoding/json"
	"fmt"
	"math"
)

// ── Tree Model ─────────────────────────────────────────────────

// TreeNode is a single node in a decision tree.
// Either "leaf" is set (leaf node), or "feature"+"threshold"+"left"+"right" (split node).
type TreeNode struct {
	Leaf      *float64  `json:"leaf,omitempty"`
	Feature   string    `json:"feature,omitempty"`
	Threshold float64   `json:"threshold,omitempty"`
	Left      *TreeNode `json:"left,omitempty"`
	Right     *TreeNode `json:"right,omitempty"`
}

// TreeEnsemble is a collection of decision trees (gradient boosting ensemble).
type TreeEnsemble struct {
	Trees        []TreeNode `json:"trees"`
	FeatureNames []string   `json:"feature_names"`
	TaskType     string     `json:"task_type"` // regression or classification
	BaseScore    float64    `json:"base_score"` // initial prediction (usually mean of y)
}

// ExportedModel is the format exported from Python ML server.
type ExportedModel struct {
	Success       bool            `json:"success"`
	ModelID       string          `json:"model_id"`
	ModelType     string          `json:"model_type"`
	TaskType      string          `json:"task_type"`
	FeatureNames  []string        `json:"feature_names"`
	FeatureConfig map[string]any  `json:"feature_config,omitempty"`
	LabelConfig   map[string]any  `json:"label_config,omitempty"`
	Trees         []TreeNode      `json:"trees"`
}

// Predictor performs inference using loaded tree ensembles.
type Predictor struct {
	ensemble *TreeEnsemble
	loaded   bool
}

// NewPredictor creates a new predictor.
func NewPredictor() *Predictor {
	return &Predictor{}
}

// Load loads an exported model from JSON bytes.
func (p *Predictor) Load(data []byte) error {
	var exported ExportedModel
	if err := json.Unmarshal(data, &exported); err != nil {
		return fmt.Errorf("predictor load: %w", err)
	}

	if len(exported.Trees) == 0 {
		return fmt.Errorf("predictor load: no trees in model")
	}

	p.ensemble = &TreeEnsemble{
		Trees:        exported.Trees,
		FeatureNames: exported.FeatureNames,
		TaskType:     exported.TaskType,
		BaseScore:    0,
	}
	p.loaded = true
	return nil
}

// LoadFromFile loads a model from a JSON file.
func (p *Predictor) LoadFromFile(path string) error {
	return fmt.Errorf("not implemented: use Load with bytes from ML server export")
}

// IsLoaded returns true if a model is loaded.
func (p *Predictor) IsLoaded() bool {
	return p.loaded
}

// Predict makes a single prediction given feature values.
// features must be in the same order as FeatureNames.
func (p *Predictor) Predict(features []float64) (float64, error) {
	if !p.loaded {
		return 0, fmt.Errorf("predictor: no model loaded")
	}

	if len(features) != len(p.ensemble.FeatureNames) {
		return 0, fmt.Errorf("predictor: expected %d features, got %d",
			len(p.ensemble.FeatureNames), len(features))
	}

	// Build feature index map
	featIdx := make(map[string]int, len(p.ensemble.FeatureNames))
	for i, name := range p.ensemble.FeatureNames {
		featIdx[name] = i
	}

	// Sum predictions from all trees (gradient boosting)
	var total float64
	for i := range p.ensemble.Trees {
		total += p.predictTree(&p.ensemble.Trees[i], features, featIdx)
	}

	return total + p.ensemble.BaseScore, nil
}

// PredictFromMap predicts using named features (maps feature name → value).
func (p *Predictor) PredictFromMap(features map[string]float64) (float64, error) {
	vals := make([]float64, len(p.ensemble.FeatureNames))
	for i, name := range p.ensemble.FeatureNames {
		v, ok := features[name]
		if !ok {
			v = 0 // missing feature defaults to 0
		}
		vals[i] = v
	}
	return p.Predict(vals)
}

// predictTree traverses a single tree and returns the leaf value.
func (p *Predictor) predictTree(node *TreeNode, features []float64, featIdx map[string]int) float64 {
	if node.Leaf != nil {
		return *node.Leaf
	}

	// Get feature value
	idx, ok := featIdx[node.Feature]
	if !ok {
		// Unknown feature — go left by default
		if node.Left != nil {
			return p.predictTree(node.Left, features, featIdx)
		}
		return 0
	}

	value := features[idx]
	if value <= node.Threshold {
		if node.Left != nil {
			return p.predictTree(node.Left, features, featIdx)
		}
	} else {
		if node.Right != nil {
			return p.predictTree(node.Right, features, featIdx)
		}
	}

	// Fallback: shouldn't happen with well-formed trees
	return 0
}

// ── Feature Calculator ─────────────────────────────────────────

// FeatureCalculator computes the same features used during training from raw OHLCV bars.
// This mirrors FeatureEngine.transform() in Python.
type FeatureCalculator struct {
	periods []int
}

// NewFeatureCalculator creates a feature calculator with default periods.
func NewFeatureCalculator(periods []int) *FeatureCalculator {
	if len(periods) == 0 {
		periods = []int{5, 10, 20, 50}
	}
	return &FeatureCalculator{periods: periods}
}

// OHLCV is a single price bar.
type OHLCV struct {
	Open, High, Low, Close, Volume float64
}

// Compute calculates features from a window of OHLCV bars.
// Returns a map of feature name → value.
func (fc *FeatureCalculator) Compute(bars []OHLCV) map[string]float64 {
	if len(bars) < 2 {
		return nil
	}

	n := len(bars)
	features := make(map[string]float64)
	closePrices := make([]float64, n)
	volumes := make([]float64, n)

	for i, b := range bars {
		closePrices[i] = b.Close
		volumes[i] = b.Volume
	}

	c := closePrices[n-1] // latest close

	// Returns
	for _, p := range fc.periods {
		if n > p {
			prev := closePrices[n-1-p]
			if prev > 0 {
				features[fmt.Sprintf("return_%d", p)] = (c - prev) / prev
				features[fmt.Sprintf("log_return_%d", p)] = math.Log(c / prev)
			}
		}
	}

	// Price vs MA
	for _, p := range fc.periods {
		ma := fc.sma(closePrices, p)
		if ma > 0 {
			features[fmt.Sprintf("price_vs_ma_%d", p)] = (c - ma) / ma
		}
	}

	// HL range
	features["hl_range"] = (bars[n-1].High - bars[n-1].Low) / c

	// OC gap
	if bars[n-1].Open > 0 {
		features["oc_gap"] = (c - bars[n-1].Open) / bars[n-1].Open
	}

	// Volume ratio
	for _, p := range fc.periods {
		vma := fc.sma(volumes, p)
		if vma > 0 {
			features[fmt.Sprintf("volume_ratio_%d", p)] = volumes[n-1] / vma
		}
	}

	// Volatility (rolling std of returns)
	returns := make([]float64, n-1)
	for i := 1; i < n; i++ {
		if closePrices[i-1] > 0 {
			returns[i-1] = (closePrices[i] - closePrices[i-1]) / closePrices[i-1]
		}
	}
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("volatility_%d", p)] = fc.std(returns[n-p:], p) * math.Sqrt(float64(p))
		}
	}

	// BB position
	for _, p := range fc.periods {
		ma := fc.sma(closePrices, p)
		std := fc.std(closePrices[n-p:], p)
		if std > 0 {
			features[fmt.Sprintf("bb_position_%d", p)] = (c - ma) / std
		}
		features[fmt.Sprintf("bb_width_%d", p)] = (2 * std) / max(ma, 0.01)
	}

	// MACD
	ema12 := fc.ema(closePrices, 12)
	ema26 := fc.ema(closePrices, 26)
	features["macd"] = ema12 - ema26

	// Z-score
	for _, p := range fc.periods {
		ma := fc.sma(closePrices, p)
		std := fc.std(closePrices[n-p:], p)
		if std > 0 {
			features[fmt.Sprintf("zscore_%d", p)] = (c - ma) / std
		}
	}

	// ── Extended Features (FreqAI-style) ─────────────────────────

	// RSI
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("rsi_%d", p)] = fc.rsi(closePrices, p)
		}
	}

	// ATR (Average True Range)
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("atr_%d", p)] = fc.atr(bars, p)
		}
	}

	// Momentum & ROC
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("momentum_%d", p)] = c - closePrices[n-1-p]
			if closePrices[n-1-p] > 0 {
				features[fmt.Sprintf("roc_%d", p)] = (c - closePrices[n-1-p]) / closePrices[n-1-p] * 100
			}
		}
	}

	// Williams %R
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("williams_r_%d", p)] = fc.williamsR(bars, p)
		}
	}

	// Stochastic
	for _, p := range fc.periods {
		if n > p {
			k, d := fc.stochastic(bars, p, 3)
			features[fmt.Sprintf("stoch_k_%d", p)] = k
			features[fmt.Sprintf("stoch_d_%d", p)] = d
		}
	}

	// CCI (Commodity Channel Index)
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("cci_%d", p)] = fc.cci(bars, p)
		}
	}

	// ADX (Average Directional Index)
	for _, p := range fc.periods {
		if n > p+1 {
			features[fmt.Sprintf("adx_%d", p)] = fc.adx(bars, p)
		}
	}

	// OBV (On Balance Volume)
	features["obv"] = fc.obv(bars)

	// CMF (Chaikin Money Flow)
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("cmf_%d", p)] = fc.cmf(bars, p)
		}
	}

	// Price position within HL range
	for _, p := range fc.periods {
		if n > p {
			low, high := fc.minMaxClose(closePrices[n-p:])
			range_ := high - low
			if range_ > 0 {
				features[fmt.Sprintf("price_position_%d", p)] = (c - low) / range_
			}
		}
	}

	// Candlestick features (latest bar)
	latest := bars[n-1]
	body := math.Abs(latest.Close - latest.Open)
	upperShadow := latest.High - math.Max(latest.Close, latest.Open)
	lowerShadow := math.Min(latest.Close, latest.Open) - latest.Low
	if c > 0 {
		features["body_pct"] = body / c
		features["upper_shadow_pct"] = upperShadow / c
		features["lower_shadow_pct"] = lowerShadow / c
	}
	if body > 0 {
		features["shadow_to_body"] = (upperShadow + lowerShadow) / body
	}

	// Linear regression slope & R²
	for _, p := range fc.periods {
		if n > p {
			slope, r2 := fc.linearRegression(closePrices[n-p:])
			features[fmt.Sprintf("linreg_slope_%d", p)] = slope
			features[fmt.Sprintf("linreg_r2_%d", p)] = r2
		}
	}

	// Skewness & Kurtosis of returns
	for _, p := range fc.periods {
		if n > p+1 && len(returns) >= p {
			start := len(returns) - p
			if start < 0 {
				start = 0
			}
			slice := returns[start:]
			features[fmt.Sprintf("skew_%d", p)] = fc.skewness(slice)
			features[fmt.Sprintf("kurt_%d", p)] = fc.kurtosis(slice)
		}
	}

	// Distance from recent high/low
	for _, p := range fc.periods {
		if n > p {
			low, high := fc.minMaxClose(closePrices[n-p:])
			if high > 0 {
				features[fmt.Sprintf("dist_from_high_%d", p)] = (high - c) / high
			}
			if low > 0 {
				features[fmt.Sprintf("dist_from_low_%d", p)] = (c - low) / low
			}
		}
	}

	// VWAP deviation
	for _, p := range fc.periods {
		if n > p {
			vwap := fc.vwap(bars[n-p:])
			if vwap > 0 {
				features[fmt.Sprintf("vwap_dev_%d", p)] = (c - vwap) / vwap
			}
		}
	}

	return features
}

func (fc *FeatureCalculator) sma(data []float64, period int) float64 {
	if period > len(data) {
		period = len(data)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

func (fc *FeatureCalculator) ema(data []float64, period int) float64 {
	if len(data) < period {
		return fc.sma(data, len(data))
	}
	alpha := 2.0 / float64(period+1)
	ema := data[0]
	for i := 1; i < len(data); i++ {
		ema = alpha*data[i] + (1-alpha)*ema
	}
	return ema
}

func (fc *FeatureCalculator) std(data []float64, period int) float64 {
	if period > len(data) {
		period = len(data)
	}
	if period < 2 {
		return 0
	}
	slice := data[len(data)-period:]
	mean := fc.sma(slice, period)
	sumSq := 0.0
	for _, v := range slice {
		sumSq += (v - mean) * (v - mean)
	}
	return math.Sqrt(sumSq / float64(period-1))
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ── Technical Indicator Helpers ────────────────────────────────

func (fc *FeatureCalculator) rsi(prices []float64, period int) float64 {
	if len(prices) < period+1 {
		return 50
	}
	var gain, loss float64
	for i := len(prices) - period; i < len(prices); i++ {
		diff := prices[i] - prices[i-1]
		if diff > 0 {
			gain += diff
		} else {
			loss += -diff
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100.0 - 100.0/(1.0+rs)
}

func (fc *FeatureCalculator) atr(bars []OHLCV, period int) float64 {
	if len(bars) < period+1 {
		return 0
	}
	var sum float64
	for i := len(bars) - period; i < len(bars); i++ {
		tr1 := bars[i].High - bars[i].Low
		tr2 := math.Abs(bars[i].High - bars[i-1].Close)
		tr3 := math.Abs(bars[i].Low - bars[i-1].Close)
		sum += max(tr1, max(tr2, tr3))
	}
	return sum / float64(period)
}

func (fc *FeatureCalculator) williamsR(bars []OHLCV, period int) float64 {
	if len(bars) < period {
		return -50
	}
	high := bars[len(bars)-1].High
	low := bars[len(bars)-1].Low
	for i := len(bars) - period; i < len(bars); i++ {
		if bars[i].High > high {
			high = bars[i].High
		}
		if bars[i].Low < low {
			low = bars[i].Low
		}
	}
	c := bars[len(bars)-1].Close
	range_ := high - low
	if range_ == 0 {
		return -50
	}
	return -100 * (high - c) / range_
}

func (fc *FeatureCalculator) stochastic(bars []OHLCV, kPeriod, dPeriod int) (k, d float64) {
	if len(bars) < kPeriod+dPeriod {
		return 50, 50
	}
	// %K
	c := bars[len(bars)-1].Close
	high := bars[len(bars)-1].High
	low := bars[len(bars)-1].Low
	for i := len(bars) - kPeriod; i < len(bars); i++ {
		if bars[i].High > high {
			high = bars[i].High
		}
		if bars[i].Low < low {
			low = bars[i].Low
		}
	}
	range_ := high - low
	if range_ == 0 {
		k = 50
	} else {
		k = 100 * (c - low) / range_
	}
	// %D = SMA of %K over dPeriod
	var sumK float64
	for i := len(bars) - kPeriod - dPeriod + 1; i <= len(bars)-kPeriod; i++ {
		subHigh := bars[i].High
		subLow := bars[i].Low
		for j := i; j < i+kPeriod && j < len(bars); j++ {
			if bars[j].High > subHigh {
				subHigh = bars[j].High
			}
			if bars[j].Low < subLow {
				subLow = bars[j].Low
			}
		}
		subRange := subHigh - subLow
		if subRange > 0 {
			sumK += 100 * (bars[i+kPeriod-1].Close - subLow) / subRange
		} else {
			sumK += 50
		}
	}
	d = sumK / float64(dPeriod)
	return k, d
}

func (fc *FeatureCalculator) cci(bars []OHLCV, period int) float64 {
	if len(bars) < period {
		return 0
	}
	var sumTP float64
	for i := len(bars) - period; i < len(bars); i++ {
		tp := (bars[i].High + bars[i].Low + bars[i].Close) / 3.0
		sumTP += tp
	}
	meanTP := sumTP / float64(period)
	var sumDev float64
	for i := len(bars) - period; i < len(bars); i++ {
		tp := (bars[i].High + bars[i].Low + bars[i].Close) / 3.0
		sumDev += math.Abs(tp - meanTP)
	}
	meanDev := sumDev / float64(period)
	if meanDev == 0 {
		return 0
	}
	latestTP := (bars[len(bars)-1].High + bars[len(bars)-1].Low + bars[len(bars)-1].Close) / 3.0
	return (latestTP - meanTP) / (0.015 * meanDev)
}

func (fc *FeatureCalculator) adx(bars []OHLCV, period int) float64 {
	if len(bars) < period+2 {
		return 25
	}
	var sumDX float64
	for i := len(bars) - period; i < len(bars); i++ {
		upMove := bars[i].High - bars[i-1].High
		downMove := bars[i-1].Low - bars[i].Low
		var plusDM, minusDM float64
		if upMove > downMove && upMove > 0 {
			plusDM = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM = downMove
		}
		tr1 := bars[i].High - bars[i].Low
		tr2 := math.Abs(bars[i].High - bars[i-1].Close)
		tr3 := math.Abs(bars[i].Low - bars[i-1].Close)
		tr := max(tr1, max(tr2, tr3))
		if tr > 0 {
			plusDI := 100 * plusDM / tr
			minusDI := 100 * minusDM / tr
			dx := math.Abs(plusDI-minusDI) / (plusDI + minusDI + 1e-10) * 100
			sumDX += dx
		}
	}
	return sumDX / float64(period)
}

func (fc *FeatureCalculator) obv(bars []OHLCV) float64 {
	if len(bars) < 2 {
		return 0
	}
	obv := 0.0
	for i := 1; i < len(bars); i++ {
		if bars[i].Close > bars[i-1].Close {
			obv += bars[i].Volume
		} else if bars[i].Close < bars[i-1].Close {
			obv -= bars[i].Volume
		}
	}
	return obv
}

func (fc *FeatureCalculator) cmf(bars []OHLCV, period int) float64 {
	if len(bars) < period {
		return 0
	}
	var sumMFV, sumVol float64
	for i := len(bars) - period; i < len(bars); i++ {
		range_ := bars[i].High - bars[i].Low
		if range_ > 0 {
			mfm := ((bars[i].Close - bars[i].Low) - (bars[i].High - bars[i].Close)) / range_
			mfv := mfm * bars[i].Volume
			sumMFV += mfv
			sumVol += bars[i].Volume
		}
	}
	if sumVol == 0 {
		return 0
	}
	return sumMFV / sumVol
}

func (fc *FeatureCalculator) minMaxClose(prices []float64) (min, max float64) {
	if len(prices) == 0 {
		return 0, 0
	}
	min = prices[0]
	max = prices[0]
	for _, p := range prices[1:] {
		if p < min {
			min = p
		}
		if p > max {
			max = p
		}
	}
	return min, max
}

func (fc *FeatureCalculator) linearRegression(prices []float64) (slope, r2 float64) {
	n := float64(len(prices))
	if n < 2 {
		return 0, 0
	}
	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i, y := range prices {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
		sumY2 += y * y
	}
	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0, 0
	}
	slope = (n*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / n
	// R²
	var ssTot, ssRes float64
	meanY := sumY / n
	for i, y := range prices {
		x := float64(i)
		pred := slope*x + intercept
		ssRes += (y - pred) * (y - pred)
		ssTot += (y - meanY) * (y - meanY)
	}
	if ssTot > 0 {
		r2 = 1 - ssRes/ssTot
	}
	return slope, r2
}

func (fc *FeatureCalculator) skewness(data []float64) float64 {
	n := float64(len(data))
	if n < 3 {
		return 0
	}
	m := fc.sma(data, len(data))
	var sum3, sum2 float64
	for _, v := range data {
		d := v - m
		sum2 += d * d
		sum3 += d * d * d
	}
	std := math.Sqrt(sum2 / n)
	if std == 0 {
		return 0
	}
	return (sum3 / n) / (std * std * std)
}

func (fc *FeatureCalculator) kurtosis(data []float64) float64 {
	n := float64(len(data))
	if n < 4 {
		return 3
	}
	m := fc.sma(data, len(data))
	var sum2, sum4 float64
	for _, v := range data {
		d := v - m
		sum2 += d * d
		sum4 += d * d * d * d
	}
	std2 := sum2 / n
	if std2 == 0 {
		return 3
	}
	return (sum4 / n) / (std2 * std2)
}

func (fc *FeatureCalculator) vwap(bars []OHLCV) float64 {
	if len(bars) == 0 {
		return 0
	}
	var sumPV, sumV float64
	for _, b := range bars {
		tp := (b.High + b.Low + b.Close) / 3.0
		sumPV += tp * b.Volume
		sumV += b.Volume
	}
	if sumV == 0 {
		return 0
	}
	return sumPV / sumV
}
