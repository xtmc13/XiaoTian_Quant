// Package alerting 接收 Alertmanager v4 webhook 告警并桥接到内部通知体系。
//
// 链路：Prometheus 评估 ops/prometheus/alerts.yml → Alertmanager 分组/路由 →
// POST /api/alerts/webhook（X-Alert-Webhook-Secret 共享密钥头）→ 本包按
// fingerprint 去重 → notify.Broadcaster 路由到已配置渠道（邮件/飞书/钉钉/
// Telegram/Discord/短信），同时落库 xt_alert_events（迁移 0033）支撑
// /api/alerts/active 与 /api/alerts/history。
//
// 去重语义（fingerprint 级状态机）：
//   - firing：同 fingerprint 首次（或上一事故已 resolved）→ 推送并标记
//     notified_firing=1；firing 期间重复到达只刷新 last_seen，不重复推送。
//   - resolved：仅当该 fingerprint 已推过 firing 且未推过 resolved 时推送一次；
//     无 firing 记录的 resolved 只落库不推送（避免无头尾通知）。
//
// 所有权语义与 alerts.Service 一致：EventStore/Broadcaster 为窄接口，
// 生产实现为 store.AlertEventRepo 与 notify.Broadcaster，测试注入 fake。
package alerting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Alertmanager v4 Webhook 载荷 ────────────────────────────────
// 字段对齐 https://prometheus.io/docs/alerting/latest/configuration/#webhook_config

// WebhookPayload 是 Alertmanager v4 webhook 的顶层载荷。
type WebhookPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"` // firing | resolved（组级）
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

