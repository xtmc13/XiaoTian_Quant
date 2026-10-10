package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/market"
)

// ── H4（2026-10-10）场景 B：同 symbol 已有策略占用 K 线供给管时，新策略的
// 直接暖机路径只养指标、不留虚持仓 ──
//
// 复现路径：先有一个 BTCUSDT 15m 策略占住供给管（EnsureSymbol 已在跑），再
// 启动第二个同 symbol+周期策略 → ensureKlineFeed 返回 !fresh → handler 直调
// RecentClosedBars 暖机。修复前直调 OnBar：第一根历史 bar 就 EnterPosition
// （面板立刻显示持仓但账本无订单——虚持仓），暖机窗口内的补仓信号被丢弃还
// 会虚挂 PendingAddCount。修复后走 strategy.Warmupper：bars_collected 增长、
// in_position 保持 false。

// fakeKlineREST 供给 4 根 K 线（3 根已闭合 + 1 根成型中）。
func fakeKlineREST(t *testing.T) *httptest.Server {
	t.Helper()
	now := time.Now().UnixMilli()
	rows := ""
	for i := 4; i >= 1; i-- {
		openTime := now - int64(i)*900000
		if rows != "" {
			rows += ","
		}
		rows += fmt.Sprintf(`[%d, "100.0", "101.0", "99.0", "%.1f", "10.0", %d, 0, 0, 0, 0, 0]`,
			openTime, 100.0-float64(i), openTime+899999)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", rows)
	}))
}

func TestDirectWarmupSharedFeedNoPhantomPosition(t *testing.T) {
	srv := fakeKlineREST(t)
	defer srv.Close()

	// 独立总线的供给管：本测试只验直接暖机路径（dispatch 暖机路由由
	// strategy 包 warmup_test.go 覆盖），避免后台轮询向引擎单例总线灌 bar。
	feeder := market.NewKlineFeeder(event.NewEventBus(64, 1))
	feeder.BaseURL = srv.URL + "/api/v3"
	feeder.Interval = time.Hour // 首轮立即抓取后即可静止
	SetKlineFeeder(feeder)
	defer SetKlineFeeder(nil)

	// 策略 A 已占住 BTCUSDT|15m 供给管（共享引用计数）。
	if !feeder.EnsureSymbol("BTCUSDT", "15m") {
		t.Fatal("pre-existing feed must start fresh")
	}
	defer feeder.ReleaseSymbol("BTCUSDT", "15m")

	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_spot", func() strategy.Strategy {
		return cra.NewCRASpotStrategy("cra_spot", "BTCUSDT")
	})

	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	cfg := buildSpotConfig(40000, 50000, 5)
	body, _ := json.Marshal(map[string]any{
		"name": "暖机B", "strategy_type": "cra_spot", "symbol": "BTCUSDT", "config": cfg,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "warmup-B create")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "warmup-B create JSON")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "warmup-B id")
	t.Cleanup(func() {
		stopStrategyInEngine(id)
		store.DeleteStrategyConfig(id)
	})

	item := store.GetStrategyConfig(id)
	assertTrue(t, item != nil, "warmup-B item")
	assertTrue(t, startStrategyInEngine(id, item) == nil, "warmup-B start")

	// 直接暖机已发生（RecentClosedBars 3 根已闭合 bar），但不许留下虚持仓。
	st, ok := eng.RuntimeStatus(id)
	assertTrue(t, ok, "runtime status available")
	assertTrue(t, st["bars_collected"] == 3, fmt.Sprintf("warmup bars = %v, want 3", st["bars_collected"]))
	assertTrue(t, st["in_position"] == false, fmt.Sprintf("no phantom position after warmup: %v", st["in_position"]))
	assertTrue(t, st["pending_add_count"] == 0, fmt.Sprintf("no phantom pending add: %v", st["pending_add_count"]))
	if _, hasEntry := st["entry_price"]; hasEntry {
		t.Fatalf("phantom entry price anchored by warmup: %v", st["entry_price"])
	}
}
