# orbit_create_policy_advanced Validation

**Date**: 2026-08-25
**Spec**: `.specs/features/mcp-advanced-policy-tool/spec.md`
**Diff range**: `4f834b2..ee6b0e5` (3 commits: `bc445aa` mapWriteError fix, `78706f3` tool, `ee6b0e5` docs)
**Verifier**: independent sub-agent (author ≠ verifier)

> Note on the range: the range supplied to the Verifier was `bc445aa..ee6b0e5`, which *excludes* `bc445aa` itself (the `mapWriteError` fix + `maperror_test.go`). The full feature surface is `4f834b2..ee6b0e5`; this report validates all three commits.

---

## Task Completion

No `tasks.md` exists for this feature (Tasks phase was skipped — small, 3-commit surgical change). Completion is tracked against the design's stated work items.

| Work item (design.md) | Status | Notes |
| --- | --- | --- |
| `mapWriteError` case for `*provisioner.ValidationError` (design Risks & Concerns, "first Execute task") | Done | `internal/mcpserver/tools.go:180`, `:222-223`; test `internal/mcpserver/maperror_test.go:17` |
| `orbitCreatePolicyAdvancedInput` / `...ClauseInput` types | Done | `internal/mcpserver/tools.go:634`, `:648` |
| `toDashboardPolicyDefAdvanced` converter | Done | `internal/mcpserver/tools.go:664` |
| `registerAdvancedPolicyTool` + wiring into `RegisterTools` | Done | `internal/mcpserver/tools.go:689`; registered at `internal/mcpserver/tools.go:73` |
| CHANGELOG entry (`Added` + `Fixed` under `[Unreleased]`) | Done | `CHANGELOG.md` (commit `ee6b0e5`) |
| Spec traceability set to Verified | Done | `.specs/features/mcp-advanced-policy-tool/spec.md:73-78` |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| **MAPT-01** — WHEN a `CanManage()` caller calls the tool with a valid `PolicyDef` (multi-clause, first clause no `logic`, later clauses `AND`/`OR`) THEN create via `CreateTablePolicyForUser` and return the created `TablePolicyRow` incl. generated `id` and `pg_policy_name` | Policy created; returned row carries `pg_policy_name` = submitted name, `action` = submitted action, both clauses persisted, and a generated `id` | `internal/mcpserver/tools_advanced_policy_test.go:58` — `if out.Name != "adv_chained_select" \|\| out.Action != "select"` (decodes `json:"pg_policy_name"`); `:61` — `if len(out.Clauses) != 2`; `:69` — `if len(schema.Tables[0].Policies) != 1` (independently re-read via `GetAppSchemaForUser`, proving persistence, not just the return value). Two-clause input with `logic: "OR"` on the second clause: `:36-39` | ⚠️ PASS with sub-gap — the generated `id` the spec names explicitly is never decoded or asserted (the local `out` struct at `:51-56` omits `id`) |
| **MAPT-02** — WHEN the policy is created THEN record the action in `audit_log` | Audit row written, same as the REST endpoint / template tool | no test evidence in this feature's diff | ⚠️ Not independently covered — explicitly waived by the spec itself (`spec.md:92`: "inherited from `CreateTablePolicyForUser`, not independently tested by this feature"). Structurally guaranteed: the tool's only write path is `CreateTablePolicyForUser` (`internal/mcpserver/tools.go:698`), which audits at `internal/dashboard/handler.go:1950` |
| **MAPT-03** — IF the caller lacks `role.CanManage()` THEN return a forbidden error AND create no policy row or DDL | Fixed message `"forbidden"`; zero policies on the table afterwards | `internal/mcpserver/tools_advanced_policy_test.go:110` — `if !ok \|\| text.Text != "forbidden"` (exact string); `:118` — `if len(schema.Tables[0].Policies) != 0` | ✅ PASS |
| **MAPT-04** — IF any field fails `provisioner.BuildPolicySQL` validation THEN return the mapped, fixed-message error (never a raw `err.Error()`) AND create nothing | Spec does not name the exact message text; it requires "not the generic internal error, not a raw leak" | `internal/mcpserver/tools_advanced_policy_test.go:162` — `if text.Text == errInternal.Error() { t.Fatalf(...) }`; `:159` — non-empty message; `:170` — `if len(schema.Tables[0].Policies) != 0`. Helper-level: `internal/mcpserver/maperror_test.go:32` — `mapped.Error() == errInternal.Error()` rejected; `:35` — `mapped.Error() != err.Error()` rejected (verbatim `ValidationError` required) | ⚠️ Spec-precision gap — spec defines no exact message string, so the assertion can only be "≠ generic internal error". Also only 1 of the 8 listed failure modes (unknown column) is exercised at the MCP layer; the other 7 are covered one layer down (see Edge Cases) |
| **MAPT-05** — IF `name` collides on the same table+action THEN return `ErrPolicyAlreadyExists` AND create no duplicate | Exactly `dashboard.ErrPolicyAlreadyExists.Error()`; table still holds exactly the 1 pre-existing policy | `internal/mcpserver/tools_advanced_policy_test.go:221` — `if !ok \|\| text.Text != dashboard.ErrPolicyAlreadyExists.Error()` (exact, sourced from the sentinel itself); `:229` — `if len(schema.Tables[0].Policies) != 1` | ✅ PASS |
| **MAPT-06** — The system SHALL NOT accept any field carrying raw SQL; every field stays `column`/`operator`/`value_source`/`value`/`logic` | Input schema contains exactly those 5 clause fields, no SQL/expression field | Compile-enforced by the type, not a runtime test: `internal/mcpserver/tools.go:634-640` — `orbitCreatePolicyAdvancedClauseInput` declares exactly `Column`, `Operator`, `ValueSource`, `Value`, `Logic`; `internal/mcpserver/tools.go:648-655` — outer input declares only `app_id`, `table_name`, `name`, `action`, `roles`, `clauses`. Converter `internal/mcpserver/tools.go:664-681` copies those 5 fields and nothing else | ⚠️ PASS by construction — verified by code inspection; no test asserts the tool's published JSON schema, so a future field addition would not be caught by the suite |

