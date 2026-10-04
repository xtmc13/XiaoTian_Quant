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
> 不再生成任何新的宏观审计报告；项目真实状态只以本文件为准
> （例外：2026-09-19~25 对标 QuantDinger 补齐批次的权威记录是
> 《对标QuantDinger补齐清单.md》，47 项已全部勾选；该批次的基线验证记录见本文末节 2026-09-29 条）。
> 背景：根目录 29 份历史报告（2026-08-27）结论互相矛盾、多数已过时，
> 其有效结论已浓缩进本清单，原文件待归档（见第 7 条）。

## P0 —— 让项目"能用"

- [x] 1. **端到端基线验证**：干净环境走通 构建 → 启动 → 登录 → 模拟盘下第一单，结果记录在本文件末节。工具链已就绪（Go 1.25.3 / Node 22 / nginx 8088）。✅ 2026-09-15 完成，见末节验证记录。
- [x] 2. **修复"全新部署风控拦截首单"**：首次下单被 `drawdown 100% > 10%` 拦截（`gateway/internal/risk/manager.go`）；paper 模式应初始化 peak equity 或首单默认放行。✅ 2026-09-15：MaxDrawdown 增加无权益基线放行防御；真正卡单主因是风控上下文构建同步等网络（60s+），已改为离线合成价格兜底。
- [x] 3. **paper 行情接真实数据源**：无 PriceProvider 时返回合成数据（Simulated 标记）；接入币安公共行情（无需密钥），或在页面明确标注"演示数据"。✅ 2026-09-16：WS 行情流修复（代理死锁：env 代理不可达时回退直连由 TUN 接管）后 connected 成功；BTC 实时快照/1m K 线实测为真实币安数据。合成价兜底逻辑保留作为断网保护。
- [x] 4. **数据层修复**（2026-09-15）：引入 `internal/store/migrations/sql/*.sql` 纯 SQL 迁移机制（`internal/store/sql_migrations.go`）；修复 `agent_audit_log` 列定义冲突；修复 `ticks` 表初始化竞态；补 4 个高频过滤列索引。

## P1 —— 稳定性与可信度

- [x] 5. **交易主链路 E2E 测试补全**：9 个用例 7 个标 `fixme`，含下单流程。✅ 2026-09-25 复核：web/e2e/ 全目录 `grep fixme` 为 0（7 个 fixme 已在补齐清单 C1.1 补全）；现共 30 个 Playwright 用例，仅 gate-status.spec.ts 1 条带条件跳过（需 `E2E_LIVE_BACKEND=1` 真实后端），与补齐清单完成记录"29 过/0 失败"口径一致。
- [x] 6. **前端路由缺口复核**：`/api/experiments` 无列表路由、`/api/onchain/*` 未注册（对照 `cmd/server/router.go` 逐条核对）。✅ 2026-09-25 复核：两缺口均已闭环——`GET /api/experiments` 已注册（router.go:697 → `internal/experiment/handler.go:178` 真实实现，按属主过滤）；`/api/onchain/*` 已注册（router.go:71 `registerOnChainRoutes` → `internal/onchain/api.go` 6 条 GET，handler+client 真实实现并带单测，数据源依赖外部 API 与 `ONCHAIN_API_KEY`，未做实机联调）。
- [x] 7. **仓库卫生**：清理根目录 `tmp_*.tsx`、`login-filled.yaml`（疑似含凭据）、`sandbox.log`、`gateway/gateway` 二进制入 `.gitignore`；29 份过期报告归档到 `docs/audits-20260827/`。✅ 2026-09-25 复核：四类垃圾文件均已不存在；`.gitignore` 已含 `gateway/gateway`（第 114 行）；过期报告实际归档于 `docs/archive/`（20 份，路径与本条原文不同，以实际为准）。
- [x] 8. **决定 Rust 引擎定位**：补 FFI 桥接接入 Go 主链路，或正式宣布 Go 执行路径为正式版、Rust 为实验分支（写进 README，消除"计划中"的幻觉）。✅ 已决策并全面落地：纯 Go 为唯一主链路、Rust 引擎弃用仅作基准——TODO 文首"明确不做"、IMPLEMENTATION_PLAN.md 文首状态说明、`build.sh`（默认跳过，需 `BUILD_RUST=1`）、CI `rust-build` 为 `continue-on-error`；2026-09-25 README/ARCHITECTURE/docs 口径已同步修正。

