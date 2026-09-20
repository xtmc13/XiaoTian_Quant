package ml_test

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func newLoadedPredictor(t *testing.T) *ml.Predictor {
	t.Helper()
	p := ml.NewPredictor()
	if err := p.Load(makeSimpleModelJSON()); err != nil {
		t.Fatalf("load model: %v", err)
	}
	return p
}

func cacheKey() ml.PredictionKey {
	return ml.PredictionKey{ModelName: "BTCUSDT_1h", Symbol: "BTCUSDT", BarTimeMs: 1700000000000}
}

func zeroFeatures() map[string]float64 {
	return map[string]float64{"feature_a": 0.0, "feature_b": 0.0}
}

// TestHashFeaturesStableAndSensitive 稳定 hash：与 map 顺序无关，对值敏感。
func TestHashFeaturesStableAndSensitive(t *testing.T) {
	h1 := ml.HashFeatures(map[string]float64{"feature_a": 0.0, "feature_b": 0.0})
	h2 := ml.HashFeatures(map[string]float64{"feature_b": 0.0, "feature_a": 0.0})
	if h1 != h2 {
		t.Fatalf("hash should be order-independent: %s vs %s", h1, h2)
	}
	if len(h1) != 16 {
		t.Fatalf("hash length = %d, want 16", len(h1))
	}
	h3 := ml.HashFeatures(map[string]float64{"feature_a": 0.000000000001, "feature_b": 0.0})
	if h1 == h3 {
		t.Fatalf("hash should differ when a feature value changes")
	}
}

// TestPredictionCacheMissThenHit 未命中计算并落库；再次查询命中（DB 唯一约束去重）。
func TestPredictionCacheMissThenHit(t *testing.T) {
	setupMLTestDB(t)
	repo := store.NewMLPredictionRepo()
	cache := ml.NewPredictionCache(repo)
	p := newLoadedPredictor(t)

	key := cacheKey()
	v1, err := cache.Predict(p, key, zeroFeatures())
	if err != nil {
		t.Fatalf("predict: %v", err)
	}
	ptAssertFloat(t, v1, 0.25, "first call computes 0.25")
	if n, _ := repo.Count(); n != 1 {
		t.Fatalf("count = %d, want 1 (miss stored)", n)
	}

	v2, err := cache.Predict(p, key, zeroFeatures())
	if err != nil || v2 != v1 {
		t.Fatalf("second call = %v, %v; want cache hit %v", v2, err, v1)
	}
	if n, _ := repo.Count(); n != 1 {
		t.Fatalf("count = %d, want 1 (no duplicate row)", n)
	}

	// 模拟进程重启：新缓存实例直接命中 DB，不再插入。
	cache2 := ml.NewPredictionCache(repo)
	v3, err := cache2.Predict(p, key, zeroFeatures())
	if err != nil || v3 != v1 {
		t.Fatalf("restart call = %v, %v; want DB hit %v", v3, err, v1)
	}
	if n, _ := repo.Count(); n != 1 {
		t.Fatalf("count = %d, want 1 (DB hit, no insert)", n)
	}
}

// TestPredictionCacheDBHitReturnsStoredValue 命中直接返回落库值（不经模型推理）。
func TestPredictionCacheDBHitReturnsStoredValue(t *testing.T) {
	setupMLTestDB(t)
	repo := store.NewMLPredictionRepo()
	cache := ml.NewPredictionCache(repo)
	p := newLoadedPredictor(t)

	key := cacheKey()
	hash := ml.HashFeatures(zeroFeatures())
	// 预置一个模型算不出来的值：能原样返回即证明走了缓存命中路径。
	if err := repo.Insert(key.ModelName, key.Symbol, key.BarTimeMs, hash, 999.0); err != nil {
		t.Fatalf("insert: %v", err)
	}

	v, err := cache.Predict(p, key, zeroFeatures())
	if err != nil {
		t.Fatalf("predict: %v", err)
	}
	if v != 999.0 {
		t.Fatalf("predict = %v, want 999.0 (cached value)", v)
	}
}

