package dashboard

// builder_settings_test.go — integration tests (real Postgres through
// TEST_DATABASE_URL, no DB mocking) for the builder settings and versioned
// persona/rules endpoints (ai-frontend-builder T22, AFB-01/02/25). The
// handlers are called directly, like the other dashboard handler tests.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func builderSettingsTestPool(t *testing.T) (*db.Pool, *Handler) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision schema: %v", err)
	}

	truncate := func() {
		_, _ = pool.Exec(context.Background(),
			`TRUNCATE zeep_system.builder_settings, zeep_system.builder_prompt_versions, zeep_system.audit_log, zeep_system.dashboard_users CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)

	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret-for-builder-settings")
	return pool, NewHandler(pool, registry.New(), zap.NewNop())
}

func callBuilder(t *testing.T, fn http.HandlerFunc, user *DashboardUser, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if user != nil {
		req = withUser(req, user)
	}
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func builderSettingsRowCount(t *testing.T, pool *db.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.builder_settings`).Scan(&n); err != nil {
		t.Fatalf("count builder_settings: %v", err)
	}
	return n
}

func builderPromptRowCount(t *testing.T, pool *db.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.builder_prompt_versions`).Scan(&n); err != nil {
		t.Fatalf("count builder_prompt_versions: %v", err)
	}
	return n
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

// 403 for a non-superadmin (admin) on every route, 401 without a user, and no
// mutation either way.
func TestBuilderSettings_NonSuperadminForbiddenNoMutation(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	admin := mustCreateRegularUser(t, pool) // role "admin": not a superadmin

	routes := []struct {
		name string
		fn   http.HandlerFunc
		meth string
		body string
	}{
		{"GET settings", h.GetBuilderSettingsConfig, http.MethodGet, ""},
		{"PUT settings", h.UpsertBuilderSettingsConfig, http.MethodPut, `{"enabled":true,"session_token_cap":1000}`},
		{"GET prompt", h.GetBuilderPromptConfig, http.MethodGet, ""},
		{"PUT prompt", h.UpsertBuilderPromptConfig, http.MethodPut, `{"persona":"x","rules":"y"}`},
		{"GET versions", h.ListBuilderPromptVersionsConfig, http.MethodGet, ""},
	}
	for _, rt := range routes {
		if w := callBuilder(t, rt.fn, admin, rt.meth, "/dashboard/api/builder/x", rt.body); w.Code != http.StatusForbidden {
			t.Errorf("%s as admin: expected 403, got %d: %s", rt.name, w.Code, w.Body.String())
		}
		if w := callBuilder(t, rt.fn, nil, rt.meth, "/dashboard/api/builder/x", rt.body); w.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated: expected 401, got %d", rt.name, w.Code)
		}
	}
	if n := builderSettingsRowCount(t, pool); n != 0 {
		t.Errorf("forbidden PUT created a settings row (%d)", n)
	}
	if n := builderPromptRowCount(t, pool); n != 0 {
		t.Errorf("forbidden PUT created a prompt version (%d)", n)
	}
}

// With no row saved, GET returns sane defaults (disabled, caps 0) and does not
// write one: a missing row must stay "blocked" for the budget checker.
func TestBuilderSettings_GetDefaultsWithoutWriting(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := callBuilder(t, h.GetBuilderSettingsConfig, super, http.MethodGet, "/dashboard/api/builder/settings", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSettings](t, w)
	if got.Enabled || got.SessionTokenCap != 0 || got.UserMonthlyTokenCap != 0 || got.Configured {
		t.Errorf("defaults must be disabled, caps 0, configured=false; got %+v", got)
	}
	if got.Provider != "openai" || got.ContextWindowTokens != 128000 || got.MaxStepsPerTurn != 40 ||
		got.MaxFixIterations != 3 || got.MaxTurnSeconds != 600 {
		t.Errorf("unexpected default limits: %+v", got)
	}
	if got.BackendApprovalMode != "ask" || got.PublishRLSGate != "warn" {
		t.Errorf("defaults must be approval ask and publish check warn: %+v", got)
	}
	if got.UpdatedAt != nil || got.UpdatedBy != nil {
		t.Errorf("defaults carry no updated_at/updated_by: %+v", got)
	}
	if n := builderSettingsRowCount(t, pool); n != 0 {
		t.Fatalf("GET must not write the settings row, found %d", n)
	}
}

// The first PUT upserts the singleton row; the caps are persisted and read
// back; updated_by is the caller.
func TestBuilderSettings_PutCreatesRowAndPersistsCaps(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/dashboard/api/builder/settings",
		`{"enabled":true,"provider":"openai_compatible","context_window_tokens":32000,"session_token_cap":200000,"user_monthly_token_cap":5000000,"max_steps_per_turn":25,"max_fix_iterations":2,"max_turn_seconds":300}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if n := builderSettingsRowCount(t, pool); n != 1 {
		t.Fatalf("expected exactly one settings row, got %d", n)
	}

	got := decodeJSON[BuilderSettings](t, callBuilder(t, h.GetBuilderSettingsConfig, super, http.MethodGet, "/x", ""))
	want := BuilderSettings{
		Enabled: true, Provider: "openai_compatible", ContextWindowTokens: 32000,
		SessionTokenCap: 200000, UserMonthlyTokenCap: 5000000,
		MaxStepsPerTurn: 25, MaxFixIterations: 2, MaxTurnSeconds: 300, Configured: true,
		BackendApprovalMode: "ask", PublishRLSGate: "warn",
	}
	got.UpdatedAt = nil
	if got.UpdatedBy == nil || *got.UpdatedBy != super.ID {
		t.Errorf("updated_by = %v, want %s", got.UpdatedBy, super.ID)
	}
	got.UpdatedBy = nil
	if got != want {
		t.Errorf("settings = %+v, want %+v", got, want)
	}
}

