package app

import (
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── G1：CRA 手动操控 paper 全链路 ──
//
// 端点→引擎→信号→OMS→paper 账户的真实下单链路（与自动信号零通道差异）：
// 一键补仓/自定义减仓/清仓卖出的 paper 市价单必须正确记账（base/quote 余额、
// 持仓净量、成交账本 ":manual:" 打标），成交回报灌回策略后引擎态正确演进
// （手动补仓入档不推阶梯、减仓 FIFO 核销、清仓空仓+entry_paused）。
//
// 初始持仓经真实 OMS paper 买入建立（与策略首单同形态），策略侧走生产重启
// 恢复口径（restored_position_qty/vwap 聚合注入）。

const manualPaperCID = "map-craspot"
const manualPaperSymbol = "XRPUSDT"

// setupManualPaperTest 初始化临时 DB + 完整 app 上下文，paper 账户重置为
// 10000 USDT + 200 XRP 持仓快照（模拟"策略首单已成交"的账户状态；策略首单
// 的 OMS 下单/记账链路已有 paper_restore_close_test 覆盖，本测试聚焦手动
// 操控四件套——若经 OMS 真实下单建种，异步事件总线会把种子单回投给新注册
// 策略造成竞态双记）。
func setupManualPaperTest(t *testing.T) (*Context, float64) {
	t.Helper()
	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{
		Enabled: true, Balance: 10000, InitialBalance: 10000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 10000, Free: 10000},
		},
	})
	price := getLastPrice(manualPaperSymbol)
	if price <= 0 {
		t.Fatal("no price for XRPUSDT")
	}
	pe := paper.GetPaperExchange()
	pe.RestoreAccount(paper.AccountSnapshot{
		Enabled: true, Balance: 10000, InitialBalance: 10000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 10000, Free: 10000},
			"XRP":  {Currency: "XRP", Total: 200, Free: 200},
		},
		Positions: []paper.PositionSnapshot{{Data: model.PositionData{
			ID: manualPaperSymbol + "-spot", Symbol: manualPaperSymbol, Side: "LONG",
			Quantity: 200, AvgEntryPrice: price,
		}, Trades: []model.TradeData{{
			Symbol: manualPaperSymbol, ID: "seed-trade", Price: price,
			Quantity: 200, Side: "BUY", Timestamp: time.Now().UnixMilli(),
		}}}},
	})
	// 种子单直接落成交账本（close_all 的账本兜底平仓按 NetFilledByStrategy
	// 计算净持仓，必须与账户背书一致）。
	nowMs := time.Now().UnixMilli()
	if err := store.NewOrderRepo().Create(&store.OrderRecord{
		ID: "ord-seed-" + manualPaperCID, Symbol: manualPaperSymbol, Side: "BUY",
		OrderType: "MARKET", Quantity: 200, Filled: 200, Status: "FILLED",
		Exchange: "paper", ClientOID: "sig:" + manualPaperCID + ":seed",
		AvgFillPrice: price, CreatedAt: nowMs, UpdatedAt: nowMs,
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	return ctx, price
}

// latestManualOrder 取该策略最近一张 ":manual:" 打标的账本单（OMS 落库事实源）。
func latestManualOrder(t *testing.T) *store.OrderRecord {
	t.Helper()
	recs, err := store.GetOrderRepo().List(map[string]any{"symbol": manualPaperSymbol}, 50)
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	for _, r := range recs { // List 按 updated_at DESC
		if strings.Contains(r.ClientOID, "sig:"+manualPaperCID+":manual:") {
			return r
		}
	}
	t.Fatal("no manual order found in ledger")
	return nil
}

// feedToStrategy 把账本单灌回引擎内策略（事件总线 dispatch → wrapper 前缀路由
// 的确定性等价物）。
func feedToStrategy(t *testing.T, ctx *Context, r *store.OrderRecord) {
	t.Helper()
	s := ctx.StrategyEngine.Get(manualPaperCID)
	if s == nil {
		t.Fatal("strategy not in engine")
	}
	od := model.OrderData{
		ID: r.ID, Symbol: r.Symbol, Side: model.OrderSide(r.Side),
		Status: model.OrderStatus(r.Status), Filled: r.Filled,
		AvgFillPrice: r.AvgFillPrice, ClientOID: r.ClientOID,
	}
	if _, err := s.OnOrderUpdate(od, nil); err != nil {
		t.Fatalf("feed order update: %v", err)
	}
}

// awaitOrFeed 等成交回报经事件总线送达策略（生产路径）；引擎单例的事件总线
// 若是更早测试 Init 的旧实例（本包测试共享全局引擎），成交事件到不了本策略，
// 1.5s 超时后直喂兜底——两种情形下策略都恰好消费一次（deterministic）。
func awaitOrFeed(t *testing.T, ctx *Context, r *store.OrderRecord, done func(st map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if st, ok := ctx.StrategyEngine.RuntimeStatus(manualPaperCID); ok && done(st) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	feedToStrategy(t, ctx, r)
	if st, ok := ctx.StrategyEngine.RuntimeStatus(manualPaperCID); !ok || !done(st) {
		t.Fatalf("order %s not consumed by strategy: %v", r.ID, st)
	}
}

func TestCRAManualActionPaperFullLink(t *testing.T) {
	ctx, seedPrice := setupManualPaperTest(t)
	pe := paper.GetPaperExchange()
	feeRate := pe.FeeRate()
	// 风控对同 symbol 订单有最小间隔（risk "minimum 500ms between orders"）——
	// 手动单与自动单过的是同一条风控管道，步进操作前等够间隔。
	riskPace := func() { time.Sleep(650 * time.Millisecond) }
	seedQty := 200.0

	// 策略配置落库（execution_mode=paper：下单链路强制 paper 防线的配置源）。
	store.SetStrategyConfig(manualPaperCID, map[string]any{
		"id": manualPaperCID, "name": manualPaperCID, "strategy_type": "cra_spot",
		"symbol": manualPaperSymbol, "status": "running", "execution_mode": "paper",
		"config_json": `{"first_order_amount":100,"tp_mode":"static","take_profit_ratio":0.013}`,
	})
	store.PersistStrategyConfigs()
	t.Cleanup(func() {
		store.DeleteStrategyConfig(manualPaperCID)
		_ = store.NewStrategyConfigRepo().Delete(manualPaperCID)
	})

	// ① 初始状态：账户 10000 USDT + 200 XRP 持仓（种子单已落账本）。
	usdtAfterSeed := paperFree(pe, "USDT")
	if got := paperFree(pe, "XRP"); got != seedQty {
		t.Fatalf("XRP after seed = %v, want %v", got, seedQty)
	}

	// ② 策略注册进引擎并按生产口径恢复持仓（聚合 qty/vwap 注入）。
	eng := ctx.StrategyEngine
	wrapped := strategy.WrapStrategy(manualPaperCID, cra.NewCRASpotStrategy("cra_spot", manualPaperSymbol))
	if err := eng.Register(wrapped); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_ = eng.Stop(manualPaperCID)
		_ = eng.Unregister(manualPaperCID)
	})
	if err := eng.Start(manualPaperCID, map[string]any{
		"symbol": manualPaperSymbol, "first_order_amount": 100,
		"tp_mode": "static", "take_profit_ratio": 0.013,
		"restored_position_qty": seedQty, "restored_position_vwap": seedPrice,
	}); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	st, _ := eng.RuntimeStatus(manualPaperCID)
	if st["in_position"] != true || st["position_qty"] != seedQty {
		t.Fatalf("restored state: %v", st)
	}
	// 生产在 Start 后由 K 线供给管喂 100 根暖机 bar；测试手动喂一根（恢复价
	// 附近，不触发止盈/止损），手动补仓折算数量的价格基准即它。
	if _, err := wrapped.OnBar(model.Bar{Symbol: manualPaperSymbol, Close: seedPrice, High: seedPrice, Low: seedPrice, Interval: "15m"}, nil); err != nil {
		t.Fatalf("warmup bar: %v", err)
	}

	// ③ 一键补仓 50 USDT：paper 买入记账正确（XRP 增加、USDT 扣款含费），
	// 引擎成交回报入 Manual 档、不推自动阶梯。
	riskPace()
	addDetail, err := eng.ManualAction(manualPaperCID, map[string]any{"action": "add_position", "amount": 50.0})
	if err != nil {
		t.Fatalf("manual add: %v", err)
	}
	// 折算价以引擎 detail 透出的为准（策略最近 K 线收盘价）。
	addPrice, _ := addDetail["price"].(float64)
	addQty, _ := addDetail["qty"].(float64)
	if addPrice <= 0 || addQty != cra.RoundQty(50/addPrice) {
		t.Fatalf("add detail = %v, want qty=RoundQty(50/price)", addDetail)
	}
	addOrd := latestManualOrder(t)
	if addOrd.Status != "FILLED" || addOrd.Side != "BUY" {
		t.Fatalf("manual add order = %+v, want FILLED BUY", addOrd)
	}
	if got, want := paperFree(pe, "XRP"), seedQty+addOrd.Filled; !approxEq(got, want, 1e-9) {
		t.Fatalf("XRP after manual add = %v, want %v", got, want)
	}
	if got, want := paperFree(pe, "USDT"), usdtAfterSeed-addOrd.AvgFillPrice*addOrd.Filled*(1+feeRate); !approxEq(got, want, 1e-6) {
		t.Fatalf("USDT after manual add = %v, want %v（含费）", got, want)
	}
	awaitOrFeed(t, ctx, addOrd, func(st map[string]any) bool { return st["manual_add_count"] == 1 })
	st, _ = eng.RuntimeStatus(manualPaperCID)
	if st["position_qty"] != seedQty+addOrd.Filled {
		t.Fatalf("position_qty after manual add = %v, want %v", st["position_qty"], seedQty+addOrd.Filled)
	}
	if st["filled_orders"] != 1 || st["manual_add_count"] != 1 {
		t.Fatalf("手动补仓不得推自动阶梯: filled_orders=%v manual_add_count=%v",
			st["filled_orders"], st["manual_add_count"])
	}

	// ④ 自定义减仓 50%：paper 卖出记账正确（XRP 减半、USDT 回款扣费），
	// 引擎 FIFO 核销、持仓续存。
	totalBefore := seedQty + addOrd.Filled
	usdtBeforeReduce := paperFree(pe, "USDT")
	riskPace()
	redDetail, err := eng.ManualAction(manualPaperCID, map[string]any{"action": "reduce_position", "ratio": 0.5})
	if err != nil {
		t.Fatalf("manual reduce: %v", err)
	}
	redQty, _ := redDetail["qty"].(float64)
	if redQty != cra.RoundQty(totalBefore*0.5) {
		t.Fatalf("reduce qty = %v, want %v", redQty, cra.RoundQty(totalBefore*0.5))
	}
	redOrd := latestManualOrder(t)
	if redOrd.Status != "FILLED" || redOrd.Side != "SELL" || !approxEq(redOrd.Filled, redQty, 1e-9) {
		t.Fatalf("manual reduce order = %+v, want FILLED SELL %v", redOrd, redQty)
	}
	if got, want := paperFree(pe, "XRP"), totalBefore-redQty; !approxEq(got, want, 1e-9) {
		t.Fatalf("XRP after reduce = %v, want %v", got, want)
	}
	if got, want := paperFree(pe, "USDT"), usdtBeforeReduce+redOrd.AvgFillPrice*redOrd.Filled*(1-feeRate); !approxEq(got, want, 1e-6) {
		t.Fatalf("USDT after reduce = %v, want %v（扣费）", got, want)
	}
	awaitOrFeed(t, ctx, redOrd, func(st map[string]any) bool {
		q, _ := st["position_qty"].(float64)
		return st["in_position"] == true && approxEq(q, totalBefore-redQty, 1e-9)
	})
	st, _ = eng.RuntimeStatus(manualPaperCID)
	if !approxEq(st["position_qty"].(float64), totalBefore-redQty, 1e-9) || st["in_position"] != true {
		t.Fatalf("position after reduce = %v in_position=%v", st["position_qty"], st["in_position"])
	}

	// ⑤ 清仓卖出：paper 全平（XRP 归零、USDT 回款），引擎空仓 + entry_paused、
	// 策略保持 running。
	usdtBeforeClose := paperFree(pe, "USDT")
	riskPace()
	if _, err := eng.ManualAction(manualPaperCID, map[string]any{"action": "close_all"}); err != nil {
		t.Fatalf("close_all: %v", err)
	}
	closeOrd := latestManualOrder(t)
	remain := totalBefore - redQty
	if closeOrd.Status != "FILLED" || closeOrd.Side != "SELL" || !approxEq(closeOrd.Filled, remain, 1e-6) {
		t.Fatalf("close order = %+v, want FILLED SELL %v", closeOrd, remain)
	}
	if got := paperFree(pe, "XRP"); !approxEq(got, 0, 1e-9) {
		t.Fatalf("XRP after close_all = %v, want 0", got)
	}
	if got, want := paperFree(pe, "USDT"), usdtBeforeClose+closeOrd.AvgFillPrice*closeOrd.Filled*(1-feeRate); !approxEq(got, want, 1e-6) {
		t.Fatalf("USDT after close_all = %v, want %v（扣费）", got, want)
	}
	if got := pe.NetPositionQuantity(manualPaperSymbol); !approxEq(got, 0, 1e-9) {
		t.Fatalf("paper position net qty after close_all = %v, want 0", got)
	}
	awaitOrFeed(t, ctx, closeOrd, func(st map[string]any) bool { return st["in_position"] == false })
	st, _ = eng.RuntimeStatus(manualPaperCID)
	if st["in_position"] != false || st["running"] != true || st["entry_paused"] != true {
		t.Fatalf("after close_all: in_position=%v running=%v entry_paused=%v",
			st["in_position"], st["running"], st["entry_paused"])
	}

	// ⑥ 关闭补仓开关：运行时状态如实翻转（止盈/止损分支不受影响的引擎侧
	// 验证见 cra 包测试）。
	togDetail, err := eng.ManualAction(manualPaperCID, map[string]any{"action": "toggle_add_position", "enabled": false})
	if err != nil || togDetail["add_position_enabled"] != false {
		t.Fatalf("toggle: detail=%v err=%v", togDetail, err)
	}
	if st, _ := eng.RuntimeStatus(manualPaperCID); st["add_position_enabled"] != false {
		t.Fatal("runtime status must flip add_position_enabled")
	}
}

// approxEq 浮点近似（容差取相对与绝对较大者，适配余额量级）。
func approxEq(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	m := b
	if m < 0 {
		m = -m
	}
	if m*tol > tol {
		return d <= m*tol
	}
	return d <= tol
}
