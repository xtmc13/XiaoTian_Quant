# XiaoTian_Quant 行动清单（2026-09-15 重整）

> ## 产品方向（2026-09-15 定）
>
> **定位：面向中文用户、单二进制自托管、模拟盘即开即用、机器人订阅分成的量化平台。**
> 对标：CryptoRobotics 的商业形态（机器人市场+订阅）、freqtrade 的核心链路严谨性、
> 区别于 QuantDinger 的重型全家桶（Docker+Postgres+Redis 多进程）——我们一个二进制就能跑。
>
> 路线：P0 打通可用 → 网格机器人一个做到底（真实行情+7×24 paper+真实权益曲线）
> → 市场 MVP（发布/订阅/分成跑通一个类型）。
>
> **明确不做**（防开新坑）：Rust FFI 接入主链路（纯 Go 为正式路径）、AI/ML 价格预测、
> 链上模块、社区/论坛、多券商接入。

> 规则：做完一条，勾一条，立即 `commit + push`。
> 不再生成任何新的宏观审计报告；项目真实状态只以本文件为准。
> 背景：根目录 29 份历史报告（2026-08-27）结论互相矛盾、多数已过时，
> 其有效结论已浓缩进本清单，原文件待归档（见第 7 条）。

## P0 —— 让项目"能用"

- [x] 1. **端到端基线验证**：干净环境走通 构建 → 启动 → 登录 → 模拟盘下第一单，结果记录在本文件末节。工具链已就绪（Go 1.25.3 / Node 22 / nginx 8088）。✅ 2026-09-15 完成，见末节验证记录。
- [x] 2. **修复"全新部署风控拦截首单"**：首次下单被 `drawdown 100% > 10%` 拦截（`gateway/internal/risk/manager.go`）；paper 模式应初始化 peak equity 或首单默认放行。✅ 2026-09-15：MaxDrawdown 增加无权益基线放行防御；真正卡单主因是风控上下文构建同步等网络（60s+），已改为离线合成价格兜底。
- [x] 3. **paper 行情接真实数据源**：无 PriceProvider 时返回合成数据（Simulated 标记）；接入币安公共行情（无需密钥），或在页面明确标注"演示数据"。✅ 2026-09-16：WS 行情流修复（代理死锁：env 代理不可达时回退直连由 TUN 接管）后 connected 成功；BTC 实时快照/1m K 线实测为真实币安数据。合成价兜底逻辑保留作为断网保护。
- [x] 4. **数据层修复**（2026-09-15）：引入 `internal/store/migrations/sql/*.sql` 纯 SQL 迁移机制（`internal/store/sql_migrations.go`）；修复 `agent_audit_log` 列定义冲突；修复 `ticks` 表初始化竞态；补 4 个高频过滤列索引。

## P1 —— 稳定性与可信度

- [ ] 5. **交易主链路 E2E 测试补全**：9 个用例 7 个标 `fixme`，含下单流程。
- [ ] 6. **前端路由缺口复核**：`/api/experiments` 无列表路由、`/api/onchain/*` 未注册（对照 `cmd/server/router.go` 逐条核对）。
- [ ] 7. **仓库卫生**：清理根目录 `tmp_*.tsx`、`login-filled.yaml`（疑似含凭据）、`sandbox.log`、`gateway/gateway` 二进制入 `.gitignore`；29 份过期报告归档到 `docs/audits-20260827/`。
- [ ] 8. **决定 Rust 引擎定位**：补 FFI 桥接接入 Go 主链路，或正式宣布 Go 执行路径为正式版、Rust 为实验分支（写进 README，消除"计划中"的幻觉）。

## P2 —— 质量

- [ ] 9. **CRA 参数表单去重收尾**：Settings / Strategy / Bots 三页仍是约 400 行/份的重复块。
- [ ] 10. **新代码纪律**：时间戳统一 INTEGER (unix ms)；新表/新索引必须走 `migrations/sql/*.sql`，禁止再散落 `CREATE TABLE`；JSON 列在应用层做校验。

## 端到端验证记录

（每次基线验证的结果写在这里，替换 DEPLOY_VERIFICATION.md 等死文档）

