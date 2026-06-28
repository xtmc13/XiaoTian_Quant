# XiaoTian_Quant 前端问题与重构方案  briefing

> 本文档汇总 2026-06-28 对 XiaoTian_Quant v3.0 前端的全面检查结果
> 供后续开发 agent 参考执行

---

## 一、前端缺失功能（47项，按严重程度）

### 严重缺失（12项）— 竞品标配

| # | 缺失功能 | 来源竞品 | 当前状态 | 说明 |
|---|---------|---------|---------|------|
| 1 | **图表配置器 (Plot Configurator)** | Freqtrade | 无 | UI拖拽配置K线指标，不需写代码 |
| 2 | **Favicon交易指示器** | Freqtrade | 无 | 浏览器标签页显示持仓数量 |
| 3 | **快速交易面板** | QuantDinger | 无 | AI分析页一键下单，不需跳转 |
| 4 | **策略源码查看器** | Freqtrade | 无 | UI查看内置策略完整源码 |
| 5 | **策略偏差监控** | QuantDinger | 无 | 监控策略实际执行vs预期信号的偏差 |
| 6 | **入场/出场标签混合分析** | Freqtrade | 无 | 按入场×出场二维矩阵分析 |
| 7 | **交易对锁定管理** | Freqtrade | 无 | 手动锁定特定交易对阻止交易 |
| 8 | **Bot控制面板** | Freqtrade | 无 | 全局启动/停止/暂停按钮 |
| 9 | **数据下载管理器** | Freqtrade | 无 | UI下载历史K线数据 |
| 10 | **实时日志查看器** | Freqtrade | 无 | 前端实时查看Bot运行日志 |
| 11 | **多Bot管理** | Freqtrade | 无 | 同时监控多个Bot实例 |
| 12 | **信号交易机器人完整UI** | CryptoRobotics | 部分 | 信号+机器人执行闭环不完整 |

### 重要缺失（18项）— 竞争力差距

| 缺失功能 | 来源 | 说明 |
|---------|------|------|
| 周度盈亏统计 | Freqtrade | 只有日度和月度，缺周度 |
| 历史钱包余额图表 | Freqtrade | 不显示随时间的余额变化 |
| AI Copilot多轮对话 | QuantDinger | 当前只有单次AI生成，不能连续追问 |
| AI分析历史记录 | QuantDinger | 分析结果不保存 |
| 搜索引擎集成 | QuantDinger | AI分析时不能联网搜索 |
| 积分计费系统 | QuantDinger | AI功能无计费机制 |
| Turnstile人机验证 | QuantDinger | 无防机器人验证 |
| MFA/TOTP双因素认证 | QuantDinger | 安全缺失 |
| OAuth第三方登录 | QuantDinger | 只有用户名密码 |
| 策略执行模式切换 | QuantDinger | 缺signal/paper/live三种模式 |
| 持仓与交易所对账 | QuantDinger | 不对比策略持仓vs实际持仓 |
| 交易机会雷达 | QuantDinger | 无主动扫描交易机会功能 |
| 指标翻译功能 | QuantDinger | 不支持Python→PineScript/MQL翻译 |
| 策略评价/评分系统 | QuantDinger | 用户无法对策略评价 |
| USDT多链支付 | QuantDinger | 只有基础支付，缺链选择 |
| 移动端原生应用 | QuantDinger | 只有PWA |
| Demo模式切换 | CryptoRobotics | 无显眼的模拟/实盘切换 |
| 品牌白标配置 | QuantDinger | Logo/主题不可自定义 |

### 建议增强（17项）— 长期规划

开场标签统计、出场原因统计、最佳/最差交易日、利润因子、自定义数据查看、回测结果比较、回测市场变化分析、回测钱包历史、FreqAI模型管理UI、后台任务管理、通知未读计数实时显示、策略快照管理、网格策略双仓快照、收益率曲线、Put/Call比率、信号机器人发布管理

---

## 二、FreqAI ML 差距分析

### XiaoTian 现有 ML
- LightGBM / XGBoost 回归+分类
- 基础特征工程（~50特征）
- 模型训练/预测/部署

### FreqAI 具备但 XiaoTian 缺失的

