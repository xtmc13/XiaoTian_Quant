import { useState, useCallback } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAppStore } from '@/stores/appStore'
import { useAuthStore } from '@/stores/authStore'
import {
  BarChart3,
  LineChart,
  Cpu,
  FlaskConical,
  Bot,
  Settings,
  PieChart,
  User,
  Users,
  Key,
  ChevronDown,
  ArrowLeftRight,
  Share2,
  Layers,
} from 'lucide-react'

interface NavItem {
  path?: string
  label: string
  icon: React.ComponentType<{ className?: string }>
  adminOnly?: boolean
  children?: { path: string; label: string }[]
}

// Helper: check if a child path is active
const isChildActive = (location: ReturnType<typeof useLocation>, childPath: string) => {
  return location.pathname === childPath
}

const navItems: NavItem[] = [
  { path: '/dashboard', label: '仪表盘', icon: BarChart3 },

  // 市场数据
  { path: '/market', label: '市场数据', icon: BarChart3 },

  // 交易
  {
    label: '交易',
    icon: LineChart,
    children: [
      { path: '/trading/spot', label: '现货交易' },
      { path: '/trading/contract', label: '合约交易' },
    ],
  },

  // 策略实验室
  {
    label: '策略实验室',
    icon: FlaskConical,
    children: [
      { path: '/strategy', label: '策略管理' },
      { path: '/strategy/editor', label: '策略编辑器' },
      { path: '/backtest', label: '回测' },
      { path: '/indicator-ide', label: '指标 IDE' },
      { path: '/indicator-community', label: '指标市场' },
      { path: '/strategy-leaderboard', label: '排行榜' },
    ],
  },

  // AI 研究
  {
    label: 'AI 研究',
    icon: Cpu,
    children: [
      { path: '/ai/analysis', label: 'AI 分析' },
      { path: '/ai/freqai', label: 'FreqAI' },
      { path: '/ai/rl', label: 'RL 强化学习' },
      { path: '/ai/tensorboard', label: 'TensorBoard' },
      { path: '/model-management', label: '模型管理' },
    ],
  },

  // 机器人中心
  {
    label: '机器人中心',
    icon: Bot,
    children: [
      { path: '/bots/strategy', label: '策略机器人' },
      { path: '/bots/signal', label: '信号机器人' },
      { path: '/bots/ai', label: 'AI 机器人' },
    ],
  },

  // 套利
  {
    label: '套利',
    icon: ArrowLeftRight,
    children: [
      { path: '/arbitrage/cross', label: '跨所套利' },
      { path: '/arbitrage/triangular', label: '三角套利' },
    ],
  },

  // 资产与风控
  {
    label: '资产与风控',
    icon: PieChart,
    children: [
      { path: '/portfolio', label: '资产监测' },
      { path: '/risk-control', label: '风控中心' },
    ],
  },

  // 系统与数据
  {
    label: '系统与数据',
    icon: Layers,
    children: [
      { path: '/data', label: '数据下载' },
      { path: '/status', label: '系统状态' },
      { path: '/logs', label: '系统日志' },
    ],
  },

  // 账户中心
  {
    label: '账户中心',
    icon: User,
    children: [
      { path: '/profile', label: '个人资料' },
      { path: '/billing', label: '订阅' },
      { path: '/exchange-account', label: '交易所账户' },
    ],
  },

  // 社区
  {
    label: '社区',
    icon: Share2,
    children: [
      { path: '/social-trading', label: '信号市场' },
      { path: '/author-dashboard', label: '作者后台' },
    ],
  },

  // 高级
  {
    label: '高级',
    icon: Layers,
    children: [
      { path: '/pairlist', label: '交易对筛选' },
      { path: '/advanced-orders', label: '高级订单' },
      { path: '/hyperopt', label: '参数优化' },
      { path: '/onchain', label: '链上数据' },
    ],
  },

  { path: '/users', label: '用户管理', icon: Users, adminOnly: true },
  { path: '/agent-tokens', label: 'Agent令牌', icon: Key, adminOnly: true },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, setSidebarCollapsed, sidebarBehavior, toggleSidebar } = useAppStore()
  const { user } = useAuthStore()
  const isAdmin = user?.role === 'admin'

  // Auto-expand groups whose child is currently active
  const initiallyExpanded = navItems
    .filter((item) => item.children?.some((child) => isChildActive(location, child.path)))
    .map((item) => item.label)
    .join('|')

  const [expandedItem, setExpandedItem] = useState<string | null>(initiallyExpanded || null)
  const isHover = sidebarBehavior === 'hover'

  const handleMouseEnter = useCallback(() => {
    if (isHover) setSidebarCollapsed(false)
  }, [setSidebarCollapsed, isHover])
  const handleMouseLeave = useCallback(() => {
    if (isHover) {
      setSidebarCollapsed(true)
      setExpandedItem(null)
    }
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
      <div className="h-14 flex items-center px-3 border-b border-quant-border cursor-pointer" onClick={handleToggle}>
        <Link
          to="/"
          className="flex items-center gap-2 text-quant-gold font-bold tracking-tight"
          onClick={(e) => e.stopPropagation()}
        >
          <span className="w-7 h-7 bg-quant-gold rounded-md flex items-center justify-center text-white text-sm font-black shrink-0">
            小
          </span>
          {!sidebarCollapsed && <span className="truncate">小天量化</span>}
        </Link>
      </div>

      {/* Nav */}
      <nav className="flex-1 overflow-y-auto py-3 px-1.5 space-y-1">
        {navItems
          .filter((item) => !item.adminOnly || isAdmin)
          .map((item) => {
            const active = item.path
              ? location.pathname === item.path
              : (item.children?.some((child) => isChildActive(location, child.path)) ?? false)
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
                    onMouseEnter={() => {
                      if (isHover) setExpandedItem(item.label)
                    }}
                    aria-expanded={isExpanded}
                    className={cn(
                      'w-full flex items-center gap-3 px-2 py-2.5 rounded-md text-sm font-medium transition-colors',
                      active
                        ? 'bg-quant-gold/10 text-quant-gold'
                        : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                    )}
                    title={item.label}
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
                        const childActive = isChildActive(location, child.path)
                        return (
                          <Link
                            key={child.path}
                            to={child.path}
                            onClick={() => {
                              if (!isHover) {
                                setSidebarCollapsed(true)
                                setExpandedItem(null)
                              }
                            }}
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
                onClick={() => {
                  if (!isHover) setSidebarCollapsed(true)
                }}
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
