import React, { useState, useEffect } from 'react'
import { X, Bell, Percent, Shield, TrendingDown, ListFilter } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Slider } from '@/components/ui/Slider'
import { Switch } from '@/components/ui/Switch'
import { socialApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useAuthStore } from '@/stores/authStore'

interface AIFollowConfigModalProps {
  provider: {
    provider_id: number
    provider_name?: string
  }
  open: boolean
  onClose: () => void
  onSaved?: () => void
}

export const AIFollowConfigModal: React.FC<AIFollowConfigModalProps> = ({
  provider,
  open,
  onClose,
  onSaved,
}) => {
  const { user } = useAuthStore()
  const followerId = user ? Number(user.id) : 0

  const [enabled, setEnabled] = useState(true)
  const [multiplier, setMultiplier] = useState(1)
  const [maxPosition, setMaxPosition] = useState(10)
  const [maxDailyLoss, setMaxDailyLoss] = useState(5)
  const [slippage, setSlippage] = useState(0.5)
  const [autoExecute, setAutoExecute] = useState(false)
  const [symbols, setSymbols] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  useEffect(() => {
    if (!open || !provider?.provider_id || followerId === 0) return
    socialApi.followerConfigs(followerId).then((configs) => {
      const cfg = configs.find((c: any) => c.provider_id === provider.provider_id)
      if (cfg) {
        setEnabled(cfg.enabled !== false)
        setMultiplier(cfg.multiplier || 1)
        setMaxPosition((cfg.max_position || 0.1) * 100)
        setMaxDailyLoss((cfg.max_daily_loss || 0.05) * 100)
        setSlippage(cfg.slippage_pct || 0.5)
        setAutoExecute(cfg.auto_execute || false)
        setSymbols((cfg.symbols || []).join(','))
      } else {
        setEnabled(true)
        setMultiplier(1)
        setMaxPosition(10)
        setMaxDailyLoss(5)
        setSlippage(0.5)
        setAutoExecute(false)
        setSymbols('')
      }
    })
  }, [open, provider?.provider_id, followerId])

  const handleSave = async () => {
    if (followerId === 0) return
    setIsSubmitting(true)
    try {
      await socialApi.saveFollowerConfig({
        provider_id: provider.provider_id,
        follower_id: followerId,
        enabled,
        multiplier,
        max_position: maxPosition / 100,
        max_daily_loss: maxDailyLoss / 100,
        slippage_pct: slippage,
        auto_execute: autoExecute,
        symbols: symbols.split(',').map((s) => s.trim().toUpperCase()).filter(Boolean),
      })
      toast('success', '自动跟单配置已保存')
      onSaved?.()
      onClose()
    } catch (err: any) {
      toast('error', err?.message || '保存失败')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-full max-w-md rounded-2xl border border-[#2a2a2a] bg-[#111111] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[#1c1c1c] px-5 py-4">
          <div className="flex items-center gap-2">
            <Bell className="w-4 h-4 text-[#1890ff]" />
            <span className="text-sm font-semibold text-white">自动跟单配置</span>
          </div>
          <button onClick={onClose} className="text-[#888] hover:text-white">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="p-5 space-y-5">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-sm text-[#ccc]">启用自动跟单</span>
            </div>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs text-[#888]">
              <span className="flex items-center gap-1.5">
                <Percent className="w-3 h-3" /> 仓位倍数
              </span>
              <span>{multiplier.toFixed(1)}x</span>
            </div>
            <Slider value={multiplier} onChange={setMultiplier} min={0.1} max={5} step={0.1} />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs text-[#888]">
              <span className="flex items-center gap-1.5">
                <Shield className="w-3 h-3" /> 单仓最大仓位
              </span>
              <span>{maxPosition.toFixed(0)}%</span>
            </div>
            <Slider value={maxPosition} onChange={setMaxPosition} min={1} max={100} step={1} />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs text-[#888]">
              <span className="flex items-center gap-1.5">
                <TrendingDown className="w-3 h-3" /> 日最大亏损
              </span>
              <span>{maxDailyLoss.toFixed(1)}%</span>
            </div>
            <Slider value={maxDailyLoss} onChange={setMaxDailyLoss} min={0.5} max={50} step={0.5} />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs text-[#888]">
              <span className="flex items-center gap-1.5">允许滑点</span>
              <span>{slippage.toFixed(1)}%</span>
            </div>
            <Slider value={slippage} onChange={setSlippage} min={0.1} max={5} step={0.1} />
          </div>

          <div className="flex items-center justify-between">
            <span className="text-sm text-[#ccc]">自动执行（无需手动确认）</span>
            <Switch checked={autoExecute} onCheckedChange={setAutoExecute} />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs text-[#888] flex items-center gap-1.5">
              <ListFilter className="w-3 h-3" />
              交易对白名单（逗号分隔，留空=全部）
            </label>
            <input
              type="text"
              value={symbols}
              onChange={(e) => setSymbols(e.target.value)}
              placeholder="BTCUSDT, ETHUSDT"
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444] outline-none focus:border-[#1890ff]/40"
            />
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-[#1c1c1c] px-5 py-4">
          <Button size="sm" variant="ghost" onClick={onClose}>取消</Button>
          <Button size="sm" onClick={handleSave} isLoading={isSubmitting}>保存配置</Button>
        </div>
      </div>
    </div>
  )
}

export default AIFollowConfigModal
