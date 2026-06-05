package strategy

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// mockStrategy is a test double that always returns a fixed signal.
type mockStrategy struct {
	BaseStrategy
	name      string
	direction string
	strength  float64
	running   bool
}

func (m *mockStrategy) Name() string   { return m.name }
func (m *mockStrategy) Symbol() string { return "BTCUSDT" }
func (m *mockStrategy) Params() map[string]any {
	return map[string]any{"name": m.name}
}
func (m *mockStrategy) Start(params map[string]any) error {
	m.running = true
	return nil
}
func (m *mockStrategy) Stop() error {
	m.running = false
	return nil
}
func (m *mockStrategy) IsRunning() bool { return m.running }
func (m *mockStrategy) OnTick(_ model.Tick, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (m *mockStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (m *mockStrategy) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (m *mockStrategy) OnBar(_ model.Bar, _ *event.EventBus) (*model.Signal, error) {
	if m.direction == "" {
		return nil, nil
	}
	return &model.Signal{
		Symbol:    "BTCUSDT",
		Direction: m.direction,
		Strength:  m.strength,
		Strategy:  m.name,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

func setupMockFactory(name, direction string, strength float64) {
	RegisterStrategyFactory(name, func() Strategy {
		return &mockStrategy{name: name, direction: direction, strength: strength}
	})
}

func TestVoteAggregation(t *testing.T) {
	setupMockFactory("mock_long", "LONG", 0.8)
	setupMockFactory("mock_short", "SHORT", 0.7)

	cfg := &ComboConfig{
		ID:              "test-vote",
		Name:            "Vote Combo",
		Symbol:          "BTCUSDT",
		AggregationMode: "vote",
		Members: []ComboMember{
			{StrategyName: "mock_long", Weight: 0.5, Enabled: true},
			{StrategyName: "mock_long", Weight: 0.3, Enabled: true},
			{StrategyName: "mock_short", Weight: 0.2, Enabled: true},
		},
	}

	combo, err := NewStrategyCombo(cfg)
	if err != nil {
		t.Fatalf("create combo: %v", err)
	}
	if err := combo.Start(nil); err != nil {
		t.Fatalf("start combo: %v", err)
	}

	bar := model.Bar{Symbol: "BTCUSDT", Close: 100}
	sig, err := combo.OnBar(bar, nil)
	if err != nil {
		t.Fatalf("onbar error: %v", err)
	}
	if sig == nil {
		t.Fatal("expected signal, got nil")
	}
	if sig.Direction != "LONG" {
		t.Errorf("expected LONG, got %s", sig.Direction)
	}
	// Average strength of the two LONG signals: (0.8+0.8)/2 = 0.8
	if sig.Strength < 0.79 || sig.Strength > 0.81 {
		t.Errorf("expected strength ~0.8, got %f", sig.Strength)
	}
}

func TestWeightedAggregation(t *testing.T) {
	setupMockFactory("mock_w1", "LONG", 0.8)
	setupMockFactory("mock_w2", "LONG", 0.6)

	cfg := &ComboConfig{
		ID:              "test-weighted",
		Name:            "Weighted Combo",
		Symbol:          "BTCUSDT",
		AggregationMode: "weighted",
		Members: []ComboMember{
			{StrategyName: "mock_w1", Weight: 0.6, Enabled: true},
			{StrategyName: "mock_w2", Weight: 0.4, Enabled: true},
		},
	}

	combo, err := NewStrategyCombo(cfg)
	if err != nil {
		t.Fatalf("create combo: %v", err)
	}
	if err := combo.Start(nil); err != nil {
		t.Fatalf("start combo: %v", err)
	}

	bar := model.Bar{Symbol: "BTCUSDT", Close: 100}
	sig, err := combo.OnBar(bar, nil)
	if err != nil {
		t.Fatalf("onbar error: %v", err)
	}
	if sig == nil {
		t.Fatal("expected signal, got nil")
	}
	if sig.Direction != "LONG" {
		t.Errorf("expected LONG, got %s", sig.Direction)
	}
	// weighted avg = (0.8*0.6 + 0.6*0.4) / (0.6+0.4) = 0.72
	if sig.Strength < 0.70 || sig.Strength > 0.74 {
		t.Errorf("expected strength ~0.72, got %f", sig.Strength)
	}
}

func TestUnanimousAggregation(t *testing.T) {
	setupMockFactory("mock_u1", "LONG", 0.9)
	setupMockFactory("mock_u2", "LONG", 0.8)

	cfg := &ComboConfig{
		ID:              "test-unanimous",
		Name:            "Unanimous Combo",
		Symbol:          "BTCUSDT",
		AggregationMode: "unanimous",
		Members: []ComboMember{
			{StrategyName: "mock_u1", Weight: 0.5, Enabled: true},
			{StrategyName: "mock_u2", Weight: 0.5, Enabled: true},
		},
	}

	combo, err := NewStrategyCombo(cfg)
	if err != nil {
		t.Fatalf("create combo: %v", err)
	}
	if err := combo.Start(nil); err != nil {
		t.Fatalf("start combo: %v", err)
	}

	bar := model.Bar{Symbol: "BTCUSDT", Close: 100}
	sig, err := combo.OnBar(bar, nil)
	if err != nil {
		t.Fatalf("onbar error: %v", err)
	}
	if sig == nil {
		t.Fatal("expected signal, got nil")
	}
	if sig.Direction != "LONG" {
		t.Errorf("expected LONG, got %s", sig.Direction)
	}
	// Average strength: (0.9+0.8)/2 = 0.85
	if sig.Strength < 0.84 || sig.Strength > 0.86 {
		t.Errorf("expected strength ~0.85, got %f", sig.Strength)
	}
}

func TestUnanimousDisagreement(t *testing.T) {
	setupMockFactory("mock_d1", "LONG", 0.9)
	setupMockFactory("mock_d2", "SHORT", 0.8)

	cfg := &ComboConfig{
		ID:              "test-unanimous-disagree",
		Name:            "Unanimous Disagree",
		Symbol:          "BTCUSDT",
		AggregationMode: "unanimous",
		Members: []ComboMember{
			{StrategyName: "mock_d1", Weight: 0.5, Enabled: true},
			{StrategyName: "mock_d2", Weight: 0.5, Enabled: true},
		},
	}

	combo, err := NewStrategyCombo(cfg)
	if err != nil {
		t.Fatalf("create combo: %v", err)
	}
	if err := combo.Start(nil); err != nil {
		t.Fatalf("start combo: %v", err)
	}

	bar := model.Bar{Symbol: "BTCUSDT", Close: 100}
	sig, err := combo.OnBar(bar, nil)
	if err != nil {
		t.Fatalf("onbar error: %v", err)
	}
	if sig != nil {
		t.Fatalf("expected nil signal when members disagree, got %+v", sig)
	}
}

func TestSignalStrengthCalculation(t *testing.T) {
	setupMockFactory("mock_s1", "LONG", 1.0)
	setupMockFactory("mock_s2", "LONG", 0.5)
	setupMockFactory("mock_s3", "LONG", 0.0)

	cfg := &ComboConfig{
		ID:              "test-strength",
		Name:            "Strength Combo",
		Symbol:          "BTCUSDT",
		AggregationMode: "vote",
		Members: []ComboMember{
			{StrategyName: "mock_s1", Weight: 0.33, Enabled: true},
			{StrategyName: "mock_s2", Weight: 0.33, Enabled: true},
			{StrategyName: "mock_s3", Weight: 0.34, Enabled: true},
		},
	}

	combo, err := NewStrategyCombo(cfg)
	if err != nil {
		t.Fatalf("create combo: %v", err)
	}
	if err := combo.Start(nil); err != nil {
		t.Fatalf("start combo: %v", err)
	}

	bar := model.Bar{Symbol: "BTCUSDT", Close: 100}
	sig, err := combo.OnBar(bar, nil)
	if err != nil {
		t.Fatalf("onbar error: %v", err)
	}
	if sig == nil {
		t.Fatal("expected signal, got nil")
	}
	// Average of all three LONG strengths: (1.0+0.5+0.0)/3 = 0.5
	if sig.Strength < 0.49 || sig.Strength > 0.51 {
		t.Errorf("expected strength ~0.5, got %f", sig.Strength)
	}
}