| 维度 | FreqAI | XiaoTian | 差距 |
|------|--------|---------|------|
| **模型种类** | 11种（含PyTorch/RandomForest） | 2种 | 缺9种 |
| **特征自动扩展** | 自动展开10k+特征 | ~50手动特征 | 巨大 |
| **多时间框架** | include_timeframes | 单周期 | 核心缺失 |
| **偏移蜡烛** | include_shifted_candles | 无 | 核心缺失 |
| **相关交易对特征** | include_corr_pairs | 无 | 核心缺失 |
| **离群值检测** | DBSCAN/DI | 无 | 数据清洗 |
| **PCA降维** | 自动 | 无 | 数据清洗 |
| **滑动窗口训练** | 自动滑动 | 固定训练集 | 训练机制 |
| **回测集成** | 回测时自动训练+预测 | 无 | 联动缺失 |
| **持续学习** | continual_learning | 无 | 模型更新 |
| **自动重训练** | live_retrain_hours | 无 | 实盘关键 |
| **模型过期控制** | expiration_hours | 无 | 模型管理 |
| **MultiTarget输出** | 同时预测多目标 | 单目标 | 能力缺失 |

### 结论
XiaoTian 现有 ML 是**基础版**，离 FreqAI 差距大。前端已拆分出独立 `/ai/freqai` 页面，后端需大幅扩展。

---

## 三、前端架构问题（10个坑）

### 严重

**坑1：ML/AI 硬挤一个页面，布尔值切换**
```tsx
const [mlMode, setMlMode] = useState(false)
```
AI分析（LLM）和ML预测是两个完全不相关的功能，用一个布尔值切换，刷新状态全丢。

**坑2：Tab套Tab嵌套地狱**
```tsx
// MLPanel 5个Tab
// 其中 RL Tab 里面又有 3个Tab
```

**坑3：596行文件，20+个状态**
AI/index.tsx 管理了指数/热力图/日历/自选股/LLM/ML/RL/TensorBoard 全部状态。

**坑4：现货/合约70%重复代码**
ContractTrading.tsx (1470行) 和 SpotTrading.tsx (978行) 大量重复。

**坑5：22个空 catch 块**
```tsx
catch { /* API may not exist */ }
catch { /* ignore */ }
```
错误全部静默吞掉，API挂了用户不知道。

### 中等

**坑6：Dashboard.tsx 1329行内联7个子组件**
```tsx
function RiskControlCard() {}
function MLStatusCard() {}
// ... 7个内联组件
```

**坑7：68个布尔 loading 状态**
每个页面手动管理 loading，应该用 React Query 的 isLoading。

**坑8：75个 useEffect 散落**
数据获取逻辑混乱，有的用 Query 有的手写。

**坑9：13处 localStorage 直接操作**
硬编码 key、无过期、无版本控制。

**坑10：API单文件800+行**
32个 API 模块在一个文件里。

---

## 四、页面拆分重构（已执行）

### 问题总结
5个页面把17个独立功能硬塞在一起，用Tab或查询参数切换：

| 问题页面 | 原嵌套功能数 | 切换方式 |
|---------|:----------:|---------|
| Strategy.tsx | 5 | Tab |
| AI/index.tsx | 5（指数/热力图/LLM/ML/RL+TensorBoard） | 布尔值+Tab |
| Bots.tsx | 3（策略/信号/AI机器人） | botType状态 |
| Trading.tsx | 2（现货/合约） | if/else空壳 |
| ArbitrageMonitor.tsx | 2（跨所/三角） | Tab |

### 拆分结果

**13个新页面文件已创建：**

| 新文件 | 路由 | 功能 |
|--------|------|------|
| Market.tsx | /market | 市场数据（从AI拆出） |
| AIAnalysis.tsx | /ai/analysis | LLM AI分析（从AI拆出） |
| FreqAI.tsx | /ai/freqai | ML机器学习（从MLPanel拆出） |
| RLTraining.tsx | /ai/rl | RL强化学习（从MLPanel拆出） |
| TensorBoard.tsx | /ai/tensorboard | TensorBoard（从MLPanel拆出） |
| StrategyEditor.tsx | /strategy/editor | 代码编辑器（从Strategy拆出） |
| BotsStrategy.tsx | /bots/strategy | 策略机器人（从Bots拆出） |
| BotsSignal.tsx | /bots/signal | 信号机器人（从Bots拆出） |
| BotsAI.tsx | /bots/ai | AI机器人（从AIBots合并） |
| TradingSpot.tsx | /trading/spot | 现货交易（从Trading拆出） |
| TradingContract.tsx | /trading/contract | 合约交易（从Trading拆出） |
| ArbitrageCross.tsx | /arbitrage/cross | 跨所套利（从Arbitrage拆出） |
| ArbitrageTriangular.tsx | /arbitrage/triangular | 三角套利（从Arbitrage拆出） |

