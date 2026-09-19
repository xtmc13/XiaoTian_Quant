import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import type { EChartsType } from 'echarts'
import {
  FlaskConical,
  Loader2,
  Play,
  Layers,
  Activity,
  Sigma,
  TrendingUp,
  TrendingDown,
  Database,
  History,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  factorApi,
  type FactorMeta,
  type FactorEvaluation,
  type FactorLayersResult,
  type FactorValue,
} from '@/lib/api'
import { INTERVAL_OPTIONS } from '@/lib/constants'
import { getEcharts } from '@/lib/echarts'
import { KPICard, KPIGrid } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { toast } from '@/lib/useToast'

/* ── Types ───────────────────────────────────────────────────────── */

interface FactorValuesResponse {
  factor: string
  version: number
  category: string
  symbol: string
  tf: string
  bars_used: number
  source: string
  values: FactorValue[]
  closes: { time: number; close: number }[]
  default_params?: Record<string, unknown>
}

const CATEGORY_LABELS: Record<string, string> = {
  momentum: '动量类',
  volatility: '波动类',
  trend: '趋势类',
  volume: '成交量类',
  mean_reversion: '均值回复类',
}

const fmt = (v: number | undefined | null, digits = 4) =>
  v == null || Number.isNaN(v) ? '—' : Number(v).toFixed(digits)

/* ── Factor values chart (factor vs close, dual axis) ────────────── */
function FactorValuesChart({ data }: { data: FactorValuesResponse }) {
  const chartRef = useRef<HTMLDivElement>(null)
  const instance = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!chartRef.current || data.values.length < 2) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      if (instance.current) instance.current.dispose()
      instance.current = echarts.init(chartRef.current, 'dark')
      instance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 56, right: 56, top: 32, bottom: 28 },
        legend: { textStyle: { color: '#888', fontSize: 10 }, top: 4 },
        xAxis: {
          type: 'category',
          data: data.values.map((v) => new Date(v.time).toLocaleDateString()),
          axisLabel: { fontSize: 9, color: '#555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
        },
        yAxis: [
          { type: 'value', name: '因子值', axisLabel: { fontSize: 9, color: '#555' }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
          { type: 'value', name: '收盘', axisLabel: { fontSize: 9, color: '#555' }, splitLine: { show: false } },
        ],
        tooltip: { trigger: 'axis', textStyle: { fontSize: 11 } },
        series: [
          {
            name: data.factor,
            type: 'line',
            showSymbol: false,
            data: data.values.map((v) => v.value),
            lineStyle: { width: 1.5, color: '#1890ff' },
            itemStyle: { color: '#1890ff' },
          },
          {
            name: '收盘',
            type: 'line',
            yAxisIndex: 1,
            showSymbol: false,
            data: data.closes.slice(-data.values.length).map((v) => v.close),
            lineStyle: { width: 1, color: '#faad14', opacity: 0.7 },
            itemStyle: { color: '#faad14' },
          },
        ],
      })
    })
    return () => {
      instance.current?.dispose()
      instance.current = null
    }
  }, [data])

  return <div ref={chartRef} className="h-64 w-full" />
}

/* ── IC series chart ─────────────────────────────────────────────── */
function ICSeriesChart({ series }: { series: FactorEvaluation['ic_series'] }) {
  const chartRef = useRef<HTMLDivElement>(null)
  const instance = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!chartRef.current || series.length < 2) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      if (instance.current) instance.current.dispose()
      instance.current = echarts.init(chartRef.current, 'dark')
      instance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 48, right: 16, top: 32, bottom: 28 },
        legend: { textStyle: { color: '#888', fontSize: 10 }, top: 4 },
        xAxis: {
          type: 'category',
          data: series.map((p) => new Date(p.time).toLocaleDateString()),
          axisLabel: { fontSize: 9, color: '#555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
        },
        yAxis: {
          type: 'value',
          axisLabel: { fontSize: 9, color: '#555' },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
        },
        tooltip: { trigger: 'axis', textStyle: { fontSize: 11 } },
        series: [
          {
            name: 'IC',
            type: 'line',
            showSymbol: false,
            data: series.map((p) => p.ic),
            lineStyle: { width: 1.5, color: '#1890ff' },
            itemStyle: { color: '#1890ff' },
            markLine: {
              silent: true,
              symbol: 'none',
              label: { fontSize: 9, color: '#666' },
              lineStyle: { color: '#444', type: 'dashed' },
              data: [{ yAxis: 0 }],
            },
          },
          {
            name: 'RankIC',
            type: 'line',
            showSymbol: false,
            data: series.map((p) => p.rank_ic),
            lineStyle: { width: 1.5, color: '#52c41a' },
            itemStyle: { color: '#52c41a' },
          },
        ],
      })
    })
    return () => {
      instance.current?.dispose()
      instance.current = null
    }
  }, [series])

  return <div ref={chartRef} className="h-64 w-full" />
}

