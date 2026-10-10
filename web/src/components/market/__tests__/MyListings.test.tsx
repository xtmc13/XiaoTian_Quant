import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
// 测试环境不经 main.tsx，需显式注册词条（默认语言 zh-CN）
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/market'
import { I18nProvider } from '@/i18n'
import { marketListingApi } from '@/lib/api'
import { MyListings } from '@/components/market/MyListings'

const aiInstance = { id: 'aibot-1', name: 'AI 策略A', symbol: 'BTCUSDT', execution_mode: 'paper' }
const gridBot = {
  id: 'grid-1',
  name: 'ETH 网格',
  symbol: 'ETHUSDT',
  mode: 'long' as const,
  status: 'stopped' as const,
  lower_price: 90,
  upper_price: 110,
  grid_count: 10,
  investment: 10000,
  fee_rate: 0.001,
  realized_pnl: 0,
  total_trades: 0,
  base_qty: 0,
  quote_balance: 0,
  initial_equity: 10000,
  created_at: 0,
  updated_at: 0,
}

vi.mock('@/lib/api', () => ({
  aiBotApi: {
    list: vi.fn(() => Promise.resolve([aiInstance])),
  },
  gridApi: {
    list: vi.fn(() => Promise.resolve([gridBot])),
  },
  marketListingApi: {
    myListings: vi.fn(() => Promise.resolve([])),
    rules: vi.fn(() => Promise.resolve({ min_days: 30, min_trades: 10, max_drawdown_pct: 50 })),
    create: vi.fn(() => Promise.resolve({ id: 'ml-new', status: 'probation' })),
    submit: vi.fn(),
    cancel: vi.fn(),
  },
}))

vi.mock('@/lib/useToast', () => ({
  toast: vi.fn(),
  ToastContainer: () => null,
}))

function renderMyListings() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider>
        <MyListings />
      </I18nProvider>
    </QueryClientProvider>
  )
}

describe('MyListings 上架对象类型切换', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => localStorage.removeItem('xt-locale'))

  it('默认 AI 机器人候选，切换网格机器人后候选列表与占位文案随之切换', async () => {
    renderMyListings()
    fireEvent.click(await screen.findByText('提交考核'))

    // 对象类型切换：AI 机器人 / 网格机器人
    expect(screen.getByText('上架对象类型')).toBeTruthy()
    const gridToggle = screen.getByText('网格机器人')
    expect(screen.getByText('AI 机器人')).toBeTruthy()

    // 默认 ai_bot：候选为 AI 实例
    const select = screen.getAllByRole('combobox')[0]
    expect(screen.getByText('— 选择 AI 机器人实例 —')).toBeTruthy()
    expect(screen.getByText('AI 策略A · BTCUSDT · paper')).toBeTruthy()
    expect(screen.queryByText('ETH 网格 · ETHUSDT · long')).toBeNull()

    // 切换到网格机器人：候选换成网格列表，选择被重置
    fireEvent.click(gridToggle)
    expect(screen.getByText('— 选择网格机器人 —')).toBeTruthy()
    expect(screen.getByText('ETH 网格 · ETHUSDT · long')).toBeTruthy()
    expect(screen.queryByText('AI 策略A · BTCUSDT · paper')).toBeNull()
    expect((select as HTMLSelectElement).value).toBe('')
  })

  it('选网格机器人提交时 create 携带 object_kind/object_id', async () => {
    renderMyListings()
    fireEvent.click(await screen.findByText('提交考核'))
    fireEvent.click(screen.getByText('网格机器人'))
    fireEvent.change(screen.getAllByRole('combobox')[0], { target: { value: 'grid-1' } })
    fireEvent.click(screen.getByText('提交并进入考核期'))
    await waitFor(() =>
      expect(marketListingApi.create).toHaveBeenCalledWith(
        expect.objectContaining({ object_kind: 'grid_bot', object_id: 'grid-1', kind: 'robot', submit: true })
      )
    )
  })

  it('默认路径提交时按 ai_bot 携带实例 id', async () => {
    renderMyListings()
    fireEvent.click(await screen.findByText('提交考核'))
    fireEvent.change(screen.getAllByRole('combobox')[0], { target: { value: 'aibot-1' } })
    fireEvent.click(screen.getByText('提交并进入考核期'))
    await waitFor(() =>
      expect(marketListingApi.create).toHaveBeenCalledWith(
        expect.objectContaining({ object_kind: 'ai_bot', object_id: 'aibot-1', submit: true })
      )
    )
  })
})
