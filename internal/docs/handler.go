package docs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

//go:embed swaggerui.html
var swaggerHTML []byte

// Handler serve a spec OpenAPI e o Swagger UI por app.
type Handler struct {
	reg *registry.Registry
}

// NewHandler cria Handler com registry injetado.
func NewHandler(reg *registry.Registry) *Handler {
	return &Handler{reg: reg}
}

// HandleIndex serve GET /docs/ — lista todos os apps com links para a doc de cada um.
func (h *Handler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	apps := h.reg.Apps()
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Config.Name < apps[j].Config.Name
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html><html lang="pt-BR"><head><meta charset="utf-8"/>`)
	fmt.Fprint(w, `<title>zeep-orbit API docs</title>`)
	fmt.Fprint(w, `<style>body{font-family:sans-serif;padding:2rem}ul{line-height:2}</style>`)
	fmt.Fprint(w, `</head><body><h1>zeep-orbit API</h1><ul>`)
	for _, app := range apps {
		fmt.Fprintf(w, `<li><a href="/docs/%s">%s</a></li>`, app.Config.Name, app.Config.Name)
	}
	fmt.Fprint(w, `</ul></body></html>`)
}

// HandleUI serve GET /docs/{app} — Swagger UI para o app especificado.
func (h *Handler) HandleUI(w http.ResponseWriter, r *http.Request) {
	appName := chi.URLParam(r, "app")
	if _, ok := h.reg.Get(appName); !ok {
		http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(swaggerHTML)
}

// HandleSpec serve GET /docs/{app}/openapi.json — spec OpenAPI só daquele app.
func (h *Handler) HandleSpec(w http.ResponseWriter, r *http.Request) {
	appName := chi.URLParam(r, "app")
	app, ok := h.reg.Get(appName)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"app not found"}`)
		return
	}
	spec := GenerateForApp(app)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}
