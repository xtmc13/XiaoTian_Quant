import { useState, useCallback } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useAppStore } from '@/stores/appStore'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/authStore'
import {
  BarChart3,
  LineChart,
  Brain,
  BrainCircuit,
  FlaskConical,
  LayoutGrid,
  Settings,
  PieChart,
  Users,
  Key,
  ChevronDown,
  ArrowLeftRight,
  Share2,
  Layers,
} from 'lucide-react'

interface NavItem {
  path?: string
  labelKey: string
  icon: React.ComponentType<{ className?: string }>
  adminOnly?: boolean
  children?: { path: string; labelKey: string }[]
}

// Helper: check if a child path is active
const isChildActive = (location: ReturnType<typeof useLocation>, childPath: string) => {
  return location.pathname === childPath
}

const navItems: NavItem[] = [
  { path: '/dashboard', labelKey: 'nav.dashboard', icon: BarChart3 },

  // 第一页：纯机器人管理（网格/马丁/华尔街/AI 实例；新建走右上角模板向导）。
  // DCA 定投与分层马丁格尔为独立管理页（A1.2/A1.3），收在 Bots 菜单组下。
  {
    labelKey: 'nav.bots',
    icon: LayoutGrid,
    children: [
      { path: '/bots', labelKey: 'nav.bots-center' },
      { path: '/bots/dca', labelKey: 'nav.dca-bots' },
      { path: '/bots/layered-martin', labelKey: 'nav.layered-martin' },
    ],
  },

  // 策略实验室（AI生成/回测/指标IDE 等研究工具）
  {
    labelKey: 'nav.strategy-lab',
    icon: FlaskConical,
    children: [
      { path: '/strategy', labelKey: 'nav.strategy' },
      { path: '/strategy/editor', labelKey: 'nav.strategy-editor' },
      { path: '/backtest', labelKey: 'nav.backtest' },
      { path: '/backtest/portfolio', labelKey: 'nav.portfolio-backtest' },
      { path: '/factor-research', labelKey: 'nav.factor-research' },
      { path: '/indicator-ide', labelKey: 'nav.indicator-ide' },
      { path: '/indicator-community', labelKey: 'nav.indicator-community' },
      { path: '/strategy-leaderboard', labelKey: 'nav.leaderboard' },
    ],
  },

  // 交易
  {
    labelKey: 'nav.trading',
    icon: LineChart,
    children: [
      { path: '/trading/spot', labelKey: 'nav.trading-spot' },
      { path: '/trading/contract', labelKey: 'nav.trading-contract' },
    ],
  },

  // AI 分析
  { path: '/ai', labelKey: 'nav.ai', icon: Brain },


  // 套利
  {
    labelKey: 'nav.arbitrage',
    icon: ArrowLeftRight,
    children: [
      { path: '/arbitrage/cross', labelKey: 'nav.arbitrage-cross' },
      { path: '/arbitrage/triangular', labelKey: 'nav.arbitrage-triangular' },
    ],
  },

  // 资产与风控
  {
    labelKey: 'nav.assets-risk',
    icon: PieChart,
    children: [
      { path: '/portfolio', labelKey: 'nav.asset-monitor' },
      { path: '/risk-control', labelKey: 'nav.risk-control' },
    ],
  },

  // 社区
  {
    labelKey: 'nav.community',
    icon: Share2,
    children: [{ path: '/social-trading', labelKey: 'nav.signal-market' }],
  },

  // 高级
  {
    labelKey: 'nav.advanced',
    icon: Layers,
    children: [
      { path: '/pairlist', labelKey: 'nav.pairlist' },
      { path: '/advanced-orders', labelKey: 'nav.advanced-orders' },
      { path: '/hyperopt', labelKey: 'nav.hyperopt' },
      { path: '/onchain', labelKey: 'nav.onchain' },
      { path: '/author-dashboard', labelKey: 'nav.author-dashboard' },
    ],
  },

  { path: '/users', labelKey: 'nav.user-mgmt', icon: Users, adminOnly: true },
  { path: '/agent-tokens', labelKey: 'nav.agent-tokens', icon: Key, adminOnly: true },
]

export function Sidebar() {
  const location = useLocation()
  const { sidebarCollapsed, setSidebarCollapsed, sidebarBehavior, toggleSidebar } = useAppStore()
  const { user } = useAuthStore()
  const isAdmin = user?.role === 'admin'
  const { t } = useI18n()

  // Auto-expand groups whose child is currently active
  const initiallyExpanded = navItems
    .filter((item) => item.children?.some((child) => isChildActive(location, child.path)))
    .map((item) => item.labelKey)
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
          <span className="w-7 h-7 bg-quant-gold rounded-md flex items-center justify-center text-foreground text-sm font-black shrink-0">
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
            const isExpanded = expandedItem === item.labelKey

            if (hasChildren) {
              return (
                <div key={item.labelKey}>
                  {/* Parent item */}
                  <button
                    onClick={() => {
                      if (sidebarCollapsed) setSidebarCollapsed(false)
                      setExpandedItem(isExpanded ? null : item.labelKey)
                    }}
                    onMouseEnter={() => {
                      if (isHover) setExpandedItem(item.labelKey)
                    }}
                    aria-expanded={isExpanded}
                    className={cn(
                      'w-full flex items-center gap-3 px-2 py-2.5 rounded-md text-sm font-medium transition-colors',
                      active
                        ? 'bg-quant-gold/10 text-quant-gold'
                        : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                    )}
                    title={t(item.labelKey)}
                  >
                    <item.icon className="w-[18px] h-[18px] shrink-0" />
                    {!sidebarCollapsed && (
                      <>
                        <span className="flex-1 text-left">{t(item.labelKey)}</span>
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
                            {t(child.labelKey)}
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
                title={sidebarCollapsed ? t(item.labelKey) : undefined}
              >
                <item.icon className="w-[18px] h-[18px] shrink-0" />
                {!sidebarCollapsed && <span>{t(item.labelKey)}</span>}
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
