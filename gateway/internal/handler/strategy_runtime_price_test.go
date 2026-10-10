package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/exchange"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── 2026-10-10 运行面板"现价"陈旧根修 ──
// 生产实证：cra_contract SOLUSDT 1h 策略（id 79a0d8fd）04:36 引擎按 K 线
// 收盘 120.1 开首单（正确），运行面板"现价"却显示 108.72——旧链路
// price = BinanceWS.GetPrice(sym) 裸读 WS 缓存：该 symbol 没有新鲜 ticker
// 推送时（未订阅/断连），缓存返回数小时前的冻结残留值。
// 修复：WS 缓存加写入时间戳（GetPriceFresh 超 2min 视为不可用），面板现价
// 取最新鲜来源——WS 新鲜优先，否则回落该策略工作周期 K 线供给的最新收盘
// （kline_feeder → 事件总线 → 引擎 MarketData，运行中策略必有该序列）。

// fakeFreshPricer 是 WS 新鲜价读取（freshPricer）的测试替身。
type fakeFreshPricer struct{ price float64 }

func (f fakeFreshPricer) GetPriceFresh(string, time.Duration) float64 { return f.price }

// newTestMarketData 建独立总线驱动的 MarketData（与生产同一条数据路径：
// 总线 TypeBar 事件 → MarketData 分桶累积），并用给定 K 线喂入。
func newTestMarketData(t *testing.T, bars ...model.Bar) *strategy.MarketData {
	t.Helper()
	bus := event.NewEventBus(1024, 1)
	t.Cleanup(bus.Close)
	md := strategy.NewMarketData(bus)
	t.Cleanup(md.Close)
	for _, b := range bars {
		bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: b.Symbol, Data: b})
	}
	return md
}

// WS 新鲜优先：K 线源有值时仍用 WS 新鲜价。
func TestResolveRuntimePriceWSFreshWins(t *testing.T) {
	md := newTestMarketData(t, model.Bar{Symbol: "SOLUSDT", Interval: "1h", Close: 120.1, Time: 1720000000000})
	price, src := resolveRuntimePrice(fakeFreshPricer{price: 121.5}, md, "1h", "SOLUSDT")
	if price != 121.5 || src != "ws" {
		t.Fatalf("fresh WS must win: got price=%v source=%q, want 121.5/ws", price, src)
	}
}

// WS 无订阅/断连（GetPriceFresh 返回 0）→ 回落工作周期 K 线最新收盘。
func TestResolveRuntimePriceFallsBackToKlineClose(t *testing.T) {
	md := newTestMarketData(t, model.Bar{Symbol: "SOLUSDT", Interval: "1h", Close: 120.1, Time: 1720000000000})
	price, src := resolveRuntimePrice(fakeFreshPricer{price: 0}, md, "1h", "SOLUSDT")
	if price != 120.1 || src != "kline" {
		t.Fatalf("stale WS must fall back to kline close: got price=%v source=%q, want 120.1/kline", price, src)
	}
	// ws 为 nil（BinanceWS 未接线）同样回落。
	price, src = resolveRuntimePrice(nil, md, "1h", "SOLUSDT")
	if price != 120.1 || src != "kline" {
		t.Fatalf("nil WS must fall back to kline close: got price=%v source=%q", price, src)
	}
}

// 两个源都取不到 → price=0、无 source（面板显示"获取中..."，与旧行为一致）。
func TestResolveRuntimePriceNoSource(t *testing.T) {
	md := newTestMarketData(t) // 无任何 K 线
	price, src := resolveRuntimePrice(fakeFreshPricer{price: 0}, md, "1h", "SOLUSDT")
	if price != 0 || src != "" {
		t.Fatalf("no source: got price=%v source=%q, want 0/\"\"", price, src)
	}
	// symbol 为空（配置缺 symbol+coin）直接 0。
	price, src = resolveRuntimePrice(fakeFreshPricer{price: 123}, md, "1h", "")
	if price != 0 || src != "" {
		t.Fatalf("empty symbol: got price=%v source=%q", price, src)
	}
}

