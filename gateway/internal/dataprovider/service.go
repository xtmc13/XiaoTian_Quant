package dataprovider

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ── 源抽象 ─────────────────────────────────────────────────────

// Source 是一个外部数据适配器。Fetch 返回的数据必须可 JSON 序列化。
type Source interface {
	Name() string               // 源 id，如 "fear_greed"
	Description() string        // 展示用一句话说明
	TTL() time.Duration         // 缓存有效期 / 调度刷新周期
	MinInterval() time.Duration // 两次上游请求的最小间隔（限流）
	RequiresKey() bool          // 是否需要 API key
	Configured() bool           // key 齐备（免 key 源恒为 true）
	Fetch(ctx context.Context) (any, error)
}

// CacheStore 缓存落地接口（由 store 包的 SQLite repo 实现；测试用内存 fake）。
type CacheStore interface {
	UpsertDataProviderCache(source, cacheKey, payload string, fetchedAt, expiresAt int64) error
	GetDataProviderCache(source, cacheKey string) (payload string, fetchedAt, expiresAt int64, err error)
}

// ── 结果与健康 ─────────────────────────────────────────────────

// Result 是 API 返回统一包：status=ok|stale|not_configured|unavailable|refreshing。
// stale 表示上游失败/限流，返回的是过期缓存；unavailable 表示没有任何数据可取。
type Result struct {
	Source    string `json:"source"`
	Status    string `json:"status"`
	Data      any    `json:"data,omitempty"`
	FetchedAt int64  `json:"fetched_at,omitempty"`
	AgeSec    int64  `json:"age_sec,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SourceHealth 是 /sources 的每源健康状态。
type SourceHealth struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Configured  bool   `json:"configured"`
	RequiresKey bool   `json:"requires_key"`
	State       string `json:"state"` // ok | stale | no_data | not_configured
	Circuit     string `json:"circuit"`
	Failures    int    `json:"failures"`
	LastSuccess int64  `json:"last_success,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	TTLSec      int64  `json:"ttl_sec"`
}

// ── Service ────────────────────────────────────────────────────

type sourceSlot struct {
	src        Source
	limiter    *rateLimiter
	breaker    *circuitBreaker
	mu         sync.RWMutex
	data       any // 最近一次成功数据（内存缓存）
	cachedAt   time.Time
	lastErr    string
	hydrated   bool // 已尝试过从 DB 回灌
	refreshing atomic.Int32
}

