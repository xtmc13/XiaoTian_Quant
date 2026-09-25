# 可观测性（Prometheus + Alertmanager + Grafana）

XiaoTian Quant 的 Go 网关在 `gateway/cmd/server/router.go` 中挂载了
`/metrics` 端点（`PROMETHEUS_ENABLED=false` 时不挂载，默认启用），由
`gateway/internal/metrics` 以 Prometheus 文本格式输出指标。
访问控制可二选一（均为环境变量）：`PROMETHEUS_TOKEN`（要求
`Authorization: Bearer <token>` 或 `?token=`）、`PROMETHEUS_LOCALHOST_ONLY=true`
（仅回环地址）。本目录（`ops/`）与 `docker-compose.observability.yml` 在其之上补齐了
抓取、告警推送与看板，对齐 QuantDinger 的 `ops/` 监控布局。

## 告警链路

```
Prometheus（ops/prometheus/alerts.yml 评估）
  │  alerting → alertmanager:9093（ops/prometheus/prometheus.yml）
  ▼
Alertmanager（ops/alertmanager/alertmanager.yml 路由树，silence 持久化在
  │  alertmanager_data 卷；启动时 docker-entrypoint.sh 展开 ${VAR} 并按
  │  env 门控剪掉未配置的直连接收器）
  ├─ 默认 receiver gateway-webhook ──────────────────────────────┐
  │   POST /api/alerts/webhook                                    │
  │   Header: X-Alert-Webhook-Secret（env ALERT_WEBHOOK_SECRET）  │
  └─ severity=critical → receiver critical-direct（continue:      │
       true，同时仍走 gateway-webhook）直连邮件 + Telegram        │
       （配齐 ALERT_EMAIL_TO / ALERT_TELEGRAM_BOT_TOKEN 才启用）  │
                                                                ▼
                                        gateway alerting.Service
                                 （fingerprint 级去重：firing 期间同 fingerprint
                                   不重复推送，resolved 推送一次；状态落库
                                   xt_alert_events，迁移 0033，重启不丢）
                                                │
                                                ▼
                              notify.Broadcaster → 路由规则（event=alert）
                              → 渠道：邮件/飞书/钉钉/Telegram/Discord/短信
                                                │
                                                ▼
                       前端「系统状态 → 告警」区块与通知中心
                       （GET /api/alerts/active、/api/alerts/history）
```

关键点：

- **共享密钥**：`ALERT_WEBHOOK_SECRET` 必须在 gateway 与 alertmanager 两侧
  同值（同一份 `.env` 即可）。gateway 侧未配置时 `/api/alerts/webhook`
  fail-closed 返回 503，启动日志有警告。
- **critical 直达通道是兜底**：即使 gateway 挂了，critical 告警（如
  GatewayDown）仍可由 Alertmanager 直接发邮件/Telegram。未配置对应 env 时
  入口脚本自动剪掉空块，配置校验不会失败。
- **去重语义**：Alertmanager 按 `group_interval`（5m）重发同一组告警，gateway
  侧按 fingerprint 抑制重复推送；firing→resolved→firing 视为新事故重新推送。
- **前端告警路由**：新部署默认规则 `infra-alerts`（event=alert，全部级别 →
  telegram/lark）。已有自定义路由表的部署需通过 `/api/notify/routes`
  为 `alert` 事件补一条规则，否则告警只落库/通知中心不出渠道。

## 快速开始

监控栈通过 compose profile `observability` 启用，需与主 compose 文件叠加：

```bash
# 0. 在 .env 配置告警共享密钥（必填，否则 webhook 链路不生效）
#    ALERT_WEBHOOK_SECRET=<随机长串>
#    可选 critical 直连：ALERT_EMAIL_TO / ALERT_SMTP_* / ALERT_TELEGRAM_*

# 开发环境（或把 docker-compose.yml 换成 docker-compose.prod.yml）
docker compose -f docker-compose.yml -f docker-compose.observability.yml \
  --profile observability up -d
```

- Grafana: <http://127.0.0.1:3000>（默认 `admin/admin`，首次登录请改密）
- Prometheus: <http://127.0.0.1:9090>（`/targets` 看抓取状态，`/alerts` 看告警）
- Alertmanager: <http://127.0.0.1:9093>（`/api/v2/silences` 管理静默）

端口只绑定 `127.0.0.1`。若网关不在 compose 中运行（如宿主机直接
`./start-local.sh`），覆盖抓取目标与 webhook 地址：

