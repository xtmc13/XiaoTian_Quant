# RUNBOOK：支付/短信闭环验证（Stripe + Twilio）

适用范围：`gateway/`（Go 后端）。目标：用 Stripe test mode 与 Twilio 测试凭证，把
**Stripe Checkout 支付闭环** 与 **Twilio 短信通知闭环** 各走通一遍，确认订单状态机、
积分发放、webhook 验签、短信收发与通知路由均符合预期。

标注约定：**【需密钥】** = 步骤需要由执行者提供真实密钥/账号（Stripe test mode 或
Twilio 控制台），本仓库与验证包均不提供。

## 0. 自动化验证包（无密钥，先跑）

```bash
bash scripts/verify-payments.sh
```

覆盖并已绿（2026-09-20 验证）：

- ✅ Stripe webhook 验签与官方 v1 算法对齐：`HMAC-SHA256(whsec, "t.payload")`，
  多 `v1` 签名任一匹配即通过（密钥轮换场景），时间戳容忍 ±300s；
  正确签名通过 / 篡改 payload 拒绝 / 错误密钥拒绝 / 非 hex v1 拒绝 / 过期时间戳拒绝。
- ✅ webhook 状态机：`checkout.session.completed` → 订单 `paid` + 积分发放（幂等，不重复发放）；
  `checkout.session.async_payment_failed` / `payment_intent.payment_failed` → 订单 `failed`；
  已支付订单不被失败事件回退；未知事件 200 ignored。
- ✅ Checkout 表单构造：`metadata[order_id]` 回链、`price_data` 内联价 / `STRIPE_PRICE_ID_*`
  映射、同用户同套餐 pending 订单复用（不重复下单）。
- ✅ Twilio 请求构造：POST + `application/x-www-form-urlencoded` + Basic Auth(SID:token) +
  `/2010-04-01/Accounts/<SID>/Messages.json` endpoint + To/From/Body 表单 + Markdown 剥离 +
  1600 字符截断（UTF-8 安全）+ JSON 错误（code 21211 等）与非 JSON 错误解析 + 连接失败包装。

## 1. 通用环境信息（观测点速查）

| 项 | 值 |
|---|---|
| Gateway 默认端口 | `8080`（`PORT` 或 config `server.port` 覆盖） |
| 数据库 | `./runtime/gateway.db`（`DB_PATH` 覆盖，WAL 模式，可直接 `sqlite3` 只读查询） |
| 订单表 | `billing_orders`（`status`/`fail_reason`/`confirmed_at`/`tx_hash`） |
| 积分/会员 | `xt_users` 表（`credits`、`plan`、`vip_expires_at`） |
| Stripe webhook 路由 | `POST /api/billing/stripe/webhook`（公开，验签替代登录） |
| 通知日志关键字 | `[Notify] sms error: ...`（异步失败）、`[Notify:sms] ...`（log 渠道回显）、`[Notify] Queue full, dropping` |
| 通知历史 | `GET /api/notifications`（登录后） |
| 前端页面 | Web UI `/billing`（浏览器标题“会员 - 小天量化”），通知设置页可查看渠道与路由 |

启动本地 gateway（Stripe/Twilio 环境变量见各章节）：

```bash
cd gateway && go run ./cmd/server
```

## 2. Stripe 支付闭环

前置环境变量（gateway 进程）：

| 变量 | 说明 |
|---|---|
| `STRIPE_SECRET_KEY` | `sk_test_...`，Checkout 创建用 |
| `STRIPE_PUBLISHABLE_KEY` | `pk_test_...`，`/api/billing/stripe/config` 透传给前端 |
| `STRIPE_WEBHOOK_SECRET` | `whsec_...`，验签用（stripe CLI 场景用 CLI 打印的那个） |
| `STRIPE_SUCCESS_URL` / `STRIPE_CANCEL_URL` | 支付完成/取消回跳地址（前端不传时兜底） |

