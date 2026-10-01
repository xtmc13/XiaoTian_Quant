# XiaoTian_Quant 对标检查报告（QuantDinger / FreqTrade / CryptoRobotics）

> 生成时间：2026-10-01。方法：三个对标项目功能调研 + 本项目前后端全量实测（470 个后端路由探测 + 50 个前端页面真实浏览器访问）。

---

## 一、总体结论

功能广度上项目已经很完整（54 个路由模块、41 个页面，QuantDinger 补齐清单 19+11 项全部打勾）。但**实测发现一批"页面在、接口通、实际不可用"的问题**，以及相对三个对标项目的若干能力差距。

**严重度分级**：🔴 阻断使用（已修/待修）｜🟡 功能缺失｜🔵 体验差距

---

## 二、🔴 实测故障（前后端真问题）

### 后端 500 错误（6 个，全部已修复 ✅ 2026-10-02 验证）
| 接口 | 错误 | 根因 | 修复 |
|---|---|---|---|
| /api/community/author/revenue | 缺表 indicator_revenue | schema.go migrateV7 只建了 marketplace 三表，漏建 revenue 分成表 | 新增 `migrations/sql/0035_indicator_revenue.sql`，启动自动执行 |
| /api/ml/strategy-models | 连不上 localhost:8001 | sandbox 容器只起了 main:app(9000)，ml_server(8001) 未启动；且 gateway 的 ML_SERVER_URL 默认 localhost 指向自身 | sandbox CMD 同时起两个 uvicorn；compose 注入 ML_SERVER_URL=http://sandbox:8001 |
| /api/rl/models | 连不上 localhost:8001 | 同上 | 同上（RL client 复用 ml baseURL） |
| /api/tensorboard/runs | 连不上 localhost:8001 | TensorBoardClient 是独立客户端，空 baseURL 时**不读** ML_SERVER_URL 环境变量，硬编码 localhost | tensorboard.go 对齐 ml/client.go：空 baseURL 依次读 ML_SERVER_URL env、默认 localhost |
| /api/pairlist/refresh | no producers configured | init() 只创建 manager 未注册任何 producer，全新系统无配置即 500 | init() 默认注册 VolumePairList（对齐 freqtrade 默认行为），保存配置后重建 |
| /api/pairlist/whitelist | no producers configured | 同上 | 同上，实测返回 30 对真实白名单 |

### 本轮另修的 4 个隐藏 bug（2026-10-02）
1. **响应包装器破坏 PWA**：`UnifiedResponseWrapper` 把 `/manifest.json`（JSON 内容类型）包成统一信封 `{success,data}`，浏览器 PWA 安装解析必失败；且包装后 body 变长但沿用 handler 设置的旧 `Content-Length`，导致客户端 IncompleteRead。修复：skipPaths 增加 /manifest.json /sw.js /favicon.svg 原样透传；包装写回前 `Del("Content-Length")`（主分支 + wrapPrimitive 分支）。
2. **日志 fields 静默丢弃**：`logging.toFields` 按 key,value 成对解析参数，RequestLogger 传单个 map 时（奇数个参数）循环不执行，map 被丢弃 → text 日志永远看不到 status/path。修复：奇数参数且末位为 map[string]any 时合并其键值。
3. **sw.js 缓存版本写死**：`CACHE_VERSION='v4'` 不随发版变化，旧缓存永不清理。修复：Dockerfile 构建时 sed 替换为构建时间戳，每次发版版本必变，activate 自动清全部旧缓存。实测版本 `v1790871340`。
4. **E2E 缺路由直开覆盖**：新增 `web/e2e/route-smoke.spec.ts`，60+ 路由数据驱动 deep-link 用例（对齐 App.tsx 路由表），断言 HTTP 200 + #root 非空 + body 有可见文本，防 301 循环/白屏回归。

### 指标 IDE 图表链路（2026-10-02 第二轮修复）

用户反馈"指标 IDE 写代码图表不能正常显示"，排查出**三层叠加问题**：

