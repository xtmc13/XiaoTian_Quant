package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 布林带开仓门槛（币富"布林带策略"：指标策略快捷卡/选择器写入 open_bollinger_*）──

// bollingerRecoverLongSeries 22 根：20 根走平 100 → 95（跌破下轨）→ 99（收回轨内）。
func bollingerRecoverLongSeries() []float64 {
	closes := make([]float64, 0, 22)
	for i := 0; i < 20; i++ {
		closes = append(closes, 100)
	}
	return append(closes, 95, 99)
}

func TestBollingerConfirmed(t *testing.T) {
	// 做多：跌破下轨后收回轨内（假跌破反转）。
	rec := makeBars(bollingerRecoverLongSeries()...)
	if !BollingerConfirmed(rec, BollingerTunables{}, SideLong) {
		t.Error("bollinger should confirm long when price recovers back inside the lower band")
	}
	if BollingerConfirmed(rec, BollingerTunables{}, SideShort) {
		t.Error("bollinger should reject short on the same recover series")
	}

	// 做多：收盘仍低于下轨但出现向上拐点（与跌破收回是两条独立路径）。
	dip := makeBars(append(func() []float64 {
		c := make([]float64, 0, 22)
		for i := 0; i < 20; i++ {
			c = append(c, 100)
		}
		return append(c, 90, 91)
	}())...)
	if !BollingerConfirmed(dip, BollingerTunables{}, SideLong) {
		t.Error("bollinger should confirm long when close stays below lower band with an upward inflection")
	}

	// 不触发：序列走平收于轨内（既无跌破收回也无轨外拐点）。
	flat := makeBars(func() []float64 {
		c := make([]float64, 0, 25)
		for i := 0; i < 25; i++ {
			c = append(c, 100)
		}
		return c
	}()...)
	if BollingerConfirmed(flat, BollingerTunables{}, SideLong) {
		t.Error("bollinger must not confirm long on a flat in-band series")
	}
	if BollingerConfirmed(flat, BollingerTunables{}, SideShort) {
		t.Error("bollinger must not confirm short on a flat in-band series")
	}

	// 做空镜像：突破上轨后收回轨内。
	spike := makeBars(append(func() []float64 {
		c := make([]float64, 0, 22)
		for i := 0; i < 20; i++ {
			c = append(c, 100)
		}
		return append(c, 105, 101)
	}())...)
	if !BollingerConfirmed(spike, BollingerTunables{}, SideShort) {
		t.Error("bollinger should confirm short when price falls back inside the upper band")
	}
	if BollingerConfirmed(spike, BollingerTunables{}, SideLong) {
		t.Error("bollinger should reject long on the same spike series")
	}

	// 序列不足（< period+1）：不确认、不 panic。
	short := makeBars(100, 100, 100, 100, 100, 80, 92)
	if BollingerConfirmed(short, BollingerTunables{}, SideLong) {
		t.Error("bollinger must not confirm when series shorter than period+1")
	}
	// 参数化生效①：同一收回序列 period=22 时窗口不足（len 22 < period+1）→ 不确认。
	if BollingerConfirmed(rec, BollingerTunables{Period: 22, Std: 2}, SideLong) {
		t.Error("period=22 must not confirm on a 22-bar series (insufficient window)")
	}
	// 参数化生效②：std 加宽带也随之加宽——双根浅跌收回序列在 std=2 跌破
	// 下轨成立（18 根走平 100 → 90,90 → 95），std=3 时恰好压在下轨上不确认。
	twoBarDip := makeBars(append(func() []float64 {
		c := make([]float64, 0, 21)
		for i := 0; i < 18; i++ {
			c = append(c, 100)
		}
		return append(c, 90, 90, 95)
	}())...)
	if !BollingerConfirmed(twoBarDip, BollingerTunables{Period: 20, Std: 2}, SideLong) {
		t.Error("std=2 should confirm long on the two-bar dip recover series")
	}
	if BollingerConfirmed(twoBarDip, BollingerTunables{Period: 20, Std: 3}, SideLong) {
		t.Error("std=3 widens the band; the same series must not confirm")
	}
}

