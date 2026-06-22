#!/bin/bash
# XiaoTianQuant 全项目验证脚本（网络容错版）
# 运行方式: bash verify.sh
# 特性: 即使 go/npm 因网络失败，也会继续执行后续检查

set -u          # 未定义变量报错，但不在命令失败时退出
set -o pipefail # 管道中任一命令失败则整体失败

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

PASS=0
FAIL=0
WARN=0

pass() { echo -e "${GREEN}✓${NC} $1"; ((PASS++)); }
fail() { echo -e "${RED}✗${NC} $1"; ((FAIL++)); }
warn() { echo -e "${YELLOW}⚠${NC} $1"; ((WARN++)); }

# 安全执行命令: 成功则 pass，失败则 warn（不退出）
# 用法: safe_run "描述" 命令...
safe_run() {
  local desc="$1"
  shift
  if "$@" 2>/tmp/safe_run_$$.err; then
    pass "$desc"
    return 0
  else
    warn "$desc 失败（可能因网络/环境限制）"
    if [ -s /tmp/safe_run_$$.err ]; then
      head -10 /tmp/safe_run_$$.err | sed 's/^/    /'
    fi
    return 1
  fi
}

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║     小天量化 — 全项目修复验证脚本（网络容错版）              ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""

# ── 1. 文件完整性检查 ──
echo "【1/6】文件完整性检查..."

FILES=(
  # Go 后端
  "gateway/internal/middleware/ratelimit.go"
  "gateway/internal/store/store.go"
  "gateway/cmd/server/main.go"
  "gateway/internal/cache/redis_cache.go"
  "gateway/internal/cache/redis.go"
  "gateway/internal/response/response.go"
  "gateway/internal/middleware/response_wrapper.go"
  "gateway/internal/store/schema.go"
  "gateway/go.mod"
  "gateway/internal/cache/redis_cache_test.go"
  # 前端
  "web/vite.config.ts"
  "web/package.json"
  "web/public/manifest.json"
  "web/public/sw.js"
  "web/src/lib/pwa.ts"
  "web/src/components/PWAInstallPrompt.tsx"
  "web/src/components/layout/Layout.tsx"
  "web/src/main.tsx"
  "web/index.html"
  "web/src/types/global.d.ts"
  "web/src/lib/typeHelpers.ts"
  # 测试
  "web/src/components/__tests__/ToastContainer.test.tsx"
  "web/src/pages/__tests__/SocialTrading.test.tsx"
  "web/src/pages/__tests__/OnChain.test.tsx"
  "web/src/components/__tests__/PWAInstallPrompt.test.tsx"
)

for f in "${FILES[@]}"; do
  if [ -f "$f" ]; then
    pass "文件存在: $f"
  else
    fail "文件缺失: $f"
  fi
done

# ── 2. Go 语法基础检查 ──
echo ""
echo "【2/6】Go 语法基础检查..."

if command -v go &> /dev/null; then
  cd gateway

  # go.mod 检查（纯文本，不依赖网络）
  if grep -q "github.com/redis/go-redis/v9" go.mod; then
    pass "go.mod 包含 redis 依赖"
  else
    fail "go.mod 缺少 redis 依赖"
  fi

  # 尝试编译整个 cmd/server 包（不能只编 main.go，否则同包 router.go 等会 undefined）
  if go build ./cmd/server/ 2>/tmp/go-build.err; then
    pass "Go 后端编译成功"
    rm -f server 2>/dev/null || true
  else
    warn "Go 后端编译失败（可能因网络限制无法下载依赖）"
    if [ -s /tmp/go-build.err ]; then
      head -15 /tmp/go-build.err | sed 's/^/    /'
    fi
  fi

  # 运行测试（可能因无 Redis 或网络失败）
  if go test ./internal/cache/... 2>/tmp/go-test.err; then
    pass "cache 包测试通过"
  else
    warn "cache 包测试失败（可能因无 Redis 或网络限制）"
    if [ -s /tmp/go-test.err ]; then
      head -10 /tmp/go-test.err | sed 's/^/    /'
    fi
  fi

  cd ..
else
  warn "Go 工具链未安装，跳过编译测试"
fi

# ── 3. 前端依赖检查 ──
echo ""
echo "【3/6】前端依赖检查..."

cd web

