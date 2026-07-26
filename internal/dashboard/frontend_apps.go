package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/deploy"
	"github.com/zeeplabs/zeep-orbit/internal/deploy/render"
	"github.com/zeeplabs/zeep-orbit/internal/github"
	"github.com/zeeplabs/zeep-orbit/internal/sshkey"
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
	Name         string `json:"name"`
	TemplateID   string `json:"template_id"`
	BackendAppID string `json:"backend_app_id"`
	Subdomain    string `json:"subdomain"`
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
		BackendAppID:  body.BackendAppID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// T-05: Generate sync credential after successful repo creation.
	if status == "ready" {
		if createErr := CreateSyncCredential(r.Context(), h.pool, app.ID); createErr == nil {
			ghClient, ghErr := h.buildClient(r.Context())
			if ghErr == nil {
				h.setupSync(r.Context(), ghClient, app)
			}
		}

		// T-10: Deploy step — create service on the connected provider.
		h.attemptDeploy(r.Context(), app, tmpl, body.Subdomain)
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

				// T-10: Best-effort deploy key revocation.
				if sc, scErr := GetSyncCredential(r.Context(), h.pool, app.ID); scErr == nil && sc.GithubKeyID != nil {
					_ = client.RevokeDeployKey(r.Context(), owner, repo, *sc.GithubKeyID)
				}
			}
		}
	}

	// T-12: Best-effort deploy service removal.
	if app.DeployServiceID != "" {
		if dcfg, dcfErr := GetDeployProviderConfig(r.Context(), h.pool); dcfErr == nil && dcfg != nil {
			if provider, provErr := buildDeployProvider(r.Context(), dcfg); provErr == nil {
				_ = provider.DeleteService(r.Context(), app.DeployServiceID)
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

func (h *FrontendAppsHandler) SyncStatus(w http.ResponseWriter, r *http.Request) {
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

	sc, err := GetSyncCredential(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sync_status":   sc.SyncStatus,
		"public_key":    sc.PublicKey,
		"error_message": sc.ErrorMessage,
	})
}

func (h *FrontendAppsHandler) RevealKey(w http.ResponseWriter, r *http.Request) {
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

	sc, err := GetSyncCredential(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if sc.SyncStatus != "ready" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no credential available to reveal"})
		return
	}

	privateKey, err := crypto.Decrypt(sc.PrivateKeyEncrypted)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"frontend_app_id": id})
	h.audit(r.Context(), user.ID, user.Email, "frontend_app.sync.reveal", "frontend_app_sync_credentials", sc.ID, "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"private_key": privateKey})
}

func (h *FrontendAppsHandler) SyncRetry(w http.ResponseWriter, r *http.Request) {
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

	sc, err := GetSyncCredential(r.Context(), h.pool, id)
	if err != nil || sc.SyncStatus == "ready" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sync already configured"})
		return
	}

	app, err := GetFrontendApp(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	client, err := h.buildClient(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "github not connected"})
		return
	}

	h.setupSync(r.Context(), client, app)

	meta, _ := json.Marshal(map[string]string{"frontend_app_id": id})
	h.audit(r.Context(), user.ID, user.Email, "frontend_app.sync.retry", "frontend_app_sync_credentials", sc.ID, "", meta, r.RemoteAddr)

	updated, _ := GetSyncCredential(r.Context(), h.pool, id)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"sync_status": "pending"})
	}
}

func (h *FrontendAppsHandler) SyncRegenerate(w http.ResponseWriter, r *http.Request) {
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

	sc, err := GetSyncCredential(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	app, err := GetFrontendApp(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	client, err := h.buildClient(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "github not connected"})
		return
	}

	// Best-effort: revoke old key if one exists.
	if sc.GithubKeyID != nil && app.GithubRepoURL != "" {
		if owner, repo := parseGitHubOwnerRepo(app.GithubRepoURL); owner != "" && repo != "" {
			_ = client.RevokeDeployKey(r.Context(), owner, repo, *sc.GithubKeyID)
		}
	}

	h.setupSync(r.Context(), client, app)

	meta, _ := json.Marshal(map[string]string{"frontend_app_id": id})
	h.audit(r.Context(), user.ID, user.Email, "frontend_app.sync.regenerate", "frontend_app_sync_credentials", sc.ID, "", meta, r.RemoteAddr)

	updated, _ := GetSyncCredential(r.Context(), h.pool, id)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"sync_status": "pending"})
	}
}

func (h *FrontendAppsHandler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	_ = InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)
}

func (h *FrontendAppsHandler) buildClient(ctx context.Context) (*github.Client, error) {
	cfg, err := GetGitHubConfig(ctx, h.pool)
	if err != nil || cfg == nil || cfg.InstallationID == nil {
		return nil, err
	}
	return github.NewClient(github.AppConfig{
		AppID:          cfg.AppID,
		InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
		PrivateKeyPEM:  []byte(cfg.PrivateKey),
		HTTPClient:     h.httpClient,
	}), nil
}

