# Settings Consolidation — S2.4 (Statement timeout) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development to implement task-by-task. Checkbox (`- [ ]`) steps.

**Goal:** Add a global, configurable `statement_timeout_ms` setting (default 30000 = 30s, `0` disables) that Postgres enforces on every app data-plane CRUD query, so a runaway query is aborted instead of holding a connection indefinitely.

**Architecture:** Extend `zeep_system.system_config` + `dashboard.SystemConfig` with `statement_timeout_ms` (idempotent `ADD COLUMN`, default 30000), plumb it through the existing merge-on-absent PUT and into the registry's in-memory `SystemConfig` cache (so the hot data path reads it without a DB roundtrip — same channel as `SoftDeleteEnabled`). Enforce it with a new `db.Pool.WithTimeout` helper: when the timeout is `> 0` it runs the query inside a short transaction with `SET LOCAL statement_timeout` (tx-scoped, so it never leaks to the pooled connection); when `0` it runs on the bare pool exactly as today. The 5 table-CRUD handlers in `internal/server/handler.go` route their queries through the helper.

**Tech Stack:** Go (pgx v5, pgxpool, chi), React/Vite/TS, react-i18next.

## Global Constraints

- **Behavior change:** the 30000 default backfills existing installs, so app queries running longer than 30s begin to abort (Postgres error `57014`, `canceling statement due to statement timeout`). This MUST get an explicit `CHANGELOG.md` `[Unreleased]` callout (AGENTS §8). `0` preserves the old unbounded behavior.
- **Timeout is enforced, not just stored** (spec Goal): a real `SET LOCAL statement_timeout` on the query path, verified — persisting the value alone does not satisfy this slice.
- **`SET LOCAL` requires a transaction** and is auto-scoped to it — never `SET statement_timeout` (session-level) on a pooled connection without resetting, as it would leak to the next request on that connection.
- Config PUT stays merge-on-absent: a field absent from the request body must NOT change its stored value; pointer patch fields distinguish "absent" from "zero" (AGENTS §4 — see `mergeSystemConfig`). `statement_timeout_ms: 0` is a **valid, meaningful** value (disabled) and must persist as `0` — do NOT clamp `0` up to the default the way `max_csv_export_rows` does. Clamp only negatives to `0`.
- Schema change is additive + idempotent (`ADD COLUMN IF NOT EXISTS ... NOT NULL DEFAULT 30000`) so it runs safely on every boot (AGENTS §8 — additive only).
- API error strings in English; don't leak raw `err.Error()` into 500s (AGENTS §4).
- Every user-facing string via react-i18next in BOTH `en.json` and `pt-BR.json`, same change (AGENTS §5).
- Backend gate (AGENTS §3): `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l <changed .go>` — all clean. `go test ./...` skips DB-backed tests when `TEST_DATABASE_URL` is unset; the new tests (merge, `IsStatementTimeout`) are **pure unit tests** (no DB) and always run.
- Frontend gate: `python3 -c "import json; ..."` on both locales + `npm run build` (pinned `tsc -b && vite build`). Bare `npx tsc -b` falsely errors (TS5102 baseUrl) — do not use it. IDE `Cannot find module '@/...'`/`implicitly any` are false positives.
- **Scope of enforcement this slice:** only the 5 table-CRUD handlers (`HandleList`, `HandleCreate`, `HandleGetByID`, `HandleUpdate`, `HandleDelete` in `internal/server/handler.go`). `storage_handler.go` (file-metadata) and `HandleAppHealth` are intentionally NOT wrapped this slice — note that, don't silently skip.
- **Perf note (not a blocker):** with the default `> 0`, each CRUD op now runs inside a `BEGIN/…/COMMIT` (was a bare pool call). Acceptable for zeep-orbit's scale; mention it in the CHANGELOG callout.
- **Do not commit** — Julio commits. No `git add`/`git commit` steps.

Backend files live under repo root; frontend base dir `internal/dashboard/ui/`.

