package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"github.com/zeeplabs/zeep-orbit/internal/storage"
)

func appsStoreTestPool(t *testing.T) *db.Pool {
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

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.app_tables, zeep_system.apps, zeep_system.dashboard_users CASCADE`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.app_tables, zeep_system.apps, zeep_system.dashboard_users CASCADE`)
	})

	return pool
}

func appsStoreTestApp(t *testing.T, pool *db.Pool) (appID string) {
	t.Helper()
	ownerID := testUser(t, pool, "owner@example.com", "superadmin")
	err := pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"test-app", ownerID,
	).Scan(&appID)
	if err != nil {
		t.Fatalf("create test app: %v", err)
	}
	return appID
}

// TestRedactAuthProviderSecrets covers redactAuthProviderSecrets in
// isolation (no DB needed) — the exact shape internal/auth/google.go's
// getGoogleConfig reads (enabled/client_id/client_secret/redirect_url).
func TestRedactAuthProviderSecrets(t *testing.T) {
	in := json.RawMessage(`{"google":{"enabled":true,"client_id":"abc123","client_secret":"super-secret-value","redirect_url":"https://example.com/cb"}}`)
	out := redactAuthProviderSecrets(in)

	if strings.Contains(string(out), "super-secret-value") {
		t.Fatalf("expected client_secret to be stripped, got %s", out)
	}
	var decoded map[string]map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal redacted output: %v", err)
	}
	google := decoded["google"]
	if google["client_id"] != "abc123" {
		t.Errorf("expected client_id to survive redaction, got %+v", google)
	}
	if google["redirect_url"] != "https://example.com/cb" {
		t.Errorf("expected redirect_url to survive redaction, got %+v", google)
	}
	if _, hasSecret := google["client_secret"]; hasSecret {
		t.Errorf("expected client_secret key to be removed entirely, got %+v", google)
	}
	if set, _ := google["client_secret_set"].(bool); !set {
		t.Errorf("expected client_secret_set=true, got %+v", google)
	}

	// Malformed input fails closed to {} rather than passing anything
	// unrecognized through unredacted.
	if got := string(redactAuthProviderSecrets(json.RawMessage(`not json`))); got != "{}" {
		t.Errorf("expected malformed input to fail closed to {}, got %s", got)
	}
}

