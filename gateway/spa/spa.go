// Package spa provides the embedded frontend static assets for the Go gateway.
package spa

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed *
var content embed.FS

var fsContent = http.FS(content)

// IndexHTML returns the embedded index.html bytes.
func IndexHTML() ([]byte, error) {
	return content.ReadFile("index.html")
}

// AssetsFS returns a http.FileSystem rooted at the embedded assets/ directory.
func AssetsFS() http.FileSystem {
	assets, err := fs.Sub(content, "assets")
	if err != nil {
		return http.FS(content)
	}
	return http.FS(assets)
}

// ServeRootFile returns a Gin handler that serves a single file from the SPA root.
func ServeRootFile(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.FileFromFS(name, fsContent)
	}
}
