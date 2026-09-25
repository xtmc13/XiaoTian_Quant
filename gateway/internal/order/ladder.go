package order

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 阶梯智能单（Ladder Smart Orders，对标 CryptoRobotics 终端阶梯单）──
//
// 一张阶梯单 = 最多 10 档入场限价单 + 最多 10 个止盈目标 + 单一止损。
//   - 每档 entry 独立限价挂单（复用 OMS，client_oid "lad-<id>-e<i>" 前缀）；
//   - entry 成交 → 按瀑布式分配把已成交量摊到各 target（close_pct × 计划总量），
//     target 以反向限价单挂出（"lad-<id>-t<i>"），成交量随 entry 继续成交而
//     增长（撤旧换新，保持每目标在途单 ≤1 张）；
//   - target 成交 = 部分止盈；每成交一个目标按规则上移/下移止损：
//       breakeven_after_target=N：第 N 个目标成交后 SL 移到加权平均入场价（保本位）；
//       trailing_step_pct：目标成交价回撤 step% 处设为候选 SL，只朝有利方向棘轮；
//   - SL 触发 / 一键全平：撤全部在途挂单 + 市价平掉剩余仓位（"lad-<id>-x"）；
//   - 状态落 xt_ladder_orders（state_json 全量），重启 RestoreFromStore 恢复。
//
// paper 与 live 同路径：子单全部走 OMS PlaceOrder（paper 进撮合引擎，live 走
// 交易所 adapter），成交回报靠轮询 placer.GetOrder（live 由 WS/reconcile 回填，
// 与 ltm 同一机制）。

const (
	ladderIDPrefix   = "lad-"
	ladderScanPeriod = 500 * time.Millisecond
	// ladderExitConfirm 市价平仓单确认窗口（对齐 ltm 的 5s）。
	ladderExitConfirm = 5 * time.Second
	ladderQtyEps      = 1e-9
)

// 阶梯单状态机（终态：completed/cancelled/stopped/flattened/failed）。
const (
	LadderStatusActive    = "active"    // 工作中（挂单/止盈/止损监控）
	LadderStatusStopping  = "stopping"  // SL 触发或全平中：已撤挂单，市价平仓待确认
	LadderStatusCompleted = "completed" // 目标全部成交（或买入量已全部止盈）
	LadderStatusCancelled = "cancelled" // 用户撤销（保留仓位）
	LadderStatusStopped   = "stopped"   // 止损平仓完成
	LadderStatusFlattened = "flattened" // 一键全平完成
	LadderStatusFailed    = "failed"    // 不可恢复错误（如市价平仓失败）
)

// 档位/目标状态。
const (
	ladderLegPending   = "pending"   // 挂单已提交待确认
	ladderLegOpen      = "open"      // 限价单在簿
	ladderLegPartial   = "partial"   // 部分成交
	ladderLegFilled    = "filled"    // 全部成交
	ladderLegCancelled = "cancelled" // 已撤销
	ladderLegWaiting   = "waiting"   // target 专用：尚无入场量可分配
)

// LadderEntry 一档入场。
type LadderEntry struct {
	Price      float64 `json:"price"`
	Qty        float64 `json:"qty"`                   // 计划数量（amount_usdt 已在创建时换算）
	AmountUSDT float64 `json:"amount_usdt,omitempty"` // 创建时的 USDT 金额（展示用）
	OrderID    string  `json:"order_id,omitempty"`
	Filled     float64 `json:"filled"`
	AvgPrice   float64 `json:"avg_price,omitempty"`
	Status     string  `json:"status"`
}

// LadderTarget 一个止盈目标（部分止盈 close_pct% 的计划总量）。
type LadderTarget struct {
	Price    float64 `json:"price"`
	ClosePct float64 `json:"close_pct"` // 占计划总量百分比，全部目标合计必须 = 100
	OrderID  string  `json:"order_id,omitempty"`
	Assigned float64 `json:"assigned"` // 瀑布式分配到的累计数量（含已成交）
	Filled   float64 `json:"filled"`   // 跨撤换单累计成交量
	// OrderFilledBase 当前在途单挂出时 Filled 的值：在途单成交要叠加在该基数上
	// （撤旧换新后新单的 ord.Filled 从 0 计，不能与累计 Filled 直接比较）。
	OrderFilledBase float64 `json:"order_filled_base"`
	Status          string  `json:"status"`
}

