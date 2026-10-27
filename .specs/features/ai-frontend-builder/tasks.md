# AI Frontend Builder Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. If the skill cannot be activated, STOP and tell the user.

Commits only when the user asks (AGENTS.md §2). Tasks touching `docker-compose.yml`, `easypanel.json`, `k8s/`, `charts/` (T38–T40) are higher-risk (AGENTS.md §8): explain the diff and get explicit confirmation before applying.

---

**Spec**: `.specs/features/ai-frontend-builder/spec.md`
**Design**: `.specs/features/ai-frontend-builder/design.md`
**Status**: Approved (2026-10-26) — execution on branch `feat/ai-frontend-builder`. Phase 0 (T01–T10) implemented, gated and pushed 2026-10-26. Pending: T02 manual real-endpoint run (needs OpenAI + Gemini keys from the owner). Phase 1 (T11–T20) implemented, gated and pushed 2026-10-27. Backend Phases 2–3 (T21–T32, incl. snapshot `part` column) implemented, gated and pushed 2026-10-27. Phase 4 UI (T33–T37) implemented 2026-10-27 and gated (`tsc -b` + `npm run build` clean, en/pt-BR parity); uncommitted until the owner confirms; not yet clicked through in a browser. Added `user_message` to the turn API (the chat could not be rebuilt after a reload without it). Open [Incerto]: live GitHub empty-repo commit (Contents-API placeholder fallback), `orbit_preview` cookie in a sandboxed iframe (needs a real browser). Phase 5+ (deployment, eval) not started; T38–T40 need explicit confirmation.

---

## Test Coverage Matrix

> Derived from `AGENTS.md` §3 and the conventions recorded in `.specs/features/ai-build-chat/tasks.md` (Go tests are integration-style against a real Postgres via `TEST_DATABASE_URL`; no DB mocking; no frontend test framework). Confirm before Execute.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command | Parallel-Safe |
| --- | --- | --- | --- | --- | --- |
| Pure Go packages without DB (`internal/agent/provider`, `internal/agent/tools`, `internal/agent` loop with fakes, `internal/builder`, `cmd/zeep-registry-proxy`, `internal/github` client) | unit | Every branch tied to a spec AC/edge case; httptest for HTTP; temp dirs for filesystem/git | `*_test.go` next to code | `go test ./<pkg>/...` | Yes |
| Postgres-backed stores (`internal/agent/store`, dashboard stores) | integration | Query paths, constraints (one active turn), lease semantics, scoping | `*_store_test.go` | `go test -p 1 -parallel 4 ./...` | No (shared `TEST_DATABASE_URL`) |
| Dashboard HTTP handlers | integration | Happy path + every RBAC/edge case in the spec for that route | `internal/dashboard/builder_*_test.go` | `go test -p 1 -parallel 4 ./...` | No |
| DDL in `provisioner.go` | none | build gate (exercised by integration tests of later tasks) | `internal/dashboard/provisioner.go` | build gate | — |
| Frontend (`internal/dashboard/ui`) | none (no framework in repo) | `tsc -b` + production build + i18n JSON validation | `src/**` | frontend build gate | — |
| Deployment manifests | none | `docker compose config`, `helm lint`, `helm template`, `kubectl kustomize` | `charts/`, `k8s/`, compose | manifest gate | — |
| Eval suite | eval (opt-in, real LLM) | Runs, produces report | `internal/agent/eval` | `go test -tags eval ./internal/agent/eval/...` | — |

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && go test -p 1 -parallel 4 ./...` |
| Build | End of phase, DDL-only, frontend | Backend: Full + `gofmt -l <changed files>` (empty). Frontend: `cd internal/dashboard/ui && npx tsc -b && npm run build && python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json'))"` |
| Manifest | Deployment tasks | `docker compose config -q`, `helm lint charts/zeep-orbit`, `helm template charts/zeep-orbit --set builder.enabled=true`, `kubectl kustomize k8s` |

---

## Execution Plan

### Phase 0: Builder runtime + provider (de-risk first)

```
T01 [P] ──→ T02
T03 [P] ──┬→ T04 ──┐
          └→ T05 ──┤
T06 [P] ───────────┼→ T07 ──┐
T08 [P] ───────────┼────────┼→ T10
T09 [P] ───────────┘        │
                    T07 ────┘
```

(T10 depends on T07, T08, T09.)

### Phase 1: Agent core

```
T11 ──┬→ T12 ──┐
      └→ T13 ──┤
T06, T07 → T14 ──→ T15 ──→ T16
T17 [P]            │
T01, T12, T13, T15, T17 ──→ T18 ──→ T19
T12, T14, T18 ──→ T20
```

### Phase 2: Dashboard API

```
T11 → T21 [*]   T11 → T22   T11 → T23
T14, T22 → T24
T12, T14, T22, T23 → T25
T20, T25 → T26
T16, T20, T25 → T27
T20, T25 → T28
T06, T25 → T29
T21..T29 → T30
```

`[*]` T21–T23 are independent in code but share the integration DB → run sequentially.

### Phase 3: Publish

```
T31 [P] (can start any time)
T25, T31 → T32
```

### Phase 4: Dashboard UI

```
T30, T32 → T33 ──┬→ T34
                 ├→ T35
                 └→ T36 ──→ T37
```

### Phase 5: Deployment + docs

```
T10, T30 → T38 → T39 → T40
T38, T39 → T41
```

