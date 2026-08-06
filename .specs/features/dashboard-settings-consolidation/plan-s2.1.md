# Settings Consolidation — S2.1 (Retention period + purge) Implementation Plan

**Goal:** Add a global, opt-in `retention_days` setting. When set (>0) and soft-delete is enabled, a background job hard-deletes rows across every app schema/table where `deleted_at` is older than the retention window, on a fixed cadence, with an audit-log entry per run. Off by default (destructive, opt-in only).

**Architecture:** Extend `SystemConfig`/`systemConfigPatch`/`mergeSystemConfig` with `RetentionDays int` (mirrors the S2.2/S2.3/S2.4 pattern already in `system_config_store.go`). Add a pure function `PurgeExpiredSoftDeletes(ctx, pool, reg) (int, error)` in a new `internal/dashboard/purge.go` that iterates `reg.Apps()` and, for each table, runs `DELETE FROM <schema>.<table> WHERE deleted_at < now() - make_interval(days => $1)`. Wire a `time.NewTicker(6*time.Hour)` goroutine in `cmd/zeep/main.go` (mirrors the existing `go func()` boot patterns in `handler.go:499`/`google.go:169`/`server.go:97`) that calls it only when `RetentionDays > 0 && SoftDeleteEnabled`, logging an `audit_log` entry (`InsertAuditLog`, user_id/email empty, action `system.purge.run`, metadata = `{"rows_deleted": N, "retention_days": D}`) every run — even when it deletes 0 rows, so the audit trail shows the job is alive. Frontend adds a numeric "Retention period (days)" input + a native `confirm()` dialog on save when the value changes from 0 to a positive number (per design.md — "destrutiva → confirm dialog").

**Tech Stack:** Go (pgx), React/Vite/TS, react-i18next.

## Global Constraints

