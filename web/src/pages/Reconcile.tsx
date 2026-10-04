import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/authStore'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { Select } from '@/components/ui/Select'
import { Input } from '@/components/ui/Input'
import { Switch } from '@/components/ui/Switch'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { DataTable } from '@/components/DataTable'
import {
  reconcileApi,
  type ReconcileDiff,
  type ReconcileDeviation,
  type ReconcileDiffAction,
  type ReconcileConfig,
  type ReconcileConfigUpdate,
} from '@/lib/api'
import { Scale, RefreshCw, Play } from 'lucide-react'

const PAGE_SIZE = 20
// 与 reconcile.Service.runAll 的任务清单一致
const TASK_ORDER = ['positions', 'fills', 'funding', 'deviations', 'reported_pnl']
const DIFF_TYPES = ['position_quantity', 'position_missing_exchange', 'position_missing_local', 'funding']
const DEVIATION_KINDS = ['slippage', 'stuck']

function fmtTime(ms?: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN')
}

function fmtQty(n?: number): string {
  if (n === undefined || n === null) return '-'
  return n.toLocaleString('en-US', { maximumFractionDigits: 8 })
}

function fmtMoney(n: number): string {
  const s = n.toLocaleString('en-US', { maximumFractionDigits: 8 })
  return n > 0 ? `+${s}` : s
}

function diffStatusVariant(status: string): 'warning' | 'success' | 'neutral' {
  if (status === 'open') return 'warning'
  if (status === 'resolved') return 'success'
  return 'neutral'
}

function pnlStatusVariant(status: string): 'success' | 'error' | 'warning' | 'neutral' {
  if (status === 'ok') return 'success'
  if (status === 'mismatch') return 'error'
  if (status === 'error') return 'warning'
  return 'neutral'
}

/** 分页条（limit/offset 无总数：满页即可能有下一页） */
function Pager({ page, hasMore, onPage }: { page: number; hasMore: boolean; onPage: (p: number) => void }) {
  const { t } = useI18n()
  return (
    <div className="mt-3 flex items-center justify-end gap-2 text-xs">
      <button
        onClick={() => onPage(Math.max(0, page - 1))}
        disabled={page === 0}
        className="rounded px-2 py-1 text-muted-foreground hover:bg-white/5 disabled:opacity-30 transition-colors"
      >
        {t('reconcile.pager.prev')}
      </button>
      <span className="text-muted-foreground tabular-nums">
        {t('reconcile.pager.page').replace('{page}', String(page + 1))}
      </span>
      <button
        onClick={() => onPage(page + 1)}
        disabled={!hasMore}
        className="rounded px-2 py-1 text-muted-foreground hover:bg-white/5 disabled:opacity-30 transition-colors"
      >
        {t('reconcile.pager.next')}
      </button>
    </div>
  )
}

