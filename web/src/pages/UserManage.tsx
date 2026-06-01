import { useState, useEffect, useCallback } from 'react'
import { adminApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
  Users, UserCheck, Shield, Loader2, AlertCircle, CheckCircle,
  Edit3, X, RefreshCw, UserX, UserCog, FileText, Cpu, Activity,
  Database, Zap, Clock, HardDrive
} from 'lucide-react'

interface UserRecord {
  id: number; username: string; nickname: string; email: string
  role: string; is_active: number; created_at: string
}

interface AdminStats {
  total_users: number; active_users: number
  admin_count: number; user_count: number
}

export function UserManage() {
  const [users, setUsers] = useState<UserRecord[]>([])
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  // Edit modal state
  const [editing, setEditing] = useState<UserRecord | null>(null)
  const [editNickname, setEditNickname] = useState('')
  const [editEmail, setEditEmail] = useState('')
  const [editRole, setEditRole] = useState('')
  const [editActive, setEditActive] = useState(1)
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useState<'users' | 'audit' | 'system'>('users')
  // Enhanced stats
  const [sysStats, setSysStats] = useState<any>(null)
  const [auditLog, setAuditLog] = useState<any[]>([])
  const [auditTotal, setAuditTotal] = useState(0)

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [u, s] = await Promise.all([adminApi.users(), adminApi.stats()])
      setUsers(u)
      setStats(s)
    } catch (e: any) {
      setError(e.message || '加载失败')
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
      if (a) { setAuditLog(a.entries || []); setAuditTotal(a.total || 0) }
    } catch {}
  }, [])

  useEffect(() => {
    if (activeTab === 'system' || activeTab === 'audit') fetchEnhanced()
  }, [activeTab, fetchEnhanced])

  const showMsg = (msg: string) => { setSuccess(msg); setTimeout(() => setSuccess(''), 3000) }

  const openEdit = (u: UserRecord) => {
    setEditing(u)
    setEditNickname(u.nickname || '')
    setEditEmail(u.email || '')
    setEditRole(u.role || 'user')
    setEditActive(u.is_active)
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
    } catch (e: any) { setError(e.message || '保存失败') }
    finally { setSaving(false) }
  }

  const roleLabel = (r: string) => r === 'admin' ? '管理员' : r === 'manager' ? '经理' : '用户'

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-6 w-6 animate-spin text-muted-foreground" /></div>
  }

  return (
    <div className="h-full overflow-y-auto p-5 space-y-6">
      <div className="flex items-center justify-end">
        <button onClick={fetchData} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-white">
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
            <p className="text-2xl font-bold text-white">{c.value}</p>
          </div>
        ))}
      </div>

      {/* ── Tabs ── */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
        {[
          { k: 'users' as const, label: '用户管理', icon: Users },
          { k: 'audit' as const, label: '审计日志', icon: FileText },
          { k: 'system' as const, label: '系统监控', icon: Cpu },
        ].map(t => (
          <button key={t.k} onClick={() => setActiveTab(t.k)}
            className={cn('flex items-center gap-1.5 px-4 py-2 rounded-md text-xs font-medium transition-colors',
              activeTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
            <t.icon className="h-3.5 w-3.5" />{t.label}
          </button>
        ))}
      </div>

      {/* ── System Monitor Tab ── */}
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
              <p className="text-xl font-bold text-white">{c.value}</p>
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
              <p className="text-xl font-bold text-white">{c.value}</p>
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
          {auditLog.length === 0 ? (
            <div className="px-4 py-12 text-center text-muted-foreground text-sm">暂无操作记录</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-quant-border text-left text-muted-foreground">
                    <th className="px-4 py-2">ID</th>
                    <th className="px-4 py-2">操作者</th>
                    <th className="px-4 py-2">操作</th>
                    <th className="px-4 py-2">详情</th>
                    <th className="px-4 py-2">时间</th>
                  </tr>
                </thead>
                <tbody>
                  {auditLog.map((e: any) => (
                    <tr key={e.id} className="border-b border-quant-border/30 hover:bg-white/[0.02]">
                      <td className="px-4 py-2 text-muted-foreground">{e.id}</td>
                      <td className="px-4 py-2 font-medium">{e.actor}</td>
                      <td className="px-4 py-2">
                        <span className="px-1.5 py-0.5 rounded text-[10px] bg-blue-500/10 text-blue-400">{e.action}</span>
                      </td>
                      <td className="px-4 py-2 text-muted-foreground max-w-[300px] truncate">{e.detail}</td>
                      <td className="px-4 py-2 text-muted-foreground">
                        {e.created_at ? new Date(e.created_at * 1000).toLocaleString('zh-CN') : '-'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* ── User Table ── */}
      {activeTab === 'users' && (<>
      <div className="rounded-xl border border-quant-border bg-quant-bg-secondary overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-quant-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-3">ID</th>
                <th className="px-4 py-3">用户名</th>
                <th className="px-4 py-3">邮箱</th>
                <th className="px-4 py-3">角色</th>
                <th className="px-4 py-3">状态</th>
                <th className="px-4 py-3">注册时间</th>
                <th className="px-4 py-3 w-20">操作</th>
              </tr>
            </thead>
            <tbody>
              {users.map(u => (
                <tr key={u.id} className="border-b border-quant-border/50 hover:bg-white/[0.02]">
                  <td className="px-4 py-3 text-muted-foreground">{u.id}</td>
                  <td className="px-4 py-3 text-white font-medium">{u.username}</td>
                  <td className="px-4 py-3 text-muted-foreground">{u.email || '-'}</td>
                  <td className="px-4 py-3">
                    <span className={cn('px-2 py-0.5 rounded text-[10px] font-medium',
                      u.role === 'admin' ? 'bg-quant-gold/20 text-quant-gold' : 'bg-blue-500/10 text-blue-400')}>
                      {roleLabel(u.role)}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    <span className={cn('px-2 py-0.5 rounded text-[10px] font-medium',
                      u.is_active === 1 ? 'bg-green-500/10 text-green-400' : 'bg-red-500/10 text-red-400')}>
                      {u.is_active === 1 ? '正常' : '禁用'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-muted-foreground text-xs">{u.created_at?.slice(0, 10)}</td>
                  <td className="px-4 py-3">
                    <button onClick={() => openEdit(u)}
                      className="p-1.5 rounded text-muted-foreground hover:text-white hover:bg-white/10">
                      <Edit3 className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              ))}
              {users.length === 0 && (
                <tr><td colSpan={7} className="px-4 py-12 text-center text-muted-foreground">暂无用户数据</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      </>)}
      {/* Edit modal */}
      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setEditing(null)}>
          <div className="w-full max-w-md rounded-xl border border-quant-border bg-quant-bg p-6 space-y-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-semibold text-white">编辑用户 — {editing.username}</h3>
              <button onClick={() => setEditing(null)} className="text-muted-foreground hover:text-white"><X className="h-5 w-5" /></button>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">昵称</label>
              <input type="text" value={editNickname} onChange={e => setEditNickname(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">邮箱</label>
              <input type="email" value={editEmail} onChange={e => setEditEmail(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">角色</label>
              <select value={editRole} onChange={e => setEditRole(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                <option value="user">用户</option>
                <option value="manager">经理</option>
                <option value="admin">管理员</option>
              </select>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">状态</label>
              <select value={editActive} onChange={e => setEditActive(Number(e.target.value))}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                <option value={1}>正常</option>
                <option value={0}>禁用</option>
              </select>
            </div>

            <div className="flex gap-2 pt-2">
              <button onClick={() => setEditing(null)}
                className="flex-1 rounded-lg border border-quant-border px-4 py-2 text-sm text-muted-foreground hover:text-white">
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
    </div>
  )
}
