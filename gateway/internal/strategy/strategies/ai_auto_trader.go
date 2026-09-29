package strategies

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── AIAutoTraderStrategy（AI 全自动交易员：C 混合闭环）────────────────
//
// 量化信号 + LLM 闸门 + 双层学习的现货只做多闭环，主周期 15m：
//
//  1. 量化决策：内部异步 worker 每 scan_interval_minutes 调一次
//     ai.DecisionEngine.Decide（mode=deep 走 7-agent 管线）；signal≠long
//     或 confidence < 有效阈值（含自适应调整）→ 记录状态结束本轮；
//  2. 自适应阈值：滚动窗口（最近 20 笔）胜率 <35% 有效阈值 +10、>65% −10，
//     夹在 [threshold, 95] 内；
//  3. LLM 闸门：信号摘要 + 近 experience_window 条经验（同 symbol 优先）喂给
//     LLM，要求严格 JSON {decision:"approve"|"veto"|"reduce", size_pct, reason}；
//     veto 结束本轮、reduce 仓位降至 min(size_pct, gate_size_cap_pct)、
//     approve 满仓；provider 不可用/超时/解析失败 → 默认放行 size=cap（paper
//     环境可用性优先）并记录原因；
//  4. 下单动作：pendingAction 单槽（加锁）= LONG，stake = max_position_usdt
//     × size_pct/100；当日已实现亏损 ≥ daily_loss_limit_usdt 当日不再开新仓；
//     OnBar/OnTick 只负责把 pendingAction 发成信号，绝不阻塞引擎事件循环；
//  5. 持仓管理（OnBar/OnTick 同函数）：触止损 / 触止盈 / 持仓超时 / 浮盈 ≥
//     止盈一半后启动移动止损（峰值利润回撤 30% 离场）；
//  6. 平仓触发学习：平仓成交 → 异步复盘 goroutine（交易快照 → LLM 输出
//     {lesson, tag} → 失败/超时走规则兜底模板）→ 经验落 xt_ai_experiences
//     （单实例超 200 条删最旧），下轮 gate 的 prompt 会带上这些经验。
//
// 锁模型：单一 sync.RWMutex 保护全部可变状态；worker 与 OnBar/OnTick/
// OnOrderUpdate 互斥。worker 每轮先快照参数到本地变量再释放锁做网络调用，
// 网络结果回来后再加锁写状态；scanning 标志防 worker 重入；pendingAction
// 为加锁单槽，OnBar/OnTick 取出即置 nil（单发）。

const (
	aatStateScanning = "扫描中"  // 无 pending、无持仓、无日亏闸
	aatStatePending  = "待入场"  // pendingAction 已生成，等 K 线出口发信号
	aatStatePosition = "持仓"   // 已成交持仓中
	aatStateHalted   = "今日闸停" // 当日已实现亏损触 daily_loss_limit
)

// 自适应阈值滚动窗口与最小样本：样本不足不调整（避免 0 笔时胜率 0% 误判）。
const (
	aatRollingWindow     = 20
	aatRollingMinSamples = 5
	// 自适应调整量与有效阈值上限（下限即基础阈值 confidence_threshold）。
	aatAdaptiveStep = 10.0
	aatEffectiveMax = 95.0
)

// aiGateResult 是一次 LLM 闸门的归一化结论。
type aiGateResult struct {
	Decision string  `json:"decision"` // approve / veto / reduce / pass_default
	SizePct  float64 `json:"size_pct"` // 0-100
	Reason   string  `json:"reason"`
}

// aatTradeOutcome 是一笔已平仓交易的滚动窗口样本。
type aatTradeOutcome struct {
	Win bool
	PnL float64
}

// aatPendingLong 是待执行的多单动作（pendingAction 单槽内容）。
type aatPendingLong struct {
	Stake    float64        // USDT
	SizePct  float64        // gate 给出的仓位百分比
	Decision *ai.AIDecision // 入场时的量化决策快照
	Gate     *aiGateResult  // 入场时的 gate 结论快照
}

// aatReviewSnapshot 是平仓复盘用的交易快照（学习链路输入）。
type aatReviewSnapshot struct {
	Symbol           string   `json:"symbol"`
	EntryPrice       float64  `json:"entry_price"`
	ExitPrice        float64  `json:"exit_price"`
	EntryTime        int64    `json:"entry_time"`
	ExitTime         int64    `json:"exit_time"`
	HoldBars         int      `json:"hold_bars"`
	PositionHigh     float64  `json:"position_high"`
	PositionLow      float64  `json:"position_low"`
	PnL              float64  `json:"pnl"`
	Outcome          string   `json:"outcome"` // win / loss
	ExitReason       string   `json:"exit_reason"`
	SignalReason     string   `json:"signal_reason"`
	SignalConfidence float64  `json:"signal_confidence"`
	MarketCondition  string   `json:"market_condition"`
	Filters          []string `json:"filters"`
	GateDecision     string   `json:"gate_decision"`
	GateSizePct      float64  `json:"gate_size_pct"`
	GateReason       string   `json:"gate_reason"`
	Mode             string   `json:"mode"`
}

