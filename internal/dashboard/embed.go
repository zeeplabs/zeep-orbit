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
		path := strings.TrimPrefix(r.URL.Path, "/dashboard")
		if path == "" {
			path = "/"
		}

		// Check if the exact file exists; otherwise serve index.html for SPA routing
		trimmed := strings.TrimPrefix(path, "/")
		f, err := sub.Open(trimmed)
		if err != nil || trimmed == "" {
			// Serve index.html
			content, readErr := sub.Open("index.html")
			if readErr != nil {
				http.Error(w, "dashboard not built — run make dashboard-build", http.StatusServiceUnavailable)
				return
			}
			content.Close()
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r2)
			return
		}
		f.Close()

		r2 := r.Clone(r.Context())
		r2.URL.Path = path
		fileServer.ServeHTTP(w, r2)
	})
}