// Service 管理所有数据源：调度周期刷新、限流/熔断/缓存、健康上报。
type Service struct {
	sources  map[string]*sourceSlot
	order    []string // 稳定输出顺序
	store    CacheStore
	secrets  []string // 用于错误脱敏的密钥值
	now      func() time.Time
	logger   *log.Logger
	interval time.Duration // 调度 tick 周期

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewService 组装服务。store 可为 nil（纯内存缓存，重启冷启动）。
func NewService(srcs []Source, store CacheStore, secrets []string) *Service {
	now := func() time.Time { return time.Now() }
	s := &Service{
		sources:  make(map[string]*sourceSlot, len(srcs)),
		store:    store,
		secrets:  secrets,
		now:      now,
		logger:   log.New(os.Stderr, "[dataprovider] ", log.LstdFlags),
		interval: 30 * time.Second,
	}
	for _, src := range srcs {
		s.sources[src.Name()] = &sourceSlot{
			src:     src,
			limiter: newRateLimiter(src.MinInterval(), now),
			breaker: newCircuitBreaker(3, 5*time.Minute, now),
		}
		s.order = append(s.order, src.Name())
	}
	return s
}

// SetLogger 覆盖默认 logger（测试可丢弃输出）。
func (s *Service) SetLogger(l *log.Logger) { s.logger = l }

// SourceNames 返回已注册源 id（稳定顺序）。
func (s *Service) SourceNames() []string { return append([]string(nil), s.order...) }

func (s *Service) sanitize(msg string) string { return sanitizeSecrets(msg, s.secrets) }

// ── 取数主流程 ────────────────────────────────────────────────

// Get 返回某源的最新数据。优先内存缓存；过期则后台异步刷新并立即回旧数据
// （stale）；完全没有数据时同步拉一次（首屏体验），失败回 unavailable。
// 未配置 key 的源直接回 not_configured，绝不打上游。
func (s *Service) Get(ctx context.Context, name string) Result {
	slot, ok := s.sources[name]
	if !ok {
		return Result{Source: name, Status: "unknown_source"}
	}
	if !slot.src.Configured() {
		return Result{Source: name, Status: "not_configured"}
	}

	// 1. 内存缓存未过期 → ok
	slot.mu.RLock()
	data, cachedAt := slot.data, slot.cachedAt
	slot.mu.RUnlock()
	if data != nil && s.now().Sub(cachedAt) < slot.src.TTL() {
		return s.okResult(name, data, cachedAt)
	}

	// 2. 内存为空 → 先尝试 DB 回灌（重启后首请求不空）
	if data == nil {
		if d, at, okDb := s.hydrate(slot); okDb {
			data, cachedAt = d, at
		}
	}

	// 3. 有（过期）数据 → 后台异步刷新，立即回 stale
	if data != nil {
		s.kickRefresh(name, slot)
		res := s.okResult(name, data, cachedAt)
		if s.now().Sub(cachedAt) >= slot.src.TTL() {
			res.Status = "stale"
		}
		return res
	}

	// 4. 完全无数据 → 同步拉一次（熔断/限流拦截时回 unavailable/refreshing）
	if !slot.breaker.Allow() {
		return Result{Source: name, Status: "unavailable", Error: "circuit open: upstream suspended after repeated failures"}
	}
	if !slot.limiter.Allow() {
		return Result{Source: name, Status: "refreshing"}
	}
	if err := s.fetchAndStore(slot); err != nil {
		return Result{Source: name, Status: "unavailable", Error: err.Error()}
	}
	slot.mu.RLock()
	data, cachedAt = slot.data, slot.cachedAt
	slot.mu.RUnlock()
	return s.okResult(name, data, cachedAt)
}

func (s *Service) okResult(name string, data any, at time.Time) Result {
	age := int64(s.now().Sub(at).Seconds())
	if age < 0 {
		age = 0
	}
	return Result{Source: name, Status: "ok", Data: data, FetchedAt: at.Unix(), AgeSec: age}
}

// hydrate 从 DB 回灌缓存（payload 原样保留为 json.RawMessage，避免二次解析）。
func (s *Service) hydrate(slot *sourceSlot) (any, time.Time, bool) {
	slot.mu.Lock()
	if slot.hydrated {
		d, at := slot.data, slot.cachedAt
		slot.mu.Unlock()
		return d, at, d != nil
	}
	slot.hydrated = true
	slot.mu.Unlock()

	if s.store == nil {
		return nil, time.Time{}, false
	}
	payload, fetchedAtMs, _, err := s.store.GetDataProviderCache(slot.src.Name(), "default")
	if err != nil || payload == "" {
		return nil, time.Time{}, false
	}
	at := time.UnixMilli(fetchedAtMs)
	slot.mu.Lock()
	if slot.data == nil { // 不覆盖并发期间已刷新的内存值
		slot.data = json.RawMessage(payload)
		slot.cachedAt = at
	}
	d, at2 := slot.data, slot.cachedAt
	slot.mu.Unlock()
	return d, at2, true
}

// kickRefresh 后台刷新（去重），被熔断/限流拦截则本轮放弃。
func (s *Service) kickRefresh(name string, slot *sourceSlot) {
	if !slot.refreshing.CompareAndSwap(0, 1) {
		return
	}
	go func() {
		defer slot.refreshing.Store(0)
		if !slot.breaker.Allow() || !slot.limiter.Allow() {
			return
		}
		if err := s.fetchAndStore(slot); err != nil {
			s.logger.Printf("%s refresh failed: %s", name, err.Error())
		}
	}()
}

// fetchAndStore 调上游 → 更新内存 → 落库。错误已脱敏。
func (s *Service) fetchAndStore(slot *sourceSlot) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, err := slot.src.Fetch(ctx)
	if err != nil {
		slot.breaker.RecordFailure()
		msg := s.sanitize(err.Error())
		slot.mu.Lock()
		slot.lastErr = msg
		slot.mu.Unlock()
		return &FetchError{Source: slot.src.Name(), Msg: msg}
	}
	slot.breaker.RecordSuccess()
	at := s.now()
	slot.mu.Lock()
	slot.data = data
	slot.cachedAt = at
	slot.lastErr = ""
	slot.mu.Unlock()

	if s.store != nil {
		if payload, mErr := json.Marshal(data); mErr == nil {
			expires := at.Add(slot.src.TTL() * 24).UnixMilli() // 落库保留 24×TTL，供 stale 兜底
			if dbErr := s.store.UpsertDataProviderCache(slot.src.Name(), "default", string(payload), at.UnixMilli(), expires); dbErr != nil {
				s.logger.Printf("%s persist cache failed: %s", slot.src.Name(), s.sanitize(dbErr.Error()))
			}
		}
	}
	return nil
}

