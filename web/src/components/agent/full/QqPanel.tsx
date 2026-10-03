import { BotMessageSquare } from 'lucide-react'
import { agentQqApi, type AgentQqStatus } from '@/lib/api'
import { PlatformLinkPanel, type PlatformLinkPanelProps } from './PlatformLinkPanel'

// ── QQ 接入面板：配对绑定 / 状态 / 解绑 ──
export function QqPanel({ onClose, bare }: PlatformLinkPanelProps) {
  return (
    <PlatformLinkPanel<AgentQqStatus>
      onClose={onClose}
      bare={bare}
      config={{
        platform: 'QQ',
        icon: <BotMessageSquare size={15} />,
        queryKey: ['agent-qq-status'],
        api: agentQqApi,
        configured: (s) => s?.configured ?? false,
        linked: (s) => s?.linked ?? false,
        linkedId: (s) => s?.open_id,
        notConfiguredHint: (
          <>
            网关尚未配置 QQ 机器人：设置环境变量{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">QQ_APP_ID</code> 与{' '}
            <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">QQ_APP_SECRET</code>{' '}
            后重启网关（在 QQ 开放平台创建机器人获取）。
          </>
        ),
        pairInstructions: <>扫码添加机器人（或搜索机器人名称），然后在 QQ 私聊中把配对码发送给它完成绑定：</>,
        connectorQr: {
          start: (restart) => agentQqApi.connectorQr(restart),
          status: () => agentQqApi.connectorStatus(),
          description: (
            <>
              还没配置 QQ 机器人？用手机 QQ 扫下方二维码一键接入：扫码确认后自动创建/绑定机器人并获取凭据，
              无需手动申请 AppID/AppSecret、无需配置 IP 白名单。
            </>
          ),
          waitingHint: '等待手机 QQ 扫码确认…（如需新机器人请选「创建新机器人」）',
          successHint: '扫码绑定成功，凭据已自动注入网关，可以继续下面的配对绑定。',
          allowRebind: true,
        },
        qrLink: {
          fetchUrl: () => agentQqApi.urlLink(),
          buttonLabel: '生成「添加机器人」二维码',
          hint: '用手机 QQ 扫描上方二维码，将机器人添加为好友后，在下面生成配对码并发给机器人完成绑定。',
        },
        linkedHint: '现在可以直接在 QQ 私聊里给机器人发消息使唤助手；定时任务选择「QQ」投递也会发到你的私聊。',
      }}
    />
  )
}

export default QqPanel