```bash
GATEWAY_SCRAPE_TARGET=host.docker.internal:8080 \
ALERT_GATEWAY_WEBHOOK_URL=http://host.docker.internal:8080/api/alerts/webhook \
docker compose -f docker-compose.yml -f docker-compose.observability.yml \
  --profile observability up -d
```

## 验证告警链路

```bash
# 1. 直接打 gateway webhook（绕过 Prometheus/Alertmanager，验证去重+通知）
curl -X POST http://127.0.0.1:8080/api/alerts/webhook \
  -H "Content-Type: application/json" \
  -H "X-Alert-Webhook-Secret: $ALERT_WEBHOOK_SECRET" \
  -d '{"version":"4","status":"firing","receiver":"test","alerts":[{"status":"firing",
       "labels":{"alertname":"TestAlert","severity":"warning"},
       "annotations":{"summary":"链路自测"},
       "startsAt":"2026-01-01T00:00:00Z","fingerprint":"test-fp-1"}]}'
# → {"received":1,"notified_firing":1,...}；再发一次同 fingerprint → suppressed=1
# 密钥错误 → 401；gateway 未配 ALERT_WEBHOOK_SECRET → 503

# 2. 前端「系统状态」页告警区块应出现 TestAlert（firing）；
#    再发 status=resolved 的同 fingerprint 载荷，活跃列表清空、历史保留。

# 3. 端到端：Prometheus /alerts 触发（或临时下调阈值）→ Alertmanager
#    /api/v2/alerts 可见 → gateway 日志 [Notify] 渠道投递。
```

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
  WS 连接数与断连计数（`ws_connections`、`ws_disconnects_total`，
  `/ws` 与 `/ws/v2` 连接增减）、
  对账差异（`reconcile_diffs_total{type,exchange}`，差异落库处）、
  成交量（`fills_total{side}`，下单成交处）。
- 磁盘用量（`gateway_disk_total_bytes` / `gateway_disk_avail_bytes{path}`）
  在每次抓取时对 `DB_PATH` 所在文件系统 statfs 采样。

## 告警规则

`ops/prometheus/alerts.yml`（12 条；评估 → Alertmanager → 网关 webhook）：

| 规则 | 级别 | 条件 | for |
| --- | --- | --- | --- |
| `GatewayDown` | critical | 抓取不可达 | 1m |
| `GatewayHighErrorRate` | warning | 5xx 比例 > 5% | 5m |
| `GatewayHighLatency` | warning | p90 > 1s | 5m |
| `GatewayHighLatencyP95` | warning | p95 > 2s | 10m |
| `GatewayHighMemory` | warning | 堆内存 > 512MiB | 10m |
| `GatewayDiskLow` | warning | 数据卷可用空间 < 10% | 10m |
| `GatewayPortfolioMetricsMissing` | info | 组合指标 15 分钟未上报 | 15m |
| `GatewayOrderFailureRate` | critical | 下单 REJECTED/EXPIRED 占比 > 20%（样本 ≥5） | 10m |
| `GatewayWebSocketChurn` | warning | WS 断连 > 0.2 次/s | 10m |
| `GatewayPositionMismatch` | critical | 15m 内出现 position_* 对账差异 | 5m |
| `GatewayRiskRejectionsBurst` | warning | 风控拒绝 > 1 笔/s | 5m |
| `GatewayNotifyDeliveryFailures` | warning | 某通知渠道失败率 > 50%（失败 ≥3） | 5m |

每条规则带 `runbook` 注解（处置步骤），告警通知内容自动附带。
规则文件由 `gateway/internal/metrics/alerts_rules_test.go` 做静态自检
（结构/for/runbook/severity/指标名防漂移）；本机装有 promtool 时也可：

```bash
promtool check rules ops/prometheus/alerts.yml
```

## 如何新增指标

1. 在 `gateway/internal/metrics/metrics.go` 中通过 `NewCounter` /
   `NewGauge` / `NewHistogram` 创建指标并注册到 `globalRegistry`，
   或直接调用已有的 `RecordOrder` 等便捷函数。
2. Prometheus 每 10-15s 自动抓取，无需改动 `ops/prometheus/prometheus.yml`。
3. 在 Grafana 看板中添加面板（数据源 uid 固定为 `prometheus`），
   或将新看板 JSON 放入 `ops/grafana/dashboards/` 自动加载。
4. 如需告警，在 `ops/prometheus/alerts.yml` 中追加规则后重启 prometheus
   容器（或调用 `/-/reload`，compose 已开启 `--web.enable-lifecycle`）；
   新规则自动经 Alertmanager → gateway webhook → notify 渠道推送。
   同步把新指标名补进 `alerts_rules_test.go` 的 `knownAlertMetrics`。