// ── 状态总览（GET /reconcile/status）──
function StatusOverview() {
  const { t } = useI18n()
  const { data: status, isLoading } = useQuery({
    queryKey: ['reconcile', 'status'],
    queryFn: () => reconcileApi.status(),
    refetchInterval: 15000,
  })

  if (isLoading) {
    return (
      <SectionCard title={t('reconcile.overview.title')}>
        <Skeleton variant="text" lines={4} />
      </SectionCard>
    )
  }
  if (!status) return null

  const cfg = status.config
  const runs = status.last_runs ?? {}
  const runNames = [...TASK_ORDER, ...Object.keys(runs).filter((k) => !TASK_ORDER.includes(k))].filter(
    (k) => runs[k]
  )
  const audit = status.audit ?? []

  return (
    <>
      <SectionCard title={t('reconcile.overview.title')}>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
            <div className="text-[10px] text-muted-foreground">{t('reconcile.overview.service')}</div>
            <Badge variant={status.running === true ? 'success' : status.running === false ? 'error' : 'neutral'}>
              {status.running === true
                ? t('reconcile.overview.running')
                : status.running === false
                  ? t('reconcile.overview.stopped')
                  : t('reconcile.overview.unknown')}
            </Badge>
          </div>
          <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
            <div className="text-[10px] text-muted-foreground">{t('reconcile.overview.openDiffs')}</div>
            <div className={cn('text-sm font-mono', status.open_diffs > 0 ? 'text-[#f5222d]' : 'text-foreground')}>
              {status.open_diffs}
            </div>
          </div>
          <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
            <div className="text-[10px] text-muted-foreground">{t('reconcile.overview.openDeviations')}</div>
            <div
              className={cn(
                'text-sm font-mono',
                (status.open_deviations ?? 0) > 0 ? 'text-[#faad14]' : 'text-foreground'
              )}
            >
              {status.open_deviations ?? '-'}
            </div>
          </div>
          <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
            <div className="text-[10px] text-muted-foreground">{t('reconcile.overview.interval')}</div>
            <div className="text-sm text-foreground font-mono">
              {cfg ? t('reconcile.overview.intervalValue').replace('{n}', String(cfg.interval_sec)) : '-'}
            </div>
            {cfg && (
              <div className="mt-1 flex flex-wrap gap-1">
                {!cfg.enabled && <Badge variant="error">{t('reconcile.overview.disabled')}</Badge>}
                <Badge variant={cfg.auto_fix ? 'warning' : 'neutral'}>
                  {t('reconcile.overview.autoFix')} {cfg.auto_fix ? t('common.on') : t('common.off')}
                </Badge>
              </div>
            )}
          </div>
        </div>
      </SectionCard>

      <SectionCard title={t('reconcile.runs.title')}>
        <DataTable
          data={runNames.map((name) => runs[name])}
          keyExtractor={(run) => run.name}
          emptyText={t('reconcile.runs.empty')}
          columns={[
            {
              key: 'task',
              title: t('reconcile.runs.task'),
              render: (run) => <span className="text-sm text-foreground">{t(`reconcile.task.${run.name}`, run.name)}</span>,
            },
            {
              key: 'result',
              title: t('reconcile.runs.result'),
              render: (run) => (
                <Badge variant={run.ok ? 'success' : 'error'}>
                  {run.ok ? t('reconcile.runs.ok') : t('reconcile.runs.failed')}
                </Badge>
              ),
            },
            {
              key: 'message',
              title: t('reconcile.runs.message'),
              render: (run) => (
                <span className="text-xs text-muted-foreground font-mono">{run.message || '-'}</span>
              ),
            },
            {
              key: 'time',
              title: t('reconcile.runs.time'),
              render: (run) => <span className="text-xs text-muted-foreground">{fmtTime(run.timestamp)}</span>,
            },
          ]}
        />
      </SectionCard>

      <SectionCard title={t('reconcile.audit.title')}>
        <DataTable
          data={audit}
          keyExtractor={(row) => String(row.id)}
          emptyText={t('reconcile.audit.empty')}
          columns={[
            {
              key: 'action',
              title: t('reconcile.audit.action'),
              render: (row) => <span className="text-xs font-mono text-quant-gold">{row.action}</span>,
            },
            {
              key: 'subject',
              title: t('reconcile.audit.subject'),
              render: (row) => (
                <span className="text-xs text-foreground">
                  {row.exchange} {row.symbol}
                </span>
              ),
            },
            {
              key: 'detail',
              title: t('reconcile.audit.detail'),
              render: (row) => (
                <span className="text-xs text-muted-foreground font-mono whitespace-normal">{row.detail}</span>
              ),
            },
            {
              key: 'time',
              title: t('reconcile.audit.time'),
              render: (row) => <span className="text-xs text-muted-foreground">{fmtTime(row.created_at)}</span>,
            },
          ]}
        />
      </SectionCard>
    </>
  )
}

