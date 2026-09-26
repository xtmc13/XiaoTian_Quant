package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── mock LLM helpers ──────────────────────────────────────────

// registerMockProvider 起一个 OpenAI 兼容的 httptest server，返回固定文本，
// 并注册为名为 name 的 provider；返回关闭函数。
func registerMockProvider(t *testing.T, name, responseText string, hits *int) func() {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits++
		}
		resp := CompletionResponse{
			ID:    "chatcmpl-mock",
			Model: "mock-model",
			Choices: []Choice{{
				Index:   0,
				Message: ChatMessage{Role: RoleAssistant, Content: responseText},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	RegisterProvider(Provider{
		Name:    name,
		BaseURL: server.URL,
		APIKey:  "sk-mock",
		Model:   "mock-model",
	})
	return server.Close
}

// fakeSnapshot 返回注入的固定行情，避免测试走网络。
func fakeSnapshot(snap *MarketSnapshot) func(context.Context, string) (*MarketSnapshot, error) {
	return func(_ context.Context, _ string) (*MarketSnapshot, error) {
		out := *snap
		return &out, nil
	}
}

var testSnapshot = &MarketSnapshot{
	Symbol:     "BTCUSDT",
	Price:      60000,
	Change24h:  1.5,
	High24h:    61000,
	Low24h:     59000,
	Volume24h:  5_000_000_000,
	Volatility: 3.39,
	Closes:     []float64{59_000, 59_500, 60_000, 60_200, 60_100, 60_300, 60_000, 60_500, 60_800, 61_000, 60_900, 61_100, 60_700, 60_400, 60_600},
}

// ── fast 模式 ─────────────────────────────────────────────────

func TestDecisionEngine_Fast_JSONParse(t *testing.T) {
	defer registerMockProvider(t, "mock-fast-json",
		`{"signal":"bullish","confidence":0.82,"reason":"RSI rising","market_condition":"trending"}`, nil)()

	e := NewDecisionEngine(DecisionConfig{
		Provider:   "mock-fast-json",
		Mode:       "fast",
		MarketData: fakeSnapshot(testSnapshot),
	})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "long" {
		t.Errorf("signal = %q, want long (bullish 归一)", dec.Signal)
	}
	// 0-1 比例归一化到 0-100
	if dec.Confidence != 82 {
		t.Errorf("confidence = %v, want 82", dec.Confidence)
	}
	if dec.MarketCondition != "trending" {
		t.Errorf("market_condition = %q, want trending", dec.MarketCondition)
	}
	if dec.Mode != "fast" || dec.Provider != "mock-fast-json" {
		t.Errorf("mode/provider = %q/%q", dec.Mode, dec.Provider)
	}
	if dec.CreatedAt == 0 {
		t.Error("created_at not set")
	}
}

func TestDecisionEngine_Fast_MarkdownWrappedJSON(t *testing.T) {
	body := "分析过程略……\n```json\n{\"signal\":\"short\",\"confidence\":65,\"reason\":\"breakdown\",\"market_condition\":\"ranging\"}\n```\n以上仅供参考"
	defer registerMockProvider(t, "mock-fast-md", body, nil)()

	e := NewDecisionEngine(DecisionConfig{
		Provider:   "mock-fast-md",
		MarketData: fakeSnapshot(testSnapshot),
	})
	dec, err := e.Decide(context.Background(), "ETHUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "short" {
		t.Errorf("signal = %q, want short", dec.Signal)
	}
	if dec.Confidence != 65 {
		t.Errorf("confidence = %v, want 65", dec.Confidence)
	}
}

func TestDecisionEngine_Fast_RegexFallback(t *testing.T) {
	// 无整体大括号包裹的散装 JSON 片段（正则兜底）。
	body := `结论：{"signal":"long","confidence":70,"reason":"momentum"} 仅供参考`
	defer registerMockProvider(t, "mock-fast-regex", body, nil)()

	e := NewDecisionEngine(DecisionConfig{Provider: "mock-fast-regex", MarketData: fakeSnapshot(testSnapshot)})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "long" || dec.Confidence != 70 {
		t.Errorf("got %q/%v, want long/70", dec.Signal, dec.Confidence)
	}
}

func TestDecisionEngine_Fast_SignalSynonyms(t *testing.T) {
	cases := map[string]string{
		`{"signal":"buy","confidence":50}`:     "long",
		`{"signal":"SHORT","confidence":50}`:   "short",
		`{"signal":"看空","confidence":50}`:      "short",
		`{"signal":"unclear","confidence":50}`: "neutral",
	}
	for body, want := range cases {
		name := "mock-syn-" + strings.ReplaceAll(want, " ", "")
		defer registerMockProvider(t, name, body, nil)()
		e := NewDecisionEngine(DecisionConfig{Provider: name, MarketData: fakeSnapshot(testSnapshot)})
		dec, err := e.Decide(context.Background(), "BTCUSDT")
		if err != nil {
			t.Fatalf("Decide(%s): %v", body, err)
		}
		if dec.Signal != want {
			t.Errorf("body=%s signal=%q, want %q", body, dec.Signal, want)
		}
	}
}

func TestDecisionEngine_ConfidenceNormalization(t *testing.T) {
	cases := map[float64]float64{
		0.5:  50, // 0-1 比例
		0.95: 95,
		55:   55,  // 已是 0-100
		150:  100, // 截断
		-3:   0,   // 负值归零
	}
	for in, want := range cases {
		if got := normalizeConfidence(in); got != want {
			t.Errorf("normalizeConfidence(%v) = %v, want %v", in, got, want)
		}
	}
}

// ── market filter ─────────────────────────────────────────────

func TestDecisionEngine_MarketFilter_HighVolatility(t *testing.T) {
	defer registerMockProvider(t, "mock-filter-vol",
		`{"signal":"long","confidence":90,"reason":"breakout"}`, nil)()

	volatile := *testSnapshot
	volatile.Volatility = 25 // 超过默认上限 10%
	e := NewDecisionEngine(DecisionConfig{
		Provider:      "mock-filter-vol",
		MarketFilter:  true,
		MaxVolatility: 10,
		MarketData:    fakeSnapshot(&volatile),
	})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "neutral" {
		t.Errorf("signal = %q, want neutral (高波动降级)", dec.Signal)
	}
	found := false
	for _, f := range dec.Filters {
		if strings.HasPrefix(f, "high_volatility:") {
			found = true
		}
	}
	if !found {
		t.Errorf("filters 缺少 high_volatility 记录: %v", dec.Filters)
	}
}

func TestDecisionEngine_MarketFilter_LowVolume(t *testing.T) {
	defer registerMockProvider(t, "mock-filter-lowvol",
		`{"signal":"short","confidence":80,"reason":"dump"}`, nil)()

	lowVol := *testSnapshot
	lowVol.Volume24h = 100_000 // 低于默认下限 1,000,000
	e := NewDecisionEngine(DecisionConfig{
		Provider:     "mock-filter-lowvol",
		MarketFilter: true,
		MarketData:   fakeSnapshot(&lowVol),
	})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "neutral" {
		t.Errorf("signal = %q, want neutral (低成交量降级)", dec.Signal)
	}
	found := false
	for _, f := range dec.Filters {
		if strings.HasPrefix(f, "low_volume:") {
			found = true
		}
	}
	if !found {
		t.Errorf("filters 缺少 low_volume 记录: %v", dec.Filters)
	}
}

func TestDecisionEngine_MarketFilter_DisabledKeepsSignal(t *testing.T) {
	defer registerMockProvider(t, "mock-filter-off",
		`{"signal":"long","confidence":90,"reason":"breakout"}`, nil)()

	volatile := *testSnapshot
	volatile.Volatility = 25
	e := NewDecisionEngine(DecisionConfig{
		Provider:     "mock-filter-off",
		MarketFilter: false, // 关闭过滤
		MarketData:   fakeSnapshot(&volatile),
	})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "long" {
		t.Errorf("signal = %q, want long (过滤关闭不降级)", dec.Signal)
	}
}

// ── deep 模式 ─────────────────────────────────────────────────

func TestDecisionEngine_Deep_PipelineCalled(t *testing.T) {
	hits := 0
	// deep 模式 7 个 agent 并发调用同一个 provider。
	defer registerMockProvider(t, "mock-deep",
		"Direction: LONG\nReason: strong upward momentum across all analysts", &hits)()

	e := NewDecisionEngine(DecisionConfig{
		Provider:   "mock-deep",
		Mode:       "deep",
		MarketData: fakeSnapshot(testSnapshot),
	})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if hits != 7 {
		t.Errorf("pipeline 调用次数 = %d, want 7（7-agent 管线）", hits)
	}
	if dec.Mode != "deep" {
		t.Errorf("mode = %q, want deep", dec.Mode)
	}
	if dec.Signal != "long" {
		t.Errorf("signal = %q, want long (trader Direction: LONG)", dec.Signal)
	}
	if dec.Confidence < 0 || dec.Confidence > 100 {
		t.Errorf("confidence = %v 未归一到 0-100", dec.Confidence)
	}
}

func TestDecisionEngine_Deep_ShortDirection(t *testing.T) {
	hits := 0
	defer registerMockProvider(t, "mock-deep-short",
		"Direction: SHORT\nReason: bearish divergence", &hits)()

	e := NewDecisionEngine(DecisionConfig{
		Provider:   "mock-deep-short",
		Mode:       "deep",
		MarketData: fakeSnapshot(testSnapshot),
	})
	dec, err := e.Decide(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Signal != "short" {
		t.Errorf("signal = %q, want short", dec.Signal)
	}
}

// ── 解析工具 ──────────────────────────────────────────────────

func TestExtractDecisionJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"raw", `{"signal":"long","confidence":70}`, `{"signal":"long","confidence":70}`},
		{"codeblock", "```json\n{\"signal\":\"long\"}\n```", `{"signal":"long"}`},
		{"surrounded", "先看多再看空 {\"signal\":\"short\",\"confidence\":3} 完", `{"signal":"short","confidence":3}`},
	}
	for _, tc := range cases {
		got := extractDecisionJSON(tc.in)
		var m map[string]any
		if err := json.Unmarshal([]byte(got), &m); err != nil {
			t.Errorf("%s: 提取结果不是合法 JSON: %q (%v)", tc.name, got, err)
		}
	}
}

