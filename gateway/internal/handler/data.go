package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/data"
)

// DataDownloader is the global data downloader instance.
var DataDownloader *data.Downloader

func init() {
	DataDownloader = data.NewDownloader(data.NewStorage())
}

// GetDataCoverage returns the coverage of stored historical data.
func GetDataCoverage(c *gin.Context) {
	if DataDownloader == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data downloader not initialized"})
		return
	}

	store := data.NewStorage()
	coverage := store.GetCoverage()

	c.JSON(http.StatusOK, gin.H{
		"coverage": coverage,
		"symbols":  store.GetAvailableSymbols(),
	})
}

// GetDataInfo returns metadata for a specific symbol/interval.
func GetDataInfo(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	if symbol == "" || interval == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol and interval required"})
		return
	}

	if DataDownloader == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data downloader not initialized"})
		return
	}

	info := DataDownloader.GetDataInfo(symbol, interval)
	c.JSON(http.StatusOK, info)
}

// StartDataDownload initiates a historical data download job.
func StartDataDownload(c *gin.Context) {
	var body data.DownloadConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if DataDownloader == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data downloader not initialized"})
		return
	}

	jobID, err := DataDownloader.StartDownload(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "started",
		"job_id": jobID,
	})
}

// GetDownloadJob returns the status of a download job.
func GetDownloadJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
		return
	}

	if DataDownloader == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data downloader not initialized"})
		return
	}

	job := DataDownloader.GetJob(jobID)
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// GetHistoricalBars returns historical bars for backtesting.
func GetHistoricalBars(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	if symbol == "" || interval == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol and interval required"})
		return
	}

	fromMs := int64(0)
	toMs := time.Now().UnixMilli()

	if f := c.Query("from"); f != "" {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil {
			fromMs = v
		}
	}
	if t := c.Query("to"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			toMs = v
		}
	}

	if DataDownloader == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data downloader not initialized"})
		return
	}

	bars := DataDownloader.LoadBarsForBacktest(symbol, interval, fromMs, toMs)
	if len(bars) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no data found for " + symbol + " " + interval})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":   symbol,
		"interval": interval,
		"from":     fromMs,
		"to":       toMs,
		"count":    len(bars),
		"bars":     bars,
	})
}
