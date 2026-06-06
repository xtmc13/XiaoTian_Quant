import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { KPICard } from '../ui/KPICard'

describe('KPICard', () => {
  it('renders label and value', () => {
    render(
      <KPICard
        icon={<span data-testid="icon">$</span>}
        label="Total Equity"
        value="$12,345"
      />
    )
    expect(screen.getByText('Total Equity')).toBeTruthy()
    expect(screen.getByText('$12,345')).toBeTruthy()
    expect(screen.getByTestId('icon')).toBeTruthy()
  })

  it('renders subValue with trend up', () => {
    render(
      <KPICard
        icon={<span>▲</span>}
        label="PnL"
        value="+$500"
        subValue="5.2%"
        trend="up"
      />
    )
    expect(screen.getByText('5.2%')).toBeTruthy()
  })

  it('renders subValue with trend down', () => {
    render(
      <KPICard
        icon={<span>▼</span>}
        label="Drawdown"
        value="-3%"
        subValue="1.2%"
        trend="down"
      />
    )
    expect(screen.getByText('1.2%')).toBeTruthy()
  })

  it('renders ring progress', () => {
    render(
      <KPICard
        icon={<span>●</span>}
        label="Progress"
        value="Done"
        ringProgress={75}
      />
    )
    // The ring progress shows "75%" inside the SVG circle
    expect(screen.getByText('75%')).toBeTruthy()
  })

  it('calls onClick when clicked', () => {
    const handleClick = vi.fn()
    const { container } = render(
      <KPICard
        icon={<span>●</span>}
        label="Clickable"
        value="100"
        onClick={handleClick}
      />
    )
    ;(container.firstElementChild as HTMLElement)?.click()
    expect(handleClick).toHaveBeenCalledTimes(1)
  })

  it('calls onNavigate when navigate button clicked', () => {
    const handleNavigate = vi.fn()
    render(
      <KPICard
        icon={<span>●</span>}
        label="Nav"
        value="100"
        onNavigate={handleNavigate}
      />
    )
    const btn = screen.getByRole('button')
    fireEvent.click(btn)
    expect(handleNavigate).toHaveBeenCalledTimes(1)
  })

  it('applies primary styling', () => {
    const { container } = render(
      <KPICard
        icon={<span>●</span>}
        label="Primary"
        value="100"
        primary
      />
    )
    expect(container.querySelector('.ring-1')).toBeTruthy()
  })
})
