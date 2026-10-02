import { Bird } from 'lucide-react'
import { agentFeishuApi, type AgentFeishuStatus } from '@/lib/api'
import { PlatformLinkPanel, type PlatformLinkPanelProps } from './PlatformLinkPanel'

// ── 飞书接入面板：配对绑定 / 状态 / 解绑 ──
export function FeishuPanel({ onClose, bare }: PlatformLinkPanelProps) {
  return (
    <PlatformLinkPanel<AgentFeishuStatus>
      onClose={onClose}
      bare={bare}
      config={{
        platform: '飞书',
        icon: <Bird size={15} />,
        queryKey: ['agent-feishu-status'],
        api: agentFeishuApi,
        configured: (s) => s?.configured ?? false,
        linked: (s) => s?.linked ?? false,
        linkedId: (s) => s?.open_id,
        notConfiguredHint: (
          <>
            网关尚未配置飞书应用：设置环境变量{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">FEISHU_APP_ID</code> 与{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">FEISHU_APP_SECRET</code>{' '}
            后重启网关（在飞书开放平台创建自建应用获取）。
          </>
        ),
        pairInstructions: (
          <>
            在飞书里打开你的应用机器人并发送{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">/start</code>
            ，然后生成配对码发给它：
          </>
        ),
        linkedHint: '现在可以直接在飞书里给机器人发消息使唤助手；定时任务选择「飞书」投递也会发到你的聊天。',
      }}
    />
  )
}

export default FeishuPanel
