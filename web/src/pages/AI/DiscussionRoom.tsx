import { useState, useCallback, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Users,
  Loader2,
  Copy,
  Check,
  RefreshCw,
  AlertTriangle,
  LineChart,
  ShieldCheck,
  Globe2,
  HeartPulse,
  TrendingUp,
  TrendingDown,
  Sparkles,
} from 'lucide-react'
import { aiApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'

import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'

/* ── Response shape ──
   Mirrors gateway/internal/handler/ai.go AIMultiAgent: the endpoint returns the
   whole multi-role debate as one freeform text (debate_summary / agents.analysis);
   per-role views are embedded in the text and split client-side in parseDebateText. */
interface MultiAgentDebate {
  status: 'ok' | 'error'
  msg?: string
  strategy_name?: string
  strategy_code?: string
  description?: string
  agents?: Record<string, string>
  debate_summary?: string
}

type RoleKey = 'technical' | 'risk' | 'macro' | 'sentiment' | 'bull' | 'bear'

const ROLE_LIST: {
  key: RoleKey
  name: string
  en: string
  icon: typeof LineChart
  text: string
  badge: string
}[] = [
  { key: 'technical', name: '技术分析师', en: 'Technical Analyst', icon: LineChart, text: 'text-quant-gold', badge: 'bg-quant-gold/10 border-quant-gold/30' },
  { key: 'risk', name: '风控官', en: 'Risk Manager', icon: ShieldCheck, text: 'text-quant-orange', badge: 'bg-quant-orange/10 border-quant-orange/30' },
  { key: 'macro', name: '宏观分析师', en: 'Macro Analyst', icon: Globe2, text: 'text-quant-gold', badge: 'bg-quant-gold/10 border-quant-gold/30' },
  { key: 'sentiment', name: '情绪分析师', en: 'Sentiment Analyst', icon: HeartPulse, text: 'text-quant-orange', badge: 'bg-quant-orange/10 border-quant-orange/30' },
  { key: 'bull', name: '多方辩护', en: 'Bull Advocate', icon: TrendingUp, text: 'text-quant-green', badge: 'bg-quant-green/10 border-quant-green/30' },
  { key: 'bear', name: '空方辩护', en: 'Bear Advocate', icon: TrendingDown, text: 'text-quant-red', badge: 'bg-quant-red/10 border-quant-red/30' },
]

const QUICK_SYMBOLS = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'BNBUSDT', 'XRPUSDT', 'DOGEUSDT']

/* ── Debate text splitter ──
   The AI is prompted to answer role by role, but the output format is freeform.
   Detect "Role Name:"-style line starts (markdown/numbering tolerant) and bucket
   the following lines under that role; anything before the first header (or a
   Consensus/Summary header) becomes the consensus summary. */
const ROLE_HEADER_RES: [RoleKey, RegExp][] = [
  ['technical', /^(?:technical\s+analyst|technical|technician|chartist)\s*[:：—–-]\s*/i],
  ['risk', /^(?:risk\s+manager|risk\s+management|risk)\s*[:：—–-]\s*/i],
  ['macro', /^(?:macro\s+analyst|fundamental\s+analyst|macro|fundamentals?)\s*[:：—–-]\s*/i],
  ['sentiment', /^(?:sentiment\s+analyst|market\s+sentiment|sentiment)\s*[:：—–-]\s*/i],
  ['bull', /^(?:bull\s+advocate|bull\s+case|bull|bullish)\s*[:：—–-]\s*/i],
  ['bear', /^(?:bear\s+advocate|bear\s+case|bear|bearish)\s*[:：—–-]\s*/i],
]

const CONSENSUS_HEADER_RE =
  /^(?:consensus|summary|conclusion|verdict|in\s+summary|final\s+thoughts|bottom\s+line|overall)\s*[:：—–,，]\s*/i
const CONSENSUS_HEADER_ZH_RE = /^(?:共识|总结|结论)\s*[:：]?\s*/

interface ParsedDebate {
  sections: Partial<Record<RoleKey, string>>
  consensus: string
  matchedRoles: number
}

function cleanLine(raw: string): string {
  return raw
    .replace(/[*_]/g, '')
    .replace(/^[\s>•-]+/, '')
    .replace(/^\d+\s*[.、)]\s*/, '')
    .trim()
}

function matchSectionHeader(line: string): { kind: RoleKey | 'consensus'; rest: string } | null {
  if (line.length > 120) return null
  for (const re of [CONSENSUS_HEADER_ZH_RE, CONSENSUS_HEADER_RE]) {
    const m = line.match(re)
    if (m) return { kind: 'consensus', rest: line.slice(m[0].length).trim() }
  }
  for (const [key, re] of ROLE_HEADER_RES) {
    const m = line.match(re)
    if (m) return { kind: key, rest: line.slice(m[0].length).trim() }
  }
  return null
}

