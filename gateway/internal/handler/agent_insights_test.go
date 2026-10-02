package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/agentskills"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── /agent/insights 与 /agent/journey 端点测试 ──

func registerInsightsRoutes(r *gin.Engine, uid int) {
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	r.GET("/agent/insights", h, AgentInsights)
	r.GET("/agent/journey", h, AgentJourney)
}

func doInsightsRequest(t *testing.T, uid int, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	registerInsightsRoutes(r, uid)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// seedInsightsUser 给 user 55 造一轮对话用量 + 一条记忆 + 一个技能。
func seedInsightsUser(t *testing.T, uid int64) {
	t.Helper()
	repo := store.DefaultAgentChatRepo()
	rec := &store.AgentConversationRecord{UserID: uid, Title: "报告会话", Model: "kimi:kimi-for-coding"}
	if err := repo.CreateConversation(rec); err != nil {
		t.Fatalf("create conv: %v", err)
	}
	if err := repo.InsertMessage(&store.AgentMessageRecord{
		ConversationID: rec.ID, Role: "assistant", Content: "a",
		PromptTokens: 120, CompletionTokens: 60, LLMMs: 800,
		ToolCalls: `[{"name":"get_klines","status":"done"}]`,
	}); err != nil {
		t.Fatalf("insert msg: %v", err)
	}
	if err := agentmemory.NewRepo().Create(&agentmemory.Memory{
		ID: agentmemory.NewID(), UserID: uid, Kind: "preference", Content: "偏好低杠杆", Origin: "auto",
	}); err != nil {
		t.Fatalf("create memory: %v", err)
	}
	sk := &agentskills.Skill{ID: agentskills.NewID(), UserID: uid, Name: "复盘交易", Description: "每日复盘流程", Body: "步骤"}
	if err := agentskills.NewRepo().Upsert(sk); err != nil {
		t.Fatalf("upsert skill: %v", err)
	}
	if err := agentskills.NewRepo().TouchUsage(sk.ID, uid); err != nil {
		t.Fatalf("touch skill: %v", err)
	}
}

func TestAgentInsightsEndpoint(t *testing.T) {
	seedInsightsUser(t, 55)

	w := doInsightsRequest(t, 55, "/agent/insights?days=30")
	assertEq(t, w.Code, http.StatusOK, "status")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["success"] != true || body["days"].(float64) != 30 {
		t.Fatalf("success/days = %v/%v", body["success"], body["days"])
	}
	if body["active_days"].(float64) != 1 {
		t.Fatalf("active_days = %v, want 1", body["active_days"])
	}
	totals, _ := body["totals"].(map[string]any)
	if totals["prompt_tokens"].(float64) != 120 || totals["completion_tokens"].(float64) != 60 ||
		totals["llm_ms"].(float64) != 800 || totals["rounds"].(float64) != 1 {
		t.Fatalf("totals = %v", totals)
	}
	byDay, _ := body["by_day"].([]any)
	if len(byDay) != 1 {
		t.Fatalf("by_day = %v, want 1 项", byDay)
	}
	day, _ := byDay[0].(map[string]any)
	if day["date"] != time.Now().Format("2006-01-02") || day["rounds"].(float64) != 1 {
		t.Fatalf("by_day[0] = %v", day)
	}
	byModel, _ := body["by_model"].([]any)
	if len(byModel) != 1 || byModel[0].(map[string]any)["model"] != "kimi:kimi-for-coding" {
		t.Fatalf("by_model = %v", byModel)
	}
	topTools, _ := body["top_tools"].([]any)
	if len(topTools) != 1 || topTools[0].(map[string]any)["name"] != "get_klines" ||
		topTools[0].(map[string]any)["count"].(float64) != 1 {
		t.Fatalf("top_tools = %v", topTools)
	}
	topSkills, _ := body["top_skills"].([]any)
	if len(topSkills) != 1 || topSkills[0].(map[string]any)["name"] != "复盘交易" ||
		topSkills[0].(map[string]any)["count"].(float64) != 1 {
		t.Fatalf("top_skills = %v", topSkills)
	}
	if body["memories_added"].(float64) != 1 || body["skills_added"].(float64) != 1 {
		t.Fatalf("added = %v/%v, want 1/1", body["memories_added"], body["skills_added"])
	}

	// 他人视角：全为空
	w = doInsightsRequest(t, 56, "/agent/insights")
	assertEq(t, w.Code, http.StatusOK, "status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["totals"].(map[string]any)["prompt_tokens"].(float64) != 0 {
		t.Fatalf("用户隔离失败: totals = %v", body["totals"])
	}
	if body["by_day"] == nil || body["top_tools"] == nil || body["top_skills"] == nil {
		t.Fatal("空结果数组应序列化为 [] 而非 null")
	}
}

func TestAgentJourneyEndpoint(t *testing.T) {
	seedInsightsUser(t, 57)

	w := doInsightsRequest(t, 57, "/agent/journey?limit=50")
	assertEq(t, w.Code, http.StatusOK, "status")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success = %v", body["success"])
	}
	items, _ := body["items"].([]any)
	// 1 记忆 + 1 技能 + ≥1 会话（共享库中 user 55 仅此一家）
	if len(items) < 3 {
		t.Fatalf("items = %v, want ≥3", items)
	}
	kinds := map[string]bool{}
	var prevTs float64 = 1 << 60
	for _, raw := range items {
		it, _ := raw.(map[string]any)
		kinds[it["kind"].(string)] = true
		ts := it["ts"].(float64)
		if ts > prevTs {
			t.Fatalf("items 未按 ts 倒序: %v", items)
		}
		prevTs = ts
	}
	for _, k := range []string{"memory", "skill", "conversation"} {
		if !kinds[k] {
			t.Fatalf("缺少 kind=%s: %v", k, items)
		}
	}

	// limit=1：只留最新一条
	w = doInsightsRequest(t, 57, "/agent/journey?limit=1")
	assertEq(t, w.Code, http.StatusOK, "status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if items, _ := body["items"].([]any); len(items) != 1 {
		t.Fatalf("limit=1 items = %v", items)
	}
}
