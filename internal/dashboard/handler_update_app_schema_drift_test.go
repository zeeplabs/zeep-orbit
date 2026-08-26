package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// TestUpdateApp_DoesNotReconcileTableSchema covers app-update-schema-drift-fix
// spec AUSD-01/AUSD-02: PUT /dashboard/api/apps/{id} (the Login/Storage/API
// tabs' save endpoint) must not fail — or touch table schema at all — when
// an unrelated table on the same app has drift (a configured column type
// that no longer matches its physical Postgres type, the exact real-world
// shape found on "internal-portal-rh"). Before the fix, UpdateApp called
// h.prov.Apply unconditionally, which reconciled every table and surfaced
// this table's TypeChangeError on a request that never mentioned it.
func TestUpdateApp_DoesNotReconcileTableSchema(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	owner := actors["loner"]

	if _, err := h.CreateAppTableForUser(context.Background(), owner, appID, TableRequestBody{
		Name: "drifted",
		Columns: []config.ColumnConfig{
			{Name: "note", Type: "text"},
		},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	// Simulate the historical pgType() bug's exact end state: the
	// configured column type says "numeric" but the physical column,
	// already created as TEXT above, was never altered — a real
	// provisioner.Apply reconciliation of this table would hit
	// applyTypeChange's unsafe-narrowing rejection (text -> numeric is
	// never a safe implicit conversion when the column holds free text).
	if _, err := pool.Exec(context.Background(),
		`UPDATE zeep_system.app_tables
		 SET columns = '[{"name":"note","type":"numeric"}]'::jsonb
		 WHERE app_id = $1 AND name = 'drifted'`,
		appID,
	); err != nil {
		t.Fatalf("inject drift: %v", err)
	}

	body, _ := json.Marshal(AppRequestBody{
		Name:             "test-app",
		AuthEmailEnabled: true,
	})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/api/apps/"+appID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, owner)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()

	h.UpdateApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite the drifted table, got %d: %s", w.Code, w.Body.String())
	}

	// The drifted column must be untouched — this endpoint must not have
	// attempted any DDL against it (config still says "numeric", physical
	// column is still TEXT; a real Apply would have failed trying to
	// reconcile them, not silently succeeded).
	var physicalType string
	if err := pool.QueryRow(context.Background(),
		`SELECT data_type FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = 'drifted' AND column_name = 'note'`,
		schemaNameForDB("test-app"),
	).Scan(&physicalType); err != nil {
		t.Fatalf("query physical column type: %v", err)
	}
	if physicalType != "text" {
		t.Fatalf("expected the drifted column to remain physically text (untouched), got %q", physicalType)
	}
}
