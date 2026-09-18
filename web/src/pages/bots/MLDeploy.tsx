import { useState, useEffect } from 'react'
import { mlApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { FormField } from '@/components/strategy/StrategyFormFields'
import { toast } from '@/lib/useToast'
import type { MLModelInfo } from '@/types'
import { BrainCircuit, RotateCcw, Zap } from 'lucide-react'

export function MLDeploy() {
  const [strategyName, setStrategyName] = useState('ML_Strategy')
  const [mlModels, setMlModels] = useState<MLModelInfo[]>([])
  const [selectedMlModel, setSelectedMlModel] = useState('')
  const [mlSymbol, setMlSymbol] = useState('BTCUSDT')
  const [mlMinConfidence, setMlMinConfidence] = useState(0.3)
  const [mlDeploying, setMlDeploying] = useState(false)
  const [mlDeployed, setMlDeployed] = useState(false)
  const [mlError, setMlError] = useState<string | null>(null)

  useEffect(() => {
    mlApi
      .list()
      .then((models) => {
        setMlModels(models || [])
        if (models?.length > 0 && !selectedMlModel) setSelectedMlModel(models[0].model_id)
        setMlError(null)
      })
      .catch((e: unknown) => {
        setMlError('ML 服务不可用，请检查 ML Server 是否运行')
        console.error('ML list error:', e)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleDeployMl = async () => {
    if (!selectedMlModel) return
    setMlDeploying(true)
    setMlDeployed(false)
    try {
      await mlApi.deploy({ model_id: selectedMlModel, symbol: mlSymbol, min_confidence: mlMinConfidence })
      toast('success', `ML 策略已部署: ${selectedMlModel} → ${mlSymbol}`)
      setMlDeployed(true)
    } catch (e: unknown) {
      toast('error', '部署失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setMlDeploying(false)
    }
  }

  return (
    <div className="h-full overflow-y-auto p-6 space-y-5 max-w-4xl mx-auto">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <input
            value={strategyName}
            onChange={(e) => setStrategyName(e.target.value)}
            className="bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-sm w-56 focus:outline-none focus:border-quant-gold"
          />
          <span className="px-2 py-1 bg-blue-500/10 text-blue-400 rounded text-[10px] font-medium border border-blue-500/20">
            ML 策略
          </span>
        </div>
        {mlError && (
          <div className="px-3 py-2 rounded-lg bg-quant-red/10 border border-quant-red/20 text-xs text-quant-red mb-2">
            {mlError}
          </div>
        )}
        <button
          onClick={handleDeployMl}
          disabled={mlDeploying || !selectedMlModel || mlDeployed}
          className={cn(
            'px-4 py-2 rounded-lg text-xs font-medium transition-opacity',
            mlDeployed
              ? 'bg-quant-green/20 text-quant-green cursor-default'
              : !selectedMlModel
                ? 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed'
                : mlDeploying
                  ? 'bg-quant-gold/50 text-white cursor-wait'
                  : 'bg-quant-green text-white hover:opacity-90'
          )}
        >
          {mlDeployed ? '已部署 ✓' : mlDeploying ? '部署中...' : '部署 ML 策略'}
        </button>
      </div>

      <SectionCard title="模型配置">
        <div className="space-y-4">
          <FormField label="选择已训练模型">
            {mlModels.length > 0 ? (
              <select
                value={selectedMlModel}
                onChange={(e) => setSelectedMlModel(e.target.value)}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
              >
                {mlModels.map((m: MLModelInfo) => (
                  <option key={m.model_id} value={m.model_id}>
                    {m.model_id} ({m.model_type} · {m.task_type})
                  </option>
                ))}
              </select>
            ) : (
              <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
                暂无已训练模型。请到 <b>AI 研究 → ML 预测 → 训练</b> 先训练一个模型。
              </div>
            )}
          </FormField>
          <div className="grid grid-cols-2 gap-4">
            <FormField label="交易对">
              <input
                value={mlSymbol}
                onChange={(e) => setMlSymbol(e.target.value.toUpperCase())}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
              />
            </FormField>
            <FormField label="最小置信度 (0~1)">
              <input
                type="number"
                step={0.05}
                min={0}
                max={1}
                value={mlMinConfidence}
                onChange={(e) => setMlMinConfidence(Number(e.target.value))}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
              />
            </FormField>
          </div>
        </div>
      </SectionCard>

      <SectionCard title="策略说明">
        <div className="space-y-3 text-xs text-muted-foreground">
          <div className="flex items-start gap-2">
            <BrainCircuit className="h-4 w-4 text-blue-400 mt-0.5 shrink-0" />
            <div>
              <p className="font-medium text-foreground mb-1">自动预测交易</p>
              <p>模型在每个 K 线上预测价格方向，超过置信度阈值时自动开仓，信号反转时平仓，自带 5% 止损。</p>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <RotateCcw className="h-4 w-4 text-quant-gold mt-0.5 shrink-0" />
            <div>
              <p className="font-medium text-foreground mb-1">在线学习</p>
              <p>策略每 24 小时自动用最新数据重训练模型，持续适应市场变化。</p>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <Zap className="h-4 w-4 text-quant-green mt-0.5 shrink-0" />
            <div>
              <p className="font-medium text-foreground mb-1">即插即用</p>
              <p>无需编写代码。在 AI 研究页面训练模型后，在此一键部署为实盘策略。</p>
            </div>
          </div>
        </div>
      </SectionCard>
    </div>
  )
}
