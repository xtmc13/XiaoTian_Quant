package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 能力评测（Evals）测试 ──

// doEvalRequest 发起评测 REST 请求；jwtUID > 0 时模拟 JWT 中间件注入用户。
func doEvalRequest(t *testing.T, method, path, body string, jwtUID int) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	handlers := make([]gin.HandlerFunc, 0, 2)
	if jwtUID > 0 {
		handlers = append(handlers, func(c *gin.Context) {
			c.Set(middleware.UserIDKey, jwtUID)
		})
	}
	switch {
	case method == http.MethodGet && path == "/agent/evals":
		handlers = append(handlers, AgentEvalsList)
		r.GET("/agent/evals", handlers...)
	case method == http.MethodPost && path == "/agent/evals/run":
		handlers = append(handlers, AgentEvalsRun)
		r.POST("/agent/evals/run", handlers...)
	default: // GET /agent/evals/runs/:id
		handlers = append(handlers, AgentEvalsRunGet)
		r.GET("/agent/evals/runs/:id", handlers...)
	}

	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// withEvalCaseRunner 注入假的单用例执行器，测试结束恢复真实实现。
func withEvalCaseRunner(t *testing.T, fn func(userID int64, ec agentEvalCase) AgentEvalCaseResult) {
	t.Helper()
	prev := agentEvalCaseRunner
	agentEvalCaseRunner = fn
	t.Cleanup(func() { agentEvalCaseRunner = prev })
}

// 套件完整性：id/名称/用例数量与用例名稳定（外部契约）；期望工具必须存在于装配后的工具集。
func TestAgentEvals_SuiteIntegrity(t *testing.T) {
	suite := agentEvalSuiteByID("core")
	if suite == nil {
		t.Fatal("core suite missing")
	}
	assertStrEq(t, suite.Name, "核心能力", "suite name")
	assertEq(t, len(suite.Cases), 8, "case count")
	wantNames := []string{"行情查询", "持仓查看", "余额查看", "策略列表", "记忆写入", "看板任务", "联网搜索", "网格策略知识问答"}
	for i, name := range wantNames {
		if suite.Cases[i].Name != name {
			t.Fatalf("case[%d] = %q, want %q", i, suite.Cases[i].Name, name)
		}
		if len(suite.Cases[i].ExpectedKeywords) == 0 {
			t.Fatalf("case %q missing expected keywords", name)
		}
	}
	// 期望工具在核心 + 插件装配后的工具集中真实存在。
	available := map[string]bool{}
	for _, tool := range append(agent.AllTools(), builtin.Manager().Registry().Tools()...) {
		available[tool.Name] = true
	}
	for _, ec := range suite.Cases {
		for _, tool := range ec.ExpectedTools {
			if !available[tool] {
				t.Fatalf("case %q expects unknown tool %q", ec.Name, tool)
			}
		}
	}
}

// 评分逻辑：工具子集判定 + 关键词大小写不敏感匹配 + missing 列表。
func TestAgentEvals_Scoring(t *testing.T) {
	ec := agentEvalCase{
		Name:             "样例",
		ExpectedTools:    []string{"get_klines"},
		ExpectedKeywords: []string{"涨", "BTC"},
	}
	// 全部命中：大小写不敏感。
	res := scoreAgentEvalCase(ec, "BTC 最近在涨", []string{"get_klines", "get_balance"})
	if !res.OK || len(res.MissingKeywords) != 0 {
		t.Fatalf("should pass: %+v", res)
	}
	// 关键词缺失：missing 按期望原样记录。
	res = scoreAgentEvalCase(ec, "最近在涨", []string{"get_klines"})
	if res.OK || len(res.MissingKeywords) != 1 || res.MissingKeywords[0] != "BTC" {
		t.Fatalf("missing keyword: %+v", res)
	}
	// 工具缺失：即使关键词全中也不通过。
	res = scoreAgentEvalCase(ec, "BTC 在涨", nil)
	if res.OK {
		t.Fatalf("missing tool must fail: %+v", res)
	}
	if res.UsedTools == nil {
		t.Fatalf("used_tools must be [] not nil")
	}
	// 纯知识问答（无期望工具）：仅看关键词。
	kw := agentEvalCase{Name: "知识", ExpectedKeywords: []string{"网格"}}
	if res := scoreAgentEvalCase(kw, "网格策略适合震荡行情", nil); !res.OK {
		t.Fatalf("knowledge case should pass: %+v", res)
	}
}

