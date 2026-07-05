# XiaoTianQuant 数据契约与分层边界

> 本文档定义 Rust(撮合) / Go(网关/账户) / TypeScript(前端) 三层之间的数据结构边界。
> 目标：每层只持有自己应该持有的数据形态，禁止跨层类型泄漏。

---

## 1. 分层职责

| 层级 | 职责 | 允许持有的数据 |
|---|---|---|
| **Rust Engine** | 高性能撮合、订单簿、仓位执行、信号处理 | 纯市场/订单/仓位/信号结构，不含用户、账户、UI 元数据 |
| **Go Gateway** | 账户聚合、风控、策略编排、API 网关、持久化 | 业务 DTO、账户模型、风控上下文、与 Engine 的 FFI 边界类型 |
| **TS Frontend** | 用户界面、图表、配置面板、监控 | UI 模型、API 响应类型、组件 Props，不含原始密钥和内部执行细节 |

---

## 2. Rust Engine 数据边界

### 2.1 核心模块

```
engine/src/
  orderbook.rs    # Order, PriceLevel, OrderBook, Trade, Side, OrderType, OrderStatus
  matching.rs     # MatchingEngine (价格-时间优先撮合)
  executor/
    signal.rs     # Signal — 策略信号输入
    position.rs   # Position — 引擎内部仓位
    execution.rs  # ExecutionEngine, EntryResult
    tpsl.rs       # TP/SL 配置与触发
  ffi.rs          # C 接口，供 Go 调用
```

### 2.2 数据形态规则

- `Order` 必须包含：id, side, price, quantity, order_type, status, timestamp。
- `Position` 使用枚举（`Side`, `PositionStatus`），禁止用 `String` 表示枚举字段。
- `Signal` 只描述"开/平/调仓"意图，不包含 UI 展示字段（如颜色、标签、AI reason 等）。
- **禁止**：Rust 层出现用户 ID、API 密钥、账户余额、前端路由状态。

### 2.3 FFI 边界

- FFI 只传递标量、数组和简单结构体。
- Go 层负责把 Rust 返回的原始数据转换为业务 DTO。
- Rust 不直接序列化为 JSON；Go 用 `C.GoString` 接收后再解析。

---

## 3. Go Gateway 数据边界

### 3.1 核心模块

```
gateway/internal/
  model/          # 跨模块共享的域模型
    market.go     # Tick, Bar, TradeData, OrderBookData
    orders.go     # OrderData, OrderSide, OrderStatus, OrderType
    portfolio.go  # PositionData, Balance, AccountData
    strategy.go   # Signal, StrategyConfig
    risk.go       # RiskAlert, RiskLevel
  store/          # 按域拆分的 repository
  adapter/        # 交易所适配器与凭证读取
  handler/        # HTTP / WebSocket handler
```

### 3.2 数据形态规则

- `model` 层定义核心业务实体；`store` 层的 Record/DTO 从 model 派生。
- 多个 `Position` 定义必须显式命名：`PositionData`（实盘）、`StrategyPosition`（策略回调）、`BacktestPosition`（回测）。
- 凭证只从环境变量读取；`config.yaml` 只保存非敏感配置。
- **禁止**：Handler 直接把 `map[string]any` 当作业务模型传递。

### 3.3 与 Rust 的边界

- Go 通过 FFI 调用 Rust 时，先把自己的 `model.Order` / `model.Signal` 转换为 Rust FFI 结构。
- Rust 返回的成交/仓位数据，由 Go 转换为 `model.TradeData` / `model.PositionData` 后再进入业务逻辑。

### 3.4 与前端的边界

- HTTP/WebSocket API 返回的数据结构是 **UI-ready DTO**，不是内部 model。
- 例如：Rust `Position` → Go `model.PositionData` → HTTP `PortfolioPositionResponse`。
- 敏感字段（api_key, secret, jwt_secret）绝不进入任何响应体。

---

## 4. TypeScript Frontend 数据边界

### 4.1 目录结构

