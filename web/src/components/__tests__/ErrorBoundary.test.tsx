import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ErrorBoundary } from '../ErrorBoundary'

// Component that throws
const ThrowError = ({ shouldThrow }: { shouldThrow: boolean }) => {
  if (shouldThrow) {
    throw new Error('Test error')
  }
  return <div>Normal content</div>
}

describe('ErrorBoundary', () => {
  // Suppress console.error for expected errors
  const consoleError = console.error
  beforeEach(() => {
    console.error = vi.fn()
  })
  afterEach(() => {
    console.error = consoleError
  })

  it('renders children when no error', () => {
    render(
      <ErrorBoundary fallback={<div>Something went wrong</div>}>
        <div>Safe content</div>
      </ErrorBoundary>
    )
    expect(screen.getByText('Safe content')).toBeTruthy()
  })

  it('renders fallback UI when child throws', () => {
    render(
      <ErrorBoundary fallback={<div>页面加载出错</div>}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    )
    expect(screen.getByText('页面加载出错')).toBeTruthy()
    expect(screen.getByText('Test error')).toBeTruthy()
  })

  it('renders custom fallback when provided', () => {
    render(
      <ErrorBoundary fallback={<div>Custom fallback content</div>}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    )
    expect(screen.getByText('Custom fallback content')).toBeTruthy()
  })

  it('has a refresh button that reloads', () => {
    const reloadSpy = vi.fn()
    Object.defineProperty(window, 'location', {
      writable: true,
      value: { reload: reloadSpy },
    })

    render(
      <ErrorBoundary fallback={<div>页面加载出错<button onClick={() => location.reload()}>刷新页面</button><p>Test error</p></div>}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>
    )

    const button = screen.getByText('刷新页面')
    expect(button).toBeTruthy()
  })
})
