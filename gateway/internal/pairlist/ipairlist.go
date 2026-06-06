// Package pairlist implements exchange pair whitelist generation and filtering.
// Pattern: Chain of Responsibility — Producer generates an initial list,
// then Filters are applied in sequence to narrow down to the final whitelist.
package pairlist

// PairInfo holds basic information about a trading pair.
type PairInfo struct {
	Symbol        string  `json:"symbol"`
	BaseAsset     string  `json:"base_asset"`
	QuoteAsset    string  `json:"quote_asset"`
	Price         float64 `json:"price"`
	Volume24h     float64 `json:"volume_24h"`     // 24h volume in quote currency
	Spread        float64 `json:"spread"`          // percentage spread (bid-ask / bid * 100)
	Volatility    float64 `json:"volatility"`      // 24h volatility percentage
	PricePrecision int   `json:"price_precision"`  // decimal places for price
	QtyPrecision  int   `json:"qty_precision"`     // decimal places for quantity
	MinNotional   float64 `json:"min_notional"`    // minimum order value
	Status        string  `json:"status"`          // TRADING, BREAK, DELISTED
	Exchange      string  `json:"exchange"`

	// Extended fields for advanced filters
	AgeDays         int     `json:"age_days"`         // 上市天数
	MarketCap       float64 `json:"market_cap"`       // 市值
	BidDepth        float64 `json:"bid_depth"`        // 买盘深度
	AskDepth        float64 `json:"ask_depth"`        // 卖盘深度
	FundingRate     float64 `json:"funding_rate"`     // 资金费率
	VolumeChange    float64 `json:"volume_change"`    // 成交量变化率
	PriceChange     float64 `json:"price_change"`     // 价格变化率
	Correlation     float64 `json:"correlation"`      // 与BTC的相关性
	HasPosition     bool    `json:"has_position"`     // 是否已有持仓
	RecentReturn    float64 `json:"recent_return"`    // 近期收益率
	PriceRangeRatio float64 `json:"price_range_ratio"` // 价格区间比率 (high-low)/low
}

// IProducer generates an initial whitelist of trading pairs.
type IProducer interface {
	Name() string
	// Generate returns a list of pair symbols to consider.
	// The volume producer might return top-100 by volume, etc.
	Generate(exchange string, quoteAsset string) ([]string, error)
}

// IFilter filters or transforms a whitelist.
type IFilter interface {
	Name() string
	// Filter takes a list of pair symbols and returns the filtered subset.
	// It may need pair info (price, spread, etc.) to make decisions.
	Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error)
}

// ProducerFunc wraps a function as an IProducer.
type ProducerFunc struct {
	name string
	fn   func(exchange string, quoteAsset string) ([]string, error)
}

func (p *ProducerFunc) Name() string { return p.name }
func (p *ProducerFunc) Generate(exchange string, quoteAsset string) ([]string, error) {
	return p.fn(exchange, quoteAsset)
}

// FilterFunc wraps a function as an IFilter.
type FilterFunc struct {
	name string
	fn   func(pairs []string, infoMap map[string]*PairInfo) ([]string, error)
}

func (f *FilterFunc) Name() string { return f.name }
func (f *FilterFunc) Filter(pairs []string, infoMap map[string]*PairInfo) ([]string, error) {
	return f.fn(pairs, infoMap)
}
