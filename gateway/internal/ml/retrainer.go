package ml

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Retrainer（自动滚动重训，对标 FreqAI live_retrain_hours）───────────
//
// 调度模型与 reconcile.Service 对齐：Start 拉起周期循环、Stop 优雅退出，
// 生命周期由 cmd/server 管理（先 Stop 再关 store）。到点触发 TrainingPipeline
// 重训；失败记 last_error 并 notify 告警；连续失败 3 次自动暂停（active=0）
// 并升级 CRITICAL 告警；顺带按 TTL 清理预测缓存表。
//
// 依赖走窄接口（PipelineRunner / NotifySender），训练与告警均可 mock 测试。

// PipelineRunner 训练入口窄接口，*TrainingPipeline 天然满足。
type PipelineRunner interface {
	Run(cfg PipelineConfig) (*PipelineResult, error)
}

// NotifySender 告警窄接口，*notify.Manager 天然满足。
type NotifySender interface {
	Send(msg notify.Message)
}

const (
	DefaultRetrainCheckInterval   = 60 * time.Second // 任务到期扫描周期
	DefaultPredictionTTL          = 30 * 24 * time.Hour
	DefaultMaxConsecutiveFailures = 3
	defaultRetrainIntervalMin     = 1440 // 24h，对应 freqtrade live_retrain_hours 默认
)

// RetrainerConfig 重训引擎配置。读取顺序：ml_settings 覆盖 > 环境变量 > 默认值。
type RetrainerConfig struct {
	CheckInterval          time.Duration // 到期扫描周期
	PredictionTTL          time.Duration // 预测缓存保留期
	MaxConsecutiveFailures int           // 连续失败自动暂停阈值
	Enabled                bool          // 总开关
}

func mlEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func mlEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// mlEnvDays 读正整数天数并转 Duration（非法/非正回默认）。
func mlEnvDays(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	return def
}

func mlEnvDurationSec(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

// LoadRetrainerConfig 组装配置：env 打底，ml_settings 键值覆盖。
func LoadRetrainerConfig(repo *store.MLRetrainRepo) RetrainerConfig {
	cfg := RetrainerConfig{
		CheckInterval:          mlEnvDurationSec("ML_RETRAIN_CHECK_SEC", DefaultRetrainCheckInterval),
		PredictionTTL:          mlEnvDays("ML_PREDICTION_TTL_D", DefaultPredictionTTL),
		MaxConsecutiveFailures: mlEnvInt("ML_RETRAIN_MAX_FAILS", DefaultMaxConsecutiveFailures),
		Enabled:                mlEnvBool("ML_RETRAIN_ENABLED", true),
	}
	if repo == nil {
		return cfg
	}
	if v := repo.GetSetting("prediction_ttl_d"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PredictionTTL = time.Duration(n) * 24 * time.Hour
		}
	}
	if v := repo.GetSetting("max_consecutive_failures"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConsecutiveFailures = n
		}
	}
	if v := repo.GetSetting("enabled"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Enabled = b
		}
	}
	return cfg
}

// JobPipelineConfig 把任务行解析成训练配置：feature_set 为 JSON
// （symbol/interval/model_type/task_type/lookback_days/feature_periods/label_horizon/model_params），
// 空串/解析失败回落默认值；ModelID 固定为任务 model_name（重训原地更新同一模型）。
func JobPipelineConfig(job *store.MLRetrainJob) PipelineConfig {
	cfg := DefaultPipelineConfig()
	if job == nil {
		return cfg
	}
	cfg.ModelID = job.ModelName
	if job.FeatureSet == "" {
		return cfg
	}
	var spec struct {
		Symbol         string         `json:"symbol"`
		Interval       string         `json:"interval"`
		ModelType      string         `json:"model_type"`
		TaskType       string         `json:"task_type"`
		LookbackDays   int            `json:"lookback_days"`
		FeaturePeriods []int          `json:"feature_periods"`
		LabelHorizon   int            `json:"label_horizon"`
		ModelParams    map[string]any `json:"model_params"`
	}
	if err := json.Unmarshal([]byte(job.FeatureSet), &spec); err != nil {
		log.Printf("[ml-retrainer] job %d feature_set 解析失败，用默认配置: %v", job.ID, err)
		return cfg
	}
	if spec.Symbol != "" {
		cfg.Symbol = spec.Symbol
	}
	if spec.Interval != "" {
		cfg.Interval = spec.Interval
	}
	if spec.ModelType != "" {
		cfg.ModelType = spec.ModelType
	}
	if spec.TaskType != "" {
		cfg.TaskType = spec.TaskType
	}
	if spec.LookbackDays > 0 {
		cfg.LookbackDays = spec.LookbackDays
	}
	if len(spec.FeaturePeriods) > 0 {
		cfg.FeaturePeriods = spec.FeaturePeriods
	}
	if spec.LabelHorizon > 0 {
		cfg.LabelHorizon = spec.LabelHorizon
	}
	cfg.ModelParams = spec.ModelParams
	return cfg
}

