package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/aigate"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// webhook OMS 收口回归：入场单必须走 OMS 全管线（风控 → AI 决策门 → 余额锁 → 下单），
// 被拒返回错误且不落成交单；出场单保持直发但必须留审计。

// freshOMSRateWindow 让 OMS 令牌桶跨过 100ms 窗口重置，避免与同包其他用例互相挤占。
func freshOMSRateWindow() { time.Sleep(110 * time.Millisecond) }

func postWebhook(t *testing.T, route, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	r.POST("/webhook/tv", TradingViewWebhook)
	r.POST("/webhook/generic", GenericWebhook)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", route, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// 默认通路：入场单经 OMS 下单成功（paper 即时成交），且可在展示层查到。
func TestTradingViewWebhookEntryViaOMS(t *testing.T) {
	freshOMSRateWindow()
	w := postWebhook(t, "/webhook/tv", `{"symbol":"WBTVUSDT","action":"buy","quantity":0.05,"price":50000}`)
	assertEq(t, w.Code, http.StatusOK, "entry via OMS must succeed")

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse resp: %v", err)
	}
	ordMap, _ := resp["order"].(map[string]any)
	if ordMap == nil {
		t.Fatalf("missing order in resp: %s", w.Body.String())
	}
	if ordMap["status"] != "FILLED" {
		t.Fatalf("paper entry should be FILLED, got %v (%s)", ordMap["status"], w.Body.String())
	}
	id, _ := ordMap["id"].(string)
	if !strings.HasPrefix(id, "ord-") {
		t.Fatalf("order id should come from OMS (ord- prefix), got %q", id)
	}
	// 展示层事实源可查（与界面下单同口径）。
	if got := store.GetOrderByID(id); got == nil || got["status"] != "FILLED" {
		t.Fatalf("order must be visible in store: %+v", got)
	}
	if om := order.GetOrderManager().GetOrder(id); om == nil || om.Status != model.StatusFilled {
		t.Fatalf("OMS order should be FILLED: %+v", om)
	}
}

// 风控拒绝：webhook 返回 400 且订单 REJECTED 不成交（收口前此路径完全绕过风控）。
func TestTradingViewWebhookEntryRiskRejected(t *testing.T) {
	freshOMSRateWindow()
	om := order.GetOrderManager()
	origRisk := om.RiskCheck
	om.RiskCheck = func(req *order.Request) error {
		return fmt.Errorf("mock 风控阻断")
	}
	t.Cleanup(func() { om.RiskCheck = origRisk })

	w := postWebhook(t, "/webhook/tv", `{"symbol":"WBRISKUSDT","action":"buy","quantity":0.05,"price":50000}`)
	assertEq(t, w.Code, http.StatusBadRequest, "risk-rejected entry must return error")

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "error" {
		t.Fatalf("expected error status, got %s", w.Body.String())
	}
	orderID, _ := resp["order_id"].(string)
	if orderID == "" {
		t.Fatalf("rejected order id must be returned for audit: %s", w.Body.String())
	}
	ord := om.GetOrder(orderID)
	if ord == nil || ord.Status != model.StatusRejected || ord.Filled != 0 {
		t.Fatalf("order must be REJECTED and unfilled: %+v", ord)
	}
	rec, err := store.GetOrderRepo().GetByID(orderID)
	if err != nil || rec == nil || rec.Status != "REJECTED" {
		t.Fatalf("persisted order must be REJECTED audit row: %+v err=%v", rec, err)
	}
}

// AI 决策门拒绝：webhook 源不在默认豁免列表（grid/dca/lmartin），入场被拦。
func TestTradingViewWebhookEntryAIGateRejected(t *testing.T) {
	freshOMSRateWindow()
	g := &aigate.Gate{
		Sources: aigate.ContextSources{
			RecentBars: func(string, int) []aigate.Bar { return nil },
			Now:        func() time.Time { return time.UnixMilli(1700000000000) },
		},
		LoadConfigFn: func() aigate.Config {
			cfg := aigate.DefaultConfig()
			cfg.Enabled = true
			return cfg
		},
		ProviderChain: func(aigate.Config) []aigate.ProviderCaller {
			return []aigate.ProviderCaller{{Name: "fake", Model: "m", Caller: webhookRejectCaller{}}}
		},
	}
	aigate.SetGlobal(g)
	t.Cleanup(func() { aigate.SetGlobal(aigate.NewGate()) })

	w := postWebhook(t, "/webhook/tv", `{"symbol":"WBGATEUSDT","action":"buy","quantity":0.05,"price":50000}`)
	assertEq(t, w.Code, http.StatusBadRequest, "gate-rejected entry must return error")
	if !strings.Contains(w.Body.String(), "AI 决策门拦截") {
		t.Fatalf("error should mention AI gate: %s", w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	orderID, _ := resp["order_id"].(string)
	ord := order.GetOrderManager().GetOrder(orderID)
	if ord == nil || ord.Status != model.StatusRejected || ord.Filled != 0 {
		t.Fatalf("gate-rejected order must be REJECTED and unfilled: %+v", ord)
	}
}

// 出场单保持直发（AI 不能拦出场），但必须留审计痕迹。
func TestTradingViewWebhookExitAudited(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	w := postWebhook(t, "/webhook/tv", `{"symbol":"WBEXITUSDT","action":"exit","quantity":0.05,"price":50000}`)
	assertEq(t, w.Code, http.StatusOK, "exit stays direct")

	found := false
	for _, entry := range store.GetAuditLog(50, 0) {
		if entry["action"] == "tv_exit_direct" && strings.Contains(entry["detail"].(string), "WBEXITUSDT") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("exit direct dispatch must be audited (tv_exit_direct)")
	}
}

// generic webhook 入场同样收口到 OMS；风控拒绝 → 400 不成交。
func TestGenericWebhookEntryViaOMSAndRiskRejected(t *testing.T) {
	freshOMSRateWindow()
	w := postWebhook(t, "/webhook/generic", `{"symbol":"WBGENUSDT","side":"BUY","type":"LIMIT","quantity":0.05,"price":50000}`)
	assertEq(t, w.Code, http.StatusOK, "generic entry via OMS")
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	ordMap, _ := resp["order"].(map[string]any)
	if ordMap == nil || ordMap["status"] != "FILLED" {
		t.Fatalf("generic entry should fill on paper: %s", w.Body.String())
	}

	freshOMSRateWindow()
	om := order.GetOrderManager()
	origRisk := om.RiskCheck
	om.RiskCheck = func(req *order.Request) error { return fmt.Errorf("mock 风控阻断") }
	t.Cleanup(func() { om.RiskCheck = origRisk })

	w2 := postWebhook(t, "/webhook/generic", `{"symbol":"WBGENUSDT","side":"BUY","type":"LIMIT","quantity":0.05,"price":50000}`)
	assertEq(t, w2.Code, http.StatusBadRequest, "risk-rejected generic entry must error")
}

type webhookRejectCaller struct{}

func (webhookRejectCaller) ChatCompletion(req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	return &ai.CompletionResponse{
		Choices: []ai.Choice{{Message: ai.ChatMessage{
			Role:    ai.RoleAssistant,
			Content: `{"decision":"reject","confidence":0.95,"reasons":["mock 决策门拒绝"]}`,
		}}},
	}, nil
}
