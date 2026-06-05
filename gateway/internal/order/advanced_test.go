package order

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestOCOOrderValidate(t *testing.T) {
	// Valid BUY OCO
	oco := &OCOOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideBuy,
		Quantity:   1.0,
		LimitPrice: 65000,
		StopPrice:  70000,
	}
	if err := oco.Validate(); err != nil {
		t.Errorf("valid BUY OCO failed: %v", err)
	}

	// Valid SELL OCO
	oco2 := &OCOOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideSell,
		Quantity:   1.0,
		LimitPrice: 75000,
		StopPrice:  70000,
	}
	if err := oco2.Validate(); err != nil {
		t.Errorf("valid SELL OCO failed: %v", err)
	}

	// Invalid: BUY with limit > stop
	oco3 := &OCOOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideBuy,
		Quantity:   1.0,
		LimitPrice: 75000,
		StopPrice:  70000,
	}
	if err := oco3.Validate(); err == nil {
		t.Error("expected error for invalid BUY OCO")
	}
}

func TestBracketOrderValidate(t *testing.T) {
	// Valid BUY bracket
	b := &BracketOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideBuy,
		Quantity:   1.0,
		EntryPrice: 68000,
		TakeProfit: 75000,
		StopLoss:   65000,
		EntryType:  model.TypeLimit,
	}
	if err := b.Validate(); err != nil {
		t.Errorf("valid bracket failed: %v", err)
	}

	// Invalid: TP below entry for BUY
	b2 := &BracketOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideBuy,
		Quantity:   1.0,
		EntryPrice: 68000,
		TakeProfit: 65000,
		StopLoss:   70000,
		EntryType:  model.TypeLimit,
	}
	if err := b2.Validate(); err == nil {
		t.Error("expected error for invalid bracket")
	}
}

func TestIcebergOrderValidate(t *testing.T) {
	i := &IcebergOrder{
		Symbol:          "BTCUSDT",
		Side:            model.SideBuy,
		TotalQuantity:   10.0,
		VisibleQuantity: 1.0,
		Price:           68000,
		Variance:        0.2,
	}
	if err := i.Validate(); err != nil {
		t.Errorf("valid iceberg failed: %v", err)
	}

	// Invalid: visible >= total
	i2 := &IcebergOrder{
		Symbol:          "BTCUSDT",
		TotalQuantity:   10.0,
		VisibleQuantity: 10.0,
		Price:           68000,
	}
	if err := i2.Validate(); err == nil {
		t.Error("expected error for visible >= total")
	}
}

func TestIcebergNextSlice(t *testing.T) {
	i := &IcebergOrder{
		TotalQuantity:   10.0,
		VisibleQuantity: 2.0,
		Variance:        0,
	}

	s1 := i.NextSlice()
	if s1 != 2.0 {
		t.Errorf("expected slice 2.0, got %f", s1)
	}

	i.FilledTotal = 2.0
	s2 := i.NextSlice()
	if s2 != 2.0 {
		t.Errorf("expected slice 2.0, got %f", s2)
	}

	i.FilledTotal = 9.0
	s3 := i.NextSlice()
	if s3 != 1.0 {
		t.Errorf("expected slice 1.0 (remaining), got %f", s3)
	}

	i.FilledTotal = 10.0
	s4 := i.NextSlice()
	if s4 != 0 {
		t.Errorf("expected slice 0 (complete), got %f", s4)
	}
}

func TestIcebergIsComplete(t *testing.T) {
	i := &IcebergOrder{TotalQuantity: 10.0, FilledTotal: 5.0}
	if i.IsComplete() {
		t.Error("expected not complete")
	}
	i.FilledTotal = 10.0
	if !i.IsComplete() {
		t.Error("expected complete")
	}
}

func TestAdvancedManagerPlaceOCO(t *testing.T) {
	am := NewAdvancedManager()
	am.PlaceOrder = func(req *Request) (*model.OrderData, error) {
		return &model.OrderData{ID: "test-" + string(req.OrderType)}, nil
	}
	am.CancelOrder = func(string) error { return nil }

	oco := &OCOOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideBuy,
		Quantity:   1.0,
		LimitPrice: 65000,
		StopPrice:  70000,
	}
	result, err := am.PlaceOCO(oco)
	if err != nil {
		t.Fatalf("place OCO failed: %v", err)
	}
	if result.ID == "" {
		t.Error("expected OCO ID")
	}
	if result.LimitOrderID == "" {
		t.Error("expected limit order ID")
	}
	if result.StopOrderID == "" {
		t.Error("expected stop order ID")
	}
}

