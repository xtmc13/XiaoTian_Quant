package strategies

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── LiquidityHeatStrategy（流动性热力扫单反包，现货只做多）────────────
//
// Pine「Dynamic Liquidity HeatMap Profile [BigBeluga]」的 Go 移植 + 交易层：
//
// 流动性引擎（忠实移植，指标本体）：
//  1. 每根闭合 bar：vol=Σvolume(10)，nVol=vol/窗口内max(vol)×100，
//     atr=ATR(5)/50，offset=窗口内 max(atr×nVol)；
//  2. pivot：High==最高(最近pivot_bars根) → 在其上方挂卖方流动性池
//     （空头止损/突破买单，价位=high+atr×nVol 量化到网格）；Low 同理在下方
//     挂买方流动性池（多头止损，价位=最近网格中点−atr×nVol）；
//  3. 消耗规则：买方池被 low<池价 收割移除，卖方池被 high>池价 移除——
//     池子列表=还活着的止损密集区；
//  4. profile：存活池按量聚合到 bins 档（窗口=high+offset ~ low−offset），
//     POC=量最大档，池子强度=池量/POC量。
//
// 交易层（A：扫流动性反包，用户在 2026-09-30 选定，分钟级工作 K 线、周期可选）：
//  - 入场：价格跌破买方池（止损被触发）但同根收盘 reclaim 回池上方
//    （close>池价）→ 做多；只打强度≥min_pool_strength_pct×POC 的池，
//    一个池只打一次（打掉即被消耗）。
//  - 非 CRA 模式：止损=被扫池价−sl_buffer_atr×ATR(5)；止盈=上方最近存活
//    卖方池（下一个磁吸），强度不足时退化为固定 tp_fallback_pct。
//  - CRA 模式（config 含 CRA 键即启用，现货语义 2026-09-30 用户明确）：
//    不带止损；止盈按 CRA（移动止盈档位优先，否则静态 full/tail/head_tail）；
//    补仓触发=跌破下一个存活买方池并收回（同入场判定，池价须在均价下方），
//    金额=first_order_amount×CRA 阶梯乘数（递增，非百分比间距）；超时平仓
//    仅作最后安全闸。
//  - 超时：持仓满 max_hold_bars 根离场。单仓位，现货只做多。
//
// 数量：信号不带 Qty，由引擎 applyTradeHooks 按 CustomStakeAmount
// （CRA 模式=first_order_amount，否则 position_size USDT ÷ 最新价）折算。

const (
	lhStateIdle      = "IDLE"     // 未持仓，扫描扫单反包机会
	lhStatePosition  = "POSITION" // 持仓中（止损/止盈/超时管理）

	liquidityHeatMaxBars = 560 // 覆盖最大 lookback(500)+量能/ATR 余量
)

// lhPool 一个存活流动性池（Pine pivot 的 Go 版）。
type lhPool struct {
	price float64 // 池价位（买池在下方，卖池在上方）
	vol   float64 // 池权重 = Σvolume(volume_len)（出生 bar）
	isBuy bool    // true=买方流动性（pivot 低下方），false=卖方（pivot 高上方）
	seq   int     // 出生 bar 内部序号（调试用）
}

type LiquidityHeatStrategy struct {
	strategy.BaseStrategy
	name      string
	symbol    string
	timeframe string
	running   bool
	mu        sync.RWMutex

	// ── 参数（ParamRegistry 注册，ApplyParams 从 map 同步）──
	lookbackBars         int     // 观察窗口（Pine Calculated Bars，默认 300）
	bins                 int     // profile 档数（Pine Resolution，默认 50）
	volumeLen            int     // 池权重成交量窗口（Pine sum(volume,10)）
	atrLen               int     // ATR 周期（Pine ta.atr(5)）
	pivotBars            int     // pivot 判定窗口（Pine ta.highest(2)=2）
	minPoolStrengthPct   float64 // 入场池最小强度（占 POC 量 %）
	tpMinPoolStrengthPct float64 // 作止盈目标的卖方池最小强度（占 POC 量 %）
	tpFallbackPct        float64 // 无合格卖方池时的固定止盈涨幅
	slBufferATR          float64 // 止损缓冲（ATR(5) 倍数，在池价下方）
	positionSize         float64 // 每单 USDT（引擎折算数量）
	maxHoldBars          int     // 超时离场（根）

	// ── K 线与指标历史 ──
	bars      []model.Bar
	barSeq    int       // 内部单调 bar 计数
	vol10Hist []float64 // Σvolume(volume_len) 历史（窗口取 max → nVol）
	offsetHist []float64 // atr×nVol 历史（窗口取 max → offset）
	trSeed    int       // ATR Wilder 种子计数
	atrRaw    float64   // ATR(atrLen) 原始值（Wilder 平滑）
	prevClose float64

	// ── 流动性池 ──
	pools []*lhPool

	// ── 持仓状态 ──
	inPosition     bool
	entryPending   bool // 入场单已发未成交（乐观记账待确认；终态未成交须回滚）
	restored       bool // 重启仓位重建恢复出的持仓，待下一根 K 线重挂止损/目标
	// 持仓 K 线计数起点：早于该时间戳的 bar（暖机重放的历史 K 线）不计入
	// 超时离场的 holdBars——否则每次重启重放 99 根历史 K 线会瞬间把持仓
	// 时长顶满，持仓中的策略一重启就秒发超时平仓（2026-10-01 重启重建时实测）。
	holdCountFrom  int64
	entryPrice     float64
	entryPoolPrice float64 // 被扫的买方池价（SL 基准，非 CRA 模式）
	stopPrice      float64
	targetPrice    float64
	targetIsPool   bool    // 当前止盈是否挂在卖方池上（非 CRA 模式）
	holdBars       int
	closeEmitted   bool // 平仓信号已发、成交回报前防重复

	// ── CRA 仓位管理（可选）：config 含任一 CRA 键即启用（现货语义：无止损，
	// 池触发补仓 + CRA 止盈；超时为最后安全闸）。须持锁访问。
	craEnabled bool
	craParams  *cra.CRAParams
	craState   *cra.CRAState

	params *strategy.ParamRegistry
}