/* ── Layered bar chart ───────────────────────────────────────────── */
function LayersBarChart({ result }: { result: FactorLayersResult }) {
  const chartRef = useRef<HTMLDivElement>(null)
  const instance = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!chartRef.current || !result.layers.length) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      if (instance.current) instance.current.dispose()
      instance.current = echarts.init(chartRef.current, 'dark')
      instance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 56, right: 16, top: 32, bottom: 28 },
        legend: { textStyle: { color: '#888', fontSize: 10 }, top: 4 },
        xAxis: {
          type: 'category',
          data: result.layers.map((l) => `L${l.layer}`),
          axisLabel: { fontSize: 10, color: '#888' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
        },
        yAxis: [
          { type: 'value', name: '年化收益 %', axisLabel: { fontSize: 9, color: '#555' }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
          { type: 'value', name: '累计净值', axisLabel: { fontSize: 9, color: '#555' }, splitLine: { show: false } },
        ],
        tooltip: { trigger: 'axis', textStyle: { fontSize: 11 } },
        series: [
          {
            name: '年化收益 %',
            type: 'bar',
            barMaxWidth: 40,
            data: result.layers.map((l) => ({
              value: l.annualized_ret,
              itemStyle: { color: l.annualized_ret >= 0 ? '#52c41a' : '#f5222d' },
            })),
          },
          {
            name: '累计收益',
            type: 'line',
            yAxisIndex: 1,
            data: result.layers.map((l) => Number((l.total_return * 100).toFixed(2))),
            lineStyle: { width: 2, color: '#faad14' },
            itemStyle: { color: '#faad14' },
          },
        ],
      })
    })
    return () => {
      instance.current?.dispose()
      instance.current = null
    }
  }, [result])

  return <div ref={chartRef} className="h-64 w-full" />
}

