import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Skeleton } from '../ui/Skeleton'

describe('Skeleton', () => {
  it('renders text variant by default', () => {
    const { container } = render(<Skeleton />)
    const divs = container.querySelectorAll('.animate-pulse')
    expect(divs.length).toBeGreaterThanOrEqual(1)
  })

  it('renders multiple lines for text variant', () => {
    const { container } = render(<Skeleton lines={3} />)
    const divs = container.querySelectorAll('.animate-pulse')
    expect(divs.length).toBe(3)
  })

  it('renders circle variant', () => {
    const { container } = render(<Skeleton variant="circle" />)
    const circle = container.querySelector('.rounded-full')
    expect(circle).toBeTruthy()
  })

  it('renders card variant', () => {
    const { container } = render(<Skeleton variant="card" />)
    const card = container.querySelector('.rounded-xl')
    expect(card).toBeTruthy()
  })

  it('renders rect variant', () => {
    const { container } = render(<Skeleton variant="rect" />)
    const rect = container.querySelector('.rounded-lg')
    expect(rect).toBeTruthy()
  })

  it('applies custom width and height', () => {
    const { container } = render(<Skeleton variant="rect" width={100} height={50} />)
    const el = container.firstElementChild as HTMLElement
    expect(el.style.width).toBe('100px')
    expect(el.style.height).toBe('50px')
  })

  it('applies custom className', () => {
    const { container } = render(<Skeleton className="my-skeleton" />)
    expect(container.querySelector('.my-skeleton')).toBeTruthy()
  })
})