// AIAutoTraderStrategy 实现 strategy.Strategy。
type AIAutoTraderStrategy struct {
	strategy.BaseStrategy
	name       string
	symbol     string
	timeframe  string
	userID     int64
	instanceID string // 策略实例标识（params 可覆盖，默认 策略名:symbol）
	running    bool
	mu         sync.RWMutex

	// ── worker 生命周期 ──
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// scanning 防 worker 重入（上一轮未扫完跳过本轮）。
	scanning bool

	// ── 参数（ParamRegistry 注册，ApplyParams 从 map 同步）──
	scanIntervalMinutes int     // 决策扫描间隔（分钟）
	mode                string  // fast / deep
	provider            string  // LLM provider 名，空 = 走配置链
	confidenceThreshold float64 // 开仓置信度阈值 0-100
	maxPositionUSDT     float64 // 单笔最大仓位（USDT）
	takeProfitPct       float64 // 止盈百分比（如 0.08）
	stopLossPct         float64 // 止损百分比（如 0.04）
	maxHoldBars         int     // 超时离场（根，15m）
	enableLLMGate       bool    // 是否启用 LLM 闸门
	gateSizeCapPct      float64 // gate 默认放行/reduce 的仓位上限 10-100
	experienceWindow    int     // gate prompt 携带的最近经验条数 0-20
	learningEnabled     bool    // 平仓复盘学习开关
	dailyLossLimitUSDT  float64 // 日亏闸（0 关闭）

	// marketData 可注入行情源（测试用）；nil 时走币安公共接口。
	marketData func(ctx context.Context, symbol string) (*ai.MarketSnapshot, error)

	// ── 决策/gate 状态（RuntimeStatus 暴露）──
	lastDecision *ai.AIDecision
	lastGate     *aiGateResult
	lastErr      string // 最近一轮扫描的错误（无则空）
	lastLesson   string // 最近一条入库经验

	// ── 自适应阈值（滚动窗口内存维护）──
	rolling []aatTradeOutcome

	// ── 日亏闸（内存日计数）──
	todayKey    string  // yyyy-mm-dd
	todayPnL    float64 // 当日已实现盈亏（USDT）
	dailyHalted bool    // 当日已触日亏闸

	// ── pendingAction 加锁单槽 ──
	pendingAction *aatPendingLong
	// lastStake 是 pending 被消费时记忆的 stake，供 CustomStakeAmount 返回。
	lastStake float64
	// pendingEntry 是信号发出时暂存的入场快照来源（买单成交后转入持仓快照）。
	pendingEntry *aatPendingLong

	// ── 持仓状态 ──
	inPosition     bool
	entryPrice     float64
	entryTime      int64
	holdBars       int
	positionHigh   float64
	positionLow    float64
	peakPrice      float64
	trailingActive bool
	closeEmitted   bool // 管理函数已发 CLOSE，等成交，避免重复信号
	exitReason     string
	entrySnap      *aatReviewSnapshot // 入场时固化的决策/gate 快照

	expRepo *store.AIExperienceRepo
	params  *strategy.ParamRegistry
}

// NewAIAutoTraderStrategy 创建默认参数的 AI 全自动交易员策略实例。
func NewAIAutoTraderStrategy() *AIAutoTraderStrategy {
	s := &AIAutoTraderStrategy{
		name:                "ai_auto_trader",
		symbol:              "BTCUSDT",
		timeframe:           "15m",
		scanIntervalMinutes: 30,
		mode:                "fast",
		provider:            "",
		confidenceThreshold: 60,
		maxPositionUSDT:     500,
		takeProfitPct:       0.08,
		stopLossPct:         0.04,
		maxHoldBars:         96,
		enableLLMGate:       true,
		gateSizeCapPct:      100,
		experienceWindow:    5,
		learningEnabled:     true,
		dailyLossLimitUSDT:  200,
		expRepo:             store.DefaultAIExperienceRepo(),
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.CategoricalParameter("symbol", "BTCUSDT",
		[]string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT"}, "buy"))
	s.params.Register(strategy.IntParameter("scan_interval_minutes", 30, 5, 240, "buy"))
	s.params.Register(strategy.CategoricalParameter("mode", "fast", []string{"fast", "deep"}, "buy"))
	s.params.Register(strategy.CategoricalParameter("provider", "", append([]string{""}, ai.ListProviders()...), "buy"))
	s.params.Register(strategy.FloatParameter("confidence_threshold", 60, 0, 100, 1, "buy"))
	s.params.Register(strategy.FloatParameter("max_position_usdt", 500, 50, 100000, 50, "buy"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.08, 0.01, 0.5, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.04, 0.005, 0.2, 0.005, "stoploss"))
	s.params.Register(strategy.IntParameter("max_hold_bars", 96, 20, 1000, "roi"))
	s.params.Register(strategy.BoolParameter("enable_llm_gate", true, "buy"))
	s.params.Register(strategy.FloatParameter("gate_size_cap_pct", 100, 10, 100, 5, "buy"))
	s.params.Register(strategy.IntParameter("experience_window", 5, 0, 20, "buy"))
	s.params.Register(strategy.BoolParameter("learning_enabled", true, "roi"))
	s.params.Register(strategy.FloatParameter("daily_loss_limit_usdt", 200, 0, 100000, 10, "stoploss"))
	return s
}

func (s *AIAutoTraderStrategy) Name() string   { return s.name }
func (s *AIAutoTraderStrategy) Symbol() string { return s.symbol }

func (s *AIAutoTraderStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol":                s.symbol,
		"scan_interval_minutes": s.scanIntervalMinutes,
		"mode":                  s.mode,
		"provider":              s.provider,
		"confidence_threshold":  s.confidenceThreshold,
		"max_position_usdt":     s.maxPositionUSDT,
		"take_profit_pct":       s.takeProfitPct,
		"stop_loss_pct":         s.stopLossPct,
		"max_hold_bars":         s.maxHoldBars,
		"enable_llm_gate":       s.enableLLMGate,
		"gate_size_cap_pct":     s.gateSizeCapPct,
		"experience_window":     s.experienceWindow,
		"learning_enabled":      s.learningEnabled,
		"daily_loss_limit_usdt": s.dailyLossLimitUSDT,
	}
}

func (s *AIAutoTraderStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// PrimaryTimeframe 声明主周期 15m：引擎只把 15m K 线分发进 OnBar。
func (s *AIAutoTraderStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeframe
}

func (s *AIAutoTraderStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("ai_auto_trader strategy apply params: %w", err)
	}
	if v := getFloat(params, "user_id", 0); v > 0 {
		s.userID = int64(v)
	}
	// instance_id 可用 params 显式覆盖（不经注册表），否则按 策略名:symbol 派生。
	if v := getString(params, "instance_id", ""); v != "" {
		s.instanceID = v
	} else {
		s.instanceID = s.name + ":" + s.symbol
	}
	if s.scanIntervalMinutes <= 0 {
		s.scanIntervalMinutes = 30
	}

	// 全新启动 = 清状态：决策/gate 快照、滚动窗口、日亏闸、pending、持仓。
	s.lastDecision = nil
	s.lastGate = nil
	s.lastErr = ""
	s.lastLesson = ""
	s.rolling = nil
	s.todayKey = ""
	s.todayPnL = 0
	s.dailyHalted = false
	s.pendingAction = nil
	s.lastStake = 0
	s.pendingEntry = nil
	s.resetPositionLocked()

	s.running = true

	// 内部异步决策 worker：每 scan_interval_minutes 一轮，Stop 取消。
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	interval := time.Duration(s.scanIntervalMinutes) * time.Minute
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runScanCycle(ctx)
			}
		}
	}()
	log.Printf("[ai_auto_trader] started: instance=%s symbol=%s mode=%s interval=%dmin",
		s.instanceID, s.symbol, s.mode, s.scanIntervalMinutes)
	return nil
}

