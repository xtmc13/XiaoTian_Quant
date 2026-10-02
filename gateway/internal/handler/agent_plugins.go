package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/plugin"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// AgentPluginsManifest GET /api/agent/plugins —— 万物皆可插件的清单接口。
// 前端据此渲染侧栏导航、斜杠命令等插件贡献的 UI 入口。
// 清单按请求角色过滤：AdminOnly 导航（如 files 文件回滚）仅管理员可见。
func AgentPluginsManifest(c *gin.Context) {
	if err := builtin.BuildError(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	manifests := builtin.Manager().Manifest()
	if c.GetString("role") == "admin" {
		c.JSON(http.StatusOK, gin.H{"plugins": manifests})
		return
	}
	filtered := make([]plugin.Manifest, 0, len(manifests))
	for _, m := range manifests {
		if m.UI.Nav != nil && m.UI.Nav.AdminOnly {
			m.UI.Nav = nil // 普通用户剔除管理员专属导航
		}
		filtered = append(filtered, m)
	}
	c.JSON(http.StatusOK, gin.H{"plugins": filtered})
}