Current state (already merged from S2.3): `dashboard.SystemConfig{ SoftDeleteEnabled bool; StorageConfig *GlobalStorageConfig; MaxCSVExportRows int }`, with `systemConfigPatch` (pointer fields) + pure `mergeSystemConfig`, `GetSystemConfig`/`UpsertSystemConfig(ctx, pool, *SystemConfig)`, and `UpdateSystemConfig` doing load→merge→upsert. `registry.SystemConfig{ SoftDeleteEnabled bool }` cached via `reg.SystemConfig()`, populated at `cmd/zeep/main.go:88-90` and `internal/dashboard/handler.go:425`.

---

### Task 1: Backend — persist `statement_timeout_ms` and cache it in the registry

**Files:**
- Modify: `internal/dashboard/system_config_store.go` (struct + patch + merge + `GetSystemConfig` scan + `UpsertSystemConfig` persist)
- Modify: `internal/dashboard/provisioner.go:146` (add `ADD COLUMN` after the `max_csv_export_rows` one)
- Modify: `internal/registry/registry.go:49-51` (`SystemConfig` struct field)
- Modify: `cmd/zeep/main.go:90` (boot wiring)
- Modify: `internal/dashboard/handler.go:425` (update wiring)
- Test: `internal/dashboard/system_config_merge_test.go` (extend existing)

**Interfaces:**
- Produces: `dashboard.SystemConfig.StatementTimeoutMs int`; `systemConfigPatch.StatementTimeoutMs *int`; `registry.SystemConfig.StatementTimeoutMs int`. Task 2 reads `h.reg.SystemConfig().StatementTimeoutMs`.

- [ ] **Step 1: Extend the merge unit test** — append to `internal/dashboard/system_config_merge_test.go` (do not touch the two existing tests):

```go
func TestMergeSystemConfigStatementTimeout(t *testing.T) {
	cur := SystemConfig{SoftDeleteEnabled: true, StatementTimeoutMs: 30000, MaxCSVExportRows: 10000}

	// Absent from patch → preserved.
	sd := false
	merged := mergeSystemConfig(cur, systemConfigPatch{SoftDeleteEnabled: &sd})
	if merged.StatementTimeoutMs != 30000 {
		t.Fatalf("statement timeout must be preserved when absent, got %d", merged.StatementTimeoutMs)
	}

	// Present zero → applied (0 disables the timeout; it is a real value, not "absent").
	zero := 0
	merged = mergeSystemConfig(cur, systemConfigPatch{StatementTimeoutMs: &zero})
	if merged.StatementTimeoutMs != 0 {
		t.Fatalf("statement timeout 0 must be applied (disabled), got %d", merged.StatementTimeoutMs)
	}

	// Present positive → applied.
	n := 5000
	merged = mergeSystemConfig(cur, systemConfigPatch{StatementTimeoutMs: &n})
	if merged.StatementTimeoutMs != 5000 {
		t.Fatalf("statement timeout should update to 5000, got %d", merged.StatementTimeoutMs)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (compile error: `StatementTimeoutMs` undefined on `SystemConfig`/`systemConfigPatch`):

```bash
go test ./internal/dashboard/ -run TestMergeSystemConfig -v
```

- [ ] **Step 3: Add the field to `dashboard.SystemConfig`, the patch, and the merge fn** in `internal/dashboard/system_config_store.go`. In the `SystemConfig` struct (after `MaxCSVExportRows`):

```go
	StatementTimeoutMs int `json:"statement_timeout_ms"`
```

In `systemConfigPatch` (after `MaxCSVExportRows`):

```go
	StatementTimeoutMs *int `json:"statement_timeout_ms,omitempty"`
```

In `mergeSystemConfig`, before `return cur`:

```go
	if patch.StatementTimeoutMs != nil {
		cur.StatementTimeoutMs = *patch.StatementTimeoutMs
	}
