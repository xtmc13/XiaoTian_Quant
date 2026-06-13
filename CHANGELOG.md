# CHANGELOG

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
