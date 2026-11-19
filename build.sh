#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
VERSION="${VERSION:-$(git describe --tags 2>/dev/null || echo 'dev')}"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Parse arguments: --docker for Docker build, default to native build
BUILD_MODE="${1:-native}"
BUILD_RUST="${BUILD_RUST:-1}"
RUST_TARGET="${RUST_TARGET:-}"

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

    echo "--- Building Go gateway ---"
    cd "$ROOT/gateway"
    mkdir -p "$ROOT/dist"
    if [ "$BUILD_RUST" = "1" ]; then
        command -v cargo >/dev/null 2>&1 || { echo "cargo is required for the Rust production matcher" >&2; exit 1; }
        CC_COMMAND="${CC:-gcc}"
        command -v "${CC_COMMAND%% *}" >/dev/null 2>&1 || { echo "a C compiler is required for the Rust CGo bridge" >&2; exit 1; }

        echo "--- Building Rust matching engine ---"
        cd "$ROOT/engine"
        if [ -n "$RUST_TARGET" ]; then
            cargo build --release --target "$RUST_TARGET"
            RUST_LIB_DIR="$ROOT/engine/target/$RUST_TARGET/release"
        else
            cargo build --release
            RUST_LIB_DIR="$ROOT/engine/target/release"
        fi

        case "$(uname -s)" in
            Darwin) RUST_LIB="$RUST_LIB_DIR/libxt_matching.dylib"; RPATH="-Wl,-rpath,@loader_path" ;;
            MINGW*|MSYS*|CYGWIN*) RUST_LIB="$RUST_LIB_DIR/xt_matching.dll"; RPATH="" ;;
            *) RUST_LIB="$RUST_LIB_DIR/libxt_matching.so"; RPATH="-Wl,-rpath,\$ORIGIN" ;;
        esac
        test -f "$RUST_LIB" || { echo "Rust shared library not found: $RUST_LIB" >&2; exit 1; }
        cp "$RUST_LIB" "$ROOT/dist/"

        echo "--- Building Go gateway with Rust matching engine ---"
        cd "$ROOT/gateway"
        CGO_ENABLED=1 CC="${CC:-gcc}" \
        CGO_LDFLAGS="-L$RUST_LIB_DIR $RPATH" \
        go build -tags xtengine \
            -ldflags="-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
            -o "$ROOT/dist/gateway" ./cmd/server
    else
        echo "Building explicit development fallback with the Go matcher"
        CGO_ENABLED=0 go build \
            -ldflags="-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
            -o "$ROOT/dist/gateway" ./cmd/server
    fi

    echo "=== Build complete: $ROOT/dist/gateway ==="
    echo "Run with: $ROOT/dist/gateway"
    echo ""
    echo "For Docker build, use: ./build.sh --docker"
fi