### Phase 6: Eval + validation

```
T10, T18 → T42
T02, T42 → T43
T32, T37, T38, T39, T40 → T44
```

---

## Task Breakdown

### Phase 0

### T01: Provider interface + OpenAI/OpenAI-compatible adapter [P]

**What**: `Provider` interface, request/response/usage types, typed errors, and the `openai` adapter with configurable base URL, multi-tool-call parsing, `usage` parsing and bounded retry on 429/5xx.
**Where**: `internal/agent/provider/provider.go`, `internal/agent/provider/openai.go`
**Depends on**: None
**Reuses**: request/parse patterns of `internal/dashboard/ai/client.go` (copy, don't import)
**Requirement**: AFB-05, AFB-14 (usage)

**Done when**:
- [ ] Base URL configurable, default `https://api.openai.com/v1`
- [ ] Parallel tool calls returned in order; usage populated
- [ ] 401/403 → `ErrAuth`, 429 → `ErrRateLimited` (honors `Retry-After`), context-length error → `ErrContextTooLong`, others → `ErrProvider`; raw body never in `Error()` beyond status
- [ ] ≥8 httptest unit tests (plain msg, 1 tool call, 2 tool calls, usage, 401, 429 retry then success, 429 exhausted, malformed JSON)
- [ ] Gate: Quick (`./internal/agent/provider/...`)

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add provider-neutral LLM client for the frontend builder`

---

### T02: Provider contract test table

**What**: Shared contract table run against the fake in CI and against real endpoints with `-tags providercontract`.
**Where**: `internal/agent/provider/contract_test.go`, `internal/agent/provider/contract_real_test.go` (`//go:build providercontract`)
**Depends on**: T01
**Requirement**: AFB-05 (spec "Resolved": contract tests before trusting Gemini)

**Done when**:
- [ ] Cases: plain message, single tool call, two parallel tool calls, tool-result round trip, usage present
- [ ] Real run documented (`CONTRACT_PROVIDERS="openai:<model>,openai_compatible:<model>@<url>"`, keys via env)
- [ ] **Manual**: real run executed against OpenAI and Gemini compat; result (pass/fail per case + model ids) recorded in `design.md` Risks table
- [ ] Gate: Quick

**Tests**: unit (+ manual real run) · **Gate**: quick · **Commit**: `test: add provider contract suite`

---

### T03: Builder workspace — path safety + file ops + idempotency log [P]

**What**: Session root management, path resolution (`Clean` + `EvalSymlinks`, reject outside root / `.git` / `node_modules` / `dist`), `list_dir`, `read_file` (offset/limit), `write_file`, `edit_file` (exact match, `replace_all`, "not found"/"not unique" errors), text-only + size caps, `ops.jsonl` idempotency by `op_id`.
**Where**: `internal/builder/workspace.go`
**Depends on**: None
**Requirement**: AFB-15, AFB-18 (idempotency), edge case "writes outside project"

**Done when**:
- [ ] ≥12 unit tests incl. `../` traversal, symlink escape, `.git` write, oversized file, binary content, edit not-found, edit ambiguous, repeated `op_id` returns stored result without re-applying
- [ ] Gate: Quick (`./internal/builder/...`)

**Tests**: unit · **Gate**: quick · **Skill**: `golang-security` · **Commit**: `feat: add builder workspace file operations`

---

### T04: Builder exec — fixed build steps

**What**: Map `install|typecheck|lint|build` to fixed argv (no shell), minimal env allowlist, per-step timeout and resource limits, output tail capture, `--base <preview_base>` for build, `--ignore-scripts` on install.
**Where**: `internal/builder/exec.go`
**Depends on**: T03
**Requirement**: AFB-07, AFB-15

**Done when**:
- [ ] Executable path injectable for tests (fake script), so tests don't need Node
- [ ] Tests: argv per step, env does not leak a sentinel parent env var, timeout kills process, output truncated with marker, non-zero exit propagated
- [ ] ≥6 unit tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Skill**: `golang-security` · **Commit**: `feat: add builder fixed build steps`

---

### T05: Builder git — init, commit+bundle, restore, revert, archive

**What**: Init repo from template tar (commit `turn 0`, full bundle), commit turn → incremental bundle `prev..HEAD`, restore from ordered bundles, revert = new commit with tree of target sha, archive tree as tar. `.gitignore` enforces `node_modules`/`dist`.
**Where**: `internal/builder/git.go`
**Depends on**: T03
**Requirement**: AFB-09, AFB-10 (revert), AFB-19

**Done when**:
- [ ] Tests (temp dirs, real `git` binary): init→commit×2→restore into empty dir yields same HEAD; revert keeps history and matches target tree; archive excludes ignored dirs; bundle of a no-change turn handled
- [ ] ≥6 unit tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add builder git snapshots`

---

### T06: Builder request token sign/verify [P]

**What**: HMAC-SHA256 token `{sid, m, p, exp}` (60s), and preview token `{sid, uid, exp}` (10 min), `hmac.Equal` verification.
**Where**: `internal/builder/auth.go`
**Depends on**: None
**Reuses**: format of `signState`/`verifyState` (`internal/auth/google.go:246-264`)
**Requirement**: AFB-17, AFB-08

**Done when**:
- [ ] Tests: valid, expired, wrong secret, wrong session, wrong method/path, tampered payload, empty secret refused at construction
- [ ] ≥7 unit tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Skill**: `golang-security` · **Commit**: `feat: add builder request signing`

---

### T07: Builder HTTP server + `cmd/zeep-builder`

**What**: HTTP API from design §5 over T03–T06, capacity limit (`BUILDER_MAX_ACTIVE_SESSIONS` → 429), `/v1/health`, preview static serving of last successful build with CSP `sandbox` header, env config.
**Where**: `internal/builder/server.go`, `cmd/zeep-builder/main.go`
**Depends on**: T03, T04, T05, T06
**Requirement**: AFB-03, AFB-08, AFB-15, AFB-17

**Done when**:
- [ ] httptest tests: 401 without token; init→ops→commit happy path; unknown session → 404 code `unknown_session`; capacity 429; preview returns CSP sandbox header and 404 before first build
- [ ] ≥8 tests · Gate: Quick

**Tests**: unit (httptest, no DB) · **Gate**: quick · **Commit**: `feat: add zeep-builder service`

---

### T08: Builder image + default template v0 [P]

**What**: `Dockerfile.builder` (Node LTS, git, non-root uid 1000, `/work` + `/tmp` writable); `builder/templates/default/` (React + Vite + TS, `@zeeptech/orbit-client` pinned, ESLint config, `ORBIT_AGENT.md`, router using `import.meta.env.BASE_URL`, `src/orbit.config.ts` placeholder, SDK token storage with memory fallback when `localStorage` throws, `TEMPLATE_VERSION`).
**Where**: `Dockerfile.builder`, `builder/templates/default/**`
**Depends on**: None
**Requirement**: AFB-04, AFB-26 (template mechanism)

**Done when**:
- [ ] `docker build -f Dockerfile.builder .` succeeds
- [ ] Inside the image: `npm ci && npx tsc -b && npm run lint && npx vite build --base /x/` succeed on the template
- [ ] Build command / publish path for Render recorded in the template (consumed by T32)

**Tests**: none (build verification) · **Gate**: build (docker) · **Commit**: `feat: add builder image and default template`

---

### T09: Registry proxy [P]

**What**: `cmd/zeep-registry-proxy`: reverse proxy, GET/HEAD only, upstream allowlist with scopes, optional token injection per upstream, no token in logs.
**Where**: `cmd/zeep-registry-proxy/main.go`, `cmd/zeep-registry-proxy/proxy.go`
**Depends on**: None
**Requirement**: AFB-16, AFB-27

**Done when**:
- [ ] Tests: POST/PUT rejected 405; unknown upstream host rejected; token added only for its upstream; token absent from logged request line
- [ ] ≥5 unit tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Skill**: `golang-security` · **Commit**: `feat: add npm registry proxy for the builder`

---

### T10: Local builder stack + npm-through-proxy spike

**What**: `deploy/builder.compose.yml` (builder + registry-proxy, `builder_internal` internal network, egress network) for local dev/eval; prove `npm ci` of the default template through the proxy, and a scoped package from a second registry via `replace-registry-host`.
**Where**: `deploy/builder.compose.yml`, findings in `design.md` Risks table
**Depends on**: T07, T08, T09
**Requirement**: AFB-16, AFB-27

**Done when**:
- [ ] From inside builder: `curl https://example.com` fails; install through proxy succeeds
- [ ] Scoped-registry behavior verified **or** documented as not working with the alternative chosen
- [ ] `docker compose -f deploy/builder.compose.yml config -q` passes

**Tests**: none (manual spike) · **Gate**: manifest · **Commit**: `chore: add local builder stack`

---

### Phase 1

### T11: zeep_system DDL for the builder

**What**: All statements from design "Data Models" appended to `stmts` in the correct FK order.
**Where**: `internal/dashboard/provisioner.go`
**Depends on**: None
**Requirement**: AFB-01, 02, 06, 09, 14, 18, 25, 26

**Done when**:
- [ ] Idempotent (`IF NOT EXISTS`), after referenced tables; boot twice against the same DB succeeds
- [ ] Gate: Build

**Tests**: none · **Gate**: build · **Skill**: `orbit-internals` · **Commit**: `feat: add builder tables to zeep_system`

---

### T12: Builder store (sessions, turns, messages, events, snapshots, lease)

**What**: `internal/agent/store` on `db.Pool`: session CRUD (owner-scoped), create turn (one-active constraint → `ErrTurnActive`), append message/event, list events after cursor, claim turn (`SKIP LOCKED`, lease), renew lease, lease-checked writes (`ErrLeaseLost`), snapshot save/list.
**Where**: `internal/agent/store/store.go`
**Depends on**: T11
**Requirement**: AFB-06, AFB-09, AFB-18

**Done when**:
- [ ] Integration tests: one-active-turn violation; two concurrent claims → one wins; expired lease reclaimable; write with stale lease rejected; events cursor ordering; owner scoping
- [ ] ≥10 tests · Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `golang-testing` · **Commit**: `feat: add builder session store`

---

### T13: Usage ledger + budget checker

**What**: Record usage per call; `BudgetChecker.Check(ctx, userID, sessionID)` against session cap and user calendar-month cap (cap 0 = sessions blocked).
**Where**: `internal/agent/store/usage.go`
**Depends on**: T11
**Requirement**: AFB-02, AFB-14

**Done when**:
- [ ] Tests: under cap ok; session cap hit; monthly cap hit; previous month not counted; cap 0 blocks
- [ ] ≥5 tests · Gate: Full

**Tests**: integration · **Gate**: full · **Commit**: `feat: add AI usage ledger and budget checks`

---

### T14: Sandbox interface + HTTP client

**What**: `Sandbox` interface (design §4) + `httpSandbox` signing every request; `ErrUnknownSession` mapping; `WithRestore` helper (restore from snapshots on unknown session, retry once).
**Where**: `internal/agent/sandbox/sandbox.go`, `internal/agent/sandbox/http.go`
**Depends on**: T06, T07
**Requirement**: AFB-17, AFB-19

**Done when**:
- [ ] Tests against an in-process T07 server: full op cycle; unknown session triggers restore then succeeds; second unknown → error
- [ ] ≥5 tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add builder sandbox client`

---

### T15: Tool registry + workspace tools

**What**: Registry (specs, dispatch, kinds, result truncation) and the six workspace tools bound to `Sandbox` (`op_id` = tool call id).
**Where**: `internal/agent/tools/registry.go`, `internal/agent/tools/workspace.go`
**Depends on**: T14
**Requirement**: AFB-05, AFB-07

**Done when**:
- [ ] Tests with fake sandbox: unknown tool → tool error; invalid args → tool error; truncation marker; `run_build` failure returns tail as result, not Go error
- [ ] ≥6 tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add builder agent tools`

---

### T16: Orbit tools (scoped reads + proposal)

**What**: `orbit_get_app_schema`, `orbit_list_auth_providers` (fixed to the session's backend app id, user's permissions) and `propose_backend_change` (validates into `ai.EditOperation`, Kind=Proposal). Registered by `internal/dashboard` to avoid an import cycle.
**Where**: `internal/dashboard/builder_tools.go`
**Depends on**: T15
**Reuses**: `buildChatReadToolInvoker` (`ai_build_chat_handlers.go:194`), `ai.EditOperation`
**Requirement**: AFB-11, AFB-12

**Done when**:
- [ ] Tests: schema of linked app returned; any app-id argument ignored (cannot read another app); user without access → tool error; malformed proposal → tool error
- [ ] ≥5 tests · Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `orbit-internals` · **Commit**: `feat: add Orbit tools for the builder agent`

---

### T17: Prompt assembly + core prompt [P]

**What**: `core.md` (embedded) and `BuildSystemPrompt(core, company, templateGuide, packsIndex, sessionCtx)` with section headers, size caps per section, locale instruction.
**Where**: `internal/agent/prompt.go`, `internal/agent/prompts/core.md`
**Depends on**: None
**Requirement**: AFB-25, AFB-04

**Done when**:
- [ ] Tests: order of sections; core section always first and contains the precedence statement; oversized template guide truncated; empty company layer omitted cleanly
- [ ] ≥4 tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add builder prompt assembly`

---

### T18: Agent loop `RunTurn`

**What**: Loop per design §1: budget check → chat → persist assistant + usage → dispatch every tool call (persist call before, result after) → proposal pauses (`awaiting_confirmation`) → limits → commit + snapshot + final event. Resume from DB state.
**Where**: `internal/agent/loop.go`, `internal/agent/limits.go`
**Depends on**: T01, T12, T13, T15, T17
**Requirement**: AFB-05, 06, 07, 09, 12, 14, 18

**Done when**:
- [ ] Tests with fake provider/sandbox and in-memory store implementing the store interface: happy path ends with commit; parallel tool calls all executed; fix-iteration limit → `limit_reached` + commit; step limit; budget exceeded before call (no provider call made); proposal pauses without commit; resume after crash between call-persisted and result-persisted re-sends same `op_id`; provider error → `failed` with generic event code
- [ ] ≥10 tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add builder agent loop`

---

### T19: Context compaction

**What**: Estimate prompt size; above threshold, summarize older turns with one model call, store `summary` message, build prompt from summary + recent turns.
**Where**: `internal/agent/compaction.go`
**Depends on**: T18
**Requirement**: AFB-10

**Done when**:
- [ ] Tests: below threshold untouched; above threshold summary used and recent turns kept verbatim; summary reused on next turn (no second summarize)
- [ ] ≥3 tests · Gate: Quick

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add context compaction to the builder agent`

---

### T20: Turn worker

**What**: Per-replica worker: poll + in-process poke, capacity check via `Health` (→ `queued` + one event), claim, lease renewal goroutine, `BUILDER_WORKER_CONCURRENCY`, graceful stop.
**Where**: `internal/agent/worker.go`
**Depends on**: T12, T14, T18
**Requirement**: AFB-18, edge case "builder at max concurrency"

**Done when**:
- [ ] Integration tests: pending turn executed; builder full → `queued` then runs when capacity frees; two workers never run the same turn; killed worker's turn resumed after lease expiry
- [ ] ≥4 tests · Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `golang-testing` · **Commit**: `feat: add builder turn worker`

---

### Phase 2

### T21: `openai_compatible` provider with base URL

**What**: Accept provider `openai_compatible` and a `base_url` field in `GET/PUT /api/ai-providers/{provider}`. Validation: absolute `http`/`https` URL (http is needed for an Ollama container on the internal network); reject link-local and cloud-metadata addresses (`169.254.0.0/16`, `fd00:ec2::254`); private hosts allowed (superadmin-only setting). Merge-on-absent-key preserved.
**Where**: `internal/dashboard/ai_provider_handlers.go`, `ai_providers_store.go`
**Depends on**: T11
**Requirement**: AFB-01

**Done when**:
- [ ] Tests: save with base_url; GET never returns key; model-only update keeps key and base_url; non-http(s) scheme and metadata address rejected; non-superadmin 403; existing `openai` behavior unchanged (existing tests still pass)
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Commit**: `feat: support OpenAI-compatible AI providers`

---

### T22: Builder settings + persona/rules versions

**What**: Store + `GET/PUT /api/builder/settings`, `GET/PUT /api/builder/prompt`, `GET /api/builder/prompt/versions` (superadmin via `ActionManageIntegrations`), size caps, audit entries.
**Where**: `internal/dashboard/builder_settings.go`, `builder_settings_store.go`
**Depends on**: T11
**Requirement**: AFB-02, AFB-25

**Done when**:
- [ ] Tests: 403 for admin/editor; caps persisted; prompt save creates version with author; oversized rules → 400; audit row written
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Commit**: `feat: add builder settings and versioned prompt`

---

### T23: Builder templates + tarball download

**What**: `PATCH /api/github/templates/{id}/builder` (superadmin), `GET /api/builder/templates` (default + enabled), `github.Client.DownloadTarball`.
**Where**: `internal/dashboard/builder_templates.go`, `internal/github/client.go`
**Depends on**: T11
**Requirement**: AFB-26

**Done when**:
- [ ] Tests: toggle 403 for non-superadmin; list includes default + only enabled; tarball client test with httptest (redirect followed)
- [ ] Gate: Full

**Tests**: integration (+ unit for client) · **Gate**: full · **Commit**: `feat: allow GitHub templates as builder base templates`

---

### T24: Builder status endpoint

**What**: `GET /api/builder/status` → `{available, provider_configured, reason}` from `BUILDER_URL`, settings.enabled, provider row, builder `Health`.
**Where**: `internal/dashboard/builder_handlers.go`
**Depends on**: T14, T22
**Requirement**: AFB-03

**Done when**:
- [ ] Tests: URL unset → unavailable `not_configured`; builder down → `builder_unreachable`; provider missing → `provider_not_configured`; all ok → available
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Commit**: `feat: add builder status endpoint`

---

### T25: Session create / list / get / delete

**What**: Create (≥ viewer on backend app via `GetApp`; template resolution; `Init` with default or tarball; write `orbit.config.ts` with app API base URL; snapshot turn 0; record template/prompt versions, locale), list/get owner-only, delete = archive + `Destroy`.
**Where**: `internal/dashboard/builder_handlers.go`
**Depends on**: T12, T14, T22, T23
**Requirement**: AFB-04, AFB-11 (access), edge case "app deleted / access lost"

**Done when**:
- [ ] Tests (fake sandbox): user without access → 404; viewer can create; other user's session → 404; delete archives; builder unavailable → 503 generic
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `orbit-internals` · **Commit**: `feat: add builder session endpoints`

---

### T26: Messages + events endpoints

**What**: `POST .../messages` (create turn → 202, `409` when a turn is active, poke worker) and `GET .../events?after=` (owner-only, page size cap).
**Where**: `internal/dashboard/builder_handlers.go`
**Depends on**: T20, T25
**Requirement**: AFB-05, AFB-06, edge case "message while turn running"

**Done when**:
- [ ] Tests: 202 + turn pending; second message 409; events after cursor ordered; payloads contain no file contents or provider error text
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Commit**: `feat: add builder chat endpoints`

---

### T27: Proposal confirm / reject

**What**: Confirm: `CanWrite` on backend app → `applyEditOperation(..., "ai_builder")` → tool result appended → turn back to `pending`. Reject: tool result "rejected by user" → `pending`. Repeat confirm is a no-op.
**Where**: `internal/dashboard/builder_proposals.go`
**Depends on**: T16, T20, T25
**Reuses**: `applyEditOperation` (`ai_edit_chat_handlers.go:330`)
**Requirement**: AFB-12

**Done when**:
- [ ] Tests: viewer confirm → 403 and turn stays paused; editor confirm applies DDL and audit `ip_address = 'ai_builder'`; reject → no DDL; validation error surfaced like edit chat; double confirm no-op
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `orbit-internals` · **Commit**: `feat: add builder schema proposal confirmation`

---

### T28: Revert endpoint

**What**: `POST .../revert {turn_seq}` → turn of kind `revert` executed by worker (no LLM call): `Sandbox.Revert` → build → snapshot → `revert` event.
**Where**: `internal/dashboard/builder_handlers.go`, `internal/agent/loop.go` (revert branch)
**Depends on**: T20, T25
**Requirement**: AFB-10

**Done when**:
- [ ] Tests: revert to turn 1 creates new turn + commit whose tree equals turn 1; unknown turn_seq → 400; active turn → 409
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Commit**: `feat: add builder revert`

---

### T29: Preview proxy

**What**: `GET .../preview/*` reverse proxy to builder, accepts preview token (`?pt=`) instead of cookie, sets `X-Frame-Options: SAMEORIGIN` (overriding `SecurityHeaders`) and CSP `sandbox …`; endpoint to mint preview token for the owner.
**Where**: `internal/dashboard/builder_preview.go`
**Depends on**: T06, T25
**Requirement**: AFB-08, AFB-13 (no secrets), design §10

**Done when**:
- [ ] Tests: no token → 401; other user's token → 401; expired → 401; response carries CSP sandbox + SAMEORIGIN; path traversal in `*` rejected
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `golang-security` · **Commit**: `feat: add sandboxed builder preview proxy`

---

### T30: Route registration + worker wiring

**What**: Register all builder routes in `internal/server/server.go` (with `aiChatLimiterMW` on mutations); start worker in `cmd/zeep/main.go` only when `BUILDER_URL` set; graceful shutdown.
**Where**: `internal/server/server.go`, `cmd/zeep/main.go`
**Depends on**: T21, T22, T23, T24, T25, T26, T27, T28, T29
**Requirement**: all P1 API

**Done when**:
- [ ] `BUILDER_URL` empty → routes return `503 builder not configured` (status route still answers) and no worker goroutine
- [ ] Existing server tests pass · Gate: Build

**Tests**: integration (smoke) · **Gate**: build · **Commit**: `feat: wire builder routes and worker`

---

### Phase 3

### T31: GitHub writer — `CreateEmptyRepo` + `CommitTree` [P]

**What**: New client methods per design §11 incl. `ErrRemoteMoved` (ref update with `force:false` and parent check).
**Where**: `internal/github/client.go`
**Depends on**: None
**Requirement**: AFB-22

**Done when**:
- [ ] httptest tests: first commit without parent creates ref; second commit with matching parent; mismatched head → `ErrRemoteMoved`, no ref update; blob failure aborts before ref update
- [ ] ≥5 tests · Gate: Quick (`./internal/github/...`)

**Tests**: unit · **Gate**: quick · **Commit**: `feat: add GitHub commit API to the client`

---

### T32: Publish + ZIP download

**What**: `POST .../publish` (first publish / republish per design §12, preconditions → 409 with reason, `remote_moved` → 409) and `GET .../download.zip` (owner-only, from `Archive(HEAD)`).
**Where**: `internal/dashboard/builder_publish.go`
**Depends on**: T25, T31
**Reuses**: `CreateFrontendApp` store, `setupSync` (`frontend_apps.go:568`), `attemptDeploy` (`:604`)
**Requirement**: AFB-22

**Done when**:
- [ ] Tests (fake GitHub/Render following `frontend_apps_handler_test.go` patterns): first publish creates frontend app linked to backend app + `published_sha`; republish pushes with parent; remote moved → 409 no push; GitHub not configured → 409 before side effects; Render failure → frontend app `failed`, session intact; ZIP contains tree, not `node_modules`
- [ ] Gate: Full

**Tests**: integration · **Gate**: full · **Skill**: `orbit-internals` · **Commit**: `feat: publish builder sessions as frontend apps`

---

### Phase 4

### T33: API types + hooks

**What**: Types and TanStack hooks in `src/lib/api.ts` per design §16; events polling only while running/queued; every mutation has `onError: toast.error`.
**Where**: `internal/dashboard/ui/src/lib/api.ts`
**Depends on**: T30, T32
**Requirement**: AFB-05, 06

**Done when**: [ ] Gate: Build (frontend)

**Tests**: none · **Gate**: build · **Skill**: `vercel-react-best-practices` · **Commit**: `feat: add builder API hooks`

---

### T34: Settings → "AI Builder" tab

**What**: Provider selection (incl. OpenAI-compatible base URL), caps/limits, persona + rules editor with version history, builder template toggles. Superadmin-only render guard.
**Where**: `internal/dashboard/ui/src/pages/settings/AIBuilderSettings.tsx` (+ tab registration), locales
**Depends on**: T33
**Requirement**: AFB-01, 02, 25, 26

**Done when**: [ ] All strings in `en.json` + `pt-BR.json` · [ ] clearing a field sends the key explicitly (AGENTS.md §4) · [ ] Gate: Build (frontend)

**Tests**: none · **Gate**: build · **Skill**: `shadcn`, `ui-ux-pro-max` · **Commit**: `feat: add AI Builder settings UI`

---

### T35: "Create with AI" entry dialog

**What**: Button in Frontend apps section of `AppsPage` (disabled with reason from `useBuilderStatus`), dialog choosing backend app + template, creates session, navigates.
**Where**: `internal/dashboard/ui/src/components/patterns/NewBuilderSessionDialog.tsx`, `AppsPage.tsx`, locales
**Depends on**: T33
**Requirement**: AFB-04, AFB-03

**Done when**: [ ] i18n both locales · [ ] Gate: Build (frontend)

**Tests**: none · **Gate**: build · **Skill**: `shadcn` · **Commit**: `feat: add builder session entry point`

---

### T36: BuilderPage — chat, timeline, proposal card

**What**: `/builder/:sessionId` route; chat (reuses `ChatMarkdown`), activity timeline from events, proposal card with confirm/reject, states for queued / limit / budget / error.
**Where**: `internal/dashboard/ui/src/pages/BuilderPage.tsx`, router, locales
**Depends on**: T33
**Requirement**: AFB-05, 06, 12, 14

**Done when**: [ ] i18n both locales · [ ] Gate: Build (frontend)

**Tests**: none · **Gate**: build · **Skill**: `ui-ux-pro-max`, `web-design-guidelines` · **Commit**: `feat: add builder chat page`

---

### T37: BuilderPage — preview + actions

**What**: Sandboxed iframe (`sandbox="allow-scripts allow-forms allow-popups allow-modals"`, token URL, reload on `build_succeeded`), Revert (per turn), Publish (disabled with reason), Download ZIP.
**Where**: `internal/dashboard/ui/src/pages/BuilderPage.tsx`, locales
**Depends on**: T36
**Requirement**: AFB-08, 10, 22

**Done when**: [ ] iframe has no `allow-same-origin` · [ ] i18n both locales · [ ] Gate: Build (frontend)

**Tests**: none · **Gate**: build · **Skill**: `web-design-guidelines` · **Commit**: `feat: add builder preview and publish actions`

---

### Phase 5 (confirm each diff with the user before applying)

### T38: docker-compose + easypanel

**What**: Services `builder`, `registry-proxy`; networks `default`, `builder_internal` (internal), `egress`; volume `builder_work`; env wiring. Easypanel: same, or documented reduced isolation if internal networks are unsupported.
**Where**: `docker-compose.yml`, `easypanel.json`
**Depends on**: T10, T30
**Requirement**: AFB-16

**Done when**: [ ] Manifest gate · [ ] builder cannot reach `db:5432` (verified) · [ ] user confirmed diff

**Tests**: none · **Gate**: manifest · **Commit**: `feat: add builder services to compose`

---

### T39: Helm chart

**What**: `builder.enabled` (default false), builder Deployment + Service + PVC, registry-proxy Deployment + Service, NetworkPolicy, security contexts, secret keys for `BUILDER_SHARED_SECRET` and proxy tokens.
**Where**: `charts/zeep-orbit/templates/*`, `values.yaml`, `Chart.yaml` untouched (version bump is release-time)
**Depends on**: T38
**Requirement**: AFB-16

**Done when**: [ ] `helm lint` + `helm template` with enabled true/false · [ ] disabled renders zero new objects · [ ] user confirmed diff

**Tests**: none · **Gate**: manifest · **Commit**: `feat: add builder to the Helm chart`

---

### T40: Kustomize manifests

**What**: Optional builder overlay in `k8s/` mirroring T39.
**Where**: `k8s/builder/*`
**Depends on**: T39
**Requirement**: AFB-16

**Done when**: [ ] `kubectl kustomize k8s/builder` renders · [ ] user confirmed diff

**Tests**: none · **Gate**: manifest · **Commit**: `feat: add builder kustomize overlay`

---

### T41: Docs

**What**: README env table (new vars), README feature row + `i18n/README.pt-BR.md`, `README.pt-PT.md`, `README.es.md`; `CHANGELOG.md` `[Unreleased]`; docs page on builder deployment, isolation model and limits.
**Where**: `README.md`, `i18n/*`, `CHANGELOG.md`, `docs/docs/ai-frontend-builder.md`
**Depends on**: T38, T39
**Requirement**: AGENTS.md §6

**Done when**: [ ] all 4 READMEs in sync · [ ] CHANGELOG entry · [ ] docs build if applicable

**Tests**: none · **Gate**: build · **Commit**: `docs: document the AI frontend builder`

---

### Phase 6

### T42: Eval runner + 10 tasks

**What**: `//go:build eval` runner per design §15, 10 task fixtures, JSON + markdown report.
**Where**: `internal/agent/eval/*`
**Depends on**: T10, T18
**Requirement**: AFB-20

**Done when**: [ ] `go test ./...` does not run it · [ ] `go test -tags eval ./internal/agent/eval/...` runs with local builder stack and produces report

**Tests**: eval · **Gate**: quick + eval smoke (1 task) · **Commit**: `test: add builder eval suite`

---

### T43: Eval run + go/no-go

**What**: Run the suite on one OpenAI model and one Gemini model (compat); record report; decision in `.specs/project/STATE.md`.
**Where**: `.specs/features/ai-frontend-builder/eval-report.md`, `.specs/project/STATE.md`
**Depends on**: T02, T42
**Requirement**: AFB-20, Success Criteria

**Done when**: [ ] both models ≥ 8/10 **or** documented failure analysis and decision · [ ] model ids, tokens, cost inputs recorded

**Tests**: eval · **Gate**: — · **Commit**: `docs: record builder eval results`

---

### T44: Cross-orchestrator UAT

**What**: Run every P1 independent test from the spec on compose, swarm and k8s (kind/k3d acceptable), incl. key-isolation and network-isolation checks; record measured RAM/CPU per session → minimum hardware in docs.
**Where**: `.specs/features/ai-frontend-builder/validation.md`, `docs/docs/ai-frontend-builder.md`
**Depends on**: T32, T37, T38, T39, T40
**Requirement**: Success Criteria, AFB-13, AFB-16

**Done when**: [ ] validation.md with pass/fail per test per orchestrator · [ ] hardware numbers documented

**Tests**: manual UAT · **Gate**: — · **Commit**: `docs: add builder validation report`

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T01 | 1 interface + 1 adapter, same package | ⚠️ cohesive (adapter is the interface's only P1 impl) |
| T02, T03, T04, T05, T06, T09, T13–T17, T19, T21, T24, T28, T29, T31, T33 | 1 file / 1 concern | ✅ |
| T07 | server + 15-line main | ⚠️ cohesive |
| T08 | Dockerfile + template dir | ⚠️ cohesive (template is only meaningful inside the image) |
| T10 | 1 compose file + spike notes | ✅ |
| T11 | 1 file (DDL list) | ✅ |
| T12 | 1 store file | ✅ |
| T18 | loop + limits | ⚠️ cohesive |
| T20, T22, T23, T25, T26, T27, T32 | 1 endpoint group / 1 file each | ✅ |
| T30 | wiring in 2 files | ⚠️ cohesive |
| T34–T37 | 1 component/page area each | ✅ |
| T38–T41 | 1 manifest family / docs each | ✅ |
| T42–T44 | 1 deliverable each | ✅ |

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram | Status |
| --- | --- | --- | --- |
| T01 | — | root | ✅ |
| T02 | T01 | T01→T02 | ✅ |
| T03 | — | root | ✅ |
| T04 | T03 | T03→T04 | ✅ |
| T05 | T03 | T03→T05 | ✅ |
| T06 | — | root | ✅ |
| T07 | T03,T04,T05,T06 | T04,T05,T06→T07 (T03 via T04/T05) | ✅ |
| T08 | — | root | ✅ |
| T09 | — | root | ✅ |
| T10 | T07,T08,T09 | noted under diagram | ✅ |
| T11 | — | root | ✅ |
| T12 | T11 | T11→T12 | ✅ |
| T13 | T11 | T11→T13 | ✅ |
| T14 | T06,T07 | T06,T07→T14 | ✅ |
| T15 | T14 | T14→T15 | ✅ |
| T16 | T15 | T15→T16 | ✅ |
| T17 | — | root [P] | ✅ |
| T18 | T01,T12,T13,T15,T17 | listed | ✅ |
| T19 | T18 | T18→T19 | ✅ |
| T20 | T12,T14,T18 | listed | ✅ |
| T21 | T11 | T11→T21 | ✅ |
| T22 | T11 | T11→T22 | ✅ |
| T23 | T11 | T11→T23 | ✅ |
| T24 | T14,T22 | listed | ✅ |
| T25 | T12,T14,T22,T23 | listed | ✅ |
| T26 | T20,T25 | listed | ✅ |
| T27 | T16,T20,T25 | listed | ✅ |
| T28 | T20,T25 | listed | ✅ |
| T29 | T06,T25 | listed | ✅ |
| T30 | T21–T29 | listed | ✅ |
| T31 | — | root [P] | ✅ |
| T32 | T25,T31 | listed | ✅ |
| T33 | T30,T32 | listed | ✅ |
| T34 | T33 | T33→T34 | ✅ |
| T35 | T33 | T33→T35 | ✅ |
| T36 | T33 | T33→T36 | ✅ |
| T37 | T36 | T36→T37 | ✅ |
| T38 | T10,T30 | listed | ✅ |
| T39 | T38 | T38→T39 | ✅ |
| T40 | T39 | T39→T40 | ✅ |
| T41 | T38,T39 | listed | ✅ |
| T42 | T10,T18 | listed | ✅ |
| T43 | T02,T42 | listed | ✅ |
| T44 | T32,T37,T38,T39,T40 | listed | ✅ |

Parallel flags: only on tasks whose tests are unit (parallel-safe) and with no unfinished dependency in the same phase (T01, T03, T06, T08, T09, T17, T31). ✅

## Test Co-location Validation

| Task | Layer | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T01–T07, T09, T14, T15, T17–T19, T31 | pure Go | unit | unit | ✅ |
| T08, T10 | image / manifest | none | none (build/manual) | ✅ |
| T11 | DDL | none | none | ✅ |
| T12, T13, T20 | Postgres store | integration | integration | ✅ |
| T16, T21–T29, T32 | dashboard handlers/stores | integration | integration | ✅ |
| T30 | server wiring | integration | integration (smoke) | ✅ |
| T33–T37 | frontend | none | none | ✅ |
| T38–T41 | manifests/docs | none | none | ✅ |
| T42, T43 | eval | eval | eval | ✅ |
| T44 | UAT | manual | manual | ✅ |

## Requirement Coverage

| Req | Tasks |
| --- | --- |
| AFB-01 | T11, T21, T34 |
| AFB-02 | T13, T22, T34 |
| AFB-03 | T07, T24, T35 |
| AFB-04 | T08, T17, T25, T35 |
| AFB-05 | T01, T02, T15, T18, T26, T36 |
| AFB-06 | T12, T18, T26, T33, T36 |
| AFB-07 | T04, T15, T18 |
| AFB-08 | T06, T07, T29, T37 |
| AFB-09 | T05, T12, T18 |
| AFB-10 | T05, T19, T28, T37 |
| AFB-11 | T16, T25 |
| AFB-12 | T16, T18, T27, T36 |
| AFB-13 | T29, T44 (+ design: builder holds no key) |
| AFB-14 | T01, T13, T18, T36 |
| AFB-15 | T03, T04, T07 |
| AFB-16 | T09, T10, T38, T39, T40, T44 |
| AFB-17 | T06, T07, T14 |
| AFB-18 | T03, T12, T18, T20 |
| AFB-19 | T05, T14 |
| AFB-20 | T42, T43 |
| AFB-22 | T31, T32, T37 |
| AFB-25 | T17, T22, T34 |
| AFB-26 | T08, T23, T34 |
| AFB-27 | T09, T10 |
| AFB-21, 23, 24, 28–30 | P2/P3 — not in this task list |

Coverage: 24 P1 requirements, 24 mapped. ✅
