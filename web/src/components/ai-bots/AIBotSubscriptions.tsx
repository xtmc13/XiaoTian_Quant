import React from 'react'
import { CreditCard, Crown, Percent, Calendar } from 'lucide-react'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { aiBotApi } from '@/lib/api'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from '@/lib/useToast'
import type { AIBotSubscription, AIBotInstance } from '@/types'

interface AIBotSubscriptionsProps {
  subscriptions: AIBotSubscription[]
  instances: AIBotInstance[]
  isLoading?: boolean
}

export const AIBotSubscriptions: React.FC<AIBotSubscriptionsProps> = ({
  subscriptions,
  instances,
  isLoading,
}) => {
  const queryClient = useQueryClient()

  const handleCancel = (id: number) => {
    if (!confirm('确定取消该订阅吗？')) return
    aiBotApi.cancelSubscription(id).then(() => {
      queryClient.invalidateQueries({ queryKey: ['ai-bots', 'subscriptions'] })
      toast('success', '订阅已取消')
    }).catch((err: Error) => {
      toast('error', err.message || '取消失败')
    })
  }

  const instanceMap = React.useMemo(() => {
    const map: Record<string, AIBotInstance> = {}
    instances.forEach((b) => { map[b.id] = b })
    return map
  }, [instances])

  if (isLoading) {
    return (
      <div className="space-y-4">
        {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-24 rounded-xl" />)}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <SectionCard title="我的订阅">
        {subscriptions.length === 0 ? (
          <div className="text-center py-12 text-[#666]">
            暂无订阅记录，从机器人市场部署收费机器人后会自动创建
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-[#666] border-b border-[#1c1c1c]">
                  <th className="text-left py-2 px-3">机器人</th>
                  <th className="text-left py-2 px-3">费用类型</th>
                  <th className="text-right py-2 px-3">费率</th>
                  <th className="text-right py-2 px-3">下期账单</th>
                  <th className="text-center py-2 px-3">状态</th>
                  <th className="text-right py-2 px-3">操作</th>
                </tr>
              </thead>
              <tbody>
                {subscriptions.map((sub) => {
                  const bot = instanceMap[sub.bot_instance_id]
                  return (
                    <tr key={sub.id} className="border-b border-[#1c1c1c]/50 hover:bg-[#0a0a0a]">
                      <td className="py-3 px-3">
                        <div className="flex items-center gap-2">
                          <Crown className="w-4 h-4 text-[#faad14]" />
                          <span className="text-white font-medium">{bot?.name || sub.bot_instance_id}</span>
                        </div>
                      </td>
                      <td className="py-3 px-3">
                        <div className="flex items-center gap-1.5">
                          {sub.fee_type === 'profit_share' ? (
                            <>
                              <Percent className="w-3 h-3 text-[#1890ff]" />
                              <span className="text-[#888]">盈利分成</span>
                            </>
                          ) : (
                            <>
                              <CreditCard className="w-3 h-3 text-[#52c41a]" />
                              <span className="text-[#888]">月费</span>
                            </>
                          )}
                        </div>
                      </td>
                      <td className="py-3 px-3 text-right text-[#888]">
                        {sub.fee_type === 'profit_share'
                          ? `${sub.fee_percent}%`
                          : `$${sub.monthly_fee}/月`}
                      </td>
                      <td className="py-3 px-3 text-right text-[#888]">
                        <div className="flex items-center justify-end gap-1">
                          <Calendar className="w-3 h-3" />
                          {sub.next_billing_at
                            ? new Date(sub.next_billing_at * 1000).toLocaleDateString()
                            : '-'}
                        </div>
                      </td>
                      <td className="py-3 px-3 text-center">
                        <Badge variant={sub.status === 'active' ? 'success' : 'neutral'} className="text-[10px]">
                          {sub.status === 'active' ? '生效中' : sub.status === 'cancelled' ? '已取消' : '已过期'}
                        </Badge>
                      </td>
                      <td className="py-3 px-3 text-right">
                        {sub.status === 'active' && (
                          <Button
                            size="sm"
                            variant="ghost"
                            className="text-[#f5222d] hover:bg-[#f5222d]/10"
                            onClick={() => handleCancel(sub.id)}
                          >
                            取消
                          </Button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </SectionCard>
    </div>
  )
}

export default AIBotSubscriptions
