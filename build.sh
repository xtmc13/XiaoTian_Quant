#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
VERSION="${VERSION:-$(git describe --tags 2>/dev/null || echo 'dev')}"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Parse arguments: --docker for Docker build, default to native build
BUILD_MODE="${1:-native}"

echo "=== Building XiaoTianQuant v3.0 ($VERSION) ==="

if [ "$BUILD_MODE" == "--docker" ]; then
    # Option 1: Docker multi-stage build
    echo "--- Building with Docker ---"
    docker build \
      --build-arg VERSION="$VERSION" \
      --build-arg BUILD_TIME="$BUILD_TIME" \
      -t "xiaotian-quant/gateway:$VERSION" \
      -t "xiaotian-quant/gateway:latest" \
      "$ROOT"

    echo "=== Build complete: xiaotian-quant/gateway:$VERSION ==="
    echo "Run with: docker compose up -d"
else
    # Option 2: Native build (default)
    echo "--- Building web frontend ---"
    cd "$ROOT/web"
    npm ci --silent
    npm run build

    echo "--- Copying frontend to gateway/spa/ ---"
    rm -rf "$ROOT/gateway/spa/assets"
    mkdir -p "$ROOT/gateway/spa"
    cp "$ROOT/web/dist/index.html" "$ROOT/gateway/spa/index.html"
    cp -r "$ROOT/web/dist/assets" "$ROOT/gateway/spa/assets" 2>/dev/null || true

    echo "--- Building Rust matching engine (optional) ---"
    RUST_LIB_COPIED=false
    if [ -d "$ROOT/engine" ] && command -v cargo &> /dev/null; then
        cd "$ROOT/engine"
        cargo build --release
        # Copy the dynamic library next to the gateway binary so the loader can find it.
        # Windows: xt_matching.dll; Linux: libxt_matching.so; macOS: libxt_matching.dylib
        if cp "$ROOT/engine/target/release/xt_matching.dll" "$ROOT/dist/" 2>/dev/null || \
           cp "$ROOT/engine/target/release/libxt_matching.so" "$ROOT/dist/" 2>/dev/null || \
           cp "$ROOT/engine/target/release/libxt_matching.dylib" "$ROOT/dist/" 2>/dev/null; then
            RUST_LIB_COPIED=true
        fi
        # Also copy import library / rlib to gateway/ for cgo linking fallback
        cp "$ROOT/engine/target/release/libxt_matching.dll.a" "$ROOT/gateway/" 2>/dev/null || true
        cp "$ROOT/engine/target/release/libxt_matching.rlib" "$ROOT/gateway/" 2>/dev/null || true
        if [ "$RUST_LIB_COPIED" = true ]; then
            echo "Rust engine library copied to $ROOT/dist/"
        else
            echo "Rust engine build skipped (no compatible library found)"
        fi
    else
        echo "Rust engine build skipped (no cargo or engine directory)"
    fi

    echo "--- Building Go gateway ---"
    cd "$ROOT/gateway"
    # Auto-detect C compiler for cgo (required to link the Rust matching engine).
    # If gcc is unavailable, fall back to pure-Go matching engine.
    if command -v gcc &> /dev/null && [ "$RUST_LIB_COPIED" = true ]; then
        echo "C compiler found; building with CGO + Rust matching engine"
        CGO_ENABLED=1 go build -tags cgo \
            -ldflags="-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
            -o "$ROOT/dist/gateway" ./cmd/server
    else
        if [ "$RUST_LIB_COPIED" = true ]; then
            echo "C compiler not found; building pure-Go gateway (Rust engine will not be linked)"
        else
            echo "C compiler or Rust engine unavailable; building pure-Go gateway"
        fi
        CGO_ENABLED=0 go build \
            -ldflags="-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
            -o "$ROOT/dist/gateway" ./cmd/server
    fi

    echo "=== Build complete: $ROOT/dist/gateway ==="
    echo "Run with: $ROOT/dist/gateway"
    echo ""
    echo "For Docker build, use: ./build.sh --docker"
fi
