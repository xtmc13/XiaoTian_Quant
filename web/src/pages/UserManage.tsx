import { useState, useEffect, useCallback } from 'react'
import { DataTable } from '@/components/DataTable'
import { AdminMarketReview } from '@/components/market/AdminMarketReview'
import { AdminIndicatorReview } from '@/components/community/AdminIndicatorReview'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { adminApi, type AdminSummary, type AdminActivityItem } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useI18n } from '@/i18n'
import type { AdminUser, AdminStats, AdminAuditLog } from '@/types'
import {
  Users, UserCheck, Shield, Loader2, AlertCircle, CheckCircle,
  Edit3, X, RefreshCw, UserX, UserCog, FileText, Cpu, Activity,
  Database, Zap, Clock, HardDrive
} from 'lucide-react'

// activity 时间戳单位混用（秒/毫秒/字符串），防御性渲染
function fmtActivityTs(ts?: number | string): string {
  if (ts == null || ts === '') return '-'
  if (typeof ts === 'string') return ts.replace('T', ' ').slice(0, 19)
  const ms = ts > 1e12 ? ts : ts * 1000
  return new Date(ms).toLocaleString('zh-CN')
}

const ACTIVITY_TYPE_COLOR: Record<string, string> = {
  trade: 'bg-green-500/10 text-green-400',
  risk: 'bg-red-500/10 text-red-400',
  audit: 'bg-blue-500/10 text-blue-400',
  notification: 'bg-amber-500/10 text-amber-400',
}

