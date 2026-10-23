# Multi-Replica Consistency Tasks

**Design**: `.specs/features/multi-replica-consistency/design.md`
**Status**: In progress (T1–T9 done; phases 1–2 complete)

There is no `.specs/codebase/TESTING.md`. The test types and gates come from AGENTS.md §3 and the existing suites:
- **unit**: plain `go test` with no DB.
- **integration**: `TEST_DATABASE_URL`, which shares `zeep_system` rows. These tests are **not parallel-safe** across packages, which is why the suite runs with `-p 1`.
- **Quick gate**: `go build ./... && go vet ./... && gofmt -l <changed>` plus `go test ./internal/<pkg>/...`.
- **Full gate**: `DASHBOARD_BOOTSTRAP_SECRET=ci-test-bootstrap-secret-not-for-prod go test -p 1 -parallel 4 ./...` against a disposable `postgres:16`.

Branch: `feat/multi-replica-consistency` from current `develop`. One commit per task.

---

## Execution Plan

### Phase 1: Foundation

```
T1 (db DDL lock helper) [P]
T2 (db pool size)       [P]
T3 (state_version + triggers)
T6 (registry Replace/MissHook, drop LoadFromDB) [P]
```

### Phase 2: State sync

```
T3 ──→ T5 (snapshot loader) ──┐
T6 ──→ T7 (Syncer) ───────────┼──→ T8 (main wiring) ──→ T9 (convergence tests)
T2 ───────────────────────────┘
T4 (boot provisioning bound) depends on T1, T3
```

### Phase 3: Bounded DDL

```
T1 ──→ T10 (transactional provisioner) ──┬──→ T11 (dashboard DDL call sites)
                                          └──→ T12 (409 + diagnostics + MCP)
```

### Phase 4: Ops + docs

```
T13 (/ready) ──→ T14 (chart + k8s)
T15 (purge lock) [P]
T9, T11, T12, T14, T15 ──→ T16 (docs + CHANGELOG) ──→ T17 (full gate + multi-process smoke)
```

---

## Task Breakdown

### T1: DDL lock helper in `db` [P]

**What**: Add `DDLLockTimeout` (var, 5s), `LockAppDDL(ctx, tx, schemaName)` (`SET LOCAL lock_timeout` + `pg_advisory_xact_lock(hashtextextended('zeep-orbit-ddl:'||$1,0))`) and `IsLockTimeout(err)` (SQLSTATE `55P03`).
**Where**: `internal/db/ddl_lock.go`, `internal/db/ddl_lock_test.go`
**Depends on**: None
**Reuses**: `IsStatementTimeout` pattern (`internal/db/client.go:106`), the webhook `lock_timeout` precedent
**Requirement**: MREP-09, MREP-10

**Done when**:
- [x] Integration test: session A holds the same per-app advisory lock. Session B's `LockAppDDL` with `DDLLockTimeout = 300ms` fails within about 300ms + margin, and `IsLockTimeout(err) == true`. **This proves `lock_timeout` covers advisory waits (design assumption).** If it fails, stop and report: the design falls back to a try-lock loop.
- [x] Integration test: a different schema name does not wait.
- [x] The `SET LOCAL` does not leak. After commit, `SHOW lock_timeout` on the released connection returns the server default.
- [x] Quick gate passes.

**Tests**: integration
**Gate**: quick (`go test ./internal/db/...`)
**Status**: Done (2026-10-23). The advisory wait aborted at ~350ms with 55P03, so the design assumption holds.

---

### T2: Configurable pool size [P]

**What**: Add `MaxConnsFromEnv() (int32, error)` (`DB_MAX_CONNS`; unset gives 10; non-numeric or ≤ 0 is an error) and `NewWithMaxConns(ctx, dsn, n)`. `New` delegates to it with 10. `MinConns = min(2, n)`.
**Where**: `internal/db/client.go`, `internal/db/client_test.go`
**Depends on**: None
**Requirement**: MREP-17

**Done when**:
- [x] Unit table test: unset, `"20"`, `"0"`, `"-1"`, `"abc"`, `" 5 "` (decide trim vs. reject and document it).
- [x] The `New(ctx, dsn)` signature is unchanged, so existing callers and tests still compile.
- [x] Quick gate passes.

**Tests**: unit
**Gate**: quick
**Status**: Done (2026-10-23). Surrounding whitespace is trimmed; a blank value means unset.

---

