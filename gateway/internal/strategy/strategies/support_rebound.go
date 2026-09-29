package strategies

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── SupportReboundStrategy（支撑回踩反弹，现货只做多）────────────────
//
// 四步入场逻辑（默认 4h 工作 K 线，每根闭合 bar 在 OnBar 推进状态机）：
//  1. 支撑位识别：lookback_bars 窗口内最低价 minLow，支撑带 = minLow×(1±tol)，
//     窗口内 Low 落入支撑带的 bar 数（含当前）≥ min_support_touches → 支撑确认；
//  2. 暴跌检测（放量恐慌）：crash_lookback_bars 窗口内 refHigh，最低那根 bar
//     回撤 (refHigh−low)/refHigh ≥ crash_drop_pct 且其成交量 ≥
//     volSMA(volume_sma_period)×volume_spike_mult → 进入 ARMED；
//  3. 反弹确认：armed 后 Close ≥ crashLow×(1+rebound_pct) → reboundOK；
//  4. 回踩确认：reboundOK 后 pullback_bars 根内出现 Low ≤ supportTop 且
//     Close ≥ supportBottom（下影探支撑、收盘守稳）→ pullbackOK；
//  5. 入场：pullbackOK 后首根放量阳线（Close>Open 且 Volume ≥
//     volSMA×entry_volume_mult）→ 产生 long 信号，随后进入持仓管理。
//
// armed 期间（含反弹/回踩子态）若 Close < supportBottom → 支撑失效，
// 回 IDLE 重新找支撑。持仓管理（OnBar/OnTick）：触止损/触目标/超时离场，
// 浮盈 ≥ rebound_pct 后止损上移到 max(stop, entry×1.002) 保本。

// 支撑回踩反弹状态机状态。
const (
	srStateIdle     = "IDLE"     // 预热完成，寻找支撑位
	srStateSupport  = "SUPPORT"  // 支撑确认，等待放量暴跌
	srStateArmed    = "ARMED"    // 暴跌确认（放量恐慌），等待反弹
	srStateRebound  = "REBOUND"  // 反弹确认，等待回踩支撑
	srStatePullback = "PULLBACK" // 回踩确认，等待放量阳线入场
	srStatePosition = "POSITION" // 持仓中（IN_POSITION）
)

// supportReboundMaxBars 内存保留的 K 线上限：覆盖最大 lookback(300)
// + 量能均线周期(60) + 余量，防止窗口被截断。
const supportReboundMaxBars = 400

type SupportReboundStrategy struct {
	strategy.BaseStrategy
	name      string
	symbol    string
	timeframe string
	running   bool
	mu        sync.RWMutex

	// ── 参数（ParamRegistry 注册，ApplyParams 从 map 同步）──
	lookbackBars             int     // 支撑观察窗口（默认 120×4h≈20 天）
	supportTouchTolerancePct float64 // 支撑带容差 ±1.5%
	minSupportTouches        int     // 最少触及次数
	crashLookbackBars        int     // 暴跌检测窗口（默认 6×4h=24h）
	crashDropPct             float64 // 暴跌幅度 ≥12%
	volumeSMAPeriod          int     // 量能均线周期
	volumeSpikeMult          float64 // 异常量倍数（暴跌 K）
	reboundPct               float64 // 自低点反弹确认幅度
	pullbackBars             int     // 回踩有效窗口（根）
	entryVolumeMult          float64 // 入场放量倍数
	stopBufferPct            float64 // 止损缓冲（支撑下沿再让 2%）
	takeProfitPct            float64 // 目标涨幅上限
	positionSize             float64 // 每单 USDT
	maxHoldBars              int     // 超时离场（根）

	bars []model.Bar

	// ── 状态机 ──
	state         string
	supportBottom float64 // 支撑带下沿 = minLow×(1−tol)
	supportTop    float64 // 支撑带上沿 = minLow×(1+tol)
	minLow        float64 // 窗口内最低价
	crashLow      float64 // 暴跌窗口最低 Low
	crashRef      float64 // 暴跌窗口参考最高价
	refBarIdx     int     // 暴跌最低 bar 的序号
	reboundBarIdx int     // 反弹确认 bar 的序号

	// ── 持仓状态 ──
	inPosition           bool
	entryPrice           float64
	stopPrice            float64
	targetPrice          float64
	holdBars             int
	stopMovedToBreakeven bool

	params *strategy.ParamRegistry
}

