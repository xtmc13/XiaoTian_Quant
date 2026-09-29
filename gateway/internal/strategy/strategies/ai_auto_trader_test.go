package strategies

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── ai_auto_trader 测试 ────────────────────────────────────────
//
// mock LLM 走 ai.RegisterProvider + httptest OpenAI 兼容 server（decision_test.go
// 同款模式），按 prompt 内容路由决策/gate/复盘三类响应并捕获 prompt 供断言；
// 行情走注入的假 MarketData，全程零网络。

// aatMockLLM 捕获每次请求的 user prompt 并按内容路由响应。
type aatMockLLM struct {
	mu      sync.Mutex
	prompts []string
	respond func(prompt string) string
}

// registerAATMockLLM 起 httptest server 注册为名为 name 的 provider。
func registerAATMockLLM(t *testing.T, name string, m *aatMockLLM) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ai.CompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var user string
		for _, msg := range req.Messages {
			if msg.Role == ai.RoleUser {
				user = msg.Content
			}
		}
		m.mu.Lock()
		m.prompts = append(m.prompts, user)
		text := m.respond(user)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ai.CompletionResponse{
			ID:    "chatcmpl-aat-mock",
			Model: "mock-model",
			Choices: []ai.Choice{{
				Index:   0,
				Message: ai.ChatMessage{Role: ai.RoleAssistant, Content: text},
			}},
		})
	}))
	ai.RegisterProvider(ai.Provider{
		Name:    name,
		BaseURL: server.URL,
		APIKey:  "sk-mock",
		Model:   "mock-model",
	})
	t.Cleanup(server.Close)
}

func (m *aatMockLLM) capturedPrompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.prompts))
	copy(out, m.prompts)
	return out
}

// aatRouteResponses 按 prompt 内容分发三类响应：复盘/gate/量化决策。
func aatRouteResponses(prompt string) string {
	switch {
	case strings.Contains(prompt, "Review this closed trade"):
		return `{"lesson":"盈利单应及时移动止损保护利润","tag":"risk_management"}`
	case strings.Contains(prompt, "Review the proposed LONG entry"):
		return `{"decision":"approve","size_pct":100,"reason":"风险可接受"}`
	default:
		return `{"signal":"long","confidence":75,"reason":"测试信号：趋势向上放量突破","market_condition":"trending"}`
	}
}

// aatTestSnapshot 注入的固定行情（避免网络）。
var aatTestSnapshot = &ai.MarketSnapshot{
	Symbol:     "BTCUSDT",
	Price:      60000,
	Change24h:  1.5,
	High24h:    61000,
	Low24h:     59000,
	Volume24h:  5_000_000_000,
	Volatility: 3.39,
	Closes:     []float64{59_000, 59_500, 60_000, 60_200, 60_100, 60_300, 60_000, 60_500, 60_800, 61_000, 60_900, 61_100, 60_700, 60_400, 60_600},
}

