package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-core/internal/config"
	"github.com/zeeplabs/zeep-core/internal/db"
	"github.com/zeeplabs/zeep-core/internal/registry"
)

// ----------------------------------------------------------------------------
// Setup de integração
// ----------------------------------------------------------------------------

const testSchema = "testhandler"
const testTable = "items"

const (
	rlsSchema  = "rls_test_app"
	rlsAppName = "rls_test_app"
	rlsSecret  = "rls-jwt-secret"
)

var (
	testPool *db.Pool
	testReg  *registry.Registry
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		// Sem banco configurado: pula todos os testes de integração.
		os.Exit(0)
	}

	ctx := context.Background()
	var err error
	testPool, err = db.New(ctx, dsn)
	if err != nil {
		panic("TestMain: falha ao conectar no banco: " + err.Error())
	}
	defer testPool.Close()

	// Cria schema e tabela de teste (CRUD sem RLS).
	setup := []string{
		"DROP SCHEMA IF EXISTS " + testSchema + " CASCADE",
		"CREATE SCHEMA " + testSchema,
		`CREATE TABLE ` + testSchema + `.` + testTable + ` (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name       TEXT NOT NULL,
			value      TEXT,
			created_at TIMESTAMPTZ DEFAULT now(),
			updated_at TIMESTAMPTZ DEFAULT now()
		)`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			panic("TestMain: setup falhou: " + err.Error())
		}
	}

	// Cria schema e tabelas para testes de RLS.
	rlsSetup := []string{
		"DROP SCHEMA IF EXISTS " + rlsSchema + " CASCADE",
		"CREATE SCHEMA " + rlsSchema,
		`CREATE TABLE ` + rlsSchema + `."_auth_users" (
			"id"                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"              TEXT        NOT NULL UNIQUE,
			"phone"              TEXT,
			"password_hash"      TEXT        NOT NULL,
			"name"               TEXT,
			"avatar_url"         TEXT,
			"email_confirmed_at" TIMESTAMPTZ,
			"last_sign_in_at"    TIMESTAMPTZ,
			"created_at"         TIMESTAMPTZ NOT NULL DEFAULT now(),
			"updated_at"         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE ` + rlsSchema + `.notes (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			owner_id   UUID NOT NULL REFERENCES ` + rlsSchema + `."_auth_users"("id"),
			created_at TIMESTAMPTZ DEFAULT now(),
			updated_at TIMESTAMPTZ DEFAULT now()
		)`,
	}
	for _, sql := range rlsSetup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			panic("TestMain: rls setup falhou: " + err.Error())
		}
	}

	// Monta registry em memória (sem apps.yaml).
	testReg = registry.New()
	_ = testReg.Load(&config.Config{
		Apps: []config.AppConfig{
			{
				Name: "testhandler",
				Auth: config.AuthConfig{JWTSecret: "test-secret"},
				Tables: []config.TableConfig{
					{
						Name: testTable,
						Columns: []config.ColumnConfig{
							{Name: "name", Type: "text", Required: true},
							{Name: "value", Type: "text", Required: false},
						},
					},
				},
			},
			{
				Name: rlsAppName,
				Auth: config.AuthConfig{
					JWTSecret: rlsSecret,
					Providers: config.AuthProviders{Email: true},
				},
				Tables: []config.TableConfig{
					{
						Name: "notes",
						RLS:  "owner",
						Columns: []config.ColumnConfig{
							{Name: "title", Type: "text", Required: true},
						},
					},
				},
			},
		},
	})

	code := m.Run()

	// Cleanup
	_, _ = testPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+testSchema+" CASCADE")
	_, _ = testPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+rlsSchema+" CASCADE")

	os.Exit(code)
}

// ----------------------------------------------------------------------------
// Helpers de teste
// ----------------------------------------------------------------------------

