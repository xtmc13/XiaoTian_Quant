package cra

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/risk"
)

// ── CRAParams 全参数覆盖矩阵（2026-10-10）──
//
// 验收口径：ParseCRAParams 读取的每个字段都有一个确定性测试证明"改这个参数 →
// 引擎行为按预期变化"。下表逐字段标注锁定测试（文件:测试名）；标注"本文件"的
// 为本次矩阵补缺口新增。
//
// 开仓/基础：
//   config_json（嵌套合并）        cra_test.go:TestBaseCRAStrategyFirstOrder
//   first_order_price              cra_refine_test.go:TestCRAFirstOrderPriceGate（多头闸）
//                                  cra_refine_test.go:TestCRAFirstOrderPriceZeroIsMarket（0=市价）
//                                  本文件:TestCRAMatrixFirstOrderPriceShortGate（空头镜像闸）
//   first_order_amount             数量公式 qty=amount×mult×lev/price：
//                                  cra_open_double_test.go:TestCRAOpenDoubleFirstOrderDoubled（10U×5→0.5）
//                                  cra_add_ladder_test.go:TestCRAAddLadderConsumesOrderOneFirst
//                                  本文件:TestCRAMatrixFirstOrderQtyParams（A/B 对照）
//   first_order_multiplier         解析 cra_test.go:TestParseCRAParamsFull；
//                                  引擎数量 本文件:TestCRAMatrixFirstOrderQtyParams；
//                                  校验边界 本文件:TestCRAMatrixValidationGaps
//   trade_count_mode               single 不循环 cra_test.go:TestBaseCRAStrategyLoopLimit；
//                                  非法值 本文件:TestCRAMatrixValidationGaps
//   loop_count                     本文件:TestCRAMatrixCycleLoopCountLimit（cycle 上限闸）
//   enable_add_position            本文件:TestCRAMatrixEnableAddPositionSwitch（false 全跌不补）
//   order_count                    上限=补仓次数 cra_add_ladder_test.go:TestCRAAddLadderCapEqualsAddCount；
//                                  网格最深档 cra_add_ladder_test.go:TestCRAGridLadderDeepestTierIsUserConfig；
//                                  校验边界 本文件:TestCRAMatrixValidationGaps
//   add_positions[].order          档序=第 N 次补仓 cra_add_ladder_test.go:TestCRAAddLadderConsumesOrderOneFirst（H3）
//   add_positions[].multiplier     数量倍投 cra_add_ladder_test.go:TestCRAAddLadderConsumesOrderOneFirst/#CapEqualsAddCount
//   add_positions[].spread         差价挂起 cra_test.go:TestCRAStateAddPositionLong + 阶梯 arm 价（cra_add_ladder_test.go）
//   add_positions[].callback       反弹触发 cra_test.go:TestCRAStateAddPositionLong + ladderDriveAdd 反弹价
//   add_positions[].ema_enabled    档级×全局双闸 cra_open_gate_test.go:TestAddPositionEmaDoubleGate；
//                                  "ema" 别名解析 本文件:TestCRAMatrixParseAddPositionEmaAlias
//   add_positions 缺档补齐          cra_test.go:TestParseCRAParamsDefaults；网格不补默认档 cra_add_ladder_test.go:TestCRAGridLadderDeepestTierIsUserConfig
//
// 止盈：
//   take_profit_method full/tail/head_tail
//                                  cra_tp_method_test.go:TestCRAFullTakeProfitRegression / TestCRATailTakeProfitFlow /
//                                  TestCRAHeadTailTakeProfitFlow（+state 层 TestCheckTailTakeProfit*/TestCheckHeadTail*）
//   tp_mode static/moving          moving 覆盖 method cra_tp_method_test.go:TestCRAMovingModeOverridesMethod；
//                                  移动档位 state 层 cra_test.go:TestCRAStateMovingTakeProfit
//   take_profit_ratio              达线触发 cra_tp_method_test.go:TestCRAFullTakeProfitRegression（0.02@102.1）；
//                                  0.5 隔离价区为全测试组共用前提
//   profit_callback                本文件:TestCRAMatrixStaticProfitCallback（静态止盈峰值回调）
//   moving_take_profit_tiers       cra_test.go:TestCRAStateMovingTakeProfit + TestCRAMovingModeOverridesMethod；
//                                  解析 cra_test.go:TestParseCRAParamsFull；4 档校验 本文件:TestCRAMatrixValidationGaps
//
// 开仓指标八类（现货/合约同闸 openIndicatorsConfirmed）：
//   open_macd_enabled/period       合约 1h 副周期 cra_refine_test.go:TestCRAIndicatorBarsMultiTimeframe；
//                                  降级路径 cra_refine_test.go:TestCRAIndicatorBarsFallbackToWorkingTF；
//                                  现货同闸 cra_open_gate_test.go:TestCRASpotOpenGateMACD / cra_refine_test.go:TestCRAOpenIndicatorGateSpot；
//                                  period=close 直通 cra_refine_test.go:TestMACDTunablesFlowIntoGate
//   open_counter_ema_enabled/period cra_refine_test.go:TestCounterEmaConfirmed + TestOpenIndicatorsTrendCounterSplit
//   open_trend_ema_enabled/period  cra_refine_test.go:TestTrendEmaConfirmed + TestOpenIndicatorsTrendCounterSplit
//   open_bollinger_enabled/period  cra_bollinger_test.go:TestBollingerConfirmed / TestCRAOpenBollingerGate /
//                                  TestCRAOpenBollingerMultiTimeframe
//   open_indicator=rsi             cra_open_gate_test.go:TestRSIOversoldReboundConfirmed + TestCRASpotOpenGateRSI；
//                                  阈值/周期生效 本文件:TestCRAMatrixOpenRsiThresholdParams
//   open_indicator=range           cra_open_gate_test.go:TestRSINeutralBandConfirmed + TestCRASpotOpenGateRange；
//                                  带宽进引擎 本文件:TestCRAMatrixOpenRangeBandParam
//   open_indicator=trend           cra_open_gate_test.go:TestTrendMAConfirmed（period/ma_type/钳制）+ TestCRASpotOpenGateTrend（引擎 period A/B）
//   open_indicator=custom          custom_indicator_test.go:TestCustomCodeID / TestLastBarSignalMatch / TestExecuteCustomIndicator；
//                                  引擎接线（无 code_id 直通、沙箱/DB 故障 fail-open/fail-close）本文件:TestCRAMatrixCustomIndicatorGate
//   open_indicator=macd/ema_cross  标记键本身不挂门槛，由旧 open_*_enabled 键承载（params.go 注释口径，前端双写）
//   indicator_params.macd.fast/slow/signal
//                                  解析 cra_refine_test.go:TestParseCRAParamsIndicatorTunables；
//                                  函数级 cra_refine_test.go:TestMACDTunablesFlowIntoGate；
//                                  引擎接线（3/6/2 改补仓判定）本文件:TestCRAMatrixAddMacdGate
//   indicator_params.macd.period→open_macd_period 回退
//                                  本文件:TestCRAMatrixParseIndicatorPeriodBackfill
//   indicator_params.ema_cross.fast/slow（trend_long/trend_short 回退）
//                                  解析 cra_refine_test.go:TestParseCRAParamsIndicatorTunables；
//                                  引擎接线（2/5 改开仓判定）本文件:TestCRAMatrixOpenTrendEmaTunables
//   indicator_params.ema_cross.period→open_trend_ema_period 回退
//                                  本文件:TestCRAMatrixParseIndicatorPeriodBackfill
//   indicator_params.bollinger.period/std/timeframe
//                                  cra_bollinger_test.go:TestParseCRAParamsBollinger + TestBollingerBandsStdScaling + TestBollingerConfirmed
//   indicator_params.rsi.period/oversold/overbought
//                                  解析 cra_open_gate_test.go:TestParseCRAParamsOpenGateTunables；
//                                  引擎阈值/数据不足不放行 本文件:TestCRAMatrixOpenRsiThresholdParams
//   indicator_params.range.period/neutral_band
//                                  解析 cra_open_gate_test.go:TestParseCRAParamsOpenGateTunables；
//                                  函数 ±10/±60 cra_open_gate_test.go:TestRSINeutralBandConfirmed；
//                                  引擎带宽 A/B 本文件:TestCRAMatrixOpenRangeBandParam
//   indicator_params.trend.period/ma_type
//                                  cra_open_gate_test.go:TestTrendMAConfirmed + TestCRASpotOpenGateTrend
//   indicator_params 校验（类型/正数/custom code_id+name）
//                                  cra_refine_test.go:TestParseCRAParamsIndicatorTunables（负数拒绝）等；
//                                  custom 合法化 本文件:TestCRAMatrixValidationGaps
//   timeframe（工作周期）           cra_refine_test.go:TestCRAPrimaryTimeframe / TestCRATimeframesDeclaration；
//                                  大小写归一 本文件:TestCRAMatrixTimeframeNormalization
//
// 补仓指标双闸（档级+全局，缺一不开）：
//   add_macd_enabled/add_macd_period
//                                  本文件:TestCRAMatrixAddMacdGate（金叉放行/自定义参数拦截/period=close 直通/关闭对照）；
//                                  订阅声明 cra_refine_test.go:TestCRATimeframesDeclaration
//   add_ema_enabled/add_ema_period cra_open_gate_test.go:TestAddPositionEmaDoubleGate
//
// 风控：
//   waterfall_enabled              触发/恢复/挡首单 cra_waterfall_gate_test.go:TestCRAWaterfallBlocksFirstOrderAnySymbol；
//                                  =false 无闸、暂停挡补仓 本文件:TestCRAMatrixWaterfallParams
//   waterfall_protection           阈值参数化（0.10 不触发 vs 0.02 触发）本文件:TestCRAMatrixWaterfallParams
//
// 止损（仅合约）：
//   stop_loss_enabled              触发 cra_reverse_exit_test.go:TestCRAReverseExitPriority；默认关闭全测试组共用前提
//   stop_loss_type=ratio           cra_test.go:TestCRAStateStopLoss + cra_reverse_exit_test.go:TestCRAReverseExitPriority
//   stop_loss_type=amount          本文件:TestCRAMatrixStopLossAmount（金额口径+0 护栏）
//   stop_loss_type=price           本文件:TestCRAMatrixStopLossPrice（多/空镜像+0 护栏）
//   stop_loss_ratio                cra_test.go:TestCRAStateStopLoss
//   stop_loss_amount/price         本文件对应两测试
//
// 反向出场（仅合约）：
//   reverse_take_profit_period     cra_reverse_exit_test.go:TestCRAReverseTakeProfitTriggers / NotTriggered / ShortMirror /
//                                  ExtendedPeriods / TimeframesDeclaration / Priority
//   reverse_stop_loss              cra_reverse_exit_test.go:TestCRAReverseStopLossTriggers / ShortMirror / NotTriggered
//
// 燃烧斩仓（仅合约）：
//   burn_global_enabled/threshold/close_ratio
//                                  cra_burn_test.go:TestCRABurnGlobalTriggersAtThreshold / TestCRABurnGlobalCloseRatioParam /
//                                  TestCRABurnThresholdGuards / TestCRABurnDisabledByDefault / TestCRABurnGlobalFiredRebuiltOnRestart
//   burn_dual_enabled/threshold    cra_burn_test.go:TestCRABurnDualTriggersAtThreshold / TestCRABurnOncePerLoopAndResetNextLoop /
//                                  TestCRABurnShortMirror / TestCRABurnRejectRearms / TestRebuildBurnMarksFromFills
//
// 合约扩展：
//   open_double                    cra_open_double_test.go 全组（Doubled/DisabledByDefault/AddLadderBaseUnchanged/SpotIgnored）
//   follow_trend                   cra_follow_trend_test.go 全组（Boost/CapFive/AnchorCleared/SameSide/Stacks/Gates/Restart）
//   online_order_limit             引擎不消费（2026-10-10 迁移风控中心，params.go 字段注释）；handler 闸
//                                  handler/cra_online_limit_test.go 全组；解析兼容 本文件:TestCRAMatrixParsePassThroughFields
//   leverage                       数量公式 cra_open_double_test.go:TestCRAOpenDoubleFirstOrderDoubled（×5）+
//                                  cra_manual_action_test.go:TestCRAManualAddPositionContractLeverage（×10）；
//                                  校验 1..150 cra_test.go:TestParseCRAParamsValidateBoundaries；
//                                  A/B 本文件:TestCRAMatrixFirstOrderQtyParams
//   direction long/short/dual      short：cra_reverse_exit_test.go 镜像组、cra_short_restore_test.go:TestCRARestoreShortEndToEnd、
//                                  cra_burn_test.go:TestCRABurnShortMirror；dual EMA20 选边
//                                  cra_warmup_test.go:TestCRAWarmupIndicatorEquivalence + cra_follow_trend_test.go 全组
//   market_type                    现货/合约引擎两分（全部现货/合约用例）；futures/margin 归合约校验 本文件:TestCRAMatrixValidationGaps
//   position_side/margin_mode/selected_exchanges
//                                  透传配置（引擎不消费，执行层/订单层读取）；解析+Params() 透出 本文件:TestCRAMatrixParsePassThroughFields
//
// 手动操控四件套（运行时开关，非 config 参数，参考）：
//   cra_manual_action_test.go 全组 + manual_action.go。