// TestAppRowRedactSecrets_StripsCredentialsButKeepsDisplayFields is the
// regression test for the incident that motivated RedactSecrets: a real
// production app's schema (asset-manager) returned a plaintext Google
// OAuth client_secret and AWS S3 secret_access_key through
// GetApp/ListApps/orbit_list_apps, none of which masked anything before
// this fix. Seeds real-looking secrets via the same store functions the
// dashboard UI uses to save them, fetches through both GetApp and
// ListApps, and proves the secret values are gone after RedactSecrets()
// while the fields a UI legitimately needs to display (bucket, region,
// client_id, jwt_secret presence) survive.
func TestAppRowRedactSecrets_StripsCredentialsButKeepsDisplayFields(t *testing.T) {
	pool := appsStoreTestPool(t)
	ctx := context.Background()
	appID := appsStoreTestApp(t, pool)

	const fakeSecretKey = "AKIAFAKESECRETACCESSKEYVALUE"
	const fakeClientSecret = "fake-google-oauth-client-secret"

	if err := UpdateAppStorageConfig(ctx, pool, appID, &storage.StorageConfig{
		Bucket:          "starbem-apps",
		Region:          "us-east-1",
		Endpoint:        "https://s3.amazonaws.com",
		AccessKeyID:     "AKIAFAKEACCESSKEYID",
		SecretAccessKey: fakeSecretKey,
	}); err != nil {
		t.Fatalf("UpdateAppStorageConfig: %v", err)
	}
	authProviders := json.RawMessage(fmt.Sprintf(
		`{"google":{"enabled":true,"client_id":"fake-client-id.apps.googleusercontent.com","client_secret":%q,"redirect_url":"https://example.com/cb"}}`,
		fakeClientSecret,
	))
	if err := UpdateAppAuthProvidersRaw(ctx, pool, appID, authProviders); err != nil {
		t.Fatalf("UpdateAppAuthProvidersRaw: %v", err)
	}

	user := &DashboardUser{ID: testUser(t, pool, "reader@example.com", "superadmin"), Role: "superadmin"}

	assertRedacted := func(t *testing.T, app *AppRow, label string) {
		t.Helper()
		app.RedactSecrets()
		if app.JWTSecret != "" {
			t.Errorf("%s: expected JWTSecret stripped, got non-empty", label)
		}
		if app.StorageConfig == nil {
			t.Fatalf("%s: expected StorageConfig to survive redaction (non-secret fields still needed)", label)
		}
		if app.StorageConfig.SecretAccessKey != "" {
			t.Errorf("%s: expected SecretAccessKey stripped, got non-empty", label)
		}
		if app.StorageConfig.Bucket != "starbem-apps" {
			t.Errorf("%s: expected Bucket to survive redaction, got %q", label, app.StorageConfig.Bucket)
		}
		if strings.Contains(string(app.AuthProviders), fakeClientSecret) {
			t.Errorf("%s: expected client_secret value gone from AuthProviders, found in %s", label, app.AuthProviders)
		}
		if !strings.Contains(string(app.AuthProviders), "fake-client-id.apps.googleusercontent.com") {
			t.Errorf("%s: expected client_id to survive redaction, got %s", label, app.AuthProviders)
		}
	}

	getApp, _, err := GetApp(ctx, pool, appID, user)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if getApp.StorageConfig.SecretAccessKey != fakeSecretKey {
		t.Fatal("sanity check failed: GetApp should return the real secret before redaction")
	}
	assertRedacted(t, getApp, "GetApp")

	listed, err := ListApps(ctx, pool, user)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	var found *AppRow
	for _, a := range listed {
		if a.ID == appID {
			found = a
		}
	}
	if found == nil {
		t.Fatal("expected the test app in ListApps results")
	}
	if found.StorageConfig.SecretAccessKey != fakeSecretKey {
		t.Fatal("sanity check failed: ListApps should return the real secret before redaction")
	}
	assertRedacted(t, found, "ListApps")
}

// TestListAppsHandler_ResponseBodyNeverContainsSecrets and
// TestGetAppHandler_ResponseBodyNeverContainsSecrets are the boundary-level
// regression tests for the incident: they call the real HTTP handlers
// (not RedactSecrets directly) and assert the actual serialized response
// body — the thing an MCP client or a browser's network tab would
// actually see — never contains the fake secret values, catching a
// regression where a handler stops calling RedactSecrets() even though
// the store-level tests above would keep passing.
func TestListAppsHandler_ResponseBodyNeverContainsSecrets(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	const fakeSecretKey = "AKIAFAKESECRETACCESSKEYVALUE"
	const fakeClientSecret = "fake-google-oauth-client-secret"
	seedAppSecrets(t, pool, appID, fakeSecretKey, fakeClientSecret)

	h := NewHandler(pool, registry.New(), zap.NewNop())
	user := &DashboardUser{ID: testUser(t, pool, "reader-list@example.com", "superadmin"), Role: "superadmin"}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/apps", nil)
	req = withUser(req, user)
	rr := httptest.NewRecorder()
	h.ListApps(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, fakeSecretKey) {
		t.Errorf("ListApps response body leaked the S3 secret key: %s", body)
	}
	if strings.Contains(body, fakeClientSecret) {
		t.Errorf("ListApps response body leaked the OAuth client_secret: %s", body)
	}
	if !strings.Contains(body, "starbem-apps") {
		t.Errorf("expected non-secret bucket name to survive redaction, got %s", body)
	}
}

