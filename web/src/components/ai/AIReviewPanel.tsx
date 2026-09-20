import { useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Sparkles, ClipboardList, TrendingUp, TrendingDown, Percent, Activity, Loader2, History } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  aiReviewApi,
  strategyApi,
  aiBotApi,
  gridApi,
  dcaBotApi,
  layeredMartinApi,
  type AIReviewReport,
  type AIReviewScopeType,
} from '@/lib/api'
import { KPIGrid } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Select } from '@/components/ui/Select'
import { Input } from '@/components/ui/Input'
import { toast } from '@/lib/useToast'

/* ── AI 策略复盘面板（对标 QuantDinger strategy_review） ─────────────
 * 选择策略/机器人 + 时间窗口 → 基于真实成交同步生成 AI 复盘报告，
 * 支持历史报告回看。由主控挂到策略详情页或 AI 页。
 * ─────────────────────────────────────────────────────────────── */

const DAY_OPTIONS = [7, 14, 30, 60, 90, 180]

const fmtMoney = (v: number | undefined | null) =>
  v == null || Number.isNaN(v) ? '—' : `${v < 0 ? '-' : ''}$${Math.abs(Number(v)).toFixed(2)}`
const fmtPct = (v: number | undefined | null) => (v == null || Number.isNaN(v) ? '—' : `${Number(v).toFixed(1)}%`)
const fmtTime = (ms: number) => (ms ? new Date(ms).toLocaleString() : '—')

function statusBadge(status: AIReviewReport['status']) {
  if (status === 'done') return <Badge variant="success">已完成</Badge>
  if (status === 'failed') return <Badge variant="error">失败</Badge>
  return <Badge variant="warning">生成中</Badge>
}

/* ── 报告正文：按行渲染，纯文本四段式 ─────────────────────────────── */
function ReportBody({ text }: { text: string }) {
  if (!text) return null
  const lines = text.split('\n').filter((l) => l.trim())
  return (
    <div className="space-y-2 text-sm leading-6 text-[#d0d0d0] whitespace-pre-wrap">
      {lines.map((line, i) => {
        const isHeading = /^(一|二|三|四|五|六)[、.]/.test(line.trim())
        return isHeading ? (
          <p key={i} className="pt-2 text-[#e0e0e0] font-semibold">
            {line}
          </p>
        ) : (
          <p key={i}>{line}</p>
        )
      })}
    </div>
  )
}

