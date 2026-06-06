import { useState, useEffect, useCallback, memo } from 'react'
import { BrainCircuit, Play, Loader2, CheckCircle2, Trash2, Zap, TrendingUp, TrendingDown, Minus, BarChart3, X, Clock, AlertCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { rlApi, marketApi } from '@/lib/api'
import type { RLTrainResult, RLModelInfo, RLPredictResult, RLJob } from '@/types'

const RLModelCard = memo(function RLModelCard({
  model,
  onDelete,
  onPredict,
  isPredicting,
}: {
  model: RLModelInfo
  onDelete: (id: string) => void
  onPredict: (id: string) => void
  isPredicting: boolean
}) {
  return (
    <div className="flex items-center justify-between p-3 rounded-lg border border-quant-border bg-quant-bg-secondary">
      <div className="min-w-0 flex-1">
        <div className="text-xs font-medium truncate">{model.model_id}</div>
        <div className="text-[10px] text-muted-foreground">
          {model.model_type} · {model.task_type}
          {model.metrics?.total_pnl != null && ` · PnL: ${Number(model.metrics.total_pnl).toFixed(2)}`}
          {model.metrics?.best_reward != null && ` · Best: ${Number(model.metrics.best_reward).toFixed(2)}`}
        </div>
      </div>
      <button onClick={() => onPredict(model.model_id)}
        disabled={isPredicting}
        className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-quant-gold/10 shrink-0 text-[10px]"
        title="预测">
        {isPredicting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Zap className="h-3 w-3" />}
      </button>
      <button onClick={() => onDelete(model.model_id)}
        className="p-1 rounded text-muted-foreground hover:text-red-400 hover:bg-red-500/10 shrink-0">
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  )
})

export function RLPanel({
  selectedSymbol,
  rlModels,
  loadRlModels,
}: {
  selectedSymbol: string | undefined
  rlModels: RLModelInfo[]
  loadRlModels: () => void
}) {
  const [rlTab, setRlTab] = useState<'predict' | 'train' | 'models'>('train')
  const [rlTraining, setRlTraining] = useState(false)
  const [rlPredicting, setRlPredicting] = useState('')
  const [rlResult, setRlResult] = useState<RLPredictResult | null>(null)
  const [rlError, setRlError] = useState('')
  const [rlTrainResult, setRlTrainResult] = useState<RLTrainResult | null>(null)
  const [selectedRlModel, setSelectedRlModel] = useState('')

  // Async job states
  const [activeJob, setActiveJob] = useState<RLJob | null>(null)
  const [jobId, setJobId] = useState<string | null>(null)

  // Training config
  const [algorithm, setAlgorithm] = useState<'qlearning' | 'ppo' | 'a2c' | 'sac'>('qlearning')
  const [nActions, setNActions] = useState(3)
  const [episodes, setEpisodes] = useState(100)
  const [lookbackDays, setLookbackDays] = useState(90)
  const [learningRate, setLearningRate] = useState(0.01)
  const [discount, setDiscount] = useState(0.99)
  const [useTensorBoard, setUseTensorBoard] = useState(true)

  // Poll async job status
  useEffect(() => {
    if (!jobId || !activeJob || activeJob.status === 'completed' || activeJob.status === 'failed' || activeJob.status === 'cancelled') {
      return
    }

    const interval = setInterval(async () => {
      try {
        const job = await rlApi.getJob(jobId)
        setActiveJob(job)
        if (job.status === 'completed' && job.result) {
          setRlTrainResult(job.result)
          setRlTraining(false)
          loadRlModels()
          clearInterval(interval)
        } else if (job.status === 'failed') {
          setRlError(job.error || '训练失败')
          setRlTraining(false)
          clearInterval(interval)
        }
      } catch {
        // ignore polling errors
      }
    }, 2000)

    return () => clearInterval(interval)
  }, [jobId, activeJob, loadRlModels])

  const isAdvancedAlgorithm = (algo: string) => ['ppo', 'a2c', 'sac'].includes(algo)

  const runRlTrain = useCallback(async () => {
    if (!selectedSymbol) return
    setRlTraining(true); setRlError(''); setRlTrainResult(null); setActiveJob(null); setJobId(null)
    try {
      const symbol = selectedSymbol.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol
      const bars = await marketApi.klines(symbol, '1h', lookbackDays * 24)
      const result = await rlApi.train({
        algorithm,
        n_actions: nActions,
        symbol,
        interval: '1h',
        bars,
        episodes,
        learning_rate: learningRate,
        discount,
        use_tensorboard: useTensorBoard,
      })

      // Check if async job was returned
      if ('job_id' in result && result.job_id) {
        setJobId(result.job_id)
        setActiveJob({
          id: result.job_id,
          status: 'pending',
          algorithm,
          n_actions: nActions,
          symbol,
          interval: '1h',
          created_at: new Date().toISOString(),
        } as RLJob)
        // Keep rlTraining true for async jobs
      } else {
        // Sync result (Q-Learning)
        setRlTrainResult(result as RLTrainResult)
        setRlTraining(false)
        loadRlModels()
      }
    } catch (e: unknown) { 
      const err = e instanceof Error ? e : new Error(String(e))
      setRlError(err.message || 'RL 训练失败')
      setRlTraining(false)
    }
  }, [selectedSymbol, algorithm, nActions, episodes, lookbackDays, learningRate, discount, useTensorBoard, loadRlModels])

  const cancelJob = useCallback(async () => {
    if (!jobId) return
    try {
      await rlApi.cancelJob(jobId)
      setRlTraining(false)
      setActiveJob(null)
      setJobId(null)
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      setRlError(err.message || '取消失败')
    }
  }, [jobId])

  const runRlPredict = useCallback(async (modelId: string) => {
    if (!selectedSymbol) return
    setRlPredicting(modelId); setRlError(''); setRlResult(null)
    try {
      const symbol = selectedSymbol.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol
      const bars = await marketApi.klines(symbol, '1h', 200)
      const result = await rlApi.predict({ model_id: modelId, bars })
      setRlResult(result)
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setRlError(err.message || 'RL 预测失败') }
    finally { setRlPredicting('') }
  }, [selectedSymbol])

  const actionIcon = (action: number, nAct: number) => {
    if (nAct === 3) {
      if (action === 0) return <TrendingDown className="h-5 w-5 text-quant-red" />
      if (action === 2) return <TrendingUp className="h-5 w-5 text-quant-green" />
      return <Minus className="h-5 w-5 text-muted-foreground" />
    }
    if (action <= 1) return <TrendingDown className="h-5 w-5 text-quant-red" />
    if (action >= 3) return <TrendingUp className="h-5 w-5 text-quant-green" />
    return <Minus className="h-5 w-5 text-muted-foreground" />
  }

  // Progress bar for async jobs
  const progressPct = activeJob?.progress
    ? Math.min(100, (activeJob.progress.current_episode / Math.max(1, activeJob.progress.total_episodes)) * 100)
    : 0

  return (
    <div className="space-y-4">
      {/* Sub-tabs */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
        {[
          { k: 'train' as const, label: '训练', icon: Play },
          { k: 'predict' as const, label: '预测', icon: BrainCircuit },
          { k: 'models' as const, label: `模型 (${rlModels.length})`, icon: BarChart3 },
        ].map(t => (
          <button key={t.k} onClick={() => setRlTab(t.k)}
            className={cn('flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium transition-colors',
              rlTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
            <t.icon className="h-3 w-3" />{t.label}
          </button>
        ))}
      </div>

      {/* Error banner */}
      {rlError && (
        <div role="alert" className="flex items-start gap-2 text-xs text-red-400 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
          <X className="h-3.5 w-3.5 mt-0.5 shrink-0" /> {rlError}
        </div>
      )}

      {/* Train Tab */}
      {rlTab === 'train' && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">算法</label>
              <select value={algorithm} onChange={e => setAlgorithm(e.target.value as any)}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                <option value="qlearning">Q-Learning (同步)</option>
                <option value="ppo">PPO (异步Worker)</option>
                <option value="a2c">A2C (异步Worker)</option>
                <option value="sac">SAC (异步Worker)</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">动作空间</label>
              <select value={nActions} onChange={e => setNActions(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                <option value={3}>3动作 (短/中/长)</option>
                <option value={5}>5动作 (全短/半短/中/半长/全长)</option>
              </select>
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">回合数</label>
              <input type="number" min={10} max={1000} value={episodes}
                onChange={e => setEpisodes(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">回看天数</label>
              <input type="number" min={7} max={365} value={lookbackDays}
                onChange={e => setLookbackDays(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">学习率</label>
              <input type="number" step={0.001} min={0.0001} max={1} value={learningRate}
                onChange={e => setLearningRate(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
            </div>
            <div>
              <label className="text-[10px] text-muted-foreground mb-0.5 block">折扣因子</label>
              <input type="number" step={0.01} min={0.5} max={1} value={discount}
                onChange={e => setDiscount(Number(e.target.value))}
                className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
            </div>
          </div>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input type="checkbox" checked={useTensorBoard} onChange={e => setUseTensorBoard(e.target.checked)}
              className="rounded border-quant-border" />
            启用 TensorBoard 日志记录
          </label>

          {/* Algorithm mode indicator */}
          {isAdvancedAlgorithm(algorithm) && (
            <div className="flex items-center gap-2 text-xs text-quant-gold bg-quant-gold/5 p-2 rounded-lg border border-quant-gold/20">
              <AlertCircle className="h-3.5 w-3.5" />
              {algorithm.toUpperCase()} 将通过独立 Python Worker 进程异步训练，通过 Redis 队列通信
            </div>
          )}

          <button onClick={runRlTrain} disabled={rlTraining}
            className={cn('w-full flex items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium',
              rlTraining ? 'bg-quant-gold/50 cursor-wait' : !selectedSymbol ? 'bg-quant-gold/30 cursor-not-allowed' : 'bg-quant-gold text-white hover:opacity-90')}
            title={!selectedSymbol ? '请先选择一个交易对' : undefined}>
            {rlTraining ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
            {rlTraining
              ? (isAdvancedAlgorithm(algorithm) && activeJob?.status === 'pending'
                ? '已提交到 Worker 队列...'
                : 'RL 训练中...')
              : !selectedSymbol ? '请先选择交易对' : `开始 ${algorithm.toUpperCase()} 训练`}
          </button>

          {/* Async job status */}
          {rlTraining && isAdvancedAlgorithm(algorithm) && activeJob && (
            <div className="space-y-2 p-3 bg-quant-bg-secondary border border-quant-border rounded-lg">
              <div className="flex items-center justify-between">
                <div className="text-xs font-medium flex items-center gap-1.5">
                  <Clock className="h-3 w-3 text-quant-gold" />
                  任务: {activeJob.id}
                </div>
                <span className={cn('text-[10px] px-1.5 py-0.5 rounded',
                  activeJob.status === 'pending' ? 'bg-yellow-500/10 text-yellow-500' :
                  activeJob.status === 'running' ? 'bg-quant-green/10 text-quant-green' :
                  'bg-quant-gold/10 text-quant-gold')}>
                  {activeJob.status === 'pending' ? '队列中' :
                   activeJob.status === 'running' ? '训练中' : activeJob.status}
                </span>
              </div>

              {/* Progress bar */}
              {activeJob.progress && (
                <>
                  <div className="w-full bg-quant-bg-tertiary rounded-full h-1.5">
                    <div className="bg-quant-gold h-1.5 rounded-full transition-all" style={{ width: `${progressPct}%` }} />
                  </div>
                  <div className="text-[10px] text-muted-foreground">
                    回合 {activeJob.progress.current_episode} / {activeJob.progress.total_episodes}
                    {activeJob.progress.best_reward !== 0 && ` · 最佳奖励: ${activeJob.progress.best_reward.toFixed(2)}`}
                    {activeJob.progress.current_balance !== 0 && ` · 余额: ${activeJob.progress.current_balance.toFixed(0)}`}
                  </div>
                </>
              )}

              <button onClick={cancelJob}
                className="text-[10px] text-red-400 hover:text-red-300 flex items-center gap-1">
                <X className="h-3 w-3" /> 取消任务
              </button>
            </div>
          )}

          {/* Sync result */}
          {rlTrainResult && !isAdvancedAlgorithm(algorithm) && (
            <div className="text-xs space-y-1 p-3 bg-quant-green/5 border border-quant-green/20 rounded-lg">
              <div className="flex items-center gap-1 text-quant-green font-medium">
                <CheckCircle2 className="h-3 w-3" /> 训练完成
              </div>
              <div className="text-muted-foreground">
                模型: {rlTrainResult.model_id} · 算法: {rlTrainResult.algorithm}
              </div>
              <div className="text-muted-foreground">
                最终余额: {rlTrainResult.final_balance?.toFixed(2)} · 总PnL: {rlTrainResult.total_pnl?.toFixed(2)}
              </div>
              <div className="text-muted-foreground">
                最佳奖励: {rlTrainResult.best_reward?.toFixed(2)} · 近10均: {rlTrainResult.avg_reward_last_10?.toFixed(2)}
              </div>
              {rlTrainResult.q_table_size != null && (
                <div className="text-muted-foreground">Q表大小: {rlTrainResult.q_table_size}</div>
              )}
            </div>
          )}

          {/* Async completed result */}
          {rlTrainResult && isAdvancedAlgorithm(algorithm) && activeJob?.status === 'completed' && (
            <div className="text-xs space-y-1 p-3 bg-quant-green/5 border border-quant-green/20 rounded-lg">
              <div className="flex items-center gap-1 text-quant-green font-medium">
                <CheckCircle2 className="h-3 w-3" /> Worker 训练完成
              </div>
              <div className="text-muted-foreground">
                算法: {rlTrainResult.algorithm} · 动作空间: {rlTrainResult.n_actions}
              </div>
              <div className="text-muted-foreground">
                最终余额: {rlTrainResult.final_balance?.toFixed(2)} · 总PnL: {rlTrainResult.total_pnl?.toFixed(2)}
              </div>
              <div className="text-muted-foreground">
                最佳奖励: {rlTrainResult.best_reward?.toFixed(2)}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Predict Tab */}
      {rlTab === 'predict' && (
        <div className="space-y-3">
          {rlModels.length > 0 ? (
            <select value={selectedRlModel} onChange={e => setSelectedRlModel(e.target.value)}
              className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-xs outline-none focus:border-quant-gold">
              {rlModels.map((m: RLModelInfo) => (
                <option key={m.model_id} value={m.model_id}>{m.model_id} ({m.model_type})</option>
              ))}
            </select>
          ) : (
            <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
              暂无已训练RL模型，请先到「训练」标签训练模型
            </div>
          )}
          <button onClick={() => selectedRlModel && runRlPredict(selectedRlModel)}
            disabled={rlPredicting !== '' || !selectedRlModel || !selectedSymbol}
            className={cn('w-full flex items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium',
              !selectedRlModel ? 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed' :
              rlPredicting ? 'bg-quant-gold/50 cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
            {rlPredicting ? <Loader2 className="h-4 w-4 animate-spin" /> : <BrainCircuit className="h-4 w-4" />}
            {rlPredicting ? '预测中...' : 'RL 预测'}
          </button>
          {rlResult && (
            <div className={cn('flex items-center gap-3 p-4 rounded-xl border',
              rlResult.position > 0 ? 'bg-quant-green/5 border-quant-green/20' :
              rlResult.position < 0 ? 'bg-quant-red/5 border-quant-red/20' :
              'bg-quant-bg-secondary border-quant-border')}>
              <div className={cn('w-10 h-10 rounded-full flex items-center justify-center',
                rlResult.position > 0 ? 'bg-quant-green/10' :
                rlResult.position < 0 ? 'bg-quant-red/10' : 'bg-quant-bg-tertiary')}>
                {actionIcon(rlResult.action, rlResult.action >= 3 ? 5 : 3)}
              </div>
              <div>
                <div className={cn('text-base font-bold',
                  rlResult.position > 0 ? 'text-quant-green' :
                  rlResult.position < 0 ? 'text-quant-red' : 'text-muted-foreground')}>
                  {rlResult.action_name}
                </div>
                <div className="text-[11px] text-muted-foreground">
                  动作: {rlResult.action} · 置信度: {(rlResult.confidence * 100).toFixed(0)}% · 仓位: {rlResult.position.toFixed(1)}
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Models Tab */}
      {rlTab === 'models' && (
        <div className="space-y-2">
          {rlModels.map((m) => (
            <RLModelCard
              key={m.model_id}
              model={m}
              onDelete={async (id) => { await rlApi.deleteModel(id); loadRlModels() }}
              onPredict={runRlPredict}
              isPredicting={rlPredicting === m.model_id}
            />
          ))}
        </div>
      )}
    </div>
  )
}
