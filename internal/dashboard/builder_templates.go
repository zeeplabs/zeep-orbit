package dashboard

// builder_templates.go — base templates of the AI Frontend Builder
// (ai-frontend-builder T23, AFB-26). The builder always offers its built-in
// default template and, in addition, every registered GitHub template a
// superadmin marked builder_enabled (the existing github_templates table; no
// new table).
//
// Routes (mounted by the router under /dashboard, both on
// *GitHubTemplatesHandler):
//
//	PATCH /api/github/templates/{id}/builder   SetBuilderEnabled      (superadmin)
//	GET   /api/builder/templates               ListBuilderTemplates   (any authenticated user)

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Built-in default template metadata. It must match
// builder/templates/default/orbit-template.json (the file that ships in the
// builder image and defines how the default template is built and published);
// the dashboard does not read that file, so a change there needs a change here.
const (
	builderTemplateKindDefault = "default"
	builderTemplateKindGitHub  = "github"

	builderDefaultTemplateName        = "Orbit default (React + Vite)"
	builderDefaultTemplateDescription = "React, Vite and TypeScript with the Orbit client preconfigured."
	builderDefaultTemplateFramework   = "react-vite"
	builderDefaultTemplateRenderType  = "static_site"
	builderDefaultTemplateBuildCmd    = "npm ci && npm run build"
	builderDefaultTemplatePublishPath = "dist"
)

// BuilderTemplate is one choice offered to a user starting a builder session.
// Kind "default" has an empty ID; kind "github" carries the github_templates id
// that POST /api/builder/sessions takes as template_id. The GitHub owner/repo
// are deliberately not exposed: users pick by name, the server resolves the
// repository.
type BuilderTemplate struct {
	Kind              string `json:"kind"`
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Framework         string `json:"framework"`
	RenderServiceType string `json:"render_service_type"`
	BuildCommand      string `json:"build_command"`
	PublishPath       string `json:"publish_path"`
}

func defaultBuilderTemplate() BuilderTemplate {
	return BuilderTemplate{
		Kind:              builderTemplateKindDefault,
		Name:              builderDefaultTemplateName,
		Description:       builderDefaultTemplateDescription,
		Framework:         builderDefaultTemplateFramework,
		RenderServiceType: builderDefaultTemplateRenderType,
		BuildCommand:      builderDefaultTemplateBuildCmd,
		PublishPath:       builderDefaultTemplatePublishPath,
	}
}

type builderEnabledRequest struct {
	// BuilderEnabled is a pointer so an absent field is a 400, not a silent
	// "false".
	BuilderEnabled *bool `json:"builder_enabled"`
}

// SetBuilderEnabled handles PATCH /dashboard/api/github/templates/{id}/builder.
// Superadmin only (ActionManageIntegrations, like the other builder config).
// Toggling does not verify the repository with GitHub: it only flips a flag on
// a template that was verified when it was registered.
func (h *GitHubTemplatesHandler) SetBuilderEnabled(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if !HasPlatformPermission(user.Role, ActionManageIntegrations) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	id := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body builderEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BuilderEnabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: builder_enabled (boolean) is required"})
		return
	}

	t, err := SetGitHubTemplateBuilderEnabled(r.Context(), h.pool, id, *body.BuilderEnabled)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	action := "github.template.builder_disable"
	if t.BuilderEnabled {
		action = "github.template.builder_enable"
	}
	h.audit(r.Context(), user.ID, user.Email, action, "github_template", t.ID, t.Name, nil, r.RemoteAddr)

	writeJSON(w, http.StatusOK, t)
}

// ListBuilderTemplates handles GET /dashboard/api/builder/templates. Any
// authenticated user may call it (it feeds the "new session" picker): the
// built-in default template first, then the active GitHub templates marked
// builder_enabled, by name.
func (h *GitHubTemplatesHandler) ListBuilderTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFromContext(r.Context()); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	enabled, err := ListBuilderEnabledGitHubTemplates(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	out := make([]BuilderTemplate, 0, len(enabled)+1)
	out = append(out, defaultBuilderTemplate())
	for _, t := range enabled {
		out = append(out, BuilderTemplate{
			Kind:              builderTemplateKindGitHub,
			ID:                t.ID,
			Name:              t.Name,
			Description:       t.Description,
			Framework:         t.Framework,
			RenderServiceType: t.RenderServiceType,
			BuildCommand:      t.BuildCommand,
			PublishPath:       t.PublishPath,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
