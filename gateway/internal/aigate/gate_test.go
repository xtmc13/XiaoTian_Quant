package aigate

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Fakes ──

type fakeCaller struct {
	mu       sync.Mutex
	calls    int
	content  string
	err      error
	blockFor time.Duration
}

func (f *fakeCaller) ChatCompletion(req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.blockFor > 0 {
		time.Sleep(f.blockFor)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &ai.CompletionResponse{
		Choices: []ai.Choice{{Message: ai.ChatMessage{Role: ai.RoleAssistant, Content: f.content}}},
	}, nil
}

func (f *fakeCaller) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type persistCapture struct {
	mu      sync.Mutex
	records []*store.AIGateDecisionRecord
}

func (p *persistCapture) add(rec *store.AIGateDecisionRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, rec)
	return nil
}

func (p *persistCapture) all() []*store.AIGateDecisionRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*store.AIGateDecisionRecord(nil), p.records...)
}

func silentGate(chain []ProviderCaller, cap *persistCapture) *Gate {
	seq := 0
	return &Gate{
		Sources: ContextSources{
			RecentBars: func(string, int) []Bar { return nil },
			Now:        func() time.Time { return time.UnixMilli(1700000000000) },
		},
		LoadConfigFn:  func() Config { return DefaultConfig() },
		ProviderChain: func(Config) []ProviderCaller { return chain },
		Persist:       cap.add,
		MarkOutcome:   func(string, string, bool) error { return nil },
		NewID: func() string {
			seq++
			return fmt.Sprintf("aig-test-%d", seq)
		},
		Logger: log.New(io.Discard, "", 0),
	}
}

func enabledCfg() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	return cfg
}

func entryReq() *order.Request {
	return &order.Request{
		Symbol:    "BTCUSDT",
		Side:      model.SideBuy,
		OrderType: model.TypeMarket,
		Price:     50000,
		Quantity:  0.1,
		Exchange:  "paper",
		UserID:    7,
	}
}

const approveJSON = `{"decision":"approve","confidence":0.9,"reasons":["趋势与信号一致","账户风险可控"]}`

// ── 分类矩阵 ──

func TestClassifyEntry(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(r *order.Request)
		wantEntry bool
	}{
		{"spot buy 是入场", func(r *order.Request) {}, true},
		{"spot sell 是出场（减持）", func(r *order.Request) { r.Side = model.SideSell }, false},
		{"close_position 永远出场", func(r *order.Request) { r.ClosePosition = true }, false},
		{"止损限价单不过门", func(r *order.Request) { r.OrderType = model.TypeStopLossLimit }, false},
		{"止盈单不过门", func(r *order.Request) { r.OrderType = model.TypeTakeProfit }, false},
		{"跟踪止损不过门", func(r *order.Request) { r.OrderType = model.TypeTrailingStop }, false},
		{"引擎子单/触发单不过门", func(r *order.Request) { r.AIGateBypass = true }, false},
		{"合约开多是入场", func(r *order.Request) {
			r.MarketType = model.MarketSwap
			r.PositionSide = model.PositionLong
		}, true},
		{"合约开空是入场", func(r *order.Request) {
			r.MarketType = model.MarketSwap
			r.PositionSide = model.PositionShort
			r.Side = model.SideSell
		}, true},
		{"合约平多是出场", func(r *order.Request) {
			r.MarketType = model.MarketSwap
			r.PositionSide = model.PositionLong
			r.Side = model.SideSell
		}, false},
		{"合约平空是出场", func(r *order.Request) {
			r.MarketType = model.MarketSwap
			r.PositionSide = model.PositionShort
			r.Side = model.SideBuy
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := entryReq()
			tc.mutate(req)
			entry, why := ClassifyEntry(req)
			if entry != tc.wantEntry {
				t.Fatalf("ClassifyEntry=%v (reason %q), want %v", entry, why, tc.wantEntry)
			}
			if !tc.wantEntry && why == "" {
				t.Fatal("bypass 必须带原因")
			}
		})
	}
}

// ── 决策语义 ──

