import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Link2, QrCode, Unlink, X } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { toast } from '@/lib/useToast'
import { copyText } from '../types'

export interface PlatformLinkConfig<S> {
  /** 平台名（用于标题与 aria 标签），如「飞书」 */
  platform: string
  icon: React.ReactNode
  queryKey: string[]
  api: {
    status: () => Promise<S>
    pairCode: () => Promise<{ code: string; expires_in: number }>
    unlink: () => Promise<unknown>
  }
  configured: (s: S | undefined) => boolean
  linked: (s: S | undefined) => boolean
  /** 已绑定时展示的账号标识（如 open_id / staff_id），可空 */
  linkedId: (s: S | undefined) => string | undefined
  /** 未配置时的环境变量提示 */
  notConfiguredHint: React.ReactNode
  /** 未绑定时的配对引导文案 */
  pairInstructions: React.ReactNode
  /** 已绑定后的补充说明 */
  linkedHint: string
  /** 「扫码添加」能力（如 QQ url-link）：点击拉取链接并渲染二维码，可空 */
  qrLink?: {
    fetchUrl: () => Promise<{ url: string }>
    buttonLabel: string
    hint: React.ReactNode
  }
}

export interface PlatformLinkPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

function fmtCountdown(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

// ── 平台接入面板共享骨架（飞书 / 钉钉）：状态卡 + 配对码（倒计时）+ 解绑 ──
export function PlatformLinkPanel<S>({
  config,
  onClose,
  bare,
}: { config: PlatformLinkConfig<S> } & PlatformLinkPanelProps) {
  const queryClient = useQueryClient()
  const [code, setCode] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [secondsLeft, setSecondsLeft] = useState(0)
  const [confirmUnlink, setConfirmUnlink] = useState(false)
  const [qrUrl, setQrUrl] = useState<string | null>(null)
  const [qrLoading, setQrLoading] = useState(false)

  const loadQr = async () => {
    if (!config.qrLink) return
    setQrLoading(true)
    try {
      const res = await config.qrLink.fetchUrl()
      setQrUrl(res.url)
    } catch (e) {
      toast('error', e instanceof Error ? e.message : '获取二维码失败')
    } finally {
      setQrLoading(false)
    }
  }

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  // 配对码有效期倒计时：归零后自动清除配对码
  useEffect(() => {
    if (!code) return
    const t = setInterval(() => setSecondsLeft((s) => Math.max(0, s - 1)), 1000)
    return () => clearInterval(t)
  }, [code])

  useEffect(() => {
    if (code && secondsLeft <= 0) setCode(null)
  }, [code, secondsLeft])

  const { data: status } = useQuery({
    queryKey: config.queryKey,
    queryFn: () => config.api.status(),
    retry: false,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: config.queryKey })

  const pairMut = useMutation({
    mutationFn: () => config.api.pairCode(),
    onSuccess: (res) => {
      setCode(res.code)
      setSecondsLeft(res.expires_in || 600)
      setCopied(false)
    },
    onError: (e: Error) => toast('error', e.message || '生成失败'),
  })

  const unlinkMut = useMutation({
    mutationFn: () => config.api.unlink(),
    onSuccess: () => {
      setCode(null)
      setConfirmUnlink(false)
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

  const configured = config.configured(status)
  const linked = config.linked(status)
  const linkedId = config.linkedId(status)

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label={`${config.platform} 接入`}
    >
      <div
        className={
          bare
            ? 'mx-auto flex min-h-0 w-full max-w-2xl flex-1 flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
            : 'w-full max-w-md overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
        }
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <span className="text-[var(--ag-accent)]">{config.icon}</span>
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">{config.platform} 接入</span>
          <span
            aria-label={`${config.platform} 配置状态`}
            className={`rounded-full border px-1.5 py-px text-[10px] font-medium ${
              configured
                ? 'border-[var(--ag-green)]/40 bg-[var(--ag-green)]/10 text-[var(--ag-green)]'
                : 'border-[var(--ag-stroke3)] text-[var(--ag-text4)]'
            }`}
          >
            {configured ? '已配置' : '未配置'}
          </span>
          <span
            aria-label={`${config.platform} 绑定状态`}
            className={`rounded-full border px-1.5 py-px text-[10px] font-medium ${
              linked
                ? 'border-[var(--ag-green)]/40 bg-[var(--ag-green)]/10 text-[var(--ag-green)]'
                : 'border-[var(--ag-stroke3)] text-[var(--ag-text4)]'
            }`}
          >
            {linked ? '已绑定' : '未绑定'}
          </span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label={`关闭 ${config.platform} 面板`}
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="space-y-3 px-4 py-4">
          {/* 配置态 */}
          {!configured && (
            <div className="rounded-xl border border-[var(--ag-amber)]/30 bg-[var(--ag-amber)]/6 px-3 py-2.5 text-[12px] leading-relaxed text-[var(--ag-text2)]">
              {config.notConfiguredHint}
            </div>
          )}

          {/* 绑定态 */}
          {configured && linked && (
            <div className="flex items-center gap-2 rounded-xl border border-[var(--ag-green)]/30 bg-[var(--ag-green)]/6 px-3 py-2.5">
              <Check size={14} className="shrink-0 text-[var(--ag-green)]" />
              <div className="min-w-0 flex-1 text-[12px] text-[var(--ag-text1)]">
                已绑定
                {linkedId && <span className="ml-1 font-mono text-[10px] text-[var(--ag-text4)]">{linkedId}</span>}
              </div>
              {confirmUnlink ? (
                <button
                  type="button"
                  onClick={() => unlinkMut.mutate()}
                  disabled={unlinkMut.isPending}
                  aria-label={`确认解绑 ${config.platform}`}
                  className="flex shrink-0 items-center gap-1 rounded-md border border-[var(--ag-red)]/40 px-2 py-1 text-[11px] font-medium text-[var(--ag-red)] hover:bg-[var(--ag-red)]/8 disabled:opacity-50"
                >
                  <Unlink size={11} />
                  确认解绑？
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => setConfirmUnlink(true)}
                  aria-label={`解绑 ${config.platform}`}
                  className="flex shrink-0 items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-red)]"
                >
                  <Unlink size={11} />
                  解绑
                </button>
              )}
            </div>
          )}

          {configured && linked && (
            <p className="text-[11px] leading-relaxed text-[var(--ag-text3)]">{config.linkedHint}</p>
          )}

          {/* 未绑定 → 配对 */}
          {configured && !linked && (
            <>
              {config.qrLink && (
                <div className="flex flex-col items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-3 py-3">
                  {qrUrl ? (
                    <>
                      <QRCodeSVG value={qrUrl} size={150} />
                      <p className="text-center text-[11px] leading-relaxed text-[var(--ag-text3)]">
                        {config.qrLink.hint}
                      </p>
                    </>
                  ) : (
                    <button
                      type="button"
                      onClick={loadQr}
                      disabled={qrLoading}
                      className="flex items-center gap-1.5 rounded-md border border-[var(--ag-stroke2)] px-3 py-1.5 text-[12px] text-[var(--ag-text2)] hover:text-[var(--ag-text1)] disabled:opacity-50"
                    >
                      <QrCode size={13} />
                      {qrLoading ? '生成中…' : config.qrLink.buttonLabel}
                    </button>
                  )}
                </div>
              )}
              <p className="text-[12px] leading-relaxed text-[var(--ag-text2)]">{config.pairInstructions}</p>
              {code ? (
                <div className="flex items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-3 py-2.5">
                  <span className="font-mono text-[20px] font-bold tracking-[0.3em] text-[var(--ag-accent)]">
                    {code}
                  </span>
                  <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]" aria-label="配对码倒计时">
                    {fmtCountdown(secondsLeft)}
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

export default PlatformLinkPanel
