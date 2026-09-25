import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ShieldCheck,
  ShieldAlert,
  Activity,
  Percent,
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  Loader2,
  Save,
  Ban,
  CheckCircle2,
  MinusCircle,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { aiGateApi, type AIGateConfig, type AIGateDecision } from '@/lib/api'
import { KPIGrid } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Select } from '@/components/ui/Select'
import { Input } from '@/components/ui/Input'
import { Switch } from '@/components/ui/Switch'
import { toast } from '@/lib/useToast'

/* ── AI 交易决策门面版（对标 QuantDinger JEV 决策门） ─────────────
 * 入场单强制 AI 审批：开关/阈值/处置策略配置 + 拦截率/fail-open 率
 * 统计卡片 + 决策时间线（可展开看理由与上下文摘要）。
 * 由主控挂到 AI 页（与 AIReviewPanel 同级）。
 * ─────────────────────────────────────────────────────────────── */

const fmtTime = (ms: number) => (ms ? new Date(ms).toLocaleString() : '—')
const fmtPct = (v: number | undefined) => (v == null || Number.isNaN(v) ? '—' : `${(v * 100).toFixed(1)}%`)

function decisionBadge(d: AIGateDecision) {
  if (d.fail_open) return <Badge variant="warning">fail-open</Badge>
  switch (d.decision) {
    case 'approve':
      return <Badge variant="success">approve</Badge>
    case 'reject':
      return <Badge variant="error">reject</Badge>
    case 'abstain':
      return <Badge variant="neutral">abstain</Badge>
    case 'bypassed_exit':
      return <Badge variant="info">出场绕过</Badge>
    case 'skipped':
      return <Badge variant="neutral">skipped</Badge>
    default:
      return <Badge variant="neutral">{d.decision}</Badge>
  }
}

function parseContext(ctxJson: string): Record<string, unknown> | null {
  if (!ctxJson) return null
  try {
    return JSON.parse(ctxJson) as Record<string, unknown>
  } catch {
    return null
  }
}

