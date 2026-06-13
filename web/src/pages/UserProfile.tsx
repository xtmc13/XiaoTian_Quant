import { useState, useEffect, useCallback } from 'react'
import { userApi } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import {
  User, Mail, Calendar, Shield, Loader2, AlertCircle, CheckCircle,
  Save, Key, Bell, Copy, ExternalLink, Eye, EyeOff, RefreshCw,
  UserCog, Lock, BellRing,
} from 'lucide-react'

interface Profile {
  id: number; username: string; nickname: string; email: string
  role: string; is_active: number; email_verified: number
  created_at: string; credits: number; is_vip: boolean
  referral_code: string; referral_count: number
}

type ProfileTab = 'info' | 'password' | 'notifications'

const NOTIFY_CHANNELS = [
  { key: 'browser', label: '浏览器通知', desc: '在浏览器中接收交易信号和告警' },
  { key: 'email', label: '邮件通知', desc: '通过电子邮件接收重要通知' },
  { key: 'telegram', label: 'Telegram', desc: '通过 Telegram Bot 推送消息' },
  { key: 'sms', label: '短信通知', desc: '通过短信接收紧急告警' },
  { key: 'discord', label: 'Discord', desc: '通过 Discord Webhook 通知' },
  { key: 'webhook', label: 'Webhook', desc: '自定义 Webhook 地址' },
]

const TABS = [
  { key: 'info' as ProfileTab, label: '基本资料', icon: UserCog },
  { key: 'password' as ProfileTab, label: '修改密码', icon: Lock },
  { key: 'notifications' as ProfileTab, label: '通知设置', icon: BellRing },
]

