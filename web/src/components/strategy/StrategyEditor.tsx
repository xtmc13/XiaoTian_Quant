import { useMemo, useRef, useState } from 'react'
import { aiApi, strategyApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { getDefaultStrategyCode } from './StrategyFormFields'
import { FileCode2, RotateCcw, Save, BrainCircuit, Sparkles, Wand2, Copy, Check, Loader2, X } from 'lucide-react'

interface StrategyEditorProps {
  strategyType?: string
}

export function StrategyEditor({ strategyType }: StrategyEditorProps) {
  const [code, setCode] = useState(getDefaultStrategyCode(strategyType || 'custom'))
  const [aiPrompt, setAiPrompt] = useState('')
  const [aiCode, setAiCode] = useState('')
  const [aiExplanation, setAiExplanation] = useState('')
  const [aiError, setAiError] = useState('')
  const [generating, setGenerating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [copied, setCopied] = useState(false)
  const { confirm, prompt, Dialog } = useConfirmDialog()

  const lineNumbers = useMemo(() => {
    const count = code.split('\n').length
    return Array.from({ length: count }, (_, i) => i + 1)
  }, [code])
  const gutterRef = useRef<HTMLDivElement>(null)

  const handleReset = async () => {
    const ok = await confirm({
      title: '重置代码',
      message: '将恢复为默认模板，当前修改会丢失。',
      confirmText: '重置',
      variant: 'danger',
    })
    if (ok) setCode(getDefaultStrategyCode(strategyType || 'custom'))
  }

  const handleSave = async () => {
    if (saving) return
    const name = await prompt({
      title: '保存策略',
      message: '将当前代码保存为新的策略配置',
      inputLabel: '策略名称',
      defaultValue: '我的脚本策略',
      confirmText: '保存',
    })
    if (name === null) return
    const trimmed = name.trim()
    if (!trimmed) {
      toast('warning', '策略名称不能为空')
      return
    }
    setSaving(true)
    try {
      await strategyApi.create({
        name: trimmed,
        strategy_name: trimmed,
        symbol: 'BTCUSDT',
        timeframe: '1h',
        trade_direction: 'dual',
        market_type: 'swap',
        strategy_type: 'ScriptStrategy',
        strategy_mode: 'script',
        strategy_code: code,
        status: 'stopped',
      })
      toast('success', '策略创建成功！请到策略管理页面启动。')
    } catch (e: unknown) {
      toast('error', '创建策略失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setSaving(false)
    }
  }

  const handleGenerate = async () => {
    if (!aiPrompt.trim() || generating) return
    setGenerating(true)
    setAiError('')
    setAiCode('')
    setAiExplanation('')
    try {
      const res = await aiApi.generate({ prompt: aiPrompt })
      if (res?.strategy_code) {
        setAiCode(res.strategy_code)
        setAiExplanation(res.explanation || '')
      } else if (res?.explanation) {
        setAiExplanation(res.explanation)
      } else {
        setAiError(res?.error || 'AI 未返回内容，请调整描述后重试')
      }
    } catch (e: unknown) {
      setAiError(e instanceof Error ? e.message : '生成失败，请稍后重试')
    } finally {
      setGenerating(false)
    }
  }

  const handleCopy = async () => {
    const text = aiCode || aiExplanation
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      toast('success', '已复制到剪贴板')
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      toast('error', '复制失败，请手动选择复制')
    }
  }

  const handleApplyCode = () => {
    if (!aiCode) return
    setCode(aiCode)
    toast('success', 'AI 代码已应用到编辑器')
  }

  const hasResult = Boolean(aiCode || aiExplanation)

  return (
    <div className="h-full flex bg-quant-bg">
      {/* ── 左：代码编辑区 ── */}
      <div className="flex-1 flex flex-col min-w-0 min-h-0">
        <div className="shrink-0 flex items-center justify-between gap-3 px-4 py-2.5 border-b border-quant-border bg-quant-bg-secondary">
          <div className="flex items-center gap-2 min-w-0">
            <FileCode2 className="w-3.5 h-3.5 text-quant-gold shrink-0" />
            <span className="text-xs font-semibold">Python 策略代码</span>
            <span className="hidden sm:inline px-1.5 py-0.5 rounded border border-quant-border bg-quant-bg font-mono text-[10px] text-muted-foreground">
              main.py
            </span>
            <span className="hidden lg:inline text-[10px] text-muted-foreground">{lineNumbers.length} 行</span>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <button
              onClick={handleReset}
              className="px-2.5 py-1.5 rounded-lg border border-quant-border bg-quant-bg text-[11px] text-muted-foreground hover:bg-quant-hover hover:text-foreground transition-colors flex items-center gap-1"
            >
              <RotateCcw className="w-3 h-3" /> 重置
            </button>
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-3 py-1.5 rounded-lg bg-quant-gold text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed transition-all flex items-center gap-1"
            >
              <Save className="w-3 h-3" /> {saving ? '保存中...' : '保存'}
            </button>
          </div>
        </div>
        <div className="flex-1 min-h-0 p-3">
          <div className="h-full min-h-[360px] rounded-xl border border-quant-border bg-quant-bg overflow-hidden flex">
            <div
              ref={gutterRef}
              aria-hidden
              className="w-11 shrink-0 overflow-hidden border-r border-quant-border bg-quant-bg-secondary py-3 pr-2 text-right font-mono text-[11px] leading-[18px] text-muted-foreground/50 select-none"
            >
              {lineNumbers.map((n) => (
                <div key={n}>{n}</div>
              ))}
            </div>
            <textarea
              value={code}
              onChange={(e) => setCode(e.target.value)}
              onScroll={(e) => {
                if (gutterRef.current) gutterRef.current.scrollTop = e.currentTarget.scrollTop
              }}
              wrap="off"
              spellCheck={false}
              className="flex-1 min-w-0 resize-none overflow-auto bg-transparent py-3 pl-3 pr-4 font-mono text-[11px] leading-[18px] text-foreground/90 whitespace-pre focus:outline-none"
            />
          </div>
        </div>
      </div>

      {/* ── 右：AI 助手面板 ── */}
      <aside className="hidden md:flex w-80 shrink-0 flex-col min-h-0 border-l border-quant-border bg-quant-bg-secondary">
        <div className="shrink-0 px-4 py-3 border-b border-quant-border">
          <div className="flex items-center gap-2">
            <div className="w-6 h-6 rounded-lg bg-quant-gold/10 border border-quant-gold/20 flex items-center justify-center shrink-0">
              <BrainCircuit className="w-3.5 h-3.5 text-quant-gold" />
            </div>
            <div className="min-w-0">
              <div className="text-xs font-semibold">AI 策略助手</div>
              <div className="text-[10px] text-muted-foreground">自然语言生成 Python 策略</div>
            </div>
          </div>
        </div>
        <div className="shrink-0 p-3 space-y-2 border-b border-quant-border">
          <label className="block text-[10px] text-muted-foreground">交易思路描述</label>
          <textarea
            value={aiPrompt}
            onChange={(e) => setAiPrompt(e.target.value)}
            placeholder="例如：BTC 15分钟均线金叉做多，死叉平仓，RSI 超买过滤"
            className="w-full h-24 bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs resize-none focus:outline-none focus:border-quant-gold"
          />
          <button
            onClick={handleGenerate}
            disabled={generating || !aiPrompt.trim()}
            className="w-full py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed transition-all flex items-center justify-center gap-1.5"
          >
            {generating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Sparkles className="w-3.5 h-3.5" />}
            {generating ? '生成中...' : '生成策略'}
          </button>
        </div>
        <div className="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
          {aiError && (
            <div role="alert" className="flex items-start gap-2 text-xs text-red-400 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
              <X className="h-3.5 w-3.5 mt-0.5 shrink-0" /> {aiError}
            </div>
          )}
          {generating && (
            <div className="space-y-2 animate-pulse" aria-label="生成中">
              <div className="h-3 rounded bg-quant-hover w-full" />
              <div className="h-3 rounded bg-quant-hover w-5/6" />
              <div className="h-3 rounded bg-quant-hover w-4/6" />
              <div className="h-3 rounded bg-quant-hover w-3/6" />
            </div>
          )}
          {!generating && !hasResult && !aiError && (
            <div className="h-full min-h-[200px] flex flex-col items-center justify-center text-center gap-2">
              <div className="w-10 h-10 rounded-full bg-quant-gold/10 flex items-center justify-center">
                <BrainCircuit className="w-5 h-5 text-quant-gold/60" />
              </div>
              <div className="text-xs text-muted-foreground">AI 建议将显示在这里</div>
              <div className="text-[10px] text-muted-foreground/70 max-w-[220px] leading-relaxed">
                描述你的交易思路并点击生成，AI 将输出可运行的策略代码与说明
              </div>
            </div>
          )}
          {!generating && hasResult && (
            <div className="rounded-xl border border-quant-border bg-quant-card overflow-hidden">
              <div className="flex items-center justify-between gap-2 px-3 py-2 border-b border-quant-border bg-quant-bg-secondary">
                <span className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">生成结果</span>
                <div className="flex items-center gap-1.5">
                  {aiCode && (
                    <button
                      onClick={handleApplyCode}
                      className="px-2 py-1 rounded bg-quant-gold/10 border border-quant-gold/20 text-quant-gold text-[10px] hover:bg-quant-gold/20 transition-colors flex items-center gap-1"
                    >
                      <Wand2 className="w-3 h-3" /> 应用
                    </button>
                  )}
                  <button
                    onClick={handleCopy}
                    title="复制"
                    className="p-1 rounded text-muted-foreground hover:text-quant-gold hover:bg-quant-gold/10 transition-colors"
                  >
                    {copied ? <Check className="w-3.5 h-3.5 text-quant-green" /> : <Copy className="w-3.5 h-3.5" />}
                  </button>
                </div>
              </div>
              {aiCode ? (
                <>
                  <pre className="p-3 overflow-x-auto font-mono text-[11px] leading-relaxed text-foreground/90 whitespace-pre">{aiCode}</pre>
                  {aiExplanation && (
                    <div className="border-t border-quant-border p-3 text-[11px] leading-relaxed text-muted-foreground whitespace-pre-wrap">
                      {aiExplanation}
                    </div>
                  )}
                </>
              ) : (
                <div className="p-3 text-[11px] leading-relaxed text-muted-foreground whitespace-pre-wrap">{aiExplanation}</div>
              )}
            </div>
          )}
        </div>
      </aside>

      <Dialog />
    </div>
  )
}
