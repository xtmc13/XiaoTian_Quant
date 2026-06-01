#!/usr/bin/env bash
set -euo pipefail

# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant v3.0 — 一键部署脚本
# ═══════════════════════════════════════════════════════════════════

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

step()  { echo -e "${GREEN}[✓]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

echo ""
echo "  ╔══════════════════════════════════════════╗"
echo "  ║   XiaoTianQuant v3.0 — 一键部署         ║"
echo "  ╚══════════════════════════════════════════╝"
echo ""

# ── 1. Pre-flight checks ──────────────────────────────────────────
step "Checking prerequisites..."

if ! command -v docker &> /dev/null; then
    error "Docker not installed. Install: curl -fsSL https://get.docker.com | sh"
fi

if ! docker compose version &> /dev/null; then
    error "Docker Compose not available. Requires Docker 24+"
fi

# ── 2. Environment config ─────────────────────────────────────────
if [ ! -f .env ]; then
    if [ -f .env.example ]; then
        warn ".env not found — creating from .env.example"
        cp .env.example .env
        warn ">> EDIT .env with your API keys before deploying!"
        warn ">> Required: BINANCE_API_KEY, BINANCE_API_SECRET, etc."
    else
        error ".env.example not found"
    fi
else
    step ".env already exists"
fi

# ── 3. SSL cert ───────────────────────────────────────────────────
if [ ! -f nginx/ssl/cert.pem ]; then
    warn "SSL certs not found — generating self-signed certs..."
    bash nginx/generate-cert.sh
else
    step "SSL certs found"
fi

# ── 4. Build & Start ──────────────────────────────────────────────
step "Building Docker images (this may take a few minutes on first run)..."
docker compose build --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

step "Starting services..."
docker compose up -d

# ── 5. Wait for healthy ───────────────────────────────────────────
step "Waiting for gateway to be ready..."
for i in $(seq 1 30); do
    if curl -fsS http://localhost:8080/api/health > /dev/null 2>&1; then
        break
    fi
    sleep 2
done

# ── 6. Done ───────────────────────────────────────────────────────
echo ""
echo "  ╔══════════════════════════════════════════╗"
echo "  ║   ✅ 部署完成!                           ║"
echo "  ╠══════════════════════════════════════════╣"
echo "  ║   Web UI:  http://localhost:8080          ║"
echo "  ║   Status:  docker compose ps              ║"
echo "  ║   Logs:    docker compose logs -f         ║"
echo "  ╚══════════════════════════════════════════╝"
echo ""