// setupAATDB 初始化临时 sqlite（经验库测试用）。
func setupAATDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway-aat.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-ai-auto-trader")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	// DefaultAIExperienceRepo 的 sync.Once 只在首个库上 Ensure 过，换库后需显式建表。
	if err := store.EnsureAIExperienceSchema(); err != nil {
		t.Fatalf("ensure ai_experience schema: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// newStartedAAT 构造已启动的策略：注入假行情 + mock provider（参数显式指定）。
func newStartedAAT(t *testing.T, mockName string, overrides map[string]any) *AIAutoTraderStrategy {
	t.Helper()
	s := NewAIAutoTraderStrategy()
	s.marketData = func(_ context.Context, _ string) (*ai.MarketSnapshot, error) {
		out := *aatTestSnapshot
		return &out, nil
	}
	params := map[string]any{
		"symbol":   "BTCUSDT",
		"provider": mockName,
	}
	for k, v := range overrides {
		params[k] = v
	}
	if err := s.Start(params); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s
}

// waitFor 轮询直到 cond 成立（异步学习链路用），超时即失败。
func waitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waitFor 超时: %s", msg)
}

// ── 1. 决策→阈值过滤：低置信度不出单 ─────────────────────────────

func TestAIAutoTrader_LowConfidenceNoOrder(t *testing.T) {
	m := &aatMockLLM{respond: func(string) string {
		return `{"signal":"long","confidence":40,"reason":"弱势反弹","market_condition":"ranging"}`
	}}
	registerAATMockLLM(t, "aat-mock-lowconf", m)
	s := newStartedAAT(t, "aat-mock-lowconf", nil)

	s.runScanCycle(context.Background())

	s.mu.RLock()
	pending := s.pendingAction
	dec := s.lastDecision
	s.mu.RUnlock()
	if pending != nil {
		t.Fatal("低置信度(40<60)不应产生 pendingAction")
	}
	if dec == nil || dec.Confidence != 40 {
		t.Fatalf("lastDecision 应记录本轮决策, got %+v", dec)
	}
}

// ── 2a. gate veto 不出单 ────────────────────────────────────────

func TestAIAutoTrader_GateVetoNoOrder(t *testing.T) {
	m := &aatMockLLM{respond: func(prompt string) string {
		if strings.Contains(prompt, "Review the proposed LONG entry") {
			return `{"decision":"veto","size_pct":0,"reason":"宏观风险事件前不开新仓"}`
		}
		return aatRouteResponses(prompt)
	}}
	registerAATMockLLM(t, "aat-mock-veto", m)
	s := newStartedAAT(t, "aat-mock-veto", nil)

	s.runScanCycle(context.Background())

	s.mu.RLock()
	pending := s.pendingAction
	gate := s.lastGate
	s.mu.RUnlock()
	if pending != nil {
		t.Fatal("gate veto 不应产生 pendingAction")
	}
	if gate == nil || gate.Decision != "veto" {
		t.Fatalf("lastGate 应记录 veto, got %+v", gate)
	}
}

// ── 2b. gate reduce 50% → stake 减半 ────────────────────────────

func TestAIAutoTrader_GateReduceHalfStake(t *testing.T) {
	m := &aatMockLLM{respond: func(prompt string) string {
		if strings.Contains(prompt, "Review the proposed LONG entry") {
			return `{"decision":"reduce","size_pct":50,"reason":"波动偏高，仓位减半"}`
		}
		return aatRouteResponses(prompt)
	}}
	registerAATMockLLM(t, "aat-mock-reduce", m)
	s := newStartedAAT(t, "aat-mock-reduce", nil)

	s.runScanCycle(context.Background())

	s.mu.RLock()
	pending := s.pendingAction
	s.mu.RUnlock()
	if pending == nil {
		t.Fatal("gate reduce 应产生 pendingAction")
	}
	if pending.Stake != 250 { // max_position_usdt 500 × 50%
		t.Fatalf("pending stake = %.2f, want 250 (500×50%%)", pending.Stake)
	}

	// OnBar 把 pendingAction 发成 LONG 信号，CustomStakeAmount 返回减半后的 stake。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: time.Now().UnixMilli()}, nil)
	if err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("OnBar 应发出 LONG 信号, sig=%v err=%v", sig, err)
	}
	if stake := s.CustomStakeAmount(1000, sig); stake != 250 {
		t.Fatalf("CustomStakeAmount = %.2f, want 250 (gate reduce 50%% 后减半)", stake)
	}
	if stake := s.CustomStakeAmount(100, sig); stake != 100 {
		t.Fatalf("余额不足应退化: CustomStakeAmount = %.2f, want 100", stake)
	}
}

// ── 3. 经验注入：预插经验 → gate prompt 包含经验文本 ─────────────

func TestAIAutoTrader_GatePromptIncludesExperiences(t *testing.T) {
	setupAATDB(t)
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-exp", m)
	s := newStartedAAT(t, "aat-mock-exp", nil)

	// 预插 2 条经验（同 symbol），lesson 用特征文本。
	repo := store.DefaultAIExperienceRepo()
	lessons := []string{"不要在缩量反弹时追高入场", "止损应等放量确认再执行"}
	for i, lesson := range lessons {
		rec := &store.AIExperienceRecord{
			InstanceID: s.instanceIDOrDefault(),
			Symbol:     "BTCUSDT",
			Lesson:     lesson,
			Tag:        "entry_timing",
			Outcome:    "loss",
			PnL:        -10,
			CreatedAt:  time.Now().Unix() - int64(i),
		}
		if err := repo.Insert(rec); err != nil {
			t.Fatalf("insert experience: %v", err)
		}
	}

	s.runScanCycle(context.Background())

	found := map[string]bool{}
	for _, p := range m.capturedPrompts() {
		for _, lesson := range lessons {
			if strings.Contains(p, lesson) {
				found[lesson] = true
			}
		}
	}
	for _, lesson := range lessons {
		if !found[lesson] {
			t.Errorf("gate prompt 未包含经验文本 %q; prompts=%v", lesson, m.capturedPrompts())
		}
	}
}

// ── 4. 平仓→复盘→经验入库 ──────────────────────────────────────