func TestGetAppHandler_ResponseBodyNeverContainsSecrets(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	const fakeSecretKey = "AKIAFAKESECRETACCESSKEYVALUE"
	const fakeClientSecret = "fake-google-oauth-client-secret"
	seedAppSecrets(t, pool, appID, fakeSecretKey, fakeClientSecret)

	h := NewHandler(pool, registry.New(), zap.NewNop())
	user := &DashboardUser{ID: testUser(t, pool, "reader-get@example.com", "superadmin"), Role: "superadmin"}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/apps/"+appID, nil)
	req = withUser(req, user)
	req = withChiParams(req, map[string]string{"id": appID})
	rr := httptest.NewRecorder()
	h.GetApp(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, fakeSecretKey) {
		t.Errorf("GetApp response body leaked the S3 secret key: %s", body)
	}
	if strings.Contains(body, fakeClientSecret) {
		t.Errorf("GetApp response body leaked the OAuth client_secret: %s", body)
	}
	if strings.Contains(body, "jwt_secret") {
		// jwt_secret has omitempty; a blanked string must not even appear
		// as a key, not just an empty value, to rule out a partial fix.
		t.Errorf("GetApp response body should omit jwt_secret entirely once blanked, got %s", body)
	}
}

// seedAppSecrets writes a fake AWS storage config and a fake Google OAuth
// provider config onto appID via the same store functions the dashboard UI
// uses to save them — not a direct SQL UPDATE — so these tests exercise
// the identical write path a real save would.
func seedAppSecrets(t *testing.T, pool *db.Pool, appID, fakeSecretKey, fakeClientSecret string) {
	t.Helper()
	ctx := context.Background()
	if err := UpdateAppStorageConfig(ctx, pool, appID, &storage.StorageConfig{
		Bucket:          "starbem-apps",
		Region:          "us-east-1",
		Endpoint:        "https://s3.amazonaws.com",
		AccessKeyID:     "AKIAFAKEACCESSKEYID",
		SecretAccessKey: fakeSecretKey,
	}); err != nil {
		t.Fatalf("UpdateAppStorageConfig: %v", err)
	}
	authProviders := json.RawMessage(fmt.Sprintf(
		`{"google":{"enabled":true,"client_id":"fake-client-id.apps.googleusercontent.com","client_secret":%q,"redirect_url":"https://example.com/cb"}}`,
		fakeClientSecret,
	))
	if err := UpdateAppAuthProvidersRaw(ctx, pool, appID, authProviders); err != nil {
		t.Fatalf("UpdateAppAuthProvidersRaw: %v", err)
	}
}

// TestOrbitListAppsTool_ResponseNeverContainsSecrets is the MCP-side
// boundary test — orbit_list_apps' actual tool response (StructuredContent,
// the thing an MCP client receives) must never carry the same secrets,
// even though it goes through a different call path (ListAppsForUser, not
// the REST ListApps handler) than the two tests above.
func TestOrbitListAppsTool_ResponseNeverContainsSecrets(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	const fakeSecretKey = "AKIAFAKESECRETACCESSKEYVALUE"
	const fakeClientSecret = "fake-google-oauth-client-secret"
	seedAppSecrets(t, pool, appID, fakeSecretKey, fakeClientSecret)

	apps, err := ListAppsForUser(context.Background(), pool, &DashboardUser{
		ID: testUser(t, pool, "reader-mcp@example.com", "superadmin"), Role: "superadmin",
	})
	if err != nil {
		t.Fatalf("ListAppsForUser: %v", err)
	}
	out, err := json.Marshal(apps)
	if err != nil {
		t.Fatalf("marshal ListAppsForUser result: %v", err)
	}
	body := string(out)
	if strings.Contains(body, fakeSecretKey) {
		t.Errorf("orbit_list_apps (ListAppsForUser) leaked the S3 secret key: %s", body)
	}
	if strings.Contains(body, fakeClientSecret) {
		t.Errorf("orbit_list_apps (ListAppsForUser) leaked the OAuth client_secret: %s", body)
	}
}

