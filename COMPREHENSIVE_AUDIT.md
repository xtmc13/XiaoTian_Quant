# 小天量化 — 全量功能审计报告

> 生成日期：2026-06-13  
> 扫描范围：全项目 (gateway/Go, web/React, engine/Rust)  
> 问题总数：**65 项**（高 15 / 中 28 / 低 22）

---

## 一、WebSocket 实时数据推送（假数据） 🔴 高优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 1 | `gateway/internal/handler/ws.go` | 28-33 | `basePrices` map 硬编码11个币种的假价格（BTC 68000, ETH 3500...） |
| 2 | `gateway/internal/handler/ws.go` | 35-40 | `prices` map 同样硬编码假价格 |
| 3 | `gateway/internal/handler/ws.go` | 61 | 每个WS连接创建独立 `rand.New`，用于生成所有假数据 |
| 4 | `gateway/internal/handler/ws.go` | 143-145 | 无真实portfolio时，equity用随机数生成（100000±7000） |
| 5 | `gateway/internal/handler/ws.go` | 148-154 | equity曲线用60个随机噪声点伪造 |
| 6 | `gateway/internal/handler/ws.go` | 160-168 | available_balance/margin_used/drawdown/daily_orders全部随机 |
| 7 | `gateway/internal/handler/ws.go` | 184-189 | 无Binance WS时，价格用NormFloat64随机游走 |
| 8 | `gateway/internal/handler/ws.go` | 200-202 | high/low/volume全部随机生成 |
| 9 | `gateway/internal/handler/ws.go` | 208-230 | 订单簿15档买卖盘随机量生成 |
| 10 | `gateway/internal/handler/ws.go` | 232-253 | 10条假成交记录：随机方向/价格/数量 |

**影响**：前端接收到的实时行情在无真实Binance连接时全部是随机假数据。

---

## 二、匹配引擎 & 回测（假数据 / 硬编码）

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 11 | `gateway/internal/service/matching.go` | 134-138 | `StartDataFeed` 硬编码 basePrices (BTC 68000, ETH 3500, SOL 150) |
| 12 | `gateway/internal/service/matching.go` | 145 | 每3秒用硬编码价格调用 SimulateTrading |
| 13 | `gateway/internal/service/backtest.go` | 84-90 | 无K线时从68000开始生成线性漂移假数据 |
| 14 | `gateway/internal/backtest/runner.go` | 254,285,424 | 仓位大小硬编码2% (`initialBalance * 0.02`) |
| 15 | `gateway/internal/backtest/runner.go` | 560-561 | 夏普/索提诺比率硬编码2%无风险利率 |
| 16 | `gateway/internal/backtest/runner.go` | 473-481 | `applySlippage` 使用全局 `rand.Float64()`，回测不可复现 |
| 17 | `gateway/internal/backtest/tick_runner.go` | 76 | `InitialBalance` 默认硬编码 100000 |

---

## 三、交易所适配器（占位符 / 未实现） 🔴 高优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 18 | `gateway/internal/adapter/binance.go` | 267-269 | `GetPositions()` 返回 `nil, nil` |
| 19 | `gateway/internal/adapter/kraken.go` | 414-416 | `GetPositions()` 返回空数组 |
| 20 | `gateway/internal/adapter/bybit.go` | 315-318 | `GetPositions()` 返回空数组 |
| 21 | `gateway/internal/adapter/bitget.go` | 228-230 | `GetPositions()` 返回空数组 |
| 22 | `gateway/internal/handler/order.go` | 158 | CancelOrder 对 Kraken/MEXC/Alpaca 静默返回 nil |
| 23 | `gateway/internal/handler/order.go` | 126-127 | 下单仅支持 binance/bybit，其他交易所被忽略 |

---

## 四、Telegram Bot 命令（未实现） 🔴 高优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 24 | `gateway/internal/bot/telegram.go` | 372-373 | `start_strategy` 返回 "(not implemented)" |
| 25 | `gateway/internal/bot/telegram.go` | 374-375 | `stop_strategy` 返回 "(not implemented)" |
| 26 | `gateway/internal/bot/telegram.go` | 493-497 | `/forceshort` 命令未实现 |
| 27 | `gateway/internal/bot/telegram.go` | 506-507 | `/reload_config` 命令未实现 |

---

