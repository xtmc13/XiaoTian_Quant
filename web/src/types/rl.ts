export interface RLTrainConfig {
  model_id?: string
  algorithm: 'qlearning' | 'ppo' | 'a2c' | 'sac'
  n_actions?: number
  symbol: string
  interval: string
  lookback_days?: number
  episodes?: number
  learning_rate?: number
  discount?: number
  epsilon?: number
  window_size?: number
  initial_balance?: number
  commission?: number
  use_tensorboard?: boolean
}

export interface RLTrainResult {
  success: boolean
  model_id: string
  algorithm: string
  n_actions: number
  episodes: number
  final_balance: number
  total_pnl: number
  best_reward: number
  avg_reward_last_10: number
  q_table_size?: number
  episode_rewards: number[]
  tensorboard_url?: string
  duration_ms: number
  error?: string
}

export interface RLPredictResult {
  success: boolean
  model_id: string
  action: number
  action_name: string
  confidence: number
  position: number
}

export interface RLEvalResult {
  success: boolean
  model_id: string
  total_return_pct: number
  sharpe_ratio: number
  max_drawdown_pct: number
  win_rate: number
  trades: number
  avg_trade_return: number
  metrics: Record<string, unknown>
}

import type { MLModelInfo } from './ai'

export type RLModelInfo = MLModelInfo

export interface RLJobProgress {
  current_episode: number
  total_episodes: number
  current_step: number
  total_steps: number
  best_reward: number
  current_balance: number
  epsilon?: number
  q_table_size?: number
  mean_reward?: number
  loss?: number
}

export interface RLJob {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  algorithm: string
  n_actions: number
  symbol: string
  interval: string
  config?: Record<string, unknown>
  result?: RLTrainResult
  error?: string
  progress?: RLJobProgress
  created_at: string
  started_at?: string
  completed_at?: string
  tensorboard_run_id?: string
}

export interface RLWorkerInfo {
  worker_id: string
  status: string
  current_job?: string
  last_seen: string
  pid: number
}

export interface RLWorkerStatus {
  workers: RLWorkerInfo[]
  queue_length: number
  redis_connected: boolean
}

export interface TensorBoardScalar {
  tag: string
  step: number
  wall_time: number
  value: number
}

export interface TensorBoardRun {
  run_id: string
  run_name: string
  model_type: string
  model_id: string
  started_at: string
  updated_at: string
  status: string
  tags: string[]
  scalars?: TensorBoardScalar[]
}

export interface TensorBoardSummary {
  runs: TensorBoardRun[]
  total_runs: number
}

export interface TensorBoardQueryResult {
  run_id: string
  scalars: Record<string, TensorBoardScalar[]>
}
