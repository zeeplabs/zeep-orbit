package dashboard

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
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
