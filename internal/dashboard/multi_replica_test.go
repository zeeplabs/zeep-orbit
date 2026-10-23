package dashboard

// multi_replica_test.go — multi-replica-consistency T9. Two registries on
// one database stand in for two pods. Pod A serves the write through the
// dashboard Handler (which registers the change locally, as in production);
// pod B never sees the request and must converge through its Syncer. Before
// this feature, B kept serving the old state until it restarted.
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"github.com/zeeplabs/zeep-orbit/internal/tokencache"
)

type testPod struct {
	reg    *registry.Registry
	syncer *registry.Syncer
}

func newTestPod(t *testing.T, pool *db.Pool, interval time.Duration) *testPod {
	t.Helper()
	reg := registry.New()
	s := registry.NewSyncer(reg, registry.SyncerConfig{
		Version:  func(ctx context.Context) (int64, error) { return ReadStateVersion(ctx, pool) },
		Load:     func(ctx context.Context) (registry.Snapshot, error) { return LoadRegistrySnapshot(ctx, pool) },
		Interval: interval,
		OnChange: []func(){tokencache.Clear},
	})
	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	return &testPod{reg: reg, syncer: s}
}

func (p *testPod) sync(t *testing.T) {
	t.Helper()
	if err := p.syncer.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
}

// multiReplicaFixture provisions zeep_system, creates an owner and an app
// with one table through pod A's handler, and syncs pod B.
func multiReplicaFixture(t *testing.T) (*db.Pool, *Handler, *DashboardUser, *AppRow, *testPod) {
	t.Helper()
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("mrep-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	podA := newTestPod(t, pool, time.Hour)
	h := NewHandler(pool, podA.reg, zap.NewNop())

	app, err := h.CreateAppForUser(ctx, owner, AppRequestBody{Name: fmt.Sprintf("mrep%d", time.Now().UnixNano())}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, owner, app.ID, TableRequestBody{
		Name: "items",
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text"},
			{Name: "status", Type: "enum", AllowedValues: []string{"draft", "live"}},
		},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	t.Cleanup(func() {
		_ = DeleteApp(context.Background(), pool, app.ID, owner)
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schemaNameForDB(app.Name)))
	})

	podB := newTestPod(t, pool, time.Hour)
	if _, ok := podB.reg.GetTable(app.Name, "items"); !ok {
		t.Fatal("pod B should see the fixture table after its initial sync")
	}
	return pool, h, owner, app, podB
}

func columnNames(tbl *registry.Table) []string {
	names := make([]string, 0, len(tbl.Columns))
	for _, c := range tbl.Columns {
		names = append(names, c.Name)
	}
	return names
}

func TestMultiReplica_PodBConvergesAfterSync(t *testing.T) {
	pool, h, owner, app, podB := multiReplicaFixture(t)
	ctx := context.Background()

	t.Run("table create", func(t *testing.T) {
		if _, err := h.CreateAppTableForUser(ctx, owner, app.ID, TableRequestBody{
			Name: "orders", Columns: []config.ColumnConfig{{Name: "total", Type: "integer"}},
		}, "127.0.0.1"); err != nil {
			t.Fatalf("CreateAppTableForUser: %v", err)
		}
		podB.sync(t)
		if _, ok := podB.reg.GetTable(app.Name, "orders"); !ok {
			t.Fatal("pod B did not pick up the new table")
		}
	})

	t.Run("column add", func(t *testing.T) {
		if _, err := h.AddTableColumnForUser(ctx, owner, app.ID, "items", config.ColumnConfig{Name: "price", Type: "integer"}, "127.0.0.1"); err != nil {
			t.Fatalf("AddTableColumnForUser: %v", err)
		}
		podB.sync(t)
		tbl, _ := podB.reg.GetTable(app.Name, "items")
		if tbl == nil || len(tbl.Columns) != 3 {
			t.Fatalf("pod B columns = %v, want title, status, price", tbl)
		}
	})

	t.Run("enum values change", func(t *testing.T) {
		if _, err := h.UpdateColumnEnumValuesForUser(ctx, owner, app.ID, "items", "status", []string{"draft", "live", "archived"}, "127.0.0.1"); err != nil {
			t.Fatalf("UpdateColumnEnumValuesForUser: %v", err)
		}
		podB.sync(t)
		tbl, _ := podB.reg.GetTable(app.Name, "items")
		var got []string
		for _, c := range tbl.Columns {
			if c.Name == "status" {
				got = c.AllowedValues
			}
		}
		if len(got) != 3 {
			t.Fatalf("pod B enum values = %v, want 3 values", got)
		}
	})

	t.Run("auth provider change", func(t *testing.T) {
		if err := UpdateAppAuthProvidersRaw(ctx, pool, app.ID, json.RawMessage(`{"google":{"enabled":true,"client_id":"cid"}}`)); err != nil {
			t.Fatalf("UpdateAppAuthProvidersRaw: %v", err)
		}
		podB.sync(t)
		got, _ := podB.reg.Get(app.Name)
		if got == nil || got.AuthProviders["google"] == nil {
			t.Fatalf("pod B auth providers = %v, want google", got.AuthProviders)
		}
	})

	t.Run("jwt_secret rotation", func(t *testing.T) {
		before, _ := podB.reg.Get(app.Name)
		// Same statement as the regenerate-secret handler.
		if _, err := pool.Exec(ctx,
			`UPDATE zeep_system.apps SET jwt_secret = encode(gen_random_bytes(32), 'hex') WHERE id = $1`, app.ID); err != nil {
			t.Fatalf("rotate secret: %v", err)
		}
		podB.sync(t)
		after, _ := podB.reg.Get(app.Name)
		if after.Config.Auth.JWTSecret == before.Config.Auth.JWTSecret {
			t.Fatal("pod B still holds the old jwt_secret")
		}
	})

	t.Run("soft-delete toggle", func(t *testing.T) {
		cur, err := GetSystemConfig(ctx, pool)
		if err != nil {
			t.Fatalf("GetSystemConfig: %v", err)
		}
		want := !cur.SoftDeleteEnabled
		cur.SoftDeleteEnabled = want
		if _, err := UpsertSystemConfig(ctx, pool, cur); err != nil {
			t.Fatalf("UpsertSystemConfig: %v", err)
		}
		podB.sync(t)
		if podB.reg.SystemConfig().SoftDeleteEnabled != want {
			t.Fatalf("pod B soft delete = %v, want %v", !want, want)
		}
	})

	t.Run("token revoke clears the token cache", func(t *testing.T) {
		tok, err := CreateAppToken(ctx, pool, CreateAppTokenInput{AppID: app.ID, Name: "t"})
		if err != nil {
			t.Fatalf("CreateAppToken: %v", err)
		}
		tokencache.Set(tok.JTI, true)
		if err := RevokeAppToken(ctx, pool, tok.ID, app.ID); err != nil {
			t.Fatalf("RevokeAppToken: %v", err)
		}
		podB.sync(t)
		if _, fresh := tokencache.IsActive(tok.JTI); fresh {
			t.Fatal("pod B still caches the revoked token as active")
		}
	})

	t.Run("app delete", func(t *testing.T) {
		if err := DeleteApp(ctx, pool, app.ID, owner); err != nil {
			t.Fatalf("DeleteApp: %v", err)
		}
		podB.sync(t)
		if _, ok := podB.reg.Get(app.Name); ok {
			t.Fatal("pod B still serves the deleted app")
		}
	})
}

