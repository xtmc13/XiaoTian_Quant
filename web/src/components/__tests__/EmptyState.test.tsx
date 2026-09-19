import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { EmptyState } from '../ui/EmptyState'
import { PackageOpen } from 'lucide-react'

describe('EmptyState', () => {
  it('renders title and icon when provided', () => {
    render(<EmptyState title="No Data" icon={<PackageOpen />} />)
    expect(screen.getByText('No Data')).toBeTruthy()
    // PackageOpen renders as an svg, not an img role
    expect(document.querySelector('svg')).toBeTruthy()
  })

  it('renders no icon when not provided', () => {
    render(<EmptyState title="No Data" />)
    expect(document.querySelector('svg')).toBeNull()
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

  it('does not render action button when no action props are given', () => {
    render(<EmptyState title="No Data" />)
    expect(screen.queryByText('Create')).toBeFalsy()
    expect(document.querySelector('button')).toBeNull()
  })

  it('renders action button from actionLabel even without onAction', () => {
    render(<EmptyState title="No Data" actionLabel="Create" />)
    expect(screen.queryByText('Create')).toBeTruthy()
  })

  it('applies custom className', () => {
    const { container } = render(<EmptyState title="Test" className="my-empty" />)
    expect(container.querySelector('.my-empty')).toBeTruthy()
  })
})
