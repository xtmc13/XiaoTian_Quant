import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useI18n } from '@/i18n'
import {
  LayoutDashboard, TrendingUp, BrainCircuit, Settings,
  ChevronUp, X, Bot, FlaskConical, BarChart3, LineChart,
  Cpu, Shield, Wallet, Zap, Code, ShoppingBag, Users, Link2
} from 'lucide-react'

const MAIN_NAV = [
  { path: '/dashboard', key: 'nav.dashboard', icon: LayoutDashboard },
  { path: '/trading', key: 'nav.trading', icon: TrendingUp },
  { path: '/ai', key: 'nav.ai', icon: BrainCircuit },
  { path: '/settings', key: 'nav.settings', icon: Settings },
]

const MORE_NAV = [
  { path: '/bots', key: 'nav.bots', icon: Bot },
  { path: '/strategy', key: 'nav.strategy', icon: Zap },
  { path: '/backtest', key: 'nav.backtest', icon: FlaskConical },
  { path: '/portfolio', key: 'nav.portfolio', icon: Wallet },
  { path: '/risk-control', key: 'nav.riskControl', icon: Shield },
  { path: '/hyperopt', key: 'nav.hyperopt', icon: Cpu },
  { path: '/model-management', key: 'nav.modelManagement', icon: LineChart },
  { path: '/indicator-community', key: 'nav.indicatorCommunity', icon: ShoppingBag },
  { path: '/indicator-ide', key: 'nav.indicatorIDE', icon: Code },
  { path: '/arbitrage', key: 'nav.arbitrage', icon: BarChart3 },
  { path: '/social-trading', key: 'nav.socialTrading', icon: Users },
  { path: '/onchain', key: 'nav.onchain', icon: Link2 },
]

export function BottomNav() {
  const navigate = useNavigate()
  const location = useLocation()
  const [showMore, setShowMore] = useState(false)
  const { t } = useI18n()

  const isActive = (path: string) =>
    location.pathname === path || location.pathname.startsWith(path + '/')

  return (
    <>
      <nav className="md:hidden fixed bottom-0 left-0 right-0 z-50 bg-quant-bg-secondary border-t border-quant-border safe-area-bottom">
        <div className="flex items-center justify-around h-14">
          {MAIN_NAV.map((item) => {
            const active = isActive(item.path)
            return (
              <button
                key={item.path}
                onClick={() => navigate(item.path)}
                className={cn(
                  'flex flex-col items-center justify-center gap-0.5 px-3 py-1.5 min-w-0 flex-1 transition-colors',
                  active ? 'text-quant-gold' : 'text-muted-foreground'
                )}
              >
                <item.icon className={cn('h-5 w-5', active && 'text-quant-gold')} />
                <span className="text-[10px] font-medium truncate">{t(item.key)}</span>
              </button>
            )
          })}
          <button
            onClick={() => setShowMore(true)}
            className={cn(
              'flex flex-col items-center justify-center gap-0.5 px-3 py-1.5 min-w-0 flex-1 transition-colors',
              showMore ? 'text-quant-gold' : 'text-muted-foreground'
            )}
          >
            <ChevronUp className={cn('h-5 w-5', showMore && 'text-quant-gold')} />
            <span className="text-[10px] font-medium truncate">{t('common.more')}</span>
          </button>
        </div>
      </nav>

      {/* More Drawer */}
      {showMore && (
        <div className="md:hidden fixed inset-0 z-[60] bg-black/60 backdrop-blur-sm" onClick={() => setShowMore(false)} onKeyDown={(e) => { if (e.key === 'Escape') setShowMore(false) }} tabIndex={-1} role="presentation">
          <div
            className="absolute bottom-16 left-2 right-2 rounded-xl border border-quant-border bg-quant-card shadow-2xl p-4"
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
          >
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs font-semibold text-muted-foreground">{t('common.all')}</span>
              <button onClick={() => setShowMore(false)} className="p-1 rounded text-muted-foreground hover:text-foreground">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="grid grid-cols-5 gap-2">
              {MORE_NAV.map((item) => {
                const active = isActive(item.path)
                return (
                  <button
                    key={item.path}
                    onClick={() => { navigate(item.path); setShowMore(false) }}
                    className={cn(
                      'flex flex-col items-center gap-1 p-2 rounded-lg transition-colors',
                      active ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:bg-white/5 hover:text-foreground'
                    )}
                  >
                    <item.icon className="h-5 w-5" />
                    <span className="text-[10px] font-medium">{t(item.key)}</span>
                  </button>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </>
  )
}
