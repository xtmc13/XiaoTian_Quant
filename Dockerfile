# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant Gateway — Multi-stage Dockerfile
# Builds: Rust cdylib → Web SPA → Go backend (CGO-linked) → Alpine runtime
#
# 多架构：base 镜像（rust/node/golang/alpine）均为多架构官方镜像。
# 使用 buildx 构建 arm64：
#   docker buildx build --platform linux/arm64 -t xiaotian-quant/gateway:latest .
# buildx 自动注入 TARGETARCH（amd64/arm64）；Rust target 默认随之推导，
# 也可用 --build-arg RUST_TARGET=<triple> 显式覆盖。
# 注意：alpine 是 musl 工具链，默认选用 *-linux-musl target 与运行时匹配
#（旧默认 x86_64-unknown-linux-gnu 在 rust:alpine 上既未安装 std 也缺 gnu 链接器）。
# ═══════════════════════════════════════════════════════════════════

# ── Stage 0: Rust matching engine builder ──────────────────────────
FROM rust:1-alpine AS rust-builder

RUN apk add --no-cache git musl-dev

# musl 默认静态链接(crt-static)，与 cdylib 不兼容会导致 .so 无法生成；
# 关闭 crt-static 以输出动态库（运行时同为 musl 的 alpine 可正常加载）。
ENV RUSTFLAGS="-C target-feature=-crt-static"

WORKDIR /src/engine

COPY engine/Cargo.toml engine/Cargo.lock ./
COPY engine/src/ ./src/
COPY engine/benches/ ./benches/

ARG TARGETARCH=amd64
ARG RUST_TARGET=""
RUN set -e; \
    if [ -z "${RUST_TARGET}" ]; then \
      case "${TARGETARCH}" in \
        amd64) RUST_TARGET=x86_64-unknown-linux-musl ;; \
        arm64) RUST_TARGET=aarch64-unknown-linux-musl ;; \
        *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
      esac; \
    fi; \
    cargo build --release --target "${RUST_TARGET}"; \
    mkdir -p /engine-dist; \
    cp "target/${RUST_TARGET}/release/libxt_matching.so" /engine-dist/libxt_matching.so

# ── Stage 1: Web frontend builder ──────────────────────────────────
FROM node:22-alpine AS web-builder

WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --silent
COPY web/ ./
# 容器内嵌部署必须用绝对基路径：相对路径在深链接（如 /trading/spot）下
# 会解析成 /trading/assets/... 导致资产 404 白屏。注意不能写 `npm run build -- --base=/`
# （npm 会把参数追加到整条命令末尾，vite 收不到），必须直接调 vite。
# Electron/本地构建仍走 package.json 的 build 脚本（vite.config.ts 里 base: './'）。
RUN npx vite build --base=/ \
 && sed -i "s/const CACHE_VERSION = 'v4'/const CACHE_VERSION = 'v$(date +%s)'/" dist/sw.js

# ── Stage 2: Go backend builder (CGO enabled to link Rust cdylib) ──
FROM golang:1.25-alpine AS go-builder

RUN apk add --no-cache git ca-certificates tzdata musl-dev gcc g++

WORKDIR /src

# Copy Rust library output（固定路径，与 target triple 解耦）
COPY --from=rust-builder /engine-dist/ /engine-dist/

# Copy Go module files and download dependencies
COPY gateway/go.mod gateway/go.sum ./
RUN go mod download

# Copy Go source
COPY gateway/ ./

# Copy pre-built web assets into spa/ directory (embedded via //go:embed)
# 必须放在 COPY gateway/ 之后，否则 repo 内 spa/ 占位文件会覆盖前端产物
COPY --from=web-builder /src/web/dist/ ./spa/

# Ensure go.sum is complete with all dependencies
RUN go mod tidy

# Build with CGO enabled so the Rust cdylib can be linked
ARG VERSION=dev
ARG BUILD_TIME
ARG TARGETARCH=amd64
RUN CGO_ENABLED=1 \
    CGO_LDFLAGS="-L/engine-dist -lxt_matching -lstdc++ -ldl -lm" \
    GOOS=linux GOARCH=${TARGETARCH} \
    go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -trimpath \
    -o /gateway ./cmd/server

# ── Stage 3: Production runtime ────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl libstdc++ \
    && addgroup -g 1000 xiaotian \
    && adduser -u 1000 -G xiaotian -s /bin/sh -D xiaotian

WORKDIR /app

# Copy binary
COPY --from=go-builder /gateway ./gateway

# Copy Rust library for runtime linking
COPY --from=rust-builder /engine-dist/libxt_matching.so /app/libxt_matching.so

# Create data directory
RUN mkdir -p /app/data && chown -R xiaotian:xiaotian /app

USER xiaotian

ENV PORT=8080
ENV GIN_MODE=release
ENV TZ=Asia/Shanghai
ENV LD_LIBRARY_PATH=/app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8080/api/health || exit 1

ENTRYPOINT ["./gateway"]
