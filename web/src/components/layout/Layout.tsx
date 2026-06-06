import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'
import { BottomNav } from './BottomNav'
import { ToastContainer } from '@/components/ToastContainer'
import { PWAInstallPrompt } from '@/components/PWAInstallPrompt'

export function Layout() {
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
          <Outlet />
        </main>
      </div>
      {/* Bottom navigation — visible only on mobile (< md) */}
      <BottomNav />
      <ToastContainer />
      <PWAInstallPrompt />
    </div>
  )
}
