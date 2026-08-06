package db_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func TestNew(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping() after New() failed: %v", err)
	}
}

func TestNewInvalidDSN(t *testing.T) {
	ctx := context.Background()

	_, err := db.New(ctx, "not-a-valid-dsn")
	if err == nil {
		t.Fatal("New() with invalid DSN should return an error, got nil")
	}
}

func TestNewBadHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dsn := "postgres://user:pass@localhost:19999/nonexistent_db_zeep_test"
	_, err := db.New(ctx, dsn)
	if err == nil {
		t.Fatal("New() with unreachable host should return an error, got nil")
	}
}

func TestIsStatementTimeout(t *testing.T) {
	if !db.IsStatementTimeout(&pgconn.PgError{Code: "57014"}) {
		t.Fatal("57014 (query_canceled) should be detected as a statement timeout")
	}
	if db.IsStatementTimeout(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique_violation must not be treated as a statement timeout")
	}
	if db.IsStatementTimeout(errors.New("some other error")) {
		t.Fatal("a non-pg error must not be treated as a statement timeout")
	}
	if db.IsStatementTimeout(nil) {
		t.Fatal("nil must not be treated as a statement timeout")
	}
}
