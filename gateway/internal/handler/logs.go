package handler

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// GetLogs returns the last N lines of the gateway log file.
// Query param: tail (default 100, max 1000)
func GetLogs(c *gin.Context) {
	tailStr := c.DefaultQuery("tail", "100")
	tail, err := strconv.Atoi(tailStr)
	if err != nil || tail <= 0 {
		tail = 100
	}
	if tail > 1000 {
		tail = 1000
	}

	// Look for gateway.log relative to the current working directory.
	logPath := filepath.Join("logs", "gateway.log")
	file, err := os.Open(logPath)
	if err != nil {
		c.String(http.StatusServiceUnavailable, "日志文件暂不可用: "+err.Error())
		return
	}
	defer file.Close()

	// Read all lines into a ring buffer of size `tail`.
	lines := make([]string, 0, tail)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > tail {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		c.String(http.StatusInternalServerError, "读取日志失败: "+err.Error())
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, strings.Join(lines, "\n"))
}
