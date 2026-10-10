package app

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── 2026-10-10 生产实锤回归：信号单风控拒单 → 幽灵持仓 ──
// 事故：策略 67a503f2（cra_contract SOLUSDT dual 100x 1m）首根 K 线发 LONG
// 首单 → PlaceOrder 风控拒单（net exposure 224.4% exceeds limit 80.0%）→
// context.go 错误分支只记日志 return；策略在信号时已 EnterPosition，拒单
// 终态没有回传消费 → 引擎永久虚持仓（面板：做多/均价/在仓时长，数量 0），
// xt_orders 与币安均无仓。本文件锁两条链路：
//   A. 风控/AI门/锁仓/交易所拒绝（ord!=nil，OMS 已广播）——事件恰好一次
//      到达策略并完全回滚；同根 bar 不重发，次根 bar 允许重试。
//   B. 订单创建前失败（ord==nil，OMS 不覆盖）——context.go 补发同型拒绝
//      事件，策略同样回滚。

// rejectWatcher 包裹生产已接线的 om.OnOrderUpdate，按 client_oid 前缀捕获
// 拒绝事件（断言事件只发一次），随后放行给原通道（组合入账 + 事件总线）。
type rejectWatcher struct {
	mu     sync.Mutex
	orders []model.OrderData
}

func (w *rejectWatcher) wrap(t *testing.T, om *order.OrderManager) {
	t.Helper()
	orig := om.OnOrderUpdate
	if orig == nil {
		t.Fatal("OnOrderUpdate not wired（wireOrderManager 未生效）")
	}
	om.OnOrderUpdate = func(o *model.OrderData) {
		if o.Status == model.StatusRejected {
			w.mu.Lock()
			w.orders = append(w.orders, *o)
			w.mu.Unlock()
		}
		orig(o)
	}
	t.Cleanup(func() { om.OnOrderUpdate = orig })
}

func (w *rejectWatcher) forPrefix(prefix string) []model.OrderData {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []model.OrderData
	for _, o := range w.orders {
		if strings.HasPrefix(o.ClientOID, prefix) {
			out = append(out, o)
		}
	}
	return out
}

// registerSignalRejectCRA 落库 paper 合约 CRA 配置并注册/启动进引擎（与生产
// 策略创建→运行同口径：配置 id 包装、信号按 "sig:<id>:" 打标路由）。
func registerSignalRejectCRA(t *testing.T, ctx *Context, cid, symbol string) {
	t.Helper()
	store.SetStrategyConfig(cid, map[string]any{
		"id": cid, "name": cid, "strategy_type": "cra_contract",
		"symbol": symbol, "status": "running", "execution_mode": "paper",
		"config_json": `{"market_type":"swap","direction":"dual","leverage":100,"first_order_amount":100,"tp_mode":"static","take_profit_ratio":0.5}`,
	})
	t.Cleanup(func() {
		store.DeleteStrategyConfig(cid)
		_ = store.NewStrategyConfigRepo().Delete(cid)
	})

	eng := ctx.StrategyEngine
	wrapped := strategy.WrapStrategy(cid, cra.NewCRAContractStrategy("cra_contract", symbol))
	if err := eng.Register(wrapped); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_ = eng.Stop(cid)
		_ = eng.Unregister(cid)
	})
	if err := eng.Start(cid, map[string]any{
		"symbol": symbol, "direction": "dual", "leverage": 100,
		"first_order_amount": 100, "tp_mode": "static", "take_profit_ratio": 0.5,
	}); err != nil {
		t.Fatalf("engine start: %v", err)
	}
}

// feedSignalBar 经引擎事件总线同步分发一根实时 K 线（dispatch→emitSignal→
// OnSignal→PlaceOrder 全链路；Time 取当前之后，避开暖机分类）。
func feedSignalBar(t *testing.T, ctx *Context, symbol string, close float64, openTime int64) {
	t.Helper()
	ctx.StrategyEngine.Bus().PublishSync(event.Event{
		Type:   event.TypeBar,
		Symbol: symbol,
		Data:   model.Bar{Symbol: symbol, Interval: "1m", Close: close, High: close, Low: close, Time: openTime},
	})
}

// consumeRejectedAndRetry 驱动"拒单消费 + 次根重试"：上一张拒单被策略消费前
// （InPosition 乐观占位），次根 bar 发不出新首单；watcher 计数递增即为消费
// 证据（真实总线送达优先）。引擎单例 bus 若是更早测试 Init 的旧实例则送达
// 不到，1.5s 超时直喂兜底（与 awaitOrFeed 同模式）——两种情形策略都恰好
// 消费一次，回滚断言确定。
func consumeRejectedAndRetry(t *testing.T, ctx *Context, cid, symbol string, watcher *rejectWatcher, prefix string, rej model.OrderData, nextBarOpen int64, wantCount int) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		feedSignalBar(t, ctx, symbol, 110.60, nextBarOpen)
		if len(watcher.forPrefix(prefix)) == wantCount {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s := ctx.StrategyEngine.Get(cid)
	if s == nil {
		t.Fatal("strategy not in engine")
	}
	if _, err := s.OnOrderUpdate(rej, nil); err != nil {
		t.Fatalf("feed reject: %v", err)
	}
	feedSignalBar(t, ctx, symbol, 110.60, nextBarOpen)
	if got := len(watcher.forPrefix(prefix)); got != wantCount {
		t.Fatalf("rejected events = %d, want %d（拒单消费后次根 bar 必须重试）", got, wantCount)
	}
}