/* ── 面板 ─────────────────────────────────────────────────────────── */
export function AIReviewPanel({ className }: { className?: string }) {
  const [scopeType, setScopeType] = useState<AIReviewScopeType>('strategy')
  const [scopeId, setScopeId] = useState('')
  const [days, setDays] = useState(30)
  const [current, setCurrent] = useState<AIReviewReport | null>(null)

  /* scope 下拉数据源：策略 + 全量机器人类型 */
  const strategiesQuery = useQuery({
    queryKey: ['ai-review', 'strategies'],
    queryFn: () => strategyApi.list(),
    staleTime: 60_000,
  })
  const aiBotsQuery = useQuery({
    queryKey: ['ai-review', 'ai-bots'],
    queryFn: () => aiBotApi.list(),
    staleTime: 60_000,
  })
  const gridBotsQuery = useQuery({
    queryKey: ['ai-review', 'grid-bots'],
    queryFn: () => gridApi.list(),
    staleTime: 60_000,
  })
  const dcaBotsQuery = useQuery({
    queryKey: ['ai-review', 'dca-bots'],
    queryFn: () => dcaBotApi.list(),
    staleTime: 60_000,
  })
  const lmartinBotsQuery = useQuery({
    queryKey: ['ai-review', 'lmartin-bots'],
    queryFn: () => layeredMartinApi.list(),
    staleTime: 60_000,
  })

  const scopeOptions = useMemo(() => {
    if (scopeType === 'strategy') {
      return (strategiesQuery.data ?? []).map((s) => ({
        value: s.id,
        label: `${s.name}${s.symbol ? ` (${s.symbol})` : ''}`,
      }))
    }
    const opts: { value: string; label: string }[] = []
    for (const b of aiBotsQuery.data ?? []) opts.push({ value: b.id, label: `[AI] ${b.name}` })
    for (const b of gridBotsQuery.data ?? []) opts.push({ value: b.id, label: `[网格] ${b.name}` })
    for (const b of dcaBotsQuery.data ?? []) opts.push({ value: b.id, label: `[定投] ${b.name}` })
    for (const b of lmartinBotsQuery.data ?? []) opts.push({ value: b.id, label: `[马丁] ${b.name}` })
    return opts
  }, [scopeType, strategiesQuery.data, aiBotsQuery.data, gridBotsQuery.data, dcaBotsQuery.data, lmartinBotsQuery.data])

  const historyQuery = useQuery({
    queryKey: ['ai-review', 'history', scopeType, scopeId],
    queryFn: () =>
      aiReviewApi.listReports({
        scope_type: scopeType,
        scope_id: scopeId || undefined,
        limit: 20,
      }),
  })

  const generateMutation = useMutation({
    mutationFn: () => aiReviewApi.generate({ scope_type: scopeType, scope_id: scopeId, days }),
    onSuccess: (data) => {
      setCurrent(data.report)
      historyQuery.refetch()
      if (data.status === 'done') {
        toast('success', 'AI 复盘报告已生成')
      } else {
        toast('warning', `复盘生成失败：${data.msg || data.report?.error || '未知错误'}`)
      }
    },
    onError: (err: Error) => toast('error', `复盘生成失败: ${err.message}`),
  })

  const loadReport = async (id: string) => {
    try {
      const report = await aiReviewApi.getReport(id)
      setCurrent(report)
      setScopeType(report.scope_type)
      setScopeId(report.scope_id)
    } catch (e) {
      toast('error', `加载报告失败: ${(e as Error).message}`)
    }
  }

  const report: AIReviewReport | null = current
  const pnl = report?.total_pnl ?? null

  return (
    <div className={cn('space-y-4', className)}>
      {/* ── 生成条件 ── */}
      <SectionCard title="AI 策略复盘" headerAction={<Sparkles className="w-4 h-4 text-[#1890ff]" />}>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Select
            label="复盘对象类型"
            value={scopeType}
            onChange={(e) => {
              setScopeType(e.target.value as AIReviewScopeType)
              setScopeId('')
              setCurrent(null)
            }}
            options={[
              { value: 'strategy', label: '策略' },
              { value: 'bot', label: '机器人' },
            ]}
          />
          <div className="sm:col-span-2">
            <Select
              label={`选择${scopeType === 'strategy' ? '策略' : '机器人'}`}
              value={scopeId}
              onChange={(e) => {
                setScopeId(e.target.value)
                setCurrent(null)
              }}
              placeholder="请选择…"
              options={scopeOptions}
            />
          </div>
          <label className="block">
            <span className="mb-1 block text-xs text-[#888]">时间窗口（天）</span>
            <Input
              type="number"
              min={1}
              max={365}
              value={days}
              onChange={(e) => setDays(Math.max(1, Math.min(365, Number(e.target.value) || 30)))}
            />
          </label>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {DAY_OPTIONS.map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setDays(d)}
              className={cn(
                'rounded-full border px-3 py-1 text-xs transition-colors',
                days === d
                  ? 'border-[#1890ff] bg-[#1890ff]/10 text-[#1890ff]'
                  : 'border-[#2a2a2a] text-[#888] hover:text-[#ccc]'
              )}
            >
              {d}天
            </button>
          ))}
          <div className="flex-1" />
          <Button
            variant="primary"
            size="sm"
            disabled={!scopeId || generateMutation.isPending}
            onClick={() => generateMutation.mutate()}
          >
            {generateMutation.isPending ? (
              <>
                <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
                生成中…
              </>
            ) : (
              <>
                <Sparkles className="mr-1.5 h-4 w-4" />
                生成复盘报告
              </>
            )}
          </Button>
        </div>
      </SectionCard>

      {/* ── 当前报告 ── */}
      {report && (
        <SectionCard
          title={
            <span className="flex items-center gap-2">
              复盘报告
              {statusBadge(report.status)}
              {report.model && <Badge variant="neutral">{report.model}</Badge>}
            </span>
          }
          headerAction={<span className="text-xs text-[#666]">{fmtTime(report.created_at)}</span>}
        >
          {report.status === 'failed' ? (
            <div className="rounded-lg border border-[#f5222d]/20 bg-[#f5222d]/5 p-4 text-sm text-[#f5222d]">
              生成失败：{report.error || '未知错误'}
            </div>
          ) : (
            <>
              <KPIGrid
                className="mb-4"
                items={[
                  {
                    label: '成交笔数',
                    value: report.trades_count ?? '—',
                    icon: <Activity className="h-4 w-4 text-[#1890ff]" />,
                  },
                  {
                    label: '已实现盈亏',
                    value: fmtMoney(pnl),
                    variant: (pnl ?? 0) >= 0 ? 'success' : 'error',
                    icon:
                      (pnl ?? 0) >= 0 ? (
                        <TrendingUp className="h-4 w-4 text-[#52c41a]" />
                      ) : (
                        <TrendingDown className="h-4 w-4 text-[#f5222d]" />
                      ),
                  },
                  {
                    label: '胜率',
                    value: fmtPct(report.win_rate),
                    icon: <Percent className="h-4 w-4 text-[#faad14]" />,
                  },
                  {
                    label: '最大回撤',
                    value: fmtMoney(report.max_drawdown),
                    icon: <TrendingDown className="h-4 w-4 text-[#f5222d]" />,
                  },
                ]}
              />
              <div className="rounded-lg border border-[#1c1c1c] bg-[#0d0d0d] p-4">
                <ReportBody text={report.report_text} />
              </div>
            </>
          )}
        </SectionCard>
      )}

      {/* ── 历史报告 ── */}
      <SectionCard
        title={
          <span className="flex items-center gap-2">
            <History className="h-4 w-4 text-[#888]" />
            历史报告
          </span>
        }
      >
        {(historyQuery.data ?? []).length === 0 ? (
          <div className="flex items-center gap-2 py-6 text-sm text-[#666]">
            <ClipboardList className="h-4 w-4" />
            暂无复盘记录，选择对象后点击"生成复盘报告"
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-[#1c1c1c] text-xs text-[#666]">
                  <th className="pb-2 pr-4 font-medium">时间</th>
                  <th className="pb-2 pr-4 font-medium">对象</th>
                  <th className="pb-2 pr-4 font-medium">窗口</th>
                  <th className="pb-2 pr-4 font-medium">成交</th>
                  <th className="pb-2 pr-4 font-medium">盈亏</th>
                  <th className="pb-2 pr-4 font-medium">状态</th>
                </tr>
              </thead>
              <tbody>
                {(historyQuery.data ?? []).map((r) => (
                  <tr
                    key={r.id}
                    onClick={() => loadReport(r.id)}
                    className={cn(
                      'cursor-pointer border-b border-[#161616] text-[#c0c0c0] transition-colors hover:bg-[#1a1a1a]',
                      current?.id === r.id && 'bg-[#1a1a1a]'
                    )}
                  >
                    <td className="py-2 pr-4 text-xs">{fmtTime(r.created_at)}</td>
                    <td className="py-2 pr-4 text-xs">
                      <Badge variant={r.scope_type === 'strategy' ? 'info' : 'neutral'}>
                        {r.scope_type === 'strategy' ? '策略' : '机器人'}
                      </Badge>{' '}
                      {r.scope_id}
                    </td>
                    <td className="py-2 pr-4 text-xs">
                      {r.period_start && r.period_end
                        ? `${Math.max(1, Math.round((r.period_end - r.period_start) / 86400000))}天`
                        : '—'}
                    </td>
                    <td className="py-2 pr-4 text-xs">{r.trades_count}</td>
                    <td
                      className={cn('py-2 pr-4 text-xs', (r.total_pnl ?? 0) >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}
                    >
                      {fmtMoney(r.total_pnl)}
                    </td>
                    <td className="py-2 text-xs">{statusBadge(r.status)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </SectionCard>
    </div>
  )
}

export default AIReviewPanel
