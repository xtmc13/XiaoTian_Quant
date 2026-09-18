package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// adminOnly 模拟已认证 admin（AdminRequired 读取 role）。
func adminOnly(c *gin.Context) {
	c.Set("user_id", int64(1))
	c.Set("role", "admin")
	GetRiskConfig(c)
}

func adminOnlyPut(c *gin.Context) {
	c.Set("user_id", int64(1))
	c.Set("role", "admin")
	UpdateRiskConfig(c)
}

// userOnlyPut 仅做占位（真实鉴权由 AdminRequired 依据 JWT 完成）。
func userOnlyPut(c *gin.Context) {}

func TestRiskConfigPutValidation(t *testing.T) {
	r := setupRouter()
	r.PUT("/risk/config", adminOnlyPut)

	cases := []struct {
		name string
		body string
	}{
		{"max_concurrent 0", `{"max_concurrent_orders":0,"position_limit_pct":100,"profit_protection_enabled":false}`},
		{"max_concurrent 51", `{"max_concurrent_orders":51,"position_limit_pct":100,"profit_protection_enabled":false}`},
		{"position_limit 0", `{"max_concurrent_orders":5,"position_limit_pct":0,"profit_protection_enabled":false}`},
		{"position_limit 101", `{"max_concurrent_orders":5,"position_limit_pct":101,"profit_protection_enabled":false}`},
		{"position_limit 2500（历史事故值）", `{"max_concurrent_orders":5,"position_limit_pct":2500,"profit_protection_enabled":false}`},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/risk/config", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusBadRequest, tc.name+" must be 400")
	}
}

func TestRiskConfigPutAdminOnly(t *testing.T) {
	r := setupRouter()
	r.PUT("/risk/config", userOnlyPut, middleware.AdminRequired(), UpdateRiskConfig)

	// 有效 user 角色 JWT → 403（middleware 惯例：GenerateJWT(1,"user",...)）。
	token, err := store.GenerateJWT(1, "testuser", "user", 1)
	assertTrue(t, err == nil, "generate user jwt")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/risk/config", strings.NewReader(`{"max_concurrent_orders":5,"position_limit_pct":100,"profit_protection_enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusForbidden, "non-admin PUT must be 403")
}

// TestRiskConfigGetPutRoundtrip PUT 写回 → GET 回读一致 + config.yaml risk 段
// 合并写回（保留其他键）。
func TestRiskConfigGetPutRoundtrip(t *testing.T) {
	r := setupRouter()
	r.GET("/risk/config", adminOnly)
	r.PUT("/risk/config", adminOnlyPut)

	// 预置 risk 段其他键，验证合并写回不丢失。
	assertTrue(t, store.SaveRiskSection(map[string]any{"daily_limit": 12345.0}) == nil, "seed risk section")

	putBody := `{"max_concurrent_orders":3,"position_limit_pct":100,"profit_protection_enabled":true}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/risk/config", strings.NewReader(putBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "PUT status")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/risk/config", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "GET status")
	var got RiskConfigPayload
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &got) == nil, "GET JSON")
	assertTrue(t, got.MaxConcurrentOrders == 3, "max_concurrent_orders roundtrip")
	assertTrue(t, got.PositionLimitPct == 100, "position_limit_pct roundtrip")
	assertTrue(t, got.ProfitProtectionEnabled, "profit_protection_enabled roundtrip")

	// config.yaml risk 段：三个键已写回 + 其他键保留。
	sec := store.LoadRiskSection()
	assertTrue(t, sec != nil, "risk section persisted")
	assertTrue(t, sec["daily_limit"] == 12345.0, "other risk keys preserved")
	assertTrue(t, sec["max_concurrent_orders"] == 3, "yaml max_concurrent_orders")
	assertTrue(t, sec["position_limit_pct"] == 100.0, "yaml position_limit_pct")
	assertTrue(t, sec["profit_protection_enabled"] == true, "yaml profit_protection_enabled")

	t.Cleanup(func() {
		risk.SetProfitProtectionEnabled(false)
		_ = store.SaveRiskSection(map[string]any{"max_concurrent_orders": 5, "position_limit_pct": 100, "profit_protection_enabled": false})
	})
}

// TestRiskConfigRuntimeEffect PUT 后 risk manager 检查链即时生效（无需重启）：
// position_limit_pct 改为 1 后，大曝光单必须被拒。
func TestRiskConfigRuntimeEffect(t *testing.T) {
	mgr := risk.GetManager()
	orig := mgr.Config()
	t.Cleanup(func() { mgr.UpdateConfig(orig) })

	r := setupRouter()
	r.PUT("/risk/config", adminOnlyPut)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/risk/config", strings.NewReader(`{"max_concurrent_orders":5,"position_limit_pct":1,"profit_protection_enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "PUT status")

	// 名义 10000 USDT / 权益 100 USDT = 10000% 曝光 ≫ 1% 上限。
	err := mgr.Check(&risk.Context{Symbol: "BTCUSDT", OrderPrice: 1000, OrderQuantity: 10, TotalEquity: 100})
	assertTrue(t, err != nil, "position limit must reject after runtime update, got nil")
	assertTrue(t, strings.Contains(err.Error(), "position exposure"), "error must be position limit, got "+err.Error())

	// 盈利保护开关同步生效。
	assertTrue(t, !risk.ProfitProtectionEnabled(), "profit protection must be off")
}