// NewLiquidityHeatStrategy 创建默认参数的流动性热力扫单反包策略。
func NewLiquidityHeatStrategy() *LiquidityHeatStrategy {
	s := &LiquidityHeatStrategy{
		name:                 "liquidity_heat",
		symbol:               "BTCUSDT",
		timeframe:            "15m",
		lookbackBars:         300,
		bins:                 50,
		volumeLen:            10,
		atrLen:               5,
		pivotBars:            2,
		minPoolStrengthPct:   30,
		tpMinPoolStrengthPct: 15,
		tpFallbackPct:        0.03,
		slBufferATR:          0.5,
		positionSize:         100,
		maxHoldBars:          96,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("lookback_bars", 300, 50, 500, "buy"))
	s.params.Register(strategy.IntParameter("bins", 50, 10, 100, "buy"))
	s.params.Register(strategy.IntParameter("volume_len", 10, 3, 50, "buy"))
	s.params.Register(strategy.IntParameter("atr_len", 5, 2, 30, "stoploss"))
	s.params.Register(strategy.IntParameter("pivot_bars", 2, 2, 10, "buy"))
	s.params.Register(strategy.FloatParameter("min_pool_strength_pct", 30, 5, 100, 1, "buy"))
	s.params.Register(strategy.FloatParameter("tp_min_pool_strength_pct", 15, 0, 100, 1, "roi"))
	s.params.Register(strategy.FloatParameter("tp_fallback_pct", 0.03, 0.005, 0.2, 0.005, "roi"))
	s.params.Register(strategy.FloatParameter("sl_buffer_atr", 0.5, 0.1, 3, 0.1, "stoploss"))
	s.params.Register(strategy.FloatParameter("position_size", 100, 10, 10000, 10, "buy"))
	s.params.Register(strategy.IntParameter("max_hold_bars", 96, 10, 500, "roi"))
	return s
}

func (s *LiquidityHeatStrategy) Name() string   { return s.name }
func (s *LiquidityHeatStrategy) Symbol() string { return s.symbol }

func (s *LiquidityHeatStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol":                   s.symbol,
		"timeframe":                s.timeframe,
		"lookback_bars":            s.lookbackBars,
		"bins":                     s.bins,
		"volume_len":               s.volumeLen,
		"atr_len":                  s.atrLen,
		"pivot_bars":               s.pivotBars,
		"min_pool_strength_pct":    s.minPoolStrengthPct,
		"tp_min_pool_strength_pct": s.tpMinPoolStrengthPct,
		"tp_fallback_pct":          s.tpFallbackPct,
		"sl_buffer_atr":            s.slBufferATR,
		"position_size":            s.positionSize,
		"max_hold_bars":            s.maxHoldBars,
	}
}

func (s *LiquidityHeatStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// PrimaryTimeframe 声明主周期：引擎只把该周期 K 线分发进 OnBar，
// K 线供给管按 params["timeframe"] 订阅（默认 15m，分钟级）。
func (s *LiquidityHeatStrategy) PrimaryTimeframe() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeframe
}

func (s *LiquidityHeatStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("liquidity_heat strategy apply params: %w", err)
	}
	if s.lookbackBars <= 0 {
		s.lookbackBars = 300
	}
	if s.pivotBars < 2 {
		s.pivotBars = 2
	}

	// 重新启动 = 全新状态：清 K 线/指标历史/流动性池/持仓。
	s.bars = nil
	s.barSeq = 0
	s.vol10Hist = nil
	s.offsetHist = nil
	s.trSeed = 0
	s.atrRaw = 0
	s.prevClose = 0
	s.pools = nil
	s.resetPositionLocked()

	// CRA 模式判定：参数中只要出现任一 CRA 专属键即启用（presence 判定，
	// 与 support_rebound 同）。现货语义（用户 2026-09-30 明确）：不带止损
	// ——无论是否配 stop_loss_ratio 都禁用比例止损；止盈/补仓阶梯按 CRA。
	s.craEnabled = false
	s.craParams = nil
	s.craState = nil
	if hasCRAKeys(params) {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("liquidity_heat cra: marshal params: %w", err)
		}
		cp, err := cra.ParseCRAParams(string(data))
		if err != nil {
			return fmt.Errorf("liquidity_heat cra: parse params: %w", err)
		}
		if err := cp.Validate(); err != nil {
			return fmt.Errorf("liquidity_heat cra: validate: %w", err)
		}
		cp.StopLossEnabled = false
		cp.StopLossType = ""
		cp.StopLossRatio = 0
		s.craParams = cp
		s.craState = &cra.CRAState{}
		s.craEnabled = true
	}

	// 重启仓位重建（handler 按本地成交账本注入 Start 参数）：在 Start 清态
	// 之后、暖机重放之前恢复持仓态——恢复若发生在 Start 之前会被上面的
	// resetPositionLocked 清掉，若发生在重放之后则已重复入场（竞态窗口）。
	if q, ok := params["restored_position_qty"].(float64); ok && q > 0 {
		v, _ := params["restored_position_vwap"].(float64)
		s.restorePositionLocked(q, v)
	}

	s.holdCountFrom = time.Now().UnixMilli()
	s.running = true
	return nil
}

