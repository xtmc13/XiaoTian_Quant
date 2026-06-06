package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ── RL Worker Status ─────────────────────────────────────────

// RLWorkerInfo represents a single RL worker process.
type RLWorkerInfo struct {
	WorkerID    string `json:"worker_id"`
	Status      string `json:"status"`
	CurrentJob  string `json:"current_job,omitempty"`
	LastSeen    string `json:"last_seen"`
	PID         int    `json:"pid"`
}

// RLWorkerStatusResponse returns worker and queue status.
type RLWorkerStatusResponse struct {
	Workers      []RLWorkerInfo `json:"workers"`
	QueueLength  int64          `json:"queue_length"`
	RedisConnected bool         `json:"redis_connected"`
}

// getRedisClient creates a Redis client from environment.
func getRedisClient() *redis.Client {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}
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

// GetRLWorkerStatus returns the status of RL workers and queue.
func GetRLWorkerStatus(c *gin.Context) {
	client := getRedisClient()
	if client == nil {
		c.JSON(http.StatusOK, RLWorkerStatusResponse{
			Workers:        []RLWorkerInfo{},
			QueueLength:    0,
			RedisConnected: false,
		})
		return
	}
	defer client.Close()

	ctx := context.Background()

	// Get queue length
	queueLen, _ := client.LLen(ctx, "rl:queue").Result()

	// Get all workers
	var workers []RLWorkerInfo
	iter := client.Scan(ctx, 0, "rl:worker:*", 100).Iterator()
	for iter.Next(ctx) {
		data, err := client.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var info RLWorkerInfo
		if err := json.Unmarshal([]byte(data), &info); err == nil {
			workers = append(workers, info)
		}
	}

	c.JSON(http.StatusOK, RLWorkerStatusResponse{
		Workers:        workers,
		QueueLength:    queueLen,
		RedisConnected: true,
	})
}

// ── RL Worker Control ────────────────────────────────────────

// StartRLWorkerRequest configures worker startup.
type StartRLWorkerRequest struct {
	RedisURL     string `json:"redis_url,omitempty"`
	QueueDir     string `json:"queue_dir,omitempty"`
	FileMode     bool   `json:"file_mode"`
	MaxJobs      int    `json:"max_jobs,omitempty"`
	PollInterval int    `json:"poll_interval,omitempty"`
}

// StartRLWorkerResponse returns startup result.
type StartRLWorkerResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	WorkerPID int    `json:"worker_pid,omitempty"`
	Command   string `json:"command,omitempty"`
	Error     string `json:"error,omitempty"`
}

// StartRLWorker launches an independent Python RL worker process.
func StartRLWorker(c *gin.Context) {
	var req StartRLWorkerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find Python executable
	pythonExe := findPythonExe()
	if pythonExe == "" {
		c.JSON(http.StatusInternalServerError, StartRLWorkerResponse{
			Success: false,
			Error:   "Python executable not found. Please install Python 3.10+.",
		})
		return
	}

	// Find worker script
	workerScript := findWorkerScript()
	if workerScript == "" {
		c.JSON(http.StatusInternalServerError, StartRLWorkerResponse{
			Success: false,
			Error:   "rl_worker.py not found. Please check sandbox/ml_server/ directory.",
		})
		return
	}

	// Build command arguments
	args := []string{workerScript}
	if req.FileMode {
		args = append(args, "--file-mode")
		if req.QueueDir != "" {
			args = append(args, "--queue-dir", req.QueueDir)
		}
	} else {
		redisURL := req.RedisURL
		if redisURL == "" {
			redisURL = os.Getenv("REDIS_URL")
		}
		if redisURL == "" {
			redisURL = "redis://localhost:6379/0"
		}
		args = append(args, "--redis-url", redisURL)
	}
	if req.MaxJobs > 0 {
		args = append(args, "--max-jobs", fmt.Sprintf("%d", req.MaxJobs))
	}
	if req.PollInterval > 0 {
		args = append(args, "--poll-interval", fmt.Sprintf("%d", req.PollInterval))
	}

	// Start process
	cmd := exec.Command(pythonExe, args...)
	cmd.Dir = filepath.Dir(workerScript)

	// Detach process (platform-specific)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = windowsSysProcAttr()
	}

	// Redirect output to log file
	logFile := filepath.Join(filepath.Dir(workerScript), "rl_worker.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
		defer f.Close()
	}

	if err := cmd.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, StartRLWorkerResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to start worker: %v", err),
			Command: fmt.Sprintf("%s %v", pythonExe, args),
		})
		return
	}

	c.JSON(http.StatusOK, StartRLWorkerResponse{
		Success:   true,
		Message:   "RL Worker started successfully",
		WorkerPID: cmd.Process.Pid,
		Command:   fmt.Sprintf("%s %v", pythonExe, args),
	})
}

// findPythonExe locates the Python executable.
func findPythonExe() string {
	candidates := []string{
		"C:\\Users\\20545\\AppData\\Local\\Programs\\Python\\Python312\\python.exe",
		"python3",
		"python",
	}
	for _, exe := range candidates {
		if _, err := exec.LookPath(exe); err == nil {
			return exe
		}
		if _, err := os.Stat(exe); err == nil {
			return exe
		}
	}
	return ""
}

// findWorkerScript locates the rl_worker.py script.
func findWorkerScript() string {
	candidates := []string{
		"sandbox\\ml_server\\rl_worker.py",
		"..\\sandbox\\ml_server\\rl_worker.py",
		"C:\\Users\\20545\\Desktop\\xiaotian_quant\\sandbox\\ml_server\\rl_worker.py",
	}
	for _, path := range candidates {
		if abs, err := filepath.Abs(path); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}

// windowsSysProcAttr returns Windows-specific process attributes for detachment.
func windowsSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow: true,
	}
}
