import { useState, useEffect, useRef, useMemo } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import type { EChartsType } from 'echarts'
import {
  PieChart,
  Plus,
  Trash2,
  Play,
  Loader2,
  ArrowLeft,
  History,
  Scale,
  TrendingUp,
  TrendingDown,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  portfolioBacktestApi,
  shareApi,
  type PortfolioBacktestRequest,
  type PortfolioBacktestResult,
  type PortfolioLegConfig,
  type ShareBacktestCard,
} from '@/lib/api'
import { INTERVAL_OPTIONS } from '@/lib/constants'
import { getEcharts } from '@/lib/echarts'
import { KPICard, KPIGrid } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { ShareCardModal } from '@/components/ShareCardModal'
import { toast } from '@/lib/useToast'

/* ── 单策略目录（与后端 RunBacktest 目录一致） ─────────────────────── */
const LEG_STRATEGIES = [
  { value: 'sma_cross', label: 'SMA 金叉' },
  { value: 'breakout', label: '突破' },
  { value: 'trend_long', label: '趋势做多' },
  { value: 'trend_short', label: '趋势做空' },
  { value: 'macd_golden_long', label: 'MACD 金叉做多' },
  { value: 'macd_death_short', label: 'MACD 死叉做空' },
  { value: 'ema_follow_trend', label: 'EMA 顺势' },
  { value: 'ema_counter_trend', label: 'EMA 逆势' },
  { value: 'martin_trend', label: '马丁趋势' },
  { value: 'wallstreet', label: '华尔街' },
  { value: 'dual_burn', label: '双向燃烧' },
  { value: 'global_burn', label: '全局燃烧' },
  { value: 'counter_stable', label: '逆势稳健' },
  { value: 'head_tail_arb', label: '首尾套利' },
]

const fmtPct = (v: number | undefined | null, digits = 2) =>
  v == null || Number.isNaN(v) ? '—' : `${Number(v).toFixed(digits)}%`
const fmtNum = (v: number | undefined | null, digits = 2) =>
  v == null || Number.isNaN(v) ? '—' : Number(v).toFixed(digits)

/* ── 组合权益曲线图 ───────────────────────────────────────────────── */
function PortfolioEquityChart({ result }: { result: PortfolioBacktestResult }) {
  const chartRef = useRef<HTMLDivElement>(null)
  const instance = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!chartRef.current || result.equity_curve.length < 2) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      if (instance.current) instance.current.dispose()
      instance.current = echarts.init(chartRef.current, 'dark')
      instance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 56, right: 16, top: 16, bottom: 28 },
        tooltip: {
          trigger: 'axis',
          textStyle: { fontSize: 11 },
          valueFormatter: (v: unknown) => (typeof v === 'number' ? v.toFixed(2) : String(v)),
        },
        xAxis: {
          type: 'category',
          data: result.equity_curve.map((p) => new Date(p.timestamp).toLocaleDateString()),
          axisLabel: { fontSize: 9, color: '#555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
        },
        yAxis: {
          type: 'value',
          axisLabel: { fontSize: 9, color: '#555' },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
        },
        series: [
          {
            name: '组合权益',
            type: 'line',
            showSymbol: false,
            data: result.equity_curve.map((p) => p.equity),
            lineStyle: { width: 1.5, color: '#1890ff' },
            itemStyle: { color: '#1890ff' },
            areaStyle: { opacity: 0.08 },
          },
        ],
      })
    })
    return () => {
      instance.current?.dispose()
      instance.current = null
    }
  }, [result])

  return <div ref={chartRef} className="h-72 w-full" />
}

/* ── 页面 ─────────────────────────────────────────────────────────── */
let legSeq = 0

