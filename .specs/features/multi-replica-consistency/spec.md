# Multi-Replica Consistency Specification

## Problem Statement

Zeep Orbit runs with `replicaCount: 2` behind a non-sticky load balancer (`charts/zeep-orbit/values.yaml`), but several pieces of state that a request depends on live only in each pod's memory and are written only by the pod that handled the mutation. A schema or config change made through one pod is therefore invisible to the others until they restart. Separately, table DDL runs with no lock wait bound, so a single blocked `ALTER TABLE` can hang the request and queue every later query on that table behind it.

Reported in production (2026-10): creating or editing tables "locked up" and only recovered after restarting the Kubernetes pods. The exact root cause of that incident was **not** confirmed — no `pg_stat_activity` / per-pod evidence was captured before the restart. This spec covers both mechanisms that can produce that symptom (stale per-pod state, unbounded DDL lock waits), plus the diagnostics needed to tell them apart next time.

### Evidence from the code (as of `develop` `0954a88`)

| # | Finding | Where | Effect with 2+ pods |
|---|---|---|---|
| H1 | `registry.Registry` (apps, tables, columns, auth providers, storage, rate-limit config) is loaded once at boot (`LoadFromDB`) and afterwards updated only via `Register`/`Unregister` on the pod that served the dashboard/MCP mutation. No cross-pod invalidation, no periodic reload. | `cmd/zeep/main.go:83`, `internal/registry/registry.go`, ~15 `h.reg.Register` calls in `internal/dashboard/handler.go` | Pod B answers 404 / "table not found" for a table created on pod A; serves a dropped app; uses old columns for query building; uses old Google/storage/rate-limit config. Data Browser (`handler.go:2783`, `:2877`) and the public REST API (`internal/server/handler.go`) both read from it. A restart "fixes" it because `LoadFromDB` runs again. |
| H1b | `SystemConfig` (`SoftDeleteEnabled`, `StatementTimeoutMs`) is read at boot and updated only on the pod that served `PUT /system-config`. | `cmd/zeep/main.go:106`, `internal/dashboard/handler.go:483` | Pods disagree on soft-delete: the same `DELETE` is a hard delete on one pod and a soft delete on another. Statement-timeout protection differs per pod. **Data-correctness issue, not just staleness.** |
| H1c | App-token activity cache (`tokencache`, 30s TTL) is invalidated only on the pod that served the revoke/regenerate. | `internal/tokencache/tokencache.go`, `internal/dashboard/app_tokens_store.go:120,145` | A revoked app token keeps working for up to 30s on other pods. Bounded, but a security-relevant window. |
| H2 | `provisioner.Apply` runs DDL (`CREATE TABLE`, `ALTER TABLE ... ADD/ALTER COLUMN`, `CREATE INDEX`) on the shared pool with no `lock_timeout` and no cross-pod serialization per app. | `internal/provisioner/provisioner.go:32`, called from 6 dashboard handlers | `ALTER TABLE` needs `ACCESS EXCLUSIVE`. Behind any long-running or `idle in transaction` holder it waits forever, and every later query on that table queues behind the waiting DDL. Restarting pods kills the holders' connections, which releases the queue — matches the reported symptom. |
| H3 | `ProvisionZeepSystem` runs ~34 `ALTER`s in one transaction at boot, under a session-wide advisory xact lock, with no `lock_timeout`. | `internal/dashboard/provisioner.go:12` | During a rolling deploy, the new pod can block behind live traffic holding locks on `zeep_system.*`, and live traffic then queues behind it. |
| H4 | Purge takes `pg_try_advisory_lock` (session-level) via `pool.QueryRow` and releases it via `pool.Exec` — two calls that may run on **different** pooled connections. | `internal/dashboard/purge.go:32,39` | Unlock can miss; the lock stays held by an idle pooled connection until that connection is recycled. Purge then silently no-ops on every pod. Not a lock-up of user requests, but the same class of cross-pod bug. |
| H5 | Pool size is hardcoded (`MaxConns = 10`, `MinConns = 2`), not configurable. | `internal/db/client.go:25` | No way to size connections per replica against the Postgres `max_connections` budget. |
| H6 | `/health` returns `200 {"status":"ok"}` without touching the database. | `internal/server/handler.go:711` | A pod whose pool is exhausted or blocked stays "ready" and keeps receiving traffic. |

## Goals

- [ ] A schema or config change accepted by any pod is reflected on every pod within a bounded window (target: **≤ 5s**, see Assumptions), without restarting pods.
- [ ] No dashboard/MCP table mutation can hang indefinitely on a Postgres lock; it either completes or fails with a clear, retryable error within a bounded time.
- [ ] When a lock-up happens again, the logs and health endpoints are enough to tell which mechanism caused it, without restarting first.

