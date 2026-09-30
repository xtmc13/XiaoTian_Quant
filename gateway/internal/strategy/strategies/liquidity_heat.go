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
// 交易层（A：扫流动性反包，用户在 2026-09-30 选定）：
//  - 入场：价格跌破买方池（止损被触发）但同根收盘 reclaim 回池上方
//    （close>池价）→ 做多；只打强度≥min_pool_strength_pct×POC 的池，
//    一个池只打一次（打掉即被消耗）。
//  - 止损：被扫池价 − sl_buffer_atr×ATR(5)（池下沿缓冲）。
//  - 止盈：上方最近的存活卖方池（下一个磁吸），强度不足时退化为
//    固定 tp_fallback_pct。
//  - 超时：持仓满 max_hold_bars 根离场。单仓位，现货只做多。
//
// 数量：信号不带 Qty，由引擎 applyTradeHooks 按 CustomStakeAmount
// （position_size USDT ÷ 最新价）自动折算。

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
	entryPrice     float64
	entryPoolPrice float64 // 被扫的买方池价（SL 基准）
	stopPrice      float64
	targetPrice    float64
	targetIsPool   bool    // 当前止盈是否挂在卖方池上
	holdBars       int
	closeEmitted   bool // 平仓信号已发、成交回报前防重复

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
	return nil
}

func (s *LiquidityHeatStrategy) resetPositionLocked() {
	s.inPosition = false
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

// CustomStakeAmount：position_size（USDT）经引擎 applyTradeHooks 折算
// 为下单数量（Qty=stake/最新价）；余额不足退化为全额可用（现货只做多）。
func (s *LiquidityHeatStrategy) CustomStakeAmount(availableBalance float64, _ *model.Signal) float64 {
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
	return m
}

func (s *LiquidityHeatStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderUpdate 成交回填：首笔买单成交 → 以实际成交价建持仓基准；
// 清仓单成交 → 清状态（兜底：平仓信号路径已自行复位，成交回执双保险）。
func (s *LiquidityHeatStrategy) OnOrderUpdate(order model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if order.Status != model.StatusFilled {
		return nil, nil
	}
	if order.ClosePosition || order.Side == model.SideSell {
		if s.inPosition {
			s.resetPositionLocked()
		}
		return nil, nil
	}
	// 买单成交：信号价换实际成交均价（信号发出≠成交）。
	if order.Side == model.SideBuy && s.inPosition && order.AvgFillPrice > 0 {
		fill := order.AvgFillPrice
		// 止损锚定被扫池价不变；止盈按成交价重挂最近的存活卖方池。
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

func (s *LiquidityHeatStrategy) OnTick(tick model.Tick, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || !s.inPosition || tick.Last <= 0 {
		return nil, nil
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

	// 2. 持仓管理优先（收盘价判定；K 线计数只在此累计）。
	if s.inPosition {
		// 止盈挂在卖方池上且该池被本 bar 打掉（high≥池价）→ 按池价离场。
		if s.targetIsPool && s.targetPrice > 0 && bar.High >= s.targetPrice {
			return s.closePositionLocked(fmt.Sprintf(
				"流动性热力止盈离场: 触及卖方池 %.4f", s.targetPrice), bar.Time), nil
		}
		return s.checkPositionExitLocked(bar.Close, bar.Time, true), nil
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

// checkPositionExitLocked 持仓管理（Close/最新价判定）：
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
	if price <= s.stopPrice {
		return s.closePositionLocked(fmt.Sprintf("流动性热力止损离场: %.4f ≤ 池 %.4f−%.1f×ATR",
			price, s.entryPoolPrice, s.slBufferATR), ts)
	}
	if price >= s.targetPrice {
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
