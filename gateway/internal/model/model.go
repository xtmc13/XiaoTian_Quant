package model

import "math"

// ── Tick ──

type Tick struct {
	Symbol    string  `json:"symbol"`
	Bid       float64 `json:"bid"`
	Ask       float64 `json:"ask"`
	BidSize   float64 `json:"bid_size"`
	AskSize   float64 `json:"ask_size"`
	Last      float64 `json:"last"`
	Volume    float64 `json:"volume"`
	Timestamp int64   `json:"timestamp"`
}

// ── OrderBook ──

type OrderBookData struct {
	Symbol    string       `json:"symbol"`
	Bids      [][2]float64 `json:"bids"`
	Asks      [][2]float64 `json:"asks"`
	Timestamp int64        `json:"timestamp"`
}

func (ob *OrderBookData) MidPrice() float64 {
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return 0
	}
	return (ob.Bids[0][0] + ob.Asks[0][0]) / 2
}

func (ob *OrderBookData) Spread() float64 {
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return 0
	}
	return ob.Asks[0][0] - ob.Bids[0][0]
}

func (ob *OrderBookData) SpreadBps() float64 {
	mid := ob.MidPrice()
	if mid == 0 {
		return 0
	}
	return ob.Spread() / mid * 10000
}

func (ob *OrderBookData) Imbalance(depth int) float64 {
	if depth <= 0 {
		depth = 5
	}
	if depth > len(ob.Bids) {
		depth = len(ob.Bids)
	}
	if depth > len(ob.Asks) {
		depth = len(ob.Asks)
	}
	if depth == 0 {
		return 0
	}
	var bidVol, askVol float64
	for i := 0; i < depth; i++ {
		bidVol += ob.Bids[i][1]
		askVol += ob.Asks[i][1]
	}
	total := bidVol + askVol
	if total == 0 {
		return 0
	}
	return (bidVol - askVol) / total
}

func (ob *OrderBookData) WeightedMid() float64 {
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return 0
	}
	bestBid, bestAsk := ob.Bids[0][0], ob.Asks[0][0]
	bidVol, askVol := ob.Bids[0][1], ob.Asks[0][1]
	total := bidVol + askVol
	if total == 0 {
		return (bestBid + bestAsk) / 2
	}
	return (bestBid*askVol + bestAsk*bidVol) / total
}

// ── Bar ──

type Bar struct {
	Symbol   string  `json:"symbol"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	Interval string  `json:"interval"`
	Time     int64   `json:"time"`
}

func (b *Bar) IsBullish() bool  { return b.Close > b.Open }
func (b *Bar) IsBearish() bool  { return b.Close < b.Open }
func (b *Bar) Body() float64    { return math.Abs(b.Close - b.Open) }
func (b *Bar) UpperWick() float64 { return b.High - math.Max(b.Open, b.Close) }
func (b *Bar) LowerWick() float64 { return math.Min(b.Open, b.Close) - b.Low }

// ── Trade ──

type TradeData struct {
	Symbol    string  `json:"symbol"`
	ID        string  `json:"id"`
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"`
	Side      string  `json:"side"`
	Timestamp int64   `json:"timestamp"`
}

// ── Order ──

type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

type OrderType string

const (
	TypeMarket           OrderType = "MARKET"
	TypeLimit            OrderType = "LIMIT"
	TypeStopLoss         OrderType = "STOP_LOSS"
	TypeTakeProfit       OrderType = "TAKE_PROFIT"
	TypeStopLossLimit    OrderType = "STOP_LOSS_LIMIT"
	TypeTakeProfitLimit  OrderType = "TAKE_PROFIT_LIMIT"
	TypeTrailingStop     OrderType = "TRAILING_STOP"
	TypeOCO              OrderType = "OCO"
	TypeBracket          OrderType = "BRACKET"
	TypeIceberg          OrderType = "ICEBERG"
	TypeTWAP             OrderType = "TWAP"
	TypeVWAP             OrderType = "VWAP"
)

type OrderStatus string

const (
	StatusCreated         OrderStatus = "CREATED"
	StatusPending         OrderStatus = "PENDING"
	StatusNew             OrderStatus = "NEW"
	StatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	StatusFilled          OrderStatus = "FILLED"
	StatusCancelled       OrderStatus = "CANCELLED"
	StatusRejected        OrderStatus = "REJECTED"
	StatusExpired         OrderStatus = "EXPIRED"
)