export function UserManage() {
  const { t } = useI18n()
  const { confirm, Dialog } = useConfirmDialog()
  const [users, setUsers] = useState<AdminUser[]>([])
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [summary, setSummary] = useState<AdminSummary | null>(null)
  const [activities, setActivities] = useState<AdminActivityItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  // Edit modal state
  const [editing, setEditing] = useState<AdminUser | null>(null)
  const [editNickname, setEditNickname] = useState('')
  const [editEmail, setEditEmail] = useState('')
  const [editRole, setEditRole] = useState('')
  const [editActive, setEditActive] = useState(1)
  const [saving, setSaving] = useState(false)
  const [togglingId, setTogglingId] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<'users' | 'audit' | 'system' | 'market' | 'community'>('users')
  // Enhanced stats
  const [sysStats, setSysStats] = useState<AdminStats | null>(null)
  const [auditLog, setAuditLog] = useState<AdminAuditLog[]>([])
  const [auditTotal, setAuditTotal] = useState(0)

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [u, s, sum, act] = await Promise.all([
        adminApi.users(),
        adminApi.stats(),
        adminApi.summary().catch(() => null),
        adminApi.activity(12).catch(() => [] as AdminActivityItem[]),
      ])
      setUsers(u)
      setStats(s)
      if (sum) setSummary(sum)
      setActivities(act)
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setError(err.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const fetchEnhanced = useCallback(async () => {
    try {
      const [s, a] = await Promise.all([
        adminApi.enhancedStats().catch(() => null),
        adminApi.auditLog({ limit: 50 }).catch(() => null),
      ])
      if (s) setSysStats(s)
      if (a) { setAuditLog(a.logs || []); setAuditTotal(a.total || 0) }
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    if (activeTab === 'system' || activeTab === 'audit') fetchEnhanced()
  }, [activeTab, fetchEnhanced])

  const showMsg = (msg: string) => { setSuccess(msg); setTimeout(() => setSuccess(''), 3000) }

  const openEdit = (u: AdminUser) => {
    setEditing(u)
    setEditNickname(u.nickname || '')
    setEditEmail(u.email || '')
    setEditRole(u.role || 'user')
    setEditActive(u.is_active ?? 1)
    setError('')
  }

  const handleSave = async () => {
    if (!editing) return
    setSaving(true); setError('')
    try {
      await adminApi.updateUser(editing.id, {
        nickname: editNickname,
        email: editEmail,
        role: editRole,
        is_active: editActive,
      })
      showMsg('用户已更新')
      setEditing(null)
      fetchData()
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setError(err.message || '保存失败') }
    finally { setSaving(false) }
  }

  // 禁用/启用（POST /admin/users/:id/disable|enable；禁用为危险操作，二次确认）
  const handleToggleActive = async (u: AdminUser) => {
    const isActive = u.is_active === 1
    if (isActive) {
      const ok = await confirm({
        title: t('admin.users.disableConfirmTitle'),
        message: t('admin.users.disableConfirmMsg').replace('{name}', u.username),
        confirmText: t('admin.users.disable'),
        variant: 'danger',
      })
      if (!ok) return
    }
    setTogglingId(u.id); setError('')
    try {
      if (isActive) {
        await adminApi.disableUser(u.id)
        showMsg(t('admin.users.disabledOk'))
      } else {
        await adminApi.enableUser(u.id)
        showMsg(t('admin.users.enabledOk'))
      }
      fetchData()
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setError(err.message || t('admin.users.actionFail'))
    } finally { setTogglingId(null) }
  }

  const roleLabel = (r: string) => r === 'admin' ? '管理员' : r === 'manager' ? '经理' : '用户'

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-6 w-6 animate-spin text-muted-foreground" /></div>
  }

  return (
    <div className="h-full overflow-y-auto p-5 space-y-6">
      <div className="flex items-center justify-end">
        <button onClick={fetchData} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          <RefreshCw className="h-3.5 w-3.5" />刷新
        </button>
      </div>

      {/* Messages */}
      {success && <div className="flex items-center gap-2 rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-2 text-xs text-green-400"><CheckCircle className="h-3.5 w-3.5" />{success}</div>}
      {error && <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400"><AlertCircle className="h-3.5 w-3.5" />{error}</div>}

      {/* KPI cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {[
          { label: '总用户', value: stats?.total_users || 0, icon: Users, color: 'text-blue-400' },
          { label: '活跃用户', value: stats?.active_users || 0, icon: UserCheck, color: 'text-green-400' },
          { label: '管理员', value: stats?.admin_count || 0, icon: Shield, color: 'text-quant-gold' },
          { label: '普通用户', value: stats?.user_count || 0, icon: UserCog, color: 'text-purple-400' },
        ].map(c => (
          <div key={c.label} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
            <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
              <c.icon className={cn('h-4 w-4', c.color)} />{c.label}
            </div>
            <p className="text-2xl font-bold text-foreground">{c.value}</p>
          </div>
        ))}
      </div>

      {/* ── 运营概览 + 最近活动（GET /admin/summary、/admin/activity）── */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground mb-3">
            <Activity className="h-4 w-4 text-quant-gold" />{t('admin.summary.title')}
          </div>
          <div className="grid grid-cols-3 gap-3">
            {[
              { label: t('admin.summary.pendingOrders'), value: summary?.pending_orders },
              { label: t('admin.summary.totalTrades'), value: summary?.total_trades },
              { label: t('admin.summary.activeStrategies'), value: summary?.active_strategies },
              { label: t('admin.summary.unreadAlerts'), value: summary?.unread_alerts },
              { label: t('admin.summary.uptimeHours'), value: summary?.uptime_hours },
              { label: t('admin.summary.memoryMb'), value: summary?.memory_mb != null ? summary.memory_mb.toFixed(1) : undefined },
            ].map(c => (
              <div key={c.label}>
                <div className="text-[10px] text-muted-foreground">{c.label}</div>
                <p className="text-lg font-bold text-foreground">{c.value ?? '-'}</p>
              </div>
            ))}
          </div>
        </div>
        <div className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground mb-3">
            <Clock className="h-4 w-4 text-quant-gold" />{t('admin.activity.title')}
          </div>
          {activities.length === 0 ? (
            <div className="text-xs text-muted-foreground py-4 text-center">{t('admin.activity.empty')}</div>
          ) : (
            <ul className="space-y-1.5 max-h-36 overflow-y-auto pr-1">
              {activities.map((a, i) => (
                <li key={i} className="flex items-center gap-2 text-xs">
                  <span className={cn('shrink-0 px-1.5 py-0.5 rounded text-[10px] font-medium', ACTIVITY_TYPE_COLOR[a.type] || 'bg-white/5 text-muted-foreground')}>
                    {t(`admin.activity.type.${a.type}`, a.type)}
                  </span>
                  <span className="flex-1 min-w-0 truncate text-foreground">{a.message}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{fmtActivityTs(a.timestamp)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* ── Tabs ── */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
        {[
          { k: 'users' as const, label: '用户管理', icon: Users },
          { k: 'audit' as const, label: '审计日志', icon: FileText },
          { k: 'system' as const, label: '系统监控', icon: Cpu },
          { k: 'market' as const, label: '上架审核', icon: Shield },
          { k: 'community' as const, label: t('admin.tabs.communityReview'), icon: CheckCircle },
        ].map(t => (
          <button key={t.k} onClick={() => setActiveTab(t.k)}
            className={cn('flex items-center gap-1.5 px-4 py-2 rounded-md text-xs font-medium transition-colors',
              activeTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
            <t.icon className="h-3.5 w-3.5" />{t.label}
          </button>
        ))}
      </div>

      {/* ── System Monitor Tab ── */}
      {activeTab === 'market' && <AdminMarketReview />}
      {activeTab === 'community' && <AdminIndicatorReview />}
      {activeTab === 'system' && sysStats?.system && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {[
            { label: 'Goroutines', value: sysStats.system.goroutines, icon: Activity },
            { label: 'Heap (MB)', value: (sysStats.system.heap_alloc_mb || 0).toFixed(1), icon: HardDrive },
            { label: 'Uptime (h)', value: Math.floor((sysStats.system.uptime_seconds || 0) / 3600), icon: Clock },
            { label: 'Go Version', value: sysStats.system.go_version, icon: Zap },
          ].map(c => (
            <div key={c.label} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
              <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
                <c.icon className="h-4 w-4 text-quant-gold" />{c.label}
              </div>
              <p className="text-xl font-bold text-foreground">{c.value}</p>
            </div>
          ))}
        </div>
      )}
      {activeTab === 'system' && sysStats?.trading && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {[
            { label: '总订单', value: sysStats.trading.total_orders },
            { label: '挂单', value: sysStats.trading.pending_orders },
            { label: '总成交', value: sysStats.trading.total_trades },
            { label: '活跃策略', value: sysStats.trading.active_strategies },
          ].map(c => (
            <div key={c.label} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
              <div className="text-xs text-muted-foreground mb-1">{c.label}</div>
              <p className="text-xl font-bold text-foreground">{c.value}</p>
            </div>
          ))}
        </div>
      )}

      {/* ── Audit Log Tab ── */}
      {activeTab === 'audit' && (
        <div className="rounded-xl border border-quant-border bg-quant-bg-secondary overflow-hidden">
          <div className="px-4 py-3 border-b border-quant-border flex items-center justify-between">
            <span className="text-sm font-medium">操作记录</span>
            <span className="text-xs text-muted-foreground">共 {auditTotal} 条</span>
          </div>
          <DataTable<AdminAuditLog>
            data={auditLog}
            columns={[
              { key: 'id', title: 'ID', render: (e) => e.id },
              { key: 'actor', title: '操作者', render: (e) => <span className="font-medium">{e.actor}</span> },
              { key: 'action', title: '操作', render: (e) => <span className="px-1.5 py-0.5 rounded text-[10px] bg-blue-500/10 text-blue-400">{e.action}</span> },
              { key: 'detail', title: '详情', render: (e) => <span className="max-w-[300px] truncate">{e.detail}</span> },
              { key: 'time', title: '时间', render: (e) => e.created_at ? new Date(e.created_at * 1000).toLocaleString('zh-CN') : '-' },
            ]}
            keyExtractor={(e) => String(e.id)}
            emptyText="暂无操作记录"
          />
        </div>
      )}

      {/* ── User Table ── */}
      {activeTab === 'users' && (
        <div className="rounded-xl border border-quant-border bg-quant-bg-secondary overflow-hidden">
          <DataTable<AdminUser>
            data={users}
            columns={[
              { key: 'id', title: 'ID', render: (u) => <span className="text-muted-foreground">{u.id}</span> },
              { key: 'username', title: '用户名', render: (u) => (
                <span className={cn('font-medium', u.is_active === 1 ? 'text-foreground' : 'text-muted-foreground line-through opacity-60')}>
                  {u.username}
                </span>
              )},
              { key: 'email', title: '邮箱', render: (u) => <span className="text-muted-foreground">{u.email || '-'}</span> },
              { key: 'role', title: '角色', render: (u) => (
                <span className={cn('px-2 py-0.5 rounded text-[10px] font-medium',
                  u.role === 'admin' ? 'bg-quant-gold/20 text-quant-gold' : 'bg-blue-500/10 text-blue-400')}>
                  {roleLabel(u.role)}
                </span>
              )},
              { key: 'status', title: '状态', render: (u) => (
                <span className={cn('px-2 py-0.5 rounded text-[10px] font-medium',
                  u.is_active === 1 ? 'bg-green-500/10 text-green-400' : 'bg-red-500/10 text-red-400')}>
                  {u.is_active === 1 ? '正常' : '禁用'}
                </span>
              )},
              { key: 'created', title: '注册时间', render: (u) => <span className="text-muted-foreground text-xs">{u.created_at?.slice(0, 10)}</span> },
              { key: 'action', title: '操作', render: (u) => (
                <div className="flex items-center gap-1">
                  <button onClick={() => openEdit(u)} title="编辑"
                    className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/10">
                    <Edit3 className="h-3.5 w-3.5" />
                  </button>
                  <button
                    onClick={() => handleToggleActive(u)}
                    disabled={togglingId === u.id}
                    title={u.is_active === 1 ? t('admin.users.disable') : t('admin.users.enable')}
                    className={cn('p-1.5 rounded hover:bg-white/10 disabled:opacity-50',
                      u.is_active === 1 ? 'text-muted-foreground hover:text-red-400' : 'text-muted-foreground hover:text-green-400')}>
                    {togglingId === u.id
                      ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      : u.is_active === 1 ? <UserX className="h-3.5 w-3.5" /> : <UserCheck className="h-3.5 w-3.5" />}
                  </button>
                </div>
              )},
            ]}
            keyExtractor={(u) => String(u.id)}
            emptyText="暂无用户数据"
          />
        </div>
      )}
      {/* Edit modal */}
      {editing && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setEditing(null)}>
          <div className="w-full max-w-md rounded-xl border border-quant-border bg-quant-bg p-6 space-y-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-semibold text-foreground">编辑用户 — {editing.username}</h3>
              <button onClick={() => setEditing(null)} className="text-muted-foreground hover:text-foreground"><X className="h-5 w-5" /></button>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">昵称</label>
              <input type="text" value={editNickname} onChange={e => setEditNickname(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-foreground outline-none focus:border-quant-gold" />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">邮箱</label>
              <input type="email" value={editEmail} onChange={e => setEditEmail(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-foreground outline-none focus:border-quant-gold" />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">角色</label>
              <select value={editRole} onChange={e => setEditRole(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-foreground outline-none focus:border-quant-gold">
                <option value="user">用户</option>
                <option value="manager">经理</option>
                <option value="admin">管理员</option>
              </select>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">状态</label>
              <select value={editActive} onChange={e => setEditActive(Number(e.target.value))}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-foreground outline-none focus:border-quant-gold">
                <option value={1}>正常</option>
                <option value={0}>禁用</option>
              </select>
            </div>

            <div className="flex gap-2 pt-2">
              <button onClick={() => setEditing(null)}
                className="flex-1 rounded-lg border border-quant-border px-4 py-2 text-sm text-muted-foreground hover:text-foreground">
                取消
              </button>
              <button onClick={handleSave} disabled={saving}
                className="flex-1 rounded-lg bg-quant-gold px-4 py-2 text-sm font-medium text-black hover:opacity-90 disabled:opacity-50">
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* 危险操作二次确认（禁用用户） */}
      <Dialog />
    </div>
  )
}
