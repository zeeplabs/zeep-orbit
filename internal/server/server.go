package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/docs"
	"github.com/zeeplabs/zeep-orbit/internal/mcpserver"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zeep_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "status"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "zeep_http_request_duration_seconds",
		Help:    "HTTP request duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	activeApps = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "zeep_active_apps",
		Help: "Number of active apps",
	})
)

func init() {
	prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, activeApps)
}

// Server wraps the http.Server and its dependencies.
type Server struct {
	httpServer *http.Server
	reg        *registry.Registry
	logger     *zap.Logger
}

// New creates a Server with a configured router ready for Start.
func New(reg *registry.Registry, pool *db.Pool, port int) (*Server, error) {
	logger, err := buildLogger()
	if err != nil {
		return nil, fmt.Errorf("server: failed to build logger: %w", err)
	}

	h := NewHandler(pool, reg)
	dashH := dashboard.NewHandler(pool, reg, logger)
	githubConfigH := dashboard.NewGitHubConfigHandler(pool)
	githubTemplatesH := dashboard.NewGitHubTemplatesHandler(pool)
	frontendAppsH := dashboard.NewFrontendAppsHandler(pool)
	deployProviderH := dashboard.NewDeployProviderConfigHandler(pool)
	r := newRouter(reg, h, pool, logger, dashH, githubConfigH, githubTemplatesH, frontendAppsH, deployProviderH)

	s := &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      r,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		reg:    reg,
		logger: logger,
	}

	return s, nil
}

// Router retorna o handler configurado — usado em testes sem Start().
func (s *Server) Router() http.Handler {
	return s.httpServer.Handler
}

// Start blocks until SIGINT or SIGTERM, then performs a graceful shutdown (30s).
func (s *Server) Start() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("server starting", zap.String("addr", s.httpServer.Addr))
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server: listen error: %w", err)
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server: shutdown error: %w", err)
	}

	s.logger.Info("server stopped gracefully")
	return nil
}