## P2 —— 质量

- [x] 9. **CRA 参数表单去重收尾**：Settings / Strategy / Bots 三页仍是约 400 行/份的重复块。✅ 2026-09-25 复核：去重已完成（补齐清单 C5.1）——共享组件 `web/src/components/strategy/CRAParamForm.tsx`、`components/bots/BotParamForm.tsx` 就位并带测试；Settings.tsx / Strategy.tsx 中 `fast_period/slow_period` 重复参数块为 0 匹配。
- [ ] 10. **新代码纪律**：时间戳统一 INTEGER (unix ms)；新表/新索引必须走 `migrations/sql/*.sql`，禁止再散落 `CREATE TABLE`；JSON 列在应用层做校验。（长期纪律项，非一次性任务，保持未勾作为常态提醒。）

## 端到端验证记录

（每次基线验证的结果写在这里，替换 DEPLOY_VERIFICATION.md 等死文档）

- 2026-09-15：工具链就绪（Go 1.25.3 安装并清理了 /usr/local/go 旧版残留；前端 nginx 8088 部署成功；后端旧二进制可启动，testnet+dry_run 双保险）。
- 2026-09-15：数据层修复（清单第 4 条）验证通过——新装路径：32 表、SQL 迁移 0001 应用、审计日志新列齐、6 新索引在、ticks 懒建表生效；升级路径：现有库 30 表行数零变化，admin/admin123 登录正常。CGO_ENABLED=0 全量编译 + store/data 单测通过（ Rust 引擎库缺失时纯 Go 构建为正式路径，见第 8 条）。
- 2026-09-15：【里程碑·项目第一次"能用"】线上全链路实测：登录 → paper LIMIT 单（秒回，NEW 已持久化）→ paper 市价单（0.7s 成交 FILLED）→ 持仓可见（ETH/USDT 0.01 @ 合成价 2484.87）。修复内容：风控上下文构建由"每单串行 2×30s 网络等待"改为"WS 缓存→2s 探测→合成价三级兜底 + 在线活性门"；`getLastPrice` 消除无超时 http.DefaultClient；MaxDrawdown 无权益基线放行。31 个包单测全绿。已知遗留：paper LIMIT 单会在列表出现两条（OMS+撮合镜像，预置行为）。【2026-09-25 已修：撮合镜像不再回写展示 store，OMS 为唯一事实源；回归测试 `TestPaperLimitOrderNotDuplicated`(app)、`TestPlaceOrderDoesNotMirrorIntoDisplayStore`(service)】下一步：接真实行情源（第 3 条），持仓盈亏才能反映真实价格。
- 2026-09-15：真实权益接入（配合用户"不要模拟盘假数据"）：挖出并修复凭证链路三个坑——① InitDB 的 SECRET_KEY 早退导致路径未初始化、LoadConfig 读空路径产出空配置；② config.yaml 密钥引号内 trailing space 会使签名 secret 多一个空格（网络恢复后必 401，已 TrimSpace + 清文件）；③ 10 万 USDT 假模拟账户改为 `paper_account.enabled: false` 可关（已关，accounts_count 3→0）。实测：带签名的币安账户请求已正常发出，仅剩网络不通（手机代理 7897 未开）；代理开启后真实权益自动同步，无需再改代码。
- 2026-09-16：【里程碑·网格机器人上线】产品方向第一阶段首个真商品完成（切片1-5：引擎 b1b3e1c / Runner 3b676e5 / API 38083e0 / 前端 c0d758a）。E2E：BTCUSDT 74818-77097 12格 1000U 机器人已于真实行情启动（id 22e9f3c8e8e7），真实价格穿越成交（BUY@75957.5）、权益快照正常落库（equity 998.82=1000-手续费-半格价差，记账正确）、前端页面可实时监控。机器人保持运行作为演示，可随时在页面停止/删除。遗留：①跨用户越权检查（多用户市场阶段前必须补）②paper 限价单重复展示【2026-09-25 已修，见上条标注】③纯下跌行情 quote 余额为负的纸面记账（权益计算正确）。
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
- 2026-09-16：【权益改真实数据】用户要求"权益必须是真实数据"。改前 total_equity=100004.75，  其中 10 万是假 paper 种子（真实币安账户仅 0.2U：earn 0.2064+futures dust 0.000146）。
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
- 2026-09-16：【策略投入资金显示修复】用户问"333 初始投入为什么变 100，1U×150 杠杆
  应 150U"。结论：下单口径一直正确（first_order_amount=1 保证金 ×150 杠杆=150U 名义，
  日志实证）；100 是修复脚本填的占位值。揪出病根：投入资金自动维护逻辑自写出就没生效
  ——signal.Strategy 带策略内部名（cra_contract），下游按配置 id 查永远落空；且累加语义
  遇 CRA 重启重放首单会虚增（实测一次重启 150→297.92）。修复：①引擎 dispatch 统一把
  signal.Strategy 覆盖为配置 id（下游精确命中，顺带消除 222/333 同类型匹配张冠李戴）；
  ②改 SET 语义：开仓=该单名义价值、平仓=0（用户口径 1U×150x=150U，2026-09-16 用户
  明确）。实测：日志 strategy= 已显示 7bb9a9a6；连续两笔 FILLED 后 333 显示稳定
  147.88（随 BTC 75900 的名义值），不再累加。commit 86ea66d。
