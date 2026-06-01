package factor

import (
	"math"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// Factor is the interface for all technical factors.
type Factor interface {
	Name() string
	Update(bar model.Bar)
	Current() float64
	History(n int) []float64
	Series() []float64
	Stats() FactorStats
	Reset()
}

// FactorStats holds summary statistics for a factor.
type FactorStats struct {
	Mean   float64 `json:"mean"`
	Std    float64 `json:"std"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Latest float64 `json:"latest"`
}

func computeStats(values []float64) FactorStats {
	if len(values) == 0 {
		return FactorStats{}
	}
	stats := FactorStats{
		Min:    values[0],
		Max:    values[0],
		Latest: values[len(values)-1],
	}
	sum := 0.0
	for _, v := range values {
		sum += v
		if v < stats.Min {
			stats.Min = v
		}
		if v > stats.Max {
			stats.Max = v
		}
	}
	stats.Mean = sum / float64(len(values))

	variance := 0.0
	for _, v := range values {
		variance += (v - stats.Mean) * (v - stats.Mean)
	}
	stats.Std = math.Sqrt(variance / float64(len(values)))
	return stats
}

// ── Factor Pipeline ──

// Pipeline combines multiple factors into a feature vector.
type Pipeline struct {
	factors []Factor
	names   []string
}

func NewPipeline(factors ...Factor) *Pipeline {
	p := &Pipeline{}
	for _, f := range factors {
		p.Add(f)
	}
	return p
}

func (p *Pipeline) Add(f Factor) {
	p.factors = append(p.factors, f)
	p.names = append(p.names, f.Name())
}

func (p *Pipeline) Update(bar model.Bar) {
	for _, f := range p.factors {
		f.Update(bar)
	}
}

// GetFeatureVector returns the current value of each factor as a float64 slice.
func (p *Pipeline) GetFeatureVector() []float64 {
	vec := make([]float64, len(p.factors))
	for i, f := range p.factors {
		vec[i] = f.Current()
	}
	return vec
}

// GetFeatureVectorAt returns values at a specific history index.
func (p *Pipeline) GetFeatureVectorAt(index int) []float64 {
	vec := make([]float64, len(p.factors))
	for i, f := range p.factors {
		hist := f.History(index + 1)
		if len(hist) > 0 {
			vec[i] = hist[len(hist)-1]
		}
	}
	return vec
}

// FeatureNames returns the names of all factors in the pipeline.
func (p *Pipeline) FeatureNames() []string {
	return p.names
}

// Correlations computes the correlation matrix between all factors.
func (p *Pipeline) Correlations() [][]float64 {
	n := len(p.factors)
	if n == 0 {
		return nil
	}
	mat := make([][]float64, n)
	for i := 0; i < n; i++ {
		mat[i] = make([]float64, n)
	}
	series := make([][]float64, n)
	for i, f := range p.factors {
		series[i] = f.Series()
	}
	for i := 0; i < n; i++ {
		mat[i][i] = 1.0
		for j := i + 1; j < n; j++ {
			c := correlation(series[i], series[j])
			mat[i][j] = c
			mat[j][i] = c
		}
	}
	return mat
}

func correlation(x, y []float64) float64 {
	n := len(x)
	if n < 2 || len(y) < 2 {
		return 0
	}
	if len(x) != len(y) {
		minLen := len(x)
		if len(y) < minLen {
			minLen = len(y)
		}
		x = x[len(x)-minLen:]
		y = y[len(y)-minLen:]
		n = minLen
	}
	sumX, sumY := 0.0, 0.0
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
	}
	meanX, meanY := sumX/float64(n), sumY/float64(n)
	cov, varX, varY := 0.0, 0.0, 0.0
	for i := 0; i < n; i++ {
		dx, dy := x[i]-meanX, y[i]-meanY
		cov += dx * dy
		varX += dx * dx
		varY += dy * dy
	}
	if varX == 0 || varY == 0 {
		return 0
	}
	return cov / math.Sqrt(varX*varY)
}

// ── Base Factor ──

// BaseFactor provides common factor infrastructure.
type BaseFactor struct {
	name   string
	values []float64
}

func NewBaseFactor(name string) BaseFactor {
	return BaseFactor{name: name}
}

func (b *BaseFactor) Name() string      { return b.name }
func (b *BaseFactor) Reset()            { b.values = nil }

func (b *BaseFactor) Current() float64 {
	if len(b.values) == 0 {
		return 0
	}
	return b.values[len(b.values)-1]
}

func (b *BaseFactor) History(n int) []float64 {
	if n <= 0 || n > len(b.values) {
		n = len(b.values)
	}
	start := len(b.values) - n
	result := make([]float64, n)
	copy(result, b.values[start:])
	return result
}

func (b *BaseFactor) Series() []float64 {
	result := make([]float64, len(b.values))
	copy(result, b.values)
	return result
}

func (b *BaseFactor) Stats() FactorStats {
	return computeStats(b.values)
}

func (b *BaseFactor) push(v float64) {
	b.values = append(b.values, v)
	if len(b.values) > 500 {
		b.values = b.values[len(b.values)-500:]
	}
}
