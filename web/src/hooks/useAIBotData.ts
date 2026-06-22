import { useMemo, useState, useCallback } from 'react'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { aiBotApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import type { AIBotInstance } from '@/types'

export function useAIBotData() {
  const queryClient = useQueryClient()
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)

  const { data: instances = [], isLoading } = useQuery({
    queryKey: ['ai-bots', 'instances'],
    queryFn: () => aiBotApi.list(),
    refetchInterval: 30_000,
  })

  const { data: catalog = [], isLoading: catalogLoading } = useQuery({
    queryKey: ['ai-bots', 'catalog'],
    queryFn: () => aiBotApi.catalog(),
    staleTime: 60_000,
  })

  const { data: subscriptions = [] } = useQuery({
    queryKey: ['ai-bots', 'subscriptions'],
    queryFn: () => aiBotApi.subscriptions(),
    staleTime: 60_000,
  })

  const kpi = useMemo(() => {
    const running = instances.filter((b: AIBotInstance) => b.status === 'running').length
    const stopped = instances.filter((b: AIBotInstance) => b.status === 'stopped').length
    const totalPnl = instances.reduce((sum, b) => sum + (b.unrealized_pnl || 0), 0)
    const best = instances.reduce((max, b) => {
      const ret = b.total_return_pct || 0
      return ret > max ? ret : max
    }, 0)
    return { running, stopped, total: instances.length, totalPnl, best }
  }, [instances])

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['ai-bots', 'instances'] })
  }, [queryClient])

  const withLoading = useCallback(async (id: string, fn: () => Promise<void>) => {
    setActionLoadingId(id)
    try {
      await fn()
      invalidate()
    } catch (err: any) {
      toast('error', err?.message || '操作失败')
    } finally {
      setActionLoadingId(null)
    }
  }, [invalidate])

  const startBot = useCallback((bot: AIBotInstance) => {
    withLoading(bot.id, () => aiBotApi.start(bot.id).then(() => toast('success', '机器人已启动')))
  }, [withLoading])

  const stopBot = useCallback((bot: AIBotInstance) => {
    withLoading(bot.id, () => aiBotApi.stop(bot.id).then(() => toast('success', '机器人已停止')))
  }, [withLoading])

  const pauseBot = useCallback((bot: AIBotInstance) => {
    withLoading(bot.id, () => aiBotApi.pause(bot.id).then(() => toast('success', '机器人已暂停')))
  }, [withLoading])

  const resumeBot = useCallback((bot: AIBotInstance) => {
    withLoading(bot.id, () => aiBotApi.resume(bot.id).then(() => toast('success', '机器人已恢复')))
  }, [withLoading])

  const deleteBot = useCallback((bot: AIBotInstance) => {
    if (bot.status === 'running') {
      toast('info', '请先停止机器人')
      return
    }
    if (!confirm(`确定删除 "${bot.name}" 吗？`)) return
    withLoading(bot.id, () => aiBotApi.delete(bot.id).then(() => toast('success', '已删除')))
  }, [withLoading])

  const cloneBot = useCallback((bot: AIBotInstance) => {
    if (!confirm(`克隆机器人 "${bot.name}"？`)) return
    withLoading(bot.id, () => aiBotApi.clone(bot.id).then(() => toast('success', '克隆成功')))
  }, [withLoading])

  const updateBot = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<AIBotInstance> }) => aiBotApi.update(id, data),
    onSuccess: () => {
      invalidate()
      toast('success', '机器人配置已更新')
    },
    onError: (err: Error) => toast('error', err.message || '更新失败'),
  })

  const batchStart = useMutation({
    mutationFn: (ids: string[]) => aiBotApi.batchStart(ids),
    onSuccess: (res) => {
      invalidate()
      toast('success', `批量启动 ${res.started} 个机器人`)
    },
    onError: (err: Error) => toast('error', err.message || '批量启动失败'),
  })

  const batchStop = useMutation({
    mutationFn: (ids: string[]) => aiBotApi.batchStop(ids),
    onSuccess: (res) => {
      invalidate()
      toast('success', `批量停止 ${res.stopped} 个机器人`)
    },
    onError: (err: Error) => toast('error', err.message || '批量停止失败'),
  })

  const batchDelete = useMutation({
    mutationFn: (ids: string[]) => aiBotApi.batchDelete(ids),
    onSuccess: (res) => {
      invalidate()
      toast('success', `批量删除 ${res.deleted} 个机器人`)
    },
    onError: (err: Error) => toast('error', err.message || '批量删除失败'),
  })

  const createBot = useMutation({
    mutationFn: aiBotApi.create,
    onSuccess: () => {
      invalidate()
      toast('success', '机器人创建成功')
    },
    onError: (err: Error) => toast('error', err.message || '创建失败'),
  })

  return {
    instances,
    catalog,
    subscriptions,
    isLoading,
    catalogLoading,
    kpi,
    actionLoadingId,
    startBot,
    stopBot,
    pauseBot,
    resumeBot,
    deleteBot,
    cloneBot,
    updateBot,
    batchStart,
    batchStop,
    batchDelete,
    createBot,
  }
}
