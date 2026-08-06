# S2.2b — Create-table form default follows "require RLS by default" Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the dashboard's create-table form default its RLS selection to Restricted when the global `require_rls_default` setting is on and the app has email auth, closing the S2.2 gap where the setting only affected API/omitted creates.

**Architecture:** Frontend-only. Introduce a shared `useSystemConfig()` react-query hook (fixes the existing redundant ad-hoc fetch in BrandSettingsPage per AGENTS §5), then read `require_rls_default` in AppDetailsPage's DatabaseTab to seed the create-table draft's RLS, mirroring the backend `resolveTableRLS` logic (`require_rls_default && auth_email_enabled → "enabled"`, else `"disabled"`). Add a hint line on the draft card explaining the defaulted-to-Restricted case.

**Tech Stack:** React, TypeScript, @tanstack/react-query, react-i18next, Vite.

## Global Constraints

- Every user-facing string goes through `react-i18next` (`t()`), added to BOTH `src/locales/en.json` and `src/locales/pt-BR.json` in the same change. No hardcoded PT/EN in components.
- No redundant fetch of the same endpoint from multiple components (AGENTS §5) — `/dashboard/api/config/system` must be fetched through the shared hook, not per-page.
- RLS literals: Restricted = `"enabled"`; Public = `"disabled"`. Restricted requires email auth (`auth_email_enabled`).
- Authoritative frontend gate: `npm run build` (runs `tsc -b && vite build`) from `internal/dashboard/ui`. Bare `npx tsc -b` falsely errors TS5102 — do not use it. IDE `Cannot find module '@/...'` [2307] and implicit-any [7006] are known false positives (Vite alias); `npm run build` governs.
- i18n JSON must stay valid: `python3 -c "import json; json.load(open('src/locales/en.json'))"` (and `pt-BR.json`).
- All paths below are relative to `internal/dashboard/ui/`.

---

### Task 1: Shared `useSystemConfig()` hook

**Files:**
- Modify: `src/lib/api.ts` (add interface + hook near `PublicConfig`/`usePublicConfig`, ~line 65-72)

**Interfaces:**
- Consumes: existing `apiFetch<T>`, `useQuery`, `UseQueryResult` (already imported in this file).
- Produces: `interface SystemConfig` and `function useSystemConfig(): UseQueryResult<SystemConfig>` consumed by Tasks 2 and 3.

- [ ] **Step 1: Add the interface and hook**

Insert after the `usePublicConfig()` function (immediately following its closing `}`), before the `apiFetch` definition:

```ts
export interface SystemConfig {
  soft_delete_enabled: boolean
  max_csv_export_rows: number
  statement_timeout_ms: number
  require_rls_default: boolean
}

export function useSystemConfig(): UseQueryResult<SystemConfig> {
  return useQuery({
    queryKey: ['system-config'],
    queryFn: () => apiFetch<SystemConfig>('/dashboard/api/config/system'),
    staleTime: 60000,
  })
}
```

Note: `apiFetch` is defined later in the file than `usePublicConfig`, but it is a hoisted function declaration, so referencing it here is fine (the file already calls `apiFetch` from hooks declared before its definition — e.g. `useApps`). Keep the `SystemConfig` interface to only the four fields the dashboard consumers need; `storage_config` is intentionally omitted (no consumer here, and the BrandSettings storage card is separate).

- [ ] **Step 2: Verify the build**

Run: `npm run build`
Expected: PASS, no new TS errors. (No consumer yet — this is a pure addition.)

- [ ] **Step 3: Commit** — skip; Julio commits at end (no-commit adapted SDD).

---

### Task 2: Migrate BrandSettingsPage to the shared hook

**Files:**
- Modify: `src/pages/BrandSettingsPage.tsx` (`SoftDeleteCard`, ~line 142-185)

**Interfaces:**
- Consumes: `useSystemConfig` (Task 1), `useQueryClient` from `@tanstack/react-query`.
- Produces: nothing consumed downstream.

Context: `SoftDeleteCard` currently fetches `/dashboard/api/config/system` in a `useEffect` and seeds four `useState` values. That is the redundant fetch Task 3 would otherwise duplicate. Replace the fetch with `useSystemConfig()`, keep the editable local state (the form stays editable), seed it from the query `data` via `useEffect([data])`, gate loading on the query, and invalidate the query after a successful PUT so AppDetailsPage sees the new value without a reload.

