package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// analysis 任务管理端点（对标 hyperopt cancel/delete 模式）：
// cancel 仅对运行中任务生效（context 取消；孤儿任务直接落库 cancelled）；
// delete 仅终态可删。

func seedAnalysisJob(t *testing.T, id, status string) {
	t.Helper()
	rec := &store.AnalysisJobRecord{
		ID: id, UserID: 0, Kind: "lookahead", Status: status,
		Symbol: "BTCUSDT", Interval: "1h", StrategyType: "sma_cross",
	}
	if err := store.NewAnalysisJobRepo().Create(rec); err != nil {
		t.Fatalf("seed job %s: %v", id, err)
	}
	t.Cleanup(func() { _ = store.NewAnalysisJobRepo().Delete(id) })
}

func analysisJobRouter() *gin.Engine {
	r := setupRouter()
	r.POST("/analysis/jobs/:id/cancel", CancelAnalysisJob)
	r.DELETE("/analysis/jobs/:id", DeleteAnalysisJob)
	return r
}

func TestCancelAnalysisJob(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	r := analysisJobRouter()

	// 1. 不存在的任务 → 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/analysis/jobs/ana-nope/cancel", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "cancel missing job")

	// 2. 运行中 + 进程内句柄 → 200，context cancel 被触发
	seedAnalysisJob(t, "ana-cancel-live", "running")
	cancelled := make(chan struct{}, 1)
	registerAnalysisJobCancel("ana-cancel-live", func() { cancelled <- struct{}{} })
	t.Cleanup(func() { unregisterAnalysisJobCancel("ana-cancel-live") })

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/analysis/jobs/ana-cancel-live/cancel", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "cancel running job")
	select {
	case <-cancelled:
	default:
		t.Fatal("cancel endpoint must invoke the registered context.CancelFunc")
	}

	// 3. 运行中但无句柄（进程重启遗留孤儿任务）→ 200 且直接落库 cancelled
	seedAnalysisJob(t, "ana-cancel-orphan", "running")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/analysis/jobs/ana-cancel-orphan/cancel", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "cancel orphan job")
	job, err := store.NewAnalysisJobRepo().GetByID("ana-cancel-orphan")
	if err != nil || job == nil || job.Status != "cancelled" {
		t.Fatalf("orphan job must be marked cancelled: %+v err=%v", job, err)
	}

	// 4. 已终态任务 → 409（重复取消不幂等成功）
	seedAnalysisJob(t, "ana-cancel-done", "completed")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/analysis/jobs/ana-cancel-done/cancel", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusConflict, "cancel terminal job")
}

func TestDeleteAnalysisJob(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	r := analysisJobRouter()

	// 1. 不存在的任务 → 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/analysis/jobs/ana-nope", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "delete missing job")

	// 2. 运行中 → 409（须先取消）
	seedAnalysisJob(t, "ana-del-running", "running")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/analysis/jobs/ana-del-running", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusConflict, "delete running job")

	// 3. 终态（completed/failed/cancelled）→ 200 且记录消失
	for _, tc := range []struct{ id, status string }{
		{"ana-del-completed", "completed"},
		{"ana-del-failed", "failed"},
		{"ana-del-cancelled", "cancelled"},
	} {
		seedAnalysisJob(t, tc.id, tc.status)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("DELETE", "/analysis/jobs/"+tc.id, nil)
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "delete terminal job "+tc.status)
		job, err := store.NewAnalysisJobRepo().GetByID(tc.id)
		if err != nil || job != nil {
			t.Fatalf("deleted job must be gone: %+v err=%v", job, err)
		}
	}
}
