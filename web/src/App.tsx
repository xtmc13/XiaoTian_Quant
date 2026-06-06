import { BrowserRouter, Routes, Route, Navigate, useLocation, Outlet } from 'react-router-dom'
import { lazy, Suspense, ComponentType, useEffect } from 'react'
import { useAuthStore } from '@/stores/authStore'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/en-US'
import { Layout } from './components/layout/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'

// Eager-loaded: shell + entry pages
import { Login } from './pages/Login'

// ── Lazy-loading helper for named exports ──
function lazyPage(
  factory: () => Promise<unknown>,
  name: string
) {
  return lazy(() => factory().then((m) => ({ default: (m as Record<string, unknown>)[name] as React.ComponentType<unknown> })))
}

// Lazy-loaded: all feature pages
const Dashboard = lazyPage(() => import('./pages/Dashboard'), 'Dashboard')
const Trading = lazyPage(() => import('./pages/Trading'), 'Trading')
const Strategy = lazyPage(() => import('./pages/Strategy'), 'Strategy')
const AI = lazyPage(() => import('./pages/AI'), 'AI')
const Backtest = lazyPage(() => import('./pages/Backtest'), 'Backtest')
const Bots = lazyPage(() => import('./pages/Bots'), 'Bots')
const Settings = lazyPage(() => import('./pages/Settings'), 'Settings')
const ExchangeAccount = lazyPage(() => import('./pages/ExchangeAccount'), 'ExchangeAccount')
const IndicatorCommunity = lazyPage(() => import('./pages/IndicatorCommunity'), 'IndicatorCommunity')
const Portfolio = lazyPage(() => import('./pages/Portfolio'), 'Portfolio')
const IndicatorIDE = lazyPage(() => import('./pages/IndicatorIDE'), 'IndicatorIDE')
const UserProfile = lazyPage(() => import('./pages/UserProfile'), 'UserProfile')
const UserManage = lazyPage(() => import('./pages/UserManage'), 'UserManage')
const AgentTokens = lazyPage(() => import('./pages/AgentTokens'), 'AgentTokens')
const IndicatorDetail = lazyPage(() => import('./pages/IndicatorDetail'), 'IndicatorDetail')
const AuthorDashboard = lazyPage(() => import('./pages/AuthorDashboard'), 'AuthorDashboard')
const Billing = lazyPage(() => import('./pages/Billing'), 'Billing')
const StrategyLeaderboard = lazyPage(() => import('./pages/StrategyLeaderboard'), 'StrategyLeaderboard')
const ModelManagement = lazyPage(() => import('./pages/ModelManagement'), 'ModelManagement')
const RiskControl = lazyPage(() => import('./pages/RiskControl'), 'RiskControl')
const PairlistManagement = lazyPage(() => import('./pages/PairlistManagement'), 'PairlistManagement')
const AdvancedOrderManagement = lazyPage(() => import('./pages/AdvancedOrderManagement'), 'AdvancedOrderManagement')
const ArbitrageMonitor = lazyPage(() => import('./pages/ArbitrageMonitor'), 'ArbitrageMonitor')
const HyperoptManagement = lazyPage(() => import('./pages/HyperoptManagement'), 'HyperoptManagement')
const SocialTrading = lazyPage(() => import('./pages/SocialTrading'), 'SocialTrading')
const OnChain = lazyPage(() => import('./pages/OnChain'), 'OnChain')