func buildLogger() (*zap.Logger, error) {
	if os.Getenv("LOG_LEVEL") == "debug" {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

// newRouter builds the chi.Mux with all routes and middleware.
func newRouter(reg *registry.Registry, h *Handler, pool *db.Pool, logger *zap.Logger, dashH *dashboard.Handler, githubConfigH *dashboard.GitHubConfigHandler, githubTemplatesH *dashboard.GitHubTemplatesHandler, frontendAppsH *dashboard.FrontendAppsHandler, deployProviderH *dashboard.DeployProviderConfigHandler) *chi.Mux {
	logBuf := dashH.Logs
	r := chi.NewRouter()

	r.Use(logMiddleware(logger, logBuf))
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		ExposedHeaders:   []string{"Link", "X-Truncated"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	r.Get("/health", h.HandleHealth)
	r.Handle("/metrics", promhttp.Handler())

	// Public, unauthenticated inbound webhook route: no dashboard session,
	// no end-user JWT, no {app} prefix. The token lives in the URL itself
	// (see design.md); registered with HandleFunc (not a verb-specific
	// method) since the webhook's configured HTTP method is a per-webhook
	// setting the handler itself checks, not a routing-level constraint.
	// Rate limited per webhookId (not per source IP): this service runs
	// multiple replicas behind a non-sticky load balancer, so remoteIP would
	// be the LB's address, sharing one budget across every tenant's webhooks
	// on that replica — a single noisy provider would 429 everyone else's
	// deliveries too. Keying by webhookId scopes the budget to one webhook
	// subscription instead — wired via SetRateLimiter (not router middleware)
	// so the handler can resolve {webhookId} against the database first and
	// key on the real wh.ID, instead of charging budget against whatever
	// string sits in the URL, existing or not (D-175).
	webhookH := NewWebhookHandler(pool, reg)
	webhookH.SetRateLimiter(dashboard.NewRateLimiter(120, time.Minute))
	r.HandleFunc("/hooks/{webhookId}/{token}", webhookH.HandleWebhookDelivery)

	// MCP transport (mcp-server spec): sibling to the /dashboard route
	// group, not nested inside it — it authenticates with a bearer PAT via
	// mcpserver.RequirePAT, not the zeep_session cookie the /dashboard
	// group's handlers use. Rate limited per-PAT-id (mcpserver.NewHandler),
	// same rationale as the webhook route above.
	mcpLimiter := dashboard.NewRateLimiter(120, time.Minute)
	r.Handle("/dashboard/mcp", mcpserver.NewHandler(pool, dashH, mcpLimiter))

	// OAuth 2.1 authorization-code-with-PKCE front door (mcp-server spec):
	// unauthenticated by nature (metadata discovery and registration
	// precede any credential). Registration is rate limited per-IP —
	// registration alone grants no data access, so there's no logical
	// caller identity to key by yet (design.md Risks & Concerns).
	oauthH := dashboard.NewOAuthHandler(pool)
	oauthRegisterLimiter := dashboard.NewRateLimiter(20, time.Minute)
	r.Get("/.well-known/oauth-authorization-server", oauthH.GetMetadata)
	r.With(oauthRegisterLimiter.Middleware).Post("/dashboard/oauth/register", oauthH.RegisterClient)
	r.Get("/dashboard/oauth/authorize", oauthH.Authorize)
	r.With(dashboard.RequireAuth(pool)).Post("/dashboard/oauth/authorize", oauthH.Decide)
	r.Post("/dashboard/oauth/token", oauthH.Token)

	dh := docs.NewHandler(reg)
	r.Get("/docs/", dh.HandleIndex)
	r.Get("/docs/{app}", dh.HandleUI)
	r.Get("/docs/{app}/openapi.json", dh.HandleSpec)

	authLimiter := dashboard.NewRateLimiter(5, time.Minute)
	// AI chat routes (build-chat and edit-chat) proxy to a single shared
	// OpenAI key/budget for the whole org, and each turn can cost multiple
	// model round-trips (tool calls). Unlike the dashboard's other
	// authenticated routes, an unlimited loop here has a real, direct cost —
	// so this one is rate limited per-user despite sitting behind
	// RequireAuth, not left at "authenticated is enough." Keyed by user ID
	// (not remoteIP): correct behind the non-sticky LB, and it means one
	// noisy user can't burn another user's budget by sharing an IP.
	aiChatLimiter := dashboard.NewRateLimiter(30, time.Minute)
	aiChatLimiterMW := aiChatLimiter.MiddlewareKeyedBy(func(r *http.Request) string {
		if user, ok := dashboard.UserFromContext(r.Context()); ok {
			return user.ID
		}
		return remoteIP(r)
	})
	r.Route("/dashboard", func(r chi.Router) {
		r.Use(dashboard.SecurityHeaders)
		r.Get("/api/config", dashH.Config)
		r.Get("/api/bootstrap/status", dashH.BootstrapStatus)
		r.With(authLimiter.Middleware).Post("/api/bootstrap", dashH.Bootstrap)
		r.With(authLimiter.Middleware).Post("/api/login", dashH.Login)
		r.Post("/api/logout", dashH.Logout)
		r.With(dashboard.RequireAuth(pool)).Get("/api/me", dashH.Me)
		r.With(dashboard.RequireAuth(pool)).Put("/api/me/password", dashH.ChangeMyPassword)
		r.With(dashboard.RequireAuth(pool)).Put("/api/me/language", dashH.SetLanguage)
		r.With(dashboard.RequireAuth(pool)).Post("/api/me/google-setup", dashH.CompleteGoogleSetup)
		r.With(dashboard.RequireAuth(pool)).Post("/api/me/pats", dashH.CreatePAT)
		r.With(dashboard.RequireAuth(pool)).Get("/api/me/pats", dashH.ListPATs)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/me/pats/{patId}", dashH.RevokePAT)
		r.With(dashboard.RequireAuth(pool)).Get("/api/oauth-clients", dashH.ListOAuthClients)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/oauth-clients/{clientId}", dashH.DeleteOAuthClient)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps", dashH.ListApps)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps", dashH.CreateApp)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}", dashH.GetApp)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}", dashH.UpdateApp)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}", dashH.DeleteApp)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/tables", dashH.CreateAppTable)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/tables/{tableId}", dashH.UpdateAppTable)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}/tables/{tableId}", dashH.DeleteAppTable)
		r.With(dashboard.RequireAuth(pool)).Patch("/api/apps/{id}/tables/{tableId}/columns/{columnName}/enum-values", dashH.UpdateColumnEnumValues)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/webhooks", dashH.ListWebhooks)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/webhooks", dashH.CreateWebhook)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/webhooks/{webhookId}", dashH.GetWebhook)
		r.With(dashboard.RequireAuth(pool)).Patch("/api/apps/{id}/webhooks/{webhookId}", dashH.UpdateWebhook)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}/webhooks/{webhookId}", dashH.DeleteWebhook)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/webhooks/{webhookId}/rotate-token", dashH.RotateWebhookToken)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/webhooks/{webhookId}/mappings", dashH.ListEventMappings)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/webhooks/{webhookId}/mappings", dashH.SaveEventMapping)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}/webhooks/{webhookId}/mappings/{mappingId}", dashH.DeleteEventMapping)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/webhooks/{webhookId}/activate", dashH.ActivateWebhook)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/webhooks/{webhookId}/deliveries", dashH.ListWebhookDeliveries)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/tables/{table}/policies", dashH.ListTablePolicies)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/tables/{table}/policies", dashH.CreateTablePolicy)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/tables/{table}/policies/{policyId}", dashH.UpdateTablePolicy)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}/tables/{table}/policies/{policyId}", dashH.DeleteTablePolicy)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/tokens", dashH.ListAppTokens)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/tokens", dashH.CreateAppToken)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/tokens/{tokenId}/revoke", dashH.RevokeAppToken)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/regenerate-secret", dashH.RegenerateAppSecret)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/secret", dashH.GetAppSecret)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/users", dashH.ListAppUsers)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/users/{userId}/deactivate", dashH.DeactivateAppUser)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/users/{userId}/activate", dashH.ActivateAppUser)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/users/{userId}", dashH.UpdateAppUser)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/roles", dashH.UpdateAppEnduserRoles)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/users/{userId}/reset-sessions", dashH.ResetAppUserSessions)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Get("/api/apps/{id}/ai/edit-chat", dashH.GetEditChatSession)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Post("/api/apps/{id}/ai/edit-chat", dashH.EditChatTurn)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Post("/api/apps/{id}/ai/edit-chat/{session_id}/confirm", dashH.EditChatConfirm)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Post("/api/apps/{id}/ai/edit-chat/restart", dashH.RestartEditChatSession)
		r.With(dashboard.RequireAuth(pool)).Get("/api/users", dashH.ListUsers)
		r.With(dashboard.RequireAuth(pool)).Post("/api/users", dashH.CreateUser)
		r.With(dashboard.RequireAuth(pool)).Patch("/api/users/{id}", dashH.UpdateUserRole)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/users/{id}", dashH.DeleteUser)
		r.With(dashboard.RequireAuth(pool)).Put("/api/users/{id}/password", dashH.ChangeUserPassword)
		r.With(dashboard.RequireAuth(pool)).Get("/api/logs", dashH.ListLogs)
		r.With(dashboard.RequireAuth(pool)).Get("/api/logs/metrics", dashH.LogsMetrics)
		r.With(dashboard.RequireAuth(pool)).Put("/api/config", dashH.UpdateConfig)
		r.With(dashboard.RequireAuth(pool)).Get("/api/config/auth/providers", dashH.ListAuthProviders)
		r.With(dashboard.RequireAuth(pool)).Get("/api/config/auth/providers/{provider}", dashH.GetAuthProvider)
		r.With(dashboard.RequireAuth(pool)).Put("/api/config/auth/providers/{provider}", dashH.UpsertAuthProvider)
		r.With(dashboard.RequireAuth(pool)).Get("/api/ai-providers/{provider}", dashH.GetAIProviderConfig)
		r.With(dashboard.RequireAuth(pool)).Put("/api/ai-providers/{provider}", dashH.UpsertAIProviderConfig)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Get("/api/ai/build-chat/session", dashH.GetBuildChatSession)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Post("/api/ai/build-chat", dashH.BuildChatTurn)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Post("/api/ai/build-chat/{session_id}/confirm", dashH.BuildChatConfirm)
		r.With(dashboard.RequireAuth(pool), aiChatLimiterMW).Post("/api/ai/build-chat/restart", dashH.RestartBuildChatSession)
		r.With(dashboard.RequireAuth(pool)).Get("/api/config/system", dashH.GetSystemConfig)
		r.With(dashboard.RequireAuth(pool)).Put("/api/config/system", dashH.UpdateSystemConfig)
		r.Get("/api/brand/config", dashH.GetPublicBrandConfig)
		r.With(dashboard.RequireAuth(pool)).Get("/api/audit-log", dashH.ListAuditLog)
		r.With(dashboard.RequireAuth(pool)).Get("/api/data-browser/apps", dashH.ListDataBrowserApps)
		r.With(dashboard.RequireAuth(pool)).Get("/api/data-browser/query", dashH.DataBrowserQuery)
		r.With(dashboard.RequireAuth(pool)).Get("/api/data-browser/export", dashH.DataBrowserExport)
		r.With(dashboard.RequireAuth(pool)).Post("/api/data-browser/row", dashH.DataBrowserCreate)
		r.With(dashboard.RequireAuth(pool)).Put("/api/data-browser/row", dashH.DataBrowserUpdate)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/data-browser/row", dashH.DataBrowserDelete)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/auth/providers", dashH.ListAppProviders)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/auth/providers", dashH.UpdateAppProviders)
		r.With(dashboard.RequireAuth(pool)).Get("/api/github/config", githubConfigH.GetConfig)
		r.With(dashboard.RequireAuth(pool)).Post("/api/github/config", githubConfigH.UpsertConfig)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/github/config", githubConfigH.DeleteConfig)
		r.With(dashboard.RequireAuth(pool)).Get("/api/github/status", githubConfigH.Status)
		r.With(dashboard.RequireAuth(pool)).Get("/api/github/install/start", githubConfigH.InstallStart)
		r.With(dashboard.RequireAuth(pool)).Get("/api/github/templates", githubTemplatesH.List)
		r.With(dashboard.RequireAuth(pool)).Post("/api/github/templates", githubTemplatesH.Create)
		r.With(dashboard.RequireAuth(pool)).Put("/api/github/templates/{id}", githubTemplatesH.Update)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/github/templates/{id}", githubTemplatesH.Delete)
		r.With(dashboard.RequireAuth(pool)).Put("/api/github/templates/{id}/active", githubTemplatesH.SetActive)
		r.With(dashboard.RequireAuth(pool)).Get("/api/frontend-apps", frontendAppsH.List)
		r.With(dashboard.RequireAuth(pool)).Get("/api/frontend-apps/{id}", frontendAppsH.Get)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps", frontendAppsH.Create)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps/{id}/retry", frontendAppsH.Retry)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/frontend-apps/{id}", frontendAppsH.Delete)
		r.With(dashboard.RequireAuth(pool)).Get("/api/frontend-apps/{id}/sync", frontendAppsH.SyncStatus)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps/{id}/reveal-key", frontendAppsH.RevealKey)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps/{id}/sync/retry", frontendAppsH.SyncRetry)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps/{id}/sync/regenerate", frontendAppsH.SyncRegenerate)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps/{id}/deploy/retry", frontendAppsH.DeployRetry)
		r.With(dashboard.RequireAuth(pool)).Put("/api/frontend-apps/{id}/custom-domain", frontendAppsH.SetCustomDomain)
		// Member management API. Four handlers
		// (`dashH.ListAppMembers`, `dashH.AddAppMember`,
		// `dashH.UpdateAppMember`, `dashH.RemoveAppMember`) are mounted
		// at both /api/apps/{id}/members and
		// /api/frontend-apps/{id}/members — the handler determines the
		// axis from the request URL path.
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/members", dashH.ListAppMembers)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/members", dashH.AddAppMember)
		r.With(dashboard.RequireAuth(pool)).Patch("/api/apps/{id}/members/{userId}", dashH.UpdateAppMember)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}/members/{userId}", dashH.RemoveAppMember)
		r.With(dashboard.RequireAuth(pool)).Get("/api/frontend-apps/{id}/members", dashH.ListAppMembers)
		r.With(dashboard.RequireAuth(pool)).Post("/api/frontend-apps/{id}/members", dashH.AddAppMember)
		r.With(dashboard.RequireAuth(pool)).Patch("/api/frontend-apps/{id}/members/{userId}", dashH.UpdateAppMember)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/frontend-apps/{id}/members/{userId}", dashH.RemoveAppMember)
		r.With(dashboard.RequireAuth(pool)).Get("/api/deploy-provider/status", deployProviderH.Status)
		r.With(dashboard.RequireAuth(pool)).Post("/api/deploy-provider/config", deployProviderH.UpsertConfig)
		r.With(dashboard.RequireAuth(pool)).Put("/api/deploy-provider/config", deployProviderH.UpdateFields)
		r.With(dashboard.RequireAuth(pool)).Get("/api/deploy-provider/recent-deploys", deployProviderH.RecentDeploys)
		r.With(dashboard.RequireAuth(pool)).Get("/api/changelog", dashboard.ChangelogHandler)
		r.With(dashboard.RequireAuth(pool)).Get("/api/version-check", dashboard.VersionCheckHandler)
		r.Handle("/*", dashboard.StaticHandler())
	})

	// Unauthenticated: GitHub redirects the superadmin's browser here
	// directly after installation, with no session cookie guaranteed.
	// CSRF protection is via the state token instead (see github_config.go).
	r.Get("/dashboard/api/github/install/callback", githubConfigH.InstallCallback)

	googleH := dashboard.NewGoogleOAuthHandler(pool, nil)
	r.Get("/dashboard/api/auth/google/login", googleH.Login)
	r.Get("/dashboard/api/auth/google/callback", googleH.Callback)

	ah := auth.New(pool, reg)
	appGoogleH := auth.NewAppGoogleHandler(pool, reg)
	r.Route("/{app}/auth", func(r chi.Router) {
		r.Get("/providers", appGoogleH.ListProviders)
		r.With(ah.RateLimit).Post("/register", ah.Register)
		r.With(ah.RateLimit).Post("/login", ah.Login)
		r.Post("/refresh", ah.Refresh)
		r.With(AuthJWTMiddleware(reg)).Post("/logout", ah.Logout)
		r.With(AuthJWTMiddleware(reg)).Get("/me", ah.Me)
		r.With(AuthJWTMiddleware(reg)).Patch("/me", ah.UpdateMe)
		r.With(ah.RateLimit).Post("/token/refresh", ah.TokenRefresh)
		r.Get("/google/login", appGoogleH.Login)
		r.Get("/google/callback", appGoogleH.Callback)
	})

	appLimiter := NewAppRateLimiter()
	appRateLimit := RateLimitMiddleware(appLimiter, reg)

	r.Route("/{app}/files", func(r chi.Router) {
		r.Use(appRateLimit)
		r.Use(JWTMiddleware(reg, pool))
		r.Post("/", h.HandleFileUpload)
		r.Get("/", h.HandleFileList)
		r.Get("/{id}", h.HandleFileGet)
		r.Get("/{id}/download", h.HandleFileDownload)
		r.Delete("/{id}", h.HandleFileDelete)
		r.Get("/{id}/url", h.HandleFileSignedURL)
	})

	r.Get("/{app}/health", h.HandleAppHealth)

	r.Route("/{app}/{table}", func(r chi.Router) {
		r.Use(appRateLimit)
		r.Use(JWTMiddleware(reg, pool))
		r.Get("/", h.HandleList)
		r.Post("/", h.HandleCreate)
	})

	r.Route("/{app}/{table}/{id}", func(r chi.Router) {
		r.Use(appRateLimit)
		r.Use(JWTMiddleware(reg, pool))
		r.Get("/", h.HandleGetByID)
		r.Put("/", h.HandleUpdate)
		r.Patch("/", h.HandleUpdate)
		r.Delete("/", h.HandleDelete)
	})

	return r
}

