package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestGetFactors 因子列表：全局目录，无需数据/网络。
func TestGetFactors(t *testing.T) {
	r := setupRouter()
	r.GET("/api/factors", GetFactors)
	req := httptest.NewRequest(http.MethodGet, "/api/factors", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "GetFactors status")

	var resp struct {
		Factors []struct {
			Name     string `json:"name"`
			Version  int    `json:"version"`
			Versions []int  `json:"versions"`
			Category string `json:"category"`
		} `json:"factors"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertTrue(t, resp.Count >= 30, "expected >= 30 factors")
	assertTrue(t, len(resp.Factors) == resp.Count, "factors length mismatch")
	for _, f := range resp.Factors {
		assertTrue(t, f.Name != "", "factor name empty")
		assertTrue(t, f.Version >= 1, "factor version")
	}
	// 版本化注册表：versions 列表非空（内置因子均为 v1）
	for _, f := range resp.Factors {
		assertTrue(t, len(f.Versions) >= 1, "expected versions list")
	}
}

// TestEvaluateFactorValidation 参数校验路径（不触网：数据不足/因子不存在）。
func TestEvaluateFactorValidation(t *testing.T) {
	r := setupRouter()
	r.POST("/api/factors/evaluate", EvaluateFactor)

	// 未知因子 → 404
	body := `{"name":"no_such_factor","symbol":"BTCUSDT"}`
	req := httptest.NewRequest(http.MethodPost, "/api/factors/evaluate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "unknown factor should 404")

	// 非法 JSON → 400
	req = httptest.NewRequest(http.MethodPost, "/api/factors/evaluate", strings.NewReader(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "bad json should 400")
}

// TestFactorLayersValidation 同上：未知因子 404。
func TestFactorLayersValidation(t *testing.T) {
	r := setupRouter()
	r.POST("/api/factors/layers", FactorLayers)
	body := `{"name":"no_such_factor","symbol":"BTCUSDT"}`
	req := httptest.NewRequest(http.MethodPost, "/api/factors/layers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "unknown factor should 404")
}

// TestGetFactorValuesUnknown 未知因子 404。
func TestGetFactorValuesUnknown(t *testing.T) {
	r := setupRouter()
	r.GET("/api/factors/:name/values", GetFactorValues)
	req := httptest.NewRequest(http.MethodGet, "/api/factors/no_such_factor/values?symbol=BTCUSDT", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "unknown factor should 404")
}

// TestListFactorEvaluationsEmpty 空库返回空数组而非 null。
func TestListFactorEvaluationsEmpty(t *testing.T) {
	r := setupRouter()
	r.GET("/api/factors/evaluations", ListFactorEvaluations)
	req := httptest.NewRequest(http.MethodGet, "/api/factors/evaluations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "list evaluations status")
	var resp struct {
		Evaluations []any `json:"evaluations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertTrue(t, resp.Evaluations != nil, "evaluations should be [] not null")
}

// TestPortfolioBacktestValidation 组合回测参数校验（不触网）。
func TestPortfolioBacktestValidation(t *testing.T) {
	r := setupRouter()
	r.POST("/api/backtests/portfolio", RunPortfolioBacktest)

	// 无 legs → 400
	body := `{"name":"t","legs":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/backtests/portfolio", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "empty legs should 400")

	// 非法再平衡 → 400
	body = `{"name":"t","rebalance":"hourly","legs":[{"strategy_type":"trend_long","symbol":"BTCUSDT","weight":1}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/backtests/portfolio", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "bad rebalance should 400")

	// 缺 symbol → 400
	body = `{"name":"t","legs":[{"strategy_type":"trend_long","weight":1}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/backtests/portfolio", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "missing symbol should 400")

	// 非法日期 → 400
	body = `{"name":"t","start":"not-a-date","legs":[{"strategy_type":"trend_long","symbol":"BTCUSDT","weight":1}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/backtests/portfolio", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "bad date should 400")
}

// TestPortfolioBacktestNotFound 详情属主/存在性校验：不存在的 id → 404。
func TestPortfolioBacktestNotFound(t *testing.T) {
	r := setupRouter()
	r.GET("/api/backtests/portfolio/:id", GetPortfolioBacktest)
	req := httptest.NewRequest(http.MethodGet, "/api/backtests/portfolio/pb-none", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "missing portfolio backtest should 404")
}

var _ = gin.Mode()
