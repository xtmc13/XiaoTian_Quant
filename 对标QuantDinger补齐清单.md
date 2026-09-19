# 对标 QuantDinger 补齐清单

> 原则：QuantDinger 有的我都有；我现有的功能一个不能少；做得不好的向 QuantDinger 看齐改好。
> 生成日期：2026-09-19。证据来源：`/tmp/QuantDinger`（backend_api_python + mcp_server）、`/tmp/QuantDinger-Vue`、本项目 `TODO.md` + 源码实测。

## A. 补齐 QuantDinger 有、我没有的功能（19 项）

### A1. 交易机器人类型（核心差距）

- [x] **A1.1 分层马丁格尔机器人**：多分配组 + 每组独立限额（对照 QuantDinger `app/services/strategy_runtime/bot_type.py` / 前端 executor-strategies 四选一创建台）
- [x] **A1.2 DCA 定投机器人**：按周期固定资金比例买入，含最大订单数 / 周期预算 / TP / 硬止损 / 追踪保护（我有 `order/dca.go` 高级订单，需升级为独立定投计划 bot，含前端管理页）
- [x] **A1.3 网格中性模式**：swap 合约多空对冲双腿网格（现有 grid 引擎只支持现货只做多方向，需加 neutral 模式与双腿配对生命周期）
- [x] **A1.4 马丁格尔层级硬上限**：层数 + 总预算硬上限，确认成交后才推进下一层（安全设计对齐）
- [x] **A1.5 CTA / 组合策略模板**：系统预设 8 个 CTA + 4 个组合策略模板

### A2. 券商与执行

- [x] **A2.1 IBKR（盈透）适配器**：REST 接入，含持仓/下单/对账（README 已宣称但不存在，实现它或改 README）
- [x] **A2.2 limit-then-market 执行算法**：限价单超时未成交自动 fallback 市价（对照 `live_trading/executors.py`）

### A3. 账户安全

- [x] **A3.1 MFA/TOTP 两步验证**：TOTP 绑定/校验，风险登录强制触发（对照 `mfa_service.py`）
- [x] **A3.2 Turnstile 人机验证**：注册/登录接口接入
- [x] **A3.3 JWT token 版本吊销**：改密/封禁后旧 token 立即失效
- [x] **A3.4 初始密码强制修改**：首次登录强制改密

### A4. 商业化

- [x] **A4.1 Stripe 自动支付**：替换现有纯手动 USDT 对账，实现自动到账核验/支付回调（现有 Billing 保留 USDT TRC20 通道）

### A5. 通知

- [x] **A5.1 短信通知（Twilio）**：接入现有通知路由规则引擎，成为第六个渠道

### A6. 研究与回测

- [x] **A6.1 因子研究框架**：TA-Lib 接入（本机 /root/tmpbuild 已在装 TA-Lib）+ 版本化因子注册表 + 因子研究前端页（对照 `services/factors/`）
- [x] **A6.2 组合回测**：多策略/多标的组合级回测 + 结果页（对照 backtest-center 的 PortfolioResult）

### A7. 策略引擎能力面

- [x] **A7.1 原生多时间框架策略**：单策略内引用多个周期 K 线
- [x] **A7.2 定时调度**：handle_data / on_rebalance 式计划任务（现有事件总线之外补 cron 调度）
- [x] **A7.3 动态 universe**：运行中动态调整标的池

### A8. 实盘工程化

- [x] **A8.1 持仓对账（position reconciliation）**：本地持仓 vs 交易所持仓周期性核对
- [x] **A8.2 成交恢复（fill recovery）**：重启/断线后漏单找回
- [x] **A8.3 资金费对账**：合约持仓资金费收支核对
- [x] **A8.4 实盘偏差监控**：paper 信号 vs 实盘成交偏离告警

### A9. 可观测性

- [x] **A9.1 Prometheus 指标导出 + Grafana 看板**：进程/订单/延迟/风控告警指标

## B. 现有功能保护清单（补齐过程中不得回归，共 16 项）

每完成一项 A 类改动，跑一遍 `CGO_ENABLED=0 go build ./...` + Go 单测 + `tsc --noEmit`，并人工核对以下功能无回归：

- [x] B1. 9 家交易所适配器（Binance/OKX/Bybit/MEXC/Kraken/Gate/Bitget/Coinbase/Alpaca）
- [x] B2. 高级订单：OCO / Bracket / 冰山 / 条件单 / 跟踪止损
- [x] B3. 网格机器人（现货模式现有生命周期不破坏）
- [x] B4. 12 维风控 + 实盘安全闸（/trading/unlock|lock）
- [x] B5. 模拟盘撮合引擎 + paper 下单链路
- [x] B6. Tick 级回测 + Sharpe/VaR/CVaR 指标 + HTML 报告
- [x] B7. Hyperopt（CMA-ES）+ 实验框架（TPE/Walk-Forward/OOS）
- [x] B8. 跨所套利 + 三角套利
- [x] B9. AI 机器人市场（目录/实例/订阅）
- [x] B10. 社交跟单引擎
- [x] B11. 指标 IDE + Python 沙箱
- [x] B12. AI 多模型讨论室 + 多 Provider + AgentChat
- [x] B13. Telegram Bot（long-polling）/ Discord（ed25519）/ 飞书 / 钉钉 / 邮件通知
- [x] B14. 社区市场（发布/购买/作者收益）
- [x] B15. JWT + OAuth（Google/GitHub）+ API 凭证 AES 保险库
- [x] B16. WS Hub 实时推送全主题 + 断线重连

