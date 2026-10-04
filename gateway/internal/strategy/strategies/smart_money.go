package strategies

// SmartMoneyStrategy（主力行为策略）：每根闭合 K 线跑一遍主力行为分析
// （Wyckoff 量价状态机，见 internal/analysis），在阶段转换点产生信号：
//
//	洗盘确认（弹簧线收回）/ 拉升启动（放量破区间） → LONG
//	出货确认（顶部高潮/上冲破+背离）              → SHORT
//	拉升结束回落吸筹                              → CLOSE
//
// 单仓位、阶段转换才出手（非逐 bar）。重启按账本净持仓重建（PositionRestorer）。
// 数量：信号不带 Qty，由引擎 applyTradeHooks 按 CustomStakeAmount
// （position_size USDT ÷ 最新价）折算——与 liquidity_heat 同一契约。

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/analysis"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

const smartMoneyMaxBars = 400

type SmartMoneyStrategy struct {
	strategy.BaseStrategy
	name         string
	symbol       string
	timeframe    string
	positionSize float64 // 单次下单 USDT 本金（CustomStakeAmount 折算数量）
	running      bool
	mu           sync.RWMutex

	bars      []model.Bar
	lastPhase analysis.Phase
	lastRes   *analysis.Result // 最近一次分析结论（运行面板直出）

	inPosition bool
	entryPrice float64
}

func NewSmartMoneyStrategy() *SmartMoneyStrategy {
	return &SmartMoneyStrategy{
		name:         "smart_money",
		symbol:       "BTCUSDT",
		timeframe:    "1h",
		positionSize: 100,
	}
}

func (s *SmartMoneyStrategy) Name() string   { return s.name }
func (s *SmartMoneyStrategy) Symbol() string { return s.symbol }
func (s *SmartMoneyStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// PrimaryTimeframe 声明主周期：引擎只把该周期 K 线分发进 OnBar。
func (s *SmartMoneyStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeframe
}

func (s *SmartMoneyStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{"symbol": s.symbol, "timeframe": s.timeframe, "position_size": s.positionSize}
}

// 阈值固定于 analysis 包常量，不进参数注册表——返回 nil 让引擎键直通。
func (s *SmartMoneyStrategy) GetParameters() *strategy.ParamRegistry { return nil }
func (s *SmartMoneyStrategy) ValidateParams() error                  { return nil }

// ApplyParams 应用 symbol/timeframe/position_size。约定与项目内其他策略一致：
// 本函数不自加锁，调用方（Start/引擎）持锁——Start 持 s.mu 再调这里，自锁
// 会死锁（2026-10-04 启动挂死实证，全引擎对该实例的查询随 s.mu 一起冻结）。
func (s *SmartMoneyStrategy) ApplyParams(m map[string]any) error {
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if tf := getString(m, "timeframe", ""); tf != "" {
		s.timeframe = strings.ToLower(strings.TrimSpace(tf))
	}
	if v := getFloat(m, "position_size", 0); v > 0 {
		s.positionSize = v
	}
	return nil
}
func (s *SmartMoneyStrategy) ParamDefs() []map[string]any { return nil }

// CustomStakeAmount：引擎 applyTradeHooks 折算下单数量（Qty=stake/最新价），
// 与 liquidity_heat 同一契约（2026-10-04 补——此前无该钩子，信号量落到
// resolveSignalQuantity 的"可用余额 10%"兜底，单笔名义不可控）。可用余额
// 不足退化为全额可用（现货只做多）。
func (s *SmartMoneyStrategy) CustomStakeAmount(availableBalance float64, _ *model.Signal) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stake := s.positionSize
	if stake <= 0 {
		return 0
	}
	if availableBalance > 0 && stake > availableBalance {
		return availableBalance
	}
	return stake
}

func (s *SmartMoneyStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("smart_money apply params: %w", err)
	}
	s.bars = nil
	s.lastPhase = analysis.PhaseNone
	s.lastRes = nil
	s.inPosition = false
	s.entryPrice = 0
	// 重启仓位重建（经典策略同款机制，2026-10-04）。
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		s.inPosition = true
		s.entryPrice = v
		log.Printf("[smart_money] %s 重启仓位重建: qty=%.6f vwap=%.2f", s.name, q, v)
	}
	s.running = true
	return nil
}