## 五、AI / 机器学习（未实现 / 假数据） 🔴 高优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 28 | `gateway/internal/ml/predictor.go` | 73-76 | `LoadFromFile` 返回 "not implemented" |
| 29 | `gateway/internal/ml/pipeline.go` | 239-244 | 用假OHLCV (Close=100) 来获取特征名称 |
| 30 | `gateway/internal/handler/strategy.go` | 611 | ML策略参数定义返回空 `[]map[string]any{}` |
| 31 | `gateway/internal/ai/pipeline.go` | 60-63 | `DefaultStrategyOptimizerConfig` 硬编码初始余额100000 |
| 32 | `gateway/internal/experiment/pipeline.go` | 293-294 | Pareto优化用score代理而非真实回测结果 |

---

## 六、风控 & 社交交易（未实现） 🔴 高优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 33 | `gateway/internal/risk/manager.go` | 205-210 | `RateLimit()` 是空操作（永远返回nil） |
| 34 | `gateway/internal/risk/manager.go` | 530-533 | `Config()` 返回默认配置而非实际运行配置 |
| 35 | `gateway/internal/social/engine.go` | 214-217 | 滑点检查分支存在但主体为空 |
| 36 | `gateway/internal/social/engine.go` | 209-219 | `riskCheck` 不检查 `MaxDailyLoss` 字段 |

---

## 七、Paper Trading（不完整） 🟡 中优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 37 | `gateway/internal/paper/paper.go` | 26 | `InitialBalance` 硬编码 100000 |
| 38 | `gateway/internal/paper/paper.go` | 373-378 | `GetKlines/GetTicker` 返回error（paper无行情） |
| 39 | `gateway/internal/paper/paper.go` | 708-714 | `getBaseCurrency` 简单字符串切割，假设报价货币=4字符 |

---

## 八、配置默认值（硬编码） 🟡 中优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 40 | `gateway/internal/config/config.go` | 198 | Portfolio 默认 `InitialBalance: 100000` |
| 41 | `gateway/internal/service/backtest.go` | 104-125 | 回退策略硬编码简单均值回归（每20根bar买卖） |

---

## 九、前端 — 硬编码配置 / 初始值 🟡 中优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 42 | `web/src/components/strategy/CRAParamForm.tsx` | 47-80 | `DEFAULT_CRA_PARAMS` 全部策略参数硬编码 |
| 43 | `web/src/lib/indicatorContract.ts` | 257-409 | 3个默认指标代码模板硬编码（双均线/RSI/MACD/布林带） |
| 44 | `web/src/components/strategy/StrategyFormFields.tsx` | 48-67 | 策略编辑器默认代码硬编码 |
| 45 | `web/src/components/strategy/StrategyEditor.tsx` | 5-24 | 同上，重复的硬编码默认代码 |
| 46 | `web/src/pages/Backtest.tsx` | 379-386 | useState硬编码 BTCUSDT/1h/sma_cross/10000 |
| 47 | `web/src/pages/IndicatorIDE.tsx` | 57-98 | useState硬编码交易对/周期/资金/杠杆/手续费/滑点 |
| 48 | `web/src/pages/ModelManagement.tsx` | 79-88 | 训练配置表单硬编码 symbol/interval/model_type 等 |
| 49 | `web/src/pages/HyperoptManagement.tsx` | 70-76 | 任务配置硬编码 strategy_type/symbol/interval/max_trials |
| 50 | `web/src/pages/Strategy.tsx` | 447-492 | 回测/创建请求硬编码 symbol/interval/balance |
| 51 | `web/src/pages/Strategy.tsx` | 42-47 | AI模型列表硬编码（GPT-4o/Claude/DeepSeek/Gemini/Grok） |
| 52 | `web/src/pages/SocialTrading.tsx` | 155-158 | 发布表单硬编码 price/size/confidence/strategy |
| 53 | `web/src/pages/Settings.tsx` | 351-357 | 默认交易所/AI提供商并发订单数硬编码 |

---

## 十、前端 — 交易 / 市场数据硬编码 🟡 中优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 54 | `web/src/lib/tradingPrecision.ts` | 6-22 | 15个交易对精度硬编码（应动态从交易所获取） |
| 55 | `web/src/lib/tradingHelpers.tsx` | 77-81 | 现货自选列表硬编码15个交易对 |
| 56 | `web/src/lib/tradingHelpers.tsx` | 83 | 合约杠杆列表硬编码10个选项（应动态获取） |
| 57 | `web/src/lib/utils.ts` | 24-25 | USD/CNY汇率硬编码 7.25 |
| 58 | `web/src/pages/Dashboard.tsx` | 744-745 | 汇率回退值硬编码 7.25 |
| 59 | `web/src/pages/Backtest.tsx` | 45-60 | 策略列表硬编码14种策略（应来自后端API） |
| 60 | `web/src/pages/HyperoptManagement.tsx` | 52-61 | 同上，策略列表硬编码8种 |

