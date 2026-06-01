# 小天量化 XiaoTianQuant v3.0

> AI 驱动的多资产量化交易平台 — Go 网关 + React 前端 + Rust 撮合引擎 + Python ML，事件驱动，回测实盘一体。
>
> **v3.0 新增**: ML 预测管线 · 纯 Go 本地推理 · RL 环境 · Pairlist · Protection · TrailingStop · Hyperopt · DCA · 策略社区 · Admin 面板 · Telegram/Discord Bot · CCXT Bridge · 8 交易所 · i18n 多语言 · OAuth 登录 · Billing 会员 · 凭证加密 · Edge 分析

## 架构概览

```
┌─────────────────────────────────────────────────┐
│                    Web 前端                       │
│         React 19 + TypeScript + Vite             │
│     TailwindCSS · ECharts · Lightweight Charts    │
└─────────────────────┬───────────────────────────┘
                      │ HTTP REST + WebSocket
┌─────────────────────▼───────────────────────────┐
│                  Go 网关 (Gin)                    │
│  ┌──────────┬──────────┬──────────┬──────────┐  │
│  │ 策略引擎  │ AI 服务  │ 回测引擎  │ 风控系统  │  │
│  ├──────────┼──────────┼──────────┼──────────┤  │
│  │ 订单管理  │ 持仓管理  │ 通知服务  │ 监控告警  │  │
│  └──────────┴──────────┴──────────┴──────────┘  │
│          交易所适配器 (Binance/OKX/...)           │
│              SQLite · Redis (可选)                │
└─────────────────────┬───────────────────────────┘
                      │ FFI (CGo)
┌─────────────────────▼───────────────────────────┐
│               Rust 撮合引擎                       │
│         价格-时间优先订单簿 · 交易匹配             │
└─────────────────────────────────────────────────┘
```

| 层 | 语言 | 职责 |
|---|------|------|
| **Web 前端** | React 19 + TypeScript + Vite | 仪表盘、K 线图表、AI 策略生成、回测面板、交易下单、资产管理 |
| **Go 网关** | Go + Gin 框架 | REST API、策略引擎、交易所适配、回测、风控、WebSocket 推送、通知 |
| **Rust 引擎** | Rust (cdylib) | 高性能订单簿撮合，通过 FFI 被 Go 网关调用 |

## 技术栈

### 后端 (gateway/)
- **框架**: Gin v1.10 (Go)
- **数据库**: SQLite (modernc.org/sqlite, 纯 Go 实现)
- **缓存**: Redis 7 (可选)
- **认证**: JWT (golang-jwt/jwt/v5)
- **WebSocket**: gorilla/websocket
- **配置**: YAML + 环境变量覆盖

### 前端 (web/)
- **框架**: React 19 + TypeScript
- **构建**: Vite 6
- **样式**: TailwindCSS 3.4
- **图表**: ECharts 5 · Lightweight Charts 4 · KLineCharts Pro
- **状态管理**: Zustand 5 · TanStack React Query 5
- **路由**: React Router 7
- **图标**: Lucide React

### 撮合引擎 (engine/)
- **语言**: Rust (edition 2021)
- **序列化**: Serde + serde_json
- **编译**: cdylib + rlib, LTO 优化

## 项目结构

