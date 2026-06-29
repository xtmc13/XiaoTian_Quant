import React, { useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import { TrendingUp, Clock, XCircle, CheckCircle2, AlertCircle, Activity, ChevronUp, ChevronDown } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

export type BottomTabKey = 'positions' | 'orders' | 'history' | 'fills' | 'assets' | 'plans'

export interface TabDef {
  key: BottomTabKey
  label: string
  count: number
  icon: LucideIcon
}

export interface TradingBottomPanelProps {
  tabs: TabDef[]
  activeTab: BottomTabKey
  onTabChange: (tab: BottomTabKey) => void
  children: React.ReactNode
}

export function TradingBottomPanel({ tabs, activeTab, onTabChange, children }: TradingBottomPanelProps) {
  const [bottomHeight, setBottomHeight] = useState(0)
  const bottomCollapsed = bottomHeight < 20
  const dragRef = useRef<{ startY: number; startH: number } | null>(null)

  return (
    <div
      className="shrink-0 border-t border-quant-border bg-quant-bg-secondary flex flex-col"
      style={{ height: bottomCollapsed ? 'auto' : bottomHeight }}
    >
      {/* Resize handle */}
      <div
        className="h-1.5 cursor-row-resize hover:bg-quant-gold/20 active:bg-quant-gold/30 shrink-0 relative"
        onMouseDown={(e) => {
          dragRef.current = { startY: e.clientY, startH: bottomHeight }
          const onMove = (ev: MouseEvent) => {
            if (!dragRef.current) return
            const h = Math.max(60, Math.min(600, dragRef.current.startH - (ev.clientY - dragRef.current.startY)))
            setBottomHeight(h)
          }
          const onUp = () => {
            dragRef.current = null
            document.removeEventListener('mousemove', onMove)
            document.removeEventListener('mouseup', onUp)
          }
          document.addEventListener('mousemove', onMove)
          document.addEventListener('mouseup', onUp)
        }}
      >
        <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-8 h-0.5 rounded bg-quant-border/60" />
      </div>

      {/* Tab bar */}
      <div className="flex border-b border-quant-border px-2 items-center justify-between shrink-0">
        <div className="flex">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => {
                onTabChange(t.key)
                setBottomHeight((h) => Math.max(h, 180))
              }}
              className={cn(
                'px-4 py-2 text-xs font-medium transition-colors relative flex items-center gap-1.5',
                activeTab === t.key ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
              )}
            >
              <t.icon className="w-3.5 h-3.5" />
              {t.label}
              {t.count > 0 && (
                <span
                  className={cn(
                    'ml-1 px-1.5 py-0 rounded-full text-[10px] font-bold',
                    activeTab === t.key
                      ? 'bg-quant-gold/20 text-quant-gold'
                      : 'bg-quant-bg-tertiary text-muted-foreground'
                  )}
                >
                  {t.count}
                </span>
              )}
              {activeTab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold" />}
            </button>
          ))}
        </div>
        <button
          onClick={() => setBottomHeight((h) => (h < 20 ? 180 : 0))}
          className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
          title={bottomCollapsed ? '展开' : '收起'}
        >
          {bottomCollapsed ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
        </button>
      </div>

      {/* Content */}
      {!bottomCollapsed && (
        <div className="overflow-y-auto flex-1" style={{ maxHeight: bottomHeight - 40 }}>
          {children}
        </div>
      )}
    </div>
  )
}
