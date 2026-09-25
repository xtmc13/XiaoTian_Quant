package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/aigate"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI 决策门 handler 级测试 ──
// 覆盖：配置 GET/PUT 往返、决策时间线列表/详情/统计、
// OMS 集成（reject 拦截手动下单 / fail-open 放行 / 出场绕过）。

type aiGateFakeCaller struct {
	content string
	err     error
}

func (f *aiGateFakeCaller) ChatCompletion(req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &ai.CompletionResponse{
		Choices: []ai.Choice{{Message: ai.ChatMessage{Role: ai.RoleAssistant, Content: f.content}}},
	}, nil
}

// installTestGate 把全局决策门换成测试门（真实 repo 落库 + 受控 provider 链），
// 返回恢复函数。chain 为 nil 时模拟"无可用 provider"（fail-open 路径）。
func installTestGate(t *testing.T, chain []aigate.ProviderCaller) func() {
	t.Helper()
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	old := aigate.Global()
	cfg := aigate.DefaultConfig()
	cfg.Enabled = true
	repo := store.GetAIGateDecisionRepo()
	aigate.SetGlobal(&aigate.Gate{
		Sources:       aigate.ContextSources{}, // 空数据源：上下文降级但不失败
		LoadConfigFn:  func() aigate.Config { return cfg },
		ProviderChain: func(aigate.Config) []aigate.ProviderCaller { return chain },
		Persist:       repo.Create,
		MarkOutcome:   repo.MarkOutcome,
		Logger:        log.New(io.Discard, "", 0),
	})
	return func() { aigate.SetGlobal(old) }
}