// LadderOrder 一张阶梯智能单。
type LadderOrder struct {
	ID       string          `json:"id"`
	UserID   uint64          `json:"user_id"`
	Symbol   string          `json:"symbol"`
	Side     model.OrderSide `json:"side"` // BUY=买入阶梯+目标卖出；SELL=卖出阶梯+目标买回
	Exchange string          `json:"exchange"`

	Entries  []LadderEntry  `json:"entries"`
	Targets  []LadderTarget `json:"targets"`
	TotalQty float64        `json:"total_qty"` // Σ entries.qty

	StopLoss             float64 `json:"stop_loss"`              // 初始止损价（0=不启用，仍可被保本/追踪规则激活）
	BreakevenAfterTarget int     `json:"breakeven_after_target"` // 第 N 个目标成交后 SL 移保本位（0=禁用）
	TrailingStepPct      float64 `json:"trailing_step_pct"`      // 每成交一个目标 SL 朝有利方向移动 step%（0=禁用）

	// 合约透传字段（现货留零值）。
	MarketType   model.MarketType   `json:"market_type,omitempty"`
	PositionSide model.PositionSide `json:"position_side,omitempty"`
	Leverage     float64            `json:"leverage,omitempty"`
	MarginMode   model.MarginMode   `json:"margin_mode,omitempty"`

	// 运行时状态
	Status         string  `json:"status"`
	CurrentSL      float64 `json:"current_sl"`      // 当前生效止损（棘轮后）
	BreakevenArmed bool    `json:"breakeven_armed"` // 保本规则已激活
	FilledQty      float64 `json:"filled_qty"`      // Σ entries.filled
	ClosedQty      float64 `json:"closed_qty"`      // Σ targets.filled
	AvgEntry       float64 `json:"avg_entry"`       // 加权平均入场价
	ExitOrderID    string  `json:"exit_order_id,omitempty"`
	ExitFilled     float64 `json:"exit_filled,omitempty"` // 平仓单已成交量（计入 ClosedQty 的增量依据）
	ExitDeadline   int64   `json:"exit_deadline,omitempty"`
	StopReason     string  `json:"stop_reason,omitempty"` // stop_loss|manual_flatten
	FailReason     string  `json:"fail_reason,omitempty"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`

	ladMu *sync.Mutex `json:"-"` // 档位锁；Create/Restore 时初始化
	dirty bool        `json:"-"`
}

// Snapshot 返回阶梯单的安全拷贝（REST 序列化用，避免与扫描并发读写）。
// 拷贝共享同一个锁指针（快照本身不再加锁，仅用于只读序列化）。
func (l *LadderOrder) Snapshot() *LadderOrder {
	l.ladMu.Lock()
	defer l.ladMu.Unlock()
	cp := *l
	cp.Entries = append([]LadderEntry(nil), l.Entries...)
	cp.Targets = append([]LadderTarget(nil), l.Targets...)
	return &cp
}

// IsTerminal 阶梯单是否已终结。
func (l *LadderOrder) IsTerminal() bool {
	switch l.Status {
	case LadderStatusCompleted, LadderStatusCancelled, LadderStatusStopped,
		LadderStatusFlattened, LadderStatusFailed:
		return true
	}
	return false
}

// dir 方向因子：BUY 阶梯做多 +1，SELL 阶梯做空/出货 -1。
func (l *LadderOrder) dir() float64 {
	if l.Side == model.SideSell {
		return -1
	}
	return 1
}

// exitSide 平仓方向。
func (l *LadderOrder) exitSide() model.OrderSide {
	if l.Side == model.SideBuy {
		return model.SideSell
	}
	return model.SideBuy
}

// slHit 当前价是否触及止损。
func (l *LadderOrder) slHit(price float64) bool {
	if l.CurrentSL <= 0 || price <= 0 {
		return false
	}
	if l.dir() > 0 {
		return price <= l.CurrentSL
	}
	return price >= l.CurrentSL
}

// ratchetSL 止损只朝有利方向移动（多头只上移，空头只下移）。
func (l *LadderOrder) ratchetSL(candidate float64) {
	if candidate <= 0 {
		return
	}
	if l.dir() > 0 {
		if candidate > l.CurrentSL {
			l.CurrentSL = candidate
			l.dirty = true
		}
		return
	}
	if l.CurrentSL <= 0 || candidate < l.CurrentSL {
		l.CurrentSL = candidate
		l.dirty = true
	}
}

// LadderSpec 创建阶梯单的输入规格。
type LadderSpec struct {
	Symbol   string          `json:"symbol"`
	Side     model.OrderSide `json:"side"`
	Exchange string          `json:"exchange"`

	Entries []struct {
		Price      float64 `json:"price"`
		Qty        float64 `json:"qty,omitempty"`
		AmountUSDT float64 `json:"amount_usdt,omitempty"`
	} `json:"entries"`
	Targets []struct {
		Price    float64 `json:"price"`
		ClosePct float64 `json:"close_pct"`
	} `json:"targets"`

	StopLoss             float64 `json:"stop_loss"`
	BreakevenAfterTarget int     `json:"breakeven_after_target"`
	TrailingStepPct      float64 `json:"trailing_step_pct"`

	MarketType   model.MarketType   `json:"market_type,omitempty"`
	PositionSide model.PositionSide `json:"position_side,omitempty"`
	Leverage     float64            `json:"leverage,omitempty"`
	MarginMode   model.MarginMode   `json:"margin_mode,omitempty"`
}

const (
	LadderMaxLegs = 10
	ladderPctEps  = 0.01
)