// TestMergeAppAuthProviders_EmptyOrAbsentFieldKeepsExisting is the pure
// unit test for the merge semantics: absent, nil, or empty-string fields
// in the incoming payload must not clobber the corresponding stored value.
func TestMergeAppAuthProviders_EmptyOrAbsentFieldKeepsExisting(t *testing.T) {
	current := json.RawMessage(`{"google":{"enabled":true,"client_id":"old-client-id","client_secret":"old-secret","redirect_url":"https://old.example.com/cb"}}`)

	// Incoming sends an empty client_secret (the exact shape LoginTab's
	// save() sends whenever the admin doesn't retype the secret) plus a
	// changed redirect_url.
	incoming := json.RawMessage(`{"google":{"enabled":true,"client_id":"old-client-id","client_secret":"","redirect_url":"https://new.example.com/cb"}}`)

	merged, err := mergeAppAuthProviders(current, incoming)
	if err != nil {
		t.Fatalf("mergeAppAuthProviders: %v", err)
	}

	var decoded map[string]map[string]any
	if err := json.Unmarshal(merged, &decoded); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	google := decoded["google"]
	if google["client_secret"] != "old-secret" {
		t.Errorf("expected client_secret preserved from current, got %+v", google)
	}
	if google["redirect_url"] != "https://new.example.com/cb" {
		t.Errorf("expected redirect_url updated to the new value, got %+v", google)
	}

	// A non-empty client_secret in incoming DOES overwrite (secret
	// rotation must still work).
	rotateIncoming := json.RawMessage(`{"google":{"enabled":true,"client_id":"old-client-id","client_secret":"rotated-secret","redirect_url":"https://old.example.com/cb"}}`)
	rotated, err := mergeAppAuthProviders(current, rotateIncoming)
	if err != nil {
		t.Fatalf("mergeAppAuthProviders (rotate): %v", err)
	}
	var rotatedDecoded map[string]map[string]any
	if err := json.Unmarshal(rotated, &rotatedDecoded); err != nil {
		t.Fatalf("unmarshal rotated: %v", err)
	}
	if rotatedDecoded["google"]["client_secret"] != "rotated-secret" {
		t.Errorf("expected client_secret to rotate when incoming provides a new one, got %+v", rotatedDecoded["google"])
	}

	// A provider entirely absent from incoming is dropped, not merged —
	// incoming is the full desired set.
	dropped, err := mergeAppAuthProviders(current, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("mergeAppAuthProviders (drop): %v", err)
	}
	var droppedDecoded map[string]map[string]any
	if err := json.Unmarshal(dropped, &droppedDecoded); err != nil {
		t.Fatalf("unmarshal dropped: %v", err)
	}
	if _, exists := droppedDecoded["google"]; exists {
		t.Errorf("expected google to be dropped when omitted from incoming entirely, got %+v", droppedDecoded)
	}

	// A literal JSON null body must not wipe every configured provider —
	// it unmarshals to a nil map, which without an explicit guard would
	// fall through to "merged = {}" and delete everything.
	nullResult, err := mergeAppAuthProviders(current, json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("mergeAppAuthProviders (null): %v", err)
	}
	if string(nullResult) != string(current) {
		t.Errorf("expected a literal null incoming payload to be a no-op, got %s (current was %s)", nullResult, current)
	}

	// An explicit enabled:false must still take effect (a bare boolean
	// false is neither nil nor "", so it's a real overwrite, not a
	// keep-existing skip) — this is what lets the dashboard's Google
	// toggle actually disable the provider, not just look disabled in
	// the UI.
	disableIncoming := json.RawMessage(`{"google":{"enabled":false,"client_id":"old-client-id","client_secret":"","redirect_url":"https://old.example.com/cb","allowed_domains":[]}}`)
	disabled, err := mergeAppAuthProviders(current, disableIncoming)
	if err != nil {
		t.Fatalf("mergeAppAuthProviders (disable): %v", err)
	}
	var disabledDecoded map[string]map[string]any
	if err := json.Unmarshal(disabled, &disabledDecoded); err != nil {
		t.Fatalf("unmarshal disabled: %v", err)
	}
	if enabled, _ := disabledDecoded["google"]["enabled"].(bool); enabled {
		t.Errorf("expected enabled:false to actually take effect, got %+v", disabledDecoded["google"])
	}
}

