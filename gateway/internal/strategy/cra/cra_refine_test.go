package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── helpers ──

// makeBars 由收盘价序列构造 bar 序列（旧→新），Interval/Time 按需另设。
func makeBars(closes ...float64) []model.Bar {
	bars := make([]model.Bar, len(closes))
	for i, c := range closes {
		bars[i] = model.Bar{Symbol: "BTCUSDT", Close: c, High: c, Low: c, Time: int64(i + 1)}
	}
	return bars
}

// fakeBarProvider 是 strategy.BarProvider 的测试替身（按 SYMBOL|tf 键取序列，
// 与 MarketData 键口径一致）。
type fakeBarProvider struct {
	series map[string][]model.Bar
}

func (f *fakeBarProvider) GetBar(symbol, tf string) (model.Bar, bool) {
	b := f.series[symbol+"|"+tf]
	if len(b) == 0 {
		return model.Bar{}, false
	}
	return b[len(b)-1], true
}

func (f *fakeBarProvider) GetSeries(symbol, tf string) []model.Bar {
	return f.series[symbol+"|"+tf]
}

// decliningThenCrossSeries 生成一条 43 根收盘序列：40 根阴跌（100 起每根 -0.4）
// 后接 3 根 +0.2 回升——默认 MACD(12,26,9) 在最后一根柱值由负变正（金叉），
// 而自定义快线组 (3,6,2) 在倒数第二根已金叉、最后一根不金叉（参数确实生效）。
func decliningThenCrossSeries() []float64 {
	closes := make([]float64, 0, 43)
	p := 100.0
	for i := 0; i < 40; i++ {
		closes = append(closes, p)
		p -= 0.4
	}
	for i := 0; i < 3; i++ {
		p += 0.2
		closes = append(closes, p)
	}
	return closes
}

// ── A1：首单挂单价格门槛（引擎侧 first_order_price，UI 输入框映射的键）──

func TestCRAFirstOrderPriceGate(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"first_order_price":  50000,
		"first_order_amount": 100,
		"tp_mode":            "static",
		"take_profit_ratio":  0.013,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 做多首选：价格 51000 ≥ 挂单 50000，尚未到位 → 不开仓。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 51000, High: 51000, Low: 51000}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig != nil {
		t.Fatalf("expected no signal above pending price, got %+v", sig)
	}
	// 价格回落到 49000 < 50000 → 到位，开首单。
	sig, err = s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 49000, High: 49000, Low: 49000}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig == nil {
		t.Fatal("expected first order signal once price reached pending price")
	}
	if sig.Direction != "LONG" {
		t.Errorf("direction = %v, want LONG", sig.Direction)
	}
}

func TestCRAFirstOrderPriceZeroIsMarket(t *testing.T) {
	// 向后兼容：first_order_price=0（默认）= 市价，首根 K 线即开仓。
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"first_order_amount": 100,
		"tp_mode":            "static",
		"take_profit_ratio":  0.013,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 51000, High: 51000, Low: 51000}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig == nil {
		t.Fatal("expected market first order signal with default first_order_price=0")
	}
}

// ── A2：指标周期多周期供给 ──

