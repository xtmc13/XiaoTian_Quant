package pairlist

import (
	"math"
)

// ── DelistFilter ───────────────────────────────────────────────

// DelistFilter removes pairs that are not in active trading status.
// It detects exchange delisting, halts, or break status.
type DelistFilter struct {
	AllowedStatuses []string
}

func NewDelistFilter() *DelistFilter {
	return &DelistFilter{
		AllowedStatuses: []string{"TRADING"},
	}
}

func (f *DelistFilter) Name() string { return "DelistFilter" }

func (f *DelistFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		allowed := false
		for _, s := range f.AllowedStatuses {
			if info.Status == s {
				allowed = true
				break
			}
		}
		if allowed {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── RangeStabilityFilter ───────────────────────────────────────

// RangeStabilityFilter removes pairs whose price range is too narrow,
// i.e. pairs that are excessively stable and offer little trading opportunity.
type RangeStabilityFilter struct {
	MinRangeRatio float64 // minimum (high-low)/low ratio, e.g. 0.01 = 1%
}

func NewRangeStabilityFilter(minRatio float64) *RangeStabilityFilter {
	if minRatio <= 0 {
		minRatio = 0.005 // 0.5%
	}
	return &RangeStabilityFilter{MinRangeRatio: minRatio}
}

func (f *RangeStabilityFilter) Name() string { return "RangeStabilityFilter" }

func (f *RangeStabilityFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.MinRangeRatio <= 0 {
		return pairs, nil
	}
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if info.PriceRangeRatio >= f.MinRangeRatio {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── FullTradesFilter ───────────────────────────────────────────

// FullTradesFilter removes pairs that already have an open position.
// This prevents over-concentration in the same pair.
type FullTradesFilter struct{}

func NewFullTradesFilter() *FullTradesFilter {
	return &FullTradesFilter{}
}

func (f *FullTradesFilter) Name() string { return "FullTradesFilter" }

func (f *FullTradesFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if !info.HasPosition {
			result = append(result, sym)
		}
	}
	return result, nil
}

// ── MarketCapFilter ────────────────────────────────────────────

// MarketCapFilter removes pairs whose market capitalisation is outside
// the allowed range. Useful for focusing on large-caps or avoiding micro-caps.
type MarketCapFilter struct {
	MinMarketCap float64
	MaxMarketCap float64
}

func NewMarketCapFilter(minCap, maxCap float64) *MarketCapFilter {
	return &MarketCapFilter{MinMarketCap: minCap, MaxMarketCap: maxCap}
}

func (f *MarketCapFilter) Name() string { return "MarketCapFilter" }

func (f *MarketCapFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if f.MinMarketCap > 0 && info.MarketCap < f.MinMarketCap {
			continue
		}
		if f.MaxMarketCap > 0 && info.MarketCap > f.MaxMarketCap {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── VolumeChangeFilter ─────────────────────────────────────────

// VolumeChangeFilter removes pairs whose volume change % is outside the
// allowed range. Helps detect sudden volume collapse or manipulation.
type VolumeChangeFilter struct {
	MinChange float64 // minimum allowed volume change (e.g. -0.5 = -50%)
	MaxChange float64 // maximum allowed volume change (e.g. 3.0 = +300%)
}

func NewVolumeChangeFilter(minChange, maxChange float64) *VolumeChangeFilter {
	if minChange == 0 && maxChange == 0 {
		minChange = -0.8
		maxChange = 5.0
	}
	return &VolumeChangeFilter{MinChange: minChange, MaxChange: maxChange}
}

func (f *VolumeChangeFilter) Name() string { return "VolumeChangeFilter" }

func (f *VolumeChangeFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if info.VolumeChange < f.MinChange {
			continue
		}
		if f.MaxChange > 0 && info.VolumeChange > f.MaxChange {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── PriceJumpFilter ────────────────────────────────────────────

// PriceJumpFilter removes pairs that have experienced an extreme price
// jump (pump or dump) recently. Uses absolute 24h change %.
type PriceJumpFilter struct {
	MaxJumpPct float64 // maximum allowed absolute price change %
}

func NewPriceJumpFilter(maxJumpPct float64) *PriceJumpFilter {
	if maxJumpPct <= 0 {
		maxJumpPct = 20.0
	}
	return &PriceJumpFilter{MaxJumpPct: maxJumpPct}
}

func (f *PriceJumpFilter) Name() string { return "PriceJumpFilter" }

func (f *PriceJumpFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.MaxJumpPct <= 0 {
		return pairs, nil
	}
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if math.Abs(info.PriceChange) > f.MaxJumpPct {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── LiquidityFilter ────────────────────────────────────────────

// LiquidityFilter removes pairs whose order-book depth is below a threshold.
// Depth is measured in quote currency (e.g. USDT) on each side.
type LiquidityFilter struct {
	MinBidDepth float64
	MinAskDepth float64
}

func NewLiquidityFilter(minBid, minAsk float64) *LiquidityFilter {
	if minBid <= 0 {
		minBid = 50000
	}
	if minAsk <= 0 {
		minAsk = 50000
	}
	return &LiquidityFilter{MinBidDepth: minBid, MinAskDepth: minAsk}
}

func (f *LiquidityFilter) Name() string { return "LiquidityFilter" }

func (f *LiquidityFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if info.BidDepth < f.MinBidDepth {
			continue
		}
		if info.AskDepth < f.MinAskDepth {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}

// ── FundingRateFilter ────────────────────────────────────────────

// FundingRateFilter removes perpetual pairs whose funding rate is too high.
// High funding rates increase holding costs for leveraged positions.
type FundingRateFilter struct {
	MaxFundingRate float64 // max allowed absolute funding rate (e.g. 0.01 = 1%)
}

func NewFundingRateFilter(maxRate float64) *FundingRateFilter {
	if maxRate <= 0 {
		maxRate = 0.01 // 1%
	}
	return &FundingRateFilter{MaxFundingRate: maxRate}
}

func (f *FundingRateFilter) Name() string { return "FundingRateFilter" }

func (f *FundingRateFilter) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	if f.MaxFundingRate <= 0 {
		return pairs, nil
	}
	result := make([]string, 0, len(pairs))
	for _, sym := range pairs {
		info, ok := infoMap[sym]
		if !ok {
			result = append(result, sym) // keep if no data
			continue
		}
		if math.Abs(info.FundingRate) > f.MaxFundingRate {
			continue
		}
		result = append(result, sym)
	}
	return result, nil
}