/* ── 时间线行（可展开） ─────────────────────────────────────────── */
function DecisionRow({ d }: { d: AIGateDecision }) {
  const [open, setOpen] = useState(false)
  const ctx = useMemo(() => (open ? parseContext(d.context_json) : null), [open, d.context_json])
  return (
    <>
      <tr
        onClick={() => setOpen((v) => !v)}
        className="cursor-pointer border-b border-[#161616] text-[#c0c0c0] transition-colors hover:bg-[#1a1a1a]"
      >
        <td className="py-2 pr-2 text-xs">
          {open ? <ChevronDown className="h-3.5 w-3.5 text-[#666]" /> : <ChevronRight className="h-3.5 w-3.5 text-[#666]" />}
        </td>
        <td className="py-2 pr-4 text-xs whitespace-nowrap">{fmtTime(d.created_at)}</td>
        <td className="py-2 pr-4 text-xs">{d.source || 'manual'}</td>
        <td className="py-2 pr-4 text-xs">
          {d.symbol}{' '}
          <span className={d.side === 'SELL' ? 'text-[#f5222d]' : 'text-[#52c41a]'}>{d.side}</span>
        </td>
        <td className="py-2 pr-4 text-xs">{decisionBadge(d)}</td>
        <td className="py-2 pr-4 text-xs">{d.confidence > 0 ? d.confidence.toFixed(2) : '—'}</td>
        <td className="py-2 pr-4 text-xs">{d.latency_ms > 0 ? `${d.latency_ms}ms` : '—'}</td>
        <td className="py-2 pr-4 text-xs">
          {d.allowed ? (
            d.executed ? (
              <span className="text-[#52c41a]">已成交</span>
            ) : (
              <span className="text-[#888]">放行</span>
            )
          ) : (
            <span className="text-[#f5222d]">已拦截</span>
          )}
        </td>
      </tr>
      {open && (
        <tr className="border-b border-[#161616] bg-[#0d0d0d]">
          <td colSpan={8} className="px-4 py-3">
            <div className="space-y-2 text-xs text-[#999]">
              {d.reasons?.length > 0 && (
                <div>
                  <span className="text-[#666]">理由：</span>
                  <ul className="ml-4 mt-1 list-disc space-y-0.5">
                    {d.reasons.map((r, i) => (
                      <li key={i}>{r}</li>
                    ))}
                  </ul>
                </div>
              )}
              {d.degrade_reason && (
                <div className="text-[#faad14]">
                  <AlertTriangle className="mr-1 inline h-3 w-3" />
                  degrade：{d.degrade_reason}
                </div>
              )}
              <div className="flex flex-wrap gap-x-4 gap-y-1">
                {d.provider && (
                  <span>
                    provider：<span className="text-[#c0c0c0]">{d.provider}{d.model ? `/${d.model}` : ''}</span>
                  </span>
                )}
                <span>
                  数量：<span className="text-[#c0c0c0]">{d.quantity}</span>
                </span>
                <span>
                  参考价：<span className="text-[#c0c0c0]">{d.ref_price}</span>
                </span>
                {d.order_id && (
                  <span>
                    订单：<span className="text-[#c0c0c0]">{d.order_id}</span>
                  </span>
                )}
                {d.request_hash && (
                  <span title={d.request_hash}>
                    hash：<span className="text-[#666]">{d.request_hash.slice(0, 12)}…</span>
                  </span>
                )}
              </div>
              {ctx && (
                <pre className="max-h-48 overflow-auto rounded border border-[#1c1c1c] bg-[#111] p-2 text-[11px] leading-4 text-[#888]">
                  {JSON.stringify(ctx, null, 2)}
                </pre>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

/* ── 面板 ─────────────────────────────────────────────────────────── */
export function AIGatePanel({ className }: { className?: string }) {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [decisionFilter, setDecisionFilter] = useState('')
  const [draft, setDraft] = useState<Partial<AIGateConfig>>({})

  const configQuery = useQuery({
    queryKey: ['ai-gate', 'config'],
    queryFn: () => aiGateApi.getConfig(),
    staleTime: 30_000,
  })
  const statsQuery = useQuery({
    queryKey: ['ai-gate', 'stats'],
    queryFn: () => aiGateApi.stats(),
    refetchInterval: 60_000,
  })
  const listQuery = useQuery({
    queryKey: ['ai-gate', 'decisions', page, decisionFilter],
    queryFn: () =>
      aiGateApi.listDecisions({
        page,
        page_size: 10,
        decision: decisionFilter || undefined,
      }),
  })

  // 服务端配置 + 本地未保存草稿合并显示
  const cfg: AIGateConfig | undefined = configQuery.data
    ? { ...configQuery.data, ...draft }
    : undefined
  const dirty = Object.keys(draft).length > 0

  const saveMutation = useMutation({
    mutationFn: (data: Partial<AIGateConfig>) => aiGateApi.putConfig(data),
    onSuccess: (saved) => {
      setDraft({})
      queryClient.setQueryData(['ai-gate', 'config'], saved)
      toast('success', '决策门配置已保存')
    },
    onError: (err: Error) => toast('error', `保存失败（需要 admin 权限）: ${err.message}`),
  })

  const stats = statsQuery.data?.stats
  const decisions = listQuery.data?.decisions ?? []
  const total = listQuery.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / 10))

  return (
    <div className={cn('space-y-4', className)}>
      {/* ── 配置 ── */}
      <SectionCard
        title={
          <span className="flex items-center gap-2">
            AI 交易决策门
            {cfg?.enabled ? (
              <Badge variant="success">已启用</Badge>
            ) : (
              <Badge variant="neutral">已关闭</Badge>
            )}
          </span>
        }
        headerAction={<ShieldCheck className="w-4 h-4 text-[#1890ff]" />}
      >
        <p className="mb-3 text-xs text-[#777]">
          入场单在风控检查后、下单前强制 AI 审批（approve/reject/abstain + 置信度）；
          出场/止损单永远绕过；LLM 故障一律 fail-open 放行并记录 degrade 事件。
        </p>
        {cfg && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div className="flex items-end pb-1">
              <Switch
                label="启用决策门"
                checked={cfg.enabled}
                onCheckedChange={(v) => setDraft((d) => ({ ...d, enabled: v }))}
              />
            </div>
            <div className="flex items-end pb-1">
              <Switch
                label="仅 paper 生效"
                checked={cfg.paper_only}
                onCheckedChange={(v) => setDraft((d) => ({ ...d, paper_only: v }))}
              />
            </div>
            <label className="block">
              <span className="mb-1 block text-xs text-[#888]">置信度阈值（低于视为弃权）</span>
              <Input
                type="number"
                min={0}
                max={1}
                step={0.05}
                value={cfg.min_confidence}
                onChange={(e) =>
                  setDraft((d) => ({ ...d, min_confidence: Math.max(0, Math.min(1, Number(e.target.value) || 0)) }))
                }
              />
            </label>
            <Select
              label="弃权（abstain）处置"
              value={cfg.abstain_action}
              onChange={(e) => setDraft((d) => ({ ...d, abstain_action: e.target.value as 'allow' | 'block' }))}
              options={[
                { value: 'allow', label: '放行（默认，fail-open 语义）' },
                { value: 'block', label: '拦截（保守）' },
              ]}
            />
          </div>
        )}
        <div className="mt-3 flex items-center gap-2">
          <div className="flex-1" />
          {dirty && <span className="text-xs text-[#faad14]">有未保存修改</span>}
          <Button
            variant="primary"
            size="sm"
            disabled={!dirty || saveMutation.isPending}
            onClick={() => saveMutation.mutate(draft)}
          >
            {saveMutation.isPending ? <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> : <Save className="mr-1.5 h-4 w-4" />}
            保存配置
          </Button>
        </div>
      </SectionCard>

      {/* ── 统计卡片 ── */}
      <KPIGrid
        items={[
          {
            label: '决策总数',
            value: stats?.total ?? '—',
            icon: <Activity className="h-4 w-4 text-[#1890ff]" />,
          },
          {
            label: '拦截率（已评估）',
            value: statsQuery.data ? fmtPct(statsQuery.data.block_rate) : '—',
            variant: (statsQuery.data?.block_rate ?? 0) > 0.2 ? 'warning' : 'default',
            icon: <Ban className="h-4 w-4 text-[#faad14]" />,
          },
          {
            label: 'fail-open 率',
            value: statsQuery.data ? fmtPct(statsQuery.data.fail_open_rate) : '—',
            variant: (statsQuery.data?.fail_open_rate ?? 0) > 0.1 ? 'error' : 'default',
            icon: <ShieldAlert className="h-4 w-4 text-[#f5222d]" />,
          },
          {
            label: '出场绕过',
            value: stats?.bypassed_exit ?? '—',
            icon: <CheckCircle2 className="h-4 w-4 text-[#52c41a]" />,
          },
        ]}
      />

      {/* ── 决策时间线 ── */}
      <SectionCard
        title={
          <span className="flex items-center gap-2">
            <Percent className="h-4 w-4 text-[#888]" />
            决策时间线
          </span>
        }
        headerAction={
          <div className="w-44">
            <Select
              value={decisionFilter}
              onChange={(e) => {
                setDecisionFilter(e.target.value)
                setPage(1)
              }}
              options={[
                { value: '', label: '全部决策' },
                { value: 'approve', label: 'approve' },
                { value: 'reject', label: 'reject（拦截）' },
                { value: 'abstain', label: 'abstain（弃权）' },
                { value: 'fail_open', label: 'fail_open（故障放行）' },
                { value: 'bypassed_exit', label: 'bypassed_exit（出场绕过）' },
                { value: 'skipped', label: 'skipped（跳过）' },
              ]}
            />
          </div>
        }
      >
        {decisions.length === 0 ? (
          <div className="flex items-center gap-2 py-6 text-sm text-[#666]">
            <MinusCircle className="h-4 w-4" />
            暂无决策记录——开启决策门后，入场单审批会出现在这里
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="border-b border-[#1c1c1c] text-xs text-[#666]">
                    <th className="pb-2 pr-2 font-medium w-6"></th>
                    <th className="pb-2 pr-4 font-medium">时间</th>
                    <th className="pb-2 pr-4 font-medium">来源</th>
                    <th className="pb-2 pr-4 font-medium">标的</th>
                    <th className="pb-2 pr-4 font-medium">决策</th>
                    <th className="pb-2 pr-4 font-medium">置信度</th>
                    <th className="pb-2 pr-4 font-medium">延迟</th>
                    <th className="pb-2 pr-4 font-medium">结果</th>
                  </tr>
                </thead>
                <tbody>
                  {decisions.map((d) => (
                    <DecisionRow key={d.id} d={d} />
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-3 flex items-center justify-between text-xs text-[#888]">
              <span>共 {total} 条</span>
              <div className="flex items-center gap-2">
                <Button variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                  上一页
                </Button>
                <span>
                  {page} / {totalPages}
                </span>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={page >= totalPages}
                  onClick={() => setPage((p) => p + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          </>
        )}
      </SectionCard>
    </div>
  )
}

export default AIGatePanel