// sealRejected 确定性收尾：给真实总线一个送达窗口，然后直喂兜底（回滚幂等，
// 重复消费无副作用——cra 包单测已锁定）。
func sealRejected(t *testing.T, ctx *Context, cid string, rej model.OrderData) {
	t.Helper()
	time.Sleep(300 * time.Millisecond)
	s := ctx.StrategyEngine.Get(cid)
	if s == nil {
		t.Fatal("strategy not in engine")
	}
	if _, err := s.OnOrderUpdate(rej, nil); err != nil {
		t.Fatalf("seal reject: %v", err)
	}
}

// TestSignalOrderRiskRejectRollsBack 事故主链：风控拒单（ord!=nil，OMS 拒绝
// 路径广播）→ 策略完全复位无幽灵持仓；事件恰好一次（OMS 已发，context.go
// 不重复补发）；当根 bar 拦截重发，次根 bar 允许重试。
func TestSignalOrderRiskRejectRollsBack(t *testing.T) {
	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{
		Enabled: true, Balance: 10000, InitialBalance: 10000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 10000, Free: 10000},
		},
	})
	om := order.GetOrderManager()
	watcher := &rejectWatcher{}
	watcher.wrap(t, om)

	// 事故形态：风控净敞口超限恒拒（不改风控判定本身，桩在 wiring 层）。
	origRisk := om.RiskCheck
	om.RiskCheck = func(req *order.Request) error {
		return fmt.Errorf("net exposure 224.4%% exceeds limit 80.0%%")
	}
	t.Cleanup(func() { om.RiskCheck = origRisk })

	const cid = "sigrej-risk"
	const symbol = "SOLUSDT"
	prefix := "sig:" + cid + ":"
	registerSignalRejectCRA(t, ctx, cid, symbol)

	bar1 := time.Now().UnixMilli()
	feedSignalBar(t, ctx, symbol, 110.61, bar1)

	// 风控拒单事件恰好一次（OMS fireOrderUpdate；context.go ord!=nil 不补发）。
	evs := watcher.forPrefix(prefix)
	if len(evs) != 1 || evs[0].Status != model.StatusRejected {
		t.Fatalf("rejected events = %+v, want exactly one REJECTED", evs)
	}
	if evs[0].Side != model.SideBuy || evs[0].Symbol != symbol {
		t.Fatalf("rejected event = %+v, want BUY %s", evs[0], symbol)
	}

	// 同根 bar 重投：不重发首单（无新拒单事件）。
	feedSignalBar(t, ctx, symbol, 110.61, bar1)
	if evs := watcher.forPrefix(prefix); len(evs) != 1 {
		t.Fatalf("same-bar re-entry must be gated, rejected events = %d", len(evs))
	}

	// 次根 bar：拒单消费后允许重试（计数递增=消费证据）→ 风控再拒一次。
	consumeRejectedAndRetry(t, ctx, cid, symbol, watcher, prefix, evs[0], bar1+60_000, 2)
	evs = watcher.forPrefix(prefix)

	// 第二张拒单消费收尾（直喂幂等），终态：无幽灵持仓、不占循环数。
	sealRejected(t, ctx, cid, evs[1])
	st, _ := ctx.StrategyEngine.RuntimeStatus(cid)
	if st["in_position"] != false || st["direction"] != nil || st["avg_entry_price"] != nil {
		t.Fatalf("ghost position after risk reject: %v", st)
	}
	if st["loops_executed"] != 0 {
		t.Fatalf("rejected loop must not consume loop count: %v", st["loops_executed"])
	}
}

// TestSignalOrderPreCreationRejectFeedback 订单创建前失败（paper 停用 →
// PlaceOrder 返回 nil,error，OMS 四条拒绝路径不覆盖）：context.go 错误分支
// 补发同型拒绝事件（不落库），策略按 client_oid 路由回滚。
func TestSignalOrderPreCreationRejectFeedback(t *testing.T) {
	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{Enabled: false})
	om := order.GetOrderManager()
	watcher := &rejectWatcher{}
	watcher.wrap(t, om)

	const cid = "sigrej-pre"
	const symbol = "SOLUSDT"
	prefix := "sig:" + cid + ":"
	registerSignalRejectCRA(t, ctx, cid, symbol)

	feedSignalBar(t, ctx, symbol, 110.61, time.Now().UnixMilli())

	// 补发的拒绝事件恰好一次，字段与 OMS 拒绝形态一致（REJECTED + 原样打标）。
	evs := watcher.forPrefix(prefix)
	if len(evs) != 1 {
		t.Fatalf("synthetic rejected events = %d, want 1", len(evs))
	}
	if evs[0].Status != model.StatusRejected || evs[0].Symbol != symbol || evs[0].Side != model.SideBuy {
		t.Fatalf("synthetic rejected event = %+v", evs[0])
	}
	if om.GetOrder(evs[0].ID) != nil {
		t.Fatalf("pre-creation failure must not persist an order: %s", evs[0].ID)
	}

	sealRejected(t, ctx, cid, evs[0])
	st, _ := ctx.StrategyEngine.RuntimeStatus(cid)
	if st["in_position"] != false || st["loops_executed"] != 0 {
		t.Fatalf("ghost position after pre-creation reject: %v", st)
	}
}
