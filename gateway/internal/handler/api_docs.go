package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/apispec"
)

// apiDocsPageHTML Swagger UI（CDN 版），数据源指向同组的 /api/docs/openapi.yaml。
// 页面本身不含敏感数据：openapi.yaml 是仓库内的生成物（docs/api/openapi.yaml）。
// 路由仅在 API_DOCS_ENABLED=true 时挂载（见 router.go registerDocsRoutes），
// 生产环境若开启建议在网关层（nginx/basic auth）加访问控制。
const apiDocsPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <title>XiaoTian Quant API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/api/docs/openapi.yaml',
      dom_id: '#swagger-ui',
      persistAuthorization: true,
      deepLinking: true
    });
  </script>
</body>
</html>`

// APIDocsPage 返回 Swagger UI 页面（Try it out 需在 UI 里点 Authorize 粘贴 JWT）。
func APIDocsPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(apiDocsPageHTML))
}

// APIDocsSpec 返回生成的 OpenAPI 契约文件内容。
// 文件定位：OPENAPI_SPEC_PATH > 逐级向上探测 docs/api/openapi.yaml。
func APIDocsSpec(c *gin.Context) {
	p, err := apispec.FindSpecFile()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.File(p)
}
