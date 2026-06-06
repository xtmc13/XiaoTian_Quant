import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import {
  LayoutDashboard, TrendingUp, BrainCircuit, Settings,
  ChevronUp, X, Bot, FlaskConical, BarChart3, LineChart,
  Cpu, Shield, Wallet, Zap, Code, ShoppingBag, Users, Link2
} from 'lucide-react'

const MAIN_NAV = [
  { path: '/dashboard', label: '仪表盘', icon: LayoutDashboard },
  { path: '/trading', label: '交易', icon: TrendingUp },
  { path: '/ai', label: 'AI', icon: BrainCircuit },
  { path: '/settings', label: '设置', icon: Settings },
]

const MORE_NAV = [
  { path: '/bots', label: '机器人', icon: Bot },
  { path: '/strategy', label: '策略', icon: Zap },
  { path: '/backtest', label: '回测', icon: FlaskConical },
  { path: '/portfolio', label: '持仓', icon: Wallet },
  { path: '/risk-control', label: '风控', icon: Shield },
  { path: '/hyperopt', label: '优化', icon: Cpu },
  { path: '/model-management', label: 'ML模型', icon: LineChart },
  { path: '/indicator-community', label: '指标社区', icon: ShoppingBag },
  { path: '/indicator-ide', label: '指标IDE', icon: Code },
  { path: '/arbitrage', label: '套利', icon: BarChart3 },
  { path: '/social-trading', label: '社交交易', icon: Users },
  { path: '/onchain', label: '链上数据', icon: Link2 },
]

export function BottomNav() {
  const navigate = useNavigate()
  const location = useLocation()
  const [showMore, setShowMore] = useState(false)

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
                <span className="text-[10px] font-medium truncate">{item.label}</span>
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
            <span className="text-[10px] font-medium truncate">更多</span>
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
              <span className="text-xs font-semibold text-muted-foreground">全部功能</span>
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
                    <span className="text-[10px] font-medium">{item.label}</span>
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
