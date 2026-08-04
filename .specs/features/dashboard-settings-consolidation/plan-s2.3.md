# Settings Consolidation — S2.3 (Max rows per CSV export + config merge fix) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development to implement task-by-task. Checkbox (`- [ ]`) steps.

**Goal:** Make the `PUT /dashboard/api/config/system` endpoint merge-on-absent (fixing a latent bug where toggling soft-delete wipes the global storage config), then add a configurable `max_csv_export_rows` global setting that the Data Browser CSV export reads instead of the hardcoded 10000, plus its Database-tab input.

**Architecture:** Extend the single-row `zeep_system.system_config` table + `dashboard.SystemConfig` struct with `max_csv_export_rows` (idempotent `ADD COLUMN`, default 10000). Change `UpsertSystemConfig` to persist the whole config struct, and rewrite `UpdateSystemConfig` to decode a pointer-field patch, load current config, and merge (absent field = keep current) via a new pure `mergeSystemConfig` — mirroring the established `mergeProviderConfig` pattern (AGENTS §4). `DataBrowserExport` reads the cap from config. Frontend adds a numeric input to the Database tab.

**Tech Stack:** Go (pgx, chi), React/Vite/TS, react-i18next.

## Global Constraints

- API error strings in English; don't leak raw `err.Error()` into 500s (AGENTS §4).
- Config PUT is merge-on-absent: a field absent from the request body must NOT change its stored value. Pointer patch fields distinguish "absent" from "zero" (AGENTS §4 — see `mergeProviderConfig`).
- Schema change is additive + idempotent (`ADD COLUMN IF NOT EXISTS`, `NOT NULL DEFAULT 10000`) so it runs safely on every boot (AGENTS §8 — schema migration, higher-risk: additive only).
- `max_csv_export_rows` default = 10000 (preserves today's hardcoded cap). A read of 0/invalid falls back to 10000.
- Every user-facing string via react-i18next in BOTH `en.json` and `pt-BR.json`.
- Backend gate (AGENTS §3): `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l <changed .go>` — all clean. `go test ./...` skips DB-backed tests when `TEST_DATABASE_URL` is unset; the new merge test is a **pure unit test** (no DB) so it always runs.
- Frontend gate: `python3 -c "import json; ..."` on both locales + `npm run build` (pinned `tsc -b && vite build`). Bare `npx tsc -b` falsely errors (TS5102 baseUrl) — do not use it. IDE `Cannot find module '@/...'`/`implicitly any` are false positives.
- **Do not commit** — Julio commits. No `git add`/`git commit` steps.

Backend files live under repo root; frontend base dir `internal/dashboard/ui/`.

---

### Task 1: Backend — merge-on-absent config PUT + `max_csv_export_rows`

**Files:**
- Modify: `internal/dashboard/system_config_store.go` (struct + `GetSystemConfig` + `UpsertSystemConfig` signature + new `mergeSystemConfig`)
- Modify: `internal/dashboard/provisioner.go:~145` (ADD COLUMN)
- Modify: `internal/dashboard/handler.go:391-424` (`UpdateSystemConfig`) and `:1784-1823` (`DataBrowserExport`)
- Test: `internal/dashboard/system_config_merge_test.go` (new, pure unit test)

**Interfaces:**
- Produces: `SystemConfig.MaxCSVExportRows int`; `type systemConfigPatch struct{...}`; `func mergeSystemConfig(cur SystemConfig, patch systemConfigPatch) SystemConfig`; `UpsertSystemConfig(ctx, pool, cfg *SystemConfig)`.

- [ ] **Step 1: Write the failing merge unit test** — `internal/dashboard/system_config_merge_test.go`:

```go
package dashboard

import "testing"

func TestMergeSystemConfigPreservesAbsentFields(t *testing.T) {
	cur := SystemConfig{
		SoftDeleteEnabled: true,
		StorageConfig:     &GlobalStorageConfig{Bucket: "b1", Region: "r1"},
		MaxCSVExportRows:  5000,
	}
	// Patch toggles ONLY soft-delete off; storage + csv must survive.
	sd := false
	merged := mergeSystemConfig(cur, systemConfigPatch{SoftDeleteEnabled: &sd})
	if merged.SoftDeleteEnabled != false {
		t.Fatalf("soft delete should be updated to false")
	}
	if merged.StorageConfig == nil || merged.StorageConfig.Bucket != "b1" {
		t.Fatalf("storage config must be preserved when absent from patch, got %+v", merged.StorageConfig)
	}
	if merged.MaxCSVExportRows != 5000 {
		t.Fatalf("max csv rows must be preserved when absent, got %d", merged.MaxCSVExportRows)
	}
}

func TestMergeSystemConfigAppliesPresentFields(t *testing.T) {
	cur := SystemConfig{SoftDeleteEnabled: true, MaxCSVExportRows: 10000}
	n := 2000
	merged := mergeSystemConfig(cur, systemConfigPatch{MaxCSVExportRows: &n})
	if merged.MaxCSVExportRows != 2000 {
		t.Fatalf("max csv rows should update to 2000, got %d", merged.MaxCSVExportRows)
	}
	if merged.SoftDeleteEnabled != true {
		t.Fatalf("soft delete should be preserved, got %v", merged.SoftDeleteEnabled)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (compile error: `mergeSystemConfig`/`systemConfigPatch`/`MaxCSVExportRows` undefined):

```bash
go test ./internal/dashboard/ -run TestMergeSystemConfig -v
```

- [ ] **Step 3: Add the field, patch type, and merge fn** in `internal/dashboard/system_config_store.go`. Add to the `SystemConfig` struct:

```go
	MaxCSVExportRows int `json:"max_csv_export_rows"`
```

Add the patch type + merge fn (mirrors `mergeProviderConfig` — absent = keep current; pointers distinguish absent from zero):

```go
// systemConfigPatch is a partial update: a nil field means "leave unchanged".
type systemConfigPatch struct {
	SoftDeleteEnabled *bool                `json:"soft_delete_enabled,omitempty"`
	StorageConfig     *GlobalStorageConfig `json:"storage_config,omitempty"`
	MaxCSVExportRows  *int                 `json:"max_csv_export_rows,omitempty"`
}

// mergeSystemConfig overlays only the fields present in the patch onto the
// current config (merge-on-absent — see mergeProviderConfig for the pattern).
func mergeSystemConfig(cur SystemConfig, patch systemConfigPatch) SystemConfig {
	if patch.SoftDeleteEnabled != nil {
		cur.SoftDeleteEnabled = *patch.SoftDeleteEnabled
	}
	if patch.StorageConfig != nil {
		cur.StorageConfig = patch.StorageConfig
	}
	if patch.MaxCSVExportRows != nil {
		cur.MaxCSVExportRows = *patch.MaxCSVExportRows
	}
	return cur
}
```

- [ ] **Step 4: Scan the new column in `GetSystemConfig`** — update the SELECT + Scan (COALESCE guards a freshly-added NULL row, though the column is NOT NULL DEFAULT):

```go
	err := pool.QueryRow(ctx,
		`SELECT soft_delete_enabled, storage_config, COALESCE(max_csv_export_rows, 10000)
		 FROM zeep_system.system_config LIMIT 1`,
	).Scan(&cfg.SoftDeleteEnabled, &rawStorage, &cfg.MaxCSVExportRows)
```

(The `return &SystemConfig{}, nil` error path leaves `MaxCSVExportRows` at 0; read sites fall back to 10000.)

- [ ] **Step 5: Change `UpsertSystemConfig` to take the full config** and persist all columns. Replace the signature and body:

```go
func UpsertSystemConfig(ctx context.Context, pool *db.Pool, cfg *SystemConfig) (*SystemConfig, error) {
	var rawJSON string
	if cfg.StorageConfig != nil && cfg.StorageConfig.Bucket != "" {
		b, _ := json.Marshal(cfg.StorageConfig)
		rawJSON = string(b)
	}
	if rawJSON == "" {
		rawJSON = "{}"
	}
	maxCSV := cfg.MaxCSVExportRows
	if maxCSV <= 0 {
		maxCSV = 10000
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled, storage_config, max_csv_export_rows)
		 VALUES ($1, $2::jsonb, $3)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET soft_delete_enabled = $1,
		       storage_config = $2::jsonb,
		       max_csv_export_rows = $3`,
		cfg.SoftDeleteEnabled, rawJSON, maxCSV,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert system config: %w", err)
	}
	return GetSystemConfig(ctx, pool)
}
```

- [ ] **Step 6: Add the migration** in `internal/dashboard/provisioner.go`, right after the existing `storage_config` `ADD COLUMN IF NOT EXISTS` (~line 145), same idempotent style:

```go
	`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS max_csv_export_rows INT NOT NULL DEFAULT 10000`,
```

(Match the surrounding statement-list syntax exactly — read lines 140-150 first and follow the existing pattern for how these DDL strings are declared/executed.)

- [ ] **Step 7: Rewrite `UpdateSystemConfig`** (`handler.go:391`) to decode the patch, load current, merge, upsert. Replace the body block (keep the auth guard + MaxBytesReader):

```go
	var patch systemConfigPatch
	if !h.decodeJSONBody(w, r, &patch) {
		return
	}

	current, err := GetSystemConfig(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load system config", err)
		return
	}
	merged := mergeSystemConfig(*current, patch)

	cfg, err := UpsertSystemConfig(r.Context(), h.pool, &merged)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update system config", err)
		return
	}

	if cfg.StorageConfig != nil {
		cfg.StorageConfig.SecretAccessKey = ""
	}

	h.reg.SetSystemConfig(registry.SystemConfig{SoftDeleteEnabled: cfg.SoftDeleteEnabled})

	writeJSON(w, http.StatusOK, cfg)
	h.audit(r.Context(), user.ID, user.Email, "config.system.update", "config", "system", "", nil, r.RemoteAddr)
```

- [ ] **Step 8: Read the cap in `DataBrowserExport`** (`handler.go:~1784`). Replace `const exportLimit = 10000` with a config read (place it after the `table, ok := app.Tables[...]` lookup, before building params):

```go
	exportLimit := 10000
	if sysCfg, cfgErr := GetSystemConfig(r.Context(), h.pool); cfgErr == nil && sysCfg.MaxCSVExportRows > 0 {
		exportLimit = sysCfg.MaxCSVExportRows
	}
```

Leave the existing uses of `exportLimit` (params["limit"] and the `len(sanitized) == exportLimit` truncation check) unchanged — they now read the variable.

- [ ] **Step 9: Run the merge test — expect PASS:**

```bash
go test ./internal/dashboard/ -run TestMergeSystemConfig -v
```

- [ ] **Step 10: Full backend gate:**

```bash
go build ./... && go vet ./... && go test ./... 2>&1 | grep -vE "^(ok|---|\?)" ; gofmt -l internal/dashboard/system_config_store.go internal/dashboard/handler.go internal/dashboard/provisioner.go internal/dashboard/system_config_merge_test.go
```
Expected: build/vet clean, tests pass (no non-ok lines), gofmt prints nothing.

- [ ] **Step 11 (optional, real DB): run the store round-trip against the audit DB** if available:
```bash
TEST_DATABASE_URL="postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable" go test ./internal/dashboard/ -run TestMergeSystemConfig -v
```
(Pure unit test needs no DB; this just confirms nothing else in the package regressed with a DB present.)

---

### Task 2: Frontend — Max-CSV input in the Database tab

**Files:**
- Modify: `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx` (`SoftDeleteCard` — the Database tab)
- Modify: `src/locales/en.json`, `src/locales/pt-BR.json`

**Interfaces:**
- Consumes: the `PUT /dashboard/api/config/system` merge endpoint from Task 1 (send only the fields this card owns).

- [ ] **Step 1: i18n keys (en.json)**:

```json
"settings.maxCsvExportRows": "Max rows per CSV export",
"settings.maxCsvExportRowsHint": "The Data Browser CSV export stops after this many rows.",
```

- [ ] **Step 2: i18n keys (pt-BR.json)**:

```json
"settings.maxCsvExportRows": "Máximo de linhas por exportação CSV",
"settings.maxCsvExportRowsHint": "A exportação CSV do Data Browser para após esse número de linhas.",
```

- [ ] **Step 3: Add state + hydration in `SoftDeleteCard`.** After the `enabled`/`loading`/`saving` state, add:

```tsx
  const [maxCsvRows, setMaxCsvRows] = useState(10000);
```

In the existing `useEffect` fetch `.then((d) => { ... })`, also read the value:

```tsx
      .then((d) => {
        setEnabled(d.soft_delete_enabled);
        setMaxCsvRows(d.max_csv_export_rows ?? 10000);
      })
```

- [ ] **Step 4: Send the field in the PUT** (this card owns soft-delete + max-csv; merge preserves the rest):

```tsx
        body: JSON.stringify({ soft_delete_enabled: enabled, max_csv_export_rows: maxCsvRows }),
```

- [ ] **Step 5: Render the numeric input** below the existing approval `SoonRow`, above the Save divider, in the Database tab card:

```tsx
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <Label className="text-[13px] font-semibold text-[var(--text-secondary)]">
          {t("settings.maxCsvExportRows")}
        </Label>
        <Input
          type="number"
          min={1}
          value={maxCsvRows}
          onChange={(e) => setMaxCsvRows(Math.max(1, Number(e.target.value) || 1))}
          className="mt-2 max-w-[200px]"
        />
        <p className="mt-1 text-[11px] text-[var(--text-tertiary)]">{t("settings.maxCsvExportRowsHint")}</p>
      </div>
```

- [ ] **Step 6: Frontend gate:**

```bash
python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json')); print('JSON OK')"
npm run build
```
Expected: JSON OK, build clean. Visual (audit env `/configuracoes` → Database tab): the "Max rows per CSV export" number input shows 10000, edits persist across reload, and toggling soft-delete no longer wipes the storage tab's config.

---

## Self-Review

- **Spec coverage:** S2.3 design items — configurable CSV cap (Task 1 Steps 4-8 + Task 2), merge-on-absent PUT / storage-wipe bugfix (Task 1 Steps 3,7 + test Step 1), idempotent migration (Step 6). Covered.
- **Placeholders:** none — full code for every logic step; the one "read lines 140-150 and match the surrounding syntax" (Step 6) is a concrete instruction to match an existing idempotent-DDL list, not a TODO.
- **Type consistency:** `systemConfigPatch` / `mergeSystemConfig` / `MaxCSVExportRows` / `UpsertSystemConfig(ctx, pool, *SystemConfig)` are defined in Task 1 Steps 3,5 and used consistently in Steps 7-8 and the test. The single existing `UpsertSystemConfig` caller (handler.go:411) is rewritten in Step 7 — no stale 3-arg call remains.
- **Bug scope:** the storage-wipe fix is a direct consequence of merge-on-absent; the test (Step 1) asserts exactly the previously-broken path (soft-delete-only patch preserves storage).
- **Out of scope (not S2.3):** other net-new controls (RLS default, statement timeout, retention/purge → later slices), the registry `SystemConfig` struct (unchanged — export reads via `GetSystemConfig`, a direct DB read; soft-delete still flows through the registry cache as before).
