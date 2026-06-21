#!/usr/bin/env bash
set -e
DIR="/home/ubuntu/.openclaw/workspace/XiaoTian_Quant"
cd "$DIR"

# 检查 Redis
if ! pgrep -x redis-server >/dev/null 2>&1; then
  echo "[START] Redis :6379"
  redis-server --daemonize yes
fi

# 启动后端
echo "[START] Go Gateway :8080"
cd "$DIR/gateway"
export PATH=/usr/local/go/bin:$PATH
export LD_LIBRARY_PATH="$DIR/engine/target/release:$LD_LIBRARY_PATH"
export RUST_ENGINE_PATH="$DIR/engine/target/release/libxt_matching.so"
export CONFIG_PATH="$DIR/gateway/config.yaml"
export DATA_DIR="$DIR/gateway/data"
export REDIS_URL="redis://localhost:6379/0"
./bin/gateway &
GW_PID=$!

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  ✅ XiaoTianQuant 已启动                                 ║"
echo "║                                                          ║"
echo "║  前端: http://localhost:8080                             ║"
echo "║  API:  http://localhost:8080/api/health                    ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "按 Ctrl+C 停止服务"

trap 'kill $GW_PID 2>/dev/null; echo "[STOP] 服务已停止"; exit' INT TERM
wait
