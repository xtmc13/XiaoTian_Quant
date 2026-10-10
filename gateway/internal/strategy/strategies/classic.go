package strategies

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── EMACrossStrategy ───────────────────────────────────────────
// Enters when fast EMA crosses above/below slow EMA.

// classicPosition 四个经典指标策略（EMA/MACD/RSI/Bollinger）共用的最小持仓
// 状态与重启恢复（M10：MACD 2026-10-04 的恢复语义推广到其余三个）：handler
// 按本地成交账本净持仓注入 restored_position_*，认领后 inPosition=true，
// 暖机重放只管理已有仓（SL/TP），不再重复入场。
type classicPosition struct {
	inPosition bool
	entryPrice float64
	direction  string
	// spotOnly 现货只做多钳制（M9）：现货无持仓卖出会被交易所拒绝，反向交叉
	// 直接发 SHORT 会让 inPosition 永不复位（策略永久"持仓中"）。显式现货
	// （market_type=spot / category=spot）才钳；未声明市场的回测/直调参数
	// 保持双向历史行为不变。
	spotOnly bool
}

// restore 重启仓位重建。direction 为持仓方向（"LONG"/"SHORT"，空值按
// LONG——RestorePosition 直调路径无方向参数，保持净多历史默认；生产路径经
// Start 注入 restored_position_side，空单按账本符号如实恢复）。
func (p *classicPosition) restore(qty, avgPrice float64, direction string) {
	p.inPosition = true
	p.entryPrice = avgPrice
	p.direction = "LONG"
	if direction == "SHORT" {
		p.direction = "SHORT"
	}
	if p.entryPrice <= 0 {
		p.entryPrice = 0.01 // 无均价时的占位（checkExit 需要非零基准）
	}
}

// classicSpotOnly 判定启动参数是否为现货语义：显式 spot 钳制；
// swap/futures/margin 或 category=contract 为合约（双向）；缺省不钳。
func classicSpotOnly(m map[string]any) bool {
	mt, _ := m["market_type"].(string)
	switch strings.ToLower(strings.TrimSpace(mt)) {
	case "spot":
		return true
	case "swap", "futures", "margin":
		return false
	}
	cat, _ := m["category"].(string)
	return strings.EqualFold(strings.TrimSpace(cat), "spot")
}

type EMACrossStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	fastPeriod    int
	slowPeriod    int
	stopLossPct   float64
	takeProfitPct float64
	positionSize  float64

	bars []model.Bar
	classicPosition

	params *strategy.ParamRegistry
}

func NewEMACrossStrategy() *EMACrossStrategy {
	s := &EMACrossStrategy{
		name:          "ema_cross",
		symbol:        "BTCUSDT",
		fastPeriod:    12,
		slowPeriod:    26,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("fast_period", 12, 5, 50, "buy"))
	s.params.Register(strategy.IntParameter("slow_period", 26, 10, 100, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *EMACrossStrategy) Name() string   { return s.name }
func (s *EMACrossStrategy) Symbol() string { return s.symbol }

func (s *EMACrossStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"fast_period":     s.fastPeriod,
		"slow_period":     s.slowPeriod,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *EMACrossStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *EMACrossStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("ema_cross apply params: %w", err)
	}
	if s.fastPeriod >= s.slowPeriod {
		s.fastPeriod = 12
		s.slowPeriod = 26
	}
	// 重启仓位重建（与 MACD 同语义，M10）：handler 按账本净持仓注入
	// restored_position_*，认领后暖机重放只管理已有仓，不再重复入场。
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		dir := "LONG"
		if sd, _ := params["restored_position_side"].(string); sd == "short" {
			dir = "SHORT"
		}
		s.classicPosition.restore(q, v, dir)
		log.Printf("[ema_cross] %s 重启仓位重建: qty=%.6f vwap=%.2f direction=%s", s.symbol, q, v, s.direction)
	}
	s.running = true
	return nil
}

// RestorePosition PositionRestorer 接口（引擎/工具链直调路径，无方向参数，
// 按净多恢复；生产方向感知的重建走 Start 的 restored_position_side 注入）。
func (s *EMACrossStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.classicPosition.restore(qty, avgPrice, "LONG")
	return nil
}

