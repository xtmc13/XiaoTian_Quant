package model

import "math"

// ── Tick ──

type Tick struct {
	Symbol    string  `json:"symbol"`
	Exchange  string  `json:"exchange"`
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
	Exchange  string       `json:"exchange"`
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

func (b *Bar) IsBullish() bool    { return b.Close > b.Open }
func (b *Bar) IsBearish() bool    { return b.Close < b.Open }
func (b *Bar) Body() float64      { return math.Abs(b.Close - b.Open) }
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
