import { BrowserRouter, Routes, Route, Navigate, useLocation, Outlet, useNavigate } from 'react-router-dom'
import { lazy, Suspense, useEffect, useRef } from 'react'
import { useAuthStore } from '@/stores/authStore'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/en-US'
import { Layout } from './components/layout/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import { pageLoaders } from '@/lib/pageLoaders'

// Eager-loaded: shell + entry pages
import { Login } from './pages/Login'
import { NotFound } from './pages/NotFound'

// ── Lazy-loading helper for named exports ──
function lazyPage(factory: () => Promise<unknown>, name: string) {
  return lazy(() =>
    factory().then((m) => ({ default: (m as Record<string, unknown>)[name] as React.ComponentType<unknown> }))
  )
}

// Lazy-loaded: all feature pages
const Dashboard = lazyPage(() => import('./pages/Dashboard'), 'Dashboard')
const Trading = lazyPage(() => import('./pages/Trading'), 'Trading')
const Strategy = lazyPage(() => import('./pages/Strategy'), 'Strategy')
const AI = lazyPage(() => import('./pages/AI'), 'AI')
const Backtest = lazyPage(() => import('./pages/Backtest'), 'Backtest')
const Bots = lazyPage(() => import('./pages/Bots'), 'Bots')
const AIBots = lazyPage(() => import('./pages/AIBots'), 'default')
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

// ── Split pages (flat navigation) ──
const Market = lazyPage(() => import('./pages/Market'), 'Market')
const AIAnalysis = lazyPage(() => import('./pages/ai/AIAnalysis'), 'AIAnalysis')
const FreqAI = lazyPage(() => import('./pages/ai/FreqAI'), 'FreqAI')
const RLTraining = lazyPage(() => import('./pages/ai/RLTraining'), 'RLTraining')
const TensorBoard = lazyPage(() => import('./pages/ai/TensorBoard'), 'TensorBoard')
const StrategyEditor = lazyPage(() => import('./pages/strategy/StrategyEditor'), 'StrategyEditor')
const BotsStrategy = lazyPage(() => import('./pages/bots/BotsStrategy'), 'BotsStrategy')
const BotsSignal = lazyPage(() => import('./pages/bots/BotsSignal'), 'BotsSignal')
const BotsAI = lazyPage(() => import('./pages/bots/BotsAI'), 'BotsAI')
const TradingSpot = lazyPage(() => import('./pages/trading/TradingSpot'), 'TradingSpot')
const TradingContract = lazyPage(() => import('./pages/trading/TradingContract'), 'TradingContract')
const ArbitrageCross = lazyPage(() => import('./pages/arbitrage/ArbitrageCross'), 'ArbitrageCross')
const ArbitrageTriangular = lazyPage(() => import('./pages/arbitrage/ArbitrageTriangular'), 'ArbitrageTriangular')

// ── New P0 pages ──
const SystemStatus = lazyPage(() => import('./pages/SystemStatus'), 'SystemStatus')
const DataManager = lazyPage(() => import('./pages/DataManager'), 'DataManager')
const Logs = lazyPage(() => import('./pages/Logs'), 'Logs')

