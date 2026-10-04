import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity, TrendingUp, TrendingDown, CircleDollarSign, Target } from 'lucide-react'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'

/** 后端 analysis.Result 的字段口径。 */
interface Evidence {
  kind: string
  label: string
  bullish: boolean
  weight: number
  bar_time: number
  price: number
}
interface Segment {
  phase: string
  from: number
  to: number
  confidence: number
}
interface SmartMoneyResult {
  symbol: string
  timeframe: string
  phase: string
  confidence: number
  evidence: Evidence[]
  segments: Segment[]
  levels: Record<string, number>
  signal?: { direction: string; reason: string; strength: number } | null
  bar_count: number
  updated_at: number
}

const PHASES: Record<string, { label: string; color: string; desc: string }> = {
  none: { label: '观望', color: '#8A8F98', desc: '无明确结构，等待主力留下痕迹' },
  building: { label: '建仓', color: '#3B82F6', desc: '恐慌抛售高潮后，主力开始试探性承接' },
  accumulation: { label: '吸筹', color: '#06B6D4', desc: '区间震荡+资金持续流入，筹码向主力集中' },
  shakeout: { label: '洗盘', color: '#F59E0B', desc: '假跌破猎杀止损，弱手出局后快速收回' },
  markup: { label: '拉升', color: '#0ECB81', desc: '放量突破吸筹区间，进入抬价阶段' },
  distribution: { label: '出货', color: '#F6465D', desc: '顶部高潮+量价背离，筹码派发给追涨盘' },
}
const TIMEFRAMES = ['15m', '30m', '1h', '4h', '1d']
const SYMBOLS = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'BNBUSDT', 'DOGEUSDT', 'XRPUSDT']

