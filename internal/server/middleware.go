package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/zeep-tecnologia/zeep-core/internal/registry"
)

// contextKey é o tipo para chaves de contexto deste pacote.
type contextKey int

const appContextKey contextKey = 0

// AppFromContext recupera o *registry.App injetado pelo middleware.
func AppFromContext(ctx context.Context) (*registry.App, bool) {
	app, ok := ctx.Value(appContextKey).(*registry.App)
	return app, ok
}

// JWTMiddleware valida o token Bearer HS256 para o app da rota.
// Usa chi.URLParam(r, "app") para identificar o app.
// Injeta *registry.App no contexto se válido.
// Retorna 401 {"error": "unauthorized"} em qualquer falha.
func JWTMiddleware(reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Extrair app name do path param
			appName := chi.URLParam(r, "app")
			if appName == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			// 2. Lookup no registry
			app, ok := reg.Get(appName)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			// 3. Extrair Bearer token do header Authorization
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

			// 4. Validar HS256 + exp (se presente) com jwt_secret do app
			secret := []byte(app.Config.Auth.JWTSecret)
			token, err := jwtlib.Parse(
				rawToken,
				func(t *jwtlib.Token) (any, error) {
					return secret, nil
				},
				jwtlib.WithValidMethods([]string{"HS256"}),
			)
			if err != nil || !token.Valid {
				// 5. Nunca logar o secret — apenas retornar 401
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			// 6. Injetar app no contexto e chamar next
			ctx := context.WithValue(r.Context(), appContextKey, app)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
