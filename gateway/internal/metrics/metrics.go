package metrics

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// ── Counter ──

// Counter is a monotonically increasing metric.
type Counter struct {
	name  string
	help  string
	value int64
}

func NewCounter(name, help string) *Counter {
	counter := &Counter{name: name, help: help}
	registry.Register(counter)
	return counter
}

func (c *Counter) Name() string     { return c.name }
func (c *Counter) Help() string     { return c.help }
func (c *Counter) Type() string     { return "counter" }
func (c *Counter) Value() float64   { return float64(atomic.LoadInt64(&c.value)) }

func (c *Counter) Inc()             { atomic.AddInt64(&c.value, 1) }
func (c *Counter) Add(n int64)      { atomic.AddInt64(&c.value, n) }
func (c *Counter) Reset()           { atomic.StoreInt64(&c.value, 0) }

// ── Gauge ──

// Gauge is a metric that can go up and down.
type Gauge struct {
	name  string
	help  string
	value int64 // atomic, stored as float64 bits
}

func NewGauge(name, help string) *Gauge {
	g := &Gauge{name: name, help: help}
	registry.Register(g)
	return g
}

func (g *Gauge) Name() string   { return g.name }
func (g *Gauge) Help() string   { return g.help }
func (g *Gauge) Type() string   { return "gauge" }
func (g *Gauge) Value() float64 { return math.Float64frombits(uint64(atomic.LoadInt64(&g.value))) }

func (g *Gauge) Set(v float64) { atomic.StoreInt64(&g.value, int64(math.Float64bits(v))) }
func (g *Gauge) Inc()          { g.Add(1) }
func (g *Gauge) Dec()          { g.Add(-1) }
func (g *Gauge) Add(v float64) {
	for {
		old := atomic.LoadInt64(&g.value)
		newVal := math.Float64frombits(uint64(old)) + v
		if atomic.CompareAndSwapInt64(&g.value, old, int64(math.Float64bits(newVal))) {
			return
		}
	}
}

// ── Histogram ──

// Histogram records observed values and computes quantiles.
type Histogram struct {
	name    string
	help    string
	buckets []float64
	counts  []int64
	sum     int64 // atomic
	count   int64 // atomic
	mu      sync.Mutex
}

func NewHistogram(name, help string, buckets []float64) *Histogram {
	if len(buckets) == 0 {
		buckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	}
	h := &Histogram{
		name:    name,
		help:    help,
		buckets: buckets,
		counts:  make([]int64, len(buckets)),
	}
	registry.Register(h)
	return h
}

func (h *Histogram) Name() string { return h.name }
func (h *Histogram) Help() string { return h.help }
func (h *Histogram) Type() string { return "histogram" }

func (h *Histogram) Observe(v float64) {
	atomic.AddInt64(&h.count, 1)
	atomic.AddInt64(&h.sum, int64(math.Float64bits(v)))

	h.mu.Lock()
	for i, b := range h.buckets {
		if v <= b {
			atomic.AddInt64(&h.counts[i], 1)
		}
	}
	h.mu.Unlock()
}

func (h *Histogram) Value() float64 {
	count := atomic.LoadInt64(&h.count)
	if count == 0 {
		return 0
	}
	sum := math.Float64frombits(uint64(atomic.LoadInt64(&h.sum)))
	return sum / float64(count)
}

func (h *Histogram) Buckets() map[float64]int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make(map[float64]int64)
	for i, b := range h.buckets {
		result[b] = atomic.LoadInt64(&h.counts[i])
	}
	return result
}

// ── Registry ──

// Metric is the interface all metric types implement.
type Metric interface {
	Name() string
	Help() string
	Type() string
}

type Valuer interface {
	Value() float64
}

// Registry holds all registered metrics.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]Metric
}

var registry = &Registry{metrics: make(map[string]Metric)}

func (r *Registry) Register(m Metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[m.Name()] = m
}

func (r *Registry) GetAll() map[string]Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]Metric, len(r.metrics))
	for k, v := range r.metrics {
		result[k] = v
	}
	return result
}

func (r *Registry) Get(name string) Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metrics[name]
}

// ── Pre-defined metrics ──

var (
	OrdersTotal     *Counter
	OrdersFilled    *Counter
	OrdersRejected  *Counter
	RiskIntercepts  *Counter
	WSReconnects    *Counter
	OrderLatencyMs  *Histogram
	CurrentEquity   *Gauge
	CurrentDrawdown *Gauge
)

func init() {
	OrdersTotal = NewCounter("orders_total", "Total number of orders placed")
	OrdersFilled = NewCounter("orders_filled", "Total number of orders filled")
	OrdersRejected = NewCounter("orders_rejected", "Total number of orders rejected")
	RiskIntercepts = NewCounter("risk_intercepts", "Total number of risk check interceptions")
	WSReconnects = NewCounter("ws_reconnects", "Total number of WebSocket reconnections")

	OrderLatencyMs = NewHistogram("order_latency_ms", "Order execution latency in milliseconds",
		[]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000})

	CurrentEquity = NewGauge("current_equity", "Current total portfolio equity")
	CurrentDrawdown = NewGauge("current_drawdown", "Current portfolio drawdown percentage")
}

// ── Snapshot ──

// Snapshot captures all metric values at a point in time.
type Snapshot struct {
	Timestamp int64               `json:"timestamp"`
	Metrics   map[string]float64  `json:"metrics"`
}

// SnapshotAll returns a snapshot of all metrics.
func SnapshotAll() Snapshot {
	snap := Snapshot{
		Timestamp: time.Now().UnixMilli(),
		Metrics:   make(map[string]float64),
	}
	for _, m := range registry.GetAll() {
		if v, ok := m.(Valuer); ok {
			snap.Metrics[m.Name()] = v.Value()
		}
	}
	return snap
}
