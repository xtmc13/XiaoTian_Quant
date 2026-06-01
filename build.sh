#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
VERSION="${VERSION:-$(git describe --tags 2>/dev/null || echo 'dev')}"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "=== Building XiaoTianQuant v3.0 ($VERSION) ==="

# Option 1: Docker multi-stage build (recommended)
echo "--- Building with Docker ---"
docker build \
  --build-arg VERSION="$VERSION" \
  --build-arg BUILD_TIME="$BUILD_TIME" \
  -t "xiaotian-quant/gateway:$VERSION" \
  -t "xiaotian-quant/gateway:latest" \
  "$ROOT"

echo "=== Build complete: xiaotian-quant/gateway:$VERSION ==="
echo "Run with: docker compose up -d"

# Option 2: Native build (fallback)
# Uncomment below if you prefer native build without Docker
#
# echo "--- Building web frontend ---"
# cd "$ROOT/web"
# npm ci --silent
# npm run build
#
# echo "--- Copying frontend to gateway/spa/ ---"
# rm -rf "$ROOT/gateway/spa/assets"
# cp "$ROOT/web/dist/index.html" "$ROOT/gateway/spa/index.html"
# cp -r "$ROOT/web/dist/assets" "$ROOT/gateway/spa/assets" 2>/dev/null || true
#
# echo "--- Building Go gateway ---"
# cd "$ROOT/gateway"
# CGO_ENABLED=0 go build -ldflags="-s -w" -o "$ROOT/dist/gateway" ./cmd/server
#
# echo "=== Build complete: $ROOT/dist/gateway ==="