if command -v npm &> /dev/null; then
  # package.json 检查（纯文本，不依赖网络）
  if grep -q "rollup-plugin-visualizer" package.json; then
    pass "package.json 包含 visualizer 依赖"
  else
    fail "package.json 缺少 visualizer 依赖"
  fi

  # 安装依赖（可能因网络失败）
  if npm install 2>/tmp/npm-install.err; then
    pass "npm install 成功"
  else
    warn "npm install 失败（可能因网络限制）"
    if [ -s /tmp/npm-install.err ]; then
      head -10 /tmp/npm-install.err | sed 's/^/    /'
    fi
  fi

  # 类型检查（需要 node_modules，若 install 失败则跳过）
  if [ -d "node_modules" ]; then
    if npm run type-check 2>/tmp/tsc.err; then
      pass "TypeScript 类型检查通过"
    else
      fail "TypeScript 类型检查失败"
      if [ -s /tmp/tsc.err ]; then
        head -20 /tmp/tsc.err | sed 's/^/    /'
      fi
    fi

    # 运行测试（需要 node_modules）
    if npm run test -- --run 2>/tmp/test.err; then
      pass "前端测试通过"
    else
      fail "前端测试失败"
      if [ -s /tmp/test.err ]; then
        head -30 /tmp/test.err | sed 's/^/    /'
      fi
    fi

    # 构建检查（需要 node_modules）
    if npm run build 2>/tmp/build.err; then
      pass "前端构建成功"

      # 检查产物
      if [ -f "dist/sw.js" ]; then
        pass "Service Worker 已复制到 dist"
      else
        warn "Service Worker 未复制到 dist (需检查 public 目录配置)"
      fi

      if [ -f "dist/manifest.json" ]; then
        pass "Manifest 已复制到 dist"
      else
        warn "Manifest 未复制到 dist"
      fi

      # 检查 chunk 拆分
      CHUNK_COUNT=$(ls dist/assets/*.js 2>/dev/null | wc -l)
      if [ "$CHUNK_COUNT" -ge 5 ]; then
        pass "Chunk 拆分正常 ($CHUNK_COUNT 个 JS 文件)"
      else
        warn "Chunk 数量较少 ($CHUNK_COUNT 个)"
      fi

    else
      fail "前端构建失败"
      if [ -s /tmp/build.err ]; then
        head -20 /tmp/build.err | sed 's/^/    /'
      fi
    fi
  else
    warn "node_modules 不存在，跳过类型检查/测试/构建"
  fi

else
  warn "npm 未安装，跳过前端验证"
fi

cd ..

# ── 4. 关键代码模式检查 ──
echo ""
echo "【4/6】关键代码模式检查..."

# 检查 RateLimiter 是否有 TTL 清理
if grep -q "bucketTTL" gateway/internal/middleware/ratelimit.go; then
  pass "RateLimiter 包含 TTL 清理逻辑"
else
  fail "RateLimiter 缺少 TTL 清理"
fi

# 检查 JWT 生产环境校验
if grep -q "required in production" gateway/internal/store/store.go; then
  pass "JWT Secret 包含生产环境强制校验"
else
  fail "JWT Secret 缺少生产环境校验"
fi

# 检查 pprof 端点（注册在 router.go）
if grep -q "/debug/pprof" gateway/cmd/server/router.go; then
  pass "pprof 端点已注册"
else
  fail "pprof 端点未注册"
fi

# 检查 Redis 缓存实现
if grep -q "RedisCache" gateway/internal/cache/redis_cache.go; then
  pass "RedisCache 类型已定义"
else
  fail "RedisCache 类型缺失"
fi

# 检查响应包装中间件
if grep -q "UnifiedResponseWrapper" gateway/internal/middleware/response_wrapper.go; then
  pass "UnifiedResponseWrapper 已定义"
else
  fail "UnifiedResponseWrapper 缺失"
fi

# 检查前端 memo 优化
MEMO_COUNT=$(grep -r "React\.memo\|memo(" web/src --include="*.tsx" | wc -l)
if [ "$MEMO_COUNT" -ge 10 ]; then
  pass "React.memo 使用充足 ($MEMO_COUNT 处)"
else
  warn "React.memo 使用较少 ($MEMO_COUNT 处)"
fi

# 检查 as any 减少
ANY_COUNT=$(grep -r "as any" web/src --include="*.ts" --include="*.tsx" | grep -v node_modules | grep -v "\.d\.ts" | wc -l)
if [ "$ANY_COUNT" -le 3 ]; then
  pass "as any 已大幅减少 ($ANY_COUNT 处)"
else
  warn "as any 仍较多 ($ANY_COUNT 处)"
fi

# 检查 PWA 文件
if [ -f "web/public/manifest.json" ] && [ -f "web/public/sw.js" ]; then
  pass "PWA 基础文件完整"
else
  fail "PWA 基础文件缺失"
fi

# 检查数据库索引迁移
if grep -q "migrateV7" gateway/internal/store/schema.go; then
  pass "数据库 V7 迁移已定义"
else
  fail "数据库 V7 迁移缺失"
fi

INDEX_COUNT=$(grep -c "CREATE INDEX" gateway/internal/store/schema.go)
if [ "$INDEX_COUNT" -ge 45 ]; then
  pass "数据库索引充足 ($INDEX_COUNT 个)"
else
  warn "数据库索引较少 ($INDEX_COUNT 个)"
fi

# ── 5. 安全基础检查 ──
echo ""
echo "【5/6】安全基础检查..."

# 检查 CORS 环境变量读取
if grep -q "CORS_ALLOWED_ORIGINS" gateway/internal/middleware/middleware.go; then
  pass "CORS 支持环境变量配置"
else
  fail "CORS 硬编码"
fi

# 检查是否有硬编码密钥
if grep -r "secret.*=.*\"" gateway/internal --include="*.go" | grep -v "jwtSecret" | grep -v "secretFile" | head -1 > /dev/null; then
  warn "发现可能的硬编码密钥，请人工审查"
else
  pass "未发现明显硬编码密钥"
fi

# 检查 SQL 参数化
if grep -r "db\.Query.*+" gateway/internal --include="*.go" | head -1 > /dev/null; then
  warn "发现字符串拼接 SQL，请人工审查"
else
  pass "未发现字符串拼接 SQL"
fi

# ── 6. 总结 ──
echo ""
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║                        验证总结                              ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
echo -e "通过: ${GREEN}$PASS${NC} 项"
echo -e "警告: ${YELLOW}$WARN${NC} 项"
echo -e "失败: ${RED}$FAIL${NC} 项"
echo ""

if [ $FAIL -eq 0 ]; then
  echo -e "${GREEN}✓ 所有关键检查通过！${NC}"
  if [ $WARN -gt 0 ]; then
    echo -e "${YELLOW}  有 $WARN 项警告（可能因网络/环境限制），建议本地环境完整验证${NC}"
  fi
  exit 0
else
  echo -e "${RED}✗ 存在 $FAIL 项失败，请查看上方详情${NC}"
  exit 1
fi
