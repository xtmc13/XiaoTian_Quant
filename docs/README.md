# XiaoTianQuant v3.0 文档

> AI 驱动的多资产量化交易平台 — Go 网关（单二进制主链路） + React 前端；撮合为纯 Go 实现，Rust 引擎已弃用仅作基准

## 快速导航

| 文档 | 说明 |
|------|------|
| [快速开始](quickstart.md) | 安装、配置、Docker 部署 |
| [架构概览](../ARCHITECTURE.md) | 系统架构、数据流、组件关系 |
| [API 参考](api/rest-api.md) | 完整 REST API 文档 |
| [策略开发](strategy-guide.md) | 策略编写、回测、参数优化 |
| [部署指南](DEPLOYMENT.md) | 生产环境部署 |

## 项目简介

XiaoTianQuant 是一个全栈量化交易平台，采用两层主架构（Rust 撮合引擎已弃用，仅作基准参照，不进主链路）：

```
Web 前端 (React 19 + TypeScript)
       │ HTTP REST + WebSocket
Go 网关 (Gin) — 含纯 Go 撮合引擎（价格-时间优先订单簿，唯一主链路）
```

### 核心特性

- **多交易所**: Binance, OKX, Bybit, Bitget, MEXC, Gate.io, Kraken, Coinbase, Alpaca, IBKR
- **AI 策略生成**: 多 LLM 投票 + 7 Agent 协作管道
- **回测引擎**: 事件驱动, 真实数据, 缓存, 分解分析
- **超参优化**: Grid Search + CMA-ES + TPE + 差分进化，20 种损失函数 (Sharpe/Sortino/Calmar 等)
- **风控系统**: 15 维度检查 + 8 种保护机制 + 熔断器
- **策略社区**: 发布/评论/评分/排行榜
- **通知**: Email, 飞书, 钉钉, Telegram Bot (32 命令)
- **多语言**: 中英双语

### 技术栈

| 层 | 技术 |
|----|------|
| 前端 | React 19, TypeScript, Vite, TailwindCSS, ECharts, Zustand |
| 网关 | Go, Gin, SQLite, JWT, WebSocket |
| 引擎 | 纯 Go 撮合（主链路）；Rust/Serde/cdylib（已弃用，仅基准） |
