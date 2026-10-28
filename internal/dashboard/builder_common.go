package dashboard

// builder_common.go — shared plumbing of the AI Frontend Builder's session
// endpoints (ai-frontend-builder T24/T25 and every later builder handler:
// messages/events T26, proposals T27, preview T29, publish T32, ...).
//
// One struct, BuilderHandler, carries the dependencies; the per-route files
// add methods to it. The helpers here are the stable API those files share:
//
//	NewBuilderHandler          the single constructor (server wiring, T30)
//	h.configured()             store + sandbox are wired
//	h.requireUser(w, r)        401 when there is no authenticated user
//	h.requireBuilder(w, r)     requireUser + 503 {"error":"builder not configured"}
//	h.ownedSession(w, r, allowArchived)
//	                           requireBuilder + the {id} session of the caller;
//	                           a missing, foreign or (by default) archived
//	                           session is a 404, never a 403
//	h.availability(ctx)        the status computation (settings, provider, health)
//	h.requireAvailable(w, r)   503 {"error":"builder unavailable"} when it is not
//	h.writeError(w, status, msg)
//	h.serverError(w, r, err)   logs err, answers a fixed 500 (never err.Error())
//	h.audit(ctx, user, ...)    best-effort audit row
//	newBuilderSessionView / newBuilderTurnView   API views of store rows
//
// Error strings are English and generic (AGENTS.md §4): nothing from a Go
// error, a builder response or a provider ever reaches a response body.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Public error messages of the builder routes.
const (
	// builderNotConfiguredMsg: the deployment has no builder wired (BUILDER_URL
	// unset), so every builder route except status answers 503 with it.
	builderNotConfiguredMsg = "builder not configured"
	// builderUnavailableMsg: the builder is wired but cannot serve right now
	// (disabled by the superadmin, provider missing, builder down).
	builderUnavailableMsg     = "builder unavailable"
	builderSessionNotFoundMsg = "session not found"
	builderInternalErrorMsg   = "internal error"
)

// defaultBuilderHealthTimeout bounds the builder Health probe behind the
// status endpoint and the create-session gate.
const defaultBuilderHealthTimeout = 3 * time.Second

// BuilderHandlerConfig are the optional/injected dependencies of
// BuilderHandler. Leave Store or Sandbox nil to run with the builder "not
// configured": status still answers, every other route is a 503.
//
// Pass a nil interface (not a typed nil pointer) for Sandbox when BUILDER_URL
// is empty.
type BuilderHandlerConfig struct {
	// Store is the builder persistence layer (internal/agent/store).
	Store *store.Store
	// Sandbox is the Orbit-side client of the builder daemon. Wrap it with
	// sandbox.NewRestoring if a handler should survive builder volume loss.
	Sandbox sandbox.Sandbox
	// Logger is optional (a no-op logger when nil).
	Logger *zap.Logger
	// Clock is optional (time.Now when nil). Handlers that mint or check
	// expiring tokens (preview, T29) read time through it.
	Clock func() time.Time
	// PublicBaseURL, when set, is the externally visible base URL written into
	// a new project's src/orbit.config.ts. Empty means: ORBIT_PUBLIC_URL, else
	// derived from the request (same policy as the OAuth metadata document).
	PublicBaseURL string
	// GitHubHTTPClient overrides the HTTP client of the GitHub client used to
	// download company templates. nil in production; tests inject a mock
	// transport.
	GitHubHTTPClient *http.Client
	// HealthTimeout bounds one builder Health probe (default 3s).
	HealthTimeout time.Duration
	// Poke, when set, wakes this replica's turn worker (agent.Worker.Poke) so a
	// just-created turn starts without waiting for the next poll. Optional:
	// without it (or on a replica without a worker) turns start on the next
	// poll of whichever replica runs one.
	Poke func()
	// Dashboard is the dashboard Handler whose *ForUser schema paths apply a
	// confirmed proposal (T27, origin "ai_builder"). Without it the proposal
	// confirm route answers 503 "builder not configured".
	Dashboard *Handler
	// PreviewSigner mints and verifies the iframe preview tokens (T29); in
	// production a *builder.Signer built from BUILDER_SHARED_SECRET. Without it
	// the preview routes answer 503 "builder not configured".
	PreviewSigner PreviewTokenSigner
	// DeployProviderFactory overrides how publish (T32) builds the deploy
	// provider. nil in production (the Render provider); tests inject a fake.
	DeployProviderFactory DeployProviderFactory
}

