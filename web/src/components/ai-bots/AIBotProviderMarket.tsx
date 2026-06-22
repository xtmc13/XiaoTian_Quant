import React, { useMemo, useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Users, TrendingUp, Activity, Shield, Star, Bell,
  ChevronRight, Search
} from 'lucide-react'
import { socialApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Skeleton } from '@/components/ui/Skeleton'
import { useToastStore } from '@/stores/toastStore'
import { useAuthStore } from '@/stores/authStore'
import { AIFollowConfigModal } from '@/components/ai-bots/AIFollowConfigModal'

interface Provider {
  provider_id: number
  provider_name?: string
  total_signals: number
  win_count: number
  loss_count: number
  win_rate: number
  avg_return_pct: number
  sharpe_ratio: number
  max_drawdown_pct: number
  follower_count: number
  monthly_fee: number
  is_public: boolean
}

export const AIBotProviderMarket: React.FC = () => {
  const queryClient = useQueryClient()
  const addToast = useToastStore((s) => s.addToast)
  const { user } = useAuthStore()
  const followerId = user ? Number(user.id) : 1
  const [filter, setFilter] = useState('')
  const [following, setFollowing] = useState<Set<number>>(new Set())
  const [configProvider, setConfigProvider] = useState<Provider | null>(null)

  const { data: providers = [], isLoading } = useQuery<Provider[]>({
    queryKey: ['social-providers'],
    queryFn: () => socialApi.providers(),
    staleTime: 60_000,
  })

  const { data: signals = [] } = useQuery({
    queryKey: ['social-signals'],
    queryFn: () => socialApi.signals(undefined, 20),
    staleTime: 30_000,
    refetchInterval: 30_000,
  })

  const { data: followerConfigs = [] } = useQuery({
    queryKey: ['social-follower-configs', followerId],
    queryFn: () => socialApi.followerConfigs(followerId),
    staleTime: 30_000,
  })

  useEffect(() => {
    const set = new Set<number>()
    followerConfigs.forEach((cfg: any) => {
      if (cfg.enabled !== false) set.add(cfg.provider_id)
    })
    setFollowing(set)
  }, [followerConfigs])

  const followMut = useMutation({
    mutationFn: (providerId: number) => socialApi.follow(providerId, followerId),
    onSuccess: (_, providerId) => {
      setFollowing((prev) => new Set(prev).add(providerId))
      addToast({ type: 'success', message: '关注成功', duration: 3000 })
      queryClient.invalidateQueries({ queryKey: ['social-providers'] })
      queryClient.invalidateQueries({ queryKey: ['social-follower-configs'] })
    },
    onError: (err: Error) => addToast({ type: 'error', message: '关注失败: ' + err.message, duration: 5000 }),
  })

  const unfollowMut = useMutation({
    mutationFn: (providerId: number) => socialApi.unfollow(providerId, followerId),
    onSuccess: (_, providerId) => {
      setFollowing((prev) => {
        const next = new Set(prev)
        next.delete(providerId)
        return next
      })
      addToast({ type: 'success', message: '已取消关注', duration: 3000 })
      queryClient.invalidateQueries({ queryKey: ['social-providers'] })
      queryClient.invalidateQueries({ queryKey: ['social-follower-configs'] })
    },
    onError: (err: Error) => addToast({ type: 'error', message: '取消关注失败: ' + err.message, duration: 5000 }),
  })

  const filtered = useMemo(() => {
    return providers.filter((p: Provider) => {
      const name = p.provider_name || `Provider #${p.provider_id}`
      return !filter || name.toLowerCase().includes(filter.toLowerCase())
    })
  }, [providers, filter])

  const toggleFollow = (p: Provider) => {
    if (following.has(p.provider_id)) {
      unfollowMut.mutate(p.provider_id)
    } else {
      followMut.mutate(p.provider_id)
    }
  }

  if (isLoading) {
    return (
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-56 rounded-xl" />
        ))}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row gap-3 items-start sm:items-center justify-between">
        <div className="relative w-full sm:w-80">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[#666]" />
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="搜索信号源"
            className="pl-9 bg-[#111] border-[#2a2a2a]"
          />
        </div>
        <div className="text-xs text-[#888]">
          共 {providers.length} 个信号源 · 最新 {signals.length} 条信号
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {filtered.length === 0 && (
          <div className="col-span-full text-center py-12 text-[#666]">未找到信号源</div>
        )}
        {filtered.map((p: Provider) => {
          const name = p.provider_name || `Provider #${p.provider_id}`
          const isFollowing = following.has(p.provider_id)
          const winRate = (p.win_rate || 0) * 100
          return (
            <div
              key={p.provider_id}
              className="rounded-xl border border-[#1c1c1c] bg-[#111] p-5 hover:border-[#333] transition-colors flex flex-col"
            >
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-full bg-[#faad14]/10 flex items-center justify-center text-[#faad14] text-sm font-bold">
                    {name.slice(0, 2).toUpperCase()}
                  </div>
                  <div>
                    <h3 className="text-sm font-semibold text-white">{name}</h3>
                    <div className="text-[10px] text-[#666] mt-0.5">
                      {p.total_signals} 信号 · {p.follower_count} 关注
                    </div>
                  </div>
                </div>
                {p.monthly_fee > 0 && (
                  <Badge variant="warning" className="text-[10px]">${p.monthly_fee}/月</Badge>
                )}
              </div>

              <div className="grid grid-cols-3 gap-2 mb-4">
                <StatBox icon={<TrendingUp className="w-3 h-3" />} label="胜率" value={`${winRate.toFixed(1)}%`} />
                <StatBox icon={<Activity className="w-3 h-3" />} label="平均收益" value={`${(p.avg_return_pct || 0).toFixed(2)}%`} />
                <StatBox icon={<Shield className="w-3 h-3" />} label="最大回撤" value={`${(p.max_drawdown_pct || 0).toFixed(2)}%`} />
              </div>

              <div className="mt-auto pt-4 border-t border-[#1c1c1c] flex items-center gap-2">
                <button
                  onClick={() => toggleFollow(p)}
                  disabled={followMut.isPending || unfollowMut.isPending}
                  className={cn(
                    'flex-1 py-2 rounded-lg text-xs font-medium transition-colors border',
                    isFollowing
                      ? 'bg-[#1c1c1c] border-[#2a2a2a] text-[#888] hover:text-[#f5222d]'
                      : 'bg-[#1890ff] border-[#1890ff] text-white hover:opacity-90'
                  )}
                >
                  {isFollowing ? '已关注' : '关注'}
                </button>
                <Button
                  size="sm"
                  variant="outline"
                  leftIcon={<Bell className="w-3 h-3" />}
                  onClick={() => setConfigProvider(p)}
                >
                  自动跟单
                </Button>
              </div>
            </div>
          )
        })}
      </div>

      {configProvider && (
        <AIFollowConfigModal
          provider={configProvider}
          open={!!configProvider}
          onClose={() => setConfigProvider(null)}
          onSaved={() => {
            queryClient.invalidateQueries({ queryKey: ['social-follower-configs'] })
          }}
        />
      )}
    </div>
  )
}

function StatBox({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5 text-center">
      <div className="flex items-center justify-center gap-1.5 text-[#666] mb-1">
        {icon}
        <span className="text-[10px]">{label}</span>
      </div>
      <div className="text-sm font-semibold text-[#e0e0e0]">{value}</div>
    </div>
  )
}

export default AIBotProviderMarket
