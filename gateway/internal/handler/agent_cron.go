package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentcron"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 定时任务 REST（前端面板）：全部限定当前登录用户 ──

type agentCronCreateRequest struct {
	Name     string `json:"name" binding:"required"`
	Prompt   string `json:"prompt" binding:"required"`
	Schedule string `json:"schedule" binding:"required"`
	Channel  string `json:"channel"`
	Timezone string `json:"timezone"`
}

// AgentCronList GET /api/agent/cron
func AgentCronList(c *gin.Context) {
	jobs, err := builtinCronRepo().ListByUser(int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// AgentCronCreate POST /api/agent/cron
func AgentCronCreate(c *gin.Context) {
	var req agentCronCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	resolved, err := agentcron.ResolveSchedule(req.Schedule, ResolveScheduleNL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	next, err := agentcron.NextRun(resolved, req.Timezone, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "schedule 无效：" + err.Error()})
		return
	}
	j := &agentcron.Job{
		ID:        agentcron.NewID(),
		UserID:    int64(aiBotUserID(c)),
		Name:      req.Name,
		Prompt:    req.Prompt,
		Schedule:  resolved,
		Timezone:  req.Timezone,
		Channel:   req.Channel,
		Enabled:   true,
		NextRunAt: next,
	}
	if err := builtinCronRepo().Create(j); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": j})
}

// AgentCronToggle POST /api/agent/cron/:id/toggle
func AgentCronToggle(c *gin.Context) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}
	if err := builtinCronRepo().SetEnabled(c.Param("id"), int64(aiBotUserID(c)), body.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "enabled": body.Enabled})
}

// AgentCronDelete DELETE /api/agent/cron/:id
func AgentCronDelete(c *gin.Context) {
	if err := builtinCronRepo().Delete(c.Param("id"), int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// AgentCronRun POST /api/agent/cron/:id/run（异步，立即触发一次）
func AgentCronRun(c *gin.Context) {
	j, err := builtin.CronScheduler().RunNow(c.Param("id"), int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": j.ID, "started": true})
}

func builtinCronRepo() *agentcron.Repo { return agentcron.NewRepo() }
