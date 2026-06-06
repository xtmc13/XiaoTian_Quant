package hyperopt

import (
	"context"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/strategy"
)

func TestSearchSpaceSample(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.IntParameter("lookback", 20, 5, 100, "buy"))
	reg.Register(strategy.FloatParameter("stop_loss", 0.02, 0.005, 0.20, 0.005, "stoploss"))

	space := NewSearchSpaceFromRegistry(reg)
	if space.Dimensions() != 2 {
		t.Errorf("expected 2 dimensions, got %d", space.Dimensions())
	}

	rng := rand.New(rand.NewSource(42))
	point := space.Sample(rng)
	if len(point) != 2 {
		t.Errorf("expected 2 values in point, got %d", len(point))
	}

	// Check lookback is within range
	lookback := point["lookback"].(int)
	if lookback < 5 || lookback > 100 {
		t.Errorf("lookback %d out of range [5, 100]", lookback)
	}

	// Check stop_loss is within range
	stopLoss := point["stop_loss"].(float64)
	if stopLoss < 0.005 || stopLoss > 0.20 {
		t.Errorf("stop_loss %f out of range [0.005, 0.20]", stopLoss)
	}
}

func TestSearchSpaceQuantize(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.IntParameter("lookback", 20, 5, 100, "buy"))

	space := NewSearchSpaceFromRegistry(reg)
	point := map[string]any{"lookback": 23.7}
	quantized := space.Quantize(point)

	lookback := quantized["lookback"].(int)
	// toInt truncates 23.7 to 23, then quantize rounds to nearest step
	if lookback != 23 {
		t.Errorf("expected quantized lookback 23 (truncated from 23.7), got %d", lookback)
	}
}

func TestRandomSampler(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	sampler := &RandomSampler{}
	if sampler.Name() != "random" {
		t.Errorf("expected name 'random', got %s", sampler.Name())
	}

	rng := rand.New(rand.NewSource(42))
	point := sampler.Next(space, nil, rng)
	if len(point) != 1 {
		t.Errorf("expected 1 value, got %d", len(point))
	}
}

