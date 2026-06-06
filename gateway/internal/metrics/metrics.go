package metrics

import (
	"fmt"
	"net/http"
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
	name   string
	help   string
	labels []string
	buckets []float64
	counts  map[string][]*int64
	sums   map[string]*int64
	mu     sync.RWMutex
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
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
	mu         sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

func (r *Registry) RegisterCounter(c *Counter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[c.name] = c
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
	sort.Strings(gNames)
	sort.Strings(hNames)

	for _, n := range cNames {
		r.mu.RLock()
		c := r.counters[n]
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

func runtimeMetrics() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP go_goroutines Number of goroutines\n")
	fmt.Fprintf(&b, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(&b, "go_goroutines %d\n", runtime.NumGoroutine())
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

func HTTPMiddleware(next http.Handler) http.Handler {
	requestCounter := NewCounter("http_requests_total", "Total HTTP requests", "method", "path", "status")
	requestDuration := NewHistogram("http_request_duration_seconds", "HTTP request duration", []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, "method", "path")
	globalRegistry.RegisterCounter(requestCounter)
	globalRegistry.RegisterHistogram(requestDuration)

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
		requestCounter.Inc(r.Method, path, fmt.Sprintf("%d", ww.statusCode))
		requestDuration.Observe(dur, r.Method, path)
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

// ── Convenience ──

func RecordOrder(side, status string) {
	c := NewCounter("orders_total", "Total orders placed", "side", "status")
	globalRegistry.RegisterCounter(c)
	c.Inc(side, status)
}

func RecordSignal(strategy, direction string) {
	c := NewCounter("signals_total", "Total strategy signals", "strategy", "direction")
	globalRegistry.RegisterCounter(c)
	c.Inc(strategy, direction)
}

func SetEquity(val float64) {
	g := NewGauge("portfolio_equity_usdt", "Current portfolio equity in USDT")
	globalRegistry.RegisterGauge(g)
	g.Set(val)
}

func SetPositionCount(n int) {
	g := NewGauge("portfolio_positions", "Number of open positions")
	globalRegistry.RegisterGauge(g)
	g.Set(float64(n))
}

func SetActiveStrategies(n int) {
	g := NewGauge("strategies_active", "Number of active strategies")
	globalRegistry.RegisterGauge(g)
	g.Set(float64(n))
}
