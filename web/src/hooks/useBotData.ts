import React, { useMemo, useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { strategyApi } from '@/lib/api'
import type { StrategyItem } from '@/types'
import {
  Grid3X3,
  Layers,
  ArrowLeftRight,
  Activity,
  LineChart,
  TrendingUp,
  TrendingDown,
  BarChart3,
  Zap,
  Terminal,
} from 'lucide-react'

export interface BotItem {
  id: string
  name: string
  strategy_name?: string
  status: 'running' | 'stopped' | 'paused' | 'error'
  bot_type: 'grid' | 'dca' | 'arbitrage' | 'market_making' | 'trend' | 'custom' | 'martin_trend' | 'wallstreet' | 'dual_burn' | 'macd_golden' | 'macd_death' | 'ema_follow' | 'ema_counter'
  coin?: string
  symbol?: string
  leverage?: number
  unrealized_pnl?: number
  realized_pnl?: number
  initial_capital?: number
  trading_config?: {
    initial_capital?: number
    bot_type?: string
    bot_params?: Record<string, unknown>
    symbol?: string
    timeframe?: string
    order_count?: number
    first_order_amount?: number
    add_position_spread?: number
    add_position_callback?: number
    take_profit_ratio?: number
    profit_callback?: number
    trade_count_mode?: 'single' | 'cycle'
    open_indicator?: string
    add_position_indicator?: string
    waterfall_protection?: number
    open_double?: boolean
    trend_indicator?: boolean
    trend_timeframe?: string
    take_profit_method?: string
    reverse_take_profit?: boolean
    reverse_stop_loss?: boolean
    follow_trend?: boolean
    follow_trend_max?: number
    burn_cut?: { enabled: boolean; dual_burn_start: number; global_burn_start: number }
    custom_reduce?: boolean
    online_order_limit?: number
    profit_protection?: boolean
    close_add_position?: boolean
  }
  created_at?: string
  updated_at?: string
  strategy_code?: string
  market_category?: string
  execution_mode?: 'live' | 'paper' | 'signal'
  notification_config?: { channels: string[]; targets: Record<string, unknown> }
}

export interface BotTypeDef {
  key: BotItem['bot_type']
  label: string
  desc: string
  icon: React.ReactNode
  color: string
  bg: string
}

export const BOT_TYPES: BotTypeDef[] = [
  { key: 'grid', label: '网格交易', desc: '在价格区间内自动低买高卖，适合震荡行情', icon: React.createElement(Grid3X3, { className: 'w-6 h-6' }), color: '#52c41a', bg: 'rgba(82,196,26,0.10)' },
  { key: 'dca', label: '定投策略', desc: '定时定额分批买入，平滑持仓成本', icon: React.createElement(Layers, { className: 'w-6 h-6' }), color: '#1890ff', bg: 'rgba(24,144,255,0.10)' },
  { key: 'arbitrage', label: '套利策略', desc: '跨市场或跨品种价差套利，低风险收益', icon: React.createElement(ArrowLeftRight, { className: 'w-6 h-6' }), color: '#722ed1', bg: 'rgba(114,46,209,0.10)' },
  { key: 'market_making', label: '做市策略', desc: '双边挂单赚取买卖价差，提供流动性', icon: React.createElement(Activity, { className: 'w-6 h-6' }), color: '#fa8c16', bg: 'rgba(250,140,22,0.10)' },
  { key: 'trend', label: '趋势跟踪', desc: '跟随市场趋势方向交易，适合趋势行情', icon: React.createElement(LineChart, { className: 'w-6 h-6' }), color: '#eb2f96', bg: 'rgba(235,47,150,0.10)' },
  { key: 'martin_trend', label: '马丁趋势', desc: '倍投补仓原理(2,4,8,16,32,64) + 趋势指标，浮亏减半', icon: React.createElement(TrendingUp, { className: 'w-6 h-6' }), color: '#f5222d', bg: 'rgba(245,34,45,0.10)' },
  { key: 'wallstreet', label: '华尔街策略', desc: '等比数量补仓(1,2,3,5,8,13,21,34,55) + 趋势指标', icon: React.createElement(BarChart3, { className: 'w-6 h-6' }), color: '#faad14', bg: 'rgba(250,173,20,0.10)' },
  { key: 'macd_golden', label: 'MACD金叉策略', desc: 'MACD金叉开多/补多，死叉反向信号清仓，适合合约和现货', icon: React.createElement(Activity, { className: 'w-6 h-6' }), color: '#52c41a', bg: 'rgba(82,196,26,0.10)' },
  { key: 'macd_death', label: 'MACD死叉策略', desc: 'MACD死叉开空/补空，金叉反向信号清仓，适合合约做空', icon: React.createElement(TrendingDown, { className: 'w-6 h-6' }), color: '#ff4d4f', bg: 'rgba(255,77,79,0.10)' },
  { key: 'dual_burn', label: '双向燃烧斩仓', desc: '逆势单补仓到第3仓自动开启顺势单，用盈利消耗浮亏', icon: React.createElement(Zap, { className: 'w-6 h-6' }), color: '#722ed1', bg: 'rgba(114,46,209,0.10)' },
  { key: 'ema_follow', label: 'EMA顺势策略', desc: 'EMA60均线以上做多，EMA10拐点决定开仓时机', icon: React.createElement(LineChart, { className: 'w-6 h-6' }), color: '#13c2c2', bg: 'rgba(19,194,194,0.10)' },
  { key: 'ema_counter', label: 'EMA逆势策略', desc: '以EMA60为标准线，均线以上做空，振幅决定开仓节点', icon: React.createElement(BarChart3, { className: 'w-6 h-6' }), color: '#eb2f96', bg: 'rgba(235,47,150,0.10)' },
  { key: 'custom', label: '自定义', desc: '使用 Python 脚本编写完全自定义的策略逻辑', icon: React.createElement(Terminal, { className: 'w-6 h-6' }), color: '#8c8c8c', bg: 'rgba(140,140,140,0.10)' },
]

export const BOT_TYPE_TO_STRATEGY_TYPE: Record<BotItem['bot_type'], string> = {
  grid: 'grid_trading',
  dca: 'martingale',
  arbitrage: 'arbitrage',
  market_making: 'market_making',
  trend: 'breakout',
  martin_trend: 'martingale',
  wallstreet: 'wallstreet',
  macd_golden: 'macd',
  macd_death: 'macd',
  dual_burn: 'martingale',
  ema_follow: 'ema_cross',
  ema_counter: 'ema_cross',
  custom: 'breakout',
}

export const STATUS_META: Record<string, { label: string; dot: string; border: string; text: string; bg: string }> = {
  running: {
    label: '运行中',
    dot: 'bg-emerald-400',
    border: 'border-emerald-500/20',
    text: 'text-emerald-400',
    bg: 'bg-emerald-500/10',
  },
  paused: {
    label: '已暂停',
    dot: 'bg-amber-400',
    border: 'border-amber-500/20',
    text: 'text-amber-400',
    bg: 'bg-amber-500/10',
  },
  stopped: {
    label: '已停止',
    dot: 'bg-[#555555]',
    border: 'border-[#333333]',
    text: 'text-[#888888]',
    bg: 'bg-[#1c1c1c]',
  },
  error: {
    label: '异常',
    dot: 'bg-red-400',
    border: 'border-red-500/20',
    text: 'text-red-400',
    bg: 'bg-red-500/10',
  },
}

export function useBotData() {
  const queryClient = useQueryClient()
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)

  const { data: strategies, isLoading } = useQuery({
    queryKey: ['strategies'],
    queryFn: () => strategyApi.list(),
    refetchInterval: 5000,
  })

  const bots: BotItem[] = useMemo(() => {
    const all = Array.isArray(strategies) ? strategies : []
    return all.map((s: StrategyItem) => ({
      ...s,
      id: String(s.id),
      bot_type: ((s as StrategyItem & { bot_type?: string }).bot_type || ((s.trading_config as Record<string, unknown> | undefined)?.bot_type as string) || 'custom') as BotItem['bot_type'],
      name: s.strategy_name || s.name,
    }))
  }, [strategies])

  const kpi = useMemo(() => {
    const running = bots.filter((b) => b.status === 'running').length
    const stopped = bots.filter((b) => b.status === 'stopped').length
    const totalEquity = bots.reduce((sum, b) => sum + (b.initial_capital || b.trading_config?.initial_capital || 0), 0)
    const totalPnl = bots.reduce((sum, b) => sum + (b.unrealized_pnl || 0), 0)
    return { running, stopped, total: bots.length, totalEquity, totalPnl }
  }, [bots])

  const handleStartBot = useCallback(
    async (bot: BotItem) => {
      setActionLoadingId(bot.id)
      try {
        await strategyApi.start(bot.id)
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } finally {
        setActionLoadingId(null)
      }
    },
    [queryClient]
  )

  const handleStopBot = useCallback(
    async (bot: BotItem) => {
      setActionLoadingId(bot.id)
      try {
        await strategyApi.stop(bot.id)
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } finally {
        setActionLoadingId(null)
      }
    },
    [queryClient]
  )

  const handleDeleteBot = useCallback(
    async (bot: BotItem) => {
      if (bot.status === 'running') {
        alert('请先停止机器人再删除')
        return
      }
      if (!confirm(`确定删除机器人 "${bot.name || bot.strategy_name}" 吗？`)) return
      try {
        await strategyApi.delete(bot.id)
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } catch {
        alert('删除失败')
      }
    },
    [queryClient]
  )

  const handleCloneBot = useCallback(
    async (bot: BotItem) => {
      if (!bot.strategy_code) {
        alert('该机器人没有可克隆的策略代码')
        return
      }
      if (!confirm(`克隆机器人 "${bot.name || bot.strategy_name}" 为脚本策略？`)) return
      try {
        const tc = bot.trading_config ? JSON.parse(JSON.stringify(bot.trading_config)) : {}
        delete tc.bot_type
        delete tc.bot_params
        await strategyApi.create({
          strategy_name: `${bot.name || bot.strategy_name} (克隆)`,
          strategy_type: 'ScriptStrategy',
          strategy_mode: 'script',
          strategy_code: bot.strategy_code,
          market_category: bot.market_category || tc.market_category || 'crypto',
          execution_mode: 'signal',
          notification_config: bot.notification_config || { channels: [], targets: {} },
          trading_config: tc,
        })
        queryClient.invalidateQueries({ queryKey: ['strategies'] })
      } catch {
        alert('克隆失败')
      }
    },
    [queryClient]
  )

  return {
    bots,
    isLoading,
    kpi,
    actionLoadingId,
    startBot: handleStartBot,
    stopBot: handleStopBot,
    deleteBot: handleDeleteBot,
    cloneBot: handleCloneBot,
  }
}
