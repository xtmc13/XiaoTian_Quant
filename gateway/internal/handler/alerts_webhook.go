// ── Alertmanager 告警接入 HTTP API ──
//
// POST /api/alerts/webhook：Alertmanager v4 webhook 接收端。公开路由（豁免
// JWT），用 X-Alert-Webhook-Secret 共享密钥头校验（env ALERT_WEBHOOK_SECRET，
// 常量时间比较），未配置密钥时 fail-closed 返回 503。验权后交给注入的
// AlertIngestService（生产为 alerting.Service）做 fingerprint 去重与 notify 广播。
//
// GET /api/alerts/active / GET /api/alerts/history：AuthRequired 的查询端，
// 数据来自内部落库（xt_alert_events，迁移 0033），不依赖 Prometheus 在线。
package handler

import (
	"crypto/hmac"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/alerting"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// AlertWebhookSecretEnv 共享密钥环境变量名（Alertmanager http_headers 同名头）。
const AlertWebhookSecretEnv = "ALERT_WEBHOOK_SECRET"

// alertWebhookSecretHeader Alertmanager webhook 携带的共享密钥头。
const alertWebhookSecretHeader = "X-Alert-Webhook-Secret"

// AlertIngestService 是 handler 对 alerting.Service 的窄接口，便于测试注入 fake。
type AlertIngestService interface {
	Ingest(p *alerting.WebhookPayload) alerting.IngestResult
	Active(limit int) ([]*store.AlertEventRecord, error)
	History(limit int) ([]*store.AlertEventRecord, error)
}

// AlertIngestSvc 进程内告警接入服务，main 启动时注入；为 nil 时端点返回 503。
var AlertIngestSvc AlertIngestService

// SetAlertIngestService 注入告警接入服务（生产接 alerting.Service）。
func SetAlertIngestService(s AlertIngestService) { AlertIngestSvc = s }

var alertSecretWarnOnce sync.Once

// WarnIfAlertWebhookSecretMissing 在未配置 ALERT_WEBHOOK_SECRET 时打印一次启动
// 警告：/api/alerts/webhook 将 fail-closed（503），告警推送链路不生效。
func WarnIfAlertWebhookSecretMissing() {
	if os.Getenv(AlertWebhookSecretEnv) == "" {
		alertSecretWarnOnce.Do(func() {
			log.Printf("[WARN] %s 未配置：/api/alerts/webhook 将拒绝所有推送（503），Alertmanager 告警链路不生效。", AlertWebhookSecretEnv)
		})
	}
}

// AlertsWebhook 接收 Alertmanager v4 webhook。
//
// POST /api/alerts/webhook
// Header: X-Alert-Webhook-Secret: <ALERT_WEBHOOK_SECRET>
// Body: Alertmanager v4 载荷（alerts[] 逐条去重推送）。
func AlertsWebhook(c *gin.Context) {
	secret := os.Getenv(AlertWebhookSecretEnv)
	if secret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alert webhook not configured (ALERT_WEBHOOK_SECRET unset)"})
		return
	}
	got := c.GetHeader(alertWebhookSecretHeader)
	if got == "" || !hmac.Equal([]byte(got), []byte(secret)) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing alert webhook secret"})
		return
	}
	if AlertIngestSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alert ingest service unavailable"})
		return
	}

	var payload alerting.WebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid alertmanager payload: " + err.Error()})
		return
	}

	res := AlertIngestSvc.Ingest(&payload)
	c.JSON(http.StatusOK, res)
}

// alertEventDTO 是告警事件的 API 视图（labels/annotations 由 JSON 列解出）。
type alertEventDTO struct {
	Fingerprint string            `json:"fingerprint"`
	AlertName   string            `json:"alertname"`
	Status      string            `json:"status"`
	Severity    string            `json:"severity"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	StartsAt    int64             `json:"starts_at"`
	EndsAt      int64             `json:"ends_at"`
	FirstSeen   int64             `json:"first_seen"`
	LastSeen    int64             `json:"last_seen"`
}

func toAlertEventDTOs(recs []*store.AlertEventRecord) []alertEventDTO {
	out := make([]alertEventDTO, 0, len(recs))
	for _, r := range recs {
		dto := alertEventDTO{
			Fingerprint: r.Fingerprint,
			AlertName:   r.AlertName,
			Status:      r.Status,
			Severity:    r.Severity,
			Summary:     r.Summary,
			Description: r.Description,
			Labels:      map[string]string{},
			StartsAt:    r.StartsAt,
			EndsAt:      r.EndsAt,
			FirstSeen:   r.FirstSeen,
			LastSeen:    r.LastSeen,
		}
		if r.LabelsJSON != "" {
			_ = json.Unmarshal([]byte(r.LabelsJSON), &dto.Labels)
		}
		out = append(out, dto)
	}
	return out
}

// AlertsActive 返回当前仍在 firing 的告警（已推 firing 且未推 resolved）。
//
// GET /api/alerts/active
func AlertsActive(c *gin.Context) {
	if AlertIngestSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alert ingest service unavailable"})
		return
	}
	recs, err := AlertIngestSvc.Active(100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alerts": toAlertEventDTOs(recs)})
}

// AlertsHistory 返回最近告警流水（firing 与 resolved 均在列，last_seen 倒序）。
//
// GET /api/alerts/history?limit=50
func AlertsHistory(c *gin.Context) {
	if AlertIngestSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alert ingest service unavailable"})
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = min(v, 500)
		}
	}
	recs, err := AlertIngestSvc.History(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alerts": toAlertEventDTOs(recs)})
}
