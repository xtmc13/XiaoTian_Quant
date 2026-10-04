import { useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { hyperoptApi, type HyperoptExportResult } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'
import { AlertTriangle, Loader2, X } from 'lucide-react'

/**
 * Hyperopt 最优参数一键回写（POST /hyperopt/jobs/:id/export）。
 * 弹层本身即二次确认：展示参数映射（可编辑目标字段名）与覆盖警告，
 * 用户显式点「确认导出」才会写 strategy_configs.json。
 */
export function HyperoptExportModal({
  jobId,
  bestParams,
  onClose,
}: {
  jobId: string
  bestParams: Record<string, unknown>
  onClose: () => void
}) {
  const { t } = useI18n()
  const [strategyId, setStrategyId] = useState('')
  const [strategyName, setStrategyName] = useState('')
  const [paramMap, setParamMap] = useState<Record<string, string>>(() =>
    Object.fromEntries(Object.keys(bestParams).map((k) => [k, k]))
  )
  const [exported, setExported] = useState<HyperoptExportResult | null>(null)

  const exportMut = useMutation({
    mutationFn: () => {
      const map = Object.fromEntries(Object.entries(paramMap).filter(([, v]) => v.trim() !== ''))
      return hyperoptApi.exportParams(jobId, {
        strategy_id: strategyId.trim() || undefined,
        strategy_name: strategyName.trim() || undefined,
        param_map: map,
      })
    },
    onSuccess: (d) => {
      setExported(d)
      toast('success', t('hyperopt.export.success').replace('{n}', String(Object.keys(d?.mapped_params ?? {}).length)))
    },
    onError: (err: unknown) => toast('error', err instanceof Error ? err.message : t('hyperopt.export.failed')),
  })

  const handleConfirm = () => {
    const map = Object.fromEntries(Object.entries(paramMap).filter(([, v]) => v.trim() !== ''))
    if (Object.keys(map).length === 0) {
      toast('warning', t('hyperopt.export.needMap'))
      return
    }
    exportMut.mutate()
  }

  const keys = useMemo(() => Object.keys(bestParams), [bestParams])
  const inputCls =
    'w-full rounded-md border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-foreground focus:outline-none focus:border-quant-gold'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="w-full max-w-lg rounded-xl border border-quant-border bg-quant-card p-5 space-y-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold">{t('hyperopt.export.title')}</span>
          <button onClick={onClose} aria-label="close" className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>
        <p className="text-xs text-muted-foreground">{t('hyperopt.export.desc')}</p>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label className="mb-1 block text-[11px] text-muted-foreground">{t('hyperopt.export.strategyId')}</label>
            <input
              value={strategyId}
              onChange={(e) => setStrategyId(e.target.value)}
              placeholder={t('hyperopt.export.strategyIdPlaceholder')}
              className={inputCls}
            />
          </div>
          <div>
            <label className="mb-1 block text-[11px] text-muted-foreground">{t('hyperopt.export.strategyName')}</label>
            <input
              value={strategyName}
              onChange={(e) => setStrategyName(e.target.value)}
              placeholder={t('hyperopt.export.strategyNamePlaceholder')}
              className={inputCls}
            />
          </div>
        </div>

        <div>
          <div className="mb-1 text-[11px] text-muted-foreground">{t('hyperopt.export.paramMap')}</div>
          <div className="max-h-48 space-y-1.5 overflow-y-auto rounded-md border border-quant-border p-2">
            {keys.map((k) => (
              <div key={k} className="flex items-center gap-2">
                <span className="w-2/5 truncate font-mono text-[11px] text-muted-foreground" title={k}>
                  {k}
                </span>
                <span className="text-[10px] text-muted-foreground">→</span>
                <input
                  value={paramMap[k] ?? ''}
                  onChange={(e) => setParamMap((p) => ({ ...p, [k]: e.target.value }))}
                  className={inputCls}
                  aria-label={`map-${k}`}
                />
                <span className="w-1/4 truncate text-right font-mono text-[11px]" title={String(bestParams[k])}>
                  {String(bestParams[k])}
                </span>
              </div>
            ))}
          </div>
        </div>

        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 p-2.5 text-[11px] text-amber-400">
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {t('hyperopt.export.overwriteWarn')}
        </div>

        {exported && (
          <div className="rounded-md bg-quant-bg-secondary p-2.5">
            <div className="mb-1 text-[11px] text-muted-foreground">{t('hyperopt.export.mappedParams')}</div>
            <pre className="max-h-28 overflow-y-auto whitespace-pre-wrap font-mono text-[10px] text-foreground/80">
              {JSON.stringify(exported.mapped_params, null, 2)}
            </pre>
          </div>
        )}

        <button
          onClick={handleConfirm}
          disabled={exportMut.isPending}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg bg-quant-gold px-4 py-2 text-xs font-semibold text-black transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {exportMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
          {exportMut.isPending ? t('hyperopt.export.exporting') : t('hyperopt.export.confirm')}
        </button>
      </div>
    </div>
  )
}
