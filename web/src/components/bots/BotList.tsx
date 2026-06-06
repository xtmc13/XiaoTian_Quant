import React from 'react'
import { Bot } from 'lucide-react'
import { VirtualList } from '@/components/VirtualList'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import type { BotItem } from '@/hooks/useBotData'
import { BotCard } from './BotCard'

interface BotListProps {
  bots: BotItem[]
  loading: boolean
  actionLoadingId: string | null
  selectedId: string | null
  onSelect: (bot: BotItem) => void
  onStart: (bot: BotItem) => void
  onStop: (bot: BotItem) => void
  onEdit: (bot: BotItem) => void
  onDelete: (bot: BotItem) => void
  onViewDetail: (bot: BotItem) => void
}

export function BotList({
  bots,
  loading,
  actionLoadingId,
  selectedId,
  onSelect,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onViewDetail,
}: BotListProps) {
  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} variant="rect" height="72px" />
        ))}
      </div>
    )
  }

  if (bots.length === 0) {
    return (
      <EmptyState
        icon={<Bot className="h-6 w-6" />}
        title="暂无交易机器人"
        description="从上方选择一种策略类型创建你的第一个自动化交易机器人，或使用 AI 智能创建。"
      />
    )
  }

  return (
    <div className="space-y-2" style={{ height: 400 }}>
      {bots.length > 20 ? (
        <VirtualList
          items={bots}
          itemHeight={80}
          containerHeight={400}
          overscan={3}
          renderItem={(bot) => (
            <BotCard
              bot={bot}
              isSelected={selectedId === bot.id}
              isActionLoading={actionLoadingId === bot.id}
              onSelect={onSelect}
              onStart={onStart}
              onStop={onStop}
              onEdit={onEdit}
              onDelete={onDelete}
              onViewDetail={onViewDetail}
            />
          )}
        />
      ) : (
        bots.map((bot) => (
          <BotCard
            key={bot.id}
            bot={bot}
            isSelected={selectedId === bot.id}
            isActionLoading={actionLoadingId === bot.id}
            onSelect={onSelect}
            onStart={onStart}
            onStop={onStop}
            onEdit={onEdit}
            onDelete={onDelete}
            onViewDetail={onViewDetail}
          />
        ))
      )}
    </div>
  )
}
