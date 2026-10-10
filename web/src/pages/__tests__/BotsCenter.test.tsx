import { describe, it, expect, vi } from 'vitest'
import React from 'react'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
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

describe('新建机器人向导（合约类型已精简）', () => {
  it('合约策略只剩四个真实类型，点击直达创建表单', async () => {
    renderBots('/bots')
    fireEvent.click(screen.getByRole('button', { name: /新建机器人/ }))
    // 第 1 级：点合约策略卡（名称含描述文案以区别筛选 chips）
    fireEvent.click(await screen.findByRole('button', { name: /合约策略 合约网格/ }))
    // 第 2 级：只剩 合约网格/高频策略/首尾套利/主力行为
    await screen.findByText('第 2 步：选择策略类型')
    for (const v of ['cra_contract', 'high_frequency', 'head_tail_arbitrage', 'smart_money']) {
      expect(screen.getByText(v)).toBeTruthy()
    }
    for (const gone of ['trend_long', 'trend_short', 'counter_stable', 'counter_safe']) {
      expect(screen.queryByText(gone)).toBeNull()
    }
    // 点合约网格 → 直达创建表单对应类型
    fireEvent.click(screen.getByRole('button', { name: /合约网格 cra_contract/ }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=contract&type=cra_contract')
  })

  it('指标策略节：四张快捷卡（CRA 壳 + 预选开仓指标），跳转 URL 带 type+indicator', async () => {
    renderBots('/bots')
    fireEvent.click(screen.getByRole('button', { name: /新建机器人/ }))
    fireEvent.click(await screen.findByRole('button', { name: /合约策略 合约网格/ }))
    await screen.findByText('第 2 步：选择策略类型')
    // 指标策略节标题 + 四张卡（币富模型：含补仓/移动止盈的 CRA 壳，非裸类型）
    expect(screen.getByText('指标策略（含补仓/移动止盈）')).toBeTruthy()
    for (const label of ['EMA 策略', 'MACD 策略', 'RSI 策略', '布林带策略']) {
      expect(screen.getByRole('button', { name: new RegExp(label) })).toBeTruthy()
    }
    // 点 EMA 策略 → type=cra_contract + indicator=ema_cross
    fireEvent.click(screen.getByRole('button', { name: /EMA 策略 ema_cross/ }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=contract&type=cra_contract&indicator=ema_cross')
  })

  it('现货向导同样渲染指标策略节（type=cra_spot）', async () => {
    renderBots('/bots')
    fireEvent.click(screen.getByRole('button', { name: /新建机器人/ }))
    fireEvent.click(await screen.findByRole('button', { name: /现货策略 现货网格/ }))
    await screen.findByText('第 2 步：选择策略类型')
    expect(screen.getByText('指标策略（含补仓/移动止盈）')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /布林带策略 bollinger/ }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=spot&type=cra_spot&indicator=bollinger')
  })
})
