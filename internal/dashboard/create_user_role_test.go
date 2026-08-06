package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"go.uber.org/zap"
)

// TestCreateUserRoleGate covers the role-creation gate wired into CreateUser:
//   - admin cannot create a superadmin (the key gate; 403)
//   - admin can create admin/auditor/member (the other 3 roles; 201)
//   - superadmin can create any role (201)
//   - auditor is blocked by the ActionManageUsers gate before the role-creation
//     gate (403 with the "forbidden" error from HasPlatformPermission)
//   - invalid role names are 400 (validation, not authorization)
//
// Requires TEST_DATABASE_URL pointing at a real Postgres. The test fixture
// builds a minimal dashboard_users table (4-value CHECK, columns CreateUser's
// INSERT touches) — it does NOT call ProvisionZeepSystem, so the audit_log
// table is absent. The audit() method is best-effort and silently swallows
// the resulting insert error AFTER writeJSON, so 201 responses are still
// returned and the test still passes.
func TestCreateUserRoleGate(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	for _, stmt := range []string{
		`CREATE SCHEMA IF NOT EXISTS zeep_system`,
		`DROP TABLE IF EXISTS zeep_system.dashboard_users CASCADE`,
		// New 4-value constraint + all columns that CreateUser's INSERT
		// and RETURNING reference (id, email, name, password_hash, role,
		// created_at). The real schema also has google_id and language,
		// but CreateUser doesn't write them, so they're not required here.
		`CREATE TABLE zeep_system.dashboard_users (
			id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			email         TEXT        UNIQUE NOT NULL,
			password_hash TEXT        NOT NULL DEFAULT '',
			name          TEXT        NOT NULL DEFAULT '',
			role          TEXT        NOT NULL CHECK (role IN ('superadmin','admin','auditor','member')),
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	// Seed the actor users. Their IDs are reused as the actor.ID in the
	// request context below.
	actors := []struct{ id, email, role string }{
		{"b0000000-0000-0000-0000-000000000001", "admin-actor@example.com", "admin"},
		{"b0000000-0000-0000-0000-000000000002", "super-actor@example.com", "superadmin"},
		{"b0000000-0000-0000-0000-000000000003", "auditor-actor@example.com", "auditor"},
	}
	for _, a := range actors {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.dashboard_users (id, email, name, role) VALUES ($1, $2, '', $3)`,
			a.id, a.email, a.role); err != nil {
			t.Fatalf("seed %s: %v", a.email, err)
		}
	}

	adminActor := &DashboardUser{ID: actors[0].id, Email: actors[0].email, Role: "admin"}
	superActor := &DashboardUser{ID: actors[1].id, Email: actors[1].email, Role: "superadmin"}
	auditActor := &DashboardUser{ID: actors[2].id, Email: actors[2].email, Role: "auditor"}

	// NewHandler with nil reg — CreateUser + audit don't touch h.reg/h.prov/h.Logs.
	h := NewHandler(pool, nil, zap.NewNop())

	tests := []struct {
		name       string
		actor      *DashboardUser
		targetRole string
		wantStatus int
	}{
		// Role-creation gate (T-03): the key behavior.
		{"admin_create_superadmin_403", adminActor, "superadmin", http.StatusForbidden},

		// Admin can create the 3 non-superadmin roles (T-02 action gate allows).
		{"admin_create_admin_201", adminActor, "admin", http.StatusCreated},
		{"admin_create_auditor_201", adminActor, "auditor", http.StatusCreated},
		{"admin_create_member_201", adminActor, "member", http.StatusCreated},

		// Superadmin can create any role.
		{"super_create_superadmin_201", superActor, "superadmin", http.StatusCreated},
		{"super_create_admin_201", superActor, "admin", http.StatusCreated},

		// T-02 action gate: auditor is denied before the role-creation gate fires.
		{"auditor_create_admin_403", auditActor, "admin", http.StatusForbidden},

		// Validation: invalid role names are 400 (not 403, not 201).
		{"admin_invalid_role_400", adminActor, "godmode", http.StatusBadRequest},
		{"admin_empty_role_400", adminActor, "", http.StatusBadRequest},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Unique email per subtest (UNIQUE constraint on dashboard_users.email).
			email := fmt.Sprintf("new-%d-%s@example.com", i, tc.name)

			body, _ := json.Marshal(map[string]string{
				"email":    email,
				"name":     "Test User",
				"password": "password123",
				"role":     tc.targetRole,
			})
			req := httptest.NewRequest(http.MethodPost, "/dashboard/api/users", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), userCtxKey, tc.actor))

			w := httptest.NewRecorder()
			h.CreateUser(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}