// Retrainer 重训调度器：按任务间隔驱动 pipeline 重训。
type Retrainer struct {
	repo     *store.MLRetrainRepo
	predRepo *store.MLPredictionRepo
	runner   PipelineRunner
	notifier NotifySender
	cache    *PredictionCache // 重训成功后失效该模型预测缓存；可为 nil
	cfg      RetrainerConfig

	mu          sync.Mutex
	running     bool
	stopCh      chan struct{}
	doneCh      chan struct{}
	lastCleanup time.Time
	runningJobs map[int64]bool // 同任务不重入（手动/周期撞车保护）
}

// NewRetrainer 组装重训引擎。runner 为 nil 时调度只记账不训练（不推荐，测试除外）。
func NewRetrainer(repo *store.MLRetrainRepo, runner PipelineRunner, notifier NotifySender, cache *PredictionCache) *Retrainer {
	return &Retrainer{
		repo:        repo,
		predRepo:    store.NewMLPredictionRepo(),
		runner:      runner,
		notifier:    notifier,
		cache:       cache,
		cfg:         LoadRetrainerConfig(repo),
		runningJobs: make(map[int64]bool),
	}
}

// Start 启动调度循环（首个周期后才开始检查，给启动期数据/ML server 预热时间）。
func (r *Retrainer) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	go r.loop()
}

// Stop 优雅停止：等当前轮跑完（单任务错误已隔离，不会挂死）。
func (r *Retrainer) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stopCh)
	done := r.doneCh
	r.mu.Unlock()
	<-done
}

// IsRunning 调度循环是否存活。
func (r *Retrainer) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Config 返回当前生效配置。
func (r *Retrainer) Config() RetrainerConfig { return r.cfg }

func (r *Retrainer) loop() {
	defer close(r.doneCh)
	ticker := time.NewTicker(r.cfg.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.tick()
		}
	}
}

// tick 单轮：扫描到期任务 + 顺带预测缓存 TTL 清理。panic 不得杀死循环。
func (r *Retrainer) tick() {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[ml-retrainer] tick panic: %v", rec)
		}
	}()
	if !r.cfg.Enabled {
		return
	}
	r.checkJobs(time.Now())
	r.cleanupIfDue(time.Now())
}

// checkJobs 触发所有到期的启用任务（串行，单个任务 panic 不影响其他任务）。
func (r *Retrainer) checkJobs(now time.Time) {
	if r.repo == nil || r.runner == nil {
		return
	}
	jobs, err := r.repo.ListJobs(0, true)
	if err != nil {
		log.Printf("[ml-retrainer] list jobs: %v", err)
		return
	}
	for _, job := range jobs {
		interval := time.Duration(job.IntervalMinutes) * time.Minute
		due := job.LastRunAt == 0 || now.Sub(time.UnixMilli(job.LastRunAt)) >= interval
		if !due {
			continue
		}
		if !r.acquire(job.ID) {
			continue // 手动触发正在跑同一任务，跳过本轮
		}
		r.runJob(job, "schedule")
	}
}

// RunJob 手动触发一次重训（API 用），同步返回本次运行记录。
func (r *Retrainer) RunJob(id int64) (*store.MLRetrainRun, error) {
	if r == nil || r.repo == nil {
		return nil, fmt.Errorf("retrainer not available")
	}
	job, err := r.repo.GetJob(id)
	if err != nil {
		return nil, err
	}
	if r.runner == nil {
		return nil, fmt.Errorf("pipeline runner not configured")
	}
	if !r.acquire(job.ID) {
		return nil, fmt.Errorf("job %d is already running", id)
	}
	r.runJob(job, "manual")
	runs, err := r.repo.ListRuns(id, 1)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("run record missing for job %d", id)
	}
	return runs[0], nil
}

