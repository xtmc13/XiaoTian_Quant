import React, { useState, useEffect, useCallback } from 'react';
import { rlApi } from '@/lib/api';
import { SectionCard } from '@/components/ui/SectionCard';
import { useToastStore } from '@/stores/toastStore';
import { cn } from '@/lib/utils';
import { 
  Play, 
  Activity, 
  Server, 
  Clock, 
  AlertCircle,
  CheckCircle2,
  Loader2,
  RefreshCw
} from 'lucide-react';

interface RLWorkerInfo {
  worker_id: string;
  status: string;
  current_job?: string;
  last_seen: string;
  pid: number;
}

interface RLWorkerStatus {
  workers: RLWorkerInfo[];
  queue_length: number;
  redis_connected: boolean;
}

export function RLWorkerStatusCard() {
  const [status, setStatus] = useState<RLWorkerStatus | null>(null);
  const [starting, setStarting] = useState(false);
  const [lastUpdate, setLastUpdate] = useState<Date | null>(null);
  const addToast = useToastStore((state) => state.addToast);

  const fetchStatus = useCallback(async () => {
    try {
      const data = await rlApi.getWorkerStatus();
      setStatus(data);
      setLastUpdate(new Date());
    } catch (err) {
      console.error('Failed to fetch worker status:', err);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, 5000);
    return () => clearInterval(interval);
  }, [fetchStatus]);

  const handleStartWorker = async () => {
    setStarting(true);
    try {
      const result = await rlApi.startWorker({
        redis_url: 'redis://localhost:6379/0',
        max_jobs: 10,
        poll_interval: 5
      });
      
      if (result.success) {
        addToast({ type: 'success', message: `RL Worker 启动成功 (PID: ${result.worker_pid})` });
        fetchStatus();
      } else {
        const errorMsg = result.error || result.message || '未知错误';
        addToast({ type: 'error', message: `启动失败: ${errorMsg}` });
      }
    } catch (err: any) {
      const errorMsg = err?.message || err?.response?.data?.error?.message || err?.response?.data?.detail || '网络错误或后端不支持该功能';
      addToast({ type: 'error', message: `启动请求失败: ${errorMsg}` });
    } finally {
      setStarting(false);
    }
  };

  const isOnline = status?.workers?.length > 0;

  return (
    <SectionCard 
      title={
        <div className="flex items-center gap-2">
          <Server className="w-4 h-4 text-[#d4a574]" />
          <span>RL Worker 状态</span>
        </div>
      }
      headerAction={
        <div className="flex items-center gap-2">
          {isOnline ? (
            <span className="flex items-center gap-1 px-2 py-1 rounded text-xs bg-green-500/10 text-green-400">
              <CheckCircle2 className="w-3 h-3" />
              在线
            </span>
          ) : (
            <span className="flex items-center gap-1 px-2 py-1 rounded text-xs bg-red-500/10 text-red-400">
              <AlertCircle className="w-3 h-3" />
              离线
            </span>
          )}
          {lastUpdate && (
            <span className="text-xs text-[#666666]">
              {lastUpdate.toLocaleTimeString()}
            </span>
          )}
        </div>
      }
    >
      <div className="space-y-4">
        {/* Status Overview */}
        <div className="grid grid-cols-3 gap-4">
          <div className="space-y-1">
            <div className="text-xs text-[#666666]">Worker 数量</div>
            <div className="text-2xl font-bold text-[#eeeeee]">{(status?.workers ?? []).length}</div>
          </div>
          <div className="space-y-1">
            <div className="text-xs text-[#666666]">队列长度</div>
            <div className="text-2xl font-bold text-[#eeeeee]">{status?.queue_length || 0}</div>
          </div>
          <div className="space-y-1">
            <div className="text-xs text-[#666666]">Redis 连接</div>
            <div className="flex items-center gap-1">
              <div className={cn(
                "h-2 w-2 rounded-full",
                status?.redis_connected ? 'bg-green-500' : 'bg-red-500'
              )} />
              <span className="text-sm text-[#eeeeee]">{status?.redis_connected ? '正常' : '断开'}</span>
            </div>
          </div>
        </div>

        {/* Worker Details */}
        {status?.workers && status.workers.length > 0 && (
          <div className="space-y-2">
            <div className="text-sm font-medium text-[#aaaaaa]">Worker 详情</div>
            {status.workers.map((worker) => (
              <div 
                key={worker.worker_id}
                className="flex items-center justify-between p-2 rounded-lg border border-[#1c1c1c] bg-[#0a0a0a]"
              >
                <div className="flex items-center gap-2">
                  <Activity className={cn(
                    "h-4 w-4",
                    worker.status === 'busy' ? 'text-yellow-500' : 'text-green-500'
                  )} />
                  <div>
                    <div className="text-sm font-medium text-[#eeeeee]">Worker {worker.pid}</div>
                    <div className="text-xs text-[#666666]">{worker.worker_id}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {worker.current_job && (
                    <span className="px-2 py-0.5 rounded text-xs border border-[#1c1c1c] text-[#888888]">
                      Job: {worker.current_job.slice(0, 8)}...
                    </span>
                  )}
                  <span className={cn(
                    "px-2 py-0.5 rounded text-xs",
                    worker.status === 'busy' 
                      ? 'bg-yellow-500/10 text-yellow-400' 
                      : 'bg-green-500/10 text-green-400'
                  )}>
                    {worker.status === 'busy' ? '训练中' : '空闲'}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Queue Progress */}
        {status?.queue_length && status.queue_length > 0 && (
          <div className="space-y-2">
            <div className="flex items-center justify-between text-sm">
              <span className="text-[#aaaaaa]">队列积压</span>
              <span className="text-[#eeeeee]">{status.queue_length} 个任务</span>
            </div>
            <div className="h-2 w-full rounded-full bg-[#1c1c1c] overflow-hidden">
              <div 
                className="h-full bg-[#d4a574] transition-all"
                style={{ width: `${Math.min(status.queue_length * 10, 100)}%` }}
              />
            </div>
          </div>
        )}

        {/* Action Buttons */}
        <div className="flex gap-2">
          <button 
            onClick={handleStartWorker}
            disabled={starting || isOnline}
            className={cn(
              "flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors flex-1",
              starting || isOnline
                ? 'bg-[#1c1c1c] text-[#666666] cursor-not-allowed'
                : 'bg-[#d4a574] text-[#111111] hover:bg-[#d4a574]/90'
            )}
          >
            {starting ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Play className="h-4 w-4" />
            )}
            {starting ? '启动中...' : isOnline ? 'Worker 运行中' : '启动 Worker'}
          </button>
          <button 
            onClick={fetchStatus}
            className="flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium bg-[#1c1c1c] text-[#aaaaaa] hover:bg-[#2a2a2a] transition-colors"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>

        {/* Offline Warning */}
        {!isOnline && (
          <div className="p-3 rounded-lg bg-yellow-500/5 border border-yellow-500/20">
            <div className="flex items-start gap-2">
              <AlertCircle className="h-4 w-4 text-yellow-500 mt-0.5" />
              <div className="text-sm text-yellow-400">
                <div className="font-medium">Worker 未运行</div>
                <div className="mt-1 text-yellow-400/80">
                  PPO/A2C/SAC 等高级算法需要独立 Worker 进程。
                  <div className="mt-2 p-2 bg-black/30 rounded text-xs text-[#cccccc] font-mono">
                    cd sandbox/ml_server<br/>
                    python rl_worker.py --redis-url redis://localhost:6379/0
                  </div>
                  <div className="mt-1 text-[11px] text-yellow-400/60">
                    提示：当前 Go 后端版本较旧，一键启动功能需要重新编译后端才能使用。
                    建议安装 Go 编译器后执行 go build。
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </SectionCard>
  );
}