// Validate 校验规格并归一化（amount_usdt → qty）。返回归一化后的条目/目标与总量。
func (s *LadderSpec) Validate() ([]LadderEntry, []LadderTarget, float64, error) {
	if s.Symbol == "" {
		return nil, nil, 0, fmt.Errorf("symbol is required")
	}
	if s.Side != model.SideBuy && s.Side != model.SideSell {
		return nil, nil, 0, fmt.Errorf("invalid side: %s", s.Side)
	}
	if len(s.Entries) < 1 || len(s.Entries) > LadderMaxLegs {
		return nil, nil, 0, fmt.Errorf("entries must be 1-%d, got %d", LadderMaxLegs, len(s.Entries))
	}
	if len(s.Targets) < 1 || len(s.Targets) > LadderMaxLegs {
		return nil, nil, 0, fmt.Errorf("targets must be 1-%d, got %d", LadderMaxLegs, len(s.Targets))
	}
	entries := make([]LadderEntry, 0, len(s.Entries))
	total := 0.0
	for i, e := range s.Entries {
		if e.Price <= 0 {
			return nil, nil, 0, fmt.Errorf("entry %d: price must be positive", i)
		}
		qty := e.Qty
		if qty <= 0 && e.AmountUSDT > 0 {
			qty = e.AmountUSDT / e.Price
		}
		if qty <= 0 {
			return nil, nil, 0, fmt.Errorf("entry %d: qty or amount_usdt must be positive", i)
		}
		entries = append(entries, LadderEntry{
			Price: e.Price, Qty: qty, AmountUSDT: e.AmountUSDT, Status: ladderLegPending,
		})
		total += qty
	}
	targets := make([]LadderTarget, 0, len(s.Targets))
	pctSum := 0.0
	for i, t := range s.Targets {
		if t.Price <= 0 {
			return nil, nil, 0, fmt.Errorf("target %d: price must be positive", i)
		}
		if t.ClosePct <= 0 || t.ClosePct > 100 {
			return nil, nil, 0, fmt.Errorf("target %d: close_pct must be in (0,100]", i)
		}
		targets = append(targets, LadderTarget{
			Price: t.Price, ClosePct: t.ClosePct, Status: ladderLegWaiting,
		})
		pctSum += t.ClosePct
	}
	if math.Abs(pctSum-100) > ladderPctEps {
		return nil, nil, 0, fmt.Errorf("targets close_pct must sum to 100, got %.4f", pctSum)
	}
	if s.StopLoss < 0 {
		return nil, nil, 0, fmt.Errorf("stop_loss must be >= 0")
	}
	if s.TrailingStepPct < 0 || s.TrailingStepPct >= 100 {
		return nil, nil, 0, fmt.Errorf("trailing_step_pct must be in [0,100)")
	}
	if s.BreakevenAfterTarget < 0 || s.BreakevenAfterTarget > len(s.Targets) {
		return nil, nil, 0, fmt.Errorf("breakeven_after_target must be in [0,%d]", len(s.Targets))
	}
	return entries, targets, total, nil
}

// LadderEngine 阶梯单引擎：监控成交、挂/改止盈单、棘轮止损、全平。
type LadderEngine struct {
	placer lmOrderPlacer // 复用 ltm 的窄接口（PlaceOrder/CancelOrder/GetOrder）
	price  func(symbol string) float64

	mu      sync.Mutex
	orders  map[string]*LadderOrder
	started bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

var (
	ladderEngine     *LadderEngine
	ladderEngineOnce sync.Once
)

// GetLadderEngine 返回全局引擎（生产接全局 OrderManager）。
func GetLadderEngine() *LadderEngine {
	ladderEngineOnce.Do(func() {
		ladderEngine = NewLadderEngine(GetOrderManager())
	})
	return ladderEngine
}

// NewLadderEngine placer 通常为 *OrderManager（满足窄接口）。
func NewLadderEngine(placer lmOrderPlacer) *LadderEngine {
	return &LadderEngine{
		placer: placer,
		orders: make(map[string]*LadderOrder),
		stopCh: make(chan struct{}),
	}
}

// SetPriceSource 注入最新价来源（生产接 ConditionalEngine.GetPrice，由 WS 喂价）。
func (e *LadderEngine) SetPriceSource(fn func(symbol string) float64) { e.price = fn }

// SetNotifyHook 注入异常告警回调（SL/全平市价单失败时）。
func (e *LadderEngine) SetNotifyHook(fn func(l *LadderOrder, msg string)) { ladderNotifyHook = fn }

var ladderNotifyHook func(l *LadderOrder, msg string)

// Start 拉起扫描 goroutine（幂等）。
func (e *LadderEngine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return
	}
	e.started = true
	e.doneCh = make(chan struct{})
	go e.scanLoop()
}

// Stop 优雅停止（幂等）。
func (e *LadderEngine) Stop() {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return
	}
	e.started = false
	close(e.stopCh)
	done := e.doneCh
	e.mu.Unlock()
	<-done
}

func (e *LadderEngine) scanLoop() {
	defer close(e.doneCh)
	ticker := time.NewTicker(ladderScanPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.scanOnce()
		}
	}
}

func (e *LadderEngine) scanOnce() {
	e.mu.Lock()
	ladders := make([]*LadderOrder, 0, len(e.orders))
	for _, l := range e.orders {
		if !l.IsTerminal() {
			ladders = append(ladders, l)
		}
	}
	e.mu.Unlock()
	for _, l := range ladders {
		e.processLadder(l)
	}
}

