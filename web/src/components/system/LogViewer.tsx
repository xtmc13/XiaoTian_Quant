import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { logsApi } from '@/lib/api'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { FileText } from 'lucide-react'
import { cn } from '@/lib/utils'

interface LogViewerProps {
  lines?: number
  className?: string
}

function highlightLevel(line: string) {
  const lower = line.toLowerCase()
  if (lower.includes('error') || lower.includes('fatal') || lower.includes('panic')) return 'text-quant-red'
  if (lower.includes('warn')) return 'text-quant-orange'
  if (lower.includes('info')) return 'text-quant-blue'
  return 'text-muted-foreground'
}

export function LogViewer({ lines = 100, className }: LogViewerProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const { data, isLoading, error } = useQuery({
    queryKey: ['logs', lines],
    queryFn: () => logsApi.tail(lines),
    refetchInterval: 3000,
  })

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [data])

  if (isLoading) {
    return (
      <div className={cn('space-y-2', className)}>
        <Skeleton variant="text" lines={8} />
      </div>
    )
  }

  if (error || !data) {
    return (
      <EmptyState
        icon={<FileText className="w-6 h-6" />}
        title="无法加载日志"
        description={error instanceof Error ? error.message : '未知错误'}
      />
    )
  }

  const logLines = data.split('\n').filter(Boolean)

  return (
    <div className={cn('h-full overflow-auto font-mono text-[11px] leading-5 bg-quant-bg-secondary rounded-lg border border-quant-border p-3', className)}>
      {logLines.length === 0 ? (
        <div className="text-muted-foreground">暂无日志</div>
      ) : (
        logLines.map((line, i) => (
          <div key={i} className={cn('whitespace-pre-wrap break-all', highlightLevel(line))}>
            {line}
          </div>
        ))
      )}
      <div ref={bottomRef} />
    </div>
  )
}
