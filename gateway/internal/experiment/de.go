package experiment

import (
	"math/rand"
	"sync"
)

// ── Differential Evolution (DE) ───────────────────────────────────

// DEConfig configures the differential evolution optimizer.
type DEConfig struct {
	PopulationSize int     `json:"population_size"`
	MaxGenerations int     `json:"max_generations"`
	F              float64 `json:"f"` // differential weight [0.5, 1.0]
	CR             float64 `json:"cr"` // crossover probability [0.7, 0.9]
	ParallelEvals  int     `json:"parallel_evals"`
}

func DefaultDEConfig() DEConfig {
	return DEConfig{
		PopulationSize: 15,
		MaxGenerations: 30,
		F:              0.8,
		CR:             0.9,
		ParallelEvals:  4,
	}
}

// Individual represents a candidate solution.
type Individual struct {
	Vector []float64 `json:"vector"`
	Score  float64   `json:"score"`
	Valid  bool      `json:"valid"`
	Params map[string]any `json:"params"`
}

// DifferentialEvolution runs DE optimization over a parameter space.
// fitnessFn receives a parameter map and returns a score (higher = better).
func DifferentialEvolution(
	space ParamSpace,
	cfg DEConfig,
	fitnessFn func(map[string]any) float64,
) ([]Individual, *Individual) {
	if cfg.PopulationSize < 4 {
		cfg.PopulationSize = 4
	}
	if cfg.F <= 0 {
		cfg.F = 0.8
	}
	if cfg.CR <= 0 {
		cfg.CR = 0.9
	}
	if cfg.ParallelEvals <= 0 {
		cfg.ParallelEvals = 1
	}

	dim := space.Dimension()
	if dim == 0 {
		return nil, nil
	}

	// Initialize population
	pop := make([]Individual, cfg.PopulationSize)
	for i := range pop {
		pop[i] = Individual{Vector: space.RandomVector()}
	}

	// Evaluate initial population
	evaluatePopulation(pop, space, fitnessFn, cfg.ParallelEvals)

	best := cloneBest(pop)

	// Evolution loop
	for gen := 0; gen < cfg.MaxGenerations; gen++ {
		newPop := make([]Individual, cfg.PopulationSize)

		for i := range pop {
			// Select 3 distinct random individuals different from i
			r1, r2, r3 := select3Distinct(i, cfg.PopulationSize)
			xr1, xr2, xr3 := pop[r1].Vector, pop[r2].Vector, pop[r3].Vector

			// Mutation: v = xr1 + F * (xr2 - xr3)
			mutant := make([]float64, dim)
			for j := 0; j < dim; j++ {
				mutant[j] = xr1[j] + cfg.F*(xr2[j]-xr3[j])
			}
			mutant = space.Clip(mutant)

			// Crossover: binomial
			trial := make([]float64, dim)
			copy(trial, pop[i].Vector)
			jRand := rand.Intn(dim)
			for j := 0; j < dim; j++ {
				if rand.Float64() < cfg.CR || j == jRand {
					trial[j] = mutant[j]
				}
			}
			trial = space.Clip(trial)

			newPop[i] = Individual{Vector: trial}
		}

		// Evaluate new population
		evaluatePopulation(newPop, space, fitnessFn, cfg.ParallelEvals)

		// Selection: replace if better
		for i := range pop {
			if newPop[i].Valid && (!pop[i].Valid || newPop[i].Score > pop[i].Score) {
				pop[i] = newPop[i]
			}
		}

		// Update best
		if b := cloneBest(pop); b != nil && (best == nil || b.Score > best.Score) {
			best = b
		}
	}

	return pop, best
}

func evaluatePopulation(
	pop []Individual,
	space ParamSpace,
	fitnessFn func(map[string]any) float64,
	parallel int,
) {
	if parallel <= 1 {
		for i := range pop {
			params := space.ToMap(pop[i].Vector)
			pop[i].Params = params
			pop[i].Score = fitnessFn(params)
			pop[i].Valid = true
		}
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)
	for i := range pop {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			params := space.ToMap(pop[idx].Vector)
			pop[idx].Params = params
			pop[idx].Score = fitnessFn(params)
			pop[idx].Valid = true
		}(i)
	}
	wg.Wait()
}

func select3Distinct(exclude, n int) (r1, r2, r3 int) {
	for {
		r1 = rand.Intn(n)
		if r1 != exclude {
			break
		}
	}
	for {
		r2 = rand.Intn(n)
		if r2 != exclude && r2 != r1 {
			break
		}
	}
	for {
		r3 = rand.Intn(n)
		if r3 != exclude && r3 != r1 && r3 != r2 {
			break
		}
	}
	return
}

func cloneBest(pop []Individual) *Individual {
	var best *Individual
	for i := range pop {
		if pop[i].Valid && (best == nil || pop[i].Score > best.Score) {
			cp := pop[i]
			best = &cp
		}
	}
	return best
}