func TestBollingerBandsStdScaling(t *testing.T) {
	// 带宽随 std 倍数单调放大（参数确实参与计算），中轨=SMA。
	closes := []float64{100, 102, 98, 101, 99, 100, 103, 97, 100, 100}
	up2, mid, low2 := BollingerBands(closes, 5, 2)
	up3, _, low3 := BollingerBands(closes, 5, 3)
	if len(up2) != 6 || len(up3) != 6 {
		t.Fatalf("band length = %d/%d, want 6", len(up2), len(up3))
	}
	last := len(up2) - 1
	if !(up3[last] > up2[last] && low3[last] < low2[last]) {
		t.Errorf("std=3 bands should be wider than std=2: up %v vs %v, low %v vs %v", up3[last], up2[last], low3[last], low2[last])
	}
	// 中轨 = 末 5 根 SMA。
	wantMid := (100.0 + 103 + 97 + 100 + 100) / 5
	if mid[last] != wantMid {
		t.Errorf("mid band = %v, want SMA5 %v", mid[last], wantMid)
	}
	// 非法参数返回 nil（门槛函数回退默认，不直接用零值）。
	if u, _, _ := BollingerBands(closes, 0, 2); u != nil {
		t.Error("period<1 must return nil bands")
	}
}

func TestParseCRAParamsBollinger(t *testing.T) {
	// 缺省：关闭、period=close、参数 20/2（零行为变化）。
	p, err := ParseCRAParams("{}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.OpenBollingerEnabled || p.OpenBollingerPeriod != "close" {
		t.Errorf("default bollinger gate = enabled:%v period:%q, want false/close", p.OpenBollingerEnabled, p.OpenBollingerPeriod)
	}
	if p.BollingerPeriod != 20 || p.BollingerStd != 2 {
		t.Errorf("default bollinger tunables = %d/%v, want 20/2", p.BollingerPeriod, p.BollingerStd)
	}

	// 选择器落库：open_bollinger_* 直接键 + indicator_params.bollinger 参数；
	// 监测周期从 timeframe 子键回退（period 被布林周期占用为数值型）。
	p2, err := ParseCRAParams(`{
		"open_bollinger_enabled": true,
		"indicator_params": {"bollinger": {"period": 30, "std": 2.5, "timeframe": "1h"}}
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !p2.OpenBollingerEnabled || p2.BollingerPeriod != 30 || p2.BollingerStd != 2.5 {
		t.Errorf("bollinger parse = enabled:%v %d/%v, want true 30/2.5", p2.OpenBollingerEnabled, p2.BollingerPeriod, p2.BollingerStd)
	}
	if p2.OpenBollingerPeriod != "1h" {
		t.Errorf("bollinger monitor period backfill = %q, want 1h", p2.OpenBollingerPeriod)
	}

	// 显式 open_bollinger_period 优先于子键回退。
	p3, err := ParseCRAParams(`{
		"open_bollinger_enabled": true,
		"open_bollinger_period": "30m",
		"indicator_params": {"bollinger": {"period": 20, "std": 2, "timeframe": "1h"}}
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p3.OpenBollingerPeriod != "30m" {
		t.Errorf("explicit open_bollinger_period = %q, want 30m (no backfill override)", p3.OpenBollingerPeriod)
	}

	// 非法值维持 ValidateIndicatorParams 的拒绝语义（与 macd/ema_cross 同纪律）。
	if _, err := ParseCRAParams(`{"indicator_params": {"bollinger": {"period": -3}}}`); err == nil {
		t.Error("expected parse error for negative bollinger.period")
	}
	if _, err := ParseCRAParams(`{"indicator_params": {"bollinger": {"std": 0}}}`); err == nil {
		t.Error("expected parse error for zero bollinger.std")
	}

	// 绕过解析的构造路径（DefaultContract 等直接建结构体）：零值参数由
	// 引擎 withDefaults 回退 20/2，与显式默认结果一致。
	series := makeBars(bollingerRecoverLongSeries()...)
	if BollingerConfirmed(series, BollingerTunables{}, SideLong) != BollingerConfirmed(series, BollingerTunables{Period: 20, Std: 2}, SideLong) {
		t.Error("zero bollinger tunables must fall back to 20/2")
	}
}

// TestCRAOpenBollingerGate 端到端开仓门槛：启用时满足才开仓；默认关闭回归
// （首根 K 线即开仓，零行为变化）。
func TestCRAOpenBollingerGate(t *testing.T) {
	feed := func(t *testing.T, s *BaseCRAStrategy, closes []float64) *model.Signal {
		t.Helper()
		var sig *model.Signal
		for _, c := range closes {
			r, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: c, High: c, Low: c}, nil)
			if err != nil {
				t.Fatalf("onbar: %v", err)
			}
			if r != nil {
				sig = r
			}
		}
		return sig
	}
	start := func(t *testing.T, extra map[string]any) *BaseCRAStrategy {
		t.Helper()
		cfg := map[string]any{
			"symbol":    "BTCUSDT",
			"timeframe": "15m",
			"direction": "long",
			"tp_mode":   "static",
		}
		for k, v := range extra {
			cfg[k] = v
		}
		s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
		if err := s.Start(cfg); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	flat25 := func() []float64 {
		c := make([]float64, 0, 25)
		for i := 0; i < 25; i++ {
			c = append(c, 100)
		}
		return c
	}

	// 启用 + 满足（跌破下轨收回）：开仓。
	s := start(t, map[string]any{"open_bollinger_enabled": true})
	if sig := feed(t, s, bollingerRecoverLongSeries()); sig == nil {
		t.Error("bollinger gate should allow long entry on lower-band recover")
	} else if sig.Direction != "LONG" {
		t.Errorf("direction = %v, want LONG", sig.Direction)
	}

	// 启用 + 不满足（走平轨内）：全程不开仓。
	s2 := start(t, map[string]any{"open_bollinger_enabled": true})
	if sig := feed(t, s2, flat25()); sig != nil {
		t.Errorf("bollinger gate should block entry on flat in-band series, got %+v", sig)
	}

	// 默认关闭：首根即开仓（回归零行为变化）。
	s3 := start(t, map[string]any{})
	if sig := feed(t, s3, []float64{100}); sig == nil {
		t.Error("default (bollinger disabled) must open on first bar as before")
	}

	// 做空方向镜像：突破上轨收回 → 开空。
	s4 := start(t, map[string]any{"open_bollinger_enabled": true, "direction": "short"})
	shortSeries := append(flat25()[:20], 105, 101)
	if sig := feed(t, s4, shortSeries); sig == nil {
		t.Error("bollinger gate should allow short entry on upper-band recover")
	} else if sig.Direction != "SHORT" {
		t.Errorf("direction = %v, want SHORT", sig.Direction)
	}
}

// TestCRAOpenBollingerMultiTimeframe 多周期取数复用 indicatorBars：工作周期
// 走平（不满足），1h 供给给跌破收回序列 → 以 1h 序列开仓；Timeframes 声明订阅。
func TestCRAOpenBollingerMultiTimeframe(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"timeframe":              "15m",
		"direction":              "long",
		"tp_mode":                "static",
		"open_bollinger_enabled": true,
		"open_bollinger_period":  "1h",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	declared := false
	for _, tf := range s.Timeframes() {
		if tf == "1h" {
			declared = true
		}
	}
	if !declared {
		t.Errorf("Timeframes = %v, want 1h subscribed for bollinger gate", s.Timeframes())
	}
	s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
		"BTCUSDT|1h": makeBars(bollingerRecoverLongSeries()...),
	}})

	work := make([]float64, 25)
	for i := range work {
		work[i] = 100
	}
	var sig *model.Signal
	for _, c := range work {
		r, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: c, High: c, Low: c}, nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if r != nil {
			sig = r
		}
	}
	if sig == nil {
		t.Fatal("expected first order confirmed by 1h bollinger feed (working-TF bars are flat)")
	}
}