// Alert 是载荷中的单条告警。
type Alert struct {
	Status       string            `json:"status"` // firing | resolved
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// Fingerprint 返回告警去重键；Alertmanager 未携带 fingerprint 时按排序后的
// labelset 计算稳定哈希兜底（webhook 测试/手工 curl 场景）。
func (a Alert) FingerprintKey() string {
	if a.Fingerprint != "" {
		return a.Fingerprint
	}
	keys := make([]string, 0, len(a.Labels))
	for k := range a.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\x00", k, a.Labels[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// IngestResult 汇总一次 webhook 批次的处理结果（同时作为 API 响应体）。
type IngestResult struct {
	Received         int `json:"received"`
	NotifiedFiring   int `json:"notified_firing"`
	NotifiedResolved int `json:"notified_resolved"`
	Suppressed       int `json:"suppressed"` // 去重抑制（firing 重复 / 无头 resolved）
}

// ── 依赖窄接口 ──────────────────────────────────────────────────

// EventStore 是事件持久化窄接口（生产为 *store.AlertEventRepo）。
type EventStore interface {
	GetByFingerprint(fp string) (*store.AlertEventRecord, error)
	Upsert(rec *store.AlertEventRecord) error
	ListActive(limit int) ([]*store.AlertEventRecord, error)
	ListRecent(limit int) ([]*store.AlertEventRecord, error)
}

// Broadcaster 是通知广播窄接口（生产为 *notify.Broadcaster）。
type Broadcaster interface {
	Broadcast(tpl notify.Template)
}

// ── Service ─────────────────────────────────────────────────────

// dedupState 是内存态去重标记（DB 为持久副本，重启后回载）。
type dedupState struct {
	firingNotified   bool
	resolvedNotified bool
}

// Service 是告警接收/去重/广播服务。去重决策基于内存 map（写穿透到
// EventStore），DB 不可用或回载失败时退化为进程内去重，绝不影响推送。
type Service struct {
	store  EventStore
	bcast  Broadcaster
	mu     sync.Mutex
	mem    map[string]*dedupState
	loaded bool
}

// NewService 构造服务并尽力从 EventStore 回载去重状态（失败仅记日志）。
func NewService(st EventStore, b Broadcaster) *Service {
	s := &Service{store: st, bcast: b, mem: make(map[string]*dedupState)}
	s.loadLocked()
	return s
}

func (s *Service) loadLocked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.loaded = true
	if s.store == nil {
		return
	}
	recs, err := s.store.ListRecent(1000)
	if err != nil {
		log.Printf("[Alerting] 回载告警去重状态失败（退化为进程内去重）: %v", err)
		return
	}
	for _, r := range recs {
		s.mem[r.Fingerprint] = &dedupState{
			firingNotified:   r.NotifiedFiring,
			resolvedNotified: r.NotifiedResolved,
		}
	}
}

// Ingest 处理一批 Alertmanager 告警：逐条去重判定，命中推送经 notify 路由，
// 并写穿透持久化。返回批次统计。载荷为空或为 nil 时安全返回零值。
func (s *Service) Ingest(p *WebhookPayload) IngestResult {
	var res IngestResult
	if p == nil {
		return res
	}
	for i := range p.Alerts {
		res.Received++
		a := &p.Alerts[i]
		fp := a.FingerprintKey()
		severity := a.Labels["severity"]
		if severity == "" {
			severity = "info"
		}
		alertname := a.Labels["alertname"]
		if alertname == "" {
			alertname = "unknown"
		}
		summary := firstNonEmpty(a.Annotations["summary"], commonAnnotation(p, "summary"), alertname)
		description := firstNonEmpty(a.Annotations["description"], commonAnnotation(p, "description"))
		runbook := firstNonEmpty(a.Annotations["runbook"], commonAnnotation(p, "runbook"))

		notifyNow := false
		var firingNotified, resolvedNotified bool
		s.mu.Lock()
		st := s.mem[fp]
		if st == nil {
			st = &dedupState{}
			s.mem[fp] = st
		}
		switch strings.ToLower(a.Status) {
		case "firing":
			if st.firingNotified && !st.resolvedNotified {
				res.Suppressed++ // firing 期间同 fingerprint 不重复推送
			} else {
				// 新事故（无记录或上一事故已 resolved）
				st.firingNotified = true
				st.resolvedNotified = false
				notifyNow = true
				res.NotifiedFiring++
			}
		case "resolved":
			if st.firingNotified && !st.resolvedNotified {
				st.resolvedNotified = true
				notifyNow = true
				res.NotifiedResolved++
			} else {
				res.Suppressed++ // 无头 resolved / 重复 resolved
			}
		default:
			res.Suppressed++ // 未知状态只落库
		}
		firingNotified, resolvedNotified = st.firingNotified, st.resolvedNotified
		s.mu.Unlock()

		rec := &store.AlertEventRecord{
			Fingerprint:      fp,
			AlertName:        alertname,
			Status:           strings.ToLower(a.Status),
			Severity:         strings.ToLower(severity),
			Summary:          summary,
			Description:      description,
			LabelsJSON:       marshalLabels(a.Labels),
			AnnotationsJSON:  marshalLabels(a.Annotations),
			StartsAt:         a.StartsAt.UnixMilli(),
			EndsAt:           a.EndsAt.UnixMilli(),
			NotifiedFiring:   firingNotified,
			NotifiedResolved: resolvedNotified,
			LastSeen:         time.Now().UnixMilli(),
		}
		s.persist(fp, rec)

		if notifyNow && s.bcast != nil {
			s.bcast.Broadcast(notify.NewAlertTemplate(alertname, severity, rec.Status, summary, description, runbook, a.Labels))
		}
	}
	return res
}

// commonAnnotation 取组级公共注解（Alertmanager 组告警场景下告警级注解
// 可能缺失，摘要/描述常在 commonAnnotations 上）。
func commonAnnotation(p *WebhookPayload, key string) string {
	if p == nil {
		return ""
	}
	return p.CommonAnnotations[key]
}

// persist 写穿透持久化；first_seen 仅在首条记录时落定。DB 故障只记日志，
// 不失败 webhook（否则 Alertmanager 重试会造成重复推送）。
func (s *Service) persist(fp string, rec *store.AlertEventRecord) {
	if s.store == nil {
		return
	}
	old, err := s.store.GetByFingerprint(fp)
	if err != nil {
		log.Printf("[Alerting] 读取告警事件失败 fp=%s: %v", fp, err)
	}
	if old != nil {
		rec.FirstSeen = old.FirstSeen
		rec.CreatedAt = old.CreatedAt
	} else {
		rec.FirstSeen = rec.LastSeen
	}
	if err := s.store.Upsert(rec); err != nil {
		log.Printf("[Alerting] 落库告警事件失败 fp=%s: %v", fp, err)
	}
}

// Active 返回当前仍在 firing 的告警（供 GET /api/alerts/active）。
func (s *Service) Active(limit int) ([]*store.AlertEventRecord, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.ListActive(limit)
}

// History 返回最近告警流水（供 GET /api/alerts/history）。
func (s *Service) History(limit int) ([]*store.AlertEventRecord, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.ListRecent(limit)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func marshalLabels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}