export function PortfolioBacktest() {
  const navigate = useNavigate()
  const [name, setName] = useState('我的组合')
  const [timeframe, setTimeframe] = useState('1h')
  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [initialCapital, setInitialCapital] = useState(100000)
  const [rebalance, setRebalance] = useState<'none' | 'daily' | 'weekly' | 'monthly'>('none')
  const [legs, setLegs] = useState<PortfolioLegConfig[]>([
    { strategy_type: 'trend_long', symbol: 'BTCUSDT', weight: 0.5 },
    { strategy_type: 'trend_short', symbol: 'ETHUSDT', weight: 0.5 },
  ])
  const [result, setResult] = useState<PortfolioBacktestResult | null>(null)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [shareCard, setShareCard] = useState<ShareBacktestCard | null>(null)
  const [shareLoading, setShareLoading] = useState(false)

  const openShareCard = async (id: string) => {
    setShareLoading(true)
    try {
      setShareCard(await shareApi.backtestCard(id))
    } catch (err) {
      toast('error', `分享卡生成失败: ${(err as Error).message}`)
    } finally {
      setShareLoading(false)
    }
  }

  const historyQuery = useQuery({
    queryKey: ['portfolio-backtests', 'history'],
    queryFn: () => portfolioBacktestApi.list(20),
  })

  const runMutation = useMutation({
    mutationFn: (req: PortfolioBacktestRequest) => portfolioBacktestApi.run(req),
    onSuccess: (data) => {
      setResult(data)
      setDetailId(null)
      toast('success', `组合回测完成：总收益 ${fmtPct(data.report.total_return_pct)}`)
      historyQuery.refetch()
    },
    onError: (err: Error) => toast('error', `组合回测失败: ${err.message}`),
  })

  const loadDetail = async (id: string) => {
    try {
      const d = await portfolioBacktestApi.get(id)
      setDetailId(id)
      setName(d.name)
      setTimeframe(d.timeframe)
      setRebalance((d.rebalance as 'none' | 'daily' | 'weekly' | 'monthly') ?? 'none')
      setInitialCapital(d.initial_capital)
      setStart(d.start_time ? new Date(d.start_time).toISOString().slice(0, 10) : '')
      setEnd(d.end_time ? new Date(d.end_time).toISOString().slice(0, 10) : '')
      if (d.legs?.length) {
        setLegs(d.legs.map((l) => ({ strategy_type: l.strategy_type, symbol: l.symbol, weight: l.weight })))
      }
      // 详情里的 result 是降采样后的持久化数据
      if (d.result) {
        setResult({
          id: d.id,
          name: d.name,
          report: (d.result.report as PortfolioBacktestResult['report']) ?? ({} as PortfolioBacktestResult['report']),
          equity_curve: d.result.equity_curve ?? [],
          drift: d.result.drift ?? [],
          legs: d.result.legs ?? [],
          duration_ms: d.duration_ms,
        })
      }
    } catch (e) {
      toast('error', `加载详情失败: ${(e as Error).message}`)
    }
  }

  const deleteMutation = useMutation({
    mutationFn: (id: string) => portfolioBacktestApi.delete(id),
    onSuccess: () => {
      toast('success', '已删除')
      historyQuery.refetch()
      if (detailId) {
        setResult(null)
        setDetailId(null)
      }
    },
    onError: (err: Error) => toast('error', `删除失败: ${err.message}`),
  })

  const weightSum = useMemo(() => legs.reduce((s, l) => s + (l.weight || 0), 0), [legs])

  const updateLeg = (idx: number, patch: Partial<PortfolioLegConfig>) => {
    setLegs((prev) => prev.map((l, i) => (i === idx ? { ...l, ...patch } : l)))
  }

  const run = () => {
    if (legs.length === 0) {
      toast('error', '至少需要一条 leg')
      return
    }
    runMutation.mutate({
      name,
      timeframe,
      start: start || undefined,
      end: end || undefined,
      initial_capital: initialCapital,
      rebalance,
      legs,
    })
  }

  return (
    <div className="space-y-4 p-4">
      <PageHeader
        title="组合回测"
        subtitle="多策略组合级回测：按权重合成权益曲线，支持定期再平衡"
        icon={<PieChart className="h-6 w-6" />}
        actions={
          <button
            onClick={() => navigate('/backtest')}
            className="flex items-center gap-1.5 rounded-lg border border-[#1c1c1c] bg-[#111] px-3 py-2 text-xs text-[#bbb] hover:bg-[#1c1c1c]"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            返回回测中心
          </button>
        }
      />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-5">
        {/* ── 配置 ── */}
        <SectionCard title="组合配置" className="lg:col-span-2">
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-2">
              <label className="col-span-2 text-xs text-[#888]">
                组合名称
                <input value={name} onChange={(e) => setName(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
              </label>
              <label className="text-xs text-[#888]">
                周期
                <select value={timeframe} onChange={(e) => setTimeframe(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm">
                  {INTERVAL_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </label>
              <label className="text-xs text-[#888]">
                再平衡
                <select value={rebalance} onChange={(e) => setRebalance(e.target.value as typeof rebalance)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm">
                  <option value="none">不再平衡</option>
                  <option value="daily">每日</option>
                  <option value="weekly">每周</option>
                  <option value="monthly">每月</option>
                </select>
              </label>
              <label className="text-xs text-[#888]">
                开始日期
                <input type="date" value={start} onChange={(e) => setStart(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
              </label>
              <label className="text-xs text-[#888]">
                结束日期
                <input type="date" value={end} onChange={(e) => setEnd(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
              </label>
              <label className="col-span-2 text-xs text-[#888]">
                初始资金 (USDT)
                <input type="number" min={100} value={initialCapital} onChange={(e) => setInitialCapital(Number(e.target.value) || 100000)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
              </label>
            </div>

            {/* legs */}
            <div>
              <div className="mb-1.5 flex items-center justify-between">
                <span className="text-xs text-[#888]">策略腿（权重和 {weightSum.toFixed(2)}，运行时自动归一化）</span>
                <button
                  onClick={() => setLegs((prev) => [...prev, { strategy_type: 'sma_cross', symbol: 'SOLUSDT', weight: 1 }])}
                  className="flex items-center gap-1 rounded border border-[#1c1c1c] bg-[#111] px-2 py-1 text-[11px] text-[#bbb] hover:bg-[#1c1c1c]"
                >
                  <Plus className="h-3 w-3" />添加
                </button>
              </div>
              <div className="space-y-2">
                {legs.map((leg, idx) => (
                  <div key={idx} className="flex items-end gap-2 rounded-md border border-[#1c1c1c] bg-[#0d0d0d] p-2">
                    <label className="flex-1 text-[11px] text-[#777]">
                      策略
                      <select value={leg.strategy_type} onChange={(e) => updateLeg(idx, { strategy_type: e.target.value })} className="mt-1 w-full rounded border border-[#1c1c1c] bg-[#111] px-1.5 py-1 text-xs">
                        {LEG_STRATEGIES.map((s) => (
                          <option key={s.value} value={s.value}>{s.label}</option>
                        ))}
                      </select>
                    </label>
                    <label className="w-24 text-[11px] text-[#777]">
                      交易对
                      <input value={leg.symbol} onChange={(e) => updateLeg(idx, { symbol: e.target.value.toUpperCase() })} className="mt-1 w-full rounded border border-[#1c1c1c] bg-[#111] px-1.5 py-1 font-mono text-xs" />
                    </label>
                    <label className="w-16 text-[11px] text-[#777]">
                      权重
                      <input type="number" min={0} step={0.1} value={leg.weight} onChange={(e) => updateLeg(idx, { weight: Number(e.target.value) || 0 })} className="mt-1 w-full rounded border border-[#1c1c1c] bg-[#111] px-1.5 py-1 text-xs" />
                    </label>
                    <button onClick={() => setLegs((prev) => prev.filter((_, i) => i !== idx))} className="rounded p-1.5 text-[#666] hover:bg-[#1c1c1c] hover:text-[#f5222d]" title="删除">
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            </div>

            <button
              onClick={run}
              disabled={runMutation.isPending || legs.length === 0}
              className="flex w-full items-center justify-center gap-1.5 rounded-md bg-[#1890ff] px-3 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            >
              {runMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
              运行组合回测
            </button>
          </div>
        </SectionCard>

        {/* ── 结果 ── */}
        <div className="space-y-4 lg:col-span-3">
          {result ? (
            <>
              <SectionCard
                title={`${result.name}${detailId ? '（历史记录）' : ''} · 权益曲线`}
                headerAction={
                  detailId ? (
                    <button
                      onClick={() => {
                        if (window.confirm(`确定删除组合回测「${result.name}」？该操作不可恢复。`)) deleteMutation.mutate(detailId)
                      }}
                      className="flex items-center gap-1 rounded border border-[#1c1c1c] px-2 py-1 text-[11px] text-[#f5222d] hover:bg-[#1c1c1c]"
                    >
                      <Trash2 className="h-3 w-3" />删除
                    </button>
                  ) : undefined
                }
              >
                <KPIGrid
                  items={[
                    { label: '总收益', value: fmtPct(result.report.total_return_pct), icon: (result.report.total_return_pct ?? 0) >= 0 ? <TrendingUp className="h-4 w-4" /> : <TrendingDown className="h-4 w-4" />, variant: (result.report.total_return_pct ?? 0) >= 0 ? 'success' : 'error' },
                    { label: '最大回撤', value: fmtPct(result.report.max_drawdown_pct), icon: <TrendingDown className="h-4 w-4" />, variant: 'warning' },
                    { label: 'Sharpe', value: fmtNum(result.report.sharpe_ratio), icon: <PieChart className="h-4 w-4" /> },
                    { label: 'Sortino', value: fmtNum(result.report.sortino_ratio), icon: <PieChart className="h-4 w-4" /> },
                    { label: 'Calmar', value: fmtNum(result.report.calmar_ratio), icon: <PieChart className="h-4 w-4" /> },
                    { label: '胜率 %', value: fmtNum(result.report.win_rate_pct, 1), icon: <PieChart className="h-4 w-4" /> },
                    { label: '盈亏比', value: fmtNum(result.report.profit_factor), icon: <PieChart className="h-4 w-4" /> },
                    { label: '总交易', value: result.report.total_trades ?? 0, icon: <PieChart className="h-4 w-4" /> },
                  ]}
                />
                <div className="mt-4">
                  <PortfolioEquityChart result={result} />
                </div>
              </SectionCard>

              {/* legs 贡献 */}
              <SectionCard title="各腿贡献">
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs">
                    <thead>
                      <tr className="border-b border-[#1c1c1c] text-[#666]">
                        <th className="py-1.5 pr-3">策略</th>
                        <th className="py-1.5 pr-3">交易对</th>
                        <th className="py-1.5 pr-3">目标权重</th>
                        <th className="py-1.5 pr-3">初始分配</th>
                        <th className="py-1.5 pr-3">期末价值</th>
                        <th className="py-1.5 pr-3">腿收益 %</th>
                        <th className="py-1.5 pr-3">组合贡献（百分点）</th>
                        <th className="py-1.5 pr-3">交易数</th>
                        <th className="py-1.5 pr-3">胜率 %</th>
                        <th className="py-1.5 pr-3">Sharpe</th>
                      </tr>
                    </thead>
                    <tbody>
                      {result.legs.map((l, i) => (
                        <tr key={`${l.symbol}-${l.strategy_type}-${i}`} className="border-b border-[#141414]">
                          <td className="py-1.5 pr-3 font-mono">{l.strategy_type}</td>
                          <td className="py-1.5 pr-3 font-mono">{l.symbol}</td>
                          <td className="py-1.5 pr-3">{(l.weight * 100).toFixed(1)}%</td>
                          <td className="py-1.5 pr-3">{l.initial_allocation.toFixed(0)}</td>
                          <td className="py-1.5 pr-3">{l.final_value.toFixed(0)}</td>
                          <td className={cn('py-1.5 pr-3', l.total_return_pct >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>{fmtPct(l.total_return_pct)}</td>
                          <td className={cn('py-1.5 pr-3', l.contribution_pct >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>{fmtNum(l.contribution_pct)}</td>
                          <td className="py-1.5 pr-3">{l.trades}</td>
                          <td className="py-1.5 pr-3">{fmtNum(l.win_rate, 1)}</td>
                          <td className="py-1.5 pr-3">{fmtNum(l.sharpe_ratio)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </SectionCard>

              {/* 权重漂移 */}
              {result.drift?.length > 0 && (
                <SectionCard title="权重漂移记录">
                  <div className="max-h-64 space-y-2 overflow-y-auto">
                    {result.drift.map((d, i) => (
                      <div key={i} className="rounded-md border border-[#141414] bg-[#0d0d0d] p-2 text-[11px]">
                        <div className="mb-1 flex items-center gap-2 text-[#888]">
                          <span className="font-mono">{new Date(d.time).toLocaleString()}</span>
                          <span className={cn('rounded px-1.5 py-0.5', d.trigger === 'rebalance' ? 'bg-[#1890ff]/15 text-[#1890ff]' : 'bg-[#666]/15 text-[#999]')}>
                            {d.trigger === 'rebalance' ? '再平衡' : '期末'}
                          </span>
                        </div>
                        <div className="grid grid-cols-2 gap-2">
                          <div>
                            <div className="mb-0.5 text-[#666]">再平衡前</div>
                            {Object.entries(d.weights_before).map(([k, v]) => (
                              <div key={k} className="flex justify-between font-mono text-[#aaa]">
                                <span>{k}</span>
                                <span>{(v * 100).toFixed(1)}%</span>
                              </div>
                            ))}
                          </div>
                          <div>
                            <div className="mb-0.5 text-[#666]">{d.trigger === 'rebalance' ? '再平衡后（目标）' : '目标权重'}</div>
                            {Object.entries(d.weights_after).map(([k, v]) => (
                              <div key={k} className="flex justify-between font-mono text-[#aaa]">
                                <span>{k}</span>
                                <span>{(v * 100).toFixed(1)}%</span>
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </SectionCard>
              )}
            </>
          ) : (
            <SectionCard title="结果">
              <p className="py-10 text-center text-sm text-[#666]">配置组合后点击「运行组合回测」</p>
            </SectionCard>
          )}

          {/* 历史 */}
          <SectionCard title="历史组合回测">
            {historyQuery.data?.backtests?.length ? (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead>
                    <tr className="border-b border-[#1c1c1c] text-[#666]">
                      <th className="py-1.5 pr-3">时间</th>
                      <th className="py-1.5 pr-3">名称</th>
                      <th className="py-1.5 pr-3">周期</th>
                      <th className="py-1.5 pr-3">再平衡</th>
                      <th className="py-1.5 pr-3">总收益 %</th>
                      <th className="py-1.5 pr-3">回撤 %</th>
                      <th className="py-1.5 pr-3">Sharpe</th>
                      <th className="py-1.5 pr-3" />
                    </tr>
                  </thead>
                  <tbody>
                    {historyQuery.data.backtests.map((b) => (
                      <tr key={b.id} className="border-b border-[#141414]">
                        <td className="py-1.5 pr-3">{new Date(b.created_at).toLocaleString()}</td>
                        <td className="py-1.5 pr-3">{b.name}</td>
                        <td className="py-1.5 pr-3 font-mono">{b.timeframe}</td>
                        <td className="py-1.5 pr-3 font-mono">{b.rebalance}</td>
                        <td className={cn('py-1.5 pr-3', b.total_return_pct >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>{fmtPct(b.total_return_pct)}</td>
                        <td className="py-1.5 pr-3">{fmtPct(b.max_drawdown_pct)}</td>
                        <td className="py-1.5 pr-3">{fmtNum(b.sharpe_ratio)}</td>
                        <td className="py-1.5 pr-3">
                          <div className="flex gap-1.5">
                            <button onClick={() => loadDetail(b.id)} className="rounded border border-[#1c1c1c] px-2 py-0.5 text-[11px] text-[#bbb] hover:bg-[#1c1c1c]">
                              查看
                            </button>
                            <button
                              onClick={() => openShareCard(b.id)}
                              disabled={shareLoading}
                              className="rounded border border-quant-gold/30 px-2 py-0.5 text-[11px] text-quant-gold hover:bg-quant-gold/10 disabled:opacity-50"
                            >
                              分享
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="py-3 text-center text-xs text-[#666]">暂无历史记录</p>
            )}
          </SectionCard>
        </div>
      </div>
      <ShareCardModal card={shareCard} onClose={() => setShareCard(null)} />
    </div>
  )
}

export default PortfolioBacktest