func (s *EMACrossStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *EMACrossStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}
func (s *EMACrossStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if p := s.params.Get("fast_period"); p != nil {
		s.fastPeriod = p.GetInt()
	}
	if p := s.params.Get("slow_period"); p != nil {
		s.slowPeriod = p.GetInt()
	}
	if p := s.params.Get("stop_loss_pct"); p != nil {
		s.stopLossPct = p.GetFloat()
	}
	if p := s.params.Get("take_profit_pct"); p != nil {
		s.takeProfitPct = p.GetFloat()
	}
	if p := s.params.Get("position_size"); p != nil {
		s.positionSize = p.GetFloat()
	}
	s.spotOnly = classicSpotOnly(m)
	return nil
}
func (s *EMACrossStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *EMACrossStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *EMACrossStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *EMACrossStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *EMACrossStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *EMACrossStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.slowPeriod+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	fastEMA := ema(closes, s.fastPeriod)
	slowEMA := ema(closes, s.slowPeriod)
	prevFast := ema(closes[:len(closes)-1], s.fastPeriod)
	prevSlow := ema(closes[:len(closes)-1], s.slowPeriod)

	if prevFast <= prevSlow && fastEMA > slowEMA {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "EMA golden cross"}, nil
	}
	if prevFast >= prevSlow && fastEMA < slowEMA {
		if s.spotOnly {
			// 现货只做多（M9）：死亡交叉不开空——无持仓卖出被拒会让
			// inPosition 永不复位，策略永久卡死。
			return nil, nil
		}
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "EMA death cross"}, nil
	}
	return nil, nil
}

func (s *EMACrossStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── MACDStrategy ───────────────────────────────────────────────
// Enters when MACD line crosses above/below signal line.

type MACDStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	fastPeriod    int
	slowPeriod    int
	signalPeriod  int
	stopLossPct   float64
	takeProfitPct float64
	positionSize  float64

	bars []model.Bar
	classicPosition

	// 最近信号（RuntimeStatus 展示用）：时间与方向，信号产生时更新。
	lastSignalTime      int64 // Unix 毫秒
	lastSignalDirection string

	params *strategy.ParamRegistry
}

func NewMACDStrategy() *MACDStrategy {
	s := &MACDStrategy{
		name:          "macd",
		symbol:        "BTCUSDT",
		fastPeriod:    12,
		slowPeriod:    26,
		signalPeriod:  9,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("fast_period", 12, 5, 50, "buy"))
	s.params.Register(strategy.IntParameter("slow_period", 26, 10, 100, "buy"))
	s.params.Register(strategy.IntParameter("signal_period", 9, 3, 30, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *MACDStrategy) Name() string   { return s.name }
func (s *MACDStrategy) Symbol() string { return s.symbol }

func (s *MACDStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"fast_period":     s.fastPeriod,
		"slow_period":     s.slowPeriod,
		"signal_period":   s.signalPeriod,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *MACDStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *MACDStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("macd apply params: %w", err)
	}
	// 重启仓位重建（同 liquidity_heat 的 PositionRestorer 语义，2026-10-04）：
	// 经典策略此前没有该机制——每次重启都白纸一张，暖机重放里金叉条件仍在
	// 就再开一单（MACD SOL 实例三次重启三次"加仓"实证，持仓 12.5 SOL =
	// 3×4.17）。handler 按账本净持仓注入 restored_position_*，此处认领后
	// inPosition=true，暖机重放只会管理已有仓（SL/TP），不再重复入场。
	// H1 片（2026-10-09）：方向随账本净额符号注入（restored_position_side），
	// 合约空单（净额<0）恢复为 SHORT 仓，checkExit 的空单 TP/SL 镜像继续管理。
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		dir := "LONG"
		if sd, _ := params["restored_position_side"].(string); sd == "short" {
			dir = "SHORT"
		}
		s.restorePositionLocked(q, v, dir)
	}
	s.running = true
	return nil
}