func (s *LiquidityHeatStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.bars = nil
	s.pools = nil
	s.resetPositionLocked()
	s.craEnabled = false
	s.craParams = nil
	s.craState = nil
	return nil
}

func (s *LiquidityHeatStrategy) resetPositionLocked() {
	s.inPosition = false
	s.entryPending = false
	s.restored = false
	s.entryPrice, s.entryPoolPrice = 0, 0
	s.stopPrice, s.targetPrice = 0, 0
	s.targetIsPool = false
	s.holdBars = 0
	s.closeEmitted = false
}

// ── 参数系统实现 ────────────────────────────────────────────────

func (s *LiquidityHeatStrategy) GetParameters() *strategy.ParamRegistry { return s.params }

func (s *LiquidityHeatStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *LiquidityHeatStrategy) ApplyParams(m map[string]any) error {
	if s.params != nil {
		if err := s.params.FromMap(m); err != nil {
			return err
		}
	}
	s.lookbackBars = getInt(m, "lookback_bars", s.lookbackBars)
	s.bins = getInt(m, "bins", s.bins)
	s.volumeLen = getInt(m, "volume_len", s.volumeLen)
	s.atrLen = getInt(m, "atr_len", s.atrLen)
	s.pivotBars = getInt(m, "pivot_bars", s.pivotBars)
	s.minPoolStrengthPct = getFloat(m, "min_pool_strength_pct", s.minPoolStrengthPct)
	s.tpMinPoolStrengthPct = getFloat(m, "tp_min_pool_strength_pct", s.tpMinPoolStrengthPct)
	s.tpFallbackPct = getFloat(m, "tp_fallback_pct", s.tpFallbackPct)
	s.slBufferATR = getFloat(m, "sl_buffer_atr", s.slBufferATR)
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

func (s *LiquidityHeatStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// CustomStakeAmount：引擎 applyTradeHooks 折算下单数量（Qty=stake/最新价）；
// CRA 模式首单/补仓语义用 first_order_amount，否则 position_size（USDT）；
// 可用余额不足退化为全额可用（现货只做多）。
func (s *LiquidityHeatStrategy) CustomStakeAmount(availableBalance float64, _ *model.Signal) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stake := s.positionSize
	if s.craEnabled && s.craParams != nil && s.craParams.FirstOrderAmount > 0 {
		stake = s.craParams.FirstOrderAmount
	}
	if stake <= 0 {
		return 0
	}
	if availableBalance > 0 && stake > availableBalance {
		return availableBalance
	}
	return stake
}

// RuntimeStatus 暴露流动性地图与状态机快照（运行面板数据源）。
func (s *LiquidityHeatStrategy) RuntimeStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := lhStateIdle
	if s.inPosition {
		state = lhStatePosition
	}
	m := map[string]any{
		"running":        s.running,
		"state":          state,
		"symbol":         s.symbol,
		"timeframe":      s.timeframe,
		"bars_collected": len(s.bars),
		"pools_live":     len(s.pools),
	}
	nb, ns := 0, 0
	for _, p := range s.pools {
		if p.isBuy {
			nb++
		} else {
			ns++
		}
	}
	m["pools_buy"] = nb
	m["pools_sell"] = ns
	if poc, pocVol := s.profilePOCLocked(); pocVol > 0 {
		m["poc_price"] = poc
		if ref := s.nearestSellPoolLocked(math.MaxFloat64); ref > 0 {
			m["nearest_sell_pool"] = ref
		}
		if ref := s.nearestBuyPoolLocked(0); ref > 0 {
			m["nearest_buy_pool"] = ref
		}
	}
	if s.inPosition {
		m["entry_price"] = s.entryPrice
		m["entry_pool_price"] = s.entryPoolPrice
		m["stop_price"] = s.stopPrice
		m["target_price"] = s.targetPrice
		m["target_is_pool"] = s.targetIsPool
		m["hold_bars"] = s.holdBars
	}
	// CRA 仓位管理快照（启用时输出；现货语义无止损）。
	m["cra_enabled"] = s.craEnabled
	if s.craEnabled && s.craParams != nil && s.craState != nil {
		st, p := s.craState, s.craParams
		m["cra_position_count"] = st.PositionCount
		m["cra_pending_add_count"] = st.PendingAddCount
		m["cra_order_count"] = p.OrderCount
		if st.AvgEntryPrice > 0 {
			m["cra_avg_entry_price"] = st.AvgEntryPrice
			// 下一个补仓池 = 均价下方最近的存活买方池。
			ref := s.nearestBuyPoolBelowLocked(st.AvgEntryPrice)
			if ref > 0 {
				m["cra_next_add_pool"] = ref
			}
		}
		m["cra_highest_profit_pct"] = st.HighestProfitPct
	}
	return m
}

