package handler

// paper_matcher.go —— paper（模拟盘）限价单撮合循环
//
// 背景：paper 交易所适配器只处理市价单的即时成交（PlaceOrder 返回 FILLED），
// 限价单落库后没有任何撮合方，永远停在 NEW —— 这是"下单逻辑没实现"的核心缺口。
// 本循环每 2 秒扫描一遍 OMS 里的 paper 活动限价单，价格穿越限价即整单成交：
//   - 买限价：市价 >= 限价 → 成交
//   - 卖限价：市价 <= 限价 → 成交
// 成交价一律取限价（保守模拟：买单不赚滑点差价）。
// 成交走两条更新路径，缺一不可：
//   1) order.RecordFill —— 翻转 OMS 订单状态、持久化，订单/成交列表立即可见
//   2) fillOrderAndUpdatePortfolio —— 更新组合余额与持仓（复用止损引擎同款函数）

import (
	"log"
	"time"

	"github.com/xiaotian-quant/gateway/internal/execution"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/service"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// StartPaperMatcher 启动 paper 撮合循环。由 main 在 store.InitDB() 之后
// 显式调用——虽然本循环不读 DB，但 fillOrderAndUpdatePortfolio 依赖
// portfolio.GetManager() 已就绪。初始化期单次调用，无需加锁。
func StartPaperMatcher() {
	if paperMatcherStarted {
		return
	}
	paperMatcherStarted = true
	go paperMatchLoop()
	log.Printf("[paper-match] 撮合循环已启动")
}

var paperMatcherStarted bool

func paperMatchLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		matchPaperOrders()
	}
}

func matchPaperOrders() {
	om := order.GetOrderManager()
	if om == nil {
		return
	}
	if portfolio.GetManager() == nil {
		return
	}
	priceCache := map[string]float64{}
	execCfg := paper.GetPaperExchange().ExecutionConfig()
	for _, o := range om.GetOpenOrders("") {
		if !isPaperExchange(o.Exchange) {
			continue
		}
		if o.OrderType != model.TypeLimit {
			continue // 市价/条件单各有自己的成交路径
		}
		px, ok := priceCache[o.Symbol]
		if !ok {
			px = executorFetchPrice(o.Symbol)
			priceCache[o.Symbol] = px
		}
		if px <= 0 {
			continue
		}
		var crossed bool
		if o.Side == model.SideBuy {
			crossed = px >= o.Price
		} else {
			crossed = px <= o.Price
		}
		if !crossed {
			continue
		}
		plan, ok := planPaperFill(o, execCfg)
		if !ok {
			continue
		}
		fill := plan.fill
		fee := plan.fee
		filledNow := plan.filled
		status := plan.status
		// 1) OMS 状态翻转 + 持久化（幂等：RecordFill 对已终态单直接跳过）
		changed, err := om.RecordFillIfActive(o.ID, fill, o.Price)
		if err != nil {
			log.Printf("[paper-match] RecordFill %s 失败: %v", o.ID, err)
			continue
		}
		if !changed {
			continue
		}
		// 只在整单成交后撤掉内部订单簿残留；部分成交仍是活动单。
		if status == model.StatusFilled {
			if err := service.GetMatchingService().RemoveOrderFromBook(o.Symbol, o.ID); err != nil {
				log.Printf("[paper-match] internal order-book cleanup %s failed: %v", o.ID, err)
			}
		}
		// 1b) 展示层订单表同步（GetOrders 读 store 内存表，不同步会永远停在 NEW）
		store.UpdateOrderFill(o.ID, filledNow, o.Price, string(status))
		// 2) 组合余额/持仓更新（spot 扣加余额，swap 开/平仓位与保证金）
		fillOrderAndUpdatePortfolio(map[string]any{
			"symbol":         o.Symbol,
			"side":           string(o.Side),
			"price":          o.Price,
			"quantity":       fill,
			"fee":            fee,
			"market_type":    string(o.MarketType),
			"leverage":       o.Leverage,
			"margin_mode":    string(o.MarginMode),
			"position_side":  string(o.PositionSide),
			"close_position": o.ClosePosition,
		})
		log.Printf("[paper-match] %s %s %s LIMIT %.8f/%.8f @ %.4f 成交 %.8f（市价 %.4f，费 %.6f）",
			o.Symbol, o.Side, o.ID, filledNow, o.Quantity, o.Price, fill, px, fee)
	}
}

// paperFillPlan 是一次触价后的成交计划，抽成纯函数便于用统一模型做单测。
type paperFillPlan struct {
	fill   float64
	fee    float64
	filled float64
	status model.OrderStatus
}

// planPaperFill 按共享执行模型计算本次成交量、手续费和订单状态。
// 限价单成交价取限价（不额外滑点），成交量按 PartialFillRate 拆分。
func planPaperFill(o *model.OrderData, cfg execution.Config) (paperFillPlan, bool) {
	plan, ok := execution.PlanLimitFill(o.Price, o.Quantity, o.Filled, cfg)
	if !ok {
		return paperFillPlan{}, false
	}
	status := model.StatusPartiallyFilled
	if plan.Full {
		status = model.StatusFilled
	}
	return paperFillPlan{
		fill:   plan.Fill,
		fee:    plan.Fee,
		filled: plan.Filled,
		status: status,
	}, true
}
