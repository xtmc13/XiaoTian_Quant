// Package alerts 实现指标信号告警扫描引擎（对标 QuantDinger
// indicator_signal_alerts）：周期拉取 active 告警任务，按 symbol+interval
// 批量取近期 K 线，用 alertexpr 求值，命中且过了冷却即经 notify.Manager
// 直接发送通知（不经路由表），并回写 last_triggered_at/last_value。
//
// 结构对照 reconcile.Service：Start 拉起调度循环（启动先跑一轮），Stop 优雅
// 退出；单任务失败只记日志，绝不影响扫描主循环与其它任务。周期默认 60s，
// 环境变量 ALERTS_SCAN_SEC 可调；每任务求值 bar 数默认 200，ALERTS_BARS 可调。
package alerts

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/alertexpr"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

const (
	// DefaultScanInterval 扫描主周期（ALERTS_SCAN_SEC 覆盖）。
	DefaultScanInterval = 60 * time.Second
	// DefaultBars 每次求值取用的近期 K 线根数（ALERTS_BARS 覆盖）。
	DefaultBars = 200
	// maxHistoryPerAlert 内存触发历史环形缓冲上限（per alert）。
	maxHistoryPerAlert = 50
	// notifyTagSource 通知 source 标签（前端/日志检索用）。
	notifyTagSource = "indicator_alert"
)

// AlertStore 是扫描引擎对仓库的窄接口（生产为 *store.IndicatorAlertRepo）。
type AlertStore interface {
	ListActive() ([]*store.IndicatorAlertRecord, error)
	GetByID(id string) (*store.IndicatorAlertRecord, error)
	MarkTriggered(id string, value float64, atMs int64) error
}

// KlineSource 是 K 线数据源窄接口（生产为 MarketKlineSource，测试为 mock）。
type KlineSource interface {
	RecentBars(symbol, interval string, limit int) ([]model.Bar, error)
}

// Notifier 是通知发送窄接口（生产为 *notify.Manager，测试为 mock）。
type Notifier interface {
	Send(msg notify.Message)
}

// RunResult 一次求值/触发结果（POST /alerts/:id/run 响应体）。
type RunResult struct {
	Matched              bool    `json:"matched"`
	Value                float64 `json:"value"`
	Triggered            bool    `json:"triggered"`                        // 实际发出通知（命中且过了冷却或 force）
	CooldownRemainingSec int64   `json:"cooldown_remaining_sec,omitempty"` // 未触发时距冷却结束的秒数
	Bars                 int     `json:"bars"`                             // 本次求值使用的 K 线根数
	Error                string  `json:"error,omitempty"`                  // 求值错误（表达式/数据问题）
	At                   int64   `json:"at"`                               // 执行时间（毫秒）
}

// TriggerRecord 一次命中触发记录（history 接口返回）。
type TriggerRecord struct {
	At       int64   `json:"at"`
	Symbol   string  `json:"symbol"`
	Interval string  `json:"interval"`
	Expr     string  `json:"expr"`
	Value    float64 `json:"value"`
}

// Service 告警扫描调度器。
type Service struct {
	repo   AlertStore
	src    KlineSource
	notify Notifier

	interval    time.Duration
	barsPerEval int

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	runMu   sync.Mutex
	lastRun time.Time
	lastMsg string

	logMu    sync.Mutex
	triggers map[string][]TriggerRecord
}

// NewService 组装扫描服务。interval<=0 / bars<=0 时取默认，
// 也可用环境变量 ALERTS_SCAN_SEC / ALERTS_BARS 预置（显式参数优先）。
func NewService(repo AlertStore, src KlineSource, n Notifier, interval time.Duration, bars int) *Service {
	if interval <= 0 {
		interval = envDurationSec("ALERTS_SCAN_SEC", DefaultScanInterval)
	}
	if bars <= 0 {
		bars = envInt("ALERTS_BARS", DefaultBars)
	}
	return &Service{
		repo:        repo,
		src:         src,
		notify:      n,
		interval:    interval,
		barsPerEval: bars,
		triggers:    make(map[string][]TriggerRecord),
	}
}