func placePaperOrder(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// TestAIGateConfigRoundTrip 配置读默认值 → PUT 更新 → GET 反映 → 恢复。
func TestAIGateConfigRoundTrip(t *testing.T) {
	oldCfg := store.GetConfig()
	t.Cleanup(func() { _ = store.SaveConfig(oldCfg) })

	r := setupRouter()
	r.GET("/ai/gate/config", AIGateConfigGet)
	r.PUT("/ai/gate/config", AIGateConfigPut)

	// 默认关闭
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ai/gate/config", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "get config")
	var getResp struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if enabled, _ := getResp.Config["enabled"].(bool); enabled {
		t.Fatal("gate must be disabled by default")
	}

	// PUT 开启 + 阈值
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/ai/gate/config",
		strings.NewReader(`{"enabled":true,"min_confidence":0.7,"paper_only":true,"unknown_key":"ignored"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "put config")
	var putResp struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("parse put resp: %v", err)
	}
	if en, _ := putResp.Config["enabled"].(bool); !en {
		t.Fatalf("enabled should be true after PUT: %+v", putResp.Config)
	}
	if mc, _ := putResp.Config["min_confidence"].(float64); mc != 0.7 {
		t.Fatalf("min_confidence should be 0.7: %+v", putResp.Config)
	}
	if _, leaked := putResp.Config["unknown_key"]; leaked {
		t.Fatal("unknown keys must be dropped")
	}

	// GET 反映持久化结果
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/config", nil)
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &getResp)
	if en, _ := getResp.Config["enabled"].(bool); !en {
		t.Fatal("GET after PUT should show enabled=true")
	}
}

// TestAIGateBlocksEntryOrder 集成：reject 决策经 OMS 拦截手动入场单（HTTP 400），
// 且决策记录落库（含上下文 hash 与原因）。
func TestAIGateBlocksEntryOrder(t *testing.T) {
	restore := installTestGate(t, []aigate.ProviderCaller{{
		Name:  "fake",
		Model: "m1",
		Caller: &aiGateFakeCaller{
			content: `{"decision":"reject","confidence":0.95,"reasons":["账户回撤过大"]}`,
		},
	}})
	defer restore()

	r := setupRouter()
	r.POST("/orders", PlaceOrder)

	w := placePaperOrder(t, r, `{"symbol":"GATEBLOCKUSDT","side":"BUY","order_type":"MARKET","price":100,"quantity":1,"exchange":"paper"}`)
	assertEq(t, w.Code, http.StatusBadRequest, "rejected entry must be 400")
	if !strings.Contains(w.Body.String(), "AI 决策门拦截") {
		t.Fatalf("error should mention gate: %s", w.Body.String())
	}

	// 决策时间线已落库
	recs, err := store.GetAIGateDecisionRepo().List(store.AIGateDecisionFilter{Symbol: "GATEBLOCKUSDT"})
	if err != nil || len(recs) != 1 {
		t.Fatalf("decision should be persisted: %v n=%d", err, len(recs))
	}
	rec := recs[0]
	if rec.Decision != "reject" || rec.Allowed || rec.Confidence != 0.95 {
		t.Fatalf("record mismatch: %+v", rec)
	}
	if rec.RequestHash == "" || rec.Provider != "fake" {
		t.Fatalf("audit fields missing: %+v", rec)
	}
}

// TestAIGateFailOpenAllowsOrder 集成：provider 全部不可用 → fail-open 放行，
// 订单成交，记录带 fail_open 标记与 degrade 原因。
func TestAIGateFailOpenAllowsOrder(t *testing.T) {
	restore := installTestGate(t, nil) // 无 provider
	defer restore()

	r := setupRouter()
	r.POST("/orders", PlaceOrder)

	w := placePaperOrder(t, r, `{"symbol":"GATEFOPENUSDT","side":"BUY","order_type":"MARKET","price":100,"quantity":1,"exchange":"paper"}`)
	assertEq(t, w.Code, http.StatusOK, "fail-open must allow the order")

	recs, err := store.GetAIGateDecisionRepo().List(store.AIGateDecisionFilter{Symbol: "GATEFOPENUSDT"})
	if err != nil || len(recs) != 1 {
		t.Fatalf("fail-open decision should be persisted: %v n=%d", err, len(recs))
	}
	rec := recs[0]
	if rec.Decision != "fail_open" || !rec.FailOpen || !rec.Allowed {
		t.Fatalf("fail-open record mismatch: %+v", rec)
	}
	if !strings.Contains(rec.DegradeReason, "ai_not_configured") {
		t.Fatalf("degrade reason mismatch: %q", rec.DegradeReason)
	}
}

// TestAIGateExitOrderBypasses 集成：即便 provider 会 reject 一切，现货卖出
// （出场）也绝不经过 LLM、绝不拦截——AI 不能把仓位锁死。
func TestAIGateExitOrderBypasses(t *testing.T) {
	restore := installTestGate(t, []aigate.ProviderCaller{{
		Name:  "fake",
		Model: "m1",
		Caller: &aiGateFakeCaller{
			content: `{"decision":"reject","confidence":0.99,"reasons":["全部拒绝"]}`,
		},
	}})
	defer restore()

	// 卖出需要 base 持仓（LockBalance 校验）——直接给 paper 账户充值，
	// 验证的是"AI 绝不拦出场"，与余额检查无关。
	portfolio.GetManager().UpdateBalance("default", "GATEEXIT", 10, 10, 0)

	r := setupRouter()
	r.POST("/orders", PlaceOrder)

	w := placePaperOrder(t, r, `{"symbol":"GATEEXITUSDT","side":"SELL","order_type":"MARKET","price":100,"quantity":0.5,"exchange":"paper"}`)
	assertEq(t, w.Code, http.StatusOK, "exit order must never be blocked by AI gate")

	recs, err := store.GetAIGateDecisionRepo().List(store.AIGateDecisionFilter{Symbol: "GATEEXITUSDT"})
	if err != nil || len(recs) != 1 {
		t.Fatalf("bypass should be recorded: %v n=%d", err, len(recs))
	}
	if recs[0].Decision != "bypassed_exit" || !recs[0].Allowed {
		t.Fatalf("exit bypass record mismatch: %+v", recs[0])
	}
}

// TestAIGateDecisionsAPI 列表/详情/统计端点（含分页与过滤）。
func TestAIGateDecisionsAPI(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := store.GetAIGateDecisionRepo()
	seed := []store.AIGateDecisionRecord{
		{ID: "aig-api-1", UserID: 7, Source: "manual", Symbol: "APIUSDT", Side: "BUY", Decision: "approve", Allowed: true, Confidence: 0.9},
		{ID: "aig-api-2", UserID: 7, Source: "signal:macd", Symbol: "APIUSDT", Side: "BUY", Decision: "reject", Allowed: false, Confidence: 0.8},
		{ID: "aig-api-3", UserID: 8, Source: "manual", Symbol: "APIUSDT", Side: "SELL", Decision: "bypassed_exit", Allowed: true},
	}
	for i := range seed {
		if err := repo.Create(&seed[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	r := setupRouter()
	r.GET("/ai/gate/decisions", AIGateDecisionsList)
	r.GET("/ai/gate/decisions/:id", AIGateDecisionGet)
	r.GET("/ai/gate/stats", AIGateStats)

	// 列表（未注入用户 → 全量，按符号过滤）
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ai/gate/decisions?symbol=APIUSDT", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "list")
	var listResp struct {
		Decisions []map[string]any `json:"decisions"`
		Total     int              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if listResp.Total != 3 || len(listResp.Decisions) != 3 {
		t.Fatalf("expected 3 decisions, got total=%d len=%d", listResp.Total, len(listResp.Decisions))
	}

	// decision 过滤
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/decisions?symbol=APIUSDT&decision=reject", nil)
	r.ServeHTTP(w, req)
	listResp.Decisions = nil
	listResp.Total = 0
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp.Total != 1 {
		t.Fatalf("decision=reject filter should match 1, got %d", listResp.Total)
	}

	// 分页
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/decisions?symbol=APIUSDT&page=2&page_size=2", nil)
	r.ServeHTTP(w, req)
	listResp.Decisions = nil
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if len(listResp.Decisions) != 1 {
		t.Fatalf("page 2 of size 2 over 3 rows should have 1 row, got %d", len(listResp.Decisions))
	}

	// 详情
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/decisions/aig-api-1", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "get detail")
	if !strings.Contains(w.Body.String(), "aig-api-1") {
		t.Fatalf("detail should contain id: %s", w.Body.String())
	}

	// 详情 404
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/decisions/nope", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "missing id must 404")

	// 统计
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/stats", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "stats")
	var statsResp struct {
		Stats struct {
			Total   int `json:"total"`
			Blocked int `json:"blocked"`
		} `json:"stats"`
		BlockRate float64 `json:"block_rate"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("parse stats: %v", err)
	}
	if statsResp.Stats.Total < 3 || statsResp.Stats.Blocked < 1 {
		t.Fatalf("stats should include seeded rows: %+v", statsResp)
	}

	// 注入普通用户（user_id=8，非 admin）→ 只看本人 + 无属主记录
	r2 := setupRouter()
	r2.Use(func(c *gin.Context) {
		c.Set("user_id", 8)
		c.Set("role", "user")
		c.Next()
	})
	r2.GET("/ai/gate/decisions", AIGateDecisionsList)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ai/gate/decisions?symbol=APIUSDT", nil)
	r2.ServeHTTP(w, req)
	listResp.Decisions = nil
	listResp.Total = 0
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp.Total != 1 {
		t.Fatalf("user 8 should only see own 1 decision, got %d", listResp.Total)
	}
}
