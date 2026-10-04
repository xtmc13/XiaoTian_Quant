import { describe, it, expect, vi } from 'vitest'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { Strategy } from '../Strategy'

// 策略管理页行内互通入口：编辑参数 → /create 编辑模式；状态徽标 → 机器人中心详情
const items = [
  {
    id: 's1',
    name: '合约趋势',
    strategy_type: 'cra_contract',
    symbol: 'SOLUSDT',
    timeframe: '15m',
    status: 'running',
    market_type: 'swap',
    total_pnl: -2.55,
    total_pnl_percent: -0.85,
  },
  {
    id: 's2',
    name: '现货观察',
    strategy_type: 'smart_money',
    symbol: 'BTCUSDT',
    timeframe: '4h',
    status: 'stopped',
    market_type: 'spot',
    total_pnl: null,
  },
]

vi.mock('@/lib/api', () => ({
  strategyApi: {
    list: vi.fn(() => Promise.resolve(items)),
    start: vi.fn(() => Promise.resolve({})),
    stop: vi.fn(() => Promise.resolve({})),
    delete: vi.fn(() => Promise.resolve({})),
  },
}))

function Probe() {
  const loc = useLocation()
  return <div data-testid="loc">{loc.pathname + loc.search}</div>
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Strategy />
        <Probe />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('策略管理页互通入口', () => {
  it('行内编辑按钮跳创建表单编辑模式（swap 映射 contract）', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '编辑 合约趋势' }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=contract&id=s1')
  })

  it('现货项编辑 market=spot', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '编辑 现货观察' }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=spot&id=s2')
  })

  it('状态徽标点击直达机器人中心对应卡片（?bot=id）', async () => {
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: '在机器人中心查看 合约趋势' }))
    expect(screen.getByTestId('loc').textContent).toBe('/bots?bot=s1')
  })
})
