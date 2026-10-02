import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, MessageCircle, QrCode, RefreshCw, Unlink, X } from 'lucide-react'
import { agentWeixinApi } from '@/lib/api'
import { toast } from '@/lib/useToast'

export interface WeixinPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

// QR 登录进展文案
const QR_STATUS_TEXT: Record<string, string> = {
  none: '',
  wait: '等待扫码…',
  scaned: '已扫码，请在手机上确认登录',
  confirmed: '登录成功',
  expired: '二维码已过期，请重新生成',
  binded_redirect: '该微信已绑定过此机器人，无需重复连接',
  need_verifycode: '需要在手机微信输入配对验证码，请重新扫码登录',
  verify_code_blocked: '配对验证码多次输入错误，请稍后再试',
  error: '登录失败，请重试',
}

// ── 微信接入面板：QR 扫码登录（iLink Bot API）/ 状态 / 解绑 ──
export function WeixinPanel({ onClose, bare }: WeixinPanelProps) {
  const queryClient = useQueryClient()
  const [qrImg, setQrImg] = useState<string | null>(null)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data: status } = useQuery({
    queryKey: ['agent-weixin-status'],
    queryFn: () => agentWeixinApi.status(),
    retry: false,
  })

  const loggedIn = status?.logged_in ?? false

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-weixin-status'] })

  const qrMut = useMutation({
    mutationFn: () => agentWeixinApi.qrcode(),
    onSuccess: (res) => {
      setQrImg(res.qrcode_img)
    },
    onError: (e: Error) => {
      setQrImg(null)
      toast('error', e.message || '生成二维码失败')
    },
  })

  // 登录进行中轮询 QR 状态；confirmed 后刷新总状态
  const qrPolling = qrImg !== null && !loggedIn
  const { data: qrStatus } = useQuery({
    queryKey: ['agent-weixin-qrcode-status'],
    queryFn: () => agentWeixinApi.qrcodeStatus(),
    refetchInterval: qrPolling ? 2000 : false,
    enabled: qrPolling,
    retry: false,
  })
  const qrState = qrStatus?.status ?? 'wait'
  useEffect(() => {
    if (qrState === 'confirmed') {
      setQrImg(null)
      invalidate()
    }
  }, [qrState, queryClient])
  // 过期 / 失败态停轮询（由 enabled 关闭 refetch），保留最后状态文案展示

  const unlinkMut = useMutation({
    mutationFn: () => agentWeixinApi.unlink(),
    onSuccess: () => {
      setQrImg(null)
      toast('success', '已解绑微信机器人')
      invalidate()
    },
    onError: (e: Error) => toast('error', e.message || '解绑失败'),
  })

  const qrStatusText = QR_STATUS_TEXT[qrState] ?? qrState

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="微信接入"
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
          <MessageCircle size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">微信接入</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭微信面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="space-y-3 overflow-y-auto px-4 py-4" style={{ touchAction: 'pan-y', overscrollBehavior: 'contain' }}>
          {/* 已登录状态卡 */}
          {loggedIn && (
            <div className="flex items-center gap-2 rounded-xl border border-[var(--ag-green)]/30 bg-[var(--ag-green)]/6 px-3 py-2.5">
              <Check size={14} className="shrink-0 text-[var(--ag-green)]" />
              <div className="min-w-0 flex-1 text-[12px] text-[var(--ag-text1)]">
                微信机器人已登录
                {status?.bot_id && (
                  <span className="ml-1 text-[10px] text-[var(--ag-text4)]">bot {status.bot_id}</span>
                )}
                {status?.linked && status.wxid && (
                  <div className="mt-0.5 text-[10px] text-[var(--ag-text4)]">已绑定微信：{status.wxid}</div>
                )}
              </div>
              <button
                type="button"
                onClick={() => unlinkMut.mutate()}
                disabled={unlinkMut.isPending}
                aria-label="解绑微信机器人"
                className="flex shrink-0 items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-red)] disabled:opacity-50"
              >
                <Unlink size={12} />
                解绑
              </button>
            </div>
          )}

          {/* 说明文案 */}
          <p className="text-[12px] leading-relaxed text-[var(--ag-text2)]">
            使用微信扫描二维码，将此微信账号授权为小天量化的微信机器人。登录后在微信里把网页端生成的
            6 位配对码发给机器人，即可绑定你的平台账号并直接发消息使唤助手。
          </p>

          {/* 未登录：QR 展示区 */}
          {!loggedIn && (
            <div className="flex flex-col items-center gap-2 rounded-xl border border-[var(--ag-stroke2)] px-3 py-4">
              {qrImg ? (
                <>
                  <img
                    src={qrImg.startsWith('http') ? qrImg : `data:image/png;base64,${qrImg}`}
                    alt="微信登录二维码"
                    className="size-44 rounded-lg border border-[var(--ag-stroke2)]"
                  />
                  <span className="text-[12px] text-[var(--ag-text2)]">{qrStatusText || '等待扫码…'}</span>
                  {(qrState === 'expired' || qrState === 'error' || qrState === 'need_verifycode' || qrState === 'verify_code_blocked') && (
                    <button
                      type="button"
                      onClick={() => qrMut.mutate()}
                      disabled={qrMut.isPending}
                      className="flex items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text2)] hover:text-[var(--ag-accent)] disabled:opacity-50"
                    >
                      <RefreshCw size={12} />
                      重新生成
                    </button>
                  )}
                </>
              ) : (
                <button
                  type="button"
                  onClick={() => qrMut.mutate()}
                  disabled={qrMut.isPending}
                  className="flex items-center gap-1.5 rounded-lg bg-[var(--ag-accent)] px-3 py-2 text-[12px] font-medium text-white disabled:opacity-50"
                >
                  <QrCode size={14} />
                  {qrMut.isPending ? '生成中…' : '生成登录二维码'}
                </button>
              )}
            </div>
          )}

          {/* 已登录补充说明 */}
          {loggedIn && (
            <p className="text-[11px] leading-relaxed text-[var(--ag-text3)]">
              重启网关后凭已保存的登录态自动恢复收消息。解绑会清除登录二维码授权并停止接收微信消息。
              {!status?.linked && ' 当前账号尚未绑定微信用户，请在网页端助手页生成配对码后发给机器人完成绑定。'}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

export default WeixinPanel
