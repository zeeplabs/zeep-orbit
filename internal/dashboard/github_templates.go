// This file implements the GitHub template repository CRUD endpoints:
// listing, creating, updating, soft-deleting (deactivating), and
// reactivating template repositories used by the repo-generation flow.
// POST and PUT both verify the target repository with GitHub (via
// internal/github.Client.VerifyTemplateRepo, using a real installation
// token) before persisting anything, so a nonexistent repo or a repo that
// isn't marked as a template never lands in the database.
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/github"
)

// GitHubTemplatesHandler serves the superadmin-only GitHub template CRUD
// endpoints under /dashboard/api/github/templates.
type GitHubTemplatesHandler struct {
	pool *db.Pool

	// httpClient overrides the HTTP client used by internal/github.Client
	// instances built by this handler. nil in production; tests set this to
	// a client wired to a mock transport so no real network call reaches
	// api.github.com.
	httpClient *http.Client
}

// NewGitHubTemplatesHandler builds a GitHubTemplatesHandler backed by pool.
func NewGitHubTemplatesHandler(pool *db.Pool) *GitHubTemplatesHandler {
	return &GitHubTemplatesHandler{pool: pool}
}

type githubTemplateRequest struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	GitHubOwner       string `json:"github_owner"`
	GitHubRepo        string `json:"github_repo"`
	Framework         string `json:"framework"`
	RenderServiceType string `json:"render_service_type"`
	BuildCommand      string `json:"build_command"`
	PublishPath       string `json:"publish_path"`
	StartCommand      string `json:"start_command"`
}

// buildVerifiedClient reads the current GitHub App config, requires that the
// App is fully installed (a real installation_id present), and returns a
// client capable of authenticated (installation-token) calls such as
// VerifyTemplateRepo. It writes the appropriate error response itself and
// returns ok=false when the precondition isn't met.
func (h *GitHubTemplatesHandler) buildVerifiedClient(w http.ResponseWriter, r *http.Request) (*github.Client, bool) {
	cfg, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return nil, false
	}
	if cfg == nil || cfg.InstallationID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub not connected — connect and install the GitHub App first"})
		return nil, false
	}

	client := github.NewClient(github.AppConfig{
		AppID:          cfg.AppID,
		InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
		PrivateKeyPEM:  []byte(cfg.PrivateKey),
		HTTPClient:     h.httpClient,
	})
	return client, true
}

// List handles GET /dashboard/api/github/templates. The ?active_only=true
// query param restricts results to active templates only; any other value
// (or its absence) returns all templates regardless of active status.
func (h *GitHubTemplatesHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	onlyActive := r.URL.Query().Get("active_only") == "true"

	templates, err := ListGitHubTemplates(r.Context(), h.pool, onlyActive)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, templates)
}

// Create handles POST /dashboard/api/github/templates. It verifies the
// target repository with GitHub before persisting: a repo that doesn't
// exist, isn't accessible to the installation, or isn't marked as a
// template is rejected with a clear message and nothing is written to the
// database.
func (h *GitHubTemplatesHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var body githubTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Name == "" || body.GitHubOwner == "" || body.GitHubRepo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, github_owner, and github_repo are required"})
		return
	}

	client, ok := h.buildVerifiedClient(w, r)
	if !ok {
		return
	}

	if err := client.VerifyTemplateRepo(r.Context(), body.GitHubOwner, body.GitHubRepo); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	input := GitHubTemplateInput{
		Name:              body.Name,
		Description:       body.Description,
		GitHubOwner:       body.GitHubOwner,
		GitHubRepo:        body.GitHubRepo,
		Framework:         body.Framework,
		CreatedBy:         user.ID,
		RenderServiceType: body.RenderServiceType,
		BuildCommand:      body.BuildCommand,
		PublishPath:       body.PublishPath,
		StartCommand:      body.StartCommand,
	}
	if err := validateDeployConfig(body.RenderServiceType, body.BuildCommand, body.PublishPath, body.StartCommand); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	created, err := CreateGitHubTemplate(r.Context(), h.pool, input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"github_owner": body.GitHubOwner, "github_repo": body.GitHubRepo})
	h.audit(r.Context(), user.ID, user.Email, "github.template.create", "github_template", created.ID, created.Name, meta, r.RemoteAddr)

	writeJSON(w, http.StatusCreated, created)
}

// Update handles PUT /dashboard/api/github/templates/{id}. The repository
// is re-verified with GitHub before persisting, since github_owner and
// github_repo may have changed as part of the update.
func (h *GitHubTemplatesHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var body githubTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Name == "" || body.GitHubOwner == "" || body.GitHubRepo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, github_owner, and github_repo are required"})
		return
	}

	client, ok := h.buildVerifiedClient(w, r)
	if !ok {
		return
	}

	if err := client.VerifyTemplateRepo(r.Context(), body.GitHubOwner, body.GitHubRepo); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	input := GitHubTemplateInput{
		Name:              body.Name,
		Description:       body.Description,
		GitHubOwner:       body.GitHubOwner,
		GitHubRepo:        body.GitHubRepo,
		Framework:         body.Framework,
		RenderServiceType: body.RenderServiceType,
		BuildCommand:      body.BuildCommand,
		PublishPath:       body.PublishPath,
		StartCommand:      body.StartCommand,
	}
	if err := validateDeployConfig(body.RenderServiceType, body.BuildCommand, body.PublishPath, body.StartCommand); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, err := UpdateGitHubTemplate(r.Context(), h.pool, id, input)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"github_owner": body.GitHubOwner, "github_repo": body.GitHubRepo})
	h.audit(r.Context(), user.ID, user.Email, "github.template.update", "github_template", updated.ID, updated.Name, meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /dashboard/api/github/templates/{id}. This is a
// soft toggle, not a real delete: it sets active=false and leaves the row
// in place so historical repo-generation records referencing this template
// remain valid.
func (h *GitHubTemplatesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")

	if err := SetGitHubTemplateActive(r.Context(), h.pool, id, false); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	h.audit(r.Context(), user.ID, user.Email, "github.template.delete", "github_template", id, "", nil, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

type githubTemplateActiveRequest struct {
	Active bool `json:"active"`
}

// SetActive handles PUT /dashboard/api/github/templates/{id}/active. It is
// the only way to reactivate a template previously soft-deleted via Delete
// (SetGitHubTemplateActive supports both directions, but the brief's DELETE
// only exercises the false path).
func (h *GitHubTemplatesHandler) SetActive(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body githubTemplateActiveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := SetGitHubTemplateActive(r.Context(), h.pool, id, body.Active); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	action := "github.template.deactivate"
	if body.Active {
		action = "github.template.activate"
	}
	h.audit(r.Context(), user.ID, user.Email, action, "github_template", id, "", nil, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "active": body.Active})
}

// audit records an audit_log entry, swallowing the error the same way
// GitHubConfigHandler.audit does: audit logging failures must never block
// the primary operation the caller already committed to.
func (h *GitHubTemplatesHandler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	_ = InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)
}

func validateDeployConfig(serviceType, buildCommand, publishPath, startCommand string) error {
	if serviceType == "" {
		return nil
	}
	if serviceType != "static_site" && serviceType != "web_service" {
		return fmt.Errorf("invalid render_service_type: must be 'static_site' or 'web_service'")
	}
	if buildCommand == "" {
		return fmt.Errorf("build_command is required when render_service_type is set")
	}
	if serviceType == "static_site" && publishPath == "" {
		return fmt.Errorf("publish_path is required for static_site")
	}
	if serviceType == "web_service" && startCommand == "" {
		return fmt.Errorf("start_command is required for web_service")
	}
	return nil
}
