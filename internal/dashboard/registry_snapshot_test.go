package dashboard

// registry_snapshot_test.go — multi-replica-consistency T5. The snapshot a
// pod loads on sync must be identical to what the dashboard registers after
// a write, or a restarted pod and a pod that saw the change live serve
// different behavior (the old LoadFromDB dropped enum AllowedValues and the
// storage/rate-limit config on AppConfig).
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/storage"
)

func TestLoadRegistrySnapshot_ParityWithAppRowToRegistryApp(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("snap-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	app, err := CreateApp(ctx, pool, "snapparity", owner.ID, true)
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := UpdateAppStorageConfig(ctx, pool, app.ID, &storage.StorageConfig{
		Bucket: "bucket", Region: "us-east-1", Endpoint: "https://s3.example.com", AccessKeyID: "AKIA", SecretAccessKey: "secret",
	}); err != nil {
		t.Fatalf("storage config: %v", err)
	}
	if err := UpdateAppRateLimitConfig(ctx, pool, app.ID, &config.RateLimitConfig{Enabled: true, RequestsPerMinute: 120}); err != nil {
		t.Fatalf("rate limit config: %v", err)
	}
	if err := UpdateAppAuthProvidersRaw(ctx, pool, app.ID, json.RawMessage(`{"google":{"enabled":true,"client_id":"cid"}}`)); err != nil {
		t.Fatalf("auth providers: %v", err)
	}
	for _, tbl := range []AppTableRow{
		{Name: "orders", RLS: "owner", Columns: []config.ColumnConfig{
			{Name: "status", Type: "enum", Required: true, AllowedValues: []string{"pending", "paid"}},
			{Name: "total", Type: "numeric", Default: "0"},
		}},
		{Name: "notes", Columns: []config.ColumnConfig{{Name: "body", Type: "text", Unique: true}}},
	} {
		if _, err := InsertAppTable(ctx, pool, app.ID, tbl); err != nil {
			t.Fatalf("insert table %s: %v", tbl.Name, err)
		}
	}

	snap, err := LoadRegistrySnapshot(ctx, pool)
	if err != nil {
		t.Fatalf("LoadRegistrySnapshot: %v", err)
	}
	got, ok := snap.Apps["snapparity"]
	if !ok {
		t.Fatal("snapshot is missing the app")
	}

	row, _, err := GetApp(ctx, pool, app.ID, owner)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	want := appRowToRegistryApp(row)

	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", "  ")
		wj, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("snapshot app differs from appRowToRegistryApp(GetApp):\n got: %s\nwant: %s", gj, wj)
	}
	// Spot-check the fields the old boot loader dropped, so a future change
	// to both sides can't make this test pass vacuously.
	if vals := got.Tables["orders"].Columns[0].AllowedValues; len(vals) != 2 {
		t.Fatalf("enum AllowedValues lost: %v", vals)
	}
	if got.Config.Storage == nil || got.Config.RateLimit == nil || got.StorageConfig == nil || got.RateLimit == nil {
		t.Fatal("storage/rate limit config lost in the snapshot")
	}
	if got.Config.Auth.JWTSecret == "" {
		t.Fatal("the snapshot must carry the real jwt_secret")
	}

	sys, err := GetSystemConfig(ctx, pool)
	if err != nil {
		t.Fatalf("GetSystemConfig: %v", err)
	}
	if snap.System.SoftDeleteEnabled != sys.SoftDeleteEnabled || snap.System.StatementTimeoutMs != sys.StatementTimeoutMs {
		t.Fatalf("snapshot system config %+v differs from GetSystemConfig %+v", snap.System, sys)
	}
}

func TestLoadRegistrySnapshot_SystemConfigErrorReturnsNoSnapshot(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("snaperr-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := CreateApp(ctx, pool, "snaperr", owner.ID, true); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM zeep_system.system_config`); err != nil {
		t.Fatalf("delete system_config: %v", err)
	}

	snap, err := LoadRegistrySnapshot(ctx, pool)
	if err == nil {
		t.Fatal("LoadRegistrySnapshot should fail when the system config can't be read")
	}
	if snap.Apps != nil {
		t.Fatalf("a failed load must not return a partial snapshot, got %d apps", len(snap.Apps))
	}
}

func TestReadStateVersion(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	before, err := ReadStateVersion(ctx, pool)
	if err != nil {
		t.Fatalf("ReadStateVersion: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.system_config SET retention_days = retention_days`); err != nil {
		t.Fatalf("touch system_config: %v", err)
	}
	after, err := ReadStateVersion(ctx, pool)
	if err != nil {
		t.Fatalf("ReadStateVersion after write: %v", err)
	}
	if after <= before {
		t.Fatalf("version did not move: before %d, after %d", before, after)
	}
}
