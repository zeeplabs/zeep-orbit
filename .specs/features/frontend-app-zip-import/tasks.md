# Frontend App ZIP Import Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/frontend-app-zip-import/design.md`
**Status**: Approved — execution scheduled for a later session (2-batch sub-agent plan: Batch A = Phases 1+2, Batch B = Phases 3+4+5)

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 ("Before considering any change done": `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`; frontend `npx tsc -b`, `npm run build`). No repo-wide coverage threshold config found (no `.nycrc`/coverage gate in CI) — depth otherwise inferred from sampling `internal/github/client_test.go`, `internal/dashboard/frontend_apps_handler_test.go`, `internal/dashboard/frontend_apps_store_test.go`. Frontend: no unit-test framework exists for React components anywhere in the repo, and no Playwright spec exists yet for `FrontendAppsPage.tsx` (checked `internal/dashboard/ui/e2e/*.spec.ts`) — floor for this specific page is zero, so its Coverage Expectation stays at the repo's existing depth rather than introducing new test infrastructure as a side effect of this feature.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------- | --------------------- | ------------------ | ------------ |
| `zipimport` validation (domain logic) | unit | All branches; 1:1 to ACs ZIP-04/05/06/08/09; every listed edge case (empty zip, zero-file zip, symlink/traversal entries) | `internal/zipimport/*_test.go` | `go test ./internal/zipimport/...` |
| `github.Client` additions (`CreateEmptyRepo`, `CommitInitialTree`) | unit | All branches: success + 422/403/429/rate-limit paths, matching the depth `client_test.go` already applies to `CreateRepoFromTemplate` | `internal/github/client_test.go` | `go test ./internal/github/...` |
| `FrontendAppsHandler` (`CreateFromZip`, `importZipAsync`) | integration | Happy path + every listed edge/error case: oversized body, invalid multipart, slug conflict, GitHub not connected, content-validation failure, repo-creation failure, commit failure, deploy failure | `internal/dashboard/frontend_apps_handler_test.go` | `go test ./internal/dashboard/...` |
| `frontend_apps_store.go` (widened `FrontendAppInput`, nullable `template_id`) | integration | Key insert/read paths for `source='zip'` rows + confirms `source='template'` rows are unaffected, matching existing `frontend_apps_store_test.go` depth | `internal/dashboard/frontend_apps_store_test.go` | `go test ./internal/dashboard/...` |
| `provisioner.go` migration (schema only) | none | build gate only | - | `go build ./...` |
| `FrontendAppsPage.tsx` (Tabs split, ZIP form, polling) | none | build gate only — matches this page's existing depth (no component unit tests, no e2e spec for this page in the repo today) | - | `cd internal/dashboard/ui && npx tsc -b && npm run build` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick | After a task touching only `zipimport` or `github.Client` | `go test ./internal/zipimport/... ./internal/github/...` |
| Full | After a task touching `internal/dashboard/*` (handler/store) | `go test ./... && go vet ./...` |
| Build | After phase completion, schema-only tasks, or frontend-only tasks | `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && cd internal/dashboard/ui && npx tsc -b && npm run build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation — schema, validation, widened store

```
T1 → T3
```

(T2 has no dependents within this phase; it feeds T7 in Phase 3.)

### Phase 2: GitHub client — write path

```
T4 → T5
```

### Phase 3: Backend orchestration — handler, async import, management parity

```
T3 → T6
T6 → T7
T2 → T7
T5 → T7
T7 → T8
```

### Phase 4: Frontend UI

```
T6 → T9
T9 → T10
```

### Phase 5: Docs

```
T7 → T11
T8 → T11
T9 → T11
T10 → T11
```

---

## Task Breakdown

### T1: Migrate `frontend_apps` for ZIP-sourced rows

**What**: Add the additive migration statements to `provisioner.go`'s statement list — `ALTER COLUMN template_id DROP NOT NULL`, `ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'template'`, and the `frontend_apps_source_template_id_check` CHECK constraint from the design.
**Where**: `internal/dashboard/provisioner.go`
**Depends on**: None
**Reuses**: The existing idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` pattern already used in the same statement list (e.g. the `backend_app_id`/`deploy_service_id` block).
**Requirement**: ZIP-01

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Migration statements added, idempotent (safe to re-run `ProvisionZeepSystem`)
- [ ] Existing `source='template'` default satisfies the CHECK constraint for pre-existing rows with no backfill needed
- [ ] Gate check passes: `go build ./... && go vet ./...`

