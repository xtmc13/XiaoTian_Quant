package cra

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// BaseCRAStrategy implements the shared CRA spot/contract execution engine.
type BaseCRAStrategy struct {
	strategy.BaseStrategy

	name       string
	symbol     string
	isContract bool

	mu      sync.RWMutex
	running bool
	params  *CRAParams
	state   *CRAState
	bars    []model.Bar

	// 最近信号（RuntimeStatus 展示用）。
	lastSignalTime      int64 // Unix 毫秒
	lastSignalDirection string

	// A2 多周期指标 bar 供给：引擎 Start 后经 SetBarProvider 注入
	//（BarProvider/MarketData）；nil = 未接线，指标周期一律用工作周期 bar。
	barProvider strategy.BarProvider
	// warnedFeeds 按指标周期去重"降级到工作周期"的 WARN 日志（Start 重置）。
	warnedFeeds map[string]bool

	flashDetector *strategy.FlashCrashDetector
	logger        *logging.Logger
}

// NewCRASpotStrategy creates a spot CRA strategy instance.
func NewCRASpotStrategy(name, symbol string) *BaseCRAStrategy {
	return &BaseCRAStrategy{
		name:        name,
		symbol:      symbol,
		isContract:  false,
		state:       &CRAState{},
		warnedFeeds: map[string]bool{},
		logger:      logging.New("cra_spot"),
	}
}

// NewCRAContractStrategy creates a contract CRA strategy instance.
func NewCRAContractStrategy(name, symbol string) *BaseCRAStrategy {
	return &BaseCRAStrategy{
		name:        name,
		symbol:      symbol,
		isContract:  true,
		state:       &CRAState{},
		warnedFeeds: map[string]bool{},
		logger:      logging.New("cra_contract"),
	}
}

func (s *BaseCRAStrategy) Name() string   { return s.name }
func (s *BaseCRAStrategy) Symbol() string { return s.symbol }

func (s *BaseCRAStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *BaseCRAStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	b, _ := json.Marshal(s.params)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["symbol"] = s.symbol
	return m
}

// Start initializes the strategy from a flattened parameter map.
func (s *BaseCRAStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if params == nil {
		params = map[string]any{}
	}
	// Preserve symbol/direction/market_type from top-level params.
	if v, ok := params["symbol"].(string); ok && v != "" {
		s.symbol = v
	}

	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("cra start: marshal params: %w", err)
	}
	p, err := ParseCRAParams(string(data))
	if err != nil {
		return fmt.Errorf("cra start: parse params: %w", err)
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("cra start: validate: %w", err)
	}
	if s.isContract {
		p.MarketType = "swap"
	} else {
		p.MarketType = "spot"
	}
	if s.isContract && p.Leverage < 1 {
		p.Leverage = 1
	}

	s.params = p
	s.state = &CRAState{}
	s.bars = nil
	s.warnedFeeds = map[string]bool{}
	if p.WaterfallEnabled {
		s.flashDetector = strategy.NewFlashCrashDetectorWithParams(time.Minute, p.WaterfallProtection)
	} else {
		s.flashDetector = nil
	}

	// 重启仓位重建（handler 按本地成交账本注入 Start 参数）：在清态之后、
	// 暖机重放之前恢复持仓（与 liquidity_heat Start 收尾同模式，2026-10-01
	// 修复注入时序）。逐笔明细 restored_fills 重放优先——各档成本精确
	// （尾单/首尾止盈依赖档级成本）；聚合 restored_position_qty/vwap 兜底
	// ——净持仓合成单档，tail/head_tail 退化为均价判定的单档。
	if lots, side, prevSide, prevAdds, burnDual, burnGlobal := parseRestoredLots(params, p); len(lots) > 0 {
		s.restoreLotsLocked(lots, side)
		// 顺势换向锚点随逐笔重放一并重建（E 片重启存续）：最近一个全平
		// 循环的 {方向, 峰值补仓次数}。聚合兜底路径（restored_position_qty
		// 无逐笔明细）无法反推历史循环，锚点降级为 0——重启后首轮不放大。
		s.state.PrevLoopSide = prevSide
		s.state.PrevLoopTrappedAdds = prevAdds
		// 燃烧 fired 标记随逐笔重放按数量形态推断（G2 重启存续，推断口径与
		// 局限见 RebuildBurnMarksFromFills）：restoreLotsLocked 内部
		// ResetForNextLoop 会清零，须在其后写入。聚合兜底路径无逐笔明细，
		// 标记保持 false（退回推断前行为：重启后达档重新评估触发一次）。
		s.state.BurnDualFired = burnDual
		s.state.BurnGlobalFired = burnGlobal
	} else if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		s.restorePositionLocked(q, v)
	}

	s.running = true
	s.logger.Info("cra strategy started", "symbol", s.symbol, "type", s.name, "market", p.MarketType)
	return nil
}

func (s *BaseCRAStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.state != nil {
		s.state.Reset()
	}
	s.bars = nil
	s.logger.Info("cra strategy stopped", "symbol", s.symbol)
	return nil
}

