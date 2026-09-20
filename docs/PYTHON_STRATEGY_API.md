# 用户 Python 策略契约化运行时 v1.1（PYTHON_STRATEGY_API）

对标 QuantDinger Strategy API V2 / freqtrade `IStrategy`：用户写一个约定契约的
Python 文件，平台托管 7×24 实盘执行。策略在**独立 python3 子进程沙箱**中长驻运行，
K 线经事件总线驱动 `on_bar`，`context.buy/sell` 动作转成信号走 OMS 统一执行层下单。

- 后端实现：`gateway/internal/pystrat/`（运行时 + runner）、`gateway/internal/handler/pystrat.go`（API）
- 存储：`xt_pystrategies`（迁移 0021 + v1.1 增量 0023）+ 版本快照复用 `xt_strategy_versions`（0014）
- 本文档是策略作者与平台之间的契约；v1 明确不做：visual 参数生成器、tick 级执行、多标的组合优化
- **v1.1（2026-09-20）三增强**：`on_order` 订单回报回调落地、`context.buy/sell(price=...)`
  限价单生效、`market='futures'` 合约杠杆执行（详见 §1.4/§1.5/§3）

## 1. 策略文件结构

一个策略 = 一个 Python 文件，必须包含：

1. 模块级 `STRATEGY_MANIFEST` 字典（契约声明）
2. `initialize(context)` 回调（启动时一次）
3. `on_bar(context, bar)` 回调（每根闭合 K 线）

```python
import math

STRATEGY_MANIFEST = {
    "name": "双均线 crossover",
    "symbol": "BTC/USDT",
    "interval": "15m",
    "direction": "long",          # long | short | both
    "params": {                   # 默认参数（平台保存时可覆盖）
        "fast": 9,
        "slow": 21,
    },
    "risk": {
        "max_position_pct": 0.5,  # 仓位名义价值 ≤ 权益的 50%（0=不限制）
        "stop_loss_pct": 0.03,    # 默认止损 3%
        "take_profit_pct": 0.1,   # 默认止盈 10%
    },
}

def initialize(context):
    context.fast_hist = []        # 跨回调状态挂在 context 属性上
    context.slow_hist = []
    context.log("initialized, symbol=%s" % context.symbol)

def on_bar(context, bar):
    context.fast_hist.append(bar["close"])
    context.slow_hist.append(bar["close"])
    if len(context.fast_hist) < context.params["slow"]:
        return
    fast_ma = sum(context.fast_hist[-context.params["fast"]:]) / context.params["fast"]
    slow_ma = sum(context.slow_hist[-context.params["slow"]:]) / context.params["slow"]
    has_pos = bool(context.position and context.position.get("qty", 0) > 0)
    if fast_ma > slow_ma and not has_pos:
        amount = context.equity * 0.1
        context.buy(amount=amount)
        context.set_stop_loss(0.03)
        context.set_take_profit(0.1)
    elif fast_ma < slow_ma and has_pos:
        context.close_position()
```

### 1.1 manifest 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `name` | 是 | 策略展示名 |
| `symbol` | 是 | 交易对，如 `BTC/USDT`（运行时规范为 `BTCUSDT`） |
| `interval` | 是 | K 线周期：`1m/3m/5m/15m/30m/1h/2h/4h/6h/8h/12h/1d` |
| `direction` | 是 | `long`（只做多/减仓卖出）｜`short`｜`both` |
| `params` | 否 | 默认参数字典；平台保存的 `params_json` 覆盖同名键 |
| `risk.max_position_pct` | 否 | 买入前硬校验：买后仓位名义价值 / 权益 ≤ 该值，范围 `(0,1]`，`0`=不限制（合约买入按**保证金口径**：名义价值/杠杆） |
| `risk.stop_loss_pct` | 否 | 默认止损 `(0,1)`；策略可用 `context.set_stop_loss` 覆盖 |
| `risk.take_profit_pct` | 否 | 默认止盈 `(0,1)`；`context.set_take_profit` 覆盖 |
| `risk.leverage` | 否 | v1.1：合约杠杆 `1-125`，`0`/缺省=不覆盖平台保存值（仅 `market='futures'` 有意义） |
| `risk.margin_mode` | 否 | v1.1：`cross`（默认全仓）\| `isolated`（逐仓），非空时覆盖平台保存值 |