```
xiaotian_quant/
├── gateway/                        # Go 后端网关
│   ├── cmd/server/main.go          # 入口 — 路由注册 + 启动
│   ├── internal/
│   │   ├── adapter/                # 交易所适配器
│   │   │   ├── binance.go          #   币安 (REST + WebSocket)
│   │   │   ├── okx.go              #   OKX
│   │   │   ├── coinbase.go         #   Coinbase
│   │   │   ├── gateio.go           #   Gate.io
│   │   │   ├── mexc.go             #   MEXC
│   │   │   ├── matching.go         #   撮合引擎 FFI 桥接
│   │   │   └── cgo_bridge.go       #   CGo 动态库加载
│   │   ├── agent/                  # Agent 网关 (MCP 协议)
│   │   ├── ai/                     # AI 策略生成 · 多模型投票
│   │   ├── backtest/               # 事件驱动回测引擎
│   │   ├── cache/                  # Redis 缓存
│   │   ├── clock/                  # 市场时钟
│   │   ├── config/                 # 配置管理 (YAML + ENV)
│   │   ├── event/                  # 事件总线
│   │   ├── exchange/               # 交易所注册中心 · WebSocket 会话
│   │   ├── factor/                 # 多因子 (技术指标)
│   │   ├── handler/                # HTTP 处理器
│   │   │   ├── agent.go            #   Agent 令牌管理
│   │   │   ├── ai.go               #   AI 分析/生成/回测
│   │   │   ├── auth.go             #   认证 (登录/注册/JWT)
│   │   │   ├── dashboard.go        #   仪表盘摘要
│   │   │   ├── freqtrade.go        #   Freqtrade 兼容 API
│   │   │   ├── market.go           #   行情/K线/订单簿
│   │   │   ├── order.go            #   订单管理
│   │   │   ├── portfolio.go        #   持仓/资产
│   │   │   ├── settings.go         #   系统设置
│   │   │   ├── strategy.go         #   策略 CRUD
│   │   │   ├── strategy_ai.go      #   AI 策略生成
│   │   │   └── ws.go               #   WebSocket 推送
│   │   ├── logging/                # 结构化日志
│   │   ├── metrics/                # 监控指标
│   │   ├── middleware/             # CORS · JWT 鉴权 · 限流
│   │   ├── model/                  # 数据模型
│   │   ├── notify/                 # 多渠道通知 (邮件/飞书/钉钉/Telegram)
│   │   ├── order/                  # 订单管理系统 (OMS)
│   │   ├── paper/                  # 模拟交易
│   │   ├── portfolio/              # 组合管理
│   │   ├── risk/                   # 风控引擎 (12 维度)
│   │   ├── service/                # 业务服务层
│   │   ├── store/                  # SQLite 存储层
│   │   ├── strategy/               # 策略运行时
│   │   │   ├── compiler.go         #   策略编译器
│   │   │   ├── engine.go           #   策略引擎
│   │   │   └── strategies/         #   内置策略
│   │   │       ├── breakout.go     #     突破策略
│   │   │       └── grid.go         #     网格策略
│   │   └── watchdog/               # 健康检查
│   ├── spa/                        # 前端构建产物 (go:embed 嵌入)
│   ├── go.mod
│   └── go.sum
├── web/                            # React 前端源码
│   ├── src/
│   │   ├── pages/
│   │   │   ├── Dashboard.tsx       #   仪表盘 — 总览KPI
│   │   │   ├── Trading.tsx         #   交易页 — K线+订单簿+下单
│   │   │   ├── AI.tsx              #   AI 策略生成与对话
│   │   │   ├── Strategy.tsx        #   策略管理
│   │   │   ├── Backtest.tsx        #   回测分析
│   │   │   ├── Portfolio.tsx       #   资产管理
│   │   │   ├── Bots.tsx            #   交易机器人
│   │   │   ├── Settings.tsx        #   系统设置
│   │   │   ├── ExchangeAccount.tsx #   交易所账户
│   │   │   ├── IndicatorCommunity.tsx # 指标社区
│   │   │   ├── IndicatorIDE.tsx    #   指标开发 IDE
│   │   │   └── Login.tsx           #   登录页
│   │   ├── components/             #   通用组件
│   │   ├── hooks/                  #   自定义 Hooks
│   │   └── lib/                    #   工具函数 · API 客户端
│   ├── package.json
│   └── vite.config.ts
├── engine/                         # Rust 撮合引擎
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs                  #   库入口
│       ├── orderbook.rs            #   订单簿实现
│       ├── matching.rs             #   撮合逻辑
│       └── ffi.rs                  #   FFI 导出 (供 Go CGo 调用)
├── build.sh                        # 一键构建脚本
├── Dockerfile                      # 多阶段 Docker 构建
├── docker-compose.yml              # Docker Compose 部署
├── .env.example                    # 环境变量模板
├── strategy_configs.json           # 策略配置示例
└── IMPROVEMENT_PLAN.md             # 后续改进计划
```

## 快速开始

### 前置条件

- **Go** 1.25+
- **Node.js** 20+ (仅构建前端时需要)
- **Rust** (仅构建撮合引擎时需要, 可跳过)
- **Docker** (可选, 容器化部署)

### 方式一：一键构建 (推荐)

```bash
# 构建前端 + Go 网关 → dist/gateway
bash build.sh

# 运行 (需先配置环境变量)
GIN_MODE=release ./dist/gateway
```

构建流程：
1. `npm ci && npm run build` — 编译 React 前端到 `web/dist/`
2. 复制前端产物到 `gateway/spa/`（Go embed 嵌入）
3. `go build -ldflags="-s -w"` — 编译 Go 静态二进制

