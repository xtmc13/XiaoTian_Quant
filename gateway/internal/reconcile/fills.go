package reconcile

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// A8.2 成交恢复：进程重启/断线重连后，对本地仍处 open/partially_filled 的
// 实盘订单向交易所查询最新状态与成交明细，漏记的成交按交易所 trade id
// 幂等补进 store，并回填网格/DCA/分层马丁引擎状态。

// OrderStatusQuerier 查询交易所订单最新状态（adapter 可选实现）。
type OrderStatusQuerier interface {
	QueryOrderStatus(symbol, orderID string) (OrderStatusInfo, error)
}

// OrderStatusInfo 归一化的交易所订单状态。
type OrderStatusInfo struct {
	Status    string  // NEW|PARTIALLY_FILLED|FILLED|CANCELLED|REJECTED|EXPIRED
	FilledQty float64 // 累计成交量
	AvgPrice  float64 // 累计成交均价
}

// OrderTradesQuerier 查询交易所订单的成交明细（adapter 可选实现）。
type OrderTradesQuerier interface {
	GetOrderTrades(symbol, orderID string) ([]AccountTradeLike, error)
}

// AccountTradeLike 归一化成交流水（与 adapter.AccountTrade 字段对齐，避免 reconcile 反向依赖 adapter）。
type AccountTradeLike struct {
	TradeID   string
	OrderID   string
	Symbol    string
	Side      string
	Price     float64
	Quantity  float64
	Fee       float64
	FeeAsset  string
	Timestamp int64
}

// FillApplier 把恢复出的成交回填给 bot 引擎（cmd/server 启动时注册真实实现）。
// side: BUY/SELL；qty/price 为该订单本次恢复出的总量与加权均价。
type FillApplier func(botID, side string, qty, price float64) error

var (
	fillAppliersMu sync.RWMutex
	fillAppliers   = map[string]FillApplier{}
)

// RegisterFillApplier 注册某类 bot（dca/lmartin/grid）的成交回填实现。
func RegisterFillApplier(kind string, fn FillApplier) {
	fillAppliersMu.Lock()
	defer fillAppliersMu.Unlock()
	fillAppliers[kind] = fn
}

// applyBotFill 按 client_oid 前缀 "kind:botID" 分发成交回填。
func applyBotFill(clientOID, side string, qty, price float64) {
	kind, botID, ok := splitClientOID(clientOID)
	if !ok {
		return
	}
	fillAppliersMu.RLock()
	fn := fillAppliers[kind]
	fillAppliersMu.RUnlock()
	if fn == nil {
		return
	}
	if err := fn(botID, side, qty, price); err != nil {
		log.Printf("[reconcile] bot %s %s 成交回填失败: %v", kind, botID, err)
	}
}

// splitClientOID 解析 "kind:botID" 形式的 client_oid（A8.2 bot 成交回填路由）。
func splitClientOID(oid string) (kind, botID string, ok bool) {
	idx := strings.Index(oid, ":")
	if idx <= 0 || idx == len(oid)-1 {
		return "", "", false
	}
	kind = oid[:idx]
	botID = oid[idx+1:]
	switch kind {
	case "dca", "lmartin", "grid":
		return kind, botID, true
	}
	return "", "", false
}

// FillRecoverer A8.2 成交恢复任务。
type FillRecoverer struct {
	repo        *store.ReconcileRepo
	exchangeFor func(name string) any
	maxOrders   func() int
}

// NewFillRecoverer exchangeFor 注入带凭证的 adapter（cmd/server），返回 nil 表示未配置。
func NewFillRecoverer(repo *store.ReconcileRepo, exchangeFor func(name string) any) *FillRecoverer {
	return &FillRecoverer{repo: repo, exchangeFor: exchangeFor}
}

// activeStatuses 视为"未完结"的本地订单状态（open/partially_filled）。
var activeStatuses = []string{
	string(model.StatusNew), string(model.StatusPartiallyFilled), string(model.StatusPending),
}

// Run 跑一轮成交恢复：查交易所 → 补成交 → 推进状态 → 回填 bot 引擎。
func (r *FillRecoverer) Run() (string, error) {
	orderRepo := store.GetOrderRepo()
	limit := DefaultFillRecoveryLimit

	// 收集本地未完结的实盘订单（排除 paper/空交易所）。
	var candidates []*store.OrderRecord
	seen := map[string]bool{}
	for _, st := range activeStatuses {
		rows, err := orderRepo.List(map[string]any{"status": st}, limit)
		if err != nil {
			return "", fmt.Errorf("list active orders: %w", err)
		}
		for _, o := range rows {
			ex := strings.ToLower(o.Exchange)
			if ex == "" || ex == "paper" || ex == "matching" || seen[o.ID] {
				continue
			}
			seen[o.ID] = true
			candidates = append(candidates, o)
			if len(candidates) >= limit {
				break
			}
		}
		if len(candidates) >= limit {
			break
		}
	}

	recoveredTrades := 0
	advanced := 0
	for _, o := range candidates {
		trades, statusAdvanced, err := r.recoverOne(o)
		if err != nil {
			// 单订单失败（网络/凭证）只记日志，下轮重试。
			log.Printf("[reconcile] 成交恢复 %s/%s 失败: %v", o.Exchange, o.ID, err)
			continue
		}
		recoveredTrades += trades
		if statusAdvanced {
			advanced++
		}
	}
	return fmt.Sprintf("orders=%d recovered_trades=%d advanced=%d", len(candidates), recoveredTrades, advanced), nil
}

