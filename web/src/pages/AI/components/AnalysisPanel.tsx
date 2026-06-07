import { Plus, Zap, LineChart, Gauge, Building2, Loader2, BrainCircuit, TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { AIAnalysisResult } from '@/types'

export function AnalysisPlaceholder({ onAddStock, onAnalyze, canAnalyze }: {
  onAddStock: () => void
  onAnalyze: () => void
  canAnalyze: boolean
}) {
  return (
    <div className="flex items-center justify-center h-full min-h-[300px] relative overflow-hidden">
      <div className="absolute inset-0 pointer-events-none">
        <div
          className="absolute rounded-full opacity-50 animate-hero-float"
          style={{
            width: 320, height: 320, top: -80, right: -60,
            background: 'radial-gradient(circle, rgba(234,179,8,0.10) 0%, transparent 70%)',
          }}
        />
        <div
          className="absolute rounded-full opacity-50 animate-hero-float-reverse"
          style={{
            width: 240, height: 240, bottom: -40, left: -40,
            background: 'radial-gradient(circle, rgba(168,85,247,0.08) 0%, transparent 70%)',
          }}
        />
        <div
          className="absolute inset-0"
          style={{
            backgroundImage:
              'linear-gradient(rgba(234,179,8,0.03) 1px, transparent 1px), linear-gradient(90deg, rgba(234,179,8,0.03) 1px, transparent 1px)',
            backgroundSize: '32px 32px',
          }}
        />
      </div>

      <div className="relative text-center px-8 py-10 max-w-[560px]">
        <div className="inline-block px-3 py-0.5 rounded-full text-[10px] font-bold tracking-widest text-quant-gold bg-quant-gold/10 border border-quant-gold/20 mb-4">
          AI-POWERED
        </div>
        <h2 className="text-2xl font-extrabold text-foreground mb-2 tracking-tight">AI 资产分析</h2>
        <p className="text-sm text-muted-foreground mb-8 leading-relaxed">选择标的并启动 AI 分析，获取实时交易建议与策略生成</p>

        <div className="flex gap-3 justify-center mb-8 flex-wrap">
          {[
            { icon: LineChart, title: '趋势分析', desc: '多时间框架技术研判' },
            { icon: Gauge, title: '风险评估', desc: '波动率与回撤测算' },
            { icon: Building2, title: '策略生成', desc: '自动输出交易计划' },
          ].map((f) => (
            <div
              key={f.title}
              className="flex items-center gap-2.5 px-3.5 py-3 bg-quant-card border border-quant-border rounded-xl shadow-sm flex-1 min-w-0 text-left transition-all hover:border-quant-gold/40 hover:shadow-md hover:-translate-y-0.5"
            >
              <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-quant-gold/10 text-quant-gold shrink-0">
                <f.icon className="w-4 h-4" />
              </div>
              <div className="min-w-0">
                <div className="text-xs font-bold text-foreground truncate">{f.title}</div>
                <div className="text-[10px] text-muted-foreground truncate">{f.desc}</div>
              </div>
            </div>
          ))}
        </div>

        <div className="flex gap-3 justify-center mb-4">
          <button
            onClick={onAddStock}
            className="inline-flex items-center gap-1.5 px-6 h-[42px] rounded-xl text-sm font-semibold bg-quant-gold text-white shadow-lg shadow-quant-gold/20 hover:opacity-90 transition-opacity"
          >
            <Plus className="w-4 h-4" /> 添加标的
          </button>
          <button
            onClick={onAnalyze}
            disabled={!canAnalyze}
            className="inline-flex items-center gap-1.5 px-6 h-[42px] rounded-xl text-sm font-semibold bg-quant-card border border-quant-border text-foreground hover:border-quant-gold/40 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <Zap className="w-4 h-4" /> 开始分析
          </button>
        </div>
        <p className="text-xs text-muted-foreground">从上方搜索框选择标的，或点击“添加标的”快速开始</p>
      </div>
    </div>
  )
}

export function AnalysisResultView({
  result,
  loading,
  error,
  onRetry,
}: {
  result: AIAnalysisResult | null
  loading: boolean
  error: string | null
  onRetry: () => void
}) {
  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center h-full min-h-[300px] gap-4">
        <Loader2 className="w-8 h-8 text-quant-gold animate-spin" />
        <div className="text-sm text-muted-foreground">AI 正在分析中，请稍候...</div>
        <div className="w-48 h-1.5 bg-quant-border rounded-full overflow-hidden">
          <div className="h-full bg-quant-gold animate-progress rounded-full" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center h-full min-h-[300px] gap-3">
        <div className="text-sm text-quant-red">{error}</div>
        <button
          onClick={onRetry}
          className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
        >
          重试
        </button>
      </div>
    )
  }

  if (!result) return null

  const consensusColor =
    result.consensus === 'bullish'
      ? 'text-quant-green bg-quant-green/10 border-quant-green/20'
      : result.consensus === 'bearish'
      ? 'text-quant-red bg-quant-red/10 border-quant-red/20'
      : 'text-quant-blue bg-quant-blue/10 border-quant-blue/20'

  const sentimentIcon = (s: string) =>
    s === 'bullish' ? (
      <TrendingUp className="h-3.5 w-3.5 text-quant-green" />
    ) : s === 'bearish' ? (
      <TrendingDown className="h-3.5 w-3.5 text-quant-red" />
    ) : (
      <Minus className="h-3.5 w-3.5 text-quant-blue" />
    )

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <div className={cn('px-3 py-1 rounded-lg text-xs font-bold border', consensusColor)}>
          {result.consensus === 'bullish' ? '看涨共识' : result.consensus === 'bearish' ? '看跌共识' : '中性共识'}
        </div>
        <div className="text-sm text-muted-foreground">
          标的 <span className="font-mono font-bold text-foreground">{result.symbol}</span>
        </div>
      </div>

      <div className="space-y-3">
        {result.analyses.map((a, idx) => (
          <div key={`${a.model}-${idx}`} className="bg-quant-card border border-quant-border rounded-xl p-4">
            <div className="flex items-center justify-between mb-2">
              <div className="flex items-center gap-2">
                <BrainCircuit className="h-4 w-4 text-quant-gold" />
                <span className="text-sm font-semibold text-foreground">{a.name}</span>
              </div>
              <div className={cn(
                'flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-medium',
                a.sentiment === 'bullish' ? 'border-quant-green/20 text-quant-green bg-quant-green/10' :
                a.sentiment === 'bearish' ? 'border-quant-red/20 text-quant-red bg-quant-red/10' :
                'border-quant-blue/20 text-quant-blue bg-quant-blue/10'
              )}>
                {sentimentIcon(a.sentiment)}
                {a.sentiment === 'bullish' ? '看涨' : a.sentiment === 'bearish' ? '看跌' : '中性'}
              </div>
            </div>
            <p className="text-xs text-muted-foreground leading-relaxed">{a.analysis}</p>
          </div>
        ))}
      </div>
    </div>
  )
}