// Merge-on-absent-key: a PUT with only the caps leaves every other setting
// untouched; an explicit 0 zeroes a cap.
func TestBuilderSettings_PartialPutMergesOnAbsentKey(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	put := func(body string) BuilderSettings {
		t.Helper()
		w := callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", body)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, w.Code, w.Body.String())
		}
		return decodeJSON[BuilderSettings](t, w)
	}

	put(`{"enabled":true,"provider":"openai_compatible","session_token_cap":1000,"user_monthly_token_cap":9000,"max_steps_per_turn":10}`)
	got := put(`{"session_token_cap":2000}`)
	if !got.Enabled || got.Provider != "openai_compatible" || got.UserMonthlyTokenCap != 9000 || got.MaxStepsPerTurn != 10 {
		t.Errorf("a caps-only update must keep the other settings: %+v", got)
	}
	if got.SessionTokenCap != 2000 {
		t.Errorf("session_token_cap = %d, want 2000", got.SessionTokenCap)
	}
	got = put(`{"user_monthly_token_cap":0,"enabled":false}`)
	if got.UserMonthlyTokenCap != 0 || got.Enabled || got.SessionTokenCap != 2000 {
		t.Errorf("explicit 0 / false must apply and keep the rest: %+v", got)
	}
}

// The two backend settings (ai-builder-backend B01) merge on absent key like
// the rest, and a settings row saved before they existed reads the defaults.
func TestBuilderSettings_BackendSettingsMergeAndDefault(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	put := func(body string) BuilderSettings {
		t.Helper()
		w := callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", body)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, w.Code, w.Body.String())
		}
		return decodeJSON[BuilderSettings](t, w)
	}

	// A first save that never mentions them stores the defaults.
	got := put(`{"enabled":true}`)
	if got.BackendApprovalMode != "ask" || got.PublishRLSGate != "warn" {
		t.Fatalf("first save must default to ask/warn: %+v", got)
	}

	got = put(`{"backend_approval_mode":"auto","publish_rls_gate":"block"}`)
	if got.BackendApprovalMode != "auto" || got.PublishRLSGate != "block" || !got.Enabled {
		t.Fatalf("both settings must apply and keep the rest: %+v", got)
	}

	// Sending one key keeps the other; unrelated keys keep both.
	got = put(`{"backend_approval_mode":"ask"}`)
	if got.BackendApprovalMode != "ask" || got.PublishRLSGate != "block" {
		t.Errorf("approval-only update must keep the publish check: %+v", got)
	}
	got = put(`{"max_steps_per_turn":12}`)
	if got.BackendApprovalMode != "ask" || got.PublishRLSGate != "block" || got.MaxStepsPerTurn != 12 {
		t.Errorf("an unrelated update must keep both backend settings: %+v", got)
	}
}