### T3: `state_version` table, bump function and triggers

**What**: Append the idempotent DDL from design Component 1 to `ProvisionZeepSystem`. Triggers are created in a `DO` block guarded by a `pg_trigger` lookup.
**Where**: `internal/dashboard/provisioner.go`, `internal/dashboard/state_version_test.go`
**Depends on**: None
**Requirement**: MREP-01..03, MREP-07, MREP-18

**Done when**:
- [x] Running `ProvisionZeepSystem` twice is a no-op the second time: no error, and one trigger per table.
- [x] Integration test: each of the following bumps `version` by ≥ 1:
  - INSERT, UPDATE and DELETE on `apps`;
  - INSERT, UPDATE and DELETE on `app_tables`;
  - an UPDATE on `system_config`;
  - `UPDATE app_tokens SET revoked_at`.
- [x] Integration test: `UPDATE app_tokens SET last_used_at = now()`, `UPDATE app_tokens SET expires_at = ...` and `INSERT INTO app_tokens` do **not** bump it.
- [x] Integration test: a rolled-back transaction that wrote to `apps` leaves `version` unchanged.
- [x] Full gate passes for `./internal/dashboard/...`.

**Tests**: integration
**Gate**: quick (`go test -p 1 ./internal/dashboard/...`)
**Status**: Done (2026-10-23). The full `internal/dashboard` suite passes.

---

### T4: Bound boot provisioning

**What**: In `ProvisionZeepSystem`, run `SET LOCAL lock_timeout = '5s'` after the advisory xact lock. On `IsLockTimeout`, retry the whole transaction up to 3 attempts with 1s/2s backoff, then return a wrapped error.
**Where**: `internal/dashboard/provisioner.go`, plus a test
**Depends on**: T1, T3
**Requirement**: MREP-13

**Done when**:
- [x] Integration test: another session holds `LOCK TABLE zeep_system.apps IN ACCESS EXCLUSIVE MODE`, and the timeout and backoff are shortened through package vars. `ProvisionZeepSystem` returns an error after exactly 3 attempts and does not hang.
- [x] Normal boot is unchanged; the existing tests stay green.

**Tests**: integration
**Gate**: quick (`./internal/dashboard/...`)
**Status**: Done (2026-10-23). The attempt count is checked through the error text (`after 3 attempts`), not a hook.

---

### T5: Registry snapshot loader

**What**:
- Extract the duplicated app-row JSON decoding (storage, rate limit, enduser roles) from `ListApps` and `GetApp` into one helper.
- Add `ReadStateVersion(ctx, pool)` and `LoadRegistrySnapshot(ctx, pool) (registry.Snapshot, error)`. These cover all apps, including `jwt_secret` plus `loadAppTables`, then `appRowToRegistryApp` and `GetSystemConfig`.
**Where**: `internal/dashboard/registry_snapshot.go`, `internal/dashboard/apps_store.go`, plus a test
**Depends on**: T3, T6 (needs `registry.Snapshot`)
**Requirement**: MREP-01..03, MREP-07

**Done when**:
- [x] Parity test: for an app with enum `AllowedValues`, storage config, rate limit, auth providers and 2 tables, `LoadRegistrySnapshot().Apps[name]` deep-equals `appRowToRegistryApp(GetApp(...))`.
- [x] Test: an error from `GetSystemConfig` makes `LoadRegistrySnapshot` return an error, never a partial snapshot.
- [x] The existing `ListApps`/`GetApp` tests stay green after the extraction.
- [x] List the consumers of `registry.Column.AllowedValues`, `App.Config.Storage` and `App.Config.RateLimit` in the commit message, to close the design's [Provável] on user-visible divergence.

**Tests**: integration
**Gate**: quick (`./internal/dashboard/...`)
**Status**: Done (2026-10-23). Consumers: `registry.Column.AllowedValues` is read only by `internal/docs/generator.go` (OpenAPI `enum`), so after a restart `/docs` lost the enum values. `App.Config.Storage` and `App.Config.RateLimit` have no consumers; the server reads `App.StorageConfig` and `App.RateLimit`, which the old loader did fill. The user-visible divergence was limited to the API docs.

---

### T6: Registry `Replace`, miss hook, remove `LoadFromDB` [P]