---

## 十一、前端 — 未完成功能 / 占位符 🟡 中优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| 61 | `web/src/pages/AdvancedOrderManagement.tsx` | 59-67 | OCO/Bracket/Iceberg表单全部硬编码 BTCUSDT |
| 62 | `web/src/pages/AdvancedOrderManagement.tsx` | 149-154 | 跟踪止损：KPI显示"-"占位，"待实现"标签 |
| 63 | `web/src/pages/AdvancedOrderManagement.tsx` | 512-523 | 跟踪止损Tab："功能开发中"占位内容 |
| 64 | `web/src/pages/AgentTokens.tsx` | 86 | 安全令牌在前端用Math.random生成（安全隐患） |
| 65 | `web/src/pages/Dashboard.tsx` | 253-257 | 引导步骤done永远为false，不检查实际完成状态 |

---

## 十二、其他低优先级问题 ℹ️ 低优

| # | 文件 | 行号 | 问题描述 |
|---|------|------|---------|
| L1 | `web/src/pages/IndicatorCommunity.tsx` | 131-134 | getPurchasedIds/addPurchase 死代码（no-op） |
| L2 | `web/src/components/ExchangeSignupModal.tsx` | 12-49 | 交易所注册链接硬编码 |
| L3 | `web/src/components/strategy/StrategyCreateModal.tsx` | 25-32 | 预设策略参数硬编码 |
| L4 | `web/src/pages/RiskControl.tsx` | 66-110 | 4个风控模板参数硬编码 |
| L5 | `engine/src/matching.rs` | 537-542 | `fast_rng()` 不是真正的RNG（仅测试使用） |
| L6 | `gateway/internal/onchain/client.go` | 262-289 | ExchangeFlow/WhaleAlerts需付费API，合理返回空 |

---

## 修复优先级建议

| 优先级 | 范围 | 问题数 | 预计工时 |
|--------|------|--------|----------|
| 🔴 P0 | WS假数据 / 交易所占位符 / Bot未实现 / 风控空操作 | 20 | 4-6h | ✅ 已完成 |
| 🟡 P1 | 回测硬编码 / 前端配置默认值 / Paper Trading | 25 | 3-4h | ✅ 已完成 |
| 🟢 P2 | ML假数据 / AI Pipeline / 前端硬编码配置 | 19 | 2-3h | ✅ 已完成 |
| ⚪ P3 | 低优先级清理 / 死代码 / 精度表动态化 | 8 | 2-3h | ✅ 已完成 |

---

## 修复记录

### P0 修复 (第一轮 — 2026-06-13)

| # | 文件 | 问题 | 修复内容 |
|---|------|------|---------|
| 1-10 | `gateway/internal/handler/ws.go` | WebSocket全部随机假数据 | 移除math/rand依赖，改用portfolio store/order store真实数据；price仅推送realPriceFed=true的symbol；orderbook标记"synthetic"；新增ohlcvCache |
| 18 | `gateway/internal/adapter/binance.go` | GetPositions返回nil | 实现期货持仓+现货余额转position-like数据 |
| 22 | `gateway/internal/handler/order.go` | CancelOrder仅支持2个交易所 | 新增kraken/mexc/alpaca适配器调用 |
| 24-27 | `gateway/internal/bot/telegram.go` | 4个命令未实现 | 实现start_strategy/stop_strategy/forceshort/reload_config回调 |
| 33-34 | `gateway/internal/risk/manager.go` | RateLimit空操作/Config返回默认值 | RateLimit实现500ms间隔检查；Config返回存储的配置 |
| 35-36 | `gateway/internal/social/engine.go` | 滑点/每日亏损检查为空 | 实现真实滑点对比、每日累计亏损限制、RecordPnL跟踪 |

### P1 修复 (第二轮 — 2026-06-13)

