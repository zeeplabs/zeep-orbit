# Settings Consolidation — S2.2 (Require RLS by default) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development to implement task-by-task. Checkbox (`- [ ]`) steps.

**Goal:** Add a global, opt-in `require_rls_default` setting (default `false`) so that a new app table created without an explicit access level defaults to Restricted (owner-scoped RLS) instead of Public — but only when the app has email auth enabled, otherwise it stays Public (Restricted without email auth is a guaranteed provisioning failure).

**Architecture:** Extend `zeep_system.system_config` + `dashboard.SystemConfig` with `require_rls_default bool` (idempotent `ADD COLUMN`, default `false`), through the existing merge-on-absent PUT. Enforcement lives in `CreateAppTable` (`internal/dashboard/handler.go`) via a new pure helper `resolveTableRLS(requested, requireRLSDefault, authEmailEnabled) string` that fills an empty access level with `"enabled"` only under the two conditions; the handler reads the global config with a direct `GetSystemConfig` DB read (the established pattern for a mutation handler — no registry cache needed, this is a low-frequency admin path). A Switch is added to the Settings→Database tab.

**Tech Stack:** Go (pgx v5, chi), React/Vite/TS, react-i18next.

## Global Constraints

- **Default `false` = opt-in, NO behavior change** for existing installs. Unlike S2.4 (statement timeout), this does NOT need a behavior-change callout — a normal `### Added` CHANGELOG entry is correct.
- **Restricted literal is `"enabled"`** (the UI-canonical value; `"owner"` is a legacy synonym the DDL/enforcement treat identically). When the default fires, set access to `"enabled"`, never `"owner"`.
- **The default only fills an *omitted* access level** (`body.RLS == ""`). An explicit `"disabled"` (Public) or `"enabled"` (Restricted) from the client is always respected — the setting never overrides an explicit choice. (Note: the dashboard create-table form always sends an explicit value, so this default primarily governs API-created / access-omitted tables — see the follow-up note in Task 1; making the create-table *form's* default follow the setting is a separate slice, out of scope here.)
- **Email-auth guard:** the default fires only when `app.AuthEmailEnabled` is true. With no email auth, an empty access level stays empty (→ Public) — never force Restricted, which `validateTableInput` (handler.go:118) would reject anyway (owner_id FK targets `_auth_users`, only created when email auth is on).
- **Enforcement is CREATE-only.** Apply the default in `CreateAppTable` (handler.go:1107 choke point), NOT in `UpdateAppTable` — editing a table is an explicit action and must not have a Public choice silently flipped to Restricted.
- Config PUT stays merge-on-absent: a `nil` patch field leaves the stored value unchanged; a present `*bool` (including `&false`) is applied (AGENTS §4 — see `mergeSystemConfig`).
- Schema change additive + idempotent (`ADD COLUMN IF NOT EXISTS ... NOT NULL DEFAULT false`) — runs safely on every boot (AGENTS §8, additive only).
- API error strings English; don't leak raw `err.Error()` into 500s (AGENTS §4).
- Every user-facing string via react-i18next in BOTH `en.json` and `pt-BR.json`, same change (AGENTS §5).
- Backend gate (AGENTS §3): `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l <changed .go>` — all clean. New tests (merge + `resolveTableRLS`) are **pure unit tests** (no DB) and always run.
- Frontend gate: `python3 -c "import json; ..."` on both locales + `npm run build` (pinned `tsc -b && vite build`). Bare `npx tsc -b` falsely errors (TS5102). IDE `Cannot find module '@/...'` / `implicitly any` are false positives — `npm run build` is authoritative.
- **Do not commit** — Julio commits. No `git add`/`git commit` steps.

Backend files live under repo root; frontend base dir `internal/dashboard/ui/`.

Current state (merged from S2.3 + S2.4): `dashboard.SystemConfig{ SoftDeleteEnabled bool; StorageConfig *GlobalStorageConfig; MaxCSVExportRows int; StatementTimeoutMs int }`, with `systemConfigPatch` (pointer fields) + pure `mergeSystemConfig`, `GetSystemConfig`/`UpsertSystemConfig(ctx, pool, *SystemConfig)`, `UpdateSystemConfig` load→merge→upsert. `CreateAppTable` at handler.go:1079; its choke point `table := AppTableRow{... RLS: body.RLS ...}` is line 1107, right after body decode and before `validateTableInput` (line 1108). `validateTableInput` is at handler.go:105; the email-auth guard at handler.go:118. `app.AuthEmailEnabled bool` is the "has email auth" flag. `tableRequestBody.RLS string json:"rls"` at handler.go:855. `h.writeError(w, r, status, msg, err)` and `GetSystemConfig(ctx, h.pool)` are established. `SoftDeleteCard` in `BrandSettingsPage.tsx` is the Database tab; it already renders a soft-delete `Switch`, hydrates from the config fetch, and its PUT body sends `{ soft_delete_enabled, max_csv_export_rows, statement_timeout_ms }`.

---

### Task 1: Backend — `require_rls_default` config + Restricted-by-default enforcement

**Files:**
- Modify: `internal/dashboard/system_config_store.go` (struct + patch + merge + `GetSystemConfig` scan + `UpsertSystemConfig` persist)
- Modify: `internal/dashboard/provisioner.go:147` (add `ADD COLUMN` after the `statement_timeout_ms` one)
- Modify: `internal/dashboard/handler.go` (new `resolveTableRLS` helper near `validateTableInput` ~line 105; wire it into `CreateAppTable` at ~line 1106)
- Modify: `CHANGELOG.md` (`[Unreleased]` → `### Added`)
- Test: `internal/dashboard/system_config_merge_test.go` (extend) + `internal/dashboard/table_rls_test.go` (new, pure unit test)

**Interfaces:**
- Produces: `dashboard.SystemConfig.RequireRLSDefault bool`; `systemConfigPatch.RequireRLSDefault *bool`; `func resolveTableRLS(requested string, requireRLSDefault, authEmailEnabled bool) string`.

- [ ] **Step 1: Write the failing `resolveTableRLS` unit test** — `internal/dashboard/table_rls_test.go` (new; pure, no DB):

```go
package dashboard

import "testing"

func TestResolveTableRLS(t *testing.T) {
	cases := []struct {
		name             string
		requested        string
		requireDefault   bool
		authEmailEnabled bool
		want             string
	}{
		{"omitted + require + auth → enabled", "", true, true, "enabled"},
		{"omitted + require + no auth → stays public", "", true, false, ""},
		{"omitted + no require → stays public", "", false, true, ""},
		{"explicit public always respected", "disabled", true, true, "disabled"},
		{"explicit restricted respected", "enabled", true, true, "enabled"},
		{"omitted + no require + no auth → public", "", false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveTableRLS(c.requested, c.requireDefault, c.authEmailEnabled)
			if got != c.want {
				t.Fatalf("resolveTableRLS(%q, %v, %v) = %q, want %q",
					c.requested, c.requireDefault, c.authEmailEnabled, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Extend the merge unit test** — append to `internal/dashboard/system_config_merge_test.go` (leave existing tests untouched):

```go
func TestMergeSystemConfigRequireRLSDefault(t *testing.T) {
	cur := SystemConfig{RequireRLSDefault: true, StatementTimeoutMs: 30000}

	// Absent from patch → preserved.
	n := 5000
	merged := mergeSystemConfig(cur, systemConfigPatch{StatementTimeoutMs: &n})
	if !merged.RequireRLSDefault {
		t.Fatalf("require_rls_default must be preserved when absent, got %v", merged.RequireRLSDefault)
	}

	// Present false → applied (turning it off is a real change, not "absent").
	off := false
	merged = mergeSystemConfig(cur, systemConfigPatch{RequireRLSDefault: &off})
	if merged.RequireRLSDefault {
		t.Fatalf("require_rls_default false must be applied, got %v", merged.RequireRLSDefault)
	}
}
```

- [ ] **Step 3: Run both — expect FAIL** (compile errors: `resolveTableRLS`, `RequireRLSDefault` undefined):

```bash
go test ./internal/dashboard/ -run 'TestResolveTableRLS|TestMergeSystemConfig' -v
```

- [ ] **Step 4: Add the field to `dashboard.SystemConfig`, the patch, and the merge fn** in `internal/dashboard/system_config_store.go`. In the `SystemConfig` struct (after `StatementTimeoutMs`):

```go
	RequireRLSDefault bool `json:"require_rls_default"`
```

In `systemConfigPatch` (after `StatementTimeoutMs`):

```go
	RequireRLSDefault *bool `json:"require_rls_default,omitempty"`
```

In `mergeSystemConfig`, before `return cur`:

```go
	if patch.RequireRLSDefault != nil {
		cur.RequireRLSDefault = *patch.RequireRLSDefault
	}
```

- [ ] **Step 5: Scan the new column in `GetSystemConfig`.** Add `COALESCE(require_rls_default, false)` as the 5th column of the SELECT + a matching `&cfg.RequireRLSDefault` in the Scan:

```go
	err := pool.QueryRow(ctx,
		`SELECT soft_delete_enabled, storage_config, COALESCE(max_csv_export_rows, 10000), COALESCE(statement_timeout_ms, 30000), COALESCE(require_rls_default, false)
		 FROM zeep_system.system_config LIMIT 1`,
	).Scan(&cfg.SoftDeleteEnabled, &rawStorage, &cfg.MaxCSVExportRows, &cfg.StatementTimeoutMs, &cfg.RequireRLSDefault)
```

(The `return &SystemConfig{}, nil` error path leaves `RequireRLSDefault` at `false` = off — safe fallback: a failed read never silently forces Restricted.)

- [ ] **Step 6: Persist the column in `UpsertSystemConfig`.** Extend the INSERT column list, `VALUES`, and `DO UPDATE SET` to include `require_rls_default`, and add `cfg.RequireRLSDefault` to the args (a plain bool, no clamp needed). The statement becomes:

```go
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled, storage_config, max_csv_export_rows, statement_timeout_ms, require_rls_default)
		 VALUES ($1, $2::jsonb, $3, $4, $5)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET soft_delete_enabled = $1,
		       storage_config = $2::jsonb,
		       max_csv_export_rows = $3,
		       statement_timeout_ms = $4,
		       require_rls_default = $5`,
		cfg.SoftDeleteEnabled, rawJSON, maxCSV, stmtTimeout, cfg.RequireRLSDefault,
	)
```

