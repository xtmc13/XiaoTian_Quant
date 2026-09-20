// Package reconcile 实现 A8 对账体系：持仓对账（A8.1）、成交恢复（A8.2）、
// 资金费对账（A8.3）、实盘偏差监控（A8.4）与交易所回报 PnL 对账（A8.5）。
//
// 设计要点：
//   - 五个任务由 Service 统一调度（默认 60s 一轮，可用 env/设置表覆盖），
//     生命周期跟随进程：Start 拉起调度循环，Stop 优雅退出（与 cmd/server 的
//     优雅关闭顺序对齐，先 Stop 再关 store）。
//   - 交易所访问走窄接口（PositionQuerier / OrderStatusQuerier / FundingQuerier），
//     适配器实现哪些接口就参与哪些任务，未实现的交易所跳过并记日志，
//     绝不因为对账任务拖垮交易主链路（单轮错误只记日志）。
//   - 差异/偏差统一落 store 的 reconcile_* 表，通知走 notify 模块
//     （Manager 异步投递 + NotificationStore 落库 + WS 广播）。
//   - 全部任务幂等：重复运行不会产生重复差异（open 差异复用、偏差唯一索引去重、
//     资金费镜像唯一键去重）。
package reconcile

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// 任务默认参数（可被环境变量 / reconcile_settings 表覆盖）。
const (
	DefaultInterval          = 60 * time.Second // 对账主周期
	DefaultSlippagePct       = 0.5              // 滑点告警阈值（%）
	DefaultStuckTimeout      = 15 * time.Minute // 订单未成交超时
	DefaultMinDrift          = 1e-9             // 数量漂移最小绝对值，低于视为无漂移
	DefaultFundingLookback   = 24               // 资金费每次拉取小时数
	DefaultFillRecoveryLimit = 50               // 每轮成交恢复最多处理订单数
)

// Config 对账服务配置。字段读取顺序：设置表覆盖 > 环境变量 > 默认值。
type Config struct {
	Interval                time.Duration // 主调度周期
	SlippagePct             float64       // 滑点阈值（百分比）
	StuckTimeout            time.Duration // 未成交超时
	AutoFixPositions        bool          // 持仓漂移自动修正（本地拉平到交易所）
	MinDrift                float64       // 漂移死区
	FundingLookbackH        int           // 资金费拉取窗口（小时）
	FillRecoveryMax         int           // 每轮成交恢复订单上限
	ReportedPnLWindowH      int           // 回报 PnL 对账窗口（小时）
	ReportedPnLThresholdPct float64       // 回报 PnL 差异告警阈值（百分比）
	Enabled                 bool          // 总开关（默认开）
}

// envOrDefaultInt/Float 读环境变量（不存在/非法返回默认）。
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
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

func envDurationSec(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

// LoadConfig 组装配置：env 打底，设置表（reconcile_settings）覆盖。
func LoadConfig(repo *store.ReconcileRepo) *Config {
	cfg := &Config{
		Interval:                envDurationSec("RECONCILE_INTERVAL_SEC", DefaultInterval),
		SlippagePct:             envFloat("RECONCILE_SLIPPAGE_PCT", DefaultSlippagePct),
		StuckTimeout:            envDurationSec("RECONCILE_STUCK_TIMEOUT_SEC", DefaultStuckTimeout),
		AutoFixPositions:        envBool("RECONCILE_AUTO_FIX", false),
		MinDrift:                envFloat("RECONCILE_MIN_DRIFT", DefaultMinDrift),
		FundingLookbackH:        envInt("RECONCILE_FUNDING_LOOKBACK_H", DefaultFundingLookback),
		FillRecoveryMax:         envInt("RECONCILE_FILL_RECOVERY_MAX", DefaultFillRecoveryLimit),
		ReportedPnLWindowH:      envInt("RECONCILE_REPORTED_PNL_WINDOW_H", DefaultReportedPnLWindowH),
		ReportedPnLThresholdPct: envFloat("RECONCILE_REPORTED_PNL_PCT", DefaultReportedPnLThresholdPct),
		Enabled:                 envBool("RECONCILE_ENABLED", true),
	}
	applySettingOverrides(repo, cfg)
	return cfg
}

// applySettingOverrides 设置表里的数值覆盖 env/默认值（管理接口 PUT 写入）。
func applySettingOverrides(repo *store.ReconcileRepo, cfg *Config) {
	if repo == nil {
		return
	}
	if v := repo.GetSetting("interval_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Interval = time.Duration(n) * time.Second
		}
	}
	if v := repo.GetSetting("slippage_pct"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.SlippagePct = f
		}
	}
	if v := repo.GetSetting("stuck_timeout_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.StuckTimeout = time.Duration(n) * time.Second
		}
	}
	if v := repo.GetSetting("auto_fix"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.AutoFixPositions = b
		}
	}
	if v := repo.GetSetting("reported_pnl_window_h"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ReportedPnLWindowH = n
		}
	}
	if v := repo.GetSetting("reported_pnl_pct"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.ReportedPnLThresholdPct = f
		}
	}
	if v := repo.GetSetting("enabled"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Enabled = b
		}
	}
}