// TestUpdateAppHandler_SavingOtherFieldsDoesNotWipeGoogleClientSecret is
// the end-to-end regression test for M3: reproduces exactly what
// LoginTab.save() (AppDetailsPage.tsx) sends when an admin changes
// redirect_url/allowed_domains without retyping the Google client_secret
// (the field is never re-populated from a GET response, so its state
// starts blank) — before the fix, PUT /dashboard/api/apps/{id} did a raw
// column overwrite and this silently deleted the app's working OAuth
// client_secret.
func TestUpdateAppHandler_SavingOtherFieldsDoesNotWipeGoogleClientSecret(t *testing.T) {
	pool := appsStoreTestPool(t)
	ctx := context.Background()
	appID := appsStoreTestApp(t, pool)

	const realClientSecret = "the-real-google-client-secret"
	if err := UpdateAppAuthProvidersRaw(ctx, pool, appID, json.RawMessage(fmt.Sprintf(
		`{"google":{"enabled":true,"client_id":"real-client-id","client_secret":%q,"redirect_url":"https://example.com/cb"}}`,
		realClientSecret,
	))); err != nil {
		t.Fatalf("seed UpdateAppAuthProvidersRaw: %v", err)
	}

	h := NewHandler(pool, registry.New(), zap.NewNop())
	user := &DashboardUser{ID: testUser(t, pool, "editor@example.com", "superadmin"), Role: "superadmin"}

	body, _ := json.Marshal(AppRequestBody{
		Name:             "test-app",
		AuthEmailEnabled: true,
		AuthProviders: json.RawMessage(
			`{"google":{"enabled":true,"client_id":"real-client-id","client_secret":"","redirect_url":"https://updated.example.com/cb"}}`,
		),
	})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/api/apps/"+appID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, user)
	req = withChiParams(req, map[string]string{"id": appID})
	rr := httptest.NewRecorder()
	h.UpdateApp(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	var storedRaw []byte
	if err := pool.QueryRow(ctx, `SELECT auth_providers FROM zeep_system.apps WHERE id = $1`, appID).Scan(&storedRaw); err != nil {
		t.Fatalf("read stored auth_providers: %v", err)
	}
	var stored map[string]map[string]any
	if err := json.Unmarshal(storedRaw, &stored); err != nil {
		t.Fatalf("unmarshal stored auth_providers: %v", err)
	}
	if stored["google"]["client_secret"] != realClientSecret {
		t.Fatalf("expected client_secret to survive the update (not wiped by the empty string LoginTab sends), got %+v", stored["google"])
	}
	if stored["google"]["redirect_url"] != "https://updated.example.com/cb" {
		t.Errorf("expected redirect_url to actually update, got %+v", stored["google"])
	}
}

func TestInsertAppTable_RoundTripsIndexes(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	table := AppTableRow{
		Name: "users",
		RLS:  "",
		Columns: []config.ColumnConfig{
			{Name: "email", Type: "text"},
		},
		Indexes: []config.IndexConfig{
			{Name: "idx_users_email", Columns: []string{"email"}, Unique: true},
		},
	}

	row, err := InsertAppTable(context.Background(), pool, appID, table)
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}
	if len(row.Indexes) != 1 || row.Indexes[0].Name != "idx_users_email" {
		t.Fatalf("expected indexes to round-trip, got %+v", row.Indexes)
	}

	loaded, err := loadAppTables(context.Background(), pool, appID)
	if err != nil {
		t.Fatalf("loadAppTables: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Indexes) != 1 {
		t.Fatalf("expected loaded table to carry indexes, got %+v", loaded)
	}
}

// TestEnduserRolesConfig_DefaultsAndRoundTrips covers ROLECFG-01/ROLECFG-07:
// a newly created app gets the seeded ["member"] default, and GetApp/ListApps
// decode a custom list back exactly as persisted.
func TestEnduserRolesConfig_DefaultsAndRoundTrips(t *testing.T) {
	pool := appsStoreTestPool(t)
	ownerID := testUser(t, pool, "roles-owner@example.com", "superadmin")
	owner, err := GetUser(context.Background(), pool, ownerID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	created, err := CreateApp(context.Background(), pool, "roles-app", ownerID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if len(created.EnduserRolesConfig) != 1 || created.EnduserRolesConfig[0] != "member" {
		t.Fatalf("CreateApp: expected default [\"member\"], got %+v", created.EnduserRolesConfig)
	}

	got, _, err := GetApp(context.Background(), pool, created.ID, owner)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(got.EnduserRolesConfig) != 1 || got.EnduserRolesConfig[0] != "member" {
		t.Fatalf("GetApp: expected default [\"member\"], got %+v", got.EnduserRolesConfig)
	}

	// Persist a custom list directly (bypassing the store's own writer, which
	// is added in a later task) and confirm both GetApp and ListApps decode
	// it back exactly.
	if _, err := pool.Exec(context.Background(),
		`UPDATE zeep_system.apps SET enduser_roles_config = $1 WHERE id = $2`,
		`["member","viewer"]`, created.ID,
	); err != nil {
		t.Fatalf("seed custom roles: %v", err)
	}

	got, _, err = GetApp(context.Background(), pool, created.ID, owner)
	if err != nil {
		t.Fatalf("GetApp after custom roles: %v", err)
	}
	if len(got.EnduserRolesConfig) != 2 || got.EnduserRolesConfig[0] != "member" || got.EnduserRolesConfig[1] != "viewer" {
		t.Fatalf("GetApp: expected [\"member\",\"viewer\"], got %+v", got.EnduserRolesConfig)
	}

	list, err := ListApps(context.Background(), pool, owner)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	var found *AppRow
	for _, a := range list {
		if a.ID == created.ID {
			found = a
		}
	}
	if found == nil {
		t.Fatalf("ListApps: created app not found")
	}
	if len(found.EnduserRolesConfig) != 2 || found.EnduserRolesConfig[0] != "member" || found.EnduserRolesConfig[1] != "viewer" {
		t.Fatalf("ListApps: expected [\"member\",\"viewer\"], got %+v", found.EnduserRolesConfig)
	}
}

// TestCountAppUsersByRole covers ROLECFG-05: zero use, use by end-users, and
// the isPgRelationNotFound fallback (schema/table not provisioned yet -> 0,
// no error, matching CountAppUsersByProvider's existing pattern).
func TestCountAppUsersByRole(t *testing.T) {
	pool := appsStoreTestPool(t)
	ctx := context.Background()
	schema := "cnt_role_test"

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	})
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q."_auth_users" (
		id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email         TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL DEFAULT '',
		role          TEXT NOT NULL DEFAULT 'member'
	)`, schema)); err != nil {
		t.Fatalf("create _auth_users: %v", err)
	}

	count, err := CountAppUsersByRole(ctx, pool, schema, "admin")
	if err != nil {
		t.Fatalf("CountAppUsersByRole (zero use): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 with no users, got %d", count)
	}

	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q."_auth_users" (email, role) VALUES ($1, 'admin'), ($2, 'admin'), ($3, 'member')`, schema),
		"cnt-a@example.com", "cnt-b@example.com", "cnt-c@example.com",
	); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	count, err = CountAppUsersByRole(ctx, pool, schema, "admin")
	if err != nil {
		t.Fatalf("CountAppUsersByRole (in use): %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 users with role admin, got %d", count)
	}

	count, err = CountAppUsersByRole(ctx, pool, "no_such_schema_xyz", "admin")
	if err != nil {
		t.Fatalf("CountAppUsersByRole (missing relation): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 for a schema whose _auth_users doesn't exist, got %d", count)
	}
}

