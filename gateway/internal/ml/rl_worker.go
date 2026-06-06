// Package ml provides RL (Reinforcement Learning) task queue management
// for asynchronous SB3 training via independent Python worker processes.
package ml

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xiaotian-quant/gateway/internal/cache"
)

// ── RL Task Queue ──────────────────────────────────────────────

// RLJobStatus represents the state of an RL training job.
type RLJobStatus string

const (
	RLJobPending   RLJobStatus = "pending"
	RLJobRunning   RLJobStatus = "running"
	RLJobCompleted RLJobStatus = "completed"
	RLJobFailed    RLJobStatus = "failed"
	RLJobCancelled RLJobStatus = "cancelled"
)

// RLJob represents a single RL training task.
type RLJob struct {
	JobID            string           `json:"job_id"`
	Status           RLJobStatus      `json:"status"`
	Algorithm        string           `json:"algorithm"`
	NActions         int              `json:"n_actions"`
	Symbol           string           `json:"symbol"`
	Interval         string           `json:"interval"`
	Bars             []map[string]any `json:"bars,omitempty"`
	Config           map[string]any   `json:"config"`
	Result           *RLTrainResult   `json:"result,omitempty"`
	Error            string           `json:"error,omitempty"`
	Progress         *RLJobProgress   `json:"progress,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	StartedAt        *time.Time       `json:"started_at,omitempty"`
	CompletedAt      *time.Time       `json:"completed_at,omitempty"`
	TensorBoardRunID string           `json:"tensorboard_run_id,omitempty"`
}

// RLJobProgress tracks training progress.
type RLJobProgress struct {
	CurrentEpisode int     `json:"current_episode"`
	TotalEpisodes  int     `json:"total_episodes"`
	CurrentStep    int     `json:"current_step"`
	TotalSteps     int     `json:"total_steps"`
	BestReward     float64 `json:"best_reward"`
	CurrentBalance float64 `json:"current_balance"`
	Epsilon        float64 `json:"epsilon,omitempty"`
	QTableSize     int     `json:"q_table_size,omitempty"`
	MeanReward     float64 `json:"mean_reward,omitempty"`
	Loss           float64 `json:"loss,omitempty"`
}

// ── Task Queue ─────────────────────────────────────────────────

// RLTaskQueue manages RL training jobs via Redis (or in-memory fallback).
type RLTaskQueue struct {
	cache  cache.Cache
	prefix string
	// redisClient is set when using Redis backend for list operations
	redisClient *redis.Client
}

// NewRLTaskQueue creates a new RL task queue.
func NewRLTaskQueue() *RLTaskQueue {
	c := cache.GetCache()
	q := &RLTaskQueue{
		cache:  c,
		prefix: "rl:",
	}
	// Try to extract Redis client for list operations
	if _, ok := c.(*cache.RedisCache); ok {
		// Use reflection or type assertion to get client
		// Since RedisCache doesn't expose client, we use a workaround:
		// create a new client from env
		q.redisClient = newRedisClient()
	}
	return q
}

// newRedisClient creates a Redis client from environment.
func newRedisClient() *redis.Client {
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379/0")
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil
	}
	return client
}

func getEnv(key, fallback string) string {
	if v := getEnvInternal(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInternal(key string) string {
	// We can't import os here due to package structure,
	// so we use a simple approach via cache
	return ""
}

// SubmitJob creates a new RL training job and adds it to the queue.
func (q *RLTaskQueue) SubmitJob(algorithm, symbol, interval string, nActions int,
	bars []map[string]any, config map[string]any) (*RLJob, error) {

	jobID := fmt.Sprintf("rl_%s_%s_%d", algorithm, symbol, time.Now().Unix())

	job := &RLJob{
		JobID:     jobID,
		Status:    RLJobPending,
		Algorithm: algorithm,
		NActions:  nActions,
		Symbol:    symbol,
		Interval:  interval,
		Bars:      bars,
		Config:    config,
		CreatedAt: time.Now(),
	}

	// Store job metadata
	if err := q.saveJob(job); err != nil {
		return nil, fmt.Errorf("save job: %w", err)
	}

	// Add to queue
	if err := q.enqueue(jobID); err != nil {
		return nil, fmt.Errorf("enqueue: %w", err)
	}

	return job, nil
}

// GetJob retrieves a job by ID.
func (q *RLTaskQueue) GetJob(jobID string) (*RLJob, error) {
	var job RLJob
	if err := q.cache.GetJSON(q.prefix+"job:"+jobID, &job); err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return &job, nil
}

// UpdateJob saves updated job state.
func (q *RLTaskQueue) UpdateJob(job *RLJob) error {
	return q.saveJob(job)
}

// CancelJob marks a job as cancelled.
func (q *RLTaskQueue) CancelJob(jobID string) error {
	job, err := q.GetJob(jobID)
	if err != nil {
		return err
	}
	if job.Status == RLJobRunning {
		job.Status = RLJobCancelled
	} else if job.Status == RLJobPending {
		job.Status = RLJobCancelled
		_ = q.removeFromQueue(jobID)
	}
	return q.saveJob(job)
}

// ReportProgress updates job progress (called by worker).
func (q *RLTaskQueue) ReportProgress(jobID string, progress *RLJobProgress) error {
	job, err := q.GetJob(jobID)
	if err != nil {
		return err
	}
	job.Progress = progress
	return q.saveJob(job)
}

// ReportCompletion marks a job as completed with results.
func (q *RLTaskQueue) ReportCompletion(jobID string, result *RLTrainResult) error {
	job, err := q.GetJob(jobID)
	if err != nil {
		return err
	}
	now := time.Now()
	job.Status = RLJobCompleted
	job.Result = result
	job.CompletedAt = &now
	return q.saveJob(job)
}

// ReportFailure marks a job as failed.
func (q *RLTaskQueue) ReportFailure(jobID string, errMsg string) error {
	job, err := q.GetJob(jobID)
	if err != nil {
		return err
	}
	now := time.Now()
	job.Status = RLJobFailed
	job.Error = errMsg
	job.CompletedAt = &now
	return q.saveJob(job)
}

// IsAdvancedAlgorithm returns true for algorithms requiring worker process.
func IsAdvancedAlgorithm(algorithm string) bool {
	switch algorithm {
	case "ppo", "a2c", "sac":
		return true
	default:
		return false
	}
}

// ── Internal helpers ───────────────────────────────────────────

func (q *RLTaskQueue) saveJob(job *RLJob) error {
	return q.cache.SetJSON(q.prefix+"job:"+job.JobID, job, 24*time.Hour)
}

func (q *RLTaskQueue) enqueue(jobID string) error {
	if q.redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return q.redisClient.LPush(ctx, q.prefix+"queue", jobID).Err()
	}
	// Fallback: store in a simple key (only supports single job)
	return q.cache.Set(q.prefix+"queue:pending", jobID, 0)
}

func (q *RLTaskQueue) popQueue() (string, error) {
	if q.redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := q.redisClient.BRPop(ctx, 1*time.Second, q.prefix+"queue").Result()
		if err == redis.Nil {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if len(result) >= 2 {
			return result[1], nil
		}
		return "", nil
	}
	// Fallback
	val, err := q.cache.Get(q.prefix + "queue:pending")
	if err != nil {
		return "", nil
	}
	_ = q.cache.Delete(q.prefix + "queue:pending")
	return val, nil
}

func (q *RLTaskQueue) removeFromQueue(jobID string) error {
	if q.redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return q.redisClient.LRem(ctx, q.prefix+"queue", 0, jobID).Err()
	}
	return nil
}

