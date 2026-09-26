package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 信号机器人阶梯 TP/SL 管理器（Go 版，对齐 THREE_BOTS_DESIGN.md 3.4 节）──
//
// 状态机持久化在 xt_signal_executions：信号成交后 attach 一条执行记录
// （status=active），价格 tick 驱动 evalTPSL 推进 TP1→TP2→TP3→SL，
// 每步应用后写库；平仓时回填信号记录的 realized_pnl，并按订阅定价
// 累计 profit_share 分成。重启后从 ListActive 恢复监控对象。

type tpslActionKind string

const (
	tpslActionTP1      tpslActionKind = "tp1"
	tpslActionTP2      tpslActionKind = "tp2"
	tpslActionTP3      tpslActionKind = "tp3"
	tpslActionSL       tpslActionKind = "sl"
	tpslActionTrailing tpslActionKind = "trailing"
)

// tpslAction 一次价格评估触发的一个执行动作。
type tpslAction struct {
	Kind     tpslActionKind `json:"kind"`
	ExecID   string         `json:"exec_id"`
	Symbol   string         `json:"symbol"`
	Price    float64        `json:"price"` // 触发价（TP 用档位价，SL 用 current_sl，trailing 用当前价）
	Quantity float64        `json:"quantity"`
}

type signalTPSLManager struct {
	mu     sync.Mutex
	active map[string]*store.SignalExecution // execID -> 执行记录
	loaded bool
}

var sigTPSLManager = &signalTPSLManager{active: make(map[string]*store.SignalExecution)}

// attachTPSL 挂接一条活跃执行记录（信号成交后调用）。
func (m *signalTPSLManager) attach(e *store.SignalExecution) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[e.ID] = e
}

// detachTPSL 移除已平仓的执行记录。
func (m *signalTPSLManager) detach(execID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, execID)
}

// ensureLoaded 首次使用时从库里恢复未平仓的执行记录（重启续接）。
func (m *signalTPSLManager) ensureLoaded() {
	m.mu.Lock()
	if m.loaded {
		m.mu.Unlock()
		return
	}
	m.loaded = true
	m.mu.Unlock()
	execs, err := store.NewSignalExecutionRepo().ListActive()
	if err != nil {
		log.Printf("[executor] reload active executions failed: %v", err)
		return
	}
	m.mu.Lock()
	for _, e := range execs {
		m.active[e.ID] = e
	}
	m.mu.Unlock()
}

// activeExecutions 返回全部活跃执行记录（快照）。
func (m *signalTPSLManager) activeExecutions() []*store.SignalExecution {
	m.ensureLoaded()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.SignalExecution, 0, len(m.active))
	for _, e := range m.active {
		out = append(out, e)
	}
	return out
}