// RuntimeStatus exposes live runtime state for the running panel.
// Field names are snake_case for the frontend. Only state that actually
// exists is reported (no fabrication).
func (s *BaseCRAStrategy) RuntimeStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := map[string]any{
		"running":        s.running,
		"bars_collected": len(s.bars),
	}
	if s.params != nil {
		m["tp_mode"] = s.params.TPMode
		m["total_add_tiers"] = len(s.params.AddPositions)
		m["order_count"] = s.params.OrderCount
	}
	st := s.state
	if st != nil {
		m["in_position"] = st.InPosition
		m["loops_executed"] = st.LoopExecuted
		m["waterfall_paused"] = st.WaterfallPaused
		// filled_orders = 已成交入场订单数（含首单）；当前档位同义。
		m["filled_orders"] = st.PositionCount
		m["current_tier"] = st.PositionCount
		if st.PositionCount > 1 {
			m["add_positions_triggered"] = st.PositionCount - 1
		} else {
			m["add_positions_triggered"] = 0
		}
		m["pending_add_count"] = st.PendingAddCount
		if st.InPosition {
			m["direction"] = string(st.Side)
			m["entry_price"] = st.EntryPrice
			m["avg_entry_price"] = st.AvgEntryPrice
			m["position_qty"] = st.TotalQty
			m["position_cost"] = st.TotalCost
			// open_lots = 分档记账的当前档数（0=聚合/旧态未分档）。
			m["open_lots"] = len(st.Lots)
			// F 片燃烧斩仓触发状态：只在对应开关启用时透出（默认双关不新增
			// 键，严格零行为变化）。
			if s.params.BurnDualEnabled {
				m["burn_dual_fired"] = st.BurnDualFired
			}
			if s.params.BurnGlobalEnabled {
				m["burn_global_fired"] = st.BurnGlobalFired
			}
		}
	}
	if s.lastSignalTime > 0 {
		m["last_signal_time"] = s.lastSignalTime
		m["last_signal_direction"] = s.lastSignalDirection
	}
	return m
}

// ── A2 多周期指标供给（引擎可选接口：PrimaryTimeframer/Timeframer/BarProviderSetter）──

// PrimaryTimeframe 声明策略工作周期：引擎只把该周期 K 线分发进 OnBar，
// 指标副周期供给（或同 symbol 其他策略的异周期供给）不会污染交易状态。
// 未配置 timeframe（旧配置）时返回 ""：不过滤，行为与旧版完全一致。
func (s *BaseCRAStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return ""
	}
	return s.params.Timeframe
}

// Timeframes 声明指标副周期供给需求：启用指标的周期 ≠ 工作周期时，引擎经
// kline_feeder 订阅对应周期（引用计数，Unregister 释放），策略经
// BarProvider 读取。工作周期未知时不声明副周期——没有主周期过滤，副周期
// bar 会混进 OnBar 污染交易状态；此时指标周期降级为工作周期并 WARN。
func (s *BaseCRAStrategy) Timeframes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.params
	if p == nil || p.Timeframe == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(enabled bool, period string) {
		if !enabled || !IsFeedablePeriod(period) {
			return
		}
		norm := strings.ToLower(strings.TrimSpace(period))
		if norm == p.Timeframe || seen[norm] {
			return
		}
		seen[norm] = true
		out = append(out, norm)
	}
	add(p.OpenMacdEnabled, p.OpenMacdPeriod)
	add(p.OpenCounterEmaEnabled, p.OpenCounterEmaPeriod)
	add(p.OpenTrendEmaEnabled, p.OpenTrendEmaPeriod)
	add(p.AddMacdEnabled, p.AddMacdPeriod)
	add(p.AddEmaEnabled, p.AddEmaPeriod)
	// C 片：反向信号判定周期（反向止盈/止损共用）也要订阅副周期供给。
	add(s.reversePeriodArmed(), p.ReverseTakeProfitPeriod)
	return out
}

// SetBarProvider 注入多周期数据访问器（引擎在 Start 成功后调用）。
func (s *BaseCRAStrategy) SetBarProvider(bp strategy.BarProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.barProvider = bp
}

func (s *BaseCRAStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.params == nil || s.flashDetector == nil {
		return nil, nil
	}
	s.flashDetector.AddPrice(tick.Last, tick.Timestamp)
	if s.flashDetector.IsFlashCrash() {
		if !s.state.WaterfallPaused {
			s.state.WaterfallPaused = true
			s.logger.Warn("waterfall protection triggered", "symbol", s.symbol, "drop", s.flashDetector.LastDrop())
		}
	} else if s.state.WaterfallPaused {
		// Resume when the drop has recovered below half the threshold.
		if s.flashDetector.LastDrop() < s.params.WaterfallProtection*0.5 {
			s.state.WaterfallPaused = false
			s.logger.Info("waterfall protection resumed", "symbol", s.symbol)
		}
	}
	return nil, nil
}

