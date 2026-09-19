package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// ── A1.4 马丁配置层硬限字段测试 ─────────────────────────────────

// setupMartinRouter 注册带 AuthRequired 的 martin 配置路由。
func setupMartinRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/strategies")
	g.Use(middleware.AuthRequired())
	{
		g.GET("/martin", StrategyMartinList)
		g.POST("/martin", StrategyMartinCreate)
		g.PUT("/martin/:id", StrategyMartinUpdate)
	}
	return r
}

func validMartinBody() map[string]any {
	return map[string]any{
		"first_order_amount":     100.0,
		"order_count":            7,
		"add_position_spread":    3.5,
		"add_position_callback":  0.1,
		"take_profit_ratio":      1.3,
		"profit_callback":        0.1,
		"double_first_order":     false,
		"loop_type":              "cycle",
		"loop_count":             100,
		"enable_add_position":    true,
		"flash_crash_protection": 2.0,
		"max_layers":             5,
		"max_total_budget":       3000.0,
		"symbol":                 "BTCUSDT",
		"exchange":               "paper",
	}
}

// 创建/读取往返：max_layers / max_total_budget 落库 config_json 并回读。
func TestMartinConfigCapsRoundTrip(t *testing.T) {
	r := setupMartinRouter(t)
	token := gridToken(t, 91)

	w, created := gridDo(t, r, http.MethodPost, "/api/strategies/martin", validMartinBody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created config should carry id: %v", created)
	}

	// 列表回读：两个硬限字段按提交值返回。
	_, listed := gridDo(t, r, http.MethodGet, "/api/strategies/martin", nil, token)
	strategies, ok := listed["strategies"].([]any)
	if !ok {
		t.Fatalf("list response missing strategies: %v", listed)
	}
	var found map[string]any
	for _, item := range strategies {
		if m, ok := item.(map[string]any); ok && m["id"] == id {
			found = m
		}
	}
	if found == nil {
		t.Fatalf("created config %s not in list", id)
	}
	assertNumEq(t, found["max_layers"], 5, "max_layers round-trip")
	assertNumEq(t, found["max_total_budget"], 3000, "max_total_budget round-trip")

	// 更新：收紧层数/预算 → 200 且新值生效。
	update := validMartinBody()
	update["max_layers"] = 3
	update["max_total_budget"] = 1500.0
	w, _ = gridDo(t, r, http.MethodPut, "/api/strategies/martin/"+id, update, token)
	assertEq(t, w.Code, http.StatusOK, "update status")
	_, listed = gridDo(t, r, http.MethodGet, "/api/strategies/martin", nil, token)
	for _, item := range listed["strategies"].([]any) {
		if m, ok := item.(map[string]any); ok && m["id"] == id {
			assertNumEq(t, m["max_layers"], 3, "updated max_layers")
			assertNumEq(t, m["max_total_budget"], 1500, "updated max_total_budget")
		}
	}
}

// 校验：负层数/负预算被 binding 拒绝（400）。
func TestMartinConfigCapsValidation(t *testing.T) {
	r := setupMartinRouter(t)
	token := gridToken(t, 91)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"negative max_layers", func(b map[string]any) { b["max_layers"] = -1 }},
		{"max_layers over 20", func(b map[string]any) { b["max_layers"] = 21 }},
		{"negative budget", func(b map[string]any) { b["max_total_budget"] = -100.0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validMartinBody()
			tc.mutate(body)
			w, _ := gridDo(t, r, http.MethodPost, "/api/strategies/martin", body, token)
			assertEq(t, w.Code, http.StatusBadRequest, fmt.Sprintf("%s status", tc.name))
		})
	}

	// 未提供（0 值）= 未设置：保持兼容旧前端/旧配置。
	body := validMartinBody()
	delete(body, "max_layers")
	delete(body, "max_total_budget")
	w, created := gridDo(t, r, http.MethodPost, "/api/strategies/martin", body, token)
	assertEq(t, w.Code, http.StatusOK, "caps optional status")
	id, _ := created["id"].(string)
	if id != "" {
		_, listed := gridDo(t, r, http.MethodGet, "/api/strategies/martin", nil, token)
		for _, item := range listed["strategies"].([]any) {
			if m, ok := item.(map[string]any); ok && m["id"] == id {
				assertNumEq(t, m["max_layers"], 0, "default max_layers")
				assertNumEq(t, m["max_total_budget"], 0, "default max_total_budget")
			}
		}
	}
}