export function UserProfile() {
  const { user } = useAuthStore()

  const [profile, setProfile] = useState<Profile | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [tab, setTab] = useState<ProfileTab>('info')

  // ── Edit profile form ──
  const [nickname, setNickname] = useState('')
  const [email, setEmail] = useState('')
  const [savingProfile, setSavingProfile] = useState(false)

  // ── Change password form ──
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [showOldPw, setShowOldPw] = useState(false)
  const [showNewPw, setShowNewPw] = useState(false)
  const [changingPw, setChangingPw] = useState(false)

  // ── Notification settings ──
  const [notifyChannels, setNotifyChannels] = useState<Record<string, boolean>>({})
  const [savingNotif, setSavingNotif] = useState(false)

  const fetchProfile = useCallback(async () => {
    try {
      const p = await userApi.profile()
      setProfile(p)
      setNickname(p.nickname || '')
      setEmail(p.email || '')
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setError(err.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  const fetchNotifySettings = useCallback(async () => {
    try {
      const res = await userApi.notificationSettings()
      setNotifyChannels(res.channels || {})
    } catch { /* use defaults */ }
  }, [])

  useEffect(() => { fetchProfile(); fetchNotifySettings() }, [fetchProfile, fetchNotifySettings])

  const showMsg = (msg: string) => { setSuccess(msg); setTimeout(() => setSuccess(''), 3000) }

  const handleSaveProfile = async () => {
    setSavingProfile(true); setError('')
    try {
      await userApi.updateProfile({ nickname, email })
      showMsg('个人资料已更新')
      fetchProfile()
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setError(err.message || '保存失败') }
    finally { setSavingProfile(false) }
  }

  const handleChangePw = async (e: React.FormEvent) => {
    e.preventDefault(); setChangingPw(true); setError('')
    try {
      await userApi.changePassword(oldPw, newPw)
      showMsg('密码已修改，下次登录请使用新密码')
      setOldPw(''); setNewPw('')
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setError(err.message || '修改失败') }
    finally { setChangingPw(false) }
  }

  const handleSaveNotif = async () => {
    setSavingNotif(true); setError('')
    try {
      await userApi.saveNotificationSettings(notifyChannels)
      showMsg('通知设置已保存')
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setError(err.message || '保存失败') }
    finally { setSavingNotif(false) }
  }

  const copyReferral = () => {
    const link = `${window.location.origin}/login?ref=${profile?.referral_code || ''}`
    navigator.clipboard.writeText(link)
    showMsg('推荐链接已复制到剪贴板')
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const inputCls = 'w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2.5 text-sm text-white placeholder-muted-foreground outline-none focus:border-quant-gold'

  return (
    <div className="h-full flex flex-col bg-quant-bg">
      {/* Fixed header */}
      <div className="shrink-0 pl-4 pr-6 pt-2 pb-2">
        <PageHeader
          subtitle="管理个人资料、密码与通知偏好"
          actions={
            tab === 'info' ? (
              <button onClick={handleSaveProfile} disabled={savingProfile}
                className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50">
                {savingProfile ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                {savingProfile ? '保存中...' : '保存'}
              </button>
            ) : tab === 'password' ? (
              <button form="pw-form" type="submit" disabled={changingPw || !oldPw || !newPw}
                className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50">
                {changingPw ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Key className="h-3.5 w-3.5" />}
                {changingPw ? '修改中...' : '修改密码'}
              </button>
            ) : (
              <button onClick={handleSaveNotif} disabled={savingNotif}
                className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50">
                {savingNotif ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                {savingNotif ? '保存中...' : '保存'}
              </button>
            )
          }
        />

        {/* Messages */}
        {success && (
          <div className="mt-2 flex items-center gap-2 rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-2 text-xs text-green-400">
            <CheckCircle className="h-3.5 w-3.5" />{success}
          </div>
        )}
        {error && (
          <div className="mt-2 flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400">
            <AlertCircle className="h-3.5 w-3.5" />{error}
          </div>
        )}

        {/* User card */}
        <div className="mt-2 rounded-xl border border-quant-border bg-quant-bg-secondary p-4 flex items-center gap-4">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-quant-gold/20 text-quant-gold text-lg font-bold shrink-0">
            {(profile?.nickname || profile?.username || '?')[0].toUpperCase()}
          </div>
          <div className="flex-1 min-w-0 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span className="text-sm font-semibold text-white">{profile?.nickname || profile?.username}</span>
            <span className="flex items-center gap-1"><User className="h-3 w-3" />@{profile?.username}</span>
            <span className="flex items-center gap-1"><Mail className="h-3 w-3" />{profile?.email || '未设置'}</span>
            <span className="flex items-center gap-1"><Shield className="h-3 w-3" />{profile?.role === 'admin' ? '管理员' : '用户'}</span>
            <span className="flex items-center gap-1"><Calendar className="h-3 w-3" />{profile?.created_at?.slice(0, 10) || '-'}</span>
            {profile && profile.credits > 0 && (
              <span className="text-quant-gold">{profile.credits} 积分 {profile.is_vip && <span className="rounded bg-quant-gold/20 px-1 py-0.5 text-[10px]">VIP</span>}</span>
            )}
          </div>
        </div>
      </div>

      {/* Content area */}
      <div className="flex-1 flex gap-5 pl-4 pr-6 min-h-0">
        {/* Left nav */}
        <div className="w-36 shrink-0 space-y-0.5 overflow-y-auto pt-1">
          {TABS.map((t) => (
            <button
              key={t.key}
              onClick={() => { setTab(t.key); setError('') }}
              className={cn(
                'flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors',
                tab === t.key ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:bg-quant-card hover:text-foreground'
              )}
            >
              <t.icon className="h-4 w-4 shrink-0" />
              <span>{t.label}</span>
            </button>
          ))}
        </div>

        {/* Right content */}
        <div className="flex-1 space-y-4 overflow-y-auto min-h-0 pb-6">
          {/* ══════ INFO TAB ══════ */}
          {tab === 'info' && (
            <SectionCard title="基本资料" bodyClassName="space-y-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">用户名</label>
                <input type="text" value={profile?.username || ''} disabled
                  className={cn(inputCls, 'opacity-50 cursor-not-allowed')} />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">昵称</label>
                <input type="text" value={nickname} onChange={e => setNickname(e.target.value)}
                  placeholder="给自己起个名字" className={inputCls} />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">
                  邮箱 {profile?.email_verified === 1 && <span className="text-green-400 ml-1">✓ 已验证</span>}
                </label>
                <input type="email" value={email} onChange={e => setEmail(e.target.value)}
                  placeholder="your@email.com" className={inputCls} />
              </div>

              {/* Referral */}
              <div className="border-t border-quant-border pt-4">
                <h3 className="text-sm font-medium text-white mb-2 flex items-center gap-2">
                  <ExternalLink className="h-4 w-4" /> 推荐链接
                </h3>
                <p className="text-xs text-muted-foreground mb-2">
                  邀请好友注册，双方各得奖励积分 · 已邀请 <span className="text-quant-gold font-medium">{profile?.referral_count || 0}</span> 人
                </p>
                <div className="flex gap-2">
                  <input type="text" readOnly
                    value={`${window.location.origin}/login?ref=${profile?.referral_code || ''}`}
                    className={cn(inputCls, 'text-xs')} />
                  <button onClick={copyReferral} className="shrink-0 rounded-lg bg-quant-bg px-3 py-2 text-muted-foreground hover:text-white">
                    <Copy className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </SectionCard>
          )}

          {/* ══════ PASSWORD TAB ══════ */}
          {tab === 'password' && (
            <SectionCard title="修改密码" bodyClassName="space-y-4">
              <form id="pw-form" onSubmit={handleChangePw} className="space-y-4">
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">当前密码</label>
                  <div className="relative">
                    <input type={showOldPw ? 'text' : 'password'} value={oldPw} onChange={e => setOldPw(e.target.value)}
                      placeholder="输入当前密码" className={cn(inputCls, 'pr-10')} />
                    <button type="button" onClick={() => setShowOldPw(!showOldPw)}
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground">
                      {showOldPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">新密码</label>
                  <div className="relative">
                    <input type={showNewPw ? 'text' : 'password'} value={newPw} onChange={e => setNewPw(e.target.value)}
                      placeholder="至少6个字符" className={cn(inputCls, 'pr-10')} />
                    <button type="button" onClick={() => setShowNewPw(!showNewPw)}
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground">
                      {showNewPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
              </form>
            </SectionCard>
          )}

          {/* ══════ NOTIFICATIONS TAB ══════ */}
          {tab === 'notifications' && (
            <SectionCard title="通知设置" bodyClassName="space-y-3">
              {NOTIFY_CHANNELS.map(ch => (
                <div key={ch.key} className="flex items-center justify-between py-2 border-b border-quant-border last:border-0">
                  <div className="flex-1">
                    <p className="text-sm text-white">{ch.label}</p>
                    <p className="text-xs text-muted-foreground">{ch.desc}</p>
                  </div>
                  <button onClick={() => setNotifyChannels(s => ({ ...s, [ch.key]: !s[ch.key] }))}
                    className={cn('relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0',
                      notifyChannels[ch.key] ? 'bg-quant-gold' : 'bg-quant-border')}>
                    <span className={cn('inline-block h-4 w-4 transform rounded-full bg-white transition-transform',
                      notifyChannels[ch.key] ? 'translate-x-6' : 'translate-x-1')} />
                  </button>
                </div>
              ))}
            </SectionCard>
          )}
        </div>
      </div>
    </div>
  )
}
