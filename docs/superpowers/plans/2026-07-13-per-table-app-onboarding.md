# Per-table app onboarding — implementation plan (lean)

Spec: `docs/superpowers/specs/2026-07-13-per-table-app-onboarding-design.md`

Executed inline this session (no subagent dispatch). Task list, not full
line-by-line code — architecture and file responsibilities only.

## Backend

1. `internal/provisioner/table.go`: add `DropTable(ctx, schemaName, tableName) error`
   (`DROP TABLE IF EXISTS %q.%q`, mirrors `createTable`'s quoting).
2. `internal/dashboard/apps_store.go`: add `InsertAppTable`, `UpdateAppTable`
   (rls+columns only, name immutable after creation), `DeleteAppTable`
   (`DELETE ... RETURNING name`). Drop the `tables` param from `CreateApp`/
   `UpdateApp` — bulk endpoints stop touching tables.
3. `internal/dashboard/handler.go`:
   - `validateTableInput(t AppTableRow, authEmailEnabled bool, otherTables []AppTableRow) error`
     — duplicate table name (vs `otherTables`), duplicate column name, invalid
     type, RLS×auth rule. Replaces the table-related checks currently inside
     `validateAppInput`, which shrinks back to app-name-only.
   - New handlers `CreateAppTable`, `UpdateAppTable`, `DeleteAppTable`:
     load full app via `GetApp`, validate, mutate via apps_store, call
     `provisioner.Apply` with a config containing just that one table (or
     `DropTable` for delete), re-fetch full app, `h.reg.Register(...)` to
     resync the live registry, `h.writeError`/`writeJSON` per existing
     conventions, audit log entry.
   - Update `appRequestBody` (drop `tables` field), `CreateApp`/`UpdateApp`
     handlers (drop tables handling), `handler_test.go` (drop the now-removed
     `validateAppInput` table cases, add `validateTableInput` cases: dup
     table name, dup column name, RLS-without-auth, accepts valid).
4. `internal/server/server.go`: register
   `POST/PUT/DELETE /api/apps/{id}/tables[/{tableId}]`, same
   `RequireAuth` middleware as sibling routes.

## Frontend

5. `internal/dashboard/ui/src/lib/api.ts`: `CreateAppInput` drops `tables`;
   add `createAppTable`, `updateAppTable`, `deleteAppTable` fetch functions.
6. New `internal/dashboard/ui/src/components/TableCard.tsx`: encapsulates one
   table's saved/editing state machine (save → POST/PUT, cancel, delete →
   DELETE, inline per-card error), including the column editor rows moved
   from `AppFormPage.tsx`. RLS "Restrito" option disabled when
   `!authEmailEnabled`.
7. New `internal/dashboard/ui/src/pages/AppOnboardingPage.tsx`: name +
   auth-email toggle only, `POST /apps`, navigate to `/apps/:id`.
8. New `internal/dashboard/ui/src/pages/AppDetailsPage.tsx`: tabs shell
   (Banco de Dados via `TableCard` list + "Adicionar Tabela" gated to one
   draft at a time, Login/Storage/Rate Limit/API — moved near-verbatim from
   `AppFormPage.tsx`, each keeping its own "Salvar" button against the
   existing bulk `UpdateApp` for those non-table fields).
9. `App.tsx`: `/apps/new` → `AppOnboardingPage`, `/apps/:id` → `AppDetailsPage`,
   `/apps/:id/edit` → redirect to `/apps/:id`. Delete `AppFormPage.tsx`.
10. `e2e/helpers.ts` + `e2e/apps.spec.ts`: rewrite `createTestApp` (redirects
    to `/apps/:id` now) and the "create app with table" test for the new
    flow (add table → save → assert saved → edit → save → delete).

## Verification

- `go build ./... && go vet ./... && go test ./...` after each backend step.
- Manual repro same as the bug-fix session: local Postgres via
  `docker compose up -d db`, `go run ./cmd/zeep serve`, Vite dev server,
  Playwright driving the real browser flow end to end.
