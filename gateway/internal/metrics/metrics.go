package metrics

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Metric Types ──

type Counter struct {
	name   string
	help   string
	labels []string
	values map[string]*int64
	mu     sync.RWMutex
}

func NewCounter(name, help string, labels ...string) *Counter {
	return &Counter{
		name:   name,
		help:   help,
		labels: labels,
		values: make(map[string]*int64),
	}
}

func (c *Counter) Inc(labelValues ...string) {
	key := c.key(labelValues)
	c.mu.Lock()
	v, ok := c.values[key]
	if !ok {
		v = new(int64)
		c.values[key] = v
	}
	c.mu.Unlock()
	atomic.AddInt64(v, 1)
}

func (c *Counter) Add(delta float64, labelValues ...string) {
	key := c.key(labelValues)
	c.mu.Lock()
	v, ok := c.values[key]
	if !ok {
		v = new(int64)
		c.values[key] = v
	}
	c.mu.Unlock()
	atomic.AddInt64(v, int64(delta))
}

func (c *Counter) key(labelValues []string) string {
	if len(c.labels) == 0 {
		return "_"
	}
	parts := make([]string, 0, len(c.labels))
	for i, l := range c.labels {
		v := ""
		if i < len(labelValues) {
			v = labelValues[i]
		}
		parts = append(parts, fmt.Sprintf("%s=%q", l, v))
	}
	return strings.Join(parts, ",")
}

func (c *Counter) String() string {
	var b strings.Builder
	if c.help != "" {
		fmt.Fprintf(&b, "# HELP %s %s\n", c.name, c.help)
	}
	fmt.Fprintf(&b, "# TYPE %s counter\n", c.name)
	c.mu.RLock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	c.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		c.mu.RLock()
		v := atomic.LoadInt64(c.values[k])
		c.mu.RUnlock()
		if k == "_" {
			fmt.Fprintf(&b, "%s %d\n", c.name, v)
		} else {
			fmt.Fprintf(&b, "%s{%s} %d\n", c.name, k, v)
		}
	}
	return b.String()
}

// FloatCounter 浮点累加计数器（成交量等小数数量场景），输出为 counter 类型。
type FloatCounter struct {
	name   string
	help   string
	labels []string
	values map[string]*int64 // milli 存储
	mu     sync.RWMutex
}

func NewFloatCounter(name, help string, labels ...string) *FloatCounter {
	return &FloatCounter{name: name, help: help, labels: labels, values: make(map[string]*int64)}
}

func (c *FloatCounter) Add(delta float64, labelValues ...string) {
	key := c.key(labelValues)
	c.mu.Lock()
	v, ok := c.values[key]
	if !ok {
		v = new(int64)
		c.values[key] = v
	}
	c.mu.Unlock()
	atomic.AddInt64(v, int64(delta*1000))
}

func (c *FloatCounter) key(labelValues []string) string {
	if len(c.labels) == 0 {
		return "_"
	}
	parts := make([]string, 0, len(c.labels))
	for i, l := range c.labels {
		v := ""
		if i < len(labelValues) {
			v = labelValues[i]
		}
		parts = append(parts, fmt.Sprintf("%s=%q", l, v))
	}
	return strings.Join(parts, ",")
}

func (c *FloatCounter) String() string {
	var b strings.Builder
	if c.help != "" {
		fmt.Fprintf(&b, "# HELP %s %s\n", c.name, c.help)
	}
	fmt.Fprintf(&b, "# TYPE %s counter\n", c.name)
	c.mu.RLock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	c.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		c.mu.RLock()
		v := atomic.LoadInt64(c.values[k])
		c.mu.RUnlock()
		if k == "_" {
			fmt.Fprintf(&b, "%s %.3f\n", c.name, float64(v)/1000.0)
		} else {
			fmt.Fprintf(&b, "%s{%s} %.3f\n", c.name, k, float64(v)/1000.0)
		}
	}
	return b.String()
}

type Gauge struct {
	name   string
	help   string
	labels []string
	values map[string]*int64
	mu     sync.RWMutex
}

func NewGauge(name, help string, labels ...string) *Gauge {
	return &Gauge{
		name:   name,
		help:   help,
		labels: labels,
		values: make(map[string]*int64),
	}
}

func (g *Gauge) Set(val float64, labelValues ...string) {
	key := g.key(labelValues)
	g.mu.Lock()
	v, ok := g.values[key]
	if !ok {
		v = new(int64)
		g.values[key] = v
	}
	g.mu.Unlock()
	atomic.StoreInt64(v, int64(val*1000)) // store as milli
}

