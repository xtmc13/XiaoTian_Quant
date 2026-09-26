package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

/* ── P0-1 修复回归：凭证走保险库 ─────────────────────────────── */

// TestSaveExchangeCredentialsEndpoint：写入 → GetCredential 取回新值；空字段
// 保留旧值；全空且无现存 → 400。
func TestSaveExchangeCredentialsEndpoint(t *testing.T) {
	r := setupRouter()
	r.PUT("/config/exchanges/credentials", SaveExchangeCredentials)

	put := func(body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/config/exchanges/credentials", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp
	}

	// 全新凭证写入。
	code, resp := put(`{"name":"binance","api_key":"NEW_KEY_A","secret":"NEW_SECRET_A","passphrase":""}`)
	assertEq(t, code, http.StatusOK, "put new credentials")
	assertTrue(t, resp["success"] == true, "put success flag")
	k, s, _ := credStubGet(t, "binance")
	assertTrue(t, k == "NEW_KEY_A" && s == "NEW_SECRET_A", "GetCredential must return new value")

	// 空字段保存 = 不改动现有凭证。
	code, resp = put(`{"name":"binance","api_key":"","secret":"","passphrase":""}`)
	assertEq(t, code, http.StatusOK, "empty save keeps existing")
	k, s, _ = credStubGet(t, "binance")
	assertTrue(t, k == "NEW_KEY_A" && s == "NEW_SECRET_A", "empty save must not change creds")

	// 部分更新：只换 secret。
	code, _ = put(`{"name":"binance","api_key":"","secret":"NEW_SECRET_B","passphrase":""}`)
	assertEq(t, code, http.StatusOK, "partial update")
	k, s, _ = credStubGet(t, "binance")
	assertTrue(t, k == "NEW_KEY_A" && s == "NEW_SECRET_B", "partial update must merge")

	// 无现存且无新值 → 400。
	code, _ = put(`{"name":"bybit","api_key":"","secret":"","passphrase":""}`)
	assertEq(t, code, http.StatusBadRequest, "no existing + empty must be 400")
}

func credStubGet(t *testing.T, name string) (string, string, string) {
	t.Helper()
	k, s, p, err := store.GetVault().GetOrReload(name)
	assertTrue(t, err == nil, "vault get "+name)
	return k, s, p
}

// TestSaveConfigStripsExchangeSecrets：PUT /config 带明文 exchanges → 落盘内容
// 必须无明文（已收进保险库），运行期 GetCredential 从 vault 取到新值。
func TestSaveConfigStripsExchangeSecrets(t *testing.T) {
	r := setupRouter()
	r.PUT("/config", SaveConfig)

	body := `{"server":{"port":"9999"},"exchanges":{"binance":{"enabled":true,"api_key":"PLAINTEXT_K","secret":"PLAINTEXT_S","testnet":true}}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "save config")

	// 运行期从 vault 取到（SaveConfig 已自动吸收）。
	k, s, _, verr := store.GetVault().GetOrReload("binance")
	assertTrue(t, verr == nil, "vault absorb get")
	assertTrue(t, k == "PLAINTEXT_K" && s == "PLAINTEXT_S", "vault must absorb plaintext")

	// config.yaml 落盘无明文（risk 段等其他键不受影响由既有测试覆盖）。
	raw, err := os.ReadFile(store.ConfigFilePath())
	assertTrue(t, err == nil, "read config yaml")
	assertTrue(t, !strings.Contains(string(raw), "PLAINTEXT_K"), "yaml must not contain plaintext key")
	assertTrue(t, strings.Contains(string(raw), "testnet: true"), "non-secret fields persist")
}

// ExchangeTest 凭证回落：表单留空（"留空保持不变"）时必须从保险库取已存凭证，
// 而不是直接报 "API key and secret required"（生产回归：用户已配置却测不了）。
func TestExchangeTestFallsBackToVault(t *testing.T) {
	r := setupRouter()
	r.POST("/exchange/test", ExchangeTest)

	post := func(body string) map[string]any {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/exchange/test", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp
	}

	// 保险库无凭证 → 仍然报缺凭证。
	resp := post(`{"name":"phemex","api_key":"","secret":""}`)
	assertTrue(t, resp["success"] == false, "no creds must fail")
	assertTrue(t, strings.Contains(resp["message"].(string), "required"), "expect required msg")

	// 保险库有凭证（zb 走"适配器未实现"分支，不触网）→ 空表单应回落到 vault 并越过凭证检查。
	assertTrue(t, store.GetVault().Store("zb", "zb", "VAULT_K", "VAULT_S", "VAULT_P") == nil, "seed vault")
	resp = post(`{"name":"zb","api_key":"","secret":""}`)
	assertTrue(t, !strings.Contains(resp["message"].(string), "required"), "vault creds must be used")
	assertTrue(t, strings.Contains(resp["message"].(string), "尚未实现"), "expect adapter-not-implemented branch")

	// 表单只给 api_key 时 secret 从 vault 补齐（部分回落）。
	resp = post(`{"name":"zb","api_key":"FORM_K","secret":""}`)
	assertTrue(t, strings.Contains(resp["message"].(string), "尚未实现"), "partial merge must pass credential gate")
}