- `retention_days` default = 0 (purge off). Values ≤ 0 disable the job — read sites must treat `<= 0` as off, never as "purge everything".
- Purge only ever deletes rows that are **already soft-deleted** (`deleted_at IS NOT NULL` and older than the window) — never touches live rows. It runs independently of whether `SoftDeleteEnabled` is currently on (a table can carry old soft-deleted rows even if the toggle was later flipped off), but the job as a whole is gated on `SoftDeleteEnabled` per design.md so purge-with-soft-delete-off (a state that shouldn't normally arise) doesn't silently reap rows nobody expected to be reapable.
- Every purge run — including 0-row runs — writes one `audit_log` row so operators can see the job executed.
- API error strings in English; don't leak raw `err.Error()` into 500s (AGENTS §4).
- Config PUT stays merge-on-absent (existing `systemConfigPatch`/`mergeSystemConfig` — just add the new field, same pattern as `MaxCSVExportRows`/`StatementTimeoutMs`/`RequireRLSDefault`).
- Schema change additive + idempotent (`ADD COLUMN IF NOT EXISTS ... NOT NULL DEFAULT 0`), same style as the existing `provisioner.go` list (AGENTS §8).
- The purge query is built with `schemaName`/`tableName` already validated identifiers sourced from the registry (never user input) — safe to interpolate the same way `query.BuildDelete`/`BuildList` already do; still use a parameterized `$1` for the interval count.
- Every user-facing string via react-i18next in BOTH `en.json` and `pt-BR.json`.
- Backend gate (AGENTS §3): `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l <changed .go>`.
- Frontend gate: JSON validation on both locales + `npm run build` (pinned `tsc -b && vite build` — bare `npx tsc -b` false-errors on baseUrl).
- **Do not commit** — Julio commits.

---

### Task 1: Backend — `retention_days` config field

**Files:** `internal/dashboard/system_config_store.go`, `internal/dashboard/provisioner.go`, `internal/dashboard/system_config_merge_test.go` (extend existing tests).

- [ ] Add `RetentionDays int \`json:"retention_days"\`` to `SystemConfig` and `*int` to `systemConfigPatch`; extend `mergeSystemConfig` with the same nil-check pattern as the other int fields.
- [ ] `GetSystemConfig`: add `COALESCE(retention_days, 0)` to the SELECT + `&cfg.RetentionDays` to Scan.
- [ ] `UpsertSystemConfig`: add `retention_days` to the INSERT/ON CONFLICT column list and bind `cfg.RetentionDays` (no floor/clamp needed — 0 and negative both mean "off", enforced at the read site in Task 2, not at write time — don't silently coerce a negative input to 0 here, surface it as-is so a bad PUT is visible in `GetSystemConfig` rather than masked).
- [ ] `provisioner.go`: add `ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS retention_days INT NOT NULL DEFAULT 0` after the existing three `ADD COLUMN` statements.
- [ ] Extend `system_config_merge_test.go` with a case asserting `RetentionDays` is preserved when absent from the patch and applied when present (same shape as the existing `MaxCSVExportRows` assertions).
- [ ] Gate: `go build ./... && go vet ./... && go test ./... && gofmt -l internal/dashboard/system_config_store.go internal/dashboard/provisioner.go internal/dashboard/system_config_merge_test.go`.

### Task 2: Backend — purge job

**Files:** new `internal/dashboard/purge.go`, new `internal/dashboard/purge_test.go` (pure unit test, no DB), `cmd/zeep/main.go`.

- [ ] `internal/dashboard/purge.go`:

```go
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// PurgeExpiredSoftDeletes hard-deletes soft-deleted rows older than
// retentionDays across every app table, and writes one audit_log entry
// per run (including zero-row runs) so operators can see the job is alive.
func PurgeExpiredSoftDeletes(ctx context.Context, pool *db.Pool, reg *registry.Registry, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	total := 0
	for _, app := range reg.Apps() {
		for _, table := range app.Tables {
			sql := fmt.Sprintf(
				`DELETE FROM %s.%s WHERE deleted_at IS NOT NULL AND deleted_at < now() - ($1 || ' days')::interval`,
				app.SchemaName, table.Name,
			)
			tag, err := pool.Exec(ctx, sql, retentionDays)
			if err != nil {
				return total, fmt.Errorf("purge %s.%s: %w", app.SchemaName, table.Name, err)
			}
			total += int(tag.RowsAffected())
		}
	}
	meta, _ := json.Marshal(map[string]any{"rows_deleted": total, "retention_days": retentionDays})
	_ = InsertAuditLog(ctx, pool, "", "system", "system.purge.run", "system_config", "", "retention purge", meta, "")
	return total, nil
}
```

  (Confirm `InsertAuditLog`'s exact parameter order/names against `audit_store.go` before pasting — match the existing call in `handler.go:1430`'s `audit()` wrapper, don't guess the signature.)

- [ ] `purge_test.go` — pure unit test with no DB, asserting only the guard clause (`retentionDays <= 0` short-circuits to `(0, nil)` without needing a pool/registry — pass `nil, nil, 0` and expect no panic):

```go
package dashboard

import "testing"

func TestPurgeExpiredSoftDeletesOffByDefault(t *testing.T) {
	n, err := PurgeExpiredSoftDeletes(nil, nil, nil, 0)
	if err != nil || n != 0 {
		t.Fatalf("expected no-op when retentionDays<=0, got n=%d err=%v", n, err)
	}
}
```

- [ ] `cmd/zeep/main.go` (`cmdServe`, after `srv, err := server.New(...)` / before `return srv.Start()` — actually start the ticker goroutine right after `reg`/`sysCfg` are loaded, since it doesn't depend on `srv`):

```go
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cfg, err := dashboard.GetSystemConfig(context.Background(), pool)
			if err != nil || cfg.RetentionDays <= 0 || !cfg.SoftDeleteEnabled {
				continue
			}
			if _, err := dashboard.PurgeExpiredSoftDeletes(context.Background(), pool, reg, cfg.RetentionDays); err != nil {
				fmt.Fprintf(os.Stderr, "purge error: %v\n", err)
			}
		}
	}()
```

  Re-reads config each tick (cheap single-row read) so a live toggle change takes effect within one interval, no restart needed.

- [ ] Gate: `go build ./... && go vet ./... && go test ./... && gofmt -l internal/dashboard/purge.go internal/dashboard/purge_test.go cmd/zeep/main.go`.

### Task 3: Frontend — Retention input + confirm dialog

**Files:** `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx`, `src/locales/en.json`, `src/locales/pt-BR.json`.

- [ ] i18n (en): `"settings.retentionDays": "Retention period (days)"`, `"settings.retentionDaysHint": "Soft-deleted rows older than this are permanently removed. 0 disables purging."`, `"settings.retentionDaysConfirm": "Enabling purge is irreversible: soft-deleted rows older than {{days}} days will be permanently deleted on the next run. Continue?"`.
- [ ] i18n (pt-BR): `"settings.retentionDays": "Período de retenção (dias)"`, `"settings.retentionDaysHint": "Linhas com soft-delete mais antigas que isso são removidas permanentemente. 0 desativa a purga."`, `"settings.retentionDaysConfirm": "Ativar a purga é irreversível: linhas com soft-delete há mais de {{days}} dias serão apagadas permanentemente na próxima execução. Continuar?"`.
- [ ] `SoftDeleteCard`: add `const [retentionDays, setRetentionDays] = useState(0)`, hydrate from `d.retention_days ?? 0` in the existing fetch `.then`.
- [ ] On save: if `retentionDays > 0` and it changed from the last-hydrated value (track the hydrated value in a ref or separate state), call `window.confirm(t("settings.retentionDaysConfirm", { days: retentionDays }))` and abort the save if the user cancels. Send `retention_days: retentionDays` in the PUT body alongside the existing fields.
- [ ] Render a numeric input (same shape as the S2.3 CSV-rows input: `<Label>` + `<Input type="number" min={0}>` + hint `<p>`), placed in the Database tab under the existing soft-delete row.
- [ ] Gate: `python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json')); print('JSON OK')"` + `npm run build`.

---

## Self-Review

- **Spec coverage:** retention config (Task 1), purge job + audit trail (Task 2), UI + destructive-action confirm (Task 3, per design.md "Purga = destrutiva → off default, confirm dialog, audit-log"). Covered.
- **Safety:** purge only targets already-`deleted_at IS NOT NULL` rows; guarded by both `retentionDays <= 0` (job-level) and `SoftDeleteEnabled` (design-level) checks; every run audit-logged even at 0 rows.
- **Cadence:** 6h ticker per Julio's choice — balances responsiveness against scan overhead on (typically small) app schemas.
- **Out of scope:** per-table retention overrides, a manual "purge now" button, pool cap (S2.5) — none requested here.
