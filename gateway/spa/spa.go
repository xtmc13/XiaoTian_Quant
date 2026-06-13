package spa

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

//go:embed assets/* index.html manifest.json sw.js favicon.svg test.html
var content embed.FS

// spaDir, when non-empty, points to an external SPA build directory on disk
// (e.g. ../web/dist). If it exists and is readable, we serve from disk — this
// lets developers run `npm run build` and see the new frontend without
// recompiling the Go binary. When empty or unreadable, we fall back to the
// embedded files compiled into the binary (used for production releases).
var spaDir string

func init() {
	spaDir = os.Getenv("SPA_DIR")
	if spaDir == "" {
		log.Printf("[spa] SPA_DIR not set — serving embedded frontend (production mode)")
		return
	}
	abs, err := filepath.Abs(spaDir)
	if err != nil {
		log.Printf("[spa] SPA_DIR=%q is invalid (%v) — falling back to embedded frontend", spaDir, err)
		spaDir = ""
		return
	}
	if _, err := os.Stat(abs); err != nil {
		log.Printf("[spa] SPA_DIR=%q is not readable (%v) — falling back to embedded frontend", abs, err)
		spaDir = ""
		return
	}
	spaDir = abs
	log.Printf("[spa] serving external frontend from %q (unset SPA_DIR to use embedded)", spaDir)
}

// IndexHTML returns the embedded or external index.html bytes.
func IndexHTML() ([]byte, error) {
	if spaDir != "" {
		return os.ReadFile(filepath.Join(spaDir, "index.html"))
	}
	return content.ReadFile("index.html")
}

// AssetsFS returns the assets filesystem (external or embedded) as http.FileSystem.
func AssetsFS() http.FileSystem {
	if spaDir != "" {
		// http.Dir implements http.FileSystem directly and is the standard way
		// to serve a directory of static files.
		return http.Dir(filepath.Join(spaDir, "assets"))
	}
	assets, err := fs.Sub(content, "assets")
	if err != nil {
		// Fallback: return empty FS
		return http.FS(&emptyFS{})
	}
	return http.FS(assets)
}

// ServeRootFile returns a gin handler that serves a root-level file
// (manifest.json, sw.js, favicon.svg, etc.). Works for both external dir and embed.
func ServeRootFile(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var data []byte
		var err error
		if spaDir != "" {
			data, err = os.ReadFile(filepath.Join(spaDir, name))
		} else {
			data, err = content.ReadFile(name)
		}
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