// FetchError 已脱敏的取数错误。
type FetchError struct{ Source, Msg string }

func (e *FetchError) Error() string { return e.Source + ": " + e.Msg }

// ── 健康状态 ──────────────────────────────────────────────────

// Health 返回所有源的健康状态（/sources 端点）。
func (s *Service) Health() []SourceHealth {
	out := make([]SourceHealth, 0, len(s.order))
	for _, name := range s.order {
		slot := s.sources[name]
		slot.mu.RLock()
		data, cachedAt, lastErr := slot.data, slot.cachedAt, slot.lastErr
		slot.mu.RUnlock()

		h := SourceHealth{
			Name:        name,
			Description: slot.src.Description(),
			Configured:  slot.src.Configured(),
			RequiresKey: slot.src.RequiresKey(),
			Circuit:     slot.breaker.State(),
			Failures:    slot.breaker.Failures(),
			LastError:   lastErr,
			TTLSec:      int64(slot.src.TTL().Seconds()),
		}
		switch {
		case !h.Configured:
			h.State = "not_configured"
		case data == nil:
			h.State = "no_data"
		case s.now().Sub(cachedAt) >= slot.src.TTL():
			h.State = "stale"
			h.LastSuccess = cachedAt.Unix()
		default:
			h.State = "ok"
			h.LastSuccess = cachedAt.Unix()
		}
		out = append(out, h)
	}
	return out
}

// ── 调度循环 ──────────────────────────────────────────────────

// Start 拉起周期刷新循环（启动先同步跑一轮，首轮无数据时 API 才有缓存可回）。
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

// Stop 优雅停止（等当前轮跑完）。
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Service) loop() {
	defer close(s.doneCh)
	s.refreshAll()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.refreshAll()
		}
	}
}

// refreshAll 把过期且配置齐全的源挨个刷新（限流 Wait 串行，熔断开闸跳过）。
func (s *Service) refreshAll() {
	for _, name := range s.order {
		select {
		case <-s.stopCh:
			return
		default:
		}
		slot := s.sources[name]
		if !slot.src.Configured() {
			continue
		}
		slot.mu.RLock()
		data, cachedAt := slot.data, slot.cachedAt
		slot.mu.RUnlock()
		needsFetch := data == nil || s.now().Sub(cachedAt) >= slot.src.TTL()
		// DB 里还有效（其它实例刚写过）就回灌而非打上游
		if needsFetch && data == nil {
			if _, at, okDb := s.hydrate(slot); okDb && s.now().Sub(at) < slot.src.TTL() {
				needsFetch = false
			}
		}
		if !needsFetch {
			continue
		}
		if !slot.breaker.Allow() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := slot.limiter.Wait(ctx)
		cancel()
		if err != nil {
			continue
		}
		if err := s.fetchAndStore(slot); err != nil {
			s.logger.Printf("%s scheduled refresh failed: %s", name, err.Error())
		}
	}
}

// RefreshNow 强制刷新某源（供测试与后续手动刷新端点用）。
func (s *Service) RefreshNow(name string) error {
	slot, ok := s.sources[name]
	if !ok {
		return &FetchError{Source: name, Msg: "unknown source"}
	}
	if !slot.src.Configured() {
		return &FetchError{Source: name, Msg: "not configured"}
	}
	return s.fetchAndStore(slot)
}

// ── 默认单例（main.go 接线，router.go 取用） ───────────────────

var (
	defaultSvc atomic.Value // *Service
)

// SetDefault 注册生产实例（main.go 在 store 就绪后调用）。
func SetDefault(s *Service) { defaultSvc.Store(s) }

// Default 返回生产实例；未接线时回退为纯内存环境变量配置实例
// （测试/独立进程场景，绝不返回 nil）。
func Default() *Service {
	if v := defaultSvc.Load(); v != nil {
		return v.(*Service)
	}
	cfg := LoadEnvConfig()
	s := NewService(BuildSources(cfg, nil), nil, cfg.Secrets())
	defaultSvc.Store(s)
	return s
}
