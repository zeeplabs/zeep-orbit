package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// webhookActiveFixture is the shared setup for T7/T8 active-mode tests: a
// real app + physical "employees" table (the context.md Google Workspace
// employee-sync use case), registered in testReg under a unique name so
// parallel-ish sequential test functions never collide, plus one webhook
// wired to that app.
type webhookActiveFixture struct {
	appName string
	schema  string
	wh      dashboard.WebhookRow
	token   string
	appID   string
}

func setupWebhookActiveFixture(t *testing.T) webhookActiveFixture {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	appName := "wh-active-" + suffix
	schema := strings.ReplaceAll(appName, "-", "_")

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, password_hash, role) VALUES ($1, '', 'superadmin') RETURNING id`,
		appName+"@example.com",
	).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}

	var appID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		appName, userID,
	).Scan(&appID); err != nil {
		t.Fatalf("create test app: %v", err)
	}

	setup := []string{
		"CREATE SCHEMA " + schema,
		`CREATE TABLE ` + schema + `.employees (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			external_id TEXT NOT NULL,
			full_name   TEXT,
			email       TEXT,
			created_at  TIMESTAMPTZ DEFAULT now(),
			updated_at  TIMESTAMPTZ DEFAULT now(),
			deleted_at  TIMESTAMPTZ
		)`,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO zeep_app_enduser`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	testReg.Register(&registry.App{
		Config:     config.AppConfig{Name: appName},
		SchemaName: schema,
		Tables: map[string]*registry.Table{
			"employees": {
				Name: "employees",
				Columns: []registry.Column{
					{Name: "external_id", Type: "text", Required: true},
					{Name: "full_name", Type: "text"},
					{Name: "email", Type: "text"},
				},
			},
		},
	})
	t.Cleanup(func() { testReg.Unregister(appName) })

	wh, token, err := dashboard.CreateWebhook(ctx, testPool, dashboard.CreateWebhookInput{
		AppID:         appID,
		Name:          "employee sync",
		Method:        "POST",
		EventTypePath: "eventType",
		EventIDPath:   "eventId",
		CreatedBy:     userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	return webhookActiveFixture{appName: appName, schema: schema, wh: wh, token: token, appID: appID}
}

func (f webhookActiveFixture) activateWithInsertMapping(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := dashboard.SaveEventMapping(ctx, testPool, testReg, f.appName, f.wh.ID, dashboard.EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []dashboard.FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
			{SourcePath: "user.name", Column: "full_name"},
		},
	}); err != nil {
		t.Fatalf("SaveEventMapping: %v", err)
	}
	if err := dashboard.ActivateWebhook(ctx, testPool, f.wh.ID); err != nil {
		t.Fatalf("ActivateWebhook: %v", err)
	}
}

func postWebhook(router http.Handler, wh dashboard.WebhookRow, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/"+token, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func buildActiveWebhookRouter(h *WebhookHandler) http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/hooks/{webhookId}/{token}", h.HandleWebhookDelivery)
	return r
}

func TestWebhookActive_UnmappedEventTypeReturns200NoWrite(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.unmapped","user":{"id":"u-1"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unmapped event type, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no row written for an unmapped event type, got %d", count)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "unmapped" {
		t.Fatalf("expected 1 delivery logged with outcome=unmapped, got %+v", list)
	}
}