**Status**: ⚠️ All 6 ACs traced to evidence; 3 flagged (1 sub-gap on MAPT-01's `id`, 1 spec-waived on MAPT-02, 1 spec-precision on MAPT-04, MAPT-06 structural). No AC is uncovered and none failed.

---

## Discrimination Sensor

Scratch isolation: temporary `git worktree add <scratch> HEAD`, mutated and run there, then `git worktree remove --force`. Real tree never touched (`git stash` not used). Baseline `git status --porcelain` = empty before and after (verified).

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/mcpserver/tools.go:671` | Field swap in `toDashboardPolicyDefAdvanced`: `Value: c.Value` → `Value: c.Column` | ✅ Killed — `TestOrbitCreatePolicyAdvanced_ChainedClauses` + `..._DuplicateNameReturnsAlreadyExists` fail |
| 2 | `internal/mcpserver/tools.go:222-223` | Removed the new `mapWriteError` case `case errors.As(err, &policyValErr): return policyValErr` (falls back to `internalErr`) | ✅ Killed — `TestMapWriteError_ProvisionerValidationErrorSurfacedVerbatim` + `..._UnknownColumnReturnsSpecificError` fail |
| 3 | `internal/mcpserver/tools.go:700` | Tool handler stops mapping: `return nil, nil, mapWriteError(err)` → `return nil, nil, errInternal` | ✅ Killed — `..._ForbiddenWithoutManageRole`, `..._UnknownColumnReturnsSpecificError`, `..._DuplicateNameReturnsAlreadyExists` all fail |
| 4 | `internal/mcpserver/tools.go:676` | Wrong-field return in `toDashboardPolicyDefAdvanced`: `Name: in.Name` → `Name: in.TableName` | ✅ Killed — `..._ChainedClauses` + `..._DuplicateNameReturnsAlreadyExists` fail |

**Sensor depth**: lightweight (4 behavior-level mutations; feature is a thin transport, not a P0 path — though the auth tier it rides on is, and mutation 3 exercises it)
**Result**: 4/4 killed — PASS ✅

Isolation check: `git status --porcelain` on the real worktree was empty before the sensor and empty after `git worktree remove --force` + `git worktree prune`. Sensor run valid.

---

## Interactive UAT Results

Not performed — backend/MCP-transport-only feature with no UI surface (validate.md §3: automated checks are sufficient for backend-only work).

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code (76 added lines in `tools.go` + 3 for the helper fix, no new package/type beyond the wire struct) | ✅ |
| Surgical changes (only `internal/mcpserver/tools.go` touched in production code; 2 new test files; `CHANGELOG.md`) | ✅ |
| No scope creep (no `orbit_update_policy_advanced`, no new auth tier, no new validation — all matching spec Out of Scope) | ✅ |
| Matches patterns (`registerAdvancedPolicyTool` mirrors `registerTemplateTools` shape; `"mcp"` ip provenance string reused; `errUnauthorized` gate identical to every other write tool) | ✅ |
| No abstractions for single-use code (converter is a flat 1:1 copy, no interface/generics) | ✅ |
| Didn't "improve" unrelated code — the one non-feature edit (`mapWriteError`) is a design-declared blocking prerequisite, committed separately with its own test | ✅ |
| Spec-anchored outcome check (asserted values match spec-defined outcome) | ⚠️ 3 flagged, see AC table |
| Per-layer Coverage Expectation met (transport layer: happy + forbidden + validation-error + conflict paths all covered for the single route in scope) | ✅ |
| Every test maps to a spec requirement — no unclaimed tests (each of the 4 new tests names its MAPT id in its doc comment; `maperror_test.go` names the design.md gap) | ✅ |
| AGENTS.md §4 respected: no raw `err.Error()` leak — `*provisioner.ValidationError` is a typed, pre-vetted error, same trust level already given to `TypeChangeError`/`ForeignKeyViolationError` | ✅ |
| Documented guidelines followed: `AGENTS.md` (§2 commits, §3 gate, §4 backend error rules, §6 CHANGELOG) | ✅ |

Minor, non-blocking observation: `README.md:386` (and the 3 translations) list the exposed MCP tools and were not updated with `orbit_create_policy_advanced`. That list is already stale from earlier features (`orbit_list_table_policies`, `orbit_list_webhooks`, `orbit_update_app` etc. are absent too), so this feature does not *introduce* the drift — but AGENTS.md §6 makes README feature tables a sync target for genuinely new user-facing surfaces, and a new MCP tool qualifies.

---

## Edge Cases

Spec's edge cases are all validation rules owned by `provisioner.BuildPolicySQL`. The tool is a declared pure transport, so they are covered one layer down by pre-existing tests, plus one end-to-end proof at the MCP layer (unknown column) that the mapping actually reaches the caller.

- [x] Empty `clauses` array → `internal/provisioner/policy_test.go` (`BuildPolicySQL` rejects); MCP-layer mapping proven by `tools_advanced_policy_test.go:162`
- [x] First clause with `logic` set / non-first clause with bad `logic` → `internal/provisioner/policy_test.go:483`, `:498`, `:514`
- [x] `IS NULL`/`IS NOT NULL` with an operand → `internal/provisioner/policy_test.go:372`
- [x] `value_source: claim` with a claim outside `role`/`sub`/`email` → `internal/provisioner/policy_test.go:203`; allowed set `:218`
- [x] RLS enabled on first policy for a table with zero policies → exercised implicitly: `tools_advanced_policy_test.go:24` provisions a fresh table and `:69` confirms the first policy lands; the enable-on-first behavior itself is `CreateTablePolicy`'s pre-existing, already-tested idempotent path

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && gofmt -l internal/mcpserver/*.go` then `go test ./internal/mcpserver/...` (env: `TEST_DATABASE_URL`, `DASHBOARD_BOOTSTRAP_SECRET`)
- **Result**: 70 passed, 0 failed, 0 skipped (`ok github.com/zeeplabs/zeep-orbit/internal/mcpserver 22.9s`)
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l internal/mcpserver/*.go` — no output (nothing unformatted)
- **Test count before feature**: 65 in `internal/mcpserver`
- **Test count after feature**: 70
- **Delta**: +5 (4 in `tools_advanced_policy_test.go`, 1 in `maperror_test.go`)
- **Skipped tests**: none
- **Failures**: none
- **Test integrity**: no test deleted, no assertion weakened in the diff range; the only edit to existing code is additive (`mapWriteError` gained one case)

---

## Fix Plans

No blocking issues. Three optional, ranked hardening items (all Minor — none blocks the feature):

### Fix 1 (Minor): MAPT-01's generated `id` is asserted nowhere

- **Root cause**: the anonymous decode struct at `tools_advanced_policy_test.go:51-56` omits `id`, so a handler returning a row with an empty/zero id would still pass.
- **Fix task**: add `ID string \`json:"id"\`` to the decode struct and assert it is non-empty.
- **Priority**: Minor

### Fix 2 (Minor): MAPT-04 only exercises one of eight validation failure modes at the MCP layer

- **Root cause**: a single table-driven test would cheaply cover bad operator, bad claim, bad `logic` placement, empty `roles`, empty `clauses`, and bad `action` through the tool; today only "unknown column" travels the full transport.
- **Fix task**: convert `..._UnknownColumnReturnsSpecificError` into a table test over those inputs, asserting each returns a non-generic message and creates nothing. (Low value if you accept the "provisioner owns validation" argument — the mapping is already proven once.)
- **Priority**: Minor

### Fix 3 (Minor): README tool list not updated

- **Root cause**: pre-existing drift in `README.md:386` + `i18n/README.{pt-BR,pt-PT,es}.md`; this feature continues it rather than causing it.
- **Fix task**: refresh the "Tools exposed" bullet in all 4 READMEs to the current registered tool set (AGENTS.md §6 requires all 4 in the same change).
- **Priority**: Minor

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| MAPT-01 | Verified (claimed by author) | ✅ Verified (sub-gap: `id` unasserted) |
| MAPT-02 | Verified (claimed by author) | ⚠️ Verified by construction — no test, waived in `spec.md:92` |
| MAPT-03 | Verified (claimed by author) | ✅ Verified |
| MAPT-04 | Verified (claimed by author) | ⚠️ Verified with spec-precision gap (no exact message defined) |
| MAPT-05 | Verified (claimed by author) | ✅ Verified |
| MAPT-06 | Verified (claimed by author) | ✅ Verified structurally (type-enforced, no test) |

No change needed to `spec.md`'s traceability table — all 6 remain Verified.

---

## Summary

**Overall**: ✅ Ready (with 3 Minor, non-blocking hardening items)

**Spec-anchored check**: 6/6 ACs traced to `file:line` evidence; 3 flagged (1 spec-waived, 1 spec-precision, 1 structural-only) — 0 uncovered, 0 failed
**Sensor**: 4/4 mutations killed
**Gate**: 70 passed, 0 failed, 0 skipped; build/vet/gofmt clean

**What works**:
- The tool is a genuine pure transport: single call to `CreateTablePolicyForUser` (`internal/mcpserver/tools.go:698`), zero duplicated validation — spec Goal #2 and Success Criteria #2 hold under code review.
- Auth, conflict, and validation-error paths each assert an exact expected string and independently re-read the table to prove nothing was created — not shallow "an error happened" assertions.
- The `mapWriteError` gap flagged in `design.md` was fixed first, in its own commit, with its own regression test — the sequence design.md prescribed.
- The sensor confirms the suite discriminates on all four high-risk behaviors: field mapping, name mapping, error mapping, and the new error case.

**Issues found**: three Minor items above — unasserted `id`, single validation failure mode at MCP layer, stale README tool list.

**Next steps**: none required to close the feature. Optionally fold Fix 1 and Fix 3 into the next change touching this area.
