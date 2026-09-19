# 可观测性（Prometheus + Grafana）

XiaoTian Quant 的 Go 网关在 `gateway/cmd/server/router.go` 中挂载了
`/metrics` 端点（`PROMETHEUS_ENABLED=false` 时不挂载，默认启用），由
`gateway/internal/metrics` 以 Prometheus 文本格式输出指标。
访问控制可二选一（均为环境变量）：`PROMETHEUS_TOKEN`（要求
`Authorization: Bearer <token>` 或 `?token=`）、`PROMETHEUS_LOCALHOST_ONLY=true`
（仅回环地址）。本目录（`ops/`）与 `docker-compose.observability.yml` 在其之上补齐了
抓取、告警与看板，对齐 QuantDinger 的 `ops/` 监控布局。

## 快速开始

监控栈通过 compose profile `observability` 启用，需与主 compose 文件叠加：

```bash
# 开发环境（或把 docker-compose.yml 换成 docker-compose.prod.yml）
docker compose -f docker-compose.yml -f docker-compose.observability.yml \
  --profile observability up -d
```

- Grafana: <http://127.0.0.1:3000>（默认 `admin/admin`，首次登录请改密）
- Prometheus: <http://127.0.0.1:9090>（`/targets` 看抓取状态，`/alerts` 看告警）

端口只绑定 `127.0.0.1`。若网关不在 compose 中运行（如宿主机直接
`./start-local.sh`），覆盖抓取目标：

```bash
GATEWAY_SCRAPE_TARGET=host.docker.internal:8080 \
docker compose -f docker-compose.yml -f docker-compose.observability.yml \
  --profile observability up -d
```

不引入 Alertmanager，保持轻量；告警仅在 Prometheus/Grafana 中可见。

## 看板内容

Grafana 自动加载 `ops/grafana/dashboards/xiaotian-overview.json`
（标题 "XiaoTian Quant Overview"），面板与指标对应关系：

| 面板 | 指标（metrics.go 来源） |
| --- | --- |
| 组合权益 / 持仓数 / 活跃策略数 | `portfolio_equity_usdt`、`portfolio_positions`、`strategies_active`（`SetEquity` 等，约 460-476 行） |
| Goroutines / 堆内存 / GC CPU 占比 | `go_goroutines`、`go_memstats_heap_*`、`go_memstats_gc_cpu_fraction`（runtimeMetrics，351-389 行） |
| HTTP 请求速率、5xx 比例、Top 5 路径 | `http_requests_total{method,path,status}`（HTTPMiddleware，407 行） |
| HTTP 延迟 p50/p90 | `http_request_duration_seconds_bucket`（直方图，408 行） |
| 订单速率、策略信号速率 | `orders_total{side,status}`、`signals_total{strategy,direction}`（449-458 行） |

注意：

- 订单、信号、组合类指标**已接线**：`RecordOrder`（下单成功，
  `handler/order.go`）、`RecordSignal`（策略信号发布，`strategy/engine.go`）；
  权益/持仓数/运行中策略数由后台任务每 15s 上报（`handler/index.go`），
  同周期上报各类型运行中机器人数量（`bots_running{type}`，
  grid/dca/layered_martin/ai）。对应告警 `GatewayPortfolioMetricsMissing`
  会在 15 分钟无数据时提示。
- 进程指标含 `process_uptime_seconds`、`process_start_time_seconds`、
  `go_gc_cycles_total`（runtimeMetrics）。
- 业务指标另含风控拒绝（`risk_order_rejections_total`，OMS RiskCheck 拦截处）、
  通知投递（`notify_sends_total{channel,result}`，通知管理器逐渠道计数）、
  WS 连接数（`ws_connections`，`/ws` 与 `/ws/v2` 连接增减）、
  对账差异（`reconcile_diffs_total{type,exchange}`，差异落库处）、
  成交量（`fills_total{side}`，下单成交处）。

## 告警规则

`ops/prometheus/alerts.yml`（评估结果见 Prometheus `/alerts`）：

- `GatewayDown`：抓取不可达 1 分钟（critical）
- `GatewayHighErrorRate`：5xx 比例 > 5% 持续 5 分钟
- `GatewayHighLatency`：p90 延迟 > 1s 持续 5 分钟
- `GatewayHighMemory`：堆内存 > 512MiB 持续 10 分钟
- `GatewayPortfolioMetricsMissing`：组合指标未上报（info，接线后可删除）

## 如何新增指标

1. 在 `gateway/internal/metrics/metrics.go` 中通过 `NewCounter` /
   `NewGauge` / `NewHistogram` 创建指标并注册到 `globalRegistry`，
   或直接调用已有的 `RecordOrder` 等便捷函数。
2. Prometheus 每 10-15s 自动抓取，无需改动 `ops/prometheus/prometheus.yml`。
3. 在 Grafana 看板中添加面板（数据源 uid 固定为 `prometheus`），
   或将新看板 JSON 放入 `ops/grafana/dashboards/` 自动加载。
4. 如需告警，在 `ops/prometheus/alerts.yml` 中追加规则后重启 prometheus
   容器（或调用 `/-/reload`，compose 已开启 `--web.enable-lifecycle`）。