// ── 对账差异（GET /reconcile/diffs + resolve）──
function DiffsSection() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const { confirm, Dialog } = useConfirmDialog()
  const [statusFilter, setStatusFilter] = useState('open')
  const [typeFilter, setTypeFilter] = useState('')
  const [page, setPage] = useState(0)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)

  const params = useMemo(
    () => ({
      status: statusFilter || undefined,
      type: typeFilter || undefined,
      limit: PAGE_SIZE,
      offset: page * PAGE_SIZE,
    }),
    [statusFilter, typeFilter, page]
  )
  const { data: diffs, isLoading } = useQuery({
    queryKey: ['reconcile', 'diffs', params],
    queryFn: () => reconcileApi.diffs(params),
  })
  const rows = diffs ?? []
  const openRows = rows.filter((d) => d.status === 'open')

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['reconcile'] })

  const doResolve = async (d: ReconcileDiff, action: ReconcileDiffAction) => {
    const localVsExchange = `${fmtQty(d.local_qty)} / ${fmtQty(d.exchange_qty)}`
    const opts = {
      accept_exchange: {
        title: t('reconcile.diffs.confirmAcceptExchange.title'),
        message: t('reconcile.diffs.confirmAcceptExchange.message')
          .replace('{symbol}', `${d.exchange} ${d.symbol}`)
          .replace('{local}', fmtQty(d.local_qty))
          .replace('{exchange}', fmtQty(d.exchange_qty)),
        variant: 'danger' as const,
      },
      accept_local: {
        title: t('reconcile.diffs.confirmAcceptLocal.title'),
        message: t('reconcile.diffs.confirmAcceptLocal.message').replace(
          '{symbol}',
          `${d.exchange} ${d.symbol}（${localVsExchange}）`
        ),
        variant: 'default' as const,
      },
      ignore: {
        title: t('reconcile.diffs.confirmIgnore.title'),
        message: t('reconcile.diffs.confirmIgnore.message').replace('{symbol}', `${d.exchange} ${d.symbol}`),
        variant: 'default' as const,
      },
    }[action]
    if (!(await confirm({ ...opts, confirmText: t('common.confirm'), cancelText: t('common.cancel') }))) return
    try {
      await reconcileApi.resolveDiff(d.id, action)
      toast('success', t('reconcile.diffs.resolveDone').replace('{action}', t(`reconcile.diffs.resolution.${action}`)))
      invalidate()
    } catch {
      toast('error', t('reconcile.diffs.resolveFailed'))
    }
  }

  const doBatch = async (action: Extract<ReconcileDiffAction, 'accept_exchange' | 'ignore'>) => {
    const ids = [...selected]
    if (ids.length === 0) return
    const ok = await confirm({
      title:
        action === 'accept_exchange'
          ? t('reconcile.diffs.batchConfirmExchange.title')
          : t('reconcile.diffs.batchConfirmIgnore.title'),
      message: (
        action === 'accept_exchange'
          ? t('reconcile.diffs.batchConfirmExchange.message')
          : t('reconcile.diffs.batchConfirmIgnore.message')
      ).replace('{n}', String(ids.length)),
      variant: action === 'accept_exchange' ? 'danger' : 'default',
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
    setBusy(true)
    let okCount = 0
    let failCount = 0
    for (const id of ids) {
      try {
        await reconcileApi.resolveDiff(id, action)
        okCount++
      } catch {
        failCount++
      }
    }
    setBusy(false)
    setSelected(new Set())
    toast(
      failCount > 0 ? 'warning' : 'success',
      t('reconcile.diffs.batchResult').replace('{ok}', String(okCount)).replace('{fail}', String(failCount))
    )
    invalidate()
  }

  const toggleAll = (checked: boolean) => {
    setSelected(checked ? new Set(openRows.map((d) => d.id)) : new Set())
  }

  return (
    <SectionCard
      title={t('reconcile.diffs.title')}
      headerAction={
        selected.size > 0 ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground normal-case">
              {t('reconcile.diffs.batchSelected').replace('{n}', String(selected.size))}
            </span>
            <Button size="sm" variant="danger" isLoading={busy} onClick={() => doBatch('accept_exchange')}>
              {t('reconcile.diffs.batchAcceptExchange')}
            </Button>
            <Button size="sm" variant="ghost" isLoading={busy} onClick={() => doBatch('ignore')}>
              {t('reconcile.diffs.batchIgnore')}
            </Button>
          </div>
        ) : undefined
      }
    >
      <div className="mb-3 flex flex-wrap items-end gap-3">
        <div className="w-36">
          <Select
            label={t('reconcile.diffs.status')}
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value)
              setPage(0)
              setSelected(new Set())
            }}
            options={[
              { value: '', label: t('reconcile.diffs.all') },
              { value: 'open', label: t('reconcile.diffs.open') },
              { value: 'resolved', label: t('reconcile.diffs.resolved') },
            ]}
          />
        </div>
        <div className="w-48">
          <Select
            label={t('reconcile.diffs.type')}
            value={typeFilter}
            onChange={(e) => {
              setTypeFilter(e.target.value)
              setPage(0)
              setSelected(new Set())
            }}
            options={[
              { value: '', label: t('reconcile.diffs.all') },
              ...DIFF_TYPES.map((tp) => ({ value: tp, label: t(`reconcile.diffs.type.${tp}`, tp) })),
            ]}
          />
        </div>
      </div>

      {isLoading ? (
        <Skeleton variant="text" lines={5} />
      ) : (
        <>
          <DataTable
            data={rows}
            keyExtractor={(d) => String(d.id)}
            emptyText={statusFilter || typeFilter ? t('reconcile.diffs.emptyFiltered') : t('reconcile.diffs.empty')}
            columns={[
              {
                key: 'select',
                title: '',
                width: '2rem',
                render: (d) =>
                  d.status === 'open' ? (
                    <input
                      type="checkbox"
                      aria-label={t('reconcile.diffs.selectRow')}
                      checked={selected.has(d.id)}
                      onChange={(e) => {
                        const next = new Set(selected)
                        if (e.target.checked) next.add(d.id)
                        else next.delete(d.id)
                        setSelected(next)
                      }}
                      className="accent-quant-gold"
                    />
                  ) : null,
              },
              {
                key: 'symbol',
                title: t('reconcile.diffs.col.symbol'),
                render: (d) => (
                  <div>
                    <div className="text-sm text-foreground font-mono">{d.symbol}</div>
                    <div className="text-[10px] text-muted-foreground">{d.exchange}</div>
                  </div>
                ),
              },
              {
                key: 'type',
                title: t('reconcile.diffs.col.type'),
                render: (d) => (
                  <Badge variant={d.diff_type === 'funding' ? 'info' : 'warning'}>
                    {t(`reconcile.diffs.type.${d.diff_type}`, d.diff_type)}
                  </Badge>
                ),
              },
              {
                key: 'local',
                title: t('reconcile.diffs.col.localValue'),
                render: (d) =>
                  d.diff_type === 'funding' ? (
                    <span className="text-xs text-muted-foreground">-</span>
                  ) : (
                    <div>
                      <div className="text-sm font-mono text-foreground">{fmtQty(d.local_qty)}</div>
                      {d.local_entry_price > 0 && (
                        <div className="text-[10px] text-muted-foreground font-mono">@ {fmtQty(d.local_entry_price)}</div>
                      )}
                    </div>
                  ),
              },
              {
                key: 'exchange',
                title: t('reconcile.diffs.col.exchangeValue'),
                render: (d) =>
                  d.diff_type === 'funding' ? (
                    <span className={cn('text-sm font-mono', d.amount < 0 ? 'text-[#f5222d]' : 'text-[#52c41a]')}>
                      {t('reconcile.diffs.fundingAmount')
                        .replace('{amount}', fmtMoney(d.amount))
                        .replace('{asset}', d.asset)}
                    </span>
                  ) : (
                    <div>
                      <div className="text-sm font-mono text-foreground">{fmtQty(d.exchange_qty)}</div>
                      {d.exchange_entry_price > 0 && (
                        <div className="text-[10px] text-muted-foreground font-mono">
                          @ {fmtQty(d.exchange_entry_price)}
                        </div>
                      )}
                    </div>
                  ),
              },
              {
                key: 'detail',
                title: t('reconcile.diffs.col.detail'),
                render: (d) => (
                  <span className="text-xs text-muted-foreground whitespace-normal">{d.detail || '-'}</span>
                ),
              },
              {
                key: 'status',
                title: t('reconcile.diffs.col.status'),
                render: (d) => (
                  <div>
                    <Badge variant={diffStatusVariant(d.status)}>
                      {d.status === 'open' ? t('reconcile.diffs.open') : t('reconcile.diffs.resolved')}
                    </Badge>
                    {d.status === 'resolved' && d.resolution && (
                      <div className="mt-0.5 text-[10px] text-muted-foreground">
                        {t(`reconcile.diffs.resolution.${d.resolution}`, d.resolution)}
                        {d.resolved_by ? ` · ${d.resolved_by}` : ''}
                      </div>
                    )}
                  </div>
                ),
              },
              {
                key: 'time',
                title: t('reconcile.diffs.col.time'),
                render: (d) => (
                  <div className="text-xs text-muted-foreground">
                    <div>{fmtTime(d.created_at)}</div>
                    {d.resolved_at > 0 && <div>{fmtTime(d.resolved_at)}</div>}
                  </div>
                ),
              },
              {
                key: 'actions',
                title: t('reconcile.diffs.col.actions'),
                render: (d) =>
                  d.status === 'open' ? (
                    <div className="flex flex-col gap-1">
                      <button
                        onClick={() => doResolve(d, 'accept_exchange')}
                        className="rounded px-2 py-1 text-xs font-medium text-[#f5222d] bg-[#f5222d]/10 hover:bg-[#f5222d]/20 transition-colors"
                      >
                        {t('reconcile.diffs.resolution.accept_exchange')}
                      </button>
                      <button
                        onClick={() => doResolve(d, 'accept_local')}
                        className="rounded px-2 py-1 text-xs font-medium text-quant-gold bg-quant-gold/10 hover:bg-quant-gold/20 transition-colors"
                      >
                        {t('reconcile.diffs.resolution.accept_local')}
                      </button>
                      <button
                        onClick={() => doResolve(d, 'ignore')}
                        className="rounded px-2 py-1 text-xs font-medium text-muted-foreground bg-white/5 hover:bg-white/10 transition-colors"
                      >
                        {t('reconcile.diffs.resolution.ignore')}
                      </button>
                    </div>
                  ) : null,
              },
            ]}
          />
          {openRows.length > 1 && (
            <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                aria-label={t('reconcile.diffs.selectAll')}
                checked={openRows.every((d) => selected.has(d.id))}
                onChange={(e) => toggleAll(e.target.checked)}
                className="accent-quant-gold"
              />
              <span>{t('reconcile.diffs.selectAll')}</span>
            </div>
          )}
          <Pager page={page} hasMore={rows.length === PAGE_SIZE} onPage={setPage} />
        </>
      )}
      <Dialog />
    </SectionCard>
  )
}

