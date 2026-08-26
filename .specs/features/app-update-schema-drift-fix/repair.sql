-- internal-portal-rh drift repair runbook
-- Spec: .specs/features/app-update-schema-drift-fix/spec.md (AUSD-05..10)
--
-- MANUAL EXECUTION ONLY. Nothing in this repo calls this file — no Go code,
-- CLI command, startup hook, or CI job. Run each block by hand, in order,
-- reviewing the SELECT output before running the next block. Safe to
-- re-run in full after a partial run: every statement only acts on rows
-- still in a drifted state (see "Idempotency" note on each block).
--
-- Root cause (already fixed in code, commit a8cfd40, 2026-07-29): between
-- 2026-06-27 and 2026-07-29, provisioner.pgType() had no "numeric" case, so
-- a column configured as "numeric" was physically created as TEXT. The
-- "internal-portal-rh" app's tables below were created/altered inside that
-- window and never touched since. The app also carries a pre-RLSP-09
-- legacy rls value ("disabled") that predates config.ValidRLS's current
-- enum ("", "owner", "enabled", "policy").
--
-- Schema note: schemaNameForDB("internal-portal-rh") = "internal_portal_rh"
-- (hyphens -> underscores, internal/dashboard/handler.go:2229).

-- ============================================================================
-- PART 1 — RLS repair (run first; low risk, single scoped UPDATE)
-- ============================================================================

-- 1a. Preview: confirm exactly the 8 expected tables, nothing more, nothing
-- less, before writing anything.
SELECT t.name AS table_name, t.rls
FROM zeep_system.app_tables t
JOIN zeep_system.apps a ON a.id = t.app_id
WHERE a.name = 'internal-portal-rh'
  AND t.rls NOT IN ('', 'owner', 'enabled', 'policy')
ORDER BY t.name;
-- Expected: collaborators, layoffs, engagement_entries, pdi_entries,
-- nps_entries, dp_indicators, vacancies, user_roles — all rls = 'disabled'.
-- If this returns anything else (different table names, different rls
-- value, or a different row count), STOP and re-diagnose before proceeding.

-- 1b. Repair. Idempotent: the WHERE clause only ever matches rows still
-- outside the valid enum, so re-running this after it already succeeded
-- once affects 0 rows, not an error.
UPDATE zeep_system.app_tables t
SET rls = ''
FROM zeep_system.apps a
WHERE a.id = t.app_id
  AND a.name = 'internal-portal-rh'
  AND t.name IN (
    'collaborators', 'layoffs', 'engagement_entries', 'pdi_entries',
    'nps_entries', 'dp_indicators', 'vacancies', 'user_roles'
  )
  AND t.rls NOT IN ('', 'owner', 'enabled', 'policy');

-- 1c. Verify: should return 0 rows.
SELECT t.name, t.rls
FROM zeep_system.app_tables t
JOIN zeep_system.apps a ON a.id = t.app_id
WHERE a.name = 'internal-portal-rh'
  AND t.rls NOT IN ('', 'owner', 'enabled', 'policy');

-- NOTE (spec Out of Scope): rls='' restores the same unfiltered-access
-- behavior 'disabled' already had today — it does NOT add row-level
-- filtering. These 8 tables have no real per-row access control after this
-- step. Designing actual RLS policies (owner/enabled/policy + clauses) for
-- HR data this sensitive is a separate, deliberately deferred follow-up —
-- confirmed with the user 2026-08-25. Do not treat this step as a security
-- fix, only as an unblock-the-save fix.