// logMiddleware logs each request with zap and feeds the dashboard ring buffer.
func logMiddleware(logger *zap.Logger, buf *dashboard.RingBuffer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			cw := &captureResponseWriter{ResponseWriter: w}

			var reqBody string
			if !isMultipart(r) {
				reqBody = readBody(r)
			}

			next.ServeHTTP(cw, r)

			latency := time.Since(start)
			status := cw.Status()
			method := r.Method
			contentType := r.Header.Get("Content-Type")
			logPath := redactWebhookToken(r.URL.Path)

			logger.Info("request",
				zap.String("method", method),
				zap.String("path", logPath),
				zap.Int("status", status),
				zap.Int64("latency_ms", latency.Milliseconds()),
			)

			entry := dashboard.LogEntry{
				Timestamp:   start,
				App:         dashboard.ExtractApp(r.URL.Path),
				Method:      method,
				Path:        logPath,
				Query:       r.URL.RawQuery,
				Status:      status,
				LatencyMs:   latency.Milliseconds(),
				UserAgent:   r.UserAgent(),
				RemoteAddr:  r.RemoteAddr,
				ContentType: contentType,
			}

			if isTextContent(contentType) && !isWebhookPath(r.URL.Path) && !isDashboardWebhookTokenPath(r.URL.Path) {
				if cw.body.Len() > 0 {
					entry.ResBody = cw.body.String()
				}
				if reqBody != "" {
					entry.ReqBody = reqBody
				}
			}

			buf.Push(entry)

			statusStr := fmt.Sprintf("%d", status)
			httpRequestsTotal.WithLabelValues(method, statusStr).Inc()
			httpRequestDuration.WithLabelValues(method).Observe(latency.Seconds())
		})
	}
}