func (s *SmartMoneyStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.bars = nil
	s.inPosition = false
	s.lastRes = nil
	return nil
}

// RestorePosition PositionRestorer 接口。
func (s *SmartMoneyStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if qty > 0 {
		s.inPosition = true
		s.entryPrice = avgPrice
	}
	return nil
}

func (s *SmartMoneyStrategy) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *SmartMoneyStrategy) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *SmartMoneyStrategy) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *SmartMoneyStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if bar.Symbol != "" && bar.Symbol != s.symbol {
		return nil, nil
	}
	s.bars = append(s.bars, bar)
	if len(s.bars) > smartMoneyMaxBars {
		s.bars = s.bars[len(s.bars)-smartMoneyMaxBars:]
	}
	if len(s.bars) < 80 {
		return nil, nil
	}

	res := analysis.Analyze(s.bars, s.symbol, s.timeframe)
	s.lastRes = res
	prev := s.lastPhase
	s.lastPhase = res.Phase
	if prev == res.Phase {
		return nil, nil // 只在阶段转换点出手
	}

	mark := func(sig *model.Signal) *model.Signal {
		sig.Symbol = s.symbol
		sig.Strategy = s.name
		sig.Timestamp = bar.Time
		return sig
	}

	// 持仓中：出货/回落 → 平仓信号。
	if s.inPosition && (res.Phase == analysis.PhaseDistrib ||
		(res.Phase == analysis.PhaseAccum && (prev == analysis.PhaseMarkup || prev == analysis.PhaseShakeout))) {
		s.inPosition = false
		dir := "CLOSE"
		if res.Phase == analysis.PhaseDistrib {
			dir = "SHORT" // 合约可直接反手；现货语义=卖出持仓
		}
		return mark(&model.Signal{Direction: dir, Strength: 0.75,
			Reason: fmt.Sprintf("主力行为: %s → %s，离场", PhaseLabelCN(prev), PhaseLabelCN(res.Phase))}), nil
	}

	// 空仓中：洗盘结束/拉升启动 → 买入。
	if !s.inPosition {
		if res.Phase == analysis.PhaseShakeout && prev != analysis.PhaseShakeout {
			s.inPosition = true
			s.entryPrice = bar.Close
			return mark(&model.Signal{Direction: "LONG", Strength: 0.7,
				Reason: "主力行为: 洗盘确认（弹簧线假跌破收回），护盘价做多"}), nil
		}
		if res.Phase == analysis.PhaseMarkup && (prev == analysis.PhaseAccum || prev == analysis.PhaseShakeout || prev == analysis.PhaseBuilding) {
			s.inPosition = true
			s.entryPrice = bar.Close
			return mark(&model.Signal{Direction: "LONG", Strength: 0.75,
				Reason: "主力行为: 拉升启动（放量突破吸筹区间）"}), nil
		}
	}
	return nil, nil
}

// PhaseLabelCN 阶段中文标签（信号文案用）。
func PhaseLabelCN(p analysis.Phase) string {
	if m, ok := analysis.PhaseMeta[p]; ok {
		return m.Label
	}
	return string(p)
}

// RuntimeStatus 直出最近一次分析结论（运行面板的数据源：阶段/置信度/
// 证据/关键位——完整证据链让面板不用二次请求分析接口）。
func (s *SmartMoneyStrategy) RuntimeStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := map[string]any{
		"running":        s.running,
		"bars_collected": len(s.bars),
		"in_position":    s.inPosition,
	}
	if s.inPosition && s.entryPrice > 0 {
		m["entry_price"] = s.entryPrice
	}
	if s.lastRes != nil {
		m["sm_phase"] = string(s.lastRes.Phase)
		m["sm_phase_label"] = PhaseLabelCN(s.lastRes.Phase)
		m["sm_confidence"] = s.lastRes.Confidence
		if len(s.lastRes.Evidence) > 0 {
			m["sm_evidence"] = s.lastRes.Evidence
		}
		if len(s.lastRes.Levels) > 0 {
			m["sm_levels"] = s.lastRes.Levels
		}
		if s.lastRes.Signal != nil {
			m["sm_signal"] = s.lastRes.Signal
		}
	}
	return m
}
