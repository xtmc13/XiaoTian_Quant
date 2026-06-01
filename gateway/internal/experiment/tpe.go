package experiment

import (
	"math"
	"sort"
	"sync"
)

// ── TPE (Tree-structured Parzen Estimator) ────────────────────────

// TPEConfig configures the TPE Bayesian optimizer.
type TPEConfig struct {
	NStartupTrials  int     `json:"n_startup_trials"`  // random sampling before TPE starts
	N_EI_Candidates int     `json:"n_ei_candidates"`   // number of candidates to sample for EI
	Gamma           float64 `json:"gamma"`             // top fraction for "good" group (e.g., 0.2)
	ParallelEvals   int     `json:"parallel_evals"`
	MaxTrials       int     `json:"max_trials"`
}

func DefaultTPEConfig() TPEConfig {
	return TPEConfig{
		NStartupTrials:  5,
		N_EI_Candidates: 100,
		Gamma:           0.2,
		ParallelEvals:   4,
		MaxTrials:       50,
	}
}

// Observation stores a parameter vector and its observed score.
type Observation struct {
	Vector []float64 `json:"vector"`
	Score  float64   `json:"score"`
	Params map[string]any `json:"params"`
}

// TPE runs Bayesian optimization using TPE.
func TPE(
	space ParamSpace,
	cfg TPEConfig,
	fitnessFn func(map[string]any) float64,
) ([]Observation, *Observation) {
	if cfg.NStartupTrials < 2 {
		cfg.NStartupTrials = 2
	}
	if cfg.Gamma <= 0 || cfg.Gamma >= 1 {
		cfg.Gamma = 0.2
	}
	if cfg.N_EI_Candidates < 10 {
		cfg.N_EI_Candidates = 10
	}
	if cfg.ParallelEvals <= 0 {
		cfg.ParallelEvals = 1
	}

	dim := space.Dimension()
	if dim == 0 {
		return nil, nil
	}

	var observations []Observation

	// Startup: random sampling
	for t := 0; t < cfg.NStartupTrials; t++ {
		vec := space.RandomVector()
		params := space.ToMap(vec)
		score := fitnessFn(params)
		observations = append(observations, Observation{Vector: vec, Score: score, Params: params})
	}

	best := bestObservation(observations)

	// TPE main loop
	for t := cfg.NStartupTrials; t < cfg.MaxTrials; t++ {
		// Split observations into good (top gamma) and bad (rest)
		good, bad := splitObservations(observations, cfg.Gamma)

		// Sample candidates and pick the one with best expected improvement
		candidates := make([][]float64, cfg.N_EI_Candidates)
		for i := range candidates {
			candidates[i] = space.RandomVector()
		}

		bestCandidate := candidates[0]
		bestEI := math.Inf(-1)

		for _, cand := range candidates {
			ei := expectedImprovement(cand, good, bad, space)
			if ei > bestEI {
				bestEI = ei
				bestCandidate = cand
			}
		}

		bestCandidate = space.Clip(bestCandidate)
		params := space.ToMap(bestCandidate)
		score := fitnessFn(params)
		observations = append(observations, Observation{Vector: bestCandidate, Score: score, Params: params})

		if b := bestObservation(observations); b != nil && (best == nil || b.Score > best.Score) {
			best = b
		}
	}

	return observations, best
}

// splitObservations splits observations into top gamma (good) and rest (bad).
func splitObservations(obs []Observation, gamma float64) (good, bad []Observation) {
	if len(obs) == 0 {
		return nil, nil
	}
	sorted := make([]Observation, len(obs))
	copy(sorted, obs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score // higher score = better
	})

	nGood := int(math.Max(1, math.Ceil(float64(len(sorted))*gamma)))
	if nGood >= len(sorted) {
		nGood = len(sorted) - 1
	}
	return sorted[:nGood], sorted[nGood:]
}

// expectedImprovement computes log(good_pdf / bad_pdf) for a candidate.
// Higher is better.
func expectedImprovement(candidate []float64, good, bad []Observation, space ParamSpace) float64 {
	if len(good) == 0 || len(bad) == 0 {
		return 0
	}

	logRatio := 0.0
	for d, bound := range space {
		if d >= len(candidate) {
			break
		}
		v := candidate[d]

		goodVals := make([]float64, len(good))
		for i, o := range good {
			if d < len(o.Vector) {
				goodVals[i] = o.Vector[d]
			}
		}
		badVals := make([]float64, len(bad))
		for i, o := range bad {
			if d < len(o.Vector) {
				badVals[i] = o.Vector[d]
			}
		}

		gPDF := kdePDF(v, goodVals, bound)
		bPDF := kdePDF(v, badVals, bound)

		if gPDF > 0 && bPDF > 0 {
			logRatio += math.Log(gPDF / bPDF)
		} else if gPDF > 0 {
			logRatio += 10 // strong preference
		} else if bPDF > 0 {
			logRatio -= 10 // strong avoidance
		}
	}

	return logRatio
}

// kdePDF computes kernel density estimate at point x using Gaussian kernel.
func kdePDF(x float64, samples []float64, bound ParamBound) float64 {
	if len(samples) == 0 {
		return 1e-10
	}

	// Bandwidth: Scott's rule approximation
	std := sampleStd(samples)
	h := std * math.Pow(float64(len(samples)), -0.2)
	if h <= 0 {
		h = (bound.Max - bound.Min) / 20
		if h <= 0 {
			h = 1.0
		}
	}

	sum := 0.0
	for _, s := range samples {
		z := (x - s) / h
		sum += math.Exp(-0.5*z*z) / (math.Sqrt(2*math.Pi) * h)
	}
	return sum / float64(len(samples))
}

func sampleStd(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))

	variance := 0.0
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	return math.Sqrt(variance / float64(len(vals)-1))
}

func bestObservation(obs []Observation) *Observation {
	var best *Observation
	for i := range obs {
		if best == nil || obs[i].Score > best.Score {
			cp := obs[i]
			best = &cp
		}
	}
	return best
}

// evaluatePopulationObs evaluates observations concurrently.
func evaluatePopulationObs(
	obs []Observation,
	space ParamSpace,
	fitnessFn func(map[string]any) float64,
	parallel int,
) {
	if parallel <= 1 {
		for i := range obs {
			if obs[i].Params == nil {
				obs[i].Params = space.ToMap(obs[i].Vector)
			}
			obs[i].Score = fitnessFn(obs[i].Params)
		}
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)
	for i := range obs {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			if obs[idx].Params == nil {
				obs[idx].Params = space.ToMap(obs[idx].Vector)
			}
			obs[idx].Score = fitnessFn(obs[idx].Params)
		}(i)
	}
	wg.Wait()
}
