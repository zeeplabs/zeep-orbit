package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/docs"
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

// Server encapsula o http.Server e suas dependências.
type Server struct {
	httpServer *http.Server
	reg        *registry.Registry
	logger     *zap.Logger
}

// New cria um Server com router configurado e pronto para Start.
func New(reg *registry.Registry, pool *db.Pool, port int) (*Server, error) {
	logger, err := buildLogger()
	if err != nil {
		return nil, fmt.Errorf("server: failed to build logger: %w", err)
	}

	h := NewHandler(pool, reg)
	dashH := dashboard.NewHandler(pool, reg)
	r := newRouter(reg, h, pool, logger, dashH)

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

// Start bloqueia até receber SIGINT ou SIGTERM, depois faz graceful shutdown (30s).
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

// buildLogger cria logger zap conforme LOG_LEVEL.
func buildLogger() (*zap.Logger, error) {
	if os.Getenv("LOG_LEVEL") == "debug" {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

// newRouter monta o chi.Mux com todas as rotas e middlewares.
func newRouter(reg *registry.Registry, h *Handler, pool *db.Pool, logger *zap.Logger, dashH *dashboard.Handler) *chi.Mux {
	logBuf := dashH.Logs
	r := chi.NewRouter()

	// Middleware stack global
	r.Use(logMiddleware(logger, logBuf))
	r.Use(chimiddleware.Recoverer)

	// Rotas sem JWT
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	r.Get("/health", h.HandleHealth)
	r.Handle("/metrics", promhttp.Handler())

	dh := docs.NewHandler(reg)
	r.Get("/docs/", dh.HandleIndex)
	r.Get("/docs/{app}", dh.HandleUI)
	r.Get("/docs/{app}/openapi.json", dh.HandleSpec)

	// Dashboard — deve vir antes dos wildcards /{app}
	authLimiter := dashboard.NewRateLimiter(5, time.Minute)
	r.Route("/dashboard", func(r chi.Router) {
		r.Use(dashboard.SecurityHeaders)
		r.Get("/api/config", dashH.Config)
		r.Get("/api/bootstrap/status", dashH.BootstrapStatus)
		r.Get("/api/config", dashH.Config)
		r.With(authLimiter.Middleware).Post("/api/bootstrap", dashH.Bootstrap)
		r.With(authLimiter.Middleware).Post("/api/login", dashH.Login)
		r.Post("/api/logout", dashH.Logout)
		r.With(dashboard.RequireAuth(pool)).Get("/api/me", dashH.Me)
		r.With(dashboard.RequireAuth(pool)).Put("/api/me/password", dashH.ChangeMyPassword)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps", dashH.ListApps)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps", dashH.CreateApp)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}", dashH.GetApp)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}", dashH.UpdateApp)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/apps/{id}", dashH.DeleteApp)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/users", dashH.ListAppUsers)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/users/{userId}/deactivate", dashH.DeactivateAppUser)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/users/{userId}/activate", dashH.ActivateAppUser)
		r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/users/{userId}/reset-sessions", dashH.ResetAppUserSessions)
		r.With(dashboard.RequireAuth(pool)).Get("/api/users", dashH.ListUsers)
		r.With(dashboard.RequireAuth(pool)).Post("/api/users", dashH.CreateUser)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/users/{id}", dashH.DeleteUser)
		r.With(dashboard.RequireAuth(pool)).Put("/api/users/{id}/password", dashH.ChangeUserPassword)
		r.With(dashboard.RequireAuth(pool)).Get("/api/logs", dashH.ListLogs)
		r.With(dashboard.RequireAuth(pool)).Get("/api/logs/metrics", dashH.LogsMetrics)
		r.With(dashboard.RequireAuth(pool)).Put("/api/config", dashH.UpdateConfig)
		r.With(dashboard.RequireAuth(pool)).Get("/api/config/auth/providers", dashH.ListAuthProviders)
		r.With(dashboard.RequireAuth(pool)).Get("/api/config/auth/providers/{provider}", dashH.GetAuthProvider)
		r.With(dashboard.RequireAuth(pool)).Put("/api/config/auth/providers/{provider}", dashH.UpsertAuthProvider)
		r.With(dashboard.RequireAuth(pool)).Get("/api/data-browser/apps", dashH.ListDataBrowserApps)
		r.With(dashboard.RequireAuth(pool)).Get("/api/data-browser/query", dashH.DataBrowserQuery)
		r.With(dashboard.RequireAuth(pool)).Get("/api/data-browser/export", dashH.DataBrowserExport)
		r.With(dashboard.RequireAuth(pool)).Post("/api/data-browser/row", dashH.DataBrowserCreate)
		r.With(dashboard.RequireAuth(pool)).Put("/api/data-browser/row", dashH.DataBrowserUpdate)
		r.With(dashboard.RequireAuth(pool)).Delete("/api/data-browser/row", dashH.DataBrowserDelete)
		r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/auth/providers", dashH.ListAppProviders)
		r.With(dashboard.RequireAuth(pool)).Put("/api/apps/{id}/auth/providers", dashH.UpdateAppProviders)
		r.Handle("/*", dashboard.StaticHandler())
	})

	// Google OAuth — rotas sempre montadas, config pode vir do DB ou env vars
	// O handler carrega a config lazymente de GetGoogleOAuthConfig()
	googleH := dashboard.NewGoogleOAuthHandler(pool, nil)
	r.Get("/dashboard/api/auth/google/login", googleH.Login)
	r.Get("/dashboard/api/auth/google/callback", googleH.Callback)

	// Auth nativo por app — deve vir antes das rotas CRUD wildcard
	ah := auth.New(pool, reg)
	appGoogleH := auth.NewAppGoogleHandler(pool, reg)
	r.Route("/{app}/auth", func(r chi.Router) {
		r.Get("/providers", appGoogleH.ListProviders)
		r.With(ah.RateLimit).Post("/register", ah.Register)
		r.With(ah.RateLimit).Post("/login", ah.Login)
		r.Post("/refresh", ah.Refresh)
		r.With(AuthJWTMiddleware(reg)).Post("/logout", ah.Logout)
		r.With(AuthJWTMiddleware(reg)).Get("/me", ah.Me)
		r.With(AuthJWTMiddleware(reg)).Put("/me", ah.UpdateMe)
		r.Get("/google/login", appGoogleH.Login)
		r.Get("/google/callback", appGoogleH.Callback)
	})

	// Rotas com JWT — grupo /{app}/{table}
	r.Route("/{app}/{table}", func(r chi.Router) {
		r.Use(JWTMiddleware(reg))
		r.Get("/", h.HandleList)
		r.Post("/", h.HandleCreate)
	})

	// Rotas com JWT — grupo /{app}/{table}/{id}
	r.Route("/{app}/{table}/{id}", func(r chi.Router) {
		r.Use(JWTMiddleware(reg))
		r.Get("/", h.HandleGetByID)
		r.Put("/", h.HandleUpdate)
		r.Patch("/", h.HandleUpdate)
		r.Delete("/", h.HandleDelete)
	})

	return r
}

// logMiddleware loga cada request com zap e alimenta o ring buffer do dashboard.
func logMiddleware(logger *zap.Logger, buf *dashboard.RingBuffer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			latency := time.Since(start)
			status := ww.Status()
			method := r.Method

			logger.Info("request",
				zap.String("method", method),
				zap.String("path", r.URL.Path),
				zap.Int("status", status),
				zap.Int64("latency_ms", latency.Milliseconds()),
			)

			buf.Push(dashboard.LogEntry{
				Timestamp: start,
				App:       dashboard.ExtractApp(r.URL.Path),
				Method:    method,
				Path:      r.URL.Path,
				Status:    status,
				LatencyMs: latency.Milliseconds(),
				UserAgent: r.UserAgent(),
			})

			statusStr := fmt.Sprintf("%d", status)
			httpRequestsTotal.WithLabelValues(method, statusStr).Inc()
			httpRequestDuration.WithLabelValues(method).Observe(latency.Seconds())
		})
	}
}
