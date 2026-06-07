package spa

import (
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

//go:embed assets/* index.html manifest.json sw.js favicon.svg test.html
var content embed.FS

// IndexHTML returns the embedded index.html bytes.
func IndexHTML() ([]byte, error) {
	return content.ReadFile("index.html")
}

// AssetsFS returns the embedded assets filesystem as http.FileSystem.
func AssetsFS() http.FileSystem {
	assets, err := fs.Sub(content, "assets")
	if err != nil {
		// Fallback: return empty FS
		return http.FS(&emptyFS{})
	}
	return http.FS(assets)
}

// ServeRootFile returns a gin handler that serves a root-level embedded file.
func ServeRootFile(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := content.ReadFile(name)
		if err != nil {
			c.Status(404)
			return
		}
		c.Data(200, mime.TypeByExtension(filepath.Ext(name)), data)
	}
}

type emptyFS struct{}

func (e *emptyFS) Open(name string) (fs.File, error) {
	return nil, fmt.Errorf("file not found: %s", name)
}
