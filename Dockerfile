# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant Gateway — Multi-stage Dockerfile
# Builds: Rust cdylib → Web SPA → Go backend (CGO-linked) → Alpine runtime
# ═══════════════════════════════════════════════════════════════════

# ── Stage 0: Rust matching engine builder ──────────────────────────
FROM rust:1-alpine AS rust-builder

RUN apk add --no-cache git musl-dev

WORKDIR /src/engine

COPY engine/Cargo.toml engine/Cargo.lock ./
COPY engine/src/ ./src/
COPY engine/benches/ ./benches/

ARG RUST_TARGET=x86_64-unknown-linux-gnu
RUN cargo build --release --target $RUST_TARGET

# ── Stage 1: Web frontend builder ──────────────────────────────────
FROM node:22-alpine AS web-builder

WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --silent
COPY web/ ./
RUN npm run build

# ── Stage 2: Go backend builder (CGO enabled to link Rust cdylib) ──
FROM golang:1.23-alpine AS go-builder

RUN apk add --no-cache git ca-certificates tzdata musl-dev gcc

WORKDIR /src

# Copy Rust library output
COPY --from=rust-builder /src/engine/target/${RUST_TARGET:-x86_64-unknown-linux-gnu}/release/ /src/engine/target/${RUST_TARGET:-x86_64-unknown-linux-gnu}/release/

# Copy Go module files and download dependencies
COPY gateway/go.mod gateway/go.sum ./
RUN go mod download

# Copy pre-built web assets into spa/ directory (embedded via //go:embed)
COPY --from=web-builder /src/web/dist/ ./spa/

# Copy Go source
COPY gateway/ ./

# Ensure go.sum is complete with all dependencies
RUN go mod tidy

# Build with CGO enabled so the Rust cdylib can be linked
ARG VERSION=dev
ARG BUILD_TIME
ARG RUST_TARGET=x86_64-unknown-linux-gnu
RUN CGO_ENABLED=1 \
    CGO_LDFLAGS="-L/src/engine/target/${RUST_TARGET:-x86_64-unknown-linux-gnu}/release -lxt_matching -lstdc++ -ldl -lm" \
    CGO_CFLAGS="-I/src/engine/target/${RUST_TARGET:-x86_64-unknown-linux-gnu}/release" \
    GOOS=linux GOARCH=amd64 \
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
COPY --from=rust-builder /src/engine/target/${RUST_TARGET:-x86_64-unknown-linux-gnu}/release/libxt_matching.so \
    /app/libxt_matching.so

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
