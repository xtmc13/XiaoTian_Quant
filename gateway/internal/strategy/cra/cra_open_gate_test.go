package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── H1：现货开仓指标门槛（修复前现货分支不评估任何指标，picker 选中即死参）──

// feedBars 逐根喂 K 线，返回最后一个非空信号。
func feedBars(t *testing.T, s *BaseCRAStrategy, closes ...float64) *model.Signal {
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

func TestCRASpotOpenGateMACD(t *testing.T) {
	start := func() *BaseCRAStrategy {
		s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":             "BTCUSDT",
			"timeframe":          "15m",
			"tp_mode":            "static",
			"take_profit_ratio":  0.5, // 远离测试价格区间，止盈不干扰
			"open_indicator":     "macd",
			"open_macd_enabled":  true,
			"open_macd_period":   "15m", // = 工作周期：门槛评估工作 bar
			"first_order_amount": 100,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}

	// 43 根"阴跌后回升"序列：默认 MACD(12,26,9) 最后一根金叉 → 金叉开仓。
	if sig := feedBars(t, start(), decliningThenCrossSeries()...); sig == nil {
		t.Fatal("spot MACD gate should open long on golden cross")
	} else if sig.Direction != "LONG" {
		t.Errorf("direction = %v, want LONG", sig.Direction)
	}

	// 纯阴跌 43 根（无金叉）→ 不开仓（修复前现货直接首根进场）。
	declining := make([]float64, 43)
	p := 100.0
	for i := range declining {
		declining[i] = p
		p -= 0.4
	}
	if sig := feedBars(t, start(), declining...); sig != nil {
		t.Fatalf("spot MACD gate should block entry without golden cross, got %+v", sig)
	}
}

func TestCRASpotOpenGateDefaultUnchanged(t *testing.T) {
	// 存量兼容：现货默认（未选指标）保持"无门槛直接进"——首根 K 线即开仓。
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
		t.Fatal("spot default (no indicator selected) must enter on first bar, unchanged")
	}
}

// ── H2：rsi/range 真门槛 + trend 参数进引擎 ──

// rsiReboundSeries 19 根阴跌（RSI 持续 0，深度超卖）后 1 根 +30 强反弹：
// RSI(14) 由 0 升至 ≈53.6——上一根 <30、当前根 ≥30，超卖反弹确认恰好发生在
// 最后一根。
func rsiReboundSeries() []float64 {
	closes := make([]float64, 0, 20)
	p := 100.0
	for i := 0; i < 19; i++ {
		closes = append(closes, p)
		p -= 2
	}
	return append(closes, p+30)
}

func TestRSIOversoldReboundConfirmed(t *testing.T) {
	series := rsiReboundSeries()
	// 缺最后一根（仍在超卖区阴跌）：不确认。
	if RSIOversoldReboundConfirmed(makeBars(series[:19]...), 14, 30, 70, SideLong) {
		t.Error("rsi gate should not confirm while still below oversold")
	}
	// 完整序列：末根反弹站上超卖线 → 做多确认、做空拒绝。
	if !RSIOversoldReboundConfirmed(makeBars(series...), 14, 30, 70, SideLong) {
		t.Error("rsi gate should confirm long on oversold rebound")
	}
	if RSIOversoldReboundConfirmed(makeBars(series...), 14, 30, 70, SideShort) {
		t.Error("rsi gate should reject short on oversold rebound")
	}
	// 序列不足（< period+2）：不确认。
	if RSIOversoldReboundConfirmed(makeBars(series[:15]...), 14, 30, 70, SideLong) {
		t.Error("rsi gate should not confirm with insufficient bars")
	}
	// 做空镜像：超买区持续上行后回落跌破超买线 → 做空确认。
	rising := make([]float64, 0, 20)
	p := 100.0
	for i := 0; i < 19; i++ {
		rising = append(rising, p)
		p += 2
	}
	if RSIOversoldReboundConfirmed(makeBars(rising...), 14, 30, 70, SideShort) {
		t.Error("rsi gate should not confirm short while still above overbought")
	}
	rising = append(rising, p-30)
	if !RSIOversoldReboundConfirmed(makeBars(rising...), 14, 30, 70, SideShort) {
		t.Error("rsi gate should confirm short on overbought pullback")
	}
}

