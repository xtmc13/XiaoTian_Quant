import React, { useRef, useEffect, useMemo } from 'react'
import { getEcharts } from '@/lib/echarts'
import type { EChartsType } from 'echarts'
import { cn } from '@/lib/utils'

export interface DepthChartProps {
  bids?: [number, number][]
  asks?: [number, number][]
  lastPrice?: number
  className?: string
}

export const DepthChart = React.memo(function DepthChart({
  bids = [],
  asks = [],
  lastPrice = 0,
  className,
}: DepthChartProps) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)

  const { bidData, askData, midPrice } = useMemo(() => {
    if (!bids.length && !asks.length) return { bidData: [], askData: [], midPrice: lastPrice }

    // Sort bids descending by price, accumulate quantity
    const sortedBids = [...bids].sort((a, b) => b[0] - a[0])
    let bidAccum = 0
    const bidSeries: [number, number][] = []
    for (const [price, qty] of sortedBids) {
      bidAccum += qty
      bidSeries.push([price, bidAccum])
    }

    // Sort asks ascending by price, accumulate quantity
    const sortedAsks = [...asks].sort((a, b) => a[0] - b[0])
    let askAccum = 0
    const askSeries: [number, number][] = []
    for (const [price, qty] of sortedAsks) {
      askAccum += qty
      askSeries.push([price, askAccum])
    }

    const mid = lastPrice || (sortedBids[0]?.[0] + sortedAsks[0]?.[0]) / 2 || 0
    return { bidData: bidSeries.reverse(), askData: askSeries, midPrice: mid }
  }, [bids, asks, lastPrice])

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 8, right: 8, top: 8, bottom: 8 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          formatter: (params: unknown) => {
            const p = params as Array<{ seriesName: string; value: [number, number] }>
            if (!p?.length) return ''
            const v = p[0].value
            return `<div style="font-size:11px">价格: <b>${v[0].toFixed(2)}</b><br/>累计: <b>${v[1].toFixed(4)}</b></div>`
          },
        },
        xAxis: {
          type: 'value',
          min: (value: { min: number }) => value.min * 0.9995,
          max: (value: { max: number }) => value.max * 1.0005,
          axisLabel: { show: false },
          axisLine: { show: false },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          axisLabel: { show: false },
          axisLine: { show: false },
          splitLine: { show: false },
        },
        series: [
          {
            name: '买盘深度',
            type: 'line',
            data: [],
            smooth: false,
            symbol: 'none',
            lineStyle: { width: 0 },
            areaStyle: { color: 'rgba(14,203,129,0.25)' },
            step: 'end',
          },
          {
            name: '卖盘深度',
            type: 'line',
            data: [],
            smooth: false,
            symbol: 'none',
            lineStyle: { width: 0 },
            areaStyle: { color: 'rgba(246,70,93,0.25)' },
            step: 'end',
          },
        ],
      })
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => { disposed = true; chartRef.current?.dispose() }
  }, [])

  useEffect(() => {
    if (!chartRef.current) return
    chartRef.current.setOption({
      series: [
        { data: bidData },
        { data: askData },
      ],
    })
  }, [bidData, askData])

  if (!bids.length && !asks.length) {
    return (
      <div className={cn('h-32 flex items-center justify-center text-xs text-muted-foreground', className)}>
        等待深度数据...
      </div>
    )
  }

  return <div ref={ref} className={cn('h-32 w-full', className)} />
})
