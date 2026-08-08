package dashboard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

func appUsersTestPool(t *testing.T) (*db.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	schema := "au_test"
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	prov := provisioner.New(pool)
	if err := prov.EnsureAuthUserColumns(ctx, schema); err != nil {
		// EnsureAuthUserColumns only ALTERs an existing table; on a fresh
		// schema the table doesn't exist yet, so create it first the same
		// way the real provisioner does on app creation.
		if _, err := pool.Exec(ctx, `CREATE TABLE `+schema+`."_auth_users" (
			"id"            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"         TEXT NOT NULL UNIQUE,
			"password_hash" TEXT NOT NULL DEFAULT '',
			"name"          TEXT,
			"avatar_url"    TEXT,
			"created_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
			"updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
			t.Fatalf("create _auth_users: %v", err)
		}
		if err := prov.EnsureAuthUserColumns(ctx, schema); err != nil {
			t.Fatalf("ensure auth user columns: %v", err)
		}
	}

	return pool, schema
}

func appUsersInsertTestUser(t *testing.T, pool *db.Pool, schema, email string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return id
}

func TestUpdateAppUserChangesRoleFromDefault(t *testing.T) {
	pool, schema := appUsersTestPool(t)
	ctx := context.Background()
	userID := appUsersInsertTestUser(t, pool, schema, "role-update@example.com")

	var initialRole string
	if err := pool.QueryRow(ctx, `SELECT role FROM `+schema+`."_auth_users" WHERE id = $1`, userID).Scan(&initialRole); err != nil {
		t.Fatalf("read initial role: %v", err)
	}
	if initialRole != "member" {
		t.Fatalf("expected default role %q, got %q", "member", initialRole)
	}

	emailChanged, err := UpdateAppUser(ctx, pool, schema, userID, "role-update@example.com", "555-0100", "approver")
	if err != nil {
		t.Fatalf("UpdateAppUser: %v", err)
	}
	if emailChanged {
		t.Fatalf("expected emailChanged=false when email is resubmitted unchanged")
	}

	var role, phone string
	if err := pool.QueryRow(ctx, `SELECT role, phone FROM `+schema+`."_auth_users" WHERE id = $1`, userID).Scan(&role, &phone); err != nil {
		t.Fatalf("read row after update: %v", err)
	}
	if role != "approver" {
		t.Fatalf("expected role %q after update, got %q", "approver", role)
	}
	if phone != "555-0100" {
		t.Fatalf("expected phone %q after update, got %q", "555-0100", phone)
	}
}

func TestUpdateAppUserEmailChangeResetsConfirmation(t *testing.T) {
	pool, schema := appUsersTestPool(t)
	ctx := context.Background()
	userID := appUsersInsertTestUser(t, pool, schema, "confirm-me@example.com")

	if _, err := pool.Exec(ctx, `UPDATE `+schema+`."_auth_users" SET email_confirmed_at = now() WHERE id = $1`, userID); err != nil {
		t.Fatalf("seed email_confirmed_at: %v", err)
	}

	emailChanged, err := UpdateAppUser(ctx, pool, schema, userID, "new-address@example.com", "", "member")
	if err != nil {
		t.Fatalf("UpdateAppUser: %v", err)
	}
	if !emailChanged {
		t.Fatalf("expected emailChanged=true when email differs")
	}

	var email string
	var confirmedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT email, email_confirmed_at FROM `+schema+`."_auth_users" WHERE id = $1`, userID).Scan(&email, &confirmedAt); err != nil {
		t.Fatalf("read row after update: %v", err)
	}
	if email != "new-address@example.com" {
		t.Fatalf("expected email %q, got %q", "new-address@example.com", email)
	}
	if confirmedAt != nil {
		t.Fatalf("expected email_confirmed_at to be reset to NULL, got %v", confirmedAt)
	}
}

func TestUpdateAppUserEmailConflictReturnsErrEmailConflict(t *testing.T) {
	pool, schema := appUsersTestPool(t)
	ctx := context.Background()
	_ = appUsersInsertTestUser(t, pool, schema, "taken@example.com")
	userID := appUsersInsertTestUser(t, pool, schema, "available@example.com")

	_, err := UpdateAppUser(ctx, pool, schema, userID, "taken@example.com", "", "member")
	if err != ErrEmailConflict {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}

	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM `+schema+`."_auth_users" WHERE id = $1`, userID).Scan(&email); err != nil {
		t.Fatalf("read row after conflicting update: %v", err)
	}
	if email != "available@example.com" {
		t.Fatalf("expected email unchanged at %q after conflict, got %q", "available@example.com", email)
	}
}

func TestUpdateAppUserUnknownUserReturnsNotFound(t *testing.T) {
	pool, schema := appUsersTestPool(t)

	_, err := UpdateAppUser(context.Background(), pool, schema, "00000000-0000-0000-0000-000000000000", "nobody@example.com", "", "approver")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown user, got %v", err)
	}
}

func TestListAppUsersIncludesRole(t *testing.T) {
	pool, schema := appUsersTestPool(t)
	ctx := context.Background()
	userID := appUsersInsertTestUser(t, pool, schema, "list-role@example.com")
	if _, err := UpdateAppUser(ctx, pool, schema, userID, "list-role@example.com", "", "approver"); err != nil {
		t.Fatalf("UpdateAppUser: %v", err)
	}

	users, total, err := ListAppUsers(ctx, pool, schema, "", 50, 0)
	if err != nil {
		t.Fatalf("ListAppUsers: %v", err)
	}
	if total != 1 || len(users) != 1 {
		t.Fatalf("expected 1 user, got total=%d len=%d", total, len(users))
	}
	if users[0].Role != "approver" {
		t.Fatalf("expected listed role %q, got %q", "approver", users[0].Role)
	}
}
