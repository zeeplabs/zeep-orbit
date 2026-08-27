# App-Update Schema-Drift Fix Specification

## Problem Statement

`Handler.UpdateApp` (`internal/dashboard/handler.go:1032-1135`, the REST handler behind `PUT /dashboard/api/apps/{id}`) unconditionally calls `h.prov.Apply` to reconcile every table/column/index on the app on **every** save — even though `AppRequestBody` (`handler.go:879-885`) has no `Tables` field and this endpoint never intends to touch tables (its own comment: "Tables are managed one at a time via the /apps/{id}/tables endpoints, not here"). This means toggling `auth_email_enabled` on the Login tab, or `storage_config`/`rate_limit` on other tabs, silently reconciles the app's entire schema as a side effect. If any table has schema drift — a column's configured type no longer matching its physical Postgres type, or an invalid legacy `rls` value — that unrelated save fails with a provisioner error naming a table/column the admin never touched.

This is not hypothetical: production app `internal-portal-rh` has exactly this drift today (7 columns across 4 tables with `numeric` configured but `TEXT` physical; 8 tables with a legacy `rls="disabled"` value the current validation no longer accepts) and cannot save any Login/Storage/API-tab change until repaired. The drift's root cause (a missing `"numeric"` case in `provisioner.pgType()`, live 2026-06-27 to 2026-07-29) is already fixed in code — this spec addresses the two things that fix did not: the handler's unrelated blast radius, and the specific data already corrupted during that window.

## Goals

- [x] `PUT /dashboard/api/apps/{id}` no longer reconciles table schema for requests that cannot and do not touch tables — bringing REST in line with the MCP path (`UpdateAppForUser`, `handler.go:1493`), which already skips this reconciliation.
- [x] A reviewed, manually-run SQL runbook exists to repair `internal-portal-rh`'s known drift (7 column types, 8 `rls` values), so the app becomes saveable again without any live code executing the repair automatically.

## Out of Scope