func (s *BaseCRAStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *BaseCRAStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.params == nil {
		return nil, nil
	}

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	p := s.params
	st := s.state

	if !st.InPosition {
		if !st.CanStartNewLoop(p.TradeCountMode, p.LoopCount) {
			return nil, nil
		}
		// Limit order price check for the first order.
		if p.FirstOrderPrice > 0 {
			if s.isLongPreferred() && bar.Close >= p.FirstOrderPrice {
				return nil, nil
			}
			if !s.isLongPreferred() && bar.Close <= p.FirstOrderPrice {
				return nil, nil
			}
		}
		if s.isContract {
			side := s.resolveContractSide()
			if !s.openIndicatorsConfirmed(side) {
				return nil, nil
			}
			st.EnterPosition(bar.Close, side)
		} else {
			st.EnterPosition(bar.Close, SideLong)
		}
		// 开仓加倍（币富名词解释 #15，open_double）：只放大首单名义（首单金额
		// ×2），不改变 add_positions 阶梯的基数——补仓第 N 档仍按首单原始金额
		// ×multiplier 计（见下方 Add positions 分支，entryQty 的 multiplier 实参
		// 是 cfg.Multiplier，不经过此处的 doubling）。仅合约消费（与 follow_trend
		// 门控同款）：D 片表单已仅合约显示，引擎层对现货存量配置的残留 true 也
		// 不生效。
		// 顺势而为（币富名词解释 #13/#40，follow_trend）同理只放大首单名义：
		// 与 open_double 叠加时 首单 = 首单金额 ×首单倍数 ×(open_double?2:1)
		// ×顺势倍数，补仓阶梯基数一律不随任何首单放大变化（同 D 片口径纪律）。
		mult := p.FirstOrderMultiplier
		if s.isContract && p.OpenDouble {
			mult *= 2
		}
		trendMult := s.followTrendMultiplier(st.Side)
		mult *= trendMult
		qty := RoundQty(s.entryQty(bar.Close, mult))
		s.logger.Info("cra first order", "symbol", s.symbol, "price", bar.Close, "qty", qty, "side", st.Side, "open_double", p.OpenDouble, "follow_trend_mult", trendMult)
		return s.signal(st.SignalDirection(), qty, "cra first order"), nil
	}

	st.UpdateExtremes(bar.Close)

	// 部分平仓/反向出场单在途（尾单/首尾止盈、反向止盈/止损信号已发出待成交
	// 终态）：不再发任何新信号——防重复出场超卖、防与在途平仓单竞争（成交
	// 确认/拒单撤单后 OnOrderUpdate 清除在途标记，下一根 K 线恢复评估）。
	if st.PendingCloseKind != "" {
		return nil, nil
	}

	// 出场优先级（币富语义，自上而下）：常规止损线 > 反向止损/反向止盈 >
	// 常规止盈（evaluateTakeProfit）。反向出场的浮亏/浮盈门槛互斥，两者同
	// 级；止损线触及（ratio/amount/price）优先于一切信号类出场。
	//
	// Stop-loss (contract only by default, but kept generic).
	if s.isContract && st.CheckStopLoss(bar.Close, p) {
		s.logger.Info("cra stop loss", "symbol", s.symbol, "price", bar.Close)
		st.ExitPosition()
		return s.signal("CLOSE", 0, "cra stop loss"), nil
	}

	// 反向止损/反向止盈（仅合约，币富名词解释 #35/#36）：判定通过即全平。
	// 出场走 B 片同一 PendingClose 在途机制——不乐观 ExitPosition，记在途
	// 标记；成交确认（OnOrderUpdate 减仓方向终态）才收尾，拒单/撤单清除
	// 标记后下一根 K 线条件仍满足可重新触发。CLOSE 不带数量=全平（与止损/
	// 全仓止盈的历史出场形态一致）。
	if s.isContract {
		if ok, kind := s.evaluateReverseExit(bar.Close); ok {
			st.PendingCloseKind = kind
			st.PendingCloseQty = st.TotalQty
			s.logger.Info("cra reverse exit", "symbol", s.symbol, "price", bar.Close, "kind", kind)
			return s.signal("CLOSE", 0, "cra "+reverseExitReason(kind)), nil
		}
	}

	// Take profit（三态分支见 evaluateTakeProfit）。
	if ok, qty, kind := s.evaluateTakeProfit(bar.Close); ok {
		if qty <= 0 || qty >= st.TotalQty {
			// 全平（full/移动止盈/止损及 tail/head_tail 仅剩一档的退化形态）：
			// 历史行为不变——不带数量的 CLOSE 信号，下游平掉全部持仓。
			s.logger.Info("cra take profit", "symbol", s.symbol, "price", bar.Close, "loops", st.LoopExecuted+1)
			st.ExitPosition()
			return s.signal("CLOSE", 0, "cra take profit"), nil
		}
		// 部分平仓（tail 只平尾档 / head_tail 平首+尾档）：状态不在信号时
		// 乐观变更，记在途标记，成交确认（OnOrderUpdate）才核销档位。
		st.PendingCloseKind = kind
		st.PendingCloseQty = qty
		s.logger.Info("cra partial take profit", "symbol", s.symbol, "price", bar.Close, "kind", kind, "qty", qty)
		return s.signal("CLOSE", RoundQty(qty), "cra take profit ("+kind+")"), nil
	}

	// 燃烧斩仓（F 片，仅合约，优先级：止损 > 反向出场 > 止盈 > 燃烧 > 补仓）：
	// 补仓达各自触发档数时市价斩仓减压，每循环每种至多一次，成交确认才核销
	// 档位（在途标记与部分止盈同机制），拒单/撤单清除 fired 重新武装。
	if s.isContract {
		if ok, qty, kind := s.evaluateBurn(); ok {
			st.PendingCloseKind = kind
			st.PendingCloseQty = qty
			if kind == "burn_global" {
				st.BurnGlobalFired = true
			} else {
				st.BurnDualFired = true
			}
			s.logger.Info("cra burn", "symbol", s.symbol, "price", bar.Close, "kind", kind,
				"qty", qty, "adds", st.PositionCount-1, "remaining_qty", st.TotalQty-qty)
			return s.signal("CLOSE", RoundQty(qty), "cra "+burnReason(kind)), nil
		}
	}

	// Add positions.
	if p.EnableAddPosition && st.PositionCount+st.PendingAddCount < p.OrderCount && !st.WaterfallPaused {
		nextOrder := st.PositionCount + st.PendingAddCount + 1
		cfg := p.AddPositionForOrder(nextOrder)
		if cfg != nil {
			// For contract, check per-order EMA switch and global add indicators.
			if s.isContract && !s.addPositionConfirmed(cfg) {
				return nil, nil
			}
			if st.ShouldAddPosition(bar.Close, cfg) {
				st.PendingAddCount++
				qty := RoundQty(s.entryQty(bar.Close, cfg.Multiplier))
				s.logger.Info("cra add position", "symbol", s.symbol, "order", nextOrder, "price", bar.Close, "qty", qty)
				return s.signal(st.SignalDirection(), qty, fmt.Sprintf("cra add position #%d", nextOrder)), nil
			}
		}
	}

	return nil, nil
}

