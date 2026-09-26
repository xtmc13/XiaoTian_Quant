package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// PortfolioSummary 凭证判定回归：P0-1 后 config.yaml 的 api_key/secret 恒为空
// （明文已收进保险库），摘要必须走统一凭证链（env→vault→config），否则仪表盘
// 资产分布永远显示"暂无交易所数据"（生产实锤）。
func TestPortfolioSummaryCredentialViaVault(t *testing.T) {
	// 保险库存入凭证、config 只留空壳（模拟生产 P0-1 后的真实形态）。
	assertTrue(t, store.GetVault().Store("zb", "zb", "VAULT_K", "VAULT_S", "") == nil, "seed vault")
	cfg := store.GetConfig()
	exchanges, _ := cfg["exchanges"].(map[string]any)
	if exchanges == nil {
		exchanges = map[string]any{}
		cfg["exchanges"] = exchanges
	}
	exchanges["zb"] = map[string]any{"enabled": true, "api_key": "", "secret": ""}
	assertTrue(t, store.SaveConfig(cfg) == nil, "persist config (GetConfig 返回深拷贝，必须落回)")

	r := setupRouter()
	r.GET("/portfolio/summary", PortfolioSummary)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/portfolio/summary", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "summary status")

	var resp struct {
		Exchanges []map[string]any `json:"exchanges"`
	}
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "parse summary")
	found := false
	for _, ex := range resp.Exchanges {
		if ex["name"] == "zb" {
			found = true
			assertTrue(t, ex["configured"] == true, "zb must be configured via vault")
			assertTrue(t, ex["connected"] == true, "non-binance configured exchange reports connected")
		}
	}
	assertTrue(t, found, "zb entry must exist in summary")
}
