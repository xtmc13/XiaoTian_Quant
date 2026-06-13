import React, { useRef, useEffect } from 'react'
import { getEcharts } from '@/lib/echarts'
import type { EChartsType } from 'echarts'
import { cn, formatCurrency } from '@/lib/utils'

export interface EquityPoint {
  time: number
  equity: number
}

export interface TradeRecord {
  side: 'buy' | 'sell'
  entry_price?: number
  exit_price?: number
  qty: number
  pnl?: number
  time: number
  bar: number
}

export interface PerformanceChartProps {
  data?: EquityPoint[]
  trades?: TradeRecord[]
  benchmarkData?: { time: number; value: number }[]
  isLoading?: boolean
  className?: string
  height?: number
}

export const PerformanceChart = React.memo(function PerformanceChart({
  data,
  trades,
  benchmarkData,
  isLoading,
  className,
  height = 320,
}: PerformanceChartProps) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)
  const hasBenchmark = benchmarkData && benchmarkData.length > 0

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      const legendData = hasBenchmark ? ['策略权益', '买入持有', '回撤'] : ['权益', '买入', '卖出', '回撤']
      const showTrades = !hasBenchmark
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        legend: hasBenchmark ? {
          data: legendData,
          bottom: 0,
          textStyle: { color: '#888', fontSize: 10 },
          itemWidth: 12,
          itemHeight: 8,
        } : undefined,
        grid: [
          { left: 56, right: 16, top: 16, bottom: hasBenchmark ? 36 : 60 },
          { left: 56, right: 16, top: '70%', bottom: 24 },
        ],
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          axisPointer: { type: 'cross', link: [{ xAxisIndex: 'all' }] },
        },
        xAxis: [
          { type: 'time', axisLabel: { fontSize: 10, color: '#555555' }, axisLine: { lineStyle: { color: '#1c1c1c' } }, splitLine: { show: false } },
          { type: 'time', gridIndex: 1, axisLabel: { fontSize: 9, color: '#555555' }, axisLine: { lineStyle: { color: '#1c1c1c' } }, splitLine: { show: false } },
        ],
        yAxis: [
          { type: 'value', axisLabel: { fontSize: 10, color: '#555555', formatter: (v: number) => `$${formatCurrency(v)}` }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
          { type: 'value', gridIndex: 1, axisLabel: { fontSize: 9, color: '#555555', formatter: '{value}%' }, splitLine: { show: false }, min: -100, max: 0 },
        ],
        series: [
          { name: '策略权益', type: 'line', data: [], smooth: true, symbol: 'none', lineStyle: { color: '#03A66D', width: 2 },
            areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(3,166,109,0.12)' }, { offset: 1, color: 'rgba(3,166,109,0)' }] } } },
          { name: '买入持有', type: 'line', data: [], smooth: true, symbol: 'none', lineStyle: { color: '#60a5fa', width: 1.5, type: 'dashed', opacity: 0.7 } },
          ...(showTrades ? [
            { name: '买入', type: 'scatter', data: [], symbol: 'triangle', symbolSize: 8, itemStyle: { color: '#03A66D' }, z: 10 },
            { name: '卖出', type: 'scatter', data: [], symbol: 'triangle', symbolSize: 8, symbolRotate: 180, itemStyle: { color: '#CF304A' }, z: 10 },
          ] as any[] : []),
          { name: '回撤', type: 'line', data: [], smooth: true, symbol: 'none', lineStyle: { color: '#ef4444', width: 1, opacity: 0.6 }, xAxisIndex: 1, yAxisIndex: 1,
            areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(239,68,68,0.15)' }, { offset: 1, color: 'rgba(239,68,68,0)' }] } } },
        ],
      })
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => { disposed = true; chartRef.current?.dispose() }
  }, [hasBenchmark])

  useEffect(() => {
    if (!chartRef.current || !data) return
    const equityData = data.map((d) => [d.time, d.equity])
    let peak = data[0]?.equity ?? 0
    const drawdownData = data.map(p => {
      if (p.equity > peak) peak = p.equity
      return [p.time, peak > 0 ? ((p.equity - peak) / peak) * 100 : 0]
    })

    if (hasBenchmark) {
      chartRef.current.setOption({
        series: [
          { data: equityData },
          { data: benchmarkData!.map(d => [d.time, d.value]) },
          { data: drawdownData },
        ],
      })
    } else {
      const buyData = trades?.filter(t => t.side === 'buy').map(t => [t.time, t.entry_price ?? 0]) ?? []
      const sellData = trades?.filter(t => t.side === 'sell').map(t => [t.time, t.exit_price ?? 0]) ?? []
      chartRef.current.setOption({
        series: [
          { data: equityData },
          { data: [] },
          { data: buyData },
          { data: sellData },
          { data: drawdownData },
        ],
      })
    }
  }, [data, trades, benchmarkData, hasBenchmark])

  if (isLoading) return <div className={cn('animate-pulse rounded-lg bg-quant-bg-secondary', className)} style={{ height }} />
  return <div ref={ref} className={cn('w-full', className)} style={{ height }} />
})
