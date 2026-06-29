package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFiles embed.FS

// Falls back to index.html for client-side routing.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("dashboard: failed to sub static FS: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/dashboard")
		if path == "" {
			path = "/"
		}

		trimmed := strings.TrimPrefix(path, "/")

		if trimmed == "" {
			serveIndexHTML(w, r, sub)
			return
		}

		f, err := sub.Open(trimmed)
		if err != nil {
			serveIndexHTML(w, r, sub)
			return
		}
		f.Close()

		r2 := r.Clone(r.Context())
		r2.URL.Path = path
		fileServer.ServeHTTP(w, r2)
	})
}

// to avoid the built-in redirect of "/index.html" paths to "./".
func serveIndexHTML(w http.ResponseWriter, _ *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "dashboard not built — run make dashboard-build", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}
