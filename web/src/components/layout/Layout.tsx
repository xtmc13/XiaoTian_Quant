import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'
import { BottomNav } from './BottomNav'

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen bg-quant-bg text-foreground overflow-hidden">
      {/* Sidebar — hidden on mobile, visible on md+ */}
      <div className="hidden md:flex shrink-0">
        <Sidebar />
      </div>
      <div className="flex-1 flex flex-col min-w-0">
        <TopBar />
        {/* Bottom padding on mobile for BottomNav */}
        <main className="flex-1 overflow-hidden relative bg-quant-bg-tertiary pb-14 md:pb-0">
          {children}
        </main>
      </div>
      {/* Bottom navigation — visible only on mobile (< md) */}
      <BottomNav />
    </div>
  )
}
