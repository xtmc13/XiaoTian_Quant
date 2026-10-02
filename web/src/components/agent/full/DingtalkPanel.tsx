import { Bell } from 'lucide-react'
import { agentDingtalkApi, type AgentDingtalkStatus } from '@/lib/api'
import { PlatformLinkPanel, type PlatformLinkPanelProps } from './PlatformLinkPanel'

// ── 钉钉接入面板：配对绑定 / 状态 / 解绑 ──
export function DingtalkPanel({ onClose, bare }: PlatformLinkPanelProps) {
  return (
    <PlatformLinkPanel<AgentDingtalkStatus>
      onClose={onClose}
      bare={bare}
      config={{
        platform: '钉钉',
        icon: <Bell size={15} />,
        queryKey: ['agent-dingtalk-status'],
        api: agentDingtalkApi,
        configured: (s) => s?.configured ?? false,
        linked: (s) => s?.linked ?? false,
        // 后端绑定 id 字段可能是 staff_id 或 open_id，读存在的那个
        linkedId: (s) => s?.staff_id ?? s?.open_id,
        notConfiguredHint: (
          <>
            网关尚未配置钉钉应用：设置环境变量{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">DINGTALK_CLIENT_ID</code>{' '}
            与{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">
              DINGTALK_CLIENT_SECRET
            </code>{' '}
            后重启网关（在钉钉开放平台创建应用获取）。
          </>
        ),
        pairInstructions: (
          <>
            在钉钉里打开你的应用机器人并发送{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">/start</code>
            ，然后生成配对码发给它：
          </>
        ),
        linkedHint: '现在可以直接在钉钉里给机器人发消息使唤助手；定时任务选择「钉钉」投递也会发到你的聊天。',
      }}
    />
  )
}

export default DingtalkPanel
