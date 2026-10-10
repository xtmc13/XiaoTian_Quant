import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Backtest } from '../Backtest'

vi.mock('@/lib/api', () => ({
  backtestApi: { run: vi.fn() },
  indicatorApi: { experiment: { run: vi.fn() } },
}))

// Tick 级回测面板（i18n 词条 + 轮询）不在本测试范围
vi.mock('@/components/backtest/TickBacktestPanel', () => ({ TickBacktestPanel: () => null }))

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/backtest']}>
        <Backtest />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('Backtest 智能补仓/移动止盈参数区（用户可见命名）', () => {
  it('默认 sma_cross（非壳策略）不渲染参数区；切壳策略后显示大白话命名', () => {
    renderPage()
    expect(screen.queryByText('智能补仓/移动止盈参数')).toBeNull()
    fireEvent.change(screen.getByLabelText('策略类型'), { target: { value: 'martin_trend' } })
    expect(screen.getByText('高级参数（智能补仓/移动止盈）')).toBeTruthy()
    expect(screen.getByText('智能补仓/移动止盈参数')).toBeTruthy()
    // 旧黑话不再出现
    expect(screen.queryByText(/CRA/)).toBeNull()
  })
})
