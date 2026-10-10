import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, BadgeCheck, ChevronLeft, ChevronRight, History, LineChart, ShieldCheck } from 'lucide-react'
import { getEcharts } from '@/lib/echarts'
import type { EChartsType } from 'echarts'
import { cn, formatCurrency } from '@/lib/utils'
import { marketListingApi, type MarketBacktestPoint, type MarketListingStats, type MarketListingTrade } from '@/lib/api'
import { useI18n } from '@/i18n'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { Button } from '@/components/ui/Button'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { useToastStore } from '@/stores/toastStore'
import { MarketSubscribeModal } from '@/components/market/MarketSubscribeModal'

const TRADES_PAGE_SIZE = 20

/** closed_at/ts 均为 Unix 秒（grid 成交由后端折算成秒）；缺字段显示 --。 */
function fmtTime(sec: number): string {
  if (!sec) return '--'
  const d = new Date(sec * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function fmtPrice(v?: number): string {
  if (v == null || !isFinite(v)) return '--'
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 6 })
}

/** 收益/回撤曲线（ECharts 懒加载，结构参考 charts/PerformanceChart）。
 *  grid_bot 条目有 backtest 时叠加同期回测虚线（图例注明口径）。 */
function ReturnChart({
  series,
  backtest,
  labels,
  height = 300,
}: {
  series: MarketListingStats[]
  backtest?: MarketBacktestPoint[] | null
  labels: string[]
  height?: number
}) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)
  const hasBacktest = (backtest?.length ?? 0) > 0

  const buildData = () => {
    const toTs = (d: string) => Date.parse(`${d}T00:00:00Z`)
    const retData = series.map((s) => [toTs(s.date), s.total_return_pct])
    let peak = -Infinity
    const ddData = series.map((s) => {
      const eq = 1 + s.total_return_pct / 100
      if (eq > peak) peak = eq
      return [toTs(s.date), peak > 0 ? ((eq - peak) / peak) * 100 : 0]
    })
    const data: { data: number[][] }[] = [{ data: retData }, { data: ddData }]
    if (hasBacktest) data.push({ data: (backtest ?? []).map((p) => [p.time, p.return_pct]) })
    return data
  }

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      const seriesDefs: Record<string, unknown>[] = [
        { name: labels[0], type: 'line', data: [], smooth: true, symbol: 'none', lineStyle: { color: '#03A66D', width: 2 },
          areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(3,166,109,0.12)' }, { offset: 1, color: 'rgba(3,166,109,0)' }] } } },
        { name: labels[1], type: 'line', data: [], smooth: true, symbol: 'none', lineStyle: { color: '#ef4444', width: 1, opacity: 0.6 }, xAxisIndex: 1, yAxisIndex: 1,
          areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(239,68,68,0.15)' }, { offset: 1, color: 'rgba(239,68,68,0)' }] } } },
      ]
      // 回测对照虚线：与实盘累计收益同轴（grid 0），不加面积
      if (hasBacktest) {
        seriesDefs.push({ name: labels[2], type: 'line', data: [], smooth: true, symbol: 'none', lineStyle: { color: '#F0B90B', width: 1.5, type: 'dashed', opacity: 0.9 } })
      }
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        legend: { data: labels, bottom: 0, textStyle: { color: '#888', fontSize: 10 }, itemWidth: 12, itemHeight: 8 },
        grid: [
          { left: 48, right: 16, top: 16, bottom: 64 },
          { left: 48, right: 16, top: '72%', bottom: 28 },
        ],
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          axisPointer: { type: 'cross', link: [{ xAxisIndex: 'all' }] },
          valueFormatter: (v: number) => `${v.toFixed(2)}%`,
        },
        xAxis: [
          { type: 'time', axisLabel: { fontSize: 10, color: '#555555' }, axisLine: { lineStyle: { color: '#1c1c1c' } }, splitLine: { show: false } },
          { type: 'time', gridIndex: 1, axisLabel: { fontSize: 9, color: '#555555' }, axisLine: { lineStyle: { color: '#1c1c1c' } }, splitLine: { show: false } },
        ],
        yAxis: [
          { type: 'value', axisLabel: { fontSize: 10, color: '#555555', formatter: '{value}%' }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
          { type: 'value', gridIndex: 1, axisLabel: { fontSize: 9, color: '#555555', formatter: '{value}%' }, splitLine: { show: false }, max: 0 },
        ],
        series: seriesDefs,
      })
      // echarts 懒加载是异步的：init 完成后立即补一次数据，避免更新 effect
      // 在 chartRef 就绪前跳过导致的空图
      chartRef.current.setOption({ series: buildData() })
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => { disposed = true; chartRef.current?.dispose() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [labels[0], labels[1], labels[2], hasBacktest])

  useEffect(() => {
    if (!chartRef.current) return
    chartRef.current.setOption({ series: buildData() })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [series, backtest, hasBacktest])

  return <div ref={ref} className="w-full" style={{ height }} data-testid="market-return-chart" />
}