// Add 对仪表值做增量修改（WS 连接数等增减型场景）。
func (g *Gauge) Add(delta float64, labelValues ...string) {
	key := g.key(labelValues)
	g.mu.Lock()
	v, ok := g.values[key]
	if !ok {
		v = new(int64)
		g.values[key] = v
	}
	g.mu.Unlock()
	atomic.AddInt64(v, int64(delta*1000))
}

func (g *Gauge) key(labelValues []string) string {
	if len(g.labels) == 0 {
		return "_"
	}
	parts := make([]string, 0, len(g.labels))
	for i, l := range g.labels {
		v := ""
		if i < len(labelValues) {
			v = labelValues[i]
		}
		parts = append(parts, fmt.Sprintf("%s=%q", l, v))
	}
	return strings.Join(parts, ",")
}

func (g *Gauge) String() string {
	var b strings.Builder
	if g.help != "" {
		fmt.Fprintf(&b, "# HELP %s %s\n", g.name, g.help)
	}
	fmt.Fprintf(&b, "# TYPE %s gauge\n", g.name)
	g.mu.RLock()
	keys := make([]string, 0, len(g.values))
	for k := range g.values {
		keys = append(keys, k)
	}
	g.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		g.mu.RLock()
		v := atomic.LoadInt64(g.values[k])
		g.mu.RUnlock()
		val := float64(v) / 1000.0
		if k == "_" {
			fmt.Fprintf(&b, "%s %.3f\n", g.name, val)
		} else {
			fmt.Fprintf(&b, "%s{%s} %.3f\n", g.name, k, val)
		}
	}
	return b.String()
}

type Histogram struct {
	name    string
	help    string
	labels  []string
	buckets []float64
	counts  map[string][]*int64
	sums    map[string]*int64
	mu      sync.RWMutex
}

func NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	if len(buckets) == 0 {
		buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	}
	return &Histogram{
		name:    name,
		help:    help,
		labels:  labels,
		buckets: buckets,
		counts:  make(map[string][]*int64),
		sums:    make(map[string]*int64),
	}
}

func (h *Histogram) Observe(val float64, labelValues ...string) {
	key := h.key(labelValues)
	h.mu.Lock()
	counts, ok := h.counts[key]
	if !ok {
		counts = make([]*int64, len(h.buckets)+1)
		for i := range counts {
			counts[i] = new(int64)
		}
		h.counts[key] = counts
		h.sums[key] = new(int64)
	}
	h.mu.Unlock()
	for i, b := range h.buckets {
		if val <= b {
			atomic.AddInt64(counts[i], 1)
		}
	}
	atomic.AddInt64(counts[len(h.buckets)], 1) // +Inf bucket
	atomic.AddInt64(h.sums[key], int64(val*1000))
}

func (h *Histogram) key(labelValues []string) string {
	if len(h.labels) == 0 {
		return "_"
	}
	parts := make([]string, 0, len(h.labels))
	for i, l := range h.labels {
		v := ""
		if i < len(labelValues) {
			v = labelValues[i]
		}
		parts = append(parts, fmt.Sprintf("%s=%q", l, v))
	}
	return strings.Join(parts, ",")
}

func (h *Histogram) String() string {
	var b strings.Builder
	if h.help != "" {
		fmt.Fprintf(&b, "# HELP %s %s\n", h.name, h.help)
	}
	fmt.Fprintf(&b, "# TYPE %s histogram\n", h.name)
	h.mu.RLock()
	keys := make([]string, 0, len(h.counts))
	for k := range h.counts {
		keys = append(keys, k)
	}
	h.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		h.mu.RLock()
		counts := h.counts[k]
		sum := atomic.LoadInt64(h.sums[k])
		h.mu.RUnlock()
		for i, bucket := range h.buckets {
			c := atomic.LoadInt64(counts[i])
			if k == "_" {
				fmt.Fprintf(&b, "%s_bucket{le=%q} %d\n", h.name, fmt.Sprintf("%.3f", bucket), c)
			} else {
				fmt.Fprintf(&b, "%s_bucket{%s,le=%q} %d\n", h.name, k, fmt.Sprintf("%.3f", bucket), c)
			}
		}
		inf := atomic.LoadInt64(counts[len(h.buckets)])
		if k == "_" {
			fmt.Fprintf(&b, "%s_bucket{le=\"+Inf\"} %d\n", h.name, inf)
			fmt.Fprintf(&b, "%s_sum %.3f\n", h.name, float64(sum)/1000.0)
			fmt.Fprintf(&b, "%s_count %d\n", h.name, inf)
		} else {
			fmt.Fprintf(&b, "%s_bucket{%s,le=\"+Inf\"} %d\n", h.name, k, inf)
			fmt.Fprintf(&b, "%s_sum{%s} %.3f\n", h.name, k, float64(sum)/1000.0)
			fmt.Fprintf(&b, "%s_count{%s} %d\n", h.name, k, inf)
		}
	}
	return b.String()
}