## Out of Scope

| Feature | Reason |
|---|---|
| Distributed rate limiting (login, MCP, webhook limiters shared across pods) | Today each pod has its own limiter, so the effective limit is N × replicas. Fixing that needs shared state (Postgres or Redis) and is a separate decision. Documented as a known limitation instead. |
| Helm chart hardening (`image.tag: latest`, PDB disabled, anti-affinity) | Chart/K8s changes are higher-risk per AGENTS.md §8 and independent from the code fix. Separate PR, separate confirmation. Exceptions: the readiness probe path (MREP-14) and the optional pool-size value (MREP-17). |
| Removing the in-memory registry entirely (read schema from Postgres on every request) | Changes the hot path of every public API request. Kept as a design alternative to evaluate, not the default. |
| Supporting PgBouncer/RDS Proxy in transaction-pooling mode for LISTEN/NOTIFY | Depends on the production answer to Q1. If a transaction-mode pooler is in front, the design must not depend on LISTEN for correctness (see MREP-05). |
| Online/zero-lock DDL strategies (`CREATE INDEX CONCURRENTLY`, shadow tables) | Bounding the wait is the fix for the reported symptom. Reducing lock strength is a separate optimization. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
|---|---|---|---|
| Q1: Is there a connection pooler (PgBouncer / RDS Proxy) between the pods and Postgres in production? | Unknown. The design must stay correct without LISTEN/NOTIFY: periodic reconcile is the correctness floor, and NOTIFY (if used) only shortens the window | LISTEN needs a dedicated session-level connection, which transaction pooling breaks silently | y (2026-10-23: "unknown, design without depending on it") |
| Q2: Acceptable propagation window for schema/config changes across pods | ≤ 5s worst case, ~immediate in the common case | Dashboard users switch pages faster than a 30–60s poll; a 5s change-detection poll is cheap at this scale | y (2026-10-23) |
| Q3: How long may table DDL wait for a lock before failing | `lock_timeout = 5s` for dashboard/MCP DDL, no automatic retry; error is retryable by the client | Below the HTTP `WriteTimeout` (15s, `internal/server/server.go:78`) so the client gets a real error instead of a dropped connection | y (2026-10-23) |
| Q4: Error contract when DDL times out on a lock | HTTP 409, English message along the lines of `table is busy with another operation, try again in a few seconds` (exact text finalized in Design); the MCP tool returns the same message | AGENTS.md §4: English API errors, no raw `err.Error()` leak | y (2026-10-23, with Q3) |
| Q5: Readiness vs liveness on DB failure | Readiness fails when a bounded DB ping fails; liveness stays process-only | Failing liveness on a DB outage would restart every pod at once and make the outage worse | y (2026-10-23) |
| Root cause of the 2026-10 incident | Not confirmed; the spec fixes both H1 and H2 | No evidence captured before the restart | — |

**Open questions:** none. Q1–Q5 confirmed by Julio on 2026-10-23.

---

## User Stories

### P1: A change made on one pod is visible on every pod ⭐ MVP

**User Story**: As an app owner using the dashboard (or MCP), I want a table, column, app or config change to take effect everywhere right after I save it, so that the public API and the Data Browser never disagree depending on which pod answered.

**Why P1**: This is the direct cause of "works after restarting the pods". Every multi-replica deployment hits it.

**Acceptance Criteria**:

1. WHEN a table is created through pod A THEN pod B SHALL serve that table on the public REST API and in the Data Browser within the propagation window, without a restart.
2. WHEN a column is added, retyped, or its constraints/enum values change through pod A THEN pod B SHALL build queries with the new column set within the propagation window.
3. WHEN an app is deleted through pod A THEN pod B SHALL return 404 for that app within the propagation window.
4. WHEN an app's auth providers, storage config, rate-limit config or email-auth flag change through pod A THEN pod B SHALL use the new values within the propagation window.
5. WHEN pod B receives a request for an app or table it does not have in memory THEN it SHALL check Postgres once before answering 404, and SHALL cache the result so repeated misses for unknown names do not hit the DB on every request (miss-flood protection).
6. WHEN the propagation signal is lost (pooler drops the listener, connection reset, notification missed) THEN the periodic reconcile SHALL still converge every pod to the DB state within the propagation window.
7. WHEN a reload fails (DB unavailable) THEN the pod SHALL keep serving its last good registry, log the failure, and retry — never clear the registry.

**Independent Test**: Run two `zeep serve` processes against the same Postgres. Create a table through process A's dashboard API; poll process B's REST API for that table and assert 200 within the window. Repeat for column add, app delete and auth-provider change.

---

### P1: System settings are consistent across pods ⭐ MVP

