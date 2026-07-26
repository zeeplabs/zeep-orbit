package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func SetupTestPool(t *testing.T) (pool *Pool, _ interface{}, cleanup func()) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	pool, err = New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS zeep_system`); err != nil {
		pool.Close()
		t.Fatalf("create zeep_system schema: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zeep_system.changelog_entries (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			version      TEXT NOT NULL,
			release_date DATE NOT NULL,
			title        TEXT NOT NULL,
			summary      TEXT NOT NULL DEFAULT '',
			sections     JSONB NOT NULL DEFAULT '[]'::jsonb,
			published    BOOLEAN NOT NULL DEFAULT false,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		pool.Close()
		t.Fatalf("create changelog_entries table: %v", err)
	}

	cleanup = func() {
		pool.Close()
	}

	return pool, nil, cleanup
}