// 运行生命周期 + 单飞 409 + handler 形状。
func TestAgentEvals_RunLifecycle(t *testing.T) {
	release := make(chan struct{})
	withEvalCaseRunner(t, func(userID int64, ec agentEvalCase) AgentEvalCaseResult {
		<-release // 阻塞直到测试放行，保证 running 窗口可观测
		return AgentEvalCaseResult{
			Name:             ec.Name,
			OK:               ec.Name != "联网搜索", // 造一个失败用例验证 pass 计数
			ExpectedTools:    ec.ExpectedTools,
			UsedTools:        ec.ExpectedTools,
			ExpectedKeywords: ec.ExpectedKeywords,
			MissingKeywords:  []string{},
			AnswerSummary:    "摘要",
		}
	})

	// 列表初始形状：suites 契约字段 + 空 runs。
	w := doEvalRequest(t, http.MethodGet, "/agent/evals", "", 701)
	assertEq(t, w.Code, http.StatusOK, "list status")
	var list struct {
		Success bool `json:"success"`
		Suites  []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"suites"`
		Runs []any `json:"runs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if !list.Success || len(list.Suites) != 1 || list.Suites[0].ID != "core" ||
		list.Suites[0].Name != "核心能力" || list.Suites[0].Count != 8 || len(list.Runs) != 0 {
		t.Fatalf("list shape: %+v", list)
	}

	// 发起运行。
	w = doEvalRequest(t, http.MethodPost, "/agent/evals/run", `{"suite":"core"}`, 701)
	assertEq(t, w.Code, http.StatusOK, "run status")
	var started struct {
		Success bool   `json:"success"`
		RunID   string `json:"run_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatalf("parse run: %v", err)
	}
	if !started.Success || !strings.HasPrefix(started.RunID, "ev_") {
		t.Fatalf("run response: %+v", started)
	}

	// 单飞：进行中再次发起 → 409。
	w = doEvalRequest(t, http.MethodPost, "/agent/evals/run", `{"suite":"core"}`, 701)
	assertEq(t, w.Code, http.StatusConflict, "single-flight status")

	// 未知套件 → 400。
	w = doEvalRequest(t, http.MethodPost, "/agent/evals/run", `{"suite":"nope"}`, 702)
	assertEq(t, w.Code, http.StatusBadRequest, "unknown suite status")

	// 进行中详情：status=running，cases 逐步追加。
	w = doEvalRequest(t, http.MethodGet, "/agent/evals/runs/"+started.RunID, "", 701)
	var running struct {
		Status string `json:"status"`
		Total  int    `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &running); err != nil {
		t.Fatalf("parse running: %v", err)
	}
	assertStrEq(t, running.Status, "running", "running status")
	assertEq(t, running.Total, 8, "total")

	// 他用户不可见 → 404。
	w = doEvalRequest(t, http.MethodGet, "/agent/evals/runs/"+started.RunID, "", 702)
	assertEq(t, w.Code, http.StatusNotFound, "foreign run status")

	// 放行并等待完成。
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	var detail struct {
		Success    bool   `json:"success"`
		ID         string `json:"id"`
		Suite      string `json:"suite"`
		Status     string `json:"status"`
		StartedAt  int64  `json:"started_at"`
		FinishedMs int64  `json:"finished_ms"`
		Pass       int    `json:"pass"`
		Total      int    `json:"total"`
		Cases      []struct {
			Name             string   `json:"name"`
			OK               bool     `json:"ok"`
			ExpectedTools    []string `json:"expected_tools"`
			UsedTools        []string `json:"used_tools"`
			ExpectedKeywords []string `json:"expected_keywords"`
			MissingKeywords  []string `json:"missing_keywords"`
			AnswerSummary    string   `json:"answer_summary"`
		} `json:"cases"`
	}
	for {
		w = doEvalRequest(t, http.MethodGet, "/agent/evals/runs/"+started.RunID, "", 701)
		if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
			t.Fatalf("parse detail: %v", err)
		}
		if detail.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run not done in time: %+v", detail)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !detail.Success || detail.ID != started.RunID || detail.Suite != "core" {
		t.Fatalf("detail scalars: %+v", detail)
	}
	assertEq(t, detail.Pass, 7, "pass")
	assertEq(t, detail.Total, 8, "total")
	assertEq(t, len(detail.Cases), 8, "cases len")
	if detail.StartedAt == 0 {
		t.Fatalf("started_at missing")
	}
	c0 := detail.Cases[0]
	if c0.Name != "行情查询" || c0.ExpectedTools[0] != "get_market_data" || c0.AnswerSummary != "摘要" {
		t.Fatalf("case shape: %+v", c0)
	}

	// 完成后可再次发起（单飞已解除）；等它跑完再收尾——goroutine 里逐个用例
	// 读取包级 agentEvalCaseRunner，若清理恢复真实实现时仍有残余用例，会向
	// 后续用例注册的 mock LLM 发出污染请求。
	w = doEvalRequest(t, http.MethodPost, "/agent/evals/run", `{"suite":"core"}`, 701)
	assertEq(t, w.Code, http.StatusOK, "rerun status")
	var rerun struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rerun); err != nil {
		t.Fatalf("parse rerun: %v", err)
	}
	for {
		w = doEvalRequest(t, http.MethodGet, "/agent/evals/runs/"+rerun.RunID, "", 701)
		var st struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
			t.Fatalf("parse rerun status: %v", err)
		}
		if st.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rerun not done in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// OnToolCall 钩子：headless 执行中工具实际派发时回调，runAgentEvalCase 据此评分。
func TestAgentEvals_OnToolCallHookInHeadless(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{equity: 100, pnl: 0}, &chatMockMatcher{})
	m := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{Name: "get_balance", Arguments: `{}`}}
		}
		return "您的账户余额为100USDT。", nil
	})
	registerMockAgentProvider(m)
	withAgentAIProvider(t, "agent-chat-mock")

	ec := agentEvalCase{
		Name:             "余额查看",
		Prompt:           "查一下我模拟盘的账户余额",
		ExpectedTools:    []string{"get_balance"},
		ExpectedKeywords: []string{"余额"},
	}
	res := runAgentEvalCase(703, ec)
	if !res.OK {
		t.Fatalf("case should pass: %+v", res)
	}
	if len(res.UsedTools) != 1 || res.UsedTools[0] != "get_balance" {
		t.Fatalf("used_tools = %v", res.UsedTools)
	}

	// 模型不调工具时：expected_tools 未满足 → 不通过。
	m2 := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "我不知道。", nil
	})
	registerMockAgentProvider(m2)
	res = runAgentEvalCase(703, ec)
	if res.OK || len(res.UsedTools) != 0 {
		t.Fatalf("no-tool case should fail: %+v", res)
	}
}