function fmtTs(ms: number): string {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
function fmtPrice(v?: number): string {
  if (!v || !isFinite(v)) return '-'
  return v.toLocaleString('en-US', { maximumFractionDigits: v >= 1000 ? 0 : 2 })
}

/** 主力行为全解：建仓/吸筹/洗盘/拉升/出货 五段状态机分析页。 */
export function SmartMoneyPage() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [tf, setTf] = useState('4h')

  const { data, isFetching, error, refetch } = useQuery<SmartMoneyResult>({
    queryKey: ['smart-money', symbol, tf],
    queryFn: () => api.get(`/analysis/smart-money?symbol=${symbol}&timeframe=${tf}`),
    refetchInterval: 60_000,
  })

  const phase = data ? (PHASES[data.phase] ?? PHASES.none) : null
  const segs = data?.segments ?? []

  return (
    <div className="space-y-4">
      <PageHeader title="主力行为全解" subtitle="建仓 · 吸筹 · 洗盘 · 拉升 · 出货 — Wyckoff 量价状态机" />

      {/* 查询条 */}
      <div className="flex flex-wrap items-center gap-2">
        <select value={symbol} onChange={(e) => setSymbol(e.target.value)} className={selCls}>
          {SYMBOLS.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <div className="flex gap-1">
          {TIMEFRAMES.map((t) => (
            <button
              key={t}
              onClick={() => setTf(t)}
              className={cn(
                'px-2.5 py-1.5 rounded-lg text-xs border transition-colors',
                tf === t ? 'border-quant-gold bg-quant-gold/10 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground'
              )}
            >
              {t}
            </button>
          ))}
        </div>
        <button onClick={() => refetch()} className={cn(selCls, 'hover:text-foreground')}>
          {isFetching ? '分析中…' : '重新分析'}
        </button>
        {data && (
          <span className="text-[10px] text-muted-foreground ml-auto">
            {data.bar_count} 根K线 · 更新于 {fmtTs(data.updated_at)}
          </span>
        )}
      </div>

      {error && (
        <div className="rounded-lg border border-quant-red/40 bg-quant-red/10 p-3 text-xs text-quant-red">
          分析失败：{(error as Error).message}
        </div>
      )}

      {data && phase && (
        <>
          {/* 当前阶段大徽章 */}
          <div className="rounded-xl border border-quant-border bg-quant-bg p-5 flex flex-wrap items-center gap-5">
            <div
              className="w-24 h-24 rounded-2xl flex flex-col items-center justify-center shrink-0"
              style={{ backgroundColor: `${phase.color}1A`, border: `2px solid ${phase.color}` }}
            >
              <span className="text-2xl font-bold" style={{ color: phase.color }}>{phase.label}</span>
              <span className="text-[10px] text-muted-foreground mt-0.5">{data.confidence}% 置信</span>
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-sm text-foreground mb-1">{phase.desc}</div>
              <div className="h-2 rounded-full bg-quant-bg-tertiary overflow-hidden max-w-md">
                <div className="h-full rounded-full transition-all" style={{ width: `${data.confidence}%`, backgroundColor: phase.color }} />
              </div>
              <div className="flex flex-wrap gap-x-4 gap-y-1 mt-3 text-xs text-muted-foreground">
                <span>区间高 <b className="text-foreground font-mono">{fmtPrice(data.levels.range_high)}</b></span>
                <span>区间低 <b className="text-foreground font-mono">{fmtPrice(data.levels.range_low)}</b></span>
                {data.levels.spring_low > 0 && (
                  <span>弹簧位 <b className="text-quant-gold font-mono">{fmtPrice(data.levels.spring_low)}</b></span>
                )}
                {data.levels.bc_high > 0 && (
                  <span>高潮位 <b className="text-quant-red font-mono">{fmtPrice(data.levels.bc_high)}</b></span>
                )}
                <span>现价 <b className="text-quant-gold font-mono">{fmtPrice(data.levels.current)}</b></span>
              </div>
            </div>
            {data.signal && (
              <div
                className={cn(
                  'px-4 py-3 rounded-xl border text-xs max-w-xs',
                  data.signal.direction === 'SHORT'
                    ? 'border-quant-red/40 bg-quant-red/10 text-quant-red'
                    : 'border-[#0ECB81]/40 bg-[#0ECB81]/10 text-[#0ECB81]'
                )}
              >
                <div className="font-bold mb-1 flex items-center gap-1">
                  {data.signal.direction === 'SHORT' ? <TrendingDown className="w-3.5 h-3.5" /> : <TrendingUp className="w-3.5 h-3.5" />}
                  转换信号：{data.signal.direction}
                </div>
                {data.signal.reason}
              </div>
            )}
          </div>

          {/* 阶段时间轴 */}
          <div className="rounded-lg border border-quant-border bg-quant-bg p-3">
            <div className="text-[11px] text-muted-foreground mb-2 flex items-center gap-1.5">
              <Activity className="w-3 h-3 text-quant-gold" /> 阶段时间轴（{tf}）
            </div>
            <div className="flex h-8 rounded-lg overflow-hidden border border-quant-border/50">
              {segs.map((s, i) => {
                const meta = PHASES[s.phase] ?? PHASES.none
                const span = Math.max(1, s.to - s.from)
                return (
                  <div
                    key={i}
                    title={`${meta.label} ${fmtTs(s.from)} → ${fmtTs(s.to)}（${Math.round(s.confidence)}%）`}
                    className="h-full flex items-center justify-center text-[9px] text-white/90 font-medium overflow-hidden whitespace-nowrap"
                    style={{ flexGrow: span, flexBasis: 0, backgroundColor: meta.color, opacity: 0.45 + (s.confidence / 200) }}
                  >
                    {span > 3.6e6 ? meta.label : ''}
                  </div>
                )
              })}
            </div>
            <div className="flex justify-between text-[9px] text-muted-foreground mt-1">
              <span>{segs.length > 0 ? fmtTs(segs[0].from) : '-'}</span>
              <span>{segs.length > 0 ? fmtTs(segs[segs.length - 1].to) : '-'}</span>
            </div>
          </div>

          {/* 证据链 */}
          <div className="rounded-lg border border-quant-border bg-quant-bg p-3">
            <div className="text-[11px] text-muted-foreground mb-2 flex items-center gap-1.5">
              <Target className="w-3 h-3 text-quant-gold" /> 证据链（按权重排序）
            </div>
            {data.evidence.length === 0 ? (
              <div className="text-xs text-muted-foreground py-2">暂无有效证据——结构不明时宁可空判，不硬编故事。</div>
            ) : (
              <ul className="space-y-1.5">
                {data.evidence.map((e, i) => (
                  <li key={i} className="flex items-start gap-2 text-xs">
                    {e.bullish ? (
                      <TrendingUp className="w-3.5 h-3.5 text-[#0ECB81] mt-0.5 shrink-0" />
                    ) : (
                      <TrendingDown className="w-3.5 h-3.5 text-quant-red mt-0.5 shrink-0" />
                    )}
                    <span className="text-foreground">{e.label}</span>
                    <span className="text-muted-foreground font-mono ml-auto shrink-0">{fmtTs(e.bar_time)}</span>
                  </li>
                ))}
              </ul>
            )}
            <div className="text-[10px] text-muted-foreground mt-3 flex items-center gap-1.5 border-t border-quant-border/50 pt-2">
              <CircleDollarSign className="w-3 h-3" />
              想自动交易这个阶段机器？创建机器人时选择「主力行为」策略类型即可挂 paper 跑。
            </div>
          </div>
        </>
      )}
    </div>
  )
}

const selCls =
  'bg-quant-bg border border-quant-border rounded-lg px-3 py-1.5 text-xs focus:outline-none focus:border-quant-gold text-foreground'

export default SmartMoneyPage
