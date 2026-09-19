# XiaoTianQuant 架构回退 — 重构完成报告

> **重构日期**: 2026-06-20
> **目标**: Go+Rust+TS+Python(4栈) → Go+Rust+TS(3栈)

---

## 重构成果总览

```
Before                          After
┌─────────────────────┐                    ┌───────────────┐
│  React 19 (TS)          │                    │  React 19 (TS)  │
├─────────────────────┤                    ├───────────────┤
│  Go Gateway (245文件)  │                    │  Go Gateway     │
│  ├─ 28 包                │                    │  ├─ 28 包        │
│  ├─ 244 API 端点         │                    │  ├─ 249 API 端点  │
│  └─ +5 配置端点(new)    │                    │  └─ +5 配置端点  │
├─────────────────────┤                    ├───────────────┤
│  Rust Engine (8文件)   │                    │  Rust Engine    │
│  ├─ CGO 模式            │                    │  ├─ CGO 模式    │
│  └─ 纯Go fallback     │                    │  └─ Dev Mock 标记 │
├─────────────────────┤                    └───────────────┘
│  Python (18文件)       │                        ↑
│  ├─ sandbox 服务 ✗      │                   3 栈架构
│  ├─ ml_server 服务 ✗    │
│  ├─ ccxt_bridge 服务 ✗  │
│  └─ CLI 工具 ✓          │
└─────────────────────┘

服务数量: 7 → 2 (gateway + redis[可送])
语言栈:   4 → 3
API 端点:  244 → 249 (+5 配置端点)
```

---

## 各 Phase 修改详情

### Phase 3: Mock 引擎格式对齐 ✅

| 文件 | 修改 |
|------|--------|
| `gateway/internal/adapter/matching.go` | Snapshot 添加 best_bid/best_ask/spread，Trade 字段统一为 buy_order_id/sell_order_id，SubmitOrder 格式与 Rust 对齐 |
| `gateway/internal/adapter/helpers.go` | 新增 stubError() 、parseFloatSafe() 、getString() 通用函数 |

### Phase 4: 适配器清理 ✅

| 文件 | 操作 |
|------|--------|
| `ibkr.go` | 删除 |
| `mt5.go` | 删除 |
| `tushare.go` | 删除 |
| `ccxt.go` | 删除 |
| `tushare_test.go` | 删除 |
| `coinbase.go` | 改为 stub |
| `kraken.go` | 改为 stub |
| `mexc.go` | 改为 stub |
| `bitget.go` | 改为 stub |
| `gateio.go` | 改为 stub |
| `alpaca.go` | 改为 stub |
| `alpaca.go`/`bitget.go`/`kraken.go` | 删除重复的 parseFloatSafe() |

**3 家真正可用**: Binance, OKX, Bybit

### Phase 5: Python 改造为 CLI 工具 ✅

| 文件 | 操作 |
|------|--------|
| `sandbox/main.py` | FastAPI 服务 → CLI 入口 (execute/analyze 子命令) |
| `sandbox/ml_server/main.py` | FastAPI 服务 → 训练 CLI 入口 (train 子命令) |
| `sandbox/train.py` | 新建简洁训练入口 |
| `docker-compose.yml` | 移除 sandbox/ml_server/ccxt_bridge 服务 |
| `sandbox/README.md` | 更新文档 |

**保留的核心能力**: executor.py (indicator执行), analyzer.py (静态分析), ml_server/特征工程/模型, ccxt_bridge/, indicators/

### Phase 2: 配置 API + 前端移民 ✅

| 文件 | 操作 |
|------|--------|
| `gateway/internal/handler/config_dynamic.go` | 新建 5 个配置端点 |
| `gateway/cmd/server/router.go` | 注册配置路由 |
| `gateway/data/markets.json` | 新建交易对配置 |
| `gateway/data/indices.json` | 新建热力图配置 |
| `web/src/hooks/useConfig.ts` | 新建 5 个配置 Hook |
| `web/src/lib/api.ts` | 新增 configApi |
| `web/src/pages/AI/index.tsx` | 热力图标的从 API 获取 |
| `web/src/lib/tradingPrecision.ts` | 支持后端注册精度 |
| `web/src/pages/Settings.tsx` | 交易所/模型从 API 获取 |

---

## 验证结果

```bash
$ CGO_ENABLED=0 go build ./...
# ✓ 编译通过，无错误

$ python sandbox/main.py --help
# ✓ CLI 正常

$ python sandbox/train.py --help
# ✓ CLI 正常
```

---

## 交付物

| 文件 | 说明 |
|------|------|
| `REFACTOR_PLAN.md` | 重构计划 |
| `REFACTOR_COMPLETE.md` | 本报告 |
| `PROJECT_COMPLETENESS_REPORT.md` | 完整度评估报告 |
| `audit-backend.md` | 后端审计报告 |
| `audit-frontend.md` | 前端审计报告 |
| `audit-engine-ml.md` | 引擎+ML审计报告 |

---

*2026-06-20 | 从 4 栈回退到 3 栈完成*
