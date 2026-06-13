# XiaoTianQuant Gateway

Go 后端网关——量化交易系统的核心业务中枢。

## 技术栈

- **语言**: Go 1.25
- **Web 框架**: Gin
- **数据库**: SQLite (modernc.org/sqlite, pure Go)
- **缓存**: Redis (可选)
- **认证**: JWT (golang-jwt)
- **WebSocket**: Gorilla WebSocket
- **配置**: YAML + `.env` + 环境变量

## 目录结构

```
gateway/
├── cmd/server/          # 服务入口、路由注册
├── internal/
│   ├── adapter/         # 交易所适配器 (Binance/OKX/Bybit/...) + Rust CGo bridge
│   ├── ai/              # AI 策略生成、多模型投票
│   ├── arbitrage/       # 套利引擎
│   ├── backtest/        # 事件驱动回测引擎
│   ├── bot/             # Telegram/Discord Bot
│   ├── cache/           # Redis 缓存
│   ├── clock/           # 市场时钟
│   ├── community/       # 指标社区 + 策略市场
│   ├── config/          # 配置管理 (YAML + env)
│   ├── data/            # 历史数据管理 (K线/Tick 下载器)
│   ├── exchange/        # 交易所注册中心 + WebSocket 会话
│   ├── experiment/      # 实验管线 (A/B 测试、Walk-Forward)
│   ├── factor/          # 多因子/技术指标计算
│   ├── handler/         # HTTP REST 处理器
│   ├── hyperopt/        # 参数优化 (CMA-ES)
│   ├── indicator/       # 指标 IDE (解析、验证、沙箱)
│   ├── logging/         # 结构化日志
│   ├── middleware/      # CORS、JWT、限流、日志
│   ├── ml/              # 机器学习管线 (LightGBM/XGBoost)
│   ├── notify/          # 多渠道通知 (邮件/飞书/钉钉/Telegram)
│   ├── onchain/         # 链上数据 API
│   ├── order/           # 订单管理 (OMS) + 高级订单 (OCO/冰山/DCA)
│   ├── paper/           # 模拟交易
│   ├── pairlist/        # 交易对筛选器
│   ├── portfolio/       # 组合管理
│   ├── protection/      # 风控保护机制
│   ├── risk/            # 风控引擎 (15 项检查 + 熔断器)
│   ├── service/         # 业务服务层
│   ├── social/          # 社交交易引擎
│   ├── store/           # SQLite 存储层
│   ├── strategy/        # 策略运行时引擎 + 13 种内置策略
│   ├── watchdog/        # 健康检查
│   ├── ws/              # WebSocket Hub
│   └── model/           # 数据模型定义
├── docs/openapi.yaml    # OpenAPI 规范
└── spa/                 # 前端构建产物 (go:embed 嵌入)
```

## 快速开始

```bash
# 1. 前置依赖：Rust 撮合引擎
cd ../engine
cargo build --release          # 生成 libxt_matching.so / .dll
cd ../gateway

# 2. 配置
cp ../.env.example .env        # 修改你的配置
# 或创建 config.yaml (支持 ${ENV_VAR} 语法)

# 3. 运行
go run ./cmd/server/

# 4. 带 CGO 标签构建
go build -tags cgo -o gateway-server ./cmd/server/
```

## API

启动后访问：
- REST API: `http://localhost:8080/api/health`
- WebSocket: `ws://localhost:8080/ws`
- Prometheus metrics: `http://localhost:8080/metrics`

OpenAPI 规范见 `docs/openapi.yaml`。

## 内置策略 (13 种)

| 策略 | 类型 |
|------|------|
| breakout | 突破策略 |
| ema_cross | EMA 交叉 |
| macd | MACD 指标 |
| rsi | RSI 超买超卖 |
| bollinger_bands | 布林带 |
| atr_trailing_stop | ATR 跟踪止损 |
| dual_thrust | DualThrust 通道 |
| renko | Renko 砖形图 |
| grid_trading | 网格交易 |
| arbitrage | 套利 |
| market_making | 做市 |
| martingale | 马丁格尔 |
| wallstreet | 华尔街 |

## 交易所适配器

Binance · OKX · Bybit · Gate.io · MEXC · Kraken · Coinbase · Bitget · Alpaca · Interactive Brokers · MT5 · Tushare (A 股)

## 测试

```bash
go test ./internal/... -race -count=1
```