// restorePositionLocked 重启仓位重建实现。须持锁。direction 为持仓方向
// （"LONG"/"SHORT"，空值按 LONG——RestorePosition 直调路径无方向参数，
// 保持净多历史默认；生产路径经 Start 注入 restored_position_side，空单
// 按账本符号如实恢复）。M10 起状态迁移逻辑由 classicPosition 共享承载。
func (s *MACDStrategy) restorePositionLocked(qty, avgPrice float64, direction string) {
	s.classicPosition.restore(qty, avgPrice, direction)
	log.Printf("[macd] %s 重启仓位重建: qty=%.6f vwap=%.2f direction=%s", s.name, qty, avgPrice, s.direction)
}

// RestorePosition PositionRestorer 接口（引擎/工具链直调路径，无方向参数，
// 按净多恢复；生产方向感知的重建走 Start 的 restored_position_side 注入）。
func (s *MACDStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restorePositionLocked(qty, avgPrice, "LONG")
	return nil
}

func (s *MACDStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *MACDStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}
func (s *MACDStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if p := s.params.Get("fast_period"); p != nil {
		s.fastPeriod = p.GetInt()
	}
	if p := s.params.Get("slow_period"); p != nil {
		s.slowPeriod = p.GetInt()
	}
	if p := s.params.Get("signal_period"); p != nil {
		s.signalPeriod = p.GetInt()
	}
	if p := s.params.Get("stop_loss_pct"); p != nil {
		s.stopLossPct = p.GetFloat()
	}
	if p := s.params.Get("take_profit_pct"); p != nil {
		s.takeProfitPct = p.GetFloat()
	}
	if p := s.params.Get("position_size"); p != nil {
		s.positionSize = p.GetFloat()
	}
	s.spotOnly = classicSpotOnly(m)
	return nil
}
func (s *MACDStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *MACDStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

// RuntimeStatus exposes live runtime state for the running panel.
// Field names are snake_case for the frontend.
func (s *MACDStrategy) RuntimeStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := map[string]any{
		"running":        s.running,
		"in_position":    s.inPosition,
		"bars_collected": len(s.bars),
	}
	if s.inPosition {
		m["direction"] = s.direction
		m["entry_price"] = s.entryPrice
	}
	if s.lastSignalTime > 0 {
		m["last_signal_time"] = s.lastSignalTime
		m["last_signal_direction"] = s.lastSignalDirection
	}
	return m
}

func (s *MACDStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *MACDStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *MACDStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *MACDStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.slowPeriod+s.signalPeriod+5 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	macdLine, signalLine := macd(closes, s.fastPeriod, s.slowPeriod, s.signalPeriod)
	prevMACD, prevSignal := macd(closes[:len(closes)-1], s.fastPeriod, s.slowPeriod, s.signalPeriod)

	if prevMACD <= prevSignal && macdLine > signalLine {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		s.lastSignalTime = time.Now().UnixMilli()
		s.lastSignalDirection = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.75, Strategy: s.name, Reason: "MACD bullish crossover"}, nil
	}
	if prevMACD >= prevSignal && macdLine < signalLine {
		if s.spotOnly {
			// 现货只做多（M9）：死叉不开空（无持仓卖出被拒会永久卡死）。
			return nil, nil
		}
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		s.lastSignalTime = time.Now().UnixMilli()
		s.lastSignalDirection = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.75, Strategy: s.name, Reason: "MACD bearish crossover"}, nil
	}
	return nil, nil
}

