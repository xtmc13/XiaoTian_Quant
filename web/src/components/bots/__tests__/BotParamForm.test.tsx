import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BotParamForm } from '../BotParamForm'

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('BotParamForm 智能补仓/移动止盈参数区（用户可见命名）', () => {
  it('壳类型（martin_trend）→ 参数区标题为「智能补仓/移动止盈参数」', () => {
    render(<BotParamForm form={{}} setForm={vi.fn()} effectiveType="martin_trend" />, { wrapper })
    expect(screen.getByText('智能补仓/移动止盈参数')).toBeTruthy()
  })

  it('非壳类型（custom）→ 不渲染参数区', () => {
    render(<BotParamForm form={{}} setForm={vi.fn()} effectiveType="custom" />, { wrapper })
    expect(screen.queryByText('智能补仓/移动止盈参数')).toBeNull()
  })
})