// TestCountTablePoliciesByRole covers ROLECFG-05: zero use, use by a policy,
// and use by both an end-user AND a policy at once (the combined case the
// handler diffs against when blocking a removal).
func TestCountTablePoliciesByRole(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	ctx := context.Background()
	appID, userID := tablePoliciesTestApp(t, pool)

	count, err := CountTablePoliciesByRole(ctx, pool, appID, "admin")
	if err != nil {
		t.Fatalf("CountTablePoliciesByRole (zero use): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 with no policies, got %d", count)
	}

	def := PolicyDef{
		Name:   "admin_only",
		Action: "select",
		Roles:  []string{"admin"},
		Clauses: []PolicyClause{
			{Column: "requester_id", Operator: "=", ValueSource: "claim", Value: "sub"},
		},
	}
	if _, err := CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def, userID); err != nil {
		t.Fatalf("CreateTablePolicy: %v", err)
	}

	count, err = CountTablePoliciesByRole(ctx, pool, appID, "admin")
	if err != nil {
		t.Fatalf("CountTablePoliciesByRole (used by policy): %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 policy referencing admin, got %d", count)
	}

	count, err = CountTablePoliciesByRole(ctx, pool, appID, "viewer")
	if err != nil {
		t.Fatalf("CountTablePoliciesByRole (unrelated role): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 policies referencing viewer, got %d", count)
	}

	// Combined case: same role also assigned to an end-user in a real app
	// schema (separate from tp_test) — both counters report nonzero for it.
	schema := "cnt_both_test"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	})
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q."_auth_users" (
		id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email         TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL DEFAULT '',
		role          TEXT NOT NULL DEFAULT 'member'
	)`, schema)); err != nil {
		t.Fatalf("create _auth_users: %v", err)
	}
	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q."_auth_users" (email, role) VALUES ($1, 'admin')`, schema),
		"cnt-both@example.com",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	userCount, err := CountAppUsersByRole(ctx, pool, schema, "admin")
	if err != nil {
		t.Fatalf("CountAppUsersByRole (combined case): %v", err)
	}
	policyCount, err := CountTablePoliciesByRole(ctx, pool, appID, "admin")
	if err != nil {
		t.Fatalf("CountTablePoliciesByRole (combined case): %v", err)
	}
	if userCount != 1 || policyCount != 1 {
		t.Fatalf("expected both counters to be 1 in the combined case, got userCount=%d policyCount=%d", userCount, policyCount)
	}
}

