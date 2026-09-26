package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── helpers ───────────────────────────────────────────────────

// newCtxWithUser 构造带用户 id 的 gin 上下文（绕过 JWT，直测 handler 逻辑）。
func newCtxWithUser(uid int, method, path string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	if uid > 0 {
		c.Set(middleware.UserIDKey, uid)
	}
	return c, w
}

func insertTestSignal(t *testing.T, userID int64, symbol, signal string, confidence float64, createdAt int64) {
	t.Helper()
	rec := &store.AISignalRecord{
		UserID: userID, Symbol: symbol, Signal: signal, Confidence: confidence,
		Reason: "test", Mode: "fast", Provider: "mock", CreatedAt: createdAt,
	}
	if err := store.DefaultAISignalRepo().Insert(rec); err != nil {
		t.Fatalf("insert signal: %v", err)
	}
}

// ── 模拟器：信号驱动 + 确定性 ───────────────────────────────────

func TestSimulator_SignalDrivenOpenAndReverseClose(t *testing.T) {
	// 固定行情价，保证确定性（不触网）。
	orig := aiBotPriceFn
	aiBotPriceFn = func(symbol string) float64 { return 60000 }
	defer func() { aiBotPriceFn = orig }()

	botID := "aibot-test-sim-1"
	resetPaperState(botID)
	defer resetPaperState(botID)

	inst := map[string]any{
		"id":              botID,
		"user_id":         42,
		"symbol":          "BTCUSDT",
		"initial_balance": 10000.0,
		"config_json":     `{"confidence_threshold":60}`,
	}

	// 无信号 → 不开仓。
	_, _, _, _, _, _, _, totalTrades, err := simulateAIBotStep(inst)
	if err != nil {
		t.Fatalf("step1: %v", err)
	}
	if totalTrades != 0 {
		t.Fatalf("无信号不应有交易, trades=%d", totalTrades)
	}

	// long 信号 → 开多（仍无平仓，trades=0 但持仓应在）。
	insertTestSignal(t, 42, "BTCUSDT", "long", 80, time.Now().Unix())
	_, _, _, _, _, _, _, _, _ = simulateAIBotStep(inst)

	// 反向 short 信号 → 信号平仓，落一条 trade。
	insertTestSignal(t, 42, "BTCUSDT", "short", 85, time.Now().Unix()+2)
	_, _, _, _, _, _, _, totalTrades, err = simulateAIBotStep(inst)
	if err != nil {
		t.Fatalf("step3: %v", err)
	}
	if totalTrades != 1 {
		t.Fatalf("信号平仓应有 1 笔交易, got %d", totalTrades)
	}
	trades := store.GetAIBotTrades(botID, 10)
	if len(trades) != 1 {
		t.Fatalf("应落 1 条 trade, got %d", len(trades))
	}
	if getString(trades[0], "side", "") != "LONG" {
		t.Errorf("trade side = %q, want LONG", getString(trades[0], "side", ""))
	}
	if getString(trades[0], "close_reason", "") != "signal" {
		t.Errorf("close_reason = %q, want signal", getString(trades[0], "close_reason", ""))
	}
}

func TestSimulator_LowConfidenceSignalNoOpen(t *testing.T) {
	orig := aiBotPriceFn
	aiBotPriceFn = func(symbol string) float64 { return 60000 }
	defer func() { aiBotPriceFn = orig }()

	botID := "aibot-test-sim-2"
	resetPaperState(botID)
	defer resetPaperState(botID)

	inst := map[string]any{
		"id":              botID,
		"user_id":         43,
		"symbol":          "ETHUSDT",
		"initial_balance": 10000.0,
		"config_json":     `{"confidence_threshold":60}`,
	}
	insertTestSignal(t, 43, "ETHUSDT", "long", 30, time.Now().Unix()) // 低于阈值
	_, _, _, _, _, _, _, totalTrades, err := simulateAIBotStep(inst)
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if totalTrades != 0 {
		t.Errorf("低置信度不应开仓, trades=%d", totalTrades)
	}
	if n := len(store.GetAIBotTrades(botID, 10)); n != 0 {
		t.Errorf("不应有 trade, got %d", n)
	}
}