// Invalid values are 400 and never create or alter the row.
func TestBuilderSettings_InvalidValuesRejected(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	for _, body := range []string{
		`{}`,
		`{"provider":"gemini"}`,
		`{"provider":""}`,
		`{"session_token_cap":-1}`,
		`{"user_monthly_token_cap":-5}`,
		`{"session_token_cap":2000000000000}`,
		`{"context_window_tokens":10}`,
		`{"context_window_tokens":99999999}`,
		`{"max_steps_per_turn":0}`,
		`{"max_steps_per_turn":1000}`,
		`{"max_fix_iterations":0}`,
		`{"max_turn_seconds":1}`,
		`{"max_turn_seconds":86400}`,
		`{"enabled":"yes"}`,
		`{"backend_approval_mode":"always"}`,
		`{"backend_approval_mode":""}`,
		`{"publish_rls_gate":"off"}`,
		`{"publish_rls_gate":""}`,
		`not json`,
	} {
		w := callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
	if n := builderSettingsRowCount(t, pool); n != 0 {
		t.Fatalf("rejected PUTs must not create the row, found %d", n)
	}
}

// A settings save writes an audit row.
func TestBuilderSettings_AuditRowWritten(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"enabled":true,"session_token_cap":1500}`)

	entries, _, err := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: "builder.settings.update"})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if len(entries) != 1 || entries[0].UserEmail != super.Email || entries[0].ResourceType != "builder_settings" {
		t.Fatalf("expected one builder.settings.update row by the superadmin, got %+v", entries)
	}
	if !strings.Contains(string(entries[0].Metadata), "1500") {
		t.Errorf("audit metadata should carry the changed values: %s", entries[0].Metadata)
	}
}

// First prompt save creates version 1 with its author; GET returns it as the
// active version.
func TestBuilderPrompt_SaveCreatesVersionWithAuthor(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	empty := decodeJSON[builderPromptResponse](t, callBuilder(t, h.GetBuilderPromptConfig, super, http.MethodGet, "/x", ""))
	if empty.ID != "" || empty.Persona != "" || empty.Rules != "" || empty.MaxChars != agent.MaxCompanyChars {
		t.Errorf("no saved version: expected an empty prompt with max_chars, got %+v", empty)
	}

	w := callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x",
		`{"persona":"You are ACME's frontend engineer.","rules":"Always use the ACME Button."}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved := decodeJSON[builderPromptResponse](t, w)
	if saved.ID == "" || saved.CreatedBy != super.ID || saved.CreatedByEmail != super.Email || saved.CreatedAt == nil {
		t.Errorf("version must carry its author and time: %+v", saved)
	}
	if saved.Changed == nil || !*saved.Changed {
		t.Errorf("changed must be true on a new version: %+v", saved)
	}

	active := decodeJSON[builderPromptResponse](t, callBuilder(t, h.GetBuilderPromptConfig, super, http.MethodGet, "/x", ""))
	if active.ID != saved.ID || active.Persona != "You are ACME's frontend engineer." || active.Rules != "Always use the ACME Button." {
		t.Errorf("GET must return the active version: %+v", active)
	}
	if active.Chars != builderPromptChars(active.Persona, active.Rules) || active.Chars == 0 {
		t.Errorf("chars = %d", active.Chars)
	}
}

