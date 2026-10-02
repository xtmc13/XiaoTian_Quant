package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentsub"
)

// AgentSubagentRuns 处理 GET /api/agent/subagents：当前用户（JWT）的子代理运行记录，
// 最新在前，上限 50（进程内存注册表，单进程部署；重启即清空）。
func AgentSubagentRuns(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	c.JSON(http.StatusOK, gin.H{"success": true, "runs": agentsub.ListRuns(uid)})
}
