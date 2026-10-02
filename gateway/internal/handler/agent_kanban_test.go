package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// ── /agent/kanban 端点契约测试（冻结契约，前端按此对接）──

func registerKanbanRoutes(r *gin.Engine, uid int) {
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	g := r.Group("/agent", h)
	g.GET("/kanban", AgentKanbanList)
	g.POST("/kanban", AgentKanbanCreate)
	g.PUT("/kanban/:id", AgentKanbanUpdate)
	g.DELETE("/kanban/:id", AgentKanbanDelete)
}

func doKanbanRequest(t *testing.T, uid int, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	registerKanbanRoutes(r, uid)
	var reader *bytes.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(bs)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func kanbanBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return body
}

func TestAgentKanbanLifecycle(t *testing.T) {

	// 创建（契约：{"success":true,"id":"kb_x"}）
	w := doKanbanRequest(t, 66, http.MethodPost, "/agent/kanban", map[string]any{"title": "整理周报", "description": "汇总本周盈亏"})
	assertEq(t, w.Code, http.StatusOK, "create status")
	body := kanbanBody(t, w)
	if body["success"] != true {
		t.Fatalf("create success = %v", body["success"])
	}
	id, _ := body["id"].(string)
	if len(id) < 4 || id[:3] != "kb_" {
		t.Fatalf("create id = %q, want kb_* 前缀", id)
	}

	// 列表（契约字段逐项校验）
	w = doKanbanRequest(t, 66, http.MethodGet, "/agent/kanban", nil)
	assertEq(t, w.Code, http.StatusOK, "list status")
	body = kanbanBody(t, w)
	if body["success"] != true {
		t.Fatalf("list success = %v", body["success"])
	}
	cards, _ := body["cards"].([]any)
	if len(cards) != 1 {
		t.Fatalf("list cards = %d, want 1", len(cards))
	}
	card, _ := cards[0].(map[string]any)
	if card["id"] != id || card["title"] != "整理周报" || card["description"] != "汇总本周盈亏" ||
		card["column"] != "todo" || card["created_by"] != "user" {
		t.Fatalf("card 字段不符: %+v", card)
	}
	for _, key := range []string{"assignee", "comment", "created_at", "updated_at"} {
		if _, ok := card[key]; !ok {
			t.Errorf("card 缺少契约字段 %s", key)
		}
	}
	if _, leaked := card["user_id"]; leaked {
		t.Error("card 不应泄露 user_id")
	}

	// 更新列 + 备注
	w = doKanbanRequest(t, 66, http.MethodPut, "/agent/kanban/"+id, map[string]any{"column": "doing"})
	assertEq(t, w.Code, http.StatusOK, "update status")
	if kanbanBody(t, w)["success"] != true {
		t.Fatal("update success != true")
	}
	w = doKanbanRequest(t, 66, http.MethodPut, "/agent/kanban/"+id, map[string]any{"column": "done", "comment": "已发出"})
	assertEq(t, w.Code, http.StatusOK, "complete status")
	w = doKanbanRequest(t, 66, http.MethodGet, "/agent/kanban", nil)
	cards, _ = kanbanBody(t, w)["cards"].([]any)
	card, _ = cards[0].(map[string]any)
	if card["column"] != "done" || card["comment"] != "已发出" {
		t.Fatalf("更新后字段不符: %+v", card)
	}

	// 删除
	w = doKanbanRequest(t, 66, http.MethodDelete, "/agent/kanban/"+id, nil)
	assertEq(t, w.Code, http.StatusOK, "delete status")
	w = doKanbanRequest(t, 66, http.MethodGet, "/agent/kanban", nil)
	cards, _ = kanbanBody(t, w)["cards"].([]any)
	if len(cards) != 0 {
		t.Fatalf("删除后 cards = %d, want 0", len(cards))
	}
}

func TestAgentKanbanOwnerIsolation(t *testing.T) {

	w := doKanbanRequest(t, 66, http.MethodPost, "/agent/kanban", map[string]any{"title": "我的卡"})
	id, _ := kanbanBody(t, w)["id"].(string)

	// 他人看不到
	w = doKanbanRequest(t, 77, http.MethodGet, "/agent/kanban", nil)
	cards, _ := kanbanBody(t, w)["cards"].([]any)
	if len(cards) != 0 {
		t.Fatalf("他人列表 got %d, want 0", len(cards))
	}
	// 他人改/删均 404
	w = doKanbanRequest(t, 77, http.MethodPut, "/agent/kanban/"+id, map[string]any{"column": "done"})
	assertEq(t, w.Code, http.StatusNotFound, "他人更新")
	w = doKanbanRequest(t, 77, http.MethodDelete, "/agent/kanban/"+id, nil)
	assertEq(t, w.Code, http.StatusNotFound, "他人删除")
	// 卡主仍能操作
	w = doKanbanRequest(t, 66, http.MethodPut, "/agent/kanban/"+id, map[string]any{"title": "改名"})
	assertEq(t, w.Code, http.StatusOK, "卡主更新")
}

func TestAgentKanbanBadRequests(t *testing.T) {

	// 缺 title
	w := doKanbanRequest(t, 66, http.MethodPost, "/agent/kanban", map[string]any{"description": "无标题"})
	assertEq(t, w.Code, http.StatusBadRequest, "缺 title")
	// 非法列
	w = doKanbanRequest(t, 66, http.MethodPost, "/agent/kanban", map[string]any{"title": "x", "column": "weird"})
	assertEq(t, w.Code, http.StatusBadRequest, "非法列")
	// 不存在 id
	w = doKanbanRequest(t, 66, http.MethodPut, "/agent/kanban/kb_404", map[string]any{"column": "done"})
	assertEq(t, w.Code, http.StatusNotFound, "更新不存在")
	w = doKanbanRequest(t, 66, http.MethodDelete, "/agent/kanban/kb_404", nil)
	assertEq(t, w.Code, http.StatusNotFound, "删除不存在")
}
