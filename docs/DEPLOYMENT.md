# 小田量化 (XiaoTianQuant) 部署运维手册

> 版本：v2.0.0  
> 更新日期：2026-06-06  
> 适用环境：Linux / macOS / Windows (WSL2)

---

## 目录

1. [系统架构](#1-系统架构)
2. [环境依赖](#2-环境依赖)
3. [快速启动](#3-快速启动)
4. [生产部署](#4-生产部署)
5. [配置详解](#5-配置详解)
6. [监控与告警](#6-监控与告警)
7. [日志管理](#7-日志管理)
8. [备份与恢复](#8-备份与恢复)
9. [故障排查](#9-故障排查)
10. [升级指南](#10-升级指南)

---

## 1. 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│                        用户层                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │  Web 前端 │  │  Mobile  │  │  CLI 工具│  │ 第三方 API│  │
│  │  React 19│  │  PWA     │  │  Python  │  │  Webhook │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘  │
└───────┼─────────────┼─────────────┼─────────────┼──────────┘
        │             │             │             │
        └─────────────┴──────┬──────┴─────────────┘
                             │
┌────────────────────────────┼──────────────────────────────┐
│                      API 网关 (Go)                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│  │  Gin HTTP│ │ WebSocket│ │  gRPC    │ │  Metrics │   │
│  │  REST API│ │ 实时行情 │ │ 内部通信 │ │ Prometheus│   │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘   │
└───────┼────────────┼────────────┼────────────┼───────────┘
        │            │            │            │
        ├────────────┴────────────┤            │
        │                         │            │
┌───────┴──────────┐  ┌──────────┴──────────┐  │
│   业务服务层      │  │    数据层            │  │
│  ┌────────────┐  │  │  ┌──────────────┐  │  │
│  │ 策略引擎    │  │  │  │  SQLite      │  │  │
│  │ 回测系统    │  │  │  │  (主存储)    │  │  │
│  │ AI/ML 服务  │  │  │  │  Redis       │  │  │
│  │ 风控中心    │  │  │  │  (缓存/队列) │  │  │
│  │ 社交交易    │  │  │  │  可选: PostgreSQL│ │
│  └────────────┘  │  │  └──────────────┘  │  │
└──────────────────┘  └──────────────────────┘  │
        │                                     │
┌───────┴─────────────────────────────────────┘
│              撮合引擎 (Rust)                 │
│         xt-matching-engine (cdylib)         │
│         price-time priority order book      │
└─────────────────────────────────────────────┘
```

### 技术栈版本要求

| 组件 | 最低版本 | 推荐版本 | 说明 |
|------|---------|---------|------|
| Go | 1.22 | 1.25+ | API 网关与业务逻辑 |
| Rust | 1.78 | 1.85+ | 撮合引擎 |
| Node.js | 20 | 22 LTS | 前端构建 |
| Python | 3.10 | 3.12 | ML 服务侧车 |
| SQLite | 3.39 | 3.45+ | 主数据库 |
| Redis | 6.2 | 7.2+ | 缓存与消息队列 |

---

## 2. 环境依赖

### 2.1 基础工具安装

```bash
# Ubuntu / Debian
sudo apt update && sudo apt install -y \
  git curl wget build-essential pkg-config \
  libssl-dev sqlite3 redis-tools

# macOS
brew install git curl wget sqlite redis

# 安装 Go (推荐通过官方安装器)
curl -L https://go.dev/dl/go1.25.linux-amd64.tar.gz | sudo tar -C /usr/local -xzf -
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc

# 安装 Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source $HOME/.cargo/env

# 安装 Node.js (通过 nvm)
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.0/install.sh | bash
nvm install 22
nvm use 22

# 安装 pnpm
npm install -g pnpm
```

### 2.2 Python 环境（ML 侧车）

```bash
# 创建虚拟环境
python3 -m venv ~/.venv/xt-ml
source ~/.venv/xt-ml/bin/activate

# 安装依赖
pip install --upgrade pip
pip install lightgbm xgboost scikit-learn pandas numpy fastapi uvicorn
```

---

## 3. 快速启动

### 3.1 克隆与初始化

```bash
git clone https://github.com/xiaotian-quant/xiaotian_quant.git
cd xiaotian_quant

# 初始化所有子模块与依赖
make init
# 或手动：
#   cd web && pnpm install && cd ..
#   cd gateway && go mod download && cd ..
#   cd engine && cargo fetch && cd ..
```

### 3.2 开发模式一键启动

```bash
# 使用项目提供的 Makefile
make dev

# 或分别启动三个服务（推荐 tmux / tmuxinator）

# Terminal 1: 前端
cd web && pnpm dev

# Terminal 2: 后端网关
cd gateway && go run cmd/server/main.go

# Terminal 3: Rust 引擎（仅在需要原生回测时）
cd engine && cargo build --release
# 引擎以 cdylib 形式被 Go 通过 cgo 加载，无需单独运行

# Terminal 4: ML 侧车（可选）
source ~/.venv/xt-ml/bin/activate
python gateway/ml/sidecar/main.py
```

### 3.3 验证启动

```bash
# 健康检查
curl http://localhost:8080/api/health
# 期望: {"status":"ok"}

# 市场数据
curl "http://localhost:8080/api/market/snapshot?symbol=BTCUSDT"

# 前端访问
open http://localhost:5173   # Vite 默认端口
```

---

## 4. 生产部署

### 4.1 前端构建

```bash
cd web

# 安装依赖
pnpm install

# 类型检查
pnpm type-check

# 生产构建
pnpm build

# 构建产物位于 web/dist/
# 包含：index.html + 静态资源 + PWA manifest
```

### 4.2 Rust 引擎编译

```bash
cd engine

# 开发模式
cargo build

# 生产模式（优化 + LTO + Strip）
cargo build --release

# 验证编译结果
ls -lh target/release/libxt_matching.so   # Linux
ls -lh target/release/libxt_matching.dylib  # macOS
ls -lh target/release/xt_matching.dll       # Windows

# 运行基准测试
cargo test --release benchmark_
```

### 4.3 后端编译

```bash
cd gateway

# 开发模式
go build -o bin/gateway-dev cmd/server/main.go

# 生产模式（静态链接 + 压缩）
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -ldflags="-s -w -X main.version=$(git describe --tags)" \
  -o bin/gateway cmd/server/main.go

# 验证二进制
./bin/gateway --version
```

### 4.4 数据库初始化

```bash
cd gateway

# 自动初始化（首次启动时自动创建）
./bin/gateway

# 手动初始化（如需重置）
rm -f data/xiaotian.db
./bin/gateway -init-db

# 查看表结构
sqlite3 data/xiaotian.db ".schema"
```

### 4.5 systemd 服务配置

创建 `/etc/systemd/system/xiaotian-gateway.service`：

```ini
[Unit]
Description=XiaoTianQuant API Gateway
After=network.target redis.service

[Service]
Type=simple
User=xtquant
Group=xtquant
WorkingDirectory=/opt/xiaotian_quant/gateway
Environment="PATH=/usr/local/go/bin:/usr/bin:/bin"
Environment="RUST_ENGINE_PATH=/opt/xiaotian_quant/engine/target/release/libxt_matching.so"
Environment="CONFIG_PATH=/opt/xiaotian_quant/gateway/config.yaml"
Environment="DATA_DIR=/opt/xiaotian_quant/gateway/data"
Environment="REDIS_URL=redis://localhost:6379/0"
Environment="DEEPSEEK_API_KEY=sk-xxxxxxxx"
ExecStart=/opt/xiaotian_quant/gateway/bin/gateway
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

启用服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable xiaotian-gateway
sudo systemctl start xiaotian-gateway
sudo systemctl status xiaotian-gateway
```

### 4.6 Nginx 反向代理

```nginx
server {
    listen 80;
    server_name api.xiaotianquant.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name api.xiaotianquant.com;

    ssl_certificate /etc/letsencrypt/live/api.xiaotianquant.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.xiaotianquant.com/privkey.pem;

    # 前端静态资源
    location / {
        root /opt/xiaotian_quant/web/dist;
        try_files $uri $uri/ /index.html;
        expires 1d;
        add_header Cache-Control "public, immutable";
    }

    # API 代理
    location /api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_connect_timeout 60s;
    }

    # WebSocket
    location /ws {
        proxy_pass http://127.0.0.1:8080/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400s;
    }

    # Prometheus metrics（内部访问）
    location /metrics {
        allow 10.0.0.0/8;
        deny all;
        proxy_pass http://127.0.0.1:8080/metrics;
    }
}
```

---

## 5. 配置详解

### 5.1 主配置文件 `gateway/config.yaml`

```yaml
# ── 服务器 ──
server:
  host: "0.0.0.0"
  port: 8080
  mode: "release"          # debug / release
  read_timeout: 30s
  write_timeout: 30s
  max_header_bytes: 1048576

# ── 数据库 ──
database:
  driver: "sqlite"
  dsn: "data/xiaotian.db"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 1h

# ── Redis ──
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
  pool_size: 10

# ── 日志 ──
log:
  level: "info"            # debug / info / warn / error
  format: "json"           # json / text
  output: "stdout"         # stdout / file / both
  file_path: "logs/gateway.log"
  max_size: 100            # MB
  max_backups: 7
  max_age: 30              # days
  compress: true

# ── 交易所适配 ──
exchanges:
  default: "binance"
  testnet: true            # 生产环境设为 false
  timeout: 10s
  rate_limit: 1200         # 请求/分钟

# ── AI / LLM ──
ai:
  default_provider: "deepseek"
  providers:
    deepseek:
      api_key: "${DEEPSEEK_API_KEY}"
      base_url: "https://api.deepseek.com/v1"
      model: "deepseek-chat"
      timeout: 60s
    openai:
      api_key: "${OPENAI_API_KEY}"
      base_url: "https://api.openai.com/v1"
      model: "gpt-4o"
      timeout: 60s

# ── 风控 ──
protection:
  enabled: true
  max_drawdown_pct: 20.0
  daily_loss_limit: 1000.0
  max_concurrent_orders: 50
  cooldown_seconds: 300

# ── 回测 ──
backtest:
  default_fee_rate: 0.001
  max_lookback_days: 365
  native_engine: true      # 使用 Rust 引擎

# ── 通知 ──
notification:
  channels:
    - telegram
    - email
    - webhook
  telegram:
    bot_token: "${TELEGRAM_BOT_TOKEN}"
    chat_id: "${TELEGRAM_CHAT_ID}"
  email:
    smtp_host: "smtp.gmail.com"
    smtp_port: 587
    username: "${SMTP_USER}"
    password: "${SMTP_PASS}"
    from: "alerts@xiaotianquant.com"

# ── 特性开关 ──
features:
  social_trading: true
  onchain_data: true
  ml_pipeline: true
  rl_training: false     # 实验性功能
  arbitrage: true
```

### 5.2 环境变量

| 变量 | 必填 | 说明 |
|------|------|------|
| `DEEPSEEK_API_KEY` | 是 | DeepSeek LLM API 密钥 |
| `OPENAI_API_KEY` | 否 | OpenAI API 密钥（备用） |
| `REDIS_URL` | 否 | Redis 连接字符串 |
| `TELEGRAM_BOT_TOKEN` | 否 | Telegram 通知机器人 |
| `TELEGRAM_CHAT_ID` | 否 | Telegram 接收聊天 ID |
| `SMTP_USER` | 否 | 邮件告警发件人 |
| `SMTP_PASS` | 否 | 邮件 SMTP 密码 |
| `ONCHAIN_API_KEY` | 否 | 链上数据 API 密钥 |
| `RUST_ENGINE_PATH` | 否 | Rust 引擎 so/dll 路径 |
| `DATA_DIR` | 否 | 数据文件目录 |
| `CONFIG_PATH` | 否 | 配置文件路径 |

---

## 6. 监控与告警

### 6.1 Prometheus Metrics

网关暴露 `/metrics` 端点，包含：

| 指标 | 类型 | 说明 |
|------|------|------|
| `http_requests_total` | Counter | HTTP 请求总数（按 method, path, status） |
| `http_request_duration_seconds` | Histogram | 请求延迟分布 |
| `gateway_active_connections` | Gauge | 当前 WebSocket 连接数 |
| `gateway_orders_processed_total` | Counter | 订单处理总数 |
| `gateway_trades_executed_total` | Counter | 成交总数 |
| `gateway_ai_requests_total` | Counter | AI 请求总数（按 provider, status） |
| `gateway_ml_inference_duration_ms` | Histogram | ML 推理耗时 |
| `gateway_rust_engine_latency_us` | Histogram | Rust 引擎延迟 |

### 6.2 Grafana Dashboard

导入 `monitoring/grafana/dashboard.json` 查看：
- QPS / 延迟 / 错误率
- 订单簿深度变化
- AI 服务健康状态
- 风控触发次数
- 交易所 API 配额使用率

### 6.3 告警规则示例

```yaml
# monitoring/prometheus/alerts.yml
groups:
  - name: xiaotianquant
    rules:
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "错误率超过 5%"

      - alert: AIServiceDown
        expr: gateway_ai_requests_total{status="error"} > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "AI 服务连续报错"

      - alert: RustEngineSlow
        expr: histogram_quantile(0.99, gateway_rust_engine_latency_us) > 1000
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Rust 引擎 P99 延迟 > 1ms"
```

---

## 7. 日志管理

### 7.1 日志级别

| 级别 | 用途 |
|------|------|
| DEBUG | 开发调试，包含请求体/响应体 |
| INFO | 正常业务流程（订单提交、策略启动） |
| WARN | 非致命异常（API 限流、网络抖动） |
| ERROR | 需要人工介入（数据库连接失败、引擎崩溃） |

### 7.2 日志轮转

```bash
# 使用 logrotate
sudo tee /etc/logrotate.d/xiaotianquant << 'EOF'
/opt/xiaotian_quant/gateway/logs/*.log {
    daily
    missingok
    rotate 30
    compress
    delaycompress
    notifempty
    create 0644 xtquant xtquant
    sharedscripts
    postrotate
        systemctl reload xiaotian-gateway
    endscript
}
EOF
```

### 7.3 结构化日志查询

```bash
# 使用 jq 过滤 JSON 日志
cat logs/gateway.log | jq 'select(.level=="ERROR") | {time, msg, path, user_id}'

# 统计某接口 QPS
cat logs/gateway.log | jq -r '.path' | grep '/api/orders' | sort | uniq -c | sort -rn
```

---

## 8. 备份与恢复

### 8.1 SQLite 备份

```bash
# 热备份（SQLite 支持在线备份）
sqlite3 data/xiaotian.db ".backup '/backup/xiaotian_$(date +%Y%m%d_%H%M%S).db'"

# 自动备份脚本 (crontab -e)
# 每天 3:00 备份
0 3 * * * /opt/xiaotian_quant/scripts/backup-db.sh

# 保留最近 14 天
find /backup -name "xiaotian_*.db" -mtime +14 -delete
```

### 8.2 配置文件备份

```bash
# 使用 git 追踪配置变更
cd /opt/xiaotian_quant
git init
git add gateway/config.yaml
git commit -m "config backup $(date)"
```

### 8.3 恢复流程

```bash
# 1. 停止服务
sudo systemctl stop xiaotian-gateway

# 2. 恢复数据库
cp /backup/xiaotian_20260606_030000.db gateway/data/xiaotian.db

# 3. 恢复配置
git checkout gateway/config.yaml

# 4. 重启服务
sudo systemctl start xiaotian-gateway

# 5. 验证
sudo systemctl status xiaotian-gateway
curl http://localhost:8080/api/health
```

---

## 9. 故障排查

### 9.1 服务无法启动

```bash
# 检查日志
sudo journalctl -u xiaotian-gateway -n 100 --no-pager

# 检查端口占用
sudo lsof -i :8080

# 检查 Rust 引擎库
ldd /opt/xiaotian_quant/engine/target/release/libxt_matching.so
# 若缺少依赖，安装：sudo apt install libc6-dev

# 检查数据库权限
ls -la gateway/data/
# 应属 xtquant:xtquant
```

### 9.2 前端白屏

```bash
# 检查构建产物
cat web/dist/index.html | head -5

# 检查 API 连通性
curl http://localhost:8080/api/health

# 浏览器控制台常见错误
# - CORS → 检查 Nginx 配置中的 proxy_set_header
# - 404 API → 检查网关路由注册
# - WebSocket 断开 → 检查防火墙 / Nginx upgrade 配置
```

### 9.3 AI 服务无响应

```bash
# 检查 API Key 有效性
curl -H "Authorization: Bearer $DEEPSEEK_API_KEY" \
  https://api.deepseek.com/v1/models

# 检查网关日志中的 AI 请求
cat logs/gateway.log | jq 'select(.path | contains("/ai/"))'

# 检查超时配置
# config.yaml: ai.providers.deepseek.timeout
```

### 9.4 Rust 引擎加载失败

```bash
# 检查 cgo 编译标志
cd gateway && go env CGO_ENABLED
# 应为 1

# 检查库路径
export LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/opt/xiaotian_quant/engine/target/release

# 验证符号表
nm -D /opt/xiaotian_quant/engine/target/release/libxt_matching.so | grep engine_create
```

### 9.5 数据库锁定

```bash
# SQLite 并发写入可能导致 database is locked
# 解决方案：
# 1. 检查是否有其他进程占用
fuser gateway/data/xiaotian.db

# 2. 增加 busy_timeout
sqlite3 gateway/data/xiaotian.db "PRAGMA busy_timeout = 5000;"

# 3. 长期方案：迁移到 PostgreSQL（配置中修改 driver）
```

---

## 10. 升级指南

### 10.1 前端升级

```bash
cd web
git pull origin main
pnpm install
pnpm type-check
pnpm build

# 滚动更新（Nginx 零停机）
sudo rsync -av --delete dist/ /var/www/xiaotianquant/
```

### 10.2 后端升级

```bash
cd gateway
git pull origin main
go mod download

# 数据库迁移（如有 schema 变更）
go run cmd/migrate/main.go

# 编译新版本
CGO_ENABLED=1 go build -ldflags="-s -w" -o bin/gateway-new cmd/server/main.go

# 蓝绿部署
sudo systemctl stop xiaotian-gateway
mv bin/gateway bin/gateway-old
mv bin/gateway-new bin/gateway
sudo systemctl start xiaotian-gateway

# 验证后删除旧版本
rm bin/gateway-old
```

### 10.3 Rust 引擎升级

```bash
cd engine
git pull origin main
cargo test --release

# 编译并替换
cargo build --release
cp target/release/libxt_matching.so /opt/xiaotian_quant/engine/

# 重启网关以加载新库
sudo systemctl restart xiaotian-gateway
```

### 10.4 回滚策略

```bash
# 前端回滚
sudo rsync -av --delete /var/www/xiaotianquant-backup/ /var/www/xiaotianquant/

# 后端回滚
sudo systemctl stop xiaotian-gateway
mv bin/gateway-old bin/gateway
sudo systemctl start xiaotian-gateway

# 数据库回滚（如有备份）
cp /backup/xiaotian_pre_upgrade.db data/xiaotian.db
```

---

## 附录

### A. 端口清单

| 端口 | 服务 | 说明 |
|------|------|------|
| 8080 | Go Gateway | 主 API 端口 |
| 6379 | Redis | 缓存/队列 |
| 5173 | Vite Dev | 前端开发服务器 |
| 9090 | Prometheus | 指标采集（可选） |
| 3000 | Grafana | 监控面板（可选） |
| 8002 | CCXT Sidecar | Python 交易所桥接 |

### B. 目录结构

```
xiaotian_quant/
├── web/                    # React 19 前端
│   ├── dist/               # 生产构建产物
│   └── src/
├── gateway/                # Go API 网关
│   ├── bin/                # 编译产物
│   ├── cmd/server/         # 入口
│   ├── internal/           # 业务模块
│   ├── data/               # SQLite 数据库
│   ├── logs/               # 日志文件
│   └── config.yaml         # 主配置
├── engine/                 # Rust 撮合引擎
│   ├── src/
│   └── target/release/     # 编译产物
├── docs/                   # 文档
│   ├── DEPLOYMENT.md       # 本文件
│   └── openapi.yaml        # API 文档
├── monitoring/             # 监控配置
│   ├── prometheus/
│   └── grafana/
└── scripts/                # 运维脚本
    ├── backup-db.sh
    └── health-check.sh
```

### C. 常用命令速查

```bash
# 查看服务状态
sudo systemctl status xiaotian-gateway

# 实时日志
sudo journalctl -u xiaotian-gateway -f

# 重启服务
sudo systemctl restart xiaotian-gateway

# 重新加载配置（不中断连接）
sudo systemctl reload xiaotian-gateway

# 性能分析
curl http://localhost:8080/debug/pprof/goroutine
curl http://localhost:8080/debug/pprof/heap

# 数据库维护
sqlite3 data/xiaotian.db "VACUUM;"
sqlite3 data/xiaotian.db "REINDEX;"

# Redis 清理
redis-cli FLUSHDB
# 或仅清理应用前缀
redis-cli EVAL "return redis.call('del', unpack(redis.call('keys', 'xt:*')))" 0
```

---

*本文档随版本迭代更新，最新版请查看 `docs/DEPLOYMENT.md`*
