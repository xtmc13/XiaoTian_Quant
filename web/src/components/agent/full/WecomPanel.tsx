import { MessagesSquare } from 'lucide-react'
import { agentWecomApi, type AgentWecomStatus } from '@/lib/api'
import { PlatformLinkPanel, type PlatformLinkPanelProps } from './PlatformLinkPanel'

// ── 企业微信接入面板：配对绑定 / 状态 / 解绑 ──
export function WecomPanel({ onClose, bare }: PlatformLinkPanelProps) {
  return (
    <PlatformLinkPanel<AgentWecomStatus>
      onClose={onClose}
      bare={bare}
      config={{
        platform: '企业微信',
        icon: <MessagesSquare size={15} />,
        queryKey: ['agent-wecom-status'],
        api: agentWecomApi,
        configured: (s) => s?.configured ?? false,
        linked: (s) => s?.linked ?? false,
        linkedId: (s) => s?.staff_id,
        notConfiguredHint: (
          <>
            网关尚未配置企业微信应用：设置环境变量{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">WECOM_CORP_ID</code> 与{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">WECOM_SECRET</code>{' '}
            后重启网关（在企业微信管理后台创建自建应用获取）。
          </>
        ),
        pairInstructions: <>生成配对码后，在企业微信应用消息中把配对码发送给应用完成绑定：</>,
        linkedHint: '现在可以直接在企业微信应用消息里使唤助手；定时任务选择「企业微信」投递也会发到你的应用消息。',
      }}
    />
  )
}

export default WecomPanel
