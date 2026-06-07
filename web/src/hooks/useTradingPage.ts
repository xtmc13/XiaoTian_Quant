import { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { marketApi, orderApi, portfolioApi, tradesApi } from '@/lib/api'
import { KLineChartPro } from '@klinecharts/pro'
import {
  createBackendDatafeed,
  handlePriceTick,
  runBackfill,
  setChartUpdater,
  clearChartUpdater,
} from '@/lib/klineDatafeed'
import { TRADING_INTERVALS } from '@/lib/constants'
import { parseInterval } from '@/lib/tradingHelpers'
import { useWebSocket } from '@/hooks/useWebSocket'
import type { ChartApi } from '@/lib/tradingHelpers'
import type { TickerSnapshot } from '@/types'
import type { Trade, Order } from '@/types'

export interface UseTradingPageOptions {
  defaultSymbol?: string
  defaultInterval?: string
}

export interface UseTradingPageReturn {
  // State
  symbol: string
  setSymbol: (s: string) => void
  interval: string
  setInterval: (i: string) => void
  liveTrades: Array<{
    id: string
    price: number
    quantity: number
    side: 'buy' | 'sell'
    time: number
  }>

  // Chart refs
  chartRef: React.RefObject<HTMLDivElement | null>

  // Queries
  klines: unknown
  orderbook: { bids?: [number, number][]; asks?: [number, number][] } | undefined
  obLoading: boolean
  recentTrades: Trade[] | undefined
  orders: Order[] | undefined
  ordersLoading: boolean
  historyOrders: Order[] | undefined
  historyLoading: boolean
  fillTrades: Trade[] | undefined
  fillsLoading: boolean
  portfolio: unknown
  snapshot: { price?: number } | undefined

  // Computed
  lastPrice: number
  prevClose: number
  change: number
  changePct: number
  isUp: boolean
  bestBid: string
  bestAsk: string
  obMax: number
  displayTrades: Trade[]

  // Actions
  handleCancelOrder: (id: string) => Promise<void>
  initChart: () => (() => void) | undefined
}

export function useTradingPage(options: UseTradingPageOptions = {}): UseTradingPageReturn {
  const { defaultSymbol = 'BTCUSDT', defaultInterval = '1h' } = options
  const [symbol, setSymbol] = useState(defaultSymbol)
  const [interval, setInterval] = useState(defaultInterval)
  const queryClient = useQueryClient()

  const chartRef = useRef<HTMLDivElement>(null)
  const chartApiRef = useRef<ChartApi | null>(null)
  const klineProRef = useRef<unknown>(null)
  const datafeed = useMemo(() => createBackendDatafeed(), [])

  /* queries */
  const { data: klines } = useQuery({
    queryKey: ['klines', symbol, interval],
    queryFn: () => marketApi.klines(symbol, interval, 1000),
    refetchInterval: 5000,
  })
  const { data: orderbook, isLoading: obLoading } = useQuery({
    queryKey: ['orderbook', symbol],
    queryFn: () => marketApi.orderBook(symbol, 20),
    refetchInterval: 2000,
  })
  const { data: recentTrades } = useQuery({
    queryKey: ['trades', symbol],
    queryFn: () => marketApi.trades(symbol, 50),
    refetchInterval: 3000,
  })
  const { data: orders, isLoading: ordersLoading } = useQuery({
    queryKey: ['orders'],
    queryFn: () => orderApi.list(),
    refetchInterval: 5000,
  })
  const { data: historyOrders, isLoading: historyLoading } = useQuery({
    queryKey: ['orders-history'],
    queryFn: () => orderApi.history({ status: 'filled' }),
    refetchInterval: 10000,
  })
  const { data: fillTrades, isLoading: fillsLoading } = useQuery({
    queryKey: ['fills'],
    queryFn: () => tradesApi.list({ limit: '30' }),
    refetchInterval: 5000,
  })
  const { data: portfolio } = useQuery({
    queryKey: ['portfolio'],
    queryFn: () => portfolioApi.summary(),
    refetchInterval: 10000,
  })
  const { data: snapshot } = useQuery({
    queryKey: ['snapshot', symbol],
    queryFn: () => marketApi.snapshot(symbol).then(d => d as TickerSnapshot),
    refetchInterval: 5000,
  })

  /* websocket */
  const { on: wsOn } = useWebSocket('/ws', {
    onReconnect: () => {
      queryClient.invalidateQueries({ queryKey: ['klines', symbol, interval] })
      queryClient.invalidateQueries({ queryKey: ['orderbook', symbol] })
      queryClient.invalidateQueries({ queryKey: ['snapshot', symbol] })
      runBackfill()
    },
  })
  const [liveTrades, setLiveTrades] = useState<
    Array<{ id: string; price: number; quantity: number; side: 'buy' | 'sell'; time: number }>
  >([])

  useEffect(() => {
    const unsub = wsOn('trade', (data: unknown) => {
      const d = data as Record<string, unknown>
      if (d.symbol === symbol) {
        setLiveTrades((prev) => [
          {
            id: String(d.id || Date.now()),
            price: Number(d.price),
            quantity: Number(d.quantity),
            side: String(d.side) as 'buy' | 'sell',
            time: Number(d.time) || Date.now(),
          },
          ...prev.slice(0, 99),
        ])
      }
    })
    return unsub
  }, [wsOn, symbol])

  useEffect(() => {
    const unsub = wsOn('price', (msg: unknown) => {
      const m = msg as Record<string, unknown>
      if (m.symbol) {
        const data = m.data as Record<string, unknown> | undefined
        handlePriceTick(
          String(m.symbol),
          Number(data?.last ?? data?.price ?? 0),
          Number(data?.volume ?? 0)
        )
      }
    })
    return unsub
  }, [wsOn])

  /* clear liveTrades when symbol changes */
  useEffect(() => {
    setLiveTrades([])
  }, [symbol])

  /* computed */
  const lastPrice = useMemo(() => {
    if (snapshot?.price) return parseFloat(String(snapshot.price))
    if (klines?.length) return parseFloat(String((klines as Array<{ close: number }>)[(klines as Array<{ close: number }>).length - 1].close))
    return 0
  }, [snapshot, klines])

  const prevClose = useMemo(() => {
    if (klines && (klines as Array<{ close: number }>).length > 1)
      return parseFloat(String((klines as Array<{ close: number }>)[(klines as Array<{ close: number }>).length - 2].close))
    return lastPrice
  }, [klines, lastPrice])

  const change = lastPrice - prevClose
  const changePct = prevClose ? (change / prevClose) * 100 : 0
  const isUp = change >= 0
  const bestBid = orderbook?.bids?.[0]?.[0] != null ? String(orderbook.bids[0][0]) : ''
  const bestAsk = orderbook?.asks?.[0]?.[0] != null ? String(orderbook.asks[0][0]) : ''

  const obMax = useMemo(() => {
    if (!orderbook) return 1
    const bidMax = Math.max(
      ...(orderbook.bids || []).map((b: [number, number]) => Number(b[1]) || 0),
      0
    )
    const askMax = Math.max(
      ...(orderbook.asks || []).map((a: [number, number]) => Number(a[1]) || 0),
      0
    )
    return Math.max(bidMax, askMax, 1)
  }, [orderbook])

  const displayTrades = useMemo(() => {
    const src = liveTrades.length ? liveTrades : (recentTrades || [])
    return src.slice(0, 50) as Trade[]
  }, [liveTrades, recentTrades])

  /* chart init */
  const initChart = useCallback(() => {
    if (!chartRef.current) return
    if (klineProRef.current) {
      chartRef.current.innerHTML = ''
      klineProRef.current = null
      chartApiRef.current = null
    }

    let intervalId: number | null = null
    try {
      const chart = new KLineChartPro({
        container: chartRef.current,
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
          } catch { /* ignore */ }
          try {
            chartApi.setBarSpace(4)
          } catch { /* ignore */ }
          if (typeof chartApi.updateData === 'function') {
            setChartUpdater((bar) => {
              try {
                chartApi.updateData(bar)
              } catch { /* ignore */ }
            })
          }
        } else {
          intervalId = window.setTimeout(checkApi, 100)
        }
      }
      checkApi()
    } catch { /* ignore init error */ }
    return () => {
      if (intervalId) window.clearTimeout(intervalId)
    }
  }, [datafeed, symbol, interval])

  /* period click observer */
  useEffect(() => {
    const el = chartRef.current
    if (!el) return
    const handleClick = (e: MouseEvent) => {
      let t = e.target as HTMLElement | null
      while (t && t !== el) {
        if (
          t.classList?.contains('period') &&
          t.parentElement?.classList?.contains('klinecharts-pro-period-bar')
        ) {
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
  }, [])

  /* cancel order */
  const handleCancelOrder = useCallback(
    async (id: string) => {
      try {
        await orderApi.cancel(id)
        queryClient.invalidateQueries({ queryKey: ['orders'] })
      } catch { /* ignore */ }
    },
    [queryClient]
  )

  return {
    symbol,
    setSymbol,
    interval,
    setInterval,
    liveTrades,
    chartRef,
    klines,
    orderbook,
    obLoading,
    recentTrades,
    orders,
    ordersLoading,
    historyOrders,
    historyLoading,
    fillTrades,
    fillsLoading,
    portfolio,
    snapshot,
    lastPrice,
    prevClose,
    change,
    changePct,
    isUp,
    bestBid,
    bestAsk,
    obMax,
    displayTrades,
    handleCancelOrder,
    initChart,
  }
}
