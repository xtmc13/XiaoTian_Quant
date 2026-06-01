import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
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

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuthStore()
  const location = useLocation()

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  return <>{children}</>
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="/*"
          element={
            <RequireAuth>
              <Layout>
                <Routes>
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
                  <Route path="/profile" element={<UserProfile />} />
                  <Route path="/users" element={<UserManage />} />
                  <Route path="/agent-tokens" element={<AgentTokens />} />
                <Route path="/billing" element={<Billing />} />
                </Routes>
              </Layout>
            </RequireAuth>
          }
        />
      </Routes>
    </BrowserRouter>
  )
}
