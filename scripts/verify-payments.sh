#!/bin/bash
# XiaoTianQuant 支付/通知通道验证包 —— 自动化部分一键执行
# 运行方式: bash scripts/verify-payments.sh
#
# 覆盖范围（无需任何真实密钥，全部离线可跑）:
#   1. gofmt 检查本轮触碰的 Go 文件
#   2. CGO_ENABLED=0 go build ./...（gateway 全量编译；CGO=0 走纯 Go 撮合引擎路径）
#   3. Stripe webhook 验签/下单/状态机单测（internal/handler）
#   4. Twilio 短信通道单测（internal/notify）
#
# 跑完后脚本会列出仍需人工执行的真实闭环步骤（密钥相关步骤带【需密钥】标注），
# 详细操作见 docs/RUNBOOK-支付闭环验证.md。

set -u
set -o pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

PASS=0
FAIL=0

pass() { echo -e "${GREEN}✓${NC} $1"; ((PASS++)); }
fail() { echo -e "${RED}✗${NC} $1"; ((FAIL++)); }

# 定位仓库根与 gateway 目录
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATEWAY_DIR="$ROOT_DIR/gateway"

# go 不在 PATH 时尝试常见安装位置
if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
  export PATH="$PATH:/usr/local/go/bin"
fi
if ! command -v go >/dev/null 2>&1; then
  fail "未找到 go，请先安装 Go 1.25+ 或 export PATH=\$PATH:/usr/local/go/bin"
  exit 1
fi

echo -e "${CYAN}═══ XiaoTianQuant 支付/通知验证包（自动化部分）═══${NC}"
echo "仓库根: $ROOT_DIR"
go version

# ── 1. gofmt：本轮触碰的文件必须格式干净 ──────────────────────
echo
echo -e "${CYAN}[1/4] gofmt 格式检查${NC}"
FMT_FILES=(
  "internal/handler/billing_stripe.go"
  "internal/handler/billing_test.go"
  "internal/notify/twilio_test.go"
)
FMT_BAD=0
for f in "${FMT_FILES[@]}"; do
  if [ -f "$GATEWAY_DIR/$f" ]; then
    if ! gofmt -l "$GATEWAY_DIR/$f" | grep -q .; then
      pass "gofmt $f"
    else
      fail "gofmt $f （需要 gofmt -w）"
      FMT_BAD=1
    fi
  else
    fail "文件缺失: $f"
    FMT_BAD=1
  fi
done

# ── 2. 全量编译（纯 Go 路径）─────────────────────────────────
echo
echo -e "${CYAN}[2/4] CGO_ENABLED=0 go build ./...${NC}"
if (cd "$GATEWAY_DIR" && CGO_ENABLED=0 go build ./...); then
  pass "gateway 全量编译通过（CGO_ENABLED=0）"
else
  fail "gateway 编译失败"
fi

# ── 3. Stripe 支付链路单测 ────────────────────────────────────
echo
echo -e "${CYAN}[3/4] Stripe webhook/checkout 单测 (internal/handler)${NC}"
if (cd "$GATEWAY_DIR" && CGO_ENABLED=0 go test ./internal/handler/ -run 'Stripe|VerifyStripe' -count=1); then
  pass "Stripe 验签/篡改拒绝/过期时间戳/下单/失败事件/幂等 全部通过"
else
  fail "Stripe 相关单测失败"
fi

# ── 4. Twilio 短信通道单测 ────────────────────────────────────
echo
echo -e "${CYAN}[4/4] Twilio 短信通道单测 (internal/notify)${NC}"
if (cd "$GATEWAY_DIR" && CGO_ENABLED=0 go test ./internal/notify/ -count=1); then
  pass "Twilio 请求构造/Basic Auth/表单/错误解析/截断 全部通过"
else
  fail "Twilio 相关单测失败"
fi

# ── 汇总 + 剩余人工步骤清单 ───────────────────────────────────
echo
echo -e "${CYAN}═══ 自动化结果汇总 ═══${NC}"
echo -e "通过: ${GREEN}$PASS${NC}  失败: ${RED}$FAIL${NC}"
echo
echo -e "${CYAN}═══ 剩余人工闭环步骤（详见 docs/RUNBOOK-支付闭环验证.md）═══${NC}"
cat <<'EOF'
Stripe 支付闭环:
  ✅ 验签算法与官方 v1 对齐（多 v1 轮换/篡改/过期拒绝）—— 已自动化
  ✅ Checkout 表单构造（metadata 回链/价格映射）—— 已自动化
  ⏳ 【需密钥】Stripe 开通 test mode，拿 sk_test_*/whsec_*
  ⏳ 【需密钥】stripe CLI listen --forward 把 webhook 转发到本地 gateway
  ⏳ 【需密钥】4242 测试卡走一遍 Checkout，观察订单 pending → paid + 积分发放
  ⏳ 【需密钥】stripe trigger checkout.session.async_payment_failed 验证 failed 状态机

Twilio 短信闭环:
  ✅ 请求构造/Basic Auth/表单/错误解析 —— 已自动化
  ⏳ 【需密钥】Twilio 购买号码，配置 TWILIO_ACCOUNT_SID/AUTH_TOKEN/FROM_NUMBER
  ⏳ 【需密钥】magic number +15005550006 测成功路径（真实收到短信）
  ⏳ 【需密钥】magic number +15005550001 测失败路径（错误码 21261 上报告警）
  ⏳ 通知路由规则引擎：POST /api/notify/test {"channel":"sms"} 触发一条，确认 sms 渠道在路由表中
EOF

echo
if [ "$FAIL" -eq 0 ]; then
  echo -e "${GREEN}自动化验证包全部通过 ✅  剩余步骤均需真实密钥，请按 runbook 执行。${NC}"
  exit 0
else
  echo -e "${RED}存在失败项，请先修复再进入人工闭环。${NC}"
  exit 1
fi