func TestSimulator_StaleSignalNoOpen(t *testing.T) {
	orig := aiBotPriceFn
	aiBotPriceFn = func(symbol string) float64 { return 60000 }
	defer func() { aiBotPriceFn = orig }()

	botID := "aibot-test-sim-3"
	resetPaperState(botID)
	defer resetPaperState(botID)

	inst := map[string]any{
		"id":              botID,
		"user_id":         44,
		"symbol":          "SOLUSDT",
		"initial_balance": 10000.0,
		"config_json":     `{"confidence_threshold":60,"signal_ttl_seconds":60}`,
	}
	insertTestSignal(t, 44, "SOLUSDT", "long", 90, time.Now().Unix()-3600) // 过期
	_, _, _, _, _, _, _, totalTrades, err := simulateAIBotStep(inst)
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if totalTrades != 0 {
		t.Errorf("过期信号不应开仓, trades=%d", totalTrades)
	}
}

func TestSimulator_DeterministicSameInput(t *testing.T) {
	orig := aiBotPriceFn
	aiBotPriceFn = func(symbol string) float64 { return 60000 }
	defer func() { aiBotPriceFn = orig }()

	botID := "aibot-test-sim-4"
	resetPaperState(botID)
	defer resetPaperState(botID)

	inst := map[string]any{
		"id":              botID,
		"user_id":         45,
		"symbol":          "BTCUSDT",
		"initial_balance": 10000.0,
		"config_json":     `{"confidence_threshold":60}`,
	}
	insertTestSignal(t, 45, "BTCUSDT", "long", 80, time.Now().Unix())
	for i := 0; i < 5; i++ {
		_, _, _, _, _, _, _, _, _ = simulateAIBotStep(inst)
	}
	// 同信号重复 tick 不重复开仓（position 保持 1 个）。
	state := getOrCreatePaperState(botID, "BTCUSDT", 10000)
	state.mu.Lock()
	n := len(state.positions)
	state.mu.Unlock()
	if n != 1 {
		t.Errorf("同一信号不应叠加仓位, positions=%d", n)
	}
}

// ── /ai-robot/config 端点 ─────────────────────────────────────