// 工作周期优先：多周期供给并存时取策略工作周期的收盘，而非跨周期最新一根。
func TestResolveRuntimePriceWorkingTFPreferred(t *testing.T) {
	md := newTestMarketData(t,
		model.Bar{Symbol: "SOLUSDT", Interval: "1h", Close: 120.1, Time: 1720000000000},
		model.Bar{Symbol: "SOLUSDT", Interval: "5m", Close: 119.5, Time: 1720003600000}, // 更新的 5m 副周期
	)
	// 工作周期 1h → 120.1（5m 是指标副周期供给，不代表工作口径）。
	price, src := resolveRuntimePrice(fakeFreshPricer{price: 0}, md, "1h", "SOLUSDT")
	if price != 120.1 || src != "kline" {
		t.Fatalf("working TF preferred: got price=%v source=%q, want 120.1/kline", price, src)
	}
	// 未声明工作周期 → 任意周期最新收盘（5m 更晚 → 119.5）。
	price, src = resolveRuntimePrice(fakeFreshPricer{price: 0}, md, "", "SOLUSDT")
	if price != 119.5 || src != "kline" {
		t.Fatalf("no TF declared must take newest bar: got price=%v source=%q, want 119.5/kline", price, src)
	}
}

// strategyPrimaryTFOf：CRA 策略声明的 timeframe 透出；nil/未实现接口 → ""。
func TestStrategyPrimaryTFOf(t *testing.T) {
	st := cra.NewCRAContractStrategy("cra_contract", "SOLUSDT")
	if err := st.Start(map[string]any{
		"symbol": "SOLUSDT", "timeframe": "4h", "first_order_amount": 10,
		"tp_mode": "static", "take_profit_ratio": 0.013, "market_type": "swap", "leverage": 10,
	}); err != nil {
		t.Fatalf("start cra: %v", err)
	}
	defer func() { _ = st.Stop() }()
	if tf := strategyPrimaryTFOf(st); tf != "4h" {
		t.Fatalf("cra primary tf: got %q, want 4h", tf)
	}
	if tf := strategyPrimaryTFOf(nil); tf != "" {
		t.Fatalf("nil strategy: got %q, want \"\"", tf)
	}
}

// GetStrategyRuntime 端到端（生产事故形态复刻）：SOLUSDT cra_contract 配置，
// BinanceWS 已接线但从未收到 SOLUSDT 行情（GetPriceFresh=0），引擎
// MarketData 里躺着 kline_feeder 供给的 1h 最新收盘 → 响应 price=收盘价、
// price_source="kline"，而不是旧行为的残留价/0。
func TestGetStrategyRuntimeKlineFallbackHTTP(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	bus := eng.Bus()
	if bus == nil {
		t.Skip("global engine has no event bus in this binary")
	}
	eng.MarketData() // 确保已订阅总线（生产由策略 Start 的 setupExtensions 触发）

	// 替换 appContext 的 BinanceWS 为全新空流（无任何 symbol 的缓存价）。
	appCtx := app.Get()
	oldWS := appCtx.BinanceWS
	appCtx.BinanceWS = exchange.NewBinanceWSStream([]string{"btcusdt"}, nil)
	t.Cleanup(func() { appCtx.BinanceWS = oldWS })

	cfg := map[string]any{"timeframe": "1h", "first_order_amount": 10}
	cj, _ := json.Marshal(cfg)
	item := map[string]any{
		"id": "rt-price-sol", "user_id": int64(41), "name": "rt-price-sol",
		"strategy_type": "cra_contract", "symbol": "SOLUSDT", "status": "running",
		"config_json": string(cj), "market_type": "swap",
	}
	store.SetStrategyConfig("rt-price-sol", item)
	store.PersistStrategyConfigs()
	t.Cleanup(func() {
		store.DeleteStrategyConfig("rt-price-sol")
		_ = store.NewStrategyConfigRepo().Delete("rt-price-sol")
	})

	bus.PublishSync(event.Event{
		Type:   event.TypeBar,
		Symbol: "SOLUSDT",
		Data:   model.Bar{Symbol: "SOLUSDT", Interval: "1h", Close: 120.1, Time: time.Now().UnixMilli()},
	})

	r := setupRouter()
	r.GET("/strategies/configs/:id/runtime", GetStrategyRuntime)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/strategies/configs/rt-price-sol/runtime", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "runtime status")

	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "runtime JSON")
	assertTrue(t, getFloat(resp, "price", 0) == 120.1, "price must be kline close 120.1, got "+getString(resp, "detail", ""))
	assertTrue(t, getString(resp, "price_source", "") == "kline", "price_source must be kline")
}
