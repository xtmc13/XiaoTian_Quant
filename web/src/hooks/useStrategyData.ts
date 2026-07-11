import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { strategyApi } from '@/lib/api'
import type { StrategyItem } from '@/types'

export function useStrategyData() {
  const queryClient = useQueryClient()

  const { data: strategies, isLoading } = useQuery({
    queryKey: ['strategies'],
    queryFn: () => strategyApi.list(),
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
      return res
    },
  })

  const batchStopMut = useMutation({
    mutationFn: (ids: string[]) => strategyApi.batchStop(ids),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      return res
    },
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
    create: createMut.mutate,
    update: updateMut.mutate,
    batchStart: batchStartMut.mutateAsync,
    batchStop: batchStopMut.mutateAsync,
    batchDelete: batchDeleteMut.mutateAsync,
    invalidate: () => queryClient.invalidateQueries({ queryKey: ['strategies'] }),
  }
}
