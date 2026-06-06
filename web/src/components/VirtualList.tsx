import { useState, useRef, useCallback, useMemo, ReactNode } from 'react'

interface VirtualListProps<T> {
  items: T[]
  itemHeight: number
  renderItem: (item: T, index: number) => ReactNode
  containerHeight?: number
  overscan?: number
  className?: string
}

/**
 * VirtualList — 只渲染可视区域内的列表项，用于大数据量表格/列表。
 */
export function VirtualList<T>({
  items,
  itemHeight,
  renderItem,
  containerHeight = 400,
  overscan = 5,
  className = '',
}: VirtualListProps<T>) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)

  const totalHeight = items.length * itemHeight

  const visibleRange = useMemo(() => {
    const startIdx = Math.max(0, Math.floor(scrollTop / itemHeight) - overscan)
    const endIdx = Math.min(
      items.length,
      Math.ceil((scrollTop + containerHeight) / itemHeight) + overscan
    )
    return { startIdx, endIdx }
  }, [scrollTop, itemHeight, containerHeight, overscan, items.length])

  const handleScroll = useCallback(() => {
    if (containerRef.current) {
      setScrollTop(containerRef.current.scrollTop)
    }
  }, [])

  const visibleItems = useMemo(() => {
    const { startIdx, endIdx } = visibleRange
    return items.slice(startIdx, endIdx).map((item, i) => ({
      item,
      index: startIdx + i,
    }))
  }, [items, visibleRange])

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className={`overflow-auto ${className}`}
      style={{ height: containerHeight }}
    >
      <div style={{ height: totalHeight, position: 'relative' }}>
        {visibleItems.map(({ item, index }) => (
          <div
            key={index}
            style={{
              position: 'absolute',
              top: index * itemHeight,
              height: itemHeight,
              left: 0,
              right: 0,
            }}
          >
            {renderItem(item, index)}
          </div>
        ))}
      </div>
    </div>
  )
}