func (s *BaseCRAStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.params == nil || s.state == nil {
		return nil, nil
	}

	if order.Status == model.StatusFilled {
		// 在途平仓（尾单/首尾止盈、反向止盈/止损）的成交确认：合约平仓单带
		// ClosePosition 标记；现货平仓走账本兜底路径是裸反向单（无标记，
		// 2026-10-01 修复的出场链路）——以在途标记+减仓方向联合认定
		// （liquidity_heat 同口径：ClosePosition || 反向单皆视为平仓）。
		// 成交量<总量按形态核销对应档位，仓位续存（剩余档成本/均价重算，见
		// CRAState.ApplyCloseFill，未知形态 FIFO 兜底）；全量成交（含反向
		// 出场的全平单）或核销后总量≈0（浮点尘埃/镜像漂移全平）按全平收尾。
		if kind := s.state.PendingCloseKind; kind != "" && s.isReduceSide(order.Side) {
			filled := order.Filled
			if filled <= 0 {
				filled = order.Quantity
			}
			s.state.PendingCloseKind = ""
			s.state.PendingCloseQty = 0
			if filled > 0 && filled < s.state.TotalQty && !approxQty(filled, s.state.TotalQty) {
				s.state.ApplyCloseFill(filled, kind)
				if s.state.TotalQty > 1e-9 {
					return nil, nil
				}
			}
			s.state.ExitPosition()
			return nil, nil
		}
		if order.ClosePosition {
			s.state.ExitPosition()
			return nil, nil
		}
		isEntry := (s.state.Side == SideLong && order.Side == model.SideBuy) ||
			(s.state.Side == SideShort && order.Side == model.SideSell)
		if isEntry {
			if s.state.PendingAddCount > 0 {
				s.state.PendingAddCount--
			}
			s.state.PositionCount++
			s.state.noteEntryFilled()
			s.state.RecordFill(order.AvgFillPrice, order.Filled, SideBuy)
		}
		return nil, nil
	}

	// 在途平仓的拒单/撤单/过期：状态从未乐观变更，只需清除在途标记，下一
	// 根 K 线出场条件（止盈/反向止盈/反向止损/燃烧斩仓）仍满足会重新发信号
	// （与 PendingAddCount 回补同口径）。同样按减仓方向认定（覆盖现货无
	// ClosePosition 标记的裸反向单）。燃烧斩仓（F 片）还要同步清除 fired
	// 标记——fired 在信号发出时置位防同循环重复，拒单不清掉就永远不会重试，
	// 变成静默失败的假燃烧（币富备注：卖出被拒必须可重试/如实记日志）。
	if s.state.PendingCloseKind != "" && s.isReduceSide(order.Side) &&
		(order.Status == model.StatusRejected || order.Status == model.StatusCancelled || order.Status == model.StatusExpired) {
		s.logger.Warn("cra close order ended without fill, re-arm exit",
			"symbol", s.symbol, "kind", s.state.PendingCloseKind, "status", order.Status)
		switch s.state.PendingCloseKind {
		case "burn_dual":
			s.state.BurnDualFired = false
		case "burn_global":
			s.state.BurnGlobalFired = false
		}
		s.state.PendingCloseKind = ""
		s.state.PendingCloseQty = 0
	}
	return nil, nil
}

// isReduceSide 判断订单方向是否为当前持仓的减仓方向（多仓=SELL，空仓=BUY）。
func (s *BaseCRAStrategy) isReduceSide(side model.OrderSide) bool {
	if s.state.Side == SideShort {
		return side == model.SideBuy
	}
	return side == model.SideSell
}

// ── helpers ──

// RestorePosition PositionRestorer 接口（引擎/工具链直调路径）：只有聚合净
// 持仓时合成单档重建；生产路径走 Start 的 restored_fills 逐笔重放（各档
// 成本精确）。
func (s *BaseCRAStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restorePositionLocked(qty, avgPrice)
	return nil
}

// restoreLotsLocked 分档重建持仓（重启恢复）。须持锁。EntryPrice=首档成本，
// 均价按分档加权，档位数=分档数（补仓阶梯据此续档）；极值/盈利峰值留待
// 下一根 K 线重记。
func (s *BaseCRAStrategy) restoreLotsLocked(lots []EntryLot, side PositionSide) {
	if len(lots) == 0 {
		return
	}
	if side != SideShort {
		side = SideLong
	}
	st := s.state
	st.ResetForNextLoop()
	st.InPosition = true
	st.Side = side
	st.Lots = append([]EntryLot(nil), lots...)
	var qty, cost float64
	for _, l := range lots {
		qty += l.Qty
		cost += l.Price * l.Qty
	}
	st.TotalQty = qty
	st.TotalCost = cost
	st.EntryPrice = lots[0].Price
	if qty > 0 {
		st.AvgEntryPrice = cost / qty
	}
	st.HighestPrice = st.AvgEntryPrice
	st.LowestPrice = st.AvgEntryPrice
	st.PositionCount = len(lots)
	// 重建的当前循环至少已有 len(lots)-1 次补仓：峰值补仓次数以此为下限
	// （重启前更高的峰值已不可考，取可见下限，与 ExitPosition 锚点口径一致）。
	if adds := len(lots) - 1; adds > st.PeakAddCount {
		st.PeakAddCount = adds
	}
	s.logger.Info("cra position restored", "symbol", s.symbol, "lots", len(lots),
		"qty", qty, "vwap", st.AvgEntryPrice, "side", side)
}

