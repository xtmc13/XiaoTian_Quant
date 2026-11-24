package adapter

// ExchangeCapabilities is the single source of truth for what the gateway
// can actually do on each venue. `Status` is "active" only when an adapter
// is wired into the order dispatch; `Certified` is stricter and means the
// venue has been exercised against a live/testnet account for the core
// spot+futures path. Do not add a venue to Status=active without an adapter.
type ExchangeCapabilities struct {
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	AssetClass      string   `json:"asset_class"`
	Status          string   `json:"status"`
	Supports        []string `json:"supports"`
	WebSocket       bool     `json:"websocket"`
	Transfer        bool     `json:"transfer"`
	Leverage        bool     `json:"leverage"`
	SetMarginMode   bool     `json:"set_margin_mode"`
	SetPositionMode bool     `json:"set_position_mode"`
	OrderTypes      []string `json:"order_types"`
	Certified       bool     `json:"certified"`
}

var exchangeCapabilities = []ExchangeCapabilities{
	{
		Key: "binance", Label: "Binance", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot", "futures", "margin"}, WebSocket: true,
		Transfer: true, Leverage: true, OrderTypes: []string{"LIMIT", "MARKET"}, Certified: true,
	},
	{
		Key: "okx", Label: "OKX", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot", "futures", "margin"}, WebSocket: true,
		// OKX carries lever per order; there is no account-level leverage call.
		OrderTypes: []string{"LIMIT", "MARKET"}, Certified: true,
	},
	{
		Key: "bybit", Label: "Bybit", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot", "futures"}, WebSocket: true,
		Leverage: true, OrderTypes: []string{"LIMIT", "MARKET"}, Certified: true,
	},
	{
		Key: "gate", Label: "Gate.io", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "mexc", Label: "MEXC", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "bitget", Label: "Bitget", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "coinbase", Label: "Coinbase", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "kraken", Label: "Kraken", AssetClass: "crypto", Status: "active",
		Supports: []string{"spot"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "kucoin", Label: "KuCoin", AssetClass: "crypto", Status: "stub",
		Supports: []string{"spot", "futures"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "htx", Label: "HTX", AssetClass: "crypto", Status: "stub",
		Supports: []string{"spot"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "phemex", Label: "Phemex", AssetClass: "crypto", Status: "stub",
		Supports: []string{"spot", "futures"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
	{
		Key: "deribit", Label: "Deribit", AssetClass: "crypto", Status: "stub",
		Supports: []string{"futures"}, OrderTypes: []string{"LIMIT", "MARKET"},
	},
}

// ExchangeCapabilityList returns a copy so callers cannot mutate the registry.
func ExchangeCapabilityList() []ExchangeCapabilities {
	out := make([]ExchangeCapabilities, len(exchangeCapabilities))
	copy(out, exchangeCapabilities)
	for i := range out {
		out[i].Supports = append([]string(nil), exchangeCapabilities[i].Supports...)
		out[i].OrderTypes = append([]string(nil), exchangeCapabilities[i].OrderTypes...)
	}
	return out
}

// ExchangeCapability looks up one venue; unknown venues report zero value
// with Certified=false so callers treat them as unsupported.
func ExchangeCapability(key string) (ExchangeCapabilities, bool) {
	for _, c := range exchangeCapabilities {
		if c.Key == normalizeExchangeName(key) {
			return c, true
		}
	}
	return ExchangeCapabilities{}, false
}