**Tests**: none
**Gate**: build

---

### T2: `zipimport` package — validate ZIP safety and structure

**What**: New package with `Validate(data []byte) (Result, error)`, `FileEntry{Path, Content}`, enforcing size/count thresholds, path-safety (traversal/absolute/symlink rejection), single-top-level-directory stripping, and `package.json`-at-root presence.
**Where**: `internal/zipimport/validate.go`
**Depends on**: None
**Reuses**: stdlib `archive/zip` only — no existing project code to build on (net-new validation domain per design).
**Requirement**: ZIP-04, ZIP-05, ZIP-06, ZIP-08, ZIP-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Rejects >500MB uncompressed total or >5000 entries with a distinct, typed error (ZIP-08)
- [ ] Rejects any entry with an absolute path, a `..` segment, or a symlink mode bit, with a distinct typed error (ZIP-09)
- [ ] Strips a single common top-level directory when every entry shares one (ZIP-04)
- [ ] Rejects when no `package.json` exists at the normalized root, with a distinct typed error (ZIP-05)
- [ ] Rejects a corrupt/non-ZIP input and a zero-entry ZIP, each with a distinct typed error (ZIP-06 + edge case)
- [ ] Accepts a valid small ZIP and returns the expected flattened `[]FileEntry`
- [ ] Gate check passes: `go test ./internal/zipimport/...`
- [ ] Test count: 8+ tests pass (one per bullet above at minimum; no silent deletions)

**Tests**: unit
**Gate**: quick

---

### T3: Widen `FrontendAppInput` and `CreateFrontendApp` for ZIP-sourced rows

**What**: Change `FrontendAppInput.TemplateID` to `*uuid.UUID`, add `Source string`, update `CreateFrontendApp` to insert `source`/nullable `template_id`; update the two existing callers (`Create`, `Retry` in `frontend_apps.go`) to pass `&templateID` and `Source: "template"` so their behavior is unchanged.
**Where**: `internal/dashboard/frontend_apps_store.go` (primary; `internal/dashboard/frontend_apps.go` gets a 2-line mechanical caller update in the same commit — same struct, no new logic)
**Depends on**: T1
**Reuses**: Existing `CreateFrontendApp`/`FrontendAppInput` shape and `frontend_apps_store_test.go` patterns (`TestCreateFrontendApp`, `TestCreateFrontendAppWithFailedStatus`).
**Requirement**: ZIP-01

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `Create` and `Retry` still compile and pass their existing tests unmodified in behavior (template path unaffected)
- [ ] A new test inserts a `source='zip'`, `template_id=nil` row successfully
- [ ] A new test confirms inserting `source='zip'` with a non-nil `template_id` (or `source='template'` with nil `template_id`) violates the CHECK constraint
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: existing store tests (15) + 2 new tests pass, none deleted

**Tests**: integration
**Gate**: full

---

### T4: `github.Client.CreateEmptyRepo`

**What**: New method `CreateEmptyRepo(ctx, owner, slug string) (htmlURL string, err error)` — `POST /orgs/{owner}/repos` with `{name, private: true, auto_init: false}`, mirroring `CreateRepoFromTemplate`'s 422/403/429 handling.
**Where**: `internal/github/client.go`
**Depends on**: None
**Reuses**: `doAuthenticated`, `rateLimitErrorFromResponse`, `unexpectedStatusError`, and the `newTestClient` mock-transport test helper already in `client_test.go`.
**Requirement**: ZIP-01, ZIP-12

**Tools**:

- MCP: `mcp__claude_ai_Github_MCP` — investigative use only, to inspect/document the GitHub App's currently granted permission set (org vs repo-level "Administration") before writing the 403-handling branch. Does not replace verifying against the real production installation.
- Skill: NONE

**Done when**:

- [ ] 201 response parses `html_url` and returns it
- [ ] 422 (name conflict/invalid) returns a clear wrapped error
- [ ] 403 returns a clear wrapped error naming the likely missing permission (mirrors the existing `CreateRepoFromTemplate` 403 message style)
- [ ] 429 / rate-limited response surfaces as `*RateLimitError` via `rateLimitErrorFromResponse`, matching existing behavior
- [ ] A code comment flags the unresolved design risk: `POST /orgs/{owner}/repos` may need an org-level "Administration" permission distinct from the repo-level one already granted for `generate` — flagging that this must be confirmed against a real installation before production rollout (cannot be verified against a live GitHub App in this environment)
- [ ] Gate check passes: `go test ./internal/github/...`
- [ ] Test count: existing `client_test.go` tests + 4 new tests pass, none deleted

