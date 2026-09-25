package ml

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/xiaotian-quant/gateway/internal/notify"
)

// ── ML Server 进程管理器（ml_server 生命周期托管）────────────────
//
// ML_SERVER_MANAGED=true 时由 gateway 拉起 sandbox/ml_server/server.py：
//   - 启动时先探测端口：已有健康实例则"收养"直接复用（ML_SERVER_ADOPT=false 关闭）
//   - 启动期健康检查（StartupTimeout 内未就绪只告警，不阻塞 gateway 启动）
//   - 崩溃自动重启（指数退避 1s→60s 封顶；稳定运行 2 分钟重置退避）
//   - 连续 5 次崩溃重启升级 CRITICAL 告警
//   - Stop 优雅退出（SIGTERM → 5s 宽限 → SIGKILL）
//
// 不托管时（默认）保持现状：ml_server 由运维自行启动，retrainer 训练失败
// 走告警/降级路径。Manager 只做进程生命周期，业务闭环在 TrainingLoop。

// ServerManagerConfig 托管配置（全部可走环境变量覆盖）。
type ServerManagerConfig struct {
	Enabled        bool          // ML_SERVER_MANAGED，默认 false
	PythonBin      string        // ML_SERVER_PYTHON，默认 python3
	ScriptPath     string        // ML_SERVER_SCRIPT，默认自动探测 sandbox/ml_server/server.py
	Host           string        // ML_SERVER_HOST，默认 127.0.0.1
	Port           int           // ML_SERVER_PORT，默认 8001
	StartupTimeout time.Duration // ML_SERVER_STARTUP_SEC，默认 30s
	HealthInterval time.Duration // ML_SERVER_HEALTH_SEC，默认 15s
	AdoptExisting  bool          // ML_SERVER_ADOPT，默认 true
	Command        []string      // 测试注入：非空时代替 "python script --port N"
}

// LoadServerManagerConfig 从环境变量组装配置。
func LoadServerManagerConfig() ServerManagerConfig {
	return ServerManagerConfig{
		Enabled:        mlEnvBool("ML_SERVER_MANAGED", false),
		PythonBin:      envOr("ML_SERVER_PYTHON", "python3"),
		ScriptPath:     os.Getenv("ML_SERVER_SCRIPT"),
		Host:           envOr("ML_SERVER_HOST", "127.0.0.1"),
		Port:           mlEnvInt("ML_SERVER_PORT", 8001),
		StartupTimeout: mlEnvDurationSec("ML_SERVER_STARTUP_SEC", 30*time.Second),
		HealthInterval: mlEnvDurationSec("ML_SERVER_HEALTH_SEC", 15*time.Second),
		AdoptExisting:  mlEnvBool("ML_SERVER_ADOPT", true),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ServerStatus 托管状态快照（loop-status API 用）。
type ServerStatus struct {
	Managed   bool   `json:"managed"`
	Running   bool   `json:"running"`    // 子进程存活（adopted 时表示外部实例健康）
	Adopted   bool   `json:"adopted"`    // 复用外部已运行实例（非本进程拉起）
	PID       int    `json:"pid"`        // adopted 时为 0
	Restarts  int    `json:"restarts"`   // 累计崩溃重启次数
	Healthy   bool   `json:"healthy"`    // 最近一次健康检查结果
	URL       string `json:"url"`        // http://host:port
	LastError string `json:"last_error"` // 最近一次启动/健康检查错误（截断）
}

// ServerManager ml_server 子进程托管器。
type ServerManager struct {
	cfg      ServerManagerConfig
	notifier NotifySender

	mu          sync.Mutex
	cmd         *exec.Cmd
	procRunning bool
	adopted     bool
	pid         int
	restarts    int
	healthy     bool
	lastErr     string
	running     bool
	stopCh      chan struct{}
	doneCh      chan struct{}
}

// NewServerManager 创建托管器（notifier 可为 nil）。
func NewServerManager(cfg ServerManagerConfig, notifier NotifySender) *ServerManager {
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port <= 0 {
		cfg.Port = 8001
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 30 * time.Second
	}
	if cfg.HealthInterval <= 0 {
		cfg.HealthInterval = 15 * time.Second
	}
	return &ServerManager{cfg: cfg, notifier: notifier}
}

// URL ml_server 的 base URL（managed 与否都给客户端指向用）。
func (m *ServerManager) URL() string {
	return fmt.Sprintf("http://%s:%d", m.cfg.Host, m.cfg.Port)
}

// Start 启动托管：未启用直接返回；已有健康实例默认收养复用；
// 否则拉起子进程并进入监控循环。任何失败只记日志/告警，不阻塞调用方。
func (m *ServerManager) Start() {
	m.mu.Lock()
	if !m.cfg.Enabled || m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})
	m.mu.Unlock()

	if m.cfg.AdoptExisting && m.checkHealth() == nil {
		m.mu.Lock()
		m.adopted = true
		m.healthy = true
		m.mu.Unlock()
		log.Printf("[ml-server] 检测到已运行的健康实例 %s，直接复用（ML_SERVER_ADOPT）", m.URL())
		go m.monitorLoop() // 收养模式也监控健康，掉线告警
		return
	}

	if err := m.spawn(); err != nil {
		m.setErr(err.Error())
		log.Printf("[ml-server] 首次启动失败: %v（监控循环将继续重试）", err)
	}
	go m.monitorLoop()
}