// NewSupportReboundStrategy 创建默认参数的支撑回踩反弹策略实例。
func NewSupportReboundStrategy() *SupportReboundStrategy {
	s := &SupportReboundStrategy{
		name:                     "support_rebound",
		symbol:                   "BTCUSDT",
		timeframe:                "4h",
		lookbackBars:             120,
		supportTouchTolerancePct: 0.015,
		minSupportTouches:        2,
		crashLookbackBars:        6,
		crashDropPct:             0.12,
		volumeSMAPeriod:          20,
		volumeSpikeMult:          2.0,
		reboundPct:               0.05,
		pullbackBars:             12,
		entryVolumeMult:          1.5,
		stopBufferPct:            0.02,
		takeProfitPct:            0.15,
		positionSize:             500,
		maxHoldBars:              90,
		state:                    srStateIdle,
		refBarIdx:                -1,
		reboundBarIdx:            -1,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("lookback_bars", 120, 50, 300, "buy"))
	s.params.Register(strategy.FloatParameter("support_touch_tolerance_pct", 0.015, 0.005, 0.05, 0.001, "buy"))
	s.params.Register(strategy.IntParameter("min_support_touches", 2, 2, 6, "buy"))
	s.params.Register(strategy.IntParameter("crash_lookback_bars", 6, 3, 24, "buy"))
	s.params.Register(strategy.FloatParameter("crash_drop_pct", 0.12, 0.05, 0.35, 0.01, "buy"))
	s.params.Register(strategy.IntParameter("volume_sma_period", 20, 10, 60, "buy"))
	s.params.Register(strategy.FloatParameter("volume_spike_mult", 2.0, 1.2, 5, 0.1, "buy"))
	s.params.Register(strategy.FloatParameter("rebound_pct", 0.05, 0.02, 0.15, 0.005, "buy"))
	s.params.Register(strategy.IntParameter("pullback_bars", 12, 3, 48, "buy"))
	s.params.Register(strategy.FloatParameter("entry_volume_mult", 1.5, 1.0, 4, 0.1, "buy"))
	s.params.Register(strategy.FloatParameter("stop_buffer_pct", 0.02, 0.005, 0.05, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.15, 0.03, 0.5, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 50, 10000, 50, "buy"))
	s.params.Register(strategy.IntParameter("max_hold_bars", 90, 20, 400, "roi"))
	return s
}

func (s *SupportReboundStrategy) Name() string   { return s.name }
func (s *SupportReboundStrategy) Symbol() string { return s.symbol }

func (s *SupportReboundStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol":                      s.symbol,
		"timeframe":                   s.timeframe,
		"lookback_bars":               s.lookbackBars,
		"support_touch_tolerance_pct": s.supportTouchTolerancePct,
		"min_support_touches":         s.minSupportTouches,
		"crash_lookback_bars":         s.crashLookbackBars,
		"crash_drop_pct":              s.crashDropPct,
		"volume_sma_period":           s.volumeSMAPeriod,
		"volume_spike_mult":           s.volumeSpikeMult,
		"rebound_pct":                 s.reboundPct,
		"pullback_bars":               s.pullbackBars,
		"entry_volume_mult":           s.entryVolumeMult,
		"stop_buffer_pct":             s.stopBufferPct,
		"take_profit_pct":             s.takeProfitPct,
		"position_size":               s.positionSize,
		"max_hold_bars":               s.maxHoldBars,
	}
}

func (s *SupportReboundStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// PrimaryTimeframe 声明主周期：引擎只把该周期 K 线分发进 OnBar，
// K 线供给管按同一周期订阅（params["timeframe"]，默认 4h）。
func (s *SupportReboundStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeframe
}

func (s *SupportReboundStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("support_rebound strategy apply params: %w", err)
	}
	if s.lookbackBars <= 0 {
		s.lookbackBars = 120
	}

	// 重新启动 = 全新状态机：清空 K 线与持仓/找支撑状态，重新预热。
	s.bars = nil
	s.resetToIdleLocked()
	s.running = true
	return nil
}

func (s *SupportReboundStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.bars = nil
	s.resetToIdleLocked()
	return nil
}

// ── 参数系统实现 ────────────────────────────────────────────────

func (s *SupportReboundStrategy) GetParameters() *strategy.ParamRegistry { return s.params }

