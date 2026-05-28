"""
指标收集 — 轻量级应用指标，无需外部依赖

支持:
  - Counter (计数器)
  - Gauge (瞬时值)
  - Histogram (分布)
  - 定期输出到日志
"""

import threading
from typing import Dict, List, Optional


class Counter:
    """单调递增计数器"""

    def __init__(self, name: str, help: str = ""):
        self.name = name
        self.help = help
        self._value: float = 0
        self._lock = threading.Lock()

    def inc(self, delta: float = 1):
        with self._lock:
            self._value += delta

    def value(self) -> float:
        return self._value


class Gauge:
    """瞬时值"""

    def __init__(self, name: str, help: str = ""):
        self.name = name
        self.help = help
        self._value: float = 0
        self._lock = threading.Lock()

    def set(self, val: float):
        with self._lock:
            self._value = val

    def value(self) -> float:
        return self._value


class Histogram:
    """分布统计"""

    def __init__(self, name: str, help: str = "", buckets: list = None):
        self.name = name
        self.help = help
        self.buckets = buckets or [1, 5, 10, 50, 100, 500, 1000, 5000]
        self._values: List[float] = []
        self._lock = threading.Lock()

    def observe(self, val: float):
        with self._lock:
            self._values.append(val)
            if len(self._values) > 10000:
                self._values = self._values[-5000:]

    def stats(self) -> dict:
        with self._lock:
            if not self._values:
                return {"count": 0}
            sorted_vals = sorted(self._values)
            n = len(sorted_vals)
            return {
                "count": n,
                "min": sorted_vals[0],
                "max": sorted_vals[-1],
                "avg": sum(sorted_vals) / n,
                "p50": sorted_vals[n // 2],
                "p95": sorted_vals[int(n * 0.95)],
                "p99": sorted_vals[int(n * 0.99)],
            }


class MetricsRegistry:
    """指标注册中心"""

    _instance: Optional["MetricsRegistry"] = None
    _lock = threading.Lock()

    def __new__(cls):
        if cls._instance is None:
            with cls._lock:
                if cls._instance is None:
                    cls._instance = super().__new__(cls)
                    cls._instance._counters: Dict[str, Counter] = {}
                    cls._instance._gauges: Dict[str, Gauge] = {}
                    cls._instance._histograms: Dict[str, Histogram] = {}
        return cls._instance

    def counter(self, name: str, help: str = "") -> Counter:
        if name not in self._counters:
            self._counters[name] = Counter(name, help)
        return self._counters[name]

    def gauge(self, name: str, help: str = "") -> Gauge:
        if name not in self._gauges:
            self._gauges[name] = Gauge(name, help)
        return self._gauges[name]

    def histogram(self, name: str, help: str = "", buckets: list = None) -> Histogram:
        if name not in self._histograms:
            self._histograms[name] = Histogram(name, help, buckets)
        return self._histograms[name]

    def get_all(self) -> dict:
        return {
            "counters": {n: c.value() for n, c in self._counters.items()},
            "gauges": {n: g.value() for n, g in self._gauges.items()},
            "histograms": {n: h.stats() for n, h in self._histograms.items()},
        }

    def snapshot(self) -> dict:
        return self.get_all()


# 全局
metrics = MetricsRegistry()

# 预定义指标
orders_total = metrics.counter("orders_total", "总订单数")
orders_filled = metrics.counter("orders_filled", "成交订单数")
orders_rejected = metrics.counter("orders_rejected", "被拒订单数")
risk_intercepts = metrics.counter("risk_intercepts", "风控拦截数")
ws_reconnects = metrics.counter("ws_reconnects", "WebSocket重连次数")
order_latency_ms = metrics.histogram("order_latency_ms", "订单延迟(ms)")
current_equity = metrics.gauge("current_equity", "当前权益")
current_drawdown = metrics.gauge("current_drawdown", "当前回撤比例")
