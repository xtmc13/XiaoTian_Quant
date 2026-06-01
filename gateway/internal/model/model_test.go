package model

import (
	"math"
	"testing"
)

func TestOrderBookData_MidPrice(t *testing.T) {
	tests := []struct {
		name string
		ob   OrderBookData
		want float64
	}{
		{
			name: "normal case",
			ob:   OrderBookData{Bids: [][2]float64{{100, 1}}, Asks: [][2]float64{{102, 1}}},
			want: 101,
		},
		{
			name: "empty bids",
			ob:   OrderBookData{Bids: nil, Asks: [][2]float64{{102, 1}}},
			want: 0,
		},
		{
			name: "empty asks",
			ob:   OrderBookData{Bids: [][2]float64{{100, 1}}, Asks: nil},
			want: 0,
		},
		{
			name: "both empty",
			ob:   OrderBookData{},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ob.MidPrice(); got != tt.want {
				t.Errorf("MidPrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrderBookData_Spread(t *testing.T) {
	ob := OrderBookData{
		Bids: [][2]float64{{100, 1}},
		Asks: [][2]float64{{103, 1}},
	}
	if got := ob.Spread(); got != 3 {
		t.Errorf("Spread() = %v, want 3", got)
	}
}

func TestOrderBookData_SpreadBps(t *testing.T) {
	ob := OrderBookData{
		Bids: [][2]float64{{100, 1}},
		Asks: [][2]float64{{101, 1}},
	}
	// mid = 100.5, spread = 1, bps = 1/100.5 * 10000 ≈ 99.5
	want := 1.0 / 100.5 * 10000
	if got := ob.SpreadBps(); math.Abs(got-want) > 1e-9 {
		t.Errorf("SpreadBps() = %v, want %v", got, want)
	}
}

func TestOrderBookData_Imbalance(t *testing.T) {
	tests := []struct {
		name  string
		ob    OrderBookData
		depth int
		want  float64
	}{
		{
			name:  "balanced",
			ob:    OrderBookData{Bids: [][2]float64{{100, 1}, {99, 1}}, Asks: [][2]float64{{101, 1}, {102, 1}}},
			depth: 2,
			want:  0,
		},
		{
			name:  "bid heavy",
			ob:    OrderBookData{Bids: [][2]float64{{100, 3}}, Asks: [][2]float64{{101, 1}}},
			depth: 1,
			want:  0.5,
		},
		{
			name:  "ask heavy",
			ob:    OrderBookData{Bids: [][2]float64{{100, 1}}, Asks: [][2]float64{{101, 3}}},
			depth: 1,
			want:  -0.5,
		},
		{
			name:  "zero depth falls back",
			ob:    OrderBookData{Bids: [][2]float64{{100, 1}}, Asks: [][2]float64{{101, 1}}},
			depth: 0,
			want:  0,
		},
		{
			name:  "empty orderbook",
			ob:    OrderBookData{},
			depth: 5,
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ob.Imbalance(tt.depth); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Imbalance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrderBookData_WeightedMid(t *testing.T) {
	ob := OrderBookData{
		Bids: [][2]float64{{100, 2}},
		Asks: [][2]float64{{104, 1}},
	}
	// weighted = (100*1 + 104*2) / 3 = 102.666...
	want := (100.0*1 + 104.0*2) / 3.0
	if got := ob.WeightedMid(); math.Abs(got-want) > 1e-9 {
		t.Errorf("WeightedMid() = %v, want %v", got, want)
	}
}

func TestBar_CandlestickMethods(t *testing.T) {
	bull := Bar{Open: 100, High: 110, Low: 95, Close: 105}
	if !bull.IsBullish() {
		t.Error("expected bullish")
	}
	if bull.IsBearish() {
		t.Error("expected not bearish")
	}
	if bull.Body() != 5 {
		t.Errorf("Body() = %v, want 5", bull.Body())
	}
	if bull.UpperWick() != 5 {
		t.Errorf("UpperWick() = %v, want 5", bull.UpperWick())
	}
	if bull.LowerWick() != 5 {
		t.Errorf("LowerWick() = %v, want 5", bull.LowerWick())
	}

	bear := Bar{Open: 105, High: 110, Low: 95, Close: 100}
	if !bear.IsBearish() {
		t.Error("expected bearish")
	}
	if bear.Body() != 5 {
		t.Errorf("Body() = %v, want 5", bear.Body())
	}
	if bear.UpperWick() != 5 {
		t.Errorf("UpperWick() = %v, want 5", bear.UpperWick())
	}
	if bear.LowerWick() != 5 {
		t.Errorf("LowerWick() = %v, want 5", bear.LowerWick())
	}
}

func TestValidStatusTransition(t *testing.T) {
	tests := []struct {
		from OrderStatus
		to   OrderStatus
		want bool
	}{
		{StatusCreated, StatusPending, true},
		{StatusCreated, StatusNew, false},
		{StatusNew, StatusFilled, true},
		{StatusNew, StatusCancelled, true},
		{StatusFilled, StatusCancelled, false},
		{StatusPartiallyFilled, StatusPartiallyFilled, true},
		{StatusPartiallyFilled, StatusFilled, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"_"+string(tt.to), func(t *testing.T) {
			if got := ValidStatusTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("ValidStatusTransition(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestOrderData_Methods(t *testing.T) {
	o := OrderData{Quantity: 10, Filled: 3, Status: StatusNew}
	if o.Remaining() != 7 {
		t.Errorf("Remaining() = %v, want 7", o.Remaining())
	}
	if !o.IsActive() {
		t.Error("expected active")
	}
	if o.IsDone() {
		t.Error("expected not done")
	}

	o.Status = StatusFilled
	if o.IsActive() {
		t.Error("expected not active")
	}
	if !o.IsDone() {
		t.Error("expected done")
	}
}

func TestPositionData_PnLPct(t *testing.T) {
	p := PositionData{UnrealizedPnL: 500, CostBasis: 5000}
	if got := p.PnLPct(); math.Abs(got-10) > 1e-9 {
		t.Errorf("PnLPct() = %v, want 10", got)
	}
	p.CostBasis = 0
	if p.PnLPct() != 0 {
		t.Error("expected 0 when CostBasis is 0")
	}
}

func TestBalance(t *testing.T) {
	b := Balance{Currency: "USDT", Total: 1000, Free: 800, Used: 200}
	if b.Total != b.Free+b.Used {
		t.Error("total should equal free + used")
	}
}