function MetricCell({ label, value, tone }: { label: string; value: string; tone?: 'up' | 'down' }) {
  return (
    <div className="text-center p-2.5 bg-quant-bg rounded-lg">
      <div className={cn('text-sm font-bold font-mono', tone === 'up' ? 'text-quant-green' : tone === 'down' ? 'text-quant-red' : 'text-foreground')}>
        {value}
      </div>
      <div className="text-[10px] text-muted-foreground mt-1">{label}</div>
    </div>
  )
}

/** 考核通过快照行：规则红线 vs 最新日快照达标值。 */
function ProbationRow({ label, required, achieved }: { label: string; required: string; achieved: string }) {
  const { t } = useI18n()
  return (
    <div className="flex items-center justify-between text-xs py-1.5 border-b border-quant-border last:border-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono text-muted-foreground">{required}</span>
      <span className="inline-flex items-center gap-1 font-mono text-quant-green">
        <BadgeCheck className="w-3 h-3" />
        {achieved} · {t('market.probation.ok')}
      </span>
    </div>
  )
}

/** 市场条目详情页：平台验证业绩（快照曲线 + 指标卡 + 考核快照 + 成交明细）+ 订阅扣费。 */
export function MarketListingDetail() {
  const { id = '' } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { t } = useI18n()
  const [tradesOffset, setTradesOffset] = useState(0)
  const queryClient = useQueryClient()
  const addToast = useToastStore((s) => s.addToast)
  const { confirm, Dialog } = useConfirmDialog()
  const [payOpen, setPayOpen] = useState(false)

  const { data, isLoading, isError } = useQuery({
    queryKey: ['market', 'listing-stats', id, tradesOffset],
    queryFn: () => marketListingApi.stats(id, { limit: 90, trades_limit: TRADES_PAGE_SIZE, trades_offset: tradesOffset }),
    enabled: !!id,
    retry: false,
  })

  // 我的订阅（判定已订阅/待支付状态；服务端幂等，重复订阅返回既有订阅）
  const subsQ = useQuery({
    queryKey: ['market', 'my-subscriptions'],
    queryFn: () => marketListingApi.mySubscriptions(),
    retry: false,
  })
  const currentSub = (subsQ.data ?? []).find(
    (s) => s.listing_id === id && (s.status === 'active' || s.status === 'pending_payment')
  )

  const subscribeMut = useMutation({
    mutationFn: () => marketListingApi.subscribe(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['market', 'my-subscriptions'] })
      addToast({ type: 'success', message: t('market.detail.subscribeOk'), duration: 3000 })
    },
    onError: (e: Error) =>
      addToast({ type: 'error', message: e.message || t('market.detail.subscribeFail'), duration: 5000 }),
  })

  // 订阅实例化（0060）：克隆源对象配置为当前用户名下实例（幂等）；
  // 成功后 my-subscriptions 带 instance_id，按钮切换为实例链接。
  const instantiateMut = useMutation({
    mutationFn: () => marketListingApi.instantiate(currentSub!.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['market', 'my-subscriptions'] })
      addToast({ type: 'success', message: t('market.detail.instantiateOk'), duration: 4000 })
    },
    onError: (e: Error) =>
      addToast({ type: 'error', message: e.message || t('market.detail.instantiateFail'), duration: 5000 }),
  })

  // 实例管理页：AI 机器人与网格都在机器人中心（/bots，带类型过滤）
  const instanceTarget = (currentSub?.object_kind ?? 'ai_bot') === 'grid_bot' ? 'grid' : 'ai'
  const goInstance = () => navigate('/bots', { state: { type: instanceTarget } })

  const listing = data?.listing
  const series = data?.series ?? []
  const trades = data?.trades ?? []
  const tradesTotal = data?.trades_total ?? 0
  const tradesPage = Math.floor(tradesOffset / TRADES_PAGE_SIZE) + 1
  const tradesPages = Math.max(1, Math.ceil(tradesTotal / TRADES_PAGE_SIZE))
  const latest = series.length > 0 ? series[series.length - 1] : listing?.stats
  const probation = data?.probation
  const verified = data?.metric_source === 'platform_verified'
  const pct = (v?: number) => `${(v ?? 0) >= 0 ? '+' : ''}${(v ?? 0).toFixed(2)}%`

  // 回测对照（回测 vs 实盘双曲线）：grid_bot 有 backtest_series 时叠加虚线；
  // 无对照/降级时只显示后端如实说明（note/error），不渲染空图。
  const backtestSeries = data?.backtest_series ?? null
  const hasBacktest = (backtestSeries?.length ?? 0) > 0
  const deviation = data?.deviation ?? null
  const backtestNoteText = data?.backtest_error || data?.backtest_note
  const chartLabels = hasBacktest
    ? [t('market.detail.chartLive'), t('market.detail.chartDrawdown'), t('market.detail.chartBacktest')]
    : [t('market.detail.chartReturn'), t('market.detail.chartDrawdown')]

  // 收费模式三态：fee_model 权威；存量行缺省按金额字段回落。
  const feeModel =
    listing?.fee_model || (listing?.monthly_fee ? 'monthly' : listing?.fee_percent ? 'profit_share' : 'free')

  const onSubscribe = async () => {
    if (!listing) return
    if (feeModel === 'monthly') {
      setPayOpen(true)
      return
    }
    const message =
      feeModel === 'profit_share'
        ? t('market.detail.subscribeConfirmShare')
            .replace('{name}', listing.name)
            .replace('{pct}', String(listing.fee_percent))
        : t('market.detail.subscribeConfirmFree').replace('{name}', listing.name)
    const ok = await confirm({
      title: t('market.detail.subscribeConfirmTitle'),
      message,
      confirmText: t('market.detail.subscribe'),
    })
    if (!ok) return
    subscribeMut.mutate()
  }

  const subExpiryLabel =
    currentSub?.status === 'active'
      ? currentSub.expires_at > 0
        ? `${t('market.detail.expiresAt')}: ${new Date(currentSub.expires_at * 1000).toLocaleDateString()}`
        : t('market.detail.noExpiry')
      : null

  return (
    <div className="h-full overflow-y-auto p-4 space-y-3">
      {/* 头部：返回 + 名称/类型/付费模式 + 订阅（free/profit_share 确认直订，monthly 走支付弹窗） */}
      <div className="flex items-center gap-2 flex-wrap">
        <button
          onClick={() => navigate('/bots/ai')}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="w-3.5 h-3.5" />
          {t('market.detail.back')}
        </button>
        <span className="flex-1" />
        {verified && (
          <span
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-quant-green/15 text-quant-green border border-quant-green/40 text-[11px]"
            title={t('market.detail.verifiedDesc')}
          >
            <ShieldCheck className="w-3.5 h-3.5" />
            {t('market.detail.verified')}
          </span>
        )}
        {currentSub?.status === 'active' ? (
          <span className="inline-flex items-center gap-2">
            <Button variant="secondary" size="sm" disabled>
              {t('market.detail.subscribed')}
            </Button>
            {currentSub.instance_id ? (
              <Button variant="secondary" size="sm" onClick={goInstance}>
                {t('market.detail.viewInstance')}
              </Button>
            ) : (
              <Button
                variant="primary"
                size="sm"
                onClick={() => instantiateMut.mutate()}
                disabled={instantiateMut.isPending}
                isLoading={instantiateMut.isPending}
              >
                {t('market.detail.instantiate')}
              </Button>
            )}
            {subExpiryLabel && <span className="text-[10px] text-muted-foreground">{subExpiryLabel}</span>}
          </span>
        ) : (
          <Button
            variant="primary"
            size="sm"
            onClick={onSubscribe}
            disabled={subscribeMut.isPending || !listing}
            isLoading={subscribeMut.isPending}
          >
            {currentSub?.status === 'pending_payment' ? t('market.detail.pendingPayment') : t('market.detail.subscribe')}
          </Button>
        )}
      </div>

      {isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-16 rounded-xl" />
          <Skeleton className="h-72 rounded-xl" />
          <Skeleton className="h-40 rounded-xl" />
        </div>
      ) : isError || !listing ? (
        <EmptyState icon={<LineChart className="w-8 h-8" />} title={t('market.detail.loadFail')} />
      ) : (
        <>
          <div className="bg-quant-card border border-quant-border rounded-xl p-3.5">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-bold text-sm text-foreground">{listing.name}</span>
              <span className="px-1 py-0.5 rounded bg-violet-500/15 text-violet-400 text-[10px]">
                {(listing.object_kind ?? 'ai_bot') === 'grid_bot' ? t('market.my.objectGridBot') : t('market.my.objectAiBot')}
              </span>
              <span className="px-1 py-0.5 rounded bg-cyan-500/15 text-cyan-400 text-[10px]">
                {listing.kind === 'signal' ? t('market.board.kindSignal') : t('market.board.kindRobot')}
              </span>
              {listing.monthly_fee > 0 ? (
                <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px]">
                  ${formatCurrency(listing.monthly_fee)}/{t('market.unit.month')}
                </span>
              ) : listing.fee_percent > 0 ? (
                <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px]">
                  {t('market.board.profitShare')} {listing.fee_percent}%
                </span>
              ) : (
                <span className="px-1.5 py-0.5 rounded bg-quant-bg text-muted-foreground border border-quant-border text-[10px]">
                  {t('market.board.free')}
                </span>
              )}
            </div>
            {listing.description && <p className="mt-1.5 text-xs text-muted-foreground">{listing.description}</p>}
          </div>

          {/* 关键指标卡（最新日快照） */}
          {latest && (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-2">
              <MetricCell label={t('market.stats.monthlyReturn')} value={pct(latest.monthly_return_pct)} tone={latest.monthly_return_pct >= 0 ? 'up' : 'down'} />
              <MetricCell label={t('market.stats.maxDrawdown')} value={`-${Math.abs(latest.max_drawdown_pct).toFixed(2)}%`} tone="down" />
              <MetricCell label={t('market.stats.winRate')} value={`${latest.win_rate.toFixed(1)}%`} />
              <MetricCell label={t('market.stats.profitFactor')} value={latest.profit_factor >= 999 ? '≥999' : latest.profit_factor.toFixed(2)} />
              <MetricCell label={t('market.stats.totalTrades')} value={String(latest.total_trades)} />
              <MetricCell label={t('market.stats.runningDays')} value={`${latest.running_days}${t('market.unit.day')}`} />
            </div>
          )}

          {/* 考核通过信息：规则快照 vs 达标值（listed 即通过双重准入） */}
          {probation && listing.probation_passed && (
            <SectionCard title={t('market.detail.probationTitle')}>
              <ProbationRow
                label={t('market.probation.days')}
                required={`≥ ${probation.rule_min_days}${t('market.unit.day')}`}
                achieved={`${probation.days_elapsed ?? 0}${t('market.unit.day')}`}
              />
              <ProbationRow
                label={t('market.stats.totalTrades')}
                required={`≥ ${probation.rule_min_trades}`}
                achieved={String(probation.trades_in_window ?? 0)}
              />
              <ProbationRow
                label={t('market.probation.drawdownLimit')}
                required={`< ${probation.rule_max_drawdown_pct}%`}
                achieved={`${(probation.max_drawdown_pct ?? 0).toFixed(2)}%`}
              />
            </SectionCard>
          )}

          {/* 收益/回撤曲线（日快照序列；grid_bot 叠加同期回测虚线 + 偏差卡） */}
          <SectionCard title={t('market.detail.equityTitle')}>
            {(deviation || backtestNoteText) && (
              <div className="mb-2 flex flex-wrap items-center gap-2">
                {deviation && (
                  <span
                    className="inline-flex items-baseline gap-1.5 px-2.5 py-1.5 bg-quant-bg rounded-lg"
                    data-testid="market-deviation"
                    title={backtestNoteText ?? undefined}
                  >
                    <span
                      className={cn(
                        'text-sm font-bold font-mono',
                        deviation.diff_pct >= 0 ? 'text-quant-green' : 'text-quant-red'
                      )}
                    >
                      {deviation.diff_pct >= 0 ? '+' : ''}
                      {deviation.diff_pct.toFixed(2)}%
                    </span>
                    <span className="text-[10px] text-muted-foreground">{t('market.detail.deviation')}</span>
                  </span>
                )}
                {backtestNoteText && (
                  <span className="text-[10px] text-muted-foreground flex-1 min-w-[200px]">{backtestNoteText}</span>
                )}
              </div>
            )}
            {series.length === 0 ? (
              <div className="flex h-40 items-center justify-center text-xs text-muted-foreground">{t('market.detail.noSeries')}</div>
            ) : (
              <ReturnChart series={series} backtest={backtestSeries} labels={chartLabels} />
            )}
          </SectionCard>

          {/* 成交明细（分页） */}
          <SectionCard
            title={
              <span className="flex items-center gap-1.5">
                <History className="h-4 w-4 text-quant-gold" />
                {t('market.detail.tradesTitle')}
                {tradesTotal > 0 && <span className="text-[10px] font-normal text-muted-foreground">{tradesTotal}</span>}
              </span>
            }
          >
            {trades.length === 0 ? (
              <div className="flex h-32 items-center justify-center rounded-lg border border-dashed border-[#1c1c1c] bg-[#0a0a0a]">
                <p className="text-xs text-[#8a8a8a]">{t('market.detail.tradesEmpty')}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-[11px]">
                  <thead>
                    <tr className="text-left text-muted-foreground">
                      <th className="py-1 pr-2 font-normal">{t('market.detail.colClosedAt')}</th>
                      <th className="py-1 pr-2 font-normal">{t('market.detail.colSymbol')}</th>
                      <th className="py-1 pr-2 font-normal">{t('market.detail.colSide')}</th>
                      <th className="py-1 pr-2 font-normal text-right">{t('market.detail.colQty')}</th>
                      <th className="py-1 pr-2 font-normal text-right">{t('market.detail.colEntry')}</th>
                      <th className="py-1 pr-2 font-normal text-right">{t('market.detail.colExit')}</th>
                      <th className="py-1 pr-2 font-normal text-right">{t('market.detail.colPnl')}</th>
                      <th className="py-1 font-normal">{t('market.detail.colReason')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {trades.map((tr: MarketListingTrade) => (
                      <tr key={tr.id} className="border-t border-[#1c1c1c]">
                        <td className="py-1.5 pr-2 font-mono text-muted-foreground whitespace-nowrap">{fmtTime(tr.closed_at || tr.opened_at)}</td>
                        <td className="py-1.5 pr-2 font-mono">{tr.symbol || '--'}</td>
                        <td className="py-1.5 pr-2">
                          <span
                            className={cn(
                              'inline-flex rounded px-1.5 py-0.5 text-[10px] font-medium',
                              tr.side === 'LONG' || tr.side === 'BUY' ? 'bg-[#0ECB81]/10 text-[#0ECB81]' : 'bg-red-500/10 text-red-400'
                            )}
                          >
                            {tr.side || '--'}
                          </span>
                        </td>
                        <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.quantity)}</td>
                        <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.entry_price)}</td>
                        <td className="py-1.5 pr-2 text-right font-mono">{fmtPrice(tr.exit_price)}</td>
                        <td
                          className={cn(
                            'py-1.5 pr-2 text-right font-mono',
                            !tr.pnl ? 'text-muted-foreground' : tr.pnl >= 0 ? 'text-[#0ECB81]' : 'text-red-400'
                          )}
                        >
                          {tr.pnl ? `${tr.pnl >= 0 ? '+' : ''}${fmtPrice(tr.pnl)}` : '--'}
                          {tr.pnl_pct ? (
                            <span className="ml-1 text-[10px] text-muted-foreground">
                              ({tr.pnl_pct >= 0 ? '+' : ''}
                              {tr.pnl_pct.toFixed(2)}%)
                            </span>
                          ) : null}
                        </td>
                        <td className="py-1.5 text-muted-foreground">{tr.close_reason || '--'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {tradesPages > 1 && (
              <div className="mt-2 flex items-center justify-center gap-3 text-xs text-muted-foreground">
                <button
                  disabled={tradesOffset <= 0}
                  onClick={() => setTradesOffset((o) => Math.max(0, o - TRADES_PAGE_SIZE))}
                  className="p-1 rounded border border-quant-border disabled:opacity-40 hover:text-foreground"
                  aria-label="prev trades page"
                >
                  <ChevronLeft className="w-3.5 h-3.5" />
                </button>
                <span>
                  {tradesPage} / {tradesPages}
                </span>
                <button
                  disabled={tradesPage >= tradesPages}
                  onClick={() => setTradesOffset((o) => o + TRADES_PAGE_SIZE)}
                  className="p-1 rounded border border-quant-border disabled:opacity-40 hover:text-foreground"
                  aria-label="next trades page"
                >
                  <ChevronRight className="w-3.5 h-3.5" />
                </button>
              </div>
            )}
          </SectionCard>
        </>
      )}

      {payOpen && listing && (
        <MarketSubscribeModal
          listing={listing}
          onClose={() => setPayOpen(false)}
          onSubscribed={() => queryClient.invalidateQueries({ queryKey: ['market', 'my-subscriptions'] })}
        />
      )}
      <Dialog />
    </div>
  )
}

export default MarketListingDetail