(Keep the existing `maxCSV` and `stmtTimeout` local normalizations above this block exactly as they are — only the SQL, the column/value lists, and the final arg are extended.)

- [ ] **Step 7: Add the migration** in `internal/dashboard/provisioner.go`, immediately after the `statement_timeout_ms` `ADD COLUMN` (line 147), same idempotent style:

```go
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS require_rls_default BOOLEAN NOT NULL DEFAULT false`,
```

- [ ] **Step 8: Add the `resolveTableRLS` helper** in `internal/dashboard/handler.go`, directly above `validateTableInput` (~line 104):

```go
// resolveTableRLS applies the global "require RLS by default" setting: when a
// create request omits the access level and the setting is on AND the app has
// email auth (required for owner-scoped RLS), the table defaults to Restricted
// ("enabled"). An explicit access level from the client is always respected,
// and without email auth an omitted level stays Public (empty) — forcing
// Restricted there would fail provisioning (owner_id FK needs _auth_users).
func resolveTableRLS(requested string, requireRLSDefault, authEmailEnabled bool) string {
	if requested == "" && requireRLSDefault && authEmailEnabled {
		return "enabled"
	}
	return requested
}
```

- [ ] **Step 9: Wire it into `CreateAppTable`** in `internal/dashboard/handler.go`. Between the body decode (ends line 1105) and the `table := AppTableRow{...}` (line 1107), insert the config read + default application (`err` is already declared from the `GetApp` call above, so `sysCfg, err :=` is valid — `sysCfg` is the new variable):

```go
	sysCfg, err := GetSystemConfig(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load system config", err)
		return
	}
	body.RLS = resolveTableRLS(body.RLS, sysCfg.RequireRLSDefault, app.AuthEmailEnabled)
