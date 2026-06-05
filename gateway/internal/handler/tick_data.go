package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/data"
)

// TickDownloader is the global tick downloader instance.
var TickDownloader *data.TickDownloader
var TickStorage *data.TickStorage

func init() {
	TickStorage = data.NewTickStorage()
	TickDownloader = data.NewTickDownloader(TickStorage)
}

// ── Tick Data Handlers ───────────────────────────────────────────

// StartTickDownload starts a tick download job.
func StartTickDownload(c *gin.Context) {
	var body struct {
		Symbol    string `json:"symbol"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if body.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}

	var startMs, endMs int64
	if body.StartDate != "" {
		t, err := time.Parse("2006-01-02", body.StartDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date"})
			return
		}
		startMs = t.UnixMilli()
	}
	if body.EndDate != "" {
		t, err := time.Parse("2006-01-02", body.EndDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date"})
			return
		}
		endMs = t.UnixMilli() + 86400000 // end of day
	}

	jobID, err := TickDownloader.StartDownload(body.Symbol, startMs, endMs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "started",
		"job_id": jobID,
	})
}

// GetTicks returns stored ticks for a symbol.
func GetTicks(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}

	startMs := int64(0)
	endMs := time.Now().UnixMilli()
	limit := 1000

	if f := c.Query("start"); f != "" {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil {
			startMs = v
		}
	}
	if t := c.Query("end"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			endMs = v
		}
	}
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
			if limit > 10000 {
				limit = 10000
			}
		}
	}

	ticks, err := TickStorage.LoadTicks(symbol, startMs, endMs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(ticks) > limit {
		ticks = ticks[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol": symbol,
		"start":  startMs,
		"end":    endMs,
		"count":  len(ticks),
		"ticks":  ticks,
	})
}

// GetTickInfo returns metadata about stored tick data.
func GetTickInfo(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}

	info := TickStorage.GetTickInfo(symbol)
	c.JSON(http.StatusOK, info)
}

// ── Tick Backtest Handlers ───────────────────────────────────────

// tickBacktestJob tracks a running tick backtest job.
type tickBacktestJob struct {
	ID        string                    `json:"id"`
	Status    string                    `json:"status"`
	Result    *backtest.TickBacktestResult `json:"result,omitempty"`
	Error     string                    `json:"error,omitempty"`
	StartedAt int64                     `json:"started_at"`
	EndedAt   int64                     `json:"ended_at,omitempty"`
}

var (
	tickBacktestJobs   = make(map[string]*tickBacktestJob)
	tickBacktestJobMu  sync.Mutex
	tickBacktestNextID int64
)

// RunTickBacktest starts a tick backtest job asynchronously.
func RunTickBacktest(c *gin.Context) {
	var body struct {
		Strategy string                 `json:"strategy"`
		Symbol   string                 `json:"symbol"`
		Start    int64                  `json:"start"`
		End      int64                  `json:"end"`
		Params   map[string]interface{} `json:"params"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	if body.Symbol == "" {
		body.Symbol = "BTCUSDT"
	}
	if body.Strategy == "" {
		body.Strategy = "sma_cross"
	}

	// Validate tick data exists
	ticks, err := TickStorage.LoadTicks(body.Symbol, body.Start, body.End)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(ticks) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no tick data found for " + body.Symbol})
		return
	}
	if len(ticks) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient tick data"})
		return
	}

	// Create job
	tickBacktestJobMu.Lock()
	tickBacktestNextID++
	jobID := fmt.Sprintf("tick_bt_%d", tickBacktestNextID)
	job := &tickBacktestJob{
		ID:        jobID,
		Status:    "running",
		StartedAt: time.Now().UnixMilli(),
	}
	tickBacktestJobs[jobID] = job
	tickBacktestJobMu.Unlock()

	// Run async
	go func() {
		cfg := backtest.TickBacktestConfig{
			InitialBalance: 100000,
			Commission:     0.001,
			SlippageModel:  backtest.SlippageFixed,
			SlippageBps:    5,
			StartTime:      body.Start,
			EndTime:        body.End,
		}

		runner := backtest.NewTickBacktestRunner(cfg)
		runner.LoadTicks(body.Symbol, ticks)

		var strategy backtest.BacktestStrategy
		switch body.Strategy {
		case "breakout":
			strategy = &breakoutBTStrategy{symbol: body.Symbol, lookback: 20, bufferPct: 0.002, stopLossPct: 0.02, takeProfitPct: 0.04}
		default:
			strategy = &smaCrossStrategy{symbol: body.Symbol, fastPeriod: 12, slowPeriod: 26}
		}

		result, err := runner.Run(strategy)

		tickBacktestJobMu.Lock()
		job.EndedAt = time.Now().UnixMilli()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "done"
			job.Result = result
		}
		tickBacktestJobMu.Unlock()
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"status": "started",
		"job_id": jobID,
	})
}

// ListTickBacktestJobs lists tick backtest jobs.
func ListTickBacktestJobs(c *gin.Context) {
	tickBacktestJobMu.Lock()
	defer tickBacktestJobMu.Unlock()

	jobs := make([]*tickBacktestJob, 0, len(tickBacktestJobs))
	for _, job := range tickBacktestJobs {
		jobs = append(jobs, job)
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// GetTickBacktestJob returns a specific tick backtest job.
func GetTickBacktestJob(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}

	tickBacktestJobMu.Lock()
	job, ok := tickBacktestJobs[id]
	tickBacktestJobMu.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}
