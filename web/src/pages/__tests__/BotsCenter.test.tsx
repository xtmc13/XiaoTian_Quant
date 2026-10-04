import { describe, it, expect, vi } from 'vitest'
import React from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { BotsCenter } from '../bots/BotsCenter'

// /bots?bot=<id> 深链：自动打开对应卡片详情弹层，消费后清参
const strategyItem = {
  id: 's1',
  name: '主力行为-paper观察',
  strategy_type: 'smart_money',
  symbol: 'BTCUSDT',
  timeframe: '4h',
  status: 'running',
  market_type: 'spot',
  total_pnl: 0,
}

vi.mock('@/lib/api', () => ({
  gridApi: { list: vi.fn(() => Promise.resolve([])) },
  strategyApi: {
    list: vi.fn((opts?: { kind?: string }) => Promise.resolve(opts?.kind === 'strategy' ? [strategyItem] : [])),
    start: vi.fn(() => Promise.resolve({})),
    stop: vi.fn(() => Promise.resolve({})),
    delete: vi.fn(() => Promise.resolve({})),
  },
  strategyConfigApi: {
    createMartin: vi.fn(),
    createWallStreet: vi.fn(),
    updateMartin: vi.fn(),
    updateWallStreet: vi.fn(),
  },
}))
// 详情弹层内的重组件打桩（5s 轮询/图表不在本测试范围）
vi.mock('@/components/strategy/RuntimePanel', () => ({ RuntimePanel: () => <div data-testid="runtime-panel" /> }))
vi.mock('@/components/strategy/StrategyTradeHistory', () => ({ StrategyTradeHistory: () => null }))

function Probe() {
  const loc = useLocation()
  return <div data-testid="loc">{loc.pathname + loc.search}</div>
}

function renderBots(initial: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <BotsCenter />
        <Probe />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('机器人中心 ?bot= 深链', () => {
  it('带 bot 参数进入自动打开详情弹层并清参', async () => {
    renderBots('/bots?bot=s1')
    // 详情弹层（含打桩的 RuntimePanel）自动打开
    await screen.findByTestId('runtime-panel')
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toBe('/bots'))
  })

  it('无参数时不弹详情', async () => {
    renderBots('/bots')
    await screen.findAllByText('主力行为-paper观察')
    expect(screen.queryByTestId('runtime-panel')).toBeNull()
  })

  it('参数指向不存在的 id：不弹详情且清参', async () => {
    renderBots('/bots?bot=ghost')
    await screen.findAllByText('主力行为-paper观察')
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toBe('/bots'))
    expect(screen.queryByTestId('runtime-panel')).toBeNull()
  })
})
