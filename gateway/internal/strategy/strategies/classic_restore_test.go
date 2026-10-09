package strategies

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// 重启仓位重建（2026-10-04）：MACD 重启后必须认领账本净持仓、按均价继续
// 管理出场——否则暖机重放里的金叉条件会再开一单（三次重启三次加仓实证）。
func TestMACDRestorePositionBlocksReentry(t *testing.T) {
	s := NewMACDStrategy()
	err := s.Start(map[string]any{
		"symbol": "SOLUSDT",
		// handler 按账本注入的重建参数（PositionRestorer 路径）。
		"restored_position_qty": 12.5, "restored_position_vwap": 119.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	if !s.inPosition || s.direction != "LONG" || s.entryPrice != 119.5 {
		t.Fatalf("restore failed: %+v", s)
	}
	s.mu.RUnlock()

	// 直接调接口同样生效（工具链路径）。
	if err := s.RestorePosition(3, 100); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	if s.entryPrice != 100 {
		t.Fatalf("RestorePosition entryPrice = %v, want 100", s.entryPrice)
	}
	s.mu.RUnlock()

	// 重建后在金叉形态的 bar 上不得再入场（inPosition 门拦截），且不报错。
	mk := func(close float64) model.Bar {
		return model.Bar{Symbol: "SOLUSDT", Time: time.Now().UnixMilli(), Close: close}
	}
	// 喂够指标窗口（slow+signal+5 根）。断言不得再开新仓（LONG/SHORT）；
	// CLOSE（止盈/止损）恰是重建后应有的出场管理，放行。
	base := 100.0
	for i := 0; i < 40; i++ {
		base *= 1.001
		if sig, _ := s.OnBar(mk(base), nil); sig != nil && sig.Direction != "CLOSE" {
			t.Fatalf("warmup bar %d must not re-enter after restore, got %v (%s)", i, sig.Direction, sig.Reason)
		}
	}
}

// H1 片（2026-10-09）：MACD 合约空单重启重建——账本净额<0 时 handler 注入
// restored_position_side=short，恢复为 SHORT 仓；出场管理对称：价格上涨
// 触发空单止损、下跌触发空单止盈；暖机重放不再重复开空。
func TestMACDRestoreShortPositionManagesExits(t *testing.T) {
	mk := func(close float64) model.Bar {
		return model.Bar{Symbol: "SOLUSDT", Time: time.Now().UnixMilli(), Close: close}
	}
	newRestoredShort := func(t *testing.T) *MACDStrategy {
		t.Helper()
		s := NewMACDStrategy()
		if err := s.Start(map[string]any{
			"symbol":                 "SOLUSDT",
			"restored_position_qty":  2.0,
			"restored_position_vwap": 100.0,
			"restored_position_side": "short",
		}); err != nil {
			t.Fatal(err)
		}
		s.mu.RLock()
		if !s.inPosition || s.direction != "SHORT" || s.entryPrice != 100 {
			t.Fatalf("short restore failed: inPos=%v dir=%s entry=%v", s.inPosition, s.direction, s.entryPrice)
		}
		s.mu.RUnlock()
		return s
	}
	// 暖机：40 根平价 bar 填满指标窗口（恢复仓只管理出场，不重复入场）。
	warm := func(t *testing.T, s *MACDStrategy) {
		t.Helper()
		for i := 0; i < 40; i++ {
			if sig, _ := s.OnBar(mk(100), nil); sig != nil {
				t.Fatalf("warmup bar %d: unexpected signal %+v（恢复态不得重复入场/误出场）", i, sig)
			}
		}
	}

	// 空单止损：成本 100，102.5 ≥ 100×(1+2%) → short stop loss。
	s := newRestoredShort(t)
	warm(t, s)
	sig, err := s.OnBar(mk(102.5), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || sig.Reason != "short stop loss" {
		t.Fatalf("short SL = %+v err=%v, want CLOSE short stop loss", sig, err)
	}
	s.mu.RLock()
	if s.inPosition {
		t.Fatal("SL 后必须复位为空仓")
	}
	s.mu.RUnlock()

	// 空单止盈：95.5 ≤ 100×(1-4%) → short take profit。
	s = newRestoredShort(t)
	warm(t, s)
	sig, err = s.OnBar(mk(95.5), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || sig.Reason != "short take profit" {
		t.Fatalf("short TP = %+v err=%v, want CLOSE short take profit", sig, err)
	}

	// 价格在上/下限度之间（100.5）：既不止盈也不止损。
	s = newRestoredShort(t)
	warm(t, s)
	if sig, _ := s.OnBar(mk(100.5), nil); sig != nil {
		t.Fatalf("mid price must not trigger exit: %+v", sig)
	}

	// RuntimeStatus 如实透出空单方向。
	rs := newRestoredShort(t).RuntimeStatus()
	if rs["in_position"] != true || rs["direction"] != "SHORT" || rs["entry_price"] != 100.0 {
		t.Fatalf("runtime status = %+v, want in_position/SHORT/100", rs)
	}
}