func (s *SupportReboundStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

// ApplyParams 先经注册表做范围校验，再用 getFloat/getInt 模式把值同步到
// 本地字段（symbol/timeframe 是引擎级键，直接读 map）。调用方须已持有锁
// （Start 内调用）或保证单 goroutine（模板实例化测试）。
func (s *SupportReboundStrategy) ApplyParams(m map[string]any) error {
	if s.params != nil {
		if err := s.params.FromMap(m); err != nil {
			return err
		}
	}
	s.lookbackBars = getInt(m, "lookback_bars", s.lookbackBars)
	s.supportTouchTolerancePct = getFloat(m, "support_touch_tolerance_pct", s.supportTouchTolerancePct)
	s.minSupportTouches = getInt(m, "min_support_touches", s.minSupportTouches)
	s.crashLookbackBars = getInt(m, "crash_lookback_bars", s.crashLookbackBars)
	s.crashDropPct = getFloat(m, "crash_drop_pct", s.crashDropPct)
	s.volumeSMAPeriod = getInt(m, "volume_sma_period", s.volumeSMAPeriod)
	s.volumeSpikeMult = getFloat(m, "volume_spike_mult", s.volumeSpikeMult)
	s.reboundPct = getFloat(m, "rebound_pct", s.reboundPct)
	s.pullbackBars = getInt(m, "pullback_bars", s.pullbackBars)
	s.entryVolumeMult = getFloat(m, "entry_volume_mult", s.entryVolumeMult)
	s.stopBufferPct = getFloat(m, "stop_buffer_pct", s.stopBufferPct)
	s.takeProfitPct = getFloat(m, "take_profit_pct", s.takeProfitPct)
	s.positionSize = getFloat(m, "position_size", s.positionSize)
	s.maxHoldBars = getInt(m, "max_hold_bars", s.maxHoldBars)
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if tf := getString(m, "timeframe", ""); tf != "" {
		s.timeframe = strings.ToLower(strings.TrimSpace(tf))
	}
	return nil
}

func (s *SupportReboundStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// CustomStakeAmount 把 position_size（USDT）折算为下单数量：
// 引擎信号出口对 LONG/SHORT 信号自动按最新价换算 Qty = stake / price。
// 可用余额不足时退化为全额可用（现货只做多，不做杠杆）。
func (s *SupportReboundStrategy) CustomStakeAmount(availableBalance float64, _ *model.Signal) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.positionSize <= 0 {
		return 0
	}
	if availableBalance > 0 && s.positionSize > availableBalance {
		return availableBalance
	}
	return s.positionSize
}

// RuntimeStatus 暴露状态机快照，供机器人页运行监控面板展示。
func (s *SupportReboundStrategy) RuntimeStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := map[string]any{
		"running":        s.running,
		"state":          s.state,
		"symbol":         s.symbol,
		"timeframe":      s.timeframe,
		"bars_collected": len(s.bars),
		"in_position":    s.inPosition,
		"support_bottom": s.supportBottom,
		"support_top":    s.supportTop,
		"crash_low":      s.crashLow,
		"crash_ref":      s.crashRef,
	}
	if s.inPosition {
		m["entry_price"] = s.entryPrice
		m["stop_price"] = s.stopPrice
		m["target_price"] = s.targetPrice
		m["hold_bars"] = s.holdBars
		m["stop_moved_to_breakeven"] = s.stopMovedToBreakeven
	}
	return m
}

func (s *SupportReboundStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *SupportReboundStrategy) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnTick 持仓管理（止损/止盈/保本移损按最新成交价判定，K 线计数只走 OnBar）。
func (s *SupportReboundStrategy) OnTick(tick model.Tick, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || !s.inPosition || tick.Last <= 0 {
		return nil, nil
	}
	return s.checkPositionExit(tick.Last, tick.Timestamp, false), nil
}

