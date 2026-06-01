# REST API 参考

> Base URL: `http://localhost:8080/api`

## 认证 `/api/auth`

| 方法 | 路径 | 认证 | 说明 |
|------|------|:----:|------|
| POST | `/auth/login` | - | 用户登录 |
| POST | `/auth/register` | - | 用户注册 |
| POST | `/auth/send-code` | - | 发送验证码 |
| POST | `/auth/login-code` | - | 验证码登录 |
| POST | `/auth/reset-password` | - | 重置密码 |
| GET | `/auth/me` | JWT | 当前用户信息 |
| GET | `/auth/refresh` | JWT | 刷新令牌 |

## 行情 `/api`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/klines/:symbol` | K 线数据 (params: interval, limit, from, to) |
| GET | `/market/klines` | 市场 K 线 (params: symbol, interval, limit, from, to) |
| GET | `/market/orderbook` | 订单簿深度 (params: symbol, depth) |
| GET | `/market/trades` | 最近成交 (params: symbol, limit) |
| GET | `/market/snapshot` | 市场快照 (params: symbol) |
| GET | `/symbols/search` | 币种搜索 (params: q) |

## 交易 `/api`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/orders` | 挂单列表 |
| POST | `/orders` | 下单 (body: symbol, side, type, price, quantity) |
| DELETE | `/orders/:id` | 撤单 |
| POST | `/orders/cancel-all` | 全部撤单 |
| GET | `/orders/history` | 成交历史 |
| GET | `/account/balance` | 账户余额 |

## 资产 `/api`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/portfolio/summary` | 资产摘要 |
| GET | `/portfolio/positions` | 持仓明细 |
| GET | `/portfolio/snapshots` | 资产快照历史 |

## 策略 `/api/strategies`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/strategies/configs` | 策略列表 |
| POST | `/strategies/configs` | 创建策略 |
| PUT | `/strategies/configs/:id` | 更新策略 |
| DELETE | `/strategies/configs/:id` | 删除策略 |
| POST | `/strategies/configs/:id/start` | 启动策略 |
| POST | `/strategies/configs/:id/stop` | 停止策略 |
| POST | `/strategies/configs/batch-start` | 批量启动 |
| GET | `/strategies/templates` | 策略模板 |

## 回测 `/api`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/backtest/run` | 运行回测 (body: symbol, interval, from, to, strategy_type, initial_balance) |
| POST | `/native/backtest` | 原生回测 |

## AI `/api`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/ai/generate` | AI 策略生成 |
| POST | `/ai/backtest` | AI 回测分析 |
| POST | `/ai/chat` | AI 对话 |
| POST | `/ai/analyze` | AI 市场分析 |
| GET | `/ai/quickscan` | 快速扫描 |
| POST | `/ai/multi-agent` | 多 Agent 分析 |

## 数据管理 `/api/data`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/data/download` | 异步下载历史数据 |
| GET | `/data/download/:jobId` | 下载进度查询 |
| GET | `/data/coverage` | 数据覆盖范围 |
| GET | `/data/load` | 加载本地数据 |
| GET | `/data/validate` | 数据质量校验 |
| DELETE | `/data/prune` | 清理旧数据 |
| GET | `/data/symbols` | 已存储交易对 |
| GET | `/data/intervals` | 可用周期 |

## 通知 `/api/notifications`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/notifications` | 通知列表 (params: limit, offset, unread) |
| GET | `/notifications/unread-count` | 未读计数 |
| POST | `/notifications/:id/read` | 标记已读 |
| POST | `/notifications/read-all` | 全部已读 |
| DELETE | `/notifications` | 清除全部 |

## 策略社区 `/api/community`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/community/strategies` | 策略市场列表 |
| GET | `/community/strategies/leaderboard` | 排行榜 |
| GET | `/community/strategies/:id` | 策略详情 + 评论 |
| POST | `/community/strategies/publish` | 发布策略 |
| POST | `/community/strategies/:id/comment` | 添加评论 |
| POST | `/community/strategies/:id/rate` | 评分 (1-5) |

## Admin `/api/admin`

| 方法 | 路径 | 权限 | 说明 |
|------|------|:----:|------|
| GET | `/admin/stats` | Admin | 系统统计 (用户/系统/交易) |
| GET | `/admin/summary` | Admin | 仪表盘摘要 |
| GET | `/admin/audit-log` | Admin | 审计日志 |
| GET | `/admin/activity` | Admin | 最近活动 |
| GET | `/admin/users` | Admin | 用户列表 |
| GET | `/admin/users/:id` | Admin | 用户详情 |
| PUT | `/admin/users/:id` | Admin | 更新用户 |
| POST | `/admin/users/:id/disable` | Admin | 禁用用户 |
| POST | `/admin/users/:id/enable` | Admin | 启用用户 |

## WebSocket

| 路径 | 说明 |
|------|------|
| `/ws` | 实时行情推送 (price, orderbook, trades, status) |

### WebSocket 消息格式

```json
// price
{"type":"price","symbol":"BTCUSDT","data":{"last":68000,"high":68500,"low":67500,"volume":1234}}

// orderbook
{"type":"orderbook","data":{"symbol":"BTCUSDT","bids":[[67990,0.5]],"asks":[[68010,0.3]]}}

// trades
{"type":"trades","data":[{"symbol":"BTCUSDT","side":"BUY","price":68000,"qty":0.1,"time":123}]}

// status
{"type":"status","data":{"portfolio":{"total_equity":100000},"risk":{"current_drawdown_pct":2.5}}}
```