// ── Registry ──

type Registry struct {
	counters      map[string]*Counter
	floatCounters map[string]*FloatCounter
	gauges        map[string]*Gauge
	histograms    map[string]*Histogram
	mu            sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		counters:      make(map[string]*Counter),
		floatCounters: make(map[string]*FloatCounter),
		gauges:        make(map[string]*Gauge),
		histograms:    make(map[string]*Histogram),
	}
}

func (r *Registry) RegisterCounter(c *Counter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[c.name] = c
}

func (r *Registry) RegisterFloatCounter(c *FloatCounter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.floatCounters[c.name] = c
}

func (r *Registry) RegisterGauge(g *Gauge) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[g.name] = g
}

func (r *Registry) RegisterHistogram(h *Histogram) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.histograms[h.name] = h
}

func (r *Registry) String() string {
	var b strings.Builder
	r.mu.RLock()
	cNames := make([]string, 0, len(r.counters))
	for n := range r.counters {
		cNames = append(cNames, n)
	}
	fcNames := make([]string, 0, len(r.floatCounters))
	for n := range r.floatCounters {
		fcNames = append(fcNames, n)
	}
	gNames := make([]string, 0, len(r.gauges))
	for n := range r.gauges {
		gNames = append(gNames, n)
	}
	hNames := make([]string, 0, len(r.histograms))
	for n := range r.histograms {
		hNames = append(hNames, n)
	}
	r.mu.RUnlock()

	sort.Strings(cNames)
	sort.Strings(fcNames)
	sort.Strings(gNames)
	sort.Strings(hNames)

	for _, n := range cNames {
		r.mu.RLock()
		c := r.counters[n]
		r.mu.RUnlock()
		b.WriteString(c.String())
	}
	for _, n := range fcNames {
		r.mu.RLock()
		c := r.floatCounters[n]
		r.mu.RUnlock()
		b.WriteString(c.String())
	}
	for _, n := range gNames {
		r.mu.RLock()
		g := r.gauges[n]
		r.mu.RUnlock()
		b.WriteString(g.String())
	}
	for _, n := range hNames {
		r.mu.RLock()
		h := r.histograms[n]
		r.mu.RUnlock()
		b.WriteString(h.String())
	}
	return b.String()
}

// ── Go Runtime Metrics ──

// startTime 进程启动时间，用于 uptime 指标。
var startTime = time.Now()

