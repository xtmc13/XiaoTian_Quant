package protection

import (
	"testing"
	"time"
)

// freqtrade 语义补全测试：
//  - StoplossGuard 全局+单对双检查 / required_profit 过滤 / 锚点式解锁 / only_per_side
//  - MaxDrawdown 交易历史回撤计算（ratios / equity）/ trade_limit / 到期解锁
//  - LowProfitPairs 累计盈利率口径 / 锚点式解锁
//  - CooldownPeriod 分钟参数与 unlock_at

func TestStoplossGuardGlobalAndPerPairBothChecked(t *testing.T) {
	// freqtrade：only_per_pair=false 时全局与单对检查同时生效（单对记录是全局子集，
	// 单对触发必伴随全局触发，但锁的归属以先命中的全局为准）。
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24,
		"trade_limit":             2,
		"stop_duration_candles":   6,
		"timeframe":               "1h",
		"only_per_pair":           false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	// BTC 单对 2 次止损（达到 limit），全局也只有这 2 次 → 全局同样触发
	p.RecordStoploss("BTCUSDT", now.Add(-1*time.Hour))
	p.RecordStoploss("BTCUSDT", now.Add(-2*time.Hour))

	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	res := p.Check(ctx)
	if !res.Blocked {
		t.Fatal("BTC should be blocked (pair check must run even when only_per_pair=false)")
	}

	// 全局 3 次来自不同对（含 BTC 1 次）→ 全局锁，BTC 单对不单独锁
	p2, _ := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24,
		"trade_limit":             2,
		"stop_duration_candles":   6,
		"timeframe":               "1h",
	})
	p2.RecordStoploss("ETHUSDT", now.Add(-1*time.Hour))
	p2.RecordStoploss("SOLUSDT", now.Add(-2*time.Hour))
	p2.RecordStoploss("BTCUSDT", now.Add(-3*time.Hour))

	res = p2.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now})
	if !res.Blocked || res.Pair != "" {
		t.Fatalf("global check should lock all pairs, got %+v", res)
	}
}

func TestStoplossGuardRequiredProfit(t *testing.T) {
	// freqtrade required_profit：盈利 >= required_profit 的"止损"（如移动止盈）不计入。
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24,
		"trade_limit":             2,
		"stop_duration_candles":   6,
		"timeframe":               "1h",
		"required_profit":         0.0,
		"only_per_pair":           true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	// 一次亏损止损（-0.05 计入）+ 一次盈利止损（+0.03 不计入）→ 只有 1 次有效 < 2
	p.RecordStoplossDetail("BTCUSDT", now.Add(-1*time.Hour), -0.05, "LONG")
	p.RecordStoplossDetail("BTCUSDT", now.Add(-2*time.Hour), 0.03, "LONG")

	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	if p.Check(ctx).Blocked {
		t.Fatal("profitable stoploss should not count toward trade_limit")
	}

	// 再来一次亏损止损 → 2 次有效 → 锁定
	p.RecordStoplossDetail("BTCUSDT", now.Add(-30*time.Minute), -0.02, "LONG")
	if !p.Check(ctx).Blocked {
		t.Fatal("second losing stoploss should trigger the guard")
	}
}

func TestStoplossGuardAnchorBasedUnlock(t *testing.T) {
	// freqtrade 锚点语义：锁到「最近一次止损 + stop_duration」，
	// 到期后即使止损仍在回溯窗内也自动解锁（不会无限续期）。
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24, // 24h 回溯
		"trade_limit":             2,
		"stop_duration_candles":   3, // 锁 3h
		"timeframe":               "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	p.RecordStoploss("BTCUSDT", now.Add(-2*time.Hour))
	p.RecordStoploss("BTCUSDT", now.Add(-2*time.Hour))

	// 锁定中（锚点 -2h + 3h = +1h）
	if !p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}).Blocked {
		t.Fatal("should be locked within stop duration")
	}

	// 2 小时后（止损发生 4h 前，仍在 24h 回溯窗内，但已过锚点+3h）→ 自动解锁
	later := now.Add(2 * time.Hour)
	if p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: later}).Blocked {
		t.Fatal("should auto-unlock after anchor + stop_duration even inside lookback window")
	}
}

