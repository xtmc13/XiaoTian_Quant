# XiaoTian_Quant 行动清单（2026-09-15 重整）

> 规则：做完一条，勾一条，立即 `commit + push`。
> 不再生成任何新的宏观审计报告；项目真实状态只以本文件为准。
> 背景：根目录 29 份历史报告（2026-08-27）结论互相矛盾、多数已过时，
> 其有效结论已浓缩进本清单，原文件待归档（见第 7 条）。

## P0 —— 让项目"能用"

- [ ] 1. **端到端基线验证**：干净环境走通 构建 → 启动 → 登录 → 模拟盘下第一单，结果记录在本文件末节。工具链已就绪（Go 1.25.3 / Node 22 / nginx 8088）。
- [ ] 2. **修复"全新部署风控拦截首单"**：首次下单被 `drawdown 100% > 10%` 拦截（`gateway/internal/risk/manager.go`）；paper 模式应初始化 peak equity 或首单默认放行。
- [ ] 3. **paper 行情接真实数据源**：无 PriceProvider 时返回合成数据（Simulated 标记）；接入币安公共行情（无需密钥），或在页面明确标注"演示数据"。
- [x] 4. **数据层修复**（2026-09-15）：引入 `internal/store/migrations/sql/*.sql` 纯 SQL 迁移机制（`internal/store/sql_migrations.go`）；修复 `agent_audit_log` 列定义冲突；修复 `ticks` 表初始化竞态；补 4 个高频过滤列索引。

## P1 —— 稳定性与可信度

- [ ] 5. **交易主链路 E2E 测试补全**：9 个用例 7 个标 `fixme`，含下单流程。
- [ ] 6. **前端路由缺口复核**：`/api/experiments` 无列表路由、`/api/onchain/*` 未注册（对照 `cmd/server/router.go` 逐条核对）。
- [ ] 7. **仓库卫生**：清理根目录 `tmp_*.tsx`、`login-filled.yaml`（疑似含凭据）、`sandbox.log`、`gateway/gateway` 二进制入 `.gitignore`；29 份过期报告归档到 `docs/audits-20260827/`。
- [ ] 8. **决定 Rust 引擎定位**：补 FFI 桥接接入 Go 主链路，或正式宣布 Go 执行路径为正式版、Rust 为实验分支（写进 README，消除"计划中"的幻觉）。

## P2 —— 质量

- [ ] 9. **CRA 参数表单去重收尾**：Settings / Strategy / Bots 三页仍是约 400 行/份的重复块。
- [ ] 10. **新代码纪律**：时间戳统一 INTEGER (unix ms)；新表/新索引必须走 `migrations/sql/*.sql`，禁止再散落 `CREATE TABLE`；JSON 列在应用层做校验。

## 端到端验证记录

（每次基线验证的结果写在这里，替换 DEPLOY_VERIFICATION.md 等死文档）

- 2026-09-15：工具链就绪（Go 1.25.3 安装并清理了 /usr/local/go 旧版残留；前端 nginx 8088 部署成功；后端旧二进制可启动，testnet+dry_run 双保险）。
- 2026-09-15：数据层修复（清单第 4 条）验证通过——新装路径：32 表、SQL 迁移 0001 应用、审计日志新列齐、6 新索引在、ticks 懒建表生效；升级路径：现有库 30 表行数零变化，admin/admin123 登录正常。CGO_ENABLED=0 全量编译 + store/data 单测通过（ Rust 引擎库缺失时纯 Go 构建为正式路径，见第 8 条）。