func TestGridSampler(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.IntParameter("a", 10, 0, 20, "buy"))
	reg.Register(strategy.IntParameter("b", 5, 0, 10, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	gs := NewGridSampler(space, 3)
	if gs.Name() != "grid" {
		t.Errorf("expected name 'grid', got %s", gs.Name())
	}

	// Should produce 3*3 = 9 points
	count := 0
	var lastPoint map[string]any
	for {
		point := gs.Next(space, nil, nil)
		if point == nil {
			break
		}
		lastPoint = point
		count++
	}

	if count != 9 {
		t.Errorf("expected 9 grid points, got %d", count)
	}

	if lastPoint == nil {
		t.Fatal("lastPoint is nil")
	}
}

func TestTPESampler(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	sampler := NewTPESampler()
	if sampler.Name() != "tpe" {
		t.Errorf("expected name 'tpe', got %s", sampler.Name())
	}

	rng := rand.New(rand.NewSource(42))

	// Before startup, should return random
	point := sampler.Next(space, nil, rng)
	if len(point) != 1 {
		t.Errorf("expected 1 value, got %d", len(point))
	}

	// After startup with history, should use TPE
	history := []Trial{
		{ID: 1, Params: map[string]any{"x": 0.1}, Loss: 10},
		{ID: 2, Params: map[string]any{"x": 0.9}, Loss: 1},
		{ID: 3, Params: map[string]any{"x": 0.5}, Loss: 5},
		{ID: 4, Params: map[string]any{"x": 0.8}, Loss: 2},
		{ID: 5, Params: map[string]any{"x": 0.2}, Loss: 8},
		{ID: 6, Params: map[string]any{"x": 0.7}, Loss: 3},
		{ID: 7, Params: map[string]any{"x": 0.6}, Loss: 4},
		{ID: 8, Params: map[string]any{"x": 0.3}, Loss: 7},
		{ID: 9, Params: map[string]any{"x": 0.4}, Loss: 6},
		{ID: 10, Params: map[string]any{"x": 0.85}, Loss: 1.5},
		{ID: 11, Params: map[string]any{"x": 0.15}, Loss: 9},
	}

	point = sampler.Next(space, history, rng)
	if len(point) != 1 {
		t.Errorf("expected 1 value after startup, got %d", len(point))
	}

	// TPE should favor values near 0.9 (low loss)
	x := point["x"].(float64)
	if x < 0.0 || x > 1.0 {
		t.Errorf("x %f out of range [0, 1]", x)
	}
}

func TestEngineRun(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	// Simple objective: minimize (x-0.3)^2
	objective := func(params map[string]any) (float64, map[string]float64, error) {
		x := params["x"].(float64)
		loss := (x - 0.3) * (x - 0.3)
		return loss, map[string]float64{"x": x, "raw": loss}, nil
	}

	cfg := EngineConfig{
		MaxEvals: 20,
		Sampler:  "random",
		Seed:     42,
	}
	engine := NewEngine(cfg, space, objective)

	ctx := context.Background()
	result, err := engine.Run(ctx)
	if err != nil {
		t.Fatalf("engine run failed: %v", err)
	}

	if result.TotalEvals != 20 {
		t.Errorf("expected 20 evals, got %d", result.TotalEvals)
	}

	if result.BestTrial == nil {
		t.Fatal("best trial is nil")
	}

	// Best should be near 0.3
	bestX := result.BestParams()["x"].(float64)
	if math.Abs(bestX-0.3) > 0.2 {
		t.Logf("best x = %f (expected near 0.3)", bestX)
	}

	if result.MinLoss < 0 {
		t.Errorf("min loss should be >= 0, got %f", result.MinLoss)
	}
}

func TestEngineCancel(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	objective := func(params map[string]any) (float64, map[string]float64, error) {
		time.Sleep(10 * time.Millisecond)
		return 0, nil, nil
	}

	cfg := EngineConfig{MaxEvals: 100, Sampler: "random", Seed: 42}
	engine := NewEngine(cfg, space, objective)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := engine.Run(ctx)
	if err == nil {
		t.Error("expected cancellation error")
	}

	if result.TotalEvals >= 100 {
		t.Errorf("expected early termination, got %d evals", result.TotalEvals)
	}
}

func TestParallelEngine(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	objective := func(params map[string]any) (float64, map[string]float64, error) {
		x := params["x"].(float64)
		loss := (x - 0.3) * (x - 0.3)
		return loss, map[string]float64{"x": x}, nil
	}

	cfg := EngineConfig{MaxEvals: 30, Sampler: "random", Seed: 42}
	pe := NewParallelEngine(cfg, space, objective, 4)

	ctx := context.Background()
	result, err := pe.RunParallel(ctx)
	if err != nil {
		t.Fatalf("parallel run failed: %v", err)
	}

	if result.TotalEvals != 30 {
		t.Errorf("expected 30 evals, got %d", result.TotalEvals)
	}

	if result.BestTrial == nil {
		t.Fatal("best trial is nil")
	}
}

func TestCMAESSampler(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	reg.Register(strategy.FloatParameter("y", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	sampler := NewCMAESSampler()
	if sampler.Name() != "cmaes" {
		t.Errorf("expected name 'cmaes', got %s", sampler.Name())
	}

	rng := rand.New(rand.NewSource(42))

	// First call should initialise and return a candidate
	point := sampler.Next(space, nil, rng)
	if len(point) != 2 {
		t.Errorf("expected 2 values, got %d", len(point))
	}

	// Simulate a generation of evaluations
	var history []Trial
	for i := 0; i < 10; i++ {
		p := sampler.Next(space, history, rng)
		if p == nil {
			t.Fatal("unexpected nil from CMA-ES sampler")
		}
		x := p["x"].(float64)
		y := p["y"].(float64)
		// Simple objective: minimum at (0.3, 0.7)
		loss := (x-0.3)*(x-0.3) + (y-0.7)*(y-0.7)
		history = append(history, Trial{
			ID:     i + 1,
			Params: p,
			Loss:   loss,
		})
	}

	// After one generation, sampler should update and continue
	p := sampler.Next(space, history, rng)
	if p == nil {
		t.Fatal("unexpected nil after generation update")
	}
}

func TestCMAESWithMixedTypes(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	reg.Register(strategy.IntParameter("n", 10, 1, 20, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	sampler := NewCMAESSampler()
	rng := rand.New(rand.NewSource(42))

	point := sampler.Next(space, nil, rng)
	if len(point) != 2 {
		t.Errorf("expected 2 values (1 float + 1 int), got %d", len(point))
	}
	// Int parameter should have a fixed random value
	if _, ok := point["n"].(int); !ok {
		t.Errorf("expected int for 'n', got %T", point["n"])
	}
}

func TestEngineCMAES(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	objective := func(params map[string]any) (float64, map[string]float64, error) {
		x := params["x"].(float64)
		loss := (x - 0.3) * (x - 0.3)
		return loss, map[string]float64{"x": x}, nil
	}

	cfg := EngineConfig{
		MaxEvals: 40,
		Sampler:  "cmaes",
		Seed:     42,
	}
	engine := NewEngine(cfg, space, objective)

	ctx := context.Background()
	result, err := engine.Run(ctx)
	if err != nil {
		t.Fatalf("engine run failed: %v", err)
	}

	if result.TotalEvals != 40 {
		t.Errorf("expected 40 evals, got %d", result.TotalEvals)
	}

	if result.BestTrial == nil {
		t.Fatal("best trial is nil")
	}

	bestX := result.BestParams()["x"].(float64)
	if math.Abs(bestX-0.3) > 0.15 {
		t.Logf("best x = %f (expected near 0.3)", bestX)
	}
}

func TestResultBestParams(t *testing.T) {
	result := &Result{
		BestTrial: &Trial{
			Params: map[string]any{"x": 0.5, "y": 10},
			Loss:   1.0,
		},
	}

	params := result.BestParams()
	if params == nil {
		t.Fatal("best params is nil")
	}
	if params["x"] != 0.5 {
		t.Errorf("expected x=0.5, got %v", params["x"])
	}
}

func TestStatisticsHelpers(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}

	if mean(vals) != 3 {
		t.Errorf("mean expected 3, got %f", mean(vals))
	}

	if median(vals) != 3 {
		t.Errorf("median expected 3, got %f", median(vals))
	}

	if stddev(vals) == 0 {
		t.Error("stddev should not be 0")
	}

	if minFloat(vals) != 1 {
		t.Errorf("min expected 1, got %f", minFloat(vals))
	}

	if maxFloat(vals) != 5 {
		t.Errorf("max expected 5, got %f", maxFloat(vals))
	}
}

func TestEarlyStopConfig(t *testing.T) {
	es := DefaultEarlyStopConfig()
	if !es.Enabled {
		t.Error("expected early stop enabled by default")
	}
	if es.MinTrials != 20 {
		t.Errorf("expected minTrials 20, got %d", es.MinTrials)
	}
	if es.Patience != 10 {
		t.Errorf("expected patience 10, got %d", es.Patience)
	}
}

func TestShouldStop(t *testing.T) {
	es := EarlyStopConfig{
		Enabled:        true,
		MinTrials:      5,
		Patience:       3,
		ImprovementPct: 0.01,
	}

	trials := []Trial{
		{ID: 1, Loss: 10.0},
		{ID: 2, Loss: 9.5},
		{ID: 3, Loss: 9.0},
		{ID: 4, Loss: 8.5},
		{ID: 5, Loss: 8.0},
		{ID: 6, Loss: 8.0},
		{ID: 7, Loss: 8.0},
		{ID: 8, Loss: 8.0},
	}

	if es.ShouldStop(trials[:4], 8.0) {
		t.Error("should not stop before minTrials")
	}

	if !es.ShouldStop(trials, 8.0) {
		t.Error("should stop after patience trials with no improvement")
	}

	if !es.ShouldStop(trials, 7.0) {
		t.Error("should stop when recent trials are worse than best")
	}

	trials[7].Loss = 6.0
	if es.ShouldStop(trials, 8.0) {
		t.Error("should not stop when there is improvement")
	}
}

func TestEngineRunWithEarlyStop(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	objective := func(params map[string]any) (float64, map[string]float64, error) {
		x := params["x"].(float64)
		loss := (x - 0.3) * (x - 0.3)
		return loss, map[string]float64{"x": x}, nil
	}

	cfg := EngineConfig{
		MaxEvals:  100,
		Sampler:   "random",
		Seed:      42,
		EarlyStop: EarlyStopConfig{Enabled: true, MinTrials: 10, Patience: 5, ImprovementPct: 0.001},
	}
	engine := NewEngine(cfg, space, objective)

	ctx := context.Background()
	result, err := engine.Run(ctx)
	if err != nil {
		t.Fatalf("engine run failed: %v", err)
	}

	if result.TotalEvals >= 100 {
		t.Logf("did not stop early, completed all %d evals", result.TotalEvals)
	}
}

func TestEngineRunParallel(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	objective := func(params map[string]any) (float64, map[string]float64, error) {
		x := params["x"].(float64)
		loss := (x - 0.3) * (x - 0.3)
		return loss, map[string]float64{"x": x}, nil
	}

	cfg := EngineConfig{MaxEvals: 30, Sampler: "random", Seed: 42}
	engine := NewEngine(cfg, space, objective)

	ctx := context.Background()
	result, err := engine.RunParallel(ctx, 4)
	if err != nil {
		t.Fatalf("parallel run failed: %v", err)
	}

	if result.TotalEvals != 30 {
		t.Errorf("expected 30 evals, got %d", result.TotalEvals)
	}

	if result.BestTrial == nil {
		t.Fatal("best trial is nil")
	}
}

func TestEngineRunParallelWithEarlyStop(t *testing.T) {
	reg := strategy.NewParamRegistry()
	reg.Register(strategy.FloatParameter("x", 0.5, 0.0, 1.0, 0.1, "buy"))
	space := NewSearchSpaceFromRegistry(reg)

	objective := func(params map[string]any) (float64, map[string]float64, error) {
		x := params["x"].(float64)
		loss := (x - 0.3) * (x - 0.3)
		return loss, map[string]float64{"x": x}, nil
	}

	cfg := EngineConfig{
		MaxEvals:  100,
		Sampler:   "random",
		Seed:      42,
		EarlyStop: EarlyStopConfig{Enabled: true, MinTrials: 10, Patience: 5, ImprovementPct: 0.001},
	}
	engine := NewEngine(cfg, space, objective)

	ctx := context.Background()
	result, err := engine.RunParallel(ctx, 4)
	if err != nil {
		t.Fatalf("parallel run with early stop failed: %v", err)
	}

	if result.TotalEvals >= 100 {
		t.Logf("did not stop early, completed all %d evals", result.TotalEvals)
	}

	if result.BestTrial == nil {
		t.Fatal("best trial is nil")
	}
}