// History: each change appends a version, partial saves merge on absent key,
// the list is newest-first with authors, older versions stay readable.
func TestBuilderPrompt_VersionHistoryAndMerge(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	other, err := CreateUser(context.Background(), pool, "second-super@example.com", "Second", "hash", "superadmin")
	if err != nil {
		t.Fatalf("create second superadmin: %v", err)
	}

	put := func(u *DashboardUser, body string) builderPromptResponse {
		t.Helper()
		w := callBuilder(t, h.UpsertBuilderPromptConfig, u, http.MethodPut, "/x", body)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, w.Code, w.Body.String())
		}
		return decodeJSON[builderPromptResponse](t, w)
	}
	v1 := put(super, `{"persona":"P1","rules":"R1"}`)
	v2 := put(other, `{"rules":"R2"}`) // persona absent: kept
	if v2.Persona != "P1" || v2.Rules != "R2" || v2.ID == v1.ID {
		t.Errorf("partial save must keep persona and add a version: %+v", v2)
	}
	v3 := put(super, `{"persona":""}`) // explicit empty: clears persona
	if v3.Persona != "" || v3.Rules != "R2" {
		t.Errorf("explicit empty persona must clear it and keep rules: %+v", v3)
	}

	list := decodeJSON[struct {
		Versions []BuilderPromptVersion `json:"versions"`
		Total    int                    `json:"total"`
	}](t, callBuilder(t, h.ListBuilderPromptVersionsConfig, super, http.MethodGet, "/dashboard/api/builder/prompt/versions", ""))
	if list.Total != 3 || len(list.Versions) != 3 {
		t.Fatalf("expected 3 versions, got total=%d len=%d", list.Total, len(list.Versions))
	}
	if list.Versions[0].ID != v3.ID || list.Versions[1].ID != v2.ID || list.Versions[2].ID != v1.ID {
		t.Errorf("versions must be newest first: %+v", list.Versions)
	}
	if list.Versions[1].CreatedByEmail != other.Email || list.Versions[2].CreatedByEmail != super.Email {
		t.Errorf("each version must carry its author: %+v", list.Versions)
	}
	if list.Versions[2].Persona != "P1" || list.Versions[2].Rules != "R1" {
		t.Errorf("the previous version must be kept intact: %+v", list.Versions[2])
	}

	// Pagination.
	page := decodeJSON[struct {
		Versions []BuilderPromptVersion `json:"versions"`
		Total    int                    `json:"total"`
	}](t, callBuilder(t, h.ListBuilderPromptVersionsConfig, super, http.MethodGet, "/x?limit=1&offset=1", ""))
	if len(page.Versions) != 1 || page.Versions[0].ID != v2.ID || page.Total != 3 {
		t.Errorf("limit/offset: %+v", page)
	}
}

// A save identical to the active version creates no row and no audit entry.
func TestBuilderPrompt_UnchangedSaveIsNoOp(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x", `{"persona":"same","rules":"same rules"}`)
	w := callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x", `{"persona":"same","rules":"same rules"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decodeJSON[builderPromptResponse](t, w); resp.Changed == nil || *resp.Changed {
		t.Errorf("changed must be false: %+v", resp)
	}
	if n := builderPromptRowCount(t, pool); n != 1 {
		t.Errorf("expected 1 version, got %d", n)
	}
	entries, _, _ := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: "builder.prompt.update"})
	if len(entries) != 1 {
		t.Errorf("expected 1 audit row, got %d", len(entries))
	}
}

