import React, { useState, useEffect, useCallback } from 'react';
import { mlApi, type MLLoopStatus, type MLTrainingRun, type MLRetrainJob } from '@/lib/api';
import { SectionCard } from '@/components/ui/SectionCard';
import { useToastStore } from '@/stores/toastStore';
import { cn } from '@/lib/utils';
import {
  Play,
  Activity,
  Server,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Loader2,
  RefreshCw,
  Clock,
  Plus,
  Zap,
} from 'lucide-react';

/* ── 训练闭环面板：闭环开关/周期配置/训练历史/漂移状态灯 ──
   链路：retrainer 调度 → 训练（python / Go 降级）→ 导出 → 热加载 → 落档。
   数据来源：GET /ml/loop-status + GET /ml/training-runs + /ml/retrain-jobs。 */

function StatusDot({ ok, warn }: { ok: boolean; warn?: boolean }) {
  return (
    <span
      className={cn(
        'inline-block w-2 h-2 rounded-full',
        ok ? 'bg-green-400' : warn ? 'bg-amber-400' : 'bg-red-400'
      )}
    />
  );
}

function fmtTs(ms?: number) {
  if (!ms) return '-';
  return new Date(ms).toLocaleString();
}

function fmtInterval(min: number) {
  if (min >= 60 && min % 60 === 0) return `${min / 60}h`;
  return `${min}m`;
}

