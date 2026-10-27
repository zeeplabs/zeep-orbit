package dashboard

// builder_handlers.go — builder status and session create/list/get/delete
// (ai-frontend-builder T24 + T25; AFB-03, AFB-04, AFB-11 access, edge case "app
// deleted / access lost"). Methods of *BuilderHandler (builder_common.go);
// the router mounts them under /dashboard, all behind RequireAuth:
//
//	GET    /api/builder/status         Status          any authenticated user
//	POST   /api/builder/sessions       CreateSession   >= viewer on the backend app
//	GET    /api/builder/sessions       ListSessions    owner only
//	GET    /api/builder/sessions/{id}  GetSession      owner only (+ turns)
//	DELETE /api/builder/sessions/{id}  DeleteSession   owner only
//
// Status answers even when no builder is wired. Every other route answers
// 503 {"error":"builder not configured"} in that case, and
// 503 {"error":"builder unavailable"} when the builder is wired but cannot take
// new work. Another user's session and an app the caller cannot see are 404,
// never 403.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/github"
)

// Reasons of GET /api/builder/status when available is false.
const (
	BuilderReasonNotConfigured         = "not_configured"          // BUILDER_URL unset: no builder wired
	BuilderReasonDisabled              = "disabled"                // builder_settings.enabled is false
	BuilderReasonProviderNotConfigured = "provider_not_configured" // the settings' LLM provider is unusable
	BuilderReasonBuilderUnreachable    = "builder_unreachable"     // builder Health failed
)

// Limits of the create-session request.
const (
	builderCreateBodyLimit = 16 << 10
	builderMaxTitleRunes   = 120
	builderMaxLocaleLen    = 35

	// builderInitTimeout bounds the whole create flow after validation
	// (template download, Init, snapshot).
	builderInitTimeout = 90 * time.Second
	// builderCleanupTimeout bounds best-effort cleanup after a failed create
	// and the Destroy of a deleted session.
	builderCleanupTimeout = 30 * time.Second

	// builderDefaultTemplateDir is the name of the template built into the
	// builder image (BUILDER_TEMPLATES_DIR/default).
	builderDefaultTemplateDir = "default"
)

// BuilderStatus is the body of GET /api/builder/status.
type BuilderStatus struct {
	// Available is true when new sessions can be created.
	Available bool `json:"available"`
	// ProviderConfigured reports whether the LLM provider chosen in the
	// builder settings is usable (row enabled, model, key or base URL). It is
	// computed even when the builder itself is unavailable, so the UI can show
	// what is still missing.
	ProviderConfigured bool `json:"provider_configured"`
	// Reason says why Available is false; empty when it is true.
	Reason string `json:"reason"`
}

// availability evaluates the builder gate in this order, cheapest first:
// not wired (not_configured) -> settings.enabled (disabled) -> provider row
// (provider_not_configured) -> builder Health (builder_unreachable). The error
// is only a database failure. Health is the builder's report, valid when
// Available.
func (h *BuilderHandler) availability(ctx context.Context) (BuilderStatus, sandbox.Health, error) {
	var st BuilderStatus
	if h.pool == nil {
		st.Reason = BuilderReasonNotConfigured
		return st, sandbox.Health{}, nil
	}
	settings, err := GetBuilderSettings(ctx, h.pool)
	if err != nil {
		return st, sandbox.Health{}, err
	}
	if _, perr := ResolveAIProviderRuntime(ctx, h.pool, settings.Provider); perr == nil {
		st.ProviderConfigured = true
	} else if !errors.Is(perr, ErrAIProviderUnavailable) {
		return st, sandbox.Health{}, perr
	}

	switch {
	case !h.configured():
		st.Reason = BuilderReasonNotConfigured
		return st, sandbox.Health{}, nil
	case !settings.Enabled:
		st.Reason = BuilderReasonDisabled
		return st, sandbox.Health{}, nil
	case !st.ProviderConfigured:
		st.Reason = BuilderReasonProviderNotConfigured
		return st, sandbox.Health{}, nil
	}

	hctx, cancel := context.WithTimeout(ctx, h.healthTimeout)
	defer cancel()
	health, herr := h.sandbox.Health(hctx)
	if herr != nil {
		h.logger.Warn("builder health check failed", zap.Error(herr))
		st.Reason = BuilderReasonBuilderUnreachable
		return st, sandbox.Health{}, nil
	}
	st.Available = true
	return st, health, nil
}

