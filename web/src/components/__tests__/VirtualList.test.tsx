import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { VirtualList } from '../VirtualList'

describe('VirtualList', () => {
  const items = Array.from({ length: 100 }, (_, i) => `item-${i}`)
  const renderItem = (item: string, index: number) => (
    <div data-testid={`item-${index}`}>{item}</div>
  )

  it('renders only visible items plus overscan', () => {
    render(
      <VirtualList
        items={items}
        itemHeight={40}
        renderItem={renderItem}
        containerHeight={200}
        overscan={2}
      />
    )

    // containerHeight=200, itemHeight=40 => 5 visible + 2 overscan top + 2 overscan bottom = 9
    const visibleItems = items.filter((_, i) => {
      const el = screen.queryByTestId(`item-${i}`)
      return el !== null
    })

    // Should render roughly 9 items (5 visible + 4 overscan)
    expect(visibleItems.length).toBeLessThanOrEqual(10)
    expect(visibleItems.length).toBeGreaterThanOrEqual(5)
  })

  it('renders empty list without crashing', () => {
    const { container } = render(
      <VirtualList
        items={[]}
        itemHeight={40}
        renderItem={renderItem}
        containerHeight={200}
      />
    )
    expect(container.querySelector('[style*="height: 0px"]')).toBeTruthy()
  })

  it('updates visible range on scroll', () => {
    render(
      <VirtualList
        items={items}
        itemHeight={40}
        renderItem={renderItem}
        containerHeight={200}
        overscan={0}
      />
    )

    // Initially item-0 should be visible
    expect(screen.queryByTestId('item-0')).toBeTruthy()

    // Scroll down by 200px (5 items)
    const container = document.querySelector('.overflow-auto')
    if (container) {
      fireEvent.scroll(container, { target: { scrollTop: 200 } })

      // After scroll, item-0 should be gone, item-5 should be at top
      // With overscan=0, only items 5-9 should be visible
      expect(screen.queryByTestId('item-0')).toBeFalsy()
      expect(screen.queryByTestId('item-5')).toBeTruthy()
    }
  })

  it('applies custom className', () => {
    const { container } = render(
      <VirtualList
        items={items}
        itemHeight={40}
        renderItem={renderItem}
        containerHeight={200}
        className="my-custom-class"
      />
    )
    expect(container.querySelector('.my-custom-class')).toBeTruthy()
  })
})