- [ ] **Step 1: Add imports**

Ensure these imports exist at the top of the file (add what's missing):

```ts
import { useSystemConfig } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
```

(`useState`/`useEffect` from React are already imported.)

- [ ] **Step 2: Replace the fetch-in-useEffect with the hook + seed effect**

In `SoftDeleteCard`, replace this block:

```ts
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [maxCsvRows, setMaxCsvRows] = useState(10000);
  const [statementTimeoutMs, setStatementTimeoutMs] = useState(30000);
  const [requireRlsDefault, setRequireRlsDefault] = useState(false);

  useEffect(() => {
    fetch("/dashboard/api/config/system", { credentials: "include" })
      .then((r) => r.json())
      .then((d) => {
        setEnabled(d.soft_delete_enabled);
        setMaxCsvRows(d.max_csv_export_rows ?? 10000);
        setStatementTimeoutMs(d.statement_timeout_ms ?? 30000);
        setRequireRlsDefault(d.require_rls_default ?? false);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);
```

with:

```ts
  const { data: sysCfg, isLoading: loading } = useSystemConfig();
  const queryClient = useQueryClient();
  const [enabled, setEnabled] = useState(false);
  const [saving, setSaving] = useState(false);
  const [maxCsvRows, setMaxCsvRows] = useState(10000);
  const [statementTimeoutMs, setStatementTimeoutMs] = useState(30000);
  const [requireRlsDefault, setRequireRlsDefault] = useState(false);

  useEffect(() => {
    if (!sysCfg) return;
    setEnabled(sysCfg.soft_delete_enabled);
    setMaxCsvRows(sysCfg.max_csv_export_rows ?? 10000);
    setStatementTimeoutMs(sysCfg.statement_timeout_ms ?? 30000);
    setRequireRlsDefault(sysCfg.require_rls_default ?? false);
  }, [sysCfg]);
```

- [ ] **Step 3: Invalidate the query after a successful save**

In `handleSave`, immediately after the `toast.success(t("system.saved"));` line (inside the `try`, after the `if (!res.ok)` check passes), add:

```ts
      queryClient.invalidateQueries({ queryKey: ["system-config"] });
```

- [ ] **Step 4: Verify the loading gate still works**

Confirm the existing `if (loading) return ...` line still references `loading` — it now comes from the query's `isLoading` alias, so no further change is needed. Do not add a separate `setLoading`.

- [ ] **Step 5: Verify the build + JSON**

Run: `npm run build`
Expected: PASS. Confirm no other component still fetches `/dashboard/api/config/system` via raw `fetch` for reading (grep: `grep -rn "config/system" src` should show only the PUT in `handleSave` and, after Task 3, none added elsewhere).

- [ ] **Step 6: Commit** — skip; Julio commits at end.

---

### Task 3: AppDetailsPage default-follows-setting + draft hint + i18n + CHANGELOG

**Files:**
- Modify: `src/pages/AppDetailsPage.tsx` (`emptyDraftTable` ~line 147-149, `DatabaseTab` ~line 169-179, the draft `TableCard` render props)
- Modify: `src/components/TableCard.tsx` (props interface ~line 95-109, destructure ~line 111-125, render near Select ~line 290-296)
- Modify: `src/locales/en.json`, `src/locales/pt-BR.json`
- Modify: `../../../CHANGELOG.md` (repo-root `CHANGELOG.md`)

**Interfaces:**
- Consumes: `useSystemConfig` (Task 1); `app.auth_email_enabled` (already on `AppDef`).
- Produces: nothing downstream.

- [ ] **Step 1: Make `emptyDraftTable` accept the default RLS**

Replace:

```ts
function emptyDraftTable(): TableDef {
  return { name: "", rls: "disabled", columns: [] };
}
```

with:

```ts
function emptyDraftTable(defaultRls: string): TableDef {
  return { name: "", rls: defaultRls, columns: [] };
}
```

- [ ] **Step 2: Compute the default in DatabaseTab and pass it**

In `DatabaseTab`, add the hook read near the other hooks (after `const { t } = useTranslation();`):

```ts
  const { data: sysCfg } = useSystemConfig();
  const requireRls = Boolean(sysCfg?.require_rls_default) && app.auth_email_enabled;
  const defaultRls = requireRls ? "enabled" : "disabled";
```

Then change `addTable`:

```ts
  const addTable = () => {
    setDraftTable(emptyDraftTable(defaultRls));
```

(Keep the rest of `addTable` unchanged.)

Ensure `useSystemConfig` is imported in AppDetailsPage — add it to the existing `@/lib/api` import (which already imports `useApp`, `useCreateAppTable`, etc.).

- [ ] **Step 3: Pass the hint to the draft TableCard**

Find where `DatabaseTab` renders the draft `TableCard` (the one built from `draftTable`, using `startInEdit`). Add a `draftRlsHint` prop that is non-empty only when the default resolves to Restricted because of the setting:

```tsx
            draftRlsHint={requireRls ? t("tableCard.defaultRlsHint") : ""}
```

Add it to the draft card's props only. If a single `TableCard` is used for both existing and draft rows, pass `draftRlsHint={requireRls ? t("tableCard.defaultRlsHint") : ""}` uniformly — the prop is only rendered for drafts inside TableCard (Step 5 gates on `isDraft`), so existing rows ignore it.

- [ ] **Step 4: Add the prop to TableCard's interface + destructure**

In `TableCardProps` (after `authEmailEnabled: boolean;`) add:

```ts
  draftRlsHint?: string;
```

In the destructured params (after `authEmailEnabled,`) add:

```ts
  draftRlsHint,
```

- [ ] **Step 5: Render the hint on drafts**

Immediately after the existing `{!authEmailEnabled && ( ... t("tableCard.restrictedHint") ... )}` block (the `</Select>`-following paragraph, ~line 294-298), add:

```tsx
      {isDraft && draftRlsHint && (
        <p className="px-4 text-[11px] text-[var(--text-secondary)]">
          {draftRlsHint}
        </p>
      )}
```

(`isDraft` is already defined as `!table.id` in TableCard.)

- [ ] **Step 6: Add i18n key to both locales**

In `src/locales/en.json`, under the existing `tableCard` object (which already has `restrictedHint`), add:

```json
"defaultRlsHint": "Restricted by default for this workspace. New email-auth tables start owner-scoped; switch to Public to opt out."
```

In `src/locales/pt-BR.json`, under `tableCard`, add:

```json
"defaultRlsHint": "Restrito por padrão neste workspace. Novas tabelas com login por email começam restritas ao dono; troque para Público para desativar."
```

(Match the surrounding key ordering/trailing-comma style of each file.)

- [ ] **Step 7: CHANGELOG entry**

In the repo-root `CHANGELOG.md`, under `## [Unreleased]`, extend the existing S2.2 "require RLS by default" note (or add a sibling bullet under the same `### Changed`/`### Added` heading it lives in) to record that the dashboard create-table form now also defaults to Restricted when the setting is on and the app has email auth (previously only API/omitted creates were affected):

```
- Dashboard create-table form now defaults new tables to Restricted (owner-scoped) when "Require RLS by default" is enabled and the app has email auth — previously the setting only affected API/omitted-`rls` creates.
```

- [ ] **Step 8: Verify build + JSON**

Run from `internal/dashboard/ui`:
- `npm run build` → PASS
- `python3 -c "import json; json.load(open('src/locales/en.json'))"` → no error
- `python3 -c "import json; json.load(open('src/locales/pt-BR.json'))"` → no error

- [ ] **Step 9: Commit** — skip; Julio commits at end.

---

## Self-Review

- **Coverage:** Shared hook (Task 1) ✓; redundant-fetch fix / BrandSettings migration (Task 2) ✓; form default follows setting (Task 3 Steps 1-2) ✓; honest hint copy (Task 3 Steps 3-6) ✓; CHANGELOG (Task 3 Step 7) ✓; i18n both locales ✓.
- **Type consistency:** `useSystemConfig` returns `UseQueryResult<SystemConfig>`; consumers read `.data` as `SystemConfig | undefined` and guard with `?.`/`Boolean(...)`. `emptyDraftTable(defaultRls: string)` — every call site updated (single call site in `addTable`). `draftRlsHint?: string` optional, safe on existing rows.
- **Mirror correctness:** `requireRls = require_rls_default && auth_email_enabled` matches backend `resolveTableRLS` (empty + require + auth → "enabled"). The form still lets the user override to Public before submit; backend only forces on omitted `rls`, and the form always sends an explicit value — so the visible default IS the effective behavior for dashboard creates. No double-enforcement conflict.
- **No behavior change when setting off:** `require_rls_default` defaults false → `defaultRls = "disabled"` → identical to today. Hint hidden.
