/* ── 指标信号告警管理（AlertManager）───────────────────────────
   对标 QuantDinger indicator_signal_alerts：用户自定义指标条件表达式，
   后端周期扫描，命中经 notify.Manager 发通知。

   本组件为自包含片段：不改动 web/src/lib/api.ts，直接复用其导出的
   通用 request 助手 `api`（已带 token/错误处理/响应解包）。

   挂接建议（主控集成，二选一）：
   1) 指标 IDE 页 web/src/pages/IndicatorIDE.tsx 增加 tab：
        {tab === 'alerts' && <AlertManager />}
   2) 设置页通知 tab 内嵌 <AlertManager />。

   依赖后端路由（见路由片段）：
     GET/POST /api/alerts/、GET/PUT/DELETE /api/alerts/:id、
     POST /api/alerts/:id/enable|disable|run(?force=1)、GET /api/alerts/:id/history
   ─────────────────────────────────────────────────────────────── */
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BellRing, ChevronDown, ChevronUp, History, Play, Plus, Trash2, Zap } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Switch } from '@/components/ui/Switch'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { toast } from '@/lib/useToast'

// ── 类型（api.ts 未内置，故本地声明）──
interface IndicatorAlert {
  id: string
  user_id: number
  name: string
  symbol: string
  interval: string
  condition_expr: string
  message: string
  cooldown_minutes: number
  last_triggered_at: number
  last_value: number
  active: boolean
  created_at: number
  updated_at: number
}

interface AlertRunResult {
  matched: boolean
  value: number
  triggered: boolean
  cooldown_remaining_sec?: number
  bars: number
  error?: string
  at: number
}

interface TriggerRecord {
  at: number
  symbol: string
  interval: string
  expr: string
  value: number
}

interface AlertPayload {
  name: string
  symbol: string
  interval: string
  condition_expr: string
  message: string
  cooldown_minutes: number
  active?: boolean
}

// ── API（走 api.ts 导出的通用助手，不新增依赖不改 api.ts）──
const alertApi = {
  list: () => api.get<{ items: IndicatorAlert[]; total: number }>('/alerts/'),
  create: (body: AlertPayload) => api.post<IndicatorAlert>('/alerts/', body),
  update: (id: string, body: AlertPayload) => api.put<IndicatorAlert>(`/alerts/${id}`, body),
  remove: (id: string) => api.del<{ success: boolean }>(`/alerts/${id}`),
  setActive: (id: string, active: boolean) =>
    api.post<IndicatorAlert>(`/alerts/${id}/${active ? 'enable' : 'disable'}`),
  run: (id: string, force: boolean) =>
    api.post<AlertRunResult>(`/alerts/${id}/run${force ? '?force=1' : ''}`),
  history: (id: string) => api.get<{ items: TriggerRecord[]; total: number }>(`/alerts/${id}/history`),
}

// ── 内置表达式示例 ──
const EXPR_EXAMPLES = [
  {
    key: 'rsi_oversold',
    label: 'RSI 超卖（<30 且站上 EMA20）',
    expr: 'rsi14 < 30 && close > ema20',
  },
  {
    key: 'ema_cross',
    label: '金叉突破（EMA12 上穿 EMA50）',
    expr: 'ema12 > ema50 && close > ema50',
  },
  {
    key: 'bb_squeeze',
    label: '布林收口（带宽 <5%）',
    expr: 'bb_width < 0.05',
  },
  {
    key: 'macd_turn',
    label: 'MACD 金叉（柱线转正且信号线为负）',
    expr: 'macd_hist > 0 && macd_signal < 0',
  },
  {
    key: 'break_upper',
    label: '突破上轨（价格贴上轨）',
    expr: 'bb_pctb > 0.95',
  },
]

const INTERVALS = ['1m', '3m', '5m', '15m', '30m', '1h', '2h', '4h', '6h', '8h', '12h', '1d', '3d', '1w']

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

