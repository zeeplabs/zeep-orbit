# MCP Safe Mutation Tools Validation

**Date**: 2026-08-17
**Spec**: `.specs/features/mcp-safe-mutation-tools/spec.md`
**Diff range**: `4b54100^..8e52c1c` (first code commit `4b54100` "AddTableColumnForUser" through last code commit `8e52c1c` "orbit_save_webhook_event_mapping")
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `AddTableColumnForUser` + `ErrColumnAlreadyExists`, `internal/dashboard/handler.go:1433-1509` |
| T2   | ✅ Done | `orbit_add_table_column` tool, `internal/mcpserver/tools.go:239-273` |
| T3   | ✅ Done | `AddTableIndexForUser` + `ErrIndexAlreadyExists`, `internal/dashboard/handler.go:1512-1580` |
| T4   | ✅ Done | `orbit_add_table_index` tool + blocking-write disclosure, `internal/mcpserver/tools.go:281-293` |
| T5   | ✅ Done | `CreateWebhookForUser` + REST refactor, `internal/dashboard/webhooks_store.go:288-327`, `webhooks_handler.go:112-150` |
| T6   | ✅ Done | `orbit_create_webhook` tool, `internal/mcpserver/tools.go:958-975` |
| T7   | ✅ Done | `SaveEventMappingForUser` + REST refactor, `internal/dashboard/webhooks_store.go:329-349`, `webhooks_handler.go:372-419` |
| T8   | ✅ Done | `orbit_save_webhook_event_mapping` tool, `internal/mcpserver/tools.go:984-1004` |

