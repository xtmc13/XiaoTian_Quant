import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CRAParamForm, DEFAULT_CRA_PARAMS } from '../CRAParamForm'
import type { CRAParams } from '../CRAParamForm'

describe('CRAParamForm', () => {
  const baseProps = {
    value: DEFAULT_CRA_PARAMS,
    onChange: vi.fn(),
    market: 'spot' as const,
  }

  it('renders default first order amount from DEFAULT_CRA_PARAMS', () => {
    render(<CRAParamForm {...baseProps} />)
    const inputs = screen.getAllByDisplayValue(String(DEFAULT_CRA_PARAMS.firstOrderAmount))
    expect(inputs.length).toBeGreaterThan(0)
  })

  it('shows static take profit inputs by default', () => {
    render(<CRAParamForm {...baseProps} />)
    expect(screen.getByText('止盈比例 (%)')).toBeTruthy()
    expect(screen.getByText('盈利回调 (%)')).toBeTruthy()
  })

  it('switches take profit mode to moving when clicked', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} onChange={onChange} />)
    fireEvent.click(screen.getByText('移动止盈'))
    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as CRAParams
    expect(lastCall.tpMode).toBe('moving')
  })

  it('renders open double checkbox', () => {
    render(<CRAParamForm {...baseProps} />)
    expect(screen.getByLabelText('开仓加倍')).toBeTruthy()
  })

  it('toggles open double checkbox', () => {
    const onChange = vi.fn()
    render(<CRAParamForm {...baseProps} onChange={onChange} />)
    const checkbox = screen.getByLabelText('开仓加倍') as HTMLInputElement
    fireEvent.click(checkbox)
    expect(onChange).toHaveBeenCalled()
    const hasOpenDouble = onChange.mock.calls.some((call) => (call[0] as CRAParams).openDouble === true)
    expect(hasOpenDouble).toBe(true)
  })
})