func TestAIAutoTrader_CloseTriggersReviewAndExperience(t *testing.T) {
	setupAATDB(t)
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-review", m)
	s := newStartedAAT(t, "aat-mock-review", nil)
	repo := store.DefaultAIExperienceRepo()
	instanceID := s.instanceIDOrDefault()

	before, err := repo.CountByInstance(instanceID)
	if err != nil {
		t.Fatalf("count before: %v", err)
	}

	// 决策+gate → pending → OnBar 发 LONG → 买单成交入场。
	s.runScanCycle(context.Background())
	if _, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 60000, Time: time.Now().UnixMilli()}, nil); err != nil {
		t.Fatalf("OnBar: %v", err)
	}
	buy := model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		AvgFillPrice: 60000, Filled: 0.008, UpdatedAt: time.Now().UnixMilli(),
	}
	if _, err := s.OnOrderUpdate(buy, nil); err != nil {
		t.Fatalf("OnOrderUpdate buy: %v", err)
	}
	s.mu.RLock()
	inPos := s.inPosition
	entry := s.entryPrice
	s.mu.RUnlock()
	if !inPos || entry != 60000 {
		t.Fatalf("买单成交应建立持仓 entry=60000, inPos=%v entry=%v", inPos, entry)
	}

	// 卖单成交平仓 → 异步复盘 → 经验入库。
	sell := model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		AvgFillPrice: 60600, Filled: 0.008, RealizedPnL: 48, UpdatedAt: time.Now().UnixMilli(),
	}
	if _, err := s.OnOrderUpdate(sell, nil); err != nil {
		t.Fatalf("OnOrderUpdate sell: %v", err)
	}

	waitFor(t, 3*time.Second, "经验入库 Count+1", func() bool {
		n, err := repo.CountByInstance(instanceID)
		return err == nil && n == before+1
	})
	recent, err := repo.ListRecent(instanceID, "BTCUSDT", 1)
	if err != nil || len(recent) != 1 {
		t.Fatalf("ListRecent: %v len=%d", err, len(recent))
	}
	rec := recent[0]
	if rec.Lesson != "盈利单应及时移动止损保护利润" {
		t.Errorf("lesson = %q, want mock 返回的复盘结论", rec.Lesson)
	}
	if !validExperienceTags[rec.Tag] {
		t.Errorf("tag = %q 非法", rec.Tag)
	}
	if rec.Outcome != "win" || rec.PnL != 48 {
		t.Errorf("outcome/pnl = %q/%.2f, want win/48", rec.Outcome, rec.PnL)
	}
}

// ── 5. 自适应阈值：连亏 10 笔后 effective_threshold 上升 ───────

func TestAIAutoTrader_AdaptiveThresholdAfterLossStreak(t *testing.T) {
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-adaptive", m)
	s := newStartedAAT(t, "aat-mock-adaptive", nil)

	// 连亏 10 笔（每笔 -10 USDT）写进滚动窗口。
	s.mu.Lock()
	for i := 0; i < 10; i++ {
		s.recordOutcomeLocked(-10)
	}
	eff := s.effectiveThresholdLocked()
	s.mu.Unlock()

	if eff != 70 { // 基础 60 + 10（胜率 0% < 35%）
		t.Fatalf("effective_threshold = %.0f, want 70 (60+10)", eff)
	}
	st := s.RuntimeStatus()
	if st["effective_threshold"] != 70.0 {
		t.Errorf("RuntimeStatus effective_threshold = %v, want 70", st["effective_threshold"])
	}
	if st["adaptive_threshold"] != 10.0 {
		t.Errorf("RuntimeStatus adaptive_threshold = %v, want 10", st["adaptive_threshold"])
	}
	if st["rolling_losses"] != 10 || st["rolling_wins"] != 0 {
		t.Errorf("rolling wins/losses = %v/%v, want 0/10", st["rolling_wins"], st["rolling_losses"])
	}

	// 低置信度 65：基础阈值 60 能过、自适应 70 不能过 → 不出单。
	m2 := &aatMockLLM{respond: func(string) string {
		return `{"signal":"long","confidence":65,"reason":"弱信号","market_condition":"ranging"}`
	}}
	registerAATMockLLM(t, "aat-mock-adaptive2", m2)
	s.mu.Lock()
	s.provider = "aat-mock-adaptive2"
	s.mu.Unlock()
	s.runScanCycle(context.Background())
	s.mu.RLock()
	pending := s.pendingAction
	s.mu.RUnlock()
	if pending != nil {
		t.Fatal("置信度 65 < 有效阈值 70，连亏后不应出单")
	}
}

// ── 6. 日亏闸：today_pnl 触发后 pending 不再产生 ────────────────

