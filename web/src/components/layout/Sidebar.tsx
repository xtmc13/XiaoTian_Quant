import { useState, useCallback } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAppStore } from '@/stores/appStore'
import { useAuthStore } from '@/stores/authStore'
import {
  BarChart3,
  LineChart,
  Wallet,
  Cpu,
  FlaskConical,
  Bot,
  Settings,
  ShoppingBag,
  PieChart,
  Code2,
  User,
  Users,
  Key, Globe,
  ChevronDown,
  BrainCircuit,
  Shield,
  ListFilter,
  ArrowUpDown,
  ArrowLeftRight,
  Search,
  Link2,
  Share2,
} from 'lucide-react'

interface NavItem {
  path?: string
  label: string
  icon: React.ComponentType<{ className?: string }>
  adminOnly?: boolean
  children?: { path: string; label: string }[]
}

const navItems: NavItem[] = [
  { path: '/dashboard', label: '仪表盘', icon: BarChart3 },
  {
    label: '交易', icon: LineChart,
    children: [
      { path: '/trading?mode=spot', label: '现货交易' },
      { path: '/trading?mode=contract', label: '合约交易' },
    ],
  },
  { path: '/strategy', label: '策略', icon: Wallet },
  { path: '/indicator-ide', label: '指标IDE', icon: Code2 },
  { path: '/ai', label: 'AI研究', icon: Cpu },
  { path: '/backtest', label: '回测', icon: FlaskConical },
  {
    label: '机器人', icon: Bot,
    children: [
      { path: '/bots?type=strategy', label: '策略机器人' },
      { path: '/bots?type=signal', label: '信号机器人' },
      { path: '/bots?type=ai', label: 'AI 机器人' },
    ],
  },
  { path: '/model-management', label: 'ML模型', icon: BrainCircuit },
  { path: '/risk-control', label: '风控中心', icon: Shield },
  { path: '/pairlist', label: '交易对筛选', icon: ListFilter },
  { path: '/advanced-orders', label: '高级订单', icon: ArrowUpDown },
  { path: '/arbitrage', label: '套利监控', icon: ArrowLeftRight },
  { path: '/hyperopt', label: '参数优化', icon: Search },
  { path: '/social-trading', label: '社交交易', icon: Share2 },
  { path: '/onchain', label: '链上数据', icon: Link2 },
  { path: '/exchange-account', label: '账户', icon: Wallet },
  { path: '/indicator-community', label: '指标市场', icon: ShoppingBag },
  { path: '/author-dashboard', label: '作者后台', icon: BarChart3 },
  { path: '/portfolio', label: '资产监测', icon: PieChart },
  { path: '/billing', label: '会员', icon: ShoppingBag },
  { path: '/profile', label: '个人中心', icon: User },
  { path: '/users', label: '用户管理', icon: Users, adminOnly: true },
  { path: '/agent-tokens', label: 'Agent令牌', icon: Key, adminOnly: true },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, setSidebarCollapsed, sidebarBehavior, toggleSidebar } = useAppStore()
  const { user } = useAuthStore()
  const isAdmin = user?.role === 'admin'
  const [expandedItem, setExpandedItem] = useState<string | null>(null)
  const isHover = sidebarBehavior === 'hover'

  const handleMouseEnter = useCallback(() => {
    if (isHover) setSidebarCollapsed(false)
  }, [setSidebarCollapsed, isHover])
  const handleMouseLeave = useCallback(() => {
    if (isHover) { setSidebarCollapsed(true); setExpandedItem(null) }
  }, [setSidebarCollapsed, isHover])
  const handleToggle = useCallback(() => {
    if (!isHover) toggleSidebar()
  }, [isHover, toggleSidebar])

  return (
    <aside
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      className={cn(
        'flex flex-col bg-quant-bg border-r border-quant-border shrink-0 transition-all duration-200',
        sidebarCollapsed ? 'w-14' : 'w-40'
      )}
    >
      {/* Logo */}
      <div
        className="h-14 flex items-center px-3 border-b border-quant-border cursor-pointer"
        onClick={handleToggle}
      >
        <Link to="/" className="flex items-center gap-2 text-quant-gold font-bold tracking-tight" onClick={e => e.stopPropagation()}>
          <span className="w-7 h-7 bg-quant-gold rounded-md flex items-center justify-center text-white text-sm font-black shrink-0">
            小
          </span>
          {!sidebarCollapsed && <span className="truncate">小天量化</span>}
        </Link>
      </div>

      {/* Nav */}
      <nav className="flex-1 overflow-y-auto py-3 px-1.5 space-y-1">
        {navItems.filter(item => !item.adminOnly || isAdmin).map((item) => {
          const active = item.path
            ? location.pathname === item.path || (item.children && item.children.some(c => location.search && location.pathname + location.search === c.path))
            : item.children?.some(c => location.pathname + (location.search || '') === c.path || location.pathname === c.path.split('?')[0])
          const hasChildren = !!item.children
          const isExpanded = expandedItem === item.label

          if (hasChildren) {
            return (
              <div key={item.label}>
                {/* Parent item */}
                <button
                  onClick={() => {
                    if (sidebarCollapsed) setSidebarCollapsed(false)
                    setExpandedItem(isExpanded ? null : item.label)
                  }}
                  onMouseEnter={() => { if (isHover) setExpandedItem(item.label) }}
                  aria-expanded={isExpanded}
                  className={cn(
                    'w-full flex items-center gap-3 px-2 py-2.5 rounded-md text-sm font-medium transition-colors',
                    active
                      ? 'bg-quant-gold/10 text-quant-gold'
                      : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                  )}
                  title="交易"
                >
                  <item.icon className="w-[18px] h-[18px] shrink-0" />
                  {!sidebarCollapsed && (
                    <>
                      <span className="flex-1 text-left">{item.label}</span>
                      <ChevronDown className={cn('w-3 h-3 transition-transform', isExpanded && 'rotate-180')} />
                    </>
                  )}
                </button>

                {/* Sub items */}
                {isExpanded && !sidebarCollapsed && (
                  <div className="ml-6 mt-0.5 space-y-0.5">
                    {item.children!.map((child) => {
                      const childActive = location.pathname + location.search === child.path
                      return (
                        <Link
                          key={child.path}
                          to={child.path}
                          onClick={() => { if (!isHover) { setSidebarCollapsed(true); setExpandedItem(null) } }}
                          aria-current={childActive ? 'page' : undefined}
                          className={cn(
                            'flex items-center gap-2 px-2 py-2 rounded-md text-xs font-medium transition-colors',
                            childActive
                              ? 'text-quant-gold bg-quant-gold/5'
                              : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                          )}
                        >
                          <span className="w-1 h-1 rounded-full bg-current opacity-50" />
                          {child.label}
                        </Link>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          }

          return (
            <Link
              key={item.path}
              to={item.path!}
              onClick={() => { if (!isHover) setSidebarCollapsed(true) }}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'flex items-center gap-3 px-2 py-2.5 rounded-md text-sm font-medium transition-colors',
                active
                  ? 'bg-quant-gold/10 text-quant-gold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
              )}
              title={sidebarCollapsed ? item.label : undefined}
            >
              <item.icon className="w-[18px] h-[18px] shrink-0" />
              {!sidebarCollapsed && <span>{item.label}</span>}
            </Link>
          )
        })}
      </nav>

      {/* Bottom: Settings */}
      <div className="p-2 border-t border-quant-border">
        <Link
          to="/settings"
          aria-current={location.pathname === '/settings' ? 'page' : undefined}
          className={cn(
            'flex items-center gap-3 px-2 py-2.5 rounded-md text-sm font-medium transition-colors',
            location.pathname === '/settings'
              ? 'bg-quant-gold/10 text-quant-gold'
              : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
          )}
          title={sidebarCollapsed ? '设置' : undefined}
        >
          <Settings className="w-[18px] h-[18px] shrink-0" />
          {!sidebarCollapsed && <span>设置</span>}
        </Link>
      </div>
    </aside>
  )
}