// nearestBuyPoolBelowLocked 返回低于 ref 的最高存活买方池价（下一个支撑位）。
func (s *LiquidityHeatStrategy) nearestBuyPoolBelowLocked(ref float64) float64 {
	best := 0.0
	for _, p := range s.pools {
		if !p.isBuy || p.price >= ref {
			continue
		}
		if p.price > best {
			best = p.price
		}
	}
	return best
}

func (s *LiquidityHeatStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderUpdate 成交/拒单回填：
//  - CRA 模式：首笔买单成交 → EnterPosition（均价=实际成交价）；补仓成交 →
//    PendingAddCount--/PositionCount++/RecordFill；清仓单 → ExitPosition。
//  - 非 CRA：首笔买单成交 → 以实际成交均价重挂止盈；清仓 → 复位状态。
//  - 终态未成交（拒单/撤销/过期，2026-10-01 起 OMS 拒绝路径也会广播）：
//    入场单被拒 → 回滚乐观记账（治"虚持仓"）；补仓单被拒 → 只减挂起计数；
//    出场单被拒 → 复位 closeEmitted 允许重试离场。
func (s *LiquidityHeatStrategy) OnOrderUpdate(order model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}

	// 终态未成交：回滚/清理挂起计数（2026-10-01 实证：-2010 余额不足
	// 的入场单被拒后引擎仍自认持仓，永久虚持仓）。
	if order.Status == model.StatusRejected || order.Status == model.StatusCancelled || order.Status == model.StatusExpired {
		s.handleUnfilledTerminalLocked(order)
		return nil, nil
	}

	if order.Status != model.StatusFilled {
		return nil, nil
	}

	if order.ClosePosition || order.Side == model.SideSell {
		s.entryPending = false
		if s.craEnabled && s.craState != nil {
			s.craState.ExitPosition()
		}
		if s.inPosition {
			s.resetPositionLocked()
		}
		return nil, nil
	}

	if order.Side != model.SideBuy || order.AvgFillPrice <= 0 {
		return nil, nil
	}
	s.entryPending = false // 入场/补仓成交：乐观记账确认

	if s.craEnabled && s.craState != nil {
		if !s.craState.InPosition {
			s.craState.EnterPosition(order.AvgFillPrice, cra.SideLong)
		}
		if s.craState.PendingAddCount > 0 {
			s.craState.PendingAddCount--
		}
		s.craState.PositionCount++
		s.craState.RecordFill(order.AvgFillPrice, order.Filled, cra.SideBuy)
		return nil, nil
	}

	// 非 CRA：信号价换实际成交均价，止盈按成交价重挂最近的存活卖方池。
	if s.inPosition {
		fill := order.AvgFillPrice
		s.entryPrice = fill
		if tp, ok := s.tpFromPoolLocked(fill); ok {
			s.targetPrice = tp
			s.targetIsPool = true
		} else {
			s.targetPrice = fill * (1 + s.tpFallbackPct)
			s.targetIsPool = false
		}
	}
	return nil, nil
}

// handleUnfilledTerminalLocked 终态未成交订单的处置。须持锁。
// 2026-10-01 实证：入场信号发出即乐观记账（inPosition=true），交易所拒单
// 若不回滚，引擎永久"虚持仓"（面板上有多仓、实际没买成、止盈/补仓乱发）。
func (s *LiquidityHeatStrategy) handleUnfilledTerminalLocked(order model.OrderData) {
	if order.Side == model.SideSell || order.ClosePosition {
		// 出场单被拒：仓位仍在，允许下一根 K 线重试离场。
		s.closeEmitted = false
		return
	}
	if order.Side != model.SideBuy {
		return
	}
	if s.entryPending {
		// 入场单终态未成交：回滚乐观记账，CRA 同步退出。
		s.entryPending = false
		if s.craEnabled && s.craState != nil && s.craState.InPosition {
			s.craState.ExitPosition()
		}
		if s.inPosition {
			s.resetPositionLocked()
		}
		return
	}
	// 补仓单被拒：回补 CRA 挂起计数（该档不再重试，等下一个池）。
	if s.craEnabled && s.craState != nil && s.craState.PendingAddCount > 0 {
		s.craState.PendingAddCount--
	}
}

// RestorePosition 重启仓位重建（strategy.PositionRestorer 接口）：
// 本地成交账本显示该策略仍有净买入持仓时恢复状态机——引擎的持仓状态
// 只在进程内存里，重启靠 K 线重放重建会"失忆"重复入场（2026-10-01
// 666 实证三次重启三次重复买入）。生产调用路径是 handler 把账本净持仓
// 注入 Start 参数、由 Start 收尾调 restorePositionLocked（见该处注释）。
func (s *LiquidityHeatStrategy) RestorePosition(qty, avgPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restorePositionLocked(qty, avgPrice)
	return nil
}

