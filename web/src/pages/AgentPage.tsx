import { AgentChatPanel } from '@/components/agent/AgentChatPanel'

// 助手整页路由（/agent）：与交易系统页面同一文档流渲染，
// 不经过桌宠的 fixed + clip-path 悬浮容器——触屏滚动与交易页完全一致。
export default function AgentPage() {
  return (
    <div className="relative h-full overflow-hidden" aria-label="AI 助手页">
      <AgentChatPanel variant="full" open onClose={() => {}} />
    </div>
  )
}
