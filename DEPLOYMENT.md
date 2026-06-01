# XiaoTianQuant v3.0 — 部署指南

> 本文档涵盖开发环境、单机生产环境以及基于 Docker 的部署流程。

---

## 目录

1. [系统要求](#系统要求)
2. [快速开始](#快速开始)
3. [开发环境](#开发环境)
4. [生产部署](#生产部署)
5. [Docker 构建优化](#docker-构建优化)
6. [环境变量清单](#环境变量清单)
7. [健康检查与监控](#健康检查与监控)
8. [备份与恢复](#备份与恢复)
9. [故障排查](#故障排查)

---

## 系统要求

| 组件 | 最低配置 | 推荐配置 |
|------|---------|---------|
| CPU | 2 核 | 4 核 |
| 内存 | 4 GB | 8 GB |
| 磁盘 | 20 GB SSD | 100 GB SSD |
| 网络 | 10 Mbps | 100 Mbps |
| OS | Ubuntu 22.04 LTS | Ubuntu 24.04 LTS |

依赖软件：
- Docker Engine >= 24.0
- Docker Compose >= 2.20
- (可选) Nginx / Caddy 用于反向代理

---

## 快速开始

```bash
# 1. 克隆代码
git clone <repo-url> xiaotian-quant
cd xiaotian-quant

# 2. 准备环境变量
cp .env.example .env
# 编辑 .env，填入交易所 API Key 和 AI 提供商密钥

# 3. 一键启动
docker compose up -d

# 4. 查看状态
docker compose ps
docker compose logs -f gateway
```

访问 `http://localhost:8080` 即可打开 Web UI。

---

## 开发环境

### 前端开发 (Vite + React)

```bash
cd web
npm install
npm run dev          # 开发服务器 http://localhost:5173
npm run build        # 生产构建
cd ..
```

### 后端开发 (Go)

```bash
cd gateway
go mod download
go run ./cmd/server  # 默认端口 8080
```

### Python 沙箱开发

```bash
cd sandbox
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn main:app --host 0.0.0.0 --port 9000 --reload
```

---

## 生产部署

### 方案 A：Docker Compose（推荐）

```bash
# 使用生产配置文件
docker compose -f docker-compose.prod.yml up -d

# 带 Nginx 反向代理 + SSL
docker compose -f docker-compose.prod.yml --profile nginx up -d
```

### 方案 B：手动二进制部署

```bash
# 1. 构建前端
cd web
npm ci
npm run build

# 2. 复制前端产物到 gateway spa 目录
cp dist/index.html ../gateway/spa/
cp -r dist/assets ../gateway/spa/

# 3. 构建 Go 二进制
cd ../gateway
CGO_ENABLED=0 go build -ldflags="-s -w" -o xiaotian-gateway ./cmd/server

# 4. 运行
export PORT=8080
export GIN_MODE=release
./xiaotian-gateway
```

### 方案 C：systemd 服务

创建 `/etc/systemd/system/xiaotian-gateway.service`：

```ini
[Unit]
Description=XiaoTianQuant Gateway
After=network.target

[Service]
Type=simple
User=xiaotian
WorkingDirectory=/opt/xiaotian-quant
EnvironmentFile=/opt/xiaotian-quant/.env
ExecStart=/opt/xiaotian-quant/xiaotian-gateway
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now xiaotian-gateway
sudo systemctl status xiaotian-gateway
```

---

## Docker 构建优化

### 多阶段构建

本项目 Dockerfile 采用 3 阶段构建：

1. **web-builder**: Node.js 编译 React 前端
2. **go-builder**: Go 编译静态二进制
3. **runtime**: 最小化 Alpine 镜像运行

优化效果：
- 最终镜像仅包含静态二进制和 CA 证书
- 无源码、无构建工具、无 Node/Go 运行时
- 镜像大小从 ~1GB 缩减至 ~30MB

### 构建缓存策略

```bash
# 利用 BuildKit 缓存挂载加速构建
DOCKER_BUILDKIT=1 docker build \
  --build-arg VERSION=$(git describe --tags) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t xiaotian-quant/gateway:latest .
```

### 本地开发构建脚本

```bash
chmod +x build.sh
./build.sh
```

---

## 环境变量清单

### 核心配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | Gateway 监听端口 | `8080` |
| `GIN_MODE` | Go-Gin 运行模式 | `release` |
| `LOG_LEVEL` | 日志级别 | `INFO` |
| `LOG_FORMAT` | 日志格式 (`text`/`json`) | `text` |
| `SECRET_KEY` | JWT 签名密钥 | 自动生成 |

### 交易所 API

| 变量 | 说明 |
|------|------|
| `BINANCE_API_KEY` | Binance API Key |
| `BINANCE_API_SECRET` | Binance API Secret |
| `OKX_API_KEY` | OKX API Key |
| `OKX_API_SECRET` | OKX API Secret |

### AI 提供商

| 变量 | 说明 |
|------|------|
| `DEEPSEEK_API_KEY` | DeepSeek API Key |
| `OPENAI_API_KEY` | OpenAI API Key |
| `ANTHROPIC_API_KEY` | Anthropic API Key |

### 通知渠道

| 变量 | 说明 |
|------|------|
| `SMTP_HOST` | SMTP 服务器 |
| `SMTP_PORT` | SMTP 端口 |
| `SMTP_USER` | SMTP 用户名 |
| `SMTP_PASS` | SMTP 密码 |
| `LARK_WEBHOOK` | 飞书 Webhook |
| `DINGTALK_WEBHOOK` | 钉钉 Webhook |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token |

### 缓存与依赖

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `CACHE_ENABLED` | 是否启用 Redis 缓存 | `false` |
| `REDIS_URL` | Redis 连接 URL | 空 |
| `FREQTRADE_URL` | Freqtrade 服务地址 | `http://freqtrade:8080` |
| `SANDBOX_URL` | Python 沙箱地址 | `http://sandbox:9000` |

---

## 健康检查与监控

### 内置端点

- `GET /api/health` — Gateway 健康检查
- `GET /api/health/components` — 各组件状态（DB、Redis、Sandbox 等）

### Docker 健康检查

所有服务均已配置 Docker Healthcheck：

```bash
# 查看健康状态
docker compose ps

# 手动检查
docker compose exec gateway wget --spider http://localhost:8080/api/health
```

### 日志聚合

生产环境建议将日志输出为 JSON 格式，并通过以下方式收集：

```bash
# 查看最近 100 条 JSON 日志
docker compose logs --tail=100 gateway | jq -r '.msg'
```

---

## 备份与恢复

### 数据卷备份

```bash
# 备份 gateway 数据库
docker compose exec gateway tar czf - /app/data > backup-gateway-$(date +%F).tar.gz

# 备份 freqtrade 数据
docker compose exec freqtrade tar czf - /freqtrade/user_data > backup-freqtrade-$(date +%F).tar.gz

# 备份 Redis (启用 AOF 后可直接复制数据文件)
docker compose exec redis redis-cli BGSAVE
```

### 恢复

```bash
# 停止服务
docker compose down

# 恢复数据
tar xzf backup-gateway-2024-01-01.tar.gz -C /var/lib/docker/volumes/xiaotian_gateway_data/_data/

# 重启
docker compose up -d
```

---

## 故障排查

### 常见问题

**Q: Gateway 启动后立即退出**
```bash
# 检查日志
docker compose logs gateway

# 常见原因：.env 中的必需变量缺失、端口被占用
```

**Q: 前端页面 404**
```bash
# 确认 spa 目录已嵌入
docker compose exec gateway ls -la /app/spa/

# 重新构建镜像
docker compose build --no-cache gateway
```

**Q: Sandbox 执行超时**
```bash
# 检查资源限制
docker stats xiaotian-sandbox

# 调大内存限制
docker compose -f docker-compose.prod.yml up -d --no-deps sandbox
```

**Q: 交易所 API 连接失败**
```bash
# 检查 API Key 是否正确设置
docker compose exec gateway env | grep BINANCE

# 测试网络连通性
docker compose exec gateway wget -qO- https://api.binance.com/api/v3/ping
```

### 调试模式

```bash
# 临时开启 debug 日志
docker compose exec gateway sh -c "export LOG_LEVEL=DEBUG; ./gateway"

# 进入容器排查
docker compose exec gateway sh
```

---

## 安全建议

1. **不要在 `.env` 中提交真实密钥** — 使用 Docker Secrets 或外部 Vault
2. **限制端口暴露** — 生产环境只暴露 80/443，Gateway 绑定到 `127.0.0.1:8080`
3. **启用 HTTPS** — 通过 Nginx/Caddy 配置 SSL 证书
4. **定期更新基础镜像** — `docker compose pull && docker compose up -d`
5. **非 root 运行** — 容器内已配置 `USER xiaotian` (UID 1000)

---

## 升级指南

```bash
# 拉取最新代码
git pull origin main

# 重新构建并滚动更新
docker compose build --no-cache
docker compose up -d

# 验证新版本
curl -s http://localhost:8080/api/health | jq
```

---

*最后更新：2026-05-31*