**User Story**: As a platform admin, I want soft-delete and statement-timeout settings to apply to every pod as soon as I save them, so that the same request never behaves differently depending on the pod.

**Why P1**: Divergent soft-delete is silent data loss (hard delete on one pod while the admin expects soft delete).

**Acceptance Criteria**:

1. WHEN `SoftDeleteEnabled` or `StatementTimeoutMs` changes through pod A THEN pod B SHALL apply the new value within the propagation window.
2. WHEN the system config cannot be read at boot THEN the pod SHALL fail startup, so Kubernetes restarts it. It SHALL NOT serve traffic with the zero-value config (no statement timeout, soft delete off). WHEN the read fails after boot THEN the pod SHALL keep its last good config and retry on the next sync tick. *(Changed from "keep retrying in the background" in the design review, approved by Julio on 2026-10-23: a pod that refuses to start is safer than one that silently hard-deletes.)*

**Independent Test**: Two processes; toggle soft delete through A; issue `DELETE` through B; assert the row is soft-deleted (has `deleted_at`), not removed.

---

### P1: Table DDL never hangs indefinitely ⭐ MVP

**User Story**: As an app owner, I want creating or editing a table to either succeed or fail with a clear message within seconds, so that one stuck operation never freezes the table for everyone.

**Why P1**: Matches the reported symptom; an unbounded `ACCESS EXCLUSIVE` wait also blocks every reader of that table.

**Acceptance Criteria**:

1. WHEN dashboard/MCP DDL cannot acquire its table lock within the lock timeout THEN the request SHALL fail with the agreed retryable error (Q4), and no metadata change SHALL be persisted (`app_tables` keeps matching the physical table — preserves the existing "provision before persist" rule in `handler.go`).
2. WHEN two pods receive DDL for the same app at the same time THEN the operations SHALL be serialized per app across pods (one waits, bounded by the same timeout), never interleaved on the same table.
3. WHEN DDL fails on the lock timeout THEN the server SHALL log the app, table, operation and the blocking backend PIDs/states from `pg_locks`/`pg_stat_activity` — never query text or row data.
4. WHEN DDL succeeds THEN behavior SHALL be unchanged from today (same responses, same audit entries).
5. WHEN the boot-time `ProvisionZeepSystem` cannot get its locks within a bounded time THEN the pod SHALL fail startup with a clear error (and be restarted by Kubernetes), instead of blocking live traffic on `zeep_system.*`.

**Independent Test**: Hold `BEGIN; LOCK TABLE <app>.<table> IN ACCESS SHARE MODE;` open in `psql`, then add a column through the dashboard API. Assert the API answers the retryable error within lock timeout + margin, the table metadata is unchanged, and a `SELECT` on the table from another session still returns immediately after the API call returns.

---

### P2: Lock-ups are diagnosable without restarting pods

**User Story**: As an operator, I want the pod health and logs to show when a pod is stuck on the database, so that I can find the blocker instead of restarting blindly.

**Why P2**: The 2026-10 incident could not be root-caused because the restart erased the evidence.

**Acceptance Criteria**:

1. WHEN the readiness endpoint is probed THEN it SHALL run a bounded DB ping (short timeout) and return non-200 if it fails, so Kubernetes stops routing to that pod.
2. WHEN the liveness endpoint is probed THEN it SHALL NOT depend on the database (Q5). Today both probes point at `/health` (`charts/zeep-orbit/values.yaml:102,107`, `k8s/deployment.yaml:45,52`), so readiness needs its own path and the chart/`k8s/` readiness probe must move to it. This is the **only** chart change in scope (AGENTS.md §8: explain and confirm before applying). `/health` keeps its current contract for existing external monitors.
3. WHEN a registry reload or propagation event runs THEN the pod SHALL log it at debug level, and SHALL log at warn level when it falls back to the periodic reconcile because the signal path is down.
4. WHEN the change ships THEN `RELEASE.md` or `docs/` SHALL contain the lock-up runbook (per-pod checks, `pg_stat_activity` + `pg_blocking_pids`, pooler check, image digest comparison) to run **before** restarting pods.

---

### P2: Pool size is configurable

**User Story**: As an operator, I want to set the per-pod Postgres pool size, so that `replicas × pool size` fits the database's `max_connections`.

**Acceptance Criteria**:

1. WHEN `DB_MAX_CONNS` (name finalized in Design) is set THEN the pool SHALL use it; WHEN unset THEN the default SHALL remain 10.
2. WHEN the variable is invalid (non-numeric, ≤ 0) THEN the process SHALL fail at boot with a clear error.
3. WHEN the variable is added THEN it SHALL be documented in the README configuration table (and the 3 translated READMEs) and in the chart values, per AGENTS.md §6/§8.

