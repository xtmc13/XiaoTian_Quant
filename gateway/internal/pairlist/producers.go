package pairlist

import (
	"fmt"
	"sort"
)

// ── StaticPairList ─────────────────────────────────────────────

// StaticPairList returns a fixed list of trading pairs.
type StaticPairList struct {
	Pairs []string
}

func NewStaticPairList(pairs []string) *StaticPairList {
	return &StaticPairList{Pairs: pairs}
}

func (s *StaticPairList) Name() string { return "StaticPairList" }

func (s *StaticPairList) Generate(exchange, quoteAsset string) ([]string, error) {
	if len(s.Pairs) == 0 {
		return nil, fmt.Errorf("static pairlist is empty")
	}
	result := make([]string, len(s.Pairs))
	copy(result, s.Pairs)
	return result, nil
}

// ── VolumePairList ─────────────────────────────────────────────

// VolumePairList returns the top N pairs by 24h volume.
type VolumePairList struct {
	TopN         int
	MinVolume    float64 // minimum 24h volume in quote currency
	InfoProvider func() ([]*PairInfo, error)
}

func NewVolumePairList(topN int, minVolume float64, provider func() ([]*PairInfo, error)) *VolumePairList {
	if topN <= 0 {
		topN = 30
	}
	return &VolumePairList{TopN: topN, MinVolume: minVolume, InfoProvider: provider}
}

func (v *VolumePairList) Name() string { return "VolumePairList" }

func (v *VolumePairList) Generate(exchange, quoteAsset string) ([]string, error) {
	if v.InfoProvider == nil {
		return nil, fmt.Errorf("volume pairlist requires InfoProvider")
	}

	allPairs, err := v.InfoProvider()
	if err != nil {
		return nil, err
	}

	// Filter by quote asset and min volume
	var eligible []*PairInfo
	for _, p := range allPairs {
		if quoteAsset != "" && p.QuoteAsset != quoteAsset {
			continue
		}
		if p.Volume24h < v.MinVolume {
			continue
		}
		if p.Status != "TRADING" {
			continue
		}
		eligible = append(eligible, p)
	}

	// Sort by volume descending
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Volume24h > eligible[j].Volume24h
	})

	// Take top N
	n := v.TopN
	if n > len(eligible) {
		n = len(eligible)
	}

	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = eligible[i].Symbol
	}
	return result, nil
}

// ── PerformancePairList ────────────────────────────────────────

// PerformancePairList returns the top N pairs by recent price performance.
type PerformancePairList struct {
	TopN         int
	LookbackHours int
	InfoProvider func() ([]*PairInfo, error)
}

func (p *PerformancePairList) Name() string { return "PerformancePairList" }

func (p *PerformancePairList) Generate(exchange, quoteAsset string) ([]string, error) {
	if p.InfoProvider == nil {
		return nil, fmt.Errorf("performance pairlist requires InfoProvider")
	}
	allPairs, err := p.InfoProvider()
	if err != nil {
		return nil, err
	}

	var eligible []*PairInfo
	for _, pair := range allPairs {
		if quoteAsset != "" && pair.QuoteAsset != quoteAsset {
			continue
		}
		if pair.Status != "TRADING" {
			continue
		}
		eligible = append(eligible, pair)
	}

	// Sort by volatility (proxy for recent performance potential)
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Volatility > eligible[j].Volatility
	})

	n := p.TopN
	if n > len(eligible) {
		n = len(eligible)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = eligible[i].Symbol
	}
	return result, nil
}