1. **沙箱四服务只起一**：sandbox 容器有 main.py(9000 指标执行)、ml_server(8001)、ccxt_bridge(8002)、strategy_engine(8003) 四个服务，CMD 只起 9000。gateway 调指标执行报 `connection refused localhost:9000`（compose 未注入 SANDBOX_URL，与此前 ML 同类）。修复：CMD 四服务全起 + compose 注入 SANDBOX_URL/CCXT_BRIDGE_URL/PYTHON_STRATEGY_URL/ML_SERVER_URL 指向 sandbox + Dockerfile 补装 strategy_engine 依赖。四端口健康检查全 200。
2. **plots 被前端丢弃**：沙箱返回完整 output（plots 均线 + signals 信号，实测 20 个信号），但 IndicatorIDE.tsx 只提取 signals，plots 直接丢弃 → 图表只有 B/S 点没有均线。修复：新增 chartPlots state 提取 output.plots，传入 KlineChart 新 customPlots prop。
3. **KlineChart 无自定义线渲染**：组件只有 signals overlay 逻辑，没有任何 createIndicator 调用（工具栏 SMA/RSI 按钮也只是 UI chip 不画线）。修复：customPlots prop 用 klinecharts createIndicator 渲染——overlay=true 叠加主图（均线类），overlay=false 开独立副图（RSI/MACD 类），calc 闭包预计算数据 + 长度对齐防错位，指纹去重防闪烁。

图表库选型与 QuantDinger 一致（同为 klinecharts），协议（output.plots/signals）无需改动。

**第二轮追加修复（2026-10-02 晚，像素级验证通过）**：
4. **klinecharts 自定义指标静默失败**：`createIndicator` 直接传对象返回 null（必须 `registerIndicator` 注册后用名字引用）+ 必须 `series: 'price'` 才能主图叠加。已修。
5. **`Number(null)===0` 坑**：plots 数据 null 映射成 0 把均线前段画到 0 价位，已改 NaN 跳点。
6. **plots 指纹去重 bug**：`prevPlotsKeyRef` 指纹未含 fallback 后颜色，后端不给 color 时指纹恒定相同跳过重绘、旧指标残留。已改用最终色参与指纹；后端未给 color 时按 `INDICATOR_COLORS` 调色板轮换（避免全默认蓝）。
7. **信号颜色全链路验证**：数据链路（后端 → api client → IndicatorIDE → KlineChart overlay extendData → createPointFigures 回调）逐层埋点验证 color 无损；canvas 精确像素计数确认 B=绿 #00E676、S=红 #FF5252。排查期间发现 `#1677FF` 蓝为 klinecharts 内置最新价标签/光标默认色，非信号渲染问题；vite `drop_console: true` 会剔除全部 console 调试日志（排障时用 window 全局变量替代）。
8. 回归 17/17 通过；`web/e2e/route-smoke.spec.ts` 冒烟用例随构建生效。

### 前端页面打不开（16+2 个，已修复 16 个）
曾受影响：`/trading/spot` `/trading/contract` `/strategy/editor` `/strategy/python` `/backtest/portfolio` `/ai/analysis` `/ai/rl` `/ai/discussion-room` `/bots/strategy` `/bots/grid` `/bots/signal` `/bots/ai` `/bots/dca` `/bots/layered-martin` `/arbitrage/cross` `/arbitrage/triangular` 等

**两个叠加的根因，均已修复并实测验证（浏览器实测 18 个页面 16 个渲染正常，2 个空白为后端 ML 服务未起）**：
1. **SPA fallback 301 循环**：fallback 用了 `http.FileServer`，它对未知路径做 301 目录重定向修正（/trading/spot → 301 → /trading/ → 301 自循环），造成 ERR_TOO_MANY_REDIRECTS。修复：NoRoute 直接回写 embed 的 index.html 字节。
2. **前端资产相对路径白屏**：vite `base: './'` 使深链接页面（/trading/spot）把 `./assets/app.js` 解析成 `/trading/assets/app.js`，NoRoute 把 HTML 当 JS 返回 → React 不挂载 → 白屏。这正是用户遇到"登录已过期"的机制：刷新页面即白屏/异常。修复：Docker 构建用 `npx vite build --base=/`（注意 `npm run build -- --base=/` 传参会被 npm 追加到命令末尾，vite 收不到）；NoRoute 对带后缀路径返回 404 做防御；Electron 本地构建仍用 vite.config.ts 的 './'。

