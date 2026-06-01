import { useState, useEffect, useCallback } from 'react'
import { agentAdminApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
  Key, Plus, Trash2, Loader2, AlertCircle, CheckCircle,
  Copy, Eye, EyeOff, Shield, Clock, Activity, X, RefreshCw
} from 'lucide-react'

interface Token {
  id: string; name: string; token?: string
  scopes?: string; created_at: number; last_used_at: number | null
}

interface AuditLog {
  id: number; token_id: number; name: string; endpoint: string
  method: string; params_summary: string; status_code: number
  ip: string; user_agent: string; timestamp: number
}

export function AgentTokens() {
  const [tokens, setTokens] = useState<Token[]>([])
  const [auditLog, setAuditLog] = useState<AuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [tab, setTab] = useState<'tokens' | 'audit'>('tokens')

  // Create dialog
  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState('')
  const [newScopes, setNewScopes] = useState('read')
  const [creating, setCreating] = useState(false)
  const [revealedToken, setRevealedToken] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [t, a] = await Promise.all([agentAdminApi.tokens(), agentAdminApi.auditLog()])
      setTokens(t || [])
      setAuditLog(a || [])
    } catch (e: any) {
      setError(e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const showMsg = (msg: string) => { setSuccess(msg); setTimeout(() => setSuccess(''), 3000) }

  const handleCreate = async () => {
    if (!newName.trim()) return
    setCreating(true); setError('')
    try {
      const tokenValue = 'qd_agent_' + Math.random().toString(36).slice(2) + Date.now().toString(36)
      await agentAdminApi.createToken({ name: newName, token: tokenValue, scopes: newScopes })
      setRevealedToken(tokenValue)
      setShowCreate(false)
      setNewName('')
      showMsg('令牌已创建')
      fetchData()
    } catch (e: any) { setError(e.message || '创建失败') }
    finally { setCreating(false) }
  }

  const handleDelete = async (id: string) => {
    try {
      await agentAdminApi.deleteToken(id)
      showMsg('令牌已删除')
      fetchData()
    } catch (e: any) { setError(e.message || '删除失败') }
  }

  const scopeLabel = (s?: string) => {
    if (!s || s === 'read') return '只读'
    if (s === 'read,write') return '读写'
    return s
  }

  if (loading) {
    return <div className="flex items-center justify-center h-64"><Loader2 className="h-6 w-6 animate-spin text-muted-foreground" /></div>
  }

  return (
    <div className="h-full overflow-y-auto p-5 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold text-white">Agent 令牌管理</h1>
        <button onClick={fetchData} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-white">
          <RefreshCw className="h-3.5 w-3.5" />刷新
        </button>
      </div>

      {/* Messages */}
      {success && <div className="flex items-center gap-2 rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-2 text-xs text-green-400"><CheckCircle className="h-3.5 w-3.5" />{success}</div>}
      {error && <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400"><AlertCircle className="h-3.5 w-3.5" />{error}</div>}

      {/* Revealed token */}
      {revealedToken && (
        <div className="rounded-xl border border-quant-gold/30 bg-quant-gold/10 p-4">
          <div className="flex items-center justify-between mb-2">
            <p className="text-sm font-medium text-quant-gold flex items-center gap-2"><Key className="h-4 w-4" />新令牌（仅显示一次）</p>
            <button onClick={() => setRevealedToken(null)} className="text-muted-foreground hover:text-white"><X className="h-4 w-4" /></button>
          </div>
          <div className="flex gap-2">
            <input type="text" readOnly value={revealedToken}
              className="flex-1 rounded-lg border border-quant-gold/30 bg-quant-bg px-3 py-2 text-sm text-white font-mono" />
            <button onClick={() => { navigator.clipboard.writeText(revealedToken); showMsg('已复制') }}
              className="shrink-0 rounded-lg bg-quant-gold px-3 py-2 text-black hover:opacity-90">
              <Copy className="h-4 w-4" />
            </button>
          </div>
          <p className="text-xs text-muted-foreground mt-2">请立即复制保存，关闭后无法再次查看。</p>
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-1 rounded-lg bg-quant-bg-secondary p-1">
        <button onClick={() => setTab('tokens')}
          className={cn('flex-1 rounded-md py-2 text-xs font-medium transition-colors', tab === 'tokens' ? 'bg-quant-gold text-black' : 'text-muted-foreground hover:text-white')}>
          令牌列表 ({tokens.length})
        </button>
        <button onClick={() => setTab('audit')}
          className={cn('flex-1 rounded-md py-2 text-xs font-medium transition-colors', tab === 'audit' ? 'bg-quant-gold text-black' : 'text-muted-foreground hover:text-white')}>
          审计日志 ({auditLog.length})
        </button>
      </div>

      {/* ══════ TOKENS TAB ══════ */}
      {tab === 'tokens' && (
        <div className="space-y-3">
          <button onClick={() => { setShowCreate(true); setError('') }}
            className="flex items-center gap-2 rounded-lg border border-dashed border-quant-border px-4 py-3 text-sm text-muted-foreground hover:text-white hover:border-quant-gold/50 w-full justify-center">
            <Plus className="h-4 w-4" />创建新令牌
          </button>

          {tokens.map(t => (
            <div key={t.id} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
              <div className="flex items-start justify-between">
                <div className="space-y-1">
                  <h3 className="text-sm font-medium text-white flex items-center gap-2">
                    <Key className="h-3.5 w-3.5 text-quant-gold" />{t.name}
                  </h3>
                  <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span className="flex items-center gap-1"><Shield className="h-3 w-3" />{scopeLabel(t.scopes as string)}</span>
                    <span className="flex items-center gap-1"><Clock className="h-3 w-3" />{new Date((t.created_at) * 1000).toLocaleDateString()}</span>
                    {t.last_used_at && <span className="flex items-center gap-1"><Activity className="h-3 w-3" />上次: {new Date((t.last_used_at) * 1000).toLocaleString()}</span>}
                  </div>
                </div>
                <button onClick={() => handleDelete(t.id)}
                  className="p-1.5 rounded text-muted-foreground hover:text-red-400 hover:bg-red-500/10">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}

          {tokens.length === 0 && (
            <p className="text-center text-muted-foreground py-8">暂无令牌</p>
          )}
        </div>
      )}

      {/* ══════ AUDIT LOG TAB ══════ */}
      {tab === 'audit' && (
        <div className="rounded-xl border border-quant-border bg-quant-bg-secondary overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-quant-border text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2">时间</th>
                  <th className="px-3 py-2">令牌</th>
                  <th className="px-3 py-2">方法</th>
                  <th className="px-3 py-2">端点</th>
                  <th className="px-3 py-2">状态</th>
                  <th className="px-3 py-2">IP</th>
                </tr>
              </thead>
              <tbody>
                {auditLog.map(log => (
                  <tr key={log.id} className="border-b border-quant-border/50 hover:bg-white/[0.02]">
                    <td className="px-3 py-2 text-xs text-muted-foreground whitespace-nowrap">
                      {new Date(log.timestamp * 1000).toLocaleString()}
                    </td>
                    <td className="px-3 py-2 text-xs text-white">{log.name || '-'}</td>
                    <td className="px-3 py-2 text-xs">
                      <span className={cn('px-1.5 py-0.5 rounded text-[10px] font-medium',
                        log.method === 'GET' ? 'bg-green-500/10 text-green-400' :
                        log.method === 'POST' ? 'bg-blue-500/10 text-blue-400' :
                        'bg-yellow-500/10 text-yellow-400')}>{log.method}</span>
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground font-mono">{log.endpoint}</td>
                    <td className="px-3 py-2 text-xs">
                      <span className={log.status_code === 200 ? 'text-green-400' : 'text-red-400'}>{log.status_code}</span>
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">{log.ip}</td>
                  </tr>
                ))}
                {auditLog.length === 0 && (
                  <tr><td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">暂无审计记录</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Create dialog */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setShowCreate(false)}>
          <div className="w-full max-w-sm rounded-xl border border-quant-border bg-quant-bg p-6 space-y-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-semibold text-white">创建 Agent 令牌</h3>
              <button onClick={() => setShowCreate(false)} className="text-muted-foreground hover:text-white"><X className="h-5 w-5" /></button>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">名称</label>
              <input type="text" value={newName} onChange={e => setNewName(e.target.value)}
                placeholder="给令牌起个名字" className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">权限范围</label>
              <select value={newScopes} onChange={e => setNewScopes(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                <option value="read">只读 (read)</option>
                <option value="read,write">读写 (read,write)</option>
                <option value="read,write,backtest">含回测 (backtest)</option>
                <option value="read,write,backtest,trade">含交易 (trade)</option>
              </select>
            </div>
            <div className="flex gap-2 pt-2">
              <button onClick={() => setShowCreate(false)}
                className="flex-1 rounded-lg border border-quant-border px-4 py-2 text-sm text-muted-foreground hover:text-white">取消</button>
              <button onClick={handleCreate} disabled={creating || !newName.trim()}
                className="flex-1 rounded-lg bg-quant-gold px-4 py-2 text-sm font-medium text-black hover:opacity-90 disabled:opacity-50">
                {creating ? '创建中...' : '创建'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
