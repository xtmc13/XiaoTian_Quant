import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/tick'
import { TickDataPanel, LocalBarsPanel } from '@/components/data/TickPanels'
import { dataApi } from '@/lib/api'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    dataApi: {
      ...actual.dataApi,
      tickDownload: vi.fn(),
      ticks: vi.fn(),
      tickInfo: vi.fn(),
      bars: vi.fn(),
    },
  }
})

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    </I18nProvider>
  )
}

const tickItem = {
  symbol: 'BTCUSDT',
  exchange: 'binance',
  bid: 70100.1,
  ask: 70100.5,
  bid_size: 0.5,
  ask_size: 0.4,
  last: 70100.3,
  volume: 0.12,
  timestamp: 1700000000000,
}

describe('TickDataPanel', () => {
  beforeEach(() => vi.clearAllMocks())

  it('下载按钮调用 /data/ticks/download 并透出后端错误说明', async () => {
    vi.mocked(dataApi.tickDownload).mockRejectedValue(
      new Error('network tick download is disabled — please import tick data manually')
    )
    const { toast } = await import('@/lib/useToast')
    render(<TickDataPanel />, { wrapper })

    fireEvent.click(screen.getByText('开始下载'))
    await waitFor(() =>
      expect(dataApi.tickDownload).toHaveBeenCalledWith(
        expect.objectContaining({ symbol: 'BTCUSDT' })
      )
    )
    await waitFor(() =>
      expect(vi.mocked(toast)).toHaveBeenCalledWith('error', expect.stringContaining('network tick download is disabled'))
    )
  })

  it('查询信息渲染 tick 计数与时间范围', async () => {
    vi.mocked(dataApi.tickInfo).mockResolvedValue({
      symbol: 'BTCUSDT',
      count: 12345,
      earliest: 1700000000000,
      latest: 1700003600000,
      earliest_time: '2023-11-14T22:13:20Z',
      latest_time: '2023-11-14T23:13:20Z',
    })
    render(<TickDataPanel />, { wrapper })

    fireEvent.click(screen.getByText('查询信息'))
    await waitFor(() => expect(screen.getByText('12,345')).toBeTruthy())
    expect(dataApi.tickInfo).toHaveBeenCalledWith('BTCUSDT')
  })

  it('Tick 查询渲染结果表格', async () => {
    vi.mocked(dataApi.ticks).mockResolvedValue({
      symbol: 'BTCUSDT',
      start: 0,
      end: 1700003600000,
      count: 1,
      ticks: [tickItem],
    })
    render(<TickDataPanel />, { wrapper })

    fireEvent.click(screen.getByText('查询'))
    await waitFor(() => expect(screen.getByText('共 1 条')).toBeTruthy())
    expect(screen.getByText('70,100.30')).toBeTruthy()
    expect(dataApi.ticks).toHaveBeenCalledWith('BTCUSDT', expect.objectContaining({ limit: 200 }))
  })
})

describe('LocalBarsPanel（/data/bars）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('查询渲染最近 K 线行', async () => {
    vi.mocked(dataApi.bars).mockResolvedValue({
      symbol: 'BTCUSDT',
      interval: '1h',
      from: 0,
      to: 1,
      count: 2,
      bars: [
        { time: 1700000000000, open: 70000, high: 70100, low: 69900, close: 70050, volume: 12.5 },
        { time: 1700003600000, open: 70050, high: 70200, low: 70000, close: 70150, volume: 9.5 },
      ],
    })
    render(<LocalBarsPanel />, { wrapper })

    fireEvent.click(screen.getByText('查询'))
    await waitFor(() => expect(screen.getByText(/共 2 根/)).toBeTruthy())
    expect(screen.getByText('70,150.00')).toBeTruthy()
    expect(dataApi.bars).toHaveBeenCalledWith('BTCUSDT', '1h', expect.any(Number), expect.any(Number))
  })

  it('查询失败显示空态并 toast 警告', async () => {
    vi.mocked(dataApi.bars).mockRejectedValue(new Error('no data found for BTCUSDT 1h'))
    const { toast } = await import('@/lib/useToast')
    render(<LocalBarsPanel />, { wrapper })

    fireEvent.click(screen.getByText('查询'))
    await waitFor(() => expect(screen.getByText('本地无该区间 K 线数据')).toBeTruthy())
    expect(vi.mocked(toast)).toHaveBeenCalledWith('warning', expect.stringContaining('no data found'))
  })
})