// Create 创建阶梯单：校验规格 → 逐档挂入场限价单 → 入库并纳入监控。
func (e *LadderEngine) Create(spec *LadderSpec, userID uint64) (*LadderOrder, error) {
	entries, targets, total, err := spec.Validate()
	if err != nil {
		return nil, err
	}
	exchange := spec.Exchange
	if exchange == "" {
		exchange = "paper"
	}
	now := time.Now().UnixMilli()
	l := &LadderOrder{
		ID:                   ladderIDPrefix + newLadderID(),
		UserID:               userID,
		Symbol:               spec.Symbol,
		Side:                 spec.Side,
		Exchange:             exchange,
		Entries:              entries,
		Targets:              targets,
		TotalQty:             total,
		StopLoss:             spec.StopLoss,
		BreakevenAfterTarget: spec.BreakevenAfterTarget,
		TrailingStepPct:      spec.TrailingStepPct,
		MarketType:           spec.MarketType,
		PositionSide:         spec.PositionSide,
		Leverage:             spec.Leverage,
		MarginMode:           spec.MarginMode,
		Status:               LadderStatusActive,
		CurrentSL:            spec.StopLoss,
		CreatedAt:            now,
		UpdatedAt:            now,
		ladMu:                &sync.Mutex{},
	}

	// 逐档挂入场限价单；任一失败则回滚已挂档位，整体失败。
	placed := 0
	for i := range l.Entries {
		ord, perr := e.placer.PlaceOrder(e.entryRequest(l, i))
		if perr != nil {
			for j := 0; j < placed; j++ {
				if l.Entries[j].OrderID != "" {
					_, _ = e.placer.CancelOrder(l.Entries[j].OrderID, l.Symbol)
				}
				l.Entries[j].Status = ladderLegCancelled
			}
			l.Status = LadderStatusFailed
			l.FailReason = fmt.Sprintf("entry %d place failed: %v", i, perr)
			l.persist()
			return l, fmt.Errorf("entry %d: %w", i, perr)
		}
		l.Entries[i].OrderID = ord.ID
		l.Entries[i].Filled = ord.Filled
		l.Entries[i].AvgPrice = avgOr(ord.AvgFillPrice, 0)
		l.Entries[i].Status = entryStatusFromOrder(ord)
		placed++
	}

	e.mu.Lock()
	e.orders[l.ID] = l
	e.mu.Unlock()
	l.dirty = true
	l.persist()
	// 立即过一轮状态机：入场单可能已（部分）成交（paper 撮合/吃单）。
	e.processLadder(l)
	return l, nil
}

// entryRequest 构造第 i 档入场限价单请求。
func (e *LadderEngine) entryRequest(l *LadderOrder, i int) *Request {
	return &Request{
		Symbol:       l.Symbol,
		Side:         l.Side,
		OrderType:    model.TypeLimit,
		Price:        l.Entries[i].Price,
		Quantity:     l.Entries[i].Qty,
		Exchange:     l.Exchange,
		UserID:       l.UserID,
		ClientOID:    fmt.Sprintf("%s%s-e%d", ladderIDPrefix, strings.TrimPrefix(l.ID, ladderIDPrefix), i),
		Source:       "ladder:" + l.ID,
		MarketType:   l.MarketType,
		PositionSide: l.PositionSide,
		Leverage:     l.Leverage,
		MarginMode:   l.MarginMode,
	}
}

// targetRequest 构造第 i 个目标的止盈限价单请求（反向）。
func (e *LadderEngine) targetRequest(l *LadderOrder, i int, qty float64) *Request {
	return &Request{
		Symbol:       l.Symbol,
		Side:         l.exitSide(),
		OrderType:    model.TypeLimit,
		Price:        l.Targets[i].Price,
		Quantity:     qty,
		Exchange:     l.Exchange,
		UserID:       l.UserID,
		ClientOID:    fmt.Sprintf("%s%s-t%d", ladderIDPrefix, strings.TrimPrefix(l.ID, ladderIDPrefix), i),
		Source:       "ladder:" + l.ID,
		AIGateBypass: true, // 止盈出场单不重复过 AI 门
		MarketType:   l.MarketType,
		PositionSide: l.PositionSide,
		Leverage:     l.Leverage,
		MarginMode:   l.MarginMode,
	}
}

// exitRequest 构造市价平仓请求（SL/全平剩余仓位）。
func (e *LadderEngine) exitRequest(l *LadderOrder, qty float64) *Request {
	return &Request{
		Symbol:        l.Symbol,
		Side:          l.exitSide(),
		OrderType:     model.TypeMarket,
		Quantity:      qty,
		Exchange:      l.Exchange,
		UserID:        l.UserID,
		ClientOID:     ladderIDPrefix + strings.TrimPrefix(l.ID, ladderIDPrefix) + "-x",
		Source:        "ladder:" + l.ID,
		AIGateBypass:  true,
		MarketType:    l.MarketType,
		PositionSide:  l.PositionSide,
		Leverage:      l.Leverage,
		MarginMode:    l.MarginMode,
		ClosePosition: true,
	}
}

// processLadder 单张阶梯单的一轮状态机推进（持 ladMu，可安全与 REST 并发）。
func (e *LadderEngine) processLadder(l *LadderOrder) {
	l.ladMu.Lock()
	defer l.ladMu.Unlock()

	if l.Status == LadderStatusStopping {
		e.confirmExit(l)
		l.persist()
		return
	}
	if l.Status != LadderStatusActive {
		return
	}

	e.refreshEntries(l)
	e.refreshTargets(l)
	if l.IsTerminal() {
		l.persist()
		return
	}
	e.allocateTargets(l)
	e.applySLRules(l)
	l.persist()

	// SL 监控（价格源可用时）。
	if e.price != nil && l.Status == LadderStatusActive && !l.IsTerminal() {
		if px := e.price(l.Symbol); l.slHit(px) {
			e.executeExit(l, "stop_loss")
		}
	}
}

// refreshEntries 刷新各档入场单成交，重算已成交量与加权均价。
func (e *LadderEngine) refreshEntries(l *LadderOrder) {
	oldFilled, oldAvg := l.FilledQty, l.AvgEntry
	var qtySum, costSum float64
	for i := range l.Entries {
		en := &l.Entries[i]
		if en.OrderID == "" || en.Status == ladderLegFilled || en.Status == ladderLegCancelled {
			if en.Status == ladderLegFilled {
				qtySum += en.Filled
				costSum += en.Filled * avgOr(en.AvgPrice, en.Price)
			}
			continue
		}
		ord := e.placer.GetOrder(en.OrderID)
		if ord == nil {
			continue
		}
		if ord.Filled > en.Filled {
			en.Filled = ord.Filled
			en.AvgPrice = avgOr(ord.AvgFillPrice, ord.Price)
			l.dirty = true
		}
		en.Status = entryStatusFromOrder(ord)
		if en.Status == ladderLegFilled || en.Status == ladderLegPartial {
			qtySum += en.Filled
			costSum += en.Filled * avgOr(en.AvgPrice, en.Price)
		}
	}
	l.FilledQty = qtySum
	if qtySum > ladderQtyEps {
		l.AvgEntry = costSum / qtySum
	}
	if l.FilledQty != oldFilled || l.AvgEntry != oldAvg {
		l.dirty = true
	}
}

