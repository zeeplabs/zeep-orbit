package dashboard

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func webhooksTestPool(t *testing.T) *db.Pool {
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
	t.Cleanup(pool.Close)

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.webhook_deliveries, zeep_system.webhook_event_mappings, zeep_system.webhook_subscriptions, zeep_system.apps, zeep_system.dashboard_users CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	return pool
}

func webhooksTestApp(t *testing.T, pool *db.Pool) (appID, userID string) {
	t.Helper()
	userID = testUser(t, pool, "webhook-admin@example.com", "superadmin")
	err := pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"webhook-test-app", userID,
	).Scan(&appID)
	if err != nil {
		t.Fatalf("create test app: %v", err)
	}
	return appID, userID
}

// webhooksTestAppWithMembers seeds an app owned by a real "member"-role
// admin (not the superadmin bypass webhooksTestApp uses), plus an editor
// member — needed by the *ForUser tests (T10-T12) to exercise the
// CanManage() forbidden tier explicitly, the way table_policies' tests do.
func webhooksTestAppWithMembers(t *testing.T, pool *db.Pool) (appID string, actors map[string]*DashboardUser) {
	t.Helper()
	ctx := context.Background()
	actors = map[string]*DashboardUser{
		"admin":    {ID: testUser(t, pool, "wh-foruser-admin@example.com", "member"), Role: "member"},
		"editor":   {ID: testUser(t, pool, "wh-foruser-editor@example.com", "member"), Role: "member"},
		"outsider": {ID: testUser(t, pool, "wh-foruser-outsider@example.com", "member"), Role: "member"},
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"webhook-foruser-app", actors["admin"].ID,
	).Scan(&appID); err != nil {
		t.Fatalf("create test app: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		appID, actors["admin"].ID,
	); err != nil {
		t.Fatalf("seed admin membership: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'editor')`,
		appID, actors["editor"].ID,
	); err != nil {
		t.Fatalf("seed editor membership: %v", err)
	}
	return appID, actors
}

// TestListWebhooksForUser_HappyPathMatchesRESTShape covers T10's Done-when:
// ListWebhooksForUser returns the same webhook list ListWebhooks itself
// returns, for a manager.
func TestListWebhooksForUser_HappyPathMatchesRESTShape(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "for-user list", Method: "POST", EventTypePath: "eventType", CreatedBy: actors["admin"].ID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	rows, err := ListWebhooksForUser(ctx, pool, actors["admin"], appID)
	if err != nil {
		t.Fatalf("ListWebhooksForUser: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != created.ID {
		t.Fatalf("expected 1 webhook matching %+v, got %+v", created, rows)
	}
}

// TestListWebhooksForUser_NonManagerForbidden covers T10's Done-when: an
// editor (a real app member whose role fails CanManage()) is rejected with
// ErrForbidden, the explicit CanManage() tier test design.md's Authorization
// Matrix requires.
func TestListWebhooksForUser_NonManagerForbidden(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	_, err := ListWebhooksForUser(ctx, pool, actors["editor"], appID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanManage()==false), got %v", err)
	}
}

// TestListWebhooksForUser_OutsiderReturnsErrNotFound covers T10's Done-when
// authorization path: an outsider with no membership at all (and not
// superadmin/CanReadAnyApp) gets the same not-found GetApp already returns.
func TestListWebhooksForUser_OutsiderReturnsErrNotFound(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	_, err := ListWebhooksForUser(ctx, pool, actors["outsider"], appID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an outsider with no app visibility, got %v", err)
	}
}

// TestGetWebhookForUser_HappyPathReturnsWebhookAndMappings covers T11's
// Done-when: GetWebhookForUser returns the webhook config plus its event
// mappings for a webhookID belonging to the given appID (spec AC2).
func TestGetWebhookForUser_HappyPathReturnsWebhookAndMappings(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "for-user get", Method: "POST", EventTypePath: "eventType", CreatedBy: actors["admin"].ID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	reg := registry.New()
	reg.Register(&registry.App{
		Config:     config.AppConfig{Name: "webhook-foruser-app"},
		SchemaName: "webhook-foruser-app",
		Tables: map[string]*registry.Table{
			"requests": {
				Name:    "requests",
				Columns: []registry.Column{{Name: "external_id", Type: "text"}},
			},
		},
	})
	if _, err := SaveEventMapping(ctx, pool, reg, "webhook-foruser-app", created.ID, EventMappingDef{
		EventTypeValue: "created",
		Action:         "insert",
		TargetTable:    "requests",
		FieldMappings:  []FieldMappingDef{{SourcePath: "id", Column: "external_id"}},
	}); err != nil {
		t.Fatalf("SaveEventMapping: %v", err)
	}

	wh, mappings, err := GetWebhookForUser(ctx, pool, actors["admin"], appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookForUser: %v", err)
	}
	if wh.ID != created.ID {
		t.Fatalf("expected webhook %s, got %s", created.ID, wh.ID)
	}
	if len(mappings) != 1 || mappings[0].EventTypeValue != "created" {
		t.Fatalf("expected 1 mapping for 'created', got %+v", mappings)
	}
}

// TestGetWebhookForUser_CrossAppWebhookReturnsErrWebhookNotFound covers T11's
// Done-when: a webhookID that belongs to a DIFFERENT app than the given
// appID returns not-found — the explicit cross-app scoping edge case the
// spec calls out (Edge Cases: "never returning another app's webhook data
// through a mismatched app_id").
func TestGetWebhookForUser_CrossAppWebhookReturnsErrWebhookNotFound(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "owned-by-app-a", Method: "POST", EventTypePath: "eventType", CreatedBy: actors["admin"].ID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	var otherAppID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"webhook-foruser-other-app", actors["admin"].ID,
	).Scan(&otherAppID); err != nil {
		t.Fatalf("create other app: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		otherAppID, actors["admin"].ID,
	); err != nil {
		t.Fatalf("seed admin membership on other app: %v", err)
	}

	_, _, err = GetWebhookForUser(ctx, pool, actors["admin"], otherAppID, created.ID)
	if !errors.Is(err, ErrWebhookNotFound) {
		t.Fatalf("expected ErrWebhookNotFound for a webhook belonging to a different app, got %v", err)
	}
}

// TestGetWebhookForUser_NonManagerForbidden covers T11's Done-when: an
// editor (fails CanManage()) is rejected with ErrForbidden.
func TestGetWebhookForUser_NonManagerForbidden(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "editor-attempt", Method: "POST", EventTypePath: "eventType", CreatedBy: actors["admin"].ID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, _, err = GetWebhookForUser(ctx, pool, actors["editor"], appID, created.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanManage()==false), got %v", err)
	}
}

func TestCreateWebhook_HappyPath(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	row, token, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID:         appID,
		Name:          "Google Workspace sync",
		Method:        "POST",
		EventTypePath: "eventType",
		EventIDPath:   "eventId",
		CreatedBy:     userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty plaintext token")
	}
	if row.TokenSecret == token {
		t.Fatal("token_secret must never equal the plaintext token (it must be encrypted)")
	}
	decrypted, err := DecryptWebhookToken(row)
	if err != nil {
		t.Fatalf("DecryptWebhookToken: %v", err)
	}
	if decrypted != token {
		t.Fatalf("decrypted token %q does not round-trip to the original plaintext %q", decrypted, token)
	}
	if row.Status != "capture" {
		t.Fatalf("expected new webhook to start in capture mode, got %q", row.Status)
	}
	if row.EventIDPath == nil || *row.EventIDPath != "eventId" {
		t.Fatalf("expected event_id_path 'eventId', got %v", row.EventIDPath)
	}
}

