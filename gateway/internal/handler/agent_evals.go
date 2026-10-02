package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ── 能力评测（Evals，对标 hermes research 评测）──
// 内置套件（Go 表驱动、无文件依赖）+ 异步运行器：每个用例一次独立 headless 对话
// （工具全开、按用户角色 RBAC 门控），按"期望工具全部使用 且 期望关键词全部命中"
// 判定通过。运行记录存内存（单进程；每用户保留最近 20 条，重启即清空）。
//
// REST 契约（全部需登录；评测使用调用者自己的角色过滤工具集）：
//
//	GET  /api/agent/evals           → {"success":true,"suites":[...],"runs":[...]}
//	POST /api/agent/evals/run       {"suite":"core"} → {"success":true,"run_id":"ev_x"}（409=进行中的运行）
//	GET  /api/agent/evals/runs/{id} → {"success":true,...,"cases":[...]}

// agentEvalCase 单个评测用例：期望工具为空表示纯知识问答。
type agentEvalCase struct {
	Name             string   // 用例名（稳定，前端展示与断言用）
	Prompt           string   // 发给 agent 的用户提问
	ExpectedTools    []string // 必须全部实际调用的工具
	ExpectedKeywords []string // 必须全部出现在最终回答中的关键词（大小写不敏感子串）
}

// agentEvalSuite 内置套件清单项。
type agentEvalSuite struct {
	ID    string
	Name  string
	Cases []agentEvalCase
}

// agentEvalSuites 内置套件表（id/用例名/数量稳定，是外部契约的一部分）。
var agentEvalSuites = []agentEvalSuite{
	{
		ID:   "core",
		Name: "核心能力",
		Cases: []agentEvalCase{
			{Name: "行情查询", Prompt: "帮我查一下 BTC/USDT:USDT 现在的最新价格", ExpectedTools: []string{"get_market_data"}, ExpectedKeywords: []string{"价格"}},
			{Name: "持仓查看", Prompt: "我当前模拟盘有哪些持仓？", ExpectedTools: []string{"get_positions"}, ExpectedKeywords: []string{"持仓"}},
			{Name: "余额查看", Prompt: "查一下我模拟盘的账户余额", ExpectedTools: []string{"get_balance"}, ExpectedKeywords: []string{"余额"}},
			{Name: "策略列表", Prompt: "我现在部署了哪些策略？", ExpectedTools: []string{"list_strategies"}, ExpectedKeywords: []string{"策略"}},
			{Name: "记忆写入", Prompt: "请记住：我偏好低风险的网格策略，不喜欢高杠杆。", ExpectedTools: []string{"save_memory"}, ExpectedKeywords: []string{"记住"}},
			{Name: "看板任务", Prompt: "帮我在看板上加一个任务：复盘本周网格策略表现", ExpectedTools: []string{"kanban_add"}, ExpectedKeywords: []string{"看板"}},
			{Name: "联网搜索", Prompt: "联网搜索一下比特币现货 ETF 的最新消息并简要总结", ExpectedTools: []string{"web_search"}, ExpectedKeywords: []string{"搜索"}},
			{Name: "网格策略知识问答", Prompt: "什么是网格交易策略？它适合什么样的行情？", ExpectedTools: []string{}, ExpectedKeywords: []string{"网格"}},
		},
	},
}

// agentEvalSuiteByID 按 id 查套件（未知返回 nil）。
func agentEvalSuiteByID(id string) *agentEvalSuite {
	for i := range agentEvalSuites {
		if agentEvalSuites[i].ID == id {
			return &agentEvalSuites[i]
		}
	}
	return nil
}

// ── 运行记录（内存，单进程） ──

// AgentEvalCaseResult 单用例结果（REST 契约字段，勿改名）。
type AgentEvalCaseResult struct {
	Name             string   `json:"name"`
	OK               bool     `json:"ok"`
	ExpectedTools    []string `json:"expected_tools"`
	UsedTools        []string `json:"used_tools"`
	ExpectedKeywords []string `json:"expected_keywords"`
	MissingKeywords  []string `json:"missing_keywords"`
	AnswerSummary    string   `json:"answer_summary"` // 最终回答截断（≤120 字）
}