func (s *AIAutoTraderStrategy) Stop() error {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.running = false
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	s.pendingAction = nil
	s.lastStake = 0
	s.pendingEntry = nil
	s.resetPositionLocked()
	s.mu.Unlock()
	log.Printf("[ai_auto_trader] %s stopped", s.instanceID)
	return nil
}

// resetPositionLocked 清空持仓相关状态（须持锁）。
func (s *AIAutoTraderStrategy) resetPositionLocked() {
	s.inPosition = false
	s.entryPrice = 0
	s.entryTime = 0
	s.holdBars = 0
	s.positionHigh = 0
	s.positionLow = 0
	s.peakPrice = 0
	s.trailingActive = false
	s.closeEmitted = false
	s.exitReason = ""
	s.entrySnap = nil
}

// ── 参数系统实现 ────────────────────────────────────────────────

func (s *AIAutoTraderStrategy) GetParameters() *strategy.ParamRegistry { return s.params }

func (s *AIAutoTraderStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

// ApplyParams 先经注册表做范围校验，再用 getFloat/getInt/getString 把值同步到
// 本地字段（symbol 是引擎级键，直接读 map）。调用方须已持有锁（Start 内调用）
// 或保证单 goroutine（模板实例化测试）。
func (s *AIAutoTraderStrategy) ApplyParams(m map[string]any) error {
	if s.params != nil {
		if err := s.params.FromMap(m); err != nil {
			return err
		}
	}
	s.scanIntervalMinutes = getInt(m, "scan_interval_minutes", s.scanIntervalMinutes)
	if v := getString(m, "mode", ""); v != "" {
		s.mode = strings.ToLower(strings.TrimSpace(v))
	}
	s.provider = strings.TrimSpace(getString(m, "provider", s.provider))
	s.confidenceThreshold = getFloat(m, "confidence_threshold", s.confidenceThreshold)
	s.maxPositionUSDT = getFloat(m, "max_position_usdt", s.maxPositionUSDT)
	s.takeProfitPct = getFloat(m, "take_profit_pct", s.takeProfitPct)
	s.stopLossPct = getFloat(m, "stop_loss_pct", s.stopLossPct)
	s.maxHoldBars = getInt(m, "max_hold_bars", s.maxHoldBars)
	if v, ok := m["enable_llm_gate"].(bool); ok {
		s.enableLLMGate = v
	}
	s.gateSizeCapPct = getFloat(m, "gate_size_cap_pct", s.gateSizeCapPct)
	s.experienceWindow = getInt(m, "experience_window", s.experienceWindow)
	if v, ok := m["learning_enabled"].(bool); ok {
		s.learningEnabled = v
	}
	s.dailyLossLimitUSDT = getFloat(m, "daily_loss_limit_usdt", s.dailyLossLimitUSDT)
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	// 工作K线固定 15m：持仓超时/移动止损全部按 15m 语义设计。
	s.timeframe = "15m"
	return nil
}

func (s *AIAutoTraderStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// instanceID 字段惰性兜底（直接 new 后未 Start 时 RuntimeStatus 也能用）。
func (s *AIAutoTraderStrategy) instanceIDOrDefault() string {
	if s.instanceID != "" {
		return s.instanceID
	}
	return s.name + ":" + s.symbol
}

// CustomStakeAmount 返回当前 pending 计算的 stake（max_position_usdt×size%）；
// 余额不足退化为全额可用（现货只做多，不做杠杆）；无 pending 返回 0（用引擎默认）。
func (s *AIAutoTraderStrategy) CustomStakeAmount(availableBalance float64, _ *model.Signal) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stake := s.lastStake
	if stake <= 0 {
		return 0
	}
	if availableBalance > 0 && stake > availableBalance {
		return availableBalance
	}
	return stake
}

// RuntimeStatus 暴露闭环快照：state（扫描中/待入场/持仓/今日闸停）、
// last_decision、last_gate、adaptive/effective threshold、rolling 胜负、
// today_pnl、experience_count、last_lesson。
func (s *AIAutoTraderStrategy) RuntimeStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := aatStateScanning
	switch {
	case s.dailyHalted:
		state = aatStateHalted
	case s.inPosition:
		state = aatStatePosition
	case s.pendingAction != nil:
		state = aatStatePending
	}
	m := map[string]any{
		"running":              s.running,
		"state":                state,
		"symbol":               s.symbol,
		"timeframe":            s.timeframe,
		"instance_id":          s.instanceIDOrDefault(),
		"mode":                 s.mode,
		"last_scan_error":      s.lastErr,
		"confidence_threshold": s.confidenceThreshold,
		"effective_threshold":  s.effectiveThresholdLocked(),
		"adaptive_threshold":   s.effectiveThresholdLocked() - s.confidenceThreshold,
		"rolling_wins":         s.rollingWinsLocked(),
		"rolling_losses":       s.rollingLossesLocked(),
		"today_pnl":            s.todayPnL,
		"daily_halted":         s.dailyHalted,
		"last_lesson":          s.lastLesson,
		"experience_count":     s.experienceCount(),
		"in_position":          s.inPosition,
	}
	if s.lastDecision != nil {
		m["last_decision"] = map[string]any{
			"signal":           s.lastDecision.Signal,
			"confidence":       s.lastDecision.Confidence,
			"reason":           s.lastDecision.Reason,
			"filters":          s.lastDecision.Filters,
			"market_condition": s.lastDecision.MarketCondition,
			"mode":             s.lastDecision.Mode,
			"provider":         s.lastDecision.Provider,
		}
	}
	if s.lastGate != nil {
		m["last_gate"] = map[string]any{
			"decision": s.lastGate.Decision,
			"size_pct": s.lastGate.SizePct,
			"reason":   s.lastGate.Reason,
		}
	}
	if s.inPosition {
		m["entry_price"] = s.entryPrice
		m["entry_time"] = s.entryTime
		m["hold_bars"] = s.holdBars
		m["position_high"] = s.positionHigh
		m["position_low"] = s.positionLow
		m["trailing_active"] = s.trailingActive
		m["exit_reason"] = s.exitReason
	}
	return m
}