// refreshTargets 刷新止盈单成交；目标成交触发部分止盈计数。
func (e *LadderEngine) refreshTargets(l *LadderOrder) {
	oldClosed := l.ClosedQty
	filledTargets := 0
	var closed float64
	for i := range l.Targets {
		tg := &l.Targets[i]
		if tg.OrderID != "" && tg.Status != ladderLegFilled && tg.Status != ladderLegCancelled {
			ord := e.placer.GetOrder(tg.OrderID)
			if ord != nil {
				eff := tg.OrderFilledBase + ord.Filled
				if eff > tg.Filled {
					tg.Filled = eff
					l.dirty = true
				}
				switch {
				case ord.Status == model.StatusFilled || tg.Filled >= tg.Assigned-ladderQtyEps && tg.Assigned > 0:
					tg.Status = ladderLegFilled
				case ord.Status == model.StatusCancelled || ord.Status == model.StatusRejected || ord.Status == model.StatusExpired:
					// 在途止盈单被外部撤销/拒绝：保留已成交量，剩余由 allocateTargets 重挂。
					if tg.Filled > 0 {
						tg.Status = ladderLegPartial
					} else {
						tg.Status = ladderLegWaiting
					}
					tg.OrderID = ""
					l.dirty = true
				case tg.Filled > 0:
					tg.Status = ladderLegPartial
				default:
					tg.Status = ladderLegOpen
				}
			}
		}
		if tg.Status == ladderLegFilled {
			filledTargets++
		}
		closed += tg.Filled
	}
	l.ClosedQty = closed
	if closed != oldClosed {
		l.dirty = true
		e.onTargetProgress(l, filledTargets)
	}
}

// onTargetProgress 目标成交后的规则评估：保本位 + 目标追踪。
func (e *LadderEngine) onTargetProgress(l *LadderOrder, filledTargets int) {
	// 保本：第 N 个目标成交后 SL 移到加权平均入场价。
	if l.BreakevenAfterTarget > 0 && filledTargets >= l.BreakevenAfterTarget && l.AvgEntry > 0 {
		l.BreakevenArmed = true
		l.ratchetSL(l.AvgEntry)
	}
	// 目标追踪：以最新成交目标价为锚，回撤 step% 设候选 SL。
	if l.TrailingStepPct > 0 {
		for i := range l.Targets {
			tg := &l.Targets[i]
			if tg.Status != ladderLegFilled || tg.Filled <= 0 {
				continue
			}
			if l.dir() > 0 {
				l.ratchetSL(tg.Price * (1 - l.TrailingStepPct/100))
			} else {
				l.ratchetSL(tg.Price * (1 + l.TrailingStepPct/100))
			}
		}
	}
	// 全部买入量已止盈 → 完成（撤掉未成交的入场档）。
	if l.FilledQty > ladderQtyEps && l.ClosedQty >= l.FilledQty-ladderQtyEps {
		e.cancelOpenLegs(l)
		l.Status = LadderStatusCompleted
		l.dirty = true
	}
}

// applySLRules 每轮的被动 SL 规则：保本已激活且均价变化时跟随。
func (e *LadderEngine) applySLRules(l *LadderOrder) {
	if l.BreakevenArmed && l.AvgEntry > 0 {
		l.ratchetSL(l.AvgEntry)
	}
}

// allocateTargets 瀑布式分配：把已成交入场量按顺序摊到各目标
// （每目标容量 = close_pct × 计划总量），需要加量/重挂时撤旧单挂新单。
func (e *LadderEngine) allocateTargets(l *LadderOrder) {
	if l.FilledQty <= ladderQtyEps {
		return
	}
	unallocated := l.FilledQty
	for i := range l.Targets {
		tg := &l.Targets[i]
		capI := l.TotalQty * tg.ClosePct / 100
		want := math.Min(capI, unallocated)
		if want < 0 {
			want = 0
		}
		unallocated -= want
		if tg.Status == ladderLegFilled && want <= tg.Assigned+ladderQtyEps {
			continue // 该目标已用满当前分配量并成交；后续新入场量由瀑布分给更后面的目标
		}
		need := want - tg.Filled // 需要在途的止盈量
		if need <= ladderQtyEps {
			// 容量已全部成交（或被外部撤单后无剩余可挂）。
			if tg.Filled > 0 && tg.Status != ladderLegFilled {
				tg.Status = ladderLegFilled
				l.dirty = true
			}
			continue
		}
		// 在途单已覆盖目标量则不折腾；注意"已成交"的旧单（status=filled）不算覆盖。
		if tg.OrderID != "" && tg.Status != ladderLegFilled && want <= tg.Assigned+ladderQtyEps {
			continue
		}
		if tg.OrderID != "" && tg.Status != ladderLegFilled {
			// 加量：撤旧单换新单（损失排队位置，保持每目标在途 ≤1 张）。
			if _, err := e.placer.CancelOrder(tg.OrderID, l.Symbol); err != nil {
				log.Printf("[ladder] %s target %d cancel for resize failed: %v", l.ID, i, err)
				continue // 撤不掉就保持原单，下轮再试
			}
		}
		tg.OrderID = ""
		tg.OrderFilledBase = tg.Filled
		ord, err := e.placer.PlaceOrder(e.targetRequest(l, i, need))
		if err != nil {
			log.Printf("[ladder] %s target %d place failed: %v", l.ID, i, err)
			continue // 下轮重试（OrderID 为空会重新挂）
		}
		tg.Assigned = want
		tg.OrderID = ord.ID
		tg.Filled = tg.OrderFilledBase + ord.Filled // paper 吃单立即成交的情况
		if tg.Filled >= want-ladderQtyEps {
			tg.Status = ladderLegFilled
		} else if tg.Filled > 0 {
			tg.Status = ladderLegPartial
		} else {
			tg.Status = ladderLegOpen
		}
		l.dirty = true
	}
}

