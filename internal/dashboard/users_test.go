package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"go.uber.org/zap"
)

// usersTestPool builds the minimal fixture needed for T-04 / T-05 tests:
// dashboard_users with the 4-value CHECK constraint and all columns the
// UpdateUserRole store function touches, plus a nullable-user_id audit_log
// table so we can verify the user.role_changed entries.
//
// Skips when TEST_DATABASE_URL is not set, matching the pattern of
// provisioner_roles_test.go and create_user_role_test.go.
func usersTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	for _, stmt := range []string{
		`CREATE SCHEMA IF NOT EXISTS zeep_system`,
		// Drop in dependency order: audit_log FKs to dashboard_users, so drop
		// it first to avoid CASCADE surprises. (CASCADE would also work, but
		// being explicit makes the schema relationship visible.)
		`DROP TABLE IF EXISTS zeep_system.audit_log`,
		`DROP TABLE IF EXISTS zeep_system.dashboard_users CASCADE`,
		`CREATE TABLE zeep_system.dashboard_users (
			id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			email         TEXT        UNIQUE NOT NULL,
			password_hash TEXT        NOT NULL DEFAULT '',
			name          TEXT        NOT NULL DEFAULT '',
			role          TEXT        NOT NULL CHECK (role IN ('superadmin','admin','auditor','member')),
			language      TEXT        NOT NULL DEFAULT 'en',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// user_id is nullable per the real schema (provisioner.go drops NOT
		// NULL on the same column) — events with no authenticated actor are
		// legitimate. This matches what the existing InsertAuditLog expects.
		`CREATE TABLE zeep_system.audit_log (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id        UUID        REFERENCES zeep_system.dashboard_users(id),
			user_email     TEXT        NOT NULL,
			action         TEXT        NOT NULL,
			resource_type  TEXT        NOT NULL,
			resource_id    TEXT,
			resource_name  TEXT,
			metadata       JSONB       NOT NULL DEFAULT '{}',
			ip_address     TEXT,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return pool
}

func seedUser(t *testing.T, pool *db.Pool, id, email, role string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO zeep_system.dashboard_users (id, email, name, role) VALUES ($1, $2, '', $3)`,
		id, email, role); err != nil {
		t.Fatalf("seed %s: %v", email, err)
	}
}

// patchRequest builds a PATCH /dashboard/api/users/{id} request with the
// actor pre-injected into context (chi.URLParam set up too — the handler
// reads targetID via chi.URLParam, not via the URL directly).
func patchRequest(t *testing.T, targetID, body string, actor *DashboardUser) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/dashboard/api/users/"+targetID, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if actor != nil {
		req = req.WithContext(context.WithValue(req.Context(), userCtxKey, actor))
	}
	return req
}

// deleteRequest builds a DELETE /dashboard/api/users/{id} request similarly.
func deleteRequest(targetID string, actor *DashboardUser) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/dashboard/api/users/"+targetID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if actor != nil {
		req = req.WithContext(context.WithValue(req.Context(), userCtxKey, actor))
	}
	return req
}

func TestUpdateUserRole(t *testing.T) {
	pool := usersTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	h := NewHandler(pool, nil, zap.NewNop())

	// Seed the cast: 2 supers (one as actor, one as test target for the
	// super-only-can-create-super case), 1 admin actor, 1 auditor, 1 member.
	const (
		superA    = "b0000000-0000-0000-0000-000000000001"
		superB    = "b0000000-0000-0000-0000-000000000002"
		adminID   = "b0000000-0000-0000-0000-000000000003"
		auditorID = "b0000000-0000-0000-0000-000000000004"
		memberID  = "b0000000-0000-0000-0000-000000000005"
	)
	seedUser(t, pool, superA, "super-a@example.com", "superadmin")
	seedUser(t, pool, superB, "super-b@example.com", "superadmin")
	seedUser(t, pool, adminID, "admin-actor@example.com", "admin")
	seedUser(t, pool, auditorID, "auditor-target@example.com", "auditor")
	seedUser(t, pool, memberID, "member-target@example.com", "member")

	adminActor := &DashboardUser{ID: adminID, Email: "admin-actor@example.com", Role: "admin"}
	superActor := &DashboardUser{ID: superA, Email: "super-a@example.com", Role: "superadmin"}

	tests := []struct {
		name       string
		actor      *DashboardUser
		targetID   string
		newRole    string
		wantStatus int
	}{
		// Success: admin can change non-superadmin roles freely.
		{"admin_member_to_auditor_200", adminActor, memberID, "auditor", http.StatusOK},
		{"admin_auditor_to_admin_200", adminActor, auditorID, "admin", http.StatusOK},
		// Superadmin can promote to superadmin.
		{"super_admin_to_superadmin_200", superActor, adminID, "superadmin", http.StatusOK},
		// Admin cannot promote to superadmin (CanCreateUserWithRole).
		{"admin_to_superadmin_403", adminActor, memberID, "superadmin", http.StatusForbidden},
		// Invalid role name (validation, not authorization).
		{"admin_invalid_role_400", adminActor, memberID, "godmode", http.StatusBadRequest},
		// Non-existent target.
		{"admin_nonexistent_target_404", adminActor, "00000000-0000-0000-0000-000000000099", "auditor", http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"role": tc.newRole})
			w := httptest.NewRecorder()
			h.UpdateUserRole(w, patchRequest(t, tc.targetID, string(body), tc.actor))

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}

	// After all the success cases (3 of them), the audit_log should have
	// exactly 3 user.role_changed rows — one per successful PATCH.
	t.Run("audit_log_rows_written", func(t *testing.T) {
		var count int
		err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'user.role_changed'`).Scan(&count)
		if err != nil {
			t.Fatalf("count audit_log: %v", err)
		}
		if count != 3 {
			t.Errorf("audit_log user.role_changed count = %d, want 3 (one per successful PATCH)", count)
		}

		// Spot-check the metadata of the most recent role change (admin→superadmin
		// for the admin target). Metadata is JSONB stored as '{"from":"admin","to":"superadmin"}'.
		var metadata string
		err = pool.QueryRow(ctx,
			`SELECT metadata::text FROM zeep_system.audit_log
			 WHERE action = 'user.role_changed' AND resource_id = $1
			 ORDER BY created_at DESC LIMIT 1`, adminID).Scan(&metadata)
		if err != nil {
			t.Fatalf("read audit_log metadata: %v", err)
		}
		var meta map[string]string
		if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
			t.Fatalf("audit metadata not valid JSON: %v (raw: %s)", err, metadata)
		}
		if meta["from"] != "admin" || meta["to"] != "superadmin" {
			t.Errorf("audit metadata = %v, want {from:admin, to:superadmin}", meta)
		}
	})
}

