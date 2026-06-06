import { useState, useEffect } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useAuthStore } from '@/stores/authStore'
import { useI18n } from '@/i18n'
import { cn } from '@/lib/utils'
import { Zap, Eye, EyeOff, Loader2, AlertCircle, CheckCircle, Mail, ArrowRight } from 'lucide-react'

type Tab = 'login' | 'register' | 'reset'

interface CodeState {
  sent: boolean
  countdown: number
}

export function Login() {
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useI18n()
  const { login, loginByCode, register, sendCode, resetPassword: doResetPassword, isAuthenticated, isLoading, error, clearError } = useAuthStore()

  // ── Tab state ──
  const [tab, setTab] = useState<Tab>('login')
  const [successMsg, setSuccessMsg] = useState('')

  // ── Login form ──
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)

  // ── Register form ──
  const [regUsername, setRegUsername] = useState('')
  const [regEmail, setRegEmail] = useState('')
  const [regPassword, setRegPassword] = useState('')
  const [regShowPw, setRegShowPw] = useState(false)
  const [regCode, setRegCode] = useState('')
  const [regCodeState, setRegCodeState] = useState<CodeState>({ sent: false, countdown: 0 })

  // ── Reset password form ──
  const [resetEmail, setResetEmail] = useState('')
  const [resetCode, setResetCode] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [resetShowPw, setResetShowPw] = useState(false)
  const [resetCodeState, setResetCodeState] = useState<CodeState>({ sent: false, countdown: 0 })

  // ── Redirect if authenticated ──
  useEffect(() => {
    if (isAuthenticated) {
      const from = (location.state as { from?: { pathname: string } } | null)?.from?.pathname || '/dashboard'
      navigate(from, { replace: true })
    }
  }, [isAuthenticated, navigate, location.state])

  useEffect(() => { clearError() }, [clearError])

  // ── Code countdown timer ──
  useEffect(() => {
    if (regCodeState.countdown > 0) {
      const t = setTimeout(() => setRegCodeState(s => ({ ...s, countdown: s.countdown - 1 })), 1000)
      return () => clearTimeout(t)
    }
  }, [regCodeState.countdown])

  useEffect(() => {
    if (resetCodeState.countdown > 0) {
      const t = setTimeout(() => setResetCodeState(s => ({ ...s, countdown: s.countdown - 1 })), 1000)
      return () => clearTimeout(t)
    }
  }, [resetCodeState.countdown])

  // ── Actions ──
  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim() || !password.trim()) return
    try { await login(username, password) } catch { /* store sets error */ }
  }

  const handleSendRegCode = async () => {
    if (!regEmail.includes('@')) return
    try {
      await sendCode(regEmail, 'register')
      setRegCodeState({ sent: true, countdown: 60 })
    } catch { /* store sets error */ }
  }

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!regUsername.trim() || !regPassword.trim() || !regEmail.trim() || !regCode.trim()) return
    try {
      await register({ username: regUsername, password: regPassword, email: regEmail, code: regCode, nickname: regUsername })
    } catch { /* store sets error */ }
  }

  const handleSendResetCode = async () => {
    if (!resetEmail.includes('@')) return
    try {
      await sendCode(resetEmail, 'reset_password')
      setResetCodeState({ sent: true, countdown: 60 })
    } catch { /* store sets error */ }
  }

  const handleResetPassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!resetEmail.trim() || !resetCode.trim() || !newPassword.trim()) return
    try {
      await doResetPassword(resetEmail, resetCode, newPassword)
      setSuccessMsg(t('auth.loginSuccess'))
      setTab('login')
      setPassword('')
    } catch { /* store sets error */ }
  }

  const switchTab = (t: Tab) => {
    setTab(t)
    clearError()
    setSuccessMsg('')
  }

  // ── Shared styles ──
  const inputCls = 'w-full rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2.5 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold'
  const labelCls = 'text-xs font-medium text-muted-foreground'
  const btnCls = 'flex w-full items-center justify-center gap-2 rounded-lg bg-quant-gold px-4 py-2.5 text-sm font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed'

  return (
    <div className="flex min-h-screen items-center justify-center bg-quant-bg p-4">
      <div className="w-full max-w-sm space-y-6">
        {/* Logo */}
        <div className="flex flex-col items-center gap-3">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-quant-gold text-white shadow-lg shadow-quant-gold/20">
            <Zap className="h-6 w-6" />
          </div>
          <h1 className="text-xl font-bold tracking-tight text-white">小天量化</h1>
          <p className="text-xs text-muted-foreground">AI 驱动的量化交易平台</p>
        </div>

        {/* Tab switcher */}
        <div className="flex rounded-lg bg-quant-bg-secondary p-1">
          {(['login', 'register', 'reset'] as const).map((tabKey) => (
            <button
              key={tabKey}
              onClick={() => switchTab(tabKey)}
              className={cn(
                'flex-1 rounded-md py-1.5 text-xs font-medium transition-colors',
                tab === tabKey ? 'bg-quant-gold text-black' : 'text-muted-foreground hover:text-white'
              )}
            >
              {tabKey === 'login' ? t('auth.login') : tabKey === 'register' ? t('auth.register') : t('auth.resetPassword')}
            </button>
          ))}
        </div>

        {/* Success message */}
        {successMsg && (
          <div className="flex items-center gap-2 rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-2 text-xs text-green-400">
            <CheckCircle className="h-3.5 w-3.5 shrink-0" />
            {successMsg}
          </div>
        )}

        {/* Error message */}
        {error && (
          <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400">
            <AlertCircle className="h-3.5 w-3.5 shrink-0" />
            {error}
          </div>
        )}

        {/* ══════ LOGIN TAB ══════ */}
        {tab === 'login' && (
          <form onSubmit={handleLogin} className="space-y-4">
            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.username')}</label>
              <input type="text" value={username} onChange={e => setUsername(e.target.value)}
                placeholder={t('auth.inputUsername')} autoComplete="username" className={inputCls} />
            </div>

            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.password')}</label>
              <div className="relative">
                <input type={showPassword ? 'text' : 'password'} value={password}
                  onChange={e => setPassword(e.target.value)}
                  placeholder="输入密码" autoComplete="current-password"
                  className={cn(inputCls, 'pr-10')} />
                <button type="button" onClick={() => setShowPassword(!showPassword)}
                  aria-label={showPassword ? '隐藏密码' : '显示密码'}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                  {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>

            <button type="submit" disabled={isLoading || !username.trim() || !password.trim()} className={btnCls}>
              {isLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Zap className="h-4 w-4" />}
              {isLoading ? t('common.loading') : t('auth.loginBtn')}
            </button>

            <p className="text-center text-[11px] text-muted-foreground">
              {t('auth.noAccount')}<button type="button" onClick={() => switchTab('register')} className="text-quant-gold hover:underline">{t('auth.register')}</button>
            </p>

            {/* OAuth buttons */}
            <div className="pt-2 border-t border-quant-border">
              <p className="text-[10px] text-muted-foreground text-center mb-2">{t('auth.thirdPartyLogin')}</p>
              <div className="flex gap-2">
                <a href="/api/auth/oauth/google/login"
                  className="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-lg border border-quant-border text-xs hover:bg-white/5 transition-colors">
                  <svg className="h-4 w-4" viewBox="0 0 24 24"><path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 01-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"/><path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/><path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"/><path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"/></svg>
                  Google
                </a>
                <a href="/api/auth/oauth/github/login"
                  className="flex-1 flex items-center justify-center gap-1.5 py-2 rounded-lg border border-quant-border text-xs hover:bg-white/5 transition-colors">
                  <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>
                  GitHub
                </a>
              </div>
            </div>
          </form>
        )}

        {/* ══════ REGISTER TAB ══════ */}
        {tab === 'register' && (
          <form onSubmit={handleRegister} className="space-y-3">
            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.email')}</label>
              <div className="flex gap-2">
                <div className="relative flex-1">
                  <Mail className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                  <input type="email" value={regEmail} onChange={e => setRegEmail(e.target.value)}
                    placeholder={t('auth.inputEmail')} autoComplete="email" className={cn(inputCls, 'pl-9')} />
                </div>
                <button type="button" onClick={handleSendRegCode}
                  disabled={regCodeState.countdown > 0 || !regEmail.includes('@') || isLoading}
                  className="shrink-0 rounded-lg bg-quant-bg-secondary px-3 py-2.5 text-xs font-medium text-quant-gold border border-quant-gold/30 hover:bg-quant-gold/10 disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap">
                  {regCodeState.countdown > 0 ? `${regCodeState.countdown}s` : t('auth.sendCode')}
                </button>
              </div>
            </div>

            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.verificationCode')}</label>
              <input type="text" value={regCode} onChange={e => setRegCode(e.target.value)}
                placeholder={t('auth.inputCode')} maxLength={6} className={inputCls} />
            </div>

            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.username')}</label>
              <input type="text" value={regUsername} onChange={e => setRegUsername(e.target.value)}
                placeholder={t('auth.minChars', '至少3个字符').replace('{count}', '3')} autoComplete="username" className={inputCls} />
            </div>

            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.password')}</label>
              <div className="relative">
                <input type={regShowPw ? 'text' : 'password'} value={regPassword}
                  onChange={e => setRegPassword(e.target.value)}
                  placeholder="至少6个字符" autoComplete="new-password"
                  className={cn(inputCls, 'pr-10')} />
                <button type="button" onClick={() => setRegShowPw(!regShowPw)}
                  aria-label={regShowPw ? '隐藏密码' : '显示密码'}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                  {regShowPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>

            <button type="submit"
              disabled={isLoading || !regUsername.trim() || !regPassword.trim() || !regEmail.trim() || !regCode.trim()}
              className={btnCls}>
              {isLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowRight className="h-4 w-4" />}
              {isLoading ? t('common.loading') : t('auth.registerBtn')}
            </button>

            <p className="text-center text-[11px] text-muted-foreground">
              {t('auth.hasAccount')}<button type="button" onClick={() => switchTab('login')} className="text-quant-gold hover:underline">{t('auth.login')}</button>
            </p>
          </form>
        )}

        {/* ══════ RESET PASSWORD TAB ══════ */}
        {tab === 'reset' && (
          <form onSubmit={handleResetPassword} className="space-y-3">
            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.email')}</label>
              <div className="flex gap-2">
                <div className="relative flex-1">
                  <Mail className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                  <input type="email" value={resetEmail} onChange={e => setResetEmail(e.target.value)}
                    placeholder={t('auth.inputEmail')} autoComplete="email" className={cn(inputCls, 'pl-9')} />
                </div>
                <button type="button" onClick={handleSendResetCode}
                  disabled={resetCodeState.countdown > 0 || !resetEmail.includes('@') || isLoading}
                  className="shrink-0 rounded-lg bg-quant-bg-secondary px-3 py-2.5 text-xs font-medium text-quant-gold border border-quant-gold/30 hover:bg-quant-gold/10 disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap">
                  {resetCodeState.countdown > 0 ? `${resetCodeState.countdown}s` : t('auth.sendCode')}
                </button>
              </div>
            </div>

            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.verificationCode')}</label>
              <input type="text" value={resetCode} onChange={e => setResetCode(e.target.value)}
                placeholder={t('auth.inputCode')} maxLength={6} className={inputCls} />
            </div>

            <div className="space-y-1.5">
              <label className={labelCls}>{t('auth.newPassword')}</label>
              <div className="relative">
                <input type={resetShowPw ? 'text' : 'password'} value={newPassword}
                  onChange={e => setNewPassword(e.target.value)}
                  placeholder="至少6个字符" autoComplete="new-password"
                  className={cn(inputCls, 'pr-10')} />
                <button type="button" onClick={() => setResetShowPw(!resetShowPw)}
                  aria-label={resetShowPw ? '隐藏密码' : '显示密码'}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                  {resetShowPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>

            <button type="submit"
              disabled={isLoading || !resetEmail.trim() || !resetCode.trim() || !newPassword.trim()}
              className={btnCls}>
              {isLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowRight className="h-4 w-4" />}
              {isLoading ? t('common.loading') : t('auth.resetBtn')}
            </button>

            <p className="text-center text-[11px] text-muted-foreground">
              {t('auth.rememberPassword')}<button type="button" onClick={() => switchTab('login')} className="text-quant-gold hover:underline">{t('auth.login')}</button>
            </p>
          </form>
        )}
      </div>
    </div>
  )
}
