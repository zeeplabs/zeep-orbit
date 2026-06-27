package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/zeeplabs/zeep-core/internal/config"
	"github.com/zeeplabs/zeep-core/internal/registry"
)

// buildRegistry cria um Registry com um único app de nome appName e secret dado.
func buildRegistry(appName, secret string) *registry.Registry {
	reg := registry.New()
	_ = reg.Load(&config.Config{
		Apps: []config.AppConfig{
			{
				Name: appName,
				Auth: config.AuthConfig{JWTSecret: secret},
			},
		},
	})
	return reg
}

// buildToken gera um token HS256 assinado com secret.
// Se expired for true, o token terá exp no passado.
func buildToken(secret string, expired bool) string {
	claims := jwtlib.MapClaims{}
	if expired {
		claims["exp"] = time.Now().Add(-1 * time.Hour).Unix()
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		panic("buildToken: " + err.Error())
	}
	return signed
}

// buildRouter monta um chi.Router mínimo com JWTMiddleware para appName.
func buildRouter(reg *registry.Registry) http.Handler {
	r := chi.NewRouter()
	r.With(JWTMiddleware(reg)).Get("/{app}/{table}", func(w http.ResponseWriter, r *http.Request) {
		app, ok := AppFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "app not in context")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"app": app.Config.Name})
	})
	return r
}

func TestMiddlewareValidToken(t *testing.T) {
	const appName = "myapp"
	const secret = "supersecret"

	reg := buildRegistry(appName, secret)
	router := buildRouter(reg)

	token := buildToken(secret, false)
	req := httptest.NewRequest(http.MethodGet, "/"+appName+"/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d", rec.Code)
	}
	// verifica que o app foi injetado no contexto
	if rec.Body.String() == "" {
		t.Fatal("body vazio")
	}
}

func TestMiddlewareExpiredToken(t *testing.T) {
	const appName = "myapp"
	const secret = "supersecret"

	reg := buildRegistry(appName, secret)
	router := buildRouter(reg)

	token := buildToken(secret, true)
	req := httptest.NewRequest(http.MethodGet, "/"+appName+"/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d", rec.Code)
	}
}

func TestMiddlewareWrongSecret(t *testing.T) {
	const appName = "myapp"

	reg := buildRegistry(appName, "correctsecret")
	router := buildRouter(reg)

	token := buildToken("wrongsecret", false)
	req := httptest.NewRequest(http.MethodGet, "/"+appName+"/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d", rec.Code)
	}
}

func TestMiddlewareNoHeader(t *testing.T) {
	const appName = "myapp"
	const secret = "supersecret"

	reg := buildRegistry(appName, secret)
	router := buildRouter(reg)

	req := httptest.NewRequest(http.MethodGet, "/"+appName+"/users", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d", rec.Code)
	}
}

func TestMiddlewareUnknownApp(t *testing.T) {
	// registry tem "myapp", mas rota pede "otherapp"
	reg := buildRegistry("myapp", "supersecret")
	router := buildRouter(reg)

	token := buildToken("supersecret", false)
	req := httptest.NewRequest(http.MethodGet, "/otherapp/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d", rec.Code)
	}
}

func TestMiddlewareCrossApp(t *testing.T) {
	// app A e app B com secrets distintos
	// token gerado com secret de A usado na rota de B → deve ser 401
	reg := registry.New()
	_ = reg.Load(&config.Config{
		Apps: []config.AppConfig{
			{Name: "appa", Auth: config.AuthConfig{JWTSecret: "secret-a"}},
			{Name: "appb", Auth: config.AuthConfig{JWTSecret: "secret-b"}},
		},
	})
	router := buildRouter(reg)

	tokenA := buildToken("secret-a", false)
	req := httptest.NewRequest(http.MethodGet, "/appb/users", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401 (cross-app), obtido %d", rec.Code)
	}
}
