import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Link2, Send, Unlink, X } from 'lucide-react'
import { agentTelegramApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { copyText } from '../types'

export interface TelegramPanelProps {
  onClose: () => void
}

// ── Telegram 接入面板：配对绑定 / 状态 / 解绑 ──
export function TelegramPanel({ onClose }: TelegramPanelProps) {
  const queryClient = useQueryClient()
  const [code, setCode] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data: status } = useQuery({
    queryKey: ['agent-telegram-status'],
    queryFn: () => agentTelegramApi.status(),
    retry: false,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-telegram-status'] })

  const pairMut = useMutation({
    mutationFn: () => agentTelegramApi.pairCode(),
    onSuccess: (res) => {
      setCode(res.code)
      setCopied(false)
    },
    onError: (e: Error) => toast('error', e.message || '生成失败'),
  })

  const unlinkMut = useMutation({
    mutationFn: () => agentTelegramApi.unlink(),
    onSuccess: () => {
      setCode(null)
      toast('success', '已解绑')
      invalidate()
    },
    onError: () => toast('error', '解绑失败'),
  })

  const doCopy = async () => {
    if (code && (await copyText(code))) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }

  const configured = status?.configured ?? false
  const linked = status?.linked ?? false

  return (
    <div
      className="absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="Telegram 接入"
    >
      <div
        className="w-full max-w-md overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <Send size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">Telegram 接入</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭 Telegram 面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-white/8"
          >
            <X size={15} />
          </button>
        </div>

        <div className="space-y-3 px-4 py-4">
          {/* 配置态 */}
          {!configured && (
            <div className="rounded-xl border border-[var(--ag-amber)]/30 bg-[var(--ag-amber)]/6 px-3 py-2.5 text-[12px] leading-relaxed text-[var(--ag-text2)]">
              网关尚未配置 Telegram Bot：设置环境变量{' '}
              <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">
                TELEGRAM_BOT_TOKEN
              </code>{' '}
              后重启网关 （向 @BotFather 申请 token）。出站通知通道复用同一变量。
            </div>
          )}

          {/* 绑定态 */}
          {configured && linked && (
            <div className="flex items-center gap-2 rounded-xl border border-[var(--ag-green)]/30 bg-[var(--ag-green)]/6 px-3 py-2.5">
              <Check size={14} className="shrink-0 text-[var(--ag-green)]" />
              <div className="min-w-0 flex-1 text-[12px] text-[var(--ag-text1)]">
                已绑定 {status?.username ? `@${status.username}` : ''}
                <span className="ml-1 text-[10px] text-[var(--ag-text4)]">chat {status?.chat_id}</span>
              </div>
              <button
                type="button"
                onClick={() => unlinkMut.mutate()}
                disabled={unlinkMut.isPending}
                aria-label="解绑 Telegram"
                className="flex shrink-0 items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-red)] disabled:opacity-50"
              >
                <Unlink size={11} />
                解绑
              </button>
            </div>
          )}

          {configured && linked && (
            <p className="text-[11px] leading-relaxed text-[var(--ag-text3)]">
              现在可以直接给 bot 发消息使唤助手；定时任务选择「Telegram」投递也会发到你的聊天。
            </p>
          )}

          {/* 未绑定 → 配对 */}
          {configured && !linked && (
            <>
              <p className="text-[12px] leading-relaxed text-[var(--ag-text2)]">
                在 Telegram 里打开你的 bot 并发送{' '}
                <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 font-mono text-[11px]">/start</code>
                ，然后生成配对码发给它：
              </p>
              {code ? (
                <div className="flex items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-3 py-2.5">
                  <span className="font-mono text-[20px] font-bold tracking-[0.3em] text-[var(--ag-accent)]">
                    {code}
                  </span>
                  <span className="min-w-0 flex-1" />
                  <button
                    type="button"
                    onClick={doCopy}
                    aria-label="复制配对码"
                    className="flex items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-text1)]"
                  >
                    {copied ? <Check size={11} className="text-[var(--ag-green)]" /> : <Copy size={11} />}
                    {copied ? '已复制' : '复制'}
                  </button>
                </div>
              ) : (
                <button
                  type="button"
                  onClick={() => pairMut.mutate()}
                  disabled={pairMut.isPending}
                  className="flex w-full items-center justify-center gap-1.5 rounded-xl bg-[var(--ag-text1)] px-3 py-2 text-[12px] font-medium text-[var(--ag-bg)] hover:opacity-85 disabled:opacity-50"
                >
                  <Link2 size={13} />
                  {pairMut.isPending ? '生成中…' : '生成配对码（10 分钟有效）'}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

export default TelegramPanel
