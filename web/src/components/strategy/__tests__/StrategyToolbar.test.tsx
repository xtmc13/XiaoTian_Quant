import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { StrategyToolbar, type MarketFilter, type StatusFilter } from '../StrategyToolbar'

describe('StrategyToolbar', () => {
  const baseProps = {
    search: '',
    onSearchChange: vi.fn(),
    marketFilter: 'contract' as MarketFilter,
    onMarketFilterChange: vi.fn(),
    statusFilter: 'all' as StatusFilter,
    onStatusFilterChange: vi.fn(),
    typeFilter: '',
    onTypeFilterChange: vi.fn(),
    strategyTypes: [
      { value: 'martin_trend', label: '马丁趋势策略' },
      { value: 'wallstreet', label: '华尔街策略' },
    ],
    selectedCount: 0,
    onBatchStart: vi.fn(),
    onBatchStop: vi.fn(),
    onBatchClose: vi.fn(),
    onBatchDelete: vi.fn(),
    onBatchEdit: vi.fn(),
    onCreate: vi.fn(),
  }

  it('renders search, market toggle, status select and create button', () => {
    render(<StrategyToolbar {...baseProps} />)
    expect(screen.getByPlaceholderText('搜索策略...')).toBeTruthy()
    expect(screen.getByText('现货')).toBeTruthy()
    expect(screen.getByText('合约')).toBeTruthy()
    expect(screen.getByText('全部状态')).toBeTruthy()
    expect(screen.getByLabelText('创建策略')).toBeTruthy()
  })

  it('calls onMarketFilterChange when market buttons clicked', () => {
    render(<StrategyToolbar {...baseProps} />)
    fireEvent.click(screen.getByText('现货'))
    expect(baseProps.onMarketFilterChange).toHaveBeenCalledWith('spot')
  })

  it('calls onCreate when create button clicked', () => {
    render(<StrategyToolbar {...baseProps} />)
    fireEvent.click(screen.getByLabelText('创建策略'))
    expect(baseProps.onCreate).toHaveBeenCalled()
  })

  it('calls onSearchChange when typing in search input', () => {
    render(<StrategyToolbar {...baseProps} />)
    fireEvent.change(screen.getByPlaceholderText('搜索策略...'), { target: { value: 'BTC' } })
    expect(baseProps.onSearchChange).toHaveBeenCalledWith('BTC')
  })

  it('calls onStatusFilterChange when status select changed', () => {
    render(<StrategyToolbar {...baseProps} />)
    fireEvent.change(screen.getByText('全部状态').closest('select')!, { target: { value: 'running' } })
    expect(baseProps.onStatusFilterChange).toHaveBeenCalledWith('running')
  })

  it('shows batch actions when items are selected', () => {
    render(<StrategyToolbar {...baseProps} selectedCount={3} />)
    expect(screen.getByText('已选 3 项')).toBeTruthy()
    expect(screen.getByText('启动')).toBeTruthy()
    expect(screen.getByText('停止')).toBeTruthy()
    expect(screen.getByText('平仓')).toBeTruthy()
    expect(screen.getByText('修改')).toBeTruthy()
    expect(screen.getByText('删除')).toBeTruthy()
  })

  it('calls batch action handlers', () => {
    render(<StrategyToolbar {...baseProps} selectedCount={2} />)
    fireEvent.click(screen.getByText('启动'))
    expect(baseProps.onBatchStart).toHaveBeenCalled()
    fireEvent.click(screen.getByText('停止'))
    expect(baseProps.onBatchStop).toHaveBeenCalled()
    fireEvent.click(screen.getByText('平仓'))
    expect(baseProps.onBatchClose).toHaveBeenCalled()
    fireEvent.click(screen.getByText('修改'))
    expect(baseProps.onBatchEdit).toHaveBeenCalled()
    fireEvent.click(screen.getByText('删除'))
    expect(baseProps.onBatchDelete).toHaveBeenCalled()
  })
})
