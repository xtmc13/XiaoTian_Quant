package strategies

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── M9（2026-10-10）：经典指标策略现货钳只做多 ──
// 修复前：EMACross/MACD/RSI/Bollinger 的反向交叉直接置 inPosition=true 发
// SHORT；现货无持仓卖出被拒 → inPosition 永不复位 → 策略永久"持仓中"。
// 修复后：显式现货（market_type=spot / category=spot）不发 SHORT、不置位；
// 合约双向与未声明市场（回测/直调）的行为不变。

func classicBar(close float64) model.Bar {
	return model.Bar{Symbol: "SOLUSDT", Interval: "15m", Close: close, High: close, Low: close}
}

// crossDownSeries 缓升 40 根后急跌：EMA 死叉（bar41）、MACD 死叉（bar40）触发 SHORT。
func crossDownSeries() []float64 {
	var out []float64
	p := 100.0
	for i := 0; i < 40; i++ {
		out = append(out, p)
		p += 0.05
	}
	for i := 0; i < 5; i++ {
		p -= 2
		out = append(out, p)
	}
	return out
}

// rsiOverboughtExitSeries 19 根连涨（RSI 钉 100、峰值 119）后一根 -10：
// RSI 跌回超买线触发 SHORT；峰值 119 低于宽幅恢复仓的 20% 止盈线（120）。
func rsiOverboughtExitSeries() []float64 {
	var out []float64
	p := 100.0
	for i := 0; i < 19; i++ {
		p += 1
		out = append(out, p)
	}
	p -= 10
	return append(out, p)
}

// bollingerRejectSeries 19 根平 100、脉冲 130、回落 118/110：上轨回落触发 SHORT。
func bollingerRejectSeries() []float64 {
	var out []float64
	for i := 0; i < 19; i++ {
		out = append(out, 100)
	}
	return append(out, 130, 118, 110)
}

// collectDirections 逐根喂 bar，收集全部信号方向。
func collectDirections(t *testing.T, onBar func(model.Bar) (*model.Signal, error), closes []float64) []string {
	t.Helper()
	var dirs []string
	for _, c := range closes {
		sig, err := onBar(classicBar(c))
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if sig != nil {
			dirs = append(dirs, sig.Direction)
		}
	}
	return dirs
}

func hasDir(dirs []string, want string) bool {
	for _, d := range dirs {
		if d == want {
			return true
		}
	}
	return false
}

func TestClassicSpotClampNoShort(t *testing.T) {
	series := crossDownSeries()

	// 现货：死叉序列全程不得出现 SHORT，且结束时不得卡在持仓态。
	spot := NewEMACrossStrategy()
	if err := spot.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "spot"}); err != nil {
		t.Fatal(err)
	}
	dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return spot.OnBar(b, nil) }, series)
	if hasDir(dirs, "SHORT") {
		t.Fatalf("spot EMA must not emit SHORT, got %v", dirs)
	}
	spot.mu.RLock()
	if spot.inPosition {
		t.Fatal("spot EMA stuck inPosition after death cross (M9 卡死复现)")
	}
	spot.mu.RUnlock()

	// 合约：同一序列 SHORT 照常（双向不受影响）。
	contract := NewEMACrossStrategy()
	if err := contract.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "swap"}); err != nil {
		t.Fatal(err)
	}
	dirs = collectDirections(t, func(b model.Bar) (*model.Signal, error) { return contract.OnBar(b, nil) }, series)
	if !hasDir(dirs, "SHORT") {
		t.Fatalf("contract EMA must keep two-way SHORT, got %v", dirs)
	}

	// 未声明市场（回测/直调路径）：保持双向历史行为。
	legacy := NewEMACrossStrategy()
	if err := legacy.Start(map[string]any{"symbol": "SOLUSDT"}); err != nil {
		t.Fatal(err)
	}
	dirs = collectDirections(t, func(b model.Bar) (*model.Signal, error) { return legacy.OnBar(b, nil) }, series)
	if !hasDir(dirs, "SHORT") {
		t.Fatalf("legacy (no market_type) EMA must keep two-way SHORT, got %v", dirs)
	}
}

