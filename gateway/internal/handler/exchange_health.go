package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 交易所体检（对标 freqtrade check_exchange 的一键健康检查）──
// 触发后异步执行：每家交易所依次跑 L1 公共连通 / L2 凭证有效 /
// L3 交易能力元数据 / L4 WebSocket（分级语义见 adapter/healthcheck.go），
// 结果落库 xt_exchange_health_checks 供 latest/history 查询。
// 凭证只经 credential vault 注入适配器；报告与错误摘要均已脱敏。

type exchangeHealthJob struct {
	ID         string                                      `json:"id"`
	UserID     int64                                       `json:"user_id"`
	Exchanges  []string                                    `json:"exchanges"`
	Status     string                                      `json:"status"` // running / completed / failed
	Results    map[string]*adapter.ExchangeHealthReport    `json:"results"`
	Error      string                                      `json:"error,omitempty"`
	CreatedAt  int64                                       `json:"created_at"`
	FinishedAt int64                                       `json:"finished_at,omitempty"`
}

var (
	exchangeHealthJobs   = make(map[string]*exchangeHealthJob)
	exchangeHealthJobsMu sync.Mutex
	exchangeHealthJobSeq int
)

// ExchangeHealthCheckStart POST /api/exchanges/health-check
// body: {"exchange": "binance" | "all"}；缺省/空 = all（全部 10 家已适配交易所）。
func ExchangeHealthCheckStart(c *gin.Context) {
	var body struct {
		Exchange string `json:"exchange"`
	}
	_ = c.ShouldBindJSON(&body) // 空 body 也合法（=all）

	var targets []string
	switch {
	case body.Exchange == "" || body.Exchange == "all":
		targets = adapter.SupportedHealthCheckExchanges()
	case adapter.IsHealthCheckExchange(body.Exchange):
		targets = []string{body.Exchange}
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("不支持的交易所: %s（支持: %v）", body.Exchange, adapter.SupportedHealthCheckExchanges()),
		})
		return
	}

	exchangeHealthJobsMu.Lock()
	exchangeHealthJobSeq++
	jobID := fmt.Sprintf("xh-%d", exchangeHealthJobSeq)
	job := &exchangeHealthJob{
		ID:        jobID,
		UserID:    getUserID(c),
		Exchanges: targets,
		Status:    "running",
		Results:   make(map[string]*adapter.ExchangeHealthReport),
		CreatedAt: time.Now().UnixMilli(),
	}
	exchangeHealthJobs[jobID] = job
	pruneExchangeHealthJobsLocked()
	exchangeHealthJobsMu.Unlock()

	go runExchangeHealthJob(job)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":    jobID,
		"status":    "running",
		"exchanges": targets,
	})
}

// ExchangeHealthCheckJobStatus GET /api/exchanges/health-check/jobs/:id
func ExchangeHealthCheckJobStatus(c *gin.Context) {
	exchangeHealthJobsMu.Lock()
	job, ok := exchangeHealthJobs[c.Param("id")]
	exchangeHealthJobsMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job 不存在（内存 job，服务重启后失效；历史结果请查 /latest 或 /history）"})
		return
	}
	c.JSON(http.StatusOK, job)
}

// ExchangeHealthCheckLatest GET /api/exchanges/health-check/latest
// 每家交易所最近一次体检结果（含完整分级报告）。
func ExchangeHealthCheckLatest(c *gin.Context) {
	recs, err := store.NewExchangeHealthRepo().LatestPerExchange()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": exchangeHealthRecordsToJSON(recs)})
}

// ExchangeHealthCheckHistory GET /api/exchanges/health-check/history?exchange=&limit=
func ExchangeHealthCheckHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	recs, err := store.NewExchangeHealthRepo().History(c.Query("exchange"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": exchangeHealthRecordsToJSON(recs)})
}

// runExchangeHealthJob 异步执行体检：交易所间并发（上限 3），每家结果实时落库。
func runExchangeHealthJob(job *exchangeHealthJob) {
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	repo := store.NewExchangeHealthRepo()

	for _, ex := range job.Exchanges {
		ex := ex
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			checker := &adapter.HealthChecker{}
			report, err := checker.Run(context.Background(), ex)
			if err != nil {
				exchangeHealthJobsMu.Lock()
				job.Status = "failed"
				job.Error = fmt.Sprintf("%s: %v", ex, err)
				exchangeHealthJobsMu.Unlock()
				return
			}

			resultJSON, _ := json.Marshal(report)
			// 落库失败不阻断 job（内存结果仍可用），仅记录。
			_ = repo.Insert(&store.ExchangeHealthRecord{
				Exchange:   report.Exchange,
				JobID:      job.ID,
				Overall:    report.Overall,
				Configured: report.Configured,
				DurationMs: report.DurationMs,
				ResultJSON: string(resultJSON),
			})

			exchangeHealthJobsMu.Lock()
			job.Results[report.Exchange] = report
			exchangeHealthJobsMu.Unlock()
		}()
	}
	wg.Wait()

	exchangeHealthJobsMu.Lock()
	if job.Status != "failed" {
		job.Status = "completed"
	}
	job.FinishedAt = time.Now().UnixMilli()
	exchangeHealthJobsMu.Unlock()
}

// pruneExchangeHealthJobsLocked 上限 200 个 job，淘汰最老的已完成 job。
// 调用方须持有 exchangeHealthJobsMu。
func pruneExchangeHealthJobsLocked() {
	const maxJobs = 200
	if len(exchangeHealthJobs) <= maxJobs {
		return
	}
	ids := make([]string, 0, len(exchangeHealthJobs))
	for id, j := range exchangeHealthJobs {
		if j.Status != "running" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool {
		return exchangeHealthJobs[ids[a]].CreatedAt < exchangeHealthJobs[ids[b]].CreatedAt
	})
	for _, id := range ids[:len(exchangeHealthJobs)-maxJobs] {
		delete(exchangeHealthJobs, id)
	}
}

// exchangeHealthRecordsToJSON 把落库记录转成响应结构（result_json 解析为对象）。
func exchangeHealthRecordsToJSON(recs []*store.ExchangeHealthRecord) []gin.H {
	out := make([]gin.H, 0, len(recs))
	for _, rec := range recs {
		var report json.RawMessage
		if err := json.Unmarshal([]byte(rec.ResultJSON), &report); err != nil {
			report = json.RawMessage(`{}`)
		}
		out = append(out, gin.H{
			"id":          rec.ID,
			"exchange":    rec.Exchange,
			"job_id":      rec.JobID,
			"overall":     rec.Overall,
			"configured":  rec.Configured,
			"duration_ms": rec.DurationMs,
			"checked_at":  rec.CreatedAt,
			"report":      report,
		})
	}
	return out
}