// restorePositionLocked 聚合重建兜底（无逐笔明细）：净持仓合成单档，
// tail/head_tail 退化为以均价判定的该档。方向取参数 direction（dual 无法
// 从聚合量反推，按多处理——聚合注入本身只在净多账本时发生）。聚合量同样
// 无法反推历史循环，顺势换向锚点（PrevLoopSide/PrevLoopTrappedAdds）在此
// 路径保持 0——重启后首轮不放大，待本循环结束后按运行时口径重写。
func (s *BaseCRAStrategy) restorePositionLocked(qty, vwap float64) {
	if qty <= 0 || vwap <= 0 {
		return
	}
	side := SideLong
	if s.params != nil && s.params.Direction == "short" {
		side = SideShort
	}
	s.restoreLotsLocked([]EntryLot{{Price: vwap, Qty: qty}}, side)
}

// parseRestoredLots 解析 handler 注入的 restored_fills（[]any of
// {"side","qty","price"}，成交时间升序）并重放重建分档持仓；分档总量与聚合
// 净持仓对不上（账本残缺/越权改单）时返回空，调用方退化聚合单档。
// 顺带重建顺势换向锚点（E 片）：最近一个全平循环的 {方向, 峰值补仓次数}；
// 以及燃烧 fired 标记（G2）：当前未平仓循环是否已触发过对向/全局燃烧
// （数量形态推断，口径见 RebuildBurnMarksFromFills），阈值/比例取解析后
// 的当前配置 p。
func parseRestoredLots(params map[string]any, p *CRAParams) ([]EntryLot, PositionSide, PositionSide, int, bool, bool) {
	raw, ok := params["restored_fills"].([]any)
	if !ok || len(raw) == 0 {
		return nil, "", "", 0, false, false
	}
	fills := make([]FillRecord, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, "", "", 0, false, false
		}
		fills = append(fills, FillRecord{
			Side:  strVal(m, "side", ""),
			Qty:   numFloat(m, "qty", 0),
			Price: numFloat(m, "price", 0),
		})
	}
	lots, side, prevSide, prevAdds := replayFills(fills)
	if len(lots) == 0 {
		return nil, "", "", 0, false, false
	}
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		total := 0.0
		for _, l := range lots {
			total += l.Qty
		}
		if !approxQty(total, q) {
			return nil, "", "", 0, false, false
		}
	}
	burnDual, burnGlobal := RebuildBurnMarksFromFills(fills, p.BurnDualThreshold, p.BurnGlobalThreshold, p.BurnGlobalCloseRatio)
	return lots, side, prevSide, prevAdds, burnDual, burnGlobal
}

func (s *BaseCRAStrategy) signal(direction string, qty float64, reason string) *model.Signal {
	s.lastSignalTime = time.Now().UnixMilli()
	s.lastSignalDirection = direction
	sig := &model.Signal{
		Symbol:    s.symbol,
		Direction: direction,
		Strength:  0.8,
		Strategy:  s.name,
		Reason:    reason,
		Timestamp: time.Now().UnixMilli(),
	}
	if qty > 0 {
		sig.Qty = qty
	}
	return sig
}

func (s *BaseCRAStrategy) entryQty(price, multiplier float64) float64 {
	if price <= 0 {
		return 0
	}
	notional := s.params.FirstOrderAmount * multiplier
	if s.isContract {
		return (notional * s.params.Leverage) / price
	}
	return notional / price
}

// followTrendMaxMultiplier 顺势而为首单放大倍数硬上限（币富名词解释 #13：
// 逆势补仓 4-7 次时顺势首单都只能开 5 倍——cap=5）。
const followTrendMaxMultiplier = 5

// followTrendMultiplier 顺势而为（E 片）首单放大倍数。币富原语义（#13/#40，
// 马丁趋势/华尔街双向策略）是多空同时持仓时以被套对侧补仓次数+1 放大顺势
// 侧再开仓；本引擎为每循环单侧模型（resolveContractSide 循环起点 EMA20
// 选边，多空不同时持仓），"同时开仓"前提不成立，按可达语义实现：dual 模式
// 下上一循环以被套状态结束（峰值补仓 N 次）、新一轮换向开仓时，首单名义
// 放大 min(N+1, 5)。同向新开、上轮无补仓（干净结束已清零锚点）、非 dual、
// 非合约、开关关闭，一律返回 1（零行为变化）。
func (s *BaseCRAStrategy) followTrendMultiplier(newSide PositionSide) float64 {
	p := s.params
	st := s.state
	if !s.isContract || p.Direction != "dual" || !p.FollowTrend {
		return 1
	}
	if st.PrevLoopTrappedAdds <= 0 || st.PrevLoopSide == "" || st.PrevLoopSide == newSide {
		return 1
	}
	m := st.PrevLoopTrappedAdds + 1
	if m > followTrendMaxMultiplier {
		m = followTrendMaxMultiplier
	}
	return float64(m)
}

