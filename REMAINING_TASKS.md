# XiaoTianQuant — 最终任务清单

> 创建: 2026-06-01 | 完成: 2026-06-01
> 全部 16/16 + 4/4 ✅

## ✅ IMPROVEMENT_PLAN (20/20)

Phase 1-5 全部完成

## ✅ 对标补充 (16/16)

| # | 任务 | 文件 |
|---|------|------|
| 1 | Billing/VIP | handler/billing.go + pages/Billing.tsx |
| 2 | Trading Bot 向导 | 已有 Bots.tsx (5种+AI) |
| 3 | 交易所止损 | order/stoploss.go (3模式+交易所下单) |
| 4 | 订单超时/紧急退出 | order/timeout.go |
| 5 | 资金费率 | risk/funding_fee.go |
| 6 | Bitget 适配器 | adapter/bitget.go |
| 7 | 多语言 i18n | i18n/index.ts (7语言+TopBar切换) |
| 8 | OAuth 登录 | handler/oauth.go (Google+GitHub→Login页) |
| 9 | 快捷交易面板 | 已有 QuickTradePanel.tsx |
| 10 | IBKR 券商 | adapter/ibkr.go |
| 11 | Agent token scopes | agent/token_scopes.go (6 scopes) |
| 12 | 回测假设面板 | BacktestAssumptions.tsx→Backtest页 |
| 13 | 凭证加密 | store/credential_vault.go (AES-256-GCM) |
| 14 | 全局市场仪表盘 | 已有 AI.tsx (Fear&Greed/VIX/指数/热力图/日历) |
| 15 | 交易所注册弹窗 | ExchangeSignupModal.tsx→账户页 |
| 16 | Edge 分析 | backtest/edge.go (Kelly/WinRate/Score) |

## ✅ 收尾 (4/4)

| # | 任务 | 状态 |
|---|------|:--:|
| 1 | MT5 券商适配器 | ✅ adapter/mt5.go |
| 2 | Billing 前端页面 | ✅ pages/Billing.tsx + /billing 路由 |
| 3 | README v3.0 更新 | ✅ |
| 4 | i18n/OAuth/导航接线 | ✅ |

## 最终数据

| 指标 | 数量 |
|------|:----:|
| Go 测试包 | 16 ✅ |
| TS 测试 | 15 ✅ |
| Rust 测试 | 11 ✅ |
| 前端页面 | 20 |
| API 端点 | 100+ |
| 交易所 | 9 native + 100+ CCXT + IBKR + MT5 |
| 通知 | Email/Lark/DingTalk/Telegram/Discord |
| Docker 服务 | 5 |
| 全部编译 | ✅ |

## ✅ 精细化完善 (P1-P4)

### P1 — 功能精细化 (4/4)
| # | 任务 | 文件 |
|---|------|------|
| 1 | 前端策略参数动态表单 | pages/Strategy.tsx + api.ts |
| 2 | ML 模型部署自动注册 | handler/ml.go (auto_start→引擎注册) |
| 3 | 前端路由级代码分割 | App.tsx (lazyPage + 20+ chunks) |
| 4 | Prometheus 指标暴露 | metrics/metrics.go (纯Go实现) |

### P2 — 前端工程化 (5/5)
| # | 任务 | 文件 |
|---|------|------|
| 1 | 错误边界 + 加载状态 | components/ErrorBoundary.tsx |
| 2 | API 错误统一处理 | stores/toastStore.ts + api.ts 重试 |
| 3 | 状态管理优化 | hooks/useAsyncData.ts (5 hooks) |
| 4 | 性能优化 | VirtualList + DataTable + MemoizedTableRow |
| 5 | 可访问性改进 | components/a11y.tsx (SkipLink/FocusTrap/LiveRegion) |

### P3 — 后端工程化 (5/5)
| # | 任务 | 文件 |
|---|------|------|
| 1 | 健康检查完善 | handler/status.go (runtime stats + 全组件) |
| 2 | 配置热重载 | config/config.go Reload() + admin API |
| 3 | 结构化日志 | middleware/logger.go (替换 gin.Logger) |
| 4 | 优雅关闭 | app/context.go Shutdown() (started flag) |
| 5 | 请求限流 | middleware/ratelimit.go (Token Bucket) |