func (s *MACDStrategy) checkExit(bar model.Bar) *model.Signal {
	record := func(sig *model.Signal) *model.Signal {
		s.lastSignalTime = time.Now().UnixMilli()
		s.lastSignalDirection = "CLOSE"
		return sig
	}
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return record(&model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"})
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return record(&model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"})
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return record(&model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"})
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return record(&model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"})
		}
	}
	return nil
}

// ── RSIStrategy ────────────────────────────────────────────────
// Enters when RSI exits overbought/oversold zones.

type RSIStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	period        int
	overbought    float64
	oversold      float64
	stopLossPct   float64
	takeProfitPct float64
	positionSize  float64

	bars []model.Bar
	classicPosition

	params *strategy.ParamRegistry
}

func NewRSIStrategy() *RSIStrategy {
	s := &RSIStrategy{
		name:          "rsi",
		symbol:        "BTCUSDT",
		period:        14,
		overbought:    70,
		oversold:      30,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("period", 14, 5, 50, "buy"))
	s.params.Register(strategy.FloatParameter("overbought", 70, 50, 90, 1, "buy"))
	s.params.Register(strategy.FloatParameter("oversold", 30, 10, 50, 1, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *RSIStrategy) Name() string   { return s.name }
func (s *RSIStrategy) Symbol() string { return s.symbol }

func (s *RSIStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"period":          s.period,
		"overbought":      s.overbought,
		"oversold":        s.oversold,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *RSIStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *RSIStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("rsi apply params: %w", err)
	}
	// 重启仓位重建（与 MACD 同语义，M10）。
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		dir := "LONG"
		if sd, _ := params["restored_position_side"].(string); sd == "short" {
			dir = "SHORT"
		}
		s.classicPosition.restore(q, v, dir)
		log.Printf("[rsi] %s 重启仓位重建: qty=%.6f vwap=%.2f direction=%s", s.symbol, q, v, s.direction)
	}
	s.running = true
	return nil
}

// RestorePosition PositionRestorer 接口（引擎/工具链直调路径，无方向参数，
// 按净多恢复；生产方向感知的重建走 Start 的 restored_position_side 注入）。
func (s *RSIStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.classicPosition.restore(qty, avgPrice, "LONG")
	return nil
}

func (s *RSIStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *RSIStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}
func (s *RSIStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if p := s.params.Get("period"); p != nil {
		s.period = p.GetInt()
	}
	if p := s.params.Get("overbought"); p != nil {
		s.overbought = p.GetFloat()
	}
	if p := s.params.Get("oversold"); p != nil {
		s.oversold = p.GetFloat()
	}
	if p := s.params.Get("stop_loss_pct"); p != nil {
		s.stopLossPct = p.GetFloat()
	}
	if p := s.params.Get("take_profit_pct"); p != nil {
		s.takeProfitPct = p.GetFloat()
	}
	if p := s.params.Get("position_size"); p != nil {
		s.positionSize = p.GetFloat()
	}
	s.spotOnly = classicSpotOnly(m)
	return nil
}
func (s *RSIStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *RSIStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *RSIStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *RSIStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *RSIStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *RSIStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.period+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	rsiVal := rsi(closes, s.period)
	prevRSI := rsi(closes[:len(closes)-1], s.period)

	if prevRSI < s.oversold && rsiVal >= s.oversold {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "RSI exit oversold"}, nil
	}
	if prevRSI > s.overbought && rsiVal <= s.overbought {
		if s.spotOnly {
			// 现货只做多（M9）：超买回落不开空。
			return nil, nil
		}
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "RSI exit overbought"}, nil
	}
	return nil, nil
}

func (s *RSIStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── BollingerBandsStrategy ─────────────────────────────────────
// Enters when price touches/exits Bollinger Bands.

type BollingerBandsStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	period        int
	stdDev        float64
	stopLossPct   float64
	takeProfitPct float64
	positionSize  float64

	bars []model.Bar
	classicPosition

	params *strategy.ParamRegistry
}

func NewBollingerBandsStrategy() *BollingerBandsStrategy {
	s := &BollingerBandsStrategy{
		name:          "bollinger_bands",
		symbol:        "BTCUSDT",
		period:        20,
		stdDev:        2.0,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("period", 20, 10, 50, "buy"))
	s.params.Register(strategy.FloatParameter("std_dev", 2.0, 1.0, 3.0, 0.1, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *BollingerBandsStrategy) Name() string   { return s.name }
func (s *BollingerBandsStrategy) Symbol() string { return s.symbol }

func (s *BollingerBandsStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"period":          s.period,
		"std_dev":         s.stdDev,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *BollingerBandsStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *BollingerBandsStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("bollinger_bands apply params: %w", err)
	}
	// 重启仓位重建（与 MACD 同语义，M10）。
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		dir := "LONG"
		if sd, _ := params["restored_position_side"].(string); sd == "short" {
			dir = "SHORT"
		}
		s.classicPosition.restore(q, v, dir)
		log.Printf("[bollinger_bands] %s 重启仓位重建: qty=%.6f vwap=%.2f direction=%s", s.symbol, q, v, s.direction)
	}
	s.running = true
	return nil
}