```

- [ ] **Step 4: Scan the new column in `GetSystemConfig`.** Update the SELECT + Scan to add `COALESCE(statement_timeout_ms, 30000)` as the 4th column:

```go
	err := pool.QueryRow(ctx,
		`SELECT soft_delete_enabled, storage_config, COALESCE(max_csv_export_rows, 10000), COALESCE(statement_timeout_ms, 30000)
		 FROM zeep_system.system_config LIMIT 1`,
	).Scan(&cfg.SoftDeleteEnabled, &rawStorage, &cfg.MaxCSVExportRows, &cfg.StatementTimeoutMs)
```

(The `return &SystemConfig{}, nil` error path leaves `StatementTimeoutMs` at 0 = disabled — safe fallback: no install accidentally gets a 30s cap from a failed read.)

- [ ] **Step 5: Persist the column in `UpsertSystemConfig`.** After the `maxCSV` clamp block, add a statement-timeout normalization (clamp only negatives to 0; `0` is a valid "disabled" value and must persist):

```go
	stmtTimeout := cfg.StatementTimeoutMs
	if stmtTimeout < 0 {
		stmtTimeout = 0
	}
```

Then extend the INSERT column list, `VALUES`, and the `DO UPDATE SET` to include `statement_timeout_ms`, and add `stmtTimeout` to the args. The full statement becomes:

```go
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled, storage_config, max_csv_export_rows, statement_timeout_ms)
		 VALUES ($1, $2::jsonb, $3, $4)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET soft_delete_enabled = $1,
		       storage_config = $2::jsonb,
		       max_csv_export_rows = $3,
		       statement_timeout_ms = $4`,
		cfg.SoftDeleteEnabled, rawJSON, maxCSV, stmtTimeout,
	)
```

- [ ] **Step 6: Add the migration** in `internal/dashboard/provisioner.go`, immediately after the `max_csv_export_rows` `ADD COLUMN` (line 146), same idempotent style:

```go
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS statement_timeout_ms INT NOT NULL DEFAULT 30000`,
```

- [ ] **Step 7: Add the field to `registry.SystemConfig`** in `internal/registry/registry.go:49-51`:

```go
// SystemConfig holds global platform settings.
type SystemConfig struct {
	SoftDeleteEnabled  bool `json:"soft_delete_enabled"`
	StatementTimeoutMs int  `json:"statement_timeout_ms"`
}
```

- [ ] **Step 8: Populate the cache field at boot** in `cmd/zeep/main.go`. Replace the single-field `SetSystemConfig` (line 90):

```go
			reg.SetSystemConfig(registry.SystemConfig{
				SoftDeleteEnabled:  sysCfg.SoftDeleteEnabled,
				StatementTimeoutMs: sysCfg.StatementTimeoutMs,
			})
```

- [ ] **Step 9: Populate the cache field on update** in `internal/dashboard/handler.go:425` (inside `UpdateSystemConfig`). Replace the single-field `SetSystemConfig`:

```go
	h.reg.SetSystemConfig(registry.SystemConfig{
		SoftDeleteEnabled:  cfg.SoftDeleteEnabled,
		StatementTimeoutMs: cfg.StatementTimeoutMs,
	})