func (s *BaseCRAStrategy) isLongPreferred() bool {
	if !s.isContract {
		return true
	}
	switch s.params.Direction {
	case "short":
		return false
	case "dual":
		// For dual, choose based on short-term trend at loop start.
		closes := BarsToCloses(s.bars)
		if len(closes) < 20 {
			return true
		}
		ema := EMA(closes, 20)
		return closes[len(closes)-1] > ema[len(ema)-1]
	}
	return true
}

func (s *BaseCRAStrategy) resolveContractSide() PositionSide {
	switch s.params.Direction {
	case "short":
		return SideShort
	case "dual":
		if s.isLongPreferred() {
			return SideLong
		}
		return SideShort
	}
	return SideLong
}

// evaluateTakeProfit 止盈判定，返回 (触发, 平仓数量, 出场形态 full|tail|head_tail)。
//
// tp_mode=moving（移动止盈）时止盈方式失效——币富名词解释 #29："移动止盈开启
// 后分仓止盈/首尾止盈失效，按固定止盈执行"：一律全仓移动止盈（保持现状）。
//
// 静态止盈按 take_profit_method 真实分支：
//   - full（及未知值）：全仓均价达线+回调 → 全平（qty=0 表示全平，历史行为不变）；
//   - tail：尾档自身盈利达线+回调 → 只平尾档（qty=尾档量）；
//   - head_tail：首档与尾档同时达线+回调 → 平首+尾两档（qty=两档量）。
//
// qty>0 的部分平仓由调用方记在途标记，成交确认后才演进持仓状态。
func (s *BaseCRAStrategy) evaluateTakeProfit(price float64) (bool, float64, string) {
	p := s.params
	st := s.state
	if p.TPMode == "moving" {
		return st.CheckMovingTakeProfit(price, p.MovingTakeProfitTiers), 0, "full"
	}
	switch p.TakeProfitMethod {
	case "tail":
		ok, qty := st.CheckTailTakeProfit(price, p.TakeProfitRatio, p.ProfitCallback)
		return ok, qty, "tail"
	case "head_tail":
		ok, qty := st.CheckHeadTailTakeProfit(price, p.TakeProfitRatio, p.ProfitCallback)
		return ok, qty, "head_tail"
	default:
		return st.CheckStaticTakeProfit(price, p.TakeProfitMethod, p.TakeProfitRatio, p.ProfitCallback), 0, "full"
	}
}

// ── C 片：反向止盈/反向止损（币富名词解释 #35/#36，仅合约）──
//
// 反向信号 = 与持仓方向相反的 MACD 交叉事件：金叉开多 → 死叉为反向；死叉开空
// → 金叉为反向。判定周期 = reverse_take_profit_period（close/5m/15m/30m/1h/4h/8h，
// G2-1 起前端补齐大周期档位；副周期经 A2 多周期供给取数，等于工作周期或未接线
// 时 indicatorBars 如实降级为工作周期 bar）。
// MACD 参数沿用 A3 tunables（indicator_params.macd，缺省 12/26/9）。
// 判定点沿用 OnBar 节奏：每根工作 K 线闭合时对判定周期序列末根做交叉检测
// （与开仓 MACD 门槛同口径，只有新鲜交叉事件才算"出现反向信号"）。
//
// 触发条件（严格按币富）：
//   - 反向止盈（#35）：未补仓（PositionCount<=1，档数同源对齐）且浮盈>0 且
//     反向信号 → 全平。已补仓或触发点浮亏时不生效，自动回落原有止盈方式；
//     浮盈=0 不算浮盈。
//   - 反向止损（#36）：反向信号且浮亏<0 → 全平。#36 未限制补仓，按字面实现
//     ——有浮亏+反向信号即止损（与补仓档数无关）。
//
// period=close（"关闭"）：反向止盈关闭；反向止损是独立开关，无专属周期配置，
// 此时降级为工作周期 MACD 判定（indicatorBars 对 close 的天然语义）。两者都
// 只在止损线未触及的区间有意义——OnBar 里常规止损分支在前，优先级
// 止损 > 反向止损/反向止盈 > 常规止盈。

// reversePeriodArmed 报告反向信号判定周期是否配置了有效副周期档位
// （reverse_take_profit_period ≠ close/空）。供 Timeframes 订阅与反向止盈
// 开关判定共用。
func (s *BaseCRAStrategy) reversePeriodArmed() bool {
	p := s.params
	if p == nil {
		return false
	}
	norm := strings.ToLower(strings.TrimSpace(p.ReverseTakeProfitPeriod))
	return norm != "" && norm != "close"
}

// evaluateReverseExit 反向止盈/止损判定，返回 (触发, 出场形态
// reverse_tp|reverse_sl)。浮盈/浮亏口径与止盈/止损一致（均价基准
// ProfitPct）。全平出场，qty 由调用方按全平形态处理（不带数量的 CLOSE）。
func (s *BaseCRAStrategy) evaluateReverseExit(price float64) (bool, string) {
	p := s.params
	st := s.state
	tpArmed := s.reversePeriodArmed()
	if !tpArmed && !p.ReverseStopLoss {
		return false, ""
	}
	profit := st.ProfitPct(price)
	// 反向止盈（#35）：未补仓+浮盈+反向信号。PendingAddCount 不计入——
	// 在途补仓信号尚未成交，仓位仍是首单单档。
	wantTP := tpArmed && st.PositionCount <= 1 && profit > 0
	// 反向止损（#36）：浮亏+反向信号，不限制补仓。
	wantSL := p.ReverseStopLoss && profit < 0
	if !wantTP && !wantSL {
		return false, ""
	}
	if !s.reverseMacdSignal() {
		return false, ""
	}
	if wantSL {
		return true, "reverse_sl"
	}
	return true, "reverse_tp"
}

