# XiaoTian Quant v3.0 — 精细化修复总纲

> 创建时间：2026-06-20
> 修复目标：从 Demo 级别提升到可运行的 Alpha 版本
> 策略：P0 → P1 → P2，先底层后上层

---

## 一、问题分级体系

| 级别 | 定义 | 修复标准 |
|------|------|---------|
| **P0** | 功能不可用 / 数据造假 | 必须有真实数据链路或明确错误提示 |
| **P1** | 代码质量问题 / 不可复现 | 消除硬编码、统一规范、可测试 |
| **P2** | 体验优化 / 边界情况 | 完善交互、减少重复代码 |

---

## 二、P0 修复清单（致命问题）

### 🔴 P0-1: WebSocket 实时数据全假
**影响**：用户看到的实时行情在无 Binance 连接时全是随机数
**文件**：`gateway/internal/handler/ws.go`
**修复方案**：
- [ ] 无真实交易所连接时，WebSocket 推送明确状态 `status: "simulated"`
- [ ] 订单簿/成交记录添加 `is_simulated` 标记
- [ ] 前端展示模拟数据时显示 ⚠️ 模拟数据提示

### 🔴 P0-2: AI 页面数据全硬编码
**影响**：AI 分析面板展示的数据 90% 是假的
**文件**：`web/src/pages/AI/index.tsx`
**修复方案**：
- [ ] 移除所有硬编码市场数据，初始状态设为空
- [ ] 接入真实行情 API（crypto 用 Binance，indices/stocks 用第三方或延迟显示）
- [ ] 数据加载中添加 loading/skeleton 状态
- [ ] 无数据时显示"暂无数据"而非假数据

### 🔴 P0-3: 交易所适配器大量未实现
**影响**：除 Binance/Bybit 外，其他交易所无法下单/查持仓
**文件**：`gateway/internal/adapter/*.go`
**修复方案**：
- [ ] Kraken/GetPositions 实现真实 API 调用
- [ ] Bitget/GetPositions 实现真实 API 调用
- [ ] MEXC/Alpaca CancelOrder 实现或返回明确错误
- [ ] 下单接口支持全部 8 个交易所

### 🔴 P0-4: ML/AI 模块空壳
**影响**：AI 策略、预测、优化全部不可用
**文件**：`gateway/internal/ml/*.go`, `gateway/internal/handler/ai.go`
**修复方案**：
- [ ] `LoadFromFile` 实现真实模型加载
- [ ] ML 策略参数返回真实配置
- [ ] AI Handler 接入已有 LLM Provider（需 API Key）
- [ ] 无 API Key 时返回明确提示而非占位符

### 🔴 P0-5: Telegram Bot 核心命令未实现
**影响**：Bot 无法启动/停止策略
**文件**：`gateway/internal/bot/telegram.go`
**修复方案**：
- [ ] `/start_strategy` 调用策略引擎启动
- [ ] `/stop_strategy` 调用策略引擎停止
- [ ] `/forceshort` 实现或移除命令
- [ ] `/reload_config` 实现配置热重载

---

## 三、P1 修复清单（代码质量）

### 🟡 P1-1: 回测不可复现
**文件**：`gateway/internal/backtest/runner.go`
**修复方案**：
- [ ] `applySlippage` 使用传入的随机种子
- [ ] 回测结果添加 `seed` 字段
- [ ] 相同参数 + 相同 seed = 相同结果

### 🟡 P1-2: 风控系统空转
**文件**：`gateway/internal/risk/manager.go`
**修复方案**：
- [ ] `RateLimit()` 实现真实频率限制
- [ ] `Config()` 返回运行时实际配置
- [ ] 添加风控规则缓存和动态更新

### 🟡 P1-3: Paper Trading 无行情
**文件**：`gateway/internal/paper/paper.go`
**修复方案**：
- [ ] Paper 模式接入交易所行情（只读）
- [ ] 初始余额从配置读取
- [ ] `getBaseCurrency` 用更可靠的方式解析交易对

### 🟡 P1-4: 社交交易空壳
**文件**：`gateway/internal/social/engine.go`
**修复方案**：
- [ ] 跟单逻辑实现（复制主账户订单）
- [ ] 滑点检查实现真实计算
- [ ] `MaxDailyLoss` 检查实现

### 🟡 P1-5: 前端重复代码
**文件**：`web/src/pages/SpotTrading.tsx`, `ContractTrading.tsx`, `Backtest.tsx`
**修复方案**：
- [ ] 提取公共交易表单组件
- [ ] 统一 `INTERVAL_OPTIONS` 使用
- [ ] 提取订单簿/深度图公共组件

---

## 四、P2 修复清单（体验优化）

### 🟢 P2-1: 前端交互完善
- [ ] IndicatorIDE "发布到社区"按钮实现
- [ ] IndicatorCommunity "使用"按钮实现
- [ ] StrategyMarket "创建策略"按钮实现
- [ ] SocialTrading "跟单"按钮实现

### 🟢 P2-2: 错误处理统一
- [ ] API 错误统一返回 `{code, msg, data}` 格式
- [ ] 前端统一处理 401/403/500 错误
- [ ] 网络断线自动重连提示

### 🟢 P2-3: 数据加载优化
- [ ] 添加全局 loading 状态管理
- [ ] 长任务（回测/超参优化）显示进度条
- [ ] 大数据表格添加虚拟滚动

---

## 五、修复路线图

```
Phase 1 (数据诚实) — 让系统说真话
  ├── P0-1: WebSocket 标记模拟数据
  ├── P0-2: AI 页面移除硬编码
  └── P0-3: 交易所适配器补齐

Phase 2 (核心功能) — 让基础功能可用
  ├── P0-4: ML/AI 模块接入真实逻辑
  ├── P0-5: Telegram Bot 核心命令
  └── P1-3: Paper Trading 接入行情

Phase 3 (质量保证) — 让系统可靠
  ├── P1-1: 回测可复现
  ├── P1-2: 风控真实运行
  └── P1-4: 社交交易实现

Phase 4 (体验打磨) — 让用户舒服
  ├── P2-1: 前端按钮全部有响应
  ├── P2-2: 错误处理统一
  └── P2-3: 加载状态优化
```

---

## 六、关键文件清单（按修复优先级排序）

| 优先级 | 文件 | 问题 | 预估工作量 |
|--------|------|------|-----------|
| 1 | `gateway/internal/handler/ws.go` | 假数据 | 2h |
| 2 | `web/src/pages/AI/index.tsx` | 硬编码 | 3h |
| 3 | `gateway/internal/adapter/*.go` | 未实现 | 4h |
| 4 | `gateway/internal/ml/*.go` | 空壳 | 3h |
| 5 | `gateway/internal/bot/telegram.go` | 未实现 | 2h |
| 6 | `gateway/internal/backtest/runner.go` | 不可复现 | 1h |
| 7 | `gateway/internal/risk/manager.go` | 空转 | 2h |
| 8 | `gateway/internal/paper/paper.go` | 无行情 | 2h |
| 9 | `web/src/pages/*Trading.tsx` | 重复代码 | 2h |
| 10 | `gateway/internal/social/engine.go` | 空壳 | 2h |

**总预估工作量：约 23 小时**