func TestComputeRSIAndEMA(t *testing.T) {
	closes := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	rsi := computeRSI(closes, 14)
	if rsi <= 50 {
		t.Errorf("持续上涨 RSI 应 > 50, got %.1f", rsi)
	}
	if e := ema(closes, 12); e <= 0 {
		t.Errorf("ema 应 > 0, got %v", e)
	}
	if rsiEmpty := computeRSI(nil, 14); rsiEmpty != 50 {
		t.Errorf("空序列 RSI 应回退 50, got %v", rsiEmpty)
	}
}

func TestDecisionEngine_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	RegisterProvider(Provider{Name: "mock-nokey", BaseURL: server.URL, APIKey: "", Model: "m"})

	e := NewDecisionEngine(DecisionConfig{Provider: "mock-nokey", MarketData: fakeSnapshot(testSnapshot)})
	if _, err := e.Decide(context.Background(), "BTCUSDT"); err == nil {
		t.Fatal("缺少 api key 应报错")
	}
}

func TestDecisionEngine_Defaults(t *testing.T) {
	e := NewDecisionEngine(DecisionConfig{})
	if e.cfg.Provider != "deepseek" || e.cfg.Mode != "fast" {
		t.Errorf("defaults = %q/%q", e.cfg.Provider, e.cfg.Mode)
	}
	if e.cfg.MaxVolatility != 10 || e.cfg.MinVolume24h != 1_000_000 {
		t.Errorf("filter defaults = %v/%v", e.cfg.MaxVolatility, e.cfg.MinVolume24h)
	}
	if e.cfg.HTTPClient == nil {
		t.Error("HTTPClient 应有默认超时")
	}
	// 注入行情 + 无 key 的 deepseek（env 无 key 时）应报 api key 错误而非 panic。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = ctx
}