func TestClassicSpotClampMACDRSIBollinger(t *testing.T) {
	// MACD：同一死叉序列。
	spotMACD := NewMACDStrategy()
	if err := spotMACD.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "spot"}); err != nil {
		t.Fatal(err)
	}
	dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return spotMACD.OnBar(b, nil) }, crossDownSeries())
	if hasDir(dirs, "SHORT") {
		t.Fatalf("spot MACD must not emit SHORT, got %v", dirs)
	}
	spotMACD.mu.RLock()
	stuck := spotMACD.inPosition
	spotMACD.mu.RUnlock()
	if stuck {
		t.Fatal("spot MACD stuck inPosition after bearish crossover")
	}
	contractMACD := NewMACDStrategy()
	if err := contractMACD.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "swap"}); err != nil {
		t.Fatal(err)
	}
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return contractMACD.OnBar(b, nil) }, crossDownSeries()); !hasDir(dirs, "SHORT") {
		t.Fatalf("contract MACD must keep two-way SHORT, got %v", dirs)
	}

	// RSI：超买回落序列。
	spotRSI := NewRSIStrategy()
	if err := spotRSI.Start(map[string]any{"symbol": "SOLUSDT", "category": "spot"}); err != nil {
		t.Fatal(err)
	}
	dirs = collectDirections(t, func(b model.Bar) (*model.Signal, error) { return spotRSI.OnBar(b, nil) }, rsiOverboughtExitSeries())
	if hasDir(dirs, "SHORT") {
		t.Fatalf("spot RSI must not emit SHORT, got %v", dirs)
	}
	spotRSI.mu.RLock()
	stuck = spotRSI.inPosition
	spotRSI.mu.RUnlock()
	if stuck {
		t.Fatal("spot RSI stuck inPosition after overbought exit")
	}
	contractRSI := NewRSIStrategy()
	if err := contractRSI.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "futures"}); err != nil {
		t.Fatal(err)
	}
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return contractRSI.OnBar(b, nil) }, rsiOverboughtExitSeries()); !hasDir(dirs, "SHORT") {
		t.Fatalf("contract RSI must keep two-way SHORT, got %v", dirs)
	}

	// Bollinger：上轨回落序列。
	spotBoll := NewBollingerBandsStrategy()
	if err := spotBoll.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "spot"}); err != nil {
		t.Fatal(err)
	}
	dirs = collectDirections(t, func(b model.Bar) (*model.Signal, error) { return spotBoll.OnBar(b, nil) }, bollingerRejectSeries())
	if hasDir(dirs, "SHORT") {
		t.Fatalf("spot Bollinger must not emit SHORT, got %v", dirs)
	}
	spotBoll.mu.RLock()
	stuck = spotBoll.inPosition
	spotBoll.mu.RUnlock()
	if stuck {
		t.Fatal("spot Bollinger stuck inPosition after upper-band rejection")
	}
	contractBoll := NewBollingerBandsStrategy()
	if err := contractBoll.Start(map[string]any{"symbol": "SOLUSDT", "market_type": "swap"}); err != nil {
		t.Fatal(err)
	}
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return contractBoll.OnBar(b, nil) }, bollingerRejectSeries()); !hasDir(dirs, "SHORT") {
		t.Fatalf("contract Bollinger must keep two-way SHORT, got %v", dirs)
	}
}

// ── M10（2026-10-10）：EMA/RSI/Bollinger 重启仓位重建 ──
// 修复前四个经典策略只有 MACD 实现 PositionRestorer/Start 恢复，其余重启后
// 状态清零、暖机窗口内交叉条件仍在就重复入场。M10 把恢复提炼为
// classicPosition 共享实现给三个策略。宽幅 SL/TP（±20%）保证恢复持仓在
// 序列全程不被止盈/止损清掉——断言焦点是"持仓期间暖机 bar 不重复入场"。

