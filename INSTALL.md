# XiaoTianQuant 跨平台安装指南

> 支持 Windows / Linux / macOS / Docker / 服务器部署

## 🚀 快速开始（推荐）

### Windows（PowerShell）
```powershell
# 右键 PowerShell → 以管理员身份运行
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
cd C:\Users\20545\Desktop\xiaotian_quant
.\install.ps1
```

### Linux / macOS / WSL
```bash
cd ~/xiaotian_quant
chmod +x install.sh
./install.sh
```

### Docker（所有平台）
```bash
# 安装 Docker Desktop 后
cd ~/xiaotianquant
make docker-up
```

---

## 📋 系统要求

| 组件 | 最低要求 | 推荐 |
|------|---------|------|
| CPU | 2 核 | 4 核+ |
| 内存 | 4 GB | 8 GB+ |
| 磁盘 | 10 GB | 50 GB+ |
| 网络 | 可访问 Binance | VPN 代理 |

---

## 🖥️ 平台详细安装

### Windows 本地安装

1. **安装依赖**（如果未安装）：
   - [Go 1.23+](https://go.dev/dl/)
   - [Node.js 22+](https://nodejs.org/)
   - [Python 3.12+](https://python.org/)

2. **一键安装**：
   ```powershell
   .\install.ps1
   ```

3. **启动服务**：
   ```powershell
   .\start-all.bat
   ```

4. **访问**：http://localhost:5173

---

### Linux / Ubuntu / Debian

```bash
# 1. 克隆项目
git clone https://github.com/yourname/xiaotianquant.git
cd xiaotianquant

# 2. 一键安装
chmod +x install.sh
./install.sh

# 3. 启动
./start.sh

# 4. 或注册为系统服务
sudo systemctl start xiaotianquant
```

---

### macOS

```bash
# 1. 安装 Homebrew（如果未安装）
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# 2. 一键安装
cd ~/xiaotianquant
chmod +x install.sh
./install.sh

# 3. 启动
./start.sh
```

---

### Docker（推荐用于服务器）

```bash
# 开发环境
docker compose up --build

# 生产环境
cd scripts/setup
docker compose -f docker-compose.server.yml up -d

# 查看日志
docker compose logs -f

# 停止
docker compose down
```

---

## 🌐 服务器部署

### 云服务器（阿里云/腾讯云/AWS）

```bash
# 1. 上传项目
rsync -avz --exclude='.git' --exclude='node_modules' \
  . root@your-server:/opt/xiaotianquant/

# 2. SSH 到服务器
ssh root@your-server

# 3. 安装并启动
cd /opt/xiaotianquant
make install
make docker-server-up

# 4. 配置 Nginx SSL（可选）
cp scripts/setup/nginx.conf /etc/nginx/
# 配置 SSL 证书
certbot --nginx -d your-domain.com
```

### 使用 systemd 服务

```bash
# 安装后自动创建服务
sudo systemctl start xiaotianquant
sudo systemctl enable xiaotianquant

# 查看状态
sudo systemctl status xiaotianquant

# 查看日志
sudo journalctl -u xiaotianquant -f
```

---

## 🔧 Makefile 命令

```bash
make help          # 查看所有命令
make install       # 一键安装
make build         # 构建前后端
make start         # 启动所有服务
make stop          # 停止所有服务
make restart       # 重启服务
make logs          # 查看日志
make dev           # 开发模式（热重载）
make test          # 运行测试
make docker-up     # Docker 启动
make docker-down   # Docker 停止
make sync          # 同步市场数据
```

---

## 🌏 中国大陆用户

### 自动代理检测

安装脚本会自动检测以下代理端口：
- `7897`（Clash）
- `7890`（Clash Verge）
- `1080`（Shadowsocks）
- `10808`（V2Ray）

### 手动配置代理

```bash
# Linux/macOS
export HTTP_PROXY=http://127.0.0.1:7897
export HTTPS_PROXY=http://127.0.0.1:7897

# Windows PowerShell
$env:HTTP_PROXY="http://127.0.0.1:7897"
$env:HTTPS_PROXY="http://127.0.0.1:7897"
```

---

## 🏗️ 多系统架构

```
┌─────────────────────────────────────────────────────────────┐
│                    XiaoTianQuant v3.0                        │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Gateway    │  │   ML Server  │  │   RL Worker  │      │
│  │   :8080      │  │   :8001      │  │   (Redis)    │      │
│  │              │  │              │  │              │      │
│  │  Go + React  │  │  Python      │  │  Python      │      │
│  │  策略引擎     │  │  模型训练     │  │  强化学习     │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│         │                   │                   │             │
│         └───────────────────┼───────────────────┘             │
│                           │                                  │
│                    ┌──────────────┐                         │
│                    │    Redis     │                         │
│                    │   :6379    │                         │
│                    │  队列/缓存   │                         │
│                    └──────────────┘                         │
└─────────────────────────────────────────────────────────────┘
```

---

## 🔄 多策略并行

系统支持同时运行多个策略：

```go
// 注册多个策略
engine.Register(strategyA)  // BTCUSDT + 均线策略
engine.Register(strategyB)  // ETHUSDT + 突破策略
engine.Register(strategyC)  // SOLUSDT + ML策略

// 同时启动
engine.Start("strategyA", params)
engine.Start("strategyB", params)
engine.Start("strategyC", params)
```

每个策略：
- ✅ 独立计算信号
- ✅ 独立风控检查
- ✅ 独立下单执行
- ✅ 独立盈亏统计

---

## 📞 常见问题

### Q: 安装失败怎么办？
A: 检查日志，确保 Go/Node/Python 已正确安装。或尝试 Docker 方案。

### Q: 如何更新？
A: `git pull && make build && make restart`

### Q: 如何备份数据？
A: 备份 `gateway/gateway.db` 和 `sandbox/ml_server/models/`

### Q: 生产环境推荐？
A: Docker + Nginx + SSL + systemd

---

## 📄 许可证

MIT License — 自由使用，自担风险
