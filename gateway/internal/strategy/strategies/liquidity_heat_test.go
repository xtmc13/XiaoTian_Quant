package strategies

import (
	"math"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// mkLHBar 构造一根测试 K 线（15m 步长）。
func mkLHBar(i int, o, h, l, c, v float64) model.Bar {
	return model.Bar{
		Symbol: "BTCUSDT",
		Time:   int64(i) * 900000,
		Open:   o, High: h, Low: l, Close: c, Volume: v,
	}
}

// startLH 以紧凑参数启动策略（小窗口便于构造确定性场景）。
func startLH(t *testing.T) *LiquidityHeatStrategy {
	t.Helper()
	s := NewLiquidityHeatStrategy()
	err := s.Start(map[string]any{
		"symbol": "BTCUSDT", "timeframe": "15m",
		"lookback_bars": 50, "bins": 20, "volume_len": 3, "atr_len": 2,
		"pivot_bars": 2, "min_pool_strength_pct": 30,
		"tp_min_pool_strength_pct": 15, "tp_fallback_pct": 0.02,
		"sl_buffer_atr": 0.5, "position_size": 100, "max_hold_bars": 10,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

// feedBaseline 喂 i0..i0+n-1 的平缓基准 K 线（低量，池子弱）。
func feedBaseline(s *LiquidityHeatStrategy, i0, n int) int {
	for i := 0; i < n; i++ {
		s.OnBar(mkLHBar(i0+i, 100, 100.5, 99.5, 100, 2), nil)
	}
	return i0 + n
}

// strongBuyPool 找到量最大的存活买方池（对照 spike bar 产生的强池）。
func strongBuyPool(s *LiquidityHeatStrategy) *lhPool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var best *lhPool
	for _, p := range s.pools {
		if p.isBuy && (best == nil || p.vol > best.vol) {
			best = p
		}
	}
	return best
}

// TestLiquidityHeatSweepReclaimEntry 扫单反包入场：强买方池被跌破后收回 → LONG。
func TestLiquidityHeatSweepReclaimEntry(t *testing.T) {
	s := startLH(t)
	next := feedBaseline(s, 0, 30)
	// 高量 pivot 低（低 99.3 为窗口最低，量 300）→ 生成强买方池。
	s.OnBar(mkLHBar(next, 100, 100.5, 99.3, 100, 300), nil)
	next++

	pool := strongBuyPool(s)
	if pool == nil {
		t.Fatal("no buy pool after spike bar")
	}
	_, pocVol := s.profilePOCLocked()
	if pocVol <= 0 {
		t.Fatal("empty profile")
	}
	if got := pool.vol / pocVol * 100; got < 30 {
		t.Fatalf("strong pool strength %.1f%% < 30%%", got)
	}

	// 扫反包 bar：跌破强池后收回（close 在池上方）。
	sig, _ := s.OnBar(mkLHBar(next, 100, 100.2, pool.price-0.5, 99.9, 2), nil)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatalf("expect LONG sweep-reclaim signal, got %v", sig)
	}
	if !strings.Contains(sig.Reason, "流动性扫单反包") {
		t.Fatalf("unexpected reason: %s", sig.Reason)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.inPosition {
		t.Fatal("inPosition should be true after entry")
	}
	if s.entryPoolPrice != pool.price {
		t.Fatalf("entryPoolPrice %.4f != pool %.4f", s.entryPoolPrice, pool.price)
	}
	if s.stopPrice >= pool.price {
		t.Fatalf("stop %.4f should be below swept pool %.4f", s.stopPrice, pool.price)
	}
	if s.targetPrice <= 99.9 {
		t.Fatalf("target %.4f should be above entry close", s.targetPrice)
	}
}

// TestLiquidityHeatPoolConsumedNoReentry 未收回（收盘留在池下方）→ 池被消耗，
// 之后即使再收回也不入场（一个池只打一次）。
func TestLiquidityHeatPoolConsumedNoReentry(t *testing.T) {
	s := startLH(t)
	next := feedBaseline(s, 0, 30)
	s.OnBar(mkLHBar(next, 100, 100.5, 99.3, 100, 300), nil)
	next++
	pool := strongBuyPool(s)

	// 跌破不收回：close 留在池下方 → 无信号，池被消耗。
	sig, _ := s.OnBar(mkLHBar(next, 95.5, 100.2, pool.price-0.5, pool.price-1, 2), nil)
	if sig != nil {
		t.Fatalf("expect no signal on un-reclaimed sweep, got %v", sig.Direction)
	}
	s.mu.RLock()
	for _, p := range s.pools {
		if p == pool {
			t.Fatal("swept pool should be consumed")
		}
	}
	s.mu.RUnlock()

	// 下一根收回池上方 → 池已死，不得入场。
	sig, _ = s.OnBar(mkLHBar(next+1, 95.5, 100.2, pool.price-1.5, 99.9, 2), nil)
	if sig != nil {
		t.Fatalf("dead pool must not trigger entry, got %v", sig.Direction)
	}
}

// TestLiquidityHeatWeakPoolFiltered 弱池（强度 <30% POC）被扫反包 → 过滤不入场。
func TestLiquidityHeatWeakPoolFiltered(t *testing.T) {
	s := startLH(t)
	next := feedBaseline(s, 0, 30)
	s.OnBar(mkLHBar(next, 100, 100.5, 99.3, 100, 300), nil)
	next++
	pool := strongBuyPool(s)

	// 弱池：基准 low 99.5 下方的买方池（量 6，远低于 POC）。
	s.mu.RLock()
	var weak *lhPool
	_, pocVol := s.profilePOCLocked()
	for _, p := range s.pools {
		if p.isBuy && p.vol < pocVol*0.30 && p != pool {
			weak = p
			break
		}
	}
	s.mu.RUnlock()
	if weak == nil {
		t.Skip("no weak buy pool in fixture")
	}
	if weak.price >= 99.5 || weak.price <= pool.price {
		t.Fatalf("fixture pool price %.4f unexpected", weak.price)
	}

	// 跌破弱池（99.5 下方）但远高于强池（95.33）→ 收回 → 强度过滤不放行。
	sig, _ := s.OnBar(mkLHBar(next, 100, 100.2, weak.price-0.2, 99.9, 2), nil)
	if sig != nil {
		t.Fatalf("weak pool must be filtered, got %v (%s)", sig.Direction, sig.Reason)
	}
	s.mu.RLock()
	if s.inPosition {
		t.Fatal("must not enter on weak pool")
	}
	s.mu.RUnlock()
}

// TestLiquidityHeatExits 三种离场：止损 / 止盈（或池止盈）/ 超时。
func TestLiquidityHeatExits(t *testing.T) {
	entry := func(t *testing.T) (*LiquidityHeatStrategy, *lhPool, int) {
		s := startLH(t)
		next := feedBaseline(s, 0, 30)
		s.OnBar(mkLHBar(next, 100, 100.5, 99.3, 100, 300), nil)
		next++
		pool := strongBuyPool(s)
		sig, _ := s.OnBar(mkLHBar(next, 100, 100.2, pool.price-0.5, 99.9, 2), nil)
		if sig == nil || sig.Direction != "LONG" {
			t.Fatalf("entry failed: %v", sig)
		}
		return s, pool, next + 1
	}

	t.Run("stop loss", func(t *testing.T) {
		s, _, next := entry(t)
		s.mu.RLock()
		stop := s.stopPrice
		s.mu.RUnlock()
		sig, _ := s.OnBar(mkLHBar(next, stop-0.5, stop+0.3, stop-1, stop-0.2, 2), nil)
		if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "止损") {
			t.Fatalf("expect SL CLOSE, got %v", sig)
		}
		s.mu.RLock()
		if s.inPosition {
			t.Fatal("position should reset after SL")
		}
		s.mu.RUnlock()
	})

	t.Run("take profit", func(t *testing.T) {
		s, _, next := entry(t)
		s.mu.RLock()
		tp := s.targetPrice
		s.mu.RUnlock()
		sig, _ := s.OnBar(mkLHBar(next, tp-0.3, tp+0.5, tp-0.5, tp+0.1, 2), nil)
		if sig == nil || sig.Direction != "CLOSE" {
			t.Fatalf("expect TP CLOSE, got %v", sig)
		}
		if !strings.Contains(sig.Reason, "止盈") && !strings.Contains(sig.Reason, "卖方池") {
			t.Fatalf("unexpected exit reason: %s", sig.Reason)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		s, _, next := entry(t)
		var sig *model.Signal
		// max_hold_bars=10：第 10 根持仓 K 线应超时离场。
		for i := 0; i < 10; i++ {
			bar := mkLHBar(next+i, 99.9, 100.1, 99.7, 99.9, 2)
			// 避免触发池止盈（high 打到上方卖池）：限制 high。
			s.mu.RLock()
			if s.targetIsPool && s.targetPrice < bar.High {
				bar.High = s.targetPrice - 0.1
			}
			s.mu.RUnlock()
			sig, _ = s.OnBar(bar, nil)
		}
		if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "超时") {
			t.Fatalf("expect timeout CLOSE, got %v", sig)
		}
	})
}

// TestLiquidityHeatCustomStake 资金折算与余额退化。
func TestLiquidityHeatCustomStake(t *testing.T) {
	s := startLH(t)
	if got := s.CustomStakeAmount(1000, nil); got != 100 {
		t.Fatalf("stake = %v, want 100", got)
	}
	if got := s.CustomStakeAmount(50, nil); got != 50 {
		t.Fatalf("stake degrade = %v, want 50", got)
	}
}

// startLHCRA 以 CRA 参数启动（现货语义：无止损，池触发补仓 + CRA 止盈）。
func startLHCRA(t *testing.T) *LiquidityHeatStrategy {
	t.Helper()
	s := NewLiquidityHeatStrategy()
	err := s.Start(map[string]any{
		"symbol": "BTCUSDT", "timeframe": "15m",
		"lookback_bars": 50, "bins": 20, "volume_len": 3, "atr_len": 2,
		"pivot_bars": 2, "min_pool_strength_pct": 30,
		"tp_min_pool_strength_pct": 15, "tp_fallback_pct": 0.02,
		"sl_buffer_atr": 0.5, "position_size": 100, "max_hold_bars": 10,
		// CRA 参数包（键存在即启用 CRA 模式）。
		"first_order_amount": 100, "order_count": 3, "enable_add_position": true,
		"add_positions": []map[string]any{
			{"order": 2, "multiplier": 1.5, "spread": 0.01, "callback": 0.01},
			{"order": 3, "multiplier": 2.0, "spread": 0.01, "callback": 0.01},
		},
		"tp_mode": "static", "take_profit_method": "full", "take_profit_ratio": 0.05,
		"profit_callback": 0,
	})
	if err != nil {
		t.Fatalf("start cra: %v", err)
	}
	if !s.craEnabled {
		t.Fatal("craEnabled should be true with CRA keys")
	}
	if s.craParams.StopLossRatio != 0 {
		t.Fatal("spot CRA mode must disable stop loss")
	}
	return s
}

func fillBuy(s *LiquidityHeatStrategy, price, qty float64) {
	s.OnOrderUpdate(model.OrderData{
		Status: model.StatusFilled, Side: model.SideBuy,
		AvgFillPrice: price, Filled: qty,
	}, nil)
}

// TestLiquidityHeatCRAFlow 入场→成交→跌破下一个池补仓（CRA 乘数量）→CRA 止盈。
func TestLiquidityHeatCRAFlow(t *testing.T) {
	s := startLHCRA(t)
	next := feedBaseline(s, 0, 30)
	s.OnBar(mkLHBar(next, 100, 100.5, 99.3, 100, 300), nil) // 强买方池
	next++
	pool := strongBuyPool(s)

	// 入场：扫反包强池。
	sig, _ := s.OnBar(mkLHBar(next, 100, 100.2, pool.price-0.5, 99.9, 2), nil)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatalf("entry failed: %v", sig)
	}
	fillBuy(s, 99.9, 1.0) // 首单成交 → CRA 入场
	if !s.craState.InPosition {
		t.Fatal("cra should be in position after first fill")
	}

	// 造一个更低的强买方池（放量 pivot 低 97.5）。
	s.OnBar(mkLHBar(next+1, 98.5, 99.0, 97.5, 98.2, 300), nil)
	next += 2

	// 扫反包那个更低的池 → CRA 补仓 #2，Qty=100×1.5/close。
	lowPool := strongBuyPool(s)
	if lowPool == nil || lowPool == pool {
		t.Fatal("expected a second strong buy pool below entry")
	}
	addBar := mkLHBar(next, 97.0, 98.5, lowPool.price-0.3, 98.0, 2)
	sig, _ = s.OnBar(addBar, nil)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatalf("expect CRA add signal, got %v", sig)
	}
	if !strings.Contains(sig.Reason, "补仓 #2") {
		t.Fatalf("expect add #2, reason: %s", sig.Reason)
	}
	wantQty := 100 * 1.5 / 98.0
	if math.Abs(sig.Qty-wantQty) > 1e-3 {
		t.Fatalf("add qty %.6f != first×1.5/close %.6f", sig.Qty, wantQty)
	}
	fillBuy(s, 98.0, sig.Qty)
	if s.craState.PositionCount != 2 {
		t.Fatalf("cra position count = %d, want 2", s.craState.PositionCount)
	}

	// CRA 静态止盈（full，+5% 均价）：均价上方 6% 收盘 → CLOSE。
	avg := s.craState.AvgEntryPrice
	tpBar := mkLHBar(next+1, avg*1.05, avg*1.065, avg*1.045, avg*1.06, 2)
	sig, _ = s.OnBar(tpBar, nil)
	if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "CRA 止盈") {
		t.Fatalf("expect CRA TP close, got %v", sig)
	}
	if s.inPosition || s.craState.InPosition {
		t.Fatal("position should be flat after TP")
	}
}

// TestLiquidityHeatCRANoStopLoss 现货 CRA 模式无止损：价格深跌不割肉，
// 持仓保持（等补仓/止盈/超时）。
func TestLiquidityHeatCRANoStopLoss(t *testing.T) {
	s := startLHCRA(t)
	next := feedBaseline(s, 0, 30)
	s.OnBar(mkLHBar(next, 100, 100.5, 99.3, 100, 300), nil)
	next++
	pool := strongBuyPool(s)
	sig, _ := s.OnBar(mkLHBar(next, 100, 100.2, pool.price-0.5, 99.9, 2), nil)
	if sig == nil {
		t.Fatal("entry failed")
	}
	fillBuy(s, 99.9, 1.0)

	// 暴跌 15%：无止损 → 不平仓（CRA 模式刻意缺省止损入口）。
	sig, _ = s.OnBar(mkLHBar(next+1, 90, 91, 84.5, 85.5, 2), nil)
	if sig != nil && sig.Direction == "CLOSE" {
		t.Fatalf("CRA spot mode must not stop-loss, got %v", sig.Reason)
	}
	if !s.craState.InPosition {
		t.Fatal("position must survive crash without stop loss")
	}
}
