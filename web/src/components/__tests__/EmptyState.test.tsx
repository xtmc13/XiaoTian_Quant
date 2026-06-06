import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { EmptyState } from '../ui/EmptyState'
import { PackageOpen } from 'lucide-react'

describe('EmptyState', () => {
  it('renders title and default icon', () => {
    render(<EmptyState title="No Data" />)
    expect(screen.getByText('No Data')).toBeTruthy()
    // PackageOpen renders as an svg, not an img role
    expect(document.querySelector('svg')).toBeTruthy()
  })

  it('renders custom icon', () => {
    render(<EmptyState title="Custom" icon={<span data-testid="custom-icon">★</span>} />)
    expect(screen.getByTestId('custom-icon')).toBeTruthy()
  })

  it('renders description', () => {
    render(<EmptyState title="No Data" description="Nothing to see here" />)
    expect(screen.getByText('Nothing to see here')).toBeTruthy()
  })

  it('renders action button and calls onAction', () => {
    const handleAction = vi.fn()
    render(
      <EmptyState
        title="No Data"
        actionLabel="Create One"
        onAction={handleAction}
      />
    )
    const btn = screen.getByText('Create One')
    expect(btn).toBeTruthy()
    fireEvent.click(btn)
    expect(handleAction).toHaveBeenCalledTimes(1)
  })

  it('does not render action button when onAction is missing', () => {
    render(<EmptyState title="No Data" actionLabel="Create" />)
    expect(screen.queryByText('Create')).toBeFalsy()
  })

  it('applies custom className', () => {
    const { container } = render(<EmptyState title="Test" className="my-empty" />)
    expect(container.querySelector('.my-empty')).toBeTruthy()
  })
})
