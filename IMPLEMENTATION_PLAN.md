# 小天量化 v3.1 补齐计划
## 目标: 补齐所有功能差距(除CRA商业功能外)

---

## 原则
1. 三栈架构不变: Go(网关) + Rust(撮合) + TS(前端)
2. Python保持CLI工具，不作为常驻服务
3. 所有代码生产级质量
4. 保持向后兼容

---

## 模块清单与优先级

### Phase 1: Rust SignalExecutor (核心基石)
- [ ] `engine/src/executor/mod.rs` - SignalExecutor 主结构
- [ ] `engine/src/executor/signal.rs` - Signal 解析/校验
- [ ] `engine/src/executor/position.rs` - PositionManager 仓位管理
- [ ] `engine/src/executor/tpsl.rs` - TPSLManager (6种止盈 + 3种止损)
- [ ] `engine/src/executor/execution.rs` - ExecutionEngine
- [ ] FFI接口: `engine_execute_signal`, `engine_update_price`
- [ ] Go桥接: `gateway/internal/adapter/executor_bridge.go`

### Phase 2: Go策略引擎扩展
- [ ] `gateway/internal/strategy/martin.go` - 马丁趋势(倍投)
- [ ] `gateway/internal/strategy/wallstreet.go` - 华尔街(等比)
- [ ] `gateway/internal/strategy/flashcrash.go` - 防瀑布
- [ ] `gateway/internal/strategy/indicators/` - MACD/EMA开仓指标
- [ ] `gateway/internal/strategy/contract.go` - 合约杠杆管理
- [ ] `gateway/internal/risk/contract.go` - 保证金监控/强平预警

### Phase 3: 配置API + 数据模型
- [ ] 扩展 `gateway/internal/handler/config_dynamic.go`
- [ ] 策略参数模型更新
- [ ] 合约账户模型
- [ ] 审计日志模型

### Phase 4: MCP Server + 审计日志
- [ ] `gateway/internal/mcp/` - MCP Server实现
- [ ] `gateway/internal/audit/` - 审计日志模块
- [ ] Paper-only Default模式

### Phase 5: 前端更新
- [ ] Bots页面三标签页细化
- [ ] 策略配置页面(含马丁/华尔街参数)
- [ ] 合约交易页面
- [ ] SignalExecutor状态面板

### Phase 6: 部署
- [ ] `install.sh` - 一键安装
- [ ] GitHub Actions - GHCR镜像构建
- [ ] 文档更新

---

## 预估工时: 8周 (3子代理并行)