func TestEvaluateApprove(t *testing.T) {
	caller := &fakeCaller{content: approveJSON}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || res.Decision != "approve" || res.FailOpen {
		t.Fatalf("approve should allow: %+v", res)
	}
	if res.Provider != "fake" || res.Model != "m1" {
		t.Fatalf("provider/model should be recorded: %+v", res)
	}
	if res.RequestHash == "" || res.ContextJSON == "" {
		t.Fatal("request hash / context json should be populated")
	}
	if len(res.Reasons) != 2 {
		t.Fatalf("reasons mismatch: %+v", res.Reasons)
	}
}

func TestEvaluateReject(t *testing.T) {
	caller := &fakeCaller{content: `{"decision":"reject","confidence":0.95,"reasons":["账户回撤超限","信号与趋势矛盾"]}`}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if res.Allowed || res.Decision != "reject" {
		t.Fatalf("reject should block: %+v", res)
	}
}

func TestEvaluateAbstainDefaultAllow(t *testing.T) {
	caller := &fakeCaller{content: `{"decision":"abstain","confidence":0.5,"reasons":["证据不足"]}`}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || res.Decision != "abstain" {
		t.Fatalf("abstain default should allow: %+v", res)
	}
}

func TestEvaluateAbstainBlockWhenConfigured(t *testing.T) {
	caller := &fakeCaller{content: `{"decision":"abstain","confidence":0.5,"reasons":["证据不足"]}`}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	cfg := enabledCfg()
	cfg.AbstainAction = "block"
	res := g.Evaluate(entryReq(), cfg)
	if res.Allowed || res.Decision != "abstain" {
		t.Fatalf("abstain+block config should block: %+v", res)
	}
}

func TestEvaluateLowConfidenceDowngradesToAbstain(t *testing.T) {
	// approve 但置信度 0.4 < 阈值 0.6 → 降级 abstain → 默认放行
	caller := &fakeCaller{content: `{"decision":"approve","confidence":0.4,"reasons":["勉强可以"]}`}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if res.Decision != "abstain" || !res.Allowed {
		t.Fatalf("low-confidence approve should downgrade to abstain: %+v", res)
	}
	// reject 但置信度不足 → 降级 abstain → 默认放行（不拦）
	caller2 := &fakeCaller{content: `{"decision":"reject","confidence":0.4,"reasons":["有点可疑"]}`}
	g2 := silentGate([]ProviderCaller{{Name: "fake2", Model: "m1", Caller: caller2}}, &persistCapture{})
	res2 := g2.Evaluate(entryReq(), enabledCfg())
	if res2.Decision != "abstain" || !res2.Allowed {
		t.Fatalf("low-confidence reject should NOT block by default: %+v", res2)
	}
}

func TestEvaluateParseFailureFailOpen(t *testing.T) {
	caller := &fakeCaller{content: "I think you should buy because the market looks good"}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || !res.FailOpen || res.Decision != "fail_open" {
		t.Fatalf("parse failure must fail-open: %+v", res)
	}
	if !strings.Contains(res.DegradeReason, "parse") {
		t.Fatalf("degrade reason should mention parse: %q", res.DegradeReason)
	}
}

func TestEvaluateInvalidDecisionFailOpen(t *testing.T) {
	caller := &fakeCaller{content: `{"decision":"maybe","confidence":0.9,"reasons":[]}`}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || !res.FailOpen {
		t.Fatalf("invalid decision value must fail-open: %+v", res)
	}
}

func TestEvaluateProviderErrorFailOpen(t *testing.T) {
	caller := &fakeCaller{err: errors.New("HTTP 500 — internal error")}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || !res.FailOpen {
		t.Fatalf("provider error must fail-open: %+v", res)
	}
	if !strings.Contains(res.DegradeReason, "provider_unavailable") {
		t.Fatalf("degrade reason mismatch: %q", res.DegradeReason)
	}
}

func TestEvaluateTimeoutFailOpen(t *testing.T) {
	caller := &fakeCaller{blockFor: 3 * time.Second, content: approveJSON}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	cfg := enabledCfg()
	cfg.TimeoutSeconds = 1
	start := time.Now()
	res := g.Evaluate(entryReq(), cfg)
	if !res.Allowed || !res.FailOpen {
		t.Fatalf("timeout must fail-open: %+v", res)
	}
	if time.Since(start) > 2500*time.Millisecond {
		t.Fatalf("timeout should cut the call at ~1s, took %v", time.Since(start))
	}
}