**What**:
- Add `Snapshot`, `Replace(apps, sys)` and `SetMissHook(fn)`. `Get`/`GetTable` call the hook on a miss, outside the lock, then retry the lookup once.
- Delete `LoadFromDB` and the `internal/db` import, and update the `registry_test.go` comments that reference it.
**Where**: `internal/registry/registry.go`, `internal/registry/registry_test.go`
**Depends on**: None (`main.go` still compiles once T8 lands; until then keep `LoadFromDB` and delete it in T8, whichever keeps every commit buildable)
**Requirement**: MREP-04, MREP-06

**Done when**:
- [x] Unit tests:
  - `Replace` swaps apps and system config atomically. A concurrent reader under `-race` never sees mixed state.
  - On a miss, the hook is called exactly once per lookup.
  - A hit never calls the hook.
  - The hook can `Replace` and the retry then finds the app.
  - With no hook set, a miss behaves as today.
- [x] `go test -race ./internal/registry/...` passes.

**Tests**: unit
**Gate**: quick
**Status**: Done (2026-10-23). `LoadFromDB` was kept so `main.go` still builds; it is deleted in T8 together with the `registry_test.go` comments.

---

### T7: `registry.Syncer`

**What**: Implement design Component 3: `NewSyncer`, `SyncNow` (singleflight; version read before load; reload only on change; on error keep the last good state and set `lastVersion = -1`), `Run` (ticker), and `onMiss` (rate-limited to `MissMinInterval`, timeout `MissTimeout`). `OnChange` hooks are called after each successful swap.
**Where**: `internal/registry/syncer.go`, `internal/registry/syncer_test.go`
**Depends on**: T6
**Reuses**: `golang.org/x/sync/singleflight`
**Requirement**: MREP-04..06, MREP-15

**Done when**:
- [x] Unit tests with fake `Version`/`Load`:
  - unchanged version means no load;
  - a changed version means one load plus a `Replace` and the `OnChange` hooks;
  - a load error keeps the previous registry, and the next call reloads even with the same version;
  - a version error behaves the same way;
  - 50 concurrent `SyncNow` calls perform 1 load;
  - 100 misses within 1s perform ≤ 1 sync;
  - `Run` stops on ctx cancel without leaking a goroutine.
- [x] Logging: debug on reload, warn on failure.
- [x] `go test -race ./internal/registry/...` passes.

**Tests**: unit
**Gate**: quick
**Status**: Done (2026-10-23). Deviation: the miss hook is exported as `OnMiss`, because `main` sets it from another package.

---

### T8: Wire sync into `main`

**What**:
- In `cmd/zeep/main.go`, use `db.MaxConnsFromEnv` + `NewWithMaxConns` and build the `Syncer`.
- Run `SyncNow` at boot and exit 1 on error. This replaces `LoadFromDB` and the boot `GetSystemConfig` warning path.
- Then `reg.SetMissHook(syncer.onMiss)` and `go syncer.Run(ctx)`.
- Add `tokencache.Clear()` and register it as an `OnChange` hook.
- Delete `LoadFromDB` here if it was kept in T6.
**Where**: `cmd/zeep/main.go`, `internal/tokencache/tokencache.go` (+ test), `internal/registry/registry.go`
**Depends on**: T2, T5, T7
**Requirement**: MREP-05..08, MREP-17, MREP-18

**Done when**:
- [x] `zeep serve` boots against a fresh DB and against a DB with existing apps. `/health` reports the same app count as before.
- [x] `DB_MAX_CONNS=abc zeep serve` exits 1 with a clear message.
- [x] `tokencache.Clear` has a unit test.
- [x] Full gate passes.

**Tests**: unit (tokencache) + manual boot
**Gate**: full
**Status**: Done (2026-10-23). Deviation: added a local write generation to `Registry` (`Generation`/`ReplaceIfGeneration`). Without it, a sync whose snapshot was read before a local commit could overwrite the serving pod's `Register` and roll it back until the next poll. Manual boot was checked against a fresh DB, a DB with existing apps, and `DB_MAX_CONNS=abc` (exit 1). With two `zeep serve` processes, an app inserted via SQL reached both in under 2.5s.

---

### T9: Cross-pod convergence integration tests

**What**: Two `Registry`+`Syncer` pairs on one pool (simulating pod A and pod B), with a dashboard `Handler` wired to registry A. Mutate through A's handler, call `SyncNow` on B, and assert B's state. A second test runs `Run` on B with `Interval = 100ms` and asserts convergence within 1s without calling `SyncNow`.
**Where**: `internal/dashboard/multi_replica_test.go`
**Depends on**: T8
**Requirement**: MREP-01..05, MREP-07, MREP-18