/* ── Page ────────────────────────────────────────────────────────── */
export function FactorResearch() {
  const [selected, setSelected] = useState<FactorMeta | null>(null)
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [tf, setTf] = useState('1h')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [forwardBars, setForwardBars] = useState(5)
  const [icWindow, setIcWindow] = useState(100)
  const [layerCount, setLayerCount] = useState(5)
  const [lookback, setLookback] = useState(120)
  const [limit, setLimit] = useState(1000)
  const [paramValues, setParamValues] = useState<Record<string, string>>({})
  const [values, setValues] = useState<FactorValuesResponse | null>(null)
  const [evaluation, setEvaluation] = useState<FactorEvaluation | null>(null)
  const [layers, setLayers] = useState<FactorLayersResult | null>(null)

  const factorsQuery = useQuery({
    queryKey: ['factors', 'list'],
    queryFn: () => factorApi.list(),
    staleTime: 10 * 60 * 1000,
  })

  const grouped = useMemo(() => {
    const map = new Map<string, FactorMeta[]>()
    for (const f of factorsQuery.data?.factors ?? []) {
      const list = map.get(f.category) ?? []
      list.push(f)
      map.set(f.category, list)
    }
    return map
  }, [factorsQuery.data])

  const selectFactor = useCallback((f: FactorMeta) => {
    setSelected(f)
    setEvaluation(null)
    setLayers(null)
    setValues(null)
    const defaults: Record<string, string> = {}
    for (const p of f.params ?? []) {
      if (p.default != null) defaults[p.name] = String(p.default)
    }
    setParamValues(defaults)
  }, [])

  const buildParams = useCallback((): Record<string, unknown> => {
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(paramValues)) {
      if (v === '') continue
      const schema = selected?.params?.find((p) => p.name === k)
      if (schema?.type === 'int') {
        const n = Number(v)
        if (!Number.isNaN(n)) out[k] = Math.round(n)
      } else if (schema?.type === 'float') {
        const n = Number(v)
        if (!Number.isNaN(n)) out[k] = n
      } else {
        out[k] = v
      }
    }
    return out
  }, [paramValues, selected])

  const valuesMutation = useMutation({
    mutationFn: () =>
      factorApi.values(selected!.name, {
        symbol,
        tf,
        limit,
        from: from || undefined,
        to: to || undefined,
        params: JSON.stringify(buildParams()),
      }),
    onSuccess: (data) => setValues(data),
    onError: (err: Error) => toast('error', `因子值计算失败: ${err.message}`),
  })

  const evaluateMutation = useMutation({
    mutationFn: () =>
      factorApi.evaluate({
        name: selected!.name,
        symbol,
        tf,
        from: from || undefined,
        to: to || undefined,
        limit,
        forward_bars: forwardBars,
        ic_window: icWindow,
        params: buildParams(),
        save: true,
      }),
    onSuccess: (data) => {
      setEvaluation(data.evaluation)
      toast('success', `评价完成：${data.evaluation.samples} 个样本`)
    },
    onError: (err: Error) => toast('error', `评价失败: ${err.message}`),
  })

  const layersMutation = useMutation({
    mutationFn: () =>
      factorApi.layers({
        name: selected!.name,
        symbol,
        tf,
        from: from || undefined,
        to: to || undefined,
        limit,
        forward_bars: forwardBars,
        layer_count: layerCount,
        lookback,
        params: buildParams(),
        save: true,
      }),
    onSuccess: (data) => setLayers(data.result),
    onError: (err: Error) => toast('error', `分层回测失败: ${err.message}`),
  })

  const evaluationsQuery = useQuery({
    queryKey: ['factors', 'evaluations'],
    queryFn: () => factorApi.evaluations(undefined, 50),
  })

  const busy = valuesMutation.isPending || evaluateMutation.isPending || layersMutation.isPending

  return (
    <div className="space-y-4 p-4">
      <PageHeader
        title="因子研究"
        subtitle="因子库 · IC/RankIC/ICIR 评价 · 分层回测（对标 QuantDinger 研究能力）"
        icon={<FlaskConical className="h-6 w-6" />}
      />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {/* ── 因子库列表 ── */}
        <SectionCard title={`因子库（${factorsQuery.data?.count ?? '…'}）`}>
          <div className="max-h-[520px] space-y-3 overflow-y-auto pr-1">
            {factorsQuery.isLoading && (
              <div className="flex items-center gap-2 text-sm text-[#888]"><Loader2 className="h-4 w-4 animate-spin" />加载中…</div>
            )}
            {[...grouped.entries()].map(([category, list]) => (
              <div key={category}>
                <div className="mb-1 text-xs font-medium uppercase tracking-wide text-[#666]">
                  {CATEGORY_LABELS[category] ?? category}（{list.length}）
                </div>
                <div className="space-y-1">
                  {list.map((f) => (
                    <button
                      key={f.name}
                      onClick={() => selectFactor(f)}
                      className={cn(
                        'w-full rounded-md border px-2 py-1.5 text-left text-sm transition-colors',
                        selected?.name === f.name
                          ? 'border-[#1890ff] bg-[#1890ff]/10 text-foreground'
                          : 'border-[#1c1c1c] bg-transparent text-[#bbb] hover:bg-[#1c1c1c]'
                      )}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-mono text-xs">{f.name}</span>
                        <span className="text-[10px] text-[#666]">v{f.version}{f.versions && f.versions.length > 1 ? ` (${f.versions.join('/')})` : ''}</span>
                      </div>
                      {f.description && <div className="mt-0.5 truncate text-[11px] text-[#777]">{f.description}</div>}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </SectionCard>

        {/* ── 配置与操作 ── */}
        <div className="space-y-4 lg:col-span-2">
          <SectionCard title={selected ? `因子：${selected.name}` : '选择一个因子'}>
            {!selected ? (
              <p className="py-6 text-center text-sm text-[#666]">从左侧因子库选择一个因子开始研究</p>
            ) : (
              <div className="space-y-3">
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  <label className="text-xs text-[#888]">
                    交易对
                    <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                  <label className="text-xs text-[#888]">
                    周期
                    <select value={tf} onChange={(e) => setTf(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm">
                      {INTERVAL_OPTIONS.map((o) => (
                        <option key={o.value} value={o.value}>{o.label}</option>
                      ))}
                    </select>
                  </label>
                  <label className="text-xs text-[#888]">
                    开始日期
                    <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                  <label className="text-xs text-[#888]">
                    结束日期
                    <input type="date" value={to} onChange={(e) => setTo(e.target.value)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                  <label className="text-xs text-[#888]">
                    未来收益 bar 数
                    <input type="number" min={1} max={100} value={forwardBars} onChange={(e) => setForwardBars(Number(e.target.value) || 1)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                  <label className="text-xs text-[#888]">
                    IC 滚动窗口
                    <input type="number" min={20} value={icWindow} onChange={(e) => setIcWindow(Number(e.target.value) || 100)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                  <label className="text-xs text-[#888]">
                    分层数
                    <input type="number" min={2} max={10} value={layerCount} onChange={(e) => setLayerCount(Number(e.target.value) || 5)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                  <label className="text-xs text-[#888]">
                    数据上限
                    <input type="number" min={100} max={1500} step={100} value={limit} onChange={(e) => setLimit(Number(e.target.value) || 1000)} className="mt-1 w-full rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm" />
                  </label>
                </div>

                {(selected.params?.length ?? 0) > 0 && (
                  <div className="flex flex-wrap gap-2">
                    {selected.params!.map((p) => (
                      <label key={p.name} className="text-xs text-[#888]" title={p.description}>
                        {p.name}
                        <input
                          value={paramValues[p.name] ?? ''}
                          onChange={(e) => setParamValues((prev) => ({ ...prev, [p.name]: e.target.value }))}
                          className="mt-1 w-24 rounded-md border border-[#1c1c1c] bg-[#111] px-2 py-1.5 text-sm"
                        />
                      </label>
                    ))}
                  </div>
                )}

                <div className="flex flex-wrap gap-2">
                  <button
                    onClick={() => valuesMutation.mutate()}
                    disabled={busy}
                    className="flex items-center gap-1.5 rounded-md border border-[#1c1c1c] bg-[#111] px-3 py-1.5 text-sm hover:bg-[#1c1c1c] disabled:opacity-50"
                  >
                    {valuesMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Activity className="h-3.5 w-3.5" />}
                    因子值
                  </button>
                  <button
                    onClick={() => evaluateMutation.mutate()}
                    disabled={busy}
                    className="flex items-center gap-1.5 rounded-md bg-[#1890ff] px-3 py-1.5 text-sm text-white hover:opacity-90 disabled:opacity-50"
                  >
                    {evaluateMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                    评价（IC/RankIC/ICIR）
                  </button>
                  <button
                    onClick={() => layersMutation.mutate()}
                    disabled={busy}
                    className="flex items-center gap-1.5 rounded-md border border-[#1c1c1c] bg-[#111] px-3 py-1.5 text-sm hover:bg-[#1c1c1c] disabled:opacity-50"
                  >
                    {layersMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Layers className="h-3.5 w-3.5" />}
                    分层回测
                  </button>
                </div>
              </div>
            )}
          </SectionCard>

          {/* ── 因子值 ── */}
          {values && (
            <SectionCard
              title={`因子值序列 · ${values.factor} v${values.version} · ${values.symbol} ${values.tf} · ${values.bars_used} bars（${values.source}）`}
             
            >
              <FactorValuesChart data={values} />
            </SectionCard>
          )}

          {/* ── IC 评价结果 ── */}
          {evaluation && (
            <SectionCard title="IC 评价结果">
              <KPIGrid
                items={[
                  { label: 'Overall IC', value: fmt(evaluation.overall_ic), icon: evaluation.overall_ic >= 0 ? <TrendingUp className="h-4 w-4" /> : <TrendingDown className="h-4 w-4" />, variant: Math.abs(evaluation.overall_ic) > 0.03 ? 'success' : 'default' },
                  { label: 'Overall RankIC', value: fmt(evaluation.overall_rank_ic), icon: <Sigma className="h-4 w-4" /> },
                  { label: 'IC 均值', value: fmt(evaluation.ic_mean), icon: <Activity className="h-4 w-4" /> },
                  { label: 'RankIC 均值', value: fmt(evaluation.rank_ic_mean), icon: <Activity className="h-4 w-4" /> },
                  { label: 'ICIR', value: fmt(evaluation.icir), icon: <TrendingUp className="h-4 w-4" />, variant: Math.abs(evaluation.icir) > 0.5 ? 'success' : 'default' },
                  { label: 'RankICIR', value: fmt(evaluation.rank_icir), icon: <TrendingUp className="h-4 w-4" /> },
                  { label: 'IC>0 占比 %', value: fmt(evaluation.ic_positive_pct, 1), icon: <Activity className="h-4 w-4" /> },
                  { label: '样本数', value: evaluation.samples, icon: <Database className="h-4 w-4" /> },
                ]}
              />
              <div className="mt-4">
                <ICSeriesChart series={evaluation.ic_series} />
              </div>
            </SectionCard>
          )}

          {/* ── 分层回测结果 ── */}
          {layers && (
            <SectionCard
              title={`分层回测 · ${layers.layer_count} 层 · 单调性 ${fmt(layers.monotonicity, 3)} · 多空累计 ${fmt(layers.long_short_total_return)}`}
             
            >
              <LayersBarChart result={layers} />
              <div className="mt-3 overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead>
                    <tr className="border-b border-[#1c1c1c] text-[#666]">
                      <th className="py-1.5 pr-3">层级</th>
                      <th className="py-1.5 pr-3">样本数</th>
                      <th className="py-1.5 pr-3">平均未来收益</th>
                      <th className="py-1.5 pr-3">累计收益</th>
                      <th className="py-1.5 pr-3">年化收益 %</th>
                    </tr>
                  </thead>
                  <tbody>
                    {layers.layers.map((l) => (
                      <tr key={l.layer} className="border-b border-[#141414]">
                        <td className="py-1.5 pr-3 font-mono">L{l.layer}</td>
                        <td className="py-1.5 pr-3">{l.count}</td>
                        <td className="py-1.5 pr-3">{fmt(l.avg_forward_ret, 6)}</td>
                        <td className={cn('py-1.5 pr-3', l.total_return >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>{fmt(l.total_return)}</td>
                        <td className={cn('py-1.5 pr-3', l.annualized_ret >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>{fmt(l.annualized_ret, 2)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </SectionCard>
          )}
        </div>
      </div>

      {/* ── 评价历史 ── */}
      <SectionCard title="评价历史">
        {evaluationsQuery.data?.evaluations?.length ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="border-b border-[#1c1c1c] text-[#666]">
                  <th className="py-1.5 pr-3">时间</th>
                  <th className="py-1.5 pr-3">类型</th>
                  <th className="py-1.5 pr-3">因子</th>
                  <th className="py-1.5 pr-3">交易对</th>
                  <th className="py-1.5 pr-3">周期</th>
                  <th className="py-1.5 pr-3">样本数</th>
                </tr>
              </thead>
              <tbody>
                {evaluationsQuery.data.evaluations.map((ev) => (
                  <tr key={ev.id} className="border-b border-[#141414]">
                    <td className="py-1.5 pr-3">{new Date(ev.created_at).toLocaleString()}</td>
                    <td className="py-1.5 pr-3 font-mono">{ev.kind}</td>
                    <td className="py-1.5 pr-3 font-mono">{ev.factor_name} v{ev.factor_version}</td>
                    <td className="py-1.5 pr-3 font-mono">{ev.symbol}</td>
                    <td className="py-1.5 pr-3 font-mono">{ev.tf}</td>
                    <td className="py-1.5 pr-3">{ev.samples}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="py-3 text-center text-xs text-[#666]">暂无评价记录</p>
        )}
      </SectionCard>
    </div>
  )
}

export default FactorResearch