// StatusTransitions maps each status to valid next statuses.
var StatusTransitions = map[OrderStatus][]OrderStatus{
	StatusCreated:         {StatusPending, StatusRejected},
	StatusPending:         {StatusNew, StatusRejected},
	StatusNew:             {StatusPartiallyFilled, StatusFilled, StatusCancelled, StatusExpired},
	StatusPartiallyFilled: {StatusPartiallyFilled, StatusFilled, StatusCancelled, StatusExpired},
	StatusFilled:          {},
	StatusCancelled:       {},
	StatusRejected:        {},
	StatusExpired:         {},
}

func ValidStatusTransition(from, to OrderStatus) bool {
	for _, valid := range StatusTransitions[from] {
		if valid == to {
			return true
		}
	}
	return false
}

type OrderData struct {
	ID           string      `json:"id"`
	Symbol       string      `json:"symbol"`
	Side         OrderSide   `json:"side"`
	OrderType    OrderType   `json:"order_type"`
	Price        float64     `json:"price"`
	StopPrice    float64     `json:"stop_price,omitempty"`
	Quantity     float64     `json:"quantity"`
	Filled       float64     `json:"filled"`
	Status       OrderStatus `json:"status"`
	Exchange     string      `json:"exchange"`
	UserID       uint64      `json:"user_id"`
	ClientOID    string      `json:"client_oid,omitempty"`
	AvgFillPrice float64     `json:"avg_fill_price,omitempty"`
	CreatedAt    int64       `json:"created_at"`
	UpdatedAt    int64       `json:"updated_at"`
}

func (o *OrderData) Remaining() float64 {
	return o.Quantity - o.Filled
}

func (o *OrderData) IsDone() bool {
	return o.Status == StatusFilled || o.Status == StatusCancelled ||
		o.Status == StatusRejected || o.Status == StatusExpired
}

func (o *OrderData) IsActive() bool {
	return o.Status == StatusNew || o.Status == StatusPartiallyFilled || o.Status == StatusPending
}

// ── Balance ──

type Balance struct {
	Currency string  `json:"currency"`
	Total    float64 `json:"total"`
	Free     float64 `json:"free"`
	Used     float64 `json:"used"`
}

// ── Position ──

type PositionData struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Quantity       float64 `json:"quantity"`
	AvgEntryPrice  float64 `json:"avg_entry_price"`
	CurrentPrice   float64 `json:"current_price"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	RealizedPnL    float64 `json:"realized_pnl"`
	CostBasis      float64 `json:"cost_basis"`
	OpenedAt       int64   `json:"opened_at"`
	Exchange       string  `json:"exchange"`
}

func (p *PositionData) PnLPct() float64 {
	if p.CostBasis == 0 {
		return 0
	}
	return p.UnrealizedPnL / p.CostBasis * 100
}

// ── Signal ──

type Signal struct {
	Symbol    string  `json:"symbol"`
	Direction string  `json:"direction"` // LONG, SHORT, CLOSE
	Strength  float64 `json:"strength"`  // 0.0 - 1.0
	Strategy  string  `json:"strategy"`
	Reason    string  `json:"reason"`
	Timestamp int64   `json:"timestamp"`
}

// ── Risk Alert ──

type RiskAlert struct {
	Level     string `json:"level"` // INFO, WARN, CRITICAL
	CheckName string `json:"check_name"`
	Message   string `json:"message"`
	Symbol    string `json:"symbol,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ── Account ──

type AccountData struct {
	ID        string              `json:"id"`
	Exchange  string              `json:"exchange"`
	Balances  map[string]*Balance `json:"balances"`
	Positions map[string]*PositionData `json:"positions"`
	CreatedAt int64               `json:"created_at"`
}

// ── Portfolio Snapshot ──

type PortfolioSnapshot struct {
	TotalEquity      float64           `json:"total_equity"`
	AvailableBalance float64           `json:"available_balance"`
	MarginUsed       float64           `json:"margin_used"`
	Drawdown         float64           `json:"drawdown"`
	NetExposure      float64           `json:"net_exposure"`
	Positions        []*PositionData   `json:"positions"`
	Balances         []*Balance        `json:"balances"`
	Timestamp        int64             `json:"timestamp"`
}