### 部署配置缺陷（本次部署中发现并已修）
1. docker-compose 未传 `SECRET_KEY` → 数据库初始化整体回滚（admin 创建了也登录不了）
2. DB_PATH 默认在容器临时层 → 容器重建数据全丢，已指向持久卷
3. Dockerfile 三处 bug（musl 静态链接 / Go 1.23 vs 1.25 / 缺 g++）
4. 前端路由未注册（router.go 注释掉了 SPA 服务）

### 数据层
- `indicator_revenue` 表缺失 → 作者收益页 500
- WAL 模式下部分数据在 -wal 文件，直接 cp 主文件读不到（备份脚本要注意）

---

## 三、🟡 对标功能差距矩阵

### vs QuantDinger（v5，12.3k star）
QuantDinger 2026-09 的补齐清单已全部完成，**剩余差距**：
1. **分布式运行时架构**：QuantDinger v5 有 Kafka 事件骨干 + 分片策略调度/评估 worker + Celery 任务队列；本项目是单进程 goroutine 事件总线，策略多了会互相抢资源
2. **MCP Server**：QuantDinger 提供 MCP（AI Agent 直连交易系统的标准协议），本项目无
3. **PostgreSQL**：QuantDinger 用 PG（多用户并发写强），本项目 SQLite（单写者，多用户规模受限）
4. **移动端 H5**：QuantDinger 有手机端，本项目纯桌面 Web
5. **审计日志独立 worker**：版本化事件流独立落库

### vs FreqTrade（55k star，33000 commits）
FreqTrade 是单一机器人工具，本项目功能面比它宽（多租户/多机器人/社区），但工程成熟度差距：
1. **FreqAI 自适应机器学习**：自训练、自适应市场变化的 ML 策略框架（本项目有 ML 模块但服务没跑起来，且是训练-预测分离，非自适应滚动重训）
2. **lookahead-analysis / recursive-analysis**：策略前视偏差检测工具，防止回测造假（本项目无）
3. **策略参数 hyperopt-loss 库**：可插拔多种优化目标（本项目只有 CMA-ES 单目标）
4. **数据下载/转换 CLI 全家桶**：download-data / convert-data / trades-to-ohlcv 等运维工具链（本项目有 DataManager 页面但无 CLI）
5. ** edge 模块**（基于风险的仓位计算）：无
6. **Dry-run 持久化与实时状态分离**：FreqTrade 的 dry-run 状态机经过 8 年打磨

### vs CryptoRobotics（商业平台，56 项功能）
1. **利润分成计费（PSH）**：盈利后抽成 10-30% 的商业模式，本项目 Billing 只有 USDT 手动转账 + Stripe 订阅，无利润分成模式
2. **移动端 App**：Android/iOS，无
3. **云端 24/7 托管即开即用**：CryptoRobotics 是 SaaS，本项目需自托管（双刃剑，但也是商业差距）
4. **TradingView 告警联动**（已有 `/api/webhook/tv` 但无 TV 图表嵌入/告警配置引导）
5. **图表下单（Trade from Chart）**：在 K 线上直接下单，本项目无
6. **K线收盘止损（Candle Close SL）**：防插针，本项目止损是即时价触发
7. **追踪止盈（Trailing TP）**：本项目有追踪止损（Trailing Stop），无追踪止盈
8. **保本移动止损（Breakeven SL）**：盈利后自动推止损到成本价，无
9. **模拟盘每日重置 + 赠送练习金**：本项目 paper 模式是真实记账，无"练习模式"
10. **分析师信号频道市场**：有社交/跟单，但无"信号频道订阅 + 两键下单"形态
11. **联盟推广（Affiliate）返佣体系**：无
12. **白标（White Label）方案**：无
13. **17+ 机器人运行参数**（周末停机、最低利润、补仓开关等）：本项目网格/DCA 参数面较窄
14. **分析师/机器人透明的统计卡**（月均利润、回撤、信号数）：有 StrategyLeaderboard，但维度没这么细
15. **工作区模板**（多图表布局保存）：无

