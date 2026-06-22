package handler

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/spa"
)

func Index(c *gin.Context) {
	html, err := spa.IndexHTML()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0, s-maxage=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func StartBackgroundTasks() {
	// Portfolio snapshot recorder (every 60s)
	go func() {
		snapTicker := time.NewTicker(60 * time.Second)
		defer snapTicker.Stop()
		for range snapTicker.C {
			if mgr := portfolio.GetManager(); mgr != nil {
				mgr.Snapshot()
			}
		}
	}()

	// AI Bot performance snapshot recorder (configurable, default 60s)
	snapshotInterval := 60 * time.Second
	if v := os.Getenv("AI_BOT_SNAPSHOT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			snapshotInterval = d
		}
	}
	go StartAIBotSnapshotWorker(snapshotInterval)

	// Periodic health checks
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// Check Binance connectivity
		if _, err := fetchBinanceKlines("BTCUSDT", "1m", 1, 0, 0); err != nil {
			log.Printf("[health] Binance connectivity check failed: %v", err)
		}
		// Check ML server if configured
		if mlURL := os.Getenv("ML_SERVER_URL"); mlURL != "" {
			resp, err := http.Get(mlURL + "/health")
			if err != nil {
				log.Printf("[health] ML server unreachable: %v", err)
			} else {
				resp.Body.Close()
			}
		}
	}
}
