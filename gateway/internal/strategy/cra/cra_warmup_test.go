package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── H4（2026-10-10）：暖机路径只养指标、不动状态 ──
//
// Warmupper 语义：早于策略本次启动时刻收盘的历史回补 K 线走 OnWarmupBar——
// 只维护指标序列（s.bars），不做 EnterPosition/PendingAddCount/任何状态迁移，
// 不发信号。修复前两条暖机路径（feeder 回补 100 根上总线 / handler 直接喂
// RecentClosedBars）都直调 OnBar：fresh 路径第一根历史 bar 就按陈旧价开仓
// 发单；共享供给路径信号被丢弃但状态已迁移（虚持仓 + PendingAddCount 虚挂
// 耗尽补仓阶梯）。

// 编译期契约：CRA 引擎实现 strategy.Warmupper。
var _ strategy.Warmupper = (*BaseCRAStrategy)(nil)

// warmupSpot 启动现货 CRA（补仓梯 order 1..2，3% 差价/0.3% 回调，便于构造
// "暖机窗口内本就满足补仓形态"的剧本）。
func warmupSpot(t *testing.T) *BaseCRAStrategy {
	t.Helper()
	s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"first_order_amount":  100,
		"tp_mode":             "static",
		"take_profit_ratio":   0.5,
		"profit_callback":     0,
		"enable_add_position": true,
		"order_count":         2,
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			map[string]any{"order": 2, "multiplier": 2, "spread": 0.06, "callback": 0.003},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

// TestCRAWarmupBarsNoSideEffects 暖机 bar 不产生订单/状态迁移：100 根下跌
// 回补（OnBar 下会开仓+挂补仓）后 InPosition=false、PendingAddCount=0、
// PendingAdd 未挂起；指标序列已养（bars_collected=100）；首根实时 bar 正常
// 开仓发信号。
func TestCRAWarmupBarsNoSideEffects(t *testing.T) {
	s := warmupSpot(t)
	// 100 根单调下跌（100→50.5）：OnBar 语义下第一根就开仓、随后逐级挂补仓。
	price := 100.0
	for i := 0; i < 100; i++ {
		price -= 0.5
		s.OnWarmupBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: price, High: price, Low: price, Time: int64(i)})
	}
	st := s.RuntimeStatus()
	if st["in_position"] != false {
		t.Fatalf("warmup must not enter position: %v", st)
	}
	if st["pending_add_count"] != 0 {
		t.Fatalf("warmup must not pend adds: %v", st["pending_add_count"])
	}
	if s.state.PendingAdd {
		t.Fatal("warmup must not arm the add callback state")
	}
	if _, ok := st["entry_price"]; ok {
		t.Fatalf("warmup must not anchor entry price: %v", st["entry_price"])
	}
	if st["bars_collected"] != 100 {
		t.Fatalf("indicator series not warmed: bars_collected = %v, want 100", st["bars_collected"])
	}

	// 首根实时 bar：立即正常开仓（指标已暖，不需再等窗口）。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 50, High: 50, Low: 50}, nil)
	if err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("first live bar after warmup must emit entry: sig=%+v err=%v", sig, err)
	}
	if s.state.EntryPrice != 50 {
		t.Fatalf("entry must anchor the live price 50, got %v", s.state.EntryPrice)
	}
}

// TestCRAWarmupIndicatorEquivalence 暖机后指标判定与连续运行等价：dual 合约
// 循环起点按 EMA20 选边——暖机 25 根阴跌后首根实时 bar 开空（序列未暖够
// 20 根的对照组按默认开多）。
func TestCRAWarmupIndicatorEquivalence(t *testing.T) {
	mk := func() *BaseCRAStrategy {
		s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol": "BTCUSDT", "market_type": "swap", "leverage": 5,
			"direction": "dual", "first_order_amount": 10,
			"tp_mode": "static", "take_profit_ratio": 0.5,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	s := mk()
	price := 100.0
	for i := 0; i < 25; i++ {
		price -= 1
		s.OnWarmupBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: price, High: price, Low: price, Time: int64(i)})
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 74, High: 74, Low: 74}, nil)
	if err != nil || sig == nil {
		t.Fatalf("live bar after warmup: sig=%v err=%v", sig, err)
	}
	if sig.Direction != "SHORT" {
		t.Fatalf("warmed EMA20 window must select SHORT on declining series, got %s", sig.Direction)
	}
	if s.state.EntryPrice != 74 {
		t.Fatalf("entry must anchor live price 74, got %v", s.state.EntryPrice)
	}

	// 对照：无暖机（指标窗口不足 20 根）dual 默认开多。
	fresh := mk()
	sig, err = fresh.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 74, High: 74, Low: 74}, nil)
	if err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("cold strategy must default LONG (EMA window <20): sig=%+v err=%v", sig, err)
	}
}

// TestCRAWarmupPreservesRestoredPosition 重启恢复 + 暖机：restored_fills 重建
// 的持仓在暖机窗口内原样不动——暖机 bar 走出"补仓差价达标+回调触发"形态也
// 不挂起、不补仓、不改均价/档位；暖机结束后实时 bar 恢复完整交易评估。
func TestCRAWarmupPreservesRestoredPosition(t *testing.T) {
	s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"first_order_amount":  100,
		"tp_mode":             "static",
		"take_profit_ratio":   0.5,
		"profit_callback":     0,
		"enable_add_position": true,
		"order_count":         2,
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			map[string]any{"order": 2, "multiplier": 2, "spread": 0.06, "callback": 0.003},
		},
		// 重启前持仓：1 BTC @100（首单）。
		"restored_position_qty":  1.0,
		"restored_position_vwap": 100.0,
		"restored_fills":         []any{map[string]any{"side": "BUY", "qty": 1.0, "price": 100.0}},
	}); err != nil {
		t.Fatalf("start with restore: %v", err)
	}
	if !s.state.InPosition || s.state.PositionCount != 1 {
		t.Fatalf("restored state: %+v", s.state)
	}

	// 暖机窗口内走出完整补仓形态（跌 4% 达标 → 创新低 → 反弹 1%+）：
	// OnBar 语义下必然挂起并触发补仓 #1；暖机语义下什么都不许发生。
	for _, px := range []float64{96, 95, 96.2, 94, 95.1} {
		s.OnWarmupBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: px, High: px, Low: px})
	}
	st := s.state
	if st.PendingAddCount != 0 || st.PendingAdd {
		t.Fatalf("warmup must not pend/arm adds: pending=%d armed=%v", st.PendingAddCount, st.PendingAdd)
	}
	if st.PositionCount != 1 || st.TotalQty != 1 || st.AvgEntryPrice != 100 {
		t.Fatalf("restored position mutated by warmup: %+v", st)
	}
	if len(st.Lots) != 1 {
		t.Fatalf("warmup must not append lots: %+v", st.Lots)
	}

	// 暖机结束：实时 bar 上的同形态正常触发补仓 #1（交易能力不受暖机影响）。
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 96, High: 96, Low: 96}, nil); sig != nil {
		t.Fatalf("arm bar: unexpected signal %+v", sig)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 96.3, High: 96.3, Low: 96}, nil)
	if err != nil || sig == nil {
		t.Fatalf("live add after warmup: sig=%v err=%v", sig, err)
	}
	if sig.Reason != "cra add position #1" {
		t.Fatalf("live add reason = %q, want 'cra add position #1'", sig.Reason)
	}
}
