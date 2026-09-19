import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { arbitrageApi, configApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { useI18n } from '@/i18n'
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
  const { t } = useI18n()
  const { confirm, prompt, Dialog } = useConfirmDialog()
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
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.cross.toast-engine-started'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.cross.toast-start-failed'))
    },
  })

  const stopMutation = useMutation({
    mutationFn: arbitrageApi.stop,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.cross.toast-engine-stopped'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.cross.toast-stop-failed'))
    },
  })

  const updateConfigMut = useMutation({
    mutationFn: (data: ArbitrageConfig) => arbitrageApi.updateConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-config'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.ui.toast-config-saved'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.ui.toast-save-failed'))
    },
  })

  const registerExchangeMut = useMutation({
    mutationFn: (data: { name: string }) => arbitrageApi.registerExchange(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-exchanges'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.cross.toast-exchange-added'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.cross.toast-add-failed'))
    },
  })

  const unregisterExchangeMut = useMutation({
    mutationFn: (name: string) => arbitrageApi.unregisterExchange(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-exchanges'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.cross.toast-exchange-removed'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.cross.toast-remove-failed'))
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
      toast('success', t('arb.cross.toast-execute-submitted'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.ui.toast-execute-failed'))
    },
  })

  const closePositionMut = useMutation({
    mutationFn: ({ id, sell_price }: { id: string; sell_price: number }) => arbitrageApi.closePosition(id, sell_price),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.ui.toast-position-closed'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.ui.toast-close-failed'))
    },
  })

  const failPositionMut = useMutation({
    mutationFn: (id: string) => arbitrageApi.failPosition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['arbitrage-positions'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-history'] })
      queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
      toast('success', t('arb.ui.toast-position-failed'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('arb.ui.toast-mark-failed'))
    },
  })

  /* ── Derived ── */
  const isRunning = status?.running ?? false
  const stats = (status?.stats ?? {}) as Record<string, string | number | undefined>
  const opportunity: ArbitrageOpportunity | null = opportunities?.[0] ?? null

  /* ── Handlers ── */
  const handleSaveConfig = useCallback(
    async (selectedExchanges: string[]) => {
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

      // 应用交易所选择差异：新勾选的注册，取消勾选的注销
      const registered = new Set(exchangesMeta?.exchanges ?? [])
      const target = new Set(selectedExchanges)
      const failed: string[] = []
      for (const name of new Set([...registered, ...target])) {
        try {
          if (target.has(name) && !registered.has(name)) {
            await arbitrageApi.registerExchange({ name })
          } else if (!target.has(name) && registered.has(name)) {
            await arbitrageApi.unregisterExchange(name)
          }
        } catch (e) {
          failed.push(`${name}: ${e instanceof Error ? e.message : t('arb.cross.op-failed')}`)
        }
      }

      try {
        await arbitrageApi.updateConfig(payload)
        queryClient.invalidateQueries({ queryKey: ['arbitrage-config'] })
        queryClient.invalidateQueries({ queryKey: ['arbitrage-exchanges'] })
        queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] })
        if (failed.length > 0) {
          toast('error', `${t('arb.cross.toast-partial-failed')}${failed.join('；')}`)
        } else {
          toast('success', t('arb.ui.toast-config-saved'))
        }
        setShowConfig(false)
      } catch (e) {
        toast('error', e instanceof Error ? e.message : t('arb.ui.toast-save-failed'))
      }
    },
    [editConfig, symbolsInput, exchangesMeta, queryClient, t]
  )

  const handleExecute = useCallback(
    async (opp: ArbitrageOpportunity) => {
      if (!editConfig) return
      if (!editConfig.dry_run) {
        const ok = await confirm({
          title: t('arb.cross.confirm-execute-title'),
          message: t('arb.cross.confirm-execute-msg')
            .replace('{symbol}', opp.symbol)
            .replace('{buy}', opp.buy_exchange)
            .replace('{sell}', opp.sell_exchange)
            .replace('{spread}', opp.spread_pct.toFixed(4)),
          confirmText: t('arb.ui.execute'),
          cancelText: t('arb.ui.cancel'),
        })
        if (!ok) return
      }
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
    [editConfig, executeMut, confirm, t]
  )

  const isPositionActive = useCallback((s: string) => ['pending', 'open_buy', 'open', 'open_sell'].includes(s), [])

  const handleClosePosition = useCallback(
    async (pos: ArbitragePosition) => {
      const input = await prompt({
        title: t('arb.ui.close-position'),
        message: t('arb.ui.confirm-close-msg').replace('{target}', pos.symbol),
        inputLabel: t('arb.ui.actual-sell-price'),
        defaultValue: pos.sell_price?.toFixed(2) ?? '',
        inputType: 'number',
        confirmText: t('arb.ui.close-position'),
        cancelText: t('arb.ui.cancel'),
      })
      if (input === null) return
      const sellPrice = Number(input)
      if (Number.isNaN(sellPrice) || sellPrice <= 0) {
        toast('error', t('arb.ui.invalid-sell-price'))
        return
      }
      closePositionMut.mutate({ id: pos.id, sell_price: sellPrice })
    },
    [closePositionMut, prompt, t]
  )

  const handleFailPosition = useCallback(
    async (pos: ArbitragePosition) => {
      const ok = await confirm({
        title: t('arb.ui.mark-as-failed'),
        message: t('arb.ui.confirm-fail-msg').replace('{target}', pos.symbol),
        variant: 'danger',
        confirmText: t('arb.ui.mark-fail'),
        cancelText: t('arb.ui.cancel'),
      })
      if (!ok) return
      failPositionMut.mutate(pos.id)
    },
    [failPositionMut, confirm, t]
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
    unregisterExchangeMut,
    executeMut,
    closePositionMut,
    failPositionMut,
    handleSaveConfig,
    handleExecute,
    isPositionActive,
    handleClosePosition,
    handleFailPosition,
    Dialog,
  }
}
