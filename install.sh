#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant One-Line Installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/xiaotian-quant/xiaotian_quant/main/install.sh | bash
#   curl -fsSL ... | bash -s -- --source        # force build from source
#   curl -fsSL ... | bash -s -- --dir /opt/xtq  # custom install dir
# ═══════════════════════════════════════════════════════════════════

set -euo pipefail

# ── Colors ──────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()  { echo -e "${CYAN}[STEP]${NC}  $1"; }
log_bold()  { echo -e "${BOLD}$1${NC}"; }

# ── Configuration ─────────────────────────────────────────────
REPO="xiaotian-quant/xiaotian_quant"
VERSION="${VERSION:-v3.0.0}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.xiaotianquant}"
FORCE_SOURCE=false

# Parse args (only when not piped)
for arg in "$@"; do
  case "$arg" in
    --source) FORCE_SOURCE=true ;;
    --dir)    shift; INSTALL_DIR="$1" ;;
    --version) shift; VERSION="$1" ;;
  esac
done

# ── Detect Environment ──────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"
IS_REMOTE=false

# Detect if running via pipe (curl | bash)
if [[ ! -t 0 ]]; then
  IS_REMOTE=true
fi

# Normalize platform name
case "${OS}" in
  Linux*)   PLATFORM="linux" ;;
  Darwin*)  PLATFORM="macos" ;;
  MINGW*|CYGWIN*|MSYS*) PLATFORM="windows"; OS="Windows" ;;
  *)        PLATFORM="linux" ;;
esac

case "${ARCH}" in
  x86_64|amd64)  ARCH_NAME="amd64" ;;
  aarch64|arm64) ARCH_NAME="arm64" ;;
  *)             ARCH_NAME="amd64" ;;
esac

PLATFORM_NAME="${PLATFORM}-${ARCH_NAME}"

# ── Banner ──────────────────────────────────────────────────────
show_banner() {
  echo -e "${CYAN}"
  cat <<'EOF'
╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant 跨平台一键安装器                      ║
║                                                              ║
║   支持: Linux | macOS | Windows (Git Bash/WSL) | Docker    ║
║   架构: AMD64 | ARM64                                        ║
╚══════════════════════════════════════════════════════════════╝
EOF
  echo -e "${NC}"
}

# ── Check Command ───────────────────────────────────────────────
check_cmd() { command -v "$1" >/dev/null 2>&1; }

# ── Download Helpers ────────────────────────────────────────────
download() {
  local url="$1" out="$2"
  if check_cmd curl; then
    curl -fsSL "$url" -o "$out" --progress-bar
  elif check_cmd wget; then
    wget -q --show-progress "$url" -O "$out"
  else
    log_error "需要 curl 或 wget"
    exit 1
  fi
}

# ── Install from Pre-built Release ────────────────────────────
install_binary() {
  log_step "下载预编译版本 ${VERSION} (${PLATFORM_NAME})..."

  local base_url="https://github.com/${REPO}/releases/download/${VERSION}"
  local archive tmpdir

  if [[ "$PLATFORM" == "windows" ]]; then
    archive="xiaotianquant-${VERSION}-${PLATFORM_NAME}.zip"
  else
    archive="xiaotianquant-${VERSION}-${PLATFORM_NAME}.tar.gz"
  fi

  local url="${base_url}/${archive}"
  tmpdir="$(mktemp -d)"

  log_info "下载: ${url}"
  if ! download "$url" "${tmpdir}/${archive}"; then
    log_warn "预编译版本下载失败，将尝试从源码构建..."
    rm -rf "$tmpdir"
    return 1
  fi

  log_info "解压到 ${INSTALL_DIR}..."
  mkdir -p "$INSTALL_DIR"

  if [[ "$PLATFORM" == "windows" ]]; then
    unzip -q "${tmpdir}/${archive}" -d "$INSTALL_DIR"
  else
    tar xzf "${tmpdir}/${archive}" -C "$INSTALL_DIR"
  fi

  rm -rf "$tmpdir"
  log_info "预编译版本安装完成"
  return 0
}