// restorePositionLocked 仓位重建实现。须持锁。止损/目标价留空，由下一根
// 有足够池/ATR 的 K 线按恢复均价重挂（recomputeRestoredLevelsLocked）。
func (s *LiquidityHeatStrategy) restorePositionLocked(qty, avgPrice float64) {
	if qty <= 0 || avgPrice <= 0 {
		return
	}
	s.inPosition = true
	s.entryPending = false
	s.restored = true
	s.entryPrice = avgPrice
	s.entryPoolPrice = 0
	s.stopPrice, s.targetPrice = 0, 0
	s.targetIsPool = false
	s.holdBars = 0
	s.closeEmitted = false
	if s.craEnabled && s.craState != nil && !s.craState.InPosition {
		s.craState.EnterPosition(avgPrice, cra.SideLong)
		// 重建持仓的档位无法从账本精确反推（金额加权 ≠ 档位数，且 666 实证
		// 重启失忆期间把 5 笔全算成"只持仓 1 档"→ 重放风暴连环发补仓单真金
		// 白银买货）。保守按满档处理：重建后不补仓，只管理止盈/超时离场。
		if s.craParams != nil {
			s.craState.PositionCount = s.craParams.OrderCount
		}
	}
	log.Printf("[liquidity_heat] %s 重启仓位重建: qty=%.8f vwap=%.2f (cra=%v)",
		s.name, qty, avgPrice, s.craEnabled)
}

// recomputeRestoredLevelsLocked 恢复持仓的首根可用 K 线重挂止损/目标。
// CRA 模式的移动止盈/止损由 CRA 状态机管理，无需重挂。须持锁。
func (s *LiquidityHeatStrategy) recomputeRestoredLevelsLocked() {
	s.restored = false
	if s.craEnabled {
		return
	}
	ref := s.entryPrice
	if pool := s.nearestBuyPoolBelowLocked(ref); pool > 0 {
		s.entryPoolPrice = pool
		s.stopPrice = pool - s.slBufferATR*s.atrRaw
	} else {
		// 无存活买池可锚：按恢复价让 2% 作为保护止损（仅非 CRA 模式）。
		s.entryPoolPrice = ref
		s.stopPrice = ref * 0.98
	}
	if tp, ok := s.tpFromPoolLocked(ref); ok {
		s.targetPrice, s.targetIsPool = tp, true
	} else {
		s.targetPrice, s.targetIsPool = ref*(1+s.tpFallbackPct), false
	}
	log.Printf("[liquidity_heat] %s 恢复持仓重挂: entry=%.2f stop=%.2f target=%.2f",
		s.name, ref, s.stopPrice, s.targetPrice)
}

func (s *LiquidityHeatStrategy) OnTick(tick model.Tick, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || !s.inPosition || tick.Last <= 0 {
		return nil, nil
	}
	if s.craEnabled {
		// tick 只做止盈判定（移动/静态），补仓只在 K 线闭合时评估。
		return s.manageCRAExitsLocked(tick.Last, tick.Timestamp), nil
	}
	return s.checkPositionExitLocked(tick.Last, tick.Timestamp, false), nil
}

func (s *LiquidityHeatStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if bar.Symbol != "" && bar.Symbol != s.symbol {
		return nil, nil
	}
	s.bars = append(s.bars, bar)
	if len(s.bars) > liquidityHeatMaxBars {
		s.bars = s.bars[len(s.bars)-liquidityHeatMaxBars:]
	}
	if !s.running {
		return nil, nil
	}

	// 1. 指标与池子推进：本 bar 注册 pivot 池、计算 offset/ATR。
	s.advanceIndicatorsLocked(bar)

	// 1a. 重启恢复的持仓：首根有足够池/ATR 的 K 线重挂止损/目标价。
	if s.restored && s.inPosition && s.stopPrice <= 0 && s.atrRaw > 0 && len(s.pools) > 0 {
		s.recomputeRestoredLevelsLocked()
	}

	// 2. 持仓管理优先（收盘价判定；K 线计数只在此累计，且只计策略启动
	// 之后闭合的真实 K 线——暖机重放的历史 bar 不算持仓时长）。
	countHold := bar.Time >= s.holdCountFrom
	if s.inPosition {
		if s.craEnabled {
			return s.manageCRAPositionLocked(bar, countHold), nil
		}
		// 止盈挂在卖方池上且该池被本 bar 打掉（high≥池价）→ 按池价离场。
		if s.targetIsPool && s.targetPrice > 0 && bar.High >= s.targetPrice {
			return s.closePositionLocked(fmt.Sprintf(
				"流动性热力止盈离场: 触及卖方池 %.4f", s.targetPrice), bar.Time), nil
		}
		return s.checkPositionExitLocked(bar.Close, bar.Time, countHold), nil
	}

	// 3. 未持仓：扫描扫单反包（用本 bar 之前仍存活的池）。
	var sig *model.Signal
	if len(s.bars) >= s.pivotBars+1 {
		sig = s.scanSweepReclaimLocked(bar)
	}

	// 4. 消耗规则（Pine）：跌破买池 / 涨破卖池 → 移除（入场合格池已在上一步消费）。
	s.consumePoolsLocked(bar)

	return sig, nil
}