**Done when**: B converges for each of these changes:
- [x] table create;
- [x] column add;
- [x] enum values change;
- [x] app delete (B `Get` returns false);
- [x] auth provider change;
- [x] `jwt_secret` rotation;
- [x] soft-delete toggle;
- [x] token revoke (B's tokencache entry is gone).
- [x] Miss path: a table created through A is found by B's `GetTable` with **no** `SyncNow` call, via the hook.
- [x] Mutation check: disable the trigger on `app_tables` and confirm the column-add case fails (then restore).

**Tests**: integration
**Gate**: full
**Status**: Done (2026-10-23). The mutation check is a permanent test (`TestMultiReplica_TriggerIsLoadBearing`), not a one-off manual step.

---

### T10: Transactional, per-app-locked provisioner

**What**:
- Replace `Provisioner.pool *db.Pool` with the `querier` interface plus a `locked bool`.
- Add `withAppLock` and wrap every exported mutating method listed in design Component 5. `Apply` gets one transaction per app; `BackfillEnduserGrants` one transaction per schema.
- Add `ErrDDLLockTimeout` and map `IsLockTimeout` errors to it.
**Where**: `internal/provisioner/*.go` + tests
**Depends on**: T1
**Requirement**: MREP-09, MREP-10, MREP-12

**Done when**:
- [ ] All existing provisioner tests pass unchanged. This covers MREP-12: the success path is unchanged.
- [ ] Integration tests, with `DDLLockTimeout = 300ms`:
  - another session holding `ACCESS SHARE` on the table makes `Apply` (add column) return `errors.Is(err, ErrDDLLockTimeout)`, and the column does **not** exist afterwards;
  - two concurrent `Apply` on the same app have the second wait or time out;
  - two concurrent `Apply` on different apps both succeed;
  - a failure in the second statement of a multi-statement `Apply` leaves no partial DDL.
- [ ] `ApplyColumnConstraintChanges` still works when nested (savepoint).
- [ ] Full gate passes for `./internal/provisioner/...` and `./internal/dashboard/...`.

**Tests**: integration
**Gate**: full

---

### T11: Lock the dashboard-side DDL call sites

**What**:
- Wrap `EnsureRowLevelSecurity`/`RelaxOwnerColumn` at `apps_store.go:705-708` in a transaction plus `db.LockAppDDL`. `RelaxOwnerColumn` must not abort the transaction on `42703`: check `pg_attribute` first, or use a savepoint.
- Add `db.LockAppDDL` as the first statement of the 3 policy transactions in `table_policies_store.go`.
**Where**: `internal/dashboard/apps_store.go`, `internal/dashboard/table_policies_store.go`, `internal/provisioner/table.go` (if `RelaxOwnerColumn` changes)
**Depends on**: T10
**Requirement**: MREP-09, MREP-10

**Done when**:
- [ ] Existing RLS mode-switch and policy tests stay green.
- [ ] Integration test: policy create against a locked table returns `ErrDDLLockTimeout`-wrapped error, and there is no metadata row.
- [ ] Integration test: switching a table without `owner_id` into policy mode still succeeds (the `42703` path).

**Tests**: integration
**Gate**: full

---

### T12: 409 mapping, holders diagnostics, MCP parity

**What**:
- Add an `ErrDDLLockTimeout` case to `writeProvisioningError` returning 409 with the error's message.
- Add a holders log helper using design Component 6. It records only pid, state, backend_type, xact_start, state_change and lock mode.
- Check how `mcpserver` surfaces provisioning errors from `*ForUser` methods and map the lock timeout to the same English message.
- Check that the policy handlers return 409 too.
**Where**: `internal/dashboard/handler.go`, `internal/dashboard/ddl_diagnostics.go`, `internal/mcpserver/*` (if needed) + tests
**Depends on**: T10, T11
**Requirement**: MREP-09, MREP-11

**Done when**:
- [ ] Handler test: `PUT /tables/{id}` against a locked table returns 409 with the exact message, and `app_tables` is unchanged.
- [ ] MCP tool test: same lock, and the tool error text matches.
- [ ] A log assertion (captured logger) contains the holders' pid/state and does **not** contain `query`, `usename` or `client_addr`.

**Tests**: integration
**Gate**: full

---

### T13: `/ready` endpoint

**What**: Add `GET /ready`, which runs `pool.Ping` with a 2s timeout and returns 200 `{"status":"ready"}` or 503 `{"status":"unavailable"}`. `/health` is unchanged.
**Where**: `internal/server/server.go`, `internal/server/handler.go` + test
**Depends on**: None
**Requirement**: MREP-14

**Done when**:
- [ ] Test: a healthy pool returns 200; a pool closed or pointed at `127.0.0.1:1` returns 503 within the timeout.
- [ ] The `/health` test is unchanged and green.
- [ ] The route is not behind auth or rate limiting. Confirm it is placed exactly like `/health`.

**Tests**: unit/integration
**Gate**: quick (`./internal/server/...`)

---

### T14: Chart and `k8s/` changes (approved 2026-10-23)

**What**:
- Set `probes.readiness.path: /ready` in `charts/zeep-orbit/values.yaml`, and the readiness path in `k8s/deployment.yaml`.
- Add an optional `database.maxConns` value; `deployment.yaml` renders the `DB_MAX_CONNS` env only when it is set.
- **Do not bump the chart version here.** Release does that (RELEASE.md).
**Where**: `charts/zeep-orbit/values.yaml`, `charts/zeep-orbit/templates/deployment.yaml`, `k8s/deployment.yaml`
**Depends on**: T13, T2
**Requirement**: MREP-14, MREP-17

**Done when**:
- [ ] `helm template` with default values shows readiness `/ready`, liveness `/health` and no `DB_MAX_CONNS`.
- [ ] `helm template --set database.maxConns=20` shows `DB_MAX_CONNS: "20"`.
- [ ] `helm lint` is clean.
- [ ] `kubectl kustomize k8s/` renders, if available.

**Tests**: none (render checks)
**Gate**: build

---

### T15: Purge lock on one connection [P]

**What**: In `PurgeExpiredSoftDeletes`, `Acquire` a connection, then try-lock and defer the unlock on that same connection before `Release`.
**Where**: `internal/dashboard/purge.go` + test
**Depends on**: None
**Requirement**: MREP-19

**Done when**:
- [ ] Integration test: after a purge run, no advisory lock for `hashtext('zeep-orbit-purge')` remains in `pg_locks`, and a second run takes the lock and runs.
- [ ] The existing purge tests stay green.

**Tests**: integration
**Gate**: quick (`./internal/dashboard/...`)

---

### T16: Docs, runbook, CHANGELOG

**What**:
- Add `DB_MAX_CONNS` to the env table in `README.md` and the 3 translations (`i18n/README.pt-BR.md`, `.pt-PT.md`, `.es.md`).
- Update `docs/docs/configuration.md`.
- Add a "Running multiple replicas" section to `docs/docs/deployment.md`. It covers:
  - the propagation window;
  - the `/ready` vs. `/health` split;
  - the per-pod rate limiter caveat;
  - the **lock-up runbook to run before restarting pods**: per-pod `port-forward` checks, `pg_stat_activity` + `pg_blocking_pids`, `idle in transaction`, pooler check, image digest compare.
- Add CHANGELOG `[Unreleased]` entries (Fixed: cross-pod staleness, `jwt_secret` rotation propagation, token revoke window, unbounded DDL waits, purge lock leak, registry divergence after restart; Added: `/ready`, `DB_MAX_CONNS`; Changed: boot fails if the system config cannot be loaded).
**Where**: `README.md`, `i18n/README.*.md`, `docs/docs/*.md`, `CHANGELOG.md`
**Depends on**: T9, T11, T12, T14, T15
**Requirement**: MREP-16, MREP-17

**Done when**:
- [ ] All 4 READMEs have the same new row.
- [ ] The runbook contains no real hostnames, credentials or customer data.
- [ ] The CHANGELOG has no contradictions with the existing entries.

**Tests**: none
**Gate**: build (`cd docs && npm run build`, if the docs site build is part of CI; otherwise markdown review)

---

### T17: Final verification

**What**:
- Run the full gate.
- Run a two-process smoke test: `zeep serve` on two ports against one disposable `postgres:16`.
  - Create a table through port A and read it through port B's REST API within 5s.
  - Hold a table lock in `psql` and confirm the dashboard edit returns 409 in about 5s.
  - Stop Postgres and confirm `/ready` returns 503 while `/health` returns 200.
- Write `validation.md` with the evidence and update the traceability table in `spec.md`.
**Depends on**: T16
**Requirement**: all

**Done when**:
- [ ] Full gate green.
- [ ] Smoke evidence recorded (no secrets).
- [ ] Traceability marked Verified per requirement.

**Tests**: e2e (manual, scripted)
**Gate**: full

---

## Pre-Approval Validation

### Check 1: Granularity

| Task | One deliverable? | Notes |
|---|---|---|
| T1 | ✅ | one helper file |
| T2 | ✅ | pool size only |
| T3 | ✅ | DB objects only |
| T4 | ✅ | boot bound only |
| T5 | ✅ | loader (+ decode extraction it needs) |
| T6 | ✅ | registry API |
| T7 | ✅ | syncer |
| T8 | ✅ | wiring |
| T9 | ✅ | test file only |
| T10 | ⚠️ | touches ~8 provisioner files, but one mechanical concept (route through `withAppLock`); splitting per method would leave the package half-transactional between commits |
| T11 | ✅ | dashboard DDL call sites |
| T12 | ✅ | error contract + diagnostics |
| T13 | ✅ | one endpoint |
| T14 | ✅ | chart/k8s |
| T15 | ✅ | one function |
| T16 | ✅ | docs |
| T17 | ✅ | verification |

### Check 2: Diagram vs. `Depends on`

| Task | Depends on (definition) | Diagram | Match |
|---|---|---|---|
| T1 | — | Phase 1 | ✅ |
| T2 | — | Phase 1 | ✅ |
| T3 | — | Phase 1 | ✅ |
| T4 | T1, T3 | Phase 2 note | ✅ |
| T5 | T3, T6 | T3 → T5 (T6 implied via T7 path) | ⚠️ diagram shows T3 → T5 only; T6 → T5 added here |
| T6 | — | Phase 1 | ✅ |
| T7 | T6 | T6 → T7 | ✅ |
| T8 | T2, T5, T7 | T2, T5, T7 → T8 | ✅ |
| T9 | T8 | T8 → T9 | ✅ |
| T10 | T1 | T1 → T10 | ✅ |
| T11 | T10 | T10 → T11 | ✅ |
| T12 | T10, T11 | T10 → T12 (T11 not drawn) | ⚠️ T11 → T12 added here |
| T13 | — | Phase 4 | ✅ |
| T14 | T13, T2 | T13 → T14 (T2 not drawn) | ⚠️ T2 → T14 added here |
| T15 | — | Phase 4 | ✅ |
| T16 | T9, T11, T12, T14, T15 | → T16 | ✅ |
| T17 | T16 | T16 → T17 | ✅ |

The ⚠️ rows are extra edges that the diagram omits for readability. The `Depends on` field is authoritative.

### Check 3: Test co-location

| Task | Code layer | Tests field | OK |
|---|---|---|---|
| T1, T3, T4, T5, T9, T10, T11, T12, T15 | DB-backed Go | integration | ✅ |
| T2, T6, T7 | pure Go | unit | ✅ |
| T8 | wiring + tokencache | unit + manual boot | ✅ |
| T13 | HTTP handler | unit/integration | ✅ |
| T14 | Helm/k8s | render checks | ✅ |
| T16 | docs | none | ✅ |
| T17 | system | e2e smoke | ✅ |

`[P]` only on T1, T2, T6, T15. They have no code dependencies, but they still share the integration DB, so they run one at a time under `-p 1`. `[P]` here means they can be written in any order, not run concurrently.

---

## Requirement Coverage

| Requirement | Tasks |
|---|---|
| MREP-01 | T3, T5, T9 |
| MREP-02 | T3, T5, T9 |
| MREP-03 | T3, T5, T9 |
| MREP-04 | T6, T7, T9 |
| MREP-05 | T7, T8, T9 |
| MREP-06 | T6, T7, T8 |
| MREP-07 | T3, T5, T8, T9 |
| MREP-08 | T5, T8 |
| MREP-09 | T1, T10, T11, T12 |
| MREP-10 | T1, T10, T11 |
| MREP-11 | T12 |
| MREP-12 | T10 |
| MREP-13 | T4 |
| MREP-14 | T13, T14 |
| MREP-15 | T7 |
| MREP-16 | T16 |
| MREP-17 | T2, T8, T14, T16 |
| MREP-18 | T3, T8, T9 |
| MREP-19 | T15 |

**Coverage:** 19/19 mapped.