func TestLastSuperadminInvariant(t *testing.T) {
	pool := usersTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	h := NewHandler(pool, nil, zap.NewNop())

	const (
		superA  = "b0000000-0000-0000-0000-000000000001"
		superB  = "b0000000-0000-0000-0000-000000000002"
		adminID = "b0000000-0000-0000-0000-000000000003"
	)
	seedUser(t, pool, superA, "super-a@example.com", "superadmin")
	seedUser(t, pool, superB, "super-b@example.com", "superadmin")
	seedUser(t, pool, adminID, "admin-actor@example.com", "admin")
	adminActor := &DashboardUser{ID: adminID, Email: "admin-actor@example.com", Role: "admin"}

	// --- PATCH: with 2 superadmins, demoting one is fine (invariant OK) ---
	t.Run("patch_demote_one_of_two_200", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"role": "admin"})
		w := httptest.NewRecorder()
		h.UpdateUserRole(w, patchRequest(t, superA, string(body), adminActor))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (super-b still covers the invariant)", w.Code)
		}
		var role string
		pool.QueryRow(ctx, `SELECT role FROM zeep_system.dashboard_users WHERE id = $1`, superA).Scan(&role)
		if role != "admin" {
			t.Errorf("role after PATCH = %q, want admin", role)
		}
	})

	// Re-promote superA so we can re-trigger the invariant scenario from
	// a state with 2 superadmins.
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.dashboard_users SET role = 'superadmin' WHERE id = $1`, superA); err != nil {
		t.Fatalf("reset superA: %v", err)
	}

	// --- PATCH: with 2 superadmins, demote one, then demote the last → 400 ---
	// This is the realistic trigger: admin demotes superA first (invariant OK,
	// super-b still there), then admin tries to demote super-b → 400.
	t.Run("patch_demote_remaining_superadmin_400", func(t *testing.T) {
		// First demote superA to set up the "only 1 superadmin" state.
		body, _ := json.Marshal(map[string]string{"role": "admin"})
		w := httptest.NewRecorder()
		h.UpdateUserRole(w, patchRequest(t, superA, string(body), adminActor))
		if w.Code != http.StatusOK {
			t.Fatalf("setup PATCH (superA→admin) failed: status = %d", w.Code)
		}

		// Now super-b is the only superadmin. Try to demote super-b → 400.
		w2 := httptest.NewRecorder()
		h.UpdateUserRole(w2, patchRequest(t, superB, string(body), adminActor))
		if w2.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (would leave 0 superadmins)", w2.Code)
		}
		// Verify super-b's role was NOT changed.
		var role string
		pool.QueryRow(ctx, `SELECT role FROM zeep_system.dashboard_users WHERE id = $1`, superB).Scan(&role)
		if role != "superadmin" {
			t.Errorf("role after rejected PATCH = %q, want superadmin (unchanged)", role)
		}
	})

	// --- DELETE: with 2 superadmins, superadmin A deletes superadmin B → 200 ---
	// First reset: re-promote superA so we have 2 supers again.
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.dashboard_users SET role = 'superadmin' WHERE id = $1`, superA); err != nil {
		t.Fatalf("reset superA for DELETE test: %v", err)
	}
	t.Run("delete_one_of_two_superadmins_200", func(t *testing.T) {
		actor := &DashboardUser{ID: superA, Email: "super-a@example.com", Role: "superadmin"}
		w := httptest.NewRecorder()
		h.DeleteUser(w, deleteRequest(superB, actor))
		if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 200 (invariant OK, super-a remains)", w.Code)
		}
		var count int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM zeep_system.dashboard_users WHERE id = $1`, superB).Scan(&count)
		if count != 0 {
			t.Errorf("super-b still in DB after DELETE (count=%d), want 0", count)
		}
	})

	// --- DELETE: invariant "would leave 0" is unreachable through the API ---
	// DeleteUser requires the actor to be a superadmin AND forbids self-delete,
	// so the only way to delete a superadmin is with another superadmin acting.
	// That guarantees at least 1 superadmin (the actor) survives. The invariant
	// check exists for defense in depth + future when DeleteUser is broadened
	// (e.g. allowing admin to delete non-superadmin users — at which point an
	// admin should not be able to delete the last superadmin either).
	t.Run("delete_self_blocked_by_existing_check", func(t *testing.T) {
		// State: super-a is the only superadmin. Self-delete is blocked by
		// the existing "cannot delete yourself" check (line ~775 of handler.go).
		actor := &DashboardUser{ID: superA, Email: "super-a@example.com", Role: "superadmin"}
		w := httptest.NewRecorder()
		h.DeleteUser(w, deleteRequest(superA, actor))
		if w.Code != http.StatusBadRequest {
			t.Errorf("self-delete status = %d, want 400 (cannot delete yourself)", w.Code)
		}
	})
}
