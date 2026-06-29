import { useEffect, useRef, useCallback, useMemo } from 'react'
import { KLineChartPro } from '@klinecharts/pro'
import '@klinecharts/pro/dist/klinecharts-pro.css'
import { createBackendDatafeed, setChartUpdater, clearChartUpdater } from '@/lib/klineDatafeed'
import { TRADING_INTERVALS } from '@/lib/constants'
import { parseInterval } from '@/lib/tradingHelpers'
import type { ChartApi } from '@/lib/tradingHelpers'

export interface UseTradingChartOptions {
  symbol: string
  interval: string
  setInterval: (i: string) => void
  bottomCollapsed: boolean
}

export function useTradingChart({ symbol, interval, setInterval, bottomCollapsed }: UseTradingChartOptions) {
  const chartRef = useRef<HTMLDivElement>(null)
  const chartApiRef = useRef<ChartApi | null>(null)
  const klineProRef = useRef<unknown>(null)
  const datafeed = useMemo(() => createBackendDatafeed(), [])

  const initChart = useCallback(() => {
    const container = chartRef.current
    if (!container) return

    if (klineProRef.current) {
      container.innerHTML = ''
      klineProRef.current = null
      chartApiRef.current = null
    }

    let intervalId: number | null = null
    try {
      const chart = new KLineChartPro({
        container,
        symbol: {
          ticker: symbol,
          name: symbol.replace('USDT', '/USDT'),
          shortName: symbol,
          market: 'crypto',
          exchange: 'BINANCE',
        },
        period: { ...parseInterval(interval), text: interval },
        periods: TRADING_INTERVALS.map((i) => ({ ...parseInterval(i), text: i })),
        datafeed,
        drawingBarVisible: true,
        mainIndicators: ['MA', 'EMA'],
        subIndicators: ['VOL', 'MACD'],
        theme: 'dark',
        locale: 'zh-CN',
      })
      klineProRef.current = chart

      const checkApi = () => {
        const chartApi = (chart as unknown as { _chartApi?: unknown })._chartApi as ChartApi | undefined
        if (chartApi) {
          chartApiRef.current = chartApi
          try {
            chartApi.scrollToRealTime()
          } catch {
            /* ignore */
          }
          try {
            chartApi.setBarSpace(4)
          } catch {
            /* ignore */
          }
          if (typeof chartApi.updateData === 'function') {
            setChartUpdater((bar) => {
              try {
                chartApi.updateData(bar)
              } catch {
                /* ignore */
              }
            })
          }
        } else {
          intervalId = window.setTimeout(checkApi, 100)
        }
      }
      checkApi()
    } catch {
      /* ignore init error */
    }

    return () => {
      if (intervalId) window.clearTimeout(intervalId)
    }
  }, [symbol, interval, datafeed])

  useEffect(() => {
    const el = chartRef.current
    const cancelTimer = initChart()
    return () => {
      cancelTimer?.()
      clearChartUpdater()
      if (el) el.innerHTML = ''
      klineProRef.current = null
      chartApiRef.current = null
    }
  }, [initChart])

  useEffect(() => {
    const el = chartRef.current
    if (!el) return
    const handleClick = (e: MouseEvent) => {
      let t = e.target as HTMLElement | null
      while (t && t !== el) {
        if (t.classList?.contains('period') && t.parentElement?.classList?.contains('klinecharts-pro-period-bar')) {
          const txt = t.textContent?.trim()
          if (txt && TRADING_INTERVALS.includes(txt as (typeof TRADING_INTERVALS)[number])) {
            setInterval(txt)
          }
          return
        }
        t = t.parentElement
      }
    }
    el.addEventListener('click', handleClick, true)
    return () => el.removeEventListener('click', handleClick, true)
  }, [setInterval])

  useEffect(() => {
    if (!klineProRef.current) return
    const t = setTimeout(() => window.dispatchEvent(new Event('resize')), 100)
    return () => clearTimeout(t)
  }, [bottomCollapsed])

  return { chartRef }
}