// ── 实盘偏差（GET /reconcile/deviations + resolve）──
function DeviationsSection() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const { confirm, Dialog } = useConfirmDialog()
  const [statusFilter, setStatusFilter] = useState('open')
  const [kindFilter, setKindFilter] = useState('')
  const [page, setPage] = useState(0)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)

  const params = useMemo(
    () => ({
      status: statusFilter || undefined,
      kind: kindFilter || undefined,
      limit: PAGE_SIZE,
      offset: page * PAGE_SIZE,
    }),
    [statusFilter, kindFilter, page]
  )
  const { data: deviations, isLoading } = useQuery({
    queryKey: ['reconcile', 'deviations', params],
    queryFn: () => reconcileApi.deviations(params),
  })
  const rows = deviations ?? []
  const openRows = rows.filter((d) => d.status === 'open')

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['reconcile'] })

  const doConfirm = async (d: ReconcileDeviation) => {
    const ok = await confirm({
      title: t('reconcile.deviations.confirmTitle'),
      message: t('reconcile.deviations.confirmMessage').replace('{order}', d.order_id),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
    try {
      await reconcileApi.resolveDeviation(d.id)
      toast('success', t('reconcile.deviations.confirmDone'))
      invalidate()
    } catch {
      toast('error', t('reconcile.deviations.confirmFailed'))
    }
  }

  const doBatch = async () => {
    const ids = [...selected]
    if (ids.length === 0) return
    const ok = await confirm({
      title: t('reconcile.deviations.batchConfirmTitle'),
      message: t('reconcile.deviations.batchConfirmMessage').replace('{n}', String(ids.length)),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
    setBusy(true)
    let okCount = 0
    let failCount = 0
    for (const id of ids) {
      try {
        await reconcileApi.resolveDeviation(id)
        okCount++
      } catch {
        failCount++
      }
    }
    setBusy(false)
    setSelected(new Set())
    toast(
      failCount > 0 ? 'warning' : 'success',
      t('reconcile.deviations.batchResult').replace('{ok}', String(okCount)).replace('{fail}', String(failCount))
    )
    invalidate()
  }

  return (
    <SectionCard
      title={t('reconcile.deviations.title')}
      headerAction={
        selected.size > 0 ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground normal-case">
              {t('reconcile.diffs.batchSelected').replace('{n}', String(selected.size))}
            </span>
            <Button size="sm" variant="primary" isLoading={busy} onClick={doBatch}>
              {t('reconcile.deviations.batchConfirm')}
            </Button>
          </div>
        ) : undefined
      }
    >
      <div className="mb-3 flex flex-wrap items-end gap-3">
        <div className="w-36">
          <Select
            label={t('reconcile.diffs.status')}
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value)
              setPage(0)
              setSelected(new Set())
            }}
            options={[
              { value: '', label: t('reconcile.diffs.all') },
              { value: 'open', label: t('reconcile.diffs.open') },
              { value: 'resolved', label: t('reconcile.diffs.resolved') },
            ]}
          />
        </div>
        <div className="w-48">
          <Select
            label={t('reconcile.deviations.kind')}
            value={kindFilter}
            onChange={(e) => {
              setKindFilter(e.target.value)
              setPage(0)
              setSelected(new Set())
            }}
            options={[
              { value: '', label: t('reconcile.diffs.all') },
              ...DEVIATION_KINDS.map((k) => ({ value: k, label: t(`reconcile.deviations.kind.${k}`, k) })),
            ]}
          />
        </div>
      </div>

      {isLoading ? (
        <Skeleton variant="text" lines={5} />
      ) : (
        <>
          <DataTable
            data={rows}
            keyExtractor={(d) => String(d.id)}
            emptyText={t('reconcile.deviations.empty')}
            columns={[
              {
                key: 'select',
                title: '',
                width: '2rem',
                render: (d) =>
                  d.status === 'open' ? (
                    <input
                      type="checkbox"
                      aria-label={t('reconcile.deviations.selectRow')}
                      checked={selected.has(d.id)}
                      onChange={(e) => {
                        const next = new Set(selected)
                        if (e.target.checked) next.add(d.id)
                        else next.delete(d.id)
                        setSelected(next)
                      }}
                      className="accent-quant-gold"
                    />
                  ) : null,
              },
              {
                key: 'order',
                title: t('reconcile.deviations.col.order'),
                render: (d) => (
                  <div>
                    <div className="text-sm font-mono text-foreground">{d.order_id}</div>
                    <div className="text-[10px] text-muted-foreground">
                      {d.exchange} {d.symbol}
                    </div>
                  </div>
                ),
              },
              {
                key: 'kind',
                title: t('reconcile.deviations.kind'),
                render: (d) => (
                  <Badge variant={d.kind === 'stuck' ? 'warning' : 'info'}>
                    {t(`reconcile.deviations.kind.${d.kind}`, d.kind)}
                  </Badge>
                ),
              },
              {
                key: 'expected',
                title: t('reconcile.deviations.col.expected'),
                render: (d) => <span className="text-sm font-mono text-foreground">{d.expected_price > 0 ? fmtQty(d.expected_price) : '-'}</span>,
              },
              {
                key: 'avg',
                title: t('reconcile.deviations.col.avg'),
                render: (d) => <span className="text-sm font-mono text-foreground">{d.avg_price > 0 ? fmtQty(d.avg_price) : '-'}</span>,
              },
              {
                key: 'slippage',
                title: t('reconcile.deviations.col.slippage'),
                render: (d) => (
                  <span className={cn('text-sm font-mono', d.slippage_pct > 0 ? 'text-[#faad14]' : 'text-muted-foreground')}>
                    {d.slippage_pct > 0 ? `${d.slippage_pct.toFixed(4)}%` : '-'}
                  </span>
                ),
              },
              {
                key: 'status',
                title: t('reconcile.diffs.col.status'),
                render: (d) => (
                  <div>
                    <Badge variant={diffStatusVariant(d.status)}>
                      {d.status === 'open' ? t('reconcile.diffs.open') : t('reconcile.diffs.resolved')}
                    </Badge>
                    {d.status === 'resolved' && d.resolved_by && (
                      <div className="mt-0.5 text-[10px] text-muted-foreground">{d.resolved_by}</div>
                    )}
                  </div>
                ),
              },
              {
                key: 'time',
                title: t('reconcile.diffs.col.time'),
                render: (d) => <span className="text-xs text-muted-foreground">{fmtTime(d.created_at)}</span>,
              },
              {
                key: 'actions',
                title: t('reconcile.diffs.col.actions'),
                render: (d) =>
                  d.status === 'open' ? (
                    <button
                      onClick={() => doConfirm(d)}
                      className="rounded px-2 py-1 text-xs font-medium text-quant-gold bg-quant-gold/10 hover:bg-quant-gold/20 transition-colors"
                    >
                      {t('reconcile.deviations.confirm')}
                    </button>
                  ) : null,
              },
            ]}
          />
          {openRows.length > 1 && (
            <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                aria-label={t('reconcile.diffs.selectAll')}
                checked={openRows.every((d) => selected.has(d.id))}
                onChange={(e) => setSelected(e.target.checked ? new Set(openRows.map((d) => d.id)) : new Set())}
                className="accent-quant-gold"
              />
              <span>{t('reconcile.diffs.selectAll')}</span>
            </div>
          )}
          <Pager page={page} hasMore={rows.length === PAGE_SIZE} onPage={setPage} />
        </>
      )}
      <Dialog />
    </SectionCard>
  )
}

