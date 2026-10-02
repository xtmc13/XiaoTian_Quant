package agentcron

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/notify"
)

// Executor 到点执行任务：给定用户与指令，返回助手最终回复。
type Executor func(userID int64, prompt string) (string, error)

// JobStore 调度器依赖的最小存储接口（*Repo 实现；测试可替换）。
type JobStore interface {
	DueJobs(now int64, limit int) ([]*Job, error)
	Get(id string, userID int64) (*Job, error)
	SetEnabled(id string, userID int64, enabled bool) error
	MarkRun(id string, status, result string, lastRunAt, nextRunAt int64) error
}

// Scheduler 定时任务调度器：周期扫描到点任务并异步执行。
type Scheduler struct {
	repo     JobStore
	exec     Executor
	interval time.Duration
	stopCh   chan struct{}
	once     sync.Once
	running  sync.Map // jobID -> struct{}（防重入）
}

// NewScheduler 调度器；exec 可为 nil（此时到点仅标记错误，便于测试）。
func NewScheduler(repo JobStore, exec Executor) *Scheduler {
	return &Scheduler{repo: repo, exec: exec, interval: 15 * time.Second, stopCh: make(chan struct{})}
}

// SetExecutor 注入/替换到点执行器（main 装配 headless runner 时调用）。
func (s *Scheduler) SetExecutor(exec Executor) {
	s.exec = exec
}

// Start 启动后台轮询（幂等）。
func (s *Scheduler) Start() {
	go s.once.Do(s.loop)
}

// Stop 停止轮询。
func (s *Scheduler) Stop() { close(s.stopCh) }

func (s *Scheduler) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.scanDue()
		}
	}
}

func (s *Scheduler) scanDue() {
	jobs, err := s.repo.DueJobs(time.Now().Unix(), 20)
	if err != nil {
		log.Printf("[agent-cron] due scan: %v", err)
		return
	}
	for _, j := range jobs {
		if _, busy := s.running.LoadOrStore(j.ID, struct{}{}); busy {
			continue
		}
		go s.run(j)
	}
}

// RunNow 立即异步执行某任务（前端"立即运行"与调试）。
func (s *Scheduler) RunNow(id string, userID int64) (*Job, error) {
	j, err := s.repo.Get(id, userID)
	if err != nil {
		return nil, err
	}
	if _, busy := s.running.LoadOrStore(j.ID, struct{}{}); busy {
		return j, nil
	}
	go s.run(j)
	return j, nil
}

// run 执行单个任务：算下次触发 → 执行 → 落结果 → 非 web 通道投递通知。
func (s *Scheduler) run(j *Job) {
	defer s.running.Delete(j.ID)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[agent-cron] job %s panic: %v", j.ID, r)
		}
	}()

	now := time.Now().Unix()
	next, nextErr := NextRun(j.Schedule, j.Timezone, time.Now())

	status, result := "ok", ""
	if s.exec == nil {
		status, result = "error", "executor not configured"
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		type out struct {
			reply string
			err   error
		}
		ch := make(chan out, 1)
		go func() {
			reply, err := s.exec(j.UserID, j.Prompt)
			ch <- out{reply, err}
		}()
		select {
		case o := <-ch:
			if o.err != nil {
				status, result = "error", o.err.Error()
			} else {
				result = o.reply
			}
		case <-ctx.Done():
			status, result = "error", "执行超时（5 分钟）"
		}
	}

	if len(result) > 1900 {
		result = result[:1900] + "…[truncated]"
	}
	if nextErr != nil {
		// 下次触发算不出来：停任务并记录原因，避免每分钟重试
		_ = s.repo.SetEnabled(j.ID, j.UserID, false)
		result += "（表达式已失效，任务被停用：" + nextErr.Error() + "）"
	}
	if err := s.repo.MarkRun(j.ID, status, result, now, next); err != nil {
		log.Printf("[agent-cron] mark run: %v", err)
	}

	if j.Channel != "" && j.Channel != "web" {
		notify.GetManager().Send(notify.Message{
			Title:     "定时任务「" + j.Name + "」" + map[bool]string{true: "✅", false: "❌"}[status == "ok"],
			Content:   result,
			Level:     map[bool]string{true: "INFO", false: "WARN"}[status == "ok"],
			Tags:      map[string]string{"_channels": j.Channel, "source": "agent-cron"},
			Timestamp: time.Now().Unix(),
		})
	}
}
