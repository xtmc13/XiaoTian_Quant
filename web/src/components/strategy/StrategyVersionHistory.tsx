import { useCallback, useEffect, useState } from 'react'
import { History, RotateCcw, Eye, Camera } from 'lucide-react'
import { strategyVersionApi, type StrategyVersionItem } from '@/lib/api'
import { toast } from '@/lib/useToast'

/** 历史版本区块：列出策略配置快照，支持查看 JSON 与恢复（恢复前 confirm）。 */
export function StrategyVersionHistory({ strategyId }: { strategyId: string }) {
  const [versions, setVersions] = useState<StrategyVersionItem[]>([])
  const [loading, setLoading] = useState(false)
  const [viewing, setViewing] = useState<{ version: number; json: string } | null>(null)
  const [restoring, setRestoring] = useState<number | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setVersions(await strategyVersionApi.list(strategyId))
    } catch (e) {
      toast('error', `加载历史版本失败: ${(e as Error).message}`)
    } finally {
      setLoading(false)
    }
  }, [strategyId])

  useEffect(() => {
    void load()
  }, [load])

  const handleView = async (version: number) => {
    try {
      const detail = await strategyVersionApi.get(strategyId, version)
      setViewing({ version, json: JSON.stringify(detail.payload, null, 2) })
    } catch (e) {
      toast('error', `查看版本失败: ${(e as Error).message}`)
    }
  }

  const handleSnapshot = async () => {
    const note = window.prompt('快照备注（可选）', '') ?? ''
    try {
      const created = await strategyVersionApi.create(strategyId, note.trim() || undefined)
      toast('success', `已保存版本 v${created.version}`)
      await load()
    } catch (e) {
      toast('error', `保存快照失败: ${(e as Error).message}`)
    }
  }

  const handleRestore = async (version: number) => {
    if (!window.confirm(`确定恢复版本 v${version} 吗？当前配置会先自动保存为一个新版本。`)) return
    setRestoring(version)
    try {
      await strategyVersionApi.restore(strategyId, version)
      toast('success', `已恢复版本 v${version}`)
      await load()
    } catch (e) {
      toast('error', `恢复失败: ${(e as Error).message}`)
    } finally {
      setRestoring(null)
    }
  }

  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-4 space-y-3">
      <div className="flex items-center gap-2">
        <History className="w-4 h-4 text-quant-gold" />
        <span className="text-sm font-bold">历史版本</span>
        <span className="flex-1" />
        <button
          onClick={handleSnapshot}
          className="px-3 py-1.5 rounded-lg bg-quant-bg border border-quant-border text-xs text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1.5"
        >
          <Camera className="w-3.5 h-3.5" /> 保存当前快照
        </button>
      </div>
      {loading ? (
        <div className="text-xs text-muted-foreground">加载中...</div>
      ) : versions.length === 0 ? (
        <div className="text-xs text-muted-foreground">暂无历史版本。修改保存后会自动生成快照，也可手动保存。</div>
      ) : (
        <div className="space-y-1.5">
          {versions.map((v) => (
            <div key={v.id} className="flex items-center gap-2 rounded-lg bg-quant-bg border border-quant-border px-3 py-2">
              <span className="text-xs font-mono font-bold text-quant-gold">v{v.version}</span>
              <span className="text-xs text-muted-foreground truncate flex-1">{v.note || '（无备注）'}</span>
              <span className="text-[10px] text-muted-foreground shrink-0">
                {new Date(v.created_at).toLocaleString()}
              </span>
              <button onClick={() => handleView(v.version)} title="查看 JSON"
                className="p-1 rounded text-muted-foreground hover:text-foreground transition-colors">
                <Eye className="w-3.5 h-3.5" />
              </button>
              <button onClick={() => handleRestore(v.version)} disabled={restoring === v.version} title="恢复此版本"
                className="p-1 rounded text-muted-foreground hover:text-quant-gold transition-colors disabled:opacity-40">
                <RotateCcw className="w-3.5 h-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
      {viewing && (
        <div className="mt-2 rounded-lg border border-quant-border bg-quant-bg p-3">
          <div className="flex items-center gap-2 mb-2">
            <span className="text-xs font-bold">v{viewing.version} 配置 JSON</span>
            <span className="flex-1" />
            <button onClick={() => setViewing(null)}
              className="text-[10px] text-muted-foreground hover:text-foreground">关闭</button>
          </div>
          <pre className="text-[11px] font-mono whitespace-pre-wrap break-all max-h-72 overflow-y-auto">
            {viewing.json}
          </pre>
        </div>
      )}
    </div>
  )
}
