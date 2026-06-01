import { useNavigate, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { LayoutDashboard, TrendingUp, BrainCircuit, BarChart3, Settings } from 'lucide-react'

const NAV_ITEMS = [
  { path: '/dashboard', label: '仪表盘', icon: LayoutDashboard },
  { path: '/trading', label: '交易', icon: TrendingUp },
  { path: '/ai', label: 'AI', icon: BrainCircuit },
  { path: '/strategy', label: '策略', icon: BarChart3 },
  { path: '/settings', label: '设置', icon: Settings },
]

export function BottomNav() {
  const navigate = useNavigate()
  const location = useLocation()

  return (
    <nav className="md:hidden fixed bottom-0 left-0 right-0 z-50 bg-quant-bg-secondary border-t border-quant-border safe-area-bottom">
      <div className="flex items-center justify-around h-14">
        {NAV_ITEMS.map((item) => {
          const active = location.pathname === item.path || location.pathname.startsWith(item.path + '/')
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
      </div>
    </nav>
  )
}
