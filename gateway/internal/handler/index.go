package handler

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
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

	// ── Billing 订单核验器（30s 轮询 pending/confirming/failed 订单）──
	go StartBillingVerifier()

	// ── Prometheus 业务指标上报（15s：权益/持仓数/运行中策略数/机器人数量）──
	go func() {
		metricsTicker := time.NewTicker(15 * time.Second)
		defer metricsTicker.Stop()
		for range metricsTicker.C {
			if mgr := portfolio.GetManager(); mgr != nil {
				metrics.SetEquity(mgr.TotalEquity())
				metrics.SetPositionCount(len(mgr.GetPositions()))
			}
			running := 0
			for _, item := range store.GetStrategyConfigs() {
				if s, ok := item["status"].(string); ok && s == "running" {
					running++
				}
			}
			metrics.SetActiveStrategies(running)

			// 运行中机器人数量（按类型）：记录 status=running 即为持久化真值。
			if recs, err := store.NewGridRepo().List(map[string]any{"status": "running"}, 500); err == nil {
				metrics.SetBotsRunning("grid", len(recs))
			}
			if recs, err := store.NewDCARepo().List(map[string]any{"status": "running"}, 500); err == nil {
				metrics.SetBotsRunning("dca", len(recs))
			}
			if recs, err := store.NewLayeredMartinRepo().List(map[string]any{"status": "running"}, 500); err == nil {
				metrics.SetBotsRunning("layered_martin", len(recs))
			}
			runningAI := 0
			for _, item := range store.GetAIBotInstances(0) {
				if s, ok := item["status"].(string); ok && s == "running" {
					runningAI++
				}
			}
			metrics.SetBotsRunning("ai", runningAI)
		}
	}()

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