// experienceCount 读经验库条数（读库失败回退 -1，不阻塞状态接口）。
func (s *AIAutoTraderStrategy) experienceCount() int {
	n, err := s.expRepo.CountByInstance(s.instanceIDOrDefault())
	if err != nil {
		return -1
	}
	return n
}

func (s *AIAutoTraderStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar 15m K 线入口：持仓管理优先（计数 + SL/TP/超时/移动止损），
// 否则把 pendingAction 发成 LONG 信号（取出即清槽，单发）。
func (s *AIAutoTraderStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if bar.Symbol != "" && bar.Symbol != s.symbol {
		return nil, nil
	}
	s.rollTodayLocked(time.Now())
	if s.inPosition {
		return s.managePositionLocked(bar.Close, bar.High, bar.Low, bar.Time, true), nil
	}
	return s.emitPendingLocked(bar.Time), nil
}

// OnTick 最新价入口：与 OnBar 共用管理函数（不重复计数持仓 K 线）。
func (s *AIAutoTraderStrategy) OnTick(tick model.Tick, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || tick.Last <= 0 {
		return nil, nil
	}
	if tick.Symbol != "" && tick.Symbol != s.symbol {
		return nil, nil
	}
	s.rollTodayLocked(time.Now())
	if s.inPosition {
		return s.managePositionLocked(tick.Last, tick.Last, tick.Last, tick.Timestamp, false), nil
	}
	return s.emitPendingLocked(tick.Timestamp), nil
}

// emitPendingLocked 把 pendingAction 单槽内容发成 LONG 信号（取出即清槽）。
// 同时记忆 stake 供 CustomStakeAmount、暂存入场快照来源供买单成交回填。
func (s *AIAutoTraderStrategy) emitPendingLocked(ts int64) *model.Signal {
	p := s.pendingAction
	if p == nil || p.Stake <= 0 {
		return nil
	}
	s.pendingAction = nil
	s.lastStake = p.Stake
	s.pendingEntry = p
	conf := 0.5
	if p.Decision != nil && p.Decision.Confidence > 0 {
		conf = math.Min(p.Decision.Confidence/100, 1)
	}
	reason := "AI 全自动交易员入场"
	if p.Decision != nil && p.Decision.Reason != "" {
		reason = fmt.Sprintf("AI量化信号: %s", truncateStr(p.Decision.Reason, 120))
	}
	if p.Gate != nil {
		reason += fmt.Sprintf(" | gate=%s size=%.0f%%", p.Gate.Decision, p.Gate.SizePct)
	}
	return &model.Signal{
		Symbol:    s.symbol,
		Direction: "LONG",
		Strength:  conf,
		Strategy:  s.name,
		Reason:    reason,
		Timestamp: ts,
	}
}

// managePositionLocked 持仓管理（OnBar/OnTick 共用，price=Close/最新价；
// countHold=true 仅 OnBar，累计持仓 15m K 线数）：
//  1. Close ≤ entry×(1−sl) → 止损 CLOSE；
//  2. Close ≥ entry×(1+tp) → 止盈 CLOSE；
//  3. holdBars ≥ max_hold_bars → 超时 CLOSE；
//  4. 浮盈曾 ≥ tp/2 → 启动移动止损：峰值利润回撤 30%（利润回落到峰值 70%）离场。
//
// closeEmitted 防止成交回报前的重复 CLOSE 信号；实际平仓与学习由
// OnOrderUpdate 成交确认触发。
func (s *AIAutoTraderStrategy) managePositionLocked(price, high, low float64, ts int64, countHold bool) *model.Signal {
	if s.closeEmitted || s.entryPrice <= 0 {
		return nil
	}
	if countHold {
		s.holdBars++
	}
	if high > s.positionHigh {
		s.positionHigh = high
	}
	if s.positionLow == 0 || low < s.positionLow {
		s.positionLow = low
	}
	if price > s.peakPrice {
		s.peakPrice = price
	}
	if high > s.peakPrice {
		s.peakPrice = high
	}

	profitPct := (price - s.entryPrice) / s.entryPrice
	// 浮盈 ≥ 止盈一半 → 启动移动止损（只启动一次）。
	if !s.trailingActive && profitPct >= s.takeProfitPct/2 {
		s.trailingActive = true
	}

	closeSignal := func(reason string) *model.Signal {
		s.closeEmitted = true
		s.exitReason = reason
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    reason,
			Timestamp: ts,
		}
	}

	if price <= s.entryPrice*(1-s.stopLossPct) {
		return closeSignal(fmt.Sprintf("AI全自动交易员止损离场: %.4f ≤ %.4f", price, s.entryPrice*(1-s.stopLossPct)))
	}
	if price >= s.entryPrice*(1+s.takeProfitPct) {
		return closeSignal(fmt.Sprintf("AI全自动交易员止盈离场: %.4f ≥ %.4f", price, s.entryPrice*(1+s.takeProfitPct)))
	}
	if countHold && s.holdBars >= s.maxHoldBars {
		return closeSignal(fmt.Sprintf("AI全自动交易员持仓超时离场: %d 根", s.holdBars))
	}
	if s.trailingActive {
		peakProfit := (s.peakPrice - s.entryPrice) / s.entryPrice
		if peakProfit > 0 && price <= s.entryPrice*(1+peakProfit*0.7) {
			return closeSignal(fmt.Sprintf("AI全自动交易员移动止损离场: 峰值利润 %.2f%% 回撤 30%%", peakProfit*100))
		}
	}
	return nil
}