func runtimeMetrics() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP go_goroutines Number of goroutines\n")
	fmt.Fprintf(&b, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(&b, "go_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(&b, "# HELP process_uptime_seconds Process uptime in seconds\n")
	fmt.Fprintf(&b, "# TYPE process_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "process_uptime_seconds %.0f\n", time.Since(startTime).Seconds())
	fmt.Fprintf(&b, "# HELP process_start_time_seconds Process start time (unix seconds)\n")
	fmt.Fprintf(&b, "# TYPE process_start_time_seconds gauge\n")
	fmt.Fprintf(&b, "process_start_time_seconds %d\n", startTime.Unix())
	fmt.Fprintf(&b, "# HELP go_gc_cycles_total Number of completed GC cycles\n")
	fmt.Fprintf(&b, "# TYPE go_gc_cycles_total counter\n")
	fmt.Fprintf(&b, "go_gc_cycles_total %d\n", m.NumGC)
	fmt.Fprintf(&b, "# HELP go_memstats_alloc_bytes Bytes allocated and still in use\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_alloc_bytes %d\n", m.HeapAlloc)
	fmt.Fprintf(&b, "# HELP go_memstats_sys_bytes Bytes obtained from system\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_sys_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_sys_bytes %d\n", m.HeapSys)
	fmt.Fprintf(&b, "# HELP go_memstats_heap_alloc_bytes Bytes allocated and still in use\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_alloc_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_heap_alloc_bytes %d\n", m.HeapAlloc)
	fmt.Fprintf(&b, "# HELP go_memstats_heap_sys_bytes Bytes obtained from system for heap\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_sys_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_heap_sys_bytes %d\n", m.HeapSys)
	fmt.Fprintf(&b, "# HELP go_memstats_heap_idle_bytes Bytes in idle spans\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_idle_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_heap_idle_bytes %d\n", m.HeapIdle)
	fmt.Fprintf(&b, "# HELP go_memstats_heap_inuse_bytes Bytes in non-idle spans\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_inuse_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_heap_inuse_bytes %d\n", m.HeapInuse)
	fmt.Fprintf(&b, "# HELP go_memstats_heap_released_bytes Bytes released to OS\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_released_bytes gauge\n")
	fmt.Fprintf(&b, "go_memstats_heap_released_bytes %d\n", m.HeapReleased)
	fmt.Fprintf(&b, "# HELP go_memstats_heap_objects Number of allocated objects\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_objects gauge\n")
	fmt.Fprintf(&b, "go_memstats_heap_objects %d\n", m.HeapObjects)
	fmt.Fprintf(&b, "# HELP go_memstats_gc_cpu_fraction Fraction of CPU time used by GC\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_gc_cpu_fraction gauge\n")
	fmt.Fprintf(&b, "go_memstats_gc_cpu_fraction %.6f\n", m.GCCPUFraction)
	fmt.Fprintf(&b, "# HELP go_memstats_last_gc_time_seconds Time of last GC\n")
	fmt.Fprintf(&b, "# TYPE go_memstats_last_gc_time_seconds gauge\n")
	fmt.Fprintf(&b, "go_memstats_last_gc_time_seconds %.3f\n", float64(m.LastGC)/1e9)
	return b.String()
}

// ── HTTP Handler ──

var globalRegistry = NewRegistry()

func GetRegistry() *Registry { return globalRegistry }

func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(globalRegistry.String()))
	w.Write([]byte(runtimeMetrics()))
}

// ── Middleware ──

// HTTPMiddleware 包装 gin 引擎记录请求计数/延迟直方图（按 method+path+status 分桶）。
// 指标只注册一次（sync.Once），重复调用安全。
var httpMetricsOnce sync.Once

