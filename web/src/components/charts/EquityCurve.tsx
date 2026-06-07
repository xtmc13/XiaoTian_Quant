/**
 * EquityCurve — Simple SVG line chart for backtest equity curve.
 */
import { useMemo } from 'react'
import { cn } from '@/lib/utils'

interface EquityPoint {
  timestamp: number
  equity: number
}

interface EquityCurveProps {
  data: EquityPoint[]
  className?: string
  height?: number
}

export function EquityCurve({ data, className, height = 160 }: EquityCurveProps) {
  const padding = { top: 10, right: 10, bottom: 24, left: 50 }
  const width = 600
  const chartW = width - padding.left - padding.right
  const chartH = height - padding.top - padding.bottom

  const hasData = data && data.length >= 2

  const minEquity = hasData ? Math.min(...data.map(d => d.equity)) : 0
  const maxEquity = hasData ? Math.max(...data.map(d => d.equity)) : 0
  const equityRange = maxEquity - minEquity || 1

  const minTime = hasData ? data[0].timestamp : 0
  const maxTime = hasData ? data[data.length - 1].timestamp : 0
  const timeRange = maxTime - minTime || 1

  const toX = (t: number) => padding.left + ((t - minTime) / timeRange) * chartW
  const toY = (e: number) => padding.top + chartH - ((e - minEquity) / equityRange) * chartH

  const pathD = useMemo(() => {
    if (!hasData) return ''
    return data.map((d, i) => `${i === 0 ? 'M' : 'L'} ${toX(d.timestamp)} ${toY(d.equity)}`).join(' ')
  }, [data, hasData])

  const areaD = useMemo(() => {
    if (!hasData) return ''
    const firstX = toX(data[0].timestamp)
    const lastX = toX(data[data.length - 1].timestamp)
    const baseY = toY(minEquity)
    return `${pathD} L ${lastX} ${baseY} L ${firstX} ${baseY} Z`
  }, [data, pathD, hasData])

  if (!hasData) {
    return (
      <div className={cn('flex items-center justify-center text-xs text-muted-foreground', className)} style={{ height }}>
        无权益曲线数据
      </div>
    )
  }

  // Y-axis ticks
  const yTicks = 4
  const yTickValues = Array.from({ length: yTicks + 1 }, (_, i) => minEquity + (equityRange * i) / yTicks)

  // X-axis date labels
  const xTicks = 3
  const xTickIndices = Array.from({ length: xTicks + 1 }, (_, i) => Math.floor((data.length - 1) * i / xTicks))

  const isPositive = data[data.length - 1].equity >= data[0].equity
  const strokeColor = isPositive ? '#00E676' : '#FF5252'
  const fillColor = isPositive ? 'rgba(0,230,118,0.08)' : 'rgba(255,82,82,0.08)'

  return (
    <div className={cn('w-full', className)}>
      <svg viewBox={`0 0 ${width} ${height}`} className="w-full" style={{ height }} preserveAspectRatio="none">
        {/* Grid lines */}
        {yTickValues.map((v, i) => (
          <line key={`gy-${i}`} x1={padding.left} y1={toY(v)} x2={width - padding.right} y2={toY(v)} stroke="rgba(255,255,255,0.06)" strokeWidth={0.5} />
        ))}

        {/* Area fill */}
        <path d={areaD} fill={fillColor} />

        {/* Line */}
        <path d={pathD} fill="none" stroke={strokeColor} strokeWidth={1.5} strokeLinejoin="round" />

        {/* Y-axis labels */}
        {yTickValues.map((v, i) => (
          <text key={`y-${i}`} x={padding.left - 6} y={toY(v) + 3} textAnchor="end" fill="#888" fontSize={9} fontFamily="monospace">
            {v.toFixed(0)}
          </text>
        ))}

        {/* X-axis labels */}
        {xTickIndices.map((idx, i) => {
          const d = data[idx]
          const date = new Date(d.timestamp).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
          return (
            <text key={`x-${i}`} x={toX(d.timestamp)} y={height - 4} textAnchor="middle" fill="#888" fontSize={9}>
              {date}
            </text>
          )
        })}
      </svg>
    </div>
  )
}