func TestCRAPrimaryTimeframe(t *testing.T) {
	// 未配置 timeframe（旧配置）：不声明主周期 → 引擎不过滤，行为与旧版一致。
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "tp_mode": "static"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if tf := s.PrimaryTimeframe(); tf != "" {
		t.Errorf("PrimaryTimeframe without timeframe param = %q, want \"\"", tf)
	}

	s2 := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s2.Start(map[string]any{"symbol": "BTCUSDT", "timeframe": "15m", "tp_mode": "static"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if tf := s2.PrimaryTimeframe(); tf != "15m" {
		t.Errorf("PrimaryTimeframe = %q, want 15m", tf)
	}
}

func TestCRATimeframesDeclaration(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                   "BTCUSDT",
		"timeframe":                "15m",
		"tp_mode":                  "static",
		"open_macd_enabled":        true,
		"open_macd_period":         "1h",
		"open_counter_ema_enabled": true,
		"open_counter_ema_period":  "30m",
		"open_trend_ema_enabled":   true,
		"open_trend_ema_period":    "15m", // 等于工作周期 → 不订阅
		"add_macd_enabled":         true,
		"add_macd_period":          "close", // 关闭 → 不订阅
		"add_ema_enabled":          true,
		"add_ema_period":           "8h",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	tfs := s.Timeframes()
	want := map[string]bool{"1h": true, "30m": true, "8h": true}
	if len(tfs) != len(want) {
		t.Fatalf("Timeframes = %v, want keys %v", tfs, want)
	}
	for _, tf := range tfs {
		if !want[tf] {
			t.Errorf("unexpected timeframe %q in %v", tf, tfs)
		}
	}

	// 工作周期未知（旧配置）：不声明副周期（副周期 bar 会混进 OnBar 污染状态，
	// 指标周期走 indicatorBars 降级路径）。
	s2 := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s2.Start(map[string]any{
		"symbol":            "BTCUSDT",
		"tp_mode":           "static",
		"open_macd_enabled": true,
		"open_macd_period":  "1h",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if tfs := s2.Timeframes(); len(tfs) != 0 {
		t.Errorf("Timeframes without working timeframe = %v, want empty", tfs)
	}

	// 指标全部关闭（默认）：无副周期。
	s3 := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s3.Start(map[string]any{"symbol": "BTCUSDT", "timeframe": "15m", "tp_mode": "static"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if tfs := s3.Timeframes(); len(tfs) != 0 {
		t.Errorf("Timeframes with all indicators off = %v, want empty", tfs)
	}
}

func TestCRAIndicatorBarsMultiTimeframe(t *testing.T) {
	// 工作周期 15m（40 根阴跌，MACD 无金叉）；open MACD 监测 1h，供给管给
	// 43 根"阴跌后回升"序列（默认 12/26/9 最后一根金叉）→ 应以 1h 序列开仓。
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":            "BTCUSDT",
		"timeframe":         "15m",
		"direction":         "long",
		"tp_mode":           "static",
		"open_macd_enabled": true,
		"open_macd_period":  "1h",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
		"BTCUSDT|1h": makeBars(decliningThenCrossSeries()...),
	}})

	work := make([]float64, 40)
	p := 100.0
	for i := range work {
		work[i] = p
		p -= 0.4
	}
	var sig *model.Signal
	for _, c := range work {
		b := model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: c, High: c, Low: c}
		r, err := s.OnBar(b, nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if r != nil {
			sig = r
		}
	}
	if sig == nil {
		t.Fatal("expected first order signal confirmed by 1h MACD feed")
	}
	if sig.Direction != "LONG" {
		t.Errorf("direction = %v, want LONG", sig.Direction)
	}
}

func TestCRAIndicatorBarsFallbackToWorkingTF(t *testing.T) {
	// 副周期供给数据不足（10 根 < macd 慢线26+信号9+2=37）→ 如实降级为工作
	// 周期 bar（阴跌序列 MACD 无金叉 → 不开仓，证明确实走了降级路径）。
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":            "BTCUSDT",
		"timeframe":         "15m",
		"direction":         "long",
		"tp_mode":           "static",
		"open_macd_enabled": true,
		"open_macd_period":  "4h",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
		"BTCUSDT|4h": makeBars(1, 2, 3, 4, 5, 6, 7, 8, 9, 10),
	}})

	work := make([]float64, 40)
	p := 100.0
	for i := range work {
		work[i] = p
		p -= 0.4
	}
	for _, c := range work {
		sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: c, High: c, Low: c}, nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if sig != nil {
			t.Fatalf("expected no signal when 4h feed insufficient (working-TF fallback has no MACD cross)")
		}
	}
}