```

The existing line 1107 (`table := AppTableRow{Name: body.Name, RLS: body.RLS, ...}`) then picks up the resolved `body.RLS` with no further change. Do NOT touch `UpdateAppTable` — the default is create-only.

- [ ] **Step 10: Run both tests — expect PASS:**

```bash
go test ./internal/dashboard/ -run 'TestResolveTableRLS|TestMergeSystemConfig' -v
```

- [ ] **Step 11: Add the CHANGELOG entry** under `## [Unreleased]` → `### Added` in `CHANGELOG.md` (normal Added — opt-in, no behavior change):

```markdown
- **Require RLS by default** (Settings → Database): when enabled, a new app table created without an explicit access level defaults to Restricted (owner-scoped RLS) instead of Public. Off by default; only applies when the app has email auth enabled (Restricted requires it), and never overrides an access level the client sends explicitly.
```

- [ ] **Step 12: Full backend gate:**

```bash
go build ./... && go vet ./... && go test ./... 2>&1 | grep -vE "^(ok|---|\?)" ; gofmt -l internal/dashboard/system_config_store.go internal/dashboard/provisioner.go internal/dashboard/handler.go internal/dashboard/system_config_merge_test.go internal/dashboard/table_rls_test.go
```
Expected: build/vet clean, tests pass (no non-ok lines), gofmt prints nothing.

