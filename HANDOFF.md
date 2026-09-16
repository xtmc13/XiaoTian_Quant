# HANDOFF — 会话交接文档（2026-09-16）

> 本文档供新会话（尤其是服务器上的 Kimi Code CLI 实例）快速接手。
> 规则：读完本文 + TODO.md 即可继续工作；工作过程中持续更新这两份文档。

## 一句话现状

XiaoTian_Quant（小天量化 v3.0）已完成"从用不了到全链路真实数据跑通"：
真实币安行情/权益 → MACD/CRA 策略信号 → paper 撮合成交；网格机器人上线；
前端部署；GitHub 同步。当前主部署在服务器（见下）。

## 部署拓扑

| 环境 | 位置 | 用途 |
|---|---|---|
| 主服务器 | 43.165.179.199（ubuntu，x86_64，网络自由） | 生产。网关:8080、量化UI:8088、Claude UI:3000 |
| 手机本机 | /tmp/XiaoTian_Quant（proot 容器，arm64） | 开发副本。本机代理极不稳定，仅应急 |

- 服务器项目：`~/XiaoTian_Quant`（git main 分支，`git pull` 即最新）
- 服务器登录：admin / admin123；配置/密钥在 `gateway/config.yaml` 与 `.env`（均未入库，勿提交）
- 重启网关（SSH 易断，必须用 setsid）：`setsid ~/restart-gw.sh < /dev/null > /dev/null 2>&1 &`
- 服务器已装：Go 1.25.3（~/go）、nginx、Claude Code（Kimi K2.8）、lanyuncodingui(:3000)

## 项目方向（已定，勿偏离）

**中文用户、单二进制自托管、模拟盘即开即用、机器人订阅分成的量化平台。**
- P0 全部完成（TODO.md 第 1-4 条已勾）。下一阶段：网格机器人做成市场 MVP。
- **明确不做**：Rust FFI 接入、AI/ML 价格预测、链上、社区、多券商。
- 安全红线：一切下单默认 paper（`execution_mode=paper`）；`config.yaml` 的
  `risk.position_limit_pct: 2500` 是 paper 时期的放宽值，**开实盘前必须回调**；
  `dry_run` 开关同理。

## 用户偏好与约定

- 中文交流，回复简洁直接。
- 用户实盘标准：币安账户支持 **150x 杠杆、1U 起下单**（CRA 校验已按此放宽）。
- 用户的 222/333 策略风控参数（TP 1.3% / SL 40%）**用户明确说不动**，不要再劝。
- 纪律：小步提交（commit message 中文、写实测证据）；schema 变更走
  `gateway/internal/store/migrations/sql/*.sql`；禁止 mock（要么真数据要么下线）。

## 当前进行中的事（接手后按此继续）

1. ~~**333 修复**~~ ✅ 2026-09-16 完成。修复脚本已执行（策略类型→cra_contract、强制 paper、
   参数保留），且揪出并修复了一个 P1 死锁：dispatch 持 bus RLock 调 handler，信号→下单
   重入 Publish 与并发 Subscribe 成环（Start 请求永久挂死、174 个 goroutine 堆积、SIGTERM
   都杀不掉）。修法=event.dispatch 快照订阅后放锁再调 handler + Engine 补订阅退订。
   已验证：333/222/MACD 启动均 <12ms 返回，333 全链路（首根 15m K线→cra 首单→paper FILLED）实测跑通。
   详见 TODO.md 验证记录末节。
2. **待用户答复**：333 是在 UI 哪个页面菜单创建的（要堵保存暗道，需知道入口；
   请求日志不记路径，这是已知观测缺口，可考虑给 RequestLogger 加 path 字段）。
3. **待用户操作**：云控制台安全组放行 TCP 8088 和 3000。

## 已知坑（血泪史，别再踩）

- `pkill -f "xxx"` 会匹配到自己的命令行 → shell 自杀、后续命令不执行。用 `pkill -x 进程名`。
- 启动网关必须显式 `PORT=8080`：若 shell 环境自带 PORT（如从 Claude UI 的 node 环境带出的
  PORT=3000），config.yaml 无 server.port 时会fallback到 env，撞车 3000 直接启动失败。
- SSH 到该服务器会随机断连；重启服务一律 `setsid` 脱离会话；连续断连会触发
  fail2ban 封 IP（约几十分钟~几小时自动解，别高频重试刷新计时）。
- 旧进程残留导致"新代码不生效"的假象：改完代码先 `pgrep -x gateway` 核对进程
  启动时间 vs 二进制 mtime，再验证。
- 事件总线多 worker 并发分发不保序——K 线等有序序列必须用 `PublishSync`。
- **权益口径（2026-09-16 起）**：`TotalEquity()`=真实交易所权益（paper 账户已排除）；
  `PaperEquity()`=模拟账本。风控上下文按订单执行目标选基准（paper 单用 paper 权益）。
  真实币安账户当前只有 ~0.2U，权益显示个位数是正常的，不是 bug。
- 币安 WS 客户端以前会发"伪 1m Bar"（ticker 伪造），已移除；别恢复。
- 手机 proot 环境：`/usr/local/go` 曾有 2024 旧版残留污染（ZeroValSize 重定义），
  装 Go 前必须 `rm -rf /usr/local/go`；绑定 <1024 端口会被 proot 拒绝（nginx 用 8088）。

## 关键文件地图

- 活文档：`TODO.md`（清单+验证记录，持续更新）
- 网格机器人：`internal/grid/`（engine/runner）、`handler/grid_bot.go`、`migrations/sql/0002`
- K线供给管：`internal/market/kline_feeder.go`（回补+PublishSync）
- 策略执行链：`internal/strategy/`（engine+firstbar 观测）、`internal/app/context.go`（信号→下单）
- 响应包装器：`internal/middleware/response_wrapper.go`（曾吞 body，已修，改时注意透传）
- 前端：`web/src/pages/bots/BotsGrid.tsx`（网格页）

—— 交接完毕。新会话从这里继续。