func (h *FrontendAppsHandler) setupSync(ctx context.Context, client *github.Client, app *FrontendApp) {
	owner, repo := parseGitHubOwnerRepo(app.GithubRepoURL)
	if owner == "" || repo == "" {
		_ = UpdateSyncCredentialFailure(ctx, h.pool, app.ID, "cannot parse owner/repo from github URL")
		return
	}

	pub, priv, err := sshkey.GenerateKeyPair()
	if err != nil {
		_ = UpdateSyncCredentialFailure(ctx, h.pool, app.ID, err.Error())
		return
	}

	encrypted, err := crypto.Encrypt(priv)
	if err != nil {
		_ = UpdateSyncCredentialFailure(ctx, h.pool, app.ID, "encryption failed")
		return
	}

	keyID, err := client.AddDeployKey(ctx, owner, repo, "zeep-orbit sync key", pub)
	if err != nil {
		_ = UpdateSyncCredentialFailure(ctx, h.pool, app.ID, err.Error())
		return
	}

	_ = UpdateSyncCredentialSuccess(ctx, h.pool, app.ID, keyID, pub, encrypted)
}

func buildDeployProvider(ctx context.Context, cfg *DeployProviderConfig) (deploy.DeployProvider, error) {
	apiKey, err := crypto.Decrypt(cfg.APIKey)
	if err != nil {
		return nil, fmt.Errorf("dashboard: decrypt api key: %w", err)
	}
	return render.NewRenderProvider(ctx, apiKey, cfg.RenderProjectID)
}

func (h *FrontendAppsHandler) attemptDeploy(ctx context.Context, app *FrontendApp, tmpl *GitHubTemplate, subdomain string) {
	if tmpl.RenderServiceType == "" {
		return
	}

	dcfg, err := GetDeployProviderConfig(ctx, h.pool)
	if err != nil || dcfg == nil {
		_, _ = UpdateFrontendAppDeploy(ctx, h.pool, app.ID, "", "", "failed", "deploy provider not connected")
		return
	}

	provider, err := buildDeployProvider(ctx, dcfg)
	if err != nil {
		_, _ = UpdateFrontendAppDeploy(ctx, h.pool, app.ID, "", "", "failed", err.Error())
		return
	}

	owner, repoName := parseGitHubOwnerRepo(app.GithubRepoURL)
	if owner == "" || repoName == "" {
		_, _ = UpdateFrontendAppDeploy(ctx, h.pool, app.ID, "", "", "failed", "cannot parse owner/repo from github url")
		return
	}

	params := deploy.CreateServiceParams{
		RepoOwner:    owner,
		RepoName:     repoName,
		ServiceType:  tmpl.RenderServiceType,
		BuildCommand: tmpl.BuildCommand,
		PublishPath:  tmpl.PublishPath,
		StartCommand: tmpl.StartCommand,
		EnvVars:      make(map[string]string),
	}

	info, err := provider.CreateService(ctx, params)
	if err != nil {
		_, _ = UpdateFrontendAppDeploy(ctx, h.pool, app.ID, "", "", "failed", err.Error())
		return
	}

	deployURL := info.URL
	customDomain := app.CustomDomain

	if customDomain != "" {
		deployURL = "https://" + customDomain
	} else if dcfg.BaseDomain != "" && subdomain != "" && info.ServiceID != "" {
		customDomain = subdomain + "." + dcfg.BaseDomain
		if rp, ok := provider.(*render.RenderProvider); ok {
			_ = rp.AddCustomDomain(ctx, info.ServiceID, customDomain)
		}
		deployURL = "https://" + customDomain
	}

	_, _ = UpdateFrontendAppDeploy(ctx, h.pool, app.ID, info.ServiceID, deployURL, "ready", "")
}

func (h *FrontendAppsHandler) DeployRetry(w http.ResponseWriter, r *http.Request) {
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

	if app.DeployStatus == "ready" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "deploy already configured"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template not available"})
		return
	}

	h.attemptDeploy(r.Context(), app, tmpl, "")

	meta, _ := json.Marshal(map[string]string{"frontend_app_id": id})
	h.audit(r.Context(), user.ID, user.Email, "frontend_app.deploy.retry", "frontend_app", app.ID, app.Name, meta, r.RemoteAddr)

	updated, _ := GetFrontendApp(r.Context(), h.pool, id)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"deploy_status": "failed"})
	}
}

func (h *FrontendAppsHandler) SetCustomDomain(w http.ResponseWriter, r *http.Request) {
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

	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var body struct {
		Subdomain string `json:"subdomain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Subdomain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "subdomain required"})
		return
	}

	app, err := GetFrontendApp(r.Context(), h.pool, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if app.DeployServiceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app has no deploy service"})
		return
	}

	dcfg, err := GetDeployProviderConfig(r.Context(), h.pool)
	if err != nil || dcfg == nil || dcfg.BaseDomain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "base domain not configured"})
		return
	}

	customDomain := body.Subdomain + "." + dcfg.BaseDomain
	deployURL := "https://" + customDomain

	provider, err := buildDeployProvider(r.Context(), dcfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if rp, ok := provider.(*render.RenderProvider); ok {
		if err := rp.AddCustomDomain(r.Context(), app.DeployServiceID, customDomain); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}

	if err := UpdateFrontendAppDomain(r.Context(), h.pool, app.ID, customDomain, deployURL); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"custom_domain": customDomain,
		"deploy_url":    deployURL,
	})
}
