import { useState, useEffect } from 'react'
import { Activity, Wifi } from 'lucide-react'

export function BottomBar() {
  const [latency, setLatency] = useState<number | null>(null)
  const [uptime, setUptime] = useState(0)

  useEffect(() => {
    const fetchHealth = () => {
      fetch('/api/health')
        .then((r) => r.ok ? r.json() : null)
        .then((data) => {
          if (data?.latency_ms !== undefined) setLatency(data.latency_ms)
        })
        .catch(() => {})
    }
    fetchHealth()
    const interval = setInterval(fetchHealth, 30000)
    return () => clearInterval(interval)
  }, [])

  useEffect(() => {
    const timer = setInterval(() => setUptime((u) => u + 1), 1000)
    return () => clearInterval(timer)
  }, [])

  const d = Math.floor(uptime / 86400)
  const h = Math.floor((uptime % 86400) / 3600)
  const m = Math.floor((uptime % 3600) / 60)

  return (
    <footer className="h-7 bg-quant-bg-tertiary border-t border-quant-border flex items-center justify-between px-4 text-[11px] text-muted-foreground shrink-0 z-50">
      <div className="flex items-center gap-4">
        <span className="flex items-center gap-1.5">
          <Activity className="w-3 h-3 text-quant-green" />
          系统状态: 运行中
        </span>
        {latency !== null && (
          <span className="font-mono tabular-nums">引擎延迟: {latency}ms</span>
        )}
        <span className="flex items-center gap-1">
          <Wifi className="w-3 h-3 text-quant-green" />
          交易所: 6/6
        </span>
      </div>
      <div className="flex items-center gap-4 font-mono tabular-nums">
        <span>运行时间: {d}d {h}h {m}m</span>
        <span>v2.0.0</span>
      </div>
    </footer>
  )
}