// TestPredictionCacheDifferentFeaturesNoCrossHit 不同特征向量/不同 bar_time 互不串缓存。
func TestPredictionCacheDifferentFeaturesNoCrossHit(t *testing.T) {
	setupMLTestDB(t)
	repo := store.NewMLPredictionRepo()
	cache := ml.NewPredictionCache(repo)
	p := newLoadedPredictor(t)

	key := cacheKey()
	v0, _ := cache.Predict(p, key, zeroFeatures())

	// 不同特征值 → 未命中，重新推理（0.45 来自右侧叶子路径）。
	other := map[string]float64{"feature_a": 1.0, "feature_b": 0.0}
	v1, err := cache.Predict(p, key, other)
	if err != nil {
		t.Fatalf("predict: %v", err)
	}
	if v1 == v0 {
		t.Fatalf("different features must not reuse cached prediction: %v vs %v", v1, v0)
	}
	ptAssertFloat(t, v1, 0.45, "feature_a=1 → right path")

	// 不同 bar_time → 未命中。
	key2 := ml.PredictionKey{ModelName: key.ModelName, Symbol: key.Symbol, BarTimeMs: key.BarTimeMs + 3600000}
	v2, err := cache.Predict(p, key2, zeroFeatures())
	if err != nil || v2 != v0 {
		t.Fatalf("other bar_time = %v, %v; want fresh compute %v", v2, err, v0)
	}

	// 不同模型名 → 未命中。
	key3 := ml.PredictionKey{ModelName: "OTHER_model", Symbol: key.Symbol, BarTimeMs: key.BarTimeMs}
	if _, err := cache.Predict(p, key3, zeroFeatures()); err != nil {
		t.Fatalf("predict: %v", err)
	}
	if n, _ := repo.Count(); n != 4 {
		t.Fatalf("count = %d, want 4 distinct rows", n)
	}
}

// TestPredictionCacheInvalidateModel 按模型失效：内存 + DB 一起清，其他模型保留。
func TestPredictionCacheInvalidateModel(t *testing.T) {
	setupMLTestDB(t)
	repo := store.NewMLPredictionRepo()
	cache := ml.NewPredictionCache(repo)
	p := newLoadedPredictor(t)

	key := cacheKey()
	if _, err := cache.Predict(p, key, zeroFeatures()); err != nil {
		t.Fatalf("predict: %v", err)
	}
	other := ml.PredictionKey{ModelName: "OTHER_model", Symbol: "ETHUSDT", BarTimeMs: 1700000000000}
	if _, err := cache.Predict(p, other, zeroFeatures()); err != nil {
		t.Fatalf("predict: %v", err)
	}

	if n, err := cache.InvalidateModel(key.ModelName); err != nil || n != 1 {
		t.Fatalf("invalidate: n=%d err=%v, want 1 row", n, err)
	}

	// DB 层：该模型 miss，另一模型 hit。
	if _, ok, _ := repo.Get(key.ModelName, key.Symbol, key.BarTimeMs, ml.HashFeatures(zeroFeatures())); ok {
		t.Fatalf("invalidated model should miss in DB")
	}
	if _, ok, _ := repo.Get(other.ModelName, other.Symbol, other.BarTimeMs, ml.HashFeatures(zeroFeatures())); !ok {
		t.Fatalf("other model should be kept in DB")
	}

	// 内存层：失效后再查会 miss 落库路径（重新插入说明内存已清）。
	if n, _ := repo.Count(); n != 1 {
		t.Fatalf("count = %d, want 1 after invalidation", n)
	}
	if _, err := cache.Predict(p, key, zeroFeatures()); err != nil {
		t.Fatalf("predict after invalidate: %v", err)
	}
	if n, _ := repo.Count(); n != 2 {
		t.Fatalf("count = %d, want 2 (recomputed and re-stored)", n)
	}
}

// TestPredictionCacheNilRepoAndDegenerateKey nil 仓库/残缺 key 退化为裸推理，不 panic。
func TestPredictionCacheNilRepoAndDegenerateKey(t *testing.T) {
	p := newLoadedPredictor(t)
	cache := ml.NewPredictionCache(nil)

	v, err := cache.Predict(p, cacheKey(), zeroFeatures())
	if err != nil || v != 0.25 {
		t.Fatalf("pure-memory cache = %v, %v; want 0.25", v, err)
	}

	// bar_time 缺失 → 不走缓存，直接推理。
	badKey := ml.PredictionKey{ModelName: "m", Symbol: "BTCUSDT", BarTimeMs: 0}
	v, err = cache.Predict(p, badKey, zeroFeatures())
	if err != nil || v != 0.25 {
		t.Fatalf("degenerate key = %v, %v; want direct compute 0.25", v, err)
	}

	// 缓存层为 nil 也安全。
	var nilCache *ml.PredictionCache
	v, err = nilCache.Predict(p, cacheKey(), zeroFeatures())
	if err != nil || v != 0.25 {
		t.Fatalf("nil cache = %v, %v; want direct compute 0.25", v, err)
	}
}