### S1. 准备 Stripe test mode 凭证 —— 【需密钥】

1. <https://dashboard.stripe.com/test/dashboard> 确认右上角开关为 **Test mode**。
2. Developers → API keys：复制 `sk_test_...` 与 `pk_test_...`。
3. 写入启动环境并重启 gateway。

- 预期观测：`curl -s localhost:8080/api/billing/stripe/config`（无需登录）返回
  `{"enabled":true,"publishable_key":"pk_test_..."}`；未配置时 `enabled:false` 且前端不显示信用卡按钮。
- 回滚：清空 `STRIPE_SECRET_KEY` 等变量重启 → 通道自动禁用（`enabled:false`）。

### S2. webhook 本地转发 —— 【需密钥】

1. 安装 [stripe CLI](https://docs.stripe.com/stripe-cli) 并 `stripe login`。
2. 终端 A 启动转发，**记录打印的 `whsec_...`（CLI 为该会话生成的签名密钥）**：

   ```bash
   stripe listen --forward-to localhost:8080/api/billing/stripe/webhook
   ```

3. 把该 `whsec_...` 设为 `STRIPE_WEBHOOK_SECRET` 并重启 gateway。

- 预期观测：CLI 打印 `Ready!`；`stripe trigger checkout.session.completed` 后
  CLI 显示事件被转发且收到 HTTP 200；gateway 返回体为
  `{"received":true,"ignored":"order not found"}`（触发器事件的 metadata 无本系统订单号，
  属预期——真实订单见 S3）。
- 回滚：`Ctrl-C` 停掉 CLI；`STRIPE_WEBHOOK_SECRET` 置空后 webhook 返回 503（通道关闭）。

### S3. 4242 测试卡走完整 Checkout —— 【需密钥】

1. 登录拿 JWT：`curl -s -X POST localhost:8080/api/login -H 'Content-Type: application/json' \
   -d '{"username":"<u>","password":"<p>"}'` → `token`。
2. 创建收银台：

   ```bash
   curl -s -X POST localhost:8080/api/billing/stripe/checkout \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"plan_id":"monthly","success_url":"http://localhost:5173/billing?ok=1","cancel_url":"http://localhost:5173/billing?ok=0"}'
   ```

3. 浏览器打开返回的 `checkout_url`，用测试卡支付：
   卡号 `4242 4242 4242 4242`，有效期任意未来月份，CVC/ZIP 任意。
4. 观测点（支付完成后 1–2 秒内）：
   - **stripe CLI**：`checkout.session.completed` 事件显示 `200`；
   - **HTTP**：gateway 对该事件返回 `{"received":true}`；
   - **数据库**：`sqlite3 runtime/gateway.db "select id,plan_id,status,confirmed_at from billing_orders where chain='stripe' order by created_at desc limit 1"`
     → `status` 由 `pending` 变为 `paid`，`confirmed_at` 有值；
     `sqlite3 runtime/gateway.db "select credits,plan,vip_expires_at from xt_users where id=<uid>"`
     → `credits` 增加套餐积分（monthly=500），`vip_expires_at` 延长；
   - **UI**：Web `/billing` 页会员状态变为已开通。
5. 幂等验证：`stripe events list --limit 3` 找该事件 ID 后
   `stripe events resend <evt_id>`；再查 `xt_users.credits` **不重复增加**，
   订单仍 `paid`。
- 回滚（本地验证数据清理）：
  `sqlite3 runtime/gateway.db "update billing_orders set status='pending', confirmed_at=0 where id='<order_id>'; update xt_users set credits=credits-<n>, vip_expires_at=0 where id=<uid>;"`
  （或整行删除测试订单。）

### S4. 支付失败状态机 —— 【需密钥】

1. 重新走 S3 第 2 步生成一个新 pending 订单（记下 `order_id`，
   可在响应/数据库中取得——重复 checkout 会复用同套餐 pending 单）。
2. 触发失败事件：

   ```bash
   stripe trigger checkout.session.async_payment_failed
   # 部分 CLI 版本无此 fixture 时改用：
   stripe trigger payment_intent.payment_failed
   ```

   注意：trigger 生成的事件 metadata 不含本系统 `order_id`，会被忽略
   （`{"received":true,"ignored":"order not found"}`）。
   **精确验证方式**：在 Stripe Dashboard → Developers → Webhooks（或 CLI 转发窗口）
   对真实订单事件点 “Resend”，或对 S3 已支付订单重放失败事件——已支付订单必须
   保持 `paid`（单元测试已覆盖该路径；本步骤验证真实事件流同样不回归）。
3. 观测点：`billing_orders.status='failed'` 且 `fail_reason` 含“Stripe 支付失败”
   （仅当事件带本系统 order_id 时）；已支付订单 `status` 不变。
- 回滚：同 S3 SQL 清理。

### S5. 验签拒绝观测（安全面抽查）—— 【需密钥】

```bash
curl -s -X POST localhost:8080/api/billing/stripe/webhook \
  -H 'Stripe-Signature: t=1,v1=deadbeef' -d '{"type":"checkout.session.completed"}'
```

- 预期观测：HTTP 400 `{"error":"签名校验失败"}`；数据库无任何订单状态变化。
- 回滚：无需（只读探测）。

## 3. Twilio 短信闭环

前置环境变量（gateway 进程，改动后需重启）：

| 变量 | 说明 |
|---|---|
| `TWILIO_ACCOUNT_SID` / `TWILIO_AUTH_TOKEN` | Basic Auth；控制台 Test Credentials 的 SID/Token 即可 |
| `TWILIO_FROM_NUMBER` | 发信号码（E.164，如 `+15551234567`）；缺省整个 sms 渠道禁用 |
| `TWILIO_TO_NUMBER` | 默认接收号码；路由规则未指定 `sms_to` 时使用 |

### T1. 开通 Twilio 并配置 —— 【需密钥】

1. <https://console.twilio.com/> 注册登录；Phone Numbers → Buy a number
   （测试号即可，需 SMS 能力）。
2. 复制 **Test Credentials**（Account 菜单下，非 Live Credentials）的
   `AC...` / `auth token`，连同所购号码写入上述环境变量重启 gateway。
- 预期观测：`curl -s localhost:8080/api/notify/channels -H "Authorization: Bearer $TOKEN"`
  中 `sms` 渠道 `enabled:true`。
- 回滚：清空 `TWILIO_*` 重启 → 渠道 `enabled:false`；Twilio 控制台可 Release 号码。

### T2. 成功路径：magic number +15005550006 —— 【需密钥】

> 注意：Test Credentials 只**校验并返回响应**，不真实投递短信（不产生话费）。
> 要真实收到短信，需用 Live Credentials（见 T2b）。

```bash
curl -s -X POST localhost:8080/api/notify/test \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"channel":"sms","message":"twilio success path probe"}'
```

（需先把 `TWILIO_TO_NUMBER` 临时设为 `+15005550006`。注意：`/api/notify/test`
的 `channel` 字段仅作消息标签，实际会向**所有已启用渠道**同步发送；只配置
Twilio 时即 log + sms 两个渠道，接收号码取 `TWILIO_TO_NUMBER`。）

- 预期观测：HTTP 200 `{"status":"sent","channel":"sms"}`（任一渠道失败则
  `status:"partial"` 且 `errors` 列出各渠道错误）；
  日志关键字 `[Notify:sms]`（log 渠道回显）；
  Twilio 控制台 Monitor → Logs → Messages 出现一条 **test credential** 记录，状态 queued。
- 回滚：无需（探测消息）；置回 `TWILIO_TO_NUMBER`。

### T2b. （可选）Live 真实投递 —— 【需密钥】

改用 Live Credentials + 已购号码作为 `TWILIO_FROM_NUMBER`，`TWILIO_TO_NUMBER` 设为你的
真实手机（E.164，如 `+8613xxxxxxx`）。重跑 T2：手机应真实收到短信。此步产生短信费用
（约 $0.0079/条，美国费率，以控制台为准）。
- 回滚：切回 Test Credentials。

### T3. 失败路径：magic number +15005550001 —— 【需密钥】

把 `TWILIO_TO_NUMBER` 临时设为 `+15005550001` 后重跑 T2。

- 预期观测：HTTP 200 但
  `{"status":"partial","channel":"sms","errors":["twilio: status 400: ... (code 21211)"]}`
  （21211 = Invalid 'To' Phone Number，见 [Twilio 官方 magic number 文档](https://www.twilio.com/docs/iam/test-credentials)）；
  日志关键字 `[Notify] sms error: ... 21211`；`GET /api/notifications` 历史仍有该条记录；
  metrics `notify_send_total{channel="sms",result="failure"}` 增加（若启用 Prometheus）。
- 回滚：置回 `TWILIO_TO_NUMBER`。

### T4. 通知路由规则引擎观测

1. `curl -s localhost:8080/api/notify/routes -H "Authorization: Bearer $TOKEN"`：
   默认规则 `default-critical`（Levels=[CRITICAL]，Channels 含 `sms`）应可见。
2. 触发一条 CRITICAL 级系统通知（走事件总线 → Broadcaster → 路由 → Manager）：
   在已有持仓/策略上构造一次 CRITICAL 风控事件（或 `POST /api/notify/send`，
   `{"title":"probe","content":"sms route probe","level":"CRITICAL"}`）。
3. 观测点：
   - sms 渠道被调用（T2 已配置成功路径时日志/历史可见）；
   - `GET /api/notify/routes` 返回的规则中 `sms` 出现在 `default-critical` 的 Channels。

> ⚠️ 已知行为（2026-09-20 核实）：`Broadcaster.Broadcast` 会把路由结果写入消息的
> `_channels` tag，但 `Manager.Send/SendSync/worker` 目前**未按 `_channels` 过滤**，
> 投递范围 = 所有已启用渠道。即：规则引擎的“渠道选择”当前不限制实际投递，
> sms 是否收到消息取决于“sms 渠道是否启用”，而非路由表。修复属功能变更，不在本轮范围；
> 观测时以“sms 渠道收到/未收到消息 + `[Notify] sms error` 日志”为准。

- 回滚：无需。

## 4. 收尾检查清单

- [ ] Stripe：S3 后订单 `paid` + 积分增加 + UI 显示会员；重放事件不重复发放。
- [ ] Stripe：S5 伪造签名 400。
- [ ] Twilio：T2 200 sent；T3 partial + 21211；通知历史均有记录。
- [ ] 回滚所有 `TWILIO_TO_NUMBER` 临时改动，恢复 Test Credentials。
- [ ] 测试订单/积分按需用第 2 节 SQL 清理。

## 5. 排查速查

| 现象 | 排查 |
|---|---|
| webhook 一直 503 | `STRIPE_WEBHOOK_SECRET` 未配置或 gateway 未重启 |
| webhook 400 签名校验失败 | `STRIPE_WEBHOOK_SECRET` 与 `stripe listen` 打印的 `whsec_...` 不一致（CLI 每次会话生成新密钥） |
| checkout 返回 503 | `STRIPE_SECRET_KEY` 未配置 |
| 支付成功但订单未 paid | 看 stripe CLI 事件是否 200；`billing_orders` 该单 `chain` 是否 `stripe`、`status` 是否 `pending` |
| sms 渠道 enabled:false | 缺 `TWILIO_FROM_NUMBER`（三者 SID/Token/From 缺一不可） |
| 短信报 `no recipient` | `TWILIO_TO_NUMBER` 与消息 `sms_to` tag 均为空 |
| `+15005550006` 也报 21211 | 检查是否错把 magic number 填到了 `TWILIO_FROM_NUMBER`；To 才是触发位 |
