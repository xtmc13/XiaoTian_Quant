import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useWebSocket } from '@/hooks/useWebSocket'
import type { AIBotInstance } from '@/types'

interface BotStatusUpdate {
  type: 'bot_status_update'
  channel: 'ai-bot'
  data: {
    bot_id: string
    user_id: number
    status: string
    total_equity: number
    unrealized_pnl: number
    realized_pnl: number
    total_return_pct: number
    max_drawdown_pct: number
    sharpe_ratio: number
    win_rate: number
    total_trades: number
    timestamp: number
  }
}

export function useAIBotRealtime(currentUserId?: number) {
  const queryClient = useQueryClient()
  const { on, send, isConnected } = useWebSocket('/ws/v2', {
    reconnect: true,
    heartbeatMsg: { type: 'ping' },
  })

  const subscribedRef = useRef(false)

  useEffect(() => {
    if (!isConnected || subscribedRef.current) return
    send({ action: 'subscribe', channels: ['ai-bot'] })
    subscribedRef.current = true
  }, [isConnected, send])

  useEffect(() => {
    const unsubscribe = on('bot_status_update', (payload: unknown) => {
      const msg = payload as BotStatusUpdate
      const data = msg.data
      if (!data || (currentUserId != null && data.user_id !== currentUserId)) return

      queryClient.setQueryData(
        ['ai-bots', 'instances'],
        (old: AIBotInstance[] | undefined) => {
          if (!old) return old
          return old.map((bot) => {
            if (bot.id !== data.bot_id) return bot
            return {
              ...bot,
              status: data.status as AIBotInstance['status'],
              total_equity: data.total_equity,
              unrealized_pnl: data.unrealized_pnl,
              realized_pnl: data.realized_pnl,
              total_return_pct: data.total_return_pct,
              max_drawdown_pct: data.max_drawdown_pct,
              sharpe_ratio: data.sharpe_ratio,
              win_rate: data.win_rate,
              total_trades: data.total_trades,
            }
          })
        }
      )
    })
    return () => {
      unsubscribe()
      subscribedRef.current = false
    }
  }, [on, queryClient, currentUserId])

  return { isConnected }
}
