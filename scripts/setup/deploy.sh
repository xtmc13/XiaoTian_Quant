#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════
# XiaoTian Quant 一键部署脚本
# 适用：Ubuntu 22.04+ / Debian 12+
# ═══════════════════════════════════════════════════════════════════
set -e

REPO_URL="https://github.com/xtmc13/XiaoTian_Quant.git"
PROJECT_DIR="/opt/xiaotian-quant"
BRANCH="main"

echo -e "\033[0;36m"
cat <<'BANNER'
╔══════════════════════════════════════════════════════════════╗
║     XiaoTian Quant v3.0 — 一键部署脚本                      ║
╚══════════════════════════════════════════════════════════════╝
BANNER
echo -e "\033[0m"

# ── 检查 root ──────────────────────────────────────────────────
if [ "$EUID" -ne 0 ]; then
  echo "❌ 请使用 root 用户运行: sudo bash deploy.sh"
  exit 1
fi

# ── 安装依赖 ──────────────────────────────────────────────────
echo "[1/6] 安装系统依赖..."
apt-get update -qq
apt-get install -y -qq \
  git curl wget ca-certificates \
  gnupg lsb-release software-properties-common \
  jq net-tools

# ── 安装 Docker ────────────────────────────────────────────────
echo "[2/6] 安装 Docker..."
if ! command -v docker &> /dev/null; then
  curl -fsSL https://get.docker.com | sh
  systemctl enable docker
  systemctl start docker
fi

# 安装 docker-compose plugin
if ! docker compose version &> /dev/null; then
  apt-get install -y -qq docker-compose-plugin
fi

# ── 克隆仓库 ───────────────────────────────────────────────────
echo "[3/6] 拉取代码..."
if [ -d "$PROJECT_DIR" ]; then
  echo "   目录已存在，更新代码..."
  cd "$PROJECT_DIR"
  git pull origin "$BRANCH"
else
  git clone --depth 1 -b "$BRANCH" "$REPO_URL" "$PROJECT_DIR"
  cd "$PROJECT_DIR"
fi

# ── 配置环境变量 ───────────────────────────────────────────────
echo "[4/6] 配置环境..."
if [ ! -f .env ]; then
  cp .env.example .env
  echo "   ✅ .env 已创建（请编辑填入 API Key）"
else
  echo "   ℹ️ .env 已存在，跳过"
fi

# ── 构建并启动 ─────────────────────────────────────────────────
echo "[5/6] Docker 构建（这可能需要 10-30 分钟）..."
docker compose build --no-cache

echo "[6/6] 启动服务..."
docker compose up -d

# ── 验证 ───────────────────────────────────────────────────────
echo ""
echo "⏳ 等待服务启动..."
sleep 10

HEALTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/api/health 2>/dev/null || echo "000")

if [ "$HEALTH_STATUS" = "200" ]; then
  echo ""
  echo "╔══════════════════════════════════════════════════════════╗"
  echo "║  ✅ 部署成功！                                           ║"
  echo "║                                                          ║"
  echo "║  访问地址: http://$(curl -s ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}'):8080"
  echo "║  健康检查: http://localhost:8080/api/health             ║"
  echo "║                                                          ║"
  echo "║  查看日志: docker compose logs -f gateway               ║"
  echo "║  停止服务: docker compose down                          ║"
  echo "║  重启服务: docker compose restart                       ║"
  echo "╚══════════════════════════════════════════════════════════╝"
else
  echo ""
  echo "⚠️  服务启动中，健康检查返回 HTTP $HEALTH_STATUS"
  echo "    请稍后访问: http://localhost:8080"
  echo "    查看日志: docker compose logs -f gateway"
fi

echo ""
echo "📋 下一步:"
echo "   1. 编辑 .env 文件，填入交易所 API Key"
echo "   2. 重启服务: docker compose restart"
echo ""
