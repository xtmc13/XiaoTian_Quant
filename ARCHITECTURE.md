# Architecture

XiaoTianQuant is an AI-powered quantitative trading platform with an event-driven architecture supporting unified backtesting and live trading.

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         React 19 Frontend                       │
│  ┌─────────┐ ┌──────────┐ ┌────────┐ ┌────────┐ ┌───────────┐  │
│  │Trading │ │ Backtest │ │ AI     │ │ Strategy│ │ Portfolio │  │
│  │  Panel │ │  Engine  │ │Panel  │ │ Manager │ │  Monitor  │  │
│  └─────────┘ └──────────┘ └────────┘ └────────┘ └───────────┘  │
│         │              │             │            │              │
│         └──────────────┴─────────────┴────────────┘              │
│                              │ HTTP / WebSocket                   │
└──────────────────────────────┼───────────────────────────────────┘
                               │
┌──────────────────────────────┼───────────────────────────────────┐
│                    Go Gateway (Core)                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────────┐  │
│  │  Auth    │ │  Orders  │ │ Strategies│ │  Risk Management   │  │
│  │  (JWT)   │ │  (OMS)   │ │  Engine  │ │  (15 checks)       │  │
│  └──────────┘ └──────────┘ └──────────┘ └────────────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────────┐  │
│  │ Backtest │ │  Hyper-  │ │    ML    │ │  Notifications     │  │
│  │  Engine  │ │   opt    │ │  Pipeline │ │  (Email/Telegram)  │  │
│  └──────────┘ └──────────┘ └──────────┘ └────────────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────────┐  │
│  │  Exchange│ │  Data    │ │ Community│ │   Agent Gateway    │  │
│  │Adapters  │ │  Manager │ │ /Market  │ │    (MCP)           │  │
│  └──────────┘ └──────────┘ └──────────┘ └────────────────────┘  │
│                              │                                    │
│              ┌───────────────┼───────────────┐                   │
│              │               │               │                   │
│         ┌────▼────┐   ┌─────▼──────┐   ┌────▼─────┐             │
│         │  SQLite  │   │   Rust     │   │   Redis   │             │
│         │  (WAL)   │   │  Engine    │   │  (cache)  │             │
│         └─────────┘   │  (CGo)     │   └──────────┘             │
│                       └────────────┘                             │
└──────────────────────────────┼───────────────────────────────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
        ┌─────▼─────┐  ┌─────▼─────┐   ┌──────▼──────┐
        │  Binance   │  │   OKX     │   │  Python     │
        │  Bybit     │  │  Gate.io  │   │  Sandbox    │
        │  Coinbase  │  │  MEXC     │   │  (ML/Ind.)  │
        │  Kraken    │  │  Alpaca   │   └─────────────┘
        │  Bitget    │  │  IBKR     │
        │  MT5       │  │  Tushare  │
        └────────────┘  └───────────┘