func envDurationSec(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// Start 启动调度循环（立即跑一轮，之后按周期跑）；进程退出前调 Stop。
func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()
	go s.loop()
}

// Stop 优雅停止：等当前轮跑完再返回。
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	done := s.doneCh
	s.mu.Unlock()
	<-done
}

// IsRunning 调度循环是否存活。
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Interval 当前扫描周期。
func (s *Service) Interval() time.Duration { return s.interval }

func (s *Service) loop() {
	defer close(s.doneCh)
	s.runAll()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runAll()
		}
	}
}

// runAll 跑一轮全量扫描；单任务 panic/错误只记日志。
func (s *Service) runAll() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[alerts] scan round panic: %v", r)
		}
	}()
	recs, err := s.repo.ListActive()
	if err != nil {
		log.Printf("[alerts] list active alerts failed: %v", err)
		return
	}
	s.scanBatch(recs, time.Now(), true)
	s.runMu.Lock()
	s.lastRun = time.Now()
	s.lastMsg = fmt.Sprintf("scanned %d alerts", len(recs))
	s.runMu.Unlock()
}

// scanBatch 按 symbol+interval 分组批量取 K 线并逐任务求值。
// respectCooldown=true 用于周期扫描；force 场景走 RunAlert。
func (s *Service) scanBatch(recs []*store.IndicatorAlertRecord, now time.Time, respectCooldown bool) {
	type key struct{ symbol, interval string }
	groups := make(map[key][]*store.IndicatorAlertRecord)
	for _, rec := range recs {
		groups[key{rec.Symbol, rec.Interval}] = append(groups[key{rec.Symbol, rec.Interval}], rec)
	}
	for k, group := range groups {
		bars, err := s.src.RecentBars(k.symbol, k.interval, s.barsPerEval)
		if err != nil {
			log.Printf("[alerts] fetch klines %s %s failed: %v", k.symbol, k.interval, err)
			continue
		}
		if len(bars) == 0 {
			log.Printf("[alerts] fetch klines %s %s: empty", k.symbol, k.interval)
			continue
		}
		for _, rec := range group {
			s.runOne(rec, bars, now, respectCooldown)
		}
	}
}

// runOne 对单个任务求值并按需触发通知；任何错误只记日志。
func (s *Service) runOne(rec *store.IndicatorAlertRecord, bars []model.Bar, now time.Time, respectCooldown bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[alerts] alert %s panic: %v", rec.ID, r)
		}
	}()
	res := s.evaluate(rec, bars, now, respectCooldown)
	if res.Error != "" {
		log.Printf("[alerts] alert %s (%s) eval failed: %s", rec.ID, rec.Name, res.Error)
		return
	}
	if res.Triggered {
		log.Printf("[alerts] alert %s (%s) triggered: %s %s value=%v",
			rec.ID, rec.Name, rec.Symbol, rec.ConditionExpr, res.Value)
	}
}