// BuilderHandler serves the /dashboard/api/builder/sessions* routes and the
// builder status route. Build one with NewBuilderHandler.
type BuilderHandler struct {
	pool          *db.Pool
	store         *store.Store
	sandbox       sandbox.Sandbox
	logger        *zap.Logger
	now           func() time.Time
	publicBaseURL string
	githubHTTP    *http.Client
	healthTimeout time.Duration
	poke          func()
	dashboard     *Handler
	previewSigner PreviewTokenSigner
	deployFactory DeployProviderFactory
}

// NewBuilderHandler builds the handler. pool is the zeep_system pool (settings,
// provider rows, apps, RBAC, audit). It is the only constructor server wiring
// needs; see BuilderHandlerConfig for the optional parts.
func NewBuilderHandler(pool *db.Pool, cfg BuilderHandlerConfig) *BuilderHandler {
	h := &BuilderHandler{
		pool:          pool,
		store:         cfg.Store,
		sandbox:       cfg.Sandbox,
		logger:        cfg.Logger,
		now:           cfg.Clock,
		publicBaseURL: cfg.PublicBaseURL,
		githubHTTP:    cfg.GitHubHTTPClient,
		healthTimeout: cfg.HealthTimeout,
		poke:          cfg.Poke,
		dashboard:     cfg.Dashboard,
		previewSigner: cfg.PreviewSigner,
		deployFactory: cfg.DeployProviderFactory,
	}
	if h.logger == nil {
		h.logger = zap.NewNop()
	}
	if h.now == nil {
		h.now = time.Now
	}
	if h.healthTimeout <= 0 {
		h.healthTimeout = defaultBuilderHealthTimeout
	}
	return h
}

// configured reports whether the builder is wired at all.
func (h *BuilderHandler) configured() bool {
	return h.pool != nil && h.store != nil && h.sandbox != nil
}

func (h *BuilderHandler) writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// serverError logs err (server side only) and answers a fixed 500.
func (h *BuilderHandler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("builder request failed",
		zap.String("method", r.Method),
		zap.String("path", RedactPreviewTokenPath(r.URL.Path)),
		zap.Error(err),
	)
	h.writeError(w, http.StatusInternalServerError, builderInternalErrorMsg)
}

// requireUser resolves the authenticated user, answering 401 itself.
func (h *BuilderHandler) requireUser(w http.ResponseWriter, r *http.Request) (*DashboardUser, bool) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return user, true
}

// requireBuilder is requireUser plus the "builder wired" gate: without a store
// and sandbox the route answers 503 {"error":"builder not configured"}.
func (h *BuilderHandler) requireBuilder(w http.ResponseWriter, r *http.Request) (*DashboardUser, bool) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if !h.configured() {
		h.writeError(w, http.StatusServiceUnavailable, builderNotConfiguredMsg)
		return nil, false
	}
	return user, true
}

// ownedSession resolves the session named by the {id} URL parameter for the
// calling user. A malformed id, a missing session, another user's session and
// an archived session (unless allowArchived) are all the same 404, so a
// session id reveals nothing about other users. It writes the response itself
// and returns ok=false in those cases.
//
// Access is owner-only (design: Session access is owner-only); losing access to
// the backend app does NOT hide the session from its owner, so they can still
// read and delete it. The agent's tools fail closed on their own.
func (h *BuilderHandler) ownedSession(w http.ResponseWriter, r *http.Request, allowArchived bool) (*DashboardUser, *store.Session, bool) {
	user, ok := h.requireBuilder(w, r)
	if !ok {
		return nil, nil, false
	}
	id := chi.URLParam(r, "id")
	if !looksLikeUUID(id) {
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
		return nil, nil, false
	}
	sess, err := h.store.GetSession(r.Context(), user.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
			return nil, nil, false
		}
		h.serverError(w, r, err)
		return nil, nil, false
	}
	if sess.Status == store.SessionArchived && !allowArchived {
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
		return nil, nil, false
	}
	return user, sess, true
}