func TestEvaluateProviderFailover(t *testing.T) {
	bad := &fakeCaller{err: errors.New("connection refused")}
	good := &fakeCaller{content: approveJSON}
	g := silentGate([]ProviderCaller{
		{Name: "bad", Model: "m1", Caller: bad},
		{Name: "good", Model: "m2", Caller: good},
	}, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || res.Decision != "approve" || res.FailOpen {
		t.Fatalf("failover should succeed on second provider: %+v", res)
	}
	if res.Provider != "good" {
		t.Fatalf("should record the succeeding provider: %+v", res)
	}
	if res.DegradeReason == "" {
		t.Fatal("前置 provider 失败应记录在 degrade_reason（本次不是 fail-open）")
	}
}

func TestEvaluateNoProviderFailOpen(t *testing.T) {
	g := silentGate(nil, &persistCapture{})
	res := g.Evaluate(entryReq(), enabledCfg())
	if !res.Allowed || !res.FailOpen || !strings.Contains(res.DegradeReason, "ai_not_configured") {
		t.Fatalf("no provider must fail-open with ai_not_configured: %+v", res)
	}
}

// ── 出场/绕过绝不调用 LLM ──

func TestEvaluateExitNeverCallsLLM(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(r *order.Request)
	}{
		{"spot sell", func(r *order.Request) { r.Side = model.SideSell }},
		{"close position", func(r *order.Request) { r.ClosePosition = true }},
		{"stop loss", func(r *order.Request) { r.OrderType = model.TypeStopLossLimit }},
		{"reduce long", func(r *order.Request) {
			r.MarketType = model.MarketSwap
			r.PositionSide = model.PositionLong
			r.Side = model.SideSell
		}},
		{"engine child", func(r *order.Request) { r.AIGateBypass = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caller := &fakeCaller{content: `{"decision":"reject","confidence":0.99,"reasons":["绝不放行"]}`}
			g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
			req := entryReq()
			tc.mutate(req)
			res := g.Evaluate(req, enabledCfg())
			if !res.Allowed || res.Decision != "bypassed_exit" {
				t.Fatalf("exit must be bypassed and allowed: %+v", res)
			}
			if caller.callCount() != 0 {
				t.Fatalf("LLM must never be called for exits, got %d calls", caller.callCount())
			}
		})
	}
}

func TestEvaluatePaperOnlySkipsLive(t *testing.T) {
	caller := &fakeCaller{content: approveJSON}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	cfg := enabledCfg()
	cfg.PaperOnly = true
	req := entryReq()
	req.Exchange = "binance"
	res := g.Evaluate(req, cfg)
	if !res.Allowed || res.Decision != "skipped" {
		t.Fatalf("paper_only should skip live orders: %+v", res)
	}
	if caller.callCount() != 0 {
		t.Fatal("skipped orders must not call LLM")
	}
}

func TestEvaluateExcludedSourceSkips(t *testing.T) {
	caller := &fakeCaller{content: approveJSON}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	req := entryReq()
	req.ClientOID = "dca:42"
	res := g.Evaluate(req, enabledCfg())
	if !res.Allowed || res.Decision != "skipped" {
		t.Fatalf("excluded source should skip: %+v", res)
	}
	if caller.callCount() != 0 {
		t.Fatal("excluded source must not call LLM")
	}
}

// ── CheckOrder（OMS 钩子面） ──

func TestCheckOrderDisabledZeroOverhead(t *testing.T) {
	cap := &persistCapture{}
	g := silentGate(nil, cap)
	g.LoadConfigFn = func() Config { return DefaultConfig() } // enabled=false
	g.ProviderChain = func(Config) []ProviderCaller {
		t.Fatal("provider chain must not be built when disabled")
		return nil
	}
	id, err := g.CheckOrder(entryReq())
	if err != nil || id != "" {
		t.Fatalf("disabled gate must be a no-op, got id=%q err=%v", id, err)
	}
	if len(cap.all()) != 0 {
		t.Fatal("disabled gate must not persist anything")
	}
}