func TestGetWebhookByID_ScopedAndUnscoped(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	got, err := GetWebhookByID(ctx, pool, appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID scoped: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("expected id %s, got %s", created.ID, got.ID)
	}

	gotUnscoped, err := GetWebhookByID(ctx, pool, "", created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID unscoped: %v", err)
	}
	if gotUnscoped.ID != created.ID {
		t.Fatalf("expected unscoped lookup to resolve id %s, got %s", created.ID, gotUnscoped.ID)
	}

	if _, err := GetWebhookByID(ctx, pool, "00000000-0000-0000-0000-000000000000", created.ID); err != ErrWebhookNotFound {
		t.Fatalf("expected ErrWebhookNotFound when scoped to the wrong app, got %v", err)
	}
}

func TestListWebhooks_NewestFirst(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	first, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "first", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook (first): %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	second, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "second", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook (second): %v", err)
	}

	list, err := ListWebhooks(ctx, pool, appID)
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 webhooks, got %d", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("expected newest-first order [%s, %s], got [%s, %s]", second.ID, first.ID, list[0].ID, list[1].ID)
	}
}

func TestStoreCapturedSample_OverwritesOnSecondCall(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if err := StoreCapturedSample(ctx, pool, created.ID, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("StoreCapturedSample (1st): %v", err)
	}
	got, err := GetWebhookByID(ctx, pool, appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if got.CapturedSample["a"] != float64(1) {
		t.Fatalf("expected captured_sample.a == 1 after first capture, got %v", got.CapturedSample)
	}

	if err := StoreCapturedSample(ctx, pool, created.ID, []byte(`{"b":2}`)); err != nil {
		t.Fatalf("StoreCapturedSample (2nd): %v", err)
	}
	got, err = GetWebhookByID(ctx, pool, appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if _, hasA := got.CapturedSample["a"]; hasA {
		t.Fatalf("expected the first sample to be overwritten, still has key 'a': %v", got.CapturedSample)
	}
	if got.CapturedSample["b"] != float64(2) {
		t.Fatalf("expected captured_sample.b == 2 after overwrite, got %v", got.CapturedSample)
	}
}

func TestActivateWebhook_ZeroMappingsReturnsErrNoMappings(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if err := ActivateWebhook(ctx, pool, created.ID); err != ErrNoMappings {
		t.Fatalf("expected ErrNoMappings, got %v", err)
	}

	got, err := GetWebhookByID(ctx, pool, appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if got.Status != "capture" {
		t.Fatalf("expected status to remain 'capture' after rejected activation, got %q", got.Status)
	}
}

func TestActivateWebhook_WithMappingSucceeds(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.webhook_event_mappings (webhook_id, event_type_value, action, target_table, field_mappings)
		 VALUES ($1, 'user.created', 'insert', 'employees', '[]')`,
		created.ID,
	); err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	if err := ActivateWebhook(ctx, pool, created.ID); err != nil {
		t.Fatalf("ActivateWebhook: %v", err)
	}
	got, err := GetWebhookByID(ctx, pool, appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("expected status 'active', got %q", got.Status)
	}
}

func TestRotateToken_InvalidatesOldToken(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, oldToken, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	newToken, err := RotateToken(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("expected a new token different from the old one")
	}

	got, err := GetWebhookByID(ctx, pool, appID, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if !VerifyWebhookToken(got.TokenSecret, newToken) {
		t.Fatal("expected the new token to verify against the stored secret after rotation")
	}
	if VerifyWebhookToken(got.TokenSecret, oldToken) {
		t.Fatal("expected the old token to no longer verify after rotation")
	}
}

func TestUpdateWebhook_ChangesNameMethodAndPaths(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, token, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "old name", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	updated, err := UpdateWebhook(ctx, pool, created.ID, UpdateWebhookInput{
		Name: "new name", Method: "PUT", EventTypePath: "type", EventIDPath: "id",
	})
	if err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
	if updated.Name != "new name" || updated.Method != "PUT" || updated.EventTypePath != "type" {
		t.Fatalf("expected updated fields, got name=%q method=%q event_type_path=%q", updated.Name, updated.Method, updated.EventTypePath)
	}
	if updated.EventIDPath == nil || *updated.EventIDPath != "id" {
		t.Fatalf("expected event_id_path 'id', got %v", updated.EventIDPath)
	}

	decrypted, err := DecryptWebhookToken(updated)
	if err != nil {
		t.Fatalf("DecryptWebhookToken: %v", err)
	}
	if decrypted != token {
		t.Fatal("expected the token to be untouched by UpdateWebhook")
	}
}

func TestUpdateWebhook_InvalidMethodRejected(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if _, err := UpdateWebhook(ctx, pool, created.ID, UpdateWebhookInput{
		Name: "hook", Method: "DELETE", EventTypePath: "eventType",
	}); !errors.Is(err, ErrInvalidWebhookMethod) {
		t.Fatalf("expected ErrInvalidWebhookMethod, got %v", err)
	}
}

func TestUpdateWebhook_UnknownIDReturnsNotFound(t *testing.T) {
	pool := webhooksTestPool(t)
	ctx := context.Background()

	if _, err := UpdateWebhook(ctx, pool, "00000000-0000-0000-0000-000000000000", UpdateWebhookInput{
		Name: "hook", Method: "POST", EventTypePath: "eventType",
	}); !errors.Is(err, ErrWebhookNotFound) {
		t.Fatalf("expected ErrWebhookNotFound, got %v", err)
	}
}

func TestSoftDeleteWebhook_NeverHardDeletes(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()

	created, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if err := SoftDeleteWebhook(ctx, pool, created.ID); err != nil {
		t.Fatalf("SoftDeleteWebhook: %v", err)
	}

	if _, err := GetWebhookByID(ctx, pool, appID, created.ID); err != ErrWebhookNotFound {
		t.Fatalf("expected ErrWebhookNotFound for a soft-deleted webhook, got %v", err)
	}

	var deletedAtSet bool
	if err := pool.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM zeep_system.webhook_subscriptions WHERE id = $1`,
		created.ID,
	).Scan(&deletedAtSet); err != nil {
		t.Fatalf("check row still physically present: %v", err)
	}
	if !deletedAtSet {
		t.Fatal("expected deleted_at to be set (soft delete), row must still physically exist")
	}
}