**3个核心文件已重写：**

| 文件 | 改动 |
|------|------|
| App.tsx | 路由从27→37个，增加6个旧路由重定向 |
| Sidebar.tsx | 导航扁平化，所有子项用独立路径 |
| TopBar.tsx | 标题映射更新，删除查询参数判断 |
| pageLoaders.ts | 预加载映射完全重写 |

### 旧路由重定向
```
/trading   → /trading/spot
/ai        → /ai/analysis
/bots      → /bots/strategy
/ai-bots   → /bots/ai
/arbitrage → /arbitrage/cross
```

### 新侧边栏结构

```
仪表盘              /dashboard
市场数据            /market

交易                [组]
├── 现货交易        /trading/spot
└── 合约交易        /trading/contract

策略实验室          [组]
├── 策略管理        /strategy
├── 策略编辑器      /strategy/editor
├── 指标 IDE        /indicator-ide
├── 指标市场        /indicator-community
├── 回测            /backtest
└── 排行榜          /strategy-leaderboard

AI 研究             [组]
├── AI 分析         /ai/analysis
├── FreqAI          /ai/freqai
├── RL 强化学习     /ai/rl
└── TensorBoard     /ai/tensorboard

机器人中心          [组]
├── 策略机器人      /bots/strategy
├── 信号机器人      /bots/signal
└── AI 机器人       /bots/ai

套利                [组]
├── 跨所套利        /arbitrage/cross
└── 三角套利        /arbitrage/triangular

...其余不变
```

---

## 五、XiaoTian 已具备的优势（保持）

| 优势 | 说明 |
|------|------|
| Rust撮合引擎 (30-50K TPS) | QuantDinger和Freqtrade都没有 |
| 链上数据分析 (ETH/BTC) | 竞品缺失 |
| 三角套利 | 同交易所三角套利路径发现 |
| RL强化学习 | DQN/PPO + TensorBoard可视化 |
| 14种内置策略 | 策略丰富度领先 |
| 高级订单 (OCO/Bracket/冰山) | 订单类型丰富 |
| 风控模板系统 (6种) | 全局+交易对级别 |
| Hyperopt参数优化UI | 超参数搜索管理 |
| PWA完整支持 | SW+安装提示+更新检测 |
| AI多智能体投票 | 多LLM共识+置信度校准 |

---

## 六、待办清单（按优先级）

### P0 — 立即
- [ ] 实时日志查看器（前端+后端websocket推送日志）
- [ ] Bot控制面板（Dashboard全局启停按钮）
- [ ] Favicon交易指示器（Service Worker更新badge）
- [ ] 空catch块修复（加Toast错误提示，不要静默吞）

### P1 — 短期（1-2周）
- [ ] 图表配置器（UI拖拽配置K线指标）
- [ ] 策略源码查看器（策略详情页加源码Tab）
- [ ] 数据下载管理器（新增页面）
- [ ] 交易对锁定管理（Pairlist页加锁定功能）
- [ ] 周度盈亏+历史钱包余额（新统计视图）
- [ ] AI Copilot多轮对话（替换单次分析）

### P2 — 中期（2-4周）
- [ ] FreqAI后端扩展（+8种模型/特征工程/滑动窗口）
- [ ] FreqAI前端UI（模型选择/特征配置/训练进度）
- [ ] 多Bot管理（支持多实例监控）
- [ ] AI分析历史+搜索集成
- [ ] 策略偏差监控
- [ ] OAuth+MFA安全增强

### P3 — 长期
- [ ] 信号交易机器人完整UI
- [ ] 积分计费系统
- [ ] 移动端应用（Capacitor）
- [ ] 回测集成FreqAI
- [ ] 自动重训练/模型过期

---

*文档生成时间: 2026-06-28*
*项目: XiaoTian_Quant v3.0 前端重构*
*参考: QuantDinger v4.0.1 / Freqtrade / CryptoRobotics / CoinrichAi*
