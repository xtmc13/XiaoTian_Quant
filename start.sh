#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant 启动脚本
# ═══════════════════════════════════════════════════════════════════
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"

echo -e "\033[0;36m"
cat <<'BANNER'
╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant 多系统启动器                        ║
╚══════════════════════════════════════════════════════════════╝
BANNER
echo -e "\033[0m"

# ── 环境检查 ──────────────────────────────────────────────────
if ! command -v redis-server >/dev/null 2>&1; then
  echo "[WARN] Redis 未安装，跳过缓存服务"
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "[WARN] Python3 未安装，ML 服务不可用"
fi

# ── 启动 Redis ──────────────────────────────────────────────────
if command -v redis-server >/dev/null 2>&1 && ! pgrep -x redis-server >/dev/null 2>&1; then
  echo "[START] Redis :6379"
  redis-server --daemonize yes 2>/dev/null || true
fi

# ── 启动 ML Server ──────────────────────────────────────────────
if [ -d "${DIR}/sandbox/ml_server" ] && command -v python3 >/dev/null 2>&1; then
  echo "[START] ML Server :8001"
  cd "${DIR}/sandbox/ml_server"
  python3 -m uvicorn main:app --host 0.0.0.0 --port 8001 &
  ML_PID=$!
  cd "${DIR}"
fi

# ── 启动 Go Gateway ─────────────────────────────────────────────
if [ -f "${DIR}/gateway/gateway-server" ]; then
  echo "[START] Gateway :8080"
  cd "${DIR}/gateway"
  ./gateway-server &
  GW_PID=$!
  cd "${DIR}"
else
  echo "[ERROR] gateway-server 未构建，请先运行: make build"
  exit 1
fi

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  All services started!                                    ║"
echo "║  Frontend: http://localhost:8080                        ║"
echo "║  Gateway:  http://localhost:8080/api                    ║"
echo "║  ML:       http://localhost:8001                        ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "Press Ctrl+C to stop all services"

trap 'kill ${ML_PID} ${GW_PID} 2>/dev/null; exit' INT TERM
wait