// agentEvalRun 一次套件运行。
type agentEvalRun struct {
	ID         string
	UserID     int64
	Suite      string
	Status     string // running | done
	StartedAt  int64  // unix 秒
	FinishedMs int64  // 整轮耗时（done 后写入）
	Pass       int
	Total      int
	Cases      []AgentEvalCaseResult
}

const (
	agentEvalRunsKeep    = 20  // 每用户保留的最近运行条数（环形截断）
	agentEvalAnswerLimit = 120 // answer_summary 截断长度（rune）
	agentEvalRunIDPrefix = "ev_"
)

var agentEvalStore = struct {
	sync.Mutex
	byUser   map[int64][]*agentEvalRun // 每用户运行记录（新→旧）
	inFlight map[int64]bool            // 单飞：每用户同时仅一个运行
}{byUser: map[int64][]*agentEvalRun{}, inFlight: map[int64]bool{}}

// agentEvalCaseRunner 单用例执行（测试可注入假实现，避免真实 LLM/工具依赖）。
var agentEvalCaseRunner = runAgentEvalCase

// scoreAgentEvalCase 纯评分：期望工具全部使用 且 期望关键词全部命中（大小写不敏感）。
func scoreAgentEvalCase(ec agentEvalCase, answer string, usedTools []string) AgentEvalCaseResult {
	used := map[string]bool{}
	for _, t := range usedTools {
		used[t] = true
	}
	toolsOK := true
	for _, t := range ec.ExpectedTools {
		if !used[t] {
			toolsOK = false
			break
		}
	}
	lowerAnswer := strings.ToLower(answer)
	missing := []string{}
	for _, kw := range ec.ExpectedKeywords {
		if !strings.Contains(lowerAnswer, strings.ToLower(kw)) {
			missing = append(missing, kw)
		}
	}
	if usedTools == nil {
		usedTools = []string{}
	}
	return AgentEvalCaseResult{
		Name:             ec.Name,
		OK:               toolsOK && len(missing) == 0,
		ExpectedTools:    ec.ExpectedTools,
		UsedTools:        usedTools,
		ExpectedKeywords: ec.ExpectedKeywords,
		MissingKeywords:  missing,
		AnswerSummary:    truncateAgentChat(answer, agentEvalAnswerLimit),
	}
}

// runAgentEvalCase 单用例执行：独立 headless 对话（无会话绑定，工具按角色门控后全开），
// 通过 OnToolCall 收集实际使用的工具；执行失败记为不通过（摘要注明错误）。
func runAgentEvalCase(userID int64, ec agentEvalCase) AgentEvalCaseResult {
	var usedTools []string
	seen := map[string]bool{}
	answer, err := RunAgentHeadlessCtx(context.Background(), HeadlessOptions{
		UserID: userID,
		Prompt: ec.Prompt,
		OnToolCall: func(name string) {
			if !seen[name] {
				seen[name] = true
				usedTools = append(usedTools, name)
			}
		},
	})
	if err != nil {
		res := scoreAgentEvalCase(ec, "", usedTools)
		res.AnswerSummary = truncateAgentChat("执行失败："+err.Error(), agentEvalAnswerLimit)
		return res
	}
	return scoreAgentEvalCase(ec, answer, usedTools)
}

// agentEvalCaseInterval 用例间间隔：连续 headless 调用容易触发厂商限流（实测 kimi 429）。
// 测试置 0。
var agentEvalCaseInterval = 3 * time.Second

// executeAgentEvalRun 异步执行整轮套件：逐用例串行，结束后写 done 并解除单飞。
// 用例执行器在运行启动时捕获一次（运行中途不受包级变量切换影响）。
func executeAgentEvalRun(run *agentEvalRun, cases []agentEvalCase) {
	started := time.Now()
	caseRunner := agentEvalCaseRunner
	defer func() {
		agentEvalStore.Lock()
		delete(agentEvalStore.inFlight, run.UserID)
		agentEvalStore.Unlock()
	}()
	for i, ec := range cases {
		if i > 0 && agentEvalCaseInterval > 0 {
			time.Sleep(agentEvalCaseInterval)
		}
		res := caseRunner(run.UserID, ec)
		agentEvalStore.Lock()
		run.Cases = append(run.Cases, res)
		if res.OK {
			run.Pass++
		}
		agentEvalStore.Unlock()
	}
	agentEvalStore.Lock()
	run.Status = "done"
	run.FinishedMs = time.Since(started).Milliseconds()
	agentEvalStore.Unlock()
}

