import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { arbitrageApi, configApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import type { ArbitrageConfig, ArbitrageOpportunity, ArbitragePosition, ArbitrageHistoryItem } from '@/types'

export const DEFAULT_CONFIG: ArbitrageConfig = {
  symbol: 'BTCUSDT',
  min_spread_pct: 0.3,
  order_size: 500,
  max_positions: 3,
  fee_a: 0.001,
  fee_b: 0.001,
  poll_interval: 2,
  auto_execute: false,
  dry_run: true,
  adaptive_qty_enabled: false,
  max_slippage_pct: 0.5,
  min_order_qty: 0.001,
  min_order_value: 10.0,
}

export function useCrossArbitrage() {
  const queryClient = useQueryClient()
  const [showHistory, setShowHistory] = useState(false)
  const [showConfig, setShowConfig] = useState(false)
  const [editConfig, setEditConfig] = useState<ArbitrageConfig | null>(null)
  const [symbolsInput, setSymbolsInput] = useState<string>('BTCUSDT')

  /* ── Queries ── */
  const { data: status } = useQuery({
    queryKey: ['arbitrage-status'],
    queryFn: () => arbitrageApi.status(),
    refetchInterval: 5000,
  })

  const { data: configData } = useQuery({
    queryKey: ['arbitrage-config'],
    queryFn: () => arbitrageApi.config(),
    staleTime: 30000,
  })

  const { data: opportunities } = useQuery({
    queryKey: ['arbitrage-opportunity'],
    queryFn: () => arbitrageApi.opportunity(),
    refetchInterval: 3000,
  })

  const { data: positions } = useQuery({
    queryKey: ['arbitrage-positions'],
    queryFn: () => arbitrageApi.positions(),
    refetchInterval: 5000,
  })

  const { data: history } = useQuery({
    queryKey: ['arbitrage-history'],
    queryFn: () => arbitrageApi.history(50),
    enabled: showHistory,
  })

  const { data: exchangesMeta } = useQuery({
    queryKey: ['arbitrage-exchanges'],
    queryFn: () => arbitrageApi.exchanges(),
    enabled: showConfig,
  })

  const { data: configuredExchanges } = useQuery({
    queryKey: ['configured-exchanges'],
    queryFn: () => configApi.exchangesConfigured(),
    enabled: showConfig,
    staleTime: 30000,
  })

  /* ── Effects ── */
  useEffect(() => {
    if (configData) {
      setEditConfig({ ...DEFAULT_CONFIG, ...configData })
      const symbols = configData.symbols?.length ? configData.symbols : [configData.symbol || 'BTCUSDT']
      setSymbolsInput(symbols.join(', '))
    }
  }, [configData])

  /* ── Mutations ── */
  const startMutation = useMutation({
    mutationFn: arbitrageApi.start,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] }),
  })

  const stopMutation = useMutation({
    mutationFn: arbitrageApi.stop,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] }),
  })

  const updateConfigMut = useMutation({
    mutationFn: (data: ArbitrageConfig) => arbitrageApi.updateConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-config'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', '配置已保存')
    },
    onError: (err: Error) => {
      toast('error', err.message || '保存失败')
    },
  })

  const registerExchangeMut = useMutation({
    mutationFn: (data: { name: string }) => arbitrageApi.registerExchange(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-exchanges'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', '交易所已加入套利')
    },
    onError: (err: Error) => {
      toast('error', err.message || '加入失败')
    },
  })

  const executeMut = useMutation({
    mutationFn: (data: {
      symbol: string
      buy_exchange: string
      sell_exchange: string
      buy_price: number
      sell_price: number
      quantity: number
    }) => arbitrageApi.execute(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', '套利执行已提交')
    },
    onError: (err: Error) => {
      toast('error', err.message || '执行失败')
    },
  })

  const closePositionMut = useMutation({
    mutationFn: ({ id, sell_price }: { id: string; sell_price: number }) => arbitrageApi.closePosition(id, sell_price),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', '持仓已平仓')
    },
    onError: (err: Error) => {
      toast('error', err.message || '平仓失败')
    },
  })

  const failPositionMut = useMutation({
    mutationFn: (id: string) => arbitrageApi.failPosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', '持仓已标记为失败')
    },
    onError: (err: Error) => {
      toast('error', err.message || '标记失败')
    },
  })

  /* ── Derived ── */
  const isRunning = status?.running ?? false
  const stats = (status?.stats ?? {}) as Record<string, string | number | undefined>
  const opportunity: ArbitrageOpportunity | null = opportunities?.[0] ?? null

  /* ── Handlers ── */
  const handleSaveConfig = useCallback(() => {
    if (!editConfig) return
    const symbols = symbolsInput
      .split(',')
      .map((s) => s.trim().toUpperCase())
      .filter(Boolean)
    const payload: ArbitrageConfig = {
      ...editConfig,
      symbol: symbols[0] || editConfig.symbol,
      symbols: symbols.length > 0 ? symbols : undefined,
    }
    updateConfigMut.mutate(payload)
  }, [editConfig, symbolsInput, updateConfigMut])

  const handleRegisterExchange = useCallback(
    (name: string) => {
      registerExchangeMut.mutate({ name })
    },
    [registerExchangeMut]
  )

  const handleExecute = useCallback(
    (opp: ArbitrageOpportunity) => {
      if (!editConfig) return
      if (!editConfig.dry_run && !window.confirm('确认执行真实套利交易？')) return
      const targetQty = editConfig.order_size / opp.buy_price
      const quantity = opp.adjusted_qty ?? Math.floor(targetQty * 1e6) / 1e6
      executeMut.mutate({
        symbol: opp.symbol,
        buy_exchange: opp.buy_exchange,
        sell_exchange: opp.sell_exchange,
        buy_price: opp.buy_price,
        sell_price: opp.sell_price,
        quantity,
      })
    },
    [editConfig, executeMut]
  )

  const isPositionActive = useCallback((s: string) => ['pending', 'open_buy', 'open', 'open_sell'].includes(s), [])

  const handleClosePosition = useCallback(
    (pos: ArbitragePosition) => {
      const input = window.prompt('请输入实际卖出价（USD）', pos.sell_price?.toFixed(2) ?? '')
      if (input === null) return
      const sellPrice = Number(input)
      if (Number.isNaN(sellPrice) || sellPrice <= 0) {
        toast('error', '请输入有效的卖出价')
        return
      }
      closePositionMut.mutate({ id: pos.id, sell_price: sellPrice })
    },
    [closePositionMut]
  )

  const handleFailPosition = useCallback(
    (pos: ArbitragePosition) => {
      if (!window.confirm(`确认将持仓 ${pos.symbol} 标记为失败？`)) return
      failPositionMut.mutate(pos.id)
    },
    [failPositionMut]
  )

  return {
    isRunning,
    stats,
    opportunity,
    positions,
    history,
    showHistory,
    setShowHistory,
    showConfig,
    setShowConfig,
    editConfig,
    setEditConfig,
    symbolsInput,
    setSymbolsInput,
    configuredExchanges,
    exchangesMeta,
    startMutation,
    stopMutation,
    updateConfigMut,
    registerExchangeMut,
    executeMut,
    closePositionMut,
    failPositionMut,
    handleSaveConfig,
    handleRegisterExchange,
    handleExecute,
    isPositionActive,
    handleClosePosition,
    handleFailPosition,
  }
}
