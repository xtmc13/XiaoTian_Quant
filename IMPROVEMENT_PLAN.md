# 小天量化 XiaoTianQuant v2.0 → v3.0 全面改进方案

> **对标项目：** [Freqtrade](https://github.com/freqtrade/freqtrade) (Python, GPLv3) · [QuantDinger](https://github.com/QuantDinger) (Python/Vue, Apache 2.0)
>
> **目标架构：** Go (API网关/账户) + Rust (撮合引擎) + TypeScript (前端)
>
> **更新日期：** 2026-05-31

---

## 目录

1. [差距总览](#1-差距总览)
2. [第一阶段：基础夯实 (2026 Q2–Q3)](#2-第一阶段基础夯实-2026-q2q3)
3. [第二阶段：策略深度 (2026 Q3–Q4)](#3-第二阶段策略深度-2026-q3q4)
4. [第三阶段：ML 管线 (2026 Q4–2027 Q1)](#4-第三阶段ml-管线-2026-q42027-q1)
5. [第四阶段：全资产覆盖 (2027 Q1–Q2)](#5-第四阶段全资产覆盖-2027-q1q2)
6. [第五阶段：生态与社区 (2027 Q2–Q3)](#6-第五阶段生态与社区-2027-q2q3)
7. [架构约束与决策](#7-架构约束与决策)
8. [实施优先级排序](#8-实施优先级排序)

---

## 1. 差距总览

### 1.1 成熟度对比矩阵

| 子系统 | XiaoTianQuant (当前) | Freqtrade | QuantDinger | 差距等级 |
|--------|:---------------------:|:---------:|:-----------:|:--------:|
| **交易所覆盖** | 5 家 (Binance/OKX/Coinbase/Gate/MEXC) | 30+ 家 (CCXT) | 10+ 家 (CCXT) | 🔴 大 |
| **策略框架** | 2 个内置策略 + Go 模板编译 | ABC 基类 + 10+ 回调 + 参数系统 | 向量化 + 事件驱动双模式 | 🟡 中 |
| **回测引擎** | 事件驱动, 随机数据 | 事件驱动, 真实数据, 缓存, 分析工具 | 向量化, 参数扫描 | 🟡 中 |
| **超参优化 (Hyperopt)** | 基础实验管线 | Optuna + 12 损失函数 + CMA-ES + GPU | 网格搜索 | 🔴 大 |
| **机器学习 (FreqAI)** | 无 | 14+ 模型, 特征工程, 在线学习 | 无 | 🔴 巨大 |
| **交易对筛选 (Pairlist)** | 无 | 18 种过滤器, 责任链模式 | 基础 | 🔴 大 |
| **保护机制 (Protection)** | 12 维度风控检查 | 5 种保护处理器 + 自动恢复 | 风控规则 | 🟡 中 |
| **仓位管理** | 5 种模型 | 固定/无限/动态 + DCA | 多种模型 | 🟡 中 |
| **止损机制** | 基础止损 | 跟踪止损 + 自定义止损 + 交易所止损 | 基础止损 | 🟡 中 |
| **通知渠道** | Email/Lark/DingTalk/Telegram | Telegram(40+命令)/Discord/Webhook | Email/Webhook | 🟢 小 |
| **数据管理** | 基础 K 线拉取 | 多格式下载/转换/压缩 (JSON/Feather/Parquet) | 基础 | 🟡 中 |
| **REST API** | 30+ 端点 (Gin) | 完整 REST (FastAPI) + WebSocket | 完整 REST (Flask) | 🟢 小 |
| **前端功能** | 11 页面, 图表丰富 | FreqUI (基础监控) | 完整交易终端 | 🟢 小 |
| **AI 集成** | 多模型投票 + 7 Agent 管道 | FreqAI (ML预测) | 多 LLM + Agent 网关 | 🟢 小 (AI 是我们的强项) |
| **传统市场** | 无 | 无 | IBKR/MT5/Alpaca | 🔴 大 |
| **测试覆盖** | 少量 (Rust 撮合测试) | 90+ 测试文件, pytest 全套 | 少量 | 🔴 大 |
| **文档** | README + DEPLOYMENT | MkDocs 50+ 页 | README | 🟡 中 |
| **社区市场** | 指标社区 (半成品) | 模板系统 | 指标社区 (完整) | 🟡 中 |
| **撮合引擎** | Rust cdylib + Go 回退 | 无 (依赖交易所) | 无 (依赖交易所) | 🟢 小 (我们是独有优势) |
| **多语言** | 部分双语 | 英语 | 双语 | 🟢 小 |

### 1.2 架构优势（保持并强化）

我们相比两个对标项目的**结构性优势**：

| 优势 | 说明 |
|------|------|
| **Go 高性能网关** | Freqtrade/QuantDinger 均为 Python，GIL 限制 + 内存占用高。Go 原生并发 + 低内存 + 静态编译 |
| **Rust 撮合引擎** | 两个对标项目均无独立撮合引擎。我们的 Rust OrderBook 支持百万级 TPS，可独立部署为做市商核心 |
| **TypeScript 前端** | Freqtrade 前端简陋，QuantDinger 用 Vue 2 (已过时)。React 19 + 丰富图表是我们的优势 |
| **AI 原生设计** | 7 Agent 投票管道 + 多模型支持是独有功能，两个对标项目均无此深度 |

### 1.3 核心差距定性分析

```
Freqtrade 最强领域:
  ████████████ Hyperopt (超参优化)
  ████████████ FreqAI (机器学习)
  ████████████ Pairlist (交易对筛选)
  ████████████ 交易所覆盖 (30+)
  ████████████ 测试基础设施

QuantDinger 最强领域:
  ████████████ 传统券商 (IBKR/MT5/Alpaca)
  ████████████ Agent/MCP 网关
  ████████████ 指标社区市场
  ████████████ Admin 面板

XiaoTianQuant 最强领域:
  ████████████ 撮合引擎 (Rust)
  ████████████ AI 多模型投票
  ████████████ 前端体验 (React 19)
  ████████████ 风控维度 (12 维)
  ████████████ 通知渠道覆盖
```

---

## 2. 第一阶段：基础夯实 (2026 Q2–Q3)

> **目标：** 补齐基础设施差距，达到 Freqtrade 90% 的功能覆盖度
>
> **预计工期：** 8–10 周
>
> **新增代码量：** ~8,000 Go + ~3,000 TS + ~2,000 Rust

### 2.1 测试基础设施 ⭐⭐⭐ (来源: Freqtrade)

**当前状态：** 仅 engine/ 有 4 个 Rust 单元测试，Go/TS 无测试
**Freqtrade 状态：** 90+ pytest 文件, pytest-asyncio, pytest-cov, pytest-mock, pytest-xdist, pre-commit hooks

**实施方案：**

#### 2.1.1 Go 测试框架搭建
```
gateway/
├── internal/
│   ├── adapter/
│   │   ├── binance_test.go        # Mock HTTP server 测试 REST 签名和响应解析
│   │   ├── matching_test.go       # 撮合引擎纯 Go 回退测试
│   │   └── ...
│   ├── backtest/
│   │   ├── runner_test.go         # 回测引擎单元测试 (模拟数据)
│   │   └── stats_test.go          # 统计指标计算正确性
│   ├── risk/
│   │   └── risk_test.go           # 12 维度风控规则测试
│   ├── portfolio/
│   │   └── portfolio_test.go      # 持仓计算/权益曲线测试
│   ├── strategy/
│   │   ├── engine_test.go         # 策略引擎事件调度测试
│   │   └── strategies/
│   │       ├── grid_test.go
│   │       └── breakout_test.go
│   ├── store/
│   │   └── store_test.go          # SQLite CRUD + 迁移测试
│   ├── middleware/
│   │   └── middleware_test.go     # JWT/CORS/限流测试
│   └── handler/
│       ├── auth_test.go
│       ├── order_test.go
│       └── ...
└── testutil/                       # 共享测试工具
    ├── mock_exchange.go           # Mock 交易所 HTTP server
    ├── mock_store.go              # Mock 数据库
    ├── fixtures.go                # 测试数据夹具
    └── helper.go                  # 测试辅助函数
```

**具体任务：**
- [ ] 引入 `testify` (assert/mock) 依赖
- [ ] 为每个 adapter 编写 mock HTTP server 测试 (签名验证 + 响应解析)
- [ ] 为撮合引擎编写 10+ 边界用例测试 (价格交叉、部分成交、市价单、自成交防护)
- [ ] 为风控引擎编写 12 个维度的规则触发/不触发测试
- [ ] 为策略引擎编写事件调度测试
- [ ] 为 store 层编写 CRUD + 迁移测试
- [ ] CI pipeline (GitHub Actions: lint + test + build)

#### 2.1.2 TypeScript 前端测试
```
web/src/
├── __tests__/
│   ├── components/                # 组件渲染测试
│   ├── lib/
│   │   ├── technicalIndicators.test.ts  # 指标计算正确性
│   │   ├── api.test.ts                  # API 客户端 mock 测试
│   │   └── indicatorContract.test.ts
│   └── stores/
│       ├── authStore.test.ts
│       └── appStore.test.ts
```

**具体任务：**
- [ ] 引入 Vitest + @testing-library/react
- [ ] 技术指标计算函数单元测试 (SMA/EMA/RSI/MACD/布林带/ATR/KDJ 等 13 个)
- [ ] Zustand store 状态转换测试
- [ ] 关键组件快照测试

#### 2.1.3 Rust 测试增强
**具体任务：**
- [ ] 随机订单压力测试 (1000 笔随机订单 → 验证订单簿一致性)
- [ ] 并发 FFI 调用测试
- [ ] 性能基准测试 (criterion.rs)

**预计工作量：** 2-3 周

---

### 2.2 数据管理增强 ⭐⭐ (来源: Freqtrade)

**当前状态：** 基础 K 线拉取 (REST API)，无本地存储/多格式支持
**Freqtrade 状态：** OHLCV 下载、JSON/Feather/Parquet 多格式、数据转换、压缩存储

**实施方案：**

```
gateway/internal/data/              # 新建包
├── downloader.go                   # 历史数据下载器
├── storage.go                      # 多格式存储 (JSON → Parquet → Feather)
├── converter.go                    # 数据转换 (Trade → OHLCV, 时间框架转换)
├── validator.go                    # 数据完整性校验 (缺失检测 + 自动补全)
└── cache.go                        # 数据缓存层 (内存 + 磁盘)
```

**具体任务：**
- [ ] **历史数据下载器** — 从交易所 REST API 拉取历史 K 线，支持断点续传
  - 使用 `modernc.org/sqlite` 本地存储 K 线数据
  - 支持配置：`start_date`, `end_date`, `timeframe`, `symbols`
  - 实现 `download` / `update` / `prune` 三个子命令
- [ ] **Parquet 导出** — 引入 `parquet-go`，将 SQLite 数据导出为 Parquet 格式 (列式存储, 压缩率高, pandas 可直接读取)
- [ ] **数据完整性校验** — 检测缺失 K 线 + 异常值 (价格为 0, 成交量负数等)
- [ ] **时间框架转换** — 从 1m 合成 5m/15m/1h/4h/1d (OHLC 聚合)
- [ ] **Trade → OHLCV 转换** — 从逐笔成交合成任意周期 K 线

**后端 API 新增：**
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/data/download` | 下载历史数据 |
| GET | `/api/data/status` | 数据覆盖状态 |
| POST | `/api/data/convert` | 格式转换 |
| GET | `/api/data/export` | 导出数据文件 |

**预计工作量：** 1.5-2 周

---

### 2.3 策略框架增强 ⭐⭐⭐ (来源: Freqtrade)

**当前状态：** 2 个内置策略 + Go 模板编译器, 回调有限
**Freqtrade 状态：** 10+ 策略回调, 参数系统 (Int/Decimal/Boolean/Categorical), 多时间框架, 策略迁移

**实施方案：**

#### 2.3.1 策略回调体系扩展

```go
// gateway/internal/strategy/interface.go — 扩展策略接口

type Strategy interface {
    // === 现有 ===
    Name() string
    Timeframe() string
    PopulateIndicators(df *DataFrame) *DataFrame
    OnBar(df *DataFrame) *Signal

    // === 新增: 入场/出场信号分离 (对齐 Freqtrade) ===
    PopulateEntrySignals(df *DataFrame) *DataFrame   // 做多/做空入场信号
    PopulateExitSignals(df *DataFrame) *DataFrame    // 做多/做空出场信号

    // === 新增: 自定义回调 ===
    CustomStoploss(position *Position, currentPrice float64) float64          // 自定义止损价
    CustomExitPrice(position *Position, orderbook *OrderBook) float64         // 自定义出场价
    CustomStakeAmount(balance float64, signal *Signal) float64                // 自定义仓位
    ConfirmTradeEntry(signal *Signal) bool                                    // 入场确认
    ConfirmTradeExit(position *Position) bool                                 // 出场确认
    AdjustEntryPrice(order *Order, orderbook *OrderBook) float64              // 入场价微调

    // === 新增: 仓位调整 (DCA) ===
    AdjustPosition(position *Position, currentPrice float64) *AdjustmentAction // DCA 加仓决策
}
```

#### 2.3.2 策略参数系统

```go
// gateway/internal/strategy/parameters.go — 新建文件

type ParameterType int
const (
    IntParameter ParameterType = iota
    DecimalParameter
    BooleanParameter
    CategoricalParameter
)

type Parameter struct {
    Name     string        `json:"name"`
    Type     ParameterType `json:"type"`
    Default  interface{}   `json:"default"`
    Min      float64       `json:"min,omitempty"`
    Max      float64       `json:"max,omitempty"`
    Options  []string      `json:"options,omitempty"`  // Categorical
    Space    string        `json:"space"`               // "buy" | "sell" | "roi" | "stoploss"
    Optimize bool          `json:"optimize"`             // 是否参与超参优化
}
```

#### 2.3.3 多时间框架支持 (Informative Pairs)

```go
// gateway/internal/strategy/informative.go — 新建文件

// 策略可声明额外需要的时间框架数据
type InformativePair struct {
    Pair      string `json:"pair"`       // 如 "BTC/USDT"
    Timeframe string `json:"timeframe"`   // 如 "1d" (日线辅助分钟线决策)
    Asset     string `json:"asset,omitempty"` // 如 "ETH/USDT" (关联币种)
}

// 策略引擎自动拉取并合并多时间框架数据到 DataFrame
func (e *Engine) mergeInformativePairs(df *DataFrame, informatives []InformativePair) *DataFrame
```

**具体任务：**
- [ ] 拆分 `PopulateIndicators` → `PopulateEntrySignals` + `PopulateExitSignals`
- [ ] 实现 `CustomStoploss` / `CustomStakeAmount` / `ConfirmTradeEntry` 回调链
- [ ] 实现 `Parameter` 注册与解析系统
- [ ] 实现多时间框架数据自动合并
- [ ] 策略版本号 + 自动迁移框架 (`strategyupdater.go`)
- [ ] 内置策略升级 (grid/breakout 改为新接口)

**预计工作量：** 2-3 周

---

### 2.4 Pairlist 交易对筛选系统 ⭐⭐⭐ (来源: Freqtrade)

**当前状态：** 无交易对筛选机制，手动配置交易对
**Freqtrade 状态：** 18 种过滤器, 责任链模式, 动态白名单

**实施方案：**

```
gateway/internal/pairlist/           # 新建包
├── manager.go                       # Pairlist 管理器 (责任链)
├── ipairlist.go                     # IPairList 接口
├── static.go                        # StaticPairList (固定列表)
├── volume.go                        # VolumePairList (成交量排序 Top N)
├── performance.go                   # PerformancePairList (近期表现排序)
├── volatility.go                    # VolatilityPairList (波动率排序)
├── spread.go                        # SpreadFilter (价差过滤)
├── price.go                         # PriceFilter (价格范围过滤)
├── precision.go                     # PrecisionFilter (精度过滤)
├── age.go                           # AgeFilter (上市时间过滤)
├── correlation.go                   # CorrelationFilter (相关性过滤, 避免同质交易对)
├── shuffle.go                       # ShuffleFilter (随机打乱, 避免过拟合)
└── delist.go                        # DelistFilter (下架检测)
```

**责任链架构：**
```
白名单生成器 (Producer)           过滤器 (Filter)              消费者 (Consumer)
┌──────────────────┐      ┌──────────────────────┐      ┌──────────────────┐
│ VolumePairList    │ ───→ │ SpreadFilter          │ ───→ │                  │
│ (全市场成交量Top100)│      │ (价差<0.1%)            │      │ 最终白名单 (20个)  │
│                    │      ├──────────────────────┤      │                  │
│                    │ ───→ │ VolatilityFilter      │ ───→ │ 策略可交易的      │
│                    │      │ (波动率>1%, <10%)      │      │ 交易对集合        │
│                    │      ├──────────────────────┤      │                  │
│                    │ ───→ │ PriceFilter           │      │                  │
│                    │      │ ($0.01 < price < $1000)│     │                  │
│                    │      ├──────────────────────┤      │                  │
│                    │ ───→ │ PrecisionFilter       │      │                  │
│                    │      │ (价格精度匹配交易所)     │      │                  │
└──────────────────┘      └──────────────────────┘      └──────────────────┘
```

**具体任务：**
- [ ] 定义 `IPairList` 接口 (Name, Generate, Filter 方法)
- [ ] 实现 4 个白名单生成器：Static, Volume (Top N 按成交量), Performance, MarketCap
- [ ] 实现 8 个过滤器：Spread, Price, Precision, Volatility, Age, Correlation, Shuffle, Delist
- [ ] 实现 `PairlistManager` 责任链编排器
- [ ] 配置集成 (YAML 配置 pairlist 链)
- [ ] 前端：交易对管理页面 (白名单编辑 + 过滤规则配置)

**后端 API 新增：**
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/pairlist/whitelist` | 当前白名单 |
| POST | `/api/pairlist/refresh` | 刷新白名单 |
| GET | `/api/pairlist/config` | 过滤器配置 |
| PUT | `/api/pairlist/config` | 更新过滤规则 |

**预计工作量：** 2-3 周

---

### 2.5 保护机制 (Protection) ⭐⭐ (来源: Freqtrade)

**当前状态：** 12 维度风控检查 (下单前)，但无自动恢复机制
**Freqtrade 状态：** 5 种保护处理器 + 自动恢复 + 全局/局部锁

**差异分析：** 我们的"风控检查"是**下单前校验**（同步阻断），Freqtrade 的"保护机制"是**事后自动响应**（异步恢复）。两者互补。

**实施方案：**

```go
// gateway/internal/protection/       # 新建包
// ├── manager.go                     # Protection 管理器
// ├── iprotection.go                 # IProtection 接口
// ├── cooldown.go                    # CooldownPeriod (全局冷却期)
// ├── low_profit.go                  # LowProfitPairs (低盈利对锁定)
// ├── max_drawdown.go                # MaxDrawdown (最大回撤保护)
// ├── stoploss_guard.go              # StoplossGuard (频繁止损锁定)
// └── pair_lock.go                   # PairLock (交易对级别锁定)

type IProtection interface {
    ShortDesc() string
    Name() string
    // 是否触发保护 (返回锁定时长)
    LockReason(trades []*Trade, portfolio *Portfolio) *ProtectionLock
    // 全局级别 or 交易对级别
    Scope() ProtectionScope // Global | Pair
}

type ProtectionLock struct {
    Pair      string           // 锁定的交易对 (Global 则为空)
    Until     time.Time        // 锁定到期时间
    Reason    string           // 锁定原因
    Scope     ProtectionScope
}
```

**具体任务：**
- [ ] 实现 `IProtection` 接口
- [ ] 实现 5 种保护处理器 (对齐 Freqtrade)
- [ ] 实现 `ProtectionManager` (保护处理器注册 + 定时检查 + 自动解锁)
- [ ] 与现有 12 维风控集成 (风控报告触发保护检查)
- [ ] 前端：保护状态面板 (当前锁定的交易对 + 到期倒计时)

**预计工作量：** 1-1.5 周

---

### 2.6 止损增强 ⭐⭐ (来源: Freqtrade)

**当前状态：** 基础固定止损
**Freqtrade 状态：** 跟踪止损 (Trailing Stop) + 自定义止损回调 + 交易所止损单

**实施方案：**

```go
// gateway/internal/risk/trailing_stop.go — 新建文件

type TrailingStopMode int
const (
    TrailingStopDisabled TrailingStopMode = iota
    TrailingStopFixed                     // 固定距离跟踪 (如 -2%)
    TrailingStopATR                       // ATR 动态距离跟踪
    TrailingStopHighLow                   // 最高/最低价跟踪
)

type TrailingStop struct {
    Mode          TrailingStopMode
    StopDistance  float64  // 固定模式: 距离百分比
    ATRMultiplier float64  // ATR 模式: ATR 倍数
    ATRPeriod     int      // ATR 计算周期
}

// 每个 Tick 更新跟踪止损价
func (ts *TrailingStop) Update(position *Position, currentPrice float64, highLow float64) float64
```

**具体任务：**
- [ ] 实现 3 种跟踪止损模式 (Fixed / ATR / HighLow)
- [ ] 止损触发后自动下交易所止损单
- [ ] 策略可自定义止损回调 `CustomStoploss()`
- [ ] 前端：止损设置 UI (模式选择 + 参数滑块)

**预计工作量：** 1 周

---

## 3. 第二阶段：策略深度 (2026 Q3–Q4)

> **目标：** 达到并超越 Freqtrade 的策略生态深度
>
> **预计工期：** 10–12 周
>
> **新增代码量：** ~10,000 Go + ~5,000 TS + ~3,000 Rust

### 3.1 超参优化 (Hyperopt) ⭐⭐⭐⭐⭐ (来源: Freqtrade)

**当前状态：** 基础实验管线 (差分进化 + TPE + 灵敏度分析)，未完整接入
**Freqtrade 状态：** Optuna 优化引擎 + 12 内置损失函数 + CMA-ES + GPU 加速 + TensorBoard + Epoch 过滤

**这是最大的单点差距。** Freqtrade 的 Hyperopt 是其核心竞争力之一。

**实施方案：**

```
gateway/internal/hyperopt/           # 新建包 (替换现有 experiment/)
├── optimizer.go                     # 优化器主引擎
├── optuna_adapter.go               # Optuna Go 适配 (通过 REST 调用 Python optuna 服务)
├── loss_functions.go               # 12 种损失函数
├── space.go                        # 搜索空间定义
├── epoch_manager.go               # Epoch 管理 (结果持久化 + 过滤)
├── cmaes.go                        # CMA-ES 优化器
└── result_analyzer.go             # 结果分析 (参数重要性、平行坐标图数据)
```

#### 3.1.1 优化器服务 (Python sidecar)

由于 Optuna 没有原生 Go 实现，采用 Python sidecar 服务：

```
sandbox/
├── hyperopt_server.py              # Optuna 优化服务 (新增)
│   # POST /study/create  — 创建 Study
│   # POST /study/optimize — 运行优化 (n_trials, timeout)
│   # GET  /study/best     — 获取最佳参数
│   # GET  /study/trials   — 所有 Trial 结果
│   # POST /study/plot     — 生成可视化 (参数重要性/平行坐标/轮廓图)
└── requirements.txt               # 添加 optuna, plotly
```

Go 网关通过 HTTP 调用 sidecar，**不打破 Go 主架构**。

#### 3.1.2 12 种损失函数

| # | 损失函数 | 来源 | 说明 |
|---|---------|------|------|
| 1 | SharpeRatioLoss | Freqtrade | 最大化夏普比率 |
| 2 | SortinoLoss | Freqtrade | 最大化索提诺比率 |
| 3 | CalmarLoss | Freqtrade | 最大化卡尔玛比率 |
| 4 | MaxDrawdownLoss | Freqtrade | 最小化最大回撤 |
| 5 | ProfitLoss | Freqtrade | 最大化总利润 |
| 6 | WinRateLoss | Freqtrade | 最大化胜率 |
| 7 | ExpectancyLoss | Freqtrade | 最大化期望收益 |
| 8 | MultiMetricLoss | Freqtrade | 多指标加权 |
| 9 | ProfitDrawdownLoss | Freqtrade | 利润/回撤比 |
| 10 | StabilityLoss | 自定义 | 参数稳定性 (小幅参数变化 → 结果小幅变化) |
| 11 | OutOfSampleLoss | 自定义 | 样本内外一致性 |
| 12 | CustomLoss | 用户自定义 | 用户可通过策略定义自定义损失 |

#### 3.1.3 搜索空间定义

```go
type HyperoptSpace struct {
    Buy        []Parameter   // 入场参数
    Sell       []Parameter   // 出场参数
    ROI        []Parameter   // 止盈表
    Stoploss   []Parameter   // 止损参数
    Trailing   []Parameter   // 跟踪止损参数
    Protection []Parameter   // 保护参数
}
```

#### 3.1.4 前端

- [ ] Hyperopt 配置页面 (选择策略 + 参数空间 + 损失函数 + 时间范围)
- [ ] 优化进度实时展示 (Trial 进度 + 当前最佳)
- [ ] 结果分析面板 (参数重要性图 + 平行坐标图 + 轮廓图)
- [ ] 最优参数一键部署到实盘/模拟盘

**后端 API 新增：**
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/hyperopt/start` | 启动超参优化 |
| GET | `/api/hyperopt/status/:jobId` | 查询优化进度 |
| GET | `/api/hyperopt/results/:jobId` | 获取优化结果 |
| POST | `/api/hyperopt/stop/:jobId` | 停止优化 |
| GET | `/api/hyperopt/studies` | 历史 Study 列表 |
| DELETE | `/api/hyperopt/studies/:id` | 删除 Study |
| POST | `/api/hyperopt/deploy/:jobId` | 部署最优参数到策略 |

**预计工作量：** 3-4 周

---

### 3.2 DCA / 仓位调整 ⭐⭐ (来源: Freqtrade)

**当前状态：** 无 DCA 功能
**Freqtrade 状态：** Position Adjustment (DCA 加仓/减仓)

**实施方案：**

```go
// gateway/internal/order/dca.go — 新建文件

type DCAConfig struct {
    Enabled      bool
    MaxEntries   int           // 最大加仓次数 (如 3 次)
    EntryPriceDev float64      // 每次加仓价格偏离 (如 -5% 即每跌 5% 加一次)
    StakeScale   []float64     // 每次加仓资金比例 (如 [1.0, 1.5, 2.0] 越跌越买)
    StopOnProfit bool          // 盈利后停止加仓
}
```

**具体任务：**
- [ ] DCA 配置模型 + 状态追踪
- [ ] 策略回调 `AdjustPosition()` 接入 DCA 逻辑
- [ ] 前端：DCA 设置 UI + 加仓历史展示

**预计工作量：** 1 周

---

### 3.3 交易所覆盖扩展 ⭐⭐ (来源: Freqtrade)

**当前状态：** 5 家交易所 (Binance/OKX/Coinbase/Gate/MEXC)
**Freqtrade 状态：** 30+ 交易所 (通过 CCXT)

**注意：** CCXT 是 Python/JS 库，Go 不能直接使用。但可以通过以下方式扩展：

**实施方案：**

#### 3.3.1 新增交易所适配器 (Go native)

| # | 交易所 | 优先级 | 理由 |
|---|--------|:------:|------|
| 1 | **Bybit** | 🔴 高 | 衍生品交易量前 3 |
| 2 | **Kraken** | 🔴 高 | 欧美主流, 监管合规 |
| 3 | **Bitget** | 🟡 中 | 跟单交易特色 |
| 4 | **KuCoin** | 🟡 中 | 小币种丰富 |
| 5 | **Hyperliquid** | 🟡 中 | 去中心化永续合约 |
| 6 | **dYdX** | 🟢 低 | 去中心化衍生品 |

#### 3.3.2 CCXT 桥接服务 (可选 Python sidecar)

如果需要快速支持 30+ 交易所：
```
sandbox/
├── ccxt_bridge.py                  # CCXT → Go 统一接口桥接 (新增)
│   # POST /ccxt/markets    — 获取市场信息
│   # POST /ccxt/klines     — K 线数据
│   # POST /ccxt/order      — 下单
│   # POST /ccxt/balance    — 余额查询
│   # WS  /ccxt/ws          — WebSocket 代理
```

Go 网关在 `adapter/` 新增 `ccxt.go` 适配器，通过 HTTP 调用 CCXT bridge。
**这种方式作为加速方案**，长期仍应实现 Go native 适配器 (性能更优)。

**具体任务：**
- [ ] Bybit Go native adapter (REST + WebSocket, 1 周)
- [ ] Kraken Go native adapter (REST + WebSocket, 1 周)
- [ ] CCXT bridge sidecar + generic adapter (1.5 周)
- [ ] 前端：交易所连接向导 (API Key 配置 + 权限测试按钮)

**预计工作量：** 3-4 周

---

### 3.4 回测增强 ⭐⭐ (来源: Freqtrade)

**当前状态：** 事件驱动回测 + 随机数据
**Freqtrade 状态：** 真实数据回测 + 缓存 + 前瞻偏差分析 + 递归检测

**实施方案：**
- [ ] 回测真实数据接入 (从 2.2 数据管理模块)
- [ ] 回测结果缓存 (reuse within cache age)
- [ ] 回测分解分析 (按天/周/月/年/工作日分解收益)
- [ ] 前瞻偏差检测 (lookahead bias analysis)
- [ ] 前端：回测对比模式 (A/B 策略对比)

**预计工作量：** 2 周

---

## 4. 第三阶段：ML 管线 (2026 Q4–2027 Q1)

> **目标：** 实现类 FreqAI 的机器学习预测管线
>
> **预计工期：** 10–12 周
>
> **新增代码量：** ~5,000 Go + ~8,000 Python + ~3,000 TS

### 4.1 ML 预测管线 (FreqAI 对标) ⭐⭐⭐⭐⭐ (来源: Freqtrade)

**当前状态：** 无 ML 训练/预测能力
**Freqtrade 状态：** 完整的 FreqAI 子系统 (14+ 模型, 特征工程, 在线学习, TensorBoard)

**架构决策：** ML 训练/推理用 Python (生态优势)，Go 网关负责调度 + 数据供给 + 信号消费。

```
sandbox/
├── ml_server/                       # ML 服务 (新建)
│   ├── main.py                      # FastAPI 入口
│   ├── data_kitchen.py              # 数据准备 (对齐 FreqAI DataKitchen)
│   ├── data_drawer.py               # 数据管理 (存储/加载模型)
│   ├── feature_engine.py            # 特征工程管线
│   ├── label_creator.py             # 标签生成策略
│   ├── trainer.py                   # 模型训练器
│   ├── predictor.py                 # 在线预测器
│   ├── retrain_scheduler.py         # 定期重训练调度
│   └── models/                      # 预测模型实现
│       ├── base_model.py            # 基类
│       ├── lightgbm_model.py        # LightGBM (回归/分类)
│       ├── xgboost_model.py         # XGBoost (回归/分类)
│       ├── pytorch_mlp.py           # PyTorch MLP
│       ├── pytorch_transformer.py   # PyTorch Transformer (时序)
│       └── reinforcement_learner.py # 强化学习 (Stable-Baselines3)
```

#### 4.1.1 特征工程管线

```python
# sandbox/ml_server/feature_engine.py

class FeaturePipeline:
    """自动特征工程"""
    - 价格特征: 收益率(n), 对数收益率, 高低价差
    - 成交量特征: 量比, OBV, MFI
    - 波动率特征: ATR, 布林带宽度, 历史波动率
    - 趋势特征: ADX, MACD 组件, 均线距离
    - 统计特征: z-score, 偏度, 峰度, 分位数
    - 多时间框架: 从 1h/4h/1d 聚合特征到当前时间框架
    - PCA 降维 (可选)
    - DBSCAN 异常值去除 (可选)
```

#### 4.1.2 标签创建策略

```python
# sandbox/ml_server/label_creator.py

class LabelCreator:
    """
    将未来价格变动转换为监督学习标签
    - 分类: 未来 N 根 K 线涨幅 > X% → 1, 跌幅 > X% → -1, else → 0
    - 回归: 未来 N 根 K 线的收益率
    - 多分类: 大幅上涨 / 小幅上涨 / 震荡 / 小幅下跌 / 大幅下跌
    """
```

#### 4.1.3 训练/预测生命周期

```
┌─────────────────────────────────────────────────────┐
│                    Go 网关                           │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐   │
│  │ 数据供给  │ → │ 训练触发  │ → │ 预测结果消费  │   │
│  │ (K线+特征)│   │ (定时/手动)│   │ (信号过滤/投票)│   │
│  └────┬─────┘   └─────┬────┘   └───────┬──────┘   │
│       │               │                │           │
│       ▼               ▼                ▲           │
│  ┌─────────────────────────────────────────────┐   │
│  │            ML Service (Python)               │   │
│  │  ┌─────────┐  ┌──────────┐  ┌──────────┐   │   │
│  │  │ 特征工程  │  │ 模型训练  │  │ 在线预测  │   │   │
│  │  └─────────┘  └──────────┘  └──────────┘   │   │
│  └─────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

**具体任务：**
- [ ] ML Service FastAPI 框架搭建
- [ ] 数据准备管线 (DataKitchen: 标准化、训练/测试分割、协变量偏移检测)
- [ ] 特征工程管线 (20+ 特征自动生成)
- [ ] 标签创建器 (分类 + 回归 + 多分类)
- [ ] 5 个模型实现 (LightGBM, XGBoost, PyTorchMLP, Transformer, RL)
- [ ] 模型持久化 + 版本管理 (MLflow 或本地文件)
- [ ] 在线预测端点 (Go 网关实时调用)
- [ ] 定期重训练调度器 (cron 表达式)
- [ ] TensorBoard 集成 (训练指标可视化)
- [ ] Go 网关：ML 信号消费 (与现有 AI 投票管道集成)
- [ ] 前端：ML 配置页面 (特征选择 + 模型选择 + 训练进度 + 特征重要性图)

**后端 API 新增：**
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/ml/train` | 启动训练 |
| GET | `/api/ml/status/:modelId` | 训练状态 |
| POST | `/api/ml/predict` | 实时预测 |
| GET | `/api/ml/models` | 模型列表 |
| POST | `/api/ml/deploy` | 部署模型到策略 |
| GET | `/api/ml/features/:modelId` | 特征重要性 |
| GET | `/api/ml/tensorboard` | TensorBoard 代理 |

**预计工作量：** 5-6 周

---

### 4.2 强化学习交易环境 ⭐⭐⭐ (来源: Freqtrade FreqAI RL)

```python
# sandbox/ml_server/models/reinforcement_learner.py

class TradingEnv(gym.Env):
    """
    Gymnasium 交易环境
    动作空间: 3 (做空/持有/做多) 或 5 (满仓空/半仓空/持有/半仓多/满仓多)
    观察空间: 价格特征 + 技术指标 + 持仓状态 + 账户状态
    奖励函数: 对数收益率 - 交易成本 - 回撤惩罚
    """
```

**具体任务：**
- [ ] 实现 3-Action 和 5-Action 交易环境
- [ ] Stable-Baselines3 PPO/A2C/SAC 训练集成
- [ ] 奖励函数可配置 (夏普/收益率/风险调整)
- [ ] 训练曲线可视化

**预计工作量：** 2 周

---

## 5. 第四阶段：全资产覆盖 (2027 Q1–Q2)

> **目标：** 从纯加密货币扩展到传统金融市场
>
> **预计工期：** 8–10 周
>
> **新增代码量：** ~6,000 Go + ~3,000 TS

### 5.1 传统券商接入 ⭐⭐⭐⭐ (来源: QuantDinger)

**当前状态：** 仅加密货币
**QuantDinger 状态：** IBKR, MT5, Alpaca

**实施方案：**

```
gateway/internal/adapter/
├── ibkr.go                         # Interactive Brokers (TWS API / Client Portal REST)
├── alpaca.go                       # Alpaca Markets (REST + WebSocket)
├── mt5.go                          # MetaTrader 5 (ZeroMQ bridge to MT5 Python)
└── xtp.go                          # 中泰 XTP (A股极速交易, 可选)
```

#### 5.1.1 IBKR 适配器

IBKR 提供 `Client Portal API` (REST) 和 `TWS API` (Socket)。采用 REST 模式：

```go
// gateway/internal/adapter/ibkr.go

type IBKRAdapter struct {
    baseURL    string   // https://localhost:5000/v1/api (Client Portal Gateway)
    accountID  string
    // ...
}

// 支持功能:
// - 股票/ETF/期权/期货/外汇 多资产交易
// - 实时行情订阅
// - 账户/持仓/订单管理
// - 企业事件 (分红/拆股)
// - 期权链查询
```

#### 5.1.2 Alpaca 适配器

```go
// gateway/internal/adapter/alpaca.go

type AlpacaAdapter struct {
    apiKey     string
    apiSecret  string
    baseURL    string   // https://paper-api.alpaca.markets (模拟) or live
    // ...
}

// 支持功能:
// - 美股/ETF 交易
// - 零股交易 (Fractional Shares)
// - 实时 WebSocket 行情
// - 企业事件 (分红/拆股)
// - 期权交易 (V2 API)
```

#### 5.1.3 MT5 适配器

MT5 没有原生 HTTP API，采用 ZeroMQ Bridge 方案：

```
sandbox/
├── mt5_bridge.py                   # MT5 ZeroMQ 桥接服务 (新增)
│   # 使用 MetaTrader5 Python SDK
│   # Go 网关通过 ZMQ/HTTP 调用
```

**具体任务：**
- [ ] IBKR Client Portal REST 适配器
- [ ] Alpaca REST + WebSocket 适配器
- [ ] MT5 ZeroMQ Bridge (Python sidecar → Go 调用)
- [ ] 多资产账户模型扩展 (Account/Position 支持 stock/option/future/forex)
- [ ] 前端：券商账户连接向导 (IBKR/Alpaca/MT5)
- [ ] 前端：多资产交易面板 (股票/期权/期货下单)

**后端 API 扩展：**
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/brokers` | 已连接券商列表 |
| POST | `/api/brokers/connect` | 连接券商 |
| GET | `/api/brokers/:id/account` | 券商账户摘要 |
| GET | `/api/brokers/:id/positions` | 券商持仓 |
| POST | `/api/brokers/:id/orders` | 券商下单 |

**预计工作量：** 4-5 周

---

### 5.2 期权交易支持 ⭐⭐

**具体任务：**
- [ ] 期权链查询 (到期日 + 行权价)
- [ ] 希腊值计算 (Delta/Gamma/Theta/Vega/Rho)
- [ ] 期权策略模板 (Covered Call, Cash-Secured Put, Iron Condor, etc.)
- [ ] 前端：期权链视图 + 策略构建器

**预计工作量：** 2-3 周

---

### 5.3 A 股市场接入 ⭐⭐ (特色功能)

**具体任务：**
- [ ] 中泰 XTP 极速交易适配器 (Go native, 通过 CGo 调用 XTP C++ API)
- [ ] 东方财富/新浪免费行情数据源
- [ ] A 股特有规则适配 (T+1, 涨跌停, 可转债)
- [ ] 前端：A 股交易面板

**预计工作量：** 2 周

---

## 6. 第五阶段：生态与社区 (2027 Q2–Q3)

> **目标：** 建立开发者生态和用户社区
>
> **预计工期：** 8–10 周
>
> **新增代码量：** ~5,000 Go + ~6,000 TS

### 6.1 策略/指标市场 ⭐⭐⭐ (来源: QuantDinger)

**当前状态：** 指标社区 (后端 CRUD 完成, 前端半成品)
**QuantDinger 状态：** 完整的指标社区 (发布/购买/评论/评分)

**具体任务：**
- [ ] 完成策略发布流程 (名称 + 描述 + 代码 + 回测结果 + 参数说明)
- [ ] 评论 + 评分系统 (已完成数据库表, 需前端)
- [ ] 过拟合风险检测 (样本内外对比, 参数敏感性分析)
- [ ] 策略排行榜 (按收益率/夏普/订阅数排序)
- [ ] 策略版本管理 (发布更新, 用户可选择版本)
- [ ] 策略订阅/关注 + 更新推送
- [ ] 积分/打赏系统 (可选)

**前端页面完善：**
- [ ] IndicatorCommunity 页面完善 (购买/评论/评分 UI)
- [ ] 策略详情页 (含回测结果图表 + 评论 + 评分)
- [ ] 个人策略管理 (我的发布 + 我的购买 + 我的收藏)

**预计工作量：** 3-4 周

---

### 6.2 文档站点 ⭐⭐ (来源: Freqtrade)

**Freqtrade 状态：** MkDocs 50+ 页完整文档, ReadTheDocs 托管

**实施方案：**

```
docs/                                # 新建目录
├── index.md                         # 首页
├── getting-started/
│   ├── installation.md
│   ├── quickstart.md
│   └── configuration.md
├── trading/
│   ├── exchanges.md
│   ├── order-types.md
│   └── risk-management.md
├── strategy/
│   ├── strategy-basics.md
│   ├── strategy-callbacks.md
│   ├── strategy-parameters.md
│   └── strategy-examples.md
├── backtesting/
│   ├── backtesting-guide.md
│   └── hyperopt-guide.md
├── ai-ml/
│   ├── ai-strategy-generation.md
│   ├── multi-agent-pipeline.md
│   └── ml-prediction.md
├── api/
│   ├── rest-api.md
│   └── websocket.md
├── development/
│   ├── architecture.md
│   ├── contributing.md
│   └── testing.md
└── deployment/
    ├── docker.md
    └── production.md
```

- [ ] 使用 VitePress (Vue 驱动) 或 Docusaurus (React 驱动) 构建文档站
- [ ] 内置策略教程 (5 个由浅入深的策略示例)
- [ ] API 参考自动生成 (Go → OpenAPI → 文档)
- [ ] 视频教程 (可选)

**预计工作量：** 2-3 周

---

### 6.3 Telegram/Discord Bot ⭐⭐ (来源: Freqtrade)

**Freqtrade 状态：** Telegram Bot 40+ 命令, Discord Webhook

**当前状态：** 有 Telegram HTTP 通知 (单向)，无交互式 Bot

**具体任务：**
- [ ] Telegram Bot 交互式命令 (基于 `python-telegram-bot` 或 Go `telebot`)
  ```
  /status     — 当前状态
  /profit     — 盈亏摘要
  /daily      — 今日交易
  /performance — 性能指标
  /balance    — 余额
  /trades     — 当前持仓
  /force_entry BTC/USDT — 强制入场
  /force_exit BTC/USDT  — 强制出场
  /start /stop /pause   — 启停控制
  /whitelist /blacklist — 交易对管理
  /logs       — 最近日志
  /alert      — 设置告警
  ```
- [ ] Discord Bot (Webhook 双向)
- [ ] Go 网关集成 Bot 服务 (非 Python 依赖)

**设计决策：** 用 Go 实现 Bot 服务 (`gateway/internal/bot/`)，使用 Telegram Bot API。保持架构纯净。

```
gateway/internal/bot/               # 新建包
├── telegram.go                     # Telegram Bot 实现
├── discord.go                      # Discord Bot 实现
├── commands.go                     # 命令注册与分发
└── bot_manager.go                  # Bot 生命周期管理
```

**预计工作量：** 2 周

---

### 6.4 Admin 面板 ⭐⭐ (来源: QuantDinger)

**当前状态：** 基础 admin API (用户 CRUD, 统计)
**QuantDinger 状态：** 完整 Admin 面板

**具体任务：**
- [ ] 用户管理页面 (列表 + 搜索 + 角色/权限编辑 + 禁用)
- [ ] 系统监控页面 (CPU/内存/协程数/QPS/延迟)
- [ ] 审计日志页面 (操作记录 + 筛选 + 导出)
- [ ] 系统配置页面 (全局设置 UI)
- [ ] Agent Token 管理页面

**预计工作量：** 2 周

---

### 6.5 移动端 ⭐⭐

**当前状态：** 桌面端 only (部分响应式)

**具体任务：**
- [ ] TailwindCSS 响应式断点全面覆盖 (sm/md/lg/xl)
- [ ] 移动端导航 (底部 Tab Bar)
- [ ] 移动端 K 线图优化 (简化交互)
- [ ] PWA 支持 (离线缓存 + 主屏幕安装)
- [ ] 原生 App 包装 (React Native / Capacitor — 长期可选)

**预计工作量：** 3-4 周

---

## 7. 架构约束与决策

### 7.1 铁律：Go + Rust + TypeScript

| 禁止 | 替代方案 |
|------|---------|
| ❌ Python 做核心业务逻辑 | ✅ Go 网关统一调度, Python 仅限 sandbox sidecar |
| ❌ JavaScript 后端 | ✅ TypeScript 仅限前端 |
| ❌ 引入 Java/Spring 等新语言栈 | ✅ 全部用 Go/Rust/TS 实现 |
| ❌ 数据库从 SQLite 迁移到 PostgreSQL 作为唯一方案 | ✅ SQLite 默认, PostgreSQL/MySQL 可选适配 |

### 7.2 Python Sidecar 使用边界

Python 仅用于以下场景 (生态依赖无法避免)：
1. **机器学习训练** (Optuna / LightGBM / XGBoost / PyTorch / Stable-Baselines3 无成熟 Go 替代)
2. **TA-Lib 计算** (已有 sandbox)
3. **MT5 桥接** (MetaTrader5 只有 Python SDK)
4. **CCXT 桥接** (临时加速方案)

Python sidecar 永远是无状态的 HTTP 服务，Go 网关通过 REST 调用。不共享数据库，不共享状态。

### 7.3 文件组织原则

```
gateway/internal/{domain}/
├── handler.go          # HTTP 处理器
├── service.go          # 业务逻辑
├── model.go            # 领域模型
├── repository.go       # 数据访问 (可选, 简单场景直接调 store)
└── {domain}_test.go    # 单元测试
```

### 7.4 前端状态管理原则

- Zustand: 全局应用状态 (认证、主题、语言)
- TanStack Query: 服务端数据 (行情、订单、持仓) — 自动缓存/重取/失效
- 组件内部 useState: 纯 UI 状态

---

## 8. 实施优先级排序

### 8.1 按 ROI (投入产出比) 排序

| 排名 | 模块 | 工作量 | 价值 | ROI | 阶段 |
|:----:|------|:------:|:----:|:---:|:----:|
| 1 | 测试基础设施 | 2-3w | 高 | ⭐⭐⭐⭐⭐ | Phase 1 |
| 2 | Pairlist 交易对筛选 | 2-3w | 高 | ⭐⭐⭐⭐⭐ | Phase 1 |
| 3 | 回测真实数据接入 | 2w | 高 | ⭐⭐⭐⭐⭐ | Phase 1 |
| 4 | 策略框架增强 (回调+参数) | 2-3w | 高 | ⭐⭐⭐⭐ | Phase 1 |
| 5 | 止损增强 (跟踪止损) | 1w | 高 | ⭐⭐⭐⭐ | Phase 1 |
| 6 | 数据管理增强 | 1.5-2w | 中 | ⭐⭐⭐⭐ | Phase 1 |
| 7 | Hyperopt 超参优化 | 3-4w | 高 | ⭐⭐⭐⭐ | Phase 2 |
| 8 | 交易所覆盖扩展 | 3-4w | 中 | ⭐⭐⭐ | Phase 2 |
| 9 | DCA 仓位调整 | 1w | 中 | ⭐⭐⭐ | Phase 2 |
| 10 | 保护机制 | 1-1.5w | 中 | ⭐⭐⭐ | Phase 1 |
| 11 | ML 预测管线 | 5-6w | 高 | ⭐⭐⭐ | Phase 3 |
| 12 | 策略/指标市场完善 | 3-4w | 中 | ⭐⭐⭐ | Phase 5 |
| 13 | 传统券商接入 | 4-5w | 中 | ⭐⭐⭐ | Phase 4 |
| 14 | Telegram/Discord Bot | 2w | 中 | ⭐⭐⭐ | Phase 5 |
| 15 | 文档站点 | 2-3w | 中 | ⭐⭐ | Phase 5 |
| 16 | Admin 面板 | 2w | 中 | ⭐⭐ | Phase 5 |
| 17 | RL 交易环境 | 2w | 低 | ⭐⭐ | Phase 3 |
| 18 | 移动端适配 | 3-4w | 中 | ⭐⭐ | Phase 5 |
| 19 | 期权交易支持 | 2-3w | 低 | ⭐⭐ | Phase 4 |
| 20 | A 股市场接入 | 2w | 低 | ⭐⭐ | Phase 4 |

### 8.2 里程碑时间线

```
2026 Q2 ──── Q3 ──── Q4 ──── 2027 Q1 ──── Q2 ──── Q3
│          │          │          │          │          │
│ Phase 1  │ Phase 1  │ Phase 2  │ Phase 3  │ Phase 4  │ Phase 5  │
│ 基础夯实  │ 基础夯实  │ 策略深度  │ ML 管线  │ 全资产    │ 生态社区  │
│          │          │          │          │          │          │
│ █████████│███       │          │          │          │          │
│ 测试+数据 │ 策略增强  │          │          │          │          │
│          │          │ ████████ │████      │          │          │
│          │          │ Hyperopt │ 交易所    │          │          │
│          │          │          │ ████████ │███       │          │
│          │          │          │ ML 模型   │ 券商接入  │          │
│          │          │          │          │ ████████ │███       │
│          │          │          │          │          │ 社区+文档  │
│          │          │          │          │          │          │
▼ v2.0     ▼          ▼          ▼          ▼          ▼ v3.0     ▼
```

### 8.3 立即行动 (本周可开始)

1. **Go 测试框架搭建** — 从 `adapter/matching_test.go` 和 `risk/risk_test.go` 开始
2. **真实数据接入回测** — 修改 `backtest/runner.go`，从交易所 REST API 拉取历史 K 线
3. **WebSocket 断线重连** — 修改 `web/src/lib/klineDatafeed.ts`，增加断线补数据逻辑

### 8.4 短期行动 (本月)

4. **Pairlist 管理器** — 先实现 Volume + Spread + Price 三个最基本过滤器
5. **策略回调扩展** — 先实现 CustomStoploss + CustomStakeAmount + ConfirmTradeEntry
6. **跟踪止损** — 先实现 Fixed 模式
7. **Bybit 适配器** — 复用 Binance 适配器 (API 高度兼容)

---

## 附录 A：与 Freqtrade 的逐文件对标

| Freqtrade 文件 | 功能 | XiaoTianQuant 对应 | 差距 |
|---------------|------|-------------------|------|
| `freqtradebot.py` (2650 行) | 核心交易逻辑 | `gateway/internal/order/` + `risk/` + `strategy/engine.go` | 缺少 DCA/止损交易所下单/部分成交处理 |
| `strategy/interface.py` | 策略 ABC | `gateway/internal/strategy/` | 缺少 10 个回调, 参数系统, 多 TF |
| `optimize/backtesting.py` | 回测引擎 | `gateway/internal/backtest/runner.go` | 功能相近, 缺真实数据/缓存/分解 |
| `optimize/hyperopt/` | 超参优化 | `gateway/internal/experiment/` | 功能差距大, 无 Optuna 集成 |
| `freqai/` | ML 管线 | 无 | 完全缺失 |
| `plugins/pairlist/` (18 个) | 交易对筛选 | 无 | 完全缺失 |
| `plugins/protections/` (5 个) | 保护机制 | `gateway/internal/risk/` | 风控有但缺自动恢复 |
| `exchange/exchange.py` | 交易所基类 | `gateway/internal/adapter/` | 覆盖少 (5 vs 30+) |
| `data/` | 数据管理 | 基础 K 线拉取 | 缺下载/转换/多格式 |
| `rpc/telegram.py` | Telegram Bot | `gateway/internal/notify/` | 仅通知, 无交互 Bot |
| `persistence/` | 数据库层 | `gateway/internal/store/` | 功能相近 |
| `configuration/` | 配置管理 | `gateway/internal/config/` | 功能相近 |
| `resolvers/` | 动态加载 | Go 不支持动态加载 | 不适用 (Go 编译型) |

## 附录 B：与 QuantDinger 的逐功能对标

| QuantDinger 功能 | 文件 | XiaoTianQuant 对应 | 差距 |
|-----------------|------|-------------------|------|
| 券商账户 | `broker/` | 无 | 完全缺失 |
| 指标社区 | `indicator_community/` | `indicator/` + `community/` | 后端完成, 前端半成品 |
| Agent 网关 | `agent_gateway/` | `agent/` | 功能相近 |
| MCP 协议 | `quantdinger-mcp` | `agent/` 部分支持 | 需完善 + pip 发布 |
| 策略模板 | `strategy_templates/` | `strategy/templates/` (前端) | 功能相近 |
| Admin 面板 | Vue Admin 页面 | 基础 API | 缺完整前端 |

---

## 附录 C：技术选型参考

| 组件 | Freqtrade 方案 | QuantDinger 方案 | XiaoTianQuant 方案 | 评价 |
|------|:-------------:|:---------------:|:-----------------:|------|
| 后端语言 | Python | Python/Flask | **Go/Gin** | 性能优势 |
| 撮合引擎 | 无 (依赖交易所) | 无 (依赖交易所) | **Rust cdylib** | 独有优势 |
| 前端 | FreqUI (简易) | Vue 2 | **React 19** | 技术栈领先 |
| 数据库 | SQLAlchemy + SQLite/PG | PostgreSQL | SQLite (modernc) | 可扩展 PG |
| 交易所库 | CCXT | CCXT | Go native + CCXT bridge | 需扩展 |
| ML 框架 | Optuna + LightGBM/XGBoost | scikit-learn | Python sidecar | 生态借力 |
| 策略语言 | Python 子类 | Python | Go 模板 + Python 沙箱 | 有得有失 |

---

> **总结：** XiaoTianQuant 在架构 (Go+Rust+TS) 和前端体验上有显著优势，核心差距在于 Freqtrade 多年积累的**策略生态深度** (Hyperopt/FreqAI/Pairlist) 和 QuantDinger 的**全资产覆盖** (传统券商)。通过 5 个阶段、约 40-50 周的迭代，可以达到并部分超越两个对标项目的功能完整度，同时保持架构优势。

---

# 附录 D：代码审查与安全修复清单（2026-06-03）

> 本章节记录 2026-06-03 代码审查中发现的所有安全和质量问题，按优先级排列。修复状态用 `[ ]` 未修复 / `[x]` 已修复标注。

---

## D.1 P0 级 — 严重安全/构建阻塞问题（立即修复）

### D.1.1 🔴 Go 版本不存在 — 构建阻塞

- **位置：** `gateway/go.mod:3` 声明 `go 1.25.0`，`Dockerfile:16` 使用 `golang:1.25-alpine`
- **问题：** Go 最新稳定版为 1.24.x，1.25 尚未发布。`go mod tidy` / Docker 构建将失败
- **修复：** 降级到 `go 1.23`（已验证可用的稳定版本）
- **状态：** `[x]`

### D.1.2 🔴 SQL 注入漏洞

- **位置：** `gateway/internal/store/store.go:434`（`UpdateUserProfile`）、`:446`（`AdminUpdateUser`）
- **问题：**
  ```go
  db.Exec(`UPDATE xt_users SET `+field+`=? WHERE username=?`, value, username)  // 字段名拼接
  ```
  `field` 参数来自用户输入，未做严格白名单校验。攻击者可注入 SQL（如 `field="role='admin', password_hash='xxx' --"`）
- **修复：** 使用严格字段白名单 + map 参数映射，彻底禁止字符串拼接 SQL
- **状态：** `[x]`

### D.1.3 🔴 密码哈希算法不安全

- **位置：** `gateway/internal/store/store.go:237-248`
- **问题：** 使用 SHA256 + salt 作为密码哈希。SHA256 不是密码哈希算法，无 work factor，易被 GPU/ASIC 暴力破解
- **修复：** 改用 `golang.org/x/crypto/bcrypt`（成本因子 12）
- **状态：** `[x]`

### D.1.4 🔴 止损管理器参数传递错误

- **位置：** `gateway/internal/order/stoploss.go:305`、`:376`、`:400`
- **问题：** `CancelOrder` 调用传错参数：
  ```go
  s.orderPlacer.CancelOrder(s.exchangeOrder.OrderID, s.exchangeOrder.OrderID)  // 两次都是 OrderID
  ```
  而接口定义为 `CancelOrder(symbol, orderID string)`。导致无法正确取消旧止损单，可能重复下单
- **修复：** 修正为 `CancelOrder(s.exchangeOrder.Symbol, s.exchangeOrder.OrderID)`
- **状态：** `[x]`

### D.1.5 🔴 大量敏感 API 路由缺少鉴权

- **位置：** `gateway/cmd/server/main.go:135-176`
- **问题：** 以下路由未使用 `middleware.AuthRequired()`：
  - `GET /api/orders` — 用户订单列表
  - `POST /api/orders` — 下单
  - `DELETE /api/orders/:id` — 撤单
  - `GET /api/account/balance` — 账户余额
  - `GET /api/trades` — 成交历史
  - `GET /api/portfolio/*` — 资产信息
  - `/api/strategies/*` — 策略管理
  - `/api/backtest/*` — 回测
- **风险：** 任何人都可以访问/操作这些接口
- **修复：** 为所有敏感路由添加 `.Use(middleware.AuthRequired())`
- **状态：** `[x]`

---

## D.2 P1 级 — 中高优先级问题（本周修复）

### D.2.1 ⚠️ JWT Token 从 Query 参数读取

- **位置：** `gateway/internal/middleware/middleware.go:84-85`
- **问题：** `extractToken` 支持从 URL Query 读取 token：`return c.Query("token")`。Token 出现在 URL 中会被服务器日志、浏览器历史、第三方 Referrer 泄露
- **修复：** 移除 Query 参数支持，仅保留 `Authorization: Bearer <token>` Header
- **状态：** `[x]`

### D.2.2 ⚠️ CORS 允许所有来源

- **位置：** `gateway/internal/middleware/middleware.go:19-28`
- **问题：** `Access-Control-Allow-Origin: *` 允许任意域名跨域访问生产环境 API
- **修复：** 生产环境限制为配置的域名，开发环境允许 localhost
- **状态：** `[x]`

### D.2.3 ⚠️ 前端 Period 映射键名不一致

- **位置：** `web/src/lib/klineDatafeed.ts:16-34`
- **问题：** `PERIOD_MAP` 使用键 `'1min'`，`PERIOD_MS` 使用键 `'1minute'`，`periodKey()` 函数生成 `'1minute'` 与前者不匹配，导致 `periodMs()` 回退到默认值 1h
- **修复：** 统一键名命名约定
- **状态：** `[x]`

### D.2.4 ⚠️ KLineDatafeed limit 计算硬编码 1h

- **位置：** `web/src/lib/klineDatafeed.ts:360`
- **问题：** `const limit = Math.min(Math.ceil((toMs - fromMs) / 3600000) + 200, 1500)` 中 `3600000` 是固定的 1 小时毫秒数，对于 1m/5m 等短周期会导致 limit 计算错误
- **修复：** 使用 `periodMs(period)` 替代硬编码值
- **状态：** `[x]`

### D.2.5 ⚠️ Rust FFI 边界检查缺失

- **位置：** `engine/src/ffi.rs`
- **问题：** `engine_submit_order` 等函数对 JSON 解析失败、引擎未找到等场景返回错误字符串，但部分函数（如 `engine_trade_count`）在引擎不存在时静默返回 0，调用方无法区分"无交易"和"引擎不存在"
- **修复：** 统一错误处理模式，或添加更明确的错误返回值
- **状态：** `[ ]`

---

## D.3 P2 级 — 低优先级问题/优化建议（本月修复）

### D.3.1 🔶 订单簿 f64 精度问题

- **位置：** `engine/src/orderbook.rs:102-117`（`OrderedFloat::from_price`）
- **问题：** 使用 `f64::to_bits()` 作为 BTreeMap 键。`NaN`、`-0`/`+0`、不同 bit pattern 的相同值会产生不同键，导致订单簿中出现"相同价格但不同键"的幽灵条目
- **修复：** 改用定点数（如 `u64` 表示 price × 10^8）替代 `f64`
- **状态：** `[ ]`

### D.3.2 🔶 取消订单效率低

- **位置：** `engine/src/orderbook.rs:153-171`
- **问题：** `cancel_order` 使用线性搜索 O(n) 遍历所有订单。对大型订单簿性能差
- **修复：** 维护 `order_id → (side, price_key)` 的索引映射
- **状态：** `[ ]`

### D.3.3 🔶 Trading.tsx KLineChartPro 初始化不够健壮

- **位置：** `web/src/pages/Trading.tsx:250-272`
- **问题：**
  - 使用 `chartRef.current.innerHTML = ""` 清除 DOM（非 React 推荐方式）
  - 使用 `window.setInterval` 轮询 `_chartApi` 属性
  - 空依赖数组导致 symbol/interval 变更不重新初始化
- **修复：** 使用 ref callback 替代 innerHTML，通过 `chartApiRef` 可靠检测初始化完成
- **状态：** `[ ]`

### D.3.4 🔶 默认 admin 密码硬编码

- **位置：** `gateway/internal/store/store.go:83`
- **问题：** 默认管理员密码 `"admin123"` 硬编码在源码中，部署后若用户未修改存在安全风险
- **修复：** 首次启动生成随机密码并打印到日志，或强制首次登录修改密码
- **状态：** `[x]`

### D.3.5 🔶 JWT 过期时间过长

- **位置：** `gateway/internal/store/store.go:257`
- **问题：** `exp: time.Now().Add(7 * 24 * time.Hour)` — 7 天 Token 有效期过长，泄露后风险窗口大
- **修复：** 缩短为 24h，配合 Refresh Token 机制
- **状态：** `[x]`

### D.3.6 🔶 代码风格不一致

- **位置：** 多处
- **问题：** `main.go` 路由缩进混乱（混用 tab 和不同空格数），部分文件行过长
- **修复：** 统一使用 `gofmt` / `gofumpt` 格式化，前端使用 `eslint --fix`
- **状态：** `[x]`

### D.3.7 🔶 Docker Compose 引用不存在的目录

- **位置：** `docker-compose.yml:82-165`
- **问题：** 引用了 `./sandbox` 目录和 `ml_server` / `ccxt_bridge` / `sandbox` 服务，但项目中不存在 `sandbox/` 目录
- **修复：** 创建 `sandbox/` 目录骨架，或更新 docker-compose 移除这些服务（标记为可选）
- **状态：** `[x]` *(目录已存在，Dockerfile、main.py、ml_server、ccxt_bridge 均齐全)*

### D.3.8 🔶 二进制文件不应在版本控制中

- **位置：** `gateway/gateway-server.exe`、`gateway/gateway.exe` 等
- **问题：** 编译产物不应提交到 Git，会污染仓库历史
- **修复：** 添加到 `.gitignore`，从历史中移除（可选）
- **状态：** `[x]` *(.gitignore 已包含 `*.exe`，Git 中无跟踪的二进制文件)*

---

## D.4 修复进度追踪

| # | 问题 | 优先级 | 文件 | 状态 |
|---|------|:------:|------|:----:|
| 1 | Go 版本降级 1.25→1.23 | P0 | `go.mod`, `Dockerfile` | `[x]` |
| 2 | SQL 注入修复 | P0 | `store/store.go` | `[x]` |
| 3 | SHA256→bcrypt | P0 | `store/store.go` | `[x]` |
| 4 | 止损 CancelOrder 参数 | P0 | `order/stoploss.go` | `[x]` |
| 5 | API 路由鉴权补齐 | P0 | `cmd/server/main.go` | `[x]` |
| 6 | JWT 移除 Query 参数 | P1 | `middleware/middleware.go` | `[x]` |
| 7 | CORS 限制来源 | P1 | `middleware/middleware.go` | `[x]` |
| 8 | Period 键名统一 | P1 | `klineDatafeed.ts` | `[x]` |
| 9 | KLine limit 硬编码 | P1 | `klineDatafeed.ts` | `[x]` |
| 10 | 订单簿 f64→定点数 | P2 | `orderbook.rs` | `[ ]` *(需架构决策)* |
| 11 | cancel_order 索引优化 | P2 | `orderbook.rs` | `[ ]` *(需架构决策)* |
| 12 | Trading.tsx 初始化 | P2 | `Trading.tsx` | `[ ]` *(需前端重构)* |
| 13 | Admin 默认密码 | P2 | `store/store.go` | `[x]` |
| 14 | JWT 过期时间 | P2 | `store/store.go` | `[x]` |
| 15 | 代码风格统一 | P2 | 多处 | `[x]` |
| 16 | sandbox 目录缺失 | P2 | `docker-compose.yml` | `[x]` *(目录已存在)* |
| 17 | 二进制文件清理 | P2 | `.gitignore` | `[x]` *(已配置)* |

---

# 附录 E：交易页面 Bug 审查与修复清单（2026-06-04）

> 本章节记录 2026-06-04 对交易页面（Trading/SpotTrading/ContractTrading/KlineDatafeed/QuickTradePanel）全面代码审查中发现的所有 Bug。修复状态用 `[ ]` 未修复 / `[x]` 已修复标注。

---

## E.1 严重级别 (Critical) — 会导致功能异常

### E.1.1 🔴 切换交易对后实时成交数据未清空 — 数据串号

- **位置：** `web/src/pages/SpotTrading.tsx:169` / `ContractTrading.tsx:132`
- **问题：** 切换 `symbol`（如 BTCUSDT → ETHUSDT）时，`liveTrades` state 未清空，页面显示上一个币种的历史成交记录
- **影响：** 用户看到错误的成交数据，可能做出错误交易决策
- **修复：** 添加 `useEffect(() => { setLiveTrades([]) }, [symbol])` 监听 symbol 变化
- **状态：** `[x]` — `SpotTrading.tsx` 和 `ContractTrading.tsx` 均已添加 symbol 变化时清空 liveTrades 的逻辑

### E.1.2 🔴 K线实时更新 — 新Bar的OHLC初始化完全错误

- **位置：** `web/src/lib/klineDatafeed.ts:168-179`
- **问题：** `handlePriceTick` 创建新周期 Bar 时，错误地用**上一根历史K线**的 `open/high/low/volume` 初始化新 Bar：
  ```ts
  bar = {
    timestamp: alignedTs,
    open: hist.open,        // ❌ 应该是 lastPrice
    high: Math.max(hist.high, lastPrice),  // ❌ 应该从 lastPrice 开始
    low: Math.min(hist.low, lastPrice),    // ❌ 应该从 lastPrice 开始
    close: lastPrice,
    volume: hist.volume,    // ❌ 应该从 0 开始
  }
  ```
- **影响：** 新K线"继承"上一根K线数据，K线图出现严重视觉和数据错误
- **修复：** 新 Bar 的 `open/high/low/close` 均用 `lastPrice` 初始化，`volume` 用当前 tick 的 volume
- **状态：** `[x]` — 已移除 `historyLastBar` 依赖，新 Bar 的 OHLC 全部用 `lastPrice` 初始化，`volume` 用传入的 `volume` 参数

### E.1.3 🔴 API Response Interceptor — 标准信封格式导致数据丢失

- **位置：** `web/src/lib/api.ts:71-77` / `api.ts:191-193`
- **问题：** Response interceptor 的 unwrap 逻辑 `!('success' in data)` 导致后端返回 `{success: true, data: {klines: [...]}}` 时：
  1. Interceptor 不 unwrap（`success` 属性存在）
  2. `marketApi.klines` 中 `d?.klines` 为 `undefined`
  3. `Array.isArray(d)` 为 `false`
  4. **最终返回空数组 `[]`**
- **影响：** 如果后端使用标准信封格式，K线、订单列表等 API 全部返回空数据
- **修复：** 在 `marketApi.klines` 等 API 中增加对 envelope 格式的兼容解析
- **状态：** `[x]` — `marketApi.klines` 已增加 `d?.data?.klines` 兼容路径，同时对所有 API 保持向后兼容

### E.1.4 🔴 QuickTradePanel — 状态更新竞态条件

- **位置：** `web/src/components/QuickTradePanel.tsx:394-401`
- **问题：** `onSideChange('BUY'); onPlaceOrder()` 在同一事件处理中顺序调用，但 React state 更新是异步的，`onPlaceOrder` 可能使用父组件中尚未更新的 `side` 值
- **影响：** 用户点击"买入"按钮，实际可能用"卖出"参数下单
- **修复：** 让 `onPlaceOrder` 接收 `side` 参数，或使用回调式更新确保顺序
- **状态：** `[x]` — `onPlaceOrder` 接口改为 `onPlaceOrder(side: 'BUY' | 'SELL')`，所有按钮调用时直接传入明确的 side，消除竞态条件

---

## E.2 高级别 (High) — 影响用户体验

### E.2.1 ⚠️ 合约交易页面 — 缺少交易对切换功能

- **位置：** `web/src/pages/ContractTrading.tsx:76-578`
- **问题：** 合约交易页面完全没有提供切换交易对（Symbol）的 UI。`symbol` state 存在（默认 BTCUSDT），但用户无法切换
- **修复：** 添加交易对选择器或复用 Watchlist 组件
- **状态：** `[ ]`

### E.2.2 ⚠️ K线 Datafeed — Symbol 前缀匹配导致数据串扰

- **位置：** `web/src/lib/klineDatafeed.ts:158` / `:270`
- **问题：** `key.startsWith(symbol + ':')` 使用前缀匹配。symbol 为 "BTC" 时，会同时匹配 "BTCUSDT:1h" 和 "BTCUSD:1h"
- **修复：** 使用精确匹配 `key.split(':')[0] !== symbol`
- **状态：** `[ ]`

### E.2.3 ⚠️ 现货交易 — 卖出时百分比按钮计算错误

- **位置：** `web/src/pages/SpotTrading.tsx:507-513`
- **问题：** 百分比按钮（25%/50%/75%/100%）始终使用 `spotBalance`（USDT总余额）计算。当用户点击**卖出**时，应该用持有的**BTC余额**计算
- **修复：** 根据 `side` 选择对应余额进行计算
- **状态：** `[ ]`

### E.2.4 ⚠️ useToast.tsx — Import 语句放在文件末尾

- **位置：** `web/src/lib/useToast.tsx:64`
- **问题：** `import { cn } from './utils'` 放在文件最后一行，代码可读性极差，部分构建工具可能报错
- **修复：** 将 import 移到文件顶部
- **状态：** `[ ]`

---

## E.3 中级别 (Medium) — 需要修复

### E.3.1 🔶 QuickTradePanel — 硬编码 Mock 数据

- **位置：** `web/src/components/QuickTradePanel.tsx:92` / `:345` / `:368-369`
- **问题：** `balance = 10000`、`12,450.50 USDT`、`0.8450 BTC` 等使用硬编码值，不是真实账户数据
- **修复：** 从 props 传入真实账户数据
- **状态：** `[ ]`

### E.3.2 🔶 useWebSocket — stale `onReconnect` 闭包

- **位置：** `web/src/hooks/useWebSocket.ts:132`
- **问题：** `useEffect` 依赖数组缺少 `onReconnect`，组件重新渲染时新的 `onReconnect` 函数不会被使用
- **修复：** 将 `onReconnect` 加入依赖数组，或使用 `useRef` 保存最新值
- **状态：** `[ ]`

### E.3.3 🔶 useWebSocket — 单个 Callback 异常阻断后续 Callback

- **位置：** `web/src/hooks/useWebSocket.ts:94-105`
- **问题：** `callbacks.forEach((cb) => cb(data))` 中某个 callback 抛异常会导致同类型其他 callback 不被执行
- **修复：** 每个 callback 用 try-catch 包裹
- **状态：** `[ ]`

### E.3.4 🔶 市价单传 `price: 0` 可能不被后端接受

- **位置：** `web/src/pages/SpotTrading.tsx:314-316` / `ContractTrading.tsx:270-272`
- **问题：** 市价单将 `price: 0` 传给后端，很多交易所 API 市价单要求**不传 price 字段**或传 `null`
- **修复：** 市价单时 `price: undefined`
- **状态：** `[ ]`

### E.3.5 🔶 合约交易 — `positionMode` 声明但未使用

- **位置：** `web/src/pages/ContractTrading.tsx:85` / `:428-430`
- **问题：** `positionMode`（开仓/平仓模式）在 UI 中可切换，但下单逻辑中完全没有使用。用户点"平仓"后下单仍然是开仓
- **修复：** 在 `handlePlaceOrder` 中传入 `positionMode`，或移除该状态
- **状态：** `[ ]`

### E.3.6 🔶 合约交易 — `marginMode` 未传入订单

- **位置：** `web/src/pages/ContractTrading.tsx:84` / `:270-277`
- **问题：** 用户可选择"全仓"或"逐仓"，但下单时 `marginMode` 未传入 `orderApi.place`
- **修复：** 在下单参数中加入 `marginMode`
- **状态：** `[ ]`

### E.3.7 🔶 KlineChart — 时间戳判断远期问题

- **位置：** `web/src/components/charts/KlineChart.tsx:257-258`
- **问题：** `rawTs > 1e10` 判断秒/毫秒，在 2030 年后可能出错
- **修复：** 使用更精确的判断逻辑或记录数据源时间戳单位
- **状态：** `[ ]`

---

## E.4 低级别 (Low) — 建议优化

### E.4.1 🔹 Trading.tsx — mode 参数未验证

- **位置：** `web/src/pages/Trading.tsx:7-13`
- **问题：** `mode === 'contract'` 之外所有值（包括非法值）都渲染 SpotTrading
- **修复：** 验证 mode 值，非法值默认 spot
- **状态：** `[ ]`

### E.4.2 🔹 大量重复代码

- **位置：** `SpotTrading.tsx` vs `ContractTrading.tsx`
- **问题：** 两个文件有大量重复的辅助函数、K线图表初始化逻辑、WebSocket 处理等
- **修复：** 提取公共 hooks 和工具函数
- **状态：** `[ ]`

### E.4.3 🔹 类型安全 — 过多使用 `any`

- **位置：** 整个交易页面代码
- **问题：** `orderbook`、`orders`、`positions` 等数据大量使用 `any` 类型
- **修复：** 定义具体 TypeScript 接口
- **状态：** `[ ]`

### E.4.4 🔹 useToast — 全局状态在严格模式下可能重复

- **位置：** `web/src/lib/useToast.tsx:11-13`
- **问题：** `listeners` 和 `toasts` 是模块级全局变量，React StrictMode 下组件双重挂载/卸载可能导致 listener 管理问题
- **修复：** 使用 React Context 或更健壮的 listener 管理
- **状态：** `[ ]`

### E.4.5 🔹 API Interceptor — unwrap 逻辑过于宽松

- **位置：** `web/src/lib/api.ts:75-77`
- **问题：** 只要响应对象有 `data` 字段且没有 `success` 字段就 unwrap。会错误处理如 `{ data: [1,2,3], meta: {...} }` 的响应，丢失 `meta` 信息
- **修复：** 增加更严格的 unwrap 条件（检查响应结构）
- **状态：** `[ ]`

---

## E.5 修复进度追踪

| # | 问题 | 级别 | 文件 | 状态 |
|---|------|:----:|------|:----:|
| 1 | 切换交易对后 liveTrades 未清空 | Critical | `SpotTrading.tsx`, `ContractTrading.tsx` | `[x]` |
| 2 | K线新Bar OHLC初始化错误 | Critical | `klineDatafeed.ts` | `[x]` |
| 3 | API信封格式导致数据丢失 | Critical | `api.ts` | `[x]` |
| 4 | QuickTradePanel 状态竞态条件 | Critical | `QuickTradePanel.tsx` | `[x]` |
| 5 | 合约交易缺少交易对切换 | High | `ContractTrading.tsx` | `[ ]` |
| 6 | K线 Symbol 前缀匹配串扰 | High | `klineDatafeed.ts` | `[ ]` |
| 7 | 卖出时百分比按钮计算错误 | High | `SpotTrading.tsx` | `[ ]` |
| 8 | useToast import 位置错误 | High | `useToast.tsx` | `[ ]` |
| 9 | QuickTradePanel 硬编码 mock | Medium | `QuickTradePanel.tsx` | `[ ]` |
| 10 | useWebSocket stale onReconnect | Medium | `useWebSocket.ts` | `[ ]` |
| 11 | WebSocket callback 异常阻断 | Medium | `useWebSocket.ts` | `[ ]` |
| 12 | 市价单传 price:0 | Medium | `SpotTrading.tsx`, `ContractTrading.tsx` | `[ ]` |
| 13 | positionMode 声明未使用 | Medium | `ContractTrading.tsx` | `[ ]` |
| 14 | marginMode 未传入订单 | Medium | `ContractTrading.tsx` | `[ ]` |
| 15 | KlineChart 时间戳判断远期问题 | Medium | `KlineChart.tsx` | `[ ]` |
| 16 | Trading.tsx mode 未验证 | Low | `Trading.tsx` | `[ ]` |
| 17 | SpotTrading/ContractTrading 重复代码 | Low | `SpotTrading.tsx`, `ContractTrading.tsx` | `[ ]` |
| 18 | 过多使用 any 类型 | Low | 交易页面多处 | `[ ]` |
| 19 | useToast 全局状态严格模式问题 | Low | `useToast.tsx` | `[ ]` |
| 20 | API unwrap 逻辑过于宽松 | Low | `api.ts` | `[ ]` |

---

> **总结：** XiaoTianQuant 在架构 (Go+Rust+TS) 和前端体验上有显著优势，核心差距在于 Freqtrade 多年积累的**策略生态深度** (Hyperopt/FreqAI/Pairlist) 和 QuantDinger 的**全资产覆盖** (传统券商)。通过 5 个阶段、约 40-50 周的迭代，可以达到并部分超越两个对标项目的功能完整度，同时保持架构优势。
