# XiaoTian Quant v3.0 — 全面修复总纲 (v2.0)

> 创建时间：2026-06-20
> 目标：从框架完整走向功能完整，消灭所有空实现和硬编码

---

## 一、当前状态速览

### 已完成修复 ✅（本次会话）

| # | 修复项 | 文件 | 影响 |
|---|--------|------|------|
| 1 | 扩展 OMS 下单到 9 交易所 | `handler/order.go` | 支持 binance/bybit/kraken/mexc/bitget/okx/gateio/coinbase/alpaca |
| 2 | Kraken 持仓查询 | `adapter/kraken.go` | 真实 API `/OpenPositions` |
| 3 | Bitget 持仓查询 | `adapter/bitget.go` | 真实 API `/mix/position/allPosition` |
| 4 | Bybit 合约持仓 | `adapter/bybit.go` | 真实 API `/position/list` |

### 作者已修复 ✅（无需重复）

- WS 行情：已接入真实 ticker + synthetic 标记
- AI 页面：初始状态空数组，数据走 API
- 风控 RateLimit：500ms 真实间隔
- 回测可复现：Runner.rng + SlippageSeed
- 社交交易：riskCheck 完整实现
- 前端按钮：主要按钮已绑定 onClick
- Telegram Bot：回调改为外部注入
- ML Predictor：LoadFromFile 已实现
- AI Handler：已接入 LLM Provider

---

## 二、待修复清单（精细化）

### Phase 1：适配器完整性（进行中）

#### 1.1 适配器空方法扫描
- [ ] 检查所有适配器的 `GetPositions` 是否还有空实现
- [ ] 检查 `GetOpenOrders` 是否全部实现
- [ ] 检查 `GetTicker` 是否全部实现
- [ ] 检查 `GetKlines` 是否全部实现
- [ ] 检查 `StartMarketStream` 是否全部实现

#### 1.2 适配器错误处理
- [ ] 统一所有适配器的错误返回格式
- [ ] 添加重试逻辑（指数退避）
- [ ] 添加 API 限流处理

### Phase 2：前端完整性

#### 2.1 硬编码数据清理
- [ ] 检查所有前端页面是否有硬编码数据
- [ ] 确保所有表格初始状态为空
- [ ] 检查 Dashboard 是否有 mock 数据

#### 2.2 交互完整性
- [ ] 检查所有按钮是否有 onClick
- [ ] 检查表单提交是否有错误处理
- [ ] 检查 loading 状态是否统一

### Phase 3：后端核心功能

#### 3.1 订单管理
- [ ] 订单状态同步（轮询 + WebSocket）
- [ ] 订单历史查询
- [ ] 批量撤单

#### 3.2 风控系统
- [ ] 检查风控规则是否正确运行
- [ ] 检查风控日志是否记录
- [ ] 检查风控通知是否发送

#### 3.3 回测系统
- [ ] 检查回测结果是否正确计算
- [ ] 检查回测数据是否可复现
- [ ] 检查回测报告是否完整

### Phase 4：部署与测试

#### 4.1 构建
- [ ] 确保 Go 后端可编译
- [ ] 确保前端可构建
- [ ] 确保 Rust 引擎可编译

#### 4.2 测试
- [ ] 运行现有测试
- [ ] 检查测试覆盖率
- [ ] 补充缺失的测试

#### 4.3 文档
- [ ] 更新 API 文档
- [ ] 更新部署文档
- [ ] 更新配置文档

---

## 三、执行计划

1. **立即执行**：Phase 1 适配器完整性扫描
2. **接下来**：Phase 2 前端检查
3. **然后**：Phase 3 后端核心功能验证
4. **最后**：Phase 4 构建测试文档

---

## 四、检查工具

```bash
# 扫描所有空实现
grep -rn "return nil, nil\|return []map\[string\]any{}, nil\|return map\[string\]any{}, nil" gateway/internal/adapter/

# 扫描硬编码数据
grep -rn "68000\|3500\|52\|18.5\|mock\|fake\|dummy" web/src/pages/

# 扫描未实现的 TODO
grep -rn "TODO\|FIXME\|HACK\|XXX" gateway/ web/ engine/
```