// Stop 优雅停止：先停监控循环，再终止子进程（收养的实例不杀）。
func (m *ServerManager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopCh)
	done := m.doneCh
	cmd := m.cmd
	adopted := m.adopted
	m.mu.Unlock()

	<-done
	if !adopted && cmd != nil && cmd.Process != nil {
		pid := cmd.Process.Pid
		// 整组发信号（Setpgid 拉起）；组不存在时回退单进程信号
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		// 进程退出由 spawn 的 Wait goroutine 认领（Process.Wait 不可与 Cmd.Wait 并发，
		// 这里只轮询 procRunning 标志），5s 未退再整组 SIGKILL。
		if !m.waitProcExit(5 * time.Second) {
			if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
				_ = cmd.Process.Kill()
			}
			m.waitProcExit(2 * time.Second)
		}
	}
}

// waitProcExit 轮询等待当前子进程退出（procRunning 由 spawn 的 Wait goroutine 维护）。
func (m *ServerManager) waitProcExit(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		up := m.procRunning
		m.mu.Unlock()
		if !up {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// Status 当前托管状态快照。
func (m *ServerManager) Status() ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := ServerStatus{
		Managed:   m.cfg.Enabled,
		Running:   m.procRunning || (m.adopted && m.healthy),
		Adopted:   m.adopted,
		PID:       m.pid,
		Restarts:  m.restarts,
		Healthy:   m.healthy,
		URL:       m.URL(),
		LastError: m.lastErr,
	}
	return st
}

// spawn 拉起子进程并等待启动期健康检查。
func (m *ServerManager) spawn() error {
	m.mu.Lock()
	argv := m.cfg.Command
	if len(argv) == 0 {
		script, err := m.resolveScriptLocked()
		if err != nil {
			m.mu.Unlock()
			return err
		}
		argv = []string{m.cfg.PythonBin, script,
			"--host", m.cfg.Host, "--port", fmt.Sprintf("%d", m.cfg.Port)}
	}
	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- 命令行来自配置/测试注入，非用户输入
	cmd.Stdout = &prefixWriter{prefix: "[ml-server] "}
	cmd.Stderr = &prefixWriter{prefix: "[ml-server] "}
	// 独立进程组：Stop 时整组 SIGTERM/SIGKILL。子进程 shell 可能再 fork
	// （孙进程继承 stdout 管道），只杀主进程会让 Cmd.Wait 等管道 EOF 而卡死。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(m.cfg.Command) == 0 && m.cfg.ScriptPath != "" {
		cmd.Dir = filepath.Dir(m.cfg.ScriptPath)
	}
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("start ml_server: %w", err)
	}
	m.cmd = cmd
	m.pid = cmd.Process.Pid
	m.procRunning = true
	m.lastErr = ""
	m.mu.Unlock()
	log.Printf("[ml-server] 已拉起 ml_server (pid %d): %v", cmd.Process.Pid, argv)

	// 进程退出 → 监控循环重启（Wait 只能调一次，独占在此）
	go func(c *exec.Cmd) {
		err := c.Wait()
		m.mu.Lock()
		if m.cmd == c {
			m.procRunning = false
			m.healthy = false
			if err != nil {
				m.lastErr = truncate(fmt.Sprintf("exit: %v", err), 300)
			}
		}
		m.mu.Unlock()
	}(cmd)

	// 启动期健康检查：超时不视为致命（慢机器上 pandas 导入可达数秒）
	deadline := time.Now().Add(m.cfg.StartupTimeout)
	for time.Now().Before(deadline) {
		if m.checkHealth() == nil {
			m.mu2SetHealthy()
			return nil
		}
		m.mu.Lock()
		gone := !m.procRunning
		m.mu.Unlock()
		if gone {
			return fmt.Errorf("ml_server 启动后立即退出")
		}
		time.Sleep(300 * time.Millisecond)
	}
	log.Printf("[ml-server] 启动期 %s 内健康检查未通过，继续后台等待", m.cfg.StartupTimeout)
	return nil
}