- 2026-09-16：【444 上线 + 新策略暖机修复】用户从 UI 新建 444，保存路径又产出
  错配记录（strategy_type=trend_long、**execution_mode=live**）——证实"保存暗道"
  仍在（HANDOFF 进行中的事 #2 待用户答复入口页面）。已按 333 同款修复
  （cra_contract+paper）。启动后暴露两个 bug 并修掉（commit c8d877e）：
  ①K 线供给管回补只在轮询 goroutine 新起时发生一次，同 symbol 已有策略在跑时
  新策略最长干等一个完整周期——新增直接暖机（EnsureSymbol 返回是否新起，已有
  供给管则直接喂最近 100 根闭合 K 线养指标，信号丢弃）；②updateStrategyCapital
  两条静默 early-return 补 WARN。实测：444 秒收首根 K 线、首单 paper FILLED、
  投入资金自动写入 147.93。另：222/MACD 已被用户从 UI 删除（非系统行为）。
- 2026-09-17：【保存暗道三重封堵】用户确认 333/444 均从"合约策略"页创建，且要求
  "合约策略不能进策略机器人里面去"。三重修复（commit b285d5b + d0ab758，均已部署）：
  ①后端红线加宽：原只压 live，但前端默认发送 execution_mode='signal'——signal
  同样直连真实交易所（resolveExchange 有凭证即 binance），危险同级。现任何非
  paper 值保存时一律压回 paper 并留痕；信号下单路径改白名单式（非显式 live 一律
  paper）作第二道防线。②策略机器人列表过滤：useBotData 按 market_type=swap
  排除合约策略（列表接口未带 category，swap 是可靠判别字段）。③前端创建弹窗/
  面板下线"实盘交易"选项，唯一选项"模拟盘交易（paper）"，payload 固定 paper。
  实测：POST execution_mode=signal 落库为 paper，日志 `execution_mode=signal
  已压回 paper`；前端已重新构建部署。教训：前端"信号通知"选项文案与后端语义
  长期不一致（号称不下单实际直连交易所），UI 文案不可信，安全红线必须在后端。
