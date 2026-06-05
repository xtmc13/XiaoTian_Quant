import { BrowserRouter, Routes, Route, Navigate, useLocation, Outlet } from 'react-router-dom'
import { useAuthStore } from '@/stores/authStore'
import { Layout } from './components/layout/Layout'
import { Dashboard } from './pages/Dashboard'
import { Trading } from './pages/Trading'
import { Strategy } from './pages/Strategy'
import { AI } from './pages/AI'
import { Backtest } from './pages/Backtest'
import { Bots } from './pages/Bots'
import { Settings } from './pages/Settings'
import { Login } from './pages/Login'
import { ExchangeAccount } from './pages/ExchangeAccount'
import { IndicatorCommunity } from './pages/IndicatorCommunity'
import { Portfolio } from './pages/Portfolio'
import { IndicatorIDE } from './pages/IndicatorIDE'
import { UserProfile } from './pages/UserProfile'
import { UserManage } from './pages/UserManage'
import { AgentTokens } from './pages/AgentTokens'
import { IndicatorDetail } from './pages/IndicatorDetail'
import { AuthorDashboard } from './pages/AuthorDashboard'
import { Billing } from './pages/Billing'

import { ModelManagement } from './pages/ModelManagement'
import { RiskControl } from './pages/RiskControl'
import { PairlistManagement } from './pages/PairlistManagement'
import { AdvancedOrderManagement } from './pages/AdvancedOrderManagement'
import { ArbitrageMonitor } from './pages/ArbitrageMonitor'
import { HyperoptManagement } from './pages/HyperoptManagement'

function RequireAuth() {
  const { isAuthenticated, hydrated } = useAuthStore()
  const location = useLocation()

  // Wait for zustand persist to finish rehydration before deciding auth state.
  // This prevents a flash of <Navigate> during the initial false -> true transition.
  if (!hydrated) {
    return (
      <div className="h-screen bg-quant-bg flex items-center justify-center">
        <div className="animate-spin h-6 w-6 border-2 border-quant-gold/30 border-t-quant-gold rounded-full" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  return <Outlet />
}

export default function App() {
  return (
    <BrowserRouter>
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
            <Route path="/profile" element={<UserProfile />} />
            <Route path="/users" element={<UserManage />} />
            <Route path="/agent-tokens" element={<AgentTokens />} />
            <Route path="/billing" element={<Billing />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