// recoverOne 恢复单个订单：补成交 + 推进状态。返回 (新补成交数, 状态是否推进, err)。
func (r *FillRecoverer) recoverOne(o *store.OrderRecord) (int, bool, error) {
	exAny := r.exchangeFor(strings.ToLower(o.Exchange))
	if exAny == nil {
		return 0, false, fmt.Errorf("交易所 %s 未配置凭证", o.Exchange)
	}
	tradeQ, okTrade := exAny.(OrderTradesQuerier)
	statusQ, okStatus := exAny.(OrderStatusQuerier)
	if !okTrade && !okStatus {
		return 0, false, fmt.Errorf("交易所 %s 未实现订单查询接口", o.Exchange)
	}

	// 1) 成交明细补录（按交易所 trade id 幂等）。
	newTrades := 0
	var recoveredQty, recoveredCost float64
	tradeRepo := store.NewTradeRepo()
	if okTrade {
		trades, err := tradeQ.GetOrderTrades(o.Symbol, o.ID)
		if err != nil {
			return 0, false, fmt.Errorf("query trades: %w", err)
		}
		for _, t := range trades {
			if !strings.EqualFold(t.OrderID, o.ID) {
				continue
			}
			tradeID := fmt.Sprintf("%s:%s", strings.ToLower(o.Exchange), t.TradeID)
			if tradeRepo.Exists(tradeID) {
				continue // 已记录（幂等去重）
			}
			side := strings.ToUpper(t.Side)
			if side == "" {
				side = strings.ToUpper(o.Side)
			}
			rec := &store.TradeRecord{
				ID:          tradeID,
				UserID:      int64(o.UserID),
				OrderID:     o.ID,
				Symbol:      o.Symbol,
				Side:        side,
				Price:       t.Price,
				Quantity:    t.Quantity,
				Fee:         t.Fee,
				FeeCurrency: t.FeeAsset,
				Exchange:    o.Exchange,
				ExecPhase:   execPhaseFor(o.ClientOID),
				CreatedAt:   t.Timestamp,
			}
			if rec.CreatedAt == 0 {
				rec.CreatedAt = nowMilli()
			}
			if err := tradeRepo.Create(rec); err != nil {
				return newTrades, false, fmt.Errorf("insert trade %s: %w", tradeID, err)
			}
			newTrades++
			recoveredQty += t.Quantity
			recoveredCost += t.Price * t.Quantity
		}
		if newTrades > 0 {
			avg := recoveredCost / recoveredQty
			log.Printf("[reconcile] 订单 %s 补录 %d 笔成交 (qty=%.8f avg=%.8f)", o.ID, newTrades, recoveredQty, avg)
			applyBotFill(o.ClientOID, strings.ToUpper(o.Side), recoveredQty, avg)
			notifyDiff(
				fmt.Sprintf("成交已恢复: %s", o.Symbol),
				fmt.Sprintf("订单 %s 补录 %d 笔漏记成交 (qty=%.8f avg=%.8f)", o.ID, newTrades, recoveredQty, avg),
				"INFO", "order", "fill_recovered",
				map[string]any{"order_id": o.ID, "symbol": o.Symbol, "trades": newTrades},
			)
		}
	}

	// 2) 订单状态推进。
	if !okStatus {
		return newTrades, false, nil
	}
	info, err := statusQ.QueryOrderStatus(o.Symbol, o.ID)
	if err != nil {
		// 订单在交易所查不到（可能已撤/已完结被清理）：不强行推进，下轮再看。
		return newTrades, false, fmt.Errorf("query status: %w", err)
	}
	newStatus, ok := mapExchangeStatus(info.Status)
	if !ok {
		return newTrades, false, nil
	}
	if string(newStatus) == o.Status && abs(info.FilledQty-o.Filled) <= 0 {
		return newTrades, false, nil
	}
	// 交易所累计成交 ≥ 本地数量 → 完结；否则 partially_filled。
	o.Filled = info.FilledQty
	if info.AvgPrice > 0 {
		o.AvgFillPrice = info.AvgPrice
	}
	o.Status = string(newStatus)
	if err := store.GetOrderRepo().Update(o); err != nil {
		return newTrades, false, fmt.Errorf("update order: %w", err)
	}
	// 同步 OMS 内存态（触发 OnOrderUpdate 回调，联动持仓/通知）。
	om := order.GetOrderManager()
	if existing := om.GetOrder(o.ID); existing != nil {
		existing.Filled = o.Filled
		existing.AvgFillPrice = o.AvgFillPrice
		existing.Status = newStatus
		om.HandleOrderUpdate(existing)
	}
	return newTrades, true, nil
}

// execPhaseFor 成交阶段标记：A2.2 limit-then-market 两阶段 + 默认 recovered。
func execPhaseFor(clientOID string) string {
	if strings.HasPrefix(clientOID, "ltm-mkt:") {
		return "market"
	}
	if strings.HasPrefix(clientOID, "ltm-") {
		return "limit"
	}
	return "recovered"
}

// mapExchangeStatus 交易所原始状态 → 本地状态。
func mapExchangeStatus(s string) (model.OrderStatus, bool) {
	switch strings.ToUpper(s) {
	case "NEW", "OPEN":
		return model.StatusNew, true
	case "PARTIALLY_FILLED":
		return model.StatusPartiallyFilled, true
	case "FILLED":
		return model.StatusFilled, true
	case "CANCELED", "CANCELLED":
		return model.StatusCancelled, true
	case "REJECTED":
		return model.StatusRejected, true
	case "EXPIRED":
		return model.StatusExpired, true
	}
	return "", false
}
