# XiaoTianQuant v3.0 文档

> AI 驱动的多资产量化交易平台 — Go 网关 + React 前端 + Rust 撮合引擎

## 快速导航

| 文档 | 说明 |
|------|------|
| [快速开始](quickstart.md) | 安装、配置、Docker 部署 |
| [架构概览](architecture.md) | 系统架构、数据流、组件关系 |
| [API 参考](api/rest-api.md) | 完整 REST API 文档 |
| [策略开发](strategy-guide.md) | 策略编写、回测、参数优化 |
| [部署指南](deployment.md) | 生产环境部署 |

## 项目简介

XiaoTianQuant 是一个全栈量化交易平台，采用三层架构：

```
Web 前端 (React 19 + TypeScript)
       │ HTTP REST + WebSocket
Go 网关 (Gin)
       │ FFI (CGo)
Rust 撮合引擎 (价格-时间优先订单簿)
```

### 核心特性

- **多交易所**: Binance, OKX, Coinbase, Gate.io, MEXC, Bybit, Kraken
- **AI 策略生成**: 多 LLM 投票 + 7 Agent 协作管道
- **回测引擎**: 事件驱动, 真实数据, 缓存, 分解分析
- **超参优化**: Grid Search + 9 种损失函数
- **风控系统**: 12 维度检查 + 5 种保护机制 + 熔断器
- **策略社区**: 发布/评论/评分/排行榜
- **通知**: Email, 飞书, 钉钉, Telegram Bot (14 命令)
- **多语言**: 中英双语

### 技术栈

| 层 | 技术 |
|----|------|
| 前端 | React 19, TypeScript, Vite, TailwindCSS, ECharts, Zustand |
| 网关 | Go, Gin, SQLite, JWT, WebSocket |
| 引擎 | Rust, Serde, cdylib |