- 2026-09-17：UI 收官（main=89dbce0）。最终形态：机器人中心=唯一管理页（现货策略/合约策略/网格/马丁/华尔街/AI 八类 chips 统一卡片，详情弹层=RuntimePanel+资金三件套）；创建独立页 /create（现货/合约同一套原版 CRA 表单，现货基础信息含策略类型选择：现货网格/马丁趋势/华尔街/激进，合约由指标选择器的顺势多/空承担身份）；新建机器人模板四卡=现货/合约策略机器人+AI+AI自定义。服务器已同步，333 自动恢复跑单中。
- 2026-09-29：【健康检查 + 构建修复】对 `feature/gap-remediation-2026-09-20`（HEAD 3bca9c3，工作区干净）做全量体检：前端 `vite build` 通过、`vitest` 266/266、`tsc --noEmit` 零错误；后端 `CGO_ENABLED=0 go build ./...` 与全量 `go test` 全绿。发现默认 `go build ./...`（CGO 开启）编译失败：cgo 变体 `cgo_bridge.go` 缺纯 Go 引擎独有的 `BalanceProvider/SetBalanceProvider/SetOnFill`（c5868fd 引入），且 `cmd/download` 经 `internal/data` 传递依赖 FFI 链接 `-lxt_matching` 必失败。修复：Rust FFI 路径改为显式 opt-in `-tags xtengine`（cgo_bridge.go tag=`cgo && xtengine`，补同名 no-op API 保持 service 层可编译）；纯 Go 引擎 tag 放宽为 `!cgo || !xtengine`，xtengine 缺席时无条件生效；三个测试文件 tag 同步；build.sh/gateway README 的 `-tags cgo` 改 `-tags xtengine`。修复后双模式 `go build ./...` 与双模式 `go test -count=1 ./...` 全绿（本机 arm64 无 cargo，FFI 链接路径未经实链，仅编译期验证通过）。分支已推送 origin（`-u` 建上游）。另清理 /root 下过期快照 `XiaoTian_Quant-local-changes-20260915.diff`。文档同步：HANDOFF 刷新至 2026-09-29、CHANGELOG 补 [3.1.0]。遗留：feature 分支尚未合并回 main（合并前建议服务器走一遍 DEPLOYMENT 验证）。
- 2026-09-30：【666 无法开仓根因=Kimi temperature 400】用户新建现货 AI 全自动交易员
  666（ai_auto_trader）running 但永不开仓。日志每 30min 一轮
  `decide failed: llm: kimi: HTTP 400 — invalid temperature: only 1 is allowed
  for this model`：decide(0.3)/gate(0.2)/review(0.3) 传的 temperature 均被
  kimi-for-coding 模型拒绝，决策链全断（gate/review 有兜底所以只有 decide 是致命的）。
  修复：provider.ChatCompletion 对 kimi 钳制 temperature=1（provider.go），调用方无感。
  commit 537d09e 已部署。另注意：666 落库 execution_mode=live 是合法的——期间另一会话
  已把红线升级为实盘总闸（trading.live_enabled: true，三态开关），且现货/合约
  新记录默认 live；用户币安 key 为只读，真实下单会被币安拒绝，需用户知悉。
  工作区遗留：web/package-lock.json 有未提交改动（非本次会话产生，未动）。
- 2026-09-30：【流动性热力扫单反包上线】用户选定 BigBeluga 的 TradingView 指标
  「Dynamic Liquidity HeatMap Profile」做现货自动交易，入场=A 扫流动性反包，
  分钟级工作 K 线。Go 移植 liquidity_heat.go（引擎忠实移植：pivot 外侧挂止损池/
  ATR 偏移/消耗移除/bins 聚合 POC；交易层：跌破买方池同根收回→做多、止盈=上方
  最近存活卖方池、止损=被扫池价−0.5×ATR、96 根超时、强度≥30% POC、一池一单）。
  接：工厂/paramDefs/系统模板/前端类型；数量走引擎 applyTradeHooks 折算。
  实测：BTCUSDT 15m paper 实例（bfecd53b）暖机 99 根 → 43 存活池（买25/卖18）、
  POC 82215，扫描中。单测 7 项全绿。commit 1fdee4d。