// OnOrderUpdate 成交回填：首笔买单成交 → 记录 entryPrice/entryTime 并固化
// 入场快照（量化决策 + gate 结论）；卖单/ClosePosition 成交 → 更新滚动窗口与
// 日亏计数，异步触发复盘学习流。
func (s *AIAutoTraderStrategy) OnOrderUpdate(order model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil, nil
	}
	if order.Status != model.StatusFilled {
		s.mu.Unlock()
		return nil, nil
	}
	if order.Symbol != "" && order.Symbol != s.symbol {
		s.mu.Unlock()
		return nil, nil
	}
	s.rollTodayLocked(time.Now())

	// 平仓成交：卖单或显式 ClosePosition。
	if order.ClosePosition || order.Side == model.SideSell {
		if !s.inPosition {
			s.mu.Unlock()
			return nil, nil
		}
		exitPrice := order.AvgFillPrice
		if exitPrice <= 0 {
			exitPrice = order.Price
		}
		exitTime := order.UpdatedAt
		if exitTime == 0 {
			exitTime = order.CreatedAt
		}
		if exitTime == 0 {
			exitTime = time.Now().UnixMilli()
		}
		pnl := order.RealizedPnL
		if pnl == 0 && exitPrice > 0 && s.entryPrice > 0 && s.lastStake > 0 {
			// 成交回报缺 RealizedPnL 时按名义仓位估算（paper 兜底）。
			pnl = (exitPrice - s.entryPrice) / s.entryPrice * s.lastStake
		}
		snap := s.buildReviewSnapshotLocked(exitPrice, exitTime, pnl)
		s.recordOutcomeLocked(pnl)
		s.resetPositionLocked()
		s.lastStake = 0
		s.pendingEntry = nil
		learning := s.learningEnabled
		s.mu.Unlock()
		if learning {
			go s.runReview(snap)
		}
		return nil, nil
	}

	// 买单成交：首单入场（信号发出≠成交，基准价用实际成交价）。
	if order.Side == model.SideBuy && !s.inPosition {
		entry := order.AvgFillPrice
		if entry <= 0 {
			entry = order.Price
		}
		if entry <= 0 {
			s.mu.Unlock()
			return nil, nil
		}
		s.inPosition = true
		s.entryPrice = entry
		s.entryTime = order.UpdatedAt
		if s.entryTime == 0 {
			s.entryTime = order.CreatedAt
		}
		if s.entryTime == 0 {
			s.entryTime = time.Now().UnixMilli()
		}
		s.holdBars = 0
		s.positionHigh = entry
		s.positionLow = entry
		s.peakPrice = entry
		s.trailingActive = false
		s.closeEmitted = false
		s.exitReason = ""
		s.entrySnap = s.buildReviewSnapshotLocked(entry, s.entryTime, 0)
	}
	s.mu.Unlock()
	return nil, nil
}

// buildReviewSnapshotLocked 用入场时固化的决策/gate 快照 + 当前持仓状态
// 组织复盘快照（须持锁）。
func (s *AIAutoTraderStrategy) buildReviewSnapshotLocked(exitPrice float64, exitTime int64, pnl float64) *aatReviewSnapshot {
	snap := &aatReviewSnapshot{
		Symbol:       s.symbol,
		EntryPrice:   s.entryPrice,
		ExitPrice:    exitPrice,
		EntryTime:    s.entryTime,
		ExitTime:     exitTime,
		HoldBars:     s.holdBars,
		PositionHigh: s.positionHigh,
		PositionLow:  s.positionLow,
		PnL:          pnl,
		Mode:         s.mode,
	}
	if pnl > 0 {
		snap.Outcome = "win"
	} else {
		snap.Outcome = "loss"
	}
	if s.exitReason != "" {
		snap.ExitReason = s.exitReason
	} else {
		snap.ExitReason = "平仓成交"
	}
	// 决策/gate 快照优先取入场时固化版本（entrySnap 为入场瞬间构建，
	// 其 Signal/Gate 字段来自 pendingEntry），其次取当前 lastDecision。
	base := s.entrySnap
	if base != nil && base != snap {
		// entrySnap 自身已含入场时的 Signal/Gate 摘要，直接复用。
		snap.SignalReason = base.SignalReason
		snap.SignalConfidence = base.SignalConfidence
		snap.MarketCondition = base.MarketCondition
		snap.Filters = base.Filters
		snap.GateDecision = base.GateDecision
		snap.GateSizePct = base.GateSizePct
		snap.GateReason = base.GateReason
	} else if s.lastDecision != nil {
		snap.SignalReason = s.lastDecision.Reason
		snap.SignalConfidence = s.lastDecision.Confidence
		snap.MarketCondition = s.lastDecision.MarketCondition
		snap.Filters = s.lastDecision.Filters
	}
	if p := s.pendingEntry; p != nil {
		if p.Decision != nil {
			snap.SignalReason = p.Decision.Reason
			snap.SignalConfidence = p.Decision.Confidence
			snap.MarketCondition = p.Decision.MarketCondition
			snap.Filters = p.Decision.Filters
		}
		if p.Gate != nil {
			snap.GateDecision = p.Gate.Decision
			snap.GateSizePct = p.Gate.SizePct
			snap.GateReason = p.Gate.Reason
		}
	} else if s.lastGate != nil && snap.GateDecision == "" {
		snap.GateDecision = s.lastGate.Decision
		snap.GateSizePct = s.lastGate.SizePct
		snap.GateReason = s.lastGate.Reason
	}
	return snap
}

// recordOutcomeLocked 把一笔已平仓结果写入滚动窗口（cap 20）与当日计数。
func (s *AIAutoTraderStrategy) recordOutcomeLocked(pnl float64) {
	s.rolling = append(s.rolling, aatTradeOutcome{Win: pnl > 0, PnL: pnl})
	if len(s.rolling) > aatRollingWindow {
		s.rolling = s.rolling[len(s.rolling)-aatRollingWindow:]
	}
	s.todayPnL += pnl
	if s.dailyLossLimitUSDT > 0 && s.todayPnL <= -s.dailyLossLimitUSDT {
		s.dailyHalted = true
	}
}

// rollTodayLocked 内存日计数按自然日翻篇（须持锁）。
func (s *AIAutoTraderStrategy) rollTodayLocked(now time.Time) {
	key := now.Format("2006-01-02")
	if s.todayKey != key {
		s.todayKey = key
		s.todayPnL = 0
		s.dailyHalted = false
	}
}

func (s *AIAutoTraderStrategy) rollingWinsLocked() int {
	w := 0
	for _, o := range s.rolling {
		if o.Win {
			w++
		}
	}
	return w
}

func (s *AIAutoTraderStrategy) rollingLossesLocked() int {
	return len(s.rolling) - s.rollingWinsLocked()
}