func TestAIAutoTrader_DailyLossLimitHaltsNewEntries(t *testing.T) {
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-dailyloss", m)
	s := newStartedAAT(t, "aat-mock-dailyloss", nil)

	// 模拟当日已实现亏损 500（日亏闸 200）。
	s.mu.Lock()
	s.rollTodayLocked(time.Now())
	s.todayPnL = -500
	s.mu.Unlock()

	s.runScanCycle(context.Background())

	s.mu.RLock()
	pending := s.pendingAction
	halted := s.dailyHalted
	s.mu.RUnlock()
	if pending != nil {
		t.Fatal("日亏闸触发后不应产生 pendingAction")
	}
	if !halted {
		t.Error("dailyHalted 应为 true")
	}
	if st := s.RuntimeStatus(); st["state"] != aatStateHalted {
		t.Errorf("state = %v, want %q", st["state"], aatStateHalted)
	}
}

// ── 7. 持仓三退出路径（止损/止盈/超时）─────────────────────────

// aatEnterPosition 直接通过买单成交建立持仓（entry=60000）。
func aatEnterPosition(t *testing.T, s *AIAutoTraderStrategy) {
	t.Helper()
	buy := model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		AvgFillPrice: 60000, Filled: 0.008, UpdatedAt: time.Now().UnixMilli(),
	}
	if _, err := s.OnOrderUpdate(buy, nil); err != nil {
		t.Fatalf("enter position: %v", err)
	}
}

func TestAIAutoTrader_StopLossExit(t *testing.T) {
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-sl", m)
	s := newStartedAAT(t, "aat-mock-sl", nil) // sl 4%
	aatEnterPosition(t, s)

	// Close 跌破 entry×(1−4%) = 57600 → 止损 CLOSE。
	sig, err := s.OnTick(model.Tick{Symbol: "BTCUSDT", Last: 57590, Timestamp: time.Now().UnixMilli()}, nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("应触发止损 CLOSE, sig=%v err=%v", sig, err)
	}
	if !strings.Contains(sig.Reason, "止损") {
		t.Errorf("reason = %q, want 含「止损」", sig.Reason)
	}
}

func TestAIAutoTrader_TakeProfitExit(t *testing.T) {
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-tp", m)
	s := newStartedAAT(t, "aat-mock-tp", nil) // tp 8%
	aatEnterPosition(t, s)

	// Close 升破 entry×(1+8%) = 64800 → 止盈 CLOSE。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Open: 64000, High: 64900, Low: 63900, Close: 64850, Time: time.Now().UnixMilli()}, nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("应触发止盈 CLOSE, sig=%v err=%v", sig, err)
	}
	if !strings.Contains(sig.Reason, "止盈") {
		t.Errorf("reason = %q, want 含「止盈」", sig.Reason)
	}
}

func TestAIAutoTrader_TimeoutExit(t *testing.T) {
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-timeout", m)
	s := newStartedAAT(t, "aat-mock-timeout", map[string]any{"max_hold_bars": 20})
	aatEnterPosition(t, s)

	// 价格横盘，喂 20 根 15m K 线 → 第 20 根超时 CLOSE。
	var sig *model.Signal
	for i := 0; i < 20; i++ {
		out, err := s.OnBar(model.Bar{
			Symbol: "BTCUSDT", Open: 60000, High: 60100, Low: 59900, Close: 60050,
			Time: time.Now().UnixMilli() + int64(i)*15*60*1000,
		}, nil)
		if err != nil {
			t.Fatalf("OnBar %d: %v", i, err)
		}
		if out != nil {
			sig = out
		}
	}
	if sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("应触发超时 CLOSE, sig=%v", sig)
	}
	if !strings.Contains(sig.Reason, "超时") {
		t.Errorf("reason = %q, want 含「超时」", sig.Reason)
	}
}

// ── 附加：移动止损（浮盈 ≥ tp/2 后峰值利润回撤 30% 离场）──────────

func TestAIAutoTrader_TrailingStopExit(t *testing.T) {
	m := &aatMockLLM{respond: aatRouteResponses}
	registerAATMockLLM(t, "aat-mock-trailing", m)
	s := newStartedAAT(t, "aat-mock-trailing", nil) // tp 8%，tp/2=4% 启动移动止损
	aatEnterPosition(t, s)

	// 涨到 +5%（≥4% 启动移动止损，峰值利润 5%）后回落到 5%×0.7=3.5% → 移动止损离场。
	if _, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Open: 60000, High: 63000, Low: 60000, Close: 63000, Time: time.Now().UnixMilli()}, nil); err != nil {
		t.Fatalf("OnBar up: %v", err)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Open: 63000, High: 63050, Low: 62050, Close: 62090, Time: time.Now().UnixMilli() + 15*60*1000}, nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("应触发移动止损 CLOSE, sig=%v err=%v", sig, err)
	}
	if !strings.Contains(sig.Reason, "移动止损") {
		t.Errorf("reason = %q, want 含「移动止损」", sig.Reason)
	}
}
