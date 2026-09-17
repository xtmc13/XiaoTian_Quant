import { useQuery } from '@tanstack/react-query'
import { Activity, Layers, TrendingUp, TrendingDown, Crosshair, Target, Timer } from 'lucide-react'
import { strategyApi } from '@/lib/api'
import type { StrategyItem, StrategyRuntimeResponse, StrategyRuntimeStatus } from '@/types'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'

const POLL_MS = 5000

function fmtPrice(v?: number): string {
  if (!v || !isFinite(v)) return '-'
  const digits = v >= 1000 ? 2 : v >= 10 ? 3 : v >= 1 ? 4 : 6
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: digits })
}

function fmtPct(v?: number): string {
  if (v == null || !isFinite(v)) return '-'
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
}

/** 在仓时长：策略 updated_at（启动时会刷新）近似启动时间。 */
function holdingDuration(startedAt?: string): string {
  if (!startedAt) return '-'
  const ts = Date.parse(startedAt)
  if (!isFinite(ts)) return '-'
  const ms = Date.now() - ts
  if (ms < 0) return '-'
  const mins = Math.floor(ms / 60000)
  if (mins < 1) return '刚启动'
  if (mins < 60) return `${mins} 分钟`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} 小时 ${mins % 60} 分`
  const days = Math.floor(hours / 24)
  return `${days} 天 ${hours % 24} 小时`
}

function PanelStat({
  label,
  value,
  valueColor,
  hint,
}: {
  label: string
  value: string
  valueColor?: string
  hint?: string
}) {
  return (
    <div className="p-3 rounded-lg bg-quant-bg border border-quant-border min-w-0">
      <div className="text-[10px] text-muted-foreground truncate">{label}</div>
      <div className={cn('text-sm font-bold font-mono truncate', valueColor || 'text-foreground')}>{value}</div>
      {hint && <div className="text-[9px] text-muted-foreground mt-0.5">{hint}</div>}
    </div>
  )
}

/**
 * 运行中策略实时面板（P0-1）：5s 轮询 runtime 接口，展示持仓/现价/浮盈/
 * 加仓阶梯进度/下一档与止盈距离。未运行时由父组件渲染 EmptyState。
 */
export function RuntimePanel({ strategy }: { strategy: StrategyItem }) {
  const { data, isLoading, isError } = useQuery<StrategyRuntimeResponse>({
    queryKey: ['strategy-runtime', strategy.id],
    queryFn: () => strategyApi.runtime(strategy.id),
    refetchInterval: POLL_MS,
    enabled: strategy.status === 'running',
  })

  if (strategy.status !== 'running') {
    return (
      <EmptyState
        icon={<Activity className="w-6 h-6" />}
        title="启动后展示实时运行状态"
        description="策略启动后，这里将显示持仓、现价、浮盈与加仓阶梯进度"
      />
    )
  }
  if (isLoading && !data) {
    return <div className="text-xs text-muted-foreground py-6 text-center">加载运行状态...</div>
  }
  if (isError || !data) {
    return (
      <EmptyState
        icon={<Activity className="w-6 h-6" />}
        title="运行状态暂不可用"
        description="引擎未返回该策略的运行状态（可能尚未注册或类型不支持）"
      />
    )
  }

  const st: StrategyRuntimeStatus | null = data.status
  if (!st) {
    return (
      <EmptyState
        icon={<Activity className="w-6 h-6" />}
        title="暂无运行详情"
        description="引擎中未找到该策略的运行状态（仅 MACD/CRA 系列策略提供）"
      />
    )
  }

  const price = data.price > 0 ? data.price : undefined
  const avg = st.avg_entry_price || st.entry_price
  const inPos = !!st.in_position
  const isShort = st.direction === 'short' || st.direction === 'SHORT'
  const isContract = strategy.market_type !== 'spot' && strategy.category !== 'spot'

  // 浮盈%：现价 vs 均价；合约仓位盈亏按杠杆放大。
  let pnlPct: number | null = null
  if (inPos && price && avg && avg > 0) {
    pnlPct = ((price - avg) / avg) * 100
    if (isShort) pnlPct = -pnlPct
  }

  const totalTiers = st.total_add_tiers ?? 0
  const triggered = st.add_positions_triggered ?? 0
  const ladderPct = totalTiers > 0 ? Math.min(100, (triggered / totalTiers) * 100) : 0

  const dirLabel = !inPos
    ? '空仓'
    : isShort
      ? '做空'
      : '做多'

  return (
    <div className="space-y-3">
      {/* KPI 行 */}
      <div className="grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-6 gap-2">
        <PanelStat
          label="持仓方向"
          value={dirLabel}
          valueColor={!inPos ? 'text-muted-foreground' : isShort ? 'text-quant-red' : 'text-quant-green'}
        />
        <PanelStat
          label="持仓数量"
          value={
            inPos
              ? (st.position_qty ?? st.quantity ?? 0).toLocaleString('en-US', { maximumFractionDigits: 6 })
              : '-'
          }
        />
        <PanelStat label="持仓均价" value={inPos && avg ? fmtPrice(avg) : '-'} />
        <PanelStat label="现价" value={price ? fmtPrice(price) : '获取中...'} valueColor="text-quant-gold" />
        <PanelStat
          label="浮动盈亏"
          value={pnlPct != null ? fmtPct(pnlPct) : '-'}
          valueColor={pnlPct == null ? undefined : pnlPct >= 0 ? 'text-quant-green' : 'text-quant-red'}
          hint={isContract && pnlPct != null ? '按杠杆放大' : undefined}
        />
        <PanelStat label="在仓时长" value={inPos ? holdingDuration(strategy.updated_at) : '-'} />
      </div>

      {/* 加仓阶梯进度 + 距离 */}
      <div className="rounded-lg bg-quant-bg border border-quant-border p-3 space-y-2">
        <div className="flex items-center justify-between text-[11px]">
          <span className="text-muted-foreground flex items-center gap-1.5">
            <Layers className="w-3 h-3 text-quant-gold" />
            加仓阶梯
          </span>
          <span className="text-foreground font-mono">
            {inPos ? `${triggered}/${totalTiers} 档` : '未持仓'}
          </span>
        </div>
        <div className="h-2 rounded-full bg-quant-bg-tertiary overflow-hidden">
          <div
            className={cn('h-full rounded-full transition-all', triggered > 0 ? 'bg-quant-gold' : 'bg-transparent')}
            style={{ width: `${ladderPct}%` }}
          />
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 pt-1">
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <Crosshair className="w-3 h-3 text-quant-gold" />
            下一档补仓距离
            <span className="ml-auto font-mono text-foreground">
              {data.next_add_distance_pct != null ? `${data.next_add_distance_pct.toFixed(2)}%` : '-'}
            </span>
          </div>
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <Target className="w-3 h-3 text-quant-gold" />
            止盈距离
            <span className="ml-auto font-mono text-foreground">
              {data.take_profit_distance_pct != null ? `${data.take_profit_distance_pct.toFixed(2)}%` : '-'}
            </span>
          </div>
        </div>
        {st.tp_mode === 'moving' && (
          <div className="text-[10px] text-muted-foreground">移动止盈模式无固定目标价，按回撤档位触发</div>
        )}
      </div>

      {/* 底部信息行：最近信号 + 已收 K 线 */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[10px] text-muted-foreground">
        <span className="inline-flex items-center gap-1">
          <Activity className="w-3 h-3" />
          已收集 K 线 <span className="text-foreground font-mono">{st.bars_collected ?? 0}</span> 根
        </span>
        {st.last_signal_time ? (
          <span className="inline-flex items-center gap-1">
            {st.last_signal_direction === 'CLOSE' ? (
              <TrendingDown className="w-3 h-3 text-quant-red" />
            ) : (
              <TrendingUp className="w-3 h-3 text-quant-green" />
            )}
            最近信号{' '}
            <span className="text-foreground">{st.last_signal_direction || '-'}</span> ·{' '}
            {new Date(st.last_signal_time).toLocaleTimeString('zh-CN', { hour12: false })}
          </span>
        ) : (
          <span className="inline-flex items-center gap-1">
            <Timer className="w-3 h-3" /> 暂无信号
          </span>
        )}
        {st.loops_executed != null && st.loops_executed > 0 && (
          <span>
            已完成循环 <span className="text-foreground font-mono">{st.loops_executed}</span> 次
          </span>
        )}
      </div>
    </div>
  )
}

export default RuntimePanel
