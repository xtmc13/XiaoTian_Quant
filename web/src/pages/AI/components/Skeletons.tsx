export function SkeletonBox() {
  return (
    <div className="flex flex-col items-center gap-1 px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]">
      <div className="w-8 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-10 h-3 rounded bg-quant-border animate-pulse" />
    </div>
  )
}

export function SkeletonCell() {
  return (
    <div className="flex flex-col items-center justify-center gap-1 rounded-md p-2 bg-quant-bg-secondary">
      <div className="w-3/5 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-4/5 h-2.5 rounded bg-quant-border animate-pulse" />
    </div>
  )
}

export function SkeletonCalItem() {
  return (
    <div className="flex items-center gap-2 py-1.5 border-b border-quant-border/50">
      <div className="w-8 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-8 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-4 h-2 rounded bg-quant-border animate-pulse" />
      <div className="flex-1 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-10 h-2 rounded bg-quant-border animate-pulse" />
    </div>
  )
}