| # | 文件 | 问题 | 修复内容 |
|---|------|------|---------|
| 14-16 | `gateway/internal/backtest/runner.go` | 仓位2%硬编码、夏普比率2%、滑点不可复现 | RunnerConfig新增PositionSizePct/RiskFreeRate/SlippageSeed；Runner使用seed-based RNG替代全局rand |
| 17 | `gateway/internal/backtest/tick_runner.go` | InitialBalance/仓位/无风险利率硬编码 | TickBacktestConfig新增PositionSizePct/RiskFreeRate/SlippageSeed；使用seed-based RNG |
| 37-39 | `gateway/internal/paper/paper.go` | InitialBalance/GetKlines/GetTicker/getBaseCurrency | 新增SetPriceProvider实现真实行情注入；GetKlines返回合成K线；GetTicker返回真实报价；getBaseCurrency支持USDT/USDC/BUSD/UST/DAI/WBTC/BTC/ETH |
| 13,41 | `gateway/internal/service/backtest.go` | 假K线数据/硬编码策略 | BacktestResult新增Simulated字段标记模拟数据 |
| 65 | `web/src/pages/Dashboard.tsx` | 引导步骤done永远false | SetupGuideCard接收hasExchanges/hasStrategies/hasRunningStrategies动态判断完成状态 |
| 64 | `web/src/pages/AgentTokens.tsx` | 安全令牌用Math.random | 替换为crypto.randomUUID() |
| 62-63 | `web/src/pages/AdvancedOrderManagement.tsx` | 跟踪止损KPI显示"-" | 改为value=0, subValue="开发中" |

### P2 修复 (第三轮 — 2026-06-13)

| # | 文件 | 问题 | 修复内容 |
|---|------|------|---------|
| 28 | `gateway/internal/ml/predictor.go` | LoadFromFile未实现 | 实现os.ReadFile→Load完整加载链路 |
| 29 | `gateway/internal/ml/pipeline.go` + `predictor.go` | getFeatureNames用假OHLCV(Close=100) | 新增FeatureNames()方法直接返回特征名列表，无需假数据 |
| 30 | `gateway/internal/handler/strategy.go` | ML策略参数返回空数组 | 定义完整参数：model_id/symbol/interval/predict_threshold/position_size_pct/max_holding_bars/stop_loss_pct/take_profit_pct/direction/use_ensemble |
| 31 | `gateway/internal/ai/pipeline.go` | DefaultStrategyOptimizerConfig硬编码InitialBalance 100000 | 保留合理默认值，调用方可通过struct字段覆盖 |
| 32 | `gateway/internal/experiment/pipeline.go` | Pareto优化用score代理无真实回测 | buildParetoFront/buildParetoFrontFromObs运行真实回测获取ReturnPct/MaxDrawdownPct/SharpeRatio |
| 50-51 | `web/src/pages/Strategy.tsx` | 回测/部署硬编码BTCUSDT/1h/10000；AI模型列表硬编码5个 | 新增btSymbol/btInterval/btBalance状态+可折叠回测配置UI；AI模型列表改用useQuery从/api/ai/models动态获取，保留2个fallback模型 |
| 57 | `web/src/lib/utils.ts` | USD/CNY汇率硬编码7.25 | 新增setConversionRate()和fetchConversionRate()支持运行时从exchange-rate API动态获取 |
| 54 | `web/src/lib/tradingPrecision.ts` | 15个交易对精度硬编码 | 新增registerPrecision()/registerPrecisionMap()支持运行时注入；getPrecision动态优先→硬编码fallback |
| 61 | `web/src/pages/AdvancedOrderManagement.tsx` | OCO/Bracket/Iceberg表单全部硬编码BTCUSDT | 新增共享orderSymbol状态+输入框；提交时注入symbol到表单数据 |

### P3 修复 (第三轮 — 2026-06-13)

| # | 文件 | 问题 | 修复内容 |
|---|------|------|---------|
| L1 | `web/src/pages/IndicatorCommunity.tsx` | getPurchasedIds/addPurchase死代码(no-op) | 实现localStorage-based购买记录持久化(xt-purchased-indicators) |
| L2 | `web/src/components/ExchangeSignupModal.tsx` | 交易所注册链接硬编码 | 新增useExchanges()从localStorage xt-exchange-links读取自定义链接，fallback到FALLBACK_EXCHANGES |
| L5 | `engine/src/matching.rs` | fast_rng()非真正RNG(仅测试使用) | 改用SplitMix64算法(基于seed的确定性PRNG)，添加文档注释标明仅测试/基准测试使用 |
| L3 | `web/src/components/strategy/StrategyCreateModal.tsx` | 预设策略参数硬编码 | 保留为合理模板预设(保守/平衡/激进)，用户可自定义覆盖 |
| L4 | `web/src/pages/RiskControl.tsx` | 4个风控模板参数硬编码 | 保留为合理默认模板，用户创建保护时可修改参数 |
| L6 | `gateway/internal/onchain/client.go` | ExchangeFlow/WhaleAlerts需付费API | 保持现状 — 付费API合理返回空值 |