**Tests**: unit
**Gate**: quick

---

### T5: `github.Client.CommitInitialTree`

**What**: New method `CommitInitialTree(ctx, owner, repo string, files []zipimport.FileEntry, message string) (commitSHA string, err error)` — creates one blob per file via the Blobs API with `encoding: "base64"`, builds the tree, creates a parentless commit, then creates `refs/heads/main` pointing at it.
**Where**: `internal/github/client.go`
**Depends on**: T4
**Reuses**: Same `doAuthenticated`/error-handling helpers as T4; consumes `zipimport.FileEntry` from T2.
**Requirement**: ZIP-01, ZIP-03, ZIP-04, ZIP-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Multiple files (including a binary-like byte payload) are each blobbed with `encoding: "base64"`, never via Trees API inline `content`
- [ ] Tree, commit, and ref-creation requests are issued in the correct order with the correct bodies (asserted via the mock transport's captured requests)
- [ ] A mid-sequence failure (e.g. blob creation fails on file 3 of 5) stops the sequence and returns a wrapped error without creating the tree/commit/ref
- [ ] A rate-limited response mid-sequence surfaces as `*RateLimitError`
- [ ] Gate check passes: `go test ./internal/github/...`
- [ ] Test count: T4's test count + 4 new tests pass, none deleted

**Tests**: unit
**Gate**: quick

---

### T6: `FrontendAppsHandler.CreateFromZip` — synchronous request-shape validation

**What**: New handler `POST /dashboard/api/frontend-apps/zip-import` — `http.MaxBytesReader` at 100MB, `ParseMultipartForm`, name/slug validation + `SlugExists` check, `GetGitHubConfig` check, `CreateFrontendApp(source="zip", status="processing")`, respond `202` with the created record, then launch `importZipAsync` in a goroutine (implemented fully in T7 — this task stubs it as a no-op or minimal call so T6 is independently testable for its synchronous half). Route wired in `internal/server/server.go`.
**Where**: `internal/dashboard/frontend_apps.go` (also registers the route in `internal/server/server.go`)
**Depends on**: T3
**Reuses**: `slugify`, `SlugExists`, `GetGitHubConfig`, `CreateFrontendApp`, the existing `audit_log` write pattern, and `frontend_apps_handler_test.go`'s existing request-building helpers.
**Requirement**: ZIP-01, ZIP-02, ZIP-06, ZIP-07, ZIP-10, ZIP-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid multipart request with name + zip file returns 202 with `status: "processing"`, `source: "zip"`, `template_id: null`
- [ ] Body over 100MB is rejected before any row is created (ZIP-07)
- [ ] Malformed multipart is rejected with 400 before any row is created (ZIP-06's request-shape half)
- [ ] Slug collision returns 409 before any row is created, matching the template path (ZIP-10)
- [ ] No GitHub App connected returns the same error the template path returns (ZIP-11)
- [ ] Route registered in `server.go` alongside the existing `frontend-apps` routes
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: existing `frontend_apps_handler_test.go` tests + 5 new tests pass, none deleted

**Tests**: integration
**Gate**: full

---

### T7: `importZipAsync` — background orchestration

**What**: Implement the goroutine body wired from T6: `zipimport.Validate` → `github.Client.CreateEmptyRepo` → `github.Client.CommitInitialTree` → `UpdateFrontendAppStatus` → `setupSync` → `attemptDeploy` (called with a synthetic in-memory `*GitHubTemplate` carrying the fixed static-site preset: `RenderServiceType: "static_site"`, `BuildCommand: "npm install && npm run build"`, `PublishPath: "dist"`, `StartCommand: ""`), updating `frontend_apps` at each stage boundary per the design's error-handling table.
**Where**: `internal/dashboard/frontend_apps.go`
**Depends on**: T2, T5, T6
**Reuses**: `setupSync`, `attemptDeploy`, `UpdateFrontendAppStatus`, `UpdateFrontendAppDeploy` — called exactly as the template path already calls them.
**Requirement**: ZIP-01, ZIP-03, ZIP-04, ZIP-05, ZIP-08, ZIP-09, ZIP-12, ZIP-13, ZIP-14

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Content-validation failure (bad zip / bomb thresholds / path-unsafe / missing `package.json`) ends in `status: "failed"` with the specific error message, no GitHub repo created (ZIP-05, ZIP-08, ZIP-09)
- [ ] `CreateEmptyRepo` failure ends in `status: "failed"` with the underlying error (ZIP-12's repo-creation half)
- [ ] `CommitInitialTree` failure ends in `status: "failed"` with the underlying error, repo left in place (ZIP-12)
- [ ] Full success path ends in `status: "ready"`, `github_repo_url` populated, `setupSync` invoked, deploy attempted with the fixed static-site preset (ZIP-01, ZIP-03, ZIP-14)
- [ ] Render deploy failure after a successful commit still leaves `status: "ready"` with `deploy_status: "failed"` (ZIP-13)
- [ ] Tests use mocked `github.Client`/deploy provider (no real network calls) and poll/await the goroutine's completion deterministically (e.g. a completion channel or bounded retry-poll on the DB row) rather than a fixed `time.Sleep`
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: T6's test count + 5 new tests pass, none deleted

**Tests**: integration
**Gate**: full

**Commit**: `feat(dashboard): async ZIP-to-repo import for frontend apps`

---

### T8: Management-action parity for ZIP-sourced apps

**What**: Confirm (and add regression tests proving) that `ListFrontendApps`, `FrontendAppsHandler.Delete`, and the slug-uniqueness constraint behave identically for `source='zip'` rows as they already do for `source='template'` rows — no production code change expected unless a gap is found.
**Where**: `internal/dashboard/frontend_apps_store.go` (test-only task: adds regression tests, no new production code expected)
**Depends on**: T7
**Reuses**: Existing `TestListFrontendApps`, `TestArchiveFrontendApp`, `TestSlugUniqueIndexEnforced` as the template for the new assertions.
**Requirement**: ZIP-15, ZIP-16, ZIP-17

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] A test confirms a `source='zip'` row appears in `ListFrontendApps` output in the same shape as a `source='template'` row (ZIP-15)
- [ ] A test confirms deleting a `source='zip'` app (in `processing` and in `failed` status) runs the same soft-delete + best-effort repo-archive + deploy-key-revoke + Render-delete sequence as a `source='template'` app (ZIP-16)
- [ ] A test confirms the slug-uniqueness constraint rejects a second `source='zip'` import while an earlier one with the same slug is still `processing` (ZIP-17)
- [ ] Any gap found is fixed in the same task (kept atomic — this task's purpose is to make the parity claim true, not just check it)
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: T7's test count + 3 new tests pass, none deleted

**Tests**: integration
**Gate**: full

---

### T9: "Import ZIP" tab in the creation dialog

**What**: Split the existing single-form `Dialog` into `Tabs` ("From template" / "Import ZIP"). The new tab shows the fixed static-site preset (build command, publish path) before file selection, takes a name `Input` + `.zip` file `Input`, and submits `FormData` to `POST /dashboard/api/frontend-apps/zip-import`. `StatusPill` gains a `processing` state.
**Where**: `internal/dashboard/ui/src/pages/FrontendAppsPage.tsx` (new strings added to `en.json`/`pt-BR.json` in the same commit)
**Depends on**: T6
**Reuses**: `Dialog`, `Input`, `Label`, `Button`, `Icon`, `StatusPill` from `@/components/ui/*` / `@/components/patterns`; adds `Tabs` (already a project dependency per `package.json`).
**Requirement**: ZIP-01, ZIP-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Both tabs render inside the same `Dialog`, existing "From template" tab behavior is unchanged
- [ ] "Import ZIP" tab shows the fixed build command / publish path text before a file is chosen
- [ ] Submitting a name + `.zip` file calls the new endpoint and closes the dialog on success, showing the new row at `processing`
- [ ] Every new user-facing string is added to both `en.json` and `pt-BR.json`
- [ ] Mutation error surfaces via `toast.error(error.message)` (existing project convention)
- [ ] `StatusPill` renders a distinct visual state for `processing`
- [ ] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none
**Gate**: build

---

### T10: Poll while a row is `processing`

**What**: While any visible frontend app row has `status: "processing"`, the page polls the list/get endpoint on a fixed interval until it leaves that state.
**Where**: `internal/dashboard/ui/src/pages/FrontendAppsPage.tsx`
**Depends on**: T9
**Reuses**: Existing data-fetching hook for the frontend apps list (`fetchTemplates`/list-loading pattern already in the file).
**Requirement**: ZIP-18

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Polling starts automatically when a `processing` row is visible and stops once no row is `processing`
- [ ] No polling interval is left running after the component unmounts (cleanup on unmount)
- [ ] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none
**Gate**: build

---

### T11: CHANGELOG entry

**What**: Add a `## [Unreleased]` entry documenting the new ZIP-import creation path, per `AGENTS.md` §6 (CHANGELOG updated in the same change that ships the feature, not deferred to release day).
**Where**: `CHANGELOG.md`
**Depends on**: T7, T8, T9, T10
**Reuses**: Existing `[Unreleased]` section format.
**Requirement**: — (documentation obligation, not a spec AC)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Entry added under `## [Unreleased]` describing the new "Import ZIP" creation path
- [ ] Gate check passes: `go build ./... && go vet ./... && cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none
**Gate**: build

**Commit**: `docs(changelog): note frontend app ZIP import`

---

## Phase Execution Map

Phases run in order: Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5. Within each phase, the arrows are exactly the ones shown in that phase's diagram above:

- **Phase 1**: `T1 → T3` (T2 has no in-phase dependents; it feeds T7 in Phase 3)
- **Phase 2**: `T4 → T5`
- **Phase 3**: `T3 → T6`, `T6 → T7`, `T2 → T7`, `T5 → T7`, `T7 → T8`
- **Phase 4**: `T6 → T9`, `T9 → T10`
- **Phase 5**: `T7 → T11`, `T8 → T11`, `T9 → T11`, `T10 → T11`

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Migrate `frontend_apps` schema | 1 file, 1 concern (migration statements) | ✅ Granular |
| T2: `zipimport` validation package | 1 package, 1 function + its types | ✅ Granular |
| T3: Widen `FrontendAppInput`/`CreateFrontendApp` | 1 struct + 1 store function + 2 known caller updates | ✅ Granular (caller updates are mechanical, same change) |
| T4: `CreateEmptyRepo` | 1 method | ✅ Granular |
| T5: `CommitInitialTree` | 1 method | ✅ Granular |
| T6: `CreateFromZip` handler (sync half) | 1 handler + 1 route registration | ✅ Granular |
| T7: `importZipAsync` orchestration | 1 function (goroutine body) | ✅ Granular |
| T8: Management-action parity tests | 3 existing handlers, tests only (no new component) | ✅ Granular (verification task, not a new component) |
| T9: "Import ZIP" tab | 1 component's new UI section + 2 locale files | ✅ Granular |
| T10: Processing-status polling | 1 behavior (polling effect) in 1 file | ✅ Granular |
| T11: CHANGELOG entry | 1 file | ✅ Granular |

**Granularity check**: all tasks are single component / single function / single file-concern. No task requires splitting.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ------------------------ | --------------- | ------ |
| T1 | None | no incoming arrow | ✅ Match |
| T2 | None | no incoming arrow | ✅ Match |
| T3 | T1 | T1 → T3 | ✅ Match |
| T4 | None | no incoming arrow | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T3 | T3 → T6 | ✅ Match |
| T7 | T2, T5, T6 | T2 → T7, T5 → T7, T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T6 | T6 → T9 | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T7, T8, T9, T10 | T7 → T11, T8 → T11, T9 → T11, T10 → T11 | ✅ Match |

**Rules check**: every `Depends on` points backward (earlier phase) or within the same phase (earlier task) — no forward-phase dependency exists.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ----------------------------- | ------------------ | ----------- | ------ |
| T1: Migrate schema | Entity/config (schema) | none | none | ✅ OK |
| T2: `zipimport` validation | Domain/business-logic | unit | unit | ✅ OK |
| T3: Widen store input | Repository/data-access | integration | integration | ✅ OK |
| T4: `CreateEmptyRepo` | `github.Client` (unit-tested layer) | unit | unit | ✅ OK |
| T5: `CommitInitialTree` | `github.Client` (unit-tested layer) | unit | unit | ✅ OK |
| T6: `CreateFromZip` handler | Route/controller | integration | integration | ✅ OK |
| T7: `importZipAsync` | Route/controller (async orchestration, same layer as T6) | integration | integration | ✅ OK |
| T8: Parity tests | Route/controller + repository (existing, being verified) | integration | integration | ✅ OK |
| T9: "Import ZIP" tab | UI component | none (repo's existing depth for this page) | none | ✅ OK |
| T10: Polling | UI component | none | none | ✅ OK |
| T11: CHANGELOG | Documentation | none | none | ✅ OK |

No violations. No task defers its required tests to a later task.

---

## Task Verification Standards

Every task's `Done when` items are specific and binary pass/fail, each referencing the exact gate command from **Gate Check Commands** above, with an expected test-count delta to catch silent test deletions.