// requireAvailable answers 503 {"error":"builder unavailable"} unless the
// builder can take new work (see availability). The raw reason is logged, not
// returned. It returns the builder's health report on success.
func (h *BuilderHandler) requireAvailable(w http.ResponseWriter, r *http.Request) (sandbox.Health, bool) {
	st, health, err := h.availability(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return sandbox.Health{}, false
	}
	if !st.Available {
		h.logger.Info("builder unavailable", zap.String("reason", st.Reason))
		h.writeError(w, http.StatusServiceUnavailable, builderUnavailableMsg)
		return sandbox.Health{}, false
	}
	return health, true
}

// audit writes a best-effort audit row (failures never fail the request).
func (h *BuilderHandler) audit(ctx context.Context, user *DashboardUser, action, resourceID, resourceName string, metadata any, ip string) {
	var raw json.RawMessage
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			raw = b
		}
	}
	if err := InsertAuditLog(ctx, h.pool, user.ID, user.Email, action, "builder_session", resourceID, resourceName, raw, ip); err != nil {
		h.logger.Warn("builder audit log failed", zap.String("action", action), zap.Error(err))
	}
}

// decodeBuilderBody decodes a small JSON request body into dst (max bytes),
// answering 413/400 itself. Unknown fields are ignored.
func (h *BuilderHandler) decodeBuilderBody(w http.ResponseWriter, r *http.Request, dst any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			h.writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// BuilderSessionView is the API shape of a session. It carries no chat content
// and no tokens of any kind.
type BuilderSessionView struct {
	ID              string    `json:"id"`
	BackendAppID    string    `json:"backend_app_id"`
	Title           string    `json:"title"`
	TemplateKind    string    `json:"template_kind"`
	TemplateID      *string   `json:"template_id"`
	TemplateVersion string    `json:"template_version"`
	PromptVersionID *string   `json:"prompt_version_id"`
	Locale          string    `json:"locale"`
	Status          string    `json:"status"`
	HeadSHA         string    `json:"head_sha"`
	HeadTurnSeq     int       `json:"head_turn_seq"`
	FrontendAppID   *string   `json:"frontend_app_id"`
	BackendOrigin   string    `json:"backend_origin"`
	PublishedSHA    string    `json:"published_sha"`
	TokensIn        int64     `json:"tokens_in"`
	TokensOut       int64     `json:"tokens_out"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func newBuilderSessionView(s *store.Session) BuilderSessionView {
	return BuilderSessionView{
		ID: s.ID, BackendAppID: s.BackendAppID, Title: s.Title,
		TemplateKind: s.TemplateKind, TemplateID: s.TemplateID, TemplateVersion: s.TemplateVersion,
		PromptVersionID: s.PromptVersionID, Locale: s.Locale, Status: s.Status,
		HeadSHA: s.HeadSHA, HeadTurnSeq: s.HeadTurnSeq,
		FrontendAppID: s.FrontendAppID, BackendOrigin: s.BackendOrigin, PublishedSHA: s.PublishedSHA,
		TokensIn: s.TokensIn, TokensOut: s.TokensOut,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// BuilderTurnView is the API shape of a turn. PendingProposal is set only while
// the turn awaits confirmation (see builder_proposals.go).
type BuilderTurnView struct {
	ID              string               `json:"id"`
	Seq             int                  `json:"seq"`
	Kind            string               `json:"kind"`
	Status          string               `json:"status"`
	CommitSHA       string               `json:"commit_sha"`
	ErrorCode       *string              `json:"error_code"`
	UserMessage     string               `json:"user_message,omitempty"`
	PendingProposal *BuilderProposalView `json:"pending_proposal,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	FinishedAt      *time.Time           `json:"finished_at"`
}

func newBuilderTurnView(t *store.Turn) BuilderTurnView {
	return BuilderTurnView{
		ID: t.ID, Seq: t.Seq, Kind: t.Kind, Status: t.Status, CommitSHA: t.CommitSHA,
		ErrorCode: t.ErrorCode, UserMessage: t.UserMessage, PendingProposal: newBuilderProposalView(t),
		CreatedAt: t.CreatedAt, FinishedAt: t.FinishedAt,
	}
}
