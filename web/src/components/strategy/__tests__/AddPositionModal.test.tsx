import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AddPositionModal } from '../AddPositionModal'
import type { AddPositionItem } from '@/types'

const mockPositions: AddPositionItem[] = [
  { order: 1, multiplier: 1, spread: 1, callback: 0.5 },
  { order: 2, multiplier: 2, spread: 2, callback: 0.5 },
]

describe('AddPositionModal', () => {
  const baseProps = {
    open: true,
    onClose: vi.fn(),
    value: mockPositions,
    onChange: vi.fn(),
  }

  it('renders add position table when open', () => {
    render(<AddPositionModal {...baseProps} />)
    expect(screen.getByText('补仓参数')).toBeTruthy()
    expect(screen.getByText('倍数')).toBeTruthy()
    expect(screen.getByText('差价 (%)')).toBeTruthy()
  })

  it('calls onChange when a multiplier is edited', () => {
    const onChange = vi.fn()
    render(<AddPositionModal {...baseProps} onChange={onChange} />)
    const inputs = screen.getAllByDisplayValue('1')
    fireEvent.change(inputs[0], { target: { value: '2' } })
    expect(onChange).toHaveBeenCalled()
  })

  it('calls onClose when done button clicked', () => {
    const onClose = vi.fn()
    render(<AddPositionModal {...baseProps} onClose={onClose} />)
    fireEvent.click(screen.getByText('完成'))
    expect(onClose).toHaveBeenCalled()
  })

  it('does not render when closed', () => {
    const { container } = render(<AddPositionModal {...baseProps} open={false} />)
    expect(container.firstChild).toBeNull()
  })
})
