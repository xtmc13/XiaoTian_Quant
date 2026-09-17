import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { strategyApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import type { StrategyBatchResult, StrategyItem } from '@/types'

/** 批量启停结果汇总 toast（P1-8）：成功 X / 失败 Y，Y>0 时逐条附原因。 */
function toastBatchSummary(action: string, res: StrategyBatchResult | undefined) {
  if (!res) return
  const okCount = res.started ?? res.stopped ?? 0
  const failed = res.failed ?? 0
  if (failed > 0) {
    toast('warning', `批量${action}完成：成功 ${okCount} / 失败 ${failed}`)
    const reasons = (res.results || [])
      .filter((r) => !r.ok)
      .slice(0, 5)
      .map((r) => `${r.name || r.id}：${r.error || '未知原因'}`)
    if (reasons.length > 0) {
      toast('error', `失败原因：${reasons.join('；')}`)
    }
  } else {
    toast('success', `批量${action}完成：成功 ${okCount} 个`)
  }
}

export function useStrategyData() {
  const queryClient = useQueryClient()

  const { data: strategies, isLoading } = useQuery({
    queryKey: ['strategies', 'strategy'],
    // 归属判别收归后端 isBotItem：策略管理页只要策略实验室侧（kind=strategy）。
    queryFn: () => strategyApi.list({ kind: 'strategy' }),
  })

  const startMut = useMutation({
    mutationFn: (id: string) => strategyApi.start(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  })

  const stopMut = useMutation({
    mutationFn: (id: string) => strategyApi.stop(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => strategyApi.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  })

  const createMut = useMutation({
    mutationFn: (data: Partial<StrategyItem>) => strategyApi.create(data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  })

  const updateMut = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<StrategyItem> }) => strategyApi.update(id, data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  })

  const batchStartMut = useMutation({
    mutationFn: (ids: string[]) => strategyApi.batchStart(ids),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      toastBatchSummary('启动', res)
    },
    onError: (e) => toast('error', '批量启动失败: ' + (e instanceof Error ? e.message : String(e))),
  })

  const batchStopMut = useMutation({
    mutationFn: (ids: string[]) => strategyApi.batchStop(ids),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      toastBatchSummary('停止', res)
    },
    onError: (e) => toast('error', '批量停止失败: ' + (e instanceof Error ? e.message : String(e))),
  })

  const batchDeleteMut = useMutation({
    mutationFn: (ids: string[]) => strategyApi.batchDelete(ids),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      return res
    },
  })

  const list = (strategies || []) as StrategyItem[]

  return {
    strategies: list,
    isLoading,
    start: startMut.mutate,
    stop: stopMut.mutate,
    delete: deleteMut.mutate,
    // async 变体：面板需要 await 结果并检查 forced_paper 标记；错误继续上抛。
    create: (data: Partial<StrategyItem>) => createMut.mutateAsync(data),
    update: updateMut.mutate,
    // P0-3：async 变体供页面捕获失败原因并持久展示。
    startAsync: startMut.mutateAsync,
    stopAsync: stopMut.mutateAsync,
    batchStart: async (ids: string[]) => {
      try {
        return await batchStartMut.mutateAsync(ids)
      } catch {
        return undefined
      }
    },
    batchStop: async (ids: string[]) => {
      try {
        return await batchStopMut.mutateAsync(ids)
      } catch {
        return undefined
      }
    },
    batchDelete: batchDeleteMut.mutateAsync,
    invalidate: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  }
}
