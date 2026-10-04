import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Layers, Pencil, Play, Plus, Signal, Square, Trash2, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { combosApi, strategyApi } from '@/lib/api'
import type { ComboAggregationMode, ComboConfig, ComboMember, ComboPayload } from '@/types'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { ConfirmDialog } from '@/components/ui/ConfirmDialog'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'

/* ── 组合策略管理页（/combos 后端 8 端点）──
   列表 / 创建 / 编辑 / 删除（二次确认）/ 启停 / 聚合信号。
   成员按策略类型（工厂名）实例化，选项来自现有策略配置接口的 strategy_type 去重。 */

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

const AGG_MODES: ComboAggregationMode[] = ['vote', 'weighted', 'unanimous']

function comboStatus(c: ComboConfig) {
  return c.status === 'running'
    ? { textKey: 'combo.status.running', cls: 'text-quant-green' }
    : { textKey: 'combo.status.stopped', cls: 'text-muted-foreground' }
}

export function BotsCombo() {
  const { t } = useI18n()
  const tt = (key: string, vars?: Record<string, string | number>) => {
    let s = t(key)
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v))
    return s
  }
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<ComboConfig | 'new' | null>(null)
  const [deleting, setDeleting] = useState<ComboConfig | null>(null)
  const [signalsOf, setSignalsOf] = useState<ComboConfig | null>(null)
  const [actionId, setActionId] = useState<string | null>(null)

  const { data: combos = [], isLoading } = useQuery({
    queryKey: ['combos'],
    queryFn: combosApi.list,
    refetchInterval: 8000,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['combos'] })

  const runAction = async (combo: ComboConfig, action: () => Promise<unknown>, ok: string, errPrefix: string) => {
    setActionId(combo.id)
    try {
      await action()
      toast('success', ok)
      await invalidate()
    } catch (e) {
      // 启动/停止失败透出后端 detail（axios 层已取 detail 为 message）
      toast('error', `${errPrefix}: ${e instanceof Error ? e.message : String(e)}`)
      await invalidate()
    } finally {
      setActionId(null)
    }
  }

  const deleteMutation = useMutation({
    mutationFn: (id: string) => combosApi.delete(id),
    onSuccess: async () => {
      toast('success', t('combo.delete.ok'))
      setDeleting(null)
      await invalidate()
    },
    onError: (e) => toast('error', `${t('combo.delete.fail')}: ${e instanceof Error ? e.message : String(e)}`),
  })

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <div className="flex items-center gap-3 bg-quant-card border border-quant-border rounded-xl px-4 py-2 flex-wrap">
          <span className="font-bold text-sm flex items-center gap-1.5">
            <Layers className="w-4 h-4 text-quant-gold" /> {t('combo.title')}
          </span>
          <span className="text-[11px] text-muted-foreground">{t('combo.subtitle')}</span>
          <span className="flex-1" />
          <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setEditing('new')}>
            {t('combo.create')}
          </Button>
        </div>

        <SectionCard title={t('combo.list')} headerAction={<span className="text-xs text-[#8a8a8a]">{tt('combo.list.count', { n: combos.length })}</span>}>
          {isLoading ? (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-32 rounded-xl" />
              ))}
            </div>
          ) : combos.length === 0 ? (
            <EmptyState
              icon={<Layers className="w-8 h-8" />}
              title={t('combo.empty.title')}
              description={t('combo.empty.desc')}
              actionLabel={t('combo.create')}
              onAction={() => setEditing('new')}
            />
          ) : (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
              {combos.map((combo) => {
                const st = comboStatus(combo)
                const isLoadingAction = actionId === combo.id
                const running = combo.status === 'running'
                return (
                  <div
                    key={combo.id}
                    className={cn(
                      'bg-quant-card border border-quant-border rounded-xl p-3 transition-all hover:border-quant-gold/20',
                      !running && 'opacity-80'
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-bold text-xs text-foreground truncate">{combo.name || combo.id}</span>
                      <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px] shrink-0">
                        {t(`combo.aggregation.${combo.aggregation_mode}`)}
                      </span>
                    </div>
                    <div className="text-muted-foreground mt-1 text-[10px] truncate">
                      {combo.symbol} · {tt('combo.member.count', { n: combo.members?.length ?? 0 })}
                    </div>
                    <div className="flex items-center justify-between mt-2">
                      <span className={cn('text-[10px]', st.cls)}>
                        {running ? '●' : '○'} {t(st.textKey)}
                      </span>
                      <span className="text-[10px] text-muted-foreground">
                        {t('combo.created_at')} {combo.created_at ? new Date(combo.created_at).toLocaleDateString() : '-'}
                      </span>
                    </div>
                    <div className="flex gap-1 mt-2">
                      {running ? (
                        <button
                          className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50"
                          disabled={isLoadingAction}
                          onClick={() => runAction(combo, () => combosApi.stop(combo.id), t('combo.stop.ok'), t('combo.stop.fail'))}
                        >
                          <Square className="w-3 h-3 inline mr-1" />
                          {t('combo.stop')}
                        </button>
                      ) : (
                        <button
                          className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50"
                          disabled={isLoadingAction}
                          onClick={() => runAction(combo, () => combosApi.start(combo.id), t('combo.start.ok'), t('combo.start.fail'))}
                        >
                          <Play className="w-3 h-3 inline mr-1" />
                          {t('combo.start')}
                        </button>
                      )}
                      <button
                        className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
                        onClick={() => setSignalsOf(combo)}
                      >
                        <Signal className="w-3 h-3 inline mr-1" />
                        {t('combo.signals')}
                      </button>
                      <button
                        className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
                        onClick={() => setEditing(combo)}
                      >
                        <Pencil className="w-3 h-3 inline mr-1" />
                        {t('common.edit')}
                      </button>
                      <button
                        className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-quant-red hover:border-quant-red/30 transition-colors disabled:opacity-50"
                        disabled={isLoadingAction}
                        onClick={() => setDeleting(combo)}
                      >
                        <Trash2 className="w-3 h-3 inline mr-1" />
                        {t('combo.delete')}
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </SectionCard>
      </div>

      {editing && (
        <ComboFormModal
          combo={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={invalidate}
        />
      )}
      {signalsOf && <ComboSignalsModal combo={signalsOf} onClose={() => setSignalsOf(null)} />}
      <ConfirmDialog
        open={deleting !== null}
        title={t('combo.delete.title')}
        message={deleting ? tt('combo.delete.message', { name: deleting.name || deleting.id }) : ''}
        confirmText={t('combo.delete.confirm')}
        cancelText={t('common.cancel')}
        variant="danger"
        onConfirm={() => deleting && deleteMutation.mutate(deleting.id)}
        onCancel={() => setDeleting(null)}
      />
    </div>
  )
}

/* ── 创建 / 编辑表单 ── */
interface MemberRow {
  strategy_name: string
  weight: string
  enabled: boolean
}

function ComboFormModal({
  combo,
  onClose,
  onSaved,
}: {
  combo: ComboConfig | null
  onClose: () => void
  onSaved: () => Promise<unknown>
}) {
  const { t } = useI18n()
  const [name, setName] = useState(combo?.name ?? '')
  const [symbol, setSymbol] = useState(combo?.symbol ?? 'BTCUSDT')
  const [mode, setMode] = useState<ComboAggregationMode>(combo?.aggregation_mode ?? 'vote')
  const [members, setMembers] = useState<MemberRow[]>(
    combo?.members?.map((m) => ({ strategy_name: m.strategy_name, weight: String(m.weight), enabled: m.enabled })) ?? []
  )

  // 可选策略类型：来自现有策略配置接口（strategy_type 去重），工厂名即成员 strategy_name
  const { data: strategyOptions = [] } = useQuery({
    queryKey: ['combo-member-options'],
    queryFn: async () => {
      const items = await strategyApi.list()
      const types = new Set<string>()
      for (const it of items) {
        const st = (it.strategy_type ?? '').trim()
        if (st) types.add(st)
      }
      return [...types].sort()
    },
    staleTime: 60000,
  })

  const usedTypes = useMemo(() => new Set(members.map((m) => m.strategy_name)), [members])
  const nextFreeType = strategyOptions.find((s) => !usedTypes.has(s))

  const saveMutation = useMutation({
    mutationFn: (payload: ComboPayload) => (combo ? combosApi.update(combo.id, payload) : combosApi.create(payload)),
    onSuccess: async () => {
      await onSaved()
      toast('success', combo ? t('combo.save.ok') : t('combo.create.ok'))
      onClose()
    },
    onError: (e) => toast('error', `${t('combo.save.fail')}: ${e instanceof Error ? e.message : String(e)}`),
  })

  const addMember = () => {
    if (!nextFreeType) return
    setMembers((prev) => [...prev, { strategy_name: nextFreeType, weight: '', enabled: true }])
  }

  const updateMember = (idx: number, patch: Partial<MemberRow>) => {
    setMembers((prev) => prev.map((m, i) => (i === idx ? { ...m, ...patch } : m)))
  }

  const removeMember = (idx: number) => {
    setMembers((prev) => prev.filter((_, i) => i !== idx))
  }

  const handleSubmit = () => {
    if (!name.trim()) return toast('warning', t('combo.form.required.name'))
    if (!symbol.trim()) return toast('warning', t('combo.form.required.symbol'))
    const enabled = members.filter((m) => m.enabled)
    if (enabled.length === 0) return toast('warning', t('combo.form.required.members'))
    if (new Set(members.map((m) => m.strategy_name)).size !== members.length) {
      return toast('warning', t('combo.member.duplicate'))
    }

    let payloadMembers: ComboMember[]
    if (mode === 'weighted') {
      let sum = 0
      payloadMembers = []
      for (const m of members) {
        const w = parseFloat(m.weight)
        if (m.enabled) {
          if (!isFinite(w) || w < 0 || w > 1) return toast('warning', t('combo.form.weight.invalid'))
          sum += w
        }
        payloadMembers.push({ strategy_name: m.strategy_name, weight: isFinite(w) ? w : 0, enabled: m.enabled })
      }
      if (Math.abs(sum - 1.0) > 0.001) {
        return toast('warning', t('combo.form.weight.sum').replace('{sum}', sum.toFixed(3)))
      }
    } else {
      // vote / unanimous 权重不生效：按系统模板惯例提交启用成员等权
      const eq = enabled.length > 0 ? 1 / enabled.length : 0
      payloadMembers = members.map((m) => ({
        strategy_name: m.strategy_name,
        weight: m.enabled ? eq : 0,
        enabled: m.enabled,
      }))
    }

    saveMutation.mutate({
      name: name.trim(),
      symbol: symbol.trim().toUpperCase(),
      members: payloadMembers,
      aggregation_mode: mode,
    })
  }

  return (
    <ModalShell
      title={combo ? t('combo.edit') : t('combo.create')}
      subtitle={combo ? `${combo.symbol} · ${combo.id}` : t('combo.subtitle')}
      onClose={onClose}
      wide
    >
      <div className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">{t('combo.name')}</label>
            <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} placeholder={t('combo.name.placeholder')} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">{t('combo.symbol')}</label>
            <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className={inputCls} placeholder="BTCUSDT" />
          </div>
          <div className="sm:col-span-2">
            <label className="text-[11px] text-muted-foreground mb-1 block">{t('combo.aggregation')}</label>
            <select value={mode} onChange={(e) => setMode(e.target.value as ComboAggregationMode)} className={inputCls} aria-label={t('combo.aggregation')}>
              {AGG_MODES.map((m) => (
                <option key={m} value={m}>
                  {t(`combo.aggregation.${m}`)}
                </option>
              ))}
            </select>
            <p className="text-[10px] text-muted-foreground mt-1">{t(`combo.aggregation.${mode}.desc`)}</p>
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-[11px] text-muted-foreground">{t('combo.members')}</label>
            <button
              className="text-[11px] text-quant-gold hover:opacity-80 disabled:opacity-40"
              disabled={!nextFreeType}
              onClick={addMember}
            >
              <Plus className="w-3 h-3 inline mr-0.5" />
              {t('combo.member.add')}
            </button>
          </div>
          {strategyOptions.length === 0 ? (
            <div className="text-[11px] text-muted-foreground py-3 text-center border border-dashed border-quant-border rounded-lg">
              {t('combo.member.empty')}
            </div>
          ) : members.length === 0 ? (
            <div className="text-[11px] text-muted-foreground py-3 text-center border border-dashed border-quant-border rounded-lg">
              {t('combo.form.required.members')}
            </div>
          ) : (
            <div className="space-y-1.5">
              {members.map((m, idx) => (
                <div key={idx} className="flex items-center gap-2 p-2 rounded-lg bg-quant-bg border border-quant-border">
                  <select
                    value={m.strategy_name}
                    onChange={(e) => updateMember(idx, { strategy_name: e.target.value })}
                    className={cn(inputCls, 'flex-1')}
                    aria-label={t('combo.members')}
                  >
                    {strategyOptions.map((s) => (
                      <option key={s} value={s} disabled={usedTypes.has(s) && s !== m.strategy_name}>
                        {s}
                      </option>
                    ))}
                  </select>
                  <input
                    type="number"
                    step="0.05"
                    min="0"
                    max="1"
                    value={mode === 'weighted' ? m.weight : m.enabled ? (1 / Math.max(members.filter((x) => x.enabled).length, 1)).toFixed(3) : '0'}
                    disabled={mode !== 'weighted' || !m.enabled}
                    onChange={(e) => updateMember(idx, { weight: e.target.value })}
                    className={cn(inputCls, 'w-20 text-right disabled:opacity-50')}
                    aria-label={t('combo.member.weight')}
                    title={t('combo.member.weight')}
                  />
                  <label className="flex items-center gap-1 text-[11px] text-muted-foreground cursor-pointer shrink-0">
                    <input
                      type="checkbox"
                      checked={m.enabled}
                      onChange={(e) => updateMember(idx, { enabled: e.target.checked })}
                      className="accent-quant-gold"
                    />
                    {t('combo.member.enabled')}
                  </label>
                  <button
                    className="p-1 rounded text-muted-foreground hover:text-quant-red hover:bg-white/5 shrink-0"
                    onClick={() => removeMember(idx)}
                    aria-label={t('combo.member.remove')}
                  >
                    <X className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
            </div>
          )}
          <p className="text-[10px] text-muted-foreground mt-1.5">{t('combo.form.members.hint')}</p>
        </div>

        <div className="flex items-center gap-2 pt-1">
          <Button variant="primary" className="flex-1" isLoading={saveMutation.isPending} onClick={handleSubmit}>
            {combo ? t('common.save') : t('combo.create')}
          </Button>
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
        </div>
      </div>
    </ModalShell>
  )
}

/* ── 组合信号（聚合输出，最近 100 条内存留存）── */
function ComboSignalsModal({ combo, onClose }: { combo: ComboConfig; onClose: () => void }) {
  const { t } = useI18n()
  const { data: signals = [], isLoading } = useQuery({
    queryKey: ['combo-signals', combo.id],
    queryFn: () => combosApi.signals(combo.id, 50),
    refetchInterval: 5000,
  })

  const dirBadge = (d: string) => {
    const up = d === 'LONG' || d === 'BUY'
    const down = d === 'SHORT' || d === 'SELL'
    return (
      <span
        className={cn(
          'px-1.5 py-0.5 rounded text-[10px] border',
          up
            ? 'bg-quant-green/10 text-quant-green border-quant-green/30'
            : down
              ? 'bg-quant-red/10 text-quant-red border-quant-red/30'
              : 'bg-white/5 text-muted-foreground border-quant-border'
        )}
      >
        {d || '-'}
      </span>
    )
  }

  return (
    <ModalShell title={`${t('combo.signals.title')} · ${combo.name || combo.id}`} subtitle={combo.symbol} onClose={onClose} wide>
      {combo.status !== 'running' && (
        <div className="mb-3 text-[11px] text-muted-foreground bg-quant-bg border border-quant-border rounded-lg px-3 py-2">
          {t('combo.signals.not_running')}
        </div>
      )}
      {isLoading ? (
        <Skeleton className="h-40 rounded-xl" />
      ) : signals.length === 0 ? (
        <div className="text-[11px] text-muted-foreground py-8 text-center">{t('combo.signals.empty')}</div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-[11px]">
            <thead>
              <tr className="text-muted-foreground border-b border-quant-border">
                <th className="text-left py-1.5 pr-3 font-medium">{t('combo.signals.time')}</th>
                <th className="text-left py-1.5 pr-3 font-medium">{t('combo.signals.direction')}</th>
                <th className="text-right py-1.5 pr-3 font-medium">{t('combo.signals.strength')}</th>
                <th className="text-left py-1.5 pr-3 font-medium">{t('combo.signals.strategy')}</th>
                <th className="text-left py-1.5 font-medium">{t('combo.signals.reason')}</th>
              </tr>
            </thead>
            <tbody>
              {[...signals].reverse().map((sig, i) => (
                <tr key={`${sig.timestamp}-${i}`} className="border-b border-quant-border/50">
                  <td className="py-1.5 pr-3 text-muted-foreground whitespace-nowrap">
                    {sig.timestamp ? new Date(sig.timestamp).toLocaleString() : '-'}
                  </td>
                  <td className="py-1.5 pr-3">{dirBadge(sig.direction)}</td>
                  <td className="py-1.5 pr-3 text-right font-mono">{sig.strength?.toFixed(2) ?? '-'}</td>
                  <td className="py-1.5 pr-3">{sig.strategy || '-'}</td>
                  <td className="py-1.5 text-muted-foreground">{sig.reason || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </ModalShell>
  )
}

/* ── 通用弹层壳（对齐 BotsCenter/DCABots 惯例）── */
function ModalShell({
  title,
  subtitle,
  wide,
  onClose,
  children,
}: {
  title: string
  subtitle?: string
  wide?: boolean
  onClose: () => void
  children: React.ReactNode
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
      tabIndex={-1}
    >
      <div
        role="document"
        className={cn(
          'w-full flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden',
          wide ? 'max-w-3xl max-h-[90vh]' : 'max-w-xl max-h-[85vh]'
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <div>
            <h3 className="text-sm font-bold">{title}</h3>
            {subtitle && <p className="text-[10px] text-muted-foreground mt-0.5">{subtitle}</p>}
          </div>
          <button
            onClick={onClose}
            aria-label="关闭"
            className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
          >
            ✕
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-6">{children}</div>
      </div>
    </div>
  )
}

export default BotsCombo