```

## Layer Architecture

### 1. Frontend Layer (React 19)

Single-page application with 26+ pages covering the entire trading workflow.

- **State**: Zustand for client state, TanStack React Query for server data
- **Charts**: ECharts + Lightweight Charts + KLineCharts Pro
- **Routing**: React Router 7 with lazy loading
- **PWA**: Service worker, manifest, offline support

### 2. Gateway Layer (Go 1.25)

The core business service — handles all API requests, business logic, and orchestration.

#### Module Organization

Each `internal/` package is a bounded context:

| Package | Responsibility |
|---------|---------------|
| `adapter/` | Exchange API abstraction + Rust CGo bridge |
| `ai/` | Multi-model AI strategy generation and voting |
| `arbitrage/` | Cross-exchange arbitrage detection and execution |
| `backtest/` | Event-driven backtesting engine |
| `cache/` | Redis caching layer |
| `community/` | Indicator marketplace and strategy forum |
| `config/` | Configuration (YAML + env var expansion) |
| `data/` | Historical data download and storage |
| `exchange/` | Exchange registry and WebSocket session management |
| `experiment/` | A/B testing, sensitivity analysis, walk-forward |
| `factor/` | Technical indicator calculations (multi-factor) |
| `handler/` | HTTP request handlers (REST API) |
| `hyperopt/` | CMA-ES parameter optimization |
| `indicator/` | Indicator IDE (parsing, validation, sandbox) |
| `middleware/` | Cross-cutting concerns (auth, CORS, rate limit) |
| `ml/` | Machine learning pipeline (LightGBM/XGBoost) |
| `notify/` | Multi-channel notification routing |
| `onchain/` | On-chain analytics data |
| `order/` | Order Management System (OMS) with advanced orders |
| `paper/` | Paper trading simulation |
| `pairlist/` | Trading pair filtering and whitelisting |
| `portfolio/` | Portfolio management and position sizing |
| `protection/` | Circuit breaker and loss protection |
| `risk/` | 15-dimension risk management engine |
| `service/` | Business service layer |
| `social/` | Social trading and signal copying |
| `store/` | SQLite persistence layer |
| `strategy/` | Strategy runtime engine + 13 built-in strategies |
| `watchdog/` | Health checks and system monitoring |
| `ws/` | WebSocket hub for real-time broadcasting |

### 3. Matching Engine Layer (Rust)

High-performance price-time priority matching engine compiled as a cdylib.

- **OrderBook**: BTreeMap for price-level sorting, HashMap for O(1) ID lookup
- **Matching**: Price-time priority, limit and market orders
- **FFI**: C ABI exports for Go CGo integration
- **Performance**: > 10,000 TPS per symbol

### 4. Data Layer

- **SQLite** (WAL mode): 23 tables covering users, orders, trades, positions, strategies, backtests, indicators, community, agents
- **Redis** (optional): Caching layer for real-time data
- **File storage**: Historical K-line and tick data

### 5. External Integrations

- **12 exchange adapters**: Binance, OKX, Bybit, Gate.io, MEXC, Kraken, Coinbase, Bitget, Alpaca, IBKR, MT5, Tushare
- **3 AI providers**: OpenAI, DeepSeek, Anthropic
- **4 notification channels**: Email, Feishu, DingTalk, Telegram
- **Python sandbox**: ML inference, CCXT bridge, custom indicators

## Data Flow

### Order Placement

```
Frontend → POST /api/orders → Gateway
  ├─> Validation (handler)
  ├─> Risk checks (risk.Manager)
  ├─> Balance lock (order.OrderManager)
  ├─> Record to SQLite (store)
  ├─> Submit to exchange (adapter) or paper trading
  └─> Notify (notify.Router)
```

### Strategy Execution

```
Clock → Strategy.OnTick/OnBar → Signal emitted
  ├─> Risk check
  ├─> Order placement
  ├─> Matching engine (Rust)
  └─> Fill notification → Portfolio update
```

### Backtest Flow

```
User request → BacktestRunner
  ├─> Load historical data (SQLite or CSV)
  ├─> Replay events (bar-level or tick-level)
  ├─> Execute strategy logic
  ├─> Match orders (Rust engine or simulated)
  ├─> Calculate metrics (Sharpe, Sortino, MaxDD, etc.)
  └─> Return results + equity curve
```

## Deployment

### Development (Docker Compose)

```
gateway:8080  ← sandbox:9000  ← ml_server:8001  ← ccxt_bridge:8002  ← redis:6379
```

### Production

```
[Internet] → Nginx (SSL termination) → Gateway (Go) → [SQLite + Redis]
                                         ↘ Rust Engine (CGo)
                                         ↘ Python Sandbox (ML)
```

## Technology Stack Summary

| Layer | Technology | Purpose |
|-------|-----------|---------|
| Frontend | React 19, TypeScript 5.7, Vite 6, TailwindCSS | User interface |
| Backend | Go 1.25, Gin, SQLite, Redis | Core business logic |
| Engine | Rust 2021, serde, FFI | High-performance matching |
| ML | Python 3.12, LightGBM, XGBoost, Ray RLlib | Machine learning |
| Infra | Docker, Nginx, GitHub Actions | Deployment & CI/CD |