func TestAdvancedManagerCancelOCO(t *testing.T) {
	am := NewAdvancedManager()
	am.PlaceOrder = func(req *Request) (*model.OrderData, error) {
		return &model.OrderData{ID: "test-" + string(req.OrderType)}, nil
	}
	am.CancelOrder = func(string) error { return nil }

	oco := &OCOOrder{Symbol: "BTCUSDT", Side: model.SideBuy, Quantity: 1, LimitPrice: 65000, StopPrice: 70000}
	result, _ := am.PlaceOCO(oco)

	err := am.CancelOCO(result.ID)
	if err != nil {
		t.Errorf("cancel failed: %v", err)
	}

	if am.GetOCO(result.ID).Status != "CANCELLED" {
		t.Error("expected CANCELLED status")
	}
}

func TestAdvancedManagerPlaceBracket(t *testing.T) {
	am := NewAdvancedManager()
	am.PlaceOrder = func(req *Request) (*model.OrderData, error) {
		return &model.OrderData{ID: "test-" + string(req.OrderType)}, nil
	}

	bracket := &BracketOrder{
		Symbol:     "BTCUSDT",
		Side:       model.SideBuy,
		Quantity:   1.0,
		EntryPrice: 68000,
		TakeProfit: 75000,
		StopLoss:   65000,
		EntryType:  model.TypeLimit,
	}
	result, err := am.PlaceBracket(bracket)
	if err != nil {
		t.Fatalf("place bracket failed: %v", err)
	}
	if result.ID == "" {
		t.Error("expected bracket ID")
	}
	if result.Status != "PENDING_ENTRY" {
		t.Errorf("expected PENDING_ENTRY, got %s", result.Status)
	}
}

func TestAdvancedManagerPlaceIceberg(t *testing.T) {
	am := NewAdvancedManager()
	am.PlaceOrder = func(req *Request) (*model.OrderData, error) {
		return &model.OrderData{ID: "test-" + string(req.OrderType)}, nil
	}

	iceberg := &IcebergOrder{
		Symbol:          "BTCUSDT",
		Side:            model.SideBuy,
		TotalQuantity:   10.0,
		VisibleQuantity: 2.0,
		Price:           68000,
		Variance:        0,
	}
	result, err := am.PlaceIceberg(iceberg)
	if err != nil {
		t.Fatalf("place iceberg failed: %v", err)
	}
	if result.ID == "" {
		t.Error("expected iceberg ID")
	}
	if result.Status != "ACTIVE" {
		t.Errorf("expected ACTIVE, got %s", result.Status)
	}
}

func TestCalculateBracketPrices(t *testing.T) {
	tp, sl := CalculateBracketPrices(100, model.SideBuy, 0.05, 0.10)
	if math.Abs(tp-110) > 0.001 {
		t.Errorf("expected TP ~110, got %f", tp)
	}
	if math.Abs(sl-95) > 0.001 {
		t.Errorf("expected SL ~95, got %f", sl)
	}

	tp2, sl2 := CalculateBracketPrices(100, model.SideSell, 0.05, 0.10)
	if math.Abs(tp2-90) > 0.001 {
		t.Errorf("expected TP ~90, got %f", tp2)
	}
	if math.Abs(sl2-105) > 0.001 {
		t.Errorf("expected SL ~105, got %f", sl2)
	}
}

func TestCalculatePositionSize(t *testing.T) {
	qty := CalculatePositionSize(10000, 0.01, 100, 95)
	if qty <= 0 {
		t.Errorf("expected positive quantity, got %f", qty)
	}
	// Risk = $100, stop distance = $5, qty = 20
	if qty != 20 {
		t.Errorf("expected qty 20, got %f", qty)
	}
}

func TestAdvancedManagerListActive(t *testing.T) {
	am := NewAdvancedManager()
	am.PlaceOrder = func(req *Request) (*model.OrderData, error) {
		return &model.OrderData{ID: "test"}, nil
	}
	am.CancelOrder = func(string) error { return nil }

	// Place and cancel an OCO
	oco := &OCOOrder{Symbol: "BTCUSDT", Side: model.SideBuy, Quantity: 1, LimitPrice: 65000, StopPrice: 70000}
	result, _ := am.PlaceOCO(oco)
	am.CancelOCO(result.ID)

	active := am.ListActiveOCO()
	if len(active) != 0 {
		t.Errorf("expected 0 active OCO, got %d", len(active))
	}
}