func (s *SupportReboundStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > supportReboundMaxBars {
		s.bars = s.bars[len(s.bars)-supportReboundMaxBars:]
	}

	if !s.running {
		return nil, nil
	}
	// 预热：收集满 lookback_bars 根后才开始评估。
	if len(s.bars) < s.lookbackBars {
		return nil, nil
	}

	// 持仓管理优先：止损 / 止盈 / 超时 / 保本移损。
	if s.inPosition {
		return s.checkPositionExit(bar.Close, bar.Time, true), nil
	}

	// armed 期间（含反弹/回踩子态）收盘跌破支撑下沿 → 支撑失效，回 IDLE 重新找支撑。
	if s.isArmed() && bar.Close < s.supportBottom {
		s.resetToIdleLocked()
	}

	switch s.state {
	case srStateIdle:
		s.identifySupport()
		if s.state == srStateSupport {
			// 同根允许继续检测暴跌（恐慌 K 直接砸出双底的情形）。
			s.detectCrash()
		}
	case srStateSupport:
		s.detectCrash()
	case srStateArmed:
		// 反弹确认：收盘价自暴跌低点回升 reboundPct。
		if bar.Close >= s.crashLow*(1+s.reboundPct) {
			s.state = srStateRebound
			s.reboundBarIdx = len(s.bars) - 1
		}
	case srStateRebound:
		// 回踩有效窗口：rebound 后 pullback_bars 根内未回踩 → 形态失效。
		if len(s.bars)-1-s.reboundBarIdx > s.pullbackBars {
			s.resetToIdleLocked()
			break
		}
		// 回踩确认：下影探入支撑带（Low ≤ supportTop），收盘守稳（Close ≥ supportBottom）。
		if bar.Low <= s.supportTop && bar.Close >= s.supportBottom {
			s.state = srStatePullback
		}
	case srStatePullback:
		// 入场信号：首根放量阳线。量能基准为不含当前 bar 的 volSMA
		// （数据不足时 volSMA=0，不放量不放行）。
		if volBase := s.volumeSMA(s.volumeSMAPeriod); volBase > 0 &&
			bar.Close > bar.Open && bar.Volume >= volBase*s.entryVolumeMult {
			return s.enterLong(bar), nil
		}
	}
	return nil, nil
}

// isArmed 报告状态机是否处于 armed  umbrella（ARMED/REBOUND/PULLBACK）。
func (s *SupportReboundStrategy) isArmed() bool {
	return s.state == srStateArmed || s.state == srStateRebound || s.state == srStatePullback
}

// resetToIdleLocked 清掉支撑/暴跌/持仓状态，回到 IDLE 重新找支撑。须持锁。
func (s *SupportReboundStrategy) resetToIdleLocked() {
	s.state = srStateIdle
	s.supportBottom, s.supportTop, s.minLow = 0, 0, 0
	s.crashLow, s.crashRef = 0, 0
	s.refBarIdx, s.reboundBarIdx = -1, -1
	s.inPosition = false
	s.entryPrice, s.stopPrice, s.targetPrice = 0, 0, 0
	s.holdBars = 0
	s.stopMovedToBreakeven = false
}

// identifySupport 在 lookback_bars 窗口内识别支撑位：
// minLow = 窗口最低 Low；支撑带 = minLow×(1±tol)；触及次数 = 窗口内
// Low 落入支撑带的 bar 数（含当前）。次数达标 → 记录支撑带并转 SUPPORT。
func (s *SupportReboundStrategy) identifySupport() {
	n := s.lookbackBars
	if n > len(s.bars) {
		n = len(s.bars)
	}
	window := s.bars[len(s.bars)-n:]

	minLow := window[0].Low
	for _, b := range window[1:] {
		if b.Low < minLow {
			minLow = b.Low
		}
	}
	if minLow <= 0 {
		return
	}
	bottom := minLow * (1 - s.supportTouchTolerancePct)
	top := minLow * (1 + s.supportTouchTolerancePct)
	touches := 0
	for _, b := range window {
		if b.Low >= bottom && b.Low <= top {
			touches++
		}
	}
	if touches < s.minSupportTouches {
		return
	}
	s.minLow = minLow
	s.supportBottom = bottom
	s.supportTop = top
	s.state = srStateSupport
}

