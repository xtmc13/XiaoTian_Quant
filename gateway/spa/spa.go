package spa

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed *
var files embed.FS

func FS() http.FileSystem {
	return http.FS(files)
}

func IndexHTML() ([]byte, error) {
	return files.ReadFile("index.html")
}

func AssetsFS() http.FileSystem {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		return http.FS(files)
	}
	return http.FS(sub)
}

func LibFS() http.FileSystem {
	sub, err := fs.Sub(files, "lib")
	if err != nil {
		return http.FS(files)
	}
	return http.FS(sub)
}

// ServeRootFile returns a handler that serves a single file from the SPA root.
func ServeRootFile(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := files.ReadFile(name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		// Set content type based on extension
		switch {
		case strings.HasSuffix(name, ".json"):
			c.Header("Content-Type", "application/json")
		case strings.HasSuffix(name, ".js"):
			c.Header("Content-Type", "application/javascript")
		case strings.HasSuffix(name, ".svg"):
			c.Header("Content-Type", "image/svg+xml")
		case strings.HasSuffix(name, ".css"):
			c.Header("Content-Type", "text/css; charset=utf-8")
		default:
			c.Header("Content-Type", "text/plain")
		}
		c.Data(http.StatusOK, c.Writer.Header().Get("Content-Type"), data)
	}
}
