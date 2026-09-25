/**
 * PythonStrategyPage — 用户 Python 策略契约化运行时 v1 IDE
 * 布局：左侧策略列表 + manifest 表单；右侧 CodeMirror 代码编辑器；
 * 底部校验结果 + 日志区。保存自动打版本快照；启动/停止驱动 runner。
 * 契约见 docs/PYTHON_STRATEGY_API.md。
 */
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { CodeEditor } from '@/components/ide/CodeEditor'
import { TRADING_INTERVALS } from '@/lib/constants'
import {
  pyStrategyApi,
  type PyStrategy,
  type PyStrategyPayload,
  type PyStrategyValidateResult,
} from '@/lib/pystratApi'
import {
  Play,
  Square,
  Save,
  ShieldCheck,
  Plus,
  Trash2,
  Loader2,
  AlertCircle,
  RefreshCw,
  FileCode2,
} from 'lucide-react'

const DEFAULT_CODE = `import math

STRATEGY_MANIFEST = {
    "name": "双均线 crossover",
    "symbol": "BTC/USDT",
    "interval": "15m",
    "direction": "long",
    "params": {"fast": 9, "slow": 21},
    "risk": {"max_position_pct": 0.5, "stop_loss_pct": 0.03, "take_profit_pct": 0.1},
}

def initialize(context):
    context.fast_hist = []
    context.slow_hist = []
    context.log("initialized " + context.symbol)

def on_bar(context, bar):
    context.fast_hist.append(bar["close"])
    context.slow_hist.append(bar["close"])
    if len(context.fast_hist) < int(context.params["slow"]):
        return
    fast_ma = sum(context.fast_hist[-int(context.params["fast"]):]) / float(context.params["fast"])
    slow_ma = sum(context.slow_hist[-int(context.params["slow"]):]) / float(context.params["slow"])
    has_pos = bool(context.position and context.position.get("qty", 0) > 0)
    if fast_ma > slow_ma and not has_pos:
        context.buy(amount=context.equity * 0.1)
        context.set_stop_loss(0.03)
        context.set_take_profit(0.1)
    elif fast_ma < slow_ma and has_pos:
        context.close_position()

# ── v1.2 可选契约钩子（对标 freqtrade IStrategy，删掉注释即可启用）──
# manifest.risk 可加 "max_position_adjustments": 2（加仓次数上限）与
# "entry_timeout_minutes": 10（限价挂单超时分钟数，超时默认撤单）。
#
# def confirm_entry(context, side, price, amount):
#     return amount <= 500          # 下单前最后一刻确认；False 否决入场
#
# def confirm_exit(context, side, price, qty):
#     return True                   # False 否决出场（持仓保留）
#
# def custom_stake_amount(context, proposed_amount, price, side):
#     return proposed_amount        # 返回 >0 覆盖入场金额（USDT），0 用默认
#
# def adjust_trade_position(context, bar, position):
#     avg = position.get("avg_price", 0)
#     if avg > 0 and bar["close"] < avg * 0.97:
#         return 100                # >0 加仓 USDT 金额（DCA）；<0 减仓；0 不动
#     return 0
#
# def check_entry_timeout(context, order):
#     return True                   # 未成交挂单超时：True 撤单；False 保留
`

const STATUS_BADGE: Record<string, { label: string; className: string }> = {
  draft: { label: '草稿', className: 'bg-slate-600/30 text-slate-300' },
  active: { label: '运行中', className: 'bg-emerald-600/30 text-emerald-300' },
  paused: { label: '已暂停', className: 'bg-amber-600/30 text-amber-300' },
  error: { label: '错误', className: 'bg-red-600/30 text-red-300' },
}