- 2026-09-15：工具链就绪（Go 1.25.3 安装并清理了 /usr/local/go 旧版残留；前端 nginx 8088 部署成功；后端旧二进制可启动，testnet+dry_run 双保险）。
- 2026-09-15：数据层修复（清单第 4 条）验证通过——新装路径：32 表、SQL 迁移 0001 应用、审计日志新列齐、6 新索引在、ticks 懒建表生效；升级路径：现有库 30 表行数零变化，admin/admin123 登录正常。CGO_ENABLED=0 全量编译 + store/data 单测通过（ Rust 引擎库缺失时纯 Go 构建为正式路径，见第 8 条）。
- 2026-09-15：【里程碑·项目第一次"能用"】线上全链路实测：登录 → paper LIMIT 单（秒回，NEW 已持久化）→ paper 市价单（0.7s 成交 FILLED）→ 持仓可见（ETH/USDT 0.01 @ 合成价 2484.87）。修复内容：风控上下文构建由"每单串行 2×30s 网络等待"改为"WS 缓存→2s 探测→合成价三级兜底 + 在线活性门"；`getLastPrice` 消除无超时 http.DefaultClient；MaxDrawdown 无权益基线放行。31 个包单测全绿。已知遗留：paper LIMIT 单会在列表出现两条（OMS+撮合镜像，预置行为）。下一步：接真实行情源（第 3 条），持仓盈亏才能反映真实价格。
- 2026-09-15：真实权益接入（配合用户"不要模拟盘假数据"）：挖出并修复凭证链路三个坑——① InitDB 的 SECRET_KEY 早退导致路径未初始化、LoadConfig 读空路径产出空配置；② config.yaml 密钥引号内 trailing space 会使签名 secret 多一个空格（网络恢复后必 401，已 TrimSpace + 清文件）；③ 10 万 USDT 假模拟账户改为 `paper_account.enabled: false` 可关（已关，accounts_count 3→0）。实测：带签名的币安账户请求已正常发出，仅剩网络不通（手机代理 7897 未开）；代理开启后真实权益自动同步，无需再改代码。
- 2026-09-16：【里程碑·网格机器人上线】产品方向第一阶段首个真商品完成（切片1-5：引擎 b1b3e1c / Runner 3b676e5 / API 38083e0 / 前端 c0d758a）。E2E：BTCUSDT 74818-77097 12格 1000U 机器人已于真实行情启动（id 22e9f3c8e8e7），真实价格穿越成交（BUY@75957.5）、权益快照正常落库（equity 998.82=1000-手续费-半格价差，记账正确）、前端页面可实时监控。机器人保持运行作为演示，可随时在页面停止/删除。遗留：①跨用户越权检查（多用户市场阶段前必须补）②paper 限价单重复展示 ③纯下跌行情 quote 余额为负的纸面记账（权益计算正确）。
- 2026-09-16：合约策略跑通（goal）——MACD15m/BTCUSDT/1U×125x/逐仓/paper 配置（bd592954）真实运行：K线供给管→总线(PublishSync保序)→引擎→MACD→信号 全链路实测（双策略各发真实 LONG 信号）。关键修复：①事件总线多worker乱序→PublishSync ②供给管删除币安WS伪1m Bar（会污染指标）③启动回补100根历史K线暖机 ④risk position_limit_pct 50%→2500%（paper小权益+最大杠杆会误杀；config.yaml 未入库，开实盘前必须回调）。遗留：信号→paper成交的最终落库验证因手机代理再次断连（13:40起）暂缓，网络恢复后 feeder 每20s自动重试，下一个金叉即完成闭环。
- 2026-09-16：代码已同步 GitHub（SSH deploy key，main=f7df0eb，16 提交：SQL迁移机制/网格机器人五切片/响应包装器修复/下单离线修复/K线供给管/可观测性）。手机代理极不稳定（一天多次被杀），后续验证与部署转入用户提供的服务器进行。
- 2026-09-16：【服务器部署完成】43.165.179.199（x86_64，网络自由）：Go1.25.3+仓库+编译+真实币安数据（余额同步成功）。合约策略全链路在服务器实测跑通：真实15m/1m K线→MACD→信号→**paper撮合成交（FILLED, qty=0.0016=125U名义）**。过程中揪出并修复三个深坑：①信号配置查找 id/名字不匹配导致 execution_mode=paper 与杠杆/TP/SL 全部被跳过、信号单直连真实交易所（用户密钥为只读，未造成实际下单，虚惊）②前端 dist 曾缺 index.html（旧部署假象掩盖）③事件进程残留导致新代码不生效（pkill 自匹配自杀）。前端已部署（nginx 8088），网格机器人服务器版已启动（b8c379b57193）。遗留：①云安全组未开 8088，外网暂不可访问（需用户在云控制台放行）②paper 种子金额时序（LoadConfig 晚于 NewManager，1000 配置生效为 100000）③合约 paper 持仓的权益展示口径 ④两策略同刻信号撞 500ms 限速仅成一单。
- 2026-09-16：【里程碑·333 修复 + P1 死锁修复】①修复脚本执行：333（7bb9a9a6）策略类型
  trend_long→cra_contract、execution_mode→paper，其余参数保留（DB 实读确认）。
  ②启动 333 时触发了一个隐藏 P1 死锁：Start 请求挂死 >10min、pprof 抓栈发现 174 个
  goroutine 堆积、SIGTERM 无法退出、只能 SIGKILL。病根：`event.dispatch` 持 RLock 调
  handler，策略信号→PlaceOrder→order-update 重入 `Publish`，与并发 `Subscribe`（Register
  持 Engine 写锁等 bus 写锁）成环（Go RWMutex 不可重入）。修复：`dispatch` 改为锁内快照
  订阅、锁外调 handler（event.go）；顺带修 `Engine.Unregister` 从不退订的僵尸订阅泄漏
  （engine.go，订阅 id 登记+退订）；补回归测试 `TestReentrantPublishWithConcurrentSubscribeNoDeadlock`
  （event_test.go，-count=10 全绿，旧代码必挂）。实测证据：重启后 333/222/MACD 三个策略
  启动 API 均 <12ms 返回；333 全链路跑通（received first bar 15m close=76933.35 →
  cra first order long qty=0.001948 → paper FILLED）；goroutine 总数 218（死锁时）→35（健康）。
  ③已知坑+1：shell 环境若自带 PORT（如 node 环境的 PORT=3000），config.yaml 无 server.port
  会 fallback 到 env 撞车 Claude UI 端口，网关启动失败——重启必须显式 PORT=8080。