func TestCheckOrderRejectBlocksAndPersists(t *testing.T) {
	cap := &persistCapture{}
	caller := &fakeCaller{content: `{"decision":"reject","confidence":0.95,"reasons":["风险过高"]}`}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, cap)
	cfg := enabledCfg()
	g.LoadConfigFn = func() Config { return cfg }

	id, err := g.CheckOrder(entryReq())
	if err == nil {
		t.Fatal("reject must return error to block the order")
	}
	if id == "" {
		t.Fatal("decision id should be returned even when blocking (for audit)")
	}
	if !strings.Contains(err.Error(), "AI 决策门拦截") {
		t.Fatalf("error should carry gate context: %v", err)
	}
	records := cap.all()
	if len(records) != 1 {
		t.Fatalf("decision must be persisted, got %d records", len(records))
	}
	rec := records[0]
	if rec.Allowed || rec.Decision != "reject" || rec.Symbol != "BTCUSDT" || rec.UserID != 7 {
		t.Fatalf("persisted record mismatch: %+v", rec)
	}
	if rec.RequestHash == "" || rec.ContextJSON == "" {
		t.Fatal("audit fields must be persisted")
	}
}

func TestCheckOrderApprovePersistsTimeline(t *testing.T) {
	cap := &persistCapture{}
	caller := &fakeCaller{content: approveJSON}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, cap)
	cfg := enabledCfg()
	g.LoadConfigFn = func() Config { return cfg }

	id, err := g.CheckOrder(entryReq())
	if err != nil || id == "" {
		t.Fatalf("approve should pass with decision id, got id=%q err=%v", id, err)
	}
	records := cap.all()
	if len(records) != 1 || !records[0].Allowed {
		t.Fatalf("approved decision must be persisted: %+v", records)
	}
}

func TestCheckOrderPersistFailureDoesNotBlock(t *testing.T) {
	caller := &fakeCaller{content: approveJSON}
	g := silentGate([]ProviderCaller{{Name: "fake", Model: "m1", Caller: caller}}, &persistCapture{})
	cfg := enabledCfg()
	g.LoadConfigFn = func() Config { return cfg }
	g.Persist = func(*store.AIGateDecisionRecord) error { return errors.New("db down") }

	if _, err := g.CheckOrder(entryReq()); err != nil {
		t.Fatalf("persist failure must not block trading: %v", err)
	}
}

// ── 契约解析 ──

func TestParseDecisionJSON(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantDec string
		wantErr bool
	}{
		{"plain", `{"decision":"approve","confidence":0.9,"reasons":["a"]}`, "approve", false},
		{"fenced", "```json\n{\"decision\":\"reject\",\"confidence\":0.8,\"reasons\":[]}\n```", "reject", false},
		{"noise around", `Here is my decision: {"decision":"abstain","confidence":0.5,"reasons":["x"]} hope this helps`, "abstain", false},
		{"invalid decision", `{"decision":"hold","confidence":0.9}`, "", true},
		{"not json", "no json at all", "", true},
		{"empty", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := parseDecisionJSON(tc.content)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", dec)
				}
				return
			}
			if err != nil || dec.Decision != tc.wantDec {
				t.Fatalf("got %v/%+v, want %s", err, dec, tc.wantDec)
			}
		})
	}
}