// effectiveThresholdLocked 计算有效阈值：滚动窗口（≥5 笔）胜率 <35% → 基础
// 阈值 +10；>65% → −10；结果夹在 [threshold, 95]。样本不足直接返回基础阈值。
func (s *AIAutoTraderStrategy) effectiveThresholdLocked() float64 {
	eff := s.confidenceThreshold
	if len(s.rolling) >= aatRollingMinSamples {
		wr := float64(s.rollingWinsLocked()) / float64(len(s.rolling))
		switch {
		case wr < 0.35:
			eff += aatAdaptiveStep
		case wr > 0.65:
			eff -= aatAdaptiveStep
		}
	}
	if eff < s.confidenceThreshold {
		eff = s.confidenceThreshold
	}
	if eff > aatEffectiveMax {
		eff = aatEffectiveMax
	}
	return eff
}

// ── worker 主循环 ───────────────────────────────────────────────

// runScanCycle 跑一轮完整闭环：量化决策 → 自适应阈值 → LLM 闸门 → 日亏闸 →
// pendingAction。worker ticker 与测试都从这里进入；scanning 防重入。
func (s *AIAutoTraderStrategy) runScanCycle(ctx context.Context) {
	s.mu.Lock()
	if s.scanning {
		s.mu.Unlock()
		return
	}
	s.scanning = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.scanning = false
		s.mu.Unlock()
	}()

	// 快照参数（锁内），网络调用全程不持锁。
	s.mu.Lock()
	s.rollTodayLocked(time.Now())
	if s.dailyHalted {
		s.mu.Unlock()
		return // 今日闸停：当日不再开新仓
	}
	symbol := s.symbol
	mode := s.mode
	providerParam := s.provider
	marketData := s.marketData
	s.mu.Unlock()

	// 1. 量化决策。
	providerName := s.resolveProviderName(providerParam)
	engine := ai.NewDecisionEngine(ai.DecisionConfig{
		Provider:   providerName,
		Mode:       mode,
		MarketData: marketData,
	})
	dec, err := engine.Decide(ctx, symbol)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.lastDecision = nil
		s.mu.Unlock()
		log.Printf("[ai_auto_trader] %s decide failed: %v", s.instanceIDOrDefault(), err)
		return
	}

	s.mu.Lock()
	s.lastErr = ""
	s.lastDecision = dec
	effThreshold := s.effectiveThresholdLocked()
	s.mu.Unlock()

	// 2. 信号/阈值过滤：非 long 或置信度低于有效阈值 → 记录并结束本轮。
	if dec.Signal != "long" || dec.Confidence < effThreshold {
		return
	}

	// 3. LLM 闸门（参数快照，gate 内部自行读库取经验）。
	s.mu.Lock()
	gateEnabled := s.enableLLMGate
	gateCap := s.gateSizeCapPct
	expWindow := s.experienceWindow
	instanceID := s.instanceIDOrDefault()
	userID := s.userID
	maxPos := s.maxPositionUSDT
	dailyLimit := s.dailyLossLimitUSDT
	s.mu.Unlock()

	var gate *aiGateResult
	if gateEnabled {
		gate = s.runGate(providerName, dec, instanceID, userID, symbol, expWindow, gateCap)
	} else {
		gate = &aiGateResult{Decision: "approve", SizePct: 100, Reason: "LLM 闸门关闭，直接放行"}
	}

	s.mu.Lock()
	s.lastGate = gate
	// veto → 结束本轮。
	if gate.Decision == "veto" {
		s.mu.Unlock()
		return
	}
	sizePct := gate.SizePct
	if sizePct <= 0 {
		sizePct = 100
	}
	stake := maxPos * sizePct / 100
	if stake <= 0 {
		s.mu.Unlock()
		return
	}
	// 4. 日亏闸（生成动作前再校一次，防持仓平仓与学习并发翻状态）。
	s.rollTodayLocked(time.Now())
	if dailyLimit > 0 && s.todayPnL <= -dailyLimit {
		s.dailyHalted = true
		s.mu.Unlock()
		return
	}
	// 已有持仓/pending 时不叠加新动作（单槽）。
	if s.inPosition || s.pendingAction != nil {
		s.mu.Unlock()
		return
	}
	s.pendingAction = &aatPendingLong{
		Stake:    stake,
		SizePct:  sizePct,
		Decision: dec,
		Gate:     gate,
	}
	s.mu.Unlock()
	log.Printf("[ai_auto_trader] %s pending LONG: stake=%.2fUSDT size=%.0f%% gate=%s conf=%.0f",
		instanceID, stake, sizePct, gate.Decision, dec.Confidence)
}

// ── LLM 闸门 ──────────────────────────────────────────────────

// resolveProviderName 解析 LLM provider 名：参数显式指定 > 配置链
// （cfg["ai"]["defaults"]["provider"] / cfg["ai"]["provider"] / 顶层
// default_ai_provider，思路与 handler/strategy_ai.go getActiveAIProvider 一致，
// 此处不 import handler 自行实现）> env key 回落链。配置链里带的
// api_key/model/base_url 会同步进 ai 包全局注册表（与 ai_robot_worker 同模式），
// 使 DecisionEngine（按名查全局表）与 gate 用同一份凭据。
func (s *AIAutoTraderStrategy) resolveProviderName(param string) string {
	if p := strings.TrimSpace(param); p != "" {
		name := ai.NormalizeProviderName(p)
		s.syncProviderFromStore(name)
		return name
	}
	cfg := store.GetConfig()
	name := ""
	if aiCfg, ok := cfg["ai"].(map[string]any); ok {
		if defaults, ok := aiCfg["defaults"].(map[string]any); ok {
			if v, ok := defaults["provider"].(string); ok && v != "" {
				name = ai.NormalizeProviderName(v)
			}
		}
		if v, ok := aiCfg["provider"].(string); ok && v != "" {
			name = ai.NormalizeProviderName(v)
		}
	}
	if name == "" {
		if v, ok := cfg["default_ai_provider"].(string); ok && v != "" {
			name = ai.NormalizeProviderName(v)
		}
	}
	if name != "" {
		s.syncProviderFromStore(name)
		return name
	}
	// 回落链：第一个带 env key 的预置 provider。
	for _, n := range []string{"deepseek", "openai", "qwen", "hunyuan", "glm", "kimi", "claude", "gemini"} {
		if p := ai.GetProvider(n); p != nil && p.APIKey != "" {
			return n
		}
	}
	return "deepseek" // 无可用 provider：DecisionEngine 会明确报错
}

