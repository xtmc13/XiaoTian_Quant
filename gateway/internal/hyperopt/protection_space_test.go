package hyperopt

import (
	"math/rand"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── 空间维度生成 ──

func TestBuildProtectionSpaces(t *testing.T) {
	base := []protection.ProtectionConfig{
		{Name: "StoplossGuard", Params: map[string]any{"trade_limit": 4}},
		{Name: "MaxDrawdown"},
	}
	spaces, err := BuildProtectionSpaces(base, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// StoplossGuard 4 维 + MaxDrawdown 5 维
	if len(spaces) != 9 {
		t.Fatalf("expected 9 dims, got %d: %+v", len(spaces), spaces)
	}

	byName := map[string]Space{}
	for _, s := range spaces {
		byName[s.Name] = s
	}
	tl, ok := byName["protection__StoplossGuard__trade_limit"]
	if !ok {
		t.Fatal("missing trade_limit dim")
	}
	if tl.Type != strategy.ParamInt || tl.Min != 1 || tl.Max != 10 || tl.Step != 1 {
		t.Fatalf("trade_limit dim mismatch: %+v", tl)
	}
	cm, ok := byName["protection__MaxDrawdown__calculation_mode"]
	if !ok || cm.Type != strategy.ParamCategorical || len(cm.Options) != 2 {
		t.Fatalf("calculation_mode dim mismatch: %+v", cm)
	}

	// 同一 protection 配两次 → 维度去重
	dup, err := BuildProtectionSpaces([]protection.ProtectionConfig{{Name: "StoplossGuard"}, {Name: "StoplossGuard"}}, nil)
	if err != nil {
		t.Fatalf("build dup: %v", err)
	}
	if len(dup) != 4 {
		t.Fatalf("duplicate protection should dedupe dims, got %d", len(dup))
	}
}

func TestBuildProtectionSpacesUnknown(t *testing.T) {
	_, err := BuildProtectionSpaces([]protection.ProtectionConfig{{Name: "NoSuchProtection"}}, nil)
	if err == nil {
		t.Fatal("unknown protection should error, not silently skip")
	}
}

func TestBuildProtectionSpacesOverride(t *testing.T) {
	spaces, err := BuildProtectionSpaces(
		[]protection.ProtectionConfig{{Name: "StoplossGuard"}},
		map[string]SpaceRangeOverride{
			"protection__StoplossGuard__trade_limit": {Min: 2, Max: 6, Step: 2},
		},
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, s := range spaces {
		if s.Name == "protection__StoplossGuard__trade_limit" {
			if s.Min != 2 || s.Max != 6 || s.Step != 2 {
				t.Fatalf("override not applied: %+v", s)
			}
			return
		}
	}
	t.Fatal("trade_limit dim not found")
}

// ── 参数拆分与回写 ──

func TestSplitProtectionParams(t *testing.T) {
	params := map[string]any{
		"lookback":                                  20,
		"stop_loss":                                 0.05,
		"protection__StoplossGuard__trade_limit":    3,
		"protection__MaxDrawdown__max_drawdown_pct": 0.15,
	}
	strat, prot := SplitProtectionParams(params)

	if len(strat) != 2 || strat["lookback"] != 20 {
		t.Fatalf("strategy params mismatch: %+v", strat)
	}
	if len(prot) != 2 {
		t.Fatalf("protection params mismatch: %+v", prot)
	}
	if prot["StoplossGuard"]["trade_limit"] != 3 {
		t.Fatalf("stoploss trade_limit mismatch: %+v", prot["StoplossGuard"])
	}
	if prot["MaxDrawdown"]["max_drawdown_pct"] != 0.15 {
		t.Fatalf("maxdrawdown pct mismatch: %+v", prot["MaxDrawdown"])
	}
}

func TestApplyProtectionParams(t *testing.T) {
	base := []protection.ProtectionConfig{
		{Name: "StoplossGuard", Params: map[string]any{"trade_limit": 4, "timeframe": "1h"}},
	}
	merged := ApplyProtectionParams(base, map[string]map[string]any{
		"StoplossGuard": {"trade_limit": 7},
		"MaxDrawdown":   {"max_drawdown_pct": 0.25},
	})

	if len(merged) != 2 {
		t.Fatalf("expected 2 protections (1 merged + 1 appended), got %d", len(merged))
	}
	if merged[0].Name != "StoplossGuard" {
		t.Fatalf("base order should be preserved, got %s", merged[0].Name)
	}
	if merged[0].Params["trade_limit"] != 7 {
		t.Fatalf("trade_limit should be overridden to 7, got %v", merged[0].Params["trade_limit"])
	}
	if merged[0].Params["timeframe"] != "1h" {
		t.Fatalf("untouched base param should survive, got %v", merged[0].Params["timeframe"])
	}
	if merged[1].Name != "MaxDrawdown" || merged[1].Params["max_drawdown_pct"] != 0.25 {
		t.Fatalf("new protection should be appended: %+v", merged[1])
	}

	// 合并结果必须能被 BuildManagerFromConfig 直接消费
	mgr, err := protection.BuildManagerFromConfig(protection.Config{Protections: merged})
	if err != nil {
		t.Fatalf("merged config should build: %v", err)
	}
	if len(mgr.Protections()) != 2 {
		t.Fatalf("expected 2 built protections, got %d", len(mgr.Protections()))
	}
}

func TestProtectionSpaceIntegratesWithSearchSpace(t *testing.T) {
	spaces, err := BuildProtectionSpaces([]protection.ProtectionConfig{{Name: "CooldownPeriod"}}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ss := NewSearchSpaceFromRegistry(strategy.NewParamRegistry())
	ss.AddSpaces(spaces)
	if ss.Dimensions() != 1 {
		t.Fatalf("expected 1 dim, got %d", ss.Dimensions())
	}
	// 采样 + 量化后应能拆分
	rng := rand.New(rand.NewSource(42))
	pt := ss.Quantize(ss.Sample(rng))
	_, prot := SplitProtectionParams(pt)
	if prot["CooldownPeriod"]["stop_duration_candles"] == nil {
		t.Fatalf("sampled point should contain protection dim, got %+v", pt)
	}
}

// ── 回测门控（ProtectedBacktestStrategy） ──

// flipStrategy 永远在无持仓时开多、有持仓时以 "stop loss" 平仓。
type flipStrategy struct {
	symbol string
}

func (s *flipStrategy) Name() string   { return "flip" }
func (s *flipStrategy) Symbol() string { return s.symbol }

func (s *flipStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	if state.Position == nil || state.Position.IsClosed {
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 1, Reason: "entry"}, nil
	}
	return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1, Reason: "long stop loss"}, nil
}

func (s *flipStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}

func TestProtectedBacktestBlocksEntriesAfterStoplosses(t *testing.T) {
	symbol := "TESTUSDT"
	// 30 根阴跌 K 线：开多后下一根平仓必为负收益（freqtrade required_profit 口径下才计为止损）
	bars := make([]model.Bar, 30)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range bars {
		price := 100.0 - float64(i)
		bars[i] = model.Bar{
			Time:   base.Add(time.Duration(i) * time.Hour).UnixMilli(),
			Open:   price, High: price + 1, Low: price - 1, Close: price,
			Volume: 1000,
		}
	}

	// trade_limit=1：一次止损后锁 12h（lookback 24h 内再次开仓会被拦）
	mgr, err := protection.BuildManagerFromConfig(protection.Config{Protections: []protection.ProtectionConfig{
		{Name: "StoplossGuard", Params: map[string]any{
			"lookback_period_candles": 24,
			"trade_limit":             1,
			"stop_duration_candles":   12,
			"timeframe":               "1h",
			"only_per_pair":           true,
		}},
	}})
	if err != nil {
		t.Fatalf("build manager: %v", err)
	}

	inner := &flipStrategy{symbol: symbol}
	protected := NewProtectedBacktestStrategy(inner, mgr, "1h")

	cfg := backtest.DefaultRunnerConfig()
	cfg.Slippage = 0
	cfg.Commission = 0
	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)

	result, err := runner.Run(protected)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if protected.BlockedEntries() == 0 {
		t.Fatal("stoploss guard should block re-entries after first stoploss")
	}
	if result.TotalTrades >= 5 {
		t.Fatalf("without protection flip strategy would trade ~15 times, got %d", result.TotalTrades)
	}

	// 对照组：无 protection 时同一策略交易次数明显更多
	runner2 := backtest.NewRunner(cfg)
	runner2.LoadBars(symbol, bars)
	result2, err := runner2.Run(&flipStrategy{symbol: symbol})
	if err != nil {
		t.Fatalf("run control: %v", err)
	}
	if result2.TotalTrades <= result.TotalTrades {
		t.Fatalf("control run should trade more (%d) than protected run (%d)", result2.TotalTrades, result.TotalTrades)
	}
}

func TestProtectedBacktestCooldownSkipsImmediateReentry(t *testing.T) {
	symbol := "TESTUSDT2"
	bars := make([]model.Bar, 10)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range bars {
		bars[i] = model.Bar{
			Time:   base.Add(time.Duration(i) * time.Hour).UnixMilli(),
			Open:   100, High: 101, Low: 99, Close: 100,
			Volume: 1000,
		}
	}

	// 冷却 3 根 K 线：平仓后 3h 内不允许再开仓
	mgr, err := protection.BuildManagerFromConfig(protection.Config{Protections: []protection.ProtectionConfig{
		{Name: "CooldownPeriod", Params: map[string]any{"stop_duration_candles": 3, "timeframe": "1h"}},
	}})
	if err != nil {
		t.Fatalf("build manager: %v", err)
	}

	protected := NewProtectedBacktestStrategy(&flipStrategy{symbol: symbol}, mgr, "1h")
	cfg := backtest.DefaultRunnerConfig()
	cfg.Slippage = 0
	cfg.Commission = 0
	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)

	result, err := runner.Run(protected)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if protected.BlockedEntries() == 0 {
		t.Fatal("cooldown should block immediate re-entries")
	}
	// 10 根 K 线、每次平仓后等 3h：最多 3 笔交易
	if result.TotalTrades > 3 {
		t.Fatalf("cooldown should cap trade count to <=3, got %d", result.TotalTrades)
	}
	// 历史已喂给 protection（平仓被记录）
	if len(protected.TradeHistory()) == 0 {
		t.Fatal("closed trades should be recorded to protection history")
	}
}