export function MLTrainingLoopCard() {
  const [status, setStatus] = useState<MLLoopStatus | null>(null);
  const [runs, setRuns] = useState<MLTrainingRun[]>([]);
  const [loading, setLoading] = useState(false);
  const [runningJobId, setRunningJobId] = useState<number | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [newJob, setNewJob] = useState({ model_name: '', symbol: 'BTCUSDT', interval: '1h', hours: 24 });
  const addToast = useToastStore((state) => state.addToast);

  const fetchAll = useCallback(async () => {
    try {
      const [s, r] = await Promise.all([mlApi.loopStatus(), mlApi.trainingRuns(20)]);
      setStatus(s);
      setRuns(r);
    } catch (err) {
      console.error('Failed to fetch ML loop status:', err);
    }
  }, []);

  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, 15000);
    return () => clearInterval(interval);
  }, [fetchAll]);

  const handleToggleJob = async (job: MLLoopStatus['jobs'][number]) => {
    try {
      await mlApi.updateRetrainJob(job.id, { active: !job.active });
      addToast({ type: 'success', message: `${job.model_name} 已${job.active ? '暂停' : '启用'}` });
      fetchAll();
    } catch (err: unknown) {
      addToast({ type: 'error', message: '操作失败: ' + (err instanceof Error ? err.message : String(err)) });
    }
  };

  const handleIntervalChange = async (job: MLLoopStatus['jobs'][number], hours: number) => {
    if (!hours || hours <= 0) return;
    try {
      await mlApi.updateRetrainJob(job.id, { interval_minutes: Math.round(hours * 60) });
      addToast({ type: 'success', message: `${job.model_name} 周期已改为 ${hours}h` });
      fetchAll();
    } catch (err: unknown) {
      addToast({ type: 'error', message: '修改失败: ' + (err instanceof Error ? err.message : String(err)) });
    }
  };

  const handleRunNow = async (job: MLLoopStatus['jobs'][number]) => {
    setRunningJobId(job.id);
    try {
      const res = await mlApi.runRetrainJob(job.id);
      if (res.run?.status === 'success') {
        addToast({ type: 'success', message: `${job.model_name} 训练完成` });
      } else {
        addToast({ type: 'error', message: `${job.model_name} 训练失败: ${res.run?.error || '未知错误'}` });
      }
      fetchAll();
    } catch (err: unknown) {
      addToast({ type: 'error', message: '训练失败: ' + (err instanceof Error ? err.message : String(err)) });
    } finally {
      setRunningJobId(null);
    }
  };

  const handleCreateJob = async () => {
    if (!newJob.model_name.trim()) {
      addToast({ type: 'error', message: '请填写模型名' });
      return;
    }
    setLoading(true);
    try {
      await mlApi.createRetrainJob({
        model_name: newJob.model_name.trim(),
        interval_minutes: Math.max(1, Math.round(newJob.hours * 60)),
        feature_set: { symbol: newJob.symbol, interval: newJob.interval, lookback_days: 90 },
      });
      addToast({ type: 'success', message: `闭环任务 ${newJob.model_name} 已创建` });
      setShowCreate(false);
      setNewJob({ ...newJob, model_name: '' });
      fetchAll();
    } catch (err: unknown) {
      addToast({ type: 'error', message: '创建失败: ' + (err instanceof Error ? err.message : String(err)) });
    } finally {
      setLoading(false);
    }
  };

  const serverOk = !!status?.ml_server?.healthy || !!status?.ml_server?.reachable;
  const driftedModels = (status?.drift ?? []).filter((d) => d.drifted);
  const fallbackGo = status?.retrainer?.fallback_mode === 'go';

  return (
    <SectionCard
      title={
        <div className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-quant-gold" />
          <span>训练闭环</span>
          <span className="text-[10px] font-normal text-muted-foreground">
            训练 → 导出 → 热加载 → 预测 → 漂移监控 → 自动重训
          </span>
        </div>
      }
    >
      <div className="space-y-4">
        {/* 状态行：ml_server / 调度器 / 漂移灯 */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-xs">
          <div className="flex items-center gap-2 p-2.5 rounded-md bg-quant-bg-secondary">
            <Server className="w-3.5 h-3.5 text-muted-foreground" />
            <StatusDot ok={serverOk} warn={!status} />
            <span className="text-foreground">ML Server</span>
            <span className="text-muted-foreground ml-auto">
              {status?.ml_server?.managed
                ? `托管 pid=${status.ml_server.pid ?? '-'} 重启${status.ml_server.restarts ?? 0}次`
                : serverOk
                  ? '外部实例'
                  : '不可达'}
            </span>
          </div>
          <div className="flex items-center gap-2 p-2.5 rounded-md bg-quant-bg-secondary">
            <RefreshCw className="w-3.5 h-3.5 text-muted-foreground" />
            <StatusDot ok={!!status?.retrainer?.running} warn={!status?.retrainer?.enabled} />
            <span className="text-foreground">自动重训调度</span>
            <span className="text-muted-foreground ml-auto">
              {status?.retrainer?.running
                ? `${status.retrainer.enabled ? '启用' : '停用'}${fallbackGo ? ' · Go降级' : ''}`
                : '未运行'}
            </span>
          </div>
          <div className="flex items-center gap-2 p-2.5 rounded-md bg-quant-bg-secondary">
            <Zap className="w-3.5 h-3.5 text-muted-foreground" />
            <StatusDot ok={driftedModels.length === 0} warn={driftedModels.length > 0} />
            <span className="text-foreground">漂移监控</span>
            <span className="text-muted-foreground ml-auto">
              {driftedModels.length > 0 ? `${driftedModels.length} 个模型漂移` : '无漂移'}
            </span>
          </div>
        </div>

        {/* 任务列表：开关 + 周期 + 立即训练 */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <h3 className="text-sm font-medium text-foreground">闭环任务</h3>
            <button
              onClick={() => setShowCreate(!showCreate)}
              className="flex items-center gap-1 px-2.5 py-1 rounded-md bg-quant-gold/10 text-quant-gold text-xs hover:bg-quant-gold/20 transition-colors"
            >
              <Plus className="w-3 h-3" />
              新建任务
            </button>
          </div>

          {showCreate && (
            <div className="flex flex-wrap items-end gap-2 mb-3 p-3 rounded-md border border-white/5 bg-quant-bg-secondary">
              <div className="space-y-1">
                <label className="text-[10px] text-muted-foreground">模型名</label>
                <input
                  type="text"
                  value={newJob.model_name}
                  onChange={(e) => setNewJob({ ...newJob, model_name: e.target.value })}
                  placeholder="BTCUSDT_1h"
                  className="px-2 py-1.5 rounded-md bg-quant-bg border border-white/10 text-xs text-foreground w-36"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[10px] text-muted-foreground">交易对</label>
                <input
                  type="text"
                  value={newJob.symbol}
                  onChange={(e) => setNewJob({ ...newJob, symbol: e.target.value.toUpperCase() })}
                  className="px-2 py-1.5 rounded-md bg-quant-bg border border-white/10 text-xs text-foreground w-28"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[10px] text-muted-foreground">周期（小时）</label>
                <input
                  type="number"
                  min={1}
                  value={newJob.hours}
                  onChange={(e) => setNewJob({ ...newJob, hours: Number(e.target.value) })}
                  className="px-2 py-1.5 rounded-md bg-quant-bg border border-white/10 text-xs text-foreground w-20"
                />
              </div>
              <button
                onClick={handleCreateJob}
                disabled={loading}
                className="px-3 py-1.5 rounded-md bg-quant-gold text-white text-xs font-medium hover:bg-quant-gold/90 disabled:opacity-50"
              >
                {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : '创建'}
              </button>
            </div>
          )}

          {(status?.jobs ?? []).length === 0 ? (
            <div className="text-xs text-muted-foreground py-3 text-center">
              暂无闭环任务 — 新建后按周期自动重训，漂移时提前触发
            </div>
          ) : (
            <div className="space-y-1.5">
              {(status?.jobs ?? []).map((job) => (
                <div
                  key={job.id}
                  className="flex flex-wrap items-center gap-2 p-2.5 rounded-md border border-white/5 hover:border-white/10 transition-colors text-xs"
                >
                  <StatusDot ok={job.active && job.last_status !== 'failed'} warn={job.last_status === 'failed'} />
                  <span className="font-medium text-foreground">{job.model_name}</span>
                  <span className="text-muted-foreground">
                    上次 {job.last_run_at ? fmtTs(job.last_run_at) : '从未'}
                  </span>
                  <span className="text-muted-foreground hidden md:inline">
                    下次 {job.active ? fmtTs(job.next_run_at) : '已暂停'}
                  </span>
                  <div className="ml-auto flex items-center gap-1.5">
                    <input
                      type="number"
                      min={1}
                      defaultValue={Math.round(job.interval_minutes / 60)}
                      key={`${job.id}-${job.interval_minutes}`}
                      onBlur={(e) => {
                        const h = Number(e.target.value);
                        if (h > 0 && h * 60 !== job.interval_minutes) handleIntervalChange(job, h);
                      }}
                      className="w-14 px-1.5 py-1 rounded-md bg-quant-bg border border-white/10 text-[11px] text-foreground text-right"
                      title="重训周期（小时）"
                    />
                    <span className="text-[10px] text-muted-foreground">h</span>
                    <button
                      onClick={() => handleRunNow(job)}
                      disabled={runningJobId === job.id}
                      className="flex items-center gap-1 px-2 py-1 rounded-md bg-quant-gold/10 text-quant-gold hover:bg-quant-gold/20 transition-colors disabled:opacity-50"
                    >
                      {runningJobId === job.id ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3" />}
                      训练
                    </button>
                    <button
                      onClick={() => handleToggleJob(job)}
                      className={cn(
                        'px-2 py-1 rounded-md transition-colors',
                        job.active
                          ? 'bg-green-500/10 text-green-400 hover:bg-green-500/20'
                          : 'bg-white/5 text-muted-foreground hover:bg-white/10'
                      )}
                    >
                      {job.active ? '运行中' : '已暂停'}
                    </button>
                  </div>
                  {job.last_status === 'failed' && job.last_error && (
                    <div className="w-full text-[10px] text-red-400/80 truncate" title={job.last_error}>
                      <AlertCircle className="w-3 h-3 inline mr-1" />
                      {job.last_error}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 训练历史表格 */}
        <div>
          <h3 className="text-sm font-medium text-foreground mb-2">训练历史</h3>
          {runs.length === 0 ? (
            <div className="text-xs text-muted-foreground py-3 text-center">暂无训练记录</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-left text-muted-foreground border-b border-white/5">
                    <th className="py-1.5 pr-3 font-medium">时间</th>
                    <th className="py-1.5 pr-3 font-medium">模型</th>
                    <th className="py-1.5 pr-3 font-medium">触发</th>
                    <th className="py-1.5 pr-3 font-medium">后端</th>
                    <th className="py-1.5 pr-3 font-medium">样本</th>
                    <th className="py-1.5 pr-3 font-medium">特征</th>
                    <th className="py-1.5 pr-3 font-medium">耗时</th>
                    <th className="py-1.5 pr-3 font-medium">版本</th>
                    <th className="py-1.5 font-medium">状态</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map((run) => (
                    <tr key={run.id} className="border-b border-white/5 last:border-0">
                      <td className="py-1.5 pr-3 text-muted-foreground whitespace-nowrap">{fmtTs(run.created_at)}</td>
                      <td className="py-1.5 pr-3 text-foreground">{run.model_name}</td>
                      <td className="py-1.5 pr-3 text-muted-foreground">{run.trigger || '-'}</td>
                      <td className="py-1.5 pr-3">
                        <span
                          className={cn(
                            'px-1.5 py-0.5 rounded text-[10px]',
                            run.trainer === 'go_fallback' ? 'bg-amber-500/10 text-amber-400' : 'bg-blue-500/10 text-blue-400'
                          )}
                        >
                          {run.trainer}
                        </span>
                      </td>
                      <td className="py-1.5 pr-3 text-muted-foreground">{run.train_samples}</td>
                      <td className="py-1.5 pr-3 text-muted-foreground">{run.feature_count}</td>
                      <td className="py-1.5 pr-3 text-muted-foreground">{(run.duration_ms / 1000).toFixed(1)}s</td>
                      <td className="py-1.5 pr-3 text-muted-foreground font-mono text-[10px]">
                        {run.model_version ? run.model_version.slice(0, 8) : '-'}
                      </td>
                      <td className="py-1.5">
                        {run.status === 'success' ? (
                          <CheckCircle2 className="w-3.5 h-3.5 text-green-400" />
                        ) : run.status === 'skipped' ? (
                          <Clock className="w-3.5 h-3.5 text-amber-400" />
                        ) : (
                          <span title={run.error}>
                            <XCircle className="w-3.5 h-3.5 text-red-400" />
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </SectionCard>
  );
}
