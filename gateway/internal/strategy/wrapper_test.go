package strategy

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// fakeInner 只记录 OnOrderUpdate 是否被调用。
type fakeInner struct {
	BaseStrategy
	calls int
}

func (f *fakeInner) Name() string   { return "liquidity_heat" }
func (f *fakeInner) Symbol() string { return "BTCUSDT" }
func (f *fakeInner) Params() map[string]any {
	return map[string]any{}
}
func (f *fakeInner) Start(map[string]any) error { return nil }
func (f *fakeInner) Stop() error                { return nil }
func (f *fakeInner) IsRunning() bool            { return true }
func (f *fakeInner) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *fakeInner) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *fakeInner) OnBar(model.Bar, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *fakeInner) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	f.calls++
	return nil, nil
}

// NamedStrategy.OnOrderUpdate 归属过滤：signal 链路打标 client_oid
// "sig:<配置id>:<nonce>"——只把本策略的订单更新透传给内层策略，
// 防止同交易对其他策略的拒单/成交误触发本策略状态机（2026-10-01 虚持仓修复的精确性）。
func TestNamedStrategyOnOrderUpdateFiltersByClientOID(t *testing.T) {
	inner := &fakeInner{}
	w1 := WrapStrategy("cfg1", inner)

	// 其他策略的 sig 订单：必须被过滤
	if _, err := w1.OnOrderUpdate(model.OrderData{Symbol: "BTCUSDT", ClientOID: "sig:cfg2:1", Status: model.StatusRejected}, nil); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 0 {
		t.Fatalf("other strategy's order must be filtered, inner.calls=%d", inner.calls)
	}

	// 本策略的 sig 订单：透传
	if _, err := w1.OnOrderUpdate(model.OrderData{Symbol: "BTCUSDT", ClientOID: "sig:cfg1:99", Status: model.StatusRejected}, nil); err != nil {
		t.Fatal(err)
	}
	// 未打标的存量/手工单：保持旧行为透传
	if _, err := w1.OnOrderUpdate(model.OrderData{Symbol: "BTCUSDT", ClientOID: "", Status: model.StatusFilled}, nil); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("own + legacy orders must pass through, inner.calls=%d", inner.calls)
	}
}

// RestorePosition 透传：内层实现 PositionRestorer 时生效，未实现时 no-op。
func TestNamedStrategyRestorePositionPassthrough(t *testing.T) {
	restorer := &fakeRestorer{}
	w := WrapStrategy("cfg1", restorer)
	if err := w.RestorePosition(0.1, 50000); err != nil {
		t.Fatal(err)
	}
	if restorer.qty != 0.1 || restorer.avg != 50000 {
		t.Fatalf("passthrough not delivered: %+v", restorer)
	}
	// 未实现的内层：no-op 不panic
	w2 := WrapStrategy("cfg2", &fakeInner{})
	if err := w2.RestorePosition(0.1, 50000); err != nil {
		t.Fatalf("non-restorer must be no-op: %v", err)
	}
}

type fakeRestorer struct {
	fakeInner
	qty, avg float64
}

func (f *fakeRestorer) RestorePosition(qty, avgPrice float64) error {
	f.qty, f.avg = qty, avgPrice
	return nil
}