```

- [ ] **Step 10: Run the merge test — expect PASS:**

```bash
go test ./internal/dashboard/ -run TestMergeSystemConfig -v
```

- [ ] **Step 11: Full backend gate:**

```bash
go build ./... && go vet ./... && go test ./... 2>&1 | grep -vE "^(ok|---|\?)" ; gofmt -l internal/dashboard/system_config_store.go internal/dashboard/provisioner.go internal/registry/registry.go cmd/zeep/main.go internal/dashboard/handler.go internal/dashboard/system_config_merge_test.go
```
Expected: build/vet clean, tests pass (no non-ok lines), gofmt prints nothing.

---

### Task 2: Backend — enforce the timeout on the app data-plane

**Files:**
- Modify: `internal/db/client.go` (`Querier` interface + `WithTimeout` + `IsStatementTimeout`)
- Modify: `internal/server/handler.go` (wrap the 5 CRUD handlers)
- Test: `internal/db/client_test.go` (new, pure unit test for `IsStatementTimeout`)
- Modify: `CHANGELOG.md` (`[Unreleased]` behavior-change callout)

**Interfaces:**
- Consumes: `h.reg.SystemConfig().StatementTimeoutMs` (Task 1).
- Produces: `db.Querier` interface; `func (p *Pool) WithTimeout(ctx context.Context, timeoutMs int, fn func(q Querier) error) error`; `func IsStatementTimeout(err error) bool`.

- [ ] **Step 1: Write the failing `IsStatementTimeout` unit test** — `internal/db/client_test.go` (new; pure, no DB):

```go
package db

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsStatementTimeout(t *testing.T) {
	if !IsStatementTimeout(&pgconn.PgError{Code: "57014"}) {
		t.Fatal("57014 (query_canceled) should be detected as a statement timeout")
	}
	if IsStatementTimeout(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique_violation must not be treated as a statement timeout")
	}
	if IsStatementTimeout(errors.New("some other error")) {
		t.Fatal("a non-pg error must not be treated as a statement timeout")
	}
	if IsStatementTimeout(nil) {
		t.Fatal("nil must not be treated as a statement timeout")
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (compile error: `IsStatementTimeout` undefined):

```bash
go test ./internal/db/ -run TestIsStatementTimeout -v
```

- [ ] **Step 3: Add `Querier`, `WithTimeout`, and `IsStatementTimeout`** to `internal/db/client.go`. Add `pgx` + `pgconn` + `errors` to the imports:

```go
import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)
```

Then add at the end of the file:

```go
// Querier is the subset of query methods shared by *pgxpool.Pool and pgx.Tx,
// so a caller can run the same query with or without a timeout transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// WithTimeout runs fn against a Querier. When timeoutMs > 0, fn runs inside a
// short transaction with SET LOCAL statement_timeout, so Postgres aborts a query
// that runs longer; SET LOCAL is transaction-scoped, so the limit never leaks to
// the pooled connection. When timeoutMs <= 0, fn runs directly on the pool with
// no transaction (timeout disabled), preserving the pre-timeout behavior.
func (p *Pool) WithTimeout(ctx context.Context, timeoutMs int, fn func(q Querier) error) error {
	if timeoutMs <= 0 {
		return fn(p.Pool)
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// timeoutMs is an int from trusted global config, not user input — safe to format.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsStatementTimeout reports whether err is a Postgres statement-timeout abort
// (SQLSTATE 57014, query_canceled).
func IsStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}
```

- [ ] **Step 4: Run the test — expect PASS:**

```bash
go test ./internal/db/ -run TestIsStatementTimeout -v
```

- [ ] **Step 5: Wrap `HandleList`** in `internal/server/handler.go` (lines ~75-97). Replace the block from `ctx := r.Context()` through the `data` collection (the COUNT + SELECT) with:

```go
	ctx := r.Context()
	filterArgs := q.Args[:len(q.Args)-2]

	var count int
	var data []map[string]any
	err = h.pool.WithTimeout(ctx, h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		if err := qx.QueryRow(ctx, q.CountSQL, filterArgs...).Scan(&count); err != nil {
			return err
		}
		rows, err := qx.Query(ctx, q.SQL, q.Args...)
		if err != nil {
			return err
		}
		data, err = pgx.CollectRows(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query rows")
		return
	}
	if data == nil {
		data = []map[string]any{}
	}
```

(`q`, `err`, `limit`/`offset` from `q.Args` after this block are unchanged. Note `err` is already declared from the `query.BuildList` call above, so use `=`, not `:=`, in the wrap.)

- [ ] **Step 6: Wrap `HandleCreate`** (lines ~144-153). Replace the `rows, err := h.pool.Query(...)` + `CollectOneRow` block:

```go
	var row map[string]any
	err = h.pool.WithTimeout(r.Context(), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to insert row")
		return
	}
```

(`err` here comes from the `query.BuildInsert` call above, already declared — use `=`.)

- [ ] **Step 7: Wrap `HandleGetByID`** (lines ~183-196). Replace the `rows, err := h.pool.Query(...)` + `CollectOneRow` block:

```go
	var row map[string]any
	err := h.pool.WithTimeout(r.Context(), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query row")
		return
	}
```

(`HandleGetByID` builds `q` via `query.BuildGetByID` with no `err`, so `err` is new here — use `:=`.)

- [ ] **Step 8: Wrap `HandleUpdate`** (lines ~236-249). Replace the `rows, err := h.pool.Query(...)` + `CollectOneRow` block:

```go
	var row map[string]any
	err = h.pool.WithTimeout(r.Context(), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update row")
		return
	}
```

(`err` here comes from the `query.BuildUpdate` call above — use `=`.)

- [ ] **Step 9: Wrap `HandleDelete`** (lines ~279-288). Replace the `tag, err := h.pool.Exec(...)` block:

```go
	var affected int64
	err := h.pool.WithTimeout(r.Context(), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		tag, err := qx.Exec(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		affected = tag.RowsAffected()
		return nil
	})
	if err != nil {
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete row")
		return
	}

	if affected == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
```

(`HandleDelete` builds `q` via `query.BuildDelete` with no `err`, so `err` is new — use `:=`.)

- [ ] **Step 10: Full backend gate** (build catches any `:=`/`=` redeclare mistake or unused var from the wraps):

```bash
go build ./... && go vet ./... && go test ./... 2>&1 | grep -vE "^(ok|---|\?)" ; gofmt -l internal/db/client.go internal/db/client_test.go internal/server/handler.go
```
Expected: build/vet clean, tests pass, gofmt prints nothing.

- [ ] **Step 11: Add the CHANGELOG callout** under `## [Unreleased]` in `CHANGELOG.md` (behavior-change wording, AGENTS §8). Place under the existing `### Added`/`### Changed` grouping if present, otherwise add a `### Changed`:

```markdown
- **Statement timeout (behavior change):** app data-plane queries now run under a global `statement_timeout` defaulting to **30s** — queries running longer are aborted by Postgres (HTTP 503 `query exceeded statement timeout`). Set **Settings → Database → Statement timeout** to `0` to restore the previous unbounded behavior. Each CRUD op now runs inside a short transaction to scope the limit.
```

- [ ] **Step 12 (optional, real DB): confirm the timeout actually fires** if an audit DB is available:

```bash
TEST_DATABASE_URL="postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable" go test ./internal/... 2>&1 | grep -vE "^(ok|---|\?)"
```
(No new DB-backed test is required by this plan; this just confirms nothing in the packages regressed with a DB present. Manual check: set timeout to `100`ms in Settings, run a `SELECT pg_sleep(1)` against an app table via the API — expect HTTP 503.)

---

### Task 3: Frontend — Statement-timeout input in the Database tab

**Files:**
- Modify: `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx` (`SoftDeleteCard` — the Database tab)
- Modify: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`

**Interfaces:**
- Consumes: the `PUT /dashboard/api/config/system` merge endpoint (Task 1). `SoftDeleteCard` already owns `soft_delete_enabled` + `max_csv_export_rows`; this adds `statement_timeout_ms` to the same PUT body.

- [ ] **Step 1: i18n keys (en.json)** — add alongside the existing `settings.maxCsvExportRows` keys:

```json
"settings.statementTimeoutMs": "Statement timeout (ms)",
"settings.statementTimeoutMsHint": "Aborts any app query running longer than this. 0 disables the timeout.",
```

- [ ] **Step 2: i18n keys (pt-BR.json)** — same keys, mirrored:

```json
"settings.statementTimeoutMs": "Timeout de consulta (ms)",
"settings.statementTimeoutMsHint": "Cancela qualquer consulta de app que passe desse tempo. 0 desativa o timeout.",
```

- [ ] **Step 3: Add state + hydration in `SoftDeleteCard`.** Next to the existing `maxCsvRows` state:

```tsx
  const [statementTimeoutMs, setStatementTimeoutMs] = useState(30000);
```

In the same fetch `.then((d) => { ... })` where `max_csv_export_rows` is read:

```tsx
        setStatementTimeoutMs(d.statement_timeout_ms ?? 30000);
```

- [ ] **Step 4: Send the field in the PUT** (this card owns these three; merge preserves the rest). Extend the existing body:

```tsx
        body: JSON.stringify({
          soft_delete_enabled: enabled,
          max_csv_export_rows: maxCsvRows,
          statement_timeout_ms: statementTimeoutMs,
        }),
```

- [ ] **Step 5: Render the numeric input** directly below the existing Max-CSV `<div>` block in the Database tab card (mirror its markup; `min={0}` because 0 = disabled):

```tsx
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <Label className="text-[13px] font-semibold text-[var(--text-secondary)]">
          {t("settings.statementTimeoutMs")}
        </Label>
        <Input
          type="number"
          min={0}
          value={statementTimeoutMs}
          onChange={(e) => setStatementTimeoutMs(Math.max(0, Number(e.target.value) || 0))}
          className="mt-2 max-w-[200px]"
        />
        <p className="mt-1 text-[11px] text-[var(--text-tertiary)]">{t("settings.statementTimeoutMsHint")}</p>
      </div>
```

- [ ] **Step 6: Frontend gate:**

```bash
cd internal/dashboard/ui
python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json')); print('JSON OK')"
npm run build
```
Expected: JSON OK, build clean. Visual (audit env `/configuracoes` → Database tab): the "Statement timeout (ms)" input shows 30000, edits persist across reload, and toggling soft-delete still does not wipe the storage tab's config (merge intact).

---

## Self-Review

- **Spec coverage (design.md S2.4 line 99 + enforcement bullet line 71):** config field + migration (T1 Steps 3-9), `SET LOCAL` in a short tx on the app data-plane (T2 Step 3 helper + Steps 5-9 wrapping), 30s default (T1 Steps 4-6), 0=disabled (T1 Step 5, merge test T1 Step 1), CHANGELOG callout (T2 Step 11), UI (T3). Enforcement is real (helper runs `SET LOCAL`), not just persisted — satisfies spec Goal line 27. Covered.
- **Placeholder scan:** none — full code for every logic step; line numbers are anchors from the current files, and the plan flags each `:=` vs `=` choice so an out-of-order reader gets it right.
- **Type consistency:** `StatementTimeoutMs` is `int` in both `dashboard.SystemConfig` and `registry.SystemConfig`; the patch field is `*int`; `db.Querier` / `WithTimeout(ctx, int, func(Querier) error) error` / `IsStatementTimeout(error) bool` are defined in T2 Step 3 and used verbatim in T2 Steps 5-9. `*pgxpool.Pool` and `pgx.Tx` both satisfy `Querier` (Query→pgx.Rows, QueryRow→pgx.Row, Exec→pgconn.CommandTag). JSON key `statement_timeout_ms` is identical across Go tags, PUT body, and hydration.
- **Merge-on-absent correctness:** `0` is applied (not treated as absent) because the patch field is `*int` — a present `&0` differs from `nil`. The test (T1 Step 1) asserts exactly this, guarding the one place where statement-timeout semantics differ from `max_csv_export_rows` (which clamps 0 → default).
- **Out of scope (not S2.4):** RLS default (S2.2), retention/purge (S2.1), pool cap (deferred); `storage_handler.go` + `HandleAppHealth` timeout-wrapping (noted, not silent); README config table (setting is DB-stored, not env — CHANGELOG suffices, consistent with S2.3).
