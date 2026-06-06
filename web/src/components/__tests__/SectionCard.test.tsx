import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SectionCard } from '../ui/SectionCard'

describe('SectionCard', () => {
  it('renders title and children', () => {
    render(
      <SectionCard title="Section Title">
        <div>Content</div>
      </SectionCard>
    )
    expect(screen.getByText('Section Title')).toBeTruthy()
    expect(screen.getByText('Content')).toBeTruthy()
  })

  it('renders header action', () => {
    render(
      <SectionCard title="Title" headerAction={<button>Action</button>}>
        <div>Content</div>
      </SectionCard>
    )
    expect(screen.getByText('Action')).toBeTruthy()
  })

  it('renders without title', () => {
    render(
      <SectionCard>
        <div>Just content</div>
      </SectionCard>
    )
    expect(screen.getByText('Just content')).toBeTruthy()
  })

  it('applies custom className', () => {
    const { container } = render(
      <SectionCard className="my-section">
        <div>Content</div>
      </SectionCard>
    )
    expect(container.querySelector('.my-section')).toBeTruthy()
  })

  it('applies bodyClassName', () => {
    const { container } = render(
      <SectionCard bodyClassName="my-body">
        <div>Content</div>
      </SectionCard>
    )
    expect(container.querySelector('.my-body')).toBeTruthy()
  })

  it('removes padding when noPadding is true', () => {
    const { container } = render(
      <SectionCard noPadding>
        <div>Content</div>
      </SectionCard>
    )
    const body = container.querySelector('[class*="p-5"]')
    expect(body).toBeFalsy()
  })
})
