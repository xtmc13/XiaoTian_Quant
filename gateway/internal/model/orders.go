package model

// ── Order enums ──

type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

type PositionSide string

const (
	PositionLong  PositionSide = "LONG"
	PositionShort PositionSide = "SHORT"
)

type MarketType string

const (
	MarketSpot   MarketType = "spot"
	MarketSwap   MarketType = "swap"
	MarketMargin MarketType = "margin"
)

type MarginMode string

const (
	MarginCross    MarginMode = "cross"
	MarginIsolated MarginMode = "isolated"
)

type OrderType string

const (
	TypeMarket          OrderType = "MARKET"
	TypeLimit           OrderType = "LIMIT"
	TypeStopLoss        OrderType = "STOP_LOSS"
	TypeTakeProfit      OrderType = "TAKE_PROFIT"
	TypeStopLossLimit   OrderType = "STOP_LOSS_LIMIT"
	TypeTakeProfitLimit OrderType = "TAKE_PROFIT_LIMIT"
	TypeTrailingStop    OrderType = "TRAILING_STOP"
	TypeOCO             OrderType = "OCO"
	TypeBracket         OrderType = "BRACKET"
	TypeIceberg         OrderType = "ICEBERG"
	TypeTWAP            OrderType = "TWAP"
	TypeVWAP            OrderType = "VWAP"
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

// ── OrderData ──

type OrderData struct {
	ID            string      `json:"id"`
	Symbol        string      `json:"symbol"`
	Side          OrderSide   `json:"side"`
	OrderType     OrderType   `json:"order_type"`
	Price         float64     `json:"price"`
	StopPrice     float64     `json:"stop_price,omitempty"`
	Quantity      float64     `json:"quantity"`
	Filled        float64     `json:"filled"`
	Status        OrderStatus `json:"status"`
	Exchange      string      `json:"exchange"`
	UserID        uint64      `json:"user_id"`
	ClientOID     string      `json:"client_oid,omitempty"`
	AvgFillPrice  float64     `json:"avg_fill_price,omitempty"`
	CreatedAt     int64       `json:"created_at"`
	UpdatedAt     int64       `json:"updated_at"`

	// ── Contract fields ──
	MarketType    MarketType   `json:"market_type,omitempty"`   // spot | swap | margin
	PositionSide  PositionSide `json:"position_side,omitempty"` // LONG | SHORT
	Leverage      float64      `json:"leverage,omitempty"`
	MarginMode    MarginMode   `json:"margin_mode,omitempty"` // cross | isolated
	TPPrice       float64      `json:"tp_price,omitempty"`
	SLPrice       float64      `json:"sl_price,omitempty"`
	ClosePosition bool         `json:"close_position,omitempty"`
	RealizedPnL   float64      `json:"realized_pnl,omitempty"`
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
