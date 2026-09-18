import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/Button'
import { ArrowLeft } from 'lucide-react'
import { strategyApi } from '@/lib/api'
import {
  useStrategyCreateForm,
  StrategyCreateFormSections,
  CREATE_SECTION_IDS,
} from '@/components/strategy/StrategyCreateForm'
import { apiPayloadToCraParams } from '@/components/strategy/CRAParamForm'

const STEPS = [
  { id: 'create-sec-presets', label: '快速预设' },
  { id: 'create-sec-basic', label: '基础信息' },
  { id: 'create-sec-params', label: '参数 · 指标与壳' },
  { id: 'create-sec-exec', label: '执行设置' },
] as const

/**
 * 创建/编辑策略独立页（/create）：
 * - market=spot → 现货专用表单（SpotStrategyForm：区间/格数/每格金额/循环/费率）；
 *   market=contract → 合约表单（StrategyCreateForm，CRAParamForm + IndicatorPicker）。
 * - ?id= 编辑模式：拉取配置回填（现货自定义键优先、CRA 映射键回退），保存走 update。
 * 顶栏 = 返回 + 标题 + 现货/合约大切换 + 保存；左侧 sticky 步骤导航。
 */
export function CreateStrategyPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const urlMarket: 'spot' | 'contract' = searchParams.get('market') === 'spot' ? 'spot' : 'contract'
  const editId = searchParams.get('id') || undefined

  const { data: editingItem } = useQuery({
    queryKey: ['strategy', editId],
    queryFn: () => strategyApi.get(editId as string),
    enabled: !!editId,
  })

  // 编辑时以记录自身市场为准（覆盖 URL）。
  const market: 'spot' | 'contract' =
    editId && editingItem ? (editingItem.market_type === 'spot' ? 'spot' : 'contract') : urlMarket

  const populatedFor = useRef<string | null>(null)

  const form = useStrategyCreateForm(
    market,
    () => navigate('/bots'),
    editId && editingItem ? { editId, initialType: editingItem.strategy_type } : undefined
  )

  // 编辑回填（现货/合约同一套原创建表单）：config_json → CRAParams（指标
  // detectOpenIndicator 双键回退在 apiPayloadToCraParams 内）。现货记录含
  // CRA 映射键即可完整回填；纯现货自定义键（price_lower 等）记录回退默认。
  useEffect(() => {
    if (!editId || !editingItem) return
    if (populatedFor.current === editId) return
    populatedFor.current = editId
    let cfg: Record<string, unknown> = {}
    try {
      cfg = JSON.parse(editingItem.config_json || '{}') as Record<string, unknown>
    } catch {
      cfg = {}
    }
    form.setName(editingItem.name || '')
    if (editingItem.symbol) form.setSymbol(editingItem.symbol)
    if (typeof editingItem.initial_capital === 'number') form.setInitialCapital(editingItem.initial_capital)
    if (Array.isArray(cfg.selected_exchanges)) form.setSelectedExchanges(cfg.selected_exchanges as string[])
    form.setExecutionMode(editingItem.execution_mode === 'live' ? 'live' : 'paper')
    form.setCraParams(apiPayloadToCraParams(cfg))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editId, editingItem])

  const steps = STEPS
  const [activeStep, setActiveStep] = useState<string>(steps[0].id)
  const mainRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const root = mainRef.current
    if (!root) return
    const sections = CREATE_SECTION_IDS.map((id) => document.getElementById(id)).filter(
      (el): el is HTMLElement => el != null
    )
    if (sections.length === 0) return
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            setActiveStep(entry.target.id)
            break
          }
        }
      },
      { rootMargin: '-15% 0px -70% 0px', threshold: 0 }
    )
    sections.forEach((s) => observer.observe(s))
    return () => observer.disconnect()
  }, [market])

  const jumpTo = (id: string) => {
    setActiveStep(id)
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  const goBack = () => {
    if (window.history.length > 1) navigate(-1)
    else navigate('/bots')
  }

  const handleSave = () => {
    void form.handleSubmit()
  }

  const saveLabel = form.isSubmitting ? '保存中...' : editId ? '保存修改' : '保存策略'

  return (
    <div ref={mainRef} className="h-full overflow-y-auto p-4 sm:p-5">
      <div className="mx-auto max-w-6xl space-y-4">
        {/* 顶栏 */}
        <div className="flex items-center gap-3 flex-wrap">
          <button
            onClick={goBack}
            className="px-3 py-1.5 rounded-lg bg-quant-card border border-quant-border text-xs text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1.5"
          >
            <ArrowLeft className="w-3.5 h-3.5" /> 返回
          </button>
          {/* 标题即市场：现货策略/合约策略切换即页面身份，编辑时加小徽标 */}
          <div className="flex items-center gap-2">
            <div className="flex bg-quant-card border border-quant-border rounded-lg p-1">
              {(
                [
                  { key: 'spot', label: '现货策略' },
                  { key: 'contract', label: '合约策略' },
                ] as const
              ).map((m) => (
                <button
                  key={m.key}
                  onClick={() => setSearchParams(editId ? { market: m.key, id: editId } : { market: m.key }, { replace: true })}
                  className={cn(
                    'px-4 py-1.5 rounded-md text-sm font-bold transition-colors',
                    market === m.key ? 'bg-quant-gold text-black' : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {m.label}
                </button>
              ))}
            </div>
            {editId && (
              <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px]">编辑</span>
            )}
          </div>
          <span className="flex-1" />
          <Button variant="primary" size="sm" isLoading={form.isSubmitting} onClick={handleSave}>
            {saveLabel}
          </Button>
        </div>

        <div className="flex gap-4 items-start">
          {/* 左侧步骤导航（sticky） */}
          <div className="w-44 shrink-0 bg-quant-card border border-quant-border rounded-xl p-3 sticky top-4 hidden sm:block">
            {steps.map((s, i) => {
              const active = activeStep === s.id
              return (
                <button
                  key={s.id}
                  onClick={() => jumpTo(s.id)}
                  className={cn(
                    'w-full flex items-center gap-2 py-2 rounded-lg px-1.5 text-left transition-colors',
                    active ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  <span
                    className={cn(
                      'w-5 h-5 rounded-full text-[10px] flex items-center justify-center font-bold shrink-0',
                      active ? 'bg-quant-gold text-black' : 'bg-quant-bg border border-quant-border'
                    )}
                  >
                    {i + 1}
                  </span>
                  <span className={cn('text-xs', active && 'font-bold')}>{s.label}</span>
                </button>
              )
            })}
            <div className="mt-3 pt-3 border-t border-quant-border text-[10px] text-muted-foreground leading-relaxed">
              点击步骤可跳转
              <br />
              已填内容自动保存
            </div>
          </div>

          {/* 右侧表单区：现货/合约统一用原创建表单（CRA 全套参数，合约多杠杆/逐全仓） */}
          <div className="flex-1 space-y-4 min-w-0">
            <StrategyCreateFormSections form={form} />
            <div className="flex items-center justify-end gap-2 pb-4">
              <span className="text-[11px] text-muted-foreground mr-auto">
                预估总投入: <span className="text-foreground font-mono">${form.totalAddPosition.toFixed(2)}</span>
              </span>
              <Button variant="primary" size="sm" isLoading={form.isSubmitting} onClick={handleSave}>
                {saveLabel}
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default CreateStrategyPage