// ConfigSnapshot 返回当前生效配置的 JSON 友好形态（GET /api/reconcile/status 用）。
func (c *Config) ConfigSnapshot() map[string]any {
	return map[string]any{
		"interval_sec":          int64(c.Interval / time.Second),
		"slippage_pct":          c.SlippagePct,
		"stuck_timeout_sec":     int64(c.StuckTimeout / time.Second),
		"auto_fix":              c.AutoFixPositions,
		"min_drift":             c.MinDrift,
		"funding_lookback_h":    c.FundingLookbackH,
		"fill_recovery_max":     c.FillRecoveryMax,
		"reported_pnl_window_h": c.ReportedPnLWindowH,
		"reported_pnl_pct":      c.ReportedPnLThresholdPct,
		"enabled":               c.Enabled,
	}
}

// UpdateConfig 持久化配置覆盖到设置表并重载内存配置（键与 applySettingOverrides 读取的键一致）。
func (s *Service) UpdateConfig(overrides map[string]string) *Config {
	for k, v := range overrides {
		_ = s.repo.SetSetting(k, v)
	}
	s.cfgMu.Lock()
	s.cfg = LoadConfig(s.repo)
	cfg := s.cfg
	s.cfgMu.Unlock()
	return cfg
}

// CurrentConfig 返回当前生效配置（拷贝指针，调用方只读）。
func (s *Service) CurrentConfig() *Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// ── Service ──

// TaskRun 记录一次任务执行结果（status 接口展示）。
type TaskRun struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Service 对账调度器：按周期驱动五个对账任务。
type Service struct {
	repo *store.ReconcileRepo
	pos  *PositionReconciler
	fill *FillRecoverer
	fund *FundingReconciler
	dev  *DeviationMonitor
	pnl  *ReportedPnLChecker

	cfgMu sync.RWMutex
	cfg   *Config

	mu      sync.RWMutex
	lastRun map[string]TaskRun
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewService 组装对账服务。exchangeFor 由调用方（cmd/server）注入：
// 按交易所名构造带默认凭证的适配器；返回 nil 表示该交易所未配置/不可用。
func NewService(repo *store.ReconcileRepo, exchangeFor func(name string) any) *Service {
	cfg := LoadConfig(repo)
	s := &Service{
		repo:    repo,
		cfg:     cfg,
		lastRun: make(map[string]TaskRun),
		stopCh:  make(chan struct{}),
	}
	s.pos = NewPositionReconciler(repo, exchangeFor, func() bool {
		return s.CurrentConfig().AutoFixPositions
	}, func() float64 {
		return s.CurrentConfig().MinDrift
	})
	s.fill = NewFillRecoverer(repo, exchangeFor)
	s.fund = NewFundingReconciler(repo, exchangeFor)
	s.dev = NewDeviationMonitor(repo, s.CurrentConfig)
	s.pnl = NewReportedPnLChecker(repo, exchangeFor, func() time.Duration {
		return time.Duration(s.CurrentConfig().ReportedPnLWindowH) * time.Hour
	}, func() float64 {
		return s.CurrentConfig().ReportedPnLThresholdPct
	})
	return s
}

// Start 启动调度循环（立即跑一轮，之后按周期跑），进程退出前调 Stop。
func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.loop()
}

