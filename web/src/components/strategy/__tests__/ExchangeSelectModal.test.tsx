import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ExchangeSelectModal } from '../ExchangeSelectModal'
import type { ExchangeConfiguredStatus } from '@/types'

const mockConfigured: Record<string, ExchangeConfiguredStatus> = {
  binance: { enabled: true, has_credentials: true, testnet: false, futures: true },
  okx: { enabled: true, has_credentials: true, testnet: true, futures: true },
  mexc: { enabled: false, has_credentials: false, testnet: false, futures: false },
}

describe('ExchangeSelectModal', () => {
  const baseProps = {
    open: true,
    onClose: vi.fn(),
    value: [] as string[],
    onChange: vi.fn(),
    configuredExchanges: mockConfigured,
  }

  it('renders exchange list when open', () => {
    render(<ExchangeSelectModal {...baseProps} />)
    expect(screen.getByText('Binance')).toBeTruthy()
    expect(screen.getByText('OKX')).toBeTruthy()
    expect(screen.getByText('MEXC')).toBeTruthy()
  })

  it('selects an exchange when clicked', () => {
    const onChange = vi.fn()
    render(<ExchangeSelectModal {...baseProps} onChange={onChange} />)
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    expect(onChange).toHaveBeenCalledWith(['binance'])
  })

  it('deselects an already selected exchange', () => {
    const onChange = vi.fn()
    render(<ExchangeSelectModal {...baseProps} value={['binance']} onChange={onChange} />)
    fireEvent.click(screen.getByText('Binance'))
    fireEvent.click(screen.getByText('确认选择'))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('does not select disabled exchanges', () => {
    const onChange = vi.fn()
    render(<ExchangeSelectModal {...baseProps} onChange={onChange} />)
    fireEvent.click(screen.getByText('MEXC'))
    fireEvent.click(screen.getByText('确认选择'))
    expect(onChange).not.toHaveBeenCalled()
  })

  it('calls onClose when cancel clicked', () => {
    const onClose = vi.fn()
    render(<ExchangeSelectModal {...baseProps} onClose={onClose} />)
    fireEvent.click(screen.getByText('取消'))
    expect(onClose).toHaveBeenCalled()
  })

  it('does not render when closed', () => {
    const { container } = render(<ExchangeSelectModal {...baseProps} open={false} />)
    expect(container.firstChild).toBeNull()
  })
})