function fmtTime(ts: number): string {
  if (!ts) return '—'
  return new Date(ts).toLocaleString()
}

function fmtValue(v: number): string {
  if (v === undefined || v === null) return '—'
  return Number(v.toFixed(6)).toString()
}

export function AlertManager() {
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [actionId, setActionId] = useState<string | null>(null)
  const [runResult, setRunResult] = useState<{ id: string; res: AlertRunResult } | null>(null)
  const [historyFor, setHistoryFor] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['indicator-alerts'],
    queryFn: alertApi.list,
    refetchInterval: 15000,
  })
  const alerts = data?.items ?? []

  const { data: historyData } = useQuery({
    queryKey: ['indicator-alert-history', historyFor],
    queryFn: () => alertApi.history(historyFor!),
    enabled: !!historyFor,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['indicator-alerts'] })

  const runAction = async (alert: IndicatorAlert, action: () => Promise<unknown>, ok: string, errPrefix: string) => {
    setActionId(alert.id)
    try {
      await action()
      toast('success', ok)
      await invalidate()
    } catch (e) {
      toast('error', `${errPrefix}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setActionId(null)
    }
  }

  const runTest = async (alert: IndicatorAlert, force: boolean) => {
    setActionId(alert.id)
    try {
      const res = await alertApi.run(alert.id, force)
      setRunResult({ id: alert.id, res })
    } catch (e) {
      toast('error', `测试失败: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setActionId(null)
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3 bg-quant-card border border-quant-border rounded-xl px-4 py-2 flex-wrap">
        <span className="font-bold text-sm flex items-center gap-1.5">
          <BellRing className="w-4 h-4 text-quant-gold" /> 指标信号告警
        </span>
        <span className="text-[11px] text-muted-foreground">
          自定义指标条件，命中即通知（默认每 60s 扫描一次）
        </span>
        <span className="flex-1" />
        <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setCreating(true)}>
          新建告警
        </Button>
      </div>

      {creating && (
        <AlertForm
          onSubmit={async (payload) => {
            try {
              await alertApi.create(payload)
              toast('success', '告警已创建')
              setCreating(false)
              await invalidate()
            } catch (e) {
              toast('error', `创建失败: ${e instanceof Error ? e.message : String(e)}`)
            }
          }}
          onCancel={() => setCreating(false)}
        />
      )}

      <SectionCard
        title="告警任务"
        headerAction={<span className="text-xs text-[#8a8a8a]">{alerts.length} 个</span>}
      >
        {isLoading ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-24 rounded-xl" />
            ))}
          </div>
        ) : alerts.length === 0 ? (
          <EmptyState
            icon={<BellRing className="w-8 h-8" />}
            title="暂无告警任务"
            description="点击右上角「新建告警」，用表达式定义指标条件"
            actionLabel="新建告警"
            onAction={() => setCreating(true)}
          />
        ) : (
          <div className="space-y-3">
            {alerts.map((alert) => (
              <AlertRow
                key={alert.id}
                alert={alert}
                busy={actionId === alert.id}
                runResult={runResult?.id === alert.id ? runResult.res : null}
                history={historyFor === alert.id ? (historyData?.items ?? []) : null}
                onToggleActive={(active) =>
                  runAction(alert, () => alertApi.setActive(alert.id, active), active ? '已启用' : '已停用', '切换失败')
                }
                onTest={(force) => runTest(alert, force)}
                onToggleHistory={() => setHistoryFor(historyFor === alert.id ? null : alert.id)}
                onDelete={() =>
                  runAction(alert, () => alertApi.remove(alert.id), '已删除', '删除失败')
                }
              />
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  )
}

// ── 单行任务卡片 ──
function AlertRow(props: {
  alert: IndicatorAlert
  busy: boolean
  runResult: AlertRunResult | null
  history: TriggerRecord[] | null
  onToggleActive: (active: boolean) => void
  onTest: (force: boolean) => void
  onToggleHistory: () => void
  onDelete: () => void
}) {
  const { alert, busy, runResult, history } = props
  return (
    <div
      className={cn(
        'bg-quant-card border border-quant-border rounded-xl p-3 transition-all hover:border-quant-gold/20',
        !alert.active && 'opacity-70'
      )}
    >
      <div className="flex items-start gap-3 flex-wrap">
        <div className="flex-1 min-w-[220px] space-y-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-semibold text-sm">{alert.name}</span>
            <span className="text-[11px] px-1.5 py-0.5 rounded bg-quant-bg border border-quant-border">
              {alert.symbol} · {alert.interval}
            </span>
            {alert.message && <span className="text-[11px] text-muted-foreground">附言: {alert.message}</span>}
          </div>
          <div className="font-mono text-[11px] text-quant-gold/90">{alert.condition_expr}</div>
          <div className="text-[11px] text-muted-foreground flex gap-4 flex-wrap">
            <span>冷却 {alert.cooldown_minutes} 分钟</span>
            <span>最近触发: {fmtTime(alert.last_triggered_at)}</span>
            <span>最近值: {fmtValue(alert.last_value)}</span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Switch checked={alert.active} onCheckedChange={props.onToggleActive} disabled={busy} />
          <Button
            variant="outline"
            size="sm"
            leftIcon={<Play className="w-3 h-3" />}
            isLoading={busy}
            onClick={() => props.onTest(false)}
          >
            测试
          </Button>
          <Button variant="ghost" size="sm" leftIcon={<History className="w-3 h-3" />} onClick={props.onToggleHistory}>
            历史
          </Button>
          <Button variant="ghost" size="sm" leftIcon={<Trash2 className="w-3 h-3 text-red-400" />} onClick={props.onDelete} />
        </div>
      </div>

      {runResult && (
        <div
          className={cn(
            'mt-2 text-[11px] rounded-lg px-3 py-2 border',
            runResult.error
              ? 'border-red-500/40 bg-red-500/10 text-red-300'
              : runResult.triggered
                ? 'border-quant-gold/40 bg-quant-gold/10 text-quant-gold'
                : runResult.matched
                  ? 'border-yellow-500/40 bg-yellow-500/10 text-yellow-300'
                  : 'border-quant-border bg-quant-bg text-muted-foreground'
          )}
        >
          {runResult.error ? (
            <>求值失败：{runResult.error}</>
          ) : (
            <>
              命中: {runResult.matched ? '是' : '否'} · 当前值 {fmtValue(runResult.value)} · K线 {runResult.bars} 根
              {runResult.triggered
                ? ' · 已发送通知'
                : runResult.matched && (runResult.cooldown_remaining_sec ?? 0) > 0
                  ? ` · 冷却中（剩余 ${Math.ceil((runResult.cooldown_remaining_sec ?? 0) / 60)} 分钟）`
                  : ''}
            </>
          )}
          {!runResult.error && runResult.matched && !runResult.triggered && (
            <button className="ml-2 underline inline-flex items-center gap-0.5" onClick={() => props.onTest(true)}>
              <Zap className="w-3 h-3" /> 强制触发
            </button>
          )}
        </div>
      )}

      {history && (
        <div className="mt-2 border-t border-quant-border pt-2">
          {history.length === 0 ? (
            <div className="text-[11px] text-muted-foreground">暂无触发记录</div>
          ) : (
            <div className="space-y-1">
              {history.map((h, i) => (
                <div key={i} className="text-[11px] text-muted-foreground flex gap-3">
                  <span>{fmtTime(h.at)}</span>
                  <span>{h.symbol}</span>
                  <span>值 {fmtValue(h.value)}</span>
                  <span className="font-mono truncate">{h.expr}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── 创建表单 ──
function AlertForm(props: { onSubmit: (payload: AlertPayload) => Promise<void>; onCancel: () => void }) {
  const [name, setName] = useState('')
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  const [expr, setExpr] = useState(EXPR_EXAMPLES[0].expr)
  const [message, setMessage] = useState('')
  const [cooldown, setCooldown] = useState(60)
  const [submitting, setSubmitting] = useState(false)
  const [expandedHelp, setExpandedHelp] = useState(false)

  const submit = async () => {
    setSubmitting(true)
    try {
      await props.onSubmit({
        name: name.trim(),
        symbol: symbol.trim(),
        interval,
        condition_expr: expr.trim(),
        message: message.trim(),
        cooldown_minutes: cooldown,
      })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <SectionCard title="新建告警任务">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <label className="space-y-1">
          <div className="text-[11px] text-muted-foreground">名称</div>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="如：BTC 超卖提醒" />
        </label>
        <label className="space-y-1">
          <div className="text-[11px] text-muted-foreground">交易对</div>
          <input className={inputCls} value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} placeholder="BTCUSDT" />
        </label>
        <label className="space-y-1">
          <div className="text-[11px] text-muted-foreground">K线周期</div>
          <select className={inputCls} value={interval} onChange={(e) => setInterval(e.target.value)}>
            {INTERVALS.map((iv) => (
              <option key={iv} value={iv}>
                {iv}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1">
          <div className="text-[11px] text-muted-foreground">冷却（分钟，0=不冷却）</div>
          <input
            className={inputCls}
            type="number"
            min={0}
            max={10080}
            value={cooldown}
            onChange={(e) => setCooldown(Number(e.target.value))}
          />
        </label>
        <label className="md:col-span-2 space-y-1">
          <div className="flex items-center gap-2">
            <span className="text-[11px] text-muted-foreground">条件表达式</span>
            <select
              className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-[11px] focus:outline-none focus:border-quant-gold"
              value=""
              onChange={(e) => {
                const hit = EXPR_EXAMPLES.find((x) => x.key === e.target.value)
                if (hit) setExpr(hit.expr)
              }}
            >
              <option value="">插入示例…</option>
              {EXPR_EXAMPLES.map((x) => (
                <option key={x.key} value={x.key}>
                  {x.label}
                </option>
              ))}
            </select>
            <button type="button" className="text-[11px] text-quant-gold inline-flex items-center gap-0.5" onClick={() => setExpandedHelp(!expandedHelp)}>
              语法说明 {expandedHelp ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
            </button>
          </div>
          <textarea
            className={cn(inputCls, 'font-mono h-20')}
            value={expr}
            onChange={(e) => setExpr(e.target.value)}
            placeholder="如：rsi14 < 30 && close > ema20"
          />
          {expandedHelp && (
            <div className="text-[11px] text-muted-foreground bg-quant-bg border border-quant-border rounded-lg p-2 space-y-1">
              <div>裸标识符：open / high / low / close / volume（最新一根）</div>
              <div>
                指标函数：rsi、ema、sma、atr、macd、macd_signal、macd_hist、bb_upper、bb_mid、bb_lower、bb_width、bb_pctb；支持尾数简写（rsi14=rsi(14)）与括号参数（bb_upper(20,2)）
              </div>
              <div>运算符：&& || == != &gt; &gt;= &lt; &lt;= + - * / 与括号，支持一元负号；比较不可链式（1&lt;2&lt;3 非法）</div>
              <div>长度上限 500 字符，函数参数只允许数字字面量</div>
            </div>
          )}
        </label>
        <label className="md:col-span-2 space-y-1">
          <div className="text-[11px] text-muted-foreground">通知附言（可选）</div>
          <input className={inputCls} value={message} onChange={(e) => setMessage(e.target.value)} placeholder="命中时附加在通知正文里" />
        </label>
      </div>
      <div className="flex justify-end gap-2 mt-4">
        <Button variant="ghost" size="sm" onClick={props.onCancel}>
          取消
        </Button>
        <Button
          variant="primary"
          size="sm"
          isLoading={submitting}
          disabled={!name.trim() || !expr.trim() || !symbol.trim()}
          onClick={submit}
        >
          创建
        </Button>
      </div>
    </SectionCard>
  )
}

export default AlertManager