// evalTPSL 价格状态机（纯评估，副作用只在 TrailingPeak 上）：
// LONG 方向 price >= 档位价触发止盈、price <= current_sl 触发止损；
// SHORT 方向相反；TP1 触发后激活追踪（若有 trailing_pct）并按
// move_sl_to 上移止损。返回按 TP→SL→trailing 排序的动作列表。
func evalTPSL(e *store.SignalExecution, price float64) []tpslAction {
	var actions []tpslAction
	isLong := e.Direction != "SHORT"

	// ── 阶梯止盈（已触发的档位跳过）──
	type level struct {
		kind   tpslActionKind
		price  float64
		qty    float64
		filled bool
	}
	levels := []level{
		{tpslActionTP1, e.TP1Price, e.TP1Qty, e.TP1Filled},
		{tpslActionTP2, e.TP2Price, e.TP2Qty, e.TP2Filled},
		{tpslActionTP3, e.TP3Price, e.TP3Qty, e.TP3Filled},
	}
	for _, lv := range levels {
		if lv.filled || lv.price <= 0 {
			continue
		}
		triggered := (isLong && price >= lv.price) || (!isLong && price <= lv.price)
		if triggered {
			actions = append(actions, tpslAction{Kind: lv.kind, ExecID: e.ID, Symbol: e.Symbol, Price: lv.price, Quantity: lv.qty})
		}
	}

	// ── 追踪止盈：TP1 触发后记录峰值，回撤超过 trailing_pct 全平 ──
	if e.TrailingActive && e.TrailingPct > 0 && price > 0 {
		if (isLong && price > e.TrailingPeak) || (!isLong && (e.TrailingPeak == 0 || price < e.TrailingPeak)) {
			e.TrailingPeak = price
		}
		if e.TrailingPeak > 0 {
			var drawdown float64
			if isLong {
				drawdown = (e.TrailingPeak - price) / e.TrailingPeak
			} else {
				drawdown = (price - e.TrailingPeak) / e.TrailingPeak
			}
			if drawdown >= e.TrailingPct && e.RemainingQty > 0 {
				actions = append(actions, tpslAction{Kind: tpslActionTrailing, ExecID: e.ID, Symbol: e.Symbol, Price: price, Quantity: e.RemainingQty})
				return actions // 全平收尾，不再评估 SL
			}
		}
	}

	// ── 止损（trailing 全平优先，未触发才看 SL）──
	if e.CurrentSL > 0 && e.RemainingQty > 0 {
		hit := (isLong && price <= e.CurrentSL) || (!isLong && price >= e.CurrentSL)
		if hit {
			actions = append(actions, tpslAction{Kind: tpslActionSL, ExecID: e.ID, Symbol: e.Symbol, Price: e.CurrentSL, Quantity: e.RemainingQty})
		}
	}
	return actions
}

// TickExecutorTPSL 供外部价格源驱动状态机（后台循环与单测均走这里）。
// 返回本轮触发的动作数。
func TickExecutorTPSL(symbol string, price float64) int {
	if price <= 0 {
		return 0
	}
	n := 0
	for _, e := range sigTPSLManager.activeExecutions() {
		if e.Symbol != symbol {
			continue
		}
		for _, act := range evalTPSL(e, price) {
			applyTPSLAction(e, act)
			n++
		}
	}
	return n
}

// applyTPSLAction 应用一个动作：走组合持仓管线减仓/平仓，推进状态机并落库。
func applyTPSLAction(e *store.SignalExecution, act tpslAction) {
	repo := store.NewSignalExecutionRepo()
	now := time.Now().UnixMilli()

	closeLeg := func(price, qty float64, orderTag string) float64 {
		if qty <= 0 || price <= 0 {
			return 0
		}
		// 双腿标识：LONG 仓用 SELL 减、SHORT 仓用 BUY 减（hedge/单向同规则）
		side := "SELL"
		if e.Direction == "SHORT" {
			side = "BUY"
		}
		order := map[string]any{
			"symbol":         e.Symbol,
			"side":           side,
			"type":           "MARKET",
			"price":          price,
			"quantity":       qty,
			"source":         "signal_executor",
			"strategy":       orderTag,
			"market_type":    "swap",
			"position_side":  e.PositionSide,
			"margin_mode":    map[bool]string{true: "cross", false: "isolated"}[e.MarginMode != "isolated"],
			"close_position": true,
		}
		fillOrderAndUpdatePortfolio(order)
		pnl, _ := order["realized_pnl"].(float64)
		return pnl
	}

	switch act.Kind {
	case tpslActionTP1, tpslActionTP2, tpslActionTP3:
		if e.RemainingQty <= 0 {
			return
		}
		qty := act.Quantity
		if qty > e.RemainingQty {
			qty = e.RemainingQty
		}
		pnl := closeLeg(act.Price, qty, "signal_"+string(act.Kind))
		e.RealizedPnL += pnl
		e.RemainingQty -= qty
		switch act.Kind {
		case tpslActionTP1:
			e.TP1Filled, e.TP1Time = true, now
		case tpslActionTP2:
			e.TP2Filled, e.TP2Time = true, now
		case tpslActionTP3:
			e.TP3Filled, e.TP3Time = true, now
		}
		// 当前有效 TP = 下一未触发档位
		e.CurrentTP = 0
		for _, lv := range []struct {
			price  float64
			filled bool
		}{{e.TP1Price, e.TP1Filled}, {e.TP2Price, e.TP2Filled}, {e.TP3Price, e.TP3Filled}} {
			if !lv.filled && lv.price > 0 {
				e.CurrentTP = lv.price
				break
			}
		}
		// TP1 触发后：移动止损到保本位 + 激活追踪止盈
		if act.Kind == tpslActionTP1 {
			if e.MoveSLTo > 0 {
				e.CurrentSL = e.MoveSLTo
				e.SLPrice = e.MoveSLTo
			}
			if e.TrailingPct > 0 {
				e.TrailingActive = true
				e.TrailingPeak = act.Price
			}
		}
		if e.RemainingQty <= 1e-12 {
			e.RemainingQty = 0
			finalizeExecution(e, repo, "tp3", now)
			return
		}
		_ = repo.Update(e)

	case tpslActionSL, tpslActionTrailing:
		if e.RemainingQty <= 0 {
			return
		}
		pnl := closeLeg(act.Price, e.RemainingQty, "signal_"+string(act.Kind))
		e.RealizedPnL += pnl
		e.RemainingQty = 0
		e.SLTriggered = e.SLTriggered || act.Kind == tpslActionSL
		if act.Kind == tpslActionSL {
			e.SLTime = now
		}
		reason := string(act.Kind)
		finalizeExecution(e, repo, reason, now)
	}
}