func TestParseDecisionJSONClampsAndCaps(t *testing.T) {
	dec, err := parseDecisionJSON(`{"decision":"APPROVE","confidence":1.7,"reasons":["1","2","3","4","5","6","7","8","9","10"]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if dec.Decision != "approve" || dec.Confidence != 1 {
		t.Fatalf("should normalize case and clamp confidence: %+v", dec)
	}
	if len(dec.Reasons) != 8 {
		t.Fatalf("reasons capped at 8, got %d", len(dec.Reasons))
	}
}

// ── 上下文与摘要 ──

func TestSummarizeBarsInsufficient(t *testing.T) {
	sum := SummarizeBars([]Bar{{Close: 1, High: 1, Low: 1}, {Close: 1, High: 1, Low: 1}}, 0, time.Now())
	if sum.Available {
		t.Fatal("少于 5 根 K 线应标记 unavailable")
	}
}

func TestSummarizeBarsMath(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	var bars []Bar
	for i := 0; i < 30; i++ {
		c := 100 + float64(i)
		bars = append(bars, Bar{
			Time: now.UnixMilli() - int64(30-i)*3600_000,
			Open: c - 0.5, High: c + 1, Low: c - 1, Close: c, Volume: 1000,
		})
	}
	sum := SummarizeBars(bars, 129, now)
	if !sum.Available || sum.Bars != 30 {
		t.Fatalf("summary should be available: %+v", sum)
	}
	if sum.LastClose != 129 {
		t.Fatalf("last close = %v, want 129", sum.LastClose)
	}
	// 单调上涨：MA5 > MA20，RSI 应 = 100（无下跌）
	if sum.MA5 <= sum.MA20 {
		t.Fatalf("uptrend: ma5 %v should exceed ma20 %v", sum.MA5, sum.MA20)
	}
	if sum.RSI14 != 100 {
		t.Fatalf("monotonic up: rsi should be 100, got %v", sum.RSI14)
	}
	if sum.Return1BarPct <= 0 || sum.Return20BarPct <= 0 {
		t.Fatalf("returns should be positive: %+v", sum)
	}
	if sum.ATR14 <= 0 || sum.ATR14Pct <= 0 {
		t.Fatalf("atr should be positive: %+v", sum)
	}
	// 20 根窗口内 high 最大 130、low 最小 109 → position = (129-109)/(130-109)
	if sum.PricePosition20 < 0.9 || sum.PricePosition20 > 1 {
		t.Fatalf("price should sit near window top, got %v", sum.PricePosition20)
	}
	if sum.IsStale {
		t.Fatal("fresh bars should not be stale")
	}
	if sum.RefDeviationPct != 0 {
		t.Fatalf("ref price equals last close: deviation should be 0, got %v", sum.RefDeviationPct)
	}
}

func TestContextDeterministic(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	var bars []Bar
	for i := 0; i < 10; i++ {
		c := 100 + float64(i)
		bars = append(bars, Bar{Time: fixedNow.UnixMilli() - int64(10-i)*3600_000,
			Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 100 + float64(i)})
	}
	sources := ContextSources{
		RecentBars: func(string, int) []Bar { return bars },
		Positions: func() []PositionInfo {
			return []PositionInfo{{Symbol: "ETHUSDT", Side: "BUY", Quantity: 1, EntryPrice: 3000, CurrentPrice: 3100, UnrealizedPnL: 100}}
		},
		Account: func() AccountInfo {
			return AccountInfo{Equity: 10000, Available: 5000, PositionCount: 1, CircuitBreaker: "CLOSED"}
		},
		SourceStats: func(string) SourceStats { return SourceStats{TodayOrders: 3, TodayFilled: 2} },
		Now:         func() time.Time { return fixedNow },
	}
	c1 := sources.Build(entryReq(), "manual", 30)
	c2 := sources.Build(entryReq(), "manual", 30)
	if c1.JSON() != c2.JSON() {
		t.Fatal("context JSON must be deterministic for identical inputs")
	}
	if !strings.Contains(c1.JSON(), "BTCUSDT") || !strings.Contains(c1.JSON(), "account_risk") {
		t.Fatalf("context should contain order + account: %s", c1.JSON())
	}
}

// ── 配置 ──

func TestLoadConfigEnv(t *testing.T) {
	t.Setenv("AI_GATE_ENABLED", "true")
	t.Setenv("AI_GATE_MIN_CONFIDENCE", "0.75")
	t.Setenv("AI_GATE_PAPER_ONLY", "1")
	t.Setenv("AI_GATE_ABSTAIN_ACTION", "block")
	t.Setenv("AI_GATE_TIMEOUT_SECONDS", "99") // 越界 → 归一化到 30
	cfg := LoadConfig()
	if !cfg.Enabled || !cfg.PaperOnly {
		t.Fatalf("env flags not applied: %+v", cfg)
	}
	if cfg.MinConfidence != 0.75 {
		t.Fatalf("min confidence = %v", cfg.MinConfidence)
	}
	if cfg.AbstainAction != "block" {
		t.Fatalf("abstain action = %v", cfg.AbstainAction)
	}
	if cfg.TimeoutSeconds != 30 {
		t.Fatalf("timeout should clamp to 30, got %v", cfg.TimeoutSeconds)
	}
}

func TestDefaultConfigDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("决策门默认必须关闭（opt-in）")
	}
	if cfg.AbstainAction != "allow" {
		t.Fatal("abstain 默认必须放行（fail-open 语义）")
	}
}