| Feature | Reason |
|---|---|
| Automatic execution of the data-repair script by any application code | Production data mutation stays a human-reviewed action, not something a deploy or a background job triggers — matches `AGENTS.md` §8's "schema migrations... explain the change and confirm before applying." |
| Designing real per-table RLS policies (owner/enabled/policy + actual clauses) for the 8 `internal-portal-rh` tables | User's explicit decision (confirmed 2026-08-25): normalize to `rls=""` now to restore the same access behavior `"disabled"` already had (unfiltered) and unblock saves, without changing the app's exposure. Real access-control design is a separate follow-up, not blocking this bug fix. |
| A general drift-detection tool (scanning all apps for config/physical type or rls mismatches) | This spec fixes the one confirmed live incident. A proactive scanner is a reasonable follow-up but a different, larger deliverable — not needed to unblock `internal-portal-rh` or stop the blast-radius bug. |
| Any change to `provisioner.pgType()` or the type-mapping bug itself | Already fixed in commit `a8cfd40` (2026-07-29, v0.5.0). Nothing left to do there. |
| Detecting or repairing drift on apps other than `internal-portal-rh` | No other app's drift has been confirmed. If one turns up later, it reuses this same runbook pattern, not a new spec. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
|---|---|---|---|
| RLS repair target value | `""` (empty string / public, same as `config.ValidRLS`'s default-allowed empty mode) for all 8 tables | User's explicit choice: restore save-ability without changing today's de-facto access behavior (`"disabled"` already meant unfiltered). Real policy design is a follow-up, not part of this fix. | y — confirmed by user 2026-08-25 |
| Fix shape for Bug A | Remove the `h.prov.Apply` call from `UpdateApp` entirely — no table reconciliation of any kind on this endpoint | `AppRequestBody` has no path to carry table changes; investigation confirmed `Apply` is additive-only/idempotent (never drops), so no legitimate case observed today depends on this side effect; the MCP path already works this way | y — derived from code investigation, not a product decision |
| Repair script delivery | A standalone reviewed `.sql` file (`.specs/features/app-update-schema-drift-fix/repair.sql`) with a runbook in this spec — not a Go migration, not wired into any command or startup path | Matches "manually-reviewed one-off migration" framing; keeps the audit trail with the spec that diagnosed it, avoids adding a permanent migration-runner surface for a single historical incident | y — matches user's explicit ask |
| Non-numeric row handling in the repair script | The script validates every row of each target column parses as numeric **before** altering that column; if any row fails, that column's `ALTER` is skipped (not run) and the script reports which rows failed for manual resolution | A blind `ALTER COLUMN ... TYPE numeric USING ...` on a column with any non-numeric text value fails the whole statement (or worse, coerces bad data) — failing closed on a per-column basis is safer than an all-or-nothing script | y — derived from the risk the user's own ask named ("safe conversion check") |
| Idempotency of the repair script | Script is safe to re-run: a column already `numeric` or a table already `rls=''` is a no-op, not an error | The person running this by hand may need to retry after fixing a flagged bad row in one column — the script shouldn't force re-deriving which parts already succeeded | y — standard ops-script hygiene, not a product decision |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: Dashboard save no longer fails on unrelated schema drift ⭐ MVP

**User Story**: As a dashboard admin, I want saving the Login/Storage/API tab of an app to persist only what I changed, so that an unrelated table's schema drift elsewhere in the same app can never block my save.

**Why P1**: This is the live-blocking bug — `internal-portal-rh` cannot be administered at all today because of it, and any other app that develops similar drift in the future would hit the exact same wall.

**Acceptance Criteria**:

1. WHEN an admin submits `PUT /dashboard/api/apps/{id}` with any combination of `name`, `auth_email_enabled`, `auth_providers`, `storage_config`, `rate_limit` THEN the system SHALL persist those fields exactly as it does today, WITHOUT invoking `h.prov.Apply` to reconcile any table, column, or index on that app.
2. WHILE an app has one or more tables with schema drift (config type differs from physical Postgres type) or an invalid legacy `rls` value, WHEN an admin submits the same `PUT /dashboard/api/apps/{id}` request THEN the system SHALL succeed exactly as it would on an app with no drift — the request SHALL NOT surface any provisioner/validation error naming a table or column the request did not reference.
3. The system SHALL continue to reconcile table schema exclusively through the existing per-table endpoints (`POST/PUT /dashboard/api/apps/{id}/tables/...`), unchanged by this fix.
4. The system SHALL continue to record the same `audit_log` entries `UpdateApp` already produces today (no change to auditing behavior).

**Independent Test**: Seed an app with a table whose physical column type deliberately diverges from its configured type (or an `rls` value outside `config.ValidRLS`'s accepted set), then call `PUT /dashboard/api/apps/{id}` toggling only `auth_email_enabled`; confirm it succeeds today's-blocked case now passes, and confirm no DDL/reconciliation query runs against that table (e.g., via a test double or query-count assertion on the provisioner).

---

### P2: internal-portal-rh's known drift is repaired via a reviewed runbook

**User Story**: As the person operating Zeep Orbit in production, I want a reviewed, safe SQL script that fixes `internal-portal-rh`'s specific known drift, so that I can restore that app's saveability and correct its column types without risking a blind, all-or-nothing `ALTER`.

**Why P2**: The code fix (P1) stops new instances of this failure mode but does not repair data already corrupted during the historical `pgType()` bug window — `internal-portal-rh` needs its own repair regardless of P1 shipping.

**Acceptance Criteria**:

1. WHEN the repair script's validation query runs for one of the 7 target columns (`engagement_entries.percentage`, `engagement_entries.participation`, `nps_entries.people_note`, `dp_indicators.glassdoor_rating`, `dp_indicators.turnover_total_2025`, `vacancies.time_to_start`, `vacancies.offer_decline_count`) THEN it SHALL list every row whose current `TEXT` value does not parse as a valid Postgres `numeric` literal (via a safe cast probe, not a blind `ALTER`).
2. IF a column's validation query returns at least one non-parseable row THEN the script SHALL NOT run `ALTER COLUMN ... TYPE numeric` for that column — that column is skipped, its offending rows are reported, and the operator resolves them manually before re-running the script.
3. WHEN a column's validation query returns zero non-parseable rows THEN the script SHALL run `ALTER TABLE ... ALTER COLUMN ... TYPE numeric USING ...` for that column only.
4. WHEN the RLS portion of the script runs THEN it SHALL set `rls = ''` for exactly the 8 named tables (`collaborators`, `layoffs`, `engagement_entries`, `pdi_entries`, `nps_entries`, `dp_indicators`, `vacancies`, `user_roles`) scoped to the `internal-portal-rh` app's `app_id`, and SHALL NOT touch any `zeep_system.app_tables` row outside that set.
5. The system SHALL make the script idempotent: re-running it after a partial success (some columns fixed, one skipped) SHALL re-validate and only act on columns/tables still in a drifted state.
6. The script SHALL NOT be invoked by any Go code, CLI command, startup hook, or CI job — delivered as a standalone `.sql` file with a runbook (order of operations, expected output, rollback note) for manual execution.

**Independent Test**: Run the script's validation query alone (no `ALTER`) against a copy of `internal-portal-rh`'s data and confirm it reports the expected 7 columns' row-level parseability with zero false positives; run the full script against that copy and confirm the 4 tables' 7 columns become physically `numeric` and the 8 tables' `rls` become `''`, with zero rows affected outside the named scope.

---

## Edge Cases

- IF a column's validation finds a non-numeric row (e.g., `"N/A"`, empty string, stray text) THEN the script SHALL report the row's primary key and raw value, not just a count, so the operator can decide the correct manual fix.
- IF the script is re-run after all columns/tables are already repaired THEN it SHALL report "nothing to do" for each, not re-run any `ALTER`/`UPDATE` that would be a no-op error (e.g., `ALTER COLUMN` on an already-`numeric` column).
- WHEN `PUT /dashboard/api/apps/{id}` is called for an app with zero tables THEN the system SHALL behave exactly as before this fix (no regression for the empty-tables case, since `Apply` was already a no-op there).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| AUSD-01 | P1: Dashboard save no longer fails on unrelated schema drift | Execute | Verified |
| AUSD-02 | P1: Dashboard save no longer fails on unrelated schema drift | Execute | Verified |
| AUSD-03 | P1: Dashboard save no longer fails on unrelated schema drift | Execute | Verified |
| AUSD-04 | P1: Dashboard save no longer fails on unrelated schema drift | Execute | Verified |
| AUSD-05 | P2: internal-portal-rh's known drift is repaired via a reviewed runbook | Execute | Verified |
| AUSD-06 | P2: internal-portal-rh's known drift is repaired via a reviewed runbook | Execute | Verified |
| AUSD-07 | P2: internal-portal-rh's known drift is repaired via a reviewed runbook | Execute | Verified |
| AUSD-08 | P2: internal-portal-rh's known drift is repaired via a reviewed runbook | Execute | Verified |
| AUSD-09 | P2: internal-portal-rh's known drift is repaired via a reviewed runbook | Execute | Verified |
| AUSD-10 | P2: internal-portal-rh's known drift is repaired via a reviewed runbook | Execute | Verified |

**ID format:** `AUSD-[NUMBER]` (app-update-schema-drift-fix)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 10 total, 10 mapped across 2 P1/P2 stories, 0 unmapped, 10 verified

---

## Success Criteria

- [x] Toggling any Login/Storage/API-tab field on `internal-portal-rh` (or any app) succeeds regardless of that app's table schema state.
- [ ] `internal-portal-rh`'s 7 columns are physically `numeric` and its 8 tables' `rls` is `''`, verified by direct read-only SQL after the runbook runs. **Not yet done** — the runbook is delivered and verified against a throwaway fixture DB, but running it against real production is an explicit separate go-ahead (AGENTS.md §8), not part of this Execute pass.
- [x] Zero rows outside the named 4 tables / 8 tables are touched by the repair script (verified via scoped WHERE clauses + fixture-DB test run).
- [x] No application code path can trigger the repair script — it exists only as a file for manual execution.
