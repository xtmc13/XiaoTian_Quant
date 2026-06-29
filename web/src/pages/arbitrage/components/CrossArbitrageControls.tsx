import { cn } from '@/lib/utils'
import { Play, Square, RefreshCw, Clock, Layers } from 'lucide-react'
import type { UseMutationResult } from '@tanstack/react-query'

interface CrossArbitrageControlsProps {
  isRunning: boolean
  startMutation: UseMutationResult<unknown, Error, void, unknown>
  stopMutation: UseMutationResult<unknown, Error, void, unknown>
  showConfig: boolean
  setShowConfig: (v: boolean) => void
  showHistory: boolean
  setShowHistory: (v: boolean) => void
}

export function CrossArbitrageControls({
  isRunning,
  startMutation,
  stopMutation,
  showConfig,
  setShowConfig,
  showHistory,
  setShowHistory,
}: CrossArbitrageControlsProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {!isRunning ? (
        <button
          onClick={() => startMutation.mutate()}
          disabled={startMutation.isPending}
          className={cn(
            'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
            startMutation.isPending
              ? 'bg-muted text-muted-foreground cursor-not-allowed'
              : 'bg-green-500/20 text-green-400 hover:bg-green-500/30'
          )}
        >
          {startMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
          启动引擎
        </button>
      ) : (
        <button
          onClick={() => stopMutation.mutate()}
          disabled={stopMutation.isPending}
          className={cn(
            'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
            stopMutation.isPending
              ? 'bg-muted text-muted-foreground cursor-not-allowed'
              : 'bg-red-500/20 text-red-400 hover:bg-red-500/30'
          )}
        >
          {stopMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Square className="w-4 h-4" />}
          停止引擎
        </button>
      )}
      <button
        onClick={() => setShowConfig(!showConfig)}
        className={cn(
          'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
          showConfig
            ? 'bg-quant-gold/10 text-quant-gold'
            : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
        )}
      >
        <Layers className="w-3.5 h-3.5" />
        配置
      </button>
      <button
        onClick={() => setShowHistory(!showHistory)}
        className={cn(
          'flex items-center gap-2 px-3 py-2 rounded-md text-xs font-medium transition-colors',
          showHistory
            ? 'bg-quant-gold/10 text-quant-gold'
            : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
        )}
      >
        <Clock className="w-3.5 h-3.5" />
        历史
      </button>
    </div>
  )
}