---

## 四、🔵 工程与体验差距

1. **前端测试覆盖**：项目有 218 个单测 + 29 个 E2E，但 16 个页面 301 死循环说明 E2E 没覆盖这些路由的直开场景
2. **PWA/Service Worker**：有 sw.js 但缓存策略陈旧导致用户看到旧版（本次用户"登录已过期"就是 SW 缓存的旧状态）
3. **日志 text 模式不带 fields**：排障时看不到请求路径/状态码（本次已临时切 JSON）
4. **OpenAPI 文档默认关闭**（API_DOCS_ENABLED 未设），且 /api/docs/ 404
5. **新手引导**：Dashboard 有"快速开始 3 步"，但无 CryptoRobotics 式的一对一引导/教程
6. **数据库迁移体系**：TODO 里自己标了"新表必须走 migrations/sql"，但 indicator_revenue 缺表说明有漏网

---

## 五、修复与建议优先级

**P0 全部完成 ✅（2026-10-02 验证通过）**：
1. ~~16 个前端页面 301 死循环~~ → 已修复（spa.ReadFile 直返 index.html）
2. ~~indicator_revenue 表迁移~~ → 0035_indicator_revenue.sql
3. ~~ML/RL/TensorBoard 服务启动~~ → sandbox 起 ml_server(8001) + ML_SERVER_URL 指向 sandbox
4. ~~pairlist producers 配置~~ → 默认 VolumePairList 开箱即用
5. ~~E2E 路由直开用例~~ → route-smoke.spec.ts 60+ 路由
6. ~~sw.js 缓存策略/版本~~ → 构建期时间戳版本化
7. ~~日志 text 模式 fields~~ → toFields 兼容单个 map 参数

**P1 待做**：
8. OpenAPI 文档启用（API_DOCS_ENABLED=true）并修复 /api/docs/ 404
9. pairlist 配置持久化（当前配置仅内存，重启需重配）
10. `/backtest/advanced`、`/agent-tokens` 无内容页面深入排查

**P2 对齐对标**：
7. 利润分成计费模式（对标 CryptoRobotics PSH，商业化关键）
8. Breakeven SL + Trailing TP + Candle Close SL（对标 CryptoRobotics 订单工具）
9. K 线图下单（对标 CryptoRobotics/TradingView）
10. MCP Server（对标 QuantDinger，AI Agent 生态入口）
11. 移动端 H5 适配（仪表盘/机器人监控至少可读）
12. FreqAI 式滚动自适应 ML（或先修通现有 ML 链路）
13. lookahead-analysis 回测偏差检测（对标 freqtrade，防策略造假）

---

## 附：本项目实测健康面（好的方面）

- 470 个路由，160+ GET 接口返回真实数据，无一 stub
- 全部 6 类机器人（网格/DCA/分层马丁/信号/AI/策略）前后端闭环
- 9 交易所适配器、套利、风控 12 维、对账体系、通知 6 渠道均在
- 后端 42 个包测试全绿（仓库记录），前端 218 单测

---

## 第三轮修复（2026-10-02 深夜，信号标记"蓝盒"根因）



**现象**：指标 IDE 信号标记 B/S 渲染为"蓝底白字"（klinecharts 品牌蓝 #1677FF），
预期为绿底 B / 红底 S。