---

### P2: Revoked app tokens stop working on every pod

**User Story**: As an app owner, I want a revoked or regenerated app token to stop working immediately on every pod.

**Acceptance Criteria**:

1. WHEN an app token is revoked or regenerated through pod A THEN pod B SHALL reject it within the propagation window (instead of up to the 30s cache TTL).

---

### P3: Background purge lock cannot leak

**Acceptance Criteria**:

1. WHEN the purge job takes its advisory lock THEN it SHALL acquire and release it on the same connection (or use a transaction-scoped lock), so a missed unlock can never block future purges.
2. WHEN a purge run finishes or fails THEN the lock SHALL be released and the next tick on any pod SHALL be able to take it.

---

## Edge Cases

- WHEN an app is renamed or deleted while a reload is in progress THEN the swap SHALL be atomic: readers see either the old or the new registry, never a partial one (keep the existing build-then-swap pattern in `LoadFromDB`).
- WHEN many apps exist THEN a full reload SHALL NOT issue one query per app per reconcile tick indefinitely without bound (Design to decide: change-detection via a version/`updated_at` check before a full reload).
- WHEN a pod receives its own change notification THEN applying it again SHALL be idempotent.
- WHEN a request is in flight during a registry swap THEN it SHALL complete with the registry snapshot it started with.
- WHEN the miss cache holds a "not found" for a name that is later created THEN propagation SHALL clear that entry (AC1.5 must not delay AC1.1).
- WHEN `lock_timeout` aborts a multi-statement DDL sequence midway THEN the already-applied statements SHALL be either rolled back or left in a state that the next `Apply` reconciles (Design must state which, per statement type; `Apply` is idempotent today).
- WHEN the DB is briefly unreachable THEN readiness SHALL flap only after N consecutive failures (chart `failureThreshold`), not on a single slow ping.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| MREP-01 | P1 (propagation) AC1–AC2: table/column changes reach every pod | Done | Verified |
| MREP-02 | P1 (propagation) AC3: app deletion reaches every pod | Done | Verified |
| MREP-03 | P1 (propagation) AC4: app auth/storage/rate-limit config reaches every pod | Done | Verified |
| MREP-04 | P1 (propagation) AC5: reload-on-miss with miss-flood protection | Done | Verified |
| MREP-05 | P1 (propagation) AC6: periodic reconcile converges without the signal path | Done | Verified |
| MREP-06 | P1 (propagation) AC7: failed reload keeps last good registry | Done | Verified |
| MREP-07 | P1 (system config) AC1: system config reaches every pod | Done | Verified |
| MREP-08 | P1 (system config) AC2: boot-time config read retries | Done | Verified |
| MREP-09 | P1 (DDL) AC1: lock timeout returns retryable error, no metadata persisted | Done | Verified |
| MREP-10 | P1 (DDL) AC2: per-app DDL serialized across pods | Done | Verified |
| MREP-11 | P1 (DDL) AC3: lock-timeout log with blocker info, no sensitive data | Done | Verified |
| MREP-12 | P1 (DDL) AC4: success path unchanged | Done | Verified |
| MREP-13 | P1 (DDL) AC5: boot provisioning bounded | Done | Verified |
| MREP-14 | P2 (diagnostics) AC1–AC2: readiness pings DB, liveness does not | Done | Verified |
| MREP-15 | P2 (diagnostics) AC3: reload/fallback logging | Done | Verified |
| MREP-16 | P2 (diagnostics) AC4: lock-up runbook documented | Done | Verified |
| MREP-17 | P2 (pool) AC1–AC3: configurable pool size, documented | Done | Verified |
| MREP-18 | P2 (tokens) AC1: token revocation reaches every pod | Done | Verified |
| MREP-19 | P3 (purge) AC1–AC2: purge lock on one connection | Done | Verified |

**Coverage:** 19 total, 19 mapped to tasks (see `tasks.md` Requirement Coverage), 0 unmapped. All 19 verified on 2026-10-23 (see `validation.md`).

---

## Success Criteria

- [ ] Two-process integration test (same Postgres) passes for table create, column add, app delete, auth-provider change, system-config change and token revoke, each converging within the propagation window.
- [ ] Lock-holding integration test: dashboard DDL against a locked table returns the retryable error within lock timeout + 2s, and other sessions are not blocked after it returns.
- [ ] Rolling restart of a 2-replica deployment in a non-production cluster shows no 404s for existing tables and no request exceeding the HTTP write timeout.
- [ ] Go gates (`go build`, `go vet`, `gofmt`, `go test -p 1 -parallel 4 ./...`) and CI green.
- [ ] CHANGELOG `[Unreleased]` entries in the same change; README config table (4 languages) updated for any new env var.