func TestAIRobotConfigGetDefault(t *testing.T) {
	c, w := newCtxWithUser(9001, http.MethodGet, "/ai-robot/config", nil)
	AIRobotConfigGet(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 前端 AIRobotPanel 直读顶层字段。
	if resp["provider"] != "deepseek" {
		t.Errorf("顶层 provider = %v", resp["provider"])
	}
	cfg, _ := resp["config"].(map[string]any)
	if cfg == nil {
		t.Fatal("应返回默认配置")
	}
	if cfg["provider"] != "deepseek" {
		t.Errorf("默认 provider = %v", cfg["provider"])
	}
	if getFloat(cfg, "confidence_threshold", 0) != 60 {
		t.Errorf("默认阈值 = %v", cfg["confidence_threshold"])
	}
}

func TestAIRobotConfigSaveAndGetRoundTrip(t *testing.T) {
	body := map[string]any{
		"provider":              "openai",
		"model":                 "gpt-4o",
		"scan_interval_seconds": 120,
		"confidence_threshold":  55.0,
		"enabled":               true,
		"mode":                  "deep",
		"watchlist":             []string{"SOLUSDT"},
	}
	c, w := newCtxWithUser(9002, http.MethodPost, "/ai-robot/config", body)
	AIRobotConfigSave(c)
	if w.Code != http.StatusOK {
		t.Fatalf("save status = %d, body=%s", w.Code, w.Body.String())
	}

	c2, w2 := newCtxWithUser(9002, http.MethodGet, "/ai-robot/config", nil)
	AIRobotConfigGet(c2)
	var resp map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	cfg, _ := resp["config"].(map[string]any)
	if cfg["provider"] != "openai" || cfg["mode"] != "deep" {
		t.Errorf("roundtrip 失败: %v", cfg)
	}
	// watchlist/symbols 对齐
	wl := stringSliceFromAnyLocal(cfg["watchlist"])
	if len(wl) != 1 || wl[0] != "SOLUSDT" {
		t.Errorf("watchlist = %v", cfg["watchlist"])
	}
	if wl2 := stringSliceFromAnyLocal(cfg["symbols"]); len(wl2) != 1 {
		t.Errorf("symbols 未对齐 watchlist: %v", cfg["symbols"])
	}

	// 用户隔离：另一个用户读不到
	c3, w3 := newCtxWithUser(9003, http.MethodGet, "/ai-robot/config", nil)
	AIRobotConfigGet(c3)
	var resp3 map[string]any
	_ = json.Unmarshal(w3.Body.Bytes(), &resp3)
	cfg3, _ := resp3["config"].(map[string]any)
	if cfg3["provider"] != "deepseek" {
		t.Errorf("用户 9003 应读默认配置, got %v", cfg3["provider"])
	}
}

func TestAIRobotConfigUnauthorized(t *testing.T) {
	c, w := newCtxWithUser(0, http.MethodGet, "/ai-robot/config", nil)
	AIRobotConfigGet(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401, got %d", w.Code)
	}
}

// ── /ai/signals 端点 ──────────────────────────────────────────

func TestAISignalsEndpointListsRecords(t *testing.T) {
	uid := int64(9100)
	now := time.Now().Unix()
	insertTestSignal(t, uid, "BTCUSDT", "long", 70, now)
	insertTestSignal(t, uid, "BTCUSDT", "neutral", 40, now+1)
	insertTestSignal(t, uid, "ETHUSDT", "short", 66, now+2)

	c, w := newCtxWithUser(int(uid), http.MethodGet, "/ai/signals?limit=10", nil)
	AISignals(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	sigs, _ := resp["signals"].([]any)
	if len(sigs) != 3 {
		t.Fatalf("应返回 3 条信号, got %d", len(sigs))
	}
	first, _ := sigs[0].(map[string]any)
	if first["signal"] != "short" || first["side"] != "sell" {
		t.Errorf("最新信号应为 short/sell, got %v", first)
	}
	if first["provider"] != "mock" || first["mode"] != "fast" {
		t.Errorf("provider/mode 缺失: %v", first)
	}
	// 前端 new Date() 按毫秒解析。
	ts := getFloat(first, "timestamp", 0)
	if ts < 1_000_000_000_000 {
		t.Errorf("timestamp 应为毫秒, got %v", ts)
	}

	// symbol 过滤
	c2, w2 := newCtxWithUser(int(uid), http.MethodGet, "/ai/signals?symbol=BTCUSDT", nil)
	AISignals(c2)
	var resp2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	sigs2, _ := resp2["signals"].([]any)
	if len(sigs2) != 2 {
		t.Errorf("symbol 过滤应返回 2 条, got %d", len(sigs2))
	}
}

// ── /ai/status 端点 ───────────────────────────────────────────

func TestAIRobotStatusReadsConfigAndSignals(t *testing.T) {
	uid := 9200
	now := time.Now().Unix()
	insertTestSignal(t, int64(uid), "BTCUSDT", "long", 80, now)
	insertTestSignal(t, int64(uid), "BTCUSDT", "neutral", 50, now+1) // 被过滤降级

	c, w := newCtxWithUser(uid, http.MethodGet, "/ai/status", nil)
	AIRobotStatus(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatal("缺 data 字段")
	}
	if getFloat(data, "signals_today", 0) != 2 {
		t.Errorf("signals_today = %v, want 2", data["signals_today"])
	}
	// 平均置信度 (80+50)/2 = 65
	if getFloat(data, "avg_confidence", 0) != 65 {
		t.Errorf("avg_confidence = %v, want 65", data["avg_confidence"])
	}
	// neutral 占比 1/2 = 50
	if getFloat(data, "filter_rate", 0) != 50 {
		t.Errorf("filter_rate = %v, want 50", data["filter_rate"])
	}
	// 默认配置字段
	if getFloat(data, "confidence_threshold", 0) != 60 {
		t.Errorf("confidence_threshold = %v, want 60(默认)", data["confidence_threshold"])
	}
	if getString(data, "model", "") == "" {
		t.Error("model 不应为空")
	}
}