// executeExit 撤全部在途挂单 + 市价平剩余仓位（SL 触发或一键全平共用）。
func (e *LadderEngine) executeExit(l *LadderOrder, reason string) {
	if l.Status != LadderStatusActive {
		return
	}
	e.cancelOpenLegs(l)
	remaining := l.FilledQty - l.ClosedQty
	l.StopReason = reason
	if remaining <= ladderQtyEps {
		// 无剩余仓位：直接终态。
		if reason == "stop_loss" {
			l.Status = LadderStatusStopped
		} else {
			l.Status = LadderStatusFlattened
		}
		l.dirty = true
		l.persist()
		return
	}
	ord, err := e.placer.PlaceOrder(e.exitRequest(l, remaining))
	if err != nil {
		l.Status = LadderStatusFailed
		l.FailReason = fmt.Sprintf("exit market order failed: %v", err)
		l.dirty = true
		l.persist()
		e.notify(l, l.FailReason)
		return
	}
	l.ExitOrderID = ord.ID
	l.ExitFilled = ord.Filled
	l.ExitDeadline = time.Now().Add(ladderExitConfirm).UnixMilli()
	l.ClosedQty += ord.Filled
	l.Status = LadderStatusStopping
	l.dirty = true
	l.persist()
	e.confirmExit(l)
}

// confirmExit 确认市价平仓单结果（stopping → 终态）。
func (e *LadderEngine) confirmExit(l *LadderOrder) {
	ord := e.placer.GetOrder(l.ExitOrderID)
	if ord == nil {
		l.Status = LadderStatusFailed
		l.FailReason = "exit order missing"
		l.dirty = true
		e.notify(l, l.FailReason)
		return
	}
	if ord.Filled > l.ExitFilled {
		l.ClosedQty += ord.Filled - l.ExitFilled
		l.ExitFilled = ord.Filled
		l.dirty = true
	}
	if ord.Status == model.StatusFilled || l.ClosedQty >= l.FilledQty-ladderQtyEps {
		if l.StopReason == "stop_loss" {
			l.Status = LadderStatusStopped
		} else {
			l.Status = LadderStatusFlattened
		}
		l.dirty = true
		return
	}
	if ord.Status == model.StatusRejected || ord.Status == model.StatusCancelled || ord.Status == model.StatusExpired {
		l.Status = LadderStatusFailed
		l.FailReason = fmt.Sprintf("exit order ended %s filled=%.8f", ord.Status, ord.Filled)
		l.dirty = true
		e.notify(l, l.FailReason)
		return
	}
	if time.Now().UnixMilli() > l.ExitDeadline {
		l.Status = LadderStatusFailed
		l.FailReason = fmt.Sprintf("exit order confirm timeout filled=%.8f", ord.Filled)
		l.dirty = true
		e.notify(l, l.FailReason)
	}
}

// cancelOpenLegs 撤掉所有在途入场/止盈挂单。
func (e *LadderEngine) cancelOpenLegs(l *LadderOrder) {
	for i := range l.Entries {
		en := &l.Entries[i]
		if en.OrderID != "" && en.Status != ladderLegFilled && en.Status != ladderLegCancelled {
			if _, err := e.placer.CancelOrder(en.OrderID, l.Symbol); err != nil {
				log.Printf("[ladder] %s entry %d cancel failed: %v", l.ID, i, err)
				continue
			}
			en.Status = ladderLegCancelled
			l.dirty = true
		}
	}
	for i := range l.Targets {
		tg := &l.Targets[i]
		if tg.OrderID != "" && tg.Status != ladderLegFilled && tg.Status != ladderLegCancelled {
			if _, err := e.placer.CancelOrder(tg.OrderID, l.Symbol); err != nil {
				log.Printf("[ladder] %s target %d cancel failed: %v", l.ID, i, err)
				continue
			}
			tg.Status = ladderLegCancelled
			l.dirty = true
		}
	}
}

// Cancel 撤销阶梯单：撤全部挂单，保留已建仓位，不动止盈目标之外的仓位。
func (e *LadderEngine) Cancel(id string) (*LadderOrder, error) {
	l := e.Get(id)
	if l == nil {
		return nil, fmt.Errorf("ladder %s not found", id)
	}
	l.ladMu.Lock()
	defer l.ladMu.Unlock()
	if l.IsTerminal() {
		return l, fmt.Errorf("ladder %s already %s", id, l.Status)
	}
	e.cancelOpenLegs(l)
	l.Status = LadderStatusCancelled
	l.dirty = true
	l.persist()
	return l, nil
}

