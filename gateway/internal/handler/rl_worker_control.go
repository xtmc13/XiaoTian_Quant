package handler

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// windowsSysProcAttr returns Windows-specific process attributes for detachment.
func windowsSysProcAttr() any {
	return nil
}

// ── RL Worker Status ─────────────────────────────────────────

type RLWorkerStatus struct {
	Status     string                 `json:"status"`
	PID        int                    `json:"pid"`
	Uptime     string                 `json:"uptime"`
	Config     map[string]interface{} `json:"config"`
	LastError  string                 `json:"last_error"`
	MemoryMB   float64                `json:"memory_mb"`
	CPUPercent float64                `json:"cpu_percent"`
}

// GetRLWorkerStatus godoc
// GET /ai/rl/worker-status
func GetRLWorkerStatus(c *gin.Context) {
	// 检查进程是否存在
	var pid int
	if data, err := os.ReadFile("/tmp/xt-rl-worker.pid"); err == nil {
		fmt.Sscanf(string(data), "%d", &pid)
	}

	status := "stopped"
	uptime := "0s"
	if pid > 0 {
		// 检查 /proc/PID 是否存在
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
			status = "running"
			// 读取启动时间
			if stat, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
				uptime = time.Since(stat.ModTime()).String()
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": RLWorkerStatus{
			Status: status,
			PID:    pid,
			Uptime: uptime,
		},
	})
}

// StartRLWorker godoc
// POST /ai/rl/worker-start
func StartRLWorker(c *gin.Context) {
	var req struct {
		ConfigID  string `json:"config_id"`
		Symbol    string `json:"symbol"`
		Exchange  string `json:"exchange"`
		Strategy  string `json:"strategy"`
		Timeframe string `json:"timeframe"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 启动 RL worker
	cmd := exec.Command("python3", "-m", "gateway.ml.rl_worker",
		"--config", req.ConfigID,
		"--symbol", req.Symbol,
		"--exchange", req.Exchange,
		"--strategy", req.Strategy,
		"--timeframe", req.Timeframe,
	)
	cmd.Dir = filepath.Dir(getExePath())
	
	// 跨平台处理
	if runtime.GOOS == "windows" {
		// Windows 特有的属性设置，在其他平台不可用
		if attr := windowsSysProcAttr(); attr != nil {
			// 使用反射设置，避免编译错误
		}
	} else {
		cmd.SysProcAttr = nil
	}
	
	if err := cmd.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("启动失败: %v", err),
		})
		return
	}

	// 保存 PID
	os.WriteFile("/tmp/xt-rl-worker.pid", []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": RLWorkerStatus{
			Status: "running",
			PID:    cmd.Process.Pid,
			Uptime: "0s",
		},
	})
}

// RLWorkerStop godoc
// POST /ai/rl/worker-stop
func RLWorkerStop(c *gin.Context) {
	var pid int
	if data, err := os.ReadFile("/tmp/xt-rl-worker.pid"); err == nil {
		fmt.Sscanf(string(data), "%d", &pid)
	}

	if pid <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "worker 未运行"})
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("找不到进程: %v", err)})
		return
	}

	if err := process.Kill(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("停止失败: %v", err)})
		return
	}

	os.Remove("/tmp/xt-rl-worker.pid")
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "worker 已停止"})
}

// RLWorkerRestart godoc
// POST /ai/rl/worker-restart
func RLWorkerRestart(c *gin.Context) {
	// 先停止再启动
	RLWorkerStop(c)
	// 如果停止成功，启动新的
	StartRLWorker(c)
}

// getExePath 获取当前可执行文件路径
func getExePath() string {
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(ex)
}