func isWebhookPath(path string) bool {
	return strings.HasPrefix(path, "/hooks/")
}

// isDashboardWebhookTokenPath reports whether path is one of the dashboard's
// webhook subscription CRUD endpoints (list/create/get/update/delete/rotate)
// whose response body embeds the plaintext webhook token (toWebhookResponse)
// — never the mappings/deliveries sub-resources, which carry no token. These
// must never land in request/response log capture: a global auditor role can
// read /dashboard/api/logs despite webhookRBACGate denying it direct webhook
// access (this is the exact regression the B1 fix closed for the public
// /hooks/ route; dashboard API responses need the same exclusion).
func isDashboardWebhookTokenPath(path string) bool {
	parts := strings.Split(path, "/")
	// ["", "dashboard", "api", "apps", "{id}", "webhooks", ...]
	if len(parts) < 6 || parts[1] != "dashboard" || parts[2] != "api" || parts[3] != "apps" || parts[5] != "webhooks" {
		return false
	}
	switch len(parts) {
	case 6: // .../webhooks (list, create)
		return true
	case 7: // .../webhooks/{webhookId} (get, update, delete)
		return true
	case 8: // .../webhooks/{webhookId}/rotate-token
		return parts[7] == "rotate-token"
	default:
		return false
	}
}

func redactWebhookToken(path string) string {
	if !isWebhookPath(path) {
		return path
	}
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return path
	}
	parts[3] = "***"
	return strings.Join(parts, "/")
}

func isMultipart(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "multipart/")
}

func isTextContent(contentType string) bool {
	if contentType == "" {
		return true
	}
	base := strings.SplitN(contentType, ";", 2)[0]
	base = strings.TrimSpace(base)
	switch base {
	case "application/json", "text/plain", "text/html",
		"application/x-www-form-urlencoded", "application/xml",
		"text/xml", "application/yaml", "text/yaml",
		"application/graphql":
		return true
	}
	return false
}