// Persona + rules larger than the cap are rejected with 400 and not stored; a
// body that sits exactly on the cap is accepted. Size is in characters
// (runes), measured on the text the prompt will carry.
func TestBuilderPrompt_OversizedRejected(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	put := func(persona, rules string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"persona": persona, "rules": rules})
		return callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x", string(body))
	}
	cap := agent.MaxCompanyChars

	// Oversized rules alone.
	w := put("", strings.Repeat("r", cap+1))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "exceed") {
		t.Fatalf("oversized rules: expected 400 with a clear message, got %d: %s", w.Code, w.Body.String())
	}
	// Each field under the cap, combined over it.
	if w := put(strings.Repeat("p", cap/2+100), strings.Repeat("r", cap/2+100)); w.Code != http.StatusBadRequest {
		t.Fatalf("combined oversize: expected 400, got %d", w.Code)
	}
	// The "Rules:" header and separators count: persona alone at the cap
	// passes, but adding any rules pushes it over.
	if w := put(strings.Repeat("p", cap), "x"); w.Code != http.StatusBadRequest {
		t.Fatalf("persona at cap plus rules: expected 400, got %d", w.Code)
	}
	if n := builderPromptRowCount(t, pool); n != 0 {
		t.Fatalf("rejected saves must not store anything, found %d versions", n)
	}

	// Exactly on the cap: accepted. Multi-byte runes count as one character.
	if w := put(strings.Repeat("é", cap), ""); w.Code != http.StatusOK {
		t.Fatalf("persona exactly at the cap (%d runes): expected 200, got %d: %s", cap, w.Code, w.Body.String())
	}
	// A later partial save that would push the merged text over the cap is
	// rejected too: the cap applies to the merged result.
	body, _ := json.Marshal(map[string]string{"rules": "one more thing"})
	if w := callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x", string(body)); w.Code != http.StatusBadRequest {
		t.Fatalf("merge over the cap: expected 400, got %d", w.Code)
	}
	if n := builderPromptRowCount(t, pool); n != 1 {
		t.Fatalf("expected only the accepted version, found %d", n)
	}
}

// NUL characters cannot be stored in Postgres TEXT: 400, not a 500.
func TestBuilderPrompt_NULRejected(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x", `{"persona":"a\u0000b"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty body: expected 400, got %d", w.Code)
	}
	if n := builderPromptRowCount(t, pool); n != 0 {
		t.Fatalf("found %d versions", n)
	}
}

// A prompt save writes an audit row that records sizes and the version id,
// never the persona/rules text.
func TestBuilderPrompt_AuditRowWithoutContent(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := callBuilder(t, h.UpsertBuilderPromptConfig, super, http.MethodPut, "/x",
		`{"persona":"SECRET-PERSONA-TEXT","rules":"SECRET-RULES-TEXT"}`)
	saved := decodeJSON[builderPromptResponse](t, w)

	entries, _, err := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: "builder.prompt.update"})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if len(entries) != 1 || entries[0].ResourceID != saved.ID || entries[0].UserEmail != super.Email {
		t.Fatalf("expected one builder.prompt.update row for version %s, got %+v", saved.ID, entries)
	}
	if strings.Contains(string(entries[0].Metadata), "SECRET") {
		t.Errorf("audit metadata must not carry the prompt text: %s", entries[0].Metadata)
	}
}

// Concurrent partial saves of different fields must not lose either field
// (read-merge-insert is serialized), and the newest row is the active one.
func TestBuilderPrompt_ConcurrentPartialSavesDoNotLoseFields(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	var wg sync.WaitGroup
	for _, body := range []string{`{"persona":"concurrent persona"}`, `{"rules":"concurrent rules"}`} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			req := withUser(httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body)), super)
			w := httptest.NewRecorder()
			h.UpsertBuilderPromptConfig(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("PUT %s: %d %s", body, w.Code, w.Body.String())
			}
		}(body)
	}
	wg.Wait()

	active, err := GetActiveBuilderPrompt(context.Background(), pool)
	if err != nil || active == nil {
		t.Fatalf("GetActiveBuilderPrompt: %v %v", active, err)
	}
	if active.Persona != "concurrent persona" || active.Rules != "concurrent rules" {
		t.Fatalf("a field was lost: %+v", active)
	}
}