### 1.2 生命周期回调

| 回调 | 时机 | 说明 |
|---|---|---|
| `initialize(context)` | 启动时一次（沙箱加载后） | 初始化跨回调状态；抛异常 → 启动失败 |
| `on_bar(context, bar)` | 每根**闭合** K 线 | 异常 → 本轮跳过并计数（见错误契约） |
| `on_order(context, order)` | 可选，v1.1 起触发 | **策略产生的订单**成交/状态变化时调用（签名必须 `on_order(context, order)`，见 §1.4） |

`bar` 是 dict：`time`（开盘时间 ms）/ `open` / `high` / `low` / `close` / `volume`。

**暖机**：启动时供给管回补的最近历史 K 线（早于启动时刻）照常喂给 `on_bar`
（指标状态可暖机），但**交易动作被丢弃**并记日志。

### 1.3 context API

| 成员 | 说明 |
|---|---|
| `context.symbol` / `context.interval` | 只读 |
| `context.params` | 只读（默认参数 + 平台覆盖合并结果） |
| `context.position` | 当前持仓 dict：`{"qty","avg_price","side"}`，无持仓为 `{}` |
| `context.equity` | 当前账户权益（USDT 口径） |
| `context.buy(qty=None, amount=None, price=None)` | 买入：`qty`（币数量）或 `amount`（计价币金额）至少一个。v1.1 起 `price>0` 挂**现货限价单**（见 §1.5）；缺省市价。`market='futures'` 时走合约链路（见 §3） |
| `context.sell(qty=None, amount=None, price=None)` | 卖出/减仓：`qty` 或 `amount` 至少一个；无持仓时空操作。`price>0` 挂现货限价卖单 |
| `context.log(msg)` | 记运行日志（API `GET /logs` 可见） |
| `context.set_stop_loss(pct)` | 要求 `0<pct<1`；作用于下一次买入成交的仓位 |
| `context.set_take_profit(pct)` | 同上 |
| `context.close_position()` | 市价平掉全部当前持仓；无持仓空操作 |

### 1.4 on_order 订单回报回调（v1.1）

平台按 `client_oid` 前缀 `pystrat:<策略id>` 识别策略自有订单：OMS 订单状态
变化（下单/部分成交/成交/撤单/拒绝）经事件总线 `ORDER_UPDATE` → runner 订阅
过滤 → 沙箱 `on_order(context, order)`。**只推策略自己产生的订单**，平台上其他
订单不会进入回调；策略未定义 `on_order` 则直接跳过（零开销）。

`order` 是 dict：

| 字段 | 说明 |
|---|---|
| `id` | 订单 ID |
| `symbol` | 交易对（如 `BTCUSDT`） |
| `side` | `buy` / `sell` |
| `type` | `market` / `limit` / ... |
| `qty` / `price` | 委托量 / 委托价 |
| `filled` / `avg_price` | 已成交量 / 成交均价 |
| `status` | `new` / `partially_filled` / `filled` / `cancelled` / `rejected` / ... |
| `pnl` | 该订单已实现盈亏（USDT） |

约束（与 `on_bar` 不同的容忍度语义，见 §4）：

- **不允许在 `on_order` 里下单**：回调内产生的 buy/sell 动作被丢弃并记 warning 日志。
- 适合用途：成交确认后的记账、限价单成交后补挂保护单的联动逻辑（平台对限价买入
  成交会自动补挂 manifest/set_* 声明的 TP/SL，无需策略重复挂）、撤单/拒单告警。
- 事件驱动与 `on_bar` 串行（同一沙箱同一时刻只跑一个回调），无需考虑并发。

### 1.5 price 语义（v1.1）：限价单

- `context.buy(amount=200, price=48000)` → 挂现货 LIMIT 买单（委托额 200U，
  委托价 48000）；`qty` 与 `amount` 的换算按**限价**折算。
