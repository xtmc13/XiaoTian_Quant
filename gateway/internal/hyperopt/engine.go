package hyperopt

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// ── Engine ─────────────────────────────────────────────────────

// Engine runs hyperopt optimization jobs.
type Engine struct {
	sampler    Sampler
	space      *SearchSpace
	objective  ObjectiveFunc
	trials     []Trial
	bestTrial  *Trial
	rng        *rand.Rand

	mu         sync.RWMutex
	running    bool
	cancelled  bool
	maxEvals   int
	currentID  int

	// Callbacks
	OnTrialComplete func(trial Trial)
	OnBestUpdate    func(trial Trial)
	OnProgress      func(done, total int, bestLoss float64)
}

// EngineConfig configures the hyperopt engine.
type EngineConfig struct {
	MaxEvals   int    `json:"max_evals"`   // maximum number of evaluations
	Sampler    string `json:"sampler"`     // "tpe", "random", "grid"
	GridPoints int    `json:"grid_points"` // points per dimension for grid search
	Seed       int64  `json:"seed"`
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxEvals:   100,
		Sampler:    "tpe",
		GridPoints: 5,
		Seed:       time.Now().UnixNano(),
	}
}

// NewEngine creates a hyperopt engine.
func NewEngine(cfg EngineConfig, space *SearchSpace, objective ObjectiveFunc) *Engine {
	rng := rand.New(rand.NewSource(cfg.Seed))

	var sampler Sampler
	switch cfg.Sampler {
	case "grid":
		sampler = NewGridSampler(space, cfg.GridPoints)
	case "random":
		sampler = &RandomSampler{}
	case "cmaes":
		sampler = NewCMAESSampler()
	case "tpe":
		fallthrough
	default:
		sampler = NewTPESampler()
	}

	return &Engine{
		sampler:   sampler,
		space:     space,
		objective: objective,
		rng:       rng,
		maxEvals:  cfg.MaxEvals,
		trials:    make([]Trial, 0, cfg.MaxEvals),
	}
}

// Run executes the optimization synchronously.
func (e *Engine) Run(ctx context.Context) (*Result, error) {
	e.mu.Lock()
	e.running = true
	e.cancelled = false
	e.currentID = 0
	e.trials = e.trials[:0]
	e.bestTrial = nil
	e.mu.Unlock()

	startTime := time.Now()

	for i := 0; i < e.maxEvals; i++ {
		select {
		case <-ctx.Done():
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
			return e.buildResult(startTime), ctx.Err()
		default:
		}

		// Check if cancelled
		e.mu.RLock()
		cancelled := e.cancelled
		e.mu.RUnlock()
		if cancelled {
			break
		}

		// Get next candidate
		params := e.sampler.Next(e.space, e.trials, e.rng)
		if params == nil {
			break // grid exhausted
		}
		params = e.space.Quantize(params)

		// Evaluate
		trialStart := time.Now()
		loss, metrics, err := e.objective(params)
		trialDuration := time.Since(trialStart)

		if err != nil {
			loss = math.Inf(1)
			if metrics == nil {
				metrics = map[string]float64{"error": 1}
			}
		}

		e.mu.Lock()
		e.currentID++
		trial := Trial{
			ID:        e.currentID,
			Params:    params,
			Loss:      loss,
			Metrics:   metrics,
			Duration:  trialDuration,
			Timestamp: time.Now().UnixMilli(),
		}

		// Check if best
		if e.bestTrial == nil || loss < e.bestTrial.Loss {
			trial.IsBest = true
			if e.bestTrial != nil {
				e.bestTrial.IsBest = false
			}
			e.bestTrial = &trial
			if e.OnBestUpdate != nil {
				e.mu.Unlock()
				e.OnBestUpdate(trial)
				e.mu.Lock()
			}
		}

		e.trials = append(e.trials, trial)
		e.mu.Unlock()

		if e.OnTrialComplete != nil {
			e.OnTrialComplete(trial)
		}
		if e.OnProgress != nil {
			e.OnProgress(i+1, e.maxEvals, e.bestTrial.Loss)
		}
	}

	e.mu.Lock()
	e.running = false
	e.mu.Unlock()

	return e.buildResult(startTime), nil
}

// Cancel stops the optimization.
func (e *Engine) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelled = true
}

// IsRunning returns true if optimization is in progress.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// GetTrials returns all completed trials.
func (e *Engine) GetTrials() []Trial {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Trial, len(e.trials))
	copy(result, e.trials)
	return result
}

// GetBestTrial returns the best trial so far.
func (e *Engine) GetBestTrial() *Trial {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.bestTrial == nil {
		return nil
	}
	cp := *e.bestTrial
	return &cp
}

// GetProgress returns current progress.
func (e *Engine) GetProgress() (done, total int, bestLoss float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.trials), e.maxEvals, e.bestTrial.Loss
}

func (e *Engine) buildResult(startTime time.Time) *Result {
	e.mu.RLock()
	defer e.mu.RUnlock()

	trials := make([]Trial, len(e.trials))
	copy(trials, e.trials)

	// Sort by loss
	sort.Slice(trials, func(i, j int) bool { return trials[i].Loss < trials[j].Loss })

	var best *Trial
	if len(trials) > 0 {
		cp := trials[0]
		best = &cp
	}

	// Compute statistics
	var losses []float64
	for _, t := range trials {
		if !math.IsInf(t.Loss, 1) {
			losses = append(losses, t.Loss)
		}
	}

	return &Result{
		Trials:       trials,
		BestTrial:    best,
		TotalEvals:   len(trials),
		Duration:     time.Since(startTime),
		Sampler:      e.sampler.Name(),
		SpaceDims:    e.space.Dimensions(),
		MeanLoss:     mean(losses),
		MedianLoss:   median(losses),
		StdLoss:      stddev(losses),
		MinLoss:      minFloat(losses),
		MaxLoss:      maxFloat(losses),
	}
}