function parseDebateText(text: string): ParsedDebate {
  const buckets: Record<RoleKey, string[]> = {
    technical: [],
    risk: [],
    macro: [],
    sentiment: [],
    bull: [],
    bear: [],
  }
  let consensusLines: string[] = []
  let preambleLines: string[] = []
  let current: RoleKey | 'consensus' | null = null

  for (const raw of text.split('\n')) {
    const line = cleanLine(raw)
    if (!line) continue
    const header = matchSectionHeader(line)
    if (header) {
      current = header.kind
      if (header.rest) {
        if (current === 'consensus') consensusLines.push(header.rest)
        else buckets[current].push(header.rest)
      }
      continue
    }
    if (current === 'consensus') consensusLines.push(line)
    else if (current) buckets[current].push(line)
    else preambleLines.push(line)
  }

  // Debates that open with the summary (or never label one) → treat preamble as consensus
  if (consensusLines.length === 0 && preambleLines.length > 0) {
    consensusLines = preambleLines
    preambleLines = []
  }

  const sections: Partial<Record<RoleKey, string>> = {}
  let matchedRoles = 0
  for (const role of ROLE_LIST) {
    const lines = buckets[role.key]
    if (lines.length > 0) {
      sections[role.key] = lines.join('\n')
      matchedRoles++
    }
  }
  return { sections, consensus: consensusLines.join('\n'), matchedRoles }
}

/* ── State views ── */

function LoadingView() {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-20">
      <Loader2 className="w-8 h-8 text-quant-gold animate-spin" />
      <div className="text-sm text-muted-foreground">AI 讨论室正在辩论中，请稍候...</div>
      <div className="flex items-center gap-1.5">
        {ROLE_LIST.map((r) => (
          <div key={r.key} className={cn('w-7 h-7 rounded-lg border flex items-center justify-center animate-pulse', r.badge)}>
            <r.icon className={cn('w-3.5 h-3.5', r.text)} />
          </div>
        ))}
      </div>
      <div className="text-[10px] text-muted-foreground">6 位角色依次推理通常需要 30-90 秒</div>
    </div>
  )
}

function ErrorView({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-20">
      <AlertTriangle className="w-8 h-8 text-quant-red" />
      <div className="text-sm text-quant-red text-center max-w-md">{message}</div>
      <button
        onClick={onRetry}
        className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
      >
        <RefreshCw className="w-3.5 h-3.5" /> 重试
      </button>
    </div>
  )
}

function EmptyStateView() {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16 text-center">
      <div className="flex -space-x-2">
        {ROLE_LIST.map((r) => (
          <div key={r.key} className={cn('w-9 h-9 rounded-full border bg-quant-bg-secondary flex items-center justify-center', r.badge)}>
            <r.icon className={cn('w-4 h-4', r.text)} />
          </div>
        ))}
      </div>
      <div>
        <div className="text-sm font-semibold text-foreground">输入交易对，召集 6 位 AI 角色讨论</div>
        <div className="text-xs text-muted-foreground mt-1 leading-relaxed">
          技术分析师 / 风控官 / 宏观分析师 / 情绪分析师 / 多方辩护 / 空方辩护 依次陈述观点，最终输出共识总结
        </div>
      </div>
    </div>
  )
}