**根因**：klinecharts 9.8.12 overlay 的 **text figure 默认 `backgroundColor` 为品牌蓝
#1677FF**（`styles.overlay.text.backgroundColor` 默认值）。indicatorRuntime 移植的
signalTag/line/label overlay 中 5 处 text figure 只写了 `color:'#ffffff'` 而未覆盖
backgroundColor，于是每个字母背后被库自动垫了一个蓝色小方框，正好盖住自绘的绿/红
矩形盒中心——视觉上即"蓝盒白字"。（zone overlay 因移植时自带 backgroundColor 而幸免。）

**修复**：`indicatorRuntime.ts` 5 处 overlay text figure styles 全部补
`backgroundColor: 'transparent'`。像素级验证：修复前纯蓝 974px / 绿 825 / 红 732；
修复后纯蓝 **0** / 绿 1347 / 红 1292（绿红盒完整露出），vision 目检"绿底白字 B、
红底白字 S"确认。

**排查方法备忘（重要）**：
- klinecharts canvas 分图层增量重绘：**mousemove 只重绘十字线层，overlay 层不重绘**；
  像素扫描前必须用 `chart.applyNewData(chart.getDataList())` 触发全量重绘，否则读到
  的是旧帧（本次排查多次被"幽灵像素"误导）。
- overlay 实例可在运行时通过 `chart.getChartStore().getOverlayStore().getInstances()`
  枚举，`ov.createPointFigures.toString()` 可直接核对实际执行模板；
  KlineChart 已将实例挂到 `window.__kcChart` 便于线上诊断。
- 二分定位法：把 `createPointFigures` 包一层 filter 逐类 figure（line/circle/rect/text）
  单独渲染 + 全量重绘 + 像素计数，一轮即可锁定肇事元素。

## 第四轮修复（2026-10-02 清晨，回测闭环 + P1 收尾）

**回测时间戳 bug（链路级，影响回测/因子/AI 三条线）**：
- 现象：`POST /api/backtest/run` 返回的 `start_date`/`end_date` 为 1970-01-01，
  资金曲线所有点时间戳为 0（横轴塌缩）。
- 根因（两层）：
  1. `fetchBinanceKlines` 返回 map 用 `"timestamp"` 键（int64），
     但 4 处 Bar 转换读 `"time"`（market.go RunBacktest、factors.go、ai.go ×2）→ 全为 0；
  2. 即便读对键，`getFloat(map,key,def)` 的类型分支只有 float64/string，
     int64 的 timestamp 直接落 0。
- 修复：4 处统一 `int64(getFloat(k,"timestamp",getFloat(k,"time",0)))` 双键回退；
  `getFloat`/`parseFloat` 补 int64/int 分支。
- 验证：回测 `start=2026-09-11 end=2026-10-02`，资金曲线带真实日期。

**回测手续费/滑点（freqtrade 对齐）**：
- 引擎（internal/backtest/runner.go）本就支持 Commission/Slippage/SlippageSeed，
  HTTP 层未暴露 → `RunBacktest` 新增 `commission`/`slippage`/`slippage_seed` 入参；
  前端 Backtest 页"资金与回测区间"组下新增高级参数面板（手续费率%/滑点率%/随机种子）。
- 验证：同参数回测，带费 9998.17 vs 无费 9998.30，费用生效。

**P1-8 OpenAPI 文档**：docker-compose.yml 的 environment 是白名单，
`API_DOCS_ENABLED` 从未传入容器 → 补 `- API_DOCS_ENABLED=${API_DOCS_ENABLED:-true}`，
`/api/docs` 返回 200。

**P1-9 pairlist 配置持久化**：原 ConfigurePairlist 只重建内存 manager，重启即丢。
- 新增迁移 `0036_pairlist_settings.sql`（KV 表）+ store 读写方法；
- handler 重构出 `buildManagerFromConfig/applyPairlistConfig`，
  POST 成功即持久化 JSON；新增 `RestorePairlistConfig()` 由 main() 在
  `store.InitDB()` 后显式调用（init() 阶段 db 未开，不能读持久化配置——教训）。
- 验证：保存 top_n=20 自定义配置 → 重启容器 → 白名单仍按 20 生成。

