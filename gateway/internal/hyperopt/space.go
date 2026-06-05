package hyperopt

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── Search Space ───────────────────────────────────────────────

// Space defines a single dimension in the hyperopt search space.
type Space struct {
	Name    string
	Type    strategy.ParamType
	Min     float64
	Max     float64
	Step    float64
	Options []string
}

// Sample draws a random value from this space.
func (s *Space) Sample(rng *rand.Rand) any {
	switch s.Type {
	case strategy.ParamInt:
		steps := int((s.Max - s.Min) / s.Step)
		if steps <= 0 {
			steps = 1
		}
		return int(s.Min) + rng.Intn(steps+1)*int(s.Step)
	case strategy.ParamFloat:
		return s.Min + rng.Float64()*(s.Max-s.Min)
	case strategy.ParamBool:
		return rng.Intn(2) == 1
	case strategy.ParamCategorical:
		if len(s.Options) > 0 {
			return s.Options[rng.Intn(len(s.Options))]
		}
		return ""
	}
	return nil
}

// Quantize rounds a sampled value to valid steps/options.
func (s *Space) Quantize(v any) any {
	switch s.Type {
	case strategy.ParamInt:
		val := toInt(v)
		steps := int((s.Max - s.Min) / s.Step)
		if steps <= 0 {
			return int(s.Min)
		}
		stepIdx := int(math.Round(float64(val-int(s.Min)) / s.Step))
		if stepIdx < 0 {
			stepIdx = 0
		}
		if stepIdx > steps {
			stepIdx = steps
		}
		return int(s.Min) + stepIdx*int(s.Step)
	case strategy.ParamFloat:
		val := toFloat(v)
		steps := int((s.Max - s.Min) / s.Step)
		if steps <= 0 {
			return s.Min
		}
		stepIdx := int(math.Round((val - s.Min) / s.Step))
		if stepIdx < 0 {
			stepIdx = 0
		}
		if stepIdx > steps {
			stepIdx = steps
		}
		return s.Min + float64(stepIdx)*s.Step
	case strategy.ParamBool:
		if b, ok := v.(bool); ok {
			return b
		}
		return v.(int) == 1
	case strategy.ParamCategorical:
		if str, ok := v.(string); ok {
			for _, opt := range s.Options {
				if opt == str {
					return str
				}
			}
			if len(s.Options) > 0 {
				return s.Options[0]
			}
		}
		return v
	}
	return v
}

// SearchSpace is a collection of parameter spaces.
type SearchSpace struct {
	spaces []Space
	index  map[string]int
}

// NewSearchSpaceFromRegistry builds a search space from a ParamRegistry.
func NewSearchSpaceFromRegistry(reg *strategy.ParamRegistry) *SearchSpace {
	ss := &SearchSpace{
		index: make(map[string]int),
	}
	for _, p := range reg.Optimizable() {
		if p.Min == 0 && p.Max == 0 && len(p.Options) == 0 {
			continue
		}
		idx := len(ss.spaces)
		ss.spaces = append(ss.spaces, Space{
			Name:    p.Name,
			Type:    p.Type,
			Min:     p.Min,
			Max:     p.Max,
			Step:    p.Step,
			Options: p.Options,
		})
		ss.index[p.Name] = idx
	}
	return ss
}

// Sample draws a random point from the entire search space.
func (ss *SearchSpace) Sample(rng *rand.Rand) map[string]any {
	point := make(map[string]any, len(ss.spaces))
	for _, s := range ss.spaces {
		point[s.Name] = s.Sample(rng)
	}
	return point
}

// Quantize rounds all values in a point to valid steps.
func (ss *SearchSpace) Quantize(point map[string]any) map[string]any {
	result := make(map[string]any, len(point))
	for name, v := range point {
		if idx, ok := ss.index[name]; ok {
			result[name] = ss.spaces[idx].Quantize(v)
		} else {
			result[name] = v
		}
	}
	return result
}

// Dimensions returns the number of searchable dimensions.
func (ss *SearchSpace) Dimensions() int {
	return len(ss.spaces)
}

// Names returns all parameter names in the space.
func (ss *SearchSpace) Names() []string {
	names := make([]string, len(ss.spaces))
	for i, s := range ss.spaces {
		names[i] = s.Name
	}
	return names
}

// ── Trial ──────────────────────────────────────────────────────

// Trial represents a single hyperopt evaluation.
type Trial struct {
	ID        int               `json:"id"`
	Params    map[string]any    `json:"params"`
	Loss      float64           `json:"loss"`       // objective value (lower is better)
	Metrics   map[string]float64 `json:"metrics"`   // e.g., sharpe, win_rate, max_dd
	Duration  time.Duration     `json:"duration"`
	Timestamp int64             `json:"timestamp"`
	IsBest    bool              `json:"is_best"`
}

// ── Objective ──────────────────────────────────────────────────

// ObjectiveFunc evaluates a parameter set and returns a loss (lower = better).
type ObjectiveFunc func(params map[string]any) (loss float64, metrics map[string]float64, err error)

// ── Sampler interface ─────────────────────────────────────────

// Sampler generates parameter candidates for hyperopt.
type Sampler interface {
	// Next suggests the next parameter set to evaluate.
	Next(space *SearchSpace, history []Trial, rng *rand.Rand) map[string]any
	// Name returns the sampler name.
	Name() string
}

