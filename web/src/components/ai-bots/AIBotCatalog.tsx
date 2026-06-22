import React, { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Bot, TrendingUp, Activity, Shield, Zap, BarChart3,
  ChevronRight, Search, Filter
} from 'lucide-react'
import { aiBotApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Skeleton } from '@/components/ui/Skeleton'
import type { AIBotCatalogItem, AIBotPerformance } from '@/types'

interface AIBotCatalogProps {
  onDeploy: (bot: AIBotCatalogItem) => void
}

const RISK_META: Record<string, { label: string; variant: 'success' | 'warning' | 'error' }> = {
  low: { label: '低风险', variant: 'success' },
  medium: { label: '中风险', variant: 'warning' },
  high: { label: '高风险', variant: 'error' },
}

const MARKET_META: Record<string, { label: string; icon: React.ReactNode }> = {
  spot: { label: '现货', icon: <Bot className="w-3 h-3" /> },
  futures: { label: '合约', icon: <BarChart3 className="w-3 h-3" /> },
}

function parsePerformance(json?: string): AIBotPerformance {
  if (!json) return {}
  try {
    const parsed = JSON.parse(json)
    if (typeof parsed === 'string') {
      return JSON.parse(parsed) as AIBotPerformance
    }
    return parsed as AIBotPerformance
  } catch {
    return {}
  }
}

export const AIBotCatalog: React.FC<AIBotCatalogProps> = ({ onDeploy }) => {
  const [filter, setFilter] = useState('')
  const [marketFilter, setMarketFilter] = useState<'all' | 'spot' | 'futures'>('all')
  const [riskFilter, setRiskFilter] = useState<'all' | 'low' | 'medium' | 'high'>('all')

  const { data: catalog = [], isLoading } = useQuery({
    queryKey: ['ai-bots', 'catalog'],
    queryFn: () => aiBotApi.catalog(),
    staleTime: 60_000,
  })

  const items = useMemo(() => {
    return catalog.filter((bot: AIBotCatalogItem) => {
      const matchesSearch = !filter ||
        bot.name.toLowerCase().includes(filter.toLowerCase()) ||
        (bot.description || '').toLowerCase().includes(filter.toLowerCase())
      const matchesMarket = marketFilter === 'all' || bot.market_type === marketFilter
      const matchesRisk = riskFilter === 'all' || bot.risk_level === riskFilter
      return matchesSearch && matchesMarket && matchesRisk
    })
  }, [catalog, filter, marketFilter, riskFilter])

  if (isLoading) {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-56 rounded-xl" />
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3 items-start sm:items-center justify-between">
        <div className="relative w-full sm:w-80">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[#666]" />
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="搜索机器人 / 策略"
            className="pl-9 bg-[#111] border-[#2a2a2a]"
          />
        </div>
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-[#666]" />
          {(['all', 'spot', 'futures'] as const).map((m) => (
            <button
              key={m}
              onClick={() => setMarketFilter(m)}
              className={cn(
                'px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors',
                marketFilter === m
                  ? 'bg-[#1890ff]/10 border-[#1890ff]/30 text-[#1890ff]'
                  : 'bg-[#111] border-[#2a2a2a] text-[#888] hover:border-[#444]'
              )}
            >
              {m === 'all' ? '全部市场' : m === 'spot' ? '现货' : '合约'}
            </button>
          ))}
          {(['all', 'low', 'medium', 'high'] as const).map((r) => (
            <button
              key={r}
              onClick={() => setRiskFilter(r)}
              className={cn(
                'px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors',
                riskFilter === r
                  ? 'bg-[#1890ff]/10 border-[#1890ff]/30 text-[#1890ff]'
                  : 'bg-[#111] border-[#2a2a2a] text-[#888] hover:border-[#444]'
              )}
            >
              {r === 'all' ? '全部风险' : RISK_META[r]?.label}
            </button>
          ))}
        </div>
      </div>

      {/* Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {items.length === 0 && (
          <div className="col-span-full text-center py-12 text-[#666]">
            未找到匹配的机器人
          </div>
        )}
        {items.map((bot: AIBotCatalogItem) => {
          const perf = parsePerformance(bot.performance_json)
          const risk = RISK_META[bot.risk_level] || RISK_META.medium
          const market = MARKET_META[bot.market_type] || MARKET_META.spot
          return (
            <div
              key={bot.id}
              className="rounded-xl border border-[#1c1c1c] bg-[#111] p-5 hover:border-[#333] transition-colors flex flex-col"
            >
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-xl bg-[#1890ff]/10 flex items-center justify-center text-[#1890ff]">
                    <Bot className="w-5 h-5" />
                  </div>
                  <div>
                    <h3 className="text-sm font-semibold text-white">{bot.name}</h3>
                    <div className="flex items-center gap-2 mt-1">
                      <Badge variant={risk.variant} className="text-[10px]">{risk.label}</Badge>
                      <Badge variant="neutral" className="text-[10px] gap-1">
                        {market.icon}
                        {market.label}
                      </Badge>
                    </div>
                  </div>
                </div>
              </div>

              <p className="text-xs text-[#888] mb-4 line-clamp-2">{bot.description}</p>

              <div className="grid grid-cols-3 gap-2 mb-4">
                <StatBox
                  icon={<TrendingUp className="w-3 h-3" />}
                  label="收益"
                  value={perf.avg_monthly_profit !== undefined ? `+${perf.avg_monthly_profit}%` : perf.total_profit !== undefined ? `+${perf.total_profit}%` : '--'}
                />
                <StatBox
                  icon={<Activity className="w-3 h-3" />}
                  label="胜率"
                  value={perf.win_rate !== undefined ? `${(perf.win_rate * 100).toFixed(0)}%` : '--'}
                />
                <StatBox
                  icon={<Shield className="w-3 h-3" />}
                  label="回撤"
                  value={perf.max_drawdown !== undefined ? `${perf.max_drawdown}%` : '--'}
                />
              </div>

              <div className="mt-auto pt-4 border-t border-[#1c1c1c]">
                <div className="flex items-center justify-between">
                  <div className="text-xs text-[#888]">
                    {bot.fee_model === 'free' && <span className="text-[#52c41a]">免费</span>}
                    {bot.fee_model === 'monthly' && <span>${bot.monthly_fee}/月</span>}
                    {bot.fee_model === 'profit_share' && <span>盈利 {bot.fee_percent}%</span>}
                  </div>
                  <Button
                    size="sm"
                    leftIcon={<Zap className="w-3 h-3" />}
                    rightIcon={<ChevronRight className="w-3 h-3" />}
                    onClick={() => onDeploy(bot)}
                  >
                    部署
                  </Button>
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function StatBox({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5">
      <div className="flex items-center gap-1.5 text-[#666] mb-1">
        {icon}
        <span className="text-[10px]">{label}</span>
      </div>
      <div className="text-sm font-semibold text-[#e0e0e0]">{value}</div>
    </div>
  )
}

export default AIBotCatalog