func TestCRASpotOpenGateRSI(t *testing.T) {
	// 策略级路由：现货选 rsi 门槛，超卖反弹才开仓（H1+H2 联合：现货分支
	// 此前连门槛函数都不进）。
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"timeframe":          "15m",
		"tp_mode":            "static",
		"take_profit_ratio":  0.5,
		"first_order_amount": 100,
		"open_indicator":     "rsi",
		"indicator_params":   map[string]any{"rsi": map[string]any{"period": 14, "oversold": 30, "overbought": 70}},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if sig := feedBars(t, s, rsiReboundSeries()...); sig == nil {
		t.Fatal("spot rsi gate should open long on oversold rebound")
	}

	// 对照：纯阴跌（RSI 一直 0，无反弹）→ 不开仓。
	s2 := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s2.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"timeframe":          "15m",
		"tp_mode":            "static",
		"take_profit_ratio":  0.5,
		"first_order_amount": 100,
		"open_indicator":     "rsi",
		"indicator_params":   map[string]any{"rsi": map[string]any{"period": 14, "oversold": 30}},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	declining := make([]float64, 30)
	p := 100.0
	for i := range declining {
		declining[i] = p
		p -= 2
	}
	if sig := feedBars(t, s2, declining...); sig != nil {
		t.Fatalf("spot rsi gate should block entry without rebound, got %+v", sig)
	}
}

func TestRSINeutralBandConfirmed(t *testing.T) {
	// 交替横盘：RSI=50，落在 50±10 中性区 → 放行。
	sideways := make([]float64, 20)
	for i := range sideways {
		sideways[i] = 100 + float64(i%2)
	}
	if !RSINeutralBandConfirmed(makeBars(sideways...), 14, 10) {
		t.Error("range gate should confirm when RSI is neutral (50)")
	}
	// 稳定上行：RSI=100，超出中性区 → 拦截（超买区不开震荡仓）。
	uptrend := make([]float64, 20)
	for i := range uptrend {
		uptrend[i] = 100 + float64(i)
	}
	if RSINeutralBandConfirmed(makeBars(uptrend...), 14, 10) {
		t.Error("range gate should block overbought uptrend")
	}
	// 带宽生效：同上一路阴跌序列 RSI=0，±10 拦截；±60 全放（参数确实参与）。
	declining := make([]float64, 20)
	for i := range declining {
		declining[i] = 200 - float64(i)
	}
	if RSINeutralBandConfirmed(makeBars(declining...), 14, 10) {
		t.Error("range gate should block RSI=0 with ±10 band")
	}
	if !RSINeutralBandConfirmed(makeBars(declining...), 14, 60) {
		t.Error("range gate should pass RSI=0 with ±60 band (band param must matter)")
	}
}

