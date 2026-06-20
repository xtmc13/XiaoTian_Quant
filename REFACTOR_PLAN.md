# XiaoTianQuant 架构回退计划
## 目标: 从 Go+Rust+TS+Python 四栈 → Go+Rust+TS 三栈

---

## Phase 1: Go 端 ML 原生推理 (替代 Python ML Server)

**目标**: 让 gateway/internal/ml/ 不再依赖外部 Python HTTP 服务

### 1.1 利用现有能力
- `sandbox/ml_server/models/lightgbm_model.py` 已实现 JSON 树导出
- `gateway/internal/ml/predictor.go` 已有客户端代码
- 需要: 在 Go 端实现 LightGBM JSON 树的 native 推理

### 1.2 具体修改
- [ ] 新增 `gateway/internal/ml/inference/` - Go 原生 LightGBM/XGBoost 推理
  - `tree_parser.go` - 解析 JSON 树结构
  - `inference.go` - 单树推理 + 多树集成
  - `feature_vector.go` - 特征向量构建
- [ ] 修改 `gateway/internal/ml/client.go` - 优先本地推理，fallback 到 HTTP
- [ ] 保留 Python 训练能力（通过独立 CLI 工具，不作为常驻服务）

---

## Phase 2: 前端硬编码迁移到后端

**目标**: 所有前端硬编码配置改为后端 API 驱动

### 2.1 新增后端配置端点
- [ ] `gateway/internal/handler/config.go`
  - `GET /api/config/markets` - 交易对列表、精度
  - `GET /api/config/indices` - 热力图标的列表
  - `GET /api/config/exchanges` - 支持的交易所
  - `GET /api/config/ai-models` - AI 模型列表
  - `GET /api/config/rate` - USD/CNY 汇率

### 2.2 修改前端
- [ ] 创建 `web/src/hooks/useConfig.ts` - 统一配置获取
- [ ] 修改 AI 页面 - 热力图标的从 API 获取
- [ ] 修改交易页面 - 精度从 API 获取
- [ ] 修改 Settings 页面 - 交易所/模型从 API 获取
- [ ] 移除所有硬编码配置

---

## Phase 3: 纯 Go Fallback 降级为 Dev Mock

**目标**: 明确区分生产引擎(Rust)和开发引擎(mock)

### 3.1 修改
- [ ] `gateway/internal/adapter/matching.go`
  - 添加 `//go:build !cgo` + `//go:build dev` 双重标签
  - 所有方法开头添加 `log.Println("[DEV] Using mock matching engine - NOT for production")`
  - Snapshot 格式补齐 best_bid/best_ask/spread（与 Rust 对齐）
  - Trade 字段名统一为 buy_order_id/sell_order_id

### 3.2 编译约束
- [ ] 默认编译: `CGO_ENABLED=1 go build` (Rust 引擎)
- [ ] 开发编译: `go build -tags dev` (mock 引擎)
- [ ] 纯Go无Rust: 不允许，明确报错

---

## Phase 4: 清理占位适配器

**目标**: 移除虚假的交易所支持声明

### 4.1 分类处理
| 交易所 | 状态 | 操作 |
|--------|------|------|
| Binance | 深度实现 | 保留 ✅ |
| OKX/Bybit | 基础 REST | 保留，标记 experimental |
| Kraken/Gate/MEXC/Bitget/Coinbase/Alpaca | 浅层 | 标记 stub，返回友好错误 |
| IBKR/MT5/Tushare | 纯结构体 | 移除 ❌ |
| CCXT | Python桥 | 废弃，移除 ❌ |

### 4.2 修改
- [ ] 新增 `gateway/internal/adapter/stub.go` - 通用 stub 适配器模板
- [ ] 简化 Kraken/Gate/MEXC/Bitget/Coinbase/Alpaca 为 stub
- [ ] 删除 `ibkr.go`, `mt5.go`, `tushare.go`, `ccxt.go`
- [ ] 修改前端交易所列表从 API 动态获取

---

## Phase 5: Python 改造为独立 CLI 工具

**目标**: Python 不再是常驻服务，而是训练工具

### 5.1 改造
- [ ] 保留 `sandbox/` 但改造为独立 CLI
- [ ] `sandbox/train.py` - 模型训练（手动触发）
- [ ] `sandbox/export.py` - 导出 JSON 树到 Go 可读取的目录
- [ ] 删除 `sandbox/main.py` (不再作为服务启动)
- [ ] 删除 `sandbox/ml_server/` 目录结构，改为模块
- [ ] 保留 `sandbox/executor.py` (指标沙箱安全执行)
- [ ] 保留 `sandbox/ccxt_bridge/` 作为可选工具

### 5.2 Docker 调整
- [ ] docker-compose.yml: 移除 sandbox/ccxt_bridge/ml_server 服务
- [ ] 保留 gateway + engine + web + (可选 redis)

---

## Phase 6: 验证与清理

- [ ] `CGO_ENABLED=1 go build` 通过
- [ ] `go test ./...` 通过
- [ ] 前端构建通过
- [ ] Docker Compose 启动验证
- [ ] 清理遗留文件
- [ ] 更新 ARCHITECTURE.md
- [ ] 更新 README.md

---

## 预估工时

| Phase | 工时 | 复杂度 |
|-------|------|--------|
| Phase 1: Go ML 推理 | 2天 | 高 |
| Phase 2: 配置API + 前端改造 | 1.5天 | 中 |
| Phase 3: Mock 引擎标记 | 0.5天 | 低 |
| Phase 4: 适配器清理 | 0.5天 | 低 |
| Phase 5: Python CLI 改造 | 1天 | 中 |
| Phase 6: 验证与文档 | 1天 | 中 |
| **总计** | **6.5天** | |

---

## 预期结果

```
Before: gateway + web + engine + sandbox + ml_server + ccxt_bridge + redis (7服务)
After:  gateway + web + engine + [redis可选] (3-4服务)

Before: Go + Rust + TypeScript + Python (4语言)
After:  Go + Rust + TypeScript (3语言)

Before: 244端点 + 7个Python服务
After:  250+端点(新增配置端点) + 0个Python常驻服务
```