function ResultView({
  symbol,
  degraded,
  parsed,
  copied,
  onCopy,
  onRestart,
}: {
  symbol: string
  degraded: boolean
  parsed: ParsedDebate
  copied: boolean
  onCopy: () => void
  onRestart: () => void
}) {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <span className="px-2.5 py-1 rounded-lg bg-quant-gold/10 border border-quant-gold/30 text-quant-gold text-xs font-mono font-bold">
            {symbol}
          </span>
          <span className="text-[11px] text-muted-foreground">6 位角色已完成讨论</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={onCopy}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
          >
            {copied ? <Check className="w-3.5 h-3.5 text-quant-green" /> : <Copy className="w-3.5 h-3.5" />}
            {copied ? '已复制' : '复制结果'}
          </button>
          <button
            onClick={onRestart}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" /> 重新讨论
          </button>
        </div>
      </div>

      {degraded && (
        <div className="flex items-start gap-2 px-3 py-2.5 rounded-lg bg-quant-orange/10 border border-quant-orange/30 text-quant-orange text-xs leading-relaxed">
          <AlertTriangle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
          AI 服务异常，以下为降级返回内容，可能不包含完整的多角色辩论过程。
        </div>
      )}

      <SectionCard
        title={
          <div className="flex items-center gap-2">
            <Sparkles className="w-4 h-4 text-quant-gold" />
            共识总结
            <span className="text-[10px] font-normal normal-case text-muted-foreground">Consensus</span>
          </div>
        }
      >
        {parsed.consensus ? (
          <p className="text-xs text-foreground leading-relaxed whitespace-pre-wrap">{parsed.consensus}</p>
        ) : (
          <p className="text-xs text-muted-foreground">未生成共识总结，请查看下方各角色观点。</p>
        )}
      </SectionCard>

      {parsed.matchedRoles === 0 && parsed.consensus && (
        <div className="px-3 py-2 rounded-lg bg-quant-bg-secondary border border-quant-border text-[11px] text-muted-foreground leading-relaxed">
          本轮输出未按角色分段，上方为完整讨论记录；角色观点见下方卡片。
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
        {ROLE_LIST.map((role) => {
          const text = parsed.sections[role.key]
          return (
            <div key={role.key} className="bg-quant-card border border-quant-border rounded-xl p-4 shadow-sm flex flex-col gap-2.5">
              <div className="flex items-center gap-2">
                <div className={cn('w-7 h-7 rounded-lg border flex items-center justify-center shrink-0', role.badge)}>
                  <role.icon className={cn('w-3.5 h-3.5', role.text)} />
                </div>
                <div className="min-w-0">
                  <div className="text-xs font-bold text-foreground truncate">{role.name}</div>
                  <div className="text-[10px] text-muted-foreground truncate">{role.en}</div>
                </div>
              </div>
              {text ? (
                <p className="text-xs text-muted-foreground leading-relaxed whitespace-pre-wrap">{text}</p>
              ) : (
                <p className="text-[11px] text-muted-foreground/60 italic">本轮未单独分段发言，详见共识总结</p>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

/* ── Page ── */

export function DiscussionRoom() {
  const navigate = useNavigate()
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [discussedSymbol, setDiscussedSymbol] = useState('BTCUSDT')
  const [discussing, setDiscussing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<MultiAgentDebate | null>(null)
  const [copied, setCopied] = useState(false)

  const startDiscussion = useCallback(async () => {
    const sym = symbol.trim().toUpperCase().replace(/\s+/g, '')
    if (!sym || discussing) return
    setDiscussing(true)
    setDiscussedSymbol(sym)
    setError(null)
    setResult(null)
    setCopied(false)
    try {
      const res = (await aiApi.multiAgent({ symbol: sym })) as unknown as MultiAgentDebate
      if (res.status === 'error') {
        setError(res.msg || 'AI 服务暂不可用，请稍后重试')
        return
      }
      setResult(res)
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setError(err.message || '讨论室请求失败')
    } finally {
      setDiscussing(false)
    }
  }, [symbol, discussing])

  const degraded = !!result?.debate_summary?.startsWith('Error:')

  const debateText = useMemo(() => {
    if (!result) return ''
    if (degraded) return result.agents?.technical || result.debate_summary || ''
    return result.debate_summary || result.agents?.analysis || ''
  }, [result, degraded])

  const parsed = useMemo<ParsedDebate>(() => parseDebateText(debateText), [debateText])

  const handleCopy = useCallback(() => {
    const text = `AI 多模型讨论室 · ${discussedSymbol}\n${'—'.repeat(24)}\n${debateText}`
    navigator.clipboard?.writeText(text).then(
      () => {
        setCopied(true)
        toast('success', '讨论结果已复制')
      },
      () => toast('error', '复制失败，请手动选择复制'),
    )
  }, [debateText, discussedSymbol])

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1200px] space-y-5">
        <PageHeader
          title="AI 多模型讨论室"
          subtitle="6 位 AI 角色辩论 · 技术 / 风控 / 宏观 / 情绪 / 多空辩护 → 共识总结"
          icon={<Users className="w-5 h-5" />}
          actions={
            <button
              onClick={() => navigate('/ai')}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
            >
              <ArrowLeft className="w-3.5 h-3.5" /> 返回 AI 分析
            </button>
          }
        />

        <SectionCard title="发起讨论">
          <div className="flex flex-wrap items-center gap-2">
            <input
              value={symbol}
              onChange={(e) => setSymbol(e.target.value.toUpperCase())}
              onKeyDown={(e) => {
                if (e.key === 'Enter') startDiscussion()
              }}
              placeholder="交易对，如 BTCUSDT"
              className="w-[200px] bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs font-mono uppercase focus:outline-none focus:border-quant-gold"
            />
            <button
              onClick={startDiscussion}
              disabled={discussing || !symbol.trim()}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {discussing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Users className="w-3.5 h-3.5" />}
              {discussing ? '讨论中...' : '开始讨论'}
            </button>
            <div className="flex flex-wrap gap-1.5 md:ml-auto">
              {QUICK_SYMBOLS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => setSymbol(s)}
                  className={cn(
                    'px-2.5 py-1.5 rounded-lg text-[11px] font-mono border transition-colors',
                    symbol === s
                      ? 'bg-quant-gold/10 border-quant-gold/40 text-quant-gold'
                      : 'bg-quant-bg-secondary border-quant-border text-muted-foreground hover:border-quant-gold/40 hover:text-foreground',
                  )}
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        </SectionCard>

        {discussing ? (
          <LoadingView />
        ) : error ? (
          <ErrorView message={error} onRetry={startDiscussion} />
        ) : result ? (
          <ResultView
            symbol={discussedSymbol}
            degraded={degraded}
            parsed={parsed}
            copied={copied}
            onCopy={handleCopy}
            onRestart={startDiscussion}
          />
        ) : (
          <EmptyStateView />
        )}
      </div>
    </div>
  )
}
