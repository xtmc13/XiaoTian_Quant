package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	datapkg "github.com/xiaotian-quant/gateway/internal/data"
)

// Global data service instances
var (
	dataStore     *datapkg.Storage
	dataDownloader *datapkg.Downloader
)

func init() {
	dataStore = datapkg.NewStorage()
	dataDownloader = datapkg.NewDownloader(dataStore)
}

// ── Download ──

type downloadReq struct {
	Symbols   []string `json:"symbols"`
	Intervals []string `json:"intervals"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
}

func DataDownload(c *gin.Context) {
	var req downloadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if len(req.Symbols) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbols required"})
		return
	}
	if len(req.Intervals) == 0 {
		req.Intervals = []string{"1h"}
	}

	cfg := datapkg.DownloadConfig{
		Symbols:   req.Symbols,
		Intervals: req.Intervals,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
	}

	jobID, err := dataDownloader.StartDownload(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "started", "job_id": jobID})
}

// ── Download Status ──

func DataDownloadStatus(c *gin.Context) {
	jobID := c.Param("jobId")
	job := dataDownloader.GetJob(jobID)
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

// ── Data Coverage ──

func DataCoverage(c *gin.Context) {
	coverage := dataStore.GetCoverage()
	if coverage == nil {
		coverage = []datapkg.CoverageInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"coverage": coverage})
}

// ── Load Data ──

func DataLoad(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.DefaultQuery("interval", "1h")
	fromStr := c.Query("from")
	toStr := c.Query("to")

	var fromMs, toMs int64
	if fromStr != "" {
		if t, err := time.Parse("2006-01-02", fromStr); err == nil {
			fromMs = t.UnixMilli()
		}
	}
	if toStr != "" {
		if t, err := time.Parse("2006-01-02", toStr); err == nil {
			toMs = t.UnixMilli() + 86400000
		}
	}

	limit := 1000
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 5000 {
			limit = v
		}
	}

	bars := dataStore.LoadOHLCV(symbol, interval, fromMs, toMs)
	if bars == nil {
		bars = []datapkg.OHLCV{}
	}
	if len(bars) > limit {
		bars = bars[len(bars)-limit:]
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":   symbol,
		"interval": interval,
		"count":    len(bars),
		"bars":     bars,
	})
}

// ── Validate ──

func DataValidate(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.DefaultQuery("interval", "1h")

	bars := dataStore.LoadOHLCV(symbol, interval, 0, 0)
	if len(bars) == 0 {
		c.JSON(http.StatusOK, gin.H{"error": "no data", "valid": false})
		return
	}

	v := datapkg.NewValidator(5, 0.25)
	result := v.Validate(bars)

	c.JSON(http.StatusOK, gin.H{
		"valid":   result.IssueCount == 0,
		"result":  result,
	})
}

// ── Prune ──

func DataPrune(c *gin.Context) {
	beforeStr := c.Query("before")
	if beforeStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "before date required"})
		return
	}

	t, err := time.Parse("2006-01-02", beforeStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format: use YYYY-MM-DD"})
		return
	}

	count, err := dataStore.Prune(t.UnixMilli())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "pruned": count})
}

// ── Symbols/Intervals ──

func DataSymbols(c *gin.Context) {
	symbols := dataStore.GetAvailableSymbols()
	if symbols == nil {
		symbols = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"symbols": symbols})
}

func DataIntervals(c *gin.Context) {
	symbol := c.Query("symbol")
	intervals := dataStore.GetAvailableIntervals(symbol)
	if intervals == nil {
		intervals = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "intervals": intervals})
}
