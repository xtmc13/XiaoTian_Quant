# XiaoTianQuant Web

React 19 前端——量化交易平台的用户界面。

## 技术栈

- **框架**: React 19 + TypeScript 5.7
- **构建**: Vite 6
- **样式**: TailwindCSS 3 + PostCSS
- **状态**: Zustand 5
- **数据请求**: TanStack React Query 5
- **路由**: React Router 7
- **图表**: ECharts 5 + Lightweight Charts + KLineCharts Pro
- **代码编辑器**: CodeMirror 6
- **测试**: Vitest + Testing Library + Playwright (E2E)
- **国际化**: en-US / zh-CN

## 目录结构

```
web/
├── public/                # PWA 资源 (manifest, sw.js, icons)
├── src/
│   ├── components/        # 通用 UI 组件
│   │   ├── charts/        # K 线/深度图/ECharts 图表
│   │   ├── trading/       # 订单簿、下单表单、深度图
│   │   ├── strategy/      # 策略管理组件
│   │   ├── bots/          # 交易机器人组件
│   │   ├── ide/           # CodeMirror 代码编辑器
│   │   ├── layout/        # Sidebar / TopBar / BottomBar
│   │   └── ui/            # 基础 UI 组件
│   ├── hooks/             # 自定义 Hooks (WebSocket, useAsyncData, etc.)
│   ├── i18n/              # 国际化 (en-US / zh-CN)
│   ├── lib/               # 工具函数 (API 客户端, 技术指标, PWA)
│   ├── pages/             # 页面组件 (~26 个)
│   │   ├── Dashboard.tsx
│   │   ├── Trading/SpotTrading.tsx
│   │   ├── ContractTrading.tsx
│   │   ├── Backtest.tsx
│   │   ├── Strategy.tsx
│   │   ├── Portfolio.tsx
│   │   ├── AdvancedOrderManagement.tsx
│   │   ├── RiskControl.tsx
│   │   ├── HyperoptManagement.tsx
│   │   ├── Settings.tsx
│   │   ├── ExchangeAccount.tsx
│   │   ├── Login.tsx
│   │   ├── AI.tsx
│   │   ├── SocialTrading.tsx
│   │   ├── IndicatorIDE.tsx
│   │   └── ... (更多)
│   ├── stores/            # Zustand stores (app, auth, toast)
│   ├── App.tsx            # 路由配置 (~25 个懒加载路由)
│   └── main.tsx           # 入口 (React Query, PWA, ErrorBoundary)
├── e2e/                   # Playwright E2E 测试
├── vite.config.ts
├── tailwind.config.js
├── tsconfig.json
└── package.json
```

## 快速开始

```bash
# 安装依赖
npm install

# 开发模式
npm run dev           # 默认 http://localhost:5173

# 类型检查
npm run type-check

# 测试
npm run test          # Vitest 单元测试
npm run test:e2e      # Playwright E2E

# 构建 (自动复制到 gateway/spa/)
npm run build
```

## 构建产物

`npm run build` 将产出自动复制到 `gateway/spa/`，通过 Go 的 `//go:embed` 嵌入。

## 页面路由

| 页面 | 路由 | 说明 |
|------|------|------|
| Dashboard | `/` | 总览仪表盘 |
| SpotTrading | `/trading/spot` | 现货交易 |
| ContractTrading | `/trading/contract` | 合约交易 |
| Backtest | `/backtest` | 回测界面 |
| Strategy | `/strategy` | 策略管理 |
| Portfolio | `/portfolio` | 持仓与资产 |
| AdvancedOrder | `/orders/advanced` | 高级订单 (OCO/冰山/DCA) |
| RiskControl | `/risk` | 风控配置 |
| AI | `/ai` | AI 分析面板 |
| IndicatorIDE | `/indicator/ide` | 指标编辑器 |
| Community | `/community` | 指标/策略市场 |
| Settings | `/settings` | 系统设置 |
| Login | `/login` | 登录 |

## 测试

```bash
# 单元测试
npm run test

# E2E 测试
npm run test:e2e

# 可访问性测试
npm run test:a11y
```