// ── Result ─────────────────────────────────────────────────────

// Result holds the outcome of a hyperopt run.
type Result struct {
	Trials     []Trial       `json:"trials"`
	BestTrial  *Trial        `json:"best_trial"`
	TotalEvals int           `json:"total_evals"`
	Duration   time.Duration `json:"duration"`
	Sampler    string        `json:"sampler"`
	SpaceDims  int           `json:"space_dims"`
	MeanLoss   float64       `json:"mean_loss"`
	MedianLoss float64       `json:"median_loss"`
	StdLoss    float64       `json:"std_loss"`
	MinLoss    float64       `json:"min_loss"`
	MaxLoss    float64       `json:"max_loss"`
}

// BestParams returns the best parameter set as a map.
func (r *Result) BestParams() map[string]any {
	if r.BestTrial == nil {
		return nil
	}
	return r.BestTrial.Params
}

// ── Batch / Parallel Runner ─────────────────────────────────────

// ParallelEngine runs multiple hyperopt evaluations in parallel.
type ParallelEngine struct {
	*Engine
	workers int
}

// NewParallelEngine creates a parallel hyperopt engine.
func NewParallelEngine(cfg EngineConfig, space *SearchSpace, objective ObjectiveFunc, workers int) *ParallelEngine {
	if workers <= 0 {
		workers = 4
	}
	return &ParallelEngine{
		Engine:  NewEngine(cfg, space, objective),
		workers: workers,
	}
}

// RunParallel executes optimization with parallel workers.
func (pe *ParallelEngine) RunParallel(ctx context.Context) (*Result, error) {
	pe.mu.Lock()
	pe.running = true
	pe.cancelled = false
	pe.currentID = 0
	pe.trials = pe.trials[:0]
	pe.bestTrial = nil
	pe.mu.Unlock()

	startTime := time.Now()

	// Inner context for coordinated cancellation
	innerCtx, innerCancel := context.WithCancel(ctx)
	defer innerCancel()

	paramsCh := make(chan map[string]any, pe.workers*2)
	trialCh := make(chan Trial, pe.maxEvals)
	var wg sync.WaitGroup

	// ── Parameter generator (serial, snapshot-safe) ────────────
	go func() {
		defer close(paramsCh)
		for i := 0; i < pe.maxEvals; i++ {
			select {
			case <-innerCtx.Done():
				return
			default:
			}

			// Snapshot trials to avoid racing with workers
			pe.mu.RLock()
			cancelled := pe.cancelled
			trialSnapshot := make([]Trial, len(pe.trials))
			copy(trialSnapshot, pe.trials)
			pe.mu.RUnlock()

			if cancelled {
				return
			}

			params := pe.sampler.Next(pe.space, trialSnapshot, pe.rng)
			if params == nil {
				return // grid exhausted
			}
			params = pe.space.Quantize(params)

			select {
			case paramsCh <- params:
			case <-innerCtx.Done():
				return
			}
		}
	}()

	// ── Worker pool ──────────────────────────────────────────────
	for w := 0; w < pe.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for params := range paramsCh {
				select {
				case <-innerCtx.Done():
					return
				default:
				}

				trialStart := time.Now()
				loss, metrics, err := pe.objective(params)
				duration := time.Since(trialStart)

				if err != nil {
					loss = math.Inf(1)
					if metrics == nil {
						metrics = map[string]float64{"error": 1}
					}
				}

				pe.mu.Lock()
				pe.currentID++
				trial := Trial{
					ID:        pe.currentID,
					Params:    params,
					Loss:      loss,
					Metrics:   metrics,
					Duration:  duration,
					Timestamp: time.Now().UnixMilli(),
				}

				if pe.bestTrial == nil || loss < pe.bestTrial.Loss {
					trial.IsBest = true
					if pe.bestTrial != nil {
						pe.bestTrial.IsBest = false
					}
					pe.bestTrial = &trial
					if pe.OnBestUpdate != nil {
						pe.mu.Unlock()
						pe.OnBestUpdate(trial)
						pe.mu.Lock()
					}
				}

				pe.trials = append(pe.trials, trial)
				pe.mu.Unlock()

				if pe.OnTrialComplete != nil {
					pe.OnTrialComplete(trial)
				}

				select {
				case trialCh <- trial:
				case <-innerCtx.Done():
					return
				}
			}
		}()
	}

	// ── Collector + progress ─────────────────────────────────────
	go func() {
		wg.Wait()
		close(trialCh)
	}()

	done := 0
	for range trialCh {
		done++
		if pe.OnProgress != nil {
			pe.mu.RLock()
			bestLoss := math.Inf(1)
			if pe.bestTrial != nil {
				bestLoss = pe.bestTrial.Loss
			}
			pe.mu.RUnlock()
			pe.OnProgress(done, pe.maxEvals, bestLoss)
		}
	}

	pe.mu.Lock()
	pe.running = false
	pe.mu.Unlock()

	return pe.buildResult(startTime), ctx.Err()
}

// ── Statistics helpers ─────────────────────────────────────────

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func stddev(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	m := mean(vals)
	var sum float64
	for _, v := range vals {
		d := v - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(vals)-1))
}

func minFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
