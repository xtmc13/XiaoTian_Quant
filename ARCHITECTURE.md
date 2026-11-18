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
│         │  SQLite  │   │ Go matching│   │   Redis   │             │
│         │  (WAL)   │   │ (current)  │   │  (cache)  │             │
│         └─────────┘   │  Engine    │   └──────────┘             │
│                       └────────────┘                             │
│             Target: migrate matching to Rust                     │
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
        └────────────┘  └───────────┘
```

## Layer Architecture

### 1. Frontend Layer (React 19)

Single-page application with 60 routes (68 page components under `web/src/pages/`) covering the entire trading workflow.

- **State**: Zustand for client state, TanStack React Query for server data
- **Charts**: ECharts + Lightweight Charts + KLineCharts Pro
- **Routing**: React Router 7 with lazy loading
- **PWA**: Service worker, manifest, offline support

### 2. Gateway Layer (Go 1.26)

The core business service — handles all API requests, business logic, and orchestration.

#### Module Organization

Each `internal/` package is a bounded context:

| Package | Responsibility |
|---------|---------------|
| `adapter/` | Exchange API abstraction + current Go matcher; Rust CGo path is migration-in-progress |
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
| `strategy/` | Strategy runtime engine + 17 built-in strategy types (55 registered factory names incl. aliases) |
| `watchdog/` | Health checks and system monitoring |
| `ws/` | WebSocket hub for real-time broadcasting |

### 3. Matching Engine (current: Go; target: Rust)

The current production matching engine is **pure Go** (`gateway/internal/adapter/matching.go`): price-time priority order book, limit/market orders, partial fills. The target architecture moves matching to Rust while Go retains the OMS, accounts, risk controls, and exchange adapters.

The Rust engine (`engine/`, cdylib) exists but is **not yet connected to the production path**:

- **OrderBook**: BTreeMap for price-level sorting, HashMap for O(1) ID lookup
- **Matching**: Price-time priority, limit and market orders
- **FFI**: C ABI exports for Go CGo integration (`cgo_bridge.go` requires `cgo` and the `xtengine` build tag)
- **Build**: excluded from default builds — `build.sh` only compiles it with `BUILD_RUST=1`
- **Migration gates**: Rust must pass parity tests for balance checks, fill write-back, cancellation, and restart recovery before becoming the production matcher

### 4. Data Layer

- **SQLite** (WAL mode): 66 tables covering users, orders, trades, positions, strategies, backtests, indicators, community, agents
- **Redis** (optional): Caching layer for real-time data
- **File storage**: Historical K-line and tick data

### 5. External Integrations

- **10 exchange adapters**: Binance, OKX, Bybit, Gate.io, MEXC, Kraken, Coinbase, Bitget, Alpaca, IBKR
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
  ├─> Contract hooks (v1.2: CustomStakeAmount / ConfirmTradeEntry / ConfirmTradeExit)
  ├─> Risk check
  ├─> Order placement
  ├─> Matching engine (pure Go)
  └─> Fill notification → Portfolio update
```

### Strategy Contract Hooks (v1.2, freqtrade IStrategy parity)

Optional, detected via type assertion on unwrapped strategy instances (existing
strategies unchanged). Live engine and backtest runner share the same semantics
("回测实盘同源"):

| Hook (Go / Python pystrat) | Trigger | Contract |
|---|---|---|
| `AdjustTradePosition` / `adjust_trade_position` | Every bar while a position is open | `>0` add (quote amount), `<0` reduce, `0` hold; adds pass the same risk checks; capped by `max_position_adjustments` (adds only) |
| `CustomStakeAmount` / `custom_stake_amount` | Before entry when qty unspecified | `>0` overrides stake (USDT), `0` keeps default |
| `ConfirmTradeEntry` / `confirm_entry` | Last step before entry order | `false` vetoes the entry |
| `ConfirmTradeExit` / `confirm_exit` | Last step before exit order | `false` vetoes the exit (position kept) |
| `CheckEntryTimeout` / `check_entry_timeout` | Unfilled entry limit order past timeout | `true` cancels (default logic), `false` keeps the order; Go side also has `CheckExitTimeout` |

Python contract (pystrat v1.2) details: `docs/PYTHON_STRATEGY_API.md` §1.6.
Backtest stats expose `total_adjustments` / `avg_adjustments_per_trade` for DCA
intensity. Order-timeout hooks attach to `order.TimeoutTracker`
(`Engine.TimeoutDecider()`); the backtest engine fills immediately, so timeout
hooks are live-only (freqtrade divergence, documented).

### Backtest Flow

```
User request → BacktestRunner
  ├─> Load historical data (SQLite or CSV)
  ├─> Replay events (bar-level or tick-level)
  ├─> Execute strategy logic
  ├─> Match orders (pure-Go engine / simulated)
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
                                         ↘ Pure-Go matching engine (in-process)
                                         ↘ Python Sandbox (ML)
```

## Technology Stack Summary

| Layer | Technology | Purpose |
|-------|-----------|---------|
| Frontend | React 19, TypeScript 5.7, Vite 6, TailwindCSS | User interface |
| Backend | Go 1.26, Gin, SQLite, Redis | Core business logic |
| Engine | Go matching currently; Rust 2021 is the target matcher, not yet in production | Order matching |
| ML | Python 3.12, LightGBM, XGBoost, Ray RLlib | Machine learning |
| Infra | Docker, Nginx, GitHub Actions | Deployment & CI/CD |