---

### Task 2: Frontend — "Require RLS by default" switch in the Database tab

**Files:**
- Modify: `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx` (`SoftDeleteCard` — the Database tab)
- Modify: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`

**Interfaces:**
- Consumes: the `PUT /dashboard/api/config/system` merge endpoint. `SoftDeleteCard` already owns `soft_delete_enabled` + `max_csv_export_rows` + `statement_timeout_ms`; this adds `require_rls_default` to the same PUT body.

- [ ] **Step 1: i18n keys (en.json)** — add alongside the existing `settings.statementTimeoutMs` keys:

```json
"settings.requireRlsDefault": "Require RLS by default",
"settings.requireRlsDefaultHint": "New tables created without an explicit access level default to Restricted (owner-scoped). Only applies when the app has email auth enabled.",
```

- [ ] **Step 2: i18n keys (pt-BR.json)** — same keys, mirrored:

```json
"settings.requireRlsDefault": "Exigir RLS por padrão",
"settings.requireRlsDefaultHint": "Tabelas novas criadas sem nível de acesso explícito nascem Restritas (por dono). Só se aplica quando o app tem autenticação por e-mail habilitada.",
```

- [ ] **Step 3: Add state + hydration in `SoftDeleteCard`.** Next to the existing `statementTimeoutMs` state:

```tsx
  const [requireRlsDefault, setRequireRlsDefault] = useState(false);
```

In the same fetch `.then((d) => { ... })` where `statement_timeout_ms` is read:

```tsx
        setRequireRlsDefault(d.require_rls_default ?? false);
```

- [ ] **Step 4: Send the field in the PUT** — extend the existing body object (this card owns these four; merge preserves the rest):

```tsx
        body: JSON.stringify({
          soft_delete_enabled: enabled,
          max_csv_export_rows: maxCsvRows,
          statement_timeout_ms: statementTimeoutMs,
          require_rls_default: requireRlsDefault,
        }),
```

- [ ] **Step 5: Render a labeled Switch row.** Mirror the existing soft-delete Switch row's markup exactly (same wrapper classes / `Switch` usage this card already uses for `enabled`/`setEnabled`), substituting label `t("settings.requireRlsDefault")`, hint `t("settings.requireRlsDefaultHint")`, and `checked={requireRlsDefault}` / `onCheckedChange={setRequireRlsDefault}`. Place it directly below the soft-delete row, above the numeric-input blocks. Read the soft-delete row in the file first and copy its structure so spacing/typography match; do not invent new class names.

- [ ] **Step 6: Frontend gate:**

```bash
cd internal/dashboard/ui
python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json')); print('JSON OK')"
npm run build
```
Expected: JSON OK, build clean. Visual (audit env `/configuracoes` → Database tab): the "Require RLS by default" switch shows off, toggling + Save persists across reload, and toggling it does not wipe the storage tab's config (merge intact).

---

## Self-Review

- **Spec coverage (design.md line 70 + line 87 + line 16):** config field + migration (T1 Steps 4-7), enforcement in `CreateAppTable` with the email-auth edge (T1 Steps 8-9 + the pure test T1 Step 1 covering all six cases), off-by-default opt-in (T1 Step 7 `DEFAULT false`), UI switch (T2). Covered.
- **Placeholder scan:** none — full code for every logic step; line anchors from the current files; T2 Step 5 is a concrete "mirror the existing soft-delete Switch row" instruction (the row exists in the file), not a TODO.
- **Type consistency:** `RequireRLSDefault` is `bool` in `dashboard.SystemConfig`, `*bool` in `systemConfigPatch`; `resolveTableRLS(string, bool, bool) string` defined T1 Step 8, used T1 Step 9 and tested T1 Step 1. JSON key `require_rls_default` identical across Go tag, PUT body, hydration, and SQL column. No registry-layer change (enforcement reads DB via `GetSystemConfig`, not the hot-path cache).
- **Semantics guard:** the one place S2.2 could go wrong is forcing Restricted without email auth (provisioning failure) or overriding an explicit Public choice — both are pinned by `resolveTableRLS` (empty-only + auth-gated) and asserted by the six-case test. Enforcement is create-only (T1 Step 9 explicitly leaves `UpdateAppTable` alone).
- **Out of scope (not S2.2):** retention/purge (S2.1), pool cap (deferred); making the create-table *form's* default selection follow the setting (noted follow-up — the form always sends an explicit value today, so the backend default governs API/omitted creates); README config table (DB-stored setting, not env — CHANGELOG suffices, consistent with S2.3/S2.4).
