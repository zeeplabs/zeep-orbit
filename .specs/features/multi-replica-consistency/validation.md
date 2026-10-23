# Multi-Replica Consistency — Validation

**Date**: 2026-10-23
**Branch**: `feat/multi-replica-consistency` (commits `7d8fb11`..`d0644bc` on top of `develop` `0954a88`)
**Database**: disposable `postgres:16` container on `127.0.0.1:55433`. The password was generated per run and kept in a 0600 file outside the repo.

## Automated gate

| Check | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `gofmt -l internal cmd` | clean |
| `go test -count=1 -p 1 -parallel 4 ./...` (`TEST_DATABASE_URL` set) | 18 packages `ok`, 0 `FAIL` |
| `helm lint charts/zeep-orbit` | 0 failed |
| `cd docs && npm run build` | success |
| Frontend (`tsc -b`, `npm run build`) | not run: no UI change |

## Tests per requirement

| Req | Evidence |
|---|---|
| MREP-01..03, 07, 18 | `TestMultiReplica_PodBConvergesAfterSync`: pod B converges after a write through pod A's handler for table create, column add, enum values, auth providers, `jwt_secret` rotation, soft-delete toggle, token revoke (token cache cleared) and app delete. `TestStateVersion_*`: each trigger bumps; hot-path token writes do not; a rollback does not. |
| MREP-04 | `TestMultiReplica_MissHookFindsNewTable`, `TestMissHook`, `TestOnMiss_RateLimited` (100 misses in 1s result in 1 sync). |
| MREP-05 | `TestMultiReplica_RunConvergesWithoutSyncNow` (polling only, under 1s at a 100ms interval). |
| MREP-06, 15 | `TestSyncNow_LoadErrorKeepsLastGoodState`, `TestSyncNow_VersionErrorKeepsLastGoodState`, `TestSyncNow_LogsWarnOnFailureAndDebugOnReload`, `TestSyncNow_LocalWriteDuringLoadIsNotRolledBack`. |
| MREP-08 (amended) | Boot runs `SyncNow` and exits 1 on error. `TestLoadRegistrySnapshot_SystemConfigErrorReturnsNoSnapshot`. Manual boot checked (T8). |
| MREP-09 | `TestApply_TableLockedByReaderTimesOut`, `TestCreateTablePolicy_LockedTableTimesOutWithoutMetadata`, `TestUpdateAppTable_LockTimeoutRollsBackMetadata`, `TestUpdateAppTableHandler_LockTimeoutReturns409`, `TestOrbitAddTableColumn_LockTimeoutReturnsBusyMessage`. |
| MREP-10 | `TestLockAppDDL_*`, `TestApply_SameAppSerialized`, `TestApply_ConcurrentSameAppBothSucceed`, `TestApply_DifferentAppsDoNotBlock`. |
| MREP-11 | `TestUpdateAppTableHandler_LockTimeoutReturns409` checks the holders log has pid/state/mode and none of `query`, `usename`, `client_addr` or SQL text. |
| MREP-12 | 109 pre-existing provisioner tests unchanged and passing; full suite green. `TestApply_FailureLeavesNoPartialDDL`. |
| MREP-13 | `TestProvisionZeepSystem_BoundedByLockTimeout` (3 attempts, bounded). |
| MREP-14 | `TestHandleReady` (200 / 503). |
| MREP-16 | `docs/docs/deployment.md` "Várias réplicas" section and runbook (placeholders only). |
| MREP-17 | `TestMaxConnsFromEnv`, `TestNewWithMaxConns`. Manual: `DB_MAX_CONNS=abc` makes boot exit 1. Helm renders `DB_MAX_CONNS` only when it is set. |
| MREP-19 | `TestPurgeExpiredSoftDeletesReleasesLock`. The `pg_locks` query was confirmed to count a held lock. |

## Two-process smoke (`zeep serve` on :18080 = A and :18081 = B, one database)

```
1. bootstrap: 201
   login via A: 200
2. app created via A: yes
   B data-browser before table: 404
3. table created via A: yes
   B data-browser after create: 200 in 1.02s
4. edit while locked via B: {"error":"table is busy with another operation, try again in a few seconds"} HTTP409 in 5.07s
   holders log: [{'pid': 13372, 'state': 'active', 'mode': 'AccessShareLock', 'relation': 'items'}] | has query/usename/client_addr: False
   edit after lock released: 200
   A sees new column: 200
```

The lock was taken from `psql` with `LOCK TABLE smokeapp.items IN ACCESS SHARE MODE` and held for 12s.

Readiness with Postgres stopped (`docker stop`):

```
up:      ready=200 health=200
db down: ready={"status":"unavailable"} 503 in 0.04s   health={"apps":1,"status":"ok"} 200
db back: ready=200
```

An earlier manual check (T8) inserted an app directly via SQL with two processes running; both reported `"apps":2` within 2.5s.

## Deviations from design (all recorded in `tasks.md`)

- `Syncer.OnMiss` is exported, because `main` sets the hook from another package.
- `Registry.Generation`/`ReplaceIfGeneration` were added. A sync whose snapshot predates this pod's own commit could otherwise roll that commit back on the serving pod until the next poll.
- The 409 mapping lives in `writeError`, so every handler that runs DDL is covered, not only those that use `writeProvisioningError`.
- `EnsureAuthUserColumns` checks the catalog before taking any lock. It used to take `ACCESS EXCLUSIVE` on `_auth_users` on every dashboard app-users request, a plausible contributor to the original lock-up.
- `UpdateAppTable` writes the metadata and the RLS DDL in one transaction.
- T10, T11 and T12 are one commit (`f6485fd`) because they share files.

## Not verified here

- Behavior behind a real pooler (PgBouncer). The design avoids session state across transactions: the DDL locks are transaction-scoped and the purge holds one connection. It has not been exercised against a pooler.
- A real Kubernetes rollout. The chart was checked with `helm template`/`lint` and `kubectl kustomize` only.
- The root cause of the original incident is still not confirmed, because the restart destroyed the evidence. The runbook exists so the next occurrence can be diagnosed before restarting.
