package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/data"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// seedAnalysisBars 通过 DataDownloader 落本地 K 线（与线上同一数据口径）。
func seedAnalysisBars(t *testing.T, symbol, interval string, n int) {
	t.Helper()
	old := DataDownloader
	DataDownloader = data.NewDownloader(data.NewStorage())
	t.Cleanup(func() { DataDownloader = old })

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	bars := make([]model.Bar, n)
	prev := 100.0
	for i := 0; i < n; i++ {
		c := 100 + 10*math.Sin(float64(i)/4.0) + float64(i)*0.05
		bars[i] = model.Bar{
			Symbol: symbol, Interval: interval, Time: base + int64(i)*3600_000,
			Open: prev, High: math.Max(prev, c) + 0.5, Low: math.Min(prev, c) - 0.5,
			Close: c, Volume: 1000,
		}
		prev = c
	}
	if err := DataDownloader.SaveBars(bars); err != nil {
		t.Fatalf("seed bars: %v", err)
	}
}

// TestAnalysisJobLifecycle 端到端：启动 lookahead 任务 → 轮询至完成 → 结果含结论。
func TestAnalysisJobLifecycle(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	seedAnalysisBars(t, "ANAUSDT", "1h", 400)

	r := setupRouter()
	r.POST("/analysis/lookahead", StartLookaheadAnalysis)
	r.GET("/analysis/jobs", ListAnalysisJobs)
	r.GET("/analysis/jobs/:id", GetAnalysisJob)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/analysis/lookahead",
		strings.NewReader(`{"strategy_type":"macd_golden_long","symbol":"ANAUSDT","interval":"1h"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "start lookahead")

	var startResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &startResp)
	jobID, _ := startResp["job_id"].(string)
	assertTrue(t, jobID != "", "job id returned")
	assertTrue(t, startResp["bars"].(float64) >= 100, "bars count in response")

	var detail map[string]any
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/analysis/jobs/"+jobID, nil)
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "get job")
		detail = nil
		_ = json.Unmarshal(w.Body.Bytes(), &detail)
		if s, _ := detail["status"].(string); s == "completed" || s == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assertTrue(t, detail["status"] == "completed",
		fmt.Sprintf("job should complete, got %v (err=%v)", detail["status"], detail["error"]))
	result, ok := detail["result"].(map[string]any)
	assertTrue(t, ok, "result present")
	// sma_cross 为干净因果策略：结论应为 unbiased
	assertTrue(t, result["conclusion"] == "unbiased",
		fmt.Sprintf("clean strategy should be unbiased, got %v", result["conclusion"]))
	assertTrue(t, result["variant_count"].(float64) >= 5, "variants ran")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/analysis/jobs", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "list jobs")
	var listResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	jobs, _ := listResp["jobs"].([]any)
	assertTrue(t, len(jobs) >= 1, "jobs list non-empty")
}

func TestAnalysisStartValidation(t *testing.T) {
	r := setupRouter()
	r.POST("/analysis/recursive", StartRecursiveAnalysis)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/analysis/recursive",
		strings.NewReader(`{"strategy_type":"nope","symbol":"BTCUSDT"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "unsupported strategy")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/analysis/recursive",
		strings.NewReader(`{"strategy_type":"sma_cross","symbol":"NODATAUSDT"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "insufficient data")
}

func TestGetAnalysisJobNotFound(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	r := setupRouter()
	r.GET("/analysis/jobs/:id", GetAnalysisJob)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/analysis/jobs/ana-nope", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "not found")
}