// advanceIndicatorsLocked 更新 ATR/量历史，并按 Pine 逻辑为当前 bar 注册
// pivot 流动性池。须持锁。
func (s *LiquidityHeatStrategy) advanceIndicatorsLocked(bar model.Bar) {
	// ATR(atrLen)（Wilder 平滑，与 Pine ta.atr 一致）。
	tr := bar.High - bar.Low
	if s.prevClose > 0 {
		tr = math.Max(tr, math.Max(math.Abs(bar.High-s.prevClose), math.Abs(bar.Low-s.prevClose)))
	}
	s.prevClose = bar.Close
	n := s.atrLen
	if n < 1 {
		n = 5
	}
	if s.trSeed < n {
		s.trSeed++
		s.atrRaw += tr / float64(n)
		if s.trSeed == n {
			// 种子完成，atrRaw 已是前 n 根 TR 均值。
		}
	} else {
		s.atrRaw = (s.atrRaw*float64(n-1) + tr) / float64(n)
	}

	// vol = Σvolume(volume_len)。
	vol := 0.0
	start := len(s.bars) - s.volumeLen
	if start < 0 {
		start = 0
	}
	for _, b := range s.bars[start:] {
		vol += b.Volume
	}

	// offset = max(atr×nVol, 窗口)；nVol = vol/窗口max(vol)×100。
	s.vol10Hist = append(s.vol10Hist, vol)
	if len(s.vol10Hist) > s.lookbackBars {
		s.vol10Hist = s.vol10Hist[len(s.vol10Hist)-s.lookbackBars:]
	}
	volMax := 0.0
	for _, v := range s.vol10Hist {
		if v > volMax {
			volMax = v
		}
	}
	nVol := 0.0
	if volMax > 0 {
		nVol = vol / volMax * 100
	}
	atrNvol := s.atrRaw / 50 * nVol // Pine: ta.atr(5)/50 × nVol
	s.offsetHist = append(s.offsetHist, atrNvol)
	if len(s.offsetHist) > s.lookbackBars {
		s.offsetHist = s.offsetHist[len(s.offsetHist)-s.lookbackBars:]
	}
	offset := 0.0
	for _, v := range s.offsetHist {
		if v > offset {
			offset = v
		}
	}

	// pivot 注册：High==最高(最近pivot_bars根) → 上方卖方池；
	// Low==最低 → 下方买方池（Pine ta.highest/lowest(2) 推广为参数）。
	if len(s.bars) < s.pivotBars {
		return
	}
	win := s.bars[len(s.bars)-s.pivotBars:]
	isHigh, isLow := true, true
	for _, b := range win {
		if b.High > bar.High {
			isHigh = false
		}
		if b.Low < bar.Low {
			isLow = false
		}
	}
	if !isHigh && !isLow {
		return
	}
	// 网格：窗口 [low−offset, high+offset] 100 档（Pine resolution=100）。
	top, bot := s.envelopeLocked()
	if top <= bot {
		return
	}
	step := (top - bot) / 100
	quantize := func(level float64) float64 {
		idx := math.Round((level - bot) / step)
		if idx < 0 {
			idx = 0
		}
		if idx > 99 {
			idx = 99
		}
		return bot + step*idx + step/2 // 档中点
	}
	s.barSeq++
	if isHigh {
		price := quantize(bar.High + atrNvol)
		s.pools = append(s.pools, &lhPool{price: price, vol: vol, isBuy: false, seq: s.barSeq})
	}
	if isLow {
		price := quantize(bar.Low-atrNvol) - atrNvol // Pine: mid − atr*nVol
		s.pools = append(s.pools, &lhPool{price: price, vol: vol, isBuy: true, seq: s.barSeq})
	}
}

// envelopeLocked 窗口价格包络 [min(low−offset_i), max(high+offset_i)]（Pine h_l）。
func (s *LiquidityHeatStrategy) envelopeLocked() (top, bot float64) {
	start := len(s.bars) - s.lookbackBars
	if start < 0 {
		start = 0
	}
	offIdx := len(s.offsetHist) - (len(s.bars) - start)
	for i := start; i < len(s.bars); i++ {
		off := 0.0
		if j := offIdx + (i - start); j >= 0 && j < len(s.offsetHist) {
			off = s.offsetHist[j]
		}
		hi := s.bars[i].High + off
		lo := s.bars[i].Low - off
		if hi > top {
			top = hi
		}
		if bot == 0 || lo < bot {
			bot = lo
		}
	}
	return top, bot
}

// consumePoolsLocked 应用消耗规则：买方池 low<池价 移除，卖方池 high>池价 移除。
func (s *LiquidityHeatStrategy) consumePoolsLocked(bar model.Bar) {
	kept := s.pools[:0]
	for _, p := range s.pools {
		if p.isBuy && bar.Low < p.price {
			continue
		}
		if !p.isBuy && bar.High > p.price {
			continue
		}
		kept = append(kept, p)
	}
	s.pools = kept
}

