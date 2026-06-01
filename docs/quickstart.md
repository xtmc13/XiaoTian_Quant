# 快速开始

## 前置条件

- **Go** 1.25+
- **Node.js** 20+ (仅构建前端)
- **Rust** (可选, 仅构建撮合引擎)
- **Docker** (可选, 容器化部署)

## 方式一：一键构建

```bash
# 构建前端 + Go 网关 → dist/gateway
bash build.sh

# 配置环境变量
cp .env.example .env
# 编辑 .env 填入 API 密钥

# 运行
GIN_MODE=release ./dist/gateway
# 访问: http://localhost:8080
```

## 方式二：Docker 部署

```bash
cp .env.example .env
# 编辑 .env

cd web && npm ci && npm run build && cd ..
cp -r web/dist/* gateway/spa/

docker compose up -d
# 访问: http://localhost:8080
```

## 方式三：开发模式

```bash
# 终端 1: 前端
cd web && npm install && npm run dev
# → http://localhost:5173

# 终端 2: 后端
cd gateway && go run ./cmd/server
# → http://localhost:8080
```

## 配置

### 交易所 API

```bash
# 必填
BINANCE_API_KEY=your_key
BINANCE_API_SECRET=your_secret

# 可选
OKX_API_KEY=your_key
OKX_API_SECRET=your_secret
BYBIT_API_KEY=your_key
BYBIT_API_SECRET=your_secret
KRAKEN_API_KEY=your_key
KRAKEN_API_SECRET=your_secret
```

### AI / LLM

```bash
DEEPSEEK_API_KEY=your_key    # 默认
OPENAI_API_KEY=your_key       # 可选
ANTHROPIC_API_KEY=your_key    # 可选
```

### 通知渠道

```bash
# 邮件
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your_email@gmail.com
SMTP_PASS=your_app_password

# Telegram Bot
TELEGRAM_BOT_TOKEN=your_token
TELEGRAM_CHAT_ID=your_chat_id

# 飞书
LARK_WEBHOOK=https://open.feishu.cn/open-apis/bot/v2/hook/xxx

# 钉钉
DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=xxx
```

## 安全建议

1. **使用 Testnet 先测试** — 所有交易所均提供测试网
2. **API IP 白名单** — 交易所后台绑定固定 IP
3. **限制 API 权限** — 关闭提现权限，仅开交易+读取
4. **小资金起步** — 建议 100-500 USDT 验证策略
5. **不要共享 .env** — 已加入 .gitignore