// ── 回报 PnL 对账（GET /reconcile/reported-pnl + admin run）──
function ReportedPnlSection({ isAdmin }: { isAdmin: boolean }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const { confirm, Dialog } = useConfirmDialog()
  const [statusFilter, setStatusFilter] = useState('')
  const [daysFilter, setDaysFilter] = useState(7)
  const [page, setPage] = useState(0)
  const [runDays, setRunDays] = useState('')
  const [running, setRunning] = useState(false)

  const params = useMemo(
    () => ({
      status: statusFilter || undefined,
      days: daysFilter > 0 ? daysFilter : undefined,
      limit: PAGE_SIZE,
      offset: page * PAGE_SIZE,
    }),
    [statusFilter, daysFilter, page]
  )
  const { data: checks, isLoading } = useQuery({
    queryKey: ['reconcile', 'reported-pnl', params],
    queryFn: () => reconcileApi.reportedPnl(params),
  })
  const rows = checks ?? []

  const doRun = async () => {
    const days = runDays.trim() === '' ? 0 : Number(runDays)
    if (runDays.trim() !== '' && (!Number.isFinite(days) || days <= 0 || !Number.isInteger(days))) {
      toast('error', t('reconcile.config.invalid'))
      return
    }
    const windowLabel =
      days > 0
        ? t('reconcile.pnl.runConfirm.daysWindow').replace('{n}', String(days))
        : t('reconcile.pnl.runConfirm.defaultWindow')
    const ok = await confirm({
      title: t('reconcile.pnl.runConfirm.title'),
      message: t('reconcile.pnl.runConfirm.message').replace('{window}', windowLabel),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
    setRunning(true)
    try {
      const res = await reconcileApi.runReportedPnl(days > 0 ? days : undefined)
      toast('success', t('reconcile.pnl.runDone').replace('{message}', res.message || 'ok'))
      queryClient.invalidateQueries({ queryKey: ['reconcile'] })
    } catch {
      /* 错误 toast 由拦截器统一弹出 */
    } finally {
      setRunning(false)
    }
  }

  return (
    <SectionCard
      title={t('reconcile.pnl.title')}
      headerAction={
        isAdmin ? (
          <div className="flex items-center gap-2 normal-case">
            <div className="w-32">
              <Input
                type="number"
                min={1}
                step={1}
                value={runDays}
                onChange={(e) => setRunDays(e.target.value)}
                placeholder={t('reconcile.pnl.runDaysPlaceholder')}
                aria-label={t('reconcile.pnl.runDays')}
              />
            </div>
            <Button size="sm" variant="outline" leftIcon={<Play className="h-3 w-3" />} isLoading={running} onClick={doRun}>
              {t('reconcile.pnl.run')}
            </Button>
          </div>
        ) : undefined
      }
    >
      <div className="mb-3 flex flex-wrap items-end gap-3">
        <div className="w-36">
          <Select
            label={t('reconcile.pnl.status')}
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value)
              setPage(0)
            }}
            options={[
              { value: '', label: t('reconcile.diffs.all') },
              { value: 'ok', label: t('reconcile.pnl.status.ok') },
              { value: 'mismatch', label: t('reconcile.pnl.status.mismatch') },
              { value: 'error', label: t('reconcile.pnl.status.error') },
            ]}
          />
        </div>
        <div className="w-36">
          <Select
            label={t('reconcile.pnl.days')}
            value={String(daysFilter)}
            onChange={(e) => {
              setDaysFilter(Number(e.target.value))
              setPage(0)
            }}
            options={[
              { value: '0', label: t('reconcile.diffs.all') },
              { value: '1', label: t('reconcile.pnl.days1') },
              { value: '7', label: t('reconcile.pnl.days7') },
              { value: '30', label: t('reconcile.pnl.days30') },
            ]}
          />
        </div>
      </div>

      {isLoading ? (
        <Skeleton variant="text" lines={5} />
      ) : (
        <>
          <DataTable
            data={rows}
            keyExtractor={(c) => String(c.id)}
            emptyText={t('reconcile.pnl.empty')}
            columns={[
              {
                key: 'window',
                title: t('reconcile.pnl.col.window'),
                render: (c) => (
                  <div className="text-xs text-muted-foreground">
                    <div>{fmtTime(c.window_start)}</div>
                    <div>{fmtTime(c.window_end)}</div>
                  </div>
                ),
              },
              {
                key: 'symbol',
                title: t('reconcile.pnl.col.symbol'),
                render: (c) => (
                  <div>
                    <div className="text-sm font-mono text-foreground">{c.symbol || t('reconcile.pnl.allAccount')}</div>
                    <div className="text-[10px] text-muted-foreground">{c.exchange}</div>
                  </div>
                ),
              },
              {
                key: 'local',
                title: t('reconcile.pnl.col.local'),
                render: (c) => (
                  <span className={cn('text-sm font-mono', c.local_pnl < 0 ? 'text-[#f5222d]' : 'text-[#52c41a]')}>
                    {fmtMoney(c.local_pnl)}
                  </span>
                ),
              },
              {
                key: 'reported',
                title: t('reconcile.pnl.col.reported'),
                render: (c) => (
                  <span className={cn('text-sm font-mono', c.reported_pnl < 0 ? 'text-[#f5222d]' : 'text-[#52c41a]')}>
                    {fmtMoney(c.reported_pnl)}
                  </span>
                ),
              },
              {
                key: 'diff',
                title: t('reconcile.pnl.col.diff'),
                render: (c) => (
                  <div>
                    <div className={cn('text-sm font-mono', c.status === 'mismatch' ? 'text-[#f5222d]' : 'text-foreground')}>
                      {fmtMoney(c.diff)}
                    </div>
                    <div className="text-[10px] text-muted-foreground font-mono">{c.diff_pct.toFixed(4)}%</div>
                  </div>
                ),
              },
              {
                key: 'status',
                title: t('reconcile.pnl.status'),
                render: (c) => (
                  <div>
                    <Badge variant={pnlStatusVariant(c.status)}>{t(`reconcile.pnl.status.${c.status}`, c.status)}</Badge>
                    {c.detail && <div className="mt-0.5 text-[10px] text-muted-foreground whitespace-normal">{c.detail}</div>}
                  </div>
                ),
              },
              {
                key: 'checkedAt',
                title: t('reconcile.pnl.col.checkedAt'),
                render: (c) => <span className="text-xs text-muted-foreground">{fmtTime(c.checked_at)}</span>,
              },
            ]}
          />
          <Pager page={page} hasMore={rows.length === PAGE_SIZE} onPage={setPage} />
        </>
      )}
      <Dialog />
    </SectionCard>
  )
}

