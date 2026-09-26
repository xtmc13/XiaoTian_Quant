import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { ShareCardModal } from '../ShareCardModal'
import type { ShareTradeCard, ShareBacktestCard } from '@/lib/api'

const tradeCard: ShareTradeCard = {
  kind: 'trade',
  id: 'pos_1',
  nickname: 'E***',
  amount_mode: 'pct',
  share_url: '/share/trade/pos_1',
  generated_at: 1727000000000,
  symbol: 'BTCUSDT',
  side: 'LONG',
  exchange: 'BINANCE',
  entry_price: 60000,
  exit_price: 66000,
  quantity: 0.5,
  cost_basis: 30000,
  pnl: 3000,
  pnl_pct: 10,
  opened_at: 1726990000000,
  closed_at: 1727000000000,
  hold_ms: 10000000,
}

const backtestCard: ShareBacktestCard = {
  kind: 'backtest',
  id: 'bt_1',
  nickname: 'E***',
  amount_mode: 'pct',
  share_url: '/share/backtest/bt_1',
  generated_at: 1727000000000,
  name: '组合Alpha',
  strategy: 'portfolio',
  total_return_pct: 25,
  max_drawdown_pct: 8.5,
  sharpe_ratio: 1.8,
  sortino_ratio: 2.1,
  win_rate: 62.5,
  profit_factor: 1.6,
  total_trades: 42,
  initial_capital: 10000,
  final_equity: 12500,
  start_time: 0,
  end_time: 0,
  created_at: 0,
}

describe('ShareCardModal', () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    })
  })

  it('renders nothing when card is null', () => {
    const { container } = render(<ShareCardModal card={null} onClose={() => {}} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders trade card metrics (symbol/side/entry/exit/pnl)', () => {
    render(<ShareCardModal card={tradeCard} onClose={() => {}} />)
    expect(screen.getByText('交易收益分享卡')).toBeTruthy()
    expect(screen.getByText(/BTCUSDT LONG/)).toBeTruthy()
    expect(screen.getAllByText('+10.00%').length).toBeGreaterThan(0)
    expect(screen.getByText('60000')).toBeTruthy()
    expect(screen.getByText('66000')).toBeTruthy()
    expect(screen.getByText('E***')).toBeTruthy()
  })

  it('renders backtest card metrics', () => {
    render(<ShareCardModal card={backtestCard} onClose={() => {}} />)
    expect(screen.getByText('回测收益分享卡')).toBeTruthy()
    expect(screen.getByText('组合Alpha')).toBeTruthy()
    expect(screen.getByText('+25.00%')).toBeTruthy()
  })

  it('copies trade share text to clipboard', async () => {
    render(<ShareCardModal card={tradeCard} onClose={() => {}} />)
    fireEvent.click(screen.getByText('复制分享文案'))
    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
        expect.stringContaining('BTCUSDT')
      )
    })
    const text = (navigator.clipboard.writeText as ReturnType<typeof vi.fn>).mock.calls[0][0] as string
    expect(text).toContain('+10.00%')
    expect(text).toContain('E***')
  })

  it('closes on Escape key', () => {
    const onClose = vi.fn()
    render(<ShareCardModal card={tradeCard} onClose={onClose} />)
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })
})
