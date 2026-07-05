#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant 启动脚本
# ═══════════════════════════════════════════════════════════════════
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"

# 确保标准目录存在
mkdir -p "${DIR}/config" "${DIR}/runtime" "${DIR}/logs" "${DIR}/user_data"

# 如果不存在本地配置文件，从示例复制
if [ ! -f "${DIR}/config/config.yaml" ] && [ -f "${DIR}/config/config.example.yaml" ]; then
  cp "${DIR}/config/config.example.yaml" "${DIR}/config/config.yaml"
fi

echo -e "\033[0;36m"
cat <<'BANNER'
╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant 多系统启动器                        ║
╚══════════════════════════════════════════════════════════════╝
BANNER
echo -e "\033[0m"

# 加载 .env 环境变量
if [ -f "${DIR}/.env" ]; then
  set -a
  # shellcheck source=/dev/null
  source "${DIR}/.env"
  set +a
fi

# Python venv 路径
VENV_PYTHON="${DIR}/sandbox/.venv/bin/python"

# ── 启动 Redis（可选）────────────────────────────────────────────
if command -v redis-server >/dev/null 2>&1 && ! pgrep -x redis-server >/dev/null 2>&1; then
  echo "[START] Redis :6379"
  redis-server --daemonize yes 2>/dev/null || true
fi

# ── 启动 CCXT Bridge ─────────────────────────────────────────────
if [ -f "${DIR}/sandbox/ccxt_bridge/main.py" ] && [ -f "$VENV_PYTHON" ]; then
  echo "[START] CCXT Bridge :8002"
  cd "${DIR}/sandbox/ccxt_bridge"
  nohup "$VENV_PYTHON" main.py > "${DIR}/logs/ccxt_bridge.log" 2>&1 &
  CCXT_PID=$!
  cd "${DIR}"
fi

# ── 启动 Python 策略引擎 ─────────────────────────────────────────
if [ -f "${DIR}/sandbox/strategy_engine/main.py" ] && [ -f "$VENV_PYTHON" ]; then
  echo "[START] Python Strategy Engine :8003"
  cd "${DIR}/sandbox/strategy_engine"
  nohup "$VENV_PYTHON" main.py > "${DIR}/logs/strategy_engine.log" 2>&1 &
  STRATEGY_PID=$!
  cd "${DIR}"
fi

# ── ML Server 说明 ───────────────────────────────────────────────
# sandbox/ml_server/main.py 是训练 CLI（非常驻），不通过 uvicorn 启动。
# 需要训练模型时手动运行：
#   cd sandbox/ml_server && ../../sandbox/.venv/bin/python main.py train --data ... --output ...
# 因此这里不再尝试启动 ML Server。

# ── 启动前端 Dev Server ───────────────────────────────────────────
if [ -f "${DIR}/web/package.json" ] && command -v npm >/dev/null 2>&1; then
  echo "[START] Frontend dev server :5173"
  cd "${DIR}/web"
  nohup npm run dev > "${DIR}/logs/frontend.log" 2>&1 &
  FRONTEND_PID=$!
  cd "${DIR}"
fi

# ── 启动 Go Gateway ─────────────────────────────────────────────
GATEWAY_BIN="${DIR}/dist/gateway"
if [ -f "$GATEWAY_BIN" ]; then
  echo "[START] Gateway :8080"
  cd "${DIR}"
  export LD_LIBRARY_PATH="${DIR}/engine/target/release:${LD_LIBRARY_PATH}"
  export RUST_ENGINE_PATH="${DIR}/engine/target/release/libxt_matching.so"
  nohup "$GATEWAY_BIN" > "${DIR}/logs/gateway.log" 2>&1 &
  GW_PID=$!
else
  echo "[ERROR] dist/gateway 未构建，请先运行: make build"
  exit 1
fi

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  All services started!                                    ║"
echo "║  Frontend: http://localhost:5173                        ║"
echo "║  Gateway:  http://localhost:8080/api                    ║"
echo "║  Logs:     ${DIR}/logs/                                 ║"
echo "║  Config:   ${DIR}/config/                               ║"
echo "║  Runtime:  ${DIR}/runtime/                              ║"
echo "║  User:     ${DIR}/user_data/                            ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "Press Ctrl+C to stop all services"

trap 'kill ${CCXT_PID} ${STRATEGY_PID} ${ML_PID} ${GW_PID} ${FRONTEND_PID} 2>/dev/null; exit' INT TERM
wait