- `context.sell(qty=0.5, price=52000)` → 挂现货 LIMIT 卖单。
- `price` 缺省或为 `None` → 保持 v1 市价行为。
- 未成交的限价委托**留在订单簿**，经现有订单查询/WS 通道对前端可见（无需新接口）；
  成交回报走 `on_order`（`status='filled'`），runner 在限价买入成交时按
  manifest/`set_*` 声明补挂 TP/SL。
- `market='futures'` 时 v1.1 合约单为市价执行，`price` 忽略并记日志（见 §3）。

跨回调状态：挂在 `context` 任意属性上（如 `context.fast_hist`）。沙箱因超时/崩溃
重建时，这些内存状态会丢失（重启恢复是已知边界，后续版本考虑持久化）。

## 2. 安全契约（禁止事项）

沙箱为**独立 python3 子进程**（`-I` 隔离模式，白名单 `__import__` + 受限
builtins + AST 静态校验 + 双下划线访问拒绝 + stdout 重定向）。策略代码禁止：

- **网络访问**（`socket`/`requests`/`urllib`/`http` 等一律不在白名单）
- **文件读写**（`open()` 属危险内建，静态拒绝）
- **import 白名单以外的任何模块**。白名单：`math` / `json` / `datetime` /
  `collections` / `heapq` / `itertools` / `functools` / `statistics`
  （任务书原文 "functypes" 为 `functools` 笔误，按标准库实际名称落地）
- **危险内建**：`eval` / `exec` / `compile` / `open` / `__import__` /
  `globals` / `vars` / `input` / `getattr`+双下划线 等
- **print**：不报错，输出被捕获进 `prints`（随响应返回，不进协议流）

静态校验（`POST /api/pystrategies/:id/validate`）在**不进沙箱**的情况下即可
报出 import/签名/manifest 错误（带行号）；沙箱内 Python AST 是终审。

## 3. 执行契约

- **执行通道**：`context.buy/sell` → runner 动作 → `OMSBotExecutor`
  （`client_oid` 前缀 `pystrat:<id>`）→ 现有 OMS 管线（RiskCheck →
  LockBalance → SubmitToExchange）。paper 即时成交回报；live 进单前过
  `canPlaceLiveOrder` 实盘总闸。订单状态变化经 OMS `OnOrderUpdate` 钩子
  发布事件总线 `ORDER_UPDATE`，runner 按前缀过滤后驱动 `on_order`（v1.1）。
- **模拟盘（paper=1）**：强制 `exchange=paper`，永不触真实交易所。
- **实盘（paper=0）**：启动前要求实盘闸已 unlock（`trading_safety` 运行时
  解锁 / `LIVE_TRADING_ENABLED=true` / `trading.live_enabled: true`），
  否则拒绝启动并返回 400。执行交易所为 `binance`（v1 固定）。
- **现货执行（默认 `market='spot'`）**：`buy`=现货买入（市价或 v1.1 限价）、
  `sell`/`close_position`=现货卖出；`direction=short` 时买入开仓被拒绝（记日志）。
- **合约执行（v1.1，`market='futures'` 且 paper=0）**：买入复用现有合约下单
  路径——`OMSBotExecutor.PlaceContract`（与 grid 合约腿同一实现）：
  `market_type=swap` + `position_side`（direction=short→`SHORT`，否则 `LONG`）
  + `leverage`/`margin_mode` 进 OMS 合约链路（LockBalance 按 名义价值/杠杆
  锁保证金，live 走交易所 `PlaceFuturesOrder`）。杠杆/保证金模式解析顺序：
  `manifest.risk.leverage/margin_mode`（非 0/非空）→ 平台保存的
  `xt_pystrategies.leverage/margin_mode` 列 → 默认 `1`/`cross`；上限 125。
  数量语义：`qty` 直接作基础币张数；`amount` 视为保证金，按
  `amount×杠杆/参考价` 换算张数（与策略合约 CRA 的 `entryQty` 同语义）。
  合约买入的 `max_position_pct` 风控按**保证金口径**（名义价值/杠杆占权益比）。
  **paper=1 时 paper 撮合不支持杠杆：回落现货市价买入并记日志**（v1.1 边界）。
  v1.1 合约卖出/`close_position` 仍按现货腿执行（期货平仓链路列入后续版本）。