// ── Random Sampler ─────────────────────────────────────────────

// RandomSampler performs uniform random search.
type RandomSampler struct{}

func (r *RandomSampler) Name() string { return "random" }

func (r *RandomSampler) Next(space *SearchSpace, history []Trial, rng *rand.Rand) map[string]any {
	return space.Sample(rng)
}

// ── Grid Sampler ───────────────────────────────────────────────

// GridSampler performs exhaustive grid search.
type GridSampler struct {
	gridPoints map[string][]any
	indices    map[string]int
	total      int
	current    int
	mu         sync.Mutex
}

// NewGridSampler creates a grid sampler with specified points per dimension.
func NewGridSampler(space *SearchSpace, pointsPerDim int) *GridSampler {
	if pointsPerDim < 2 {
		pointsPerDim = 5
	}
	gs := &GridSampler{
		gridPoints: make(map[string][]any),
		indices:    make(map[string]int),
	}

	for _, s := range space.spaces {
		pts := make([]any, 0, pointsPerDim)
		switch s.Type {
		case strategy.ParamInt:
			steps := int((s.Max - s.Min) / s.Step)
			if steps < pointsPerDim {
				pointsPerDim = steps + 1
			}
			for i := 0; i < pointsPerDim; i++ {
				val := int(s.Min) + i*steps/(pointsPerDim-1)*int(s.Step)
				pts = append(pts, val)
			}
		case strategy.ParamFloat:
			for i := 0; i < pointsPerDim; i++ {
				val := s.Min + float64(i)*(s.Max-s.Min)/float64(pointsPerDim-1)
				pts = append(pts, s.Quantize(val))
			}
		case strategy.ParamBool:
			pts = append(pts, false, true)
		case strategy.ParamCategorical:
			for _, opt := range s.Options {
				pts = append(pts, opt)
			}
		}
		gs.gridPoints[s.Name] = pts
		gs.indices[s.Name] = 0
	}

	// Calculate total combinations
	gs.total = 1
	for _, pts := range gs.gridPoints {
		gs.total *= len(pts)
	}
	return gs
}

func (g *GridSampler) Name() string { return "grid" }

func (g *GridSampler) Next(space *SearchSpace, history []Trial, rng *rand.Rand) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.current >= g.total {
		return nil // exhausted
	}

	point := make(map[string]any)
	remaining := g.current
	for _, s := range space.spaces {
		pts := g.gridPoints[s.Name]
		idx := remaining % len(pts)
		point[s.Name] = pts[idx]
		remaining /= len(pts)
	}
	g.current++
	return point
}

// Progress returns current progress as fraction.
func (g *GridSampler) Progress() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.total == 0 {
		return 1.0
	}
	return float64(g.current) / float64(g.total)
}

// ── TPE Sampler (Tree-structured Parzen Estimator) ───────────

// TPESampler implements Bayesian optimization with TPE.
type TPESampler struct {
	gamma    float64 // quantile for splitting good/bad (default 0.15)
	nStartup int     // number of random samples before TPE kicks in
}

// NewTPESampler creates a TPE sampler.
func NewTPESampler() *TPESampler {
	return &TPESampler{
		gamma:    0.15,
		nStartup: 10,
	}
}

func (t *TPESampler) Name() string { return "tpe" }

func (t *TPESampler) Next(space *SearchSpace, history []Trial, rng *rand.Rand) map[string]any {
	if len(history) < t.nStartup {
		return space.Sample(rng)
	}

	// Split history into good (low loss) and bad (high loss)
	sorted := make([]Trial, len(history))
	copy(sorted, history)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Loss < sorted[j].Loss })

	nGood := max(1, int(float64(len(sorted))*t.gamma))
	good := sorted[:nGood]
	bad := sorted[nGood:]

	// Sample each dimension using TPE
	point := make(map[string]any)
	for _, s := range space.spaces {
		point[s.Name] = t.sampleTPE(s, good, bad, rng)
	}
	return point
}

func (t *TPESampler) sampleTPE(space Space, good, bad []Trial, rng *rand.Rand) any {
	// Extract values for this dimension
	goodVals := make([]float64, 0, len(good))
	badVals := make([]float64, 0, len(bad))
	for _, trial := range good {
		if v, ok := trial.Params[space.Name]; ok {
			goodVals = append(goodVals, toFloat(v))
		}
	}
	for _, trial := range bad {
		if v, ok := trial.Params[space.Name]; ok {
			badVals = append(badVals, toFloat(v))
		}
	}

	if len(goodVals) == 0 || len(badVals) == 0 {
		return space.Sample(rng)
	}

	// Use kernel density estimation (simplified: sample from good with some exploration)
	// For simplicity, sample from good distribution with 20% exploration
	if rng.Float64() < 0.8 {
		// Sample from good values with small Gaussian noise
		base := goodVals[rng.Intn(len(goodVals))]
		noise := (space.Max - space.Min) * 0.05 * rng.NormFloat64()
		val := base + noise
		if val < space.Min {
			val = space.Min
		}
		if val > space.Max {
			val = space.Max
		}
		return space.Quantize(val)
	}
	// 20% exploration: random sample
	return space.Sample(rng)
}

// ── Helpers ────────────────────────────────────────────────────

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case bool:
		if val {
			return 1
		}
		return 0
	}
	return 0
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case bool:
		if val {
			return 1.0
		}
		return 0.0
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
