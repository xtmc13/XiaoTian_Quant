import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
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

function Probe() {
  const loc = useLocation()
  return <div data-testid="loc">{loc.pathname + loc.search}</div>
}

// StrategyList 侧栏的指标策略快捷项直接 navigate（useNavigate），渲染需 Router。
function renderList(props: Partial<typeof baseProps> = {}) {
  return render(
    <MemoryRouter initialEntries={['/strategy']}>
      <StrategyList {...baseProps} {...props} />
      <Probe />
    </MemoryRouter>
  )
}

describe('StrategyList', () => {
  beforeEach(() => {
    vi.stubGlobal('confirm', () => true)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders spot strategies by default', () => {
    renderList()
    expect(screen.getByText('BTC Spot')).toBeTruthy()
    expect(screen.queryByText('ETH Contract')).toBeFalsy()
  })

  it('filters by search', () => {
    renderList()
    fireEvent.change(screen.getByPlaceholderText('搜索策略...'), { target: { value: 'BTC' } })
    expect(screen.getByText('BTC Spot')).toBeTruthy()
    expect(screen.queryByText('ETH Contract')).toBeFalsy()
  })

  it('filters by status', () => {
    renderList()
    fireEvent.change(screen.getByText('全部').closest('select')!, { target: { value: 'running' } })
    expect(screen.getByText('BTC Spot')).toBeTruthy()
    expect(screen.queryByText('ETH Contract')).toBeFalsy()
  })

  it('filters by market type', () => {
    renderList()
    fireEvent.change(screen.getByText('现货策略').closest('select')!, { target: { value: 'contract' } })
    expect(screen.queryByText('BTC Spot')).toBeFalsy()
    expect(screen.getByText('ETH Contract')).toBeTruthy()
  })

  it('selects strategy when clicking item', () => {
    renderList()
    fireEvent.click(screen.getByText('BTC Spot'))
    expect(baseProps.onSelect).toHaveBeenCalledWith('1')
  })

  it('calls onCreate when create button clicked', () => {
    renderList()
    fireEvent.click(screen.getByLabelText('创建策略'))
    expect(baseProps.onCreate).toHaveBeenCalled()
  })

  it('指标策略快捷项：渲染四张卡且点击跳 /create?type=cra_spot&indicator=xx（现货）', () => {
    renderList()
    expect(screen.getByText('指标策略（含补仓/移动止盈）')).toBeTruthy()
    for (const label of ['EMA 策略', 'MACD 策略', 'RSI 策略', '布林带策略']) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy()
    }
    fireEvent.click(screen.getByRole('button', { name: 'MACD 策略' }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=spot&type=cra_spot&indicator=macd')
  })

  it('指标策略快捷项：合约市场跳 cra_contract', () => {
    renderList()
    fireEvent.change(screen.getByText('现货策略').closest('select')!, { target: { value: 'contract' } })
    fireEvent.click(screen.getByRole('button', { name: '布林带策略' }))
    expect(screen.getByTestId('loc').textContent).toBe('/create?market=contract&type=cra_contract&indicator=bollinger')
  })
})