**P1-10 空页面排查结论**：
- `/agent-tokens`：页面已实现（令牌管理+审计日志双 Tab），
  后端 `/api/agent/tokens`、`/api/agent/audit-log` 均 200，空列表属正常未建令牌；
- `/backtest/advanced`：路由本不存在，以"回测页高级参数面板"形式落地（见上）。

**模拟盘（bots）链路冒烟**：`/api/ai-bots/instances` 创建 paper 模式 bot →
start → running，盈亏/回撤/夏普字段齐全。完整 信号→下单→成交 验证待 AI 策略
catalog  seeded 后长时运行观察（catalog 当前为空，是下一步）。

**遗留**：AI bot catalog 为空（AI 策略模板未注入，影响 AI bot 实盘信号质量）；
P2 功能差距未动（止损三件套/图表下单/利润分成/MCP/移动端）。

## 第五轮修复（2026-10-02 上午，AI bot catalog 恒空根因）

**现象**：`/api/ai-bots/catalog` 恒返回 `[]`，DB 里种子数据 17 条完好。

**根因**：`scanAIBotCatalogRows` 用裸 `string` 承接 `performance_json`，而种子数据该
列为 NULL → 每行 Scan 报错被 `continue` 静默跳过 → 列表恒空。instance 扫描函数
同病（`error_message`/`exchange_id` 为 NULL 时实例从列表消失）。

**修复**：catalog 两个扫描函数 + instance 两个扫描函数的 `performance_json`/
`config_json`/`error_message`/`exchange_id` 改 `sql.NullString`，输出经 `nullStr()`
归一；新增 `nullStr` helper。教训：**SQLite Scan 的 `err → continue` 是静默吞数据
重灾区，凡可空列必须用 Null* 类型**。

**验证**：catalog 返回 14 条（Optimus/CyberBot/AI Alpha 等）；创建 catalog 实例
→ start → running → 快照表按分钟落数；模拟盘只在 AI 信号（置信度≥阈值）时开仓
（by design），信号链路由 ai_gate 供数。

**排查工具备忘**：gateway.db 是 WAL 模式，`docker cp` 只拷 `gateway.db` 会丢未
checkpoint 的数据，必须连 `-wal`/`-shm` 一起拷。

## 第六轮修复（2026-10-02 上午，止损三件套之 K 线收盘止损）

**盘点结论**：三件套中 Trailing TP 与 Breakeven SL **早已存在**——
executor_tpsl.go 有完整实现（TP1 触发后 trailing_pct 回撤全平 + move_sl_after/
move_sl_to 移动止损到保本位）；order/ladder.go 有 breakeven_after_target。
真正缺的是 **Candle Close SL（K 线收盘止损，防插针）**，全仓零匹配。

**实现**（迁移 0037 + store/handler 四层贯通）：
- `xt_signal_executions` 新增 `candle_close_sl` / `candle_close_interval`（默认 1m）；
- `SignalExecution` 结构体 + sigExecCols + scan/Create/Update 全量接线；
- 创建入口 `executor_signal.go` 解析 `candle_close_sl`/`candle_close_interval`；
- `evalTPSL`：开启后盘中 tick 不再触发 SL；
- 新增 `TickExecutorBarClose(symbol, barClosePrice)`：收盘价跌破（涨破）CurrentSL 才平仓；
- `tpslPriceLoop` 每 5s 轮询开启该模式的 symbol 的最近 K 线（取倒数第二根已收盘柱），
  按 symbol|interval 去重，新收盘柱才判定一次。

**验证**：编译通过、迁移生效（两列存在）、容器 healthy。
行为语义：盘中插针跌破止损线不触发；只有判定周期 K 线收盘价跌破才平仓。
信号源在 execute 请求体带 `candle_close_sl:true, "candle_close_interval":"5m"` 即启用。

**遗留**：前端执行面板暂无手动触发入口（面板是只读监控），参数经信号 API 传入；
TickExecutorBarClose 已导出可单测，建议后续补 table-driven 测试。
