package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// AgentPluginsManifest GET /api/agent/plugins —— 万物皆可插件的清单接口。
// 前端据此渲染侧栏导航、斜杠命令等插件贡献的 UI 入口。
func AgentPluginsManifest(c *gin.Context) {
	if err := builtin.BuildError(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plugins": builtin.Manager().Manifest()})
}
