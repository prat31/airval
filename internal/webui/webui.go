// Package webui serves the embedded single-page app with SPA fallback: unknown paths get
// index.html so client-side routes work, and hashed assets get long-lived cache headers.
package webui

import (
	"bytes"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/airval/airval"
)

// Embedded returns the UI filesystem rooted at web/dist.
func Embedded() fs.FS {
	sub, err := fs.Sub(airval.WebDist, "web/dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Dir serves the UI from a directory on disk (frontend development against a built dist).
func Dir(dir string) fs.FS { return os.DirFS(dir) }

// Handler serves files from fsys. Requests for missing files fall back to index.html.
func Handler(fsys fs.FS) http.Handler {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		index = []byte(placeholder)
	}
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "index.html" {
			serveIndex(w, r, index)
			return
		}
		st, err := fs.Stat(fsys, p)
		if err != nil || st.IsDir() {
			// Missing asset files are real 404s; everything else is a client-side route.
			if strings.HasPrefix(p, "assets/") || path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, index)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			// Vite emits content-hashed file names under assets/.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
}

const placeholder = `<!doctype html><html><head><meta charset="utf-8"><title>airval</title></head>
<body><p>The airval UI has not been built. Run <code>make web</code> and rebuild.</p></body></html>`