func HTTPMiddleware(next http.Handler) http.Handler {
	requestCounter := NewCounter("http_requests_total", "Total HTTP requests", "method", "path", "status")
	requestDuration := NewHistogram("http_request_duration_seconds", "HTTP request duration", []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, "method", "path", "status")
	httpMetricsOnce.Do(func() {
		globalRegistry.RegisterCounter(requestCounter)
		globalRegistry.RegisterHistogram(requestDuration)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Wrap writer to capture status code
		ww := &responseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(ww, r)
		dur := time.Since(start).Seconds()
		path := r.URL.Path
		if len(path) > 100 {
			path = path[:100]
		}
		status := fmt.Sprintf("%d", ww.statusCode)
		requestCounter.Inc(r.Method, path, status)
		requestDuration.Observe(dur, r.Method, path, status)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker for WebSocket support.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supported")
	}
	return hijacker.Hijack()
}

// ── Convenience ──
// 包级单例注册：重复 Register 会替换 Registry 中的实例导致计数清零，
// 因此这些便捷指标只注册一次，调用处仅 Inc/Set。

var (
	convenienceOnce sync.Once
	ordersTotal     *Counter
	signalsTotal    *Counter
	equityGauge     *Gauge
	positionsGauge  *Gauge
	strategiesGauge *Gauge
)

func ensureConvenienceMetrics() {
	convenienceOnce.Do(func() {
		ordersTotal = NewCounter("orders_total", "Total orders placed", "side", "status")
		signalsTotal = NewCounter("signals_total", "Total strategy signals", "strategy", "direction")
		equityGauge = NewGauge("portfolio_equity_usdt", "Current portfolio equity in USDT")
		positionsGauge = NewGauge("portfolio_positions", "Number of open positions")
		strategiesGauge = NewGauge("strategies_active", "Number of active strategies")
		globalRegistry.RegisterCounter(ordersTotal)
		globalRegistry.RegisterCounter(signalsTotal)
		globalRegistry.RegisterGauge(equityGauge)
		globalRegistry.RegisterGauge(positionsGauge)
		globalRegistry.RegisterGauge(strategiesGauge)
	})
}

func RecordOrder(side, status string) {
	ensureConvenienceMetrics()
	ordersTotal.Inc(side, status)
}

func RecordSignal(strategy, direction string) {
	ensureConvenienceMetrics()
	signalsTotal.Inc(strategy, direction)
}

func SetEquity(val float64) {
	ensureConvenienceMetrics()
	equityGauge.Set(val)
}

func SetPositionCount(n int) {
	ensureConvenienceMetrics()
	positionsGauge.Set(float64(n))
}

func SetActiveStrategies(n int) {
	ensureConvenienceMetrics()
	strategiesGauge.Set(float64(n))
}

// ── A9.1 Business Metrics ────────────────────────────────────────

var (
	businessOnce        sync.Once
	fillsTotal          *FloatCounter
	riskRejectionsTotal *Counter
	notifySendsTotal    *Counter
	wsConnectionsGauge  *Gauge
	botsRunningGauge    *Gauge
	reconcileDiffsTotal *Counter
)

func ensureBusinessMetrics() {
	businessOnce.Do(func() {
		fillsTotal = NewFloatCounter("fills_total", "Total filled quantity by side", "side")
		riskRejectionsTotal = NewCounter("risk_order_rejections_total", "Orders rejected by risk control")
		notifySendsTotal = NewCounter("notify_sends_total", "Notification deliveries by channel and result", "channel", "result")
		wsConnectionsGauge = NewGauge("ws_connections", "Current WebSocket client connections")
		botsRunningGauge = NewGauge("bots_running", "Running bots by type", "type")
		reconcileDiffsTotal = NewCounter("reconcile_diffs_total", "Reconcile diffs detected by type and exchange", "type", "exchange")
		globalRegistry.RegisterFloatCounter(fillsTotal)
		globalRegistry.RegisterCounter(riskRejectionsTotal)
		globalRegistry.RegisterCounter(notifySendsTotal)
		globalRegistry.RegisterGauge(wsConnectionsGauge)
		globalRegistry.RegisterGauge(botsRunningGauge)
		globalRegistry.RegisterCounter(reconcileDiffsTotal)
	})
}

// RecordFill 记录成交量（按方向累加数量）。
func RecordFill(side string, qty float64) {
	ensureBusinessMetrics()
	if qty <= 0 {
		return
	}
	fillsTotal.Add(qty, side)
}

// RecordRiskRejection 记录一次风控拒绝。
func RecordRiskRejection() {
	ensureBusinessMetrics()
	riskRejectionsTotal.Inc()
}

// RecordNotifySend 记录某渠道一次投递结果（result=success|failure）。
func RecordNotifySend(channel, result string) {
	ensureBusinessMetrics()
	notifySendsTotal.Inc(channel, result)
}

// SetWSConnections 设置当前 WS 连接数（ws 包连接/断开时调用）。
func SetWSConnections(n int) {
	ensureBusinessMetrics()
	wsConnectionsGauge.Set(float64(n))
}

// IncWSConnections / DecWSConnections WS 连接数增删（/ws 旧端点逐连接调用）。
func IncWSConnections() {
	ensureBusinessMetrics()
	wsConnectionsGauge.Add(1)
}

func DecWSConnections() {
	ensureBusinessMetrics()
	wsConnectionsGauge.Add(-1)
}

// SetBotsRunning 设置某类型运行中的机器人数量。
func SetBotsRunning(botType string, n int) {
	ensureBusinessMetrics()
	botsRunningGauge.Set(float64(n), botType)
}

// RecordReconcileDiff 记录一次对账差异产生（diff_type, exchange）。
func RecordReconcileDiff(diffType, exchange string) {
	ensureBusinessMetrics()
	reconcileDiffsTotal.Inc(diffType, exchange)
}

// ── Config ───────────────────────────────────────────────────────

// Enabled Prometheus 指标开关：PROMETHEUS_ENABLED，默认 true。
func Enabled() bool {
	v := os.Getenv("PROMETHEUS_ENABLED")
	return v != "false" && v != "0"
}

// Token 可选鉴权 token：PROMETHEUS_TOKEN，空 = 不鉴权。
func Token() string { return os.Getenv("PROMETHEUS_TOKEN") }

// LocalhostOnly 仅允许本机访问：PROMETHEUS_LOCALHOST_ONLY。
func LocalhostOnly() bool {
	v := os.Getenv("PROMETHEUS_LOCALHOST_ONLY")
	return v == "true" || v == "1"
}
