package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFiles embed.FS

// StaticHandler returns an http.Handler that serves the embedded SPA.
// Falls back to index.html for client-side routing.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("dashboard: failed to sub static FS: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Chi strips the /dashboard prefix from r.URL.Path inside the sub-router,
		// but handle both cases for safety.
		path := strings.TrimPrefix(r.URL.Path, "/dashboard")
		if path == "" {
			path = "/"
		}

		trimmed := strings.TrimPrefix(path, "/")

		// Root or unknown path → serve index.html directly (avoid http.FileServer's
		// /index.html → "./" redirect which causes infinite loops in browsers).
		if trimmed == "" {
			serveIndexHTML(w, r, sub)
			return
		}

		// Check if the exact static asset exists.
		f, err := sub.Open(trimmed)
		if err != nil {
			// Not found → SPA client-side route, serve index.html.
			serveIndexHTML(w, r, sub)
			return
		}
		f.Close()

		r2 := r.Clone(r.Context())
		r2.URL.Path = path
		fileServer.ServeHTTP(w, r2)
	})
}

// serveIndexHTML reads and writes index.html directly, bypassing http.FileServer
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