function DocumentTitle() {
  const location = useLocation()

  const titles: Record<string, string> = {
    '/login': '登录 - 小天量化',
    '/dashboard': '仪表盘 - 小天量化',
    '/trading': '交易 - 小天量化',
    '/strategy': '策略 - 小天量化',
    '/ai': 'AI研究 - 小天量化',
    '/backtest': '回测 - 小天量化',
    '/bots': '机器人 - 小天量化',
    '/settings': '设置 - 小天量化',
    '/exchange-account': '账户 - 小天量化',
    '/indicator-community': '指标市场 - 小天量化',
    '/author-dashboard': '作者后台 - 小天量化',
    '/portfolio': '资产监测 - 小天量化',
    '/indicator-ide': '指标IDE - 小天量化',
    '/model-management': 'ML模型 - 小天量化',
    '/risk-control': '风控中心 - 小天量化',
    '/pairlist': '交易对筛选 - 小天量化',
    '/advanced-orders': '高级订单 - 小天量化',
    '/arbitrage': '套利监控 - 小天量化',
    '/hyperopt': '参数优化 - 小天量化',
    '/social-trading': '社交交易 - 小天量化',
    '/onchain': '链上数据 - 小天量化',
    '/profile': '个人中心 - 小天量化',
    '/users': '用户管理 - 小天量化',
    '/agent-tokens': 'Agent令牌 - 小天量化',
    '/billing': '会员 - 小天量化',
    '/strategy-leaderboard': '策略排行榜 - 小天量化',
  }

  const title = titles[location.pathname] || '小天量化'

  useEffect(() => {
    document.title = title
  }, [title])

  return null
}

function PageLoader() {
  return (
    <div className="h-screen bg-quant-bg flex items-center justify-center">
      <div className="animate-spin h-8 w-8 border-2 border-quant-gold/30 border-t-quant-gold rounded-full" />
    </div>
  )
}

function RequireAuth() {
  const { isAuthenticated, hydrated } = useAuthStore()
  const location = useLocation()

  // E2E test bypass: check localStorage token or window flag
  const hasToken = typeof window !== 'undefined' && !!localStorage.getItem('xt-token')
  const e2eAuth = typeof window !== 'undefined' && window.__E2E_AUTH__

  if (!hydrated && !e2eAuth && !hasToken) {
    return (
      <div className="h-screen bg-quant-bg flex items-center justify-center">
        <div className="animate-spin h-6 w-6 border-2 border-quant-gold/30 border-t-quant-gold rounded-full" />
      </div>
    )
  }

  if (!isAuthenticated && !e2eAuth && !hasToken) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  return <Outlet />
}

export default function App() {
  return (
    <I18nProvider>
      <BrowserRouter>
        <DocumentTitle />
        <ErrorBoundary>
          <Suspense fallback={<PageLoader />}>
            <Routes>
            <Route path="/login" element={<Login />} />
            <Route element={<RequireAuth />}>
              <Route element={<Layout />}>
                <Route path="/" element={<Navigate to="/dashboard" replace />} />
                <Route path="/dashboard" element={<Dashboard />} />
                <Route path="/trading" element={<Trading />} />
                <Route path="/strategy" element={<Strategy />} />
                <Route path="/ai" element={<AI />} />
                <Route path="/backtest" element={<Backtest />} />
                <Route path="/bots" element={<Bots />} />
                <Route path="/settings" element={<Settings />} />
                <Route path="/exchange-account" element={<ExchangeAccount />} />
                <Route path="/indicator-community" element={<IndicatorCommunity />} />
                <Route path="/indicator-community/:id" element={<IndicatorDetail />} />
                <Route path="/author-dashboard" element={<AuthorDashboard />} />
                <Route path="/portfolio" element={<Portfolio />} />
                <Route path="/indicator-ide" element={<IndicatorIDE />} />
                <Route path="/model-management" element={<ModelManagement />} />
                <Route path="/risk-control" element={<RiskControl />} />
                <Route path="/pairlist" element={<PairlistManagement />} />
                <Route path="/advanced-orders" element={<AdvancedOrderManagement />} />
                <Route path="/arbitrage" element={<ArbitrageMonitor />} />
                <Route path="/hyperopt" element={<HyperoptManagement />} />
                <Route path="/social-trading" element={<SocialTrading />} />
                <Route path="/onchain" element={<OnChain />} />
                <Route path="/profile" element={<UserProfile />} />
                <Route path="/users" element={<UserManage />} />
                <Route path="/agent-tokens" element={<AgentTokens />} />
                <Route path="/billing" element={<Billing />} />
                <Route path="/strategy-leaderboard" element={<StrategyLeaderboard />} />
              </Route>
            </Route>
          </Routes>
        </Suspense>
      </ErrorBoundary>
    </BrowserRouter>
    </I18nProvider>
  )
}