// Status handles GET /dashboard/api/builder/status for any authenticated user
// (AFB-03). It answers 200 whether or not a builder is wired.
func (h *BuilderHandler) Status(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireUser(w, r); !ok {
		return
	}
	st, _, err := h.availability(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type createBuilderSessionRequest struct {
	// BackendAppID is the Orbit backend app the project talks to.
	BackendAppID string `json:"backend_app_id"`
	// TemplateID is a github_templates id from GET /api/builder/templates;
	// empty selects the built-in default template.
	TemplateID string `json:"template_id"`
	// Title is optional; it defaults to the backend app's name.
	Title string `json:"title"`
	// Locale is the language of the user's UI (e.g. "pt-BR"); it defaults to
	// the first tag of Accept-Language, then "en". The agent replies in it.
	Locale string `json:"locale"`
}

var builderLocalePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,2}$`)

// normalizeBuilderLocale validates a locale tag and canonicalizes its case
// (language lower, 2-letter region upper). ok is false for anything else.
func normalizeBuilderLocale(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > builderMaxLocaleLen || !builderLocalePattern.MatchString(s) {
		return "", false
	}
	parts := strings.Split(s, "-")
	parts[0] = strings.ToLower(parts[0])
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) == 2 {
			parts[i] = strings.ToUpper(parts[i])
		}
	}
	return strings.Join(parts, "-"), true
}

// localeFromAcceptLanguage returns the first usable tag of an Accept-Language
// header, or "en".
func localeFromAcceptLanguage(header string) string {
	for _, part := range strings.Split(header, ",") {
		tag, _, _ := strings.Cut(part, ";")
		if loc, ok := normalizeBuilderLocale(tag); ok {
			return loc
		}
	}
	return "en"
}

// cleanBuilderTitle trims s and drops control characters.
func cleanBuilderTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// apiBaseURL is the Orbit API base URL written into the generated project:
// the configured public URL, else ORBIT_PUBLIC_URL, else derived from the
// request (requestBaseURL). It must be an absolute http(s) URL without
// credentials, query or fragment.
func (h *BuilderHandler) apiBaseURL(r *http.Request) (string, bool) {
	base := strings.TrimRight(strings.TrimSpace(h.publicBaseURL), "/")
	if base == "" {
		base = requestBaseURL(r)
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimRight(u.String(), "/"), true
}

// githubClient builds the GitHub client for template downloads; nil when the
// GitHub App is not installed.
func (h *BuilderHandler) githubClient(ctx context.Context) (*github.Client, error) {
	cfg, err := GetGitHubConfig(ctx, h.pool)
	if err != nil || cfg == nil || cfg.InstallationID == nil {
		return nil, err
	}
	return github.NewClient(github.AppConfig{
		AppID:          cfg.AppID,
		InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
		PrivateKeyPEM:  []byte(cfg.PrivateKey),
		HTTPClient:     h.githubHTTP,
	}), nil
}

// CreateSession handles POST /dashboard/api/builder/sessions (AFB-04).
//
// Order: authenticate, builder wired, validate the body, role on the backend
// app (>= viewer; no access is a 404), builder available, resolve the template
// (a failure here creates nothing), insert the session pinning the template and
// prompt versions and the locale, Init the workspace with the generated
// src/orbit.config.ts inside the turn 0 commit, store that commit as the
// initial snapshot. Any failure after the insert archives the session and
// destroys the workspace, so a failed create leaves nothing usable behind.
func (h *BuilderHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilder(w, r)
	if !ok {
		return
	}
	var req createBuilderSessionRequest
	if !h.decodeBuilderBody(w, r, &req, builderCreateBodyLimit) {
		return
	}

	req.BackendAppID = strings.TrimSpace(req.BackendAppID)
	if !looksLikeUUID(req.BackendAppID) {
		h.writeError(w, http.StatusBadRequest, "backend_app_id must be a valid app id")
		return
	}
	title := cleanBuilderTitle(req.Title)
	if utf8.RuneCountInString(title) > builderMaxTitleRunes {
		h.writeError(w, http.StatusBadRequest, "title is too long")
		return
	}
	locale := localeFromAcceptLanguage(r.Header.Get("Accept-Language"))
	if strings.TrimSpace(req.Locale) != "" {
		loc, valid := normalizeBuilderLocale(req.Locale)
		if !valid {
			h.writeError(w, http.StatusBadRequest, "invalid locale")
			return
		}
		locale = loc
	}
	templateID := strings.TrimSpace(req.TemplateID)

	// AFB-11: the caller needs at least viewer on the backend app. GetApp
	// answers ErrNotFound both for a missing app and for no access.
	app, _, err := GetApp(r.Context(), h.pool, req.BackendAppID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "app not found")
			return
		}
		h.serverError(w, r, err)
		return
	}

	health, ok := h.requireAvailable(w, r)
	if !ok {
		return
	}

	apiURL, ok := h.apiBaseURL(r)
	if !ok {
		h.serverError(w, r, errors.New("cannot determine the public API base URL for the generated project"))
		return
	}
	config := renderOrbitConfig(apiURL, app.Name)

	ctx, cancel := context.WithTimeout(r.Context(), builderInitTimeout)
	defer cancel()

	// Template resolution. For a company template the archive is downloaded
	// and its first entry read now, so an unusable template creates no row.
	params := store.CreateSessionParams{
		OwnerUserID:  user.ID,
		BackendAppID: app.ID,
		Title:        title,
		Locale:       locale,
	}
	if params.Title == "" {
		params.Title = app.Name
	}
	var tt *templateTar
	if templateID == "" {
		params.TemplateKind = store.TemplateDefault
		params.TemplateVersion = health.DefaultTemplateVersion
		if params.TemplateVersion == "" {
			params.TemplateVersion = "unknown"
		}
	} else {
		var tpl *GitHubTemplate
		tpl, tt, ok = h.resolveGitHubTemplate(ctx, w, r, templateID, config)
		if !ok {
			return
		}
		defer tt.Close()
		params.TemplateKind = store.TemplateGitHub
		params.TemplateID = &tpl.ID
		params.TemplateVersion = tt.Version
		if params.TemplateVersion == "" {
			params.TemplateVersion = "unknown"
		}
	}

	// Pin the persona + rules in force now; later edits do not change this
	// session.
	prompt, err := GetActiveBuilderPrompt(ctx, h.pool)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if prompt != nil {
		params.PromptVersionID = &prompt.ID
	}

	sess, err := h.store.CreateSession(ctx, params)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	res, err := h.initWorkspace(ctx, sess.ID, tt, config)
	if err != nil {
		h.abortNewSession(ctx, user.ID, sess.ID)
		h.createFailure(w, r, tt, err)
		return
	}
	if res.TemplateVersion != "" && res.TemplateVersion != sess.TemplateVersion && params.TemplateKind == store.TemplateDefault {
		h.logger.Warn("builder default template version changed during session create",
			zap.String("session_id", sess.ID),
			zap.String("recorded", sess.TemplateVersion), zap.String("actual", res.TemplateVersion))
	}
	if err := h.store.SaveInitialSnapshot(ctx, user.ID, sess.ID, res.SHA, res.Bundle); err != nil {
		h.abortNewSession(ctx, user.ID, sess.ID)
		h.serverError(w, r, err)
		return
	}

	created, err := h.store.GetSession(ctx, user.ID, sess.ID)
	if err != nil {
		h.abortNewSession(ctx, user.ID, sess.ID)
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newBuilderSessionView(created))

	h.audit(r.Context(), user, "builder.session.create", created.ID, created.Title,
		map[string]string{"backend_app_id": created.BackendAppID, "template_kind": created.TemplateKind}, r.RemoteAddr)
}

// resolveGitHubTemplate finds the builder-enabled company template, downloads
// its archive and starts the rewrite to a plain tar. It writes the error
// response itself (ok=false). The returned templateTar must be closed.
func (h *BuilderHandler) resolveGitHubTemplate(ctx context.Context, w http.ResponseWriter, r *http.Request, templateID string, config []byte) (*GitHubTemplate, *templateTar, bool) {
	notFound := func() {
		h.writeError(w, http.StatusNotFound, "template not found")
	}
	if !looksLikeUUID(templateID) {
		notFound()
		return nil, nil, false
	}
	enabled, err := ListBuilderEnabledGitHubTemplates(ctx, h.pool)
	if err != nil {
		h.serverError(w, r, err)
		return nil, nil, false
	}
	var tpl *GitHubTemplate
	for i := range enabled {
		if strings.EqualFold(enabled[i].ID, templateID) {
			tpl = &enabled[i]
			break
		}
	}
	if tpl == nil {
		notFound()
		return nil, nil, false
	}

	client, err := h.githubClient(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return nil, nil, false
	}
	if client == nil {
		h.logger.Warn("builder template requested but GitHub is not connected", zap.String("template_id", tpl.ID))
		h.writeError(w, http.StatusServiceUnavailable, "template repository unavailable")
		return nil, nil, false
	}
	rc, err := client.DownloadTarball(ctx, tpl.GitHubOwner, tpl.GitHubRepo, "")
	if err != nil {
		h.logger.Warn("builder template download failed", zap.String("template_id", tpl.ID), zap.Error(err))
		h.writeError(w, http.StatusBadGateway, "template repository unavailable")
		return nil, nil, false
	}
	tt, err := newTemplateTar(rc, true, true, builderConfigPath, config)
	if err != nil {
		_ = rc.Close()
		h.logger.Warn("builder template archive rejected", zap.String("template_id", tpl.ID), zap.Error(err))
		h.writeError(w, http.StatusUnprocessableEntity, "template could not be used")
		return nil, nil, false
	}
	tt.src = rc // closed with the tar producer
	return tpl, tt, true
}

// initWorkspace creates the session workspace from the template with config in
// the turn 0 commit. tt is the rewritten company template, or nil for the
// default template.
func (h *BuilderHandler) initWorkspace(ctx context.Context, sessionID string, tt *templateTar, config []byte) (sandbox.InitResult, error) {
	if tt != nil {
		return h.sandbox.Init(ctx, sessionID, sandbox.TemplateSource{Tar: tt})
	}

	// Default template: it lives in the builder image, so Orbit cannot add the
	// config to it before Init. Init it once, read the tree back, and Init again
	// from that tree plus the config, so turn 0 is one commit with one full
	// bundle (see builder_template_tar.go).
	first, err := h.sandbox.Init(ctx, sessionID, sandbox.TemplateSource{Name: builderDefaultTemplateDir})
	if err != nil {
		return sandbox.InitResult{}, err
	}
	rc, err := h.sandbox.Archive(ctx, sessionID, first.SHA)
	if err != nil {
		return sandbox.InitResult{}, err
	}
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxDefaultTemplateTarBytes+1))
	_ = rc.Close()
	if rerr != nil {
		return sandbox.InitResult{}, rerr
	}
	if len(raw) > maxDefaultTemplateTarBytes {
		return sandbox.InitResult{}, errTemplateTooLarge
	}
	if err := h.sandbox.Destroy(ctx, sessionID); err != nil {
		return sandbox.InitResult{}, err
	}
	tt2, err := newTemplateTar(bytes.NewReader(raw), false, false, builderConfigPath, config)
	if err != nil {
		return sandbox.InitResult{}, err
	}
	defer tt2.Close()
	res, err := h.sandbox.Init(ctx, sessionID, sandbox.TemplateSource{Tar: tt2})
	if err != nil {
		if terr := tt2.Err(); terr != nil {
			return sandbox.InitResult{}, terr
		}
		return sandbox.InitResult{}, err
	}
	return res, nil
}

// createFailure answers a failed workspace Init: an unusable template is a
// 422, everything else (builder down, at capacity, bad credentials, timeout) a
// generic 503. The cause is logged, never returned.
func (h *BuilderHandler) createFailure(w http.ResponseWriter, r *http.Request, tt *templateTar, err error) {
	h.logger.Error("builder session init failed",
		zap.String("method", r.Method), zap.String("path", r.URL.Path), zap.Error(err))

	unusable := errors.Is(err, errTemplateUnusable)
	if tt != nil {
		if terr := tt.Err(); terr != nil && errors.Is(terr, errTemplateUnusable) {
			unusable = true
		}
	}
	var se *sandbox.Error
	if errors.As(err, &se) && (se.Status == http.StatusBadRequest || se.Status == http.StatusRequestEntityTooLarge || se.Status == http.StatusUnprocessableEntity) {
		unusable = true
	}
	if unusable {
		h.writeError(w, http.StatusUnprocessableEntity, "template could not be used")
		return
	}
	h.writeError(w, http.StatusServiceUnavailable, builderUnavailableMsg)
}

// abortNewSession undoes a half-created session: it archives the row and
// destroys the workspace, both best effort and independent of the request's
// cancellation.
func (h *BuilderHandler) abortNewSession(ctx context.Context, ownerID, sessionID string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), builderCleanupTimeout)
	defer cancel()
	if err := h.store.ArchiveSession(cctx, ownerID, sessionID); err != nil {
		h.logger.Error("builder: archive failed session", zap.String("session_id", sessionID), zap.Error(err))
	}
	if err := h.sandbox.Destroy(cctx, sessionID); err != nil {
		h.logger.Error("builder: destroy workspace of failed session", zap.String("session_id", sessionID), zap.Error(err))
	}
}

// ListSessions handles GET /dashboard/api/builder/sessions: the caller's own,
// non-archived sessions, most recently updated first.
func (h *BuilderHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilder(w, r)
	if !ok {
		return
	}
	sessions, err := h.store.ListSessions(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	out := make([]BuilderSessionView, 0, len(sessions))
	for i := range sessions {
		out = append(out, newBuilderSessionView(&sessions[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// BuilderSessionDetail is the body of GET /api/builder/sessions/{id}.
type BuilderSessionDetail struct {
	BuilderSessionView
	Turns []BuilderTurnView `json:"turns"`
}

// GetSession handles GET /dashboard/api/builder/sessions/{id}: the session and
// its turns, owner only. It stays readable when the owner lost access to the
// backend app (nothing about the app is returned), so the owner can still see
// and delete it.
func (h *BuilderHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	turns, err := h.store.ListTurns(r.Context(), user.ID, sess.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
			return
		}
		h.serverError(w, r, err)
		return
	}
	detail := BuilderSessionDetail{BuilderSessionView: newBuilderSessionView(sess), Turns: make([]BuilderTurnView, 0, len(turns))}
	for i := range turns {
		detail.Turns = append(detail.Turns, newBuilderTurnView(&turns[i]))
	}
	writeJSON(w, http.StatusOK, detail)
}

// DeleteSession handles DELETE /dashboard/api/builder/sessions/{id}: archive,
// then destroy the workspace. The archive is what the user sees as deletion:
// if Destroy fails the session stays archived, the failure is logged, and the
// answer is still 204. Deleting an already archived session retries the
// Destroy, so a retry cleans a workspace left behind. Published frontend apps
// are untouched.
func (h *BuilderHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, true)
	if !ok {
		return
	}
	if err := h.store.ArchiveSession(r.Context(), user.ID, sess.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
			return
		}
		h.serverError(w, r, err)
		return
	}

	dctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), builderCleanupTimeout)
	defer cancel()
	if err := h.sandbox.Destroy(dctx, sess.ID); err != nil {
		h.logger.Error("builder: destroy workspace of archived session",
			zap.String("session_id", sess.ID), zap.Error(err))
	}
	w.WriteHeader(http.StatusNoContent)

	if sess.Status != store.SessionArchived {
		h.audit(r.Context(), user, "builder.session.delete", sess.ID, sess.Title,
			map[string]string{"backend_app_id": sess.BackendAppID}, r.RemoteAddr)
	}
}