- 2026-09-30：【流动性热力 CRA 化 + 三个前端问题修复】①按用户指定语义接入 CRA：
  现货无止损、止盈按 CRA（移动止盈档位优先）、补仓=跌破下一个存活买方池并收回
  （池价在均价下方）触发、金额=first_order_amount×CRA 阶梯乘数递增（非百分比间距），
  超时为最后安全闸。单测+2（完整流程/深跌不割肉）。②揪出并修复一个影响全部
  CRA 扩展策略的运行时 bug：启动前 param 过滤剥除 CRA 键，support_rebound 的
  CRA 模式从未真正激活（222 实证 cra_enabled 恒 False）→ 过滤白名单补 CRA 键；
  实测 222 重启后 cra_enabled=True。③编辑页显示错表单根因：接口归一化后类型在
  type 字段，前端读 strategy_type 恒空 → 双向修复（后端直通 strategy_type +
  前端 ?? type 兜底）。④新建机器人改三级选项（市场→具体策略类型→表单）。
  ⑤流动性热力工作周期可选（默认 1h，不再锁定 15m）。commit 1c82209 + 2274b4d。
- 2026-10-01：【生产恢复 + 在途工作收尾 + 安全纠偏】①恢复 gateway：9-30 12:51 后停机一天（nginx 8088 静态页制造"正常"假象），setsid+PORT=8080 重启，/api/health 通过；前端重建（修复 web/dist 缺 index.html 的半成品状态）并 rsync 至 /var/www/xiaotian。②9-30 中断的未提交工作验证后提交：node_modules 曾半安装（testing-library 缺导出，只跑到 75/290 用例 masking 了红），npm ci 修复后暴露 SupportRebound 用例红——根因是 support_rebound 纳入 CRA 兼容类型后 modal step1 的旧专属参数区成死代码（14 参数只能默认值落库），随 WIP 一并修复（策略卡+StrategyParamModal 弹窗，0c49b96+f14b463），vitest 290/290、tsc 零错误。③并入 9-29 构建修复 477dcb1（xtengine 标签化，服务器双模式 go build 通过）+ 文档 7bf7664；重生成 openapi.yaml 消契约漂移（d95d63c，542 路由）；修 TestGetLastPriceSource 环境抖动（74cbb80，网络可达时 WS 缓存污染探针语义）。④安全纠偏：liquidity_heat c58fc5d5 系 9-30 直插 DB 的 live 记录（绕过保存压回闸），断点续跑恢复后信号直连真实交易所（被币安精度 step=0 bug 在本地拦下，未实际下单；密钥只读）。经 API stop→PUT paper→start 纠正，现 paper FILLED 正常。教训：运行中的网关会用内存态回写覆盖裸 SQL 改动，改执行模式必须停服务或走 API。⑤遗留：币安 adapter 冷启动精度 step="0.00000000" bug（仅影响 live 路径，paper 不受影响）；2 核机上全量并行测试存在时序抖动用例（OKX WS/marketplace/app 单例互污），-count=1 静置可全绿。
- 2026-10-01：【币安零步进规则修复】生产冷启动暴露：币安大量交易对 MARKET_LOT_SIZE.stepSize="0.00000000"（语义=无约束），适配器盲目采用为步进 → quantize "step must be positive" → 市价单 100% 本地被拒（当时拦的是误配 live 的信号单，因祸得福）。本地修复：正值才采用 MARKET_LOT_SIZE 步进/最小量，LOT_SIZE.stepSize 与 PRICE_FILTER.tickSize 非正视为无约束（WARN+跳过步进），仅保留 min/max/notional 校验。3 个复现用例（修复前全红）+ adapter 全包回归绿，已部署生产（57a42fe）。
- 2026-10-01：【币安成交回执链路三层修复+实弹验证】实盘单 OMS 永停 NEW 的根因：① adapter 返回私有类型不满足 reconcile 窄接口（断言永远失败）② 币安 ACK 的 orderId 是 JSON 数字，.(string) 断言静默丢弃，xt_orders 无列可存 ③ 现货查询 URL 双重 /api/v3 前缀（生产 404）；顺带修 futuresRawRequest 直引常量不读 env/testnet。修复：签名改 reconcile 类型、stringifyOrderID 规整+新列 exchange_order_id(迁移0034)+OMS 持久化、reconcile 优先用交易所单号查询。实弹验证：策略666 两笔新单下单后 reconcile 自动补录成交并推进 FILLED（avg 83780/83797 与币安 myTrades 一致）；11:25 存量卡死单回填真实单号 67112224734 后同样恢复（avg 83448.88）。commit 43548d5。教训：交易所 ID 必须 json.Number/字符串化解码，float64 打印会吞位数（日志 6.7112224734e+10 曾让我回填错成 671122224734）。
- 2026-10-01：【引擎虚持仓回滚+重启仓位重建（666 实盘三连修）】用户确认 live_enabled 与删单都是自己操作后，补齐引擎层两个断层：① 信号发出即乐观记账，交易所拒单对引擎不可见 → 永久虚持仓（-2010 余额不足实证）。OMS 四条拒绝路径补 fireOrderUpdate，liquidity_heat 拒单回滚/补仓拒单减挂起/出场拒单可重试。② inPosition 只在内存，重启重放失忆重复买入（三次重启四次买入实证）。订单 sig:<配置id>: 打标 + wrapper 路由过滤 + store.NetFilledByStrategy 汇总 + PositionRestorer 注入 Start 参数清态后恢复。部署中连抓三个子 bug：Start 清态抹掉恢复（改注入式）、固定止盈分支缺 targetPrice>0 守卫（恢复后秒平）、暖机重放计持仓时长（holdCountFrom 门）、CRA 重建档位按满档（防补仓风暴）。终态：666 重建持仓 0.00066 BTC @VWAP 83931，零信号风暴，CRA 止盈/超时管理中。
- 2026-10-01：【堵死无限买入最后路径——CLOSE 信号账本兜底出场】用户质疑"6 笔买入是否是无限买入 bug"，深挖实证：现货成交按 BTCUSDT-BUY/SELL 键入本地组合，closePositionFromSignal 按 BTCUSDT-LONG/SHORT 找仓，键空间永不相遇且现货仓位不镜像 → 策略止盈/超时 CLOSE 信号永久空转，引擎发出即复位空仓、下一形态又真买——无限循环直到余额耗尽。修复：镜像无仓时按策略账本净持仓（NetFilledByStrategy）市价卖出，sig: 打标拒单可重试。今日 6 笔买入的两条根因（重启失忆重买、出场空转重买）至此全部闭合。
- 2026-10-04：【在途工作收尾 + 部署 + 全面体检】①约 2100 行未提交在途工作（主力行为 smart_money 策略、策略成交历史、paper 账户快照持久化、引擎修复）核查确认完成度约 95% 后修掉尾缺并分三提交推送：9ac0386（引擎 ABBA 死锁移订阅出锁/symbol 订阅校正/经典策略 symbol 应用/MACD 重启重建/liquidity_heat 面板契约/store 配置持久化加固）、1157986（paper 快照含持仓重启不丢+幂等入账、/api/trades 本地回退带真实手续费、策略面板 paper 绩效三件套+position_qty 补真值、前端成交历史+K线定位+浮盈美元主显）、0c82e0e（smart_money 全链路+position_size 仓位参数修"10% 余额兜底一单约 1 万名义"缺陷、创建表单执行方式选择默认 paper 替代硬编码 live 实际收紧安全口径、openapi 迁 docs/api）。②Docker 重建部署（镜像 d35366e8）生产实测：paper 账户恢复 USDT=98119.82 持仓 2 只（新持久化生效）、4 策略重启自动恢复收首根真实 K 线、微信通道 token 恢复、SPA 新前端（/smart-money 200）。③全量验证：go build+全仓 test 绿、vitest 397/397、tsc 零错误、vite build 正常、Playwright chromium 全绿（1 条 gate-status 条件跳过同 9-25 口径）。④体检实锤缺陷清单（按严重度，详见会话/下轮修）：高——7 个已注册页面（/alerts /bots/signal /status /data /logs /ai/rl /ai/tensorboard）全站无入口只能手输 URL；PUT /executor/signal-sources/:id 前端封装后端不存在（404 潜伏）；信号源管理 3 端点零调用+SignalExecutorPanel 指引死路（设置页无信号源配置）；/reconcile 对账 9 端点、/combos 组合策略 8 端点零 UI。中——用户禁用/启用、社区作者收益+审核、社交跟单订阅轨道、Tick 数据+Tick 回测、AI 异步分析等后端有前端无入口（另有 12 个零散端点）。低——Dashboard AI Agents 卡片硬编码假状态；/api/health version 硬编码 3.0.0 与构建脱节；WatchlistPanel 两 disabled 死按钮。运行时——dataprovider news 上游 401（key 失效）、calendar XML windows-1252 缺 CharsetReader 解析失败。已知限制不动：MACD 重建仅净多、adapter OKX WS 单测抖动。遗留：管理密码已被强制改密流程更换，带鉴权端点冒烟需用户提供现密码。
- 2026-10-04：【功能接线第三批：Tick/AI异步/12 零散端点收尾】①Tick 数据接 /data 页（新 components/data/TickPanels：下载[后端网络下载已下线，按钮如实透出 400 说明]+信息+查询三区块，LocalBarsPanel 接 /data/bars 预览落库 K 线，顺带修正 BarDataResponse 类型对齐后端 model.Bar 口径 time/count）；Tick 级回测接回测中心页（components/backtest/TickBacktestPanel：sma_cross/breakout 提交 → jobs 轮询[运行中 3s/静置 15s]→ 八格结果指标/失败错误透出）。②AI 异步分析选择接线不下线：/analysis/start+/analysis/result 是多模型并行+共识投票（与同步 /ai/analyze 单 provider 不同构，有差异化价值）——AI 页头部加"多模型共识"弹层（aiRobotApi.getModels 全选默认、interval 可选、2s 轮询 60s 超时、完成映射 AIAnalysisResult 复用 AnalysisResultView 渲染+入历史）。③12 零散端点：接——bracket/calculate（高级订单页 BracketCalculator，百分比→小数口径，结果一键回填下单表单）；pairlist/specs（lib/pairlistSpecs 合并驱动添加清单：交集留本地、规格独有生成通用模板、本地独有保留不裁剪[规格是能力子集]，API/Local 徽标）；exchange/usdcny（Portfolio KPI 下汇率条+总资产 CNY 约合）；notify/channels+notify/send（设置页通知区渠道状态徽标+发送测试通知[级别/渠道勾选/部分失败透出]）；strategies-python/run（指标 IDE 新 PythonSandboxPanel[mode=indicator|script，附最近 500 根 K 线，信号表渲染]，引擎 503 如实透出）；strategies/defaults+contract-defaults（StrategyCreateForm 创建场景每组合一次性兜底叠加，applyServerStrategyDefaults 纯函数[小数→百分数换算、martin_trend→martin_trend_v2 映射、both→dual]，编辑模式不叠加）；hyperopt/jobs/:id/export（结果卡"导出到策略"弹层=二次确认：可编辑参数映射+覆盖警告+成功后 mapped_params 展示）；ai-bots/instances/:id/trades（BotDetailView 新 AIBotTradesCard，Unix 秒口径，对齐 StrategyTradeHistory 模式）；experiments+/experiment/status（指标 IDE 新 ExperimentsPanel，运行中 5s 轮询）。不修——GET /klines/:symbol（与 /market/klines 同源同缓存同 Binance 数据，前端全站统一走后者，纯冗余）；GET /chart（桩端点恒返 {symbol,data:[]}，无图表快照/分享实现，无对应 UI 场景）。i18n 新增 tick/aiasync/ide/orderkit 四个 locale 文件（中英日）+ settings/portfolio 扩展。新增 10 个测试文件 30 用例。实测：tsc 零错误、vitest 458/458、vite build 正常。
- 2026-10-04：【体检缺陷全量修复收官（四批提交）】按用户"全部按顺序修"决策，审计清单全部处置完毕：①后端快修五项（75138ca）：calendar windows-1252 CharsetReader；/api/health 版本硬编码根因=Dockerfile 注入的 -X main.version 在 main.go 无接收变量，补注入点 SetVersion；Dashboard AI Agents 三卡改真实信号（WS运行/引擎实例数/风控熔断器）前端删写死兜底卡；信号源 PUT/DELETE 补齐（级联取消订阅留审计、内置 default 禁删）；news 401 根因=CryptoCompare 收紧匿名访问，新增 CRYPTOCOMPARE_API_KEY（.env.example 附申请链接）走 not_configured 语义不再刷屏。②导航+信号源 UI（918dc65）：7 个零入口页面进 Sidebar（/bots/signal 机器人中心组、/ai/rl+/ai/tensorboard 组、/data+/alerts 高级组、新建系统组收 /status+/logs）；SignalSourceManager 管理弹窗修"设置页配置信号源"死路指引。③用户/社区/社交（a2e6906）：UserManage 禁用/启用（disable 踢下线配危险确认）+运营概览+最近活动；作者收益卡；社区指标审核 tab；社交双轨订阅 MarketProviders（解决 db_id 与引擎 offset id 双 id 坑）。④对账中心（eb3e4f6）：/reconcile 五区块，accept_exchange 唯一资金副作用动作 danger 确认，配置 admin 八键白名单。⑤组合策略（59d2c6a）：/bots/combo 全管理面，后端核实真实可用（vote/weighted/unanimous 聚合引擎真实）。⑥收尾（4a6e7d6）：combo 配置持久化（迁移 0056 xt_combo_configs，重启重建，防分裂删除语义）；移除自选股两个死按钮；admin referrals 接 UserManage tab、config/reload 接 Settings（admin+确认）。vitest 从 397 增至 485/485，Playwright chromium 全绿。部署：VERSION=3.0.5 镜像重建，生产实测 health 如实报 3.0.5（版本注入修复生效）、paper 恢复 98119.82+2 持仓、4 策略恢复首根真实 K 线、/reconcile /bots/combo /smart-money /alerts /status /data /logs 全部 200、news 单次 not_configured WARN 不再 401 刷屏、calendar 不再报错。遗留：CRYPTOCOMPARE_API_KEY 待用户配置（不配新闻源保持降级）；Tick 网络下载后端已下线按钮如实透出；社交 provider 待审列表后端无端点只做单条审核；referrals created_at 秒级与毫秒纪律不一致（前端已防御后端未动）；MACD 重启重建仅净多；管理密码被强制改密流程更换，带鉴权冒烟需用户提供。
- 2026-10-04：【币富 CRA 参数对账 + 顺势多/空入口下线】①对账（用户提供币富说明书+名词解释 PDF 与截图）：主体框架已逐档一致（补仓阶梯倍数/差价/回调默认值、移动止盈四档默认值、防瀑布、止损三类型、循环类型/次数、首单额度/加倍、止盈方式三选、补仓 MACD/EMA 开关）。**参数壳引擎未实现（币富有真功能，我们表单可填会落库但引擎不读，grep 实证）**：顺势而为 follow_trend、开仓加倍 open_double、反向止盈 reverse_take_profit_period、反向止损 reverse_stop_loss、全局/对向燃烧 burn_*、online_order_limit；**行为打折扣**：take_profit_method 的 tail/head_tail 在 state.go 无分支与 full 同逻辑；EMA 顺/逆势引擎不区分同一判定；指标 fast/slow 参数落库但引擎硬编码 12/26/9；指标周期选项仅 关闭/5m/15m（币富 5m~8h 六档）；挂单价格引擎支持但表单无输入框。以上待用户定夺是否补实现。②按用户决策"只去顺势多/空类型入口"（8740fa9）：STRAT_TYPES.contract 删 trend_long/trend_short，三处创建入口（机器人中心向导/策略管理侧栏/遗留创建弹窗）同驱动封死，向导文案同步；存量兼容全保留（mapCRAFactory/工厂注册/编辑回填/回测页历史类型名）。新增 StrategyFormFields 防回归测试 2 例，vitest 493/493，已部署 3.0.7。③同日早些：策略管理页↔机器人中心双向互通入口（3a4e170）：行内编辑按钮跳 /create?id= 编辑模式、状态徽标跳 /bots?bot=<id> 深链开详情弹层并清参，修正"两页互斥"的错误认知（实为全量列表 vs 三源合并的大面积重叠）。
