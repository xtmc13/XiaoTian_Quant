import { useEffect, useState, useRef, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { useAuthStore } from '@/stores/authStore'
import { cn } from '@/lib/utils'
import { portfolioApi, notificationApi } from '@/lib/api'
import { LogOut, Bell, CheckCheck, Trash2, AlertTriangle, Info, Zap } from 'lucide-react'
import { useI18n, LANGS, type Lang } from '@/i18n'

const routeTitles: Record<string, string> = {
  '/dashboard': '仪表盘',
  '/trading': '交易',
  '/strategy': '策略工厂',
  '/ai': 'AI 研究',
  '/backtest': '回测验证',
  '/bots': '交易机器人',
  '/settings': '系统设置',
  '/exchange-account': '交易所账户',
  '/indicator-community': '指标社区',
  '/indicator-ide': '指标 IDE',
  '/portfolio': '资产监测',
  '/profile': '个人中心',
  '/users': '用户管理',
  '/agent-tokens': 'Agent 令牌',
  '/market-overview': '全球市场',
  '/bot-wizard': 'Bot 向导',
  '/social-trading': '社交交易',
  '/onchain': '链上数据',
}

import { NotificationItem } from '@/types'

const levelIcon: Record<string, React.ReactNode> = {
  CRITICAL: <AlertTriangle className="h-3.5 w-3.5 text-quant-red" />,
  WARN: <Zap className="h-3.5 w-3.5 text-quant-orange" />,
  INFO: <Info className="h-3.5 w-3.5 text-quant-blue" />,
}

export function TopBar() {
  const location = useLocation()
  const { user, logout, isAuthenticated } = useAuthStore()
  const { lang, setLang } = useI18n()
  const [time, setTime] = useState(new Date())
  const [equity, setEquity] = useState<number | null>(null)
  const [pnl, setPnl] = useState<number | null>(null)
  const [menuOpen, setMenuOpen] = useState(false)
  const [notifOpen, setNotifOpen] = useState(false)
  const [notifications, setNotifications] = useState<NotificationItem[]>([])
  const [unreadCount, setUnreadCount] = useState(0)
  const notifRef = useRef<HTMLDivElement>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    const timer = setInterval(() => setTime(new Date()), 1000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    if (!isAuthenticated) return
    portfolioApi.summary()
      .then((data) => {
        setEquity(data.total_equity ?? null)
        setPnl(data.total_pnl ?? null)
      })
      .catch(() => {})
  }, [isAuthenticated])

  const fetchNotifications = useCallback(async () => {
    if (!isAuthenticated) return
    try {
      const items = await notificationApi.list({ limit: 20 })
      setNotifications(items)
      const count = await notificationApi.unreadCount()
      setUnreadCount(count)
    } catch { /* ignore */ }
  }, [isAuthenticated])

  useEffect(() => {
    if (!isAuthenticated) return
    fetchNotifications()
    pollRef.current = setInterval(fetchNotifications, 15000)
    return () => { if (pollRef.current !== null) clearInterval(pollRef.current) }
  }, [fetchNotifications, isAuthenticated])

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (notifRef.current && !notifRef.current.contains(e.target as Node)) {
        setNotifOpen(false)
      }
    }
    if (notifOpen) document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [notifOpen])

  const handleMarkRead = async (id: number) => {
    await notificationApi.markRead(id)
    setNotifications((prev) => prev.map((n) => n.id === id ? { ...n, read: true } : n))
    setUnreadCount((c) => Math.max(0, c - 1))
  }

  const handleMarkAllRead = async () => {
    await notificationApi.markAllRead()
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })))
    setUnreadCount(0)
  }

  const handleClear = async () => {
    await notificationApi.clear()
    setNotifications([])
    setUnreadCount(0)
  }

  const formatNotifTime = (ts: number) => {
    const d = new Date(ts)
    const now = new Date()
    const diffMs = now.getTime() - d.getTime()
    if (diffMs < 60000) return '刚刚'
    if (diffMs < 3600000) return `${Math.floor(diffMs / 60000)}分钟前`
    if (diffMs < 86400000) return `${Math.floor(diffMs / 3600000)}小时前`
    return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
  }

  const title = (() => {
    if (location.pathname === '/trading') {
      const mode = new URLSearchParams(location.search).get('mode')
      if (mode === 'contract') return '合约交易'
      return '现货交易'
    }
    return routeTitles[location.pathname] || '小天量化'
  })()
  const displayName = user?.username || 'User'
  const initial = displayName.charAt(0).toUpperCase()

  return (
    <header className="h-14 bg-quant-bg-secondary border-b border-quant-border flex items-center justify-between px-5 shrink-0 z-50">
      <h1 className="text-base font-semibold">{title}</h1>

      <div className="flex items-center gap-4">
        <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="w-1.5 h-1.5 rounded-full bg-quant-green animate-pulse" />
          已连接
        </span>
        {equity !== null && (
          <span className="text-xs text-muted-foreground">
            权益 <b className="text-foreground font-mono">${equity.toLocaleString(undefined, { minimumFractionDigits: 2 })}</b>
          </span>
        )}
        {pnl !== null && (
          <span className={cn('text-xs font-mono', pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
            {pnl >= 0 ? '+' : ''}${pnl.toLocaleString(undefined, { minimumFractionDigits: 2 })}
          </span>
        )}
        <span className="font-mono text-xs text-muted-foreground tabular-nums">
          {time.toLocaleTimeString('zh-CN', { hour12: false })}
        </span>

        {/* ── Notification Bell ── */}
        <div className="relative" ref={notifRef}>
          <button
            onClick={() => { setNotifOpen(!notifOpen); if (!notifOpen) fetchNotifications() }}
            className="relative p-1.5 rounded-lg text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
            aria-label="通知"
          >
            <Bell className="h-4 w-4" />
            {unreadCount > 0 && (
              <span className="absolute -top-0.5 -right-0.5 min-w-[16px] h-4 px-1 rounded-full bg-quant-red text-white text-[9px] font-bold flex items-center justify-center">
                {unreadCount > 99 ? '99+' : unreadCount}
              </span>
            )}
          </button>

          {notifOpen && (
            <div className="absolute right-0 top-full mt-2 w-80 rounded-xl border border-quant-border bg-quant-card shadow-xl z-50 max-h-[480px] flex flex-col">
              {/* Header */}
              <div className="flex items-center justify-between px-4 py-3 border-b border-quant-border shrink-0">
                <span className="text-sm font-semibold">通知</span>
                <div className="flex items-center gap-1">
                  {unreadCount > 0 && (
                    <button
                      onClick={handleMarkAllRead}
                      className="p-1 rounded text-[10px] text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
                      title="全部已读"
                    >
                      <CheckCheck className="h-3.5 w-3.5" />
                    </button>
                  )}
                  {notifications.length > 0 && (
                    <button
                      onClick={handleClear}
                      className="p-1 rounded text-[10px] text-muted-foreground hover:text-quant-red hover:bg-red-500/10 transition-colors"
                      title="清除全部"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </div>

              {/* List */}
              <div className="overflow-y-auto flex-1">
                {notifications.length === 0 ? (
                  <div className="flex flex-col items-center justify-center py-10 text-muted-foreground">
                    <Bell className="h-8 w-8 mb-2 opacity-30" />
                    <span className="text-xs">暂无通知</span>
                  </div>
                ) : (
                  notifications.map((n) => (
                    <button
                      key={n.id}
                      onClick={() => !n.read && handleMarkRead(n.id)}
                      className={cn(
                        'w-full text-left px-4 py-3 border-b border-quant-border/30 transition-colors hover:bg-white/[0.02]',
                        !n.read && 'bg-quant-gold/[0.03]'
                      )}
                    >
                      <div className="flex items-start gap-2.5">
                        <div className="mt-0.5 shrink-0">
                          {levelIcon[n.level || 'INFO'] || levelIcon.INFO}
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <span className={cn('text-xs font-medium', !n.read && 'text-foreground')}>
                              {n.title}
                            </span>
                            {!n.read && (
                              <span className="w-1.5 h-1.5 rounded-full bg-quant-gold shrink-0" />
                            )}
                          </div>
                          <p className="text-[11px] text-muted-foreground mt-0.5 line-clamp-2">
                            {n.content}
                          </p>
                          <span className="text-[10px] text-muted-foreground/60 mt-1 block">
                            {formatNotifTime(n.created_at)}
                          </span>
                        </div>
                      </div>
                    </button>
                  ))
                )}
              </div>
            </div>
          )}
        </div>

        {/* Language Switcher */}
        <select
          value={lang}
          onChange={(e) => setLang(e.target.value as Lang)}
          aria-label="切换语言"
          className="bg-quant-bg border border-quant-border rounded px-1.5 py-0.5 text-[10px] text-muted-foreground outline-none focus:border-quant-gold cursor-pointer"
        >
          {LANGS.map((l) => (
            <option key={l.code} value={l.code}>{l.flag} {l.label}</option>
          ))}
        </select>
        <span className="text-[10px] text-muted-foreground">v3.0.0</span>

        {/* User menu */}
        <div className="relative">
          <button
            onClick={() => setMenuOpen(!menuOpen)}
            className="w-7 h-7 rounded-full bg-quant-gold/20 text-quant-gold flex items-center justify-center text-xs font-bold hover:bg-quant-gold/30 transition-colors"
            title={displayName}
          >
            {initial}
          </button>
          {menuOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setMenuOpen(false)} onKeyDown={(e) => { if (e.key === 'Escape') setMenuOpen(false) }} tabIndex={-1} role="presentation" />
              <div className="absolute right-0 top-full mt-2 w-48 rounded-xl border border-quant-border bg-quant-card shadow-xl z-50 py-1">
                <div className="px-3 py-2 border-b border-quant-border">
                  <div className="text-sm font-medium text-foreground">{displayName}</div>
                  <div className="text-[11px] text-muted-foreground capitalize">{user?.role || 'user'}</div>
                </div>
                <button
                  onClick={() => { setMenuOpen(false); logout() }}
                  className="w-full px-3 py-2 text-left text-xs text-muted-foreground hover:bg-quant-bg-secondary hover:text-foreground flex items-center gap-2 transition-colors"
                >
                  <LogOut className="h-3.5 w-3.5" />
                  退出登录
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </header>
  )
}