// Flatten 一键全平：撤全部挂单 + 市价平剩余仓位。
func (e *LadderEngine) Flatten(id string) (*LadderOrder, error) {
	l := e.Get(id)
	if l == nil {
		return nil, fmt.Errorf("ladder %s not found", id)
	}
	l.ladMu.Lock()
	defer l.ladMu.Unlock()
	if l.IsTerminal() {
		return l, fmt.Errorf("ladder %s already %s", id, l.Status)
	}
	if l.Status == LadderStatusStopping {
		return l, fmt.Errorf("ladder %s is already flattening", id)
	}
	e.executeExit(l, "manual_flatten")
	l.persist()
	return l, nil
}

// LadderAmend PUT 改价请求：仅未成交档位/目标可改价，SL/规则随时可改。
type LadderAmend struct {
	Entries []struct {
		Index int     `json:"index"`
		Price float64 `json:"price"`
	} `json:"entries,omitempty"`
	Targets []struct {
		Index int     `json:"index"`
		Price float64 `json:"price"`
	} `json:"targets,omitempty"`
	StopLoss             *float64 `json:"stop_loss,omitempty"`
	BreakevenAfterTarget *int     `json:"breakeven_after_target,omitempty"`
	TrailingStepPct      *float64 `json:"trailing_step_pct,omitempty"`
}

// Amend 拖动改价：未成交入场档/止盈目标改价（撤旧挂新），更新 SL 与规则参数。
func (e *LadderEngine) Amend(id string, am *LadderAmend) (*LadderOrder, error) {
	l := e.Get(id)
	if l == nil {
		return nil, fmt.Errorf("ladder %s not found", id)
	}
	l.ladMu.Lock()
	defer l.ladMu.Unlock()
	if l.Status != LadderStatusActive {
		return l, fmt.Errorf("ladder %s is %s, cannot amend", id, l.Status)
	}
	for _, ea := range am.Entries {
		if ea.Index < 0 || ea.Index >= len(l.Entries) {
			return l, fmt.Errorf("entry index %d out of range", ea.Index)
		}
		if ea.Price <= 0 {
			return l, fmt.Errorf("entry %d: price must be positive", ea.Index)
		}
		en := &l.Entries[ea.Index]
		if en.Filled > 0 || en.Status == ladderLegFilled {
			return l, fmt.Errorf("entry %d already filled, cannot amend", ea.Index)
		}
		if en.OrderID != "" {
			if _, err := e.placer.CancelOrder(en.OrderID, l.Symbol); err != nil {
				return l, fmt.Errorf("entry %d cancel for amend failed: %w", ea.Index, err)
			}
			en.OrderID = ""
		}
		en.Price = ea.Price
		ord, err := e.placer.PlaceOrder(e.entryRequest(l, ea.Index))
		if err != nil {
			en.Status = ladderLegCancelled
			l.dirty = true
			l.persist()
			return l, fmt.Errorf("entry %d re-place failed: %w", ea.Index, err)
		}
		en.OrderID = ord.ID
		en.Filled = ord.Filled
		en.Status = entryStatusFromOrder(ord)
		l.dirty = true
	}
	for _, ta := range am.Targets {
		if ta.Index < 0 || ta.Index >= len(l.Targets) {
			return l, fmt.Errorf("target index %d out of range", ta.Index)
		}
		if ta.Price <= 0 {
			return l, fmt.Errorf("target %d: price must be positive", ta.Index)
		}
		tg := &l.Targets[ta.Index]
		if tg.Filled > 0 || tg.Status == ladderLegFilled {
			return l, fmt.Errorf("target %d already filled, cannot amend", ta.Index)
		}
		tg.Price = ta.Price
		if tg.OrderID != "" {
			if _, err := e.placer.CancelOrder(tg.OrderID, l.Symbol); err != nil {
				return l, fmt.Errorf("target %d cancel for amend failed: %w", ta.Index, err)
			}
			tg.OrderID = ""
			qty := tg.Assigned - tg.Filled
			if qty > ladderQtyEps {
				ord, err := e.placer.PlaceOrder(e.targetRequest(l, ta.Index, qty))
				if err != nil {
					l.dirty = true
					l.persist()
					return l, fmt.Errorf("target %d re-place failed: %w", ta.Index, err)
				}
				tg.OrderID = ord.ID
				tg.Status = ladderLegOpen
			}
		}
		l.dirty = true
	}
	if am.StopLoss != nil {
		if *am.StopLoss < 0 {
			return l, fmt.Errorf("stop_loss must be >= 0")
		}
		l.StopLoss = *am.StopLoss
		l.CurrentSL = *am.StopLoss
		l.dirty = true
	}
	if am.BreakevenAfterTarget != nil {
		if *am.BreakevenAfterTarget < 0 || *am.BreakevenAfterTarget > len(l.Targets) {
			return l, fmt.Errorf("breakeven_after_target must be in [0,%d]", len(l.Targets))
		}
		l.BreakevenAfterTarget = *am.BreakevenAfterTarget
		l.dirty = true
	}
	if am.TrailingStepPct != nil {
		if *am.TrailingStepPct < 0 || *am.TrailingStepPct >= 100 {
			return l, fmt.Errorf("trailing_step_pct must be in [0,100)")
		}
		l.TrailingStepPct = *am.TrailingStepPct
		l.dirty = true
	}
	l.persist()
	return l, nil
}

// Get 查询阶梯单。
func (e *LadderEngine) Get(id string) *LadderOrder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.orders[id]
}

