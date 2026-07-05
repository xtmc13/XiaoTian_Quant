import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { StrategyList } from '../StrategyList'
import type { StrategyItem } from '@/types'

const mockStrategies: StrategyItem[] = [
  {
    id: '1',
    name: 'BTC Spot',
    symbol: 'BTCUSDT',
    status: 'running',
    market_type: 'spot',
    strategy_type: 'martin_trend',
  },
  {
    id: '2',
    name: 'ETH Contract',
    symbol: 'ETHUSDT',
    status: 'stopped',
    market_type: 'swap',
    strategy_type: 'wallstreet',
  },
]

const baseProps = {
  strategies: mockStrategies,
  isLoading: false,
  selectedId: null,
  onSelect: vi.fn(),
  onStart: vi.fn(),
  onStop: vi.fn(),
  onEdit: vi.fn(),
  onDelete: vi.fn(),
  onCreate: vi.fn(),
}

describe('StrategyList', () => {
  beforeEach(() => {
    vi.stubGlobal('confirm', () => true)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders spot strategies by default', () => {
    render(<StrategyList {...baseProps} />)
    expect(screen.getByText('BTC Spot')).toBeTruthy()
    expect(screen.queryByText('ETH Contract')).toBeFalsy()
  })

  it('filters by search', () => {
    render(<StrategyList {...baseProps} />)
    fireEvent.change(screen.getByPlaceholderText('搜索策略...'), { target: { value: 'BTC' } })
    expect(screen.getByText('BTC Spot')).toBeTruthy()
    expect(screen.queryByText('ETH Contract')).toBeFalsy()
  })

  it('filters by status', () => {
    render(<StrategyList {...baseProps} />)
    fireEvent.change(screen.getByText('全部').closest('select')!, { target: { value: 'running' } })
    expect(screen.getByText('BTC Spot')).toBeTruthy()
    expect(screen.queryByText('ETH Contract')).toBeFalsy()
  })

  it('filters by market type', () => {
    render(<StrategyList {...baseProps} />)
    fireEvent.change(screen.getByText('现货策略').closest('select')!, { target: { value: 'contract' } })
    expect(screen.queryByText('BTC Spot')).toBeFalsy()
    expect(screen.getByText('ETH Contract')).toBeTruthy()
  })

  it('selects strategy when clicking item', () => {
    render(<StrategyList {...baseProps} />)
    fireEvent.click(screen.getByText('BTC Spot'))
    expect(baseProps.onSelect).toHaveBeenCalledWith('1')
  })

  it('calls onCreate when create button clicked', () => {
    render(<StrategyList {...baseProps} />)
    fireEvent.click(screen.getByLabelText('创建策略'))
    expect(baseProps.onCreate).toHaveBeenCalled()
  })
})