// finalizeExecution 平仓收尾：状态→closed、回填信号记录、累计 profit_share 分成。
func finalizeExecution(e *store.SignalExecution, repo *store.SignalExecutionRepo, reason string, now int64) {
	e.Status = "closed"
	e.CloseReason = reason
	e.ClosedAt = now
	e.CurrentTP = 0
	e.CurrentSL = 0
	if err := repo.Update(e); err != nil {
		log.Printf("[executor] finalize execution %s failed: %v", e.ID, err)
	}
	sigTPSLManager.detach(e.ID)

	// 回填信号记录终态
	sigRepo := store.NewSignalRepo()
	if sig, err := sigRepo.GetByID(strconv.Itoa(e.SignalID)); err == nil && sig != nil {
		sig.Status = "CLOSED"
		sig.RealizedPnL = e.RealizedPnL
		sig.ClosedAt = now
		if err := sigRepo.Update(sig); err != nil {
			log.Printf("[executor] signal %d finalize failed: %v", e.SignalID, err)
		}
	}

	// profit_share 分成：盈利时给该信号源的活跃订阅累计 pending_share
	if e.RealizedPnL > 0 && e.SourceID != "" {
		srcRepo := store.NewSignalSourceRepo()
		if src, err := srcRepo.GetByID(e.SourceID); err == nil && src != nil && src.FeeModel == "profit_share" && src.FeePercent > 0 {
			share := e.RealizedPnL * src.FeePercent / 100
			if err := srcRepo.AddPendingShare(e.SourceID, share); err != nil {
				log.Printf("[executor] add pending share for source %s failed: %v", e.SourceID, err)
			}
		}
	}
	log.Printf("[executor] signal %d closed: reason=%s pnl=%.4f", e.SignalID, reason, e.RealizedPnL)
}

// ── 后台价格循环（懒启动，避免 init 期 DB 未就绪）──

var tpslLoopOnce sync.Once

// startTPSLLoop 懒启动阶梯监控循环（executor 读接口首次调用时触发）。
func startTPSLLoop() {
	tpslLoopOnce.Do(func() {
		sigTPSLManager.ensureLoaded()
		go tpslPriceLoop()
	})
}

func tpslPriceLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		symbols := map[string]bool{}
		for _, e := range sigTPSLManager.activeExecutions() {
			symbols[e.Symbol] = true
		}
		for symbol := range symbols {
			price := executorFetchPrice(symbol)
			if price > 0 {
				TickExecutorTPSL(symbol, price)
			}
		}
	}
}

// executorFetchPrice 从币安公共行情拉最新价（与 portfolio.getBinancePrice
// 同口径；失败返回 0，调用方跳过本轮）。
func executorFetchPrice(symbol string) float64 {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}
	if priceStr, ok := result["price"].(string); ok {
		f, _ := strconv.ParseFloat(priceStr, 64)
		return f
	}
	return 0
}