```
web/src/
  types/
    api.ts        # ApiResponse<T>, PaginationMeta
    auth.ts       # User, AuthUser, UserProfile
    market.ts     # Ticker, OrderBook, KlineBar, Trade, WSEvent
    orders.ts     # Order, OCOOrder, BracketOrder, IcebergOrder
    portfolio.ts  # Position, PortfolioPosition, Balance, PortfolioSummary
    strategies.ts # StrategyConfig, StrategyItem, BacktestResult, ...
    bots.ts       # BotConfig, AIBotInstance, ...
    arbitrage.ts  # ArbitrageConfig, ArbitrageOpportunity, ...
    ai.ts         # AIModel, AIJob, AgentToken, ...
    trading.ts    # ExecutorStatus, SignalSource, ContractStatus, ...
    ui.ts         # Toast, Theme, UISettings
  api/
    client.ts     # axios 实例与拦截器
    auth.ts
    market.ts
    orders.ts
    portfolio.ts
    strategies.ts
    bots.ts
    arbitrage.ts
    ai.ts
  hooks/
    data/         # 只负责数据获取与缓存
    domain/       # 业务计算与派生状态
    ui/           # UI 常量、图标、颜色映射
```

### 4.2 数据形态规则

- `types/` 只描述数据契约，不包含组件 Props。
- `api/` 只负责 HTTP/WebSocket 通信，不包含业务计算。
- Hooks 分层：数据 Hook → 业务 Hook → UI Hook。
- **禁止**：把 Go 内部字段名直接暴露到 UI 类型中（必要时做 camelCase 映射）。
- **禁止**：前端类型中出现 `api_key`, `secret`, `passphrase`, `jwt_secret`。

### 4.3 与 Gateway 的边界

- 前端类型与 Gateway HTTP DTO 一一对应，但不等同。
- Gateway 响应中的 snake_case 字段在前端类型中保持 snake_case（与现有代码一致），或在 API 层做统一转换。
- WebSocket 事件类型（`WSEvent`）只包含前端需要展示的字段。

---

## 5. 配置 / 运行时 / 用户数据分离

```
xiaotian-quant/
  config/
    config.example.yaml   # 非敏感配置模板（可提交）
    config.yaml           # 本地运行时配置（.gitignore）
  secrets/
    .env.example          # 环境变量模板（可提交）
    .env                  # 凭据（.gitignore）
  runtime/
    gateway.db
    logs/
    strategy_configs.json
    strategy_logs.json
    strategy_templates.json
    agent_tokens.json
  user_data/
    <user_id>/
      configs/
      logs/
      backtests/
      exports/
```

- `config/`：非敏感应用配置，可版本控制示例文件。
- `secrets/`：凭据和密钥，绝不提交。
- `runtime/`：运行时生成的数据（DB、日志、JSON store）。
- `user_data/`：按用户隔离的持久数据。

---

## 6. 禁止清单

| 禁止行为 | 原因 |
|---|---|
| Rust 层出现 API 密钥或用户配置 | 引擎只负责撮合 |
| Go Handler 直接返回 `model.OrderData` | 内部模型可能包含敏感字段 |
| 前端保存或展示 `api_key`/`secret` | 安全合规 |
| `config.yaml` 中出现凭据 | 防止明文泄露 |
| 用 `map[string]any` 跨层传递业务数据 | 失去类型安全 |
| 前端 Hook 混合数据获取、业务计算、UI 常量 | 职责不清 |

---

## 7. 迁移状态

- [x] 删除明文凭据文件
- [x] 凭证只从 `.env` 读取
- [x] JWT secret 强制来自环境变量
- [x] Redis 默认密码移除
- [ ] 前端类型按域拆分
- [ ] Gateway model 按域拆分
- [ ] Store repository 按域拆分
- [ ] 目录结构迁移为 config/runtime/user_data
- [ ] Rust `Position.side/status` 改为枚举
- [ ] Rust `Signal` 拆分为核心与扩展字段