func TestCRASpotOpenGateRange(t *testing.T) {
	start := func() *BaseCRAStrategy {
		s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":             "BTCUSDT",
			"timeframe":          "15m",
			"tp_mode":            "static",
			"take_profit_ratio":  0.5,
			"first_order_amount": 100,
			"open_indicator":     "range",
			"indicator_params":   map[string]any{"range": map[string]any{"period": 14, "neutral_band": 10}},
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	// 横盘 20 根：RSI=50 落中性区 → 开仓。
	sideways := make([]float64, 20)
	for i := range sideways {
		sideways[i] = 100 + float64(i%2)
	}
	if sig := feedBars(t, start(), sideways...); sig == nil {
		t.Fatal("spot range gate should open when RSI neutral")
	}
	// 稳定上行 20 根：RSI=100 出中性区 → 不开仓。
	uptrend := make([]float64, 20)
	for i := range uptrend {
		uptrend[i] = 100 + float64(i)
	}
	if sig := feedBars(t, start(), uptrend...); sig != nil {
		t.Fatalf("spot range gate should block overbought uptrend, got %+v", sig)
	}
}

// trendDipRallySeries 30 根阴跌（100→70）后 4 根 +1 弱反弹（71→74）：
// EMA(5) 滞后约 2 根 ≈72.5 < 74（站上快线），EMA(30) 滞后约 14 根 ≈84.5 > 74
// （仍压在慢线之下）——同一序列按 period 参数一放一拦，证明 period 进引擎。
func trendDipRallySeries() []float64 {
	closes := make([]float64, 0, 34)
	p := 100.0
	for i := 0; i < 30; i++ {
		closes = append(closes, p)
		p--
	}
	for i := 0; i < 4; i++ {
		p++
		closes = append(closes, p)
	}
	return closes
}

func TestTrendMAConfirmed(t *testing.T) {
	// 稳定上行：收盘价站上 MA(20) → 做多放行、做空拦截；EMA/SMA 同结论。
	up := make([]float64, 25)
	for i := range up {
		up[i] = 100 + float64(i)
	}
	if !TrendMAConfirmed(makeBars(up...), 20, "EMA", SideLong) {
		t.Error("trend gate should confirm long above EMA(20)")
	}
	if !TrendMAConfirmed(makeBars(up...), 20, "SMA", SideLong) {
		t.Error("trend gate should confirm long above SMA(20)")
	}
	if TrendMAConfirmed(makeBars(up...), 20, "EMA", SideShort) {
		t.Error("trend gate should reject short above MA")
	}
	// 稳定下行：做多拦截、做空放行。
	down := make([]float64, 25)
	for i := range down {
		down[i] = 200 - float64(i)
	}
	if TrendMAConfirmed(makeBars(down...), 20, "EMA", SideLong) {
		t.Error("trend gate should reject long below EMA(20)")
	}
	if !TrendMAConfirmed(makeBars(down...), 20, "EMA", SideShort) {
		t.Error("trend gate should confirm short below EMA(20)")
	}
	// period 生效：同一序列 period=5 放行、period=30 拦截。
	dip := trendDipRallySeries()
	if !TrendMAConfirmed(makeBars(dip...), 5, "EMA", SideLong) {
		t.Error("trend gate period=5 should confirm (close above fast EMA)")
	}
	if TrendMAConfirmed(makeBars(dip...), 30, "EMA", SideLong) {
		t.Error("trend gate period=30 should block (close still below slow EMA)")
	}
	// 未知 ma_type 钳制为 EMA（与 params.go 解析口径一致）。
	if TrendMAConfirmed(makeBars(dip...), 5, "WMA", SideLong) != TrendMAConfirmed(makeBars(dip...), 5, "EMA", SideLong) {
		t.Error("unknown ma_type must fall back to EMA semantics")
	}
	// 序列不足：不确认。
	if TrendMAConfirmed(makeBars(up[:10]...), 20, "EMA", SideLong) {
		t.Error("trend gate should not confirm with insufficient bars")
	}
}

func TestCRASpotOpenGateTrend(t *testing.T) {
	start := func(period int) *BaseCRAStrategy {
		s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":             "BTCUSDT",
			"timeframe":          "15m",
			"tp_mode":            "static",
			"take_profit_ratio":  0.5,
			"first_order_amount": 100,
			"open_indicator":     "trend",
			"indicator_params":   map[string]any{"trend": map[string]any{"period": period, "ma_type": "EMA"}},
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	// 稳定上行：站上 MA(20) → 开仓。
	up := make([]float64, 25)
	for i := range up {
		up[i] = 100 + float64(i)
	}
	if sig := feedBars(t, start(20), up...); sig == nil {
		t.Fatal("spot trend gate should open above MA(20)")
	}
	// 稳定下行：跌破 MA(20) → 做多不开仓。
	down := make([]float64, 25)
	for i := range down {
		down[i] = 200 - float64(i)
	}
	if sig := feedBars(t, start(20), down...); sig != nil {
		t.Fatalf("spot trend gate should block long below MA(20), got %+v", sig)
	}
	// period 参数经落库键进引擎：同一序列 period=5 开仓、period=30 拦截。
	dip := trendDipRallySeries()
	if sig := feedBars(t, start(5), dip...); sig == nil {
		t.Fatal("spot trend gate period=5 should open on dip rally")
	}
	if sig := feedBars(t, start(30), dip...); sig != nil {
		t.Fatalf("spot trend gate period=30 should block dip rally, got %+v", sig)
	}
}

func TestParseCRAParamsOpenGateTunables(t *testing.T) {
	// 缺省：与前端 indicatorPresets 预设一致（rsi 14/30/70、range 14/±10、
	// trend EMA20）。
	p, err := ParseCRAParams("{}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.RsiPeriod != 14 || p.RsiOversold != 30 || p.RsiOverbought != 70 {
		t.Errorf("default rsi tunables = %d/%d/%d, want 14/30/70", p.RsiPeriod, p.RsiOversold, p.RsiOverbought)
	}
	if p.RangePeriod != 14 || p.RangeNeutralBand != 10 {
		t.Errorf("default range tunables = %d/±%d, want 14/±10", p.RangePeriod, p.RangeNeutralBand)
	}
	if p.TrendPeriod != 20 || p.TrendMAType != "EMA" {
		t.Errorf("default trend tunables = %d/%s, want 20/EMA", p.TrendPeriod, p.TrendMAType)
	}

	// 落库参数接进 CRAParams；ma_type 小写归一、未知值钳制 EMA。
	p2, err := ParseCRAParams(`{
		"open_indicator": "rsi",
		"indicator_params": {
			"rsi": {"period": 9, "oversold": 25, "overbought": 80},
			"range": {"period": 21, "neutral_band": 5},
			"trend": {"period": 60, "ma_type": "sma"}
		}
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p2.RsiPeriod != 9 || p2.RsiOversold != 25 || p2.RsiOverbought != 80 {
		t.Errorf("rsi tunables = %d/%d/%d, want 9/25/80", p2.RsiPeriod, p2.RsiOversold, p2.RsiOverbought)
	}
	if p2.RangePeriod != 21 || p2.RangeNeutralBand != 5 {
		t.Errorf("range tunables = %d/±%d, want 21/±5", p2.RangePeriod, p2.RangeNeutralBand)
	}
	if p2.TrendPeriod != 60 || p2.TrendMAType != "SMA" {
		t.Errorf("trend tunables = %d/%s, want 60/SMA", p2.TrendPeriod, p2.TrendMAType)
	}

	p3, err := ParseCRAParams(`{"indicator_params": {"trend": {"period": 55, "ma_type": "WMA"}}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p3.TrendMAType != "EMA" {
		t.Errorf("unknown ma_type should clamp to EMA, got %s", p3.TrendMAType)
	}
	// 非法值维持 ValidateIndicatorParams 的拒绝语义。
	if _, err := ParseCRAParams(`{"indicator_params": {"rsi": {"period": -1}}}`); err == nil {
		t.Error("expected parse error for negative rsi.period")
	}
}

// ── M5：补仓 EMA 双闸（档级 ema_enabled + 全局 add_ema_enabled 同时满足才启用）──

func TestAddPositionEmaDoubleGate(t *testing.T) {
	// 阴跌序列：旧版共享 EMA 门槛（做多=金叉或收盘站上慢线）此时不成立——
	// 双闸都开时拦截，任一关闭时放行。
	declining := make([]float64, 20)
	p := 100.0
	for i := range declining {
		declining[i] = p
		p -= 1
	}

	start := func(addEmaEnabled bool) *BaseCRAStrategy {
		s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":             "BTCUSDT",
			"timeframe":          "15m",
			"direction":          "long",
			"tp_mode":            "static",
			"take_profit_ratio":  0.5,
			"first_order_amount": 100,
			"add_ema_enabled":    addEmaEnabled,
			"add_ema_period":     "15m",
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		feedBars(t, s, declining...)
		return s
	}

	tierOn := &AddPositionItem{Order: 1, Multiplier: 1, Spread: 0.03, Callback: 0.003, EmaEnabled: true}
	tierOff := &AddPositionItem{Order: 1, Multiplier: 1, Spread: 0.03, Callback: 0.003, EmaEnabled: false}

	// 双闸同开：阴跌中 EMA 门槛拦截补仓确认。
	if start(true).addPositionConfirmed(tierOn) {
		t.Error("dual-gate on: ema gate should block add on declining series")
	}
	// 档级关、全局开：档级未启用 EMA → 不评估，放行。
	if !start(true).addPositionConfirmed(tierOff) {
		t.Error("tier ema off: add should pass without ema evaluation")
	}
	// 档级开、全局关：全局未启用 → 不评估，放行（修复前前端无从开启全局，
	// 档级开关形同虚设；此处锁定"全局关=EMA 不生效"的既有引擎语义）。
	if !start(false).addPositionConfirmed(tierOn) {
		t.Error("global add_ema off: add should pass without ema evaluation")
	}
}
