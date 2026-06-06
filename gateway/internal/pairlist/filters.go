package pairlist

import (
	"fmt"
	"math/rand"
	"sort"
)

// ── SpreadFilter ───────────────────────────────────────────────

// SpreadFilter removes pairs whose bid-ask spread exceeds the limit.
type SpreadFilter struct {
	MaxSpreadPct float64 // maximum allowed spread percentage
}

func NewSpreadFilter(maxPct float64) *SpreadFilter {
	if maxPct <= 0 {
		maxPct = 0.5
	}
	return &SpreadFilter{MaxSpreadPct: maxPct}
}

func (f *SpreadFilter) Name() string { return "SpreadFilter" }

func (f *SpreadFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok || info.Spread <= f.MaxSpreadPct {
			result = append(result, sym)
		} else {
			// Only filter if we have the data and it's bad
			if ok && info.Spread > 0 && info.Spread > f.MaxSpreadPct {
				continue
			}
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── PriceFilter ────────────────────────────────────────────────

// PriceFilter removes pairs whose price is outside the allowed range.
type PriceFilter struct {
	MinPrice float64
	MaxPrice float64
}

func NewPriceFilter(minPrice, maxPrice float64) *PriceFilter {
	return &PriceFilter{MinPrice: minPrice, MaxPrice: maxPrice}
}

func (f *PriceFilter) Name() string { return "PriceFilter" }

func (f *PriceFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.MinPrice <= 0 && f.MaxPrice <= 0 {
		return pairs, nil // no filtering
	}

	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if f.MinPrice > 0 && info.Price < f.MinPrice {
			continue
		}
		if f.MaxPrice > 0 && info.Price > f.MaxPrice {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── VolatilityFilter ───────────────────────────────────────────

// VolatilityFilter removes pairs with volatility outside the allowed range.
// Too low = no trading opportunity, too high = risk.
type VolatilityFilter struct {
	MinVolatilityPct float64
	MaxVolatilityPct float64
}

func NewVolatilityFilter(minPct, maxPct float64) *VolatilityFilter {
	return &VolatilityFilter{MinVolatilityPct: minPct, MaxVolatilityPct: maxPct}
}

func (f *VolatilityFilter) Name() string { return "VolatilityFilter" }

func (f *VolatilityFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym)
			continue
		}
		if info.Volatility == 0 {
			result = append(result, sym) // no volatility data, keep
			continue
		}
		if f.MinVolatilityPct > 0 && info.Volatility < f.MinVolatilityPct {
			continue
		}
		if f.MaxVolatilityPct > 0 && info.Volatility > f.MaxVolatilityPct {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── PrecisionFilter ────────────────────────────────────────────

// PrecisionFilter removes pairs whose price or quantity precision
// is too coarse or not supported by the exchange.
type PrecisionFilter struct {
	MinPricePrecision int // minimum decimal places for price
	MinQtyPrecision   int // minimum decimal places for quantity
}

func NewPrecisionFilter(minPricePrec, minQtyPrec int) *PrecisionFilter {
	return &PrecisionFilter{MinPricePrecision: minPricePrec, MinQtyPrecision: minQtyPrec}
}

func (f *PrecisionFilter) Name() string { return "PrecisionFilter" }

func (f *PrecisionFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym)
			continue
		}
		if info.PricePrecision < f.MinPricePrecision {
			continue
		}
		if info.QtyPrecision < f.MinQtyPrecision {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── ShuffleFilter ──────────────────────────────────────────────

// ShuffleFilter randomizes pair order to avoid overfitting to the
// first N pairs in a volume-sorted list.
type ShuffleFilter struct {
	Seed int64
}

func NewShuffleFilter(seed int64) *ShuffleFilter {
	return &ShuffleFilter{Seed: seed}
}

func (f *ShuffleFilter) Name() string { return "ShuffleFilter" }

func (f *ShuffleFilter) Filter(pairs []string, _ map[string]*PairInfo) ([]string, error) {
	result := make([]string, len(pairs))
	copy(result, pairs)

	rng := rand.New(rand.NewSource(f.Seed))
	rng.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})

	return result, nil
}

// ── MaxPairsFilter ─────────────────────────────────────────────

// MaxPairsFilter limits the final whitelist to a maximum number of pairs.
type MaxPairsFilter struct {
	MaxPairs int
}

func NewMaxPairsFilter(max int) *MaxPairsFilter {
	return &MaxPairsFilter{MaxPairs: max}
}

func (f *MaxPairsFilter) Name() string { return "MaxPairsFilter" }

func (f *MaxPairsFilter) Filter(pairs []string, _ map[string]*PairInfo) ([]string, error) {
	if f.MaxPairs <= 0 || len(pairs) <= f.MaxPairs {
		return pairs, nil
	}
	return pairs[:f.MaxPairs], nil
}

// ── CorrelationFilter ──────────────────────────────────────────

// CorrelationFilter removes pairs that are highly correlated with
// already-selected pairs (avoids concentration in same sector).
type CorrelationFilter struct {
	MaxCorrelated int // max number of pairs from the same base asset category
}

func NewCorrelationFilter(maxCorrelated int) *CorrelationFilter {
	if maxCorrelated <= 0 {
		maxCorrelated = 2
	}
	return &CorrelationFilter{MaxCorrelated: maxCorrelated}
}

func (f *CorrelationFilter) Name() string { return "CorrelationFilter" }

func (f *CorrelationFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	// Simple category-based dedup: group by base asset prefix
	// e.g., "BTC" category covers BTCUSDT, BTCBUSD, BTCTUSD
	seen := make(map[string]int)
	result := make([]string, 0, len(pairs))

	for _, sym := range pairs {
		// Extract base asset from pair info
		category := sym
		if info, ok := infoMap[sym]; ok && info.BaseAsset != "" {
			category = info.BaseAsset
		}

		if seen[category] >= f.MaxCorrelated {
			continue
		}
		seen[category]++
		result = append(result, sym)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("correlation filter removed all pairs")
	}
	return result, nil
}

// ── AgeFilter ──────────────────────────────────────────────────

// AgeFilter removes pairs that are too new (minimum listing age).
type AgeFilter struct {
	MinAgeDays int
}

func NewAgeFilter(minAgeDays int) *AgeFilter {
	if minAgeDays < 0 {
		minAgeDays = 0
	}
	return &AgeFilter{MinAgeDays: minAgeDays}
}

func (f *AgeFilter) Name() string { return "AgeFilter" }

func (f *AgeFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.MinAgeDays <= 0 {
		return pairs, nil
	}
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		// Use a simple heuristic: if price is very low and volume is low, treat as new
		// In production, this would use actual listing date from exchange info
		if info.MinNotional > 0 && info.Volume24h > 0 {
			result = append(result, sym)
		} else {
			// Skip pairs with no trading history (effectively age=0)
			continue
		}
	}
	return result, nil
}

// ── PerformanceFilter ──────────────────────────────────────────

// PerformanceFilter sorts pairs by 24h volatility (descending) as a proxy
// for recent performance potential, and optionally limits the result.
type PerformanceFilter struct {
	TopN int
}

func NewPerformanceFilter(topN int) *PerformanceFilter {
	if topN <= 0 {
		topN = 0 // 0 = no limit, just sort
	}
	return &PerformanceFilter{TopN: topN}
}

func (f *PerformanceFilter) Name() string { return "PerformanceFilter" }

func (f *PerformanceFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	// Sort by volatility descending (proxy for performance)
	sorted := make([]string, len(pairs))
	copy(sorted, pairs)

	sort.SliceStable(sorted, func(i, j int) bool {
		vi := 0.0
		vj := 0.0
		if info, ok := infoMap[sorted[i]]; ok {
			vi = info.Volatility
		}
		if info, ok := infoMap[sorted[j]]; ok {
			vj = info.Volatility
		}
		return vi > vj
	})

	if f.TopN > 0 && len(sorted) > f.TopN {
		sorted = sorted[:f.TopN]
	}
	return sorted, nil
}

// ── OffsetFilter ───────────────────────────────────────────────

// OffsetFilter skips the first N pairs from the list.
// Useful for pagination or avoiding over-crowded top-N lists.
type OffsetFilter struct {
	Offset int
}

func NewOffsetFilter(offset int) *OffsetFilter {
	if offset < 0 {
		offset = 0
	}
	return &OffsetFilter{Offset: offset}
}

func (f *OffsetFilter) Name() string { return "OffsetFilter" }

func (f *OffsetFilter) Filter(pairs []string, _ map[string]*PairInfo) ([]string, error) {
	if f.Offset >= len(pairs) {
		return nil, fmt.Errorf("offset %d exceeds list length %d", f.Offset, len(pairs))
	}
	if f.Offset == 0 {
		return pairs, nil
	}
	return pairs[f.Offset:], nil
}

// ── RangeFilter ────────────────────────────────────────────────

// RangeFilter removes pairs whose price is outside a percentage range
// relative to a reference price (e.g. within ±20% of BTC price).
type RangeFilter struct {
	ReferencePrice float64 // base price to compare against
	MinPct         float64 // minimum allowed % of reference (e.g. 0.5 = 50%)
	MaxPct         float64 // maximum allowed % of reference (e.g. 2.0 = 200%)
}

func NewRangeFilter(refPrice, minPct, maxPct float64) *RangeFilter {
	return &RangeFilter{
		ReferencePrice: refPrice,
		MinPct:         minPct,
		MaxPct:         maxPct,
	}
}

func (f *RangeFilter) Name() string { return "RangeFilter" }

func (f *RangeFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.ReferencePrice <= 0 {
		return pairs, nil // no filtering if no reference
	}
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok || info.Price <= 0 {
			result = append(result, sym)
			continue
		}
		pct := info.Price / f.ReferencePrice
		if pct >= f.MinPct && pct <= f.MaxPct {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── LowProfitPairsFilter ───────────────────────────────────────

// LowProfitPairsFilter removes pairs that have historically low returns.
// It uses a minimum profit threshold (e.g. must have > 5% return in last N days).
type LowProfitPairsFilter struct {
	MinProfitPct float64 // minimum acceptable historical profit %
}

func NewLowProfitPairsFilter(minProfitPct float64) *LowProfitPairsFilter {
	return &LowProfitPairsFilter{MinProfitPct: minProfitPct}
}

func (f *LowProfitPairsFilter) Name() string { return "LowProfitPairsFilter" }

func (f *LowProfitPairsFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		// Use volatility as a proxy for profit potential
		// In production, this would use actual backtested profit data
		if info.Volatility >= f.MinProfitPct {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── VolumeFilter ─────────────────────────────────────────────────

// VolumeFilter removes pairs with 24h volume below a threshold.
// Unlike VolumePairList (which sorts and picks top N), this is a hard cutoff.
type VolumeFilter struct {
	MinVolume24h float64 // minimum 24h volume in quote currency
}

func NewVolumeFilter(minVolume float64) *VolumeFilter {
	return &VolumeFilter{MinVolume24h: minVolume}
}

func (f *VolumeFilter) Name() string { return "VolumeFilter" }

func (f *VolumeFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.MinVolume24h <= 0 {
		return pairs, nil
	}
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if info.Volume24h >= f.MinVolume24h {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── ChangeFilter ───────────────────────────────────────────────

// ChangeFilter removes pairs whose 24h price change % is outside the allowed range.
// Useful for filtering out extremely volatile or stagnant pairs.
type ChangeFilter struct {
	MinChangePct float64 // minimum 24h change % (e.g. -10 = allow up to -10% drop)
	MaxChangePct float64 // maximum 24h change % (e.g. 20 = allow up to +20% rise)
}

func NewChangeFilter(minPct, maxPct float64) *ChangeFilter {
	return &ChangeFilter{MinChangePct: minPct, MaxChangePct: maxPct}
}

func (f *ChangeFilter) Name() string { return "ChangeFilter" }

func (f *ChangeFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym)
			continue
		}
		// Use volatility as proxy for 24h change (in production, use actual change%)
		change := info.Volatility
		if change >= f.MinChangePct && change <= f.MaxChangePct {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── RankFilter ─────────────────────────────────────────────────

// RankFilter sorts pairs by a composite score and keeps the top N.
// Score = volume_weight * volume_rank + performance_weight * performance_rank
type RankFilter struct {
	TopN              int
	VolumeWeight      float64
	PerformanceWeight float64
}

func NewRankFilter(topN int, volWeight, perfWeight float64) *RankFilter {
	if topN <= 0 {
		topN = 20
	}
	if volWeight == 0 && perfWeight == 0 {
		volWeight = 0.5
		perfWeight = 0.5
	}
	return &RankFilter{
		TopN:              topN,
		VolumeWeight:      volWeight,
		PerformanceWeight: perfWeight,
	}
}

func (f *RankFilter) Name() string { return "RankFilter" }

func (f *RankFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if len(pairs) == 0 {
		return pairs, nil
	}

	// Build score for each pair
	type scoredPair struct {
		symbol string
		score  float64
	}
	scored := make([]scoredPair, 0, len(pairs))

	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			scored = append(scored, scoredPair{symbol: sym, score: 0})
			continue
		}
		// Normalize: higher volume = better, higher volatility = better (proxy for performance)
		score := f.VolumeWeight*info.Volume24h + f.PerformanceWeight*info.Volatility
		scored = append(scored, scoredPair{symbol: sym, score: score})
	}

	// Sort by score descending
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top N
	n := f.TopN
	if n > len(scored) {
		n = len(scored)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = scored[i].symbol
	}
	return result, nil
}