# ── Install from Source ───────────────────────────────────────
install_source() {
  log_step "从源码构建..."

  if [[ "$IS_REMOTE" == true ]]; then
    if [[ -d "$INSTALL_DIR/.git" ]]; then
      log_info "更新现有仓库..."
      cd "$INSTALL_DIR"
      git pull origin main
    else
      log_info "克隆仓库到 ${INSTALL_DIR}..."
      rm -rf "$INSTALL_DIR"
      git clone --depth 1 "https://github.com/${REPO}.git" "$INSTALL_DIR"
    fi
  fi

  cd "$INSTALL_DIR"

  # Install dependencies
  install_go
  install_node
  install_python
  install_redis

  # Build frontend
  log_step "构建前端..."
  cd web
  if [[ ! -d "node_modules" ]]; then
    npm ci
  fi
  npm run build
  cd ..

  # Build Go backend
  log_step "构建 Go 后端..."
  cd gateway
  rm -rf spa/*
  cp -r ../web/dist/* spa/ 2>/dev/null || true

  local ext=""
  [[ "$PLATFORM" == "windows" ]] && ext=".exe"

  CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -trimpath \
    -o "gateway-server${ext}" \
    ./cmd/server
  cd ..

  # Install Python deps
  log_step "安装 Python 依赖..."
  cd sandbox/ml_server
  python3 -m pip install -r requirements.txt
  python3 -m pip install stable-baselines3 gymnasium tensorboard scikit-learn
  cd ../..

  log_info "源码构建完成"
}

# ── Dependency Installers ─────────────────────────────────────
install_go() {
  if check_cmd go; then
    log_info "Go 已安装: $(go version | awk '{print $3}')"
    return 0
  fi
  log_step "安装 Go..."
  local go_ver="1.25.0"
  local go_tar="go${go_ver}.${PLATFORM}-${ARCH_NAME}.tar.gz"
  local tmp="$(mktemp -d)"
  download "https://go.dev/dl/${go_tar}" "${tmp}/${go_tar}"
  sudo tar -C /usr/local -xzf "${tmp}/${go_tar}"
  export PATH="/usr/local/go/bin:$PATH"
  rm -rf "$tmp"
  log_info "Go 安装完成"
}

install_node() {
  if check_cmd node; then
    log_info "Node.js 已安装: $(node --version)"
    return 0
  fi
  log_step "安装 Node.js..."
  if [[ "$PLATFORM" == "macos" ]] && check_cmd brew; then
    brew install node
  elif [[ "$PLATFORM" == "linux" ]]; then
    curl -fsSL https://deb.nodesource.com/setup_22.x | sudo bash -
    sudo apt-get install -y nodejs
  fi
  log_info "Node.js 安装完成"
}

install_python() {
  if check_cmd python3; then
    log_info "Python 已安装: $(python3 --version)"
    return 0
  fi
  log_step "安装 Python..."
  if [[ "$PLATFORM" == "macos" ]] && check_cmd brew; then
    brew install python@3.12
  elif [[ "$PLATFORM" == "linux" ]]; then
    sudo apt-get update
    sudo apt-get install -y python3.12 python3.12-venv python3-pip
  fi
  log_info "Python 安装完成"
}

install_redis() {
  if check_cmd redis-server; then
    log_info "Redis 已安装"
    return 0
  fi
  log_step "安装 Redis..."
  if [[ "$PLATFORM" == "macos" ]] && check_cmd brew; then
    brew install redis
    brew services start redis
  elif [[ "$PLATFORM" == "linux" ]]; then
    sudo apt-get install -y redis-server
    sudo systemctl enable --now redis-server
  fi
  log_info "Redis 安装完成"
}

# ── Create Launch Scripts ─────────────────────────────────────
create_scripts() {
  log_step "创建启动脚本..."
  cd "$INSTALL_DIR"

  # start.sh
  cat > start.sh <<'EOF'
#!/usr/bin/env bash
# XiaoTianQuant 启动脚本
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"

echo -e "\033[0;36m"
cat <<'BANNER'
╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant 多系统启动器                        ║
╚══════════════════════════════════════════════════════════════╝
BANNER
echo -e "\033[0m"

# Start Redis if not running
if ! pgrep -x redis-server >/dev/null 2>&1; then
  echo "[START] Redis :6379"
  redis-server --daemonize yes 2>/dev/null || true
fi

# Start ML Server
echo "[START] ML Server :8001"
cd "${DIR}/sandbox/ml_server"
python3 -m uvicorn main:app --host 0.0.0.0 --port 8001 &
ML_PID=$!

# Start Go Gateway
echo "[START] Gateway :8080"
cd "${DIR}/gateway"
if [[ -f gateway-server ]]; then
  ./gateway-server &
elif [[ -f gateway-server.exe ]]; then
  ./gateway-server.exe &
fi
GW_PID=$!

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
EOF
  chmod +x start.sh

  # systemd service (Linux only)
  if [[ "$PLATFORM" == "linux" ]] && check_cmd systemctl; then
    sudo tee /etc/systemd/system/xiaotianquant.service >/dev/null <<EOF
[Unit]
Description=XiaoTianQuant Trading System
After=network.target redis.service

[Service]
Type=simple
User=$USER
WorkingDirectory=${INSTALL_DIR}/gateway
Environment="PATH=/usr/local/bin:/usr/bin:/bin"
ExecStart=${INSTALL_DIR}/gateway/gateway-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
    sudo systemctl daemon-reload
    log_info "systemd 服务已创建: sudo systemctl start xiaotianquant"
  fi

  log_info "启动脚本已创建: ${INSTALL_DIR}/start.sh"
}

# ── Main ────────────────────────────────────────────────────────
main() {
  show_banner

  log_info "平台: ${OS} ${ARCH} → ${PLATFORM_NAME}"
  log_info "安装目录: ${INSTALL_DIR}"
  log_info "版本: ${VERSION}"
  [[ "$IS_REMOTE" == true ]] && log_info "模式: 远程安装 (curl | bash)"
  [[ "$FORCE_SOURCE" == true ]] && log_info "模式: 强制源码构建"

  # Create install directory
  mkdir -p "$INSTALL_DIR"

  # Try binary install first (unless forced source)
  if [[ "$FORCE_SOURCE" == false ]]; then
    if install_binary; then
      log_info "使用预编译二进制安装"
    else
      log_warn "预编译版本不可用，切换到源码构建..."
      install_source
    fi
  else
    install_source
  fi

  # Create scripts
  create_scripts

  # Done
  echo -e "${GREEN}"
  cat <<EOF

╔══════════════════════════════════════════════════════════════╗
║              ✅ 安装完成！                                    ║
║                                                              ║
║  安装目录: ${INSTALL_DIR}                                      ║
║                                                              ║
║  启动方式:                                                   ║
║    cd ${INSTALL_DIR} && ./start.sh                           ║
║                                                              ║
║  访问地址:                                                   ║
║    http://localhost:8080    # 前端 + API 网关               ║
║    http://localhost:8001    # ML 服务                         ║
║                                                              ║
║  多系统组成:                                                 ║
║    • Go Gateway    :8080  API网关 + 策略引擎                 ║
║    • ML Server     :8001  机器学习服务                       ║
║    • Redis         :6379  缓存/队列                          ║
║                                                              ║
║  默认账号: admin / admin123                                  ║
╚══════════════════════════════════════════════════════════════╝

EOF
  echo -e "${NC}"
}

main "$@"