### 方式二：Docker 部署

```bash
# 1. 复制环境变量并填写 API 密钥
cp .env.example .env

# 2. 构建前端 (首次或前端有修改时)
cd web && npm ci && npm run build && cd ..
cp -r web/dist/* gateway/spa/

# 3. 启动
docker compose up -d

# 访问: http://localhost:8080
```

### 方式三：开发模式

```bash
# 终端 1: 启动前端开发服务器
cd web
npm install
npm run dev          # → http://localhost:5173

# 终端 2: 启动 Go 网关
cd gateway
go run ./cmd/server  # → http://localhost:8080
```

## 配置

通过环境变量或 YAML 配置文件 (`gateway.yaml`) 配置：

### 交易所

```bash
# 币安
BINANCE_API_KEY=your_api_key
BINANCE_API_SECRET=your_api_secret

# OKX
OKX_API_KEY=your_api_key
OKX_API_SECRET=your_api_secret
```

### AI / LLM

```bash
# DeepSeek (默认)
DEEPSEEK_API_KEY=your_api_key

# OpenAI (可选)
OPENAI_API_KEY=your_api_key

# Anthropic Claude (可选)
ANTHROPIC_API_KEY=your_api_key
```

### 通知渠道

```bash
# 邮件
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your_email@gmail.com
SMTP_PASS=your_app_password
SMTP_FROM="XiaoTianQuant <noreply@xtquant.com>"
SMTP_TO=admin@example.com

# 飞书/Lark
LARK_WEBHOOK=https://open.feishu.cn/open-apis/bot/v2/hook/xxx
LARK_SIGNING_KEY=your_signing_key

# 钉钉
DINGTALK_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=xxx
DINGTALK_SECRET=your_secret

# Telegram
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id
```

### 缓存 (可选)

```bash
CACHE_ENABLED=true
REDIS_URL=redis://localhost:6379
```

完整配置项见 [.env.example](.env.example) 和 [gateway.yaml](gateway/gateway.yaml)。

## 功能特性

### 交易执行
- **5 家交易所**: 币安 · OKX · Coinbase · Gate.io · MEXC
- **统一接口**: REST 下单 + WebSocket 实时数据推送
- **订单类型**: 市价单 · 限价单 · 止损单 · OCO
- **模拟交易**: 内置 Paper Trading，零成本测试

### 策略引擎
- **内置策略**: 网格交易 · 突破策略
- **自定义策略**: Go 插件式策略运行时
- **AI 生成**: LLM 辅助策略代码生成与修复
- **策略生命周期**: 创建 → 回测 → 模拟 → 实盘 → 监控

### 回测系统
- **事件驱动**: 精准模拟交易所行为
- **费用建模**: 手续费 + 滑点
- **指标输出**: 夏普比率 · 最大回撤 · 胜率 · 盈亏比 · 收益率曲线

### 风控系统 (12 维度)

| 维度 | 说明 |
|------|------|
| 单笔限额 | 最大订单金额限制 |
| 日限额 | 日累计交易上限 |
| 并发限制 | 最大同时挂单数 |
| 持仓限制 | 最大持仓币种数 |
| 仓位暴露 | 单仓位/净暴露比例 |
| 最大回撤 | 日内最大回撤熔断 |
| 连续亏损 | 连续止损触发暂停 |
| 资金费率 | 合约费率上限 |
| 保证金率 | 维持保证金率 |
| 价格偏离 | 市价偏离熔断 |
| 波动率 | 异常波动限制 |
| 熔断器 | 短时间内大量错误触发熔断 |

### AI 集成
- **多模型支持**: DeepSeek · OpenAI GPT-4o · Anthropic Claude
- **多模型投票**: 多 LLM 共识决策，提高信号质量
- **策略生成**: 自然语言描述 → Python 策略代码
- **策略分析**: 回测结果 AI 诊断与优化建议
- **快速扫描**: 多币种技术面快速筛选
- **Agent 网关**: MCP 协议支持，Cursor/Claude Code 可直接操控

### 前端页面

