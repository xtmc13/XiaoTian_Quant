package spa

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed index.html
var indexHTML []byte

//go:embed assets/*
var assetsFS embed.FS

func IndexHTML() ([]byte, error) {
	return indexHTML, nil
}

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(indexHTML)
}

func AssetsFS() http.FileSystem {
	f, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return http.FS(assetsFS)
	}
	return http.FS(f)
}

func ServeRootFile(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		data, err := assetsFS.ReadFile(name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		var contentType string
		switch {
		case name == "manifest.json":
			contentType = "application/json"
		case name == "sw.js":
			contentType = "application/javascript"
		case name == "favicon.svg":
			contentType = "image/svg+xml"
		default:
			contentType = "application/octet-stream"
		}
		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "public, max-age=3600")
		c.Writer.Write(data)
	}
}
