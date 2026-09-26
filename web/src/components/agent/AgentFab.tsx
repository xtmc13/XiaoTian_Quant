import { useState } from 'react'
import { useLocation } from 'react-router-dom'
import { BrainCircuit } from 'lucide-react'
import { useAuthStore } from '@/stores/authStore'
import { cn } from '@/lib/utils'
import { AgentChatPanel } from './AgentChatPanel'

export function AgentFab() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const location = useLocation()
  const [open, setOpen] = useState(false)
  const [unread, setUnread] = useState(0)

  // 未登录或登录页不渲染
  if (!isAuthenticated || location.pathname === '/login') return null

  return (
    <>
      <button
        type="button"
        aria-label={open ? '收起小天助手' : '打开小天助手'}
        aria-expanded={open}
        onClick={() => {
          setOpen((v) => !v)
          if (!open) setUnread(0)
        }}
        className={cn(
          'fixed bottom-20 right-5 z-50 md:bottom-6 md:right-6',
          'flex h-14 w-14 items-center justify-center rounded-full',
          'bg-gradient-to-br from-[#1890ff] to-[#36cfc9]',
          'shadow-lg shadow-[#1890ff]/30 transition-transform duration-200',
          'hover:scale-105 active:scale-95',
          'focus:outline-none focus:ring-2 focus:ring-[#1890ff]/50 focus:ring-offset-2 focus:ring-offset-quant-bg'
        )}
      >
        <BrainCircuit className="text-white" size={24} />
        {unread > 0 && !open && (
          <span
            aria-label={`${unread} 条未读消息`}
            className="absolute -top-0.5 -right-0.5 flex h-5 min-w-[20px] items-center justify-center rounded-full bg-[#f5222d] px-1 text-[10px] font-bold text-white ring-2 ring-quant-card"
          >
            {unread > 9 ? '9+' : unread}
          </span>
        )}
      </button>
      <AgentChatPanel
        open={open}
        onClose={() => setOpen(false)}
        onUnread={() => setUnread((n) => Math.min(n + 1, 99))}
      />
    </>
  )
}

export default AgentFab
