import { useState, useCallback, memo } from 'react'
import { Cpu, Zap, Play, TrendingUp, Loader2, CheckCircle2, Trash2, ArrowUp, ArrowDown, X, BrainCircuit, BarChart3 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { mlApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { RLPanel } from './RLPanel'
import { TensorBoardPanel } from './TensorBoardPanel'
import type { MLModelInfo, MLTrainResult, RLModelInfo } from '@/types'

const MlModelCard = memo(function MlModelCard({
  model,
  onDeploy,
  onDelete,
  isDeploying,
}: {
  model: MLModelInfo
  onDeploy: (id: string) => void
  onDelete: (id: string) => void
  isDeploying: boolean
}) {
  return (
    <div className="flex items-center justify-between p-3 rounded-lg border border-quant-border bg-quant-bg-secondary">
      <div className="min-w-0 flex-1">
        <div className="text-xs font-medium truncate">{model.model_id}</div>
        <div className="text-[10px] text-muted-foreground">
          {model.model_type} · {model.task_type}
          {model.metrics?.test_rmse != null && ` · RMSE: ${Number(model.metrics.test_rmse).toFixed(4)}`}
          {model.metrics?.test_accuracy != null && ` · Acc: ${(Number(model.metrics.test_accuracy)*100).toFixed(1)}%`}
        </div>
      </div>
      <button onClick={() => onDeploy(model.model_id)}
        disabled={isDeploying}
        className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-quant-gold/10 shrink-0 text-[10px]"
        title="部署为交易策略">{isDeploying ? <Loader2 className="h-3 w-3 animate-spin" /> : <Zap className="h-3 w-3" />}</button>
      <button onClick={() => onDelete(model.model_id)}
        className="p-1 rounded text-muted-foreground hover:text-red-400 hover:bg-red-500/10 shrink-0">
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  )
})

export function MLPanel({
  selectedSymbol,
  mlModels,
  loadMlModels,
  rlModels,
  loadRlModels,
}: {
  selectedSymbol: string | undefined
  mlModels: MLModelInfo[]
  loadMlModels: () => void
  rlModels: RLModelInfo[]
  loadRlModels: () => void
}) {
  const [mlTab, setMlTab] = useState<'predict' | 'train' | 'models' | 'rl' | 'tensorboard'>('predict')
  const [mlPredicting, setMlPredicting] = useState(false)
  const [mlTraining, setMlTraining] = useState(false)
  const [mlResult, setMlResult] = useState<{ direction: string; prediction: number; strength: number } | null>(null)
  const [mlError, setMlError] = useState('')
  const [selectedMlModel, setSelectedMlModel] = useState('')
  const [mlModelType, setMlModelType] = useState('lightgbm')
  const [mlTaskType, setMlTaskType] = useState('regression')
  const [mlTrainBars, setMlTrainBars] = useState(500)
  const [mlHorizon, setMlHorizon] = useState(5)
  const [mlTrainResult, setMlTrainResult] = useState<MLTrainResult | null>(null)
  const [mlDeploying, setMlDeploying] = useState('')

  const deployMlStrategy = useCallback(async (modelId: string) => {
    setMlDeploying(modelId)
    try {
      const symbol = (selectedSymbol?.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol) || 'BTCUSDT'
      await mlApi.deploy({ model_id: modelId, symbol, min_confidence: 0.3 })
      toast('success', `ML 策略已部署: ${modelId} → ${symbol}`)
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); toast('error', '部署失败: ' + (err.message || err)) }
    finally { setMlDeploying('') }
  }, [selectedSymbol])

  const runMlPredict = useCallback(async () => {
    if (!selectedSymbol || !selectedMlModel) return
    setMlPredicting(true); setMlError(''); setMlResult(null)
    try {
      const symbol = selectedSymbol.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol
      const { marketApi } = await import('@/lib/api')
      const bars = await marketApi.klines(symbol, '1h', 200)
      const result = await mlApi.predict({ model_id: selectedMlModel, bars })
      setMlResult({ direction: result.prediction > 0 ? 'up' : 'down', prediction: result.prediction, strength: result.confidence })
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setMlError(err.message || 'ML 预测失败') }
    finally { setMlPredicting(false) }
  }, [selectedSymbol, selectedMlModel])

  const runMlTrain = useCallback(async () => {
    if (!selectedSymbol) return
    setMlTraining(true); setMlError(''); setMlTrainResult(null)
    try {
      const symbol = selectedSymbol.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol
      const { marketApi } = await import('@/lib/api')
      const bars = await marketApi.klines(symbol, '1h', mlTrainBars)
      const result = await mlApi.train({
        model_id: `${mlModelType}_${symbol}_${Date.now()}`,
        model_type: mlModelType, task_type: mlTaskType,
        symbol, interval: '1h', bars,
        label_config: { horizon: mlHorizon, label_type: mlTaskType },
      })
      setMlTrainResult(result)
      loadMlModels()
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setMlError(err.message || '训练失败') }
    finally { setMlTraining(false) }
  }, [selectedSymbol, mlModelType, mlTaskType, mlTrainBars, mlHorizon, loadMlModels])

  return (
    <div className="space-y-4">
      {/* Sub-tabs */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit flex-wrap">
        {[
          { k: 'predict' as const, label: '预测', icon: TrendingUp },
          { k: 'train' as const, label: '训练', icon: Play },
          { k: 'models' as const, label: `模型 (${mlModels.length})`, icon: Cpu },
          { k: 'rl' as const, label: 'RL', icon: BrainCircuit },
          { k: 'tensorboard' as const, label: 'TensorBoard', icon: BarChart3 },
        ].map(t => (
          <button key={t.k} onClick={() => setMlTab(t.k)}
            className={cn('flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium transition-colors',
              mlTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
            <t.icon className="h-3 w-3" />{t.label}
          </button>
        ))}
      </div>

      {/* Error banner */}
      {mlError && (
        <div role="alert" className="flex items-start gap-2 text-xs text-red-400 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
          <X className="h-3.5 w-3.5 mt-0.5 shrink-0" /> {mlError}
        </div>
      )}

      {/* Predict Tab */}
      {mlTab === 'predict' && (
        <div className="space-y-3">
          {mlModels.length > 0 ? (
            <select value={selectedMlModel} onChange={e => setSelectedMlModel(e.target.value)}
              className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-xs outline-none focus:border-quant-gold">
              {mlModels.map((m: MLModelInfo) => (
                <option key={m.model_id} value={m.model_id}>{m.model_id} ({m.model_type} · {m.task_type})</option>
              ))}
            </select>
          ) : (
            <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
              暂无已训练模型，请先到「训练」标签训练模型
            </div>
          )}
          <button onClick={runMlPredict} disabled={mlPredicting || !selectedMlModel || !selectedSymbol}
            className={cn('w-full flex items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium',
              !selectedMlModel ? 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed' :
              mlPredicting ? 'bg-quant-gold/50 cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
            {mlPredicting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Zap className="h-4 w-4" />}
            {mlPredicting ? '预测中...' : '生成预测'}
          </button>
          {mlResult && (
            <div className={cn('flex items-center gap-3 p-4 rounded-xl border',
              mlResult.direction === 'up' ? 'bg-quant-green/5 border-quant-green/20' : 'bg-quant-red/5 border-quant-red/20')}>
              <div className={cn('w-10 h-10 rounded-full flex items-center justify-center',
                mlResult.direction === 'up' ? 'bg-quant-green/10' : 'bg-quant-red/10')}>
                {mlResult.direction === 'up'
                  ? <ArrowUp className="h-5 w-5 text-quant-green" />
                  : <ArrowDown className="h-5 w-5 text-quant-red" />}
              </div>
              <div>
                <div className={cn('text-base font-bold', mlResult.direction === 'up' ? 'text-quant-green' : 'text-quant-red')}>
                  {mlResult.direction === 'up' ? '做多 LONG' : '做空 SHORT'}
                </div>
                <div className="text-[11px] text-muted-foreground">
                  预测值: {mlResult.prediction?.toFixed(6)} · 强度: {(mlResult.strength * 100).toFixed(0)}%
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Train Tab */}
      {mlTab === 'train' && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">模型</label>
              <select value={mlModelType} onChange={e => setMlModelType(e.target.value)}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                <option value="lightgbm">LightGBM</option>
                <option value="xgboost">XGBoost</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">任务</label>
              <select value={mlTaskType} onChange={e => setMlTaskType(e.target.value)}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                <option value="regression">回归 (收益率)</option>
                <option value="classification">分类 (涨跌)</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">K线数</label>
              <input type="number" min={100} max={2000} value={mlTrainBars}
                onChange={e => setMlTrainBars(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" aria-label="K线数" />
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">预测周期</label>
              <input type="number" min={1} max={50} value={mlHorizon}
                onChange={e => setMlHorizon(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" aria-label="预测周期" />
            </div>
          </div>
          <button onClick={runMlTrain} disabled={mlTraining || !selectedSymbol}
            className={cn('w-full flex items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium',
              mlTraining ? 'bg-quant-gold/50 cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
            {mlTraining ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
            {mlTraining ? '训练中...' : '开始训练'}
          </button>
          {mlTrainResult && (
            <div className="text-xs space-y-1 p-3 bg-quant-green/5 border border-quant-green/20 rounded-lg">
              <div className="flex items-center gap-1 text-quant-green font-medium">
                <CheckCircle2 className="h-3 w-3" /> 训练完成
              </div>
              <div className="text-muted-foreground">
                特征: {mlTrainResult.feature_count} · 训练样本: {mlTrainResult.train_samples} · 测试样本: {mlTrainResult.test_samples}
              </div>
              {mlTrainResult.metrics && Object.entries(mlTrainResult.metrics as Record<string,number>)
                .filter(([k]) => k.startsWith('test_')).slice(0, 3)
                .map(([k, v]) => (
                  <div key={k} className="text-muted-foreground">
                    {k.replace('test_', '')}: {typeof v === 'number' ? v.toFixed(4) : String(v)}
                  </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Models Tab */}
      {mlTab === 'models' && (
        <div className="space-y-2">
          {mlModels.map((m) => (
            <MlModelCard
              key={m.model_id}
              model={m}
              onDeploy={deployMlStrategy}
              onDelete={async (id) => { await mlApi.deleteModel(id); loadMlModels() }}
              isDeploying={mlDeploying === m.model_id}
            />
          ))}
        </div>
      )}

      {/* RL Tab */}
      {mlTab === 'rl' && (
        <RLPanel selectedSymbol={selectedSymbol} rlModels={rlModels} loadRlModels={loadRlModels} />
      )}

      {/* TensorBoard Tab */}
      {mlTab === 'tensorboard' && (
        <TensorBoardPanel />
      )}
    </div>
  )
}
