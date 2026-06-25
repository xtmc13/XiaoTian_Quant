// Central route prefetch map used by App.tsx (lazy routes) and Sidebar.tsx (hover preload).
export const pageLoaders: Record<string, () => Promise<unknown>> = {
  '/dashboard':            () => import('@/pages/Dashboard'),
  '/trading':              () => import('@/pages/Trading'),
  '/strategy':             () => import('@/pages/Strategy'),
  '/ai':                   () => import('@/pages/AI'),
  '/backtest':             () => import('@/pages/Backtest'),
  '/bots':                 () => import('@/pages/Bots'),
  '/ai-bots':              () => import('@/pages/AIBots'),
  '/settings':             () => import('@/pages/Settings'),
  '/exchange-account':     () => import('@/pages/ExchangeAccount'),
  '/indicator-community':  () => import('@/pages/IndicatorCommunity'),
  '/portfolio':            () => import('@/pages/Portfolio'),
  '/indicator-ide':        () => import('@/pages/IndicatorIDE'),
  '/model-management':     () => import('@/pages/ModelManagement'),
  '/risk-control':         () => import('@/pages/RiskControl'),
  '/pairlist':             () => import('@/pages/PairlistManagement'),
  '/advanced-orders':      () => import('@/pages/AdvancedOrderManagement'),
  '/arbitrage':            () => import('@/pages/ArbitrageMonitor'),
  '/hyperopt':             () => import('@/pages/HyperoptManagement'),
  '/social-trading':       () => import('@/pages/SocialTrading'),
  '/onchain':              () => import('@/pages/OnChain'),
  '/profile':              () => import('@/pages/UserProfile'),
  '/users':                () => import('@/pages/UserManage'),
  '/agent-tokens':         () => import('@/pages/AgentTokens'),
  '/billing':              () => import('@/pages/Billing'),
  '/strategy-leaderboard': () => import('@/pages/StrategyLeaderboard'),
  '/author-dashboard':     () => import('@/pages/AuthorDashboard'),
}

export function prefetchRoute(path: string) {
  const key = path.split('?')[0]
  const loader = pageLoaders[key]
  if (loader) {
    loader().catch(() => {})
  }
}
