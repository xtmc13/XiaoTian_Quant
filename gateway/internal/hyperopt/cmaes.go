package hyperopt

import (
	"math"
	"math/rand"
	"sort"

	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── CMA-ES Sampler (sep-CMA-ES simplified) ─────────────────────
//
// Covariance Matrix Adaptation Evolution Strategy for continuous
// hyperparameter optimization.  This implementation uses a diagonal
// covariance (sep-CMA-ES) which is fast, stable, and works well for
// the moderate dimensionality (< 30) typical in trading strategies.
//
// Reference: Hansen (2016) — The CMA Evolution Strategy: A Tutorial.

// cmaesDim tracks one continuous dimension from the search space.
type cmaesDim struct {
	name string
	min  float64
	max  float64
}

// cmaesEval holds a single evaluated candidate for one generation.
type cmaesEval struct {
	normed []float64       // coordinates in [0,1] space
	params map[string]any   // full parameter set (incl. non-continuous)
	loss   float64
}

// CMAESSampler implements Bayesian-like optimisation via evolution.
type CMAESSampler struct {
	// Extracted continuous dimensions
	dims []cmaesDim

	// CMA-ES state (all in normalised [0,1] space)
	mean    []float64 // current mean per dimension
	sigma   float64   // global step-size
	covDiag []float64 // diagonal of covariance matrix

	// Population settings
	lambda  int       // offspring per generation
	mu      int       // parents (elite count)
	weights []float64 // recombination weights

	// Runtime state
	queue      []map[string]any // pre-generated candidates for current gen
	historyBuf []cmaesEval      // evaluated candidates of current gen
	gen        int              // generation counter
	total      int              // total candidates emitted
	bestLoss   float64          // best loss seen in previous gen

	// Non-continuous parameter names → fixed random values
	fixedParams map[string]any
}

// NewCMAESSampler creates a CMA-ES sampler.
func NewCMAESSampler() *CMAESSampler {
	return &CMAESSampler{
		sigma:       0.3,
		bestLoss:    math.Inf(1),
		fixedParams: make(map[string]any),
	}
}

func (c *CMAESSampler) Name() string { return "cmaes" }

// Next suggests the next parameter set.  It lazily initialises on the
// first call and advances generations automatically.
func (c *CMAESSampler) Next(space *SearchSpace, history []Trial, rng *rand.Rand) map[string]any {
	// First call: extract dimensions and initialise state
	if c.dims == nil {
		c.init(space, rng)
	}

	// If we have queued candidates, return the next one
	if len(c.queue) > 0 {
		p := c.queue[0]
		c.queue = c.queue[1:]
		c.total++
		return p
	}

	// Queue empty → finish current generation, update state, generate next
	c.update(history, rng)
	c.generateGeneration(rng)

	if len(c.queue) == 0 {
		return nil // should not happen
	}
	p := c.queue[0]
	c.queue = c.queue[1:]
	c.total++
	return p
}

// ── Initialisation ───────────────────────────────────────────────

func (c *CMAESSampler) init(space *SearchSpace, rng *rand.Rand) {
	// Extract continuous (Float) dimensions
	for _, s := range space.spaces {
		switch s.Type {
		case strategy.ParamFloat:
			c.dims = append(c.dims, cmaesDim{
				name: s.Name,
				min:  s.Min,
				max:  s.Max,
			})
		default:
			// For Int / Bool / Categorical we keep a fixed random value
			c.fixedParams[s.Name] = s.Sample(rng)
		}
	}

	n := len(c.dims)
	if n == 0 {
		// No continuous dims — CMA-ES degenerates to random search
		c.lambda = 1
		c.mu = 1
		return
	}

	// Population size: λ = 4 + ⌊3·ln(n)⌋  (standard CMA-ES heuristic)
	c.lambda = 4 + int(3*math.Log(float64(n)+1))
	if c.lambda < 4 {
		c.lambda = 4
	}
	c.mu = c.lambda / 2

	// Recombination weights (log-scale, then normalised)
	c.weights = make([]float64, c.mu)
	var sumW float64
	for i := 0; i < c.mu; i++ {
		w := math.Log(float64(c.mu)+0.5) - math.Log(float64(i)+1.0)
		if w < 0 {
			w = 0
		}
		c.weights[i] = w
		sumW += w
	}
	for i := range c.weights {
		c.weights[i] /= sumW
	}

	// Initial state
	c.mean = make([]float64, n)
	c.covDiag = make([]float64, n)
	for i := 0; i < n; i++ {
		c.mean[i] = 0.5
		c.covDiag[i] = 1.0
	}
	c.sigma = 0.3
}

// ── Generation update ────────────────────────────────────────────

func (c *CMAESSampler) update(history []Trial, rng *rand.Rand) {
	if len(c.dims) == 0 {
		return // nothing to update
	}

	// Collect evaluated candidates belonging to the generation just finished.
	// We look at the most recent trials in history that match our pending count.
	need := c.lambda
	if len(c.historyBuf) > 0 {
		need = len(c.historyBuf)
	}
	start := len(history) - need
	if start < 0 {
		start = 0
	}

	var evals []cmaesEval
	for i := start; i < len(history); i++ {
		t := history[i]
		// Reconstruct normalised coordinates
		 normed := make([]float64, len(c.dims))
		for j, d := range c.dims {
			if v, ok := t.Params[d.name]; ok {
				x := toFloat(v)
				normed[j] = (x - d.min) / (d.max - d.min)
				if normed[j] < 0 {
					normed[j] = 0
				}
				if normed[j] > 1 {
					normed[j] = 1
				}
			}
		}
		evals = append(evals, cmaesEval{
			normed: normed,
			params: t.Params,
			loss:   t.Loss,
		})
	}

	if len(evals) == 0 {
		return // first generation has no prior evals
	}

	// Sort by loss ascending (lower = better)
	sort.Slice(evals, func(i, j int) bool { return evals[i].loss < evals[j].loss })

	// Track best loss for step-size heuristic
	currentBest := evals[0].loss
	improved := currentBest < c.bestLoss
	c.bestLoss = currentBest

	// ---- Update mean (weighted recombination) ----
	muEff := minInt(c.mu, len(evals))
	newMean := make([]float64, len(c.dims))
	for i := 0; i < muEff; i++ {
		w := c.weights[i]
		for j := range newMean {
			newMean[j] += w * evals[i].normed[j]
		}
	}

	// ---- Update diagonal covariance (sep-CMA-ES) ----
	newCov := make([]float64, len(c.dims))
	for j := range newCov {
		var variance float64
		for i := 0; i < muEff; i++ {
			d := evals[i].normed[j] - newMean[j]
			variance += c.weights[i] * d * d
		}
		// Regularisation: blend with old covariance to avoid collapse
		newCov[j] = 0.3*c.covDiag[j] + 0.7*variance
		if newCov[j] < 1e-8 {
			newCov[j] = 1e-8
		}
		if newCov[j] > 1.0 {
			newCov[j] = 1.0
		}
	}

	// ---- Step-size adaptation (simplified CSA) ----
	if improved {
		c.sigma *= 1.1
	} else {
		c.sigma *= 0.85
	}
	if c.sigma < 0.001 {
		c.sigma = 0.001
	}
	if c.sigma > 0.5 {
		c.sigma = 0.5
	}

	c.mean = newMean
	c.covDiag = newCov
	c.gen++
	c.historyBuf = c.historyBuf[:0] // clear buffer
}

// ── Candidate generation ───────────────────────────────────────

func (c *CMAESSampler) generateGeneration(rng *rand.Rand) {
	if len(c.dims) == 0 {
		// No continuous dims — just randomise fixed params
		c.queue = append(c.queue, c.fixedParams)
		return
	}

	for k := 0; k < c.lambda; k++ {
		params := make(map[string]any, len(c.dims)+len(c.fixedParams))

		// Copy fixed (non-continuous) params
		for k, v := range c.fixedParams {
			params[k] = v
		}

		// Sample continuous dims from N(mean, sigma^2 * covDiag)
		for j, d := range c.dims {
			std := c.sigma * math.Sqrt(c.covDiag[j])
			x := c.mean[j] + std*rng.NormFloat64()

			// Mirror boundary handling (reflect back into [0,1])
			for x < 0 || x > 1 {
				if x < 0 {
					x = -x
				}
				if x > 1 {
					x = 2 - x
				}
			}
			if x < 0 {
				x = 0
			}
			if x > 1 {
				x = 1
			}

			// Map back to original range
			val := d.min + x*(d.max-d.min)
			params[d.name] = val
		}

		c.queue = append(c.queue, params)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
