import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MovingTPModal } from '../MovingTPModal'
import type { MovingTPTier } from '@/types'

const mockTiers: MovingTPTier[] = [
  { ratio: 2, drawback: 20 },
  { ratio: 3, drawback: 20 },
]

describe('MovingTPModal', () => {
  const baseProps = {
    open: true,
    onClose: vi.fn(),
    value: mockTiers,
    onChange: vi.fn(),
  }

  it('renders moving TP tiers table when open', () => {
    render(<MovingTPModal {...baseProps} />)
    expect(screen.getByText('移动止盈止损参数')).toBeTruthy()
    expect(screen.getByText('止盈比例 (%)')).toBeTruthy()
    expect(screen.getByText('止盈回撤 (%)')).toBeTruthy()
  })

  it('calls onChange when a ratio is edited', () => {
    const onChange = vi.fn()
    render(<MovingTPModal {...baseProps} onChange={onChange} />)
    const inputs = screen.getAllByDisplayValue('2')
    fireEvent.change(inputs[0], { target: { value: '5' } })
    expect(onChange).toHaveBeenCalled()
  })

  it('calls onClose when done button clicked', () => {
    const onClose = vi.fn()
    render(<MovingTPModal {...baseProps} onClose={onClose} />)
    fireEvent.click(screen.getByText('完成'))
    expect(onClose).toHaveBeenCalled()
  })

  it('does not render when closed', () => {
    const { container } = render(<MovingTPModal {...baseProps} open={false} />)
    expect(container.firstChild).toBeNull()
  })
})
