// Central route prefetch map used by App.tsx (lazy routes) and Sidebar.tsx (hover preload).
export const pageLoaders: Record<string, () => Promise<unknown>> = {
  '/dashboard': () => import('@/pages/Dashboard'),
  '/trading': () => import('@/pages/Trading'),
  '/trading/spot': () => import('@/pages/trading/TradingSpot'),
  '/trading/contract': () => import('@/pages/trading/TradingContract'),
  '/strategy': () => import('@/pages/Strategy'),
  '/strategy/editor': () => import('@/pages/strategy/StrategyEditor'),
  '/ai': () => import('@/pages/AI'),
  '/market': () => import('@/pages/Market'),
  '/ai/analysis': () => import('@/pages/ai/AIAnalysis'),
  '/ai/freqai': () => import('@/pages/ai/FreqAI'),
  '/ai/rl': () => import('@/pages/ai/RLTraining'),
  '/ai/tensorboard': () => import('@/pages/ai/TensorBoard'),
  '/backtest': () => import('@/pages/Backtest'),
  '/bots': () => import('@/pages/Bots'),
  '/ai-bots': () => import('@/pages/AIBots'),
  '/bots/strategy': () => import('@/pages/bots/BotsStrategy'),
  '/bots/signal': () => import('@/pages/bots/BotsSignal'),
  '/bots/ai': () => import('@/pages/bots/BotsAI'),
  '/settings': () => import('@/pages/Settings'),
  '/exchange-account': () => import('@/pages/ExchangeAccount'),
  '/indicator-community': () => import('@/pages/IndicatorCommunity'),
  '/portfolio': () => import('@/pages/Portfolio'),
  '/indicator-ide': () => import('@/pages/IndicatorIDE'),
  '/model-management': () => import('@/pages/ModelManagement'),
  '/risk-control': () => import('@/pages/RiskControl'),
  '/pairlist': () => import('@/pages/PairlistManagement'),
  '/advanced-orders': () => import('@/pages/AdvancedOrderManagement'),
  '/arbitrage': () => import('@/pages/ArbitrageMonitor'),
  '/arbitrage/cross': () => import('@/pages/arbitrage/ArbitrageCross'),
  '/arbitrage/triangular': () => import('@/pages/arbitrage/ArbitrageTriangular'),
  '/hyperopt': () => import('@/pages/HyperoptManagement'),
  '/social-trading': () => import('@/pages/SocialTrading'),
  '/onchain': () => import('@/pages/OnChain'),
  '/profile': () => import('@/pages/UserProfile'),
  '/users': () => import('@/pages/UserManage'),
  '/agent-tokens': () => import('@/pages/AgentTokens'),
  '/billing': () => import('@/pages/Billing'),
  '/strategy-leaderboard': () => import('@/pages/StrategyLeaderboard'),
  '/author-dashboard': () => import('@/pages/AuthorDashboard'),
  '/status': () => import('@/pages/SystemStatus'),
  '/data': () => import('@/pages/DataManager'),
  '/logs': () => import('@/pages/Logs'),
}

export function prefetchRoute(path: string) {
  const key = path.split('?')[0]
  const loader = pageLoaders[key]
  if (loader) {
    loader().catch(() => {})
  }
}