// RestorePosition PositionRestorer 接口（引擎/工具链直调路径，无方向参数，
// 按净多恢复；生产方向感知的重建走 Start 的 restored_position_side 注入）。
func (s *BollingerBandsStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.classicPosition.restore(qty, avgPrice, "LONG")
	return nil
}

func (s *BollingerBandsStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *BollingerBandsStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}
func (s *BollingerBandsStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if sym := getString(m, "symbol", ""); sym != "" {
		s.symbol = strings.ToUpper(strings.TrimSpace(sym))
	}
	if p := s.params.Get("period"); p != nil {
		s.period = p.GetInt()
	}
	if p := s.params.Get("std_dev"); p != nil {
		s.stdDev = p.GetFloat()
	}
	if p := s.params.Get("stop_loss_pct"); p != nil {
		s.stopLossPct = p.GetFloat()
	}
	if p := s.params.Get("take_profit_pct"); p != nil {
		s.takeProfitPct = p.GetFloat()
	}
	if p := s.params.Get("position_size"); p != nil {
		s.positionSize = p.GetFloat()
	}
	s.spotOnly = classicSpotOnly(m)
	return nil
}
func (s *BollingerBandsStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *BollingerBandsStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *BollingerBandsStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *BollingerBandsStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *BollingerBandsStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *BollingerBandsStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.period+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	upper, lower := bollingerBands(closes, s.period, s.stdDev)
	prevUpper, prevLower := bollingerBands(closes[:len(closes)-1], s.period, s.stdDev)
	c := closes[len(closes)-1]
	prevC := closes[len(closes)-2]

	if prevC <= prevLower && c > lower {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "price bounced off lower band"}, nil
	}
	if prevC >= prevUpper && c < upper {
		if s.spotOnly {
			// 现货只做多（M9）：上轨回落不开空。
			return nil, nil
		}
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "price rejected at upper band"}, nil
	}
	return nil, nil
}

func (s *BollingerBandsStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── Technical Indicator Helpers ────────────────────────────────

func sma(data []float64, period int) float64 {
	if period > len(data) {
		period = len(data)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

func ema(data []float64, period int) float64 {
	if len(data) < period {
		return sma(data, len(data))
	}
	alpha := 2.0 / float64(period+1)
	e := data[0]
	for i := 1; i < len(data); i++ {
		e = alpha*data[i] + (1-alpha)*e
	}
	return e
}

func stdDev(data []float64, period int) float64 {
	if period > len(data) {
		period = len(data)
	}
	if period < 2 {
		return 0
	}
	slice := data[len(data)-period:]
	m := sma(slice, period)
	sumSq := 0.0
	for _, v := range slice {
		d := v - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(period-1))
}

func rsi(data []float64, period int) float64 {
	if len(data) < period+1 {
		return 50
	}
	gains, losses := 0.0, 0.0
	for i := len(data) - period; i < len(data); i++ {
		diff := data[i] - data[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses += -diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func macd(data []float64, fast, slow, signal int) (macdLine, signalLine float64) {
	if len(data) < slow+signal {
		return 0, 0
	}
	fastEMA := ema(data, fast)
	slowEMA := ema(data, slow)
	macdLine = fastEMA - slowEMA

	// Signal line is EMA of MACD values — approximate with current MACD
	// For simplicity, compute signal from recent MACD values
	macdVals := make([]float64, signal+1)
	for i := 0; i <= signal; i++ {
		slice := data[:len(data)-i]
		if len(slice) >= slow {
			macdVals[i] = ema(slice, fast) - ema(slice, slow)
		}
	}
	signalLine = ema(macdVals, signal)
	return macdLine, signalLine
}

func bollingerBands(data []float64, period int, multiplier float64) (upper, lower float64) {
	if len(data) < period {
		return 0, 0
	}
	m := sma(data, period)
	sd := stdDev(data, period)
	upper = m + multiplier*sd
	lower = m - multiplier*sd
	return upper, lower
}
