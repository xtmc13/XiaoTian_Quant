import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useWebSocket } from '../useWebSocket'

describe('useWebSocket', () => {
  class MockWebSocket {
    url: string
    readyState: number
    send = vi.fn()
    close = vi.fn()
    onopen: ((ev: Event) => void) | null = null
    onclose: ((ev: CloseEvent) => void) | null = null
    onmessage: ((ev: MessageEvent) => void) | null = null
    onerror: ((ev: Event) => void) | null = null

    constructor(url: string) {
      this.url = url
      this.readyState = WebSocket.CONNECTING
    }
  }

  let lastSocket: MockWebSocket | null = null
  let OriginalWebSocket: any

  beforeEach(() => {
    lastSocket = null
    OriginalWebSocket = global.WebSocket
    global.WebSocket = function (url: string) {
      lastSocket = new MockWebSocket(url)
      return lastSocket
    } as any
  })

  afterEach(() => {
    global.WebSocket = OriginalWebSocket
    vi.clearAllMocks()
  })

  // Helper to wait for useEffect to create socket
  const waitForSocket = async () => {
    await waitFor(() => expect(lastSocket).not.toBeNull(), { timeout: 3000 })
    // Give useEffect time to assign ws.current
    await new Promise((r) => setTimeout(r, 50))
  }

  it('should create WebSocket connection', async () => {
    renderHook(() => useWebSocket('/ws'))
    await waitForSocket()
    expect(lastSocket).not.toBeNull()
    expect(lastSocket!.url).toContain('/ws')
  })

  it('should set isConnected to true on open', async () => {
    const { result } = renderHook(() => useWebSocket('/ws'))
    await waitForSocket()

    act(() => {
      lastSocket!.readyState = WebSocket.OPEN
      lastSocket!.onopen?.(new Event('open'))
    })

    await waitFor(() => {
      expect(result.current.isConnected).toBe(true)
    })
  })

  it('should register message handlers and receive data', async () => {
    const { result } = renderHook(() => useWebSocket('/ws'))
    const callback = vi.fn()
    await waitForSocket()

    act(() => {
      result.current.on('tick', callback)
    })

    act(() => {
      lastSocket!.readyState = WebSocket.OPEN
      lastSocket!.onmessage?.({
        data: JSON.stringify({ type: 'tick', price: 50000 }),
      } as MessageEvent)
    })

    await waitFor(() => {
      expect(callback).toHaveBeenCalledWith(expect.objectContaining({ type: 'tick', price: 50000 }))
    })
  })

  it('should handle wildcard * messages', async () => {
    const { result } = renderHook(() => useWebSocket('/ws'))
    const callback = vi.fn()
    await waitForSocket()

    act(() => {
      result.current.on('*', callback)
    })

    act(() => {
      lastSocket!.readyState = WebSocket.OPEN
      lastSocket!.onmessage?.({
        data: JSON.stringify({ type: 'unknown', data: 'test' }),
      } as MessageEvent)
    })

    await waitFor(() => {
      expect(callback).toHaveBeenCalled()
    })
  })

  it('should send messages when connected', async () => {
    const { result } = renderHook(() => useWebSocket('/ws'))
    await waitForSocket()

    act(() => {
      lastSocket!.readyState = WebSocket.OPEN
      lastSocket!.onopen?.(new Event('open'))
    })

    await waitFor(() => expect(result.current.isConnected).toBe(true))

    act(() => {
      result.current.send({ type: 'subscribe', channel: 'BTCUSDT' })
    })

    expect(lastSocket!.send).toHaveBeenCalledWith(
      JSON.stringify({ type: 'subscribe', channel: 'BTCUSDT' })
    )
  })

  it('should not send when disconnected', async () => {
    const { result } = renderHook(() => useWebSocket('/ws'))
    await waitForSocket()

    // socket is still CONNECTING (readyState = 0)
    expect(lastSocket!.readyState).toBe(WebSocket.CONNECTING)

    act(() => {
      result.current.send({ type: 'test' })
    })

    expect(lastSocket!.send).not.toHaveBeenCalled()
  })

  it('should cleanup on unmount', async () => {
    const { unmount } = renderHook(() => useWebSocket('/ws'))
    await waitForSocket()

    act(() => {
      unmount()
    })

    expect(lastSocket!.close).toHaveBeenCalled()
  })

  it('should build correct ws URL from relative path', async () => {
    renderHook(() => useWebSocket('/ws/v2'))
    await waitForSocket()
    expect(lastSocket!.url).toMatch(/ws:\/\/.*\/ws\/v2/)
  })

  it('should use absolute URL when provided', async () => {
    renderHook(() => useWebSocket('wss://api.example.com/ws'))
    await waitForSocket()
    expect(lastSocket!.url).toBe('wss://api.example.com/ws')
  })

  it('should unsubscribe handler when cleanup called', async () => {
    const { result } = renderHook(() => useWebSocket('/ws'))
    const callback = vi.fn()
    await waitForSocket()

    let cleanup: (() => void) | undefined
    act(() => {
      cleanup = result.current.on('tick', callback)
    })

    act(() => {
      cleanup?.()
    })

    act(() => {
      lastSocket!.readyState = WebSocket.OPEN
      lastSocket!.onmessage?.({
        data: JSON.stringify({ type: 'tick', price: 50000 }),
      } as MessageEvent)
    })

    await new Promise((r) => setTimeout(r, 50))
    expect(callback).not.toHaveBeenCalled()
  })
})