// profilePOCLocked 按 bins 聚合存活池的量能 profile，返回 POC 档位中点与 POC 量。
func (s *LiquidityHeatStrategy) profilePOCLocked() (poc float64, pocVol float64) {
	if len(s.pools) == 0 || s.bins < 2 {
		return 0, 0
	}
	top, bot := s.envelopeLocked()
	if top <= bot {
		return 0, 0
	}
	step := (top - bot) / float64(s.bins)
	vols := make([]float64, s.bins)
	for _, p := range s.pools {
		idx := int(math.Round((p.price - bot) / step))
		if idx < 0 {
			idx = 0
		}
		if idx > s.bins-1 {
			idx = s.bins - 1
		}
		vols[idx] += p.vol
	}
	for i, v := range vols {
		if v > pocVol {
			pocVol = v
			poc = bot + step*float64(i) + step/2
		}
	}
	return poc, pocVol
}

// nearestSellPoolLocked 返回高于 ref 且强度≥门槛的最接近存活卖方池价。
func (s *LiquidityHeatStrategy) nearestSellPoolLocked(ref float64) float64 {
	_, pocVol := s.profilePOCLocked()
	if pocVol <= 0 {
		return 0
	}
	best := math.MaxFloat64
	for _, p := range s.pools {
		if p.isBuy || p.price <= ref {
			continue
		}
		if p.vol < pocVol*s.tpMinPoolStrengthPct/100 {
			continue
		}
		if p.price < best {
			best = p.price
		}
	}
	if best == math.MaxFloat64 {
		return 0
	}
	return best
}

// nearestBuyPoolLocked 返回低于 ref 的最接近存活买方池价（不过滤强度）。
func (s *LiquidityHeatStrategy) nearestBuyPoolLocked(ref float64) float64 {
	best := 0.0
	for _, p := range s.pools {
		if !p.isBuy || p.price >= ref {
			continue
		}
		if p.price > best {
			best = p.price
		}
	}
	return best
}

// tpFromPoolLocked 入场价上方最近的合格卖方池（止盈磁吸）。
func (s *LiquidityHeatStrategy) tpFromPoolLocked(entry float64) (float64, bool) {
	if tp := s.nearestSellPoolLocked(entry); tp > 0 {
		return tp, true
	}
	return 0, false
}

// scanSweepReclaimLocked 扫单反包扫描：存活买方池被本 bar 跌破（low<池价，
// 止损被触发）但收盘 reclaim 回池上方（close>池价）→ 入场做多。
// 只打强度≥min_pool_strength_pct×POC 的池。须持锁。
func (s *LiquidityHeatStrategy) scanSweepReclaimLocked(bar model.Bar) *model.Signal {
	_, pocVol := s.profilePOCLocked()
	if pocVol <= 0 {
		return nil
	}
	minVol := pocVol * s.minPoolStrengthPct / 100
	// 由近及远（池价从高到低），优先打最接近价格的池。
	cands := make([]*lhPool, 0, len(s.pools))
	for _, p := range s.pools {
		if p.isBuy && p.vol >= minVol && bar.Low < p.price && bar.Close > p.price {
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	best := cands[0]
	for _, p := range cands[1:] {
		if p.price > best.price {
			best = p
		}
	}

	s.inPosition = true
	s.entryPending = true // 乐观记账：成交确认(清)或终态未成交(回滚)二选一
	s.entryPrice = bar.Close
	s.entryPoolPrice = best.price
	s.stopPrice = best.price - s.slBufferATR*s.atrRaw
	if tp, ok := s.tpFromPoolLocked(bar.Close); ok {
		s.targetPrice = tp
		s.targetIsPool = true
	} else {
		s.targetPrice = bar.Close * (1 + s.tpFallbackPct)
		s.targetIsPool = false
	}
	s.holdBars = 0
	s.closeEmitted = false

	reason := fmt.Sprintf("流动性扫单反包入场: 买方池 %.4f 被扫后收回(低 %.4f 收 %.4f) 强度 %.0f%% POC",
		best.price, bar.Low, bar.Close, best.vol/pocVol*100)
	return &model.Signal{
		Symbol:    s.symbol,
		Direction: "LONG",
		Strength:  0.8,
		Strategy:  s.name,
		Reason:    reason,
		Timestamp: bar.Time,
	}
}

// manageCRAExitsLocked CRA 模式止盈判定（OnTick 与 manageCRAPositionLocked
// 共用）：移动止盈档位优先，否则静态止盈（full/tail/head_tail）。
func (s *LiquidityHeatStrategy) manageCRAExitsLocked(price float64, ts int64) *model.Signal {
	if !s.inPosition || s.closeEmitted || s.craState == nil || s.craParams == nil {
		return nil
	}
	p, st := s.craParams, s.craState
	st.UpdateExtremes(price)
	var tpHit bool
	if len(p.MovingTakeProfitTiers) > 0 {
		tpHit = st.CheckMovingTakeProfit(price, p.MovingTakeProfitTiers)
	} else {
		tpHit = st.CheckStaticTakeProfit(price, p.TakeProfitMethod, p.TakeProfitRatio, p.ProfitCallback)
	}
	if tpHit {
		st.ExitPosition()
		s.resetPositionLocked()
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    "流动性热力 CRA 止盈离场",
			Timestamp: ts,
		}
	}
	return nil
}

// manageCRAPositionLocked CRA 模式持仓管理（OnBar 入口）：
//  1. 止盈：同 manageCRAExitsLocked（tick 已判过的会重复判——CheckMovingTakeProfit
//     对已离场状态安全，二次判定无副作用）；
//  2. 池触发补仓：下一个存活买方池（池价 < CRA 均价）被本 bar 跌破收回 →
//     按 CRA 阶梯乘数加一档（Qty=first_order_amount×multiplier/price）；
//  3. 超时平仓仅作最后安全闸。
// 现货不带止损（用户 2026-09-30 明确），比例止损入口在此刻意缺省。
func (s *LiquidityHeatStrategy) manageCRAPositionLocked(bar model.Bar, countHold bool) *model.Signal {
	p, st := s.craParams, s.craState
	if p == nil || st == nil || !s.inPosition {
		return nil
	}
	if countHold {
		s.holdBars++
	}
	if sig := s.manageCRAExitsLocked(bar.Close, bar.Time); sig != nil {
		return sig
	}
	// 池触发补仓（消耗规则保证一个池只补一次）。
	if p.EnableAddPosition && st.InPosition &&
		st.PositionCount+st.PendingAddCount < p.OrderCount && !st.WaterfallPaused {
		if pool := s.sweptReclaimPoolLocked(bar, st.AvgEntryPrice); pool != nil {
			next := st.PositionCount + st.PendingAddCount + 1
			cfg := p.AddPositionForOrder(next)
			if cfg != nil {
				st.PendingAddCount++
				qty := cra.RoundQty(p.FirstOrderAmount * cfg.Multiplier / bar.Close)
				return &model.Signal{
					Symbol:    s.symbol,
					Direction: "LONG",
					Strength:  0.8,
					Strategy:  s.name,
					Reason:    fmt.Sprintf("流动性热力 CRA 补仓 #%d @买方池 %.4f", next, pool.price),
					Timestamp: bar.Time,
					Qty:       qty,
				}
			}
		}
	}
	if countHold && s.holdBars >= s.maxHoldBars {
		st.ExitPosition()
		s.resetPositionLocked()
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    "流动性热力持仓超时离场 (cra safety)",
			Timestamp: bar.Time,
		}
	}
	return nil
}