// TestUpdateAppEnduserRoles covers ROLECFG-02: persists a populated list, an
// empty list (the deliberate-empty edge case from spec.md), round-trips
// through GetApp, and returns ErrNotFound for a nonexistent app ID.
func TestUpdateAppEnduserRoles(t *testing.T) {
	pool := appsStoreTestPool(t)
	ctx := context.Background()
	ownerID := testUser(t, pool, "roles-update-owner@example.com", "superadmin")
	owner, err := GetUser(ctx, pool, ownerID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	app, err := CreateApp(ctx, pool, "roles-update-app", ownerID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if err := UpdateAppEnduserRoles(ctx, pool, app.ID, []string{"member", "viewer"}); err != nil {
		t.Fatalf("UpdateAppEnduserRoles (populated): %v", err)
	}
	got, _, err := GetApp(ctx, pool, app.ID, owner)
	if err != nil {
		t.Fatalf("GetApp after populated update: %v", err)
	}
	if len(got.EnduserRolesConfig) != 2 || got.EnduserRolesConfig[0] != "member" || got.EnduserRolesConfig[1] != "viewer" {
		t.Fatalf("expected [\"member\",\"viewer\"], got %+v", got.EnduserRolesConfig)
	}

	if err := UpdateAppEnduserRoles(ctx, pool, app.ID, []string{}); err != nil {
		t.Fatalf("UpdateAppEnduserRoles (empty): %v", err)
	}
	got, _, err = GetApp(ctx, pool, app.ID, owner)
	if err != nil {
		t.Fatalf("GetApp after empty update: %v", err)
	}
	if len(got.EnduserRolesConfig) != 0 {
		t.Fatalf("expected an empty list after deliberate removal, got %+v", got.EnduserRolesConfig)
	}

	if err := UpdateAppEnduserRoles(ctx, pool, "00000000-0000-0000-0000-000000000000", []string{"member"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent app, got %v", err)
	}
}

func TestUpdateAppTable_RoundTripsIndexes(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	created, err := InsertAppTable(context.Background(), pool, appID, AppTableRow{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "email", Type: "text"}},
	})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	newIndexes := []config.IndexConfig{{Name: "idx_users_email", Columns: []string{"email"}, Unique: true}}
	updated, err := UpdateAppTable(context.Background(), pool, appID, created.ID, "", "", created.Columns, newIndexes)
	if err != nil {
		t.Fatalf("UpdateAppTable: %v", err)
	}
	if len(updated.Indexes) != 1 || updated.Indexes[0].Name != "idx_users_email" {
		t.Fatalf("expected updated indexes to round-trip, got %+v", updated.Indexes)
	}
}

// setupPhysicalTable creates a real schema+table (outside app_tables
// metadata) so UpdateAppTable's provisioner.EnsureRowLevelSecurity call has
// something real to ALTER — apps_store_test.go's other tests only touch the
// zeep_system.app_tables metadata row, never a physical table.
func setupPhysicalTable(t *testing.T, pool *db.Pool, schema, table string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schema)); err != nil {
		t.Fatalf("create schema %q: %v", schema, err)
	}
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(
		`CREATE TABLE %q.%q (
			id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			owner_id UUID NOT NULL
		)`, schema, table)); err != nil {
		t.Fatalf("create table %q.%q: %v", schema, table, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
}

// TestUpdateAppTable_SwitchToPolicy_EnablesRowLevelSecurity covers T7's
// "Done when": a table with an existing legacy RLS mode (no policy ever
// created, so RLS was never enabled lazily) that switches to "policy" must
// come out with native RLS enabled — the mechanism spec.md AC P1-2/P3-3
// relies on for the fail-closed guarantee.
func TestUpdateAppTable_SwitchToPolicy_EnablesRowLevelSecurity(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)
	schema := "test_app"
	setupPhysicalTable(t, pool, schema, "widgets")

	created, err := InsertAppTable(context.Background(), pool, appID, AppTableRow{
		Name: "widgets",
		RLS:  "enabled",
	})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	if relRowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS disabled before switching to policy mode")
	}

	updated, err := UpdateAppTable(context.Background(), pool, appID, created.ID, schema, "policy", created.Columns, created.Indexes)
	if err != nil {
		t.Fatalf("UpdateAppTable: %v", err)
	}
	if updated.RLS != "policy" {
		t.Fatalf("expected rls %q, got %q", "policy", updated.RLS)
	}
	if !relRowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS enabled after switching to policy mode")
	}
}

