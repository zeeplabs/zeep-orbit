# Multi-Replica Consistency Design

**Spec**: `.specs/features/multi-replica-consistency/spec.md`
**Status**: Approved (2026-10-23). Julio approved: the MREP-08 AC2 deviation (spec updated), the chart/`k8s/` readiness path change plus `database.maxConns`, and the `lock_timeout`-covers-advisory-locks assumption (to be proven by a test).

---

## Architecture Overview

Two independent mechanisms, one per root-cause family:

1. **State sync (MREP-01..08, 15, 18).** Postgres stays the only source of truth. A single-row version counter in `zeep_system.state_version` is bumped by statement-level triggers on every table the in-memory state is built from. Each pod polls that one row every 2s. When the version changes, the pod rebuilds its whole registry snapshot from Postgres and swaps it in atomically. A lookup miss triggers the same sync immediately, rate-limited. No LISTEN/NOTIFY: correctness never depends on a session-level connection (Q1).
2. **Bounded DDL (MREP-09..13).** Every DDL entry point runs inside one transaction per app. The transaction first runs `SET LOCAL lock_timeout = '5s'`, then takes `pg_advisory_xact_lock` keyed by the app schema. On SQLSTATE `55P03` (lock_not_available) it returns a typed error that the dashboard maps to HTTP 409.

Plus small, separate fixes: `/ready` with a DB ping (MREP-14), `DB_MAX_CONNS` (MREP-17), and the purge lock moved onto one connection (MREP-19).