// evaluate 求值单个任务（核心逻辑，便于测试）。命中且冷却通过才通知并落库。
func (s *Service) evaluate(rec *store.IndicatorAlertRecord, bars []model.Bar, now time.Time, respectCooldown bool) *RunResult {
	res := &RunResult{Bars: len(bars), At: now.UnixMilli()}
	expr, err := alertexpr.Parse(rec.ConditionExpr)
	if err != nil {
		res.Error = "parse: " + err.Error()
		return res
	}
	er, err := expr.Eval(bars)
	if err != nil {
		res.Error = "eval: " + err.Error()
		return res
	}
	res.Matched = er.Matched
	res.Value = er.Value
	if !er.Matched {
		return res
	}
	if respectCooldown && !cooldownPassed(rec.LastTriggeredAt, rec.CooldownMinutes, now) {
		remain := (int64(rec.CooldownMinutes)*60_000 - (now.UnixMilli() - rec.LastTriggeredAt)) / 1000
		if remain < 0 {
			remain = 0
		}
		res.CooldownRemainingSec = remain
		return res
	}
	s.notify.Send(notify.Message{
		Title:   notifyTitle(rec),
		Content: notifyContent(rec, er.Value),
		Level:   "WARN",
		Tags: map[string]string{
			"source":   notifyTagSource,
			"alert_id": rec.ID,
			"symbol":   rec.Symbol,
			"interval": rec.Interval,
		},
	})
	if s.repo != nil {
		if err := s.repo.MarkTriggered(rec.ID, er.Value, now.UnixMilli()); err != nil {
			log.Printf("[alerts] alert %s mark triggered failed: %v", rec.ID, err)
		}
	}
	res.Triggered = true
	s.appendTrigger(rec, er.Value, now)
	return res
}

// RunAlert 立即执行一次任务（POST /alerts/:id/run）。
// respectCooldown=false（force）时忽略冷却，但仍通知并刷新冷却起点。
func (s *Service) RunAlert(id string, respectCooldown bool) (*RunResult, error) {
	rec, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("alert not found")
	}
	bars, err := s.src.RecentBars(rec.Symbol, rec.Interval, s.barsPerEval)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("no klines for %s %s", rec.Symbol, rec.Interval)
	}
	return s.evaluate(rec, bars, time.Now(), respectCooldown), nil
}

// History 返回任务的最近触发记录（内存环形，最新在前；进程重启后为空，
// 由 handler 层用 last_triggered_at 兜底展示）。
func (s *Service) History(id string, limit int) []TriggerRecord {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entries := s.triggers[id]
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	out := make([]TriggerRecord, 0, limit)
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, entries[i])
	}
	return out
}

func (s *Service) appendTrigger(rec *store.IndicatorAlertRecord, value float64, now time.Time) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.triggers[rec.ID] = append(s.triggers[rec.ID], TriggerRecord{
		At:       now.UnixMilli(),
		Symbol:   rec.Symbol,
		Interval: rec.Interval,
		Expr:     rec.ConditionExpr,
		Value:    value,
	})
	if n := len(s.triggers[rec.ID]); n > maxHistoryPerAlert {
		s.triggers[rec.ID] = s.triggers[rec.ID][n-maxHistoryPerAlert:]
	}
}

// LastRun 返回最近一轮扫描时间与摘要（状态展示用）。
func (s *Service) LastRun() (time.Time, string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.lastRun, s.lastMsg
}

// cooldownPassed 冷却判定：从未触发（lastTriggeredAt=0）或冷却分钟数为 0
// 视为通过；否则 now-last >= cooldown 分钟。
func cooldownPassed(lastTriggeredAt int64, cooldownMin int, now time.Time) bool {
	if lastTriggeredAt <= 0 || cooldownMin <= 0 {
		return true
	}
	return now.UnixMilli()-lastTriggeredAt >= int64(cooldownMin)*60_000
}

// notifyTitle 通知标题 = 任务名。
func notifyTitle(rec *store.IndicatorAlertRecord) string {
	if rec.Name != "" {
		return rec.Name
	}
	return fmt.Sprintf("指标告警: %s %s", rec.Symbol, rec.Interval)
}

// notifyContent 通知正文 = symbol/interval + 表达式 + 当前值 + 自定义附言。
func notifyContent(rec *store.IndicatorAlertRecord, value float64) string {
	content := fmt.Sprintf("%s %s 命中告警条件\n表达式: %s\n当前值: %s",
		rec.Symbol, rec.Interval, rec.ConditionExpr, formatValue(value))
	if rec.Message != "" {
		content += "\n" + rec.Message
	}
	return content
}

// formatValue 紧凑展示数值（整数不带小数，最多 6 位有效小数）。
func formatValue(v float64) string {
	s := strconv.FormatFloat(v, 'f', 6, 64)
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}