- **限价单（v1.1）**：`price>0` 的 `buy/sell` 挂现货 LIMIT 单（见 §1.5）。
- **TP/SL 映射**：买入成交后，按 manifest.risk 与 `set_*` 覆盖计算
  `tp=entry×(1+tpPct)`、`sl=entry×(1−slPct)`，挂到现有条件单引擎
  （`ConditionalEngine.RegisterTPSL`，trigger 后市价平仓）。市价买单在
  execAction 内即挂；限价买单在 `on_order` 收到 `filled` 时补挂。未配置
  Protector 时记日志并跳过。
- **K 线来源**：`KlineFeeder`（Binance REST 轮询，引用计数）→ 事件总线
  `TypeBar`；同 symbol 多 interval 按 `bar.Interval` 过滤。

## 4. 错误契约

- 回调（`on_bar`）抛异常 → **本轮跳过并计数**；日志可见最近错误。
- **连续 10 次** → 策略自动置 `paused` + 告警（通知中心 WARN），退出运行。
- 单轮沙箱调用超时（**5s**）→ kill 子进程、自动重建沙箱重载策略，本轮跳过
  （不计错误）。模块内状态丢失。
- **`on_order` 容忍度语义（v1.1）**：单笔订单回报调用超时（同 5s）**不杀
  子进程**——事件记日志跳过（订单事件容忍度高，订单回报链路不得拖垮策略主循环）；
  回调抛异常同样只记日志、**不计入** 10 次错误契约；沙箱死亡时事件跳过，
  由下一根 K 线的既有重建路径恢复。
- 内存限制 **256MB**（worker 启动时 POSIX `RLIMIT_AS`；非 POSIX 环境降级
  为不限制——部署目标 Linux 生效）。
- 进程重启恢复：`status=active` 的策略由 `RetryResume` 每 60s 复查拉起。

## 5. 平台 API（/api/pystrategies，AuthRequired）

| 方法/路径 | 说明 |
|---|---|
| `GET /api/pystrategies` | 列表（属主过滤；附 `is_running`） |
| `POST /api/pystrategies` | 创建（`draft`）+ 版本快照 v1 |
| `GET /api/pystrategies/:id` | 详情 |
| `PUT /api/pystrategies/:id` | 更新（运行中 409）；改代码打新快照 |
| `DELETE /api/pystrategies/:id` | 删除（运行中先停） |
| `POST /api/pystrategies/:id/validate` | 静态校验（带行号）+ 沙箱加载校验 |
| `POST /api/pystrategies/:id/start` | 启动（paper=0 过实盘闸；失败原因落 `error` 列） |
| `POST /api/pystrategies/:id/stop` | 停止 |
| `GET /api/pystrategies/:id/logs` | 最近运行日志（内存环形缓冲，最新在前） |
| `GET /api/pystrategies/:id/status` | 落库状态 + 运行时细节（连续错误数/沙箱存活） |

状态机：`draft → active（start）→ paused（stop / 连续错误自动暂停）`，
`error` 列记录最近校验/运行错误。所有权：创建写 `user_id`，单资源操作
403 拦截非属主，列表按 `user_id` 过滤（admin 看全部）。

v1.1：创建/更新载荷新增 `market`（`spot`|`futures`）、`leverage`（1-125）、
`margin_mode`（`cross`|`isolated`）三个合约执行声明字段（0023 列），响应
同名字段回显；杠杆/保证金模式运行时以 `manifest.risk.leverage/margin_mode`
覆盖为优先（见 §3）。

## 6. v1.1 已知边界

- `on_order` 只推策略自有订单（`client_oid` 前缀 `pystrat:<id>`）且只在策略
  运行时接单；进程重启后到 `RetryResume` 拉起前的回报不补发。
- 限价单为现货腿；`market='futures'` 的合约单 v1.1 为市价执行（`price` 忽略），
  合约限价/合约卖出/平仓链路列入后续版本。
- `market='futures'` 且 paper=1 回落现货撮合（paper 撮合不支持杠杆），日志注明。
- 沙箱重建丢失模块内状态；`initialize` 会重跑。
- 多标的/组合优化/tick 级/visual 参数生成器：明确不在 v1 范围。
