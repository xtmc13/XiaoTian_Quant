import { useEffect, useRef, useCallback, useState } from 'react'

interface WSOptions {
  reconnect?: boolean
  maxRetries?: number
  heartbeatInterval?: number
  heartbeatMsg?: unknown
  onReconnect?: () => void
}

export function useWebSocket(url: string, options: WSOptions = {}) {
  const {
    reconnect = true,
    maxRetries = 20,
    heartbeatInterval = 30000,
    heartbeatMsg,
    onReconnect,
  } = options

  const ws = useRef<WebSocket | null>(null)
  const handlers = useRef<Map<string, ((data: unknown) => void)[]>>(new Map())
  const retryCount = useRef(0)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const heartbeatTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const [isConnected, setIsConnected] = useState(false)

  // Stable heartbeat message — avoids re-connecting every render
  const heartbeatMsgRef = useRef(heartbeatMsg ?? { type: 'ping' })

  useEffect(() => {
    let disposed = false

    const getBackoffDelay = (retry: number) => {
      // Exponential backoff: 1s, 2s, 4s, 8s... capped at 30s
      return Math.min(1000 * Math.pow(2, retry), 30000)
    }

    const startHeartbeat = () => {
      if (heartbeatTimer.current) clearInterval(heartbeatTimer.current)
      heartbeatTimer.current = setInterval(() => {
        if (ws.current?.readyState === WebSocket.OPEN) {
          ws.current.send(JSON.stringify({ ...heartbeatMsgRef.current, ts: Date.now() }))
        }
      }, heartbeatInterval)
    }

    const stopHeartbeat = () => {
      if (heartbeatTimer.current) {
        clearInterval(heartbeatTimer.current)
        heartbeatTimer.current = null
      }
    }

    const connect = () => {
      if (disposed) return
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const fullUrl = url.startsWith('ws') ? url : `${protocol}//${window.location.host}${url}`

      try {
        const socket = new WebSocket(fullUrl)

        socket.onopen = () => {
          if (disposed) return
          console.warn('[WS] Connected')
          setIsConnected(true)
          const wasReconnect = retryCount.current > 0
          retryCount.current = 0
          startHeartbeat()
          if (wasReconnect && onReconnect) {
            onReconnect()
          }
        }

        socket.onclose = () => {
          if (disposed) return
          console.warn('[WS] Disconnected')
          setIsConnected(false)
          stopHeartbeat()

          if (reconnect && retryCount.current < maxRetries) {
            const delay = getBackoffDelay(retryCount.current)
            console.warn(`[WS] Reconnecting in ${delay}ms (retry ${retryCount.current + 1}/${maxRetries})`)
            reconnectTimer.current = setTimeout(() => {
              retryCount.current++
              connect()
            }, delay)
          }
        }

        socket.onerror = (err) => {
          console.error('[WS] Error:', err)
        }

        socket.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data)
            const type = data.type || '*'
            const callbacks = handlers.current.get(type) || []
            callbacks.forEach((cb) => cb(data))
            const wildcards = handlers.current.get('*') || []
            wildcards.forEach((cb) => cb(data))
          } catch (e) {
            // ignore non-JSON messages
          }
        }

        ws.current = socket
      } catch (err) {
        console.error('[WS] Failed to create connection:', err)
        if (reconnect && retryCount.current < maxRetries) {
          const delay = getBackoffDelay(retryCount.current)
          reconnectTimer.current = setTimeout(() => {
            retryCount.current++
            connect()
          }, delay)
        }
      }
    }

    connect()

    return () => {
      disposed = true
      stopHeartbeat()
      if (reconnectTimer.current) {
        clearTimeout(reconnectTimer.current)
        reconnectTimer.current = null
      }
      ws.current?.close()
      ws.current = null
    }
  }, [url, reconnect, maxRetries, heartbeatInterval])

  const on = useCallback((type: string, callback: (data: unknown) => void) => {
    const list = handlers.current.get(type) || []
    list.push(callback)
    handlers.current.set(type, list)
    return () => {
      const updated = (handlers.current.get(type) || []).filter((cb) => cb !== callback)
      handlers.current.set(type, updated)
    }
  }, [])

  const send = useCallback((data: unknown) => {
    const socket = ws.current
    if (socket != null && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(data))
    } else {
      console.warn('[WS] Not connected, message dropped:', data)
    }
  }, [])

  return { on, send, isConnected }
}