// ── 开仓/基础 ──

// first_order_multiplier / leverage / first_order_amount 进首单数量公式
// （合约 qty=金额×倍数×杠杆/价；现货 qty=金额×倍数/价，杠杆不消费）。
func TestCRAMatrixFirstOrderQtyParams(t *testing.T) {
	base := firstOrderQty(t, nil) // 10U×1×5杠杆 @100
	if base != 0.5 {
		t.Fatalf("baseline qty = %v, want 0.5", base)
	}
	if got := firstOrderQty(t, map[string]any{"first_order_multiplier": 2}); got != 1.0 {
		t.Fatalf("multiplier=2 qty = %v, want 1.0（倍数进首单）", got)
	}
	if got := firstOrderQty(t, map[string]any{"leverage": 10}); got != 1.0 {
		t.Fatalf("leverage=10 qty = %v, want 1.0（杠杆进首单）", got)
	}
	if got := firstOrderQty(t, map[string]any{"first_order_amount": 20}); got != 1.0 {
		t.Fatalf("amount=20 qty = %v, want 1.0（金额进首单）", got)
	}

	// 现货：qty=金额×倍数/价；杠杆不消费（残留大杠杆不改变现货数量）。
	spotQty := func(extra map[string]any) float64 {
		t.Helper()
		params := map[string]any{
			"symbol":              "BTCUSDT",
			"first_order_amount":  100,
			"tp_mode":             "static",
			"take_profit_ratio":   0.5,
			"enable_add_position": false,
		}
		for k, v := range extra {
			params[k] = v
		}
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(params); err != nil {
			t.Fatalf("start: %v", err)
		}
		sig, err := s.OnBar(bar(100), nil)
		if err != nil || sig == nil {
			t.Fatalf("spot first order: sig=%v err=%v", sig, err)
		}
		return sig.Qty
	}
	if got := spotQty(nil); got != 1.0 {
		t.Fatalf("spot baseline qty = %v, want 1.0", got)
	}
	if got := spotQty(map[string]any{"first_order_multiplier": 3}); got != 3.0 {
		t.Fatalf("spot multiplier=3 qty = %v, want 3.0", got)
	}
	if got := spotQty(map[string]any{"leverage": 50}); got != 1.0 {
		t.Fatalf("spot leverage must be ignored: qty = %v, want 1.0", got)
	}
}

