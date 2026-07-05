package model

// ── Balance ──

type Balance struct {
	Currency string  `json:"currency"`
	Total    float64 `json:"total"`
	Free     float64 `json:"free"`
	Used     float64 `json:"used"`
}

// ── Position ──

type PositionData struct {
	ID             string       `json:"id"`
	Symbol         string       `json:"symbol"`
	Side           string       `json:"side"`
	Quantity       float64      `json:"quantity"`
	AvgEntryPrice  float64      `json:"avg_entry_price"`
	CurrentPrice   float64      `json:"current_price"`
	UnrealizedPnL  float64      `json:"unrealized_pnl"`
	RealizedPnL    float64      `json:"realized_pnl"`
	CostBasis      float64      `json:"cost_basis"`
	OpenedAt       int64        `json:"opened_at"`
	Exchange       string       `json:"exchange"`

	// ── Contract fields ──
	PositionSide     PositionSide `json:"position_side,omitempty"` // LONG | SHORT
	Leverage         float64      `json:"leverage,omitempty"`
	MarginMode       MarginMode   `json:"margin_mode,omitempty"` // cross | isolated
	Margin           float64      `json:"margin,omitempty"`      // 占用保证金
	LiquidationPrice float64      `json:"liquidation_price,omitempty"`
	MarketType       MarketType   `json:"market_type,omitempty"`
}

func (p *PositionData) PnLPct() float64 {
	if p.CostBasis == 0 {
		return 0
	}
	return p.UnrealizedPnL / p.CostBasis * 100
}

// ── Account ──

type AccountData struct {
	ID        string                   `json:"id"`
	Exchange  string                   `json:"exchange"`
	Balances  map[string]*Balance      `json:"balances"`
	Positions map[string]*PositionData `json:"positions"`
	CreatedAt int64                    `json:"created_at"`
}

// ── Portfolio Snapshot ──

type PortfolioSnapshot struct {
	TotalEquity      float64         `json:"total_equity"`
	AvailableBalance float64         `json:"available_balance"`
	MarginUsed       float64         `json:"margin_used"`
	Drawdown         float64         `json:"drawdown"`
	NetExposure      float64         `json:"net_exposure"`
	Positions        []*PositionData `json:"positions"`
	Balances         []*Balance      `json:"balances"`
	Timestamp        int64           `json:"timestamp"`
}