// providerStoreCfg 从 store 配置读某 provider 的自定义段（ai.{name} /
// legacy 别名 / ai.providers.{name}），返回 nil 表示无。
func providerStoreCfg(name string) map[string]any {
	cfg := store.GetConfig()
	aiCfg, ok := cfg["ai"].(map[string]any)
	if !ok {
		return nil
	}
	if pc, ok := aiCfg[name].(map[string]any); ok {
		return pc
	}
	if legacy := ai.LegacyProviderName(name); legacy != "" {
		if pc, ok := aiCfg[legacy].(map[string]any); ok {
			return pc
		}
	}
	if providers, ok := aiCfg["providers"].(map[string]any); ok {
		if pc, ok := providers[name].(map[string]any); ok {
			return pc
		}
	}
	return nil
}

// syncProviderFromStore 把 store 配置里的 api_key/model/base_url 同步进
// ai 包全局注册表（DecisionEngine 按名查全局表，必须同步而不能只克隆）。
func (s *AIAutoTraderStrategy) syncProviderFromStore(name string) {
	pc := providerStoreCfg(name)
	if pc == nil {
		return
	}
	if v, ok := pc["api_key"].(string); ok && v != "" {
		ai.SetProviderAPIKey(name, v)
	}
	if v, ok := pc["model"].(string); ok && v != "" {
		ai.SetProviderModel(name, v)
	}
	if v, ok := pc["base_url"].(string); ok && v != "" {
		ai.SetProviderBaseURL(name, v)
	}
}

// gatePrompt 构造闸门 prompt：信号摘要 + 近窗口经验（同 symbol 优先）+
// 严格 JSON 输出要求。
func buildGatePrompt(dec *ai.AIDecision, exps []*store.AIExperienceRecord) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbol: %s\n", dec.Symbol))
	sb.WriteString(fmt.Sprintf("Proposed signal: %s (confidence %.0f)\n", dec.Signal, dec.Confidence))
	sb.WriteString(fmt.Sprintf("Market condition: %s\n", dec.MarketCondition))
	sb.WriteString(fmt.Sprintf("Quant reason: %s\n", dec.Reason))
	if len(dec.Filters) > 0 {
		sb.WriteString(fmt.Sprintf("Filters: %s\n", strings.Join(dec.Filters, "; ")))
	}
	if len(exps) > 0 {
		sb.WriteString("Recent experiences (same symbol first):\n")
		for _, e := range exps {
			sb.WriteString(fmt.Sprintf("- [%s|%s] %s (pnl %.2f)\n", e.Outcome, e.Tag, e.Lesson, e.PnL))
		}
	} else {
		sb.WriteString("Recent experiences: none\n")
	}
	sb.WriteString(`
You are a risk gate for a spot long-only paper trader. Review the proposed LONG entry.
Respond ONLY with a JSON object:
{"decision":"approve|veto|reduce","size_pct":0-100,"reason":"one sentence"}
Rules: approve = full size (size_pct 100); veto = reject entirely; reduce = approve a smaller size via size_pct.`)
	return sb.String()
}

// gateJSONRe 是 gate 输出的正则兜底（与 ai.extractDecisionJSON 同思路）。
var gateJSONRe = regexp.MustCompile(`\{[^{}]*"decision"[^{}]*\}`)

// extractGateJSON 三级解析兜底：完整 JSON → 首个 { 到最后一个 } → 正则抓
// {"decision":...} 片段。
func extractGateJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		var inner []string
		for _, ln := range lines {
			if strings.HasPrefix(strings.TrimSpace(ln), "```") {
				continue
			}
			inner = append(inner, ln)
		}
		trimmed = strings.TrimSpace(strings.Join(inner, "\n"))
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}
	if idx := strings.Index(trimmed, "{"); idx != -1 {
		if end := strings.LastIndex(trimmed, "}"); end > idx {
			if candidate := trimmed[idx : end+1]; json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	if m := gateJSONRe.FindString(trimmed); m != "" {
		return m
	}
	return trimmed
}

// runGate 执行一次 LLM 闸门并归一化结论：
//   - provider 不可用 / 调用出错（内部 30s 超时）/ JSON 解析失败 → 默认放行
//     size=gate_size_cap_pct（paper 环境可用性优先），reason 记录原因；
//   - veto → 结束本轮；reduce → min(size_pct, cap)；approve → 100。
//
// 注意：本函数不持有策略锁，LLM 调用期间允许 OnBar/OnTick 正常收发事件。
func (s *AIAutoTraderStrategy) runGate(providerName string, dec *ai.AIDecision, instanceID string, userID int64, symbol string, expWindow int, gateCap float64) *aiGateResult {
	def := func(reason string) *aiGateResult {
		return &aiGateResult{Decision: "pass_default", SizePct: gateCap, Reason: reason}
	}
	p := ai.GetProvider(providerName)
	if p == nil || p.APIKey == "" {
		return def(fmt.Sprintf("provider %q 不可用，默认放行", providerName))
	}
	var exps []*store.AIExperienceRecord
	if expWindow > 0 {
		if list, err := s.expRepo.ListRecent(instanceID, symbol, expWindow); err == nil {
			exps = list
		}
	}
	resp, err := p.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: "You are a risk gate for a spot long-only paper trader. Respond with strict JSON only, no markdown, no extra text."},
			{Role: ai.RoleUser, Content: buildGatePrompt(dec, exps)},
		},
		MaxTokens:   256,
		Temperature: 0.2,
	})
	if err != nil {
		return def("gate 调用失败(30s 超时或网络错误)，默认放行: " + truncateStr(err.Error(), 80))
	}
	if len(resp.Choices) == 0 {
		return def("gate 返回空响应，默认放行")
	}
	var parsed struct {
		Decision string  `json:"decision"`
		SizePct  float64 `json:"size_pct"`
		Reason   string  `json:"reason"`
	}
	raw := extractGateJSON(resp.Choices[0].Message.Content)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return def("gate 输出解析失败，默认放行")
	}
	reason := strings.TrimSpace(parsed.Reason)
	switch strings.ToLower(strings.TrimSpace(parsed.Decision)) {
	case "veto":
		return &aiGateResult{Decision: "veto", SizePct: 0, Reason: reason}
	case "reduce":
		pct := parsed.SizePct
		if pct > gateCap {
			pct = gateCap
		}
		if pct < 0 {
			pct = 0
		}
		return &aiGateResult{Decision: "reduce", SizePct: pct, Reason: reason}
	case "approve":
		return &aiGateResult{Decision: "approve", SizePct: 100, Reason: reason}
	default:
		// 解析成功但决策字段无法识别：保守否决。
		return &aiGateResult{Decision: "veto", SizePct: 0, Reason: "gate 决策无法识别，保守否决"}
	}
}

