package strategies

import (
	"testing"
)

func TestBreakoutStrategyParams(t *testing.T) {
	s := NewBreakoutStrategy()

	// 1. 检查参数注册表不为空
	reg := s.GetParameters()
	if reg == nil {
		t.Fatal("GetParameters() returned nil")
	}
	if reg.Count() != 5 {
		t.Fatalf("expected 5 parameters, got %d", reg.Count())
	}

	// 2. 检查默认值
	p := reg.Get("lookback")
	if p == nil {
		t.Fatal("lookback parameter not found")
	}
	if p.GetInt() != 20 {
		t.Fatalf("lookback default expected 20, got %d", p.GetInt())
	}

	// 3. 检查参数范围验证
	err := reg.Set("lookback", 200)
	if err == nil {
		t.Fatal("expected error for lookback=200 (out of range 5-100)")
	}

	// 4. 检查合法值设置
	err = reg.Set("lookback", 50)
	if err != nil {
		t.Fatalf("unexpected error setting lookback=50: %v", err)
	}
	if reg.Get("lookback").GetInt() != 50 {
		t.Fatalf("lookback expected 50, got %d", reg.Get("lookback").GetInt())
	}

	// 5. 检查 ApplyParams 同步到本地字段
	err = s.ApplyParams(map[string]any{
		"lookback":        30,
		"buffer_pct":      0.005,
		"stop_loss_pct":   0.03,
		"take_profit_pct": 0.06,
		"position_size":   1000,
	})
	if err != nil {
		t.Fatalf("ApplyParams failed: %v", err)
	}
	if s.lookback != 30 {
		t.Fatalf("s.lookback expected 30, got %d", s.lookback)
	}
	if s.bufferPct != 0.005 {
		t.Fatalf("s.bufferPct expected 0.005, got %f", s.bufferPct)
	}

	// 6. 检查 ParamDefs 返回前端定义
	defs := s.ParamDefs()
	if len(defs) != 5 {
		t.Fatalf("expected 5 param defs, got %d", len(defs))
	}

	// 7. 检查 optimizable 参数
	opt := reg.Optimizable()
	if len(opt) != 5 {
		t.Fatalf("expected 5 optimizable params, got %d", len(opt))
	}

	t.Logf("BreakoutStrategy parameter system OK: %d params registered", reg.Count())
}

func TestBreakoutStrategyParamValidation(t *testing.T) {
	s := NewBreakoutStrategy()

	// 测试非法类型
	err := s.ApplyParams(map[string]any{
		"lookback": "not_a_number",
	})
	if err == nil {
		t.Fatal("expected error for non-numeric lookback")
	}

	// 测试越界值
	err = s.ApplyParams(map[string]any{
		"stop_loss_pct": 0.50,
	})
	if err == nil {
		t.Fatal("expected error for stop_loss_pct=0.50 (out of range)")
	}

	t.Log("BreakoutStrategy validation OK")
}