// sweptReclaimPoolLocked 找到被本 bar 跌破且收回、且池价在 belowRef 下方的
// 存活买方池（池价最高者优先 = 最近的下一个支撑位）。须持锁。
func (s *LiquidityHeatStrategy) sweptReclaimPoolLocked(bar model.Bar, belowRef float64) *lhPool {
	var best *lhPool
	for _, p := range s.pools {
		if !p.isBuy || p.price >= belowRef {
			continue
		}
		if bar.Low < p.price && bar.Close > p.price {
			if best == nil || p.price > best.price {
				best = p
			}
		}
	}
	return best
}

// checkPositionExitLocked 非 CRA 模式持仓管理（Close/最新价判定）：
//  1. Close ≤ 止损（被扫池价−buffer×ATR）→ 止损离场；
//  2. Close ≥ 目标（卖方池磁吸或固定比例）→ 止盈离场；
//  3. 持仓满 max_hold_bars 根 → 超时离场。
// 平仓信号发出即复位状态（单仓位），closeEmitted 防成交回报前重复。
func (s *LiquidityHeatStrategy) checkPositionExitLocked(price float64, ts int64, countHold bool) *model.Signal {
	if !s.inPosition || s.closeEmitted {
		return nil
	}
	if countHold {
		s.holdBars++
	}
	if s.stopPrice > 0 && price <= s.stopPrice {
		return s.closePositionLocked(fmt.Sprintf("流动性热力止损离场: %.4f ≤ 池 %.4f−%.1f×ATR",
			price, s.entryPoolPrice, s.slBufferATR), ts)
	}
	// targetPrice>0 守卫：重启仓位重建后止损/目标价留空、由首根可用 K 线
	// 重挂——0 值时任意 price≥0 都会误触固定止盈（2026-10-01 重建后秒平实证）。
	if s.targetPrice > 0 && price >= s.targetPrice {
		kind := "固定止盈"
		if s.targetIsPool {
			kind = "卖方池止盈"
		}
		return s.closePositionLocked(fmt.Sprintf("流动性热力%s离场: %.4f ≥ %.4f", kind, price, s.targetPrice), ts)
	}
	if countHold && s.holdBars >= s.maxHoldBars {
		return s.closePositionLocked(fmt.Sprintf("流动性热力持仓超时离场: %d 根", s.holdBars), ts)
	}
	return nil
}

func (s *LiquidityHeatStrategy) closePositionLocked(reason string, ts int64) *model.Signal {
	s.resetPositionLocked()
	return &model.Signal{
		Symbol:    s.symbol,
		Direction: "CLOSE",
		Strength:  1.0,
		Strategy:  s.name,
		Reason:    reason,
		Timestamp: ts,
	}
}
