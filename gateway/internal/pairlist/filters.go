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
