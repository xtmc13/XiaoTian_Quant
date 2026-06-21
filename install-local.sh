#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant 本地一键安装脚本
# 适用：项目已克隆到 /home/ubuntu/.openclaw/workspace/XiaoTian_Quant
# ═══════════════════════════════════════════════════════════════════

set -euo pipefail

PROJECT_DIR="/home/ubuntu/.openclaw/workspace/XiaoTian_Quant"
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()  { echo -e "${CYAN}[STEP]${NC}  $1"; }

check_cmd() { command -v "$1" >/dev/null 2>&1; }

main() {
  echo -e "${CYAN}"
  cat <<'EOF'
╔══════════════════════════════════════════════════════════════╗
║     XiaoTianQuant 本地环境一键安装器                         ║
║     安装目录: /home/ubuntu/.openclaw/workspace/XiaoTian_Quant ║
╚══════════════════════════════════════════════════════════════╝
EOF
  echo -e "${NC}"

  cd "$PROJECT_DIR"

  # ── 1. 安装系统依赖 ──
  log_step "安装系统依赖..."
  sudo apt-get update -qq
  sudo apt-get install -y -qq \
    build-essential pkg-config libssl-dev \
    sqlite3 redis-tools git curl wget

  # ── 2. 安装 Go ──
  if check_cmd go; then
    log_info "Go 已安装: $(go version | awk '{print $3}')"
  else
    log_step "安装 Go 1.25..."
    curl -fsSL "https://go.dev/dl/go1.24.4.linux-amd64.tar.gz" | sudo tar -C /usr/local -xzf -
    echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
    export PATH=/usr/local/go/bin:$PATH
    log_info "Go 安装完成: $(go version)"
  fi

  # ── 3. 安装 Rust ──
  if check_cmd rustc; then
    log_info "Rust 已安装: $(rustc --version)"
  else
    log_step "安装 Rust..."
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source "$HOME/.cargo/env"
    log_info "Rust 安装完成: $(rustc --version)"
  fi

  # ── 4. 安装 Redis ──
  if check_cmd redis-server; then
    log_info "Redis 已安装"
  else
    log_step "安装 Redis..."
    sudo apt-get install -y -qq redis-server
    sudo systemctl enable --now redis-server 2>/dev/null || true
    log_info "Redis 安装完成"
  fi

  # ── 5. 安装前端依赖 ──
  log_step "安装前端依赖..."
  cd "$PROJECT_DIR/web"
  pnpm install
  log_info "前端依赖安装完成"

  # ── 6. 构建前端 ──
  log_step "构建前端..."
  pnpm build
  log_info "前端构建完成 → web/dist/"

  # ── 7. 安装 Go 依赖 ──
  log_step "安装 Go 依赖..."
  cd "$PROJECT_DIR/gateway"
  go mod download
  log_info "Go 依赖安装完成"

  # ── 8. 构建 Rust 引擎 ──
  log_step "构建 Rust 撮合引擎..."
  cd "$PROJECT_DIR/engine"
  cargo build --release
  log_info "Rust 引擎构建完成 → engine/target/release/libxt_matching.so"

  # ── 9. 构建后端 ──
  log_step "构建 Go 后端..."
  cd "$PROJECT_DIR/gateway"
  rm -rf spa/*
  cp -r "$PROJECT_DIR/web/dist/"* spa/ 2>/dev/null || true

  CGO_ENABLED=1 go build \
    -ldflags="-s -w -X main.version=v3.0.0" \
    -o bin/gateway cmd/server/main.go
  log_info "后端构建完成 → gateway/bin/gateway"

  # ── 10. 初始化数据库 ──
  log_step "初始化数据库..."
  mkdir -p data
  ./bin/gateway -init-db 2>/dev/null || true
  log_info "数据库初始化完成"

  # ── 11. 创建一键启动脚本 ──
  log_step "创建启动脚本..."
  cat > "$PROJECT_DIR/start-local.sh" <<'EOF'
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
echo "║  日志: sudo journalctl -f _PID=$GW_PID                   ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "按 Ctrl+C 停止服务"

trap 'kill $GW_PID 2>/dev/null; echo "[STOP] 服务已停止"; exit' INT TERM
wait
EOF
  chmod +x "$PROJECT_DIR/start-local.sh"

  # ── 12. 完成 ──
  echo -e "${GREEN}"
  cat <<EOF

╔══════════════════════════════════════════════════════════════╗
║              ✅ 本地安装完成！                                ║
║                                                              ║
║  项目目录: ${PROJECT_DIR}                                    ║
║                                                              ║
║  启动命令:                                                   ║
║    cd ${PROJECT_DIR} && ./start-local.sh                     ║
║                                                              ║
║  访问地址:                                                   ║
║    http://localhost:8080    # 前端 + API                   ║
║    http://localhost:8080/api/health  # 健康检查              ║
║                                                              ║
║  环境变量（如需手动启动）:                                    ║
║    export RUST_ENGINE_PATH="${PROJECT_DIR}/engine/target/release/libxt_matching.so"
║    export CONFIG_PATH="${PROJECT_DIR}/gateway/config.yaml"    ║
║    export REDIS_URL="redis://localhost:6379/0"              ║
╚══════════════════════════════════════════════════════════════╝

EOF
  echo -e "${NC}"
}

main "$@"
