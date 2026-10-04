import React from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { X, Plus, Pencil, Trash2, Users, Radio, Webhook, Bot, BellPlus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { executorApi } from '@/lib/api'
import type { SignalSource, SignalSourceCreatePayload } from '@/types'
import { useAuthStore } from '@/stores/authStore'
import { toast } from '@/lib/useToast'

const feeModelLabel = (model?: string): string => {
  const labels: Record<string, string> = {
    free: '免费',
    fixed_monthly: '固定月费',
    monthly: '固定月费',
    profit_share: '盈利分成',
  }
  return labels[model ?? ''] ?? model ?? '免费'
}

const typeIcon = (type: string) => {
  if (type === 'webhook') return <Webhook className="w-4 h-4 text-[#1890ff]" />
  if (type === 'api') return <Bot className="w-4 h-4 text-[#52c41a]" />
  return <Radio className="w-4 h-4 text-[#faad14]" />
}

interface FormState {
  name: string
  type: string
  enabled: boolean
  fee_model: string
  fee_percent: string
  monthly_fee: string
  tp1_pct: string
  tp2_pct: string
  tp3_pct: string
  sl_pct: string
}

const emptyForm: FormState = {
  name: '',
  type: 'webhook',
  enabled: true,
  fee_model: 'free',
  fee_percent: '',
  monthly_fee: '',
  tp1_pct: '',
  tp2_pct: '',
  tp3_pct: '',
  sl_pct: '',
}

const num = (s: string): number => {
  const n = Number(s)
  return Number.isFinite(n) ? n : 0
}

const formFromSource = (s: SignalSource): FormState => ({
  name: s.name,
  type: s.type || 'webhook',
  enabled: s.enabled,
  fee_model: s.fee_model || 'free',
  fee_percent: s.fee_percent ? String(s.fee_percent) : '',
  monthly_fee: s.monthly_fee ? String(s.monthly_fee) : '',
  tp1_pct: s.tp_sl_config?.tp1_pct ? String(s.tp_sl_config.tp1_pct) : '',
  tp2_pct: s.tp_sl_config?.tp2_pct ? String(s.tp_sl_config.tp2_pct) : '',
  tp3_pct: s.tp_sl_config?.tp3_pct ? String(s.tp_sl_config.tp3_pct) : '',
  sl_pct: s.tp_sl_config?.sl_pct ? String(s.tp_sl_config.sl_pct) : '',
})

export const SignalSourceManager: React.FC<{ open: boolean; onClose: () => void }> = ({ open, onClose }) => {
  const queryClient = useQueryClient()
  const { user } = useAuthStore()
  const { confirm, Dialog } = useConfirmDialog()
  const isAdmin = user?.role === 'admin'

  const [view, setView] = React.useState<'list' | 'form'>('list')
  const [editing, setEditing] = React.useState<SignalSource | null>(null)
  const [form, setForm] = React.useState<FormState>(emptyForm)
  const [formError, setFormError] = React.useState('')
  const [subscribersFor, setSubscribersFor] = React.useState<string | null>(null)

  const { data: sources = [], isLoading } = useQuery({
    queryKey: ['executor', 'sources'],
    queryFn: () => executorApi.getSignalSources().then((r) => r.data?.sources || []),
    enabled: open,
  })

  const { data: subscribersData, isLoading: subscribersLoading } = useQuery({
    queryKey: ['executor', 'source-subscribers', subscribersFor],
    queryFn: () =>
      executorApi.getSignalSourceSubscribers(subscribersFor!).then((r) => r.data?.subscribers || []),
    enabled: open && !!subscribersFor,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['executor', 'sources'] })

  // 属主/admin 有编辑权；owner_user_id==0（系统/匿名创建）放宽给任何登录用户（与后端 signalSourceWriteAllowed 一致）
  const canEdit = (s: SignalSource) => isAdmin || !s.owner_user_id || Number(user?.id) === s.owner_user_id

  const createMutation = useMutation({
    mutationFn: (payload: SignalSourceCreatePayload) => executorApi.createSignalSource(payload),
    onSuccess: () => {
      toast('success', '信号源已创建')
      invalidate()
      setView('list')
      setEditing(null)
    },
    onError: (e) => toast('error', '创建失败: ' + (e instanceof Error ? e.message : String(e))),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Partial<SignalSource> }) =>
      executorApi.updateSignalSource(id, payload),
    onSuccess: () => {
      toast('success', '信号源已更新')
      invalidate()
      setView('list')
      setEditing(null)
    },
    onError: (e) => toast('error', '更新失败: ' + (e instanceof Error ? e.message : String(e))),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => executorApi.deleteSignalSource(id),
    onSuccess: (r) => {
      const cancelled = r.data?.cancelled_subscriptions ?? 0
      toast('success', cancelled > 0 ? `已删除，同时取消 ${cancelled} 个订阅` : '信号源已删除')
      invalidate()
    },
    onError: (e) => toast('error', '删除失败: ' + (e instanceof Error ? e.message : String(e))),
  })

  const subscribeMutation = useMutation({
    mutationFn: (id: string) => executorApi.subscribeSignalSource(id),
    onSuccess: () => toast('success', '订阅成功'),
    onError: (e) => toast('error', '订阅失败: ' + (e instanceof Error ? e.message : String(e))),
  })

  const openCreate = () => {
    setEditing(null)
    setForm(emptyForm)
    setFormError('')
    setView('form')
  }

  const openEdit = (s: SignalSource) => {
    setEditing(s)
    setForm(formFromSource(s))
    setFormError('')
    setView('form')
  }

  const handleDelete = async (s: SignalSource) => {
    const ok = await confirm({
      title: `删除信号源「${s.name}」？`,
      message: '活跃订阅将被取消（账单流水保留），该操作不可撤销。',
      confirmText: '删除',
      variant: 'danger',
    })
    if (ok) deleteMutation.mutate(s.id)
  }

  const handleSubmit = () => {
    const name = form.name.trim()
    if (!name) {
      setFormError('名称不能为空')
      return
    }
    if (form.fee_model === 'profit_share' && num(form.fee_percent) <= 0) {
      setFormError('盈利分成模式要求分成比例 > 0')
      return
    }
    if (form.fee_model === 'fixed_monthly' && num(form.monthly_fee) <= 0) {
      setFormError('固定月费模式要求月费 > 0')
      return
    }
    setFormError('')

    const tpslValues = [num(form.tp1_pct), num(form.tp2_pct), num(form.tp3_pct), num(form.sl_pct)]
    const tp_sl = tpslValues.some((v) => v > 0)
      ? { tp1_pct: tpslValues[0], tp2_pct: tpslValues[1], tp3_pct: tpslValues[2], sl_pct: tpslValues[3] }
      : undefined

    if (editing) {
      updateMutation.mutate({
        id: editing.id,
        payload: {
          name,
          type: form.type as SignalSource['type'],
          enabled: form.enabled,
          fee_model: form.fee_model,
          fee_percent: num(form.fee_percent),
          monthly_fee: num(form.monthly_fee),
          tp_sl_config: tp_sl,
        },
      })
    } else {
      createMutation.mutate({
        name,
        type: form.type,
        enabled: form.enabled,
        fee_model: form.fee_model,
        fee_percent: num(form.fee_percent),
        monthly_fee: num(form.monthly_fee),
        tp_sl,
      })
    }
  }

  if (!open) return null

  const saving = createMutation.isPending || updateMutation.isPending

  // ConfirmDialog 渲染为遮罩的兄弟节点，避免其背景点击冒泡误关管理弹窗。
  return (
    <>
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
        className="w-full max-w-2xl max-h-[85vh] flex flex-col rounded-xl border border-quant-border bg-quant-card shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-quant-border">
          <h3 className="text-base font-semibold text-foreground">
            {view === 'form' ? (editing ? '编辑信号源' : '新建信号源') : '信号源管理'}
          </h3>
          <button
            onClick={onClose}
            className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-5">
          {view === 'list' ? (
            <div className="space-y-3">
              <div className="flex justify-end">
                <Button variant="outline" size="sm" leftIcon={<Plus className="w-3.5 h-3.5" />} onClick={openCreate}>
                  新建信号源
                </Button>
              </div>

              {isLoading ? (
                <div className="text-xs text-muted-foreground py-6 text-center">加载中...</div>
              ) : sources.length === 0 ? (
                <div className="text-xs text-muted-foreground py-6 text-center">
                  暂无信号源，点击「新建信号源」创建第一个。
                </div>
              ) : (
                sources.map((s) => (
                  <div key={s.id} className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4">
                    <div className="flex items-center justify-between gap-2 flex-wrap">
                      <div className="flex items-center gap-2 min-w-0">
                        {typeIcon(s.type)}
                        <span className="text-sm font-medium text-[#e0e0e0] truncate">{s.name}</span>
                        <Badge variant={s.enabled ? 'success' : 'neutral'} dot>
                          {s.enabled ? '启用' : '停用'}
                        </Badge>
                        <Badge
                          variant={s.fee_model === 'profit_share' ? 'warning' : s.fee_model === 'fixed_monthly' ? 'info' : 'success'}
                          className="text-[10px]"
                        >
                          {feeModelLabel(s.fee_model)}
                          {s.fee_model === 'profit_share' && s.fee_percent ? ` ${s.fee_percent}%` : ''}
                          {s.fee_model === 'fixed_monthly' && s.monthly_fee ? ` ${s.monthly_fee}` : ''}
                        </Badge>
                      </div>
                      <div className="flex items-center gap-1.5">
                        {!canEdit(s) && (
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={!s.enabled || subscribeMutation.isPending}
                            isLoading={subscribeMutation.isPending && subscribeMutation.variables === s.id}
                            leftIcon={<BellPlus className="w-3.5 h-3.5" />}
                            onClick={() => subscribeMutation.mutate(s.id)}
                          >
                            订阅
                          </Button>
                        )}
                        {canEdit(s) && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              leftIcon={<Users className="w-3.5 h-3.5" />}
                              onClick={() => setSubscribersFor(subscribersFor === s.id ? null : s.id)}
                            >
                              订阅者
                            </Button>
                            <Button variant="ghost" size="sm" leftIcon={<Pencil className="w-3.5 h-3.5" />} onClick={() => openEdit(s)}>
                              编辑
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-[#f5222d] hover:text-[#ff4d4f]"
                              leftIcon={<Trash2 className="w-3.5 h-3.5" />}
                              onClick={() => handleDelete(s)}
                            >
                              删除
                            </Button>
                          </>
                        )}
                      </div>
                    </div>

                    <div className="mt-2 text-xs text-[#555]">
                      今日信号 {s.signal_count_today} / 累计 {s.signal_count_total}
                    </div>

                    {/* 订阅者列表（属主/admin 视角） */}
                    {subscribersFor === s.id && (
                      <div className="mt-3 border-t border-[#1c1c1c] pt-3">
                        {subscribersLoading ? (
                          <div className="text-xs text-muted-foreground">加载中...</div>
                        ) : !subscribersData || subscribersData.length === 0 ? (
                          <div className="text-xs text-muted-foreground">暂无订阅者</div>
                        ) : (
                          <div className="space-y-1.5">
                            {subscribersData.map((sub) => (
                              <div
                                key={sub.id}
                                className="flex items-center justify-between gap-2 text-xs rounded-lg bg-[#111] border border-[#1c1c1c] px-3 py-2"
                              >
                                <span className="text-[#aaa]">用户 #{sub.user_id}</span>
                                <span className="text-[#888]">{feeModelLabel(sub.fee_model)}</span>
                                <Badge variant={sub.status === 'active' ? 'success' : 'neutral'} className="text-[10px]">
                                  {sub.status}
                                </Badge>
                                {sub.next_billing_at > 0 && (
                                  <span className="text-[#555]">
                                    下次扣费 {new Date(sub.next_billing_at * 1000).toLocaleDateString('zh-CN')}
                                  </span>
                                )}
                                {sub.fee_model === 'profit_share' && sub.pending_share > 0 && (
                                  <span className="text-[#faad14]">待结算 {sub.pending_share.toFixed(2)}</span>
                                )}
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                ))
              )}
            </div>
          ) : (
            /* ── 新建/编辑表单 ── */
            <div className="space-y-4">
              <Input
                label="名称"
                required
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="例如：TradingView Webhook"
              />
              <div className="grid grid-cols-2 gap-4">
                <Select
                  label="类型"
                  value={form.type}
                  onChange={(e) => setForm({ ...form, type: e.target.value })}
                  options={[
                    { value: 'webhook', label: 'Webhook' },
                    { value: 'api', label: 'API' },
                    { value: 'internal', label: '内部策略' },
                  ]}
                />
                <Select
                  label="定价模型"
                  value={form.fee_model}
                  onChange={(e) => setForm({ ...form, fee_model: e.target.value })}
                  options={[
                    { value: 'free', label: '免费' },
                    { value: 'fixed_monthly', label: '固定月费' },
                    { value: 'profit_share', label: '盈利分成' },
                  ]}
                />
              </div>

              {form.fee_model === 'profit_share' && (
                <Input
                  label="分成比例（%）"
                  required
                  type="number"
                  min="0"
                  step="0.1"
                  value={form.fee_percent}
                  onChange={(e) => setForm({ ...form, fee_percent: e.target.value })}
                  placeholder="例如：20"
                />
              )}
              {form.fee_model === 'fixed_monthly' && (
                <Input
                  label="月费（credits）"
                  required
                  type="number"
                  min="0"
                  step="1"
                  value={form.monthly_fee}
                  onChange={(e) => setForm({ ...form, monthly_fee: e.target.value })}
                  placeholder="例如：10"
                />
              )}

              <div>
                <div className="text-xs font-medium text-[#aaaaaa] mb-1.5">止盈/止损（%，可选）</div>
                <div className="grid grid-cols-4 gap-2">
                  {(
                    [
                      ['tp1_pct', 'TP1'],
                      ['tp2_pct', 'TP2'],
                      ['tp3_pct', 'TP3'],
                      ['sl_pct', 'SL'],
                    ] as const
                  ).map(([key, label]) => (
                    <Input
                      key={key}
                      label={label}
                      type="number"
                      min="0"
                      step="0.1"
                      value={form[key]}
                      onChange={(e) => setForm({ ...form, [key]: e.target.value })}
                    />
                  ))}
                </div>
              </div>

              <label className="flex items-center gap-2 text-sm text-[#aaa] cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.enabled}
                  onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                  className="accent-[#1890ff]"
                />
                启用该信号源
              </label>

              {formError && (
                <div role="alert" className="text-xs text-[#f5222d]">
                  {formError}
                </div>
              )}

              <div className="flex justify-end gap-2 pt-2">
                <Button variant="ghost" size="sm" onClick={() => setView('list')} disabled={saving}>
                  返回
                </Button>
                <Button variant="primary" size="sm" onClick={handleSubmit} isLoading={saving}>
                  {editing ? '保存修改' : '创建'}
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
      </div>
      <Dialog />
    </>
  )
}

export default SignalSourceManager
