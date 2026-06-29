package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// contextKey is the type for context keys in this package.
type contextKey int

const appContextKey contextKey = 0

// AppFromContext retrieves the *registry.App injected by middleware.
func AppFromContext(ctx context.Context) (*registry.App, bool) {
	app, ok := ctx.Value(appContextKey).(*registry.App)
	return app, ok
}

// Used for /{app}/auth/logout, /me, and PUT /me.
func AuthJWTMiddleware(reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appName := chi.URLParam(r, "app")
			app, ok := reg.Get(appName)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")

			claims, err := auth.ParseJWT([]byte(app.Config.Auth.JWTSecret), rawToken)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			ctx := auth.WithUser(r.Context(), &auth.AuthUser{
				ID:    claims.Subject,
				Email: claims.Email,
				App:   claims.App,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Returns 401 {"error": "unauthorized"} on any failure.
func JWTMiddleware(reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appName := chi.URLParam(r, "app")
			if appName == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			app, ok := reg.Get(appName)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")
			if rawToken == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			secret := []byte(app.Config.Auth.JWTSecret)
			token, err := jwtlib.Parse(
				rawToken,
				func(t *jwtlib.Token) (any, error) {
					return secret, nil
				},
				jwtlib.WithValidMethods([]string{"HS256"}),
			)
			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			ctx := r.Context()
			if authClaims, err := auth.ParseJWT(secret, rawToken); err == nil && authClaims.Subject != "" {
				ctx = auth.WithUser(ctx, &auth.AuthUser{
					ID:    authClaims.Subject,
					Email: authClaims.Email,
					App:   authClaims.App,
				})
			}
			ctx = context.WithValue(ctx, appContextKey, app)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
