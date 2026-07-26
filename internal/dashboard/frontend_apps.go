package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/github"
)

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)
var multiHyphenRe = regexp.MustCompile(`-+`)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = slugRe.ReplaceAllString(s, "")
	s = multiHyphenRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

type FrontendAppsHandler struct {
	pool *db.Pool

	httpClient *http.Client
}

func NewFrontendAppsHandler(pool *db.Pool) *FrontendAppsHandler {
	return &FrontendAppsHandler{pool: pool}
}

type frontendAppCreateBody struct {
	Name       string `json:"name"`
	TemplateID string `json:"template_id"`
}

func (h *FrontendAppsHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	_ = user

	apps, err := ListFrontendApps(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, apps)
}

func (h *FrontendAppsHandler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	_ = user

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}

	app, err := GetFrontendApp(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	writeJSON(w, http.StatusOK, app)
}

func (h *FrontendAppsHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var body frontendAppCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	if body.TemplateID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id required"})
		return
	}

	templates, err := ListGitHubTemplates(r.Context(), h.pool, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	var tmpl *GitHubTemplate
	for i := range templates {
		if templates[i].ID == body.TemplateID {
			tmpl = &templates[i]
			break
		}
	}
	if tmpl == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template not found or inactive"})
		return
	}

	cfg, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil || cfg == nil || cfg.InstallationID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "github not connected"})
		return
	}

	slug := slugify(body.Name)
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name produces empty slug"})
		return
	}

	exists, err := SlugExists(r.Context(), h.pool, slug)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "name already in use"})
		return
	}

	client := github.NewClient(github.AppConfig{
		AppID:          cfg.AppID,
		InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
		PrivateKeyPEM:  []byte(cfg.PrivateKey),
		HTTPClient:     h.httpClient,
	})

	if err := client.VerifyTemplateRepo(r.Context(), tmpl.GitHubOwner, tmpl.GitHubRepo); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template repository is not accessible or is not a template: " + err.Error()})
		return
	}

	repoURL, createErr := client.CreateRepoFromTemplate(r.Context(), tmpl.GitHubOwner, tmpl.GitHubRepo, slug)

	status := "ready"
	errMsg := ""
	if createErr != nil {
		status = "failed"
		errMsg = createErr.Error()
	}

	app, err := CreateFrontendApp(r.Context(), h.pool, FrontendAppInput{
		Name:          body.Name,
		Slug:          slug,
		TemplateID:    body.TemplateID,
		GithubRepoURL: repoURL,
		Status:        status,
		ErrorMessage:  errMsg,
		CreatedBy:     user.Email,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{
		"slug":          slug,
		"template_name": tmpl.Name,
		"github_repo":   repoURL,
	})
	h.audit(r.Context(), user.ID, user.Email, "frontend_app.create", "frontend_app", app.ID, app.Name, meta, r.RemoteAddr)

	if createErr != nil {
		writeJSON(w, http.StatusOK, app)
		return
	}
	writeJSON(w, http.StatusCreated, app)
}

func (h *FrontendAppsHandler) Retry(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}

	app, err := GetFrontendApp(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if app.Status != "failed" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app is not in failed state"})
		return
	}

	templates, err := ListGitHubTemplates(r.Context(), h.pool, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	var tmpl *GitHubTemplate
	for i := range templates {
		if templates[i].ID == app.TemplateID {
			tmpl = &templates[i]
			break
		}
	}
	if tmpl == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template no longer available"})
		return
	}

	cfg, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil || cfg == nil || cfg.InstallationID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "github not connected"})
		return
	}

	client := github.NewClient(github.AppConfig{
		AppID:          cfg.AppID,
		InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
		PrivateKeyPEM:  []byte(cfg.PrivateKey),
		HTTPClient:     h.httpClient,
	})

	repoURL, createErr := client.CreateRepoFromTemplate(r.Context(), tmpl.GitHubOwner, tmpl.GitHubRepo, app.Slug)

	status := "ready"
	errMsg := ""
	if createErr != nil {
		status = "failed"
		errMsg = createErr.Error()
	}

	updated, err := UpdateFrontendAppStatus(r.Context(), h.pool, app.ID, status, errMsg, repoURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{
		"slug":         app.Slug,
		"github_repo":  repoURL,
		"new_status":   status,
	})
	h.audit(r.Context(), user.ID, user.Email, "frontend_app.retry", "frontend_app", app.ID, app.Name, meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, updated)
}

func (h *FrontendAppsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}

	app, err := GetFrontendApp(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if err := ArchiveFrontendApp(r.Context(), h.pool, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Best-effort GitHub archival.
	if app.GithubRepoURL != "" {
		cfg, cfgErr := GetGitHubConfig(r.Context(), h.pool)
		if cfgErr == nil && cfg != nil && cfg.InstallationID != nil {
			client := github.NewClient(github.AppConfig{
				AppID:          cfg.AppID,
				InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
				PrivateKeyPEM:  []byte(cfg.PrivateKey),
				HTTPClient:     h.httpClient,
			})
			if owner, repo := parseGitHubOwnerRepo(app.GithubRepoURL); owner != "" && repo != "" {
				_ = client.ArchiveRepo(r.Context(), owner, repo)
			}
		}
	}

	h.audit(r.Context(), user.ID, user.Email, "frontend_app.delete", "frontend_app", app.ID, app.Name, nil, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func parseGitHubOwnerRepo(repoURL string) (owner, repo string) {
	s := strings.TrimPrefix(repoURL, "https://github.com/")
	s = strings.TrimSuffix(s, "/")
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (h *FrontendAppsHandler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	_ = InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)
}