func TestStoplossGuardOnlyPerSide(t *testing.T) {
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24,
		"trade_limit":             2,
		"stop_duration_candles":   6,
		"timeframe":               "1h",
		"only_per_pair":           true,
		"only_per_side":           true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	p.RecordStoplossDetail("BTCUSDT", now.Add(-1*time.Hour), -0.05, "LONG")
	p.RecordStoplossDetail("BTCUSDT", now.Add(-2*time.Hour), -0.05, "SHORT")

	// LONG 信号：只有 1 次同向止损 → 不锁
	if p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now, Side: "LONG"}).Blocked {
		t.Fatal("only 1 LONG stoploss, should not block LONG side")
	}
	// 再记一次 LONG → 2 次 → 锁 LONG
	p.RecordStoplossDetail("BTCUSDT", now.Add(-30*time.Minute), -0.05, "LONG")
	if !p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now, Side: "LONG"}).Blocked {
		t.Fatal("2 LONG stoplosses should block LONG side")
	}
	// 不限方向（Side 为空）：全部计入 → 锁
	if !p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}).Blocked {
		t.Fatal("empty side should count all stoplosses")
	}
}

func TestStoplossGuardMinutesWindow(t *testing.T) {
	// 分钟制参数（freqtrade lookback_period / stop_duration）
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period": 120, // 120 分钟
		"trade_limit":     1,
		"stop_duration":   30, // 30 分钟
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	now := time.Now()
	p.RecordStoploss("BTCUSDT", now.Add(-10*time.Minute))

	if !p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}).Blocked {
		t.Fatal("should block within 30min stop duration")
	}
	if p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now.Add(31 * time.Minute)}).Blocked {
		t.Fatal("should unlock after 30min stop duration")
	}
	// 止损落在 120 分钟回溯窗外 → 不锁
	if p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now.Add(3 * time.Hour)}).Blocked {
		t.Fatal("stoploss outside 120min lookback should not block")
	}
}