```mermaid
sequenceDiagram
    participant UI as Dashboard (via pod A)
    participant A as Pod A
    participant PG as Postgres
    participant B as Pod B
    UI->>A: PUT /tables/{id} (add column)
    A->>PG: BEGIN; SET LOCAL lock_timeout='5s'; pg_advisory_xact_lock(app)
    A->>PG: ALTER TABLE ... ; COMMIT
    A->>PG: UPDATE zeep_system.app_tables ...
    PG-->>PG: trigger: state_version.version += 1 (same tx)
    A->>A: reg.Register(app) (local, immediate)
    loop every 2s
        B->>PG: SELECT version FROM zeep_system.state_version
        alt version changed
            B->>PG: load apps + tables + system_config
            B->>B: reg.Replace(snapshot); tokencache.Clear()
        end
    end
    Note over B: Lookup miss (unknown app/table): sync now, at most 1/s
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|---|---|---|
| `appRowToRegistryApp`, `buildAppConfig` | `internal/dashboard/handler.go:2348,2384` | The **only** AppRow → `registry.App` conversion. The new loader reuses it, so a reloaded registry is identical to one built by `Register`. |
| `scanAppTableRows` | `internal/dashboard/apps_store.go` (used by `loadAppTables:590`) | Scans `app_tables` rows; the loader reuses it with one query for all apps instead of one per app. |
| `GetSystemConfig` | `internal/dashboard/system_config_store.go:62` | System config part of the snapshot. |
| `Registry` build-then-swap | `internal/registry/registry.go:216` | Same atomic swap pattern, exposed as `Replace`. |
| `ProvisionZeepSystem` statement list | `internal/dashboard/provisioner.go:12` | New table, function and triggers are appended as idempotent statements. |
| `lock_timeout` + advisory lock precedent | `internal/server/webhook_handler.go:585-603` | Same approach (bounded wait on an advisory lock), transaction-scoped here instead of session-scoped. |
| `writeProvisioningError` | `internal/dashboard/handler.go:2599` | Gains one `errors.As` case for the lock-timeout error, which becomes a 409. |
| `DASHBOARD_LOG_BUFFER_SIZE` parsing | `internal/dashboard/handler.go:56` | Precedent for an optional int env var. |
| `golang.org/x/sync/singleflight` | already in `go.mod` | Collapses concurrent sync triggers (ticker plus misses) into one load. |

### Integration Points

| System | Integration Method |
|---|---|
| `cmd/zeep/main.go` boot | Replaces `reg.LoadFromDB` + boot `GetSystemConfig` with `syncer.SyncNow` (fail boot on error), then `go syncer.Run(ctx)`. |
| Dashboard / MCP / AI chat mutations | No change needed for propagation: triggers catch every write, and the existing local `h.reg.Register` calls stay for an immediate effect on the serving pod. |
| Public API (`internal/server`), docs, auth | Unchanged call sites (`reg.Get`, `reg.GetTable`); the miss hook lives inside the registry. |
| Helm chart / `k8s/` | Readiness probe path `/health` → `/ready`; optional `database.maxConns` value → `DB_MAX_CONNS`. |

---

## Components

### 1. `zeep_system.state_version` + triggers (DB)

- **Purpose**: A transactional, monotonic "something the registry depends on changed" counter.
- **Location**: appended to `stmts` in `internal/dashboard/provisioner.go`.
- **DDL** (all idempotent, PG ≥ 11 — CI runs 16):
  ```sql
  CREATE TABLE IF NOT EXISTS zeep_system.state_version (
    id         SMALLINT PRIMARY KEY CHECK (id = 1),
    version    BIGINT      NOT NULL DEFAULT 0,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  INSERT INTO zeep_system.state_version (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

  CREATE OR REPLACE FUNCTION zeep_system.bump_state_version() RETURNS trigger
  LANGUAGE plpgsql AS $$
  BEGIN
    UPDATE zeep_system.state_version SET version = version + 1, changed_at = now() WHERE id = 1;
    RETURN NULL;
  END $$;
  ```
  Triggers (created inside a `DO` block guarded by `pg_trigger` lookups, because `CREATE OR REPLACE TRIGGER` needs PG 14):
  | Table | Trigger | Why |
  |---|---|---|
  | `zeep_system.apps` | `AFTER INSERT OR UPDATE OR DELETE ... FOR EACH STATEMENT` | app create/delete, auth providers, storage, rate limit, email auth, **`jwt_secret` rotation** (`handler.go:3706` — today a rotated secret keeps being accepted by other pods until restart) |
  | `zeep_system.app_tables` | `AFTER INSERT OR UPDATE OR DELETE ... FOR EACH STATEMENT` | tables, columns, RLS mode |
  | `zeep_system.system_config` | `AFTER INSERT OR UPDATE OR DELETE ... FOR EACH STATEMENT` | soft delete, statement timeout |
  | `zeep_system.app_tokens` | `AFTER UPDATE OF revoked_at OR DELETE ... FOR EACH STATEMENT` | revocation only. **Not** `INSERT` or a plain `UPDATE`: `last_used_at` is written on the request hot path (`app_tokens_store.go:167`) and `expires_at` by `auth/handler.go:461`. A plain `UPDATE` trigger would bump the version on every API call. |
- **Reuses**: n/a (new).

### 2. `registry.Registry` additions

- **Location**: `internal/registry/registry.go`
- **Interfaces**:
  - `Replace(apps map[string]*App, sys SystemConfig)`: atomic swap of both under one lock.
  - `SetMissHook(fn func())`: called by `Get`/`GetTable` on a miss, outside the lock. If it is set, the lookup is retried once after it returns.
- **Removed**: `LoadFromDB`. It builds `App` differently from `appRowToRegistryApp`: no `AllowedValues`, no `Config.Storage`/`Config.RateLimit`, and different nil rules. A pod's registry therefore differs before and after a restart today. Removing it also drops `registry`'s dependency on `internal/db`.

### 3. `registry.Syncer` (new)

- **Purpose**: Keep one pod's registry converged with Postgres.
- **Location**: `internal/registry/syncer.go`
- **Interfaces**:
  ```go
  type Snapshot struct {
      Apps   map[string]*App
      System SystemConfig
  }
  type SyncerConfig struct {
      Version  func(ctx context.Context) (int64, error) // cheap: one row
      Load     func(ctx context.Context) (Snapshot, error)
      Interval time.Duration // 2s
      MissMinInterval time.Duration // 1s
      MissTimeout     time.Duration // 2s
      OnChange []func()     // e.g. tokencache.Clear
      Logf     func(level, format string, args ...any)
  }
  func NewSyncer(reg *Registry, cfg SyncerConfig) *Syncer
  func (s *Syncer) SyncNow(ctx context.Context) error // version check + load if changed; singleflight
  func (s *Syncer) Run(ctx context.Context)            // ticker loop until ctx done
  func (s *Syncer) onMiss()                            // rate-limited SyncNow with MissTimeout
  ```
- **Behavior**:
  - The version is read **before** the load, so a change committed during the load is picked up on the next tick. Never miss, at worst reload twice.
  - On load or version error, keep the current registry, set `lastVersion = -1` so the next tick retries, and log a warning (MREP-06).
  - `onMiss` runs at most once per `MissMinInterval` per pod; extra misses inside the window return immediately (MREP-04 miss-flood protection, no per-name negative cache to invalidate).
- **Dependencies**: `golang.org/x/sync/singleflight`.

### 4. Snapshot loader (dashboard)

- **Location**: `internal/dashboard/registry_snapshot.go` (new)
- **Interfaces**:
  - `ReadStateVersion(ctx, pool *db.Pool) (int64, error)`
  - `LoadRegistrySnapshot(ctx, pool *db.Pool) (registry.Snapshot, error)`: one query for all apps (same columns and JSON decoding as `GetApp`, including `jwt_secret`), then `loadAppTables` per app. That is N+1 queries, the same as today's `LoadFromDB`, and acceptable because it runs only on a version change. Then `appRowToRegistryApp`, plus `GetSystemConfig`.
- **Reuses**: `appRowToRegistryApp`, `loadAppTables`/`scanAppTableRows` (`apps_store.go:590,744`), `decodeEnduserRolesConfig` (`apps_store.go:183`), `GetSystemConfig`. The app-row decoding duplicated between `ListApps` and `GetApp` gets extracted into one helper used by all three.

### 5. DDL lock helper + transactional provisioner

- **Location**: `internal/db/ddl_lock.go` (new); `internal/provisioner/*`
- **Interfaces**:
  ```go
  // db
  var DDLLockTimeout = 5 * time.Second // var so tests can shorten it
  func LockAppDDL(ctx context.Context, tx pgx.Tx, schemaName string) error
      // SET LOCAL lock_timeout = <DDLLockTimeout>;
      // SELECT pg_advisory_xact_lock(hashtextextended('zeep-orbit-ddl:' || $1, 0))
  func IsLockTimeout(err error) bool // SQLSTATE 55P03

  // provisioner
  var ErrDDLLockTimeout = errors.New("table is busy with another operation, try again in a few seconds")
  type querier interface { db.Querier; Begin(ctx context.Context) (pgx.Tx, error) }
  type Provisioner struct { q querier; locked bool }
  func (p *Provisioner) withAppLock(ctx, schema string, fn func(tp *Provisioner) error) error
  ```
- **Behavior**:
  - Each exported mutating method wraps its body in `withAppLock`: `Apply` (one transaction per app in `cfg.Apps`), `EnsureAuthTables`, `EnsureStorageTables`, `EnsureAuthUserColumns`, `ApplyColumnConstraintChanges`, `DropTable`, `AddColumnForeignKey`, `DropColumnForeignKey`, `ReplaceColumnEnumValues`, and `BackfillEnduserGrants` (one transaction per schema).
  - A provisioner that is already locked (`locked == true`) runs `fn` directly, so there is no nested lock.
  - The read-only `Check*` methods are unchanged.
  - The internal `p.pool.Begin` in `column_constraints.go:120` becomes a savepoint when it runs inside the transaction (pgx `Tx.Begin`).
  - On error, `IsLockTimeout` maps to `fmt.Errorf("%w: ...", ErrDDLLockTimeout)`, so callers use `errors.Is`.
  - The transaction rolls back on any error, so a lock timeout halfway through `Apply` leaves no partial DDL. Postgres DDL is transactional, and no `CONCURRENTLY` or `ALTER TYPE ... ADD VALUE` exists in `internal/provisioner`.
- **Package-level helpers called on the pool** (`EnsureRowLevelSecurity`, `RelaxOwnerColumn` from `apps_store.go:705-708`): wrap the caller in its own transaction plus `LockAppDDL`. `RelaxOwnerColumn` tolerates `42703` by catching the error; inside a transaction that error aborts the transaction, so it gets a `SAVEPOINT` (or a `pg_attribute` existence check first, decided in Tasks).
- **Policy DDL** (`table_policies_store.go:127,243,297`): already transactional; add `db.LockAppDDL(ctx, tx, schemaName)` as the first statement.

### 6. Lock-timeout diagnostics

- **Location**: `internal/dashboard/handler.go` (`writeProvisioningError`), plus a small helper.
- **Behavior**:
  - On `ErrDDLLockTimeout`, respond 409 with the message from `ErrDDLLockTimeout`.
  - Log `app`, `table`, `operation` and the holders from another pool connection:
    `SELECT a.pid, a.state, a.backend_type, a.xact_start, a.state_change, l.mode FROM pg_locks l JOIN pg_stat_activity a USING (pid) WHERE l.relation = to_regclass($1) AND l.granted`
  - Never log `query`, `usename` or `client_addr`. MREP-11 forbids query text. User and IP are omitted to avoid personal data in logs.
  - If the wait was on the per-app advisory lock (no relation holders found), log "waiting on per-app DDL lock held by another request".
- **MCP**: confirm in Tasks how `mcpserver` maps provisioning errors from `*ForUser` methods; it must surface the same English message, not a generic 500.

### 7. Boot provisioning bound (MREP-13)

- `ProvisionZeepSystem` gets `SET LOCAL lock_timeout = '5s'` after its advisory lock.
- It retries up to 3 attempts with 1s/2s backoff on `55P03`, about 18s worst case. That stays below the chart liveness budget (`initialDelaySeconds 5 + 3 × 10s`).
- After that it returns an error, and `main` exits 1.

### 8. Readiness (MREP-14)

- `GET /ready` (`internal/server/server.go`, next to `/health`) runs `pool.Ping` with a 2s context and returns `200 {"status":"ready"}` or `503 {"status":"unavailable"}`.
- `/health` is unchanged (liveness, docker `healthcheck` at `main.go:172`, external monitors).
- Chart `values.yaml` `probes.readiness.path: /ready` and `k8s/deployment.yaml` readiness path. **Chart change, confirm before applying (AGENTS.md §8).**

### 9. Pool size (MREP-17)

- `db.MaxConnsFromEnv() (int32, error)` reads `DB_MAX_CONNS`. Unset gives 10; an invalid value or one ≤ 0 is an error and boot exits.
- `db.New(ctx, dsn)` keeps its signature (tests). A new `db.NewWithMaxConns(ctx, dsn, n)` is used by `main`. `MinConns` stays 2, capped at `n`.
- Docs: README env table plus the 3 translations; chart `database.maxConns` (empty means unset, rendered only if set).

### 10. Purge lock (MREP-19)

- `PurgeExpiredSoftDeletes`: `conn, _ := pool.Acquire(ctx)`, then `pg_try_advisory_lock` and the deferred `pg_advisory_unlock` on that same `conn` before `conn.Release()`.
- The DELETEs keep running through the pool. The lock only needs to live on one session.

---

## Data Models

```go
// registry
type Snapshot struct {
    Apps   map[string]*App // keyed by app name, same as today
    System SystemConfig
}
```

`zeep_system.state_version`: see Component 1. There is one row and it is never deleted.

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
|---|---|---|
| Version poll or snapshot load fails (DB down) | Keep last good registry, `lastVersion = -1`, warn log, retry next tick | Requests keep working with last known schema; no 404 storm |
| Boot `SyncNow` fails | `main` exits 1 (same as today's `LoadFromDB` failure) | Pod restarts. **Spec deviation from MREP-08 AC2** — see Tech Decisions. |
| DDL waits > 5s on a table lock or the per-app lock | Transaction rolled back, `ErrDDLLockTimeout`, 409 plus a holders log | "table is busy with another operation, try again in a few seconds"; metadata untouched |
| Other DDL error | Unchanged (`writeProvisioningError` existing cases / 500 generic) | Unchanged |
| Boot `ProvisionZeepSystem` lock timeout ×3 | Exit 1 | Pod restarts, live pods keep serving |
| `/ready` ping fails | 503 | Kubernetes removes the pod from the Service until the DB is reachable |
| `DB_MAX_CONNS` invalid | Exit 1 with `invalid DB_MAX_CONNS` | Misconfiguration visible at deploy |
| Miss flood (random app/table names) | ≤ 1 sync per second per pod; the rest answer 404 from memory | None |

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
|---|---|---|
| Signal path | Poll one row every 2s; no LISTEN/NOTIFY | Pooler in prod unknown (Q1); LISTEN through a transaction-mode pooler fails silently and would hold one of the 10 pool connections. A 2s single-row read per pod is negligible. LISTEN can be added later as an accelerator without changing correctness. |
| Change detection | DB triggers, not a Go `bumpVersion()` call at each write site | Catches every write path: ~15 dashboard sites, MCP, AI chat, future handlers, and manual repair SQL (`.specs/features/app-update-schema-drift-fix/repair.sql`). Forgetting a call site is the failure mode this feature exists to remove. |
| Counter type | Row `UPDATE`, not a sequence | A sequence advances outside the transaction, so a pod could see the new value, reload before the writer commits, and record the version without the change. The row update is visible only after commit. The cost is a short row lock shared by concurrent dashboard writes, which are low volume. |
| Full reload vs. per-app delta | Full reload | Reload runs only on change, and at current scale (single-digit apps) it costs about 1 + N + 1 queries. A per-app delta needs change records and is deferred. |
| Remove `LoadFromDB` | Yes, one conversion path | Fixes an existing divergence. A restarted pod builds `App` without `AllowedValues`, `Config.Storage` and `Config.RateLimit`, unlike a pod that saw `Register`. [Provável] that this divergence has user-visible effects (`AllowedValues` and `Config.Storage` consumers to be listed in Tasks); in either case one code path is required for reload correctness. |
| Local `Register` after a mutation | Kept | Gives the serving pod an immediate update. A sync that started before the write may briefly overwrite it with the older snapshot; the next tick (≤ 2s) corrects it because the version moved. That window is within Q2. |
| DDL scope of the lock | Per app schema, transaction-scoped | Unrelated apps never wait on each other; nothing can leak, since the lock is released at COMMIT or ROLLBACK. |
| `lock_timeout` covers advisory waits | [Provável] it does: `lock_timeout` applies to any heavyweight lock wait, and the webhook code already relies on it (`webhook_handler.go:589`) | An integration test must prove it (T: two concurrent DDL on the same app; the second times out on the advisory lock). If the test disproves it, switch to a `pg_try_advisory_xact_lock` poll loop with a deadline. |
| MREP-08 AC2 | Boot fails instead of running with default system config | Today the pod boots with a zero `SystemConfig` (no statement timeout, no soft delete) and only logs a warning. Under the new loader the snapshot is all-or-nothing; a pod refusing to start is safer than one silently hard-deleting. Approved; spec updated. |
| Token cache | `tokencache.Clear()` on every version change | Simpler than per-JTI invalidation. Cost: one DB check per active token after any change, which is rare. |

---

## Testing Strategy (input for Tasks)

| Area | Type | Notes |
|---|---|---|
| `Syncer` | unit (fake `Version`/`Load`, fake clock where needed) | reload only on change; error keeps last good and retries; miss rate limit; singleflight; `OnChange` called |
| Triggers | integration (`TEST_DATABASE_URL`) | each write path bumps version; `last_used_at`/`expires_at` updates do **not** |
| Two-registry convergence | integration | two `Registry`+`Syncer` on one pool; mutate through handler A; `SyncNow` on B; assert table/column/app delete/auth provider/jwt rotation/system config/token revoke |
| Loader parity | integration | `LoadRegistrySnapshot` result deep-equals `appRowToRegistryApp(GetApp(...))` for the same app |
| DDL lock timeout | integration | hold `LOCK TABLE ... IN ACCESS SHARE MODE` in another conn; `DDLLockTimeout` shortened in test; expect `ErrDDLLockTimeout`, metadata unchanged, handler 409, no partial columns |
| Per-app serialization | integration | two concurrent `Apply` on same app → second waits/ times out on advisory lock; different apps → both proceed |
| `/ready` | unit/integration | closed pool → 503; healthy → 200; `/health` unchanged |
| `DB_MAX_CONNS` | unit | unset/valid/invalid/≤0 |
| Purge lock | integration | after a run, `pg_locks` has no advisory lock for the purge key; second run takes it |
| Gates | — | `go build`, `go vet`, `gofmt`, `go test -p 1 -parallel 4 ./...` (needs `DASHBOARD_BOOTSTRAP_SECRET`) |
