#!/bin/bash
# =============================================================================
# 小天量化 - Linux 原生部署脚本
# 服务器: 43.165.186.96
# 部署方式: Linux原生 (不使用Docker)
# =============================================================================
set -e

PROJECT_DIR="/opt/xiaotian-quant"
GITHUB_REPO="https://github.com/xtmc13/XiaoTian_Quant.git"
GO_VERSION="1.22"
NODE_VERSION="20"

echo "========================================="
echo "  小天量化 - Linux 原生部署"
echo "========================================="

# 0. 系统更新和基础依赖
echo "[1/8] 安装系统依赖..."
apt-get update -qq
apt-get install -y -qq git curl wget sqlite3 nginx build-essential

# 1. 安装 Go (如果未安装)
echo "[2/8] 检查 Go 环境..."
if ! command -v go &> /dev/null || [[ $(go version | grep -oP '\d+\.\d+') != "$GO_VERSION" ]]; then
    echo "    安装 Go ${GO_VERSION}..."
    wget -q "https://go.dev/dl/go${GO_VERSION}.0.linux-amd64.tar.gz" -O /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    rm /tmp/go.tar.gz
fi
go version

# 2. 安装 Node.js (如果未安装)
echo "[3/8] 检查 Node.js 环境..."
if ! command -v node &> /dev/null || [[ $(node -v | grep -oP '\d+' | head -1) -lt ${NODE_VERSION} ]]; then
    echo "    安装 Node.js ${NODE_VERSION}..."
    curl -fsSL "https://deb.nodesource.com/setup_${NODE_VERSION}.x" | bash -
    apt-get install -y -qq nodejs
fi
node -v && npm -v

# 3. 克隆/更新代码
echo "[4/8] 拉取最新代码..."
if [ -d "$PROJECT_DIR/.git" ]; then
    cd "$PROJECT_DIR"
    git pull origin main
else
    rm -rf "$PROJECT_DIR"
    git clone "$GITHUB_REPO" "$PROJECT_DIR"
    cd "$PROJECT_DIR"
fi

# 4. 构建前端
echo "[5/8] 构建前端..."
cd "$PROJECT_DIR/web"
npm install --legacy-peer-deps 2>&1 | tail -5
npm run build 2>&1 | tail -10
cp -r dist/* "$PROJECT_DIR/gateway/spa/"

# 5. 构建后端
echo "[6/8] 构建后端..."
cd "$PROJECT_DIR/gateway"
# 确保 spa 目录存在
mkdir -p spa
go mod tidy
go build -o xiaotian-gateway ./cmd/server/
chmod +x xiaotian-gateway

# 6. 初始化数据库
echo "[7/8] 初始化数据库..."
mkdir -p "$PROJECT_DIR/data"
if [ ! -f "$PROJECT_DIR/data/xiaotian.db" ]; then
    sqlite3 "$PROJECT_DIR/data/xiaotian.db" "VACUUM;"
fi

# 运行数据库迁移 (如果有)
if [ -f "$PROJECT_DIR/gateway/migrate" ]; then
    cd "$PROJECT_DIR/gateway" && ./migrate up 2>/dev/null || true
fi

# 7. 配置 Nginx 反向代理
echo "[8/8] 配置 Nginx..."
cat > /etc/nginx/sites-available/xiaotian << 'NGINX_EOF'
server {
    listen 80;
    server_name _;

    # API 反向代理
    location /api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    # WebSocket 支持
    location /ws {
        proxy_pass http://127.0.0.1:8080/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }

    # 静态文件 (前端)
    location / {
        proxy_pass http://127.0.0.1:8080/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
    }
}
NGINX_EOF

ln -sf /etc/nginx/sites-available/xiaotian /etc/nginx/sites-enabled/xiaotian
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl restart nginx

# 8. 创建 systemd 服务
cat > /etc/systemd/system/xiaotian-gateway.service << 'SERVICE_EOF'
[Unit]
Description=XiaoTian Quant Gateway
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/xiaotian-quant/gateway
Environment="DATABASE_URL=/opt/xiaotian-quant/data/xiaotian.db"
Environment="JWT_SECRET=xiaotian-secret-key-change-in-production"
Environment="PORT=8080"
ExecStart=/opt/xiaotian-quant/gateway/xiaotian-gateway
Restart=always
RestartSec=5
StandardOutput=append:/var/log/xiaotian-gateway.log
StandardError=append:/var/log/xiaotian-gateway.log

[Install]
WantedBy=multi-user.target
SERVICE_EOF

systemctl daemon-reload
systemctl enable xiaotian-gateway

echo ""
echo "========================================="
echo "  部署完成!"
echo "========================================="
echo ""
echo "  启动服务:  systemctl start xiaotian-gateway"
echo "  查看日志:  journalctl -u xiaotian-gateway -f"
echo "  查看状态:  systemctl status xiaotian-gateway"
echo "  重启服务:  systemctl restart xiaotian-gateway"
echo ""
echo "  访问地址:  http://43.165.186.96"
echo ""
echo "========================================="