func TestMaxDrawdownFromTradeHistoryRatios(t *testing.T) {
	// freqtrade ratios 口径：累计盈利率曲线峰谷落差。
	p, err := NewMaxDrawdown(map[string]any{
		"max_allowed_drawdown":    0.15, // freqtrade 参数名
		"lookback_period_candles": 48,
		"stop_duration_candles":   12,
		"timeframe":               "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	// +0.30, -0.20 → 峰 0.30 谷 0.10 → dd = 0.20 > 0.15 → 锁
	trades := []TradeRecord{
		{Symbol: "A", PnLPct: 0.10, ExitTime: now.Add(-4 * time.Hour)},
		{Symbol: "B", PnLPct: 0.20, ExitTime: now.Add(-3 * time.Hour)},
		{Symbol: "C", PnLPct: -0.20, ExitTime: now.Add(-2 * time.Hour)},
	}
	ctx := ProtectionContext{CurrentTime: now, TradeHistory: trades}
	res := p.Check(ctx)
	if !res.Blocked {
		t.Fatal("ratios drawdown 0.20 > 0.15 should block")
	}
	if res.Pair != "" {
		t.Fatal("max drawdown lock is global")
	}
	// 锚点 = 最近平仓(-2h) + 12h → 10h 后解锁
	if p.Check(ProtectionContext{CurrentTime: now.Add(11 * time.Hour), TradeHistory: trades}).Blocked {
		t.Fatal("should unlock after anchor + stop_duration")
	}

	// 回撤未超阈值：+0.10, -0.05 → dd = 0.05 < 0.15 → 不锁
	small := []TradeRecord{
		{Symbol: "A", PnLPct: 0.10, ExitTime: now.Add(-2 * time.Hour)},
		{Symbol: "B", PnLPct: -0.05, ExitTime: now.Add(-1 * time.Hour)},
	}
	if p.Check(ProtectionContext{CurrentTime: now, TradeHistory: small}).Blocked {
		t.Fatal("drawdown 0.05 < 0.15 should not block")
	}
}

func TestMaxDrawdownEquityMode(t *testing.T) {
	p, err := NewMaxDrawdown(map[string]any{
		"max_drawdown_pct":        0.10,
		"calculation_mode":        "equity",
		"lookback_period_candles": 48,
		"stop_duration_candles":   12,
		"timeframe":               "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	// 当前总权益 9000，窗口内累计盈亏 -1000 → 窗口起始权益 10000
	// 权益曲线：10000 → 10000+500 → ... 构造峰谷 10500 → 9000: dd = 1500/10500 ≈ 14.3% > 10%
	trades := []TradeRecord{
		{Symbol: "A", PnL: 500, ExitTime: now.Add(-3 * time.Hour)},
		{Symbol: "B", PnL: -1500, ExitTime: now.Add(-2 * time.Hour)},
		{Symbol: "C", PnL: 0, ExitTime: now.Add(-1 * time.Hour)},
	}
	ctx := ProtectionContext{CurrentTime: now, TotalBalance: 9000, TradeHistory: trades}
	if !p.Check(ctx).Blocked {
		t.Fatal("equity drawdown ~14.3% > 10% should block")
	}
}

func TestMaxDrawdownTradeLimit(t *testing.T) {
	p, err := NewMaxDrawdown(map[string]any{
		"max_drawdown_pct": 0.05,
		"trade_limit":      5, // 窗口内至少 5 笔才评估
		"timeframe":        "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now()
	trades := []TradeRecord{
		{Symbol: "A", PnLPct: -0.30, ExitTime: now.Add(-1 * time.Hour)},
	}
	if p.Check(ProtectionContext{CurrentTime: now, TradeHistory: trades}).Blocked {
		t.Fatal("fewer trades than trade_limit should not trigger (freqtrade semantics)")
	}
}

func TestMaxDrawdownLegacyFallback(t *testing.T) {
	// 无交易历史时回退到 ctx.CurrentDrawdown（旧行为）
	p, err := NewMaxDrawdown(map[string]any{"max_drawdown_pct": 0.20})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ctx := ProtectionContext{CurrentTime: time.Now(), CurrentDrawdown: 0.25}
	if !p.Check(ctx).Blocked {
		t.Fatal("legacy CurrentDrawdown fallback should block")
	}
}

func TestLowProfitPairsAnchorUnlock(t *testing.T) {
	p, err := NewLowProfitPairs(map[string]any{
		"lookback_period_candles": 24,
		"required_profit":         0.01, // freqtrade 参数名
		"trade_limit":             2,    // freqtrade 参数名
		"stop_duration_candles":   6,
		"timeframe":               "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	// 窗口内 2 笔亏损，合计 -0.07 < 0.01 → 锁
	p.RecordTrade(TradeRecord{Symbol: "BTCUSDT", PnLPct: -0.03, ExitTime: now.Add(-2 * time.Hour)})
	p.RecordTrade(TradeRecord{Symbol: "BTCUSDT", PnLPct: -0.04, ExitTime: now.Add(-1 * time.Hour)})

	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	res := p.Check(ctx)
	if !res.Blocked || res.Pair != "BTCUSDT" {
		t.Fatalf("low profit pair should be locked, got %+v", res)
	}
	// 锚点 = 最近平仓(-1h) + 6h = +5h；7h 后解锁
	if p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now.Add(7 * time.Hour)}).Blocked {
		t.Fatal("should unlock after anchor + stop_duration")
	}
	// 其他对不受影响
	if p.Check(ProtectionContext{Symbol: "ETHUSDT", CurrentTime: now}).Blocked {
		t.Fatal("unrelated pair should not be locked")
	}
}

func TestLowProfitPairsSumSemantics(t *testing.T) {
	// freqtrade 口径：各笔盈利率求和，不按名义价值加权。
	p, err := NewLowProfitPairs(map[string]any{
		"min_profit_ratio": 0.01,
		"min_trade_count":  2,
		"timeframe":        "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now()
	// 小额大亏（-0.10）+ 大额小赚（+0.005）：加权口径可能为正，求和口径 -0.095 < 0.01 → 锁
	p.RecordTrade(TradeRecord{Symbol: "X", EntryPrice: 100, Quantity: 1, PnLPct: -0.10, ExitTime: now.Add(-2 * time.Hour)})
	p.RecordTrade(TradeRecord{Symbol: "X", EntryPrice: 100, Quantity: 1000, PnLPct: 0.005, ExitTime: now.Add(-1 * time.Hour)})

	if !p.Check(ProtectionContext{Symbol: "X", CurrentTime: now}).Blocked {
		t.Fatal("sum-of-ratios semantics should lock (-0.095 < 0.01)")
	}
}

func TestCooldownPeriodMinutesAndUnlockAt(t *testing.T) {
	// stop_duration 分钟参数
	p, err := NewCooldownPeriod(map[string]any{"stop_duration": 45})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	now := time.Now()
	p.RecordExit("BTCUSDT", now.Add(-30*time.Minute))
	if !p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}).Blocked {
		t.Fatal("should block within 45min cooldown")
	}
	if p.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now.Add(20 * time.Minute)}).Blocked {
		t.Fatal("should unlock after 45min cooldown")
	}

	// unlock_at 固定时刻
	p2, err := NewCooldownPeriod(map[string]any{"unlock_at": "08:00"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	anchor := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	p2.RecordExit("BTCUSDT", anchor)
	// 当天 08:00 前应锁
	if !p2.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: anchor.Add(2 * time.Hour)}).Blocked {
		t.Fatal("should block before unlock_at 08:00")
	}
	// 08:00 之后解锁
	if p2.Check(ProtectionContext{Symbol: "BTCUSDT", CurrentTime: anchor.Add(6 * time.Hour)}).Blocked {
		t.Fatal("should unlock at 08:00")
	}
}

func TestWindowParamsLockEnd(t *testing.T) {
	w := windowParams{StopDurationMinutes: 60}
	anchor := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	if got := w.lockEnd(anchor); !got.Equal(anchor.Add(time.Hour)) {
		t.Fatalf("lockEnd should be anchor+60min, got %v", got)
	}

	w2 := windowParams{UnlockAt: "23:30"}
	got := w2.lockEnd(anchor)
	if got.Hour() != 23 || got.Minute() != 30 || got.Day() != 25 {
		t.Fatalf("unlock_at should be same day 23:30, got %v", got)
	}
	// 锚点已过当天 unlock_at → 顺延一天
	late := time.Date(2026, 9, 25, 23, 45, 0, 0, time.UTC)
	got2 := w2.lockEnd(late)
	if got2.Day() != 26 || got2.Hour() != 23 || got2.Minute() != 30 {
		t.Fatalf("unlock_at past anchor should roll to next day, got %v", got2)
	}
}

func TestBuildManagerFromConfigFreqtradeParams(t *testing.T) {
	// freqtrade 风格配置可直接构建（别名与分钟参数）
	cfg := Config{
		Protections: []ProtectionConfig{
			{Name: "CooldownPeriod", Params: map[string]any{"stop_duration": 30}},
			{Name: "StoplossGuard", Params: map[string]any{
				"lookback_period": 1440, "trade_limit": 4, "stop_duration": 60,
				"only_per_pair": false, "required_profit": 0.02,
			}},
			{Name: "MaxDrawdown", Params: map[string]any{
				"lookback_period": 1440, "trade_limit": 5, "max_allowed_drawdown": 0.2, "stop_duration": 60,
			}},
			{Name: "LowProfitPairs", Params: map[string]any{
				"lookback_period": 360, "trade_limit": 2, "required_profit": -0.05, "stop_duration": 60,
			}},
		},
	}
	mgr, err := BuildManagerFromConfig(cfg)
	if err != nil {
		t.Fatalf("build from freqtrade-style config: %v", err)
	}
	if len(mgr.Protections()) != 4 {
		t.Fatalf("expected 4 protections, got %d", len(mgr.Protections()))
	}
}