// ── 配置（GET /reconcile/config；PUT admin-only）──
const NUMERIC_FIELDS: { key: keyof ReconcileConfigUpdate & string; labelKey: string }[] = [
  { key: 'interval_sec', labelKey: 'reconcile.config.intervalSec' },
  { key: 'slippage_pct', labelKey: 'reconcile.config.slippagePct' },
  { key: 'stuck_timeout_sec', labelKey: 'reconcile.config.stuckTimeoutSec' },
  { key: 'min_drift', labelKey: 'reconcile.config.minDrift' },
  { key: 'reported_pnl_window_h', labelKey: 'reconcile.config.reportedPnlWindowH' },
  { key: 'reported_pnl_pct', labelKey: 'reconcile.config.reportedPnlPct' },
]

function ConfigSection({ isAdmin }: { isAdmin: boolean }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const { confirm, Dialog } = useConfirmDialog()
  const { data: cfg, isLoading } = useQuery({
    queryKey: ['reconcile', 'config'],
    queryFn: () => reconcileApi.getConfig(),
  })
  const [enabled, setEnabled] = useState(true)
  const [autoFix, setAutoFix] = useState(false)
  const [nums, setNums] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!cfg) return
    setEnabled(cfg.enabled)
    setAutoFix(cfg.auto_fix)
    setNums({
      interval_sec: String(cfg.interval_sec),
      slippage_pct: String(cfg.slippage_pct),
      stuck_timeout_sec: String(cfg.stuck_timeout_sec),
      min_drift: String(cfg.min_drift),
      reported_pnl_window_h: String(cfg.reported_pnl_window_h),
      reported_pnl_pct: String(cfg.reported_pnl_pct),
    })
  }, [cfg])

  const onSave = async () => {
    const body: ReconcileConfigUpdate = { enabled, auto_fix: autoFix }
    for (const f of NUMERIC_FIELDS) {
      const v = Number(nums[f.key])
      if (!Number.isFinite(v) || v <= 0) {
        toast('error', t('reconcile.config.invalid'))
        return
      }
      ;(body as Record<string, unknown>)[f.key] = v
    }
    const ok = await confirm({
      title: t('reconcile.config.confirmSave.title'),
      message: t('reconcile.config.confirmSave.message'),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
    setSaving(true)
    try {
      await reconcileApi.putConfig(body)
      toast('success', t('reconcile.config.saved'))
      queryClient.invalidateQueries({ queryKey: ['reconcile'] })
    } catch {
      /* 错误 toast 由拦截器统一弹出 */
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionCard title={t('reconcile.config.title')}>
      {isLoading ? (
        <Skeleton variant="text" lines={4} />
      ) : cfg ? (
        <div className="space-y-4">
          <div className="flex flex-wrap gap-6">
            <Switch
              label={t('reconcile.config.enabled')}
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={!isAdmin}
            />
            <div>
              <Switch
                label={t('reconcile.config.autoFix')}
                checked={autoFix}
                onCheckedChange={setAutoFix}
                disabled={!isAdmin}
              />
              <p className="mt-1 text-[10px] text-muted-foreground max-w-xs">{t('reconcile.config.autoFixHint')}</p>
            </div>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            {NUMERIC_FIELDS.map((f) => (
              <Input
                key={f.key}
                label={t(f.labelKey)}
                type="number"
                step="any"
                min={0}
                value={nums[f.key] ?? ''}
                onChange={(e) => setNums((prev) => ({ ...prev, [f.key]: e.target.value }))}
                disabled={!isAdmin}
              />
            ))}
          </div>

          <div>
            <div className="text-[10px] uppercase tracking-wider text-muted-foreground mb-1.5">
              {t('reconcile.config.readonlyTitle')}
            </div>
            <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
              <span>
                {t('reconcile.config.fundingLookbackH')}: <span className="font-mono text-foreground">{cfg.funding_lookback_h}</span>
              </span>
              <span>
                {t('reconcile.config.fillRecoveryMax')}: <span className="font-mono text-foreground">{cfg.fill_recovery_max}</span>
              </span>
            </div>
          </div>

          {isAdmin ? (
            <div>
              <Button variant="primary" size="sm" isLoading={saving} onClick={onSave}>
                {t('reconcile.config.save')}
              </Button>
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">{t('reconcile.config.adminHint')}</p>
          )}
        </div>
      ) : null}
      <Dialog />
    </SectionCard>
  )
}

export function Reconcile() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title={t('reconcile.title')}
          subtitle={t('reconcile.subtitle')}
          icon={<Scale className="w-5 h-5" />}
          actions={
            <Button
              variant="ghost"
              size="sm"
              leftIcon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={() => queryClient.invalidateQueries({ queryKey: ['reconcile'] })}
            >
              {t('common.refresh')}
            </Button>
          }
        />
        <StatusOverview />
        <DiffsSection />
        <DeviationsSection />
        <ReportedPnlSection isAdmin={isAdmin} />
        <ConfigSection isAdmin={isAdmin} />
      </div>
    </div>
  )
}
