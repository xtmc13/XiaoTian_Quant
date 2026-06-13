.PHONY: help install build start stop restart logs clean docker-build docker-up docker-down docker-logs dev prod test

# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant Makefile — Unified build & deploy commands
# ═══════════════════════════════════════════════════════════════════

PROJECT_NAME := xiaotianquant
VERSION := 3.0.0

# Detect OS
UNAME_S := $(shell uname -s 2>/dev/null || echo Windows)
ifeq ($(UNAME_S),Windows)
	OS := windows
	SHELL := cmd
else ifeq ($(UNAME_S),Darwin)
	OS := macos
else
	OS := linux
endif

# ── Help ────────────────────────────────────────────────────────
help:
	@echo "╔══════════════════════════════════════════════════════════════╗"
	@echo "║           XiaoTianQuant v$(VERSION) 构建工具                  ║"
	@echo "╚══════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "  make install        — 一键安装所有依赖并构建"
	@echo "  make build          — 构建前端 + 后端"
	@echo "  make start          — 启动所有服务（本地模式）"
	@echo "  make stop           — 停止所有服务"
	@echo "  make restart        — 重启所有服务"
	@echo "  make logs           — 查看运行日志"
	@echo "  make clean          — 清理构建产物"
	@echo ""
	@echo "  Docker 部署:"
	@echo "  make docker-build   — 构建 Docker 镜像"
	@echo "  make docker-up      — 启动 Docker 服务"
	@echo "  make docker-down    — 停止 Docker 服务"
	@echo "  make docker-logs    — 查看 Docker 日志"
	@echo ""
	@echo "  开发:"
	@echo "  make dev            — 启动开发服务器（热重载）"
	@echo "  make dev-sandbox    — 启动 Python 沙箱"
	@echo "  make dev-ccxt       — 启动 CCXT Bridge"
	@echo "  make test           — 运行测试"
	@echo "  make lint           — 代码检查"
	@echo ""

# ── Install ─────────────────────────────────────────────────────
install:
ifeq ($(OS),windows)
	@echo "Windows 系统，请运行: .\install.ps1"
	@powershell -ExecutionPolicy Bypass -File install.ps1
else
	@bash install.sh
endif

# ── Build ───────────────────────────────────────────────────────
build: build-web build-go
	@echo "✅ 构建完成"

build-web:
	@echo "📦 构建前端..."
	@cd web && npm ci && npm run build

build-go:
	@echo "🔨 构建 Go 后端..."
	@rm -rf gateway/spa/*
	@cp -r web/dist/* gateway/spa/ 2>/dev/null || true
ifeq ($(OS),windows)
	@cd gateway && CGO_ENABLED=0 go build \
		-ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-trimpath \
		-o gateway-server$(shell go env GOEXE 2>/dev/null) \
		./cmd/server
else
	@cd gateway && CGO_ENABLED=1 go build \
		-ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-trimpath \
		-o gateway-server$(shell go env GOEXE 2>/dev/null) \
		./cmd/server
endif

# ── Start/Stop ──────────────────────────────────────────────────
start:
ifeq ($(OS),windows)
	@start-all.bat
else
	@./start.sh
endif

stop:
ifeq ($(OS),windows)
	@taskkill /F /IM gateway-server.exe 2>nul || true
	@taskkill /F /IM python.exe 2>nul || true
	@taskkill /F /IM node.exe 2>nul || true
else
	@pkill -f "gateway-server" 2>/dev/null || true
	@pkill -f "uvicorn main:app" 2>/dev/null || true
	@pkill -f "vite.js" 2>/dev/null || true
endif

restart: stop start

# ── Logs ────────────────────────────────────────────────────────
logs:
ifeq ($(OS),windows)
	@echo "Windows 日志请查看各 CMD 窗口"
else
	@tail -f gateway/*.log sandbox/ml_server/*.log web/*.log 2>/dev/null || echo "日志文件不存在"
endif

# ── Clean ───────────────────────────────────────────────────────
clean:
	@echo "🧹 清理构建产物..."
	@rm -rf web/dist
	@rm -rf gateway/spa/*
	@rm -f gateway/gateway-server gateway/gateway-server.exe
	@rm -rf web/node_modules/.cache

# ── Docker ─────────────────────────────────────────────────────
docker-build:
	@echo "🐳 构建 Docker 镜像..."
	@docker compose -f docker-compose.yml build

docker-up:
	@echo "🚀 启动 Docker 服务..."
	@docker compose -f docker-compose.yml up -d

docker-down:
	@echo "🛑 停止 Docker 服务..."
	@docker compose -f docker-compose.yml down

docker-logs:
	@docker compose -f docker-compose.yml logs -f

docker-server-up:
	@echo "🚀 启动生产环境 Docker..."
	@docker compose -f scripts/setup/docker-compose.server.yml up -d

docker-server-down:
	@echo "🛑 停止生产环境 Docker..."
	@docker compose -f scripts/setup/docker-compose.server.yml down

# ── Development ────────────────────────────────────────────────
dev:
	@echo "🔧 启动开发模式..."
	@make -j3 dev-gateway dev-ml dev-web

dev-gateway:
	@cd gateway && go run ./cmd/server

dev-ml:
	@cd sandbox/ml_server && python3 -m uvicorn main:app --host 0.0.0.0 --port 8001 --reload

dev-web:
	@cd web && npm run dev

dev-sandbox:
	@echo "🐍 启动 Python 沙箱..."
	@cd sandbox && python3 main.py

dev-ccxt:
	@echo "🌐 启动 CCXT Bridge..."
	@cd sandbox && python3 ccxt_bridge.py

# ── Test ───────────────────────────────────────────────────────
test:
	@echo "🧪 运行测试..."
	@cd gateway && go test ./...
	@cd web && npm test

lint:
	@echo "🔍 代码检查..."
	@cd gateway && golangci-lint run || echo "请安装 golangci-lint"
	@cd web && npm run lint

# ── Deploy ─────────────────────────────────────────────────────
deploy-server:
	@echo "🚀 部署到服务器..."
	@rsync -avz --exclude='.git' --exclude='node_modules' --exclude='web/dist' \
		. user@server:/opt/xiaotianquant/
	@ssh user@server "cd /opt/xiaotianquant && make docker-server-up"