// List 返回全部（可选按 user 过滤；userID=0 不过滤）。
func (e *LadderEngine) List(userID uint64) []*LadderOrder {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*LadderOrder, 0, len(e.orders))
	for _, l := range e.orders {
		if userID != 0 && l.UserID != userID {
			continue
		}
		out = append(out, l)
	}
	return out
}

// RestoreFromStore 扫描 xt_ladder_orders 中未终结的阶梯单，重建内存状态继续
// 执行状态机；子单不在 OMS 内存时从 xt_orders 回填（同 ltm 恢复机制）。
// 幂等：已跟踪的 ID 跳过。返回新恢复数量；store 未初始化（单测）时返回 0。
func (e *LadderEngine) RestoreFromStore() int {
	if store.GetDB() == nil {
		return 0
	}
	recs, err := store.NewLadderOrderRepo().ListActive()
	if err != nil {
		log.Printf("[ladder] restore scan failed: %v", err)
		return 0
	}
	restored := 0
	for _, rec := range recs {
		e.mu.Lock()
		_, tracked := e.orders[rec.ID]
		e.mu.Unlock()
		if tracked {
			continue
		}
		var l LadderOrder
		if err := json.Unmarshal([]byte(rec.StateJSON), &l); err != nil {
			log.Printf("[ladder] restore %s decode failed: %v", rec.ID, err)
			continue
		}
		l.dirty = false
		l.ladMu = &sync.Mutex{}
		// 宕机前正处于 stopping 的：平仓确认窗口已过期的给一个新鲜宽限期再确认。
		if l.Status == LadderStatusStopping && l.ExitDeadline < time.Now().UnixMilli() {
			l.ExitDeadline = time.Now().Add(ladderExitConfirm).UnixMilli()
		}
		// 回填 OMS 内存：重启后 OMS 是空的，不回填则查单/撤单全部 miss。
		e.rehydrateLegs(&l)
		e.mu.Lock()
		e.orders[l.ID] = &l
		e.mu.Unlock()
		restored++
		log.Printf("[ladder] restored %s %s %s filled=%.8f/%.8f closed=%.8f sl=%.8g",
			l.ID, l.Symbol, l.Status, l.FilledQty, l.TotalQty, l.ClosedQty, l.CurrentSL)
	}
	return restored
}

// rehydrateLegs 把阶梯单各腿订单回填进 OMS 内存（从 xt_orders）。
func (e *LadderEngine) rehydrateLegs(l *LadderOrder) {
	rh, ok := e.placer.(omsRehydrater)
	if !ok {
		return
	}
	repo := store.GetOrderRepo()
	ids := make([]string, 0, len(l.Entries)+len(l.Targets)+1)
	for i := range l.Entries {
		if l.Entries[i].OrderID != "" {
			ids = append(ids, l.Entries[i].OrderID)
		}
	}
	for i := range l.Targets {
		if l.Targets[i].OrderID != "" {
			ids = append(ids, l.Targets[i].OrderID)
		}
	}
	if l.ExitOrderID != "" {
		ids = append(ids, l.ExitOrderID)
	}
	for _, id := range ids {
		if e.placer.GetOrder(id) != nil {
			continue
		}
		rec, err := repo.GetByID(id)
		if err != nil || rec == nil {
			continue
		}
		rh.HandleOrderUpdate(lmRecordToOrderData(rec))
	}
}

// persist 状态落库（dirty 才写；store 未初始化时跳过）。
func (l *LadderOrder) persist() {
	if !l.dirty || store.GetDB() == nil {
		return
	}
	l.UpdatedAt = time.Now().UnixMilli()
	state, err := json.Marshal(l)
	if err != nil {
		log.Printf("[ladder] %s marshal failed: %v", l.ID, err)
		return
	}
	spec := map[string]any{
		"symbol": l.Symbol, "side": l.Side, "exchange": l.Exchange,
		"stop_loss": l.StopLoss, "breakeven_after_target": l.BreakevenAfterTarget,
		"trailing_step_pct": l.TrailingStepPct, "total_qty": l.TotalQty,
	}
	specJSON, _ := json.Marshal(spec)
	if err := store.NewLadderOrderRepo().Upsert(&store.LadderOrderRecord{
		ID: l.ID, UserID: int64(l.UserID), Symbol: l.Symbol, Side: string(l.Side),
		Exchange: l.Exchange, Status: l.Status,
		SpecJSON: string(specJSON), StateJSON: string(state),
		CreatedAt: l.CreatedAt,
	}); err != nil {
		log.Printf("[ladder] %s persist failed: %v", l.ID, err)
		return
	}
	l.dirty = false
}

func (e *LadderEngine) notify(l *LadderOrder, msg string) {
	if ladderNotifyHook != nil {
		ladderNotifyHook(l, msg)
	}
}

// entryStatusFromOrder 由 OMS 订单状态推导档位状态。
func entryStatusFromOrder(ord *model.OrderData) string {
	switch {
	case ord.Status == model.StatusFilled || ord.Filled >= ord.Quantity && ord.Quantity > 0:
		return ladderLegFilled
	case ord.Status == model.StatusCancelled || ord.Status == model.StatusRejected || ord.Status == model.StatusExpired:
		return ladderLegCancelled
	case ord.Filled > 0:
		return ladderLegPartial
	case ord.Status == model.StatusNew || ord.Status == model.StatusPartiallyFilled:
		return ladderLegOpen
	default:
		return ladderLegPending
	}
}

var ladderSeq atomic.Int64

func newLadderID() string {
	n := ladderSeq.Add(1)
	return fmt.Sprintf("%d-%d", time.Now().UnixMilli(), n)
}