- 2026-09-16：【权益改真实数据】用户要求"权益必须是真实数据"。改前 total_equity=100004.75，
  其中 10 万是假 paper 种子（真实币安账户仅 0.2U：earn 0.2064+futures dust 0.000146）。
  修复三件事：①`TotalEquity()` 排除 paper 账户（只算真实交易所），新增 `PaperEquity()`
  单独统计模拟账本，summary 接口与 Portfolio 页同步展示（总资产估值卡下加"模拟权益"）；
  ②**paper 种子 10 万真凶实锤**：YAML 整数字面量解出为 int，`.(float64)` 断言静默失败
  回退默认值——config 的 1000 从未生效；已兼容 int/int64/float64；③权益改真实后暴露两个
  次生 bug 并修掉：`position_limit_pct: 2500` 等风控配置从未接线（恒默认 50%，此前被 10 万
  假权益掩盖）；风控上下文对 paper 单用 0.2U 真实权益做分母→曝险 70000%+ 全被误杀——
  现在按订单执行目标选基准（paper 单用 paper 权益+`NetExposureAgainst`）。顺带删掉
  TotalEquity 每次调用打一行 debug 日志的刷屏逻辑。实测：total_equity=0.2066（真实）、
  paper_equity=999.01（1000 种子-手续费，结算正常）、333 首单 paper FILLED、持仓
  BTCUSDT 0.001948 已入账。已知表现变化：历史权益曲线里旧假数据会显示一次陡降
  （xt_portfolio_snapshots 旧快照未清理）；回撤监控基准从假 10 万变为真实权益。