// ── Route-level error boundary with retry ──
function RouteErrorBoundary({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate()
  return (
    <ErrorBoundary
      fallback={
        <div className="h-full flex flex-col items-center justify-center bg-[#0a0a0a] text-center p-8">
          <div className="text-5xl mb-4 opacity-30">⚠</div>
          <h3 className="text-lg font-semibold text-white mb-2">页面加载异常</h3>
          <p className="text-sm text-[#8a8a8a] mb-4 max-w-sm">该页面遇到了意外错误，可能是网络问题或资源加载失败。</p>
          <div className="flex gap-3">
            <button
              onClick={() => window.location.reload()}
              className="rounded-lg bg-white px-4 py-2 text-sm font-medium text-[#0a0a0a] hover:opacity-90 transition-opacity"
            >
              刷新页面
            </button>
            <button
              onClick={() => navigate('/dashboard')}
              className="rounded-lg border border-[#1c1c1c] bg-[#111111] px-4 py-2 text-sm text-white hover:bg-[#1c1c1c] transition-colors"
            >
              回到首页
            </button>
          </div>
        </div>
      }
    >
      {children}
    </ErrorBoundary>
  )
}

// Wraps a lazy component with route-level error boundary
function PageShell({ children }: { children: React.ReactNode }) {
  return <RouteErrorBoundary>{children}</RouteErrorBoundary>
}

function DocumentTitle() {
  const location = useLocation()
  const prefetched = useRef<Set<string>>(new Set())

  const titles: Record<string, string> = {
    '/login': '登录 - 小天量化',
    '/dashboard': '仪表盘 - 小天量化',
    '/trading': '交易 - 小天量化',
    '/strategy': '策略 - 小天量化',
    '/ai': 'AI研究 - 小天量化',
    '/backtest': '回测 - 小天量化',
    '/bots': '机器人 - 小天量化',
    '/ai-bots': 'AI Bots - 小天量化',
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

    // Flat navigation titles
    '/market': '市场数据 - 小天量化',
    '/trading/spot': '现货交易 - 小天量化',
    '/trading/contract': '合约交易 - 小天量化',
    '/strategy/editor': '策略编辑器 - 小天量化',
    '/ai/analysis': 'AI分析 - 小天量化',
    '/ai/freqai': 'FreqAI - 小天量化',
    '/ai/rl': 'RL强化学习 - 小天量化',
    '/ai/tensorboard': 'TensorBoard - 小天量化',
    '/bots/strategy': '策略机器人 - 小天量化',
    '/bots/signal': '信号机器人 - 小天量化',
    '/bots/ai': 'AI机器人 - 小天量化',
    '/arbitrage/cross': '跨所套利 - 小天量化',
    '/arbitrage/triangular': '三角套利 - 小天量化',

    // P0 new pages
    '/status': '系统状态 - 小天量化',
    '/data': '数据下载 - 小天量化',
    '/logs': '系统日志 - 小天量化',
  }

  const title = titles[location.pathname] || '小天量化'

  useEffect(() => {
    document.title = title
  }, [title])

  // ── Prefetch adjacent routes on idle ──
  useEffect(() => {
    const currentPath = location.pathname
    if (prefetched.current.has(currentPath)) return
    prefetched.current.add(currentPath)

    // Prefetch common next pages (most likely navigation targets)
    const adjacentPaths = ['/dashboard', '/trading', '/strategy', '/backtest', '/settings']

    const idleCallback = (window as unknown as Record<string, unknown>).requestIdleCallback as
      | ((cb: () => void, opts?: { timeout: number }) => number)
      | undefined

    const schedule = (fn: () => void) => {
      if (idleCallback) {
        idleCallback(fn, { timeout: 2000 })
      } else {
        setTimeout(fn, 300)
      }
    }

    adjacentPaths.forEach((path) => {
      if (path !== currentPath && !prefetched.current.has(path) && pageLoaders[path]) {
        schedule(() => {
          prefetched.current.add(path)
          pageLoaders[path]().catch(() => {})
        })
      }
    })
  }, [location.pathname])

  return null
}

function PageLoader() {
  return (
    <div className="h-dvh bg-quant-bg flex items-center justify-center">
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
      <div className="h-dvh bg-quant-bg flex items-center justify-center">
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
        <ErrorBoundary
          fallback={
            <div className="min-h-screen flex items-center justify-center text-destructive">
              应用出现异常，请刷新页面
            </div>
          }
        >
          <Suspense fallback={<PageLoader />}>
            <Routes>
              <Route path="/login" element={<Login />} />
              <Route element={<RequireAuth />}>
                <Route element={<Layout />}>
                  <Route path="/" element={<Navigate to="/dashboard" replace />} />
                  <Route
                    path="/dashboard"
                    element={
                      <PageShell>
                        <Dashboard />
                      </PageShell>
                    }
                  />

                  {/* Trading - old routes redirect to flat routes */}
                  <Route
                    path="/trading"
                    element={
                      <PageShell>
                        <Navigate to="/trading/spot" replace />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/trading/spot"
                    element={
                      <PageShell>
                        <TradingSpot />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/trading/contract"
                    element={
                      <PageShell>
                        <TradingContract />
                      </PageShell>
                    }
                  />

                  <Route
                    path="/strategy"
                    element={
                      <PageShell>
                        <Strategy />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/strategy/editor"
                    element={
                      <PageShell>
                        <StrategyEditor />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/backtest"
                    element={
                      <PageShell>
                        <Backtest />
                      </PageShell>
                    }
                  />

                  {/* AI - old routes redirect to flat routes */}
                  <Route
                    path="/ai"
                    element={
                      <PageShell>
                        <Navigate to="/ai/analysis" replace />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/market"
                    element={
                      <PageShell>
                        <Market />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/ai/analysis"
                    element={
                      <PageShell>
                        <AIAnalysis />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/ai/freqai"
                    element={
                      <PageShell>
                        <FreqAI />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/ai/rl"
                    element={
                      <PageShell>
                        <RLTraining />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/ai/tensorboard"
                    element={
                      <PageShell>
                        <TensorBoard />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/model-management"
                    element={
                      <PageShell>
                        <ModelManagement />
                      </PageShell>
                    }
                  />

                  {/* Bots - old routes redirect to flat routes */}
                  <Route
                    path="/bots"
                    element={
                      <PageShell>
                        <Navigate to="/bots/strategy" replace />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/ai-bots"
                    element={
                      <PageShell>
                        <Navigate to="/bots/ai" replace />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/bots/strategy"
                    element={
                      <PageShell>
                        <BotsStrategy />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/bots/signal"
                    element={
                      <PageShell>
                        <BotsSignal />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/bots/ai"
                    element={
                      <PageShell>
                        <BotsAI />
                      </PageShell>
                    }
                  />

                  <Route
                    path="/settings"
                    element={
                      <PageShell>
                        <Settings />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/exchange-account"
                    element={
                      <PageShell>
                        <ExchangeAccount />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/indicator-community"
                    element={
                      <PageShell>
                        <IndicatorCommunity />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/indicator-community/:id"
                    element={
                      <PageShell>
                        <IndicatorDetail />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/author-dashboard"
                    element={
                      <PageShell>
                        <AuthorDashboard />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/portfolio"
                    element={
                      <PageShell>
                        <Portfolio />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/indicator-ide"
                    element={
                      <PageShell>
                        <IndicatorIDE />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/risk-control"
                    element={
                      <PageShell>
                        <RiskControl />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/pairlist"
                    element={
                      <PageShell>
                        <PairlistManagement />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/advanced-orders"
                    element={
                      <PageShell>
                        <AdvancedOrderManagement />
                      </PageShell>
                    }
                  />

                  {/* Arbitrage - old routes redirect to flat routes */}
                  <Route
                    path="/arbitrage"
                    element={
                      <PageShell>
                        <Navigate to="/arbitrage/cross" replace />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/arbitrage/cross"
                    element={
                      <PageShell>
                        <ArbitrageCross />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/arbitrage/triangular"
                    element={
                      <PageShell>
                        <ArbitrageTriangular />
                      </PageShell>
                    }
                  />

                  <Route
                    path="/hyperopt"
                    element={
                      <PageShell>
                        <HyperoptManagement />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/social-trading"
                    element={
                      <PageShell>
                        <SocialTrading />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/onchain"
                    element={
                      <PageShell>
                        <OnChain />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/profile"
                    element={
                      <PageShell>
                        <UserProfile />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/users"
                    element={
                      <PageShell>
                        <UserManage />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/agent-tokens"
                    element={
                      <PageShell>
                        <AgentTokens />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/billing"
                    element={
                      <PageShell>
                        <Billing />
                      </PageShell>
                    }
                  />

                  {/* P0 new pages */}
                  <Route
                    path="/status"
                    element={
                      <PageShell>
                        <SystemStatus />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/data"
                    element={
                      <PageShell>
                        <DataManager />
                      </PageShell>
                    }
                  />
                  <Route
                    path="/logs"
                    element={
                      <PageShell>
                        <Logs />
                      </PageShell>
                    }
                  />

                  <Route
                    path="/strategy-leaderboard"
                    element={
                      <PageShell>
                        <StrategyLeaderboard />
                      </PageShell>
                    }
                  />
                  {/* 404 catch-all */}
                  <Route path="*" element={<NotFound />} />
                </Route>
              </Route>
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </BrowserRouter>
    </I18nProvider>
  )
}
