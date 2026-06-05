import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { arbitrageApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import {
  ArrowLeftRight,
  Play,
  Square,
  RefreshCw,
  TrendingUp,
  DollarSign,
  Activity,
  Globe,
  Zap,
  CheckCircle2,
  AlertCircle,
  Clock,
  Target,
  Layers,
  ChevronDown,
  ChevronUp,
} from 'lucide-react'

/* ── Types ── */
interface ArbitrageOpportunity {
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  spread_pct: number
  spread_abs: number
  timestamp: number
}

interface TradePair {
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  quantity: number
  net_profit: number
  status: string
  timestamp: number
}

/* ── Page ── */
export function ArbitrageMonitor() {
  const queryClient = useQueryClient()
  const [showHistory, setShowHistory] = useState(false)
  const [showConfig, setShowConfig] = useState(false)

  // Queries
  const { data: status, isLoading: statusLoading } = useQuery({
    queryKey: ['arbitrage-status'],
    queryFn: async () => {
      const res = await arbitrageApi.status()
      return res as any
    },
    refetchInterval: 5000,
  })

  const { data: opportunity } = useQuery({
    queryKey: ['arbitrage-opportunity'],
    queryFn: async () => {
      const res = await arbitrageApi.opportunity()
      return (res as any).opportunity as ArbitrageOpportunity | null
    },
    refetchInterval: 3000,
  })

  const { data: positions } = useQuery({
    queryKey: ['arbitrage-positions'],
    queryFn: async () => {
      const res = await arbitrageApi.positions()
      return (res as any).positions as TradePair[]
    },
    refetchInterval: 5000,
  })

  const { data: history } = useQuery({
    queryKey: ['arbitrage-history'],
    queryFn: async () => {
      const res = await arbitrageApi.history(50)
      return (res as any).history as TradePair[]
    },
    enabled: showHistory,
  })

  const { data: config } = useQuery({
    queryKey: ['arbitrage-config'],
    queryFn: async () => {
      const res = await arbitrageApi.config()
      return (res as any).config as Record<string, any>
    },
    enabled: showConfig,
  })

  // Mutations
  const startMutation = useMutation({
    mutationFn: arbitrageApi.start,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] }),
  })

  const stopMutation = useMutation({
    mutationFn: arbitrageApi.stop,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['arbitrage-status'] }),
  })

  const isRunning = status?.running ?? false
  const stats = status?.stats as Record<string, any> || {}

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="套利监控"
          subtitle="跨交易所价差套利实时监控"
          actions={<ArrowLeftRight className="w-6 h-6 text-quant-gold" />}
        />

        {/* Status & Controls */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="引擎状态"
            value={isRunning ? '运行中' : '已停止'}
            icon={isRunning ? <Activity className="w-4 h-4 text-green-400" /> : <AlertCircle className="w-4 h-4 text-red-400" />}
            subValue={isRunning ? '监控中' : '点击启动'}
            trend={isRunning ? 'up' : 'down'}
          />
          <KPICard
            label="检测次数"
            value={stats.checks ?? 0}
            icon={<Target className="w-4 h-4 text-quant-gold" />}
            subValue="总扫描"
            trend="neutral"
          />
          <KPICard
            label="执行次数"
            value={stats.executions ?? 0}
            icon={<Zap className="w-4 h-4 text-quant-gold" />}
            subValue="已执行"
            trend="up"
          />
          <KPICard
            label="总利润"
            value={stats.total_profit ? `$${stats.total_profit.toFixed(2)}` : '$0.00'}
            icon={<DollarSign className="w-4 h-4 text-quant-gold" />}
            subValue="累计"
            trend="up"
          />
        </div>

        {/* Controls */}
        <div className="flex items-center gap-2">
          {!isRunning ? (
            <button
              onClick={() => startMutation.mutate()}
              disabled={startMutation.isPending}
              className={cn(
                'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                startMutation.isPending
                  ? 'bg-muted text-muted-foreground cursor-not-allowed'
                  : 'bg-green-500/20 text-green-400 hover:bg-green-500/30'
              )}
            >
              {startMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
              启动引擎
            </button>
          ) : (
            <button
              onClick={() => stopMutation.mutate()}
              disabled={stopMutation.isPending}
              className={cn(
                'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                stopMutation.isPending
                  ? 'bg-muted text-muted-foreground cursor-not-allowed'
                  : 'bg-red-500/20 text-red-400 hover:bg-red-500/30'
              )}
            >
              {stopMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Square className="w-4 h-4" />}
              停止引擎
            </button>
          )}
          <button
            onClick={() => setShowConfig(!showConfig)}
            className={cn(
              'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
              showConfig ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
            )}
          >
            <Layers className="w-3.5 h-3.5" />
            配置
          </button>
          <button
            onClick={() => setShowHistory(!showHistory)}
            className={cn(
              'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
              showHistory ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
            )}
          >
            <Clock className="w-3.5 h-3.5" />
            历史
          </button>
        </div>

        {/* Config */}
        {showConfig && config && (
          <SectionCard title="引擎配置">
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {Object.entries(config).map(([key, value]) => (
                <div key={key} className="p-2.5 rounded-md bg-quant-bg-secondary">
                  <div className="text-[10px] text-muted-foreground uppercase">{key}</div>
                  <div className="text-sm font-semibold mt-0.5">{String(value)}</div>
                </div>
              ))}
            </div>
          </SectionCard>
        )}

        {/* Latest Opportunity */}
        <SectionCard
          title="最新套利机会"
          headerAction={
            opportunity ? (
              <span className={cn(
                'px-2 py-1 rounded text-xs font-medium',
                (opportunity.spread_pct ?? 0) > 1 ? 'bg-green-500/10 text-green-400' : 'bg-quant-gold/10 text-quant-gold'
              )}>
                价差 {(opportunity.spread_pct ?? 0).toFixed(2)}%
              </span>
            ) : null
          }
        >
          {opportunity ? (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-3">
                <div className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                  <div className="flex items-center gap-2">
                    <Globe className="w-4 h-4 text-green-400" />
                    <span className="text-sm font-medium">买入</span>
                  </div>
                  <div className="text-right">
                    <div className="text-sm font-semibold">{opportunity.buy_exchange}</div>
                    <div className="text-xs text-muted-foreground">${opportunity.buy_price?.toFixed(2) ?? '-'}</div>
                  </div>
                </div>
                <div className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                  <div className="flex items-center gap-2">
                    <Globe className="w-4 h-4 text-red-400" />
                    <span className="text-sm font-medium">卖出</span>
                  </div>
                  <div className="text-right">
                    <div className="text-sm font-semibold">{opportunity.sell_exchange}</div>
                    <div className="text-xs text-muted-foreground">${opportunity.sell_price?.toFixed(2) ?? '-'}</div>
                  </div>
                </div>
              </div>
              <div className="flex flex-col justify-center p-4 rounded-md bg-quant-bg-secondary">
                <div className="text-xs text-muted-foreground text-center">交易对</div>
                <div className="text-xl font-bold text-center mt-1">{opportunity.symbol}</div>
                <div className="text-xs text-muted-foreground text-center mt-2">
                  绝对价差 ${(opportunity.spread_abs ?? 0).toFixed(2)}
                </div>
                <div className="text-xs text-muted-foreground text-center">
                  {new Date(opportunity.timestamp).toLocaleTimeString()}
                </div>
              </div>
            </div>
          ) : (
            <EmptyState
              icon={<ArrowLeftRight className="w-10 h-10 text-muted-foreground" />}
              title="暂无套利机会"
              description={isRunning ? '引擎正在扫描中...' : '启动引擎后开始扫描'}
            />
          )}
        </SectionCard>

        {/* Active Positions */}
        <SectionCard title="活跃持仓">
          <div className="space-y-2">
            {!positions || positions.length === 0 ? (
              <div className="text-sm text-muted-foreground text-center py-4">无活跃持仓</div>
            ) : (
              positions.map((pos, i) => (
                <div key={i} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                  <div className="flex items-center gap-3">
                    <ArrowLeftRight className="w-4 h-4 text-quant-gold" />
                    <div>
                      <div className="text-sm font-medium">{pos.symbol}</div>
                      <div className="text-xs text-muted-foreground">
                        {pos.buy_exchange} → {pos.sell_exchange}
                      </div>
                    </div>
                  </div>
                  <div className="text-right">
                    <div className={cn(
                      'text-sm font-semibold',
                      pos.net_profit > 0 ? 'text-green-400' : 'text-red-400'
                    )}>
                      {pos.net_profit > 0 ? '+' : ''}${pos.net_profit.toFixed(2)}
                    </div>
                    <div className="text-xs text-muted-foreground">{pos.status}</div>
                  </div>
                </div>
              ))
            )}
          </div>
        </SectionCard>

        {/* History */}
        {showHistory && (
          <SectionCard
            title={
              <button onClick={() => setShowHistory(false)} className="flex items-center gap-2">
                历史记录 <ChevronUp className="w-4 h-4" />
              </button>
            }
          >
            <div className="space-y-2 max-h-80 overflow-y-auto">
              {!history || history.length === 0 ? (
                <div className="text-sm text-muted-foreground text-center py-4">无历史记录</div>
              ) : (
                history.map((trade, i) => (
                  <div key={i} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                    <div className="flex items-center gap-3">
                      <CheckCircle2 className={cn('w-4 h-4', trade.net_profit > 0 ? 'text-green-400' : 'text-red-400')} />
                      <div>
                        <div className="text-sm font-medium">{trade.symbol}</div>
                        <div className="text-xs text-muted-foreground">
                          {trade.buy_exchange} → {trade.sell_exchange}
                        </div>
                      </div>
                    </div>
                    <div className="text-right">
                      <div className={cn(
                        'text-sm font-semibold',
                        trade.net_profit > 0 ? 'text-green-400' : 'text-red-400'
                      )}>
                        {trade.net_profit > 0 ? '+' : ''}${trade.net_profit.toFixed(2)}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {new Date(trade.timestamp).toLocaleString()}
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>
          </SectionCard>
        )}
      </div>
    </div>
  )
}