func TestWebhookActive_DuplicateEventIDSkipsSecondWrite(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	body := `{"eventType":"user.created","eventId":"evt-dup-1","user":{"id":"u-1","name":"Ana"}}`

	rec := postWebhook(router, f.wh, f.token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postWebhook(router, f.wh, f.token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("second (duplicate) call: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row despite 2 calls with the same event id, got %d", count)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 delivery log entries (one per call), got %d", len(list))
	}
	if list[0].Outcome != "duplicate_skipped" {
		t.Fatalf("expected the newest (second) delivery outcome=duplicate_skipped, got %q", list[0].Outcome)
	}
	if list[1].Outcome != "inserted" {
		t.Fatalf("expected the first delivery outcome=inserted, got %q", list[1].Outcome)
	}
}

// TestWebhookActive_ConcurrentDuplicateEventIDInsertsOnlyOnce proves the
// check-then-act dedup race is actually closed by lockEventID: two
// deliveries with the same event id fired at once (the normal
// provider-retries-a-slow-request pattern) must still produce exactly one
// row, not two — idx_webhook_deliveries_dedup alone can't guarantee this,
// since it isn't a unique index.
func TestWebhookActive_ConcurrentDuplicateEventIDInsertsOnlyOnce(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	body := `{"eventType":"user.created","eventId":"evt-race-1","user":{"id":"u-race","name":"Ana"}}`

	var wg sync.WaitGroup
	results := make([]int, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = postWebhook(router, f.wh, f.token, body).Code
		}(i)
	}
	wg.Wait()

	for _, code := range results {
		if code != http.StatusOK {
			t.Fatalf("expected both concurrent calls to return 200, got %v", results)
		}
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row despite 2 concurrent calls with the same event id, got %d", count)
	}
}