// runJob 执行一次重训并记账：运行记录 + job last_* + 告警 + 连续失败自动暂停
// + 成功后失效旧预测缓存。调用方须先 acquire。
func (r *Retrainer) runJob(job *store.MLRetrainJob, trigger string) {
	defer r.release(job.ID)
	defer func() {
		if rec := recover(); rec != nil {
			errMsg := fmt.Sprintf("panic: %v", rec)
			log.Printf("[ml-retrainer] job %d (%s) retrain panic: %s", job.ID, job.ModelName, errMsg)
			if _, err := r.repo.RecordRun(job.ID, "failed", errMsg, 0); err != nil {
				log.Printf("[ml-retrainer] record run: %v", err)
			}
			r.notify("WARN", "ML 重训任务失败: "+job.ModelName,
				fmt.Sprintf("job_id=%d trigger=%s error=%s", job.ID, trigger, errMsg))
		}
	}()

	cfg := JobPipelineConfig(job)
	start := time.Now()
	result, err := r.runner.Run(cfg)
	durationMs := time.Since(start).Milliseconds()

	status, errMsg := "success", ""
	switch {
	case err != nil:
		status, errMsg = "failed", err.Error()
	case result == nil:
		status, errMsg = "failed", "pipeline returned nil result"
	case !result.Success:
		status, errMsg = "failed", result.Error
		if errMsg == "" {
			errMsg = "pipeline reported unsuccessful"
		}
	}

	if _, recErr := r.repo.RecordRun(job.ID, status, errMsg, durationMs); recErr != nil {
		log.Printf("[ml-retrainer] record run: %v", recErr)
	}

	if status == "success" {
		log.Printf("[ml-retrainer] job %d (%s) retrain ok (%s): samples=%d features=%d duration=%dms",
			job.ID, job.ModelName, trigger, result.TrainSamples, result.FeaturesGenerated, durationMs)
		// 模型已更新：旧预测缓存全部失效，避免新旧模型结果串用。
		if r.cache != nil {
			if n, err := r.cache.InvalidateModel(job.ModelName); err == nil && n > 0 {
				log.Printf("[ml-retrainer] invalidated %d cached predictions for %s", n, job.ModelName)
			}
		} else if r.predRepo != nil {
			_, _ = r.predRepo.DeleteForModel(job.ModelName)
		}
		return
	}

	log.Printf("[ml-retrainer] job %d (%s) retrain failed (%s): %s", job.ID, job.ModelName, trigger, errMsg)
	r.notify("WARN", "ML 重训任务失败: "+job.ModelName,
		fmt.Sprintf("job_id=%d trigger=%s error=%s", job.ID, trigger, errMsg))

	fails, ferr := r.repo.ConsecutiveFailures(job.ID, r.cfg.MaxConsecutiveFailures)
	if ferr != nil {
		log.Printf("[ml-retrainer] count failures: %v", ferr)
		return
	}
	if fails >= r.cfg.MaxConsecutiveFailures {
		if err := r.repo.SetJobActive(job.ID, false); err != nil {
			log.Printf("[ml-retrainer] auto-pause job %d: %v", job.ID, err)
			return
		}
		log.Printf("[ml-retrainer] job %d (%s) auto-paused after %d consecutive failures",
			job.ID, job.ModelName, fails)
		r.notify("CRITICAL", "ML 重训任务已自动暂停: "+job.ModelName,
			fmt.Sprintf("job_id=%d 连续失败 %d 次（阈值 %d），任务已置为 inactive。请检查 last_error 修复后手动重新启用。",
				job.ID, fails, r.cfg.MaxConsecutiveFailures))
	}
}

// CleanupPredictions 按 TTL 清理预测缓存表（早于 cutoff 的删除），返回删除条数。
func (r *Retrainer) CleanupPredictions() (int64, error) {
	if r.predRepo == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-r.cfg.PredictionTTL).UnixMilli()
	return r.predRepo.DeleteOlderThan(cutoff)
}

// cleanupIfDue 预测清理每小时最多一次（DELETE 走 created_at 索引，开销可控）。
func (r *Retrainer) cleanupIfDue(now time.Time) {
	r.mu.Lock()
	due := r.lastCleanup.IsZero() || now.Sub(r.lastCleanup) >= time.Hour
	if due {
		r.lastCleanup = now
	}
	r.mu.Unlock()
	if !due {
		return
	}
	if n, err := r.CleanupPredictions(); err != nil {
		log.Printf("[ml-retrainer] cleanup predictions: %v", err)
	} else if n > 0 {
		log.Printf("[ml-retrainer] cleaned %d expired predictions (ttl %s)", n, r.cfg.PredictionTTL)
	}
}

func (r *Retrainer) acquire(jobID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runningJobs[jobID] {
		return false
	}
	r.runningJobs[jobID] = true
	return true
}

func (r *Retrainer) release(jobID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runningJobs, jobID)
}

func (r *Retrainer) notify(level, title, content string) {
	if r.notifier == nil {
		return
	}
	r.notifier.Send(notify.Message{
		Title:   title,
		Content: content,
		Level:   level,
		Tags:    map[string]string{"source": "ml_retrain"},
	})
}

// 默认任务间隔常量导出（API 层创建任务时复用）。
func DefaultRetrainIntervalMinutes() int { return defaultRetrainIntervalMin }
