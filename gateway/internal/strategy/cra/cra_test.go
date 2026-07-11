package cra

import (
	"encoding/json"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestParseCRAParamsDefaults(t *testing.T) {
	p, err := ParseCRAParams("{}")
	if err != nil {
		t.Fatalf("parse empty config: %v", err)
	}
	if p.FirstOrderAmount != 100 {
		t.Errorf("default first_order_amount = %v, want 100", p.FirstOrderAmount)
	}
	if p.TradeCountMode != "cycle" {
		t.Errorf("default trade_count_mode = %v, want cycle", p.TradeCountMode)
	}
	if len(p.AddPositions) != p.OrderCount {
		t.Errorf("filled add_positions len %d != order_count %d", len(p.AddPositions), p.OrderCount)
	}
}

func TestParseCRAParamsFull(t *testing.T) {
	raw := map[string]any{
		"first_order_amount":      50.0,
		"first_order_multiplier":  2.0,
		"trade_count_mode":        "single",
		"loop_count":              10.0,
		"enable_add_position":     true,
		"order_count":             3.0,
		"add_positions": []map[string]any{
			{"order": 1, "multiplier": 1, "spread": 0.05, "callback": 0.005},
			{"order": 2, "multiplier": 2, "spread": 0.08, "callback": 0.01},
			{"order": 3, "multiplier": 4, "spread": 0.10, "callback": 0.01},
		},
		"take_profit_method": "head_tail",
		"tp_mode":            "moving",
		"moving_take_profit_tiers": []map[string]any{
			{"ratio": 0.02, "drawback": 0.20},
			{"ratio": 0.03, "drawback": 0.20},
			{"ratio": 0.04, "drawback": 0.10},
			{"ratio": 0.05, "drawback": 0.10},
		},
		"leverage":      10.0,
		"direction":     "dual",
		"market_type":   "swap",
		"position_side": "BOTH",
	}
	b, _ := json.Marshal(raw)
	p, err := ParseCRAParams(string(b))
	if err != nil {
		t.Fatalf("parse full config: %v", err)
	}
	if p.FirstOrderAmount != 50 {
		t.Errorf("first_order_amount = %v", p.FirstOrderAmount)
	}
	if p.TakeProfitMethod != "head_tail" {
		t.Errorf("take_profit_method = %v", p.TakeProfitMethod)
	}
	if len(p.MovingTakeProfitTiers) != 4 {
		t.Errorf("moving tp tiers = %d", len(p.MovingTakeProfitTiers))
	}
	if p.Leverage != 10 {
		t.Errorf("leverage = %v", p.Leverage)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("validate failed: %v", err)
	}
}

func TestCRAStateAddPositionLong(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	cfg := &AddPositionItem{Order: 1, Multiplier: 1, Spread: 0.05, Callback: 0.005}
	// Price drop 3% is not enough.
	if st.ShouldAddPosition(97, cfg) {
		t.Error("should not add at 3% drop")
	}
	// Price drop 6% triggers pending.
	if !st.ShouldAddPosition(94, cfg) {
		t.Error("should enter pending add at 6% drop")
	}
	// Bounce 0.5% from trigger low 94 -> 94.47 triggers add.
	if !st.ShouldAddPosition(94.47, cfg) {
		t.Error("should add after bounce")
	}
	if st.PendingAdd {
		t.Error("pending should be cleared after add trigger")
	}
}

func TestCRAStateMovingTakeProfit(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	tiers := []*MovingTPTier{
		{Ratio: 0.02, Drawback: 0.20},
		{Ratio: 0.10, Drawback: 0.10},
	}
	// Profit 1% not enough.
	if st.CheckMovingTakeProfit(101, tiers) {
		t.Error("tp should not trigger at 1% profit")
	}
	// Peak at 120 (20% profit) triggers tier 2, drawback tolerance 10%.
	st.UpdateExtremes(120)
	if st.CheckMovingTakeProfit(120, tiers) {
		t.Error("tp should not trigger at peak")
	}
	// Drawback 5% from 120 -> 114, below 10%.
	if st.CheckMovingTakeProfit(114, tiers) {
		t.Error("tp should not trigger with 5% drawback")
	}
	// Drawback 11% from 120 -> 106.8.
	if !st.CheckMovingTakeProfit(106.8, tiers) {
		t.Error("tp should trigger with 11% drawback")
	}
}

func TestCRAStateStopLoss(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	p := &CRAParams{
		StopLossEnabled: true,
		StopLossType:    "ratio",
		StopLossRatio:   0.10,
	}
	if st.CheckStopLoss(95, p) {
		t.Error("5% loss should not trigger 10% SL")
	}
	if !st.CheckStopLoss(89, p) {
		t.Error("11% loss should trigger 10% SL")
	}
}

func TestEMA(t *testing.T) {
	closes := []float64{10, 11, 12, 11, 10, 9, 10, 11, 12, 13}
	ema := EMA(closes, 5)
	if len(ema) != len(closes) {
		t.Fatalf("ema length %d != %d", len(ema), len(closes))
	}
	if ema[len(ema)-1] <= ema[0] {
		t.Error("last ema should be greater than first for rising trend")
	}
}

func TestMACDBullish(t *testing.T) {
	bars := make([]model.Bar, 40)
	for i := range bars {
		bars[i] = model.Bar{Close: float64(100 + i)}
	}
	// Strong uptrend should not produce a fresh bullish cross; just assert no panic.
	_ = MACDBullish(bars)
}

func TestBaseCRAStrategyFirstOrder(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	params := DefaultSpot("martin_trend")
	b, _ := json.Marshal(params)
	if err := s.Start(map[string]any{
		"symbol":       "BTCUSDT",
		"strategy_type": "martin_trend",
		"config_json":  string(b),
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 50000, High: 50100, Low: 49900}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig == nil {
		t.Fatal("expected first order signal")
	}
	if sig.Direction != "LONG" {
		t.Errorf("direction = %v, want LONG", sig.Direction)
	}
	if sig.Qty <= 0 {
		t.Errorf("qty = %v, want >0", sig.Qty)
	}
}

func TestBaseCRAStrategyLoopLimit(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	p := DefaultSpot("martin_trend")
	p.TradeCountMode = "single"
	b, _ := json.Marshal(p)
	_ = s.Start(map[string]any{"symbol": "BTCUSDT", "strategy_type": "martin_trend", "config_json": string(b)})

	// First loop entry + close.
	_, _ = s.OnBar(model.Bar{Close: 100, High: 100, Low: 100}, nil)
	_, _ = s.OnOrderUpdate(model.OrderData{Symbol: "BTCUSDT", Side: model.SideBuy, Filled: 1, AvgFillPrice: 100, Status: model.StatusFilled}, nil)
	// Reach take profit.
	for price := 100.0; price <= 110; price += 0.5 {
		_, _ = s.OnBar(model.Bar{Close: price, High: price, Low: price}, nil)
	}
	// After close, no more signals for single mode.
	sig, _ := s.OnBar(model.Bar{Close: 100, High: 100, Low: 100}, nil)
	if sig != nil {
		t.Error("single mode should not start second loop")
	}
}
