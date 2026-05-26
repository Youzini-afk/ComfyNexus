// Package web embeds the built Vite frontend so the gateway ships as a single
// binary. When the frontend has not been built yet (developer mode), the
// embedded FS is empty and the API serves a friendly placeholder.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded "dist" directory rooted at "/", or an empty FS if
// the frontend has not been built.
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return emptyFS{}
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return emptyFS{}
	}
	return sub
}

// Handler returns an http.Handler that serves the embedded SPA. Unknown paths
// fall back to index.html so client-side routing works.
func Handler() http.Handler {
	root := FS()
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.Stat lookups.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			// Single-page-app fallback.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	if name == "." {
		return placeholderFile{}, nil
	}
	return nil, errors.New("frontend not built; run `make build-web`")
}

type placeholderFile struct{}

func (placeholderFile) Stat() (fs.FileInfo, error)              { return nil, errors.New("placeholder") }
func (placeholderFile) Read(_ []byte) (int, error)              { return 0, errors.New("placeholder") }
func (placeholderFile) Close() error                            { return nil }
