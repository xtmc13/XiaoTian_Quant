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