// Stop 优雅停止：等当前轮跑完（任务内部都有超时，不会挂死）。
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	done := s.doneCh
	s.mu.Unlock()
	<-done
}

func (s *Service) loop() {
	defer close(s.doneCh)
	// 启动即跑一轮（进程重启后的成交恢复/持仓基线），之后按周期跑。
	s.runAll()
	ticker := time.NewTicker(s.CurrentConfig().Interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runAll()
		}
	}
}

// runAll 串行跑五个任务；单任务 panic/耗时异常不得影响其他任务与主循环。
func (s *Service) runAll() {
	if !s.CurrentConfig().Enabled {
		return
	}
	tasks := []struct {
		name string
		run  func() (string, error)
	}{
		{"positions", s.pos.Run},
		{"fills", s.fill.Run},
		{"funding", s.fund.Run},
		{"deviations", s.dev.Run},
		{"reported_pnl", s.pnl.Run},
	}
	for _, t := range tasks {
		s.runTask(t.name, t.run)
	}
}

func (s *Service) runTask(name string, run func() (string, error)) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[reconcile] task %s panic: %v", name, r)
			s.setRun(name, TaskRun{Name: name, OK: false, Message: "panic", Timestamp: time.Now().UnixMilli()})
		}
	}()
	msg, err := run()
	rec := TaskRun{Name: name, OK: err == nil, Timestamp: time.Now().UnixMilli()}
	if err != nil {
		rec.Message = err.Error()
	} else if msg != "" {
		rec.Message = msg
	}
	if err != nil {
		log.Printf("[reconcile] task %s failed: %v", name, err)
	}
	s.setRun(name, rec)
}

func (s *Service) setRun(name string, rec TaskRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRun[name] = rec
}

// Status 汇总面板：各任务最近一轮结果 + 未解决差异数 + 当前配置。
func (s *Service) Status() map[string]any {
	s.mu.RLock()
	runs := make(map[string]TaskRun, len(s.lastRun))
	for k, v := range s.lastRun {
		runs[k] = v
	}
	s.mu.RUnlock()

	openDiffs, _ := s.repo.CountOpenDiffs()
	openDevs, _ := s.repo.ListDeviations(0, "", "open", "", 1000, 0)

	return map[string]any{
		"running":         s.IsRunning(),
		"last_runs":       runs,
		"open_diffs":      openDiffs,
		"open_deviations": len(openDevs),
		"config":          s.CurrentConfig().ConfigSnapshot(),
	}
}

// IsRunning 调度循环是否存活。
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// RunReportedPnL 手动触发一次回报 PnL 对账（days>0 时覆盖默认窗口；admin 接口用）。
// 与周期任务同一条 runTask 记账路径：panic 兜住、结果进 lastRun。
func (s *Service) RunReportedPnL(days int) (msg string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("reported_pnl panic: %v", r)
			s.setRun("reported_pnl", TaskRun{Name: "reported_pnl", OK: false, Message: err.Error(), Timestamp: time.Now().UnixMilli()})
		}
	}()
	window := time.Duration(s.CurrentConfig().ReportedPnLWindowH) * time.Hour
	if days > 0 {
		window = time.Duration(days) * 24 * time.Hour
	}
	msg, err = s.pnl.RunWithWindow(window)
	rec := TaskRun{Name: "reported_pnl", OK: err == nil, Timestamp: time.Now().UnixMilli()}
	if err != nil {
		rec.Message = err.Error()
	} else if msg != "" {
		rec.Message = msg
	}
	s.setRun("reported_pnl", rec)
	return msg, err
}

// 供任务内部取仓库（测试可替换）。
func (s *Service) Repo() *store.ReconcileRepo { return s.repo }

func nowMilli() int64 { return time.Now().UnixMilli() }
