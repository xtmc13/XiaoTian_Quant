import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, Clock, Play, Plus, Trash2, X } from 'lucide-react'
import { agentCronApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'

// ── 调度预设（生成 cron 表达式；自定义直接输入） ──
const SCHEDULE_PRESETS: { label: string; value: string }[] = [
  { label: '每天 08:00', value: '0 8 * * *' },
  { label: '每 30 分钟', value: '*/30 * * * *' },
  { label: '每小时整点', value: '0 * * * *' },
  { label: '工作日 09:00', value: '0 9 * * 1-5' },
  { label: '每周一 09:00', value: '0 9 * * 1' },
  { label: '每月 1 日 08:00', value: '0 8 1 * *' },
]

const CHANNELS = [
  { value: 'web', label: '助手面板' },
  { value: 'telegram', label: 'Telegram' },
  { value: 'lark', label: '飞书' },
  { value: 'dingtalk', label: '钉钉' },
  { value: 'email', label: '邮件' },
]

function fmtTime(ts: number): string {
  if (!ts) return '—'
  const d = new Date(ts < 1e12 ? ts * 1000 : ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export interface CronPanelProps {
  onClose: () => void
}

// ── 定时任务面板：列表 + 创建（cron 插件的 UI 入口） ──
export function CronPanel({ onClose }: CronPanelProps) {
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [prompt, setPrompt] = useState('')
  const [preset, setPreset] = useState(SCHEDULE_PRESETS[0].value)
  const [custom, setCustom] = useState('')
  const [channel, setChannel] = useState('web')

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-cron-jobs'],
    queryFn: () => agentCronApi.list(),
    retry: false,
  })
  const jobs = data?.jobs || []

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-cron-jobs'] })

  const createMut = useMutation({
    mutationFn: () =>
      agentCronApi.create({
        name: name.trim(),
        prompt: prompt.trim(),
        schedule: preset === 'custom' ? custom.trim() : preset,
        channel,
        timezone: 'Asia/Shanghai',
      }),
    onSuccess: () => {
      toast('success', '定时任务已创建')
      setShowCreate(false)
      setName('')
      setPrompt('')
      invalidate()
    },
    onError: (e: Error) => toast('error', e.message || '创建失败'),
  })

  const toggleMut = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => agentCronApi.toggle(id, enabled),
    onSuccess: invalidate,
    onError: () => toast('error', '操作失败'),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => agentCronApi.remove(id),
    onSuccess: invalidate,
    onError: () => toast('error', '删除失败'),
  })

  const runMut = useMutation({
    mutationFn: (id: string) => agentCronApi.run(id),
    onSuccess: () => {
      toast('success', '已触发，稍后刷新查看结果')
      setTimeout(invalidate, 8000)
    },
    onError: () => toast('error', '触发失败'),
  })

  return (
    <div
      className="absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="定时任务"
    >
      <div
        className="flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <Clock size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">定时任务</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={() => setShowCreate((v) => !v)}
            aria-label="新建定时任务"
            className="flex items-center gap-1 rounded-full bg-[var(--ag-text1)] px-2.5 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85"
          >
            <Plus size={12} />
            新建
          </button>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭定时任务面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-white/8"
          >
            <X size={15} />
          </button>
        </div>

        {/* 创建表单 */}
        {showCreate && (
          <div className="space-y-2 border-b border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-4 py-3">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="任务名，如：每日持仓汇报"
              aria-label="任务名"
              className="w-full rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
            />
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              rows={2}
              placeholder="到点后交给助手的指令，如：汇总我的持仓和今日盈亏，给出简短建议"
              aria-label="任务指令"
              className="w-full resize-none rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
            />
            <div className="flex flex-wrap items-center gap-2">
              <select
                value={preset}
                onChange={(e) => setPreset(e.target.value)}
                aria-label="调度预设"
                className="rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] focus:outline-none"
              >
                {SCHEDULE_PRESETS.map((p) => (
                  <option key={p.value} value={p.value}>
                    {p.label}
                  </option>
                ))}
                <option value="custom">自定义…</option>
              </select>
              {preset === 'custom' && (
                <input
                  value={custom}
                  onChange={(e) => setCustom(e.target.value)}
                  placeholder="cron 表达式或自然语言，如 0 8 * * 1-5 / 每天收盘后"
                  aria-label="自定义 cron 表达式"
                  className="w-44 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 font-mono text-[11px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
                />
              )}
              <select
                value={channel}
                onChange={(e) => setChannel(e.target.value)}
                aria-label="结果投递"
                className="rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] focus:outline-none"
              >
                {CHANNELS.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
              <button
                type="button"
                disabled={
                  !name.trim() || !prompt.trim() || (preset === 'custom' && !custom.trim()) || createMut.isPending
                }
                onClick={() => createMut.mutate()}
                className="ml-auto rounded-lg bg-[var(--ag-accent)] px-3 py-1.5 text-[12px] font-medium text-white hover:opacity-90 disabled:opacity-40"
              >
                创建
              </button>
            </div>
          </div>
        )}

        {/* 列表 */}
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {!isLoading && jobs.length === 0 && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              暂无定时任务，点右上角「新建」创建
            </p>
          )}
          {jobs.map((j) => (
            <div
              key={j.id}
              className="group mb-1 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2 hover:border-[var(--ag-stroke2)]"
            >
              <div className="flex items-center gap-2">
                {/* 启停 */}
                <button
                  type="button"
                  role="switch"
                  aria-checked={j.enabled}
                  aria-label={`${j.enabled ? '停用' : '启用'} ${j.name}`}
                  onClick={() => toggleMut.mutate({ id: j.id, enabled: !j.enabled })}
                  className={cn(
                    'relative h-4 w-7 shrink-0 rounded-full transition-colors',
                    j.enabled ? 'bg-[var(--ag-green)]' : 'bg-[var(--ag-stroke1)]'
                  )}
                >
                  <span
                    className={cn(
                      'absolute top-0.5 size-3 rounded-full bg-white transition-all',
                      j.enabled ? 'left-3.5' : 'left-0.5'
                    )}
                  />
                </button>
                <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--ag-text1)]">{j.name}</span>
                {j.last_status === 'error' && (
                  <span
                    title={j.last_result}
                    className="flex shrink-0 items-center gap-0.5 text-[10px] text-[var(--ag-red)]"
                  >
                    <CircleAlert size={10} />
                    上次失败
                  </span>
                )}
                <button
                  type="button"
                  title="立即运行一次"
                  aria-label={`立即运行 ${j.name}`}
                  onClick={() => runMut.mutate(j.id)}
                  className="shrink-0 rounded p-1 text-[var(--ag-text3)] opacity-0 transition-opacity hover:bg-white/8 hover:text-[var(--ag-text1)] group-hover:opacity-100"
                >
                  <Play size={12} />
                </button>
                <button
                  type="button"
                  title="删除"
                  aria-label={`删除任务 ${j.name}`}
                  onClick={() => deleteMut.mutate(j.id)}
                  className="shrink-0 rounded p-1 text-[var(--ag-text3)] opacity-0 transition-opacity hover:bg-white/8 hover:text-[var(--ag-red)] group-hover:opacity-100"
                >
                  <Trash2 size={12} />
                </button>
              </div>
              <div className="mt-1 flex items-center gap-2 pl-9 text-[10.5px] text-[var(--ag-text3)]">
                <code className="rounded bg-[var(--ag-inline-code-bg)] px-1 py-0.5 font-mono text-[10px]">
                  {j.schedule}
                </code>
                <span>下次 {fmtTime(j.next_run_at)}</span>
                <span className="capitalize">{CHANNELS.find((c) => c.value === j.channel)?.label || j.channel}</span>
              </div>
              {j.last_result && (
                <p
                  className="mt-1 line-clamp-2 pl-9 text-[10.5px] leading-relaxed text-[var(--ag-text3)]"
                  title={j.last_result}
                >
                  {j.last_result}
                </p>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export default CronPanel