// reverseMacdSignal 在判定周期序列末根检测反向 MACD 交叉（死叉对多仓、金叉
// 对空仓）。序列不足 minBars 时 indicatorBars 降级工作周期并 WARN（每周期
// 一次，A2 既有语义）；交叉检测器对不足窗口的序列返回 false，不误触发。
func (s *BaseCRAStrategy) reverseMacdSignal() bool {
	p := s.params
	macd := MACDTunables{Fast: p.MacdFast, Slow: p.MacdSlow, Signal: p.MacdSignal}
	bars := s.indicatorBars(p.ReverseTakeProfitPeriod, p.MacdSlow+p.MacdSignal+2)
	if s.state.Side == SideShort {
		return MACDBullishWithTunables(bars, macd)
	}
	return MACDBearishWithTunables(bars, macd)
}

// reverseExitReason 出场形态到信号原因的映射（RuntimeStatus/日志同源）。
func reverseExitReason(kind string) string {
	if kind == "reverse_sl" {
		return "reverse stop loss"
	}
	return "reverse take profit"
}

// ── F 片：燃烧斩仓（币富名词解释 #41，仅合约）──
//
// 币富原语义与本引擎可达语义（差异如实标注）：
//   - 对向燃烧（#41-1）：币富是逆势单补到第 N 仓时自动并行开顺势对向单，用顺势
//     单盈利抵消逆势首单的浮亏，顺势单不占在线单数。本引擎为每循环单侧持仓模型，
//     并行对向仓不可行——执行层（app/context.go handleStrategySignal）对向信号在
//     单向持仓账户=净减仓、CLOSE 信号会无差别平掉两侧镜像仓（closeMirroredPositions），
//     重启账本重建（replayFills/NetFilledByStrategy）把反向成交一律当减仓，分档记账
//     会被污染。故按任务授权的备选诚实语义实现：补仓成交达 N 次时，本循环触发一次
//     "斩首单档"——市价卖出浮亏最深的首档，实现其浮亏、下移剩余仓位均价、释放保证金。
//     差异：币富用顺势单盈利覆盖这笔亏损（净零成本解压），本实现亏损真实实现，
//     收益是仓位解压（均价下移→解套门槛降低、保证金释放）。
//   - 全局燃烧（#41-2）：币富是补到第 M 次时用所有盈利币兑的盈利跨币种消耗该逆势
//     单浮亏。本引擎实例间无 PnL 通道（策略引擎拿不到其它实例盈利数据，D 片的跨实例
//     数据仅在 handler 启停闸层），保守实现为本实例内更大力度斩仓：补仓达 M 次时
//     市价斩掉当前持仓的 burn_global_close_ratio（缺省 0.5，FIFO 从首档起核销）。
//
// 共用纪律：各自独立开关、各自每循环至多一次（fired 标记）；threshold<1 视为关闭
// （防 enabled+0 在首单后即触发）；仅合约消费（OnBar 调用点已 gate）；出场走 B 片
// PendingClose 在途机制——成交确认才核销档位，拒单/撤单/过期清除 fired 重新武装
// （OnOrderUpdate），不做静默假成功。

// defaultBurnGlobalCloseRatio 全局燃烧斩仓比例缺省值（当前持仓的 1/2，FIFO 从首档
// 起）。G2 起经 burn_global_close_ratio 参数化（0.1-0.9，params.go 解析校验）；对向
// 燃烧只斩首档，全局燃烧是对整体浮亏的升级解压，比例须明显大于首档占比。
const defaultBurnGlobalCloseRatio = 0.5

// evaluateBurn 燃烧斩仓判定，返回 (触发, 平仓数量, 出场形态 burn_dual|burn_global)。
// 触发锚点 = 当前已成交补仓次数（PositionCount−1；不用 PeakAddCount——部分止盈削档
// 后剩余仓位的实际被套深度才是燃烧要解的压力）。全局阈值更深、斩仓更重，两者同根
// K 线同时达标时全局优先；两者独立 fired，同循环可先后各触发一次（币富两机制本就
// 独立）。qty 必须严格小于 TotalQty（部分平仓形态；边界=全平时返回不触发，全平由
// 止损/止盈/反向出场承载）。
func (s *BaseCRAStrategy) evaluateBurn() (bool, float64, string) {
	p := s.params
	st := s.state
	adds := st.PositionCount - 1
	if adds < 1 || st.TotalQty <= 0 {
		return false, 0, ""
	}
	if p.BurnGlobalEnabled && p.BurnGlobalThreshold >= 1 && !st.BurnGlobalFired && adds >= p.BurnGlobalThreshold {
		if qty := RoundQty(st.TotalQty * p.BurnGlobalCloseRatio); qty > 0 && qty < st.TotalQty {
			return true, qty, "burn_global"
		}
	}
	if p.BurnDualEnabled && p.BurnDualThreshold >= 1 && !st.BurnDualFired && adds >= p.BurnDualThreshold {
		lots := st.currentLots()
		if len(lots) >= 2 {
			if qty := RoundQty(lots[0].Qty); qty > 0 && qty < st.TotalQty {
				return true, qty, "burn_dual"
			}
		}
	}
	return false, 0, ""
}