// TestWebhookActive_ConcurrentDistinctEventIDsDoNotExhaustPool proves
// lockEventID's held connection no longer competes with the main pool
// (internal/db/client.go MaxConns=10) for the connections the dedup
// check/write/log queries need to finish and release the lock. Before the
// dedicated dedupLockPool fix, this many concurrent deliveries — each with
// a distinct event id, so the lock itself never serializes them — deadlocked
// the whole process: every main-pool connection ended up held by a
// lockEventID call waiting on a connection the writes needed to release.
func TestWebhookActive_ConcurrentDistinctEventIDsDoNotExhaustPool(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	const n = 15 // above db.MaxConns (10) so a per-connection lock would starve the pool
	var wg sync.WaitGroup
	results := make([]int, n)
	done := make(chan struct{})
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"eventType":"user.created","eventId":"evt-pool-%d","user":{"id":"u-pool-%d","name":"Ana"}}`, i, i)
			results[i] = postWebhook(router, f.wh, f.token, body).Code
		}(i)
	}
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("deliveries with distinct event ids deadlocked — pool likely exhausted by lockEventID")
	}

	for i, code := range results {
		if code != http.StatusOK {
			t.Fatalf("delivery %d: expected 200, got %d", i, code)
		}
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != n {
		t.Fatalf("expected %d rows (one per distinct event id), got %d", n, count)
	}
}

func TestWebhookActive_InsertHappyPathCreatesRow(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-42","name":"Ana Souza"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var externalID, fullName string
	if err := testPool.QueryRow(context.Background(),
		`SELECT external_id, full_name FROM `+f.schema+`.employees LIMIT 1`,
	).Scan(&externalID, &fullName); err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if externalID != "u-42" || fullName != "Ana Souza" {
		t.Fatalf("expected external_id=u-42 full_name='Ana Souza', got external_id=%q full_name=%q", externalID, fullName)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "inserted" {
		t.Fatalf("expected 1 delivery logged with outcome=inserted, got %+v", list)
	}
	if list[0].TargetRowID == nil || *list[0].TargetRowID == "" {
		t.Fatal("expected target_row_id to be populated on a successful insert")
	}
}

func TestWebhookActive_InsertRLSDeniedReturns500WriteErrorNoRawErrorLeaked(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)

	// Enable native RLS on the target table with zero policies — Postgres's
	// own default-deny then blocks the webhook role's INSERT outright,
	// reproducing "table_policies doesn't yet permit role webhook" from
	// design.md's Risks & Concerns without needing the dashboard handler.
	if _, err := testPool.Exec(context.Background(), "ALTER TABLE "+f.schema+".employees ENABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatalf("enable RLS: %v", err)
	}

	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-1","name":"Ana"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when RLS denies the webhook role's write, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "row-level security") || strings.Contains(rec.Body.String(), "SQLSTATE") {
		t.Fatalf("expected a generic 500 body with no raw Postgres error leaked, got %q", rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "write_error" {
		t.Fatalf("expected 1 delivery logged with outcome=write_error, got %+v", list)
	}
	if list[0].ErrorDetail == nil || *list[0].ErrorDetail == "" {
		t.Fatal("expected the real error to be captured server-side in error_detail")
	}
}

// TestWebhookActive_InsertIntoPolicyModeTableWithGrantSucceeds reproduces the
// real, documented use case (CHANGELOG: "Writes run under a dedicated RLS
// role (webhook, via table_policies)") that TestWebhookActive_InsertHappyPathCreatesRow
// doesn't actually cover — that fixture's "employees" table has no owner_id
// column at all, so it never exercises a policy-mode table provisioned the
// real way. Here owner_id exists and is nullable (RelaxOwnerColumn /
// provisioner.createTable's "policy" branch): the webhook role has no
// end-user identity to put there, and the insert must still succeed.
func TestWebhookActive_InsertIntoPolicyModeTableWithGrantSucceeds(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`ALTER TABLE `+f.schema+`.employees ADD COLUMN owner_id UUID`,
	); err != nil {
		t.Fatalf("add owner_id: %v", err)
	}
	if _, err := testPool.Exec(ctx, "ALTER TABLE "+f.schema+".employees ENABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatalf("enable RLS: %v", err)
	}
	// Mirrors provisioner.BuildPolicySQL's actual output: every policy runs
	// "TO zeep_app_enduser" (the one Postgres role every request switches to
	// via WithRLSContext) and gates by role through the app.jwt_role GUC,
	// not a literal "webhook" Postgres role/grantee.
	if _, err := testPool.Exec(ctx,
		`CREATE POLICY webhook_insert ON `+f.schema+`.employees FOR INSERT TO zeep_app_enduser
		 WITH CHECK (current_setting('app.jwt_role', true) = 'webhook')`,
	); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`CREATE POLICY webhook_select ON `+f.schema+`.employees FOR SELECT TO zeep_app_enduser
		 USING (current_setting('app.jwt_role', true) = 'webhook')`,
	); err != nil {
		t.Fatalf("create select policy: %v", err)
	}

	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-42","name":"Ana Souza"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 inserting into a policy-mode table granted to role webhook, got %d: %s", rec.Code, rec.Body.String())
	}

	var externalID string
	var ownerID *string
	if err := testPool.QueryRow(ctx,
		`SELECT external_id, owner_id FROM `+f.schema+`.employees LIMIT 1`,
	).Scan(&externalID, &ownerID); err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if externalID != "u-42" {
		t.Fatalf("expected external_id=u-42, got %q", externalID)
	}
	if ownerID != nil {
		t.Fatalf("expected owner_id to be NULL for a webhook-originated row, got %q", *ownerID)
	}
}

func TestWebhookActive_InsertMissingMappedFieldReturns500WriteError(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	// The mapping's field_mappings link "user.name" → full_name, but this
	// payload has no "user" object at all — ResolveFields can't resolve
	// that source path.
	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when a mapped field is missing from the payload, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no row written when field resolution fails, got %d", count)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "write_error" {
		t.Fatalf("expected 1 delivery logged with outcome=write_error, got %+v", list)
	}
}

func TestWebhookActive_RetryAfterWriteErrorIsNotDeduped(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithInsertMapping(t)

	if _, err := testPool.Exec(context.Background(), "ALTER TABLE "+f.schema+".employees ENABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatalf("enable RLS: %v", err)
	}

	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	body := `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-1","name":"Ana"}}`
	first := postWebhook(router, f.wh, f.token, body)
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("expected first attempt to 500 under RLS denial, got %d", first.Code)
	}

	if _, err := testPool.Exec(context.Background(), "ALTER TABLE "+f.schema+".employees DISABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatalf("disable RLS: %v", err)
	}

	retry := postWebhook(router, f.wh, f.token, body)
	if retry.Code != http.StatusOK {
		t.Fatalf("expected retry with the same event_id to succeed once the write can go through, got %d: %s", retry.Code, retry.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 2 || list[0].Outcome != "inserted" || list[1].Outcome != "write_error" {
		t.Fatalf("expected [inserted, write_error] newest-first, got %+v", list)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row written after the retry, got %d", count)
	}
}