// first_order_price 空头镜像：direction=short 时挂单价是"涨到才开空"——
// 价格 ≤ 挂单价不开仓（与多头"跌到才开"镜像），价格 > 挂单价开 SHORT。
func TestCRAMatrixFirstOrderPriceShortGate(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"direction":         "short",
		"first_order_price": 100,
	})
	if sig, _ := s.OnBar(bar(99), nil); sig != nil {
		t.Fatalf("short entry at 99 ≤ pending 100 must be blocked: %+v", sig)
	}
	if sig, _ := s.OnBar(bar(100), nil); sig != nil {
		t.Fatalf("short entry at 100 ≤ pending 100 must be blocked: %+v", sig)
	}
	sig, err := s.OnBar(bar(101), nil)
	if err != nil || sig == nil {
		t.Fatalf("short entry above pending price: sig=%v err=%v", sig, err)
	}
	if sig.Direction != "SHORT" {
		t.Fatalf("direction = %v, want SHORT", sig.Direction)
	}

	// 对照：不配挂单价（0=市价）首根即开空。
	s2 := startReverseContract(t, map[string]any{"direction": "short"})
	if sig, _ := s2.OnBar(bar(99), nil); sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("market first order = %+v, want SHORT", sig)
	}
}

// trade_count_mode=cycle + loop_count 上限：循环次数用尽后不再开新循环；
// loop_count=3 对照放行第三轮。
func TestCRAMatrixCycleLoopCountLimit(t *testing.T) {
	start := func(loopCount int) *BaseCRAStrategy {
		t.Helper()
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":              "BTCUSDT",
			"first_order_amount":  100,
			"tp_mode":             "static",
			"take_profit_ratio":   0.02,
			"profit_callback":     0,
			"enable_add_position": false,
			"trade_count_mode":    "cycle",
			"loop_count":          loopCount,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	runLoop := func(s *BaseCRAStrategy) {
		t.Helper()
		sig, err := s.OnBar(bar(100), nil)
		if err != nil || sig == nil || sig.Direction != "LONG" {
			t.Fatalf("loop entry: sig=%+v err=%v", sig, err)
		}
		fillBuy(t, s, 100, sig.Qty)
		sig, _ = s.OnBar(bar(102.1), nil)
		if sig == nil || sig.Direction != "CLOSE" {
			t.Fatalf("loop take profit: sig=%+v", sig)
		}
	}

	s := start(2)
	runLoop(s) // 循环 1
	runLoop(s) // 循环 2
	if st := s.RuntimeStatus(); st["loops_executed"] != 2 {
		t.Fatalf("loops_executed = %v, want 2", st["loops_executed"])
	}
	if sig, _ := s.OnBar(bar(100), nil); sig != nil {
		t.Fatalf("cycle loop_count=2 exhausted, third entry must be blocked: %+v", sig)
	}

	// 对照：loop_count=3 放行第三轮。
	s3 := start(3)
	runLoop(s3)
	runLoop(s3)
	if sig, _ := s3.OnBar(bar(100), nil); sig == nil {
		t.Fatal("cycle loop_count=3 must allow the third loop")
	}
}

// enable_add_position=false：跌穿差价+反弹也不补仓（首单分支不受影响）；
// true 对照同剧本触发补仓 #1。
func TestCRAMatrixEnableAddPositionSwitch(t *testing.T) {
	start := func(enabled bool) *BaseCRAStrategy {
		t.Helper()
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":              "BTCUSDT",
			"first_order_amount":  100,
			"tp_mode":             "static",
			"take_profit_ratio":   0.5,
			"profit_callback":     0,
			"enable_add_position": enabled,
			"order_count":         1,
			"add_positions": []any{
				map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			},
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	drive := func(s *BaseCRAStrategy) *model.Signal {
		t.Helper()
		enterLong(t, s, 100)
		if sig, _ := s.OnBar(bar(96.5), nil); sig != nil {
			t.Fatalf("arm bar: unexpected signal %+v", sig)
		}
		sig, _ := s.OnBar(bar(96.8), nil)
		return sig
	}

	on := drive(start(true))
	if on == nil || !strings.Contains(on.Reason, "cra add position #1") {
		t.Fatalf("enabled: add #1 must fire after bounce, got %+v", on)
	}
	if want := RoundQty(100 / 96.8); on.Qty != want {
		t.Fatalf("enabled: add qty = %v, want %v", on.Qty, want)
	}

	off := start(false)
	if sig := drive(off); sig != nil {
		t.Fatalf("enable_add_position=false must suppress adds: %+v", sig)
	}
	if off.state.PendingAdd {
		t.Fatal("enable_add_position=false must not even arm the add callback")
	}
}

// ── 止盈 ──

// profit_callback（静态止盈回调）：达线后须自盈利峰值回撤 ≥callback 才触发；
// callback=0 对照达线即触发。
func TestCRAMatrixStaticProfitCallback(t *testing.T) {
	start := func(callback float64) *BaseCRAStrategy {
		t.Helper()
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":              "BTCUSDT",
			"first_order_amount":  100,
			"tp_mode":             "static",
			"take_profit_method":  "full",
			"take_profit_ratio":   0.01,
			"profit_callback":     callback,
			"enable_add_position": false,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}

	s := start(0.005)
	enterLong(t, s, 100)
	if sig, _ := s.OnBar(bar(102.0), nil); sig != nil {
		t.Fatalf("profit 2%% ≥1%% but no drawback yet, must not fire: %+v", sig)
	}
	if sig, _ := s.OnBar(bar(101.9), nil); sig != nil {
		t.Fatalf("drawback 0.1%% < 0.5%%, must not fire: %+v", sig)
	}
	// 峰值 2.0%，现盈利 1.4%（仍 ≥1%）：回撤 0.6% ≥ 0.5% → 全仓止盈。
	sig, _ := s.OnBar(bar(101.4), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 || sig.Reason != "cra take profit" {
		t.Fatalf("static callback tp = %+v, want full CLOSE", sig)
	}
	if s.state.InPosition || s.state.LoopExecuted != 1 {
		t.Fatalf("callback tp must close the loop: %+v", s.state)
	}

	// 对照：callback=0 达线（+1.5%）即触发。
	s0 := start(0)
	enterLong(t, s0, 100)
	if sig, _ := s0.OnBar(bar(101.5), nil); sig == nil || sig.Reason != "cra take profit" {
		t.Fatalf("callback=0 must fire at ratio line: %+v", sig)
	}
}

// ── 止损（仅合约）──

// stop_loss_type=amount：浮亏金额 = 亏损比例×持仓成本，达 stop_loss_amount 触发；
// 金额未到不触发；amount=0 护栏不触发。
func TestCRAMatrixStopLossAmount(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"stop_loss_enabled": true,
		"stop_loss_type":    "amount",
		"stop_loss_amount":  5,
	})
	seedLongPosition(s, 100, 0.5) // 成本 50U
	if sig, _ := s.OnBar(bar(95.1), nil); sig != nil {
		t.Fatalf("loss 2.45U < 5U must not fire: %+v", sig)
	}
	sig, _ := s.OnBar(bar(90), nil) // 亏 10%×50=5U 达线
	if sig == nil || sig.Direction != "CLOSE" || sig.Reason != "cra stop loss" {
		t.Fatalf("amount stop loss = %+v, want cra stop loss CLOSE", sig)
	}
	if s.state.InPosition || s.state.LoopExecuted != 1 {
		t.Fatalf("stop loss must close the loop: %+v", s.state)
	}

	// 金额口径确实生效：amount=6 同剧本不触发。
	s2 := startReverseContract(t, map[string]any{
		"stop_loss_enabled": true,
		"stop_loss_type":    "amount",
		"stop_loss_amount":  6,
	})
	seedLongPosition(s2, 100, 0.5)
	if sig, _ := s2.OnBar(bar(90), nil); sig != nil {
		t.Fatalf("loss 5U < 6U must not fire: %+v", sig)
	}

	// 护栏：amount=0（未配置）永不触发。
	s3 := startReverseContract(t, map[string]any{
		"stop_loss_enabled": true,
		"stop_loss_type":    "amount",
	})
	seedLongPosition(s3, 100, 0.5)
	if sig, _ := s3.OnBar(bar(80), nil); sig != nil {
		t.Fatalf("amount=0 guard must not fire: %+v", sig)
	}
}

// stop_loss_type=price：多头跌到 stop_loss_price 触发、空头涨到触发（镜像）；
// price=0 护栏不触发。
func TestCRAMatrixStopLossPrice(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"stop_loss_enabled": true,
		"stop_loss_type":    "price",
		"stop_loss_price":   95,
	})
	seedLongPosition(s, 100, 0.5)
	if sig, _ := s.OnBar(bar(96), nil); sig != nil {
		t.Fatalf("96 > stop price 95 must not fire: %+v", sig)
	}
	sig, _ := s.OnBar(bar(95), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Reason != "cra stop loss" {
		t.Fatalf("price stop loss (long) = %+v, want cra stop loss CLOSE", sig)
	}

	// 空头镜像：价格涨到 105 触发。
	ss := startReverseContract(t, map[string]any{
		"direction":         "short",
		"stop_loss_enabled": true,
		"stop_loss_type":    "price",
		"stop_loss_price":   105,
	})
	ss.state.EnterPosition(100, SideShort)
	ss.state.RecordFill(100, 0.1, SideBuy)
	ss.state.PositionCount = 1
	if sig, _ := ss.OnBar(bar(104), nil); sig != nil {
		t.Fatalf("104 < stop price 105 (short) must not fire: %+v", sig)
	}
	sig, _ = ss.OnBar(bar(105), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Reason != "cra stop loss" {
		t.Fatalf("price stop loss (short) = %+v, want cra stop loss CLOSE", sig)
	}

	// 护栏：price=0（未配置）永不触发。
	s3 := startReverseContract(t, map[string]any{
		"stop_loss_enabled": true,
		"stop_loss_type":    "price",
	})
	seedLongPosition(s3, 100, 0.5)
	if sig, _ := s3.OnBar(bar(50), nil); sig != nil {
		t.Fatalf("price=0 guard must not fire: %+v", sig)
	}
}

// ── 开仓指标参数进引擎 ──

// indicator_params.rsi 阈值/周期进引擎：同一超卖反弹序列（RSI 0→≈51.9），
// oversold=30 放行开多、oversold=55 拦截；period=25 数据不足（需 27 根）不放行。
func TestCRAMatrixOpenRsiThresholdParams(t *testing.T) {
	start := func(period, oversold int) *BaseCRAStrategy {
		t.Helper()
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":             "BTCUSDT",
			"timeframe":          "15m",
			"tp_mode":            "static",
			"take_profit_ratio":  0.5,
			"first_order_amount": 100,
			"open_indicator":     "rsi",
			"indicator_params":   map[string]any{"rsi": map[string]any{"period": period, "oversold": oversold, "overbought": 70}},
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	series := rsiReboundSeries()
	if sig := feedBars(t, start(14, 30), series...); sig == nil || sig.Direction != "LONG" {
		t.Fatalf("rsi oversold=30 must open on rebound, got %+v", sig)
	}
	if sig := feedBars(t, start(14, 55), series...); sig != nil {
		t.Fatalf("rsi oversold=55 must block (rebound only reaches ≈51.9), got %+v", sig)
	}
	if sig := feedBars(t, start(25, 30), series...); sig != nil {
		t.Fatalf("rsi period=25 with 20 bars must not open (insufficient data), got %+v", sig)
	}
}

// indicator_params.range.neutral_band 进引擎：一路阴跌序列 RSI=0，
// ±10 拦截、±60 放行（带宽参数决定开不开仓）。
func TestCRAMatrixOpenRangeBandParam(t *testing.T) {
	start := func(band int) *BaseCRAStrategy {
		t.Helper()
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":             "BTCUSDT",
			"timeframe":          "15m",
			"tp_mode":            "static",
			"take_profit_ratio":  0.5,
			"first_order_amount": 100,
			"open_indicator":     "range",
			"indicator_params":   map[string]any{"range": map[string]any{"period": 14, "neutral_band": band}},
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	declining := make([]float64, 20)
	p := 200.0
	for i := range declining {
		declining[i] = p
		p--
	}
	if sig := feedBars(t, start(10), declining...); sig != nil {
		t.Fatalf("range ±10 must block RSI=0, got %+v", sig)
	}
	if sig := feedBars(t, start(60), declining...); sig == nil || sig.Direction != "LONG" {
		t.Fatalf("range ±60 must open with RSI=0 inside band, got %+v", sig)
	}
}

// indicator_params.ema_cross.fast/slow 进开仓顺势 EMA 门槛：同一"阴跌后弱反弹"
// 序列，默认 5/15（快线仍压慢线之下）拦截，自定义 2/5（快线已上慢线且收盘站
// 稳）放行——参数确实进入引擎判定。
func TestCRAMatrixOpenTrendEmaTunables(t *testing.T) {
	start := func(extra map[string]any) *BaseCRAStrategy {
		t.Helper()
		cfg := map[string]any{
			"symbol":                   "BTCUSDT",
			"timeframe":                "15m",
			"direction":                "long",
			"tp_mode":                  "static",
			"take_profit_ratio":        0.5,
			"open_trend_ema_enabled":   true,
			"open_trend_ema_period":    "15m",
			"first_order_amount":       100,
			"first_order_multiplier":   1,
			"enable_add_position":      false,
			"stop_loss_enabled":        false,
			"waterfall_enabled":        false,
			"trade_count_mode":         "cycle",
			"take_profit_method":       "full",
			"reverse_stop_loss":        false,
			"open_macd_enabled":        false,
			"open_counter_ema_enabled": false,
			"open_bollinger_enabled":   false,
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
	dip := trendDipRallySeries()
	if sig := feedBars(t, start(nil), dip...); sig != nil {
		t.Fatalf("default EMA 5/15 must block long on dip rally (fast below slow), got %+v", sig)
	}
	sig := feedBars(t, start(map[string]any{
		"indicator_params": map[string]any{"ema_cross": map[string]any{"fast": 2, "slow": 5}},
	}), dip...)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatalf("custom EMA 2/5 must confirm long on dip rally, got %+v", sig)
	}
}

// open_indicator=custom 引擎接线：未配置 code_id 视为无门槛直通；配置 code_id
// 后沙箱/DB 不可用时按风控开关 fail-open（默认）放行、fail-close 拦截。
func TestCRAMatrixCustomIndicatorGate(t *testing.T) {
	start := func(extra map[string]any) *BaseCRAStrategy {
		t.Helper()
		cfg := map[string]any{
			"symbol":             "BTCUSDT",
			"first_order_amount": 100,
			"tp_mode":            "static",
			"take_profit_ratio":  0.5,
			"open_indicator":     "custom",
		}
		for k, v := range extra {
			cfg[k] = v
		}
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(cfg); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}

	// 无 indicator_params.custom：直通开仓。
	if sig, _ := start(nil).OnBar(bar(100), nil); sig == nil {
		t.Fatal("custom without code_id must pass through (no gate)")
	}

	withCode := map[string]any{"indicator_params": map[string]any{
		"custom": map[string]any{"code_id": 1, "name": "t"},
	}}
	// 有 code_id、沙箱/DB 不可用（测试进程未初始化 store）：默认 fail-open 放行。
	if sig, _ := start(withCode).OnBar(bar(100), nil); sig == nil {
		t.Fatal("custom gate failure must fail-open by default")
	}
	// fail-close：同一故障拦截开仓（证明 custom 门槛真实接线，而非静默跳过）。
	risk.SetIndicatorFailOpen(false)
	t.Cleanup(func() { risk.SetIndicatorFailOpen(true) })
	if sig, _ := start(withCode).OnBar(bar(100), nil); sig != nil {
		t.Fatalf("custom gate failure must block entry when fail-close, got %+v", sig)
	}
}

// ── 补仓 MACD 双闸（全局开关+周期，indicator_params.macd  tunables 进引擎）──

// add_macd_enabled：阴跌后反弹序列（价格形态满足补仓差价+回调）上——
// 默认 MACD(12,26,9) 末根金叉 → 补仓放行（恰好 1 笔、发生在末根）；
// indicator_params.macd={3,6,2} 时金叉提前一根、末根不金叉 → 全程拦截；
// add_macd_period=close → 门槛直通放行；不加 add_macd 键（关闭）→ 同价放行。
func TestCRAMatrixAddMacdGate(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"symbol":              "BTCUSDT",
			"timeframe":           "15m",
			"direction":           "long",
			"first_order_amount":  10,
			"leverage":            5,
			"tp_mode":             "static",
			"take_profit_ratio":   0.5,
			"profit_callback":     0,
			"stop_loss_enabled":   false,
			"enable_add_position": true,
			"order_count":         1,
			"add_positions": []any{
				map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			},
		}
	}
	drive := func(extra map[string]any) []*model.Signal {
		t.Helper()
		params := base()
		for k, v := range extra {
			params[k] = v
		}
		s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
		if err := s.Start(params); err != nil {
			t.Fatalf("start: %v", err)
		}
		seedLongPosition(s, 100, 0.5) // 直挂首单档，EntryPrice=100
		var sigs []*model.Signal
		for _, c := range decliningThenCrossSeries() {
			sig, err := s.OnBar(bar(c), nil)
			if err != nil {
				t.Fatalf("onbar: %v", err)
			}
			if sig != nil {
				sigs = append(sigs, sig)
			}
		}
		return sigs
	}

	// 开启+默认参数：末根金叉确认，补仓 #1 触发。
	on := drive(map[string]any{"add_macd_enabled": true, "add_macd_period": "15m"})
	if len(on) != 1 || !strings.Contains(on[0].Reason, "cra add position #1") {
		t.Fatalf("add macd gate on = %+v, want exactly one add #1", on)
	}
	if want := RoundQty(10 * 1 * 5 / 84.6); on[0].Qty != want {
		t.Fatalf("add qty = %v, want %v", on[0].Qty, want)
	}

	// 开启+自定义 tunables（3,6,2）：金叉发生在倒数第二根，末根门槛拦截 → 零信号。
	custom := drive(map[string]any{
		"add_macd_enabled": true,
		"add_macd_period":  "15m",
		"indicator_params": map[string]any{"macd": map[string]any{"fast": 3, "slow": 6, "signal": 2}},
	})
	if len(custom) != 0 {
		t.Fatalf("custom MACD tunables must block the add at last bar, got %+v", custom)
	}

	// 开启+period=close：不额外监测，直通放行。
	closeP := drive(map[string]any{"add_macd_enabled": true, "add_macd_period": "close"})
	if len(closeP) != 1 {
		t.Fatalf("add macd period=close must pass through, got %d signals", len(closeP))
	}

	// 关闭（默认不加键）：价格形态照常触发（对照，证明价格剧本本身到达触发点）。
	off := drive(nil)
	if len(off) != 1 {
		t.Fatalf("gate off control must fire the add, got %d signals", len(off))
	}
}

// ── 防瀑布（waterfall）──

// waterfall_enabled=false 不建检测器（暴跌 tick 不产生暂停）；waterfall_protection
// 阈值参数化（-3% 在 0.10 下不触发、0.02 下触发）；暂停期补仓分支同样被挡，
// 恢复后补仓照常触发。
func TestCRAMatrixWaterfallParams(t *testing.T) {
	barAt := model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 97, High: 97, Low: 97}
	crashTicks := []struct {
		price float64
		ts    int64
	}{{100, 1000}, {99, 1010}, {97, 1020}}
	feedCrash := func(s *BaseCRAStrategy) {
		t.Helper()
		for _, tk := range crashTicks {
			if _, err := s.OnTick(model.Tick{Symbol: "BTCUSDT", Last: tk.price, Timestamp: tk.ts}, nil); err != nil {
				t.Fatalf("ontick: %v", err)
			}
		}
	}
	startSpot := func(extra map[string]any) *BaseCRAStrategy {
		t.Helper()
		cfg := map[string]any{
			"symbol":              "BTCUSDT",
			"first_order_amount":  100,
			"tp_mode":             "static",
			"take_profit_ratio":   0.5,
			"profit_callback":     0,
			"enable_add_position": true,
			"order_count":         1,
			"add_positions": []any{
				map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			},
		}
		for k, v := range extra {
			cfg[k] = v
		}
		s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
		if err := s.Start(cfg); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}

	t.Run("disabled builds no detector", func(t *testing.T) {
		s := startSpot(map[string]any{"waterfall_enabled": false})
		feedCrash(s)
		if s.state.WaterfallPaused {
			t.Fatal("waterfall_enabled=false must never pause")
		}
		if sig, _ := s.OnBar(barAt, nil); sig == nil {
			t.Fatal("waterfall disabled: entry must proceed after a crash")
		}
	})

	t.Run("protection threshold parameterizes the trigger", func(t *testing.T) {
		// -3% < 10% 阈值：不暂停，照常开仓。
		slack := startSpot(map[string]any{"waterfall_enabled": true, "waterfall_protection": 0.10})
		feedCrash(slack)
		if slack.state.WaterfallPaused {
			t.Fatal("-3% drop must not trip a 10% waterfall threshold")
		}
		if sig, _ := slack.OnBar(barAt, nil); sig == nil {
			t.Fatal("under-threshold drop: entry must proceed")
		}
		// 同剧本 2% 阈值：暂停并挡首单。
		tight := startSpot(map[string]any{"waterfall_enabled": true, "waterfall_protection": 0.02})
		feedCrash(tight)
		if !tight.state.WaterfallPaused {
			t.Fatal("-3% drop must trip a 2% waterfall threshold")
		}
		if sig, _ := tight.OnBar(barAt, nil); sig != nil {
			t.Fatalf("paused strategy must block first order: %+v", sig)
		}
	})

	t.Run("pause blocks add positions until resume", func(t *testing.T) {
		s := startSpot(map[string]any{"waterfall_enabled": true, "waterfall_protection": 0.02})
		enterLong(t, s, 100)
		feedCrash(s)
		if !s.state.WaterfallPaused {
			t.Fatal("crash must pause")
		}
		// 暂停期：差价+反弹形态齐备也不补仓（连挂起都不发生）。
		if sig, _ := s.OnBar(bar(96.5), nil); sig != nil {
			t.Fatalf("paused add arm must be silent: %+v", sig)
		}
		if sig, _ := s.OnBar(bar(96.8), nil); sig != nil {
			t.Fatalf("paused add trigger must be silent: %+v", sig)
		}
		if s.state.PendingAdd {
			t.Fatal("paused add branch must not arm the callback state")
		}
		// 恢复（旧点出窗、新窗跌幅<阈值一半）：同形态重新挂起并触发补仓 #1。
		for i, p := range []float64{97, 97.2, 97.1} {
			if _, err := s.OnTick(model.Tick{Symbol: "BTCUSDT", Last: p, Timestamp: int64(1200 + i*10)}, nil); err != nil {
				t.Fatalf("ontick resume: %v", err)
			}
		}
		if s.state.WaterfallPaused {
			t.Fatal("recovered window must resume")
		}
		if sig, _ := s.OnBar(bar(96.5), nil); sig != nil {
			t.Fatalf("post-resume arm bar: unexpected signal %+v", sig)
		}
		sig, _ := s.OnBar(bar(96.8), nil)
		if sig == nil || !strings.Contains(sig.Reason, "cra add position #1") {
			t.Fatalf("post-resume add must fire: %+v", sig)
		}
	})
}

// ── 解析/校验层 ──

// indicator_params 子键 period 回退旧 open_*_period 键：macd.period→
// open_macd_period、ema_cross.period→open_trend_ema_period；显式旧键优先；
// "close" 不回退。
func TestCRAMatrixParseIndicatorPeriodBackfill(t *testing.T) {
	p, err := ParseCRAParams(`{"indicator_params":{"macd":{"period":"1h"},"ema_cross":{"period":"30m"}}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.OpenMacdPeriod != "1h" || p.OpenTrendEmaPeriod != "30m" {
		t.Fatalf("period backfill = macd:%q ema:%q, want 1h/30m", p.OpenMacdPeriod, p.OpenTrendEmaPeriod)
	}

	// 显式旧键优先，子键不覆盖。
	p2, err := ParseCRAParams(`{
		"open_macd_period": "4h", "open_trend_ema_period": "8h",
		"indicator_params": {"macd": {"period": "1h"}, "ema_cross": {"period": "30m"}}
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p2.OpenMacdPeriod != "4h" || p2.OpenTrendEmaPeriod != "8h" {
		t.Fatalf("explicit old keys must win = macd:%q ema:%q, want 4h/8h", p2.OpenMacdPeriod, p2.OpenTrendEmaPeriod)
	}

	// 子键 period=close（监测关闭）不回退。
	p3, err := ParseCRAParams(`{"indicator_params":{"macd":{"period":"close"}}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p3.OpenMacdPeriod != "close" {
		t.Fatalf("close period must not backfill, got %q", p3.OpenMacdPeriod)
	}
}

// 校验边界补缺口：trade_count_mode 非法、移动止盈档数 ≠4、order_count 越界、
// first_order_multiplier 越界、market_type futures/margin 归合约杠杆校验；
// indicator_params.custom 合法化（code_id+name 必填）。
func TestCRAMatrixValidationGaps(t *testing.T) {
	tiers4 := `"moving_take_profit_tiers":[{"ratio":0.02,"drawback":0.2},{"ratio":0.03,"drawback":0.2},{"ratio":0.04,"drawback":0.1},{"ratio":0.05,"drawback":0.1}]`
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"trade_count_mode invalid", `{"trade_count_mode":"forever","tp_mode":"static"}`, true},
		{"trade_count_mode single", `{"trade_count_mode":"single","tp_mode":"static"}`, false},
		{"moving tiers 3 rejected", `{"tp_mode":"moving","moving_take_profit_tiers":[{"ratio":0.02,"drawback":0.2}]}`, true},
		{"moving tiers 4 accepted", `{"tp_mode":"moving",` + tiers4 + `}`, false},
		{"order_count 21 rejected", `{"order_count":21,"tp_mode":"static"}`, true},
		{"order_count 0 accepted", `{"order_count":0,"tp_mode":"static"}`, false},
		{"multiplier 0.5 rejected", `{"first_order_multiplier":0.5,"tp_mode":"static"}`, true},
		{"multiplier 11 rejected", `{"first_order_multiplier":11,"tp_mode":"static"}`, true},
		{"multiplier 10 accepted", `{"first_order_multiplier":10,"tp_mode":"static"}`, false},
		// futures/margin 同属合约：杠杆上限校验生效（IsContract 分支真实路由）。
		{"futures leverage 200 rejected", `{"market_type":"futures","leverage":200,"first_order_amount":10,"tp_mode":"static"}`, true},
		{"futures leverage 50 accepted", `{"market_type":"futures","leverage":50,"first_order_amount":10,"tp_mode":"static"}`, false},
		{"margin leverage 151 rejected", `{"market_type":"margin","leverage":151,"first_order_amount":10,"tp_mode":"static"}`, true},
		{"spot leverage ignored", `{"market_type":"spot","leverage":999,"first_order_amount":10,"tp_mode":"static"}`, false},
	}
	for _, tc := range cases {
		p, err := ParseCRAParams(tc.raw)
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if err := p.Validate(); (err != nil) != tc.wantErr {
			t.Errorf("%s: validate err = %v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}

	// indicator_params.custom 合法化：缺 name / 缺 code_id / 非对象均拒绝。
	for _, bad := range []string{
		`{"indicator_params":{"custom":{"code_id":1}}}`,
		`{"indicator_params":{"custom":{"name":"x"}}}`,
		`{"indicator_params":{"custom":"notobject"}}`,
		`{"indicator_params":"notobject"}`,
	} {
		if _, err := ParseCRAParams(bad); err == nil {
			t.Errorf("expected parse error for %s", bad)
		}
	}
	if _, err := ParseCRAParams(`{"indicator_params":{"custom":{"code_id":1,"name":"x"}}}`); err != nil {
		t.Errorf("custom with code_id+name must parse: %v", err)
	}
}

// 透传字段解析与引擎透出：position_side/margin_mode/selected_exchanges 引擎不
// 消费（执行层读取），但解析必须忠实、Params() 必须原样透出；online_order_limit
// 为迁移遗留键——解析保留（结构兼容），引擎不做限额（限额闸在 handler 层，
// handler/cra_online_limit_test.go 全组锁定）。
func TestCRAMatrixParsePassThroughFields(t *testing.T) {
	p, err := ParseCRAParams(`{
		"position_side": "BOTH", "margin_mode": "isolated",
		"selected_exchanges": ["binance", "bybit"],
		"online_order_limit": 3, "loop_count": 7
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.PositionSide != "BOTH" || p.MarginMode != "isolated" {
		t.Fatalf("pass-through = %q/%q, want BOTH/isolated", p.PositionSide, p.MarginMode)
	}
	if len(p.SelectedExchanges) != 2 || p.SelectedExchanges[0] != "binance" || p.SelectedExchanges[1] != "bybit" {
		t.Fatalf("selected_exchanges = %v", p.SelectedExchanges)
	}
	if p.OnlineOrderLimit != 3 {
		t.Fatalf("legacy online_order_limit parse = %d, want 3（结构兼容保留）", p.OnlineOrderLimit)
	}
	if p.LoopCount != 7 {
		t.Fatalf("loop_count parse = %d, want 7", p.LoopCount)
	}

	// 引擎透出面：Start 后 Params() 原样携带（前端/执行层的读取通道）。
	s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"tp_mode":             "static",
		"position_side":       "BOTH",
		"margin_mode":         "isolated",
		"selected_exchanges":  []string{"binance", "bybit"},
		"online_order_limit":  3,
		"first_order_amount":  100,
		"take_profit_ratio":   0.5,
		"enable_add_position": false,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	m := s.Params()
	if m["position_side"] != "BOTH" || m["margin_mode"] != "isolated" {
		t.Fatalf("Params() pass-through = %v/%v", m["position_side"], m["margin_mode"])
	}
	ex, ok := m["selected_exchanges"].([]any)
	if !ok || len(ex) != 2 || ex[0] != "binance" || ex[1] != "bybit" {
		t.Fatalf("Params() selected_exchanges = %v", m["selected_exchanges"])
	}
	if m["online_order_limit"] != float64(3) {
		t.Fatalf("Params() online_order_limit = %v, want 3", m["online_order_limit"])
	}
}

// add_positions 档级 EMA 开关的 "ema" 别名解析（存量配置键）：ema_enabled 缺省/
// false 时回退读 ema。
func TestCRAMatrixParseAddPositionEmaAlias(t *testing.T) {
	p, err := ParseCRAParams(`{"enable_add_position":true,"order_count":2,"add_positions":[
		{"order":1,"multiplier":1,"spread":0.03,"callback":0.003,"ema":true},
		{"order":2,"multiplier":1,"spread":0.04,"callback":0.003,"ema_enabled":true,"ema":false}
	]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ap := p.AddPositionForOrder(1); ap == nil || !ap.EmaEnabled {
		t.Fatalf("order1 ema alias = %+v, want EmaEnabled true", ap)
	}
	if ap := p.AddPositionForOrder(2); ap == nil || !ap.EmaEnabled {
		t.Fatalf("order2 ema_enabled = %+v, want EmaEnabled true", ap)
	}
	// 两键都缺：默认关闭。
	p2, err := ParseCRAParams(`{"enable_add_position":true,"order_count":1,"add_positions":[
		{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}
	]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ap := p2.AddPositionForOrder(1); ap == nil || ap.EmaEnabled {
		t.Fatalf("default tier ema = %+v, want false", ap)
	}
}

// timeframe 归一化：大写/空白输入统一小写去空白；与工作周期相同的指标周期
// （归一后相等）不产生副周期订阅。
func TestCRAMatrixTimeframeNormalization(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":            "BTCUSDT",
		"timeframe":         " 15M ",
		"tp_mode":           "static",
		"open_macd_enabled": true,
		"open_macd_period":  "15m",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if tf := s.PrimaryTimeframe(); tf != "15m" {
		t.Fatalf("PrimaryTimeframe = %q, want normalized 15m", tf)
	}
	if tfs := s.Timeframes(); len(tfs) != 0 {
		t.Fatalf("indicator period == working timeframe after normalization: Timeframes = %v, want empty", tfs)
	}
}
