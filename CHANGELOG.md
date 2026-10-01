# CHANGELOG

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [3.1.0] - 2026-09-29

对标 QuantDinger 补齐批次：47 项全部落地（19 补齐 + 16 回归保护 + 11 改进，
权威记录见《对标QuantDinger补齐清单.md》），feature/gap-remediation-2026-09-20 分支合入。

### Added
- 交易机器人类型：分层马丁格尔、DCA 定投、网格中性（swap 双腿对冲）、马丁层级硬上限、CTA/组合策略模板（8+4）
- 账户安全：MFA/TOTP 两步验证、Turnstile 人机验证、JWT token 版本吊销、初始密码强制修改
- 商业化：双轨计费 SKU（订阅/利润分成）、USDT 多链支付（BEP20/ERC20/SOL）、Stripe 自动支付、Billing 链上自动到账核验
- 通知：Twilio 短信渠道、Alertmanager 告警链路（12 条规则带 runbook）
- 研究：TA-Lib 因子研究框架、组合回测、原生多时间框架、定时调度、动态 universe
- 实盘工程化：持仓对账、成交恢复、资金费对账、实盘偏差监控
- 可观测性：Prometheus 指标导出 + Grafana 看板
- 执行：IBKR（盈透）适配器、limit-then-market 执行算法
- 工程：OpenAPI 契约治理（526 路由契约 + CI 漂移检查）、arm64 全链路/Electron 发布物料
- 测试：Playwright E2E 扩至 80+ 用例（含全站交互点测与三语 i18n 覆盖）

### Fixed
- 9 家交易所下单精度硬化（OKX 张数换算/instruments 缓存/big.Rat 取整），修复币安 -1111 精度拒单
- 默认 `go build ./...`（CGO 开启）编译失败：Rust FFI 撮合路径改为显式 opt-in `-tags xtengine`，纯 Go 引擎在无 xtengine 时无条件生效；双模式构建+测试全绿
- 凭证保险库健壮性（损坏 key 不再静默重建孤儿化凭证、env 轮换双 key 窗口）
- 事件总线重入死锁（dispatch 锁内快照订阅、锁外调 handler）

## [Unreleased]

### Added
- AI multi-agent analysis framework
- Agent gateway with MCP protocol support
- Token management for AI agents
- CC Switch configuration for automated trading
- Indicator IDE with sandbox execution
- Strategy marketplace with author revenue sharing
- Walk-Forward experiment pipeline
- Structured parameter tuning
- On-chain data analytics integration
- Social trading engine with signal copying
- Hyperopt CMA-ES parameter optimization
- TensorBoard integration for RL training
- Telegram and Discord bot integration
- Arbitrage monitoring and execution
- Pairlist management with filtering
- Protection system (DailyLoss, ConsecutiveLosses, MaxDrawdown, Cooldown)
- Advanced order types: OCO, Bracket, Iceberg, DCA
- TradingView webhook support
- Generic webhook support
- Multi-language support (en-US, zh-CN)
- PWA support for web app

### Changed
- Major refactoring: Go gateway + Rust matching engine + React 19 frontend
- Upgraded to Go 1.25 with generics support
- Upgraded to React 19 with TypeScript 5.7
- Upgraded to Vite 6

### Fixed
- Config `${VAR}` environment variable expansion in YAML
- Docker CGO_ENABLED mismatch preventing Rust engine linkage
- SSL certificate and secret key exposure in git history

## [3.0.0] - 2025-01-XX

### Added
- Complete microservices architecture: Go gateway, Rust engine, React frontend, Python sandbox
- 13 built-in trading strategies
- 9 exchange adapters (Binance, OKX, Bybit, Gate.io, MEXC, Kraken, Coinbase, Bitget, Alpaca)
- Event-driven backtesting engine (bar-level and tick-level)
- Rust matching engine with 10k+ TPS
- 23 SQLite tables covering full trading lifecycle
- 200+ REST API endpoints
- JWT authentication with OAuth (Google, GitHub)
- 15-dimension risk management system with circuit breaker
- ML pipeline with LightGBM/XGBoost prediction
- Reinforcement learning training with Ray RLlib
- Indicator community and strategy marketplace
- Social trading with signal copying
- Multi-channel notifications (Email, Feishu, DingTalk, Telegram)

### Changed
- Complete rewrite from monolith to microservices architecture
- Frontend migrated to React 19 + TypeScript + TailwindCSS

### Removed
- Legacy Python-based trading engine

## [2.x.x] - Previous generation

- Python-based monolithic trading system
- Basic strategy execution
- Simple backtesting

## [1.x.x] - Initial release

- Basic strategy framework
- Manual trading interface