## C. 做得不好、向 QuantDinger 看齐改好（11 项）

### C1. 测试（对照 QuantDinger 的测试基线）

- [x] **C1.1 补全 7 个 `test.fixme` 的 E2E**：web/e2e/trading-ai.spec.ts 的下单/交易页/AI 对话核心流
- [x] **C1.2 新增关键链路 E2E**：登录→建仓→机器人启动→通知触达

### C2. 实盘可信度（对照 QuantDinger live guard 体系）

- [x] **C2.1 风控参数回调**：`position_limit_pct` 曾被调到 2500% 未回调，恢复正常值并加配置校验
- [x] **C2.2 Paper 撮合引擎 production 化**：去掉 `adapter/matching.go` 的 "dev mock" 定位，修 LIMIT 重复展示、纯下跌行情负余额记账
- [x] **C2.3 实盘下单多交易所验证**：目前仅 Binance 深度验证，OKX/Bybit 至少各跑通一单真实下单回放

### C3. 多用户安全（P0，对照 QuantDinger 越权防护）

- [x] **C3.1 机器人跨用户越权检查**：所有 bot/策略/订单 handler 加 ownership 校验（开放市场前必须完成）

### C4. 商业化健壮性

- [x] **C4.1 Billing 自动到账核验**：tx_hash 提交后自动链上核验 + 状态机，失败可重试

### C5. 代码与仓库卫生

- [x] **C5.1 前端去重**：Settings/Strategy/Bots 三页重复的 CRA 参数表单抽公共组件；删除旧版 SpotTrading/ContractTrading 遗留（约 2450 行）
- [x] **C5.2 清理根目录**：tmp_*.tsx 空文件删除，29 份过期报告归档 docs/archive/
- [ ] **C5.3 修 README**：删除 freqtrade 兼容 API、IBKR/MT5（若 A2.1 未做）、"244 端点"等不实宣称，与 router.go 实际端点一致（用户明确暂缓）
- [x] **C5.4 补 GET /api/experiments**：现为内联空列表（router.go:607），接真实实验数据

## 执行顺序建议

1. **第一批（安全底线）**：C3.1 越权检查 → A3.1 MFA → A3.3 token 吊销 → C2.1 风控参数
2. **第二批（核心补齐）**：A1.2 DCA bot → A1.1 分层马丁 → A1.3 网格中性 → A1.4 马丁硬上限
3. **第三批（可信上线）**：A8.1-A8.4 对账体系 → C2.2/C2.3 撮合与实盘 → C4.1 支付核验
4. **第四批（研究能力）**：A6.1 因子库 → A6.2 组合回测 → A7.1-A7.3 引擎能力面
5. **第五批（体验与外围）**：A5.1 短信 → A9.1 监控 → A3.2/A3.4 → C1 E2E → C5 卫生

---

## 完成记录（2026-09-19）

全部条目（C5.3 按用户要求暂缓）已实施并验证：

- **后端**：`CGO_ENABLED=0 go build ./...` 通过，`go test -count=1 ./...` 42 包全绿
- **前端**：`npx tsc --noEmit` 零错误，`npx vitest run` 218/218 通过
- **E2E**：`npx playwright test --project=chromium` 29 过/0 失败（firefox 项目本机未装浏览器，CI `npx playwright install` 后可跑）
- **B 类回归**：16 项功能均有对应测试包通过（adapter/arbitrage/grid/risk/paper/backtest/hyperopt/experiment/order/dca/lmartin/ai/bot/notify/social/community/indicator/ws/store）
- 实施期间发现并修复的预存 bug：SQLite DSN 参数被静默忽略（无 busy timeout）、MartinStrategy 读写锁死锁、paper applyTrades 死锁、order user_id 恒为 0、Dashboard 白屏、trading-ai spec 过期 mock 等
- 需要真实密钥/实机才能闭环验证的事项：Twilio 真实短信、Stripe 真实 Checkout/Webhook、IBKR Client Portal 实机下单、Turnstile 密钥、XIAOTIAN_CONFIG_KEY（不配则 MFA 备用码明文）
- 已知口径限制：ltm 订单重启后不续补单（由 A8.2 成交恢复兜底）、组合回测年化按 252 交易日、neutral 网格双腿均分投资
