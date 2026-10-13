package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// writeErrorsFixture builds a public (rls "") app whose "children.serial" is
// NOT NULL in Postgres while the registry says it is optional — the exact
// metadata/database drift behind the asset-manager incident — so the
// request reaches Postgres and the database itself rejects it.
type writeErrorsFixture struct {
	router   http.Handler
	jwt      string
	base     string
	parentID string
	childID  string
}

func setupWriteErrorsFixture(t *testing.T) writeErrorsFixture {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()
	const schema = "write_errors_test_app"
	const secret = "write-errors-jwt-secret"

	setup := []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		`CREATE TABLE ` + schema + `."_auth_users" (
			"id"            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"         TEXT NOT NULL UNIQUE,
			"password_hash" TEXT NOT NULL
		)`,
		`CREATE TABLE ` + schema + `.parents (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name       TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE ` + schema + `.children (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			serial     TEXT NOT NULL,
			parent_id  UUID REFERENCES ` + schema + `.parents(id),
			qty        INTEGER,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO zeep_app_enduser`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	testReg.Register(&registry.App{
		Config: config.AppConfig{
			Name: schema,
			Auth: config.AuthConfig{JWTSecret: secret, Providers: config.AuthProviders{Email: true}},
		},
		SchemaName: schema,
		Tables: map[string]*registry.Table{
			"parents": {Name: "parents", Columns: []registry.Column{{Name: "name", Type: "text"}}},
			"children": {Name: "children", Columns: []registry.Column{
				{Name: "serial", Type: "text"}, // drifted: optional here, NOT NULL in Postgres
				{Name: "parent_id", Type: "uuid"},
				{Name: "qty", Type: "integer"},
			}},
		},
	})
	t.Cleanup(func() { testReg.Unregister(schema) })

	var userID, parentID, childID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ('write-errors@test.com', 'x') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO `+schema+`.parents (name) VALUES ('p') RETURNING id`).Scan(&parentID); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`.children (serial, parent_id) VALUES ('S-1', $1) RETURNING id`, parentID,
	).Scan(&childID); err != nil {
		t.Fatalf("insert child: %v", err)
	}
	jwt, err := auth.IssueJWT([]byte(secret), userID, "write-errors@test.com", schema, "member")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	return writeErrorsFixture{
		router:   buildRLSRouter(NewHandler(testPool, testReg)),
		jwt:      jwt,
		base:     "/" + schema,
		parentID: parentID,
		childID:  childID,
	}
}

func (f writeErrorsFixture) do(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, f.base+path, nil)
	} else {
		req = httptest.NewRequest(method, f.base+path, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+f.jwt)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	msg, _ := resp["error"].(string)
	return rec.Code, msg
}

func assertNoLeak(t *testing.T, msg string, forbidden ...string) {
	t.Helper()
	for _, f := range append(forbidden, "SQLSTATE", "ERROR:") {
		if f != "" && strings.Contains(msg, f) {
			t.Fatalf("error message leaks %q: %q", f, msg)
		}
	}
}

// ERR-01: the asset-manager incident — a NOT NULL violation used to answer
// 500 "failed to insert row" with no clue for the caller.
func TestHandlerCreateNotNullViolationReturns400NamingColumn(t *testing.T) {
	f := setupWriteErrorsFixture(t)
	code, msg := f.do(t, http.MethodPost, "/children/", map[string]any{"qty": 1})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", code, msg)
	}
	if msg != `column "serial" is required` {
		t.Fatalf("unexpected message %q", msg)
	}
}

func TestHandlerUpdateNotNullViolationReturns400NamingColumn(t *testing.T) {
	f := setupWriteErrorsFixture(t)
	code, msg := f.do(t, http.MethodPatch, "/children/"+f.childID+"/", map[string]any{"serial": nil})
	if code != http.StatusBadRequest || msg != `column "serial" is required` {
		t.Fatalf("expected 400 naming serial, got %d: %q", code, msg)
	}
}

// ERR-01: a reference to a row that does not exist is the caller's input
// error (400), named by constraint, never echoing the attempted value.
func TestHandlerCreateForeignKeyViolationReturns400(t *testing.T) {
	f := setupWriteErrorsFixture(t)
	missing := "00000000-0000-4000-8000-000000000001"
	code, msg := f.do(t, http.MethodPost, "/children/", map[string]any{"serial": "S-2", "parent_id": missing})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", code, msg)
	}
	if !strings.Contains(msg, "referenced row does not exist") || !strings.Contains(msg, "children_parent_id_fkey") {
		t.Fatalf("unexpected message %q", msg)
	}
	assertNoLeak(t, msg, missing)
}

// ERR-01: deleting a row other rows still point to is a conflict (409).
func TestHandlerDeleteStillReferencedReturns409(t *testing.T) {
	f := setupWriteErrorsFixture(t)
	code, msg := f.do(t, http.MethodDelete, "/parents/"+f.parentID+"/", nil)
	if code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", code, msg)
	}
	if !strings.Contains(msg, "still referenced") || !strings.Contains(msg, "children_parent_id_fkey") {
		t.Fatalf("unexpected message %q", msg)
	}
}

// ERR-01: a value the column type cannot hold (Postgres class 22) is a 400.
func TestHandlerCreateOutOfRangeValueReturns400(t *testing.T) {
	f := setupWriteErrorsFixture(t)
	code, msg := f.do(t, http.MethodPost, "/children/", map[string]any{"serial": "S-3", "qty": 99999999999})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", code, msg)
	}
	assertNoLeak(t, msg, "99999999999")
}