| 页面 | 功能 |
|------|------|
| **仪表盘** | KPI 卡片 · 收益日历 · 排行榜 · 实时摘要 |
| **交易** | 实时 K 线 · 订单簿深度图 · 下单面板 · 持仓卡片 |
| **AI** | AI 对话 · 策略生成 · 代码编辑器 · 回测集成 |
| **策略** | 策略 CRUD · 批量启停 · 模板管理 · 运行日志 |
| **回测** | 参数配置 · 结果图表 · 交易明细 · 指标分析 |
| **资产** | 持仓列表 · 盈亏明细 · 历史收益 · 多账户聚合 |
| **机器人** | 自动化任务 · 网格机器人 · 状态监控 |
| **设置** | 交易所配置 · AI 模型 · 通知渠道 · 用户管理 |

### 通知渠道
- **邮件** (SMTP)
- **飞书/Lark** (Webhook + 签名)
- **钉钉** (Webhook + 签名)
- **Telegram** (Bot API)
- **本地日志** (结构化 JSON/Text)

## API 端点 (主要)

### 认证 `/api/auth`
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/login` | 用户登录 |
| POST | `/register` | 用户注册 |
| GET | `/me` | 当前用户信息 (需 JWT) |

### 行情 `/api`
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/klines/:symbol` | K 线数据 |
| GET | `/market/klines` | 市场 K 线 |
| GET | `/market/orderbook` | 订单簿深度 |
| GET | `/market/trades` | 最近成交 |
| GET | `/market/snapshot` | 市场快照 |
| GET | `/symbols/search` | 币种搜索 |

### 交易 `/api`
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/orders` | 挂单列表 |
| POST | `/orders` | 下单 |
| DELETE | `/orders/:id` | 撤单 |
| POST | `/orders/cancel-all` | 全部撤单 |
| GET | `/trades` | 成交历史 |
| GET | `/account/balance` | 账户余额 |
| GET | `/portfolio/summary` | 资产摘要 |
| GET | `/portfolio/positions` | 持仓明细 |

### 策略 `/api/strategies`
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/configs` | 策略列表 |
| POST | `/configs` | 创建策略 |
| PUT | `/configs/:id` | 更新策略 |
| DELETE | `/configs/:id` | 删除策略 |
| POST | `/configs/:id/start` | 启动策略 |
| POST | `/configs/:id/stop` | 停止策略 |
| POST | `/configs/batch-start` | 批量启动 |
| GET | `/templates` | 策略模板 |

### 回测 `/api`
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/backtest/run` | 运行回测 |
| POST | `/native/backtest` | 原生回测 |

### AI `/api`
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/ai/generate` | AI 策略生成 |
| POST | `/ai/backtest` | AI 回测分析 |
| POST | `/ai/chat` | AI 对话 |
| POST | `/ai/analyze` | AI 市场分析 |
| GET | `/ai/quickscan` | 快速扫描 |

### WebSocket
| 路径 | 说明 |
|------|------|
| `/ws` | 实时行情与订单更新推送 |

## 风控说明

平台内置 12 维度风控引擎，每次下单前逐项检查：

1. 价格偏离校验 (防止插针/异常价格成交)
2. 波动率熔断 (极端行情暂停)
3. 资金费率保护 (合约负费率保护)
4. 保证金率校验 (防止强平)
5. 连续亏损熔断 (追涨杀跌保护)
6. 日内最大回撤熔断
7. 仓位暴露限制
8. 日限额/并发限制
9. 平台熔断器 (短时间大量错误触发全站暂停)

## 安全提示

1. **先用 Testnet 测试** — 币安/OKX 均提供测试网
2. **设置 API IP 白名单** — 交易所后台绑定固定 IP
3. **限制 API 权限** — 关闭提现权限，仅开交易+读取
4. **小资金起步** — 建议先 100-500 USDT 验证策略逻辑
5. **不要共享 .env** — `.env` 和 `gateway.yaml` 已加入 `.gitignore`
6. **定期更新** — 关注依赖安全公告

## 路线图

参见 [IMPROVEMENT_PLAN.md](IMPROVEMENT_PLAN.md)，包含：

| 优先级 | 模块 |
|:------:|------|
| 高 | WebSocket 断线重连增强 · 回测引擎接入真实数据 · Monaco 代码编辑器 |
| 中 | 策略社区 · 券商账户连接 · 资产管理页面 · 移动端适配 |
| 低 | 前后端分离 · 策略参数优化 (Hyperopt) · 多语言完整覆盖 · 通知中心 |

## License

MIT

---

<p align="center"><sub>XiaoTianQuant v3.0 — AI 驱动的量化交易平台</sub></p>