// ── 平仓复盘学习链路 ────────────────────────────────────────────

// buildReviewPrompt 构造复盘 prompt（简化版 ai_review.go 思路：成交快照 →
// LLM → 结论），要求严格 JSON {lesson, tag} 输出。
func buildReviewPrompt(snap *aatReviewSnapshot) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbol: %s (spot long)\n", snap.Symbol))
	sb.WriteString(fmt.Sprintf("Outcome: %s (pnl %.2f)\n", snap.Outcome, snap.PnL))
	sb.WriteString(fmt.Sprintf("Entry: %.4f @ %s | Exit: %.4f @ %s (%s)\n",
		snap.EntryPrice, time.UnixMilli(snap.EntryTime).Format(time.RFC3339),
		snap.ExitPrice, time.UnixMilli(snap.ExitTime).Format(time.RFC3339), snap.ExitReason))
	sb.WriteString(fmt.Sprintf("Hold bars: %d | Range: %.4f - %.4f\n", snap.HoldBars, snap.PositionLow, snap.PositionHigh))
	sb.WriteString(fmt.Sprintf("Entry signal: confidence %.0f, condition %s, reason: %s\n",
		snap.SignalConfidence, snap.MarketCondition, snap.SignalReason))
	if len(snap.Filters) > 0 {
		sb.WriteString(fmt.Sprintf("Filters: %s\n", strings.Join(snap.Filters, "; ")))
	}
	sb.WriteString(fmt.Sprintf("Gate at entry: %s size %.0f%% (%s)\n", snap.GateDecision, snap.GateSizePct, snap.GateReason))
	sb.WriteString(`
Review this closed trade. Respond ONLY with a JSON object:
{"lesson":"one or two sentences of actionable lesson in Chinese","tag":"entry_timing|risk_management|market_regime|false_signal"}`)
	return sb.String()
}

var validExperienceTags = map[string]bool{
	"entry_timing":    true,
	"risk_management": true,
	"market_regime":   true,
	"false_signal":    true,
}

// fallbackLesson 规则兜底模板：LLM 不可用/超时/解析失败时使用。
func fallbackLesson(outcome string) (lesson, tag string) {
	if outcome == "win" {
		return "形态有效可保持", "entry_timing"
	}
	return "检查置信度与趋势一致性", "false_signal"
}

// runReview 异步复盘：交易快照 → LLM 输出 {lesson, tag} → 兜底模板 →
// 经验落 xt_ai_experiences（repo 内部单实例超 200 条删最旧）。
// 整个函数在独立 goroutine 运行，仅在写 lastLesson 时短暂持锁。
func (s *AIAutoTraderStrategy) runReview(snap *aatReviewSnapshot) {
	lesson, tag := s.reviewWithLLM(snap)
	rec := &store.AIExperienceRecord{
		InstanceID:  s.instanceIDOrDefault(),
		UserID:      s.userIDForReview(),
		Symbol:      snap.Symbol,
		Lesson:      lesson,
		Tag:         tag,
		Outcome:     snap.Outcome,
		PnL:         snap.PnL,
		ContextJSON: marshalReviewContext(snap),
		CreatedAt:   time.Now().Unix(),
	}
	if err := s.expRepo.Insert(rec); err != nil {
		log.Printf("[ai_auto_trader] %s insert experience failed: %v", rec.InstanceID, err)
		return
	}
	s.mu.Lock()
	s.lastLesson = lesson
	s.mu.Unlock()
	log.Printf("[ai_auto_trader] %s experience saved: [%s|%s] %s", rec.InstanceID, snap.Outcome, tag, lesson)
}

// userIDForReview 读取 userID 快照（不持锁读：int64 原子量级，复盘 goroutine
// 里为避免与 Stop 死锁直接快照调用方已捕获的值）。
func (s *AIAutoTraderStrategy) userIDForReview() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userID
}

// reviewWithLLM 调 LLM 生成复盘教训；provider 不可用/出错/解析失败/tag
// 非法时走规则兜底模板。provider 解析与主循环一致：参数显式指定 > 配置链。
func (s *AIAutoTraderStrategy) reviewWithLLM(snap *aatReviewSnapshot) (lesson, tag string) {
	s.mu.RLock()
	providerParam := s.provider
	s.mu.RUnlock()
	name := s.resolveProviderName(providerParam)
	p := ai.GetProvider(name)
	if p == nil || p.APIKey == "" {
		return fallbackLesson(snap.Outcome)
	}
	resp, err := p.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: "You are a post-trade review coach for a quantitative trading system. Respond with strict JSON only, no markdown."},
			{Role: ai.RoleUser, Content: buildReviewPrompt(snap)},
		},
		MaxTokens:   256,
		Temperature: 0.3,
	})
	if err != nil || len(resp.Choices) == 0 {
		return fallbackLesson(snap.Outcome)
	}
	var parsed struct {
		Lesson string `json:"lesson"`
		Tag    string `json:"tag"`
	}
	raw := extractGateJSON(resp.Choices[0].Message.Content)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return fallbackLesson(snap.Outcome)
	}
	lesson = strings.TrimSpace(parsed.Lesson)
	tag = strings.TrimSpace(parsed.Tag)
	if lesson == "" || !validExperienceTags[tag] {
		fbLesson, fbTag := fallbackLesson(snap.Outcome)
		if lesson == "" {
			lesson = fbLesson
		}
		if !validExperienceTags[tag] {
			tag = fbTag
		}
	}
	return lesson, tag
}

// marshalReviewContext 把复盘快照序列化为经验库 context_json。
func marshalReviewContext(snap *aatReviewSnapshot) string {
	raw, err := json.Marshal(snap)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// Ensure AIAutoTraderStrategy 满足接口（编译期校验）。
var _ strategy.Strategy = (*AIAutoTraderStrategy)(nil)
