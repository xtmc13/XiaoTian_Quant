# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant Gateway — Multi-stage Dockerfile
# ═══════════════════════════════════════════════════════════════════

# ── Stage 1: Web frontend builder ──────────────────────────────────
FROM node:22-alpine AS web-builder

WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --silent

COPY web/ ./
RUN npm run build

# ── Stage 2: Go backend builder ────────────────────────────────────
FROM golang:1.25-alpine AS go-builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Copy Go module files and download dependencies
COPY gateway/go.mod gateway/go.sum ./
RUN go mod download

# Copy pre-built web assets into spa/ directory (embedded via //go:embed)
COPY --from=web-builder /src/web/dist/ ./spa/

# Copy Go source
COPY gateway/ ./

# Ensure go.sum is complete with all dependencies
RUN go mod tidy

# Build static binary (CGo disabled — uses pure Go matching engine)
ARG VERSION=dev
ARG BUILD_TIME
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -trimpath \
    -o /gateway ./cmd/server

# ── Stage 3: Production runtime ────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -g 1000 xiaotian \
    && adduser -u 1000 -G xiaotian -s /bin/sh -D xiaotian

WORKDIR /app

# Copy binary
COPY --from=go-builder /gateway ./gateway

# Create data directory
RUN mkdir -p /app/data && chown -R xiaotian:xiaotian /app

USER xiaotian

ENV PORT=8080
ENV GIN_MODE=release
ENV TZ=Asia/Shanghai

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8080/api/health || exit 1

ENTRYPOINT ["./gateway"]