// detectCrash 放量恐慌暴跌检测：crash_lookback_bars 窗口内 refHigh，
// 窗口最低那根 bar 回撤 ≥ crash_drop_pct 且其成交量 ≥
// volSMA×volume_spike_mult（量能基准为不含当前 bar 的 volSMA，避免
// 恐慌量自我稀释）→ 进入 ARMED，记录 crashLow/crashRef/refBar。
func (s *SupportReboundStrategy) detectCrash() {
	n := s.crashLookbackBars
	if n > len(s.bars) {
		n = len(s.bars)
	}
	window := s.bars[len(s.bars)-n:]

	refHigh := window[0].High
	lowestIdx := 0
	for i, b := range window {
		if b.High > refHigh {
			refHigh = b.High
		}
		if b.Low < window[lowestIdx].Low {
			lowestIdx = i
		}
	}
	if refHigh <= 0 {
		return
	}
	lowest := window[lowestIdx]
	if (refHigh-lowest.Low)/refHigh < s.crashDropPct {
		return
	}
	// 量能基准不足（volSMA=0）时无法确认恐慌量，不 armed。
	volBase := s.volumeSMA(s.volumeSMAPeriod)
	if volBase <= 0 || float64(lowest.Volume) < volBase*s.volumeSpikeMult {
		return
	}
	s.crashLow = lowest.Low
	s.crashRef = refHigh
	s.refBarIdx = len(s.bars) - n + lowestIdx
	s.state = srStateArmed
}

// volumeSMA 计算不含当前 bar 的最近 n 根成交量均值（当前 bar 与
// 历史均量比较，避免自我稀释）。数据不足返回 0。
func (s *SupportReboundStrategy) volumeSMA(n int) float64 {
	if n <= 0 || len(s.bars) < n+1 {
		return 0
	}
	end := len(s.bars) - 1 // 不含当前 bar
	sum := 0.0
	for i := end - n; i < end; i++ {
		sum += s.bars[i].Volume
	}
	return sum / float64(n)
}

// enterLong 产生入场信号并进入 IN_POSITION：
// stake = position_size（USDT，经 CustomStakeAmount 折算数量）；
// 止盈目标 target = min(crashRef, entry×(1+take_profit_pct))；
// 止损 stop = supportBottom×(1−stop_buffer_pct)。
func (s *SupportReboundStrategy) enterLong(bar model.Bar) *model.Signal {
	s.inPosition = true
	s.entryPrice = bar.Close
	s.stopPrice = s.supportBottom * (1 - s.stopBufferPct)
	s.targetPrice = math.Min(s.crashRef, bar.Close*(1+s.takeProfitPct))
	s.holdBars = 0
	s.stopMovedToBreakeven = false
	s.state = srStatePosition
	return &model.Signal{
		Symbol:    s.symbol,
		Direction: "LONG",
		Strength:  0.8,
		Strategy:  s.name,
		Reason: fmt.Sprintf("支撑回踩反弹入场: 支撑带[%.4f, %.4f] 暴跌低点 %.4f 反弹确认后放量阳线",
			s.supportBottom, s.supportTop, s.crashLow),
		Timestamp: bar.Time,
	}
}

// checkPositionExit 持仓管理（全程收盘价/最新价判定）：
//  1. 浮盈 ≥ rebound_pct → 止损上移到 max(stop, entry×1.002)（保本）；
//  2. Close ≤ stop → 止损离场；Close ≥ target → 止盈离场；
//  3. 持仓 bar 数 ≥ max_hold_bars → 超时离场。
//
// countHold=true 时累计持仓 K 线数（OnBar）；OnTick 不计数。
// 平仓后清状态回 IDLE。
func (s *SupportReboundStrategy) checkPositionExit(price float64, ts int64, countHold bool) *model.Signal {
	if countHold {
		s.holdBars++
	}
	if !s.stopMovedToBreakeven && s.entryPrice > 0 &&
		(price-s.entryPrice)/s.entryPrice >= s.reboundPct {
		if be := s.entryPrice * 1.002; be > s.stopPrice {
			s.stopPrice = be
		}
		s.stopMovedToBreakeven = true
	}
	closePosition := func(reason string) *model.Signal {
		s.resetToIdleLocked()
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    reason,
			Timestamp: ts,
		}
	}
	if price <= s.stopPrice {
		return closePosition("支撑回踩反弹止损离场")
	}
	if price >= s.targetPrice {
		return closePosition("支撑回踩反弹止盈离场")
	}
	if countHold && s.holdBars >= s.maxHoldBars {
		return closePosition("支撑回踩反弹持仓超时离场")
	}
	return nil
}

// ── map 读取小工具（缺失/类型不符时回退默认值）────────────────

func getFloat(m map[string]any, key string, def float64) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	}
	return def
}

func getInt(m map[string]any, key string, def int) int {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(math.Round(n))
	case float32:
		return int(math.Round(float64(n)))
	}
	return def
}

func getString(m map[string]any, key, def string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}
