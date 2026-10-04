package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/risk"
)

func getDashboard(t *testing.T) map[string]any {
	t.Helper()
	r := setupRouter()
	r.GET("/dash", DashboardSummary)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dash", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "dashboard status code")
	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "dashboard response should parse")
	return body
}

// ai_agents 必须来自真实子系统信号：未初始化的子系统对应卡片缺席，
// 绝不出现修复前的写死文案（"实时监控中"/"所有指标安全" 等）。
func TestDashboardAIAgentsNoFakeData(t *testing.T) {
	appCtx := app.Get()
	oldWS, oldEng, oldRM := appCtx.BinanceWS, appCtx.StrategyEngine, appCtx.RiskManager
	appCtx.BinanceWS, appCtx.StrategyEngine, appCtx.RiskManager = nil, nil, nil
	defer func() { appCtx.BinanceWS, appCtx.StrategyEngine, appCtx.RiskManager = oldWS, oldEng, oldRM }()

	body := getDashboard(t)
	raw, exists := body["ai_agents"]
	if !exists {
		return // 整个字段缺席也符合约定（前端不渲染该卡）
	}
	agents, ok := raw.([]any)
	assertTrue(t, ok, "ai_agents should be an array")
	assertEq(t, len(agents), 0, "no subsystem wired → no agent cards")
}

// 注入真实风控管理器后，风控AI 卡片状态/描述必须来自 StateSnapshot。
func TestDashboardAIAgentsRiskSignal(t *testing.T) {
	appCtx := app.Get()
	oldWS, oldEng, oldRM := appCtx.BinanceWS, appCtx.StrategyEngine, appCtx.RiskManager
	appCtx.BinanceWS, appCtx.StrategyEngine = nil, nil
	appCtx.RiskManager = risk.NewManager(risk.DefaultManagerConfig())
	defer func() { appCtx.BinanceWS, appCtx.StrategyEngine, appCtx.RiskManager = oldWS, oldEng, oldRM }()

	body := getDashboard(t)
	agents, ok := body["ai_agents"].([]any)
	assertTrue(t, ok && len(agents) == 1, "only risk manager wired → exactly 1 card")
	card, ok := agents[0].(map[string]any)
	assertTrue(t, ok, "agent card should be an object")
	assertTrue(t, card["name"] == "风控AI", "risk card name")
	assertTrue(t, card["status"] == "normal", "fresh breaker should be normal")
	assertTrue(t, strings.Contains(fmt.Sprintf("%v", card["detail"]), "CLOSED"), "detail should carry real breaker state")
}