All 8 tasks checked `[x]` in `tasks.md`. No partial/blocked tasks found.

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | --------------------- | ----------------------- | ------ |
| MSMT-01: add column, others untouched | Table ends with N+1 columns, prior N byte-identical | `internal/dashboard/apps_table_column_foruser_test.go:38-58` — asserts `len(updated.Columns)==2`, `Columns[0]` name/type unchanged, then a 2nd add → `len==3` (the exact two-calls-in-a-row regression from spec Success Criteria) | ✅ PASS |
| MSMT-02: valid `references` creates FK | New column carries the reference | `internal/dashboard/apps_table_column_foruser_test.go:86-104` — asserts `c.References.Table == "categories"` | ✅ PASS |
| MSMT-03: duplicate column name rejected, table untouched | `ErrColumnAlreadyExists`, no mutation | `internal/dashboard/apps_table_column_foruser_test.go:125-140` — `errors.Is(err, ErrColumnAlreadyExists)`, re-fetch confirms `len(Columns)==1` | ✅ PASS |
| MSMT-04: bad `references` target rejected, table untouched | Same `*ValidationError` shape `config.ValidateTables` produces at table-creation time | `internal/dashboard/apps_table_column_foruser_test.go:161-177` — `errors.As(err, &valErr)`, re-fetch confirms `len(Columns)==1` | ✅ PASS |
| MSMT-05: `CanWrite()` failure / not-found | Same forbidden/not-found `UpdateTableRLSModeForUser` returns | `apps_table_column_foruser_test.go:187-192` (`ErrForbidden` for viewer role) and `:202-207` (`ErrNotFound` for unknown table) | ✅ PASS |
| MSMT-06: audit log on success | `app.table_column.create` recorded | `apps_table_column_foruser_test.go:235-243` — `SELECT count(*) ... WHERE action='app.table_column.create'` == 1 | ✅ PASS |
| MSMT-07: add index, others untouched | Table ends with N+1 indexes, columns unchanged | `internal/dashboard/apps_table_index_foruser_test.go:40-51` — `len(Indexes)==2`, `len(Columns)==2` unchanged, original index name intact | ✅ PASS |
| MSMT-08: duplicate index name / unknown index column rejected, table untouched | 400 validation error, no mutation | `apps_table_index_foruser_test.go:76-87` (`ErrIndexAlreadyExists`, re-fetch `len(Indexes)==1`) and `:108-123` (`*ValidationError` for unknown column, re-fetch `len(Indexes)==0`) | ✅ PASS |
| MSMT-09: `CanWrite()` failure / not-found + audit | Same as column tool; `app.table_index.create` logged | `apps_table_index_foruser_test.go:132-137`, `:147-152`, `:179-188` | ✅ PASS |
| MSMT-10: create webhook via `CreateWebhook` validation; `CanManage()` tier; not-found | Same validation REST handler applies; **`CanManage()`**, not `CanWrite()` (design.md's confirmed tier correction) | `internal/dashboard/webhooks_create_foruser_test.go:43-54` (editor with `CanWrite()`-level access still `ErrForbidden` since role fails `CanManage()`), `:59-70` (`ErrNotFound` for outsider), `:75-87` (`*ValidationError` for bad method); MCP-level tier test at `internal/mcpserver/tools_create_webhook_test.go:57-98` (`"forbidden"` for an editor) | ✅ PASS |
| MSMT-11: save event mapping validation; unknown-target rejection; conflict rejection leaves first intact; cross-app scoping; `CanManage()` | Same `SaveEventMapping` validation; `ErrUnknownTargetTable`/`ErrUnknownTargetColumn`; `ErrMappingConflict` with first mapping surviving; not-found for cross-app `webhook_id`; `CanManage()` tier | `internal/dashboard/webhooks_save_mapping_foruser_test.go:55-64` (unknown table), `:68-99` (conflict — re-`ListEventMappings` confirms only first survives), `:103-133` (cross-app → `ErrWebhookNotFound`), `:137-153` (`CanManage()` via editor); MCP-level at `internal/mcpserver/tools_save_event_mapping_test.go:94-133` (unknown-target distinct error), `:137-183` (conflict distinct error), `:187-230` (cross-app → `"webhook not found"`), `:234-277` (`CanManage()` editor forbidden) | ✅ PASS |

**Status**: ✅ All 11 requirements (MSMT-01 through MSMT-11) covered with `file:line` evidence targeting the spec-defined outcome, not just "an assertion exists." No spec-precision gaps found — every AC names a precise expected value (specific sentinel error, specific count, specific audit action string) and every corresponding test asserts that exact value.

**Cross-checked design.md's central finding** (table tools use `role.CanWrite()`, webhook tools use the stricter `role.CanManage()`) directly in the source:
- `internal/dashboard/handler.go` — `AddTableColumnForUser`/`AddTableIndexForUser` both gate on `role.CanWrite()`.
- `internal/dashboard/webhooks_store.go:293` (`CreateWebhookForUser`) and `:334` (`SaveEventMappingForUser`) both gate on `role.CanManage()`.
Both tiers are tested with an explicit non-privileged-but-not-zero role (`appviewer`/viewer for `CanWrite()`, `editor` for `CanManage()`) rather than a blanket "no membership" case — matching tasks.md's explicit requirement to test the *specific* tier each tool enforces.

---

## Discrimination Sensor

Ran in an isolated git worktree (`git worktree add`, never `git stash`), one mutation at a time, real ephemeral Postgres, cleaned up and removed after. Baseline `git status --porcelain` on the real tree was empty before the sensor run and confirmed empty again after `git worktree remove --force` — isolation held.

| # | File:line | Mutation | Killed? |
| - | --------- | -------- | ------- |
| 1 | `internal/dashboard/handler.go:1472-1474` (`AddTableColumnForUser`) | Overwrote the merge (`mergedColumns := []config.ColumnConfig{col}`) instead of appending to `existingTable.Columns` — reintroduces the exact orphaned-column risk the spec exists to prevent | ✅ Killed — `TestAddTableColumnForUser_AddsColumnLeavesOthersUnchanged` failed: "expected 2 columns after add, got 1" |
| 2 | `internal/dashboard/handler.go:1538` (`AddTableIndexForUser`) | Disabled the duplicate-index-name check (`if false && existing.Name == idx.Name`) | ✅ Killed — `TestAddTableIndexForUser_DuplicateNameRejected` failed: "expected ErrIndexAlreadyExists, got \<nil\>" |
| 3 | `internal/dashboard/webhooks_store.go:293` (`CreateWebhookForUser`) | Swapped `role.CanManage()` → `role.CanWrite()` — the exact tier-confusion risk design.md flags as the central finding of this spec | ✅ Killed — `TestCreateWebhookForUser_NonManagerForbidden` failed: "expected ErrForbidden for an editor (CanManage()==false), got \<nil\>" |
| 4 | `internal/dashboard/webhooks_store.go:337` (`SaveEventMappingForUser`) | Removed cross-app scoping by passing `""` instead of `appID` to `GetWebhookByID` | ✅ Killed — `TestSaveEventMappingForUser_CrossAppWebhookReturnsNotFound` failed: expected `ErrWebhookNotFound`, got `"dashboard: unknown target table"` (webhook resolved across app boundaries, then failed downstream instead of at the intended not-found gate — test still correctly fails the mutant) |
| 5 | `internal/mcpserver/tools.go:180-181` (`mapWriteError`) | Removed the `ErrMappingConflict` case, falling through to the generic `errInternal` branch | ✅ Killed — `TestOrbitSaveWebhookEventMapping_ConflictReturnsDistinctError` failed: expected the passthrough conflict message, got `"internal error"` |

**Sensor depth**: lightweight (5 targeted behavior-level mutations, one per highest-risk new code path named in design.md's Risks & Concerns and the task prompt)
**Result**: 5/5 killed — PASS ✅

**Infrastructure note (not a mutation-signal issue)**: a concurrent, unrelated process (`.../scratchpad/verify-wt`, a separate git worktree present in this same session's scratchpad but not created by this Verifier run) was independently running `go test ./internal/dashboard/...` against the **same shared `TEST_DATABASE_URL`** during this session. Since dashboard/webhook test helpers do `DROP SCHEMA IF EXISTS zeep_system CASCADE` at setup, concurrent runs against a shared Postgres can transiently fail each other with `relation "zeep_system.app_members" does not exist`-style errors unrelated to any code defect. This was observed twice (once on an initial full-suite verbose run, once on the first attempt at mutation 4) and resolved cleanly on retry once serialized. Flagged here for the record, not treated as a gate failure — the full unmutated gate (below) was run standalone and passed cleanly.

---

## Code Quality

| Principle        | Status |
| ---------------- | ------ |
| Minimum code     | ✅ — each `*ForUser` function mirrors the exact established pattern (`UpdateTableRLSModeForUser` for table tools, `webhookRBACGate` composition for webhook tools); no speculative abstraction added |
| Surgical changes | ✅ — REST handler refactors (`CreateWebhook`, `SaveEventMapping`) are behavior-preserving extractions; existing REST tests pass unmodified |
| No scope creep   | ✅ — no `orbit_update_webhook`, no FK-to-existing-column tool, no `CONCURRENTLY` adoption — all correctly deferred per spec's Out of Scope |
| Matches patterns | ✅ — tool registration mirrors `orbit_create_table`; `mapWriteError` extended with the same passthrough-message convention already established |
| Spec-anchored outcome check (asserted values match spec) | ✅ — see table above, no spec-precision gaps |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ — each `*ForUser` function has a dedicated happy path, every distinct error branch, and (for table tools) the two-calls-in-a-row regression test; MCP tool tests re-verify the two confirmed-different auth tiers rather than assuming uniformity |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — every test function's doc comment cites the specific spec AC or design.md finding it covers |
| Documented guidelines followed | `AGENTS.md` §3 (gate commands), §4 (no raw internal errors in HTTP responses — confirmed: `h.prov.Apply` DB-level failures still route through `errAppTableInternalFailed`/`mapWriteError`'s generic `errInternal` branch, no raw Postgres string surfaced) |

---

## Edge Cases

- [x] `table_name` doesn't exist → not-found (`ErrNotFound`) for both column and index tools — tested
- [x] Provisioning fails after validation passes (FK apply-time race) → falls to generic `errAppTableInternalFailed`/`errInternal`, not a raw error string — code-reviewed (`handler.go`'s `fmt.Errorf("%w: %v", errAppTableInternalFailed, err)"` wrapping), no dedicated apply-time-failure test exists (this is the one narrow edge case in the Assumptions table describing a database-level race that is impractical to trigger deterministically in an integration test — not flagged as a gap, consistent with how the same edge case is handled elsewhere in this codebase)
- [x] Superadmin/`CanReadAnyApp` caller behaves like the REST equivalent — no MCP-specific restriction added; code inspection confirms `GetApp`+role-check path is identical to every existing REST/MCP write tool, not a new gate

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && go test ./... -race` (with `-p 1` used for the full suite to avoid cross-package Postgres schema races, per task instructions; `WEBHOOK_TOKEN_ENCRYPTION_KEY` set)
- **Result**: `go build ./...` clean, `go vet ./...` clean, `gofmt -l` on the full diff range's changed `.go` files: no output (all formatted), `go test ./... -race -p 1`: all packages `ok` (dashboard 95.8s, mcpserver 21.6s, rest cached/fast)
- **Test count before feature**: not independently measured (spec-only phase at `f902f38`/`717693b`, no test count baseline captured at that point in this verification pass)
- **Test count after feature**: 39 new test functions across the 8 new test files (`apps_table_column_foruser_test.go`, `apps_table_index_foruser_test.go`, `webhooks_create_foruser_test.go`, `webhooks_save_mapping_foruser_test.go`, `tools_add_table_column_test.go`, `tools_add_table_index_test.go`, `tools_create_webhook_test.go`, `tools_save_event_mapping_test.go`)
- **Delta**: +39 new tests, 0 deleted/weakened (existing `CreateWebhook`/`SaveEventMapping` REST handler tests pass unmodified post-refactor)
- **Skipped tests**: none observed
- **Failures**: none in the real (unmutated) tree

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status  |
| ----------- | ---------------- | ----------- |
| MSMT-01     | Pending           | ✅ Verified |
| MSMT-02     | Pending           | ✅ Verified |
| MSMT-03     | Pending           | ✅ Verified |
| MSMT-04     | Pending           | ✅ Verified |
| MSMT-05     | Pending           | ✅ Verified |
| MSMT-06     | Pending           | ✅ Verified |
| MSMT-07     | Pending           | ✅ Verified |
| MSMT-08     | Pending           | ✅ Verified |
| MSMT-09     | Pending           | ✅ Verified |
| MSMT-10     | Pending           | ✅ Verified |
| MSMT-11     | Pending           | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 11/11 ACs matched spec outcome, 0 spec-precision gaps
**Sensor**: 5/5 mutations killed
**Gate**: build clean, vet clean, gofmt clean, all packages `ok` under `-race`

**What works**: All four tools (`orbit_add_table_column`, `orbit_add_table_index`, `orbit_create_webhook`, `orbit_save_webhook_event_mapping`) are implemented exactly per design.md's fetch→merge→validate→apply→persist→audit shape for the table tools, and GetApp+CanManage()+scope→store-call→audit shape for the webhook tools. The core regression this spec exists to prevent — an agent adding a column/index without resending the rest of the table — is directly tested with a real two-calls-in-a-row assertion at both the dashboard-function layer and the MCP-tool-roundtrip layer. The `CanWrite()`/`CanManage()` tier split design.md calls out as its central finding is implemented correctly in the source and independently re-verified by the discrimination sensor (mutation 3).

**Issues found**: None requiring a fix task. One process-level observation (concurrent-worktree DB contention noise during sensor execution, documented above) — infra/session-isolation issue, not a code or test defect; does not affect the verdict since the gate and sensor were both confirmed clean on isolated/retried runs.

**Next steps**: None required. Feature ready to close out (spec.md traceability table update, tasks.md already fully `[x]`).
