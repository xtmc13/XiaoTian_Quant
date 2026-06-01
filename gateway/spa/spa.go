package spa

import (
	"embed"
	"io/fs"
	"net/http"
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
