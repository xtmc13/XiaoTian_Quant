package strategy

import (
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// symbolShiftStrategy 模拟经典策略：出厂 symbol 默认 BTCUSDT，Start 才从
// params 应用真实交易对（MACD/EMA/RSI 等 13+ 个策略的这个模式曾导致实例
// 订阅错 K 线 topic，2026-10-04 MACD SOLUSDT 实证）。
type symbolShiftStrategy struct {
	BaseStrategy
	mu     sync.Mutex
	symbol string
	bars   []model.Bar
}

func (s *symbolShiftStrategy) Name() string   { return "sym_shift" }
func (s *symbolShiftStrategy) Symbol() string { return s.symbol }
func (s *symbolShiftStrategy) IsRunning() bool {
	return true
}
func (s *symbolShiftStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sym, ok := params["symbol"].(string); ok && sym != "" {
		s.symbol = sym
	}
	return nil
}
func (s *symbolShiftStrategy) Stop() error { return nil }
func (s *symbolShiftStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bars = append(s.bars, bar)
	return nil, nil
}
func (s *symbolShiftStrategy) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *symbolShiftStrategy) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *symbolShiftStrategy) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *symbolShiftStrategy) Params() map[string]any           { return nil }
func (s *symbolShiftStrategy) GetParameters() *ParamRegistry    { return nil }
func (s *symbolShiftStrategy) ValidateParams() error            { return nil }
func (s *symbolShiftStrategy) ApplyParams(map[string]any) error { return nil }
func (s *symbolShiftStrategy) ParamDefs() []map[string]any      { return nil }

// TestStartResubscribesWhenSymbolAppliedAtStart：注册时订阅默认 symbol 的
// topic，Start 应用 params 换成真实 symbol 后必须重订阅——否则策略永远收不到
// 自己交易对的 K 线（MACD SOLUSDT 订阅在 BTC topic 上实证）。
func TestStartResubscribesWhenSymbolAppliedAtStart(t *testing.T) {
	bus := event.NewEventBus(64, 1)
	eng := GetEngine(bus)
	s := &symbolShiftStrategy{symbol: "BTCUSDT"}
	if err := eng.Register(s); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start("sym_shift", map[string]any{"symbol": "SOLUSDT"}); err != nil {
		t.Fatal(err)
	}

	// BTC 的 K 线不应再送达（已退订旧 topic）。
	bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT",
		Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Time: time.Now().UnixMilli(), Close: 1}})
	// SOL 的 K 线必须送达（新 topic）。
	bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "SOLUSDT",
		Data: model.Bar{Symbol: "SOLUSDT", Interval: "15m", Time: time.Now().UnixMilli(), Close: 2}})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bars) != 1 || s.bars[0].Symbol != "SOLUSDT" {
		t.Fatalf("bars = %v, want 只收到 SOLUSDT 一根（重订阅失效）", s.bars)
	}
}