// buildHandlerRouter monta um chi.Router com o Handler para os testes CRUD.
// A rota não usa JWTMiddleware: injeta o app diretamente no contexto.
func buildHandlerRouter(h *Handler) http.Handler {
	app, _ := testReg.Get("testhandler")

	injectApp := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), appContextKey, app)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Use(injectApp)
	r.Get("/health", h.HandleHealth)
	r.Get("/{table}", h.HandleList)
	r.Post("/{table}", h.HandleCreate)
	r.Get("/{table}/{id}", h.HandleGetByID)
	r.Patch("/{table}/{id}", h.HandleUpdate)
	r.Delete("/{table}/{id}", h.HandleDelete)
	return r
}

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// ----------------------------------------------------------------------------
// Testes CRUD
// ----------------------------------------------------------------------------

func TestHandlerCRUD(t *testing.T) {
	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	var createdID string

	t.Run("CreateReturns201", func(t *testing.T) {
		body := map[string]any{"name": "foo", "value": "bar"}
		req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		id, ok := row["id"].(string)
		if !ok || id == "" {
			t.Fatal("row sem id")
		}
		if row["name"] != "foo" {
			t.Fatalf("name esperado 'foo', obtido %v", row["name"])
		}
		createdID = id
	})

	t.Run("ListReturnsData", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/"+testTable, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		data, ok := resp["data"].([]any)
		if !ok {
			t.Fatal("campo 'data' ausente ou tipo errado")
		}
		if len(data) == 0 {
			t.Fatal("esperado ao menos 1 item em data")
		}
		if _, ok := resp["count"]; !ok {
			t.Fatal("campo 'count' ausente")
		}
	})

	t.Run("GetByIDFound", func(t *testing.T) {
		if createdID == "" {
			t.Skip("CreateReturns201 não gerou ID")
		}
		req := httptest.NewRequest(http.MethodGet, "/"+testTable+"/"+createdID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		if row["id"] != createdID {
			t.Fatalf("id esperado %s, obtido %v", createdID, row["id"])
		}
	})

	t.Run("GetByIDNotFound404", func(t *testing.T) {
		fakeID := "00000000-0000-0000-0000-000000000000"
		req := httptest.NewRequest(http.MethodGet, "/"+testTable+"/"+fakeID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("UpdatePartial", func(t *testing.T) {
		if createdID == "" {
			t.Skip("CreateReturns201 não gerou ID")
		}
		body := map[string]any{"value": "updated"}
		req := httptest.NewRequest(http.MethodPatch, "/"+testTable+"/"+createdID, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		if row["value"] != "updated" {
			t.Fatalf("value esperado 'updated', obtido %v", row["value"])
		}
		// name não deve ter mudado
		if row["name"] != "foo" {
			t.Fatalf("name não deveria mudar, obtido %v", row["name"])
		}
	})

	t.Run("DeleteReturns204", func(t *testing.T) {
		if createdID == "" {
			t.Skip("CreateReturns201 não gerou ID")
		}
		req := httptest.NewRequest(http.MethodDelete, "/"+testTable+"/"+createdID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("esperado 204, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DeleteNotFound404", func(t *testing.T) {
		fakeID := "00000000-0000-0000-0000-000000000000"
		req := httptest.NewRequest(http.MethodDelete, "/"+testTable+"/"+fakeID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// ----------------------------------------------------------------------------
// Testes de Health
// ----------------------------------------------------------------------------

func TestHandlerHealth(t *testing.T) {
	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("status esperado 'ok', obtido %v", resp["status"])
	}
	if _, ok := resp["apps"]; !ok {
		t.Fatal("campo 'apps' ausente")
	}
}

// ----------------------------------------------------------------------------
// Testes de erros de request (não precisam de banco)
// ----------------------------------------------------------------------------

func TestHandlerCreateInvalidBody(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/"+testTable, bytes.NewBufferString("not-json{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateUnknownField(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"name": "x", "nonexistent_field": "y"}
	req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 por campo desconhecido, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerListUnknownTable(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent_table", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
	}
}