// TestClassicRestoreBlocksReentry：恢复持仓后喂"空仓时必触发入场"的序列，
// 恢复实例全程不得再发 LONG/SHORT（CLOSE 是出场管理，本序列宽幅参数下也不应出现）。
func TestClassicRestoreBlocksReentry(t *testing.T) {
	wideParams := func() map[string]any {
		return map[string]any{
			"symbol": "SOLUSDT", "stop_loss_pct": 0.20, "take_profit_pct": 0.20,
			"restored_position_qty": 5.0, "restored_position_vwap": 100.0,
		}
	}

	// EMA：死叉序列（空仓对照触发 SHORT；恢复持仓后不得触发）。
	ema := NewEMACrossStrategy()
	if err := ema.Start(wideParams()); err != nil {
		t.Fatal(err)
	}
	ema.mu.RLock()
	if !ema.inPosition || ema.direction != "LONG" || ema.entryPrice != 100.0 {
		ema.mu.RUnlock()
		t.Fatalf("EMA restore failed: inPos=%v dir=%s entry=%v", ema.inPosition, ema.direction, ema.entryPrice)
	}
	ema.mu.RUnlock()
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return ema.OnBar(b, nil) }, crossDownSeries()); len(dirs) != 0 {
		t.Fatalf("restored EMA must not emit entry/exit on this series, got %v", dirs)
	}
	ctrlEMA := NewEMACrossStrategy()
	if err := ctrlEMA.Start(map[string]any{"symbol": "SOLUSDT", "stop_loss_pct": 0.20, "take_profit_pct": 0.20}); err != nil {
		t.Fatal(err)
	}
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return ctrlEMA.OnBar(b, nil) }, crossDownSeries()); !hasDir(dirs, "SHORT") {
		t.Fatal("control (flat) EMA must fire SHORT on this series — 序列无效")
	}

	// RSI：超买回落序列。
	rsi := NewRSIStrategy()
	if err := rsi.Start(wideParams()); err != nil {
		t.Fatal(err)
	}
	rsi.mu.RLock()
	if !rsi.inPosition || rsi.entryPrice != 100.0 {
		rsi.mu.RUnlock()
		t.Fatalf("RSI restore failed: inPos=%v entry=%v", rsi.inPosition, rsi.entryPrice)
	}
	rsi.mu.RUnlock()
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return rsi.OnBar(b, nil) }, rsiOverboughtExitSeries()); len(dirs) != 0 {
		t.Fatalf("restored RSI must not emit signals on this series, got %v", dirs)
	}
	ctrlRSI := NewRSIStrategy()
	if err := ctrlRSI.Start(map[string]any{"symbol": "SOLUSDT", "stop_loss_pct": 0.20, "take_profit_pct": 0.20}); err != nil {
		t.Fatal(err)
	}
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return ctrlRSI.OnBar(b, nil) }, rsiOverboughtExitSeries()); !hasDir(dirs, "SHORT") {
		t.Fatal("control (flat) RSI must fire SHORT on this series — 序列无效")
	}

	// Bollinger：上轨回落序列。
	boll := NewBollingerBandsStrategy()
	if err := boll.Start(wideParams()); err != nil {
		t.Fatal(err)
	}
	boll.mu.RLock()
	if !boll.inPosition || boll.entryPrice != 100.0 {
		boll.mu.RUnlock()
		t.Fatalf("Bollinger restore failed: inPos=%v entry=%v", boll.inPosition, boll.entryPrice)
	}
	boll.mu.RUnlock()
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return boll.OnBar(b, nil) }, bollingerRejectSeries()); len(dirs) != 0 {
		t.Fatalf("restored Bollinger must not emit signals on this series, got %v", dirs)
	}
	ctrlBoll := NewBollingerBandsStrategy()
	if err := ctrlBoll.Start(map[string]any{"symbol": "SOLUSDT", "stop_loss_pct": 0.20, "take_profit_pct": 0.20}); err != nil {
		t.Fatal(err)
	}
	if dirs := collectDirections(t, func(b model.Bar) (*model.Signal, error) { return ctrlBoll.OnBar(b, nil) }, bollingerRejectSeries()); !hasDir(dirs, "SHORT") {
		t.Fatal("control (flat) Bollinger must fire SHORT on this series — 序列无效")
	}

	// RestorePosition 直调路径（PositionRestorer 接口，工具链用）。
	rsi2 := NewRSIStrategy()
	if err := rsi2.RestorePosition(3, 100); err != nil {
		t.Fatal(err)
	}
	rsi2.mu.RLock()
	if !rsi2.inPosition || rsi2.direction != "LONG" || rsi2.entryPrice != 100 {
		rsi2.mu.RUnlock()
		t.Fatal("RSI RestorePosition direct call failed")
	}
	rsi2.mu.RUnlock()
}

// TestClassicRestoreShortSide：合约空单按 restored_position_side 如实恢复
// （与 MACD H1 片同语义），空单 TP/SL 镜像继续管理。
func TestClassicRestoreShortSide(t *testing.T) {
	ema := NewEMACrossStrategy()
	if err := ema.Start(map[string]any{
		"symbol": "SOLUSDT", "market_type": "swap",
		"restored_position_qty": 2.0, "restored_position_vwap": 100.0, "restored_position_side": "short",
	}); err != nil {
		t.Fatal(err)
	}
	ema.mu.RLock()
	if !ema.inPosition || ema.direction != "SHORT" || ema.entryPrice != 100 {
		ema.mu.RUnlock()
		t.Fatalf("short restore failed: %+v", ema.classicPosition)
	}
	ema.mu.RUnlock()
	// 喂满指标窗口（slow+2 根，100 平仓价在空单 SL 102 / TP 96 带内不触发），
	// 再涨破止损线（100×1.02=102）。
	for i := 0; i < 28; i++ {
		if sig, _ := ema.OnBar(classicBar(100), nil); sig != nil {
			t.Fatalf("warmup bar %d: unexpected signal %+v（恢复态不得重复入场/误出场）", i, sig)
		}
	}
	sig, err := ema.OnBar(classicBar(102.5), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || sig.Reason != "short stop loss" {
		t.Fatalf("short SL = %+v err=%v, want CLOSE short stop loss", sig, err)
	}
}
