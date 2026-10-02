package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/agentsub"
	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// GET /agent/subagents：当前用户的子代理运行记录（最新在前），契约字段齐全；用户隔离。
func TestAgentSubagentRuns(t *testing.T) {
	uid := 920001
	rec := agentsub.StartRun(int64(uid), "分析 BTC 走势")
	agentsub.FinishRun(rec, "多头占优", false)
	rec2 := agentsub.StartRun(int64(uid), "分析 ETH 走势")
	agentsub.FinishRun(rec2, "ERROR: boom", true)
	t.Cleanup(func() {
		// 注册表进程全局：本用例用专属 uid，无需清理；仅防串扰断言见下
	})

	r := setupRouter()
	r.Use(func(c *gin.Context) { c.Set(middleware.UserIDKey, uid) })
	r.GET("/agent/subagents", AgentSubagentRuns)

	do := func() map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/agent/subagents", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "status code")
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("parse body: %v", err)
		}
		return body
	}

	body := do()
	if body["success"] != true {
		t.Fatalf("body = %v", body)
	}
	runs, _ := body["runs"].([]any)
	assertEq(t, len(runs), 2, "runs len")
	first, _ := runs[0].(map[string]any) // 最新在前
	if first["status"] != "error" || first["task"] != "分析 ETH 走势" {
		t.Fatalf("runs[0] = %v", first)
	}
	for _, k := range []string{"id", "task", "status", "started_at", "finished_ms", "result_summary"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("缺字段 %s: %v", k, first)
		}
	}
	second, _ := runs[1].(map[string]any)
	if second["status"] != "done" || second["result_summary"] != "多头占优" {
		t.Fatalf("runs[1] = %v", second)
	}

	// 用户隔离：其他用户为空数组（非 null）
	r2 := setupRouter()
	r2.Use(func(c *gin.Context) { c.Set(middleware.UserIDKey, 920002) })
	r2.GET("/agent/subagents", AgentSubagentRuns)
	req := httptest.NewRequest(http.MethodGet, "/agent/subagents", nil)
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, req)
	var body2 map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body2)
	runs2, ok := body2["runs"].([]any)
	if !ok || len(runs2) != 0 {
		t.Fatalf("其他用户应为空数组: %s", w.Body.String())
	}
}