// TestUpdateAppTable_SwitchToPolicy_PreservesExistingData covers T7's "Done
// when": the mode switch must not recreate the table or lose data already
// stored in it (spec.md AC P3-2).
func TestUpdateAppTable_SwitchToPolicy_PreservesExistingData(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)
	schema := "test_app"
	setupPhysicalTable(t, pool, schema, "widgets")

	ownerID := testUser(t, pool, "widget-owner@example.com", "superadmin")
	if _, err := pool.Exec(context.Background(),
		fmt.Sprintf(`INSERT INTO %q.widgets (owner_id) VALUES ($1)`, schema), ownerID,
	); err != nil {
		t.Fatalf("seed widgets row: %v", err)
	}

	created, err := InsertAppTable(context.Background(), pool, appID, AppTableRow{
		Name: "widgets",
		RLS:  "enabled",
	})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	if _, err := UpdateAppTable(context.Background(), pool, appID, created.ID, schema, "policy", created.Columns, created.Indexes); err != nil {
		t.Fatalf("UpdateAppTable: %v", err)
	}

	var count int
	if err := pool.QueryRow(context.Background(), fmt.Sprintf(`SELECT count(*) FROM %q.widgets`, schema)).Scan(&count); err != nil {
		t.Fatalf("count widgets rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the pre-existing row to survive the mode switch, got count=%d", count)
	}
}

// TestUpdateAppTable_SwitchPolicyToEnabled_KeepsRowLevelSecurityEnabled
// covers T7's "Done when": switching "policy" → "enabled" must keep RLS
// enabled — RLS enablement is a one-way ratchet here (RLSP-08), never
// disabled by a mode switch back.
func TestUpdateAppTable_SwitchPolicyToEnabled_KeepsRowLevelSecurityEnabled(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)
	schema := "test_app"
	setupPhysicalTable(t, pool, schema, "widgets")

	created, err := InsertAppTable(context.Background(), pool, appID, AppTableRow{
		Name: "widgets",
		RLS:  "policy",
	})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}
	if _, err := UpdateAppTable(context.Background(), pool, appID, created.ID, schema, "policy", created.Columns, created.Indexes); err != nil {
		t.Fatalf("UpdateAppTable (seed policy mode): %v", err)
	}
	if !relRowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS enabled after seeding policy mode")
	}

	updated, err := UpdateAppTable(context.Background(), pool, appID, created.ID, schema, "enabled", created.Columns, created.Indexes)
	if err != nil {
		t.Fatalf("UpdateAppTable (switch back to enabled): %v", err)
	}
	if updated.RLS != "enabled" {
		t.Fatalf("expected rls %q, got %q", "enabled", updated.RLS)
	}
	if !relRowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS to remain enabled after switching back to enabled")
	}
}
