import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { authApi, mfaApi } from '@/lib/api'
import { SectionCard } from '@/components/ui/SectionCard'
import { cn } from '@/lib/utils'
import { Check, Copy, Loader2, ShieldCheck, ShieldOff } from 'lucide-react'

// ── 两步验证 (TOTP) 设置区块（A3.1） ──
// 流程：setup（生成密钥）→ 在 authenticator 应用中录入 → enable（校验动态码）
// → 展示一次性备用码。已启用时可凭动态码关闭。

type Phase = 'idle' | 'setup' | 'confirm' | 'backup'

export function MfaSection() {
  const queryClient = useQueryClient()
  const [phase, setPhase] = useState<Phase>('idle')
  const [secret, setSecret] = useState('')
  const [otpauthUri, setOtpauthUri] = useState('')
  const [code, setCode] = useState('')
  const [backupCodes, setBackupCodes] = useState<string[]>([])
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState('')

  const { data: me } = useQuery({ queryKey: ['auth-me'], queryFn: () => authApi.me() })
  const enabled = !!me?.totp_enabled

  const refreshStatus = () => {
    void queryClient.invalidateQueries({ queryKey: ['auth-me'] })
    void queryClient.invalidateQueries({ queryKey: ['auth-user'] })
  }

  const setupMut = useMutation({
    mutationFn: () => mfaApi.setup(),
    onSuccess: (data) => {
      setSecret(data.secret)
      setOtpauthUri(data.otpauth_uri)
      setCode('')
      setError('')
      setPhase('confirm')
    },
    onError: (e: Error) => setError(e.message),
  })

  const enableMut = useMutation({
    mutationFn: (c: string) => mfaApi.enable(c),
    onSuccess: (data) => {
      setBackupCodes(data.backup_codes || [])
      setPhase('backup')
      refreshStatus()
    },
    onError: (e: Error) => setError(e.message),
  })

  const disableMut = useMutation({
    mutationFn: (c: string) => mfaApi.disable(c),
    onSuccess: () => {
      setPhase('idle')
      setCode('')
      setError('')
      refreshStatus()
    },
    onError: (e: Error) => setError(e.message),
  })

  const copyBackupCodes = () => {
    navigator.clipboard.writeText(backupCodes.join('\n')).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  const inputCls =
    'w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold'

  return (
    <SectionCard title="两步验证 (TOTP)" bodyClassName="space-y-4">
      <p className="text-xs text-muted-foreground">
        为账户登录增加第二层保护：输入密码后还需输入 authenticator 应用中的 6 位动态码。
        新 IP 登录检测将与 MFA 联动提醒。
      </p>

      {error && <p className="text-xs text-red-400">{error}</p>}

      {/* ── 未启用：引导开启 ── */}
      {!enabled && phase === 'idle' && (
        <button
          onClick={() => setupMut.mutate()}
          disabled={setupMut.isPending}
          className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {setupMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="h-3.5 w-3.5" />}
          开始设置
        </button>
      )}

      {/* ── 第一步完成：展示密钥，等待录入后确认 ── */}
      {!enabled && phase === 'confirm' && (
        <div className="space-y-3">
          <div className="rounded-lg border border-quant-border bg-quant-bg p-3">
            <p className="mb-1 text-[11px] text-muted-foreground">1. 在 authenticator 应用中手动添加，扫描/输入以下密钥：</p>
            <code className="block select-all break-all rounded bg-quant-bg-secondary px-2 py-1.5 text-sm tracking-wider text-quant-gold">
              {secret}
            </code>
            <p className="mt-1.5 mb-1 text-[11px] text-muted-foreground">或复制 otpauth URI：</p>
            <button
              onClick={() => navigator.clipboard.writeText(otpauthUri)}
              className="w-full truncate rounded bg-quant-bg-secondary px-2 py-1.5 text-left text-[10px] text-muted-foreground hover:text-foreground"
              title={otpauthUri}
            >
              {otpauthUri}
            </button>
          </div>
          <div>
            <p className="mb-1 text-[11px] text-muted-foreground">2. 输入应用中显示的 6 位动态码完成启用：</p>
            <input
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              placeholder="6 位动态码"
              inputMode="numeric"
              className={cn(inputCls, 'text-center text-lg tracking-[0.3em]')}
            />
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => enableMut.mutate(code)}
              disabled={enableMut.isPending || code.length !== 6}
              className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {enableMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
              启用两步验证
            </button>
            <button
              onClick={() => setPhase('idle')}
              className="rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
            >
              取消
            </button>
          </div>
        </div>
      )}

      {/* ── 启用成功：一次性展示备用码 ── */}
      {!enabled && phase === 'backup' && (
        <div className="space-y-3">
          <div className="flex items-center gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-400">
            <Check className="h-3.5 w-3.5" />
            两步验证已启用。请立即保存以下一次性备用码（丢失设备时用于登录），此列表仅显示一次：
          </div>
          <div className="grid grid-cols-2 gap-1.5 rounded-lg border border-quant-border bg-quant-bg p-3">
            {backupCodes.map((c) => (
              <code key={c} className="select-all text-center text-sm tracking-wider text-foreground">
                {c}
              </code>
            ))}
          </div>
          <button
            onClick={copyBackupCodes}
            className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
            {copied ? '已复制' : '复制全部备用码'}
          </button>
          <button
            onClick={() => { setPhase('idle'); setCode(''); setBackupCodes([]) }}
            className="block rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90"
          >
            我已妥善保存
          </button>
        </div>
      )}

      {/* ── 已启用：状态 + 关闭 ── */}
      {enabled && phase !== 'confirm' && (
        <div className="space-y-3">
          <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
            <div>
              <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                <ShieldCheck className="h-4 w-4 text-emerald-400" />
                两步验证已启用
              </div>
              <div className="mt-0.5 text-xs text-muted-foreground">登录时需输入 authenticator 动态码或备用码</div>
            </div>
            <button
              onClick={() => setPhase(phase === 'setup' ? 'idle' : 'setup')}
              className="flex items-center gap-1.5 rounded-md border border-red-400/30 bg-red-400/10 px-3 py-1.5 text-xs font-medium text-red-400 transition-colors hover:bg-red-400/20"
            >
              <ShieldOff className="h-3.5 w-3.5" />
              关闭
            </button>
          </div>
          {phase === 'setup' && (
            <div className="flex items-center gap-2">
              <input
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="输入当前 6 位动态码以确认关闭"
                inputMode="numeric"
                className={cn(inputCls, 'max-w-[240px] text-center tracking-[0.3em]')}
              />
              <button
                onClick={() => disableMut.mutate(code)}
                disabled={disableMut.isPending || code.length !== 6}
                className="flex items-center gap-1.5 rounded-md bg-red-400 px-3 py-2 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {disableMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldOff className="h-3.5 w-3.5" />}
                确认关闭
              </button>
              <button
                onClick={() => { setPhase('idle'); setCode(''); setError('') }}
                className="rounded-md border border-quant-border bg-quant-card px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
              >
                取消
              </button>
            </div>
          )}
        </div>
      )}
    </SectionCard>
  )
}
