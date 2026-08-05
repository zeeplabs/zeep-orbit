package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/deploy/render"
)

type DeployProviderConfigHandler struct {
	pool *db.Pool
}

func NewDeployProviderConfigHandler(pool *db.Pool) *DeployProviderConfigHandler {
	return &DeployProviderConfigHandler{pool: pool}
}

type deployProviderConfigBody struct {
	APIKey              string `json:"api_key"`
	RenderProjectID     string `json:"render_project_id"`
	RenderEnvironmentID string `json:"render_environment_id"`
	BaseDomain          string `json:"base_domain"`
}

func (h *DeployProviderConfigHandler) Status(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	cfg, err := GetDeployProviderConfig(r.Context(), h.pool)
	if err != nil || cfg == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"connected": false, "provider": "render"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connected":             true,
		"provider":              cfg.Provider,
		"render_project_id":     cfg.RenderProjectID,
		"render_environment_id": cfg.RenderEnvironmentID,
		"base_domain":           cfg.BaseDomain,
	})
}

func (h *DeployProviderConfigHandler) UpsertConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var body deployProviderConfigBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key required"})
		return
	}

	if err := render.ValidateAPIKey(r.Context(), body.APIKey); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid api key: " + err.Error()})
		return
	}

	encrypted, err := crypto.Encrypt(body.APIKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if err := UpsertDeployProviderConfig(r.Context(), h.pool, "render", encrypted, body.RenderProjectID, body.RenderEnvironmentID, body.BaseDomain); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"provider": "render"})
	h.audit(r.Context(), user.ID, user.Email, "deploy_provider.config.update", "deploy_provider_config", "", "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

const (
	recentDeploysAppLimit    = 15
	recentDeploysPerAppLimit = 3
	recentDeploysTotalLimit  = 10
	recentDeploysTimeout     = 5 * time.Second
)

var recentDeployStatuses = []string{"live", "build_failed", "update_failed", "canceled"}

// deployLister is the subset of *render.Client the aggregator needs, narrowed
// to an interface so tests can inject a fake instead of calling Render.
type deployLister interface {
	ListDeploys(ctx context.Context, serviceID string, limit int, statuses []string) ([]render.Deploy, error)
}

type recentDeployItem struct {
	AppName string `json:"appName"`
	Status  string `json:"status"`
	Time    string `json:"time"`
}

// RecentDeploys aggregates the most recent deploys across every frontend app
// with a Render service already created — no persistence, fetched live on
// every call. A failure fetching one app's deploys is logged and that app is
// skipped; it never fails the whole request.
func (h *DeployProviderConfigHandler) RecentDeploys(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	cfg, err := GetDeployProviderConfig(r.Context(), h.pool)
	if err != nil || cfg == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"deploys": []recentDeployItem{}})
		return
	}

	apiKey, err := crypto.Decrypt(cfg.APIKey)
	if err != nil {
		log.Printf("deploy provider: decrypt api key: %v", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{"deploys": []recentDeployItem{}})
		return
	}

	apps, err := ListWithDeployService(r.Context(), h.pool, recentDeploysAppLimit)
	if err != nil {
		log.Printf("deploy provider: list apps with deploy service: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), recentDeploysTimeout)
	defer cancel()

	items := aggregateRecentDeploys(ctx, render.NewClient(apiKey), apps, recentDeploysTotalLimit)
	writeJSON(w, http.StatusOK, map[string]interface{}{"deploys": items})
}

type appDeploy struct {
	appName string
	deploy  render.Deploy
}

func aggregateRecentDeploys(ctx context.Context, client deployLister, apps []DeployedApp, total int) []recentDeployItem {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []appDeploy
	)

	for _, app := range apps {
		wg.Add(1)
		go func(app DeployedApp) {
			defer wg.Done()
			deploys, err := client.ListDeploys(ctx, app.DeployServiceID, recentDeploysPerAppLimit, recentDeployStatuses)
			if err != nil {
				log.Printf("deploy provider: list deploys for app %s: %v", app.ID, err)
				return
			}
			mu.Lock()
			for _, d := range deploys {
				results = append(results, appDeploy{appName: app.Name, deploy: d})
			}
			mu.Unlock()
		}(app)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].deploy.CreatedAt.After(results[j].deploy.CreatedAt)
	})
	if len(results) > total {
		results = results[:total]
	}

	items := make([]recentDeployItem, 0, len(results))
	for _, res := range results {
		items = append(items, recentDeployItem{
			AppName: res.appName,
			Status:  mapDeployStatus(res.deploy.Status),
			Time:    relativeTime(res.deploy.CreatedAt),
		})
	}
	return items
}

func mapDeployStatus(status string) string {
	if status == "live" {
		return "Live"
	}
	return "Failed"
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (h *DeployProviderConfigHandler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	_ = InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)
}

type deployProviderFieldsBody struct {
	APIKey              string `json:"api_key"`
	RenderProjectID     string `json:"render_project_id"`
	RenderEnvironmentID string `json:"render_environment_id"`
	BaseDomain          string `json:"base_domain"`
}

func (h *DeployProviderConfigHandler) UpdateFields(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body deployProviderFieldsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	var apiKeyEncrypted string
	if body.APIKey != "" {
		if err := render.ValidateAPIKey(r.Context(), body.APIKey); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid api key: " + err.Error()})
			return
		}
		encrypted, err := crypto.Encrypt(body.APIKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		apiKeyEncrypted = encrypted
	} else {
		cfg, err := GetDeployProviderConfig(r.Context(), h.pool)
		if err != nil || cfg == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no existing config — provide an api_key"})
			return
		}
		apiKeyEncrypted = cfg.APIKey
	}

	if err := UpsertDeployProviderConfig(r.Context(), h.pool, "render", apiKeyEncrypted, body.RenderProjectID, body.RenderEnvironmentID, body.BaseDomain); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"provider": "render"})
	h.audit(r.Context(), user.ID, user.Email, "deploy_provider.config.update", "deploy_provider_config", "", "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
