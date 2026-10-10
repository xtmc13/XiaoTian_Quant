import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useStrategyCreateForm } from '../StrategyCreateForm'
import * as useStrategyDataModule from '@/hooks/useStrategyData'
import { configApi } from '@/lib/api'

vi.mock('@/hooks/useStrategyData', () => ({
  useStrategyData: vi.fn(),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    configApi: { exchangesConfigured: vi.fn() },
  }
})

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

/**
 * M8（2026-10-10）：编辑现货网格策略回填。/create?id= 首渲染时 editingItem
 * 尚未加载（initialConfig=undefined），修复前回填 effect 是 mount-only，
 * 数据到达后不再执行 → 区间/格数/每格金额永远回默认、spotGridError 硬拦保存。
 * 修复后 initialConfig 到达即回填一次，且不覆盖用户后续编辑。
 */
describe('现货网格编辑回填（M8）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useStrategyDataModule.useStrategyData).mockReturnValue({
      create: vi.fn(),
    } as unknown as ReturnType<typeof useStrategyDataModule.useStrategyData>)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({})
  })

  const gridConfig = {
    price_lower: 40000,
    price_upper: 50000,
    grid_count: 7,
    per_grid_amount: 30,
  }

  it('initialConfig 异步到达后回填网格四字段，spotGridError 解除', () => {
    const onSaved = vi.fn()
    const { result, rerender } = renderHook(
      ({ initialConfig }: { initialConfig?: Record<string, unknown> }) =>
        useStrategyCreateForm('spot', onSaved, { editId: 'edit-1', initialType: 'cra_spot', initialConfig }),
      { wrapper, initialProps: { initialConfig: undefined } as { initialConfig?: Record<string, unknown> } }
    )

    // 首渲染（editingItem 未加载）：网格为空，校验硬拦。
    expect(result.current.spotGrid.priceLower).toBe('')
    expect(result.current.spotGridError).not.toBeNull()

    // 数据到达：回填一次。
    rerender({ initialConfig: gridConfig })
    expect(result.current.spotGrid).toEqual({
      priceLower: '40000',
      priceUpper: '50000',
      gridCount: 7,
      perGridAmount: 30,
    })
    expect(result.current.spotGridError).toBeNull()
  })

  it('mount 时已有 initialConfig（缓存命中）直接回填', () => {
    const { result } = renderHook(
      () => useStrategyCreateForm('spot', vi.fn(), { editId: 'edit-2', initialType: 'cra_spot', initialConfig: gridConfig }),
      { wrapper }
    )
    expect(result.current.spotGrid.priceLower).toBe('40000')
    expect(result.current.spotGrid.gridCount).toBe(7)
    expect(result.current.spotGridError).toBeNull()
  })

  it('回填后用户编辑不被同一 config 实例重复覆盖', () => {
    const onSaved = vi.fn()
    const { result, rerender } = renderHook(
      ({ initialConfig }: { initialConfig?: Record<string, unknown> }) =>
        useStrategyCreateForm('spot', onSaved, { editId: 'edit-3', initialType: 'cra_spot', initialConfig }),
      { wrapper, initialProps: { initialConfig: undefined } as { initialConfig?: Record<string, unknown> } }
    )
    rerender({ initialConfig: gridConfig })
    expect(result.current.spotGrid.priceLower).toBe('40000')

    act(() => result.current.setSpotGrid({ priceLower: '38000' }))
    // 同一 config 引用再渲染（React Query 轮询/父组件重渲染）不得覆盖用户输入。
    rerender({ initialConfig: gridConfig })
    expect(result.current.spotGrid.priceLower).toBe('38000')
  })

  it('缺自定义键的旧记录按 CRA 键回退（区间留空仍拦截）', () => {
    const { result } = renderHook(
      () =>
        useStrategyCreateForm('spot', vi.fn(), {
          editId: 'edit-4',
          initialType: 'cra_spot',
          initialConfig: { first_order_amount: 88, order_count: 6 },
        }),
      { wrapper }
    )
    expect(result.current.spotGrid.gridCount).toBe(6)
    expect(result.current.spotGrid.perGridAmount).toBe(88)
    expect(result.current.spotGrid.priceLower).toBe('')
    expect(result.current.spotGridError).not.toBeNull()
  })
})