func TestIsFeedablePeriod(t *testing.T) {
	for _, ok := range []string{"5m", "15m", "30m", "1h", "4h", "8h"} {
		if !IsFeedablePeriod(ok) {
			t.Errorf("IsFeedablePeriod(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "close", "1d", "7m", "abc"} {
		if IsFeedablePeriod(bad) {
			t.Errorf("IsFeedablePeriod(%q) = true, want false", bad)
		}
	}
}

// ── A3：指标 fast/slow/signal 参数接进引擎 ──

func TestParseCRAParamsIndicatorTunables(t *testing.T) {
	// 缺省（无 indicator_params）：维持历史硬编码 MACD 12/26/9、EMA 5/15。
	p, err := ParseCRAParams("{}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.MacdFast != 12 || p.MacdSlow != 26 || p.MacdSignal != 9 {
		t.Errorf("default MACD tunables = %d/%d/%d, want 12/26/9", p.MacdFast, p.MacdSlow, p.MacdSignal)
	}
	if p.EmaFast != 5 || p.EmaSlow != 15 {
		t.Errorf("default EMA tunables = %d/%d, want 5/15", p.EmaFast, p.EmaSlow)
	}

	// 选择器落库的 indicator_params.macd / ema_cross 被解析进 CRAParams。
	p2, err := ParseCRAParams(`{
		"open_indicator": "macd",
		"indicator_params": {
			"macd": {"fast": 8, "slow": 21, "signal": 5},
			"ema_cross": {"fast": 9, "slow": 30}
		}
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p2.MacdFast != 8 || p2.MacdSlow != 21 || p2.MacdSignal != 5 {
		t.Errorf("MACD tunables = %d/%d/%d, want 8/21/5", p2.MacdFast, p2.MacdSlow, p2.MacdSignal)
	}
	if p2.EmaFast != 9 || p2.EmaSlow != 30 {
		t.Errorf("EMA tunables = %d/%d, want 9/30", p2.EmaFast, p2.EmaSlow)
	}

	// 存量 trend_long 记录回退 EMA 参数。
	p3, err := ParseCRAParams(`{"indicator_params": {"trend_long": {"fast": 7, "slow": 44}}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p3.EmaFast != 7 || p3.EmaSlow != 44 {
		t.Errorf("EMA tunables from trend_long = %d/%d, want 7/44", p3.EmaFast, p3.EmaSlow)
	}

	// 非法值（非正数）维持 ValidateIndicatorParams 的拒绝语义。
	if _, err := ParseCRAParams(`{"indicator_params": {"macd": {"fast": -3}}}`); err == nil {
		t.Error("expected parse error for negative macd.fast")
	}
}

func TestMACDTunablesFlowIntoGate(t *testing.T) {
	bars := makeBars(decliningThenCrossSeries()...)
	// 缺省参数与旧函数结果一致（向后兼容）。
	if MACDBullishWithTunables(bars, MACDTunables{}) != MACDBullish(bars) {
		t.Error("zero tunables must match MACDBullish default behavior")
	}
	if !MACDBullish(bars) {
		t.Fatal("series sanity: default MACD should cross bullish at last bar")
	}
	// 自定义快线组：最后一根不金叉（金叉发生在前一根）→ 参数确实参与计算。
	if MACDBullishWithTunables(bars, MACDTunables{Fast: 3, Slow: 6, Signal: 2}) {
		t.Error("custom (3,6,2) tunables should NOT confirm at last bar (cross happened one bar earlier)")
	}
	// 经 IndicatorConfirmedWithTunables 的参数化路径同样生效。
	if !IndicatorConfirmedWithTunables(bars, true, "15m", "macd", SideLong, MACDTunables{}, EMATunables{}) {
		t.Error("default tunables macd gate should confirm on the cross series")
	}
	if IndicatorConfirmedWithTunables(bars, true, "15m", "macd", SideLong, MACDTunables{Fast: 3, Slow: 6, Signal: 2}, EMATunables{}) {
		t.Error("custom tunables macd gate should not confirm on the cross series")
	}
	// close 语义不变：不额外监测，直接放行。
	if !IndicatorConfirmedWithTunables(nil, true, "close", "macd", SideLong, MACDTunables{}, EMATunables{}) {
		t.Error("period=close must pass through")
	}
}

// ── A4：EMA 顺势/逆势语义区分 ──

func TestTrendEmaConfirmed(t *testing.T) {
	// 稳定上行：快线 > 慢线 且收盘站稳慢线之上 → 做多确认、做空拒绝。
	up := makeBars(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119)
	if !TrendEmaConfirmed(up, EMATunables{}, SideLong) {
		t.Error("trend ema should confirm long on steady uptrend")
	}
	if TrendEmaConfirmed(up, EMATunables{}, SideShort) {
		t.Error("trend ema should reject short on steady uptrend")
	}
	// 稳定下行：做空确认、做多拒绝（镜像）。
	down := makeBars(120, 119, 118, 117, 116, 115, 114, 113, 112, 111, 110, 109, 108, 107, 106, 105, 104, 103, 102, 101)
	if !TrendEmaConfirmed(down, EMATunables{}, SideShort) {
		t.Error("trend ema should confirm short on steady downtrend")
	}
	if TrendEmaConfirmed(down, EMATunables{}, SideLong) {
		t.Error("trend ema should reject long on steady downtrend")
	}
}

func TestCounterEmaConfirmed(t *testing.T) {
	// 阴跌至慢线之下后出现向上拐点（100→89.5 一路下行，末根回升 90.2）：
	// 逆势做多确认（低于慢线 + 向上拐点），顺势做多拒绝（快线在慢线之下）。
	dip := makeBars(100, 98, 96, 94, 92, 90, 89.5, 90.2)
	if !CounterEmaConfirmed(dip, EMATunables{}, SideLong) {
		t.Error("counter ema should confirm long on dip below slow line with upturn")
	}
	if CounterEmaConfirmed(dip, EMATunables{}, SideShort) {
		t.Error("counter ema should reject short on the same dip series")
	}
	if TrendEmaConfirmed(dip, EMATunables{}, SideLong) {
		t.Error("trend ema should reject long on the same dip series (A4 split)")
	}
	// 没有拐点的继续阴跌：逆势不做多。
	falling := makeBars(100, 98, 96, 94, 92, 90, 89.5, 89.0)
	if CounterEmaConfirmed(falling, EMATunables{}, SideLong) {
		t.Error("counter ema must not confirm long without an upward inflection")
	}
	// 冲高于慢线之上后出现向下拐点：逆势做空确认（镜像）。
	spike := makeBars(90, 92, 94, 96, 98, 100, 100.5, 99.8)
	if !CounterEmaConfirmed(spike, EMATunables{}, SideShort) {
		t.Error("counter ema should confirm short on spike above slow line with downturn")
	}
	if CounterEmaConfirmed(spike, EMATunables{}, SideLong) {
		t.Error("counter ema should reject long on the same spike series")
	}
}

func TestOpenIndicatorsTrendCounterSplit(t *testing.T) {
	// 策略级路由：同一稳定上行序列，顺势 EMA 放行开多、逆势 EMA 拦截
	//（价格高于慢线且无向下拐点，反转条件不成立）。
	start := func(trend, counter bool) *BaseCRAStrategy {
		s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":                   "BTCUSDT",
			"timeframe":                "15m",
			"direction":                "long",
			"tp_mode":                  "static",
			"open_trend_ema_enabled":   trend,
			"open_trend_ema_period":    "15m",
			"open_counter_ema_enabled": counter,
			"open_counter_ema_period":  "15m",
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	feed := func(s *BaseCRAStrategy) *model.Signal {
		var sig *model.Signal
		for i := 0; i < 20; i++ {
			c := 100 + float64(i)
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
	if sig := feed(start(true, false)); sig == nil {
		t.Error("trend ema gate should allow long entry on steady uptrend")
	}
	if sig := feed(start(false, true)); sig != nil {
		t.Errorf("counter ema gate should block long entry on steady uptrend, got %+v", sig)
	}
}

// TestLegacyEmaGateUnchanged 锁定旧版共享 EMA 门槛语义（add_ema 沿用 "ema"
// 类型）：做多=金叉或收盘站上慢线即放行，与 A4 新门槛无关。
func TestLegacyEmaGateUnchanged(t *testing.T) {
	// 稳定上行但最后一根无金叉（早已金叉过）：旧语义凭"收盘 > 慢线"放行。
	up := makeBars(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119)
	if !IndicatorConfirmedWithTunables(up, true, "15m", "ema", SideLong, MACDTunables{}, EMATunables{}) {
		t.Error("legacy ema gate should pass long when close is above slow line")
	}
	// 空序列：旧实现做多/做空都为 false，保持不 panic 且不放大行为。
	if IndicatorConfirmedWithTunables(nil, true, "15m", "ema", SideLong, MACDTunables{}, EMATunables{}) {
		t.Error("legacy ema gate should reject long on empty bars")
	}
}
