# App-Update Schema-Drift Fix Validation

**Date**: 2026-08-25
**Spec**: `.specs/features/app-update-schema-drift-fix/spec.md`
**Diff range**: `e8feaf2~1..f6acf84` (4 commits: `e8feaf2` fix `UpdateApp`, `bc5e819` runbook SQL, `bf34e63` docs, `f6acf84` gap fixes)
**Verifier**: independent sub-agent (author ≠ verifier)
**Iteration**: 2 of max 3 (round 1 verdict: FAIL — 1 Major + 3 Minor)

## Validation Verdict: PASS

**Result**: PASS — 10/10 ACs verified with evidence. All 4 round-1 gaps independently reproduced as closed, each with a discrimination check proving the pre-fix form still fails. One non-blocking residual documented below (step 1c scoping, explicitly optional in round 1's Fix 4).

---

## Round-1 Gap Closure (the focus of this iteration)

Every closure below was reproduced by this Verifier from scratch on a **new** disposable database `repair_verify_v3` (container `zeep-orbit-db-1`), dropped at the end (`DROP DATABASE repair_verify_v3`, absence verified). The round-1 `repair_verify_v2` and the author's `repair_check` were not reused. The real `zeep` test database and production were never touched.

| Round-1 gap | Severity | Fix in `f6acf84` | Independent evidence | Status |
| ----------- | -------- | ---------------- | -------------------- | ------ |
| Fix 1 / AUSD-09 — `repair.sql` probes not re-runnable (`operator does not exist: numeric !~ unknown`) | Major | 7 probes now `<col>::text !~ '...'` (`repair.sql:95,104,113,122,131,140,149`) + idempotency note extended (`repair.sql:83-89`) | Full-file run → 7 manual `ALTER`s → **full-file re-run: exit 0, zero errors, every query `(0 rows)`, `UPDATE 0`** | ✅ CLOSED |
| Fix 2 / AUSD-04 — audit side effect untested, mutant survived | Minor | audit assertion added (`handler_update_app_schema_drift_test.go:87-97`) | Mutation re-run: deleting `handler.go:1134` now **fails** the test at `:97` | ✅ CLOSED |
| Fix 3 — Part 3 type check hides a dropped column (`NULL NOT IN (...)`) | Minor | `IS DISTINCT FROM 'numeric'` (`repair.sql:178`) | Dropped `vacancies.time_to_start`: fixed query **reports it** (`physical_type` NULL); old `NOT IN` form returns `(0 rows)` | ✅ CLOSED |
| Fix 4 — Part 3 RLS check unscoped, 9th table = false alarm | Minor | scoped to the same 8 names (`repair.sql:188-191`) | 9th table `legacy_unrelated_table` with `rls='disabled'` inserted: fixed query → `(0 rows)`; old unscoped form → **1 false-alarm row** | ✅ CLOSED at Part 3 (see residual) |

### Gap 1 detail (the Major)

Fixture: schema `zeep_system` (`apps` + `app_tables` with the 8 tables at `rls='disabled'` and `columns` JSON declaring the 7 `numeric` columns) and schema `internal_portal_rh` with the 4 physical tables / 7 columns all `TEXT`, seeded with clean numeric-parseable text (`'82.5'`, `'91'`, `'  70  '`, `'-3'`, `'9'`, `'7.5'`, `'4.2'`, `'13'`, `'0'`, `'45'`, `'2'`, `'30.0'`, `NULL`s).

- **Pass 1** (`psql -v ON_ERROR_STOP=1 -f repair.sql`): 1a listed exactly the 8 tables at `'disabled'`; 1b → `UPDATE 8`; 1c → `(0 rows)`; all 7 probes → `(0 rows)`; Part 3 correctly still listed all 7 columns as `configured=numeric / physical=text` (no `ALTER` self-executed — AUSD-06/07 re-confirmed).
- **7 manual `ALTER`s** applied: all succeeded.
- **Pass 2** (identical command, post-`ALTER` state): **`EXIT=0`** — 1a `(0 rows)`, `UPDATE 0`, 1c `(0 rows)`, all 7 probes `(0 rows)`, Part 3 type check `(0 rows)`, Part 3 RLS check `(0 rows)`. No `ERROR` of any kind.
- **Discrimination**: the pre-fix probe form run against the same now-`numeric` column still errors —
  `ERROR: operator does not exist: numeric !~ unknown` (exit 1). The `::text` cast is load-bearing, not cosmetic.

---

## Spec-Anchored Acceptance Criteria

Re-confirmed for the ACs touched by `f6acf84`; AUSD-01/02/03/05/06/07/08/10 carry forward from round 1 (unchanged code/SQL surface for those, re-confirmed green by the gate below and by pass 1 of the fixture run).

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| AUSD-01 | fields persisted, no `prov.Apply` on this path | `handler.go:1119-1130`; `handler_update_app_schema_drift_test.go:67` (`w.Code != http.StatusOK`) | ✅ PASS |
| AUSD-02 | PUT succeeds despite drift, no `TypeChangeError` | `handler_update_app_schema_drift_test.go:67-69`, drift injected `:44-51` | ✅ PASS |
| AUSD-03 | per-table endpoints still reconcile | `handler.go:1222`, `:1380`, `:1442` untouched; asserted at `handler_update_app_schema_drift_test.go:83` | ✅ PASS |
| AUSD-04 | `h.audit(..., "app.update", "app", app.ID, ...)` still recorded | `handler.go:1134` + **now asserted**: `handler_update_app_schema_drift_test.go:87-97` — `SELECT COUNT(*) FROM zeep_system.audit_log WHERE action='app.update' AND resource_id=$1` must equal exactly 1. Mutant deleting `handler.go:1134` is killed | ✅ PASS (was ❌) |
| AUSD-05 | row-level listing (PK + raw value) of non-parseable rows | `repair.sql:92-95,101-104,110-113,119-122,128-131,137-140,146-149` — all 7 `SELECT id, <col> ... <col>::text !~ '...'`. Fixture: 0 false positives on `'  70  '` (whitespace), `'-3'`, `'82.5'`, `NULL` | ✅ PASS |
| AUSD-06 | non-parseable ⇒ that column's `ALTER` skipped | protocol `repair.sql:71-81`; every `ALTER` shipped commented out; full-file run executed zero `ALTER`s (Part 3 confirmed all 7 still `text`) | ✅ PASS |
| AUSD-07 | per-column `ALTER ... USING <col>::numeric` | `repair.sql:96-98` etc.; all 7 applied cleanly on the fixture, values preserved | ✅ PASS |
| AUSD-08 | `rls=''` for exactly the 8 named tables, nothing outside | `repair.sql:41-50` → `UPDATE 8` on the fixture; the unrelated 9th table added later was **not** touched | ✅ PASS |
| AUSD-09 | script idempotent — re-run re-validates, acts only on still-drifted rows | **Full-file re-run after the 7 `ALTER`s: exit 0, all queries `(0 rows)`, `UPDATE 0`** (see Gap 1 detail). Both halves now idempotent | ✅ PASS (was ❌) |
| AUSD-10 | never invoked by Go/CLI/hook/CI | `grep -rln "repair.sql"` → only `spec.md`. No Go/YAML/Makefile/shell reference | ✅ PASS |

**Status**: ✅ 10/10 ACs verified with evidence.

---

## Discrimination Sensor

Scratch: temporary detached `git worktree` at `f6acf84`, under the session scratchpad, removed with `git worktree remove --force` + `git worktree prune`. **`git stash` never used.** Real tree `git status --porcelain` identical before and after (` M .specs/LESSONS.md`, ` M .specs/lessons.json`, `?? .../validation.md` — the same 3 entries at start and end); `git worktree list` back to the single main entry. The worktree needed `internal/dashboard/static/` copied in (gitignored embed target) to compile.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ----------- | ------- |
| 2 (re-run from round 1) | `internal/dashboard/handler.go:1134` | Deleted the required side effect `h.audit(r.Context(), user.ID, user.Email, "app.update", "app", app.ID, app.Name, nil, r.RemoteAddr)`, replaced with a comment | ✅ **Killed** (survived in round 1) — `TestUpdateApp_DoesNotReconcileTableSchema` fails at `handler_update_app_schema_drift_test.go:97`: `expected exactly 1 app.update audit_log row for this app, got 0` |

**SQL-side discrimination** (equivalent sensor for the runbook, which has no executing code — each fixed query was run head-to-head against its pre-fix form on the same fixture state):

| Probe | Pre-fix form | Post-fix form | Discriminating? |
| ----- | ------------ | ------------- | --------------- |
| Column validation on an already-`numeric` column | `ERROR: operator does not exist: numeric !~ unknown` (exit 1) | `(0 rows)`, exit 0 | ✅ yes |
| Part 3 type check with `vacancies.time_to_start` dropped | `(0 rows)` — silently clean | 1 row: `vacancies / time_to_start / numeric / (null)` | ✅ yes |
| Part 3 RLS check with an unrelated 9th table at `rls='disabled'` | 1 row: `legacy_unrelated_table / disabled` — false alarm | `(0 rows)` | ✅ yes |

**Sensor depth**: targeted (1 code mutation re-run + 3 SQL head-to-head discriminations, aimed exactly at the round-1 gaps)
**Result**: 4/4 discriminating — every fix is load-bearing, none is a no-op edit.

---

## Interactive UAT Results

Not performed — backend/infrastructure change with no UI surface (validate.md §3: automated checks are sufficient).

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ — `f6acf84` is +38/−11, all of it either a `::text` cast, a predicate swap, a scoping clause, or the missing assertion |
| Surgical changes | ✅ — no production Go logic changed in this commit; only a test assertion and the runbook SQL |
| No scope creep | ✅ — no change to `provisioner.pgType()`, no drift scanner, no auto-executed migration |
| Matches patterns/style | ✅ — `gofmt -l internal/dashboard/*.go` clean; assertion uses the package's existing `pool.QueryRow`/`t.Fatalf` idiom |
| Tests map to ACs, non-shallow | ✅ — the new block is explicitly labelled `AUSD-04` and asserts an exact count (`!= 1`), not merely `> 0`, so it also catches double-auditing |
| Spec-anchored outcome check | ✅ — AUSD-04 now asserts the exact `action`/`resource_id` the spec names |
| Per-layer Coverage Expectation | ✅ — route happy path, drifted-table edge path, and the audit side effect all covered |
| Comments explain *why* | ✅ — `repair.sql:83-89`, `:158-162`, `:180-183` each record the failure mode the change prevents, per `AGENTS.md` §2 |
| Documented guidelines followed | ✅ — CHANGELOG already carries the entry (`CHANGELOG.md:19`); English error strings unchanged; no auto-run migration (§8); `schemaNameForDB` semantics respected (`repair.sql:18-19`) |

---

## Edge Cases

- [x] Non-numeric row reported with PK + raw value, not just a count — `repair.sql:92-95` selects `id, <col>`.
- [x] **Re-run after everything is already repaired reports "nothing to do", no no-op error** — ✅ NOW HANDLED: full-file re-run exits 0 with every query `(0 rows)` and `UPDATE 0`. This was the round-1 Major.
- [x] Configured `numeric` column whose physical column was dropped entirely — now reported by Part 3 (`IS DISTINCT FROM`), verified by dropping `vacancies.time_to_start`.
- [x] Unrelated 9th table on the same app carrying a legacy `rls` value — no longer misreported as a failure of this repair by Part 3's check.
- [x] `PUT /apps/{id}` on an app with zero tables — no regression path remains; covered by the pre-existing `TestUpdateAppHandler_SavingOtherFieldsDoesNotWipeGoogleClientSecret` and `TestUpdateAppEnduserRoles`, both passing.

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && gofmt -l internal/dashboard/*.go`, then `go test ./... -count=1 -p 1` with `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable`, `DASHBOARD_BOOTSTRAP_SECRET=change-me-in-production`
- **Result**: `go build` exit 0; `go vet` exit 0; `gofmt -l` printed nothing.
  `ok github.com/zeeplabs/zeep-orbit/internal/dashboard 136.537s` plus all 16 remaining packages `ok` (`auth`, `config`, `crypto`, `dashboard/ai`, `db`, `deploy/render`, `docs`, `github`, `mcpserver`, `policytemplates`, `provisioner`, `query`, `registry`, `server`, `sshkey`, `webhookengine`) — **0 failed, 0 skipped**
- **Targeted test**: `go test ./internal/dashboard/ -run TestUpdateApp_DoesNotReconcileTableSchema -v -count=1` → `--- PASS` (uncached, `-count=1` used specifically to defeat the cached result)
- **Test count**: 440 top-level test functions pass in `internal/dashboard`; **unchanged by `f6acf84`** — the AUSD-04 fix added an assertion inside the existing test rather than a new test function, which is why the count does not move. (Round 1's "459" counted subtests too; the delta is a counting-method difference, not a lost test.)
- **Failures**: none
- **Harness note (not a product defect)**: an initial `go test ./... -p 2` run produced 14 failures in `internal/dashboard` (`deadlock detected`, `not found`, FK violations). These are Postgres contention from the parallelism I chose, not a regression — the same package is fully green at `-p 1`, consistent with commit `e9ad97a` ("cap Go test parallelism to avoid Postgres connection exhaustion"). Use `-p 1` for this repo.

---

## Residual (non-blocking, carried forward)

### R-1 (Minor, optional): step 1c's RLS verification is still unscoped

Round 1's Fix 4 named **two** queries — `repair.sql:53-57` (step 1c) and Part 3's final RLS check. `f6acf84` scoped Part 3 but left step 1c as the broad `WHERE a.name = 'internal-portal-rh' AND t.rls NOT IN (...)` form — the exact query shape demonstrated above to produce a false alarm when a 9th unrelated table carries a legacy `rls` value.

Why this does **not** block the PASS:

- No AC is violated. AUSD-08 constrains the `UPDATE`'s scope (verified: `UPDATE 8`, 9th table untouched); AUSD-09 requires deterministic re-runs (verified: 1c returns the same result on both passes, no error). Step 1c is an operator aid, not a spec'd outcome.
- Round 1 itself classified Fix 4 as "Minor, optional… a documentation/robustness nit, not a correctness bug", and offered "note in the comment" as an acceptable alternative remedy.
- Operationally it is pre-empted: step 1a (`repair.sql:27-36`) is deliberately broad and carries an explicit "If this returns anything else… STOP and re-diagnose" gate, so an operator meets the 9th table at 1a — before ever reaching 1c — and is already told what it means.

Suggested cheap follow-up (bundle with any future edit to this file, not worth a commit of its own): add the same `t.name IN (...)` filter to `repair.sql:53-57`, or one comment line saying a row here means "out-of-scope table, re-diagnose" rather than "repair failed".

---

## Requirement Traceability Update

| Requirement | Round-1 Status | New Status |
| ----------- | -------------- | ---------- |
| AUSD-01 | ✅ Verified | ✅ Verified |
| AUSD-02 | ✅ Verified | ✅ Verified |
| AUSD-03 | ✅ Verified | ✅ Verified |
| AUSD-04 | ❌ Needs Fix | ✅ Verified (assertion added; mutant now killed) |
| AUSD-05 | ✅ Verified | ✅ Verified |
| AUSD-06 | ✅ Verified | ✅ Verified |
| AUSD-07 | ✅ Verified | ✅ Verified |
| AUSD-08 | ✅ Verified | ✅ Verified |
| AUSD-09 | ❌ Needs Fix | ✅ Verified (full-file re-run clean, exit 0) |
| AUSD-10 | ✅ Verified | ✅ Verified |

---

## Lessons Reconciliation

`lessons.py list` reports no *confirmed* lessons (both entries are single-recurrence candidates). `L-034` (unasserted side effects can be deleted with the suite still green — `signal: surviving_mutant`, evidence `internal/dashboard/handler.go:1134`) and `L-035` (an idempotent repair script must be re-run end to end against a fixture already in the post-repair state — `signal: ac_gap`, evidence `repair.sql:91`) already capture both round-1 findings accurately, and this round's fixes are exactly what those lessons prescribe. **No new lesson recorded** — R-1 is a narrower instance of the same scoping observation Fix 4 already raised, not a new failure mode.

---

## Summary

**Overall**: ✅ **PASS** (10/10 ACs satisfied by evidence)

**Spec-anchored check**: 10/10 ACs matched the spec outcome with `file:line` evidence; 0 uncovered, 0 violated
**Sensor**: 4/4 discriminating (1 code mutation killed — the round-1 survivor; 3 SQL head-to-head checks where the pre-fix form provably misbehaves)
**Gate**: all 17 packages `ok`, 0 failed, 0 skipped; build/vet/gofmt clean

**What changed since round 1**:
- The Major (AUSD-09) is genuinely fixed: a from-scratch fixture ran `repair.sql` → 7 `ALTER`s → `repair.sql` again with **exit 0 and every query returning 0 rows**. The pre-fix probe still errors on the same data, so the `::text` cast is load-bearing.
- AUSD-04 now has a real, exact-count assertion; the mutant that survived 459 tests in round 1 is killed.
- Both optional hardening fixes (dropped-column detection, RLS scoping) landed in Part 3 and were verified head-to-head against their pre-fix forms.

**Remaining**: one Minor residual (R-1: step 1c still unscoped) — documented above, violates no AC, pre-empted by step 1a's STOP gate, and explicitly optional in round 1's own classification.

**Next steps**: Feature is verified and may be considered done. `repair.sql` is now safe to run against production following the runbook in order — including the re-run-after-partial-success path that was the blocker in round 1. No further verification iteration required (closed at iteration 2 of 3).