// TestMultiReplica_MissHookFindsNewTable: no SyncNow call; the lookup miss
// on pod B triggers the sync.
func TestMultiReplica_MissHookFindsNewTable(t *testing.T) {
	_, h, owner, app, podB := multiReplicaFixture(t)
	podB.reg.SetMissHook(podB.syncer.OnMiss)

	if _, err := h.CreateAppTableForUser(context.Background(), owner, app.ID, TableRequestBody{
		Name: "fresh", Columns: []config.ColumnConfig{{Name: "n", Type: "integer"}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	if _, ok := podB.reg.GetTable(app.Name, "fresh"); !ok {
		t.Fatal("pod B's miss hook should sync and find the table created on pod A")
	}
}

// TestMultiReplica_RunConvergesWithoutSyncNow: pod B only polls.
func TestMultiReplica_RunConvergesWithoutSyncNow(t *testing.T) {
	pool, h, owner, app, _ := multiReplicaFixture(t)
	podB := newTestPod(t, pool, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go podB.syncer.Run(ctx)

	if _, err := h.AddTableColumnForUser(context.Background(), owner, app.ID, "items", config.ColumnConfig{Name: "polled", Type: "text"}, "127.0.0.1"); err != nil {
		t.Fatalf("AddTableColumnForUser: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if tbl, ok := podB.reg.GetTable(app.Name, "items"); ok && len(tbl.Columns) == 3 {
			return
		}
		if time.Now().After(deadline) {
			tbl, _ := podB.reg.GetTable(app.Name, "items")
			t.Fatalf("pod B did not converge within 1s, columns = %v", columnNames(tbl))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestMultiReplica_TriggerIsLoadBearing is the mutation check: with the
// app_tables trigger disabled, the column add no longer reaches pod B. If
// this ever passes with the trigger off, the convergence tests above are
// not actually exercising the trigger path.
func TestMultiReplica_TriggerIsLoadBearing(t *testing.T) {
	pool, h, owner, app, podB := multiReplicaFixture(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `ALTER TABLE zeep_system.app_tables DISABLE TRIGGER bump_state_version`); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE zeep_system.app_tables ENABLE TRIGGER bump_state_version`)
	})

	if _, err := h.AddTableColumnForUser(ctx, owner, app.ID, "items", config.ColumnConfig{Name: "unseen", Type: "text"}, "127.0.0.1"); err != nil {
		t.Fatalf("AddTableColumnForUser: %v", err)
	}
	podB.sync(t)
	tbl, _ := podB.reg.GetTable(app.Name, "items")
	if len(tbl.Columns) != 2 {
		t.Fatalf("without the trigger pod B should stay stale, got columns %v", columnNames(tbl))
	}
}