func (m *ServerManager) mu2SetHealthy() {
	m.mu.Lock()
	m.healthy = true
	m.mu.Unlock()
}

// resolveScriptLocked 自动探测 server.py 位置（gateway 仓库根 / gateway 子目录两种 cwd）。
// 调用方须持锁（可能回写 cfg.ScriptPath）。
func (m *ServerManager) resolveScriptLocked() (string, error) {
	if m.cfg.ScriptPath != "" {
		if _, err := os.Stat(m.cfg.ScriptPath); err == nil {
			return m.cfg.ScriptPath, nil
		}
		return "", fmt.Errorf("ML_SERVER_SCRIPT 不存在: %s", m.cfg.ScriptPath)
	}
	candidates := []string{
		"sandbox/ml_server/server.py",
		"../sandbox/ml_server/server.py",
		"../../sandbox/ml_server/server.py",
	}
	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			if _, err := os.Stat(abs); err == nil {
				m.cfg.ScriptPath = abs
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("找不到 sandbox/ml_server/server.py（cwd 相关候选均不存在），请设 ML_SERVER_SCRIPT")
}

// monitorLoop 崩溃重启 + 周期健康检查。退避 1s 起步 60s 封顶，稳定 2 分钟重置。
func (m *ServerManager) monitorLoop() {
	defer close(m.doneCh)
	backoff := time.Second
	stableSince := time.Now()
	crashNotified := false

	ticker := time.NewTicker(m.cfg.HealthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
		}

		m.mu.Lock()
		adopted := m.adopted
		up := m.procRunning
		m.mu.Unlock()

		if adopted {
			if err := m.checkHealth(); err != nil {
				m.mu.Lock()
				m.healthy = false
				m.lastErr = truncate(err.Error(), 300)
				m.mu.Unlock()
			} else {
				m.mu2SetHealthy()
			}
			continue
		}

		if !up {
			if time.Since(stableSince) > 2*time.Minute {
				backoff = time.Second
				crashNotified = false
			}
			select {
			case <-m.stopCh:
				return
			case <-time.After(backoff):
			}
			m.mu.Lock()
			m.restarts++
			restarts := m.restarts
			m.mu.Unlock()
			log.Printf("[ml-server] 进程已退出，%s 后重启（第 %d 次）", backoff, restarts)
			if err := m.spawn(); err != nil {
				log.Printf("[ml-server] 重启失败: %v", err)
				m.setErr(err.Error())
			} else {
				stableSince = time.Now()
				if !crashNotified {
					m.notify("WARN", "ML Server 崩溃已自动重启",
						fmt.Sprintf("url=%s restarts=%d", m.URL(), restarts))
					crashNotified = true
				}
				if restarts >= 5 {
					m.notify("CRITICAL", "ML Server 反复崩溃",
						fmt.Sprintf("url=%s 已累计重启 %d 次，请检查 python 依赖与日志", m.URL(), restarts))
				}
			}
			if backoff < 60*time.Second {
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
			}
			continue
		}

		// 进程存活：周期健康检查
		if err := m.checkHealth(); err != nil {
			m.mu.Lock()
			m.healthy = false
			m.lastErr = truncate(err.Error(), 300)
			m.mu.Unlock()
		} else {
			m.mu2SetHealthy()
		}
	}
}

// checkHealth GET /health，2s 超时。
func (m *ServerManager) checkHealth() error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(m.URL() + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

func (m *ServerManager) setErr(msg string) {
	m.mu.Lock()
	m.lastErr = truncate(msg, 300)
	m.mu.Unlock()
}

func (m *ServerManager) notify(level, title, content string) {
	if m.notifier == nil {
		return
	}
	m.notifier.Send(notify.Message{
		Title:   title,
		Content: content,
		Level:   level,
		Tags:    map[string]string{"source": "ml_server"},
	})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// prefixWriter 给子进程输出加日志前缀。
type prefixWriter struct{ prefix string }

func (w *prefixWriter) Write(p []byte) (int, error) {
	log.Printf("%s%s", w.prefix, truncate(string(p), 500))
	return len(p), nil
}