-- ============================================================================
-- PART 2 — Column type repair (run per column; each block is independent)
-- ============================================================================
--
-- Pattern for each of the 7 columns below:
--   (a) Validate: list any row whose TEXT value does NOT parse as numeric.
--       A value passes if it is NULL, or matches ^\s*-?[0-9]+(\.[0-9]+)?\s*$
--       (optionally signed integer or decimal — adjust the regex first if a
--       column is expected to hold scientific notation or other numeric
--       formats not seen during diagnosis).
--   (b) If (a) returns 0 rows, run the ALTER for that column only.
--   (c) If (a) returns >0 rows, DO NOT run the ALTER for that column.
--       Resolve the listed rows manually (fix the value, or decide on a
--       NULL/blank convention), then re-run (a) for that column before
--       trying again.
--
-- Idempotency: ALTER COLUMN ... TYPE numeric on a column already numeric is
-- a no-op in Postgres (succeeds trivially) — safe to re-run this whole
-- section after a partial success on other columns. The validation probes
-- above cast to ::text explicitly (rather than relying on the column's
-- current type) precisely so they stay re-runnable once a column has
-- already been converted: `numeric !~ '...'` errors ("operator does not
-- exist"), `numeric::text !~ '...'` does not.

-- --- engagement_entries.percentage ---
SELECT id, percentage
FROM internal_portal_rh.engagement_entries
WHERE percentage IS NOT NULL
  AND percentage::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.engagement_entries
--   ALTER COLUMN percentage TYPE numeric USING percentage::numeric;

-- --- engagement_entries.participation ---
SELECT id, participation
FROM internal_portal_rh.engagement_entries
WHERE participation IS NOT NULL
  AND participation::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.engagement_entries
--   ALTER COLUMN participation TYPE numeric USING participation::numeric;

-- --- nps_entries.people_note ---
SELECT id, people_note
FROM internal_portal_rh.nps_entries
WHERE people_note IS NOT NULL
  AND people_note::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.nps_entries
--   ALTER COLUMN people_note TYPE numeric USING people_note::numeric;

-- --- dp_indicators.glassdoor_rating ---
SELECT id, glassdoor_rating
FROM internal_portal_rh.dp_indicators
WHERE glassdoor_rating IS NOT NULL
  AND glassdoor_rating::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.dp_indicators
--   ALTER COLUMN glassdoor_rating TYPE numeric USING glassdoor_rating::numeric;

-- --- dp_indicators.turnover_total_2025 ---
SELECT id, turnover_total_2025
FROM internal_portal_rh.dp_indicators
WHERE turnover_total_2025 IS NOT NULL
  AND turnover_total_2025::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.dp_indicators
--   ALTER COLUMN turnover_total_2025 TYPE numeric USING turnover_total_2025::numeric;

-- --- vacancies.time_to_start ---
SELECT id, time_to_start
FROM internal_portal_rh.vacancies
WHERE time_to_start IS NOT NULL
  AND time_to_start::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.vacancies
--   ALTER COLUMN time_to_start TYPE numeric USING time_to_start::numeric;

-- --- vacancies.offer_decline_count ---
SELECT id, offer_decline_count
FROM internal_portal_rh.vacancies
WHERE offer_decline_count IS NOT NULL
  AND offer_decline_count::text !~ '^\s*-?[0-9]+(\.[0-9]+)?\s*$';
-- If 0 rows above, run:
-- ALTER TABLE internal_portal_rh.vacancies
--   ALTER COLUMN offer_decline_count TYPE numeric USING offer_decline_count::numeric;

-- ============================================================================
-- PART 3 — Final verification (run after all ALTERs above)
-- ============================================================================

-- Should return 0 rows: every configured "numeric" column now matches its
-- physical type. Uses IS DISTINCT FROM (not NOT IN) so a column that went
-- missing entirely (LEFT JOIN finds no information_schema row, c.data_type
-- IS NULL) is correctly flagged too — `NULL NOT IN (...)` is NULL/unknown
-- in SQL and would silently hide that case instead of reporting it.
SELECT
  a.name AS app_name,
  t.name AS table_name,
  col->>'name' AS column_name,
  col->>'type' AS configured_type,
  c.data_type AS physical_type
FROM zeep_system.app_tables t
JOIN zeep_system.apps a ON a.id = t.app_id
CROSS JOIN LATERAL jsonb_array_elements(t.columns) AS col
LEFT JOIN information_schema.columns c
  ON c.table_schema = 'internal_portal_rh'
  AND c.table_name = t.name
  AND c.column_name = col->>'name'
WHERE a.name = 'internal-portal-rh'
  AND col->>'type' = 'numeric'
  AND c.data_type IS DISTINCT FROM 'numeric';

-- Should return 0 rows: none of the 8 tables this runbook scoped in Part 1
-- has an invalid rls value. Scoped to the same 8 names (not every table on
-- the app) to match AUSD-08 exactly — a 9th, unrelated table with its own
-- legacy rls value is a different incident, not a failure of this repair.
SELECT t.name, t.rls
FROM zeep_system.app_tables t
JOIN zeep_system.apps a ON a.id = t.app_id
WHERE a.name = 'internal-portal-rh'
  AND t.name IN (
    'collaborators', 'layoffs', 'engagement_entries', 'pdi_entries',
    'nps_entries', 'dp_indicators', 'vacancies', 'user_roles'
  )
  AND t.rls NOT IN ('', 'owner', 'enabled', 'policy');