// agentEvalRunSummary 列表态运行摘要（与详情共用标量字段）。
func agentEvalRunSummary(r *agentEvalRun) gin.H {
	return gin.H{
		"id":          r.ID,
		"suite":       r.Suite,
		"status":      r.Status,
		"started_at":  r.StartedAt,
		"finished_ms": r.FinishedMs,
		"pass":        r.Pass,
		"total":       r.Total,
	}
}

// AgentEvalsList GET /api/agent/evals：套件清单 + 当前用户的运行记录（新→旧）。
func AgentEvalsList(c *gin.Context) {
	userID := int64(aiBotUserID(c))
	suites := make([]gin.H, 0, len(agentEvalSuites))
	for _, s := range agentEvalSuites {
		suites = append(suites, gin.H{"id": s.ID, "name": s.Name, "count": len(s.Cases)})
	}
	agentEvalStore.Lock()
	stored := append([]*agentEvalRun{}, agentEvalStore.byUser[userID]...)
	agentEvalStore.Unlock()
	runs := make([]gin.H, 0, len(stored))
	for _, r := range stored {
		runs = append(runs, agentEvalRunSummary(r))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "suites": suites, "runs": runs})
}

// AgentEvalsRun POST /api/agent/evals/run：启动一次套件运行（异步）；
// 同一用户已有进行中的运行时回 409。
func AgentEvalsRun(c *gin.Context) {
	var body struct {
		Suite string `json:"suite"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body: " + err.Error()})
		return
	}
	suite := agentEvalSuiteByID(body.Suite)
	if suite == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "unknown suite: " + body.Suite})
		return
	}
	userID := int64(aiBotUserID(c))

	agentEvalStore.Lock()
	if agentEvalStore.inFlight[userID] {
		agentEvalStore.Unlock()
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": "已有进行中的评测运行，请等待完成后再发起"})
		return
	}
	agentEvalStore.inFlight[userID] = true
	run := &agentEvalRun{
		ID:        fmt.Sprintf("%s%d", agentEvalRunIDPrefix, time.Now().UnixNano()),
		UserID:    userID,
		Suite:     suite.ID,
		Status:    "running",
		StartedAt: time.Now().Unix(),
		Total:     len(suite.Cases),
		Cases:     []AgentEvalCaseResult{},
	}
	// 头部插入（新→旧），环形保留最近 agentEvalRunsKeep 条。
	runs := append([]*agentEvalRun{run}, agentEvalStore.byUser[userID]...)
	if len(runs) > agentEvalRunsKeep {
		runs = runs[:agentEvalRunsKeep]
	}
	agentEvalStore.byUser[userID] = runs
	agentEvalStore.Unlock()

	go executeAgentEvalRun(run, suite.Cases)
	c.JSON(http.StatusOK, gin.H{"success": true, "run_id": run.ID})
}

// AgentEvalsRunGet GET /api/agent/evals/runs/:id：当前用户名下某次运行的详情
// （含逐用例结果；他用户的运行按 404 处理）。
func AgentEvalsRunGet(c *gin.Context) {
	userID := int64(aiBotUserID(c))
	id := c.Param("id")
	agentEvalStore.Lock()
	var found *agentEvalRun
	for _, r := range agentEvalStore.byUser[userID] {
		if r.ID == id {
			found = r
			break
		}
	}
	agentEvalStore.Unlock()
	if found == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "run not found"})
		return
	}
	resp := agentEvalRunSummary(found)
	resp["success"] = true
	resp["cases"] = found.Cases
	c.JSON(http.StatusOK, resp)
}