// burnReason 出场形态到信号原因的映射（与 reverseExitReason 同模式）。
func burnReason(kind string) string {
	if kind == "burn_global" {
		return "global burn"
	}
	return "dual burn"
}

func (s *BaseCRAStrategy) openIndicatorsConfirmed(side PositionSide) bool {
	p := s.params
	// 开仓指标选择器：'custom'（自定义指标）经指标沙箱执行，以最后一根 K 线
	// 的 buy/sell 信号作为开仓门槛（见 custom_indicator.go）。未配置 code_id 时
	// 视为无门槛放行；沙箱/数据库执行失败时由风控开关 indicator_fail_open 决定
	// 放行（fail-open，默认）还是拦截（fail-close）。
	if strings.EqualFold(p.OpenIndicator, "custom") {
		cid, ok := customCodeID(p.IndicatorParams)
		if !ok {
			return true
		}
		confirmed, err := customOpenIndicatorConfirmed(s.symbol, cid, side, s.bars)
		if err != nil {
			if risk.IndicatorFailOpen() {
				s.logger.Warn("custom open indicator check failed, allowing entry (fail-open)", "symbol", s.symbol, "code_id", cid, "err", err.Error())
				return true
			}
			s.logger.Warn("custom open indicator check failed, blocking entry (fail-close)", "symbol", s.symbol, "code_id", cid, "err", err.Error())
			return false
		}
		return confirmed
	}
	macd := MACDTunables{Fast: p.MacdFast, Slow: p.MacdSlow, Signal: p.MacdSignal}
	ema := EMATunables{Fast: p.EmaFast, Slow: p.EmaSlow}
	macdMin := p.MacdSlow + p.MacdSignal + 2
	emaMin := p.EmaSlow + 2
	if p.OpenMacdEnabled && !IndicatorConfirmedWithTunables(s.indicatorBars(p.OpenMacdPeriod, macdMin), true, p.OpenMacdPeriod, "macd", side, macd, ema) {
		return false
	}
	// A4：逆势 EMA=反转确认、顺势 EMA=顺向确认（旧版两者共用同一 "ema" 门槛）。
	if p.OpenCounterEmaEnabled && !IndicatorConfirmedWithTunables(s.indicatorBars(p.OpenCounterEmaPeriod, emaMin), true, p.OpenCounterEmaPeriod, "ema_counter", side, macd, ema) {
		return false
	}
	if p.OpenTrendEmaEnabled && !IndicatorConfirmedWithTunables(s.indicatorBars(p.OpenTrendEmaPeriod, emaMin), true, p.OpenTrendEmaPeriod, "ema_trend", side, macd, ema) {
		return false
	}
	return true
}

func (s *BaseCRAStrategy) addPositionConfirmed(cfg *AddPositionItem) bool {
	p := s.params
	macd := MACDTunables{Fast: p.MacdFast, Slow: p.MacdSlow, Signal: p.MacdSignal}
	ema := EMATunables{Fast: p.EmaFast, Slow: p.EmaSlow}
	macdMin := p.MacdSlow + p.MacdSignal + 2
	emaMin := p.EmaSlow + 2
	if cfg.EmaEnabled {
		// 补仓 EMA 沿用旧版共享语义（"ema"），不引入 A4 顺/逆区分。
		if p.AddEmaEnabled && !IndicatorConfirmedWithTunables(s.indicatorBars(p.AddEmaPeriod, emaMin), true, p.AddEmaPeriod, "ema", s.state.Side, macd, ema) {
			return false
		}
	}
	if p.AddMacdEnabled && !IndicatorConfirmedWithTunables(s.indicatorBars(p.AddMacdPeriod, macdMin), true, p.AddMacdPeriod, "macd", s.state.Side, macd, ema) {
		return false
	}
	return true
}

// indicatorBars 取指标计算用的 bar 序列（A2）：period 为空/"close"/等于工作
// 周期时用工作周期 bar（最近 50 根，历史行为不变）；其余周期读多周期供给
// （引擎经 kline_feeder 订阅、MarketData 注入），序列不足 minBars 时如实
// WARN（每周期一次）并降级为工作周期 bar。
func (s *BaseCRAStrategy) indicatorBars(period string, minBars int) []model.Bar {
	work := s.bars
	if len(work) > 50 {
		work = work[len(work)-50:]
	}
	norm := strings.ToLower(strings.TrimSpace(period))
	if norm == "" || norm == "close" || (s.params != nil && norm == s.params.Timeframe) {
		return work
	}
	if s.barProvider != nil {
		if series := s.barProvider.GetSeries(s.symbol, norm); len(series) >= minBars {
			if len(series) > 50 {
				series = series[len(series)-50:]
			}
			return series
		}
	}
	if !s.warnedFeeds[norm] {
		s.warnedFeeds[norm] = true
		workingTF := ""
		if s.params != nil {
			workingTF = s.params.Timeframe
		}
		s.logger.Warn("indicator period feed unavailable, degrade to working timeframe bars",
			"symbol", s.symbol, "period", norm, "working_timeframe", workingTF, "min_bars", minBars)
	}
	return work
}

// RoundQty rounds quantity to a reasonable precision for order placement.
func RoundQty(qty float64) float64 {
	if qty <= 0 {
		return 0
	}
	// Round to 6 decimal places for crypto spot, 3 for larger quantities.
	prec := 1000000.0
	if qty >= 1 {
		prec = 1000.0
	}
	return math.Round(qty*prec) / prec
}