export default function PythonStrategyPage() {
  const queryClient = useQueryClient()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [name, setName] = useState('我的 Python 策略')
  const [symbol, setSymbol] = useState('BTC/USDT')
  const [interval, setInterval] = useState('15m')
  const [direction, setDirection] = useState<'long' | 'short' | 'both'>('long')
  const [market, setMarket] = useState<'spot' | 'futures'>('spot')
  const [leverage, setLeverage] = useState(1)
  const [marginMode, setMarginMode] = useState<'cross' | 'isolated'>('cross')
  const [paramsJSON, setParamsJSON] = useState('{}')
  const [paper, setPaper] = useState(true)
  const [code, setCode] = useState(DEFAULT_CODE)
  const [savedId, setSavedId] = useState<string | null>(null)
  const [validateResult, setValidateResult] = useState<PyStrategyValidateResult | null>(null)

  const { data: strategies = [], isLoading } = useQuery({
    queryKey: ['pystrategies'],
    queryFn: pyStrategyApi.list,
    refetchInterval: 10000,
  })

  const selected = useMemo(
    () => strategies.find((s) => s.id === selectedId) ?? null,
    [strategies, selectedId],
  )

  const { data: logsData, refetch: refetchLogs } = useQuery({
    queryKey: ['pystrategies', savedId, 'logs'],
    queryFn: () => pyStrategyApi.logs(savedId as string),
    enabled: !!savedId && !!selected?.is_running,
    refetchInterval: 3000,
  })

  // 选中列表项 → 装载到表单
  useEffect(() => {
    if (!selected) return
    setName(selected.name)
    setSymbol(selected.symbol)
    setInterval(selected.interval || '15m')
    setDirection((selected.direction as 'long' | 'short' | 'both') || 'long')
    setMarket((selected.market as 'spot' | 'futures') || 'spot')
    setLeverage(selected.leverage || 1)
    setMarginMode((selected.margin_mode as 'cross' | 'isolated') || 'cross')
    setParamsJSON(selected.params_json || '{}')
    setPaper(selected.paper)
    setCode(selected.code)
    setSavedId(selected.id)
    setValidateResult(null)
  }, [selected])

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['pystrategies'] })

  const saveMutation = useMutation({
    mutationFn: async () => {
      const payload: PyStrategyPayload = {
        name,
        symbol,
        interval,
        direction,
        market,
        leverage,
        margin_mode: marginMode,
        params_json: paramsJSON,
        code,
        paper,
      }
      if (savedId) return pyStrategyApi.update(savedId, payload)
      return pyStrategyApi.create(payload)
    },
    onSuccess: (rec: PyStrategy) => {
      setSavedId(rec.id)
      setSelectedId(rec.id)
      toast('success', '已保存（版本 v' + (rec.version || 1) + '）')
      invalidate()
    },
    onError: (e: Error) => toast('error', '保存失败: ' + e.message),
  })

  const validateMutation = useMutation({
    mutationFn: async () => {
      let id = savedId
      if (!id) {
        const rec = await saveMutation.mutateAsync()
        id = rec.id
      }
      return pyStrategyApi.validate(id)
    },
    onSuccess: (r) => {
      setValidateResult(r)
      toast(r.valid ? 'success' : 'error', r.valid ? '校验通过' : '校验未通过')
    },
    onError: (e: Error) => toast('error', '校验失败: ' + e.message),
  })

  const startMutation = useMutation({
    mutationFn: async () => {
      let id = savedId
      if (!id) {
        const rec = await saveMutation.mutateAsync()
        id = rec.id
      }
      return pyStrategyApi.start(id)
    },
    onSuccess: () => {
      toast('success', '已启动')
      invalidate()
      refetchLogs()
    },
    onError: (e: Error) => {
      toast('error', '启动失败: ' + e.message)
      invalidate()
    },
  })

  const stopMutation = useMutation({
    mutationFn: () => pyStrategyApi.stop(savedId as string),
    onSuccess: () => {
      toast('success', '已停止')
      invalidate()
    },
    onError: (e: Error) => toast('error', '停止失败: ' + e.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => pyStrategyApi.remove(id),
    onSuccess: () => {
      toast('success', '已删除')
      if (savedId === selectedId) {
        setSavedId(null)
        setSelectedId(null)
      }
      invalidate()
    },
    onError: (e: Error) => toast('error', '删除失败: ' + e.message),
  })

  const running = !!selected?.is_running
  const busy =
    saveMutation.isPending || validateMutation.isPending || startMutation.isPending || stopMutation.isPending

  return (
    <div className="flex h-full min-h-0 gap-4 p-4 text-slate-200">
      {/* 左栏：列表 + manifest 表单 */}
      <div className="flex w-80 shrink-0 flex-col gap-4 overflow-y-auto">
        <div className="rounded-lg border border-slate-700 bg-slate-900/60 p-3">
          <div className="mb-2 flex items-center justify-between">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <FileCode2 size={16} /> Python 策略
            </h2>
            <button
              className="rounded p-1 hover:bg-slate-700"
              title="新建策略"
              onClick={() => {
                setSelectedId(null)
                setSavedId(null)
                setName('我的 Python 策略')
                setSymbol('BTC/USDT')
                setInterval('15m')
                setDirection('long')
                setParamsJSON('{}')
                setPaper(true)
                setCode(DEFAULT_CODE)
                setValidateResult(null)
              }}
            >
              <Plus size={16} />
            </button>
          </div>
          {isLoading ? (
            <div className="flex justify-center py-4 text-slate-500">
              <Loader2 className="animate-spin" size={18} />
            </div>
          ) : strategies.length === 0 ? (
            <p className="py-3 text-center text-xs text-slate-500">暂无策略，点击 + 新建</p>
          ) : (
            <ul className="space-y-1">
              {strategies.map((s) => {
                const badge = STATUS_BADGE[s.status] ?? STATUS_BADGE.draft
                return (
                  <li
                    key={s.id}
                    className={cn(
                      'flex cursor-pointer items-center justify-between rounded px-2 py-1.5 text-sm hover:bg-slate-800',
                      selectedId === s.id && 'bg-slate-800',
                    )}
                    onClick={() => setSelectedId(s.id)}
                  >
                    <span className="truncate">{s.name}</span>
                    <span
                      className={cn(
                        'ml-2 shrink-0 rounded-full px-2 py-0.5 text-[10px]',
                        badge.className,
                      )}
                    >
                      {badge.label}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </div>

        <div className="space-y-3 rounded-lg border border-slate-700 bg-slate-900/60 p-3">
          <div>
            <label className="mb-1 block text-xs text-slate-400">名称</label>
            <input
              className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="mb-1 block text-xs text-slate-400">Symbol</label>
              <input
                className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
                value={symbol}
                onChange={(e) => setSymbol(e.target.value)}
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-400">周期</label>
              <select
                className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
                value={interval}
                onChange={(e) => setInterval(e.target.value)}
              >
                {TRADING_INTERVALS.map((it) => (
                  <option key={it} value={it}>
                    {it}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">方向</label>
            <select
              className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
              value={direction}
              onChange={(e) => setDirection(e.target.value as 'long' | 'short' | 'both')}
            >
              <option value="long">long（只做多）</option>
              <option value="short">short</option>
              <option value="both">both</option>
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">市场（v1.1）</label>
            <select
              className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
              value={market}
              onChange={(e) => setMarket(e.target.value as 'spot' | 'futures')}
            >
              <option value="spot">spot（现货执行）</option>
              <option value="futures">futures（合约杠杆，paper=0 生效）</option>
            </select>
          </div>
          {market === 'futures' && (
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="mb-1 block text-xs text-slate-400">杠杆（1-125）</label>
                <input
                  type="number" min={1} max={125}
                  className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
                  value={leverage}
                  onChange={(e) => setLeverage(Math.max(1, Math.min(125, Number(e.target.value) || 1)))}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-slate-400">保证金模式</label>
                <select
                  className="w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 text-sm"
                  value={marginMode}
                  onChange={(e) => setMarginMode(e.target.value as 'cross' | 'isolated')}
                >
                  <option value="cross">cross（全仓）</option>
                  <option value="isolated">isolated（逐仓）</option>
                </select>
              </div>
            </div>
          )}
          <div>
            <label className="mb-1 block text-xs text-slate-400">参数覆盖 JSON</label>
            <textarea
              className="h-20 w-full rounded border border-slate-600 bg-slate-800 px-2 py-1.5 font-mono text-xs"
              value={paramsJSON}
              onChange={(e) => setParamsJSON(e.target.value)}
              placeholder='{"fast": 9}'
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={paper} onChange={(e) => setPaper(e.target.checked)} />
            模拟盘（paper=1 强制模拟账户；关闭 = 实盘，需管理员解锁）
          </label>
          <div className="flex gap-2 pt-1">
            <button
              className="flex flex-1 items-center justify-center gap-1 rounded bg-slate-700 px-3 py-1.5 text-sm hover:bg-slate-600 disabled:opacity-50"
              onClick={() => saveMutation.mutate()}
              disabled={busy}
            >
              <Save size={14} /> 保存
            </button>
            <button
              className="flex flex-1 items-center justify-center gap-1 rounded bg-slate-700 px-3 py-1.5 text-sm hover:bg-slate-600 disabled:opacity-50"
              onClick={() => validateMutation.mutate()}
              disabled={busy}
            >
              <ShieldCheck size={14} /> 校验
            </button>
            {running ? (
              <button
                className="flex flex-1 items-center justify-center gap-1 rounded bg-amber-700 px-3 py-1.5 text-sm hover:bg-amber-600 disabled:opacity-50"
                onClick={() => stopMutation.mutate()}
                disabled={busy}
              >
                <Square size={14} /> 停止
              </button>
            ) : (
              <button
                className="flex flex-1 items-center justify-center gap-1 rounded bg-emerald-700 px-3 py-1.5 text-sm hover:bg-emerald-600 disabled:opacity-50"
                onClick={() => startMutation.mutate()}
                disabled={busy}
              >
                <Play size={14} /> 启动
              </button>
            )}
          </div>
          {savedId && (
            <button
              className="flex w-full items-center justify-center gap-1 rounded border border-red-900 px-3 py-1.5 text-sm text-red-400 hover:bg-red-950/40 disabled:opacity-50"
              onClick={() => deleteMutation.mutate(savedId)}
              disabled={busy || deleteMutation.isPending}
            >
              <Trash2 size={14} /> 删除
            </button>
          )}
        </div>
      </div>

      {/* 右栏：编辑器 + 校验 + 日志 */}
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div className="min-h-0 flex-1 rounded-lg border border-slate-700 bg-slate-900/60">
          <CodeEditor value={code} onChange={setCode} onSave={() => saveMutation.mutate()} />
        </div>

        {validateResult && (
          <div
            className={cn(
              'max-h-40 overflow-y-auto rounded-lg border p-3 text-sm',
              validateResult.valid
                ? 'border-emerald-800 bg-emerald-950/40 text-emerald-300'
                : 'border-red-800 bg-red-950/40 text-red-300',
            )}
          >
            <div className="mb-1 flex items-center gap-1 font-semibold">
              {validateResult.valid ? <ShieldCheck size={14} /> : <AlertCircle size={14} />}
              {validateResult.valid ? '校验通过（静态 + 沙箱）' : '校验未通过'}
            </div>
            {validateResult.issues?.map((iss, i) => (
              <div key={i} className="font-mono text-xs">
                {iss.line > 0 ? `第 ${iss.line} 行: ` : ''}
                {iss.message}
              </div>
            ))}
            {validateResult.sandbox_error && (
              <div className="font-mono text-xs">沙箱: {validateResult.sandbox_error}</div>
            )}
          </div>
        )}

        <div className="h-48 shrink-0 overflow-y-auto rounded-lg border border-slate-700 bg-slate-900/60 p-3">
          <div className="mb-2 flex items-center justify-between text-sm">
            <span className="font-semibold">运行日志</span>
            <button className="rounded p-1 hover:bg-slate-700" onClick={() => refetchLogs()}>
              <RefreshCw size={14} />
            </button>
          </div>
          {logsData?.logs?.length ? (
            <div className="space-y-0.5 font-mono text-xs">
              {logsData.logs.map((log, i) => (
                <div
                  key={i}
                  className={cn(
                    log.level === 'error' && 'text-red-400',
                    log.level === 'action' && 'text-emerald-400',
                    log.level === 'info' && 'text-slate-400',
                  )}
                >
                  [{new Date(log.ts).toLocaleTimeString()}] {log.message}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-slate-500">
              {running ? '等待日志…' : '启动策略后此处显示运行日志（context.log / 成交 / 错误）'}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
