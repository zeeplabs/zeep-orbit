# Add/Remove Foreign Key on an Existing Column Validation

## Validation: column-foreign-key - PASS ✅

**Result**: PASS ✅ — both gaps from prior iterations are confirmed closed. Iteration 1's surviving mutant (`DropColumnForeignKey` genuine-query-error path) remains fixed. Iteration 2's finding (CFK-04's dedicated test was non-discriminating because `_auth_users` was never physically provisioned) is fixed by `f187ea2`, which adds `AuthEmailEnabled: true` to the test's `CreateAppForUser` call. All 21 ACs are spec-anchored, the full 4-mutation discrimination sensor is green, and the full test gate passes.

**Date**: 2026-08-24
**Spec**: `.specs/features/column-foreign-key/spec.md`
**Diff range**: `76b37a0..HEAD` (HEAD = `f187ea2`)
**Verifier**: independent sub-agent (author ≠ verifier), **iteration 3 of 3** — bounded fix→re-verify loop closes here.

---

## Fix Commit Reviewed (since iteration 2)

| Commit | Claims to fix | Verdict |
| --- | --- | --- |
| `f187ea2` — test(column-foreign-key): make CFK-04 test provision `_auth_users` so it actually discriminates | Iteration 2's finding (CFK-04 test non-discriminating) | ✅ Confirmed — test now genuinely discriminates the `_auth_users`-requires-uuid enforcement |

**Change reviewed**: `internal/dashboard/apps_column_foreign_key_foruser_test.go:168` — `CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-au-typemis"), AuthEmailEnabled: true}, "127.0.0.1")`. This physically provisions `_auth_users` in the test's schema (gated by `internal/provisioner/provisioner.go:46-47`'s `app.Auth.Providers.Email` check in `Apply`), so `CheckForeignKeyColumnTypesMatch`'s `fetchExistingColumns` (`internal/provisioner/table.go:359`) now finds the real table instead of falling into the unrelated "table not found" branch.

---

## CFK-04 Discrimination Re-Confirmation (targeted check per iteration 2's fix task)

Isolated scratch: `git worktree add /tmp/cfk-sensor3 HEAD` (never `git stash`). Gitignored `internal/dashboard/static` build output copied into the scratch worktree only (required for `//go:embed`); real tree untouched throughout.

**Bypass applied**: both `_auth_users`-requires-uuid enforcement paths simultaneously —
- `internal/config/validate.go:144` (`if col.Type != "uuid"` → `if false`)
- `internal/provisioner/table.go:368` (`if sourceType != targetType` → `if false`)

**Result**: `TestAddColumnForeignKeyForUser_AuthUsersTargetTypeMismatchRejected` **now fails genuinely**:
```
apps_column_foreign_key_foruser_test.go:184: expected *ValidationError rejecting a non-uuid column
referencing _auth_users, got table: add foreign key on "...items"."owner_ref": ERROR: foreign key
constraint "items_owner_ref_fkey" cannot be implemented (SQLSTATE 42804) (*fmt.wrapError)
```
This is a real Postgres constraint rejection (SQLSTATE 42804, incompatible FK column types) surfacing because the application-level guard was bypassed — not the "table not found" short-circuit that made iteration 2's version of the test pass for the wrong reason. The test now exercises the exact code path CFK-04 requires.

Both bypasses reverted; scratch confirmed clean before proceeding to the full sensor run.

---

## Discrimination Sensor — Full Re-run of All 4 Original Mutations

| # | File:line | Mutation | Killed? (iter 1) | Killed? (iter 2) | Killed? (iter 3) |
| - | --- | --- | --- | --- | --- |
| 1 | `internal/dashboard/handler.go:1692` | Flipped already-has-FK check: `References != nil` → `== nil` | ✅ | ✅ | ✅ Killed — `TestAddColumnForeignKeyForUser_Success`, `_AlreadyHasReferenceRejected`, `_InvalidTargetRejected`, `_TypeMismatchRejected`, `_AuthUsersTargetTypeMismatchRejected`, `_OrphanedRowsRejected`, `_RecordsAuditLog` all fail |
| 2 | `internal/provisioner/table.go:368` | Flipped type-match comparison: `sourceType != targetType` → `sourceType == targetType` | ✅ | ✅ | ✅ Killed — all 3 `TestCheckForeignKeyColumnTypesMatch_*` fail |
| 3 | `internal/provisioner/table.go:420` | `DropColumnForeignKey`'s real-query-error branch: `return false, fmt.Errorf(...)` → `return false, nil` | ❌ (iter 1 survived) | ✅ | ✅ Killed — `TestDropColumnForeignKey_GenuineQueryErrorPropagates` fails cleanly (`expected a non-nil error ... got nil`) |
| 4 | `internal/dashboard/handler.go:1349` | Inverted `UpdateAppTable`'s existed-check: `if !existed { continue }` → `if existed { continue }` | ✅ | ✅ | ✅ Killed — `TestUpdateAppTable_RejectsReferencesChangeOnExistingColumn` and `_AllowsReferencesOnBrandNewColumn` both fail |

**Sensor depth**: lightweight (4 targeted mutations, per default tiering) plus the CFK-04-specific bypass above.
**Result**: 4/4 killed — no regression from iterations 1-2, and the previously-non-discriminating CFK-04 test now genuinely discriminates.

Post-sensor cleanup: `git worktree remove --force /tmp/cfk-sensor3` succeeded; real worktree `git status --porcelain` after cleanup is byte-identical to the pre-sensor baseline (`M .specs/LESSONS.md`, `M .specs/lessons.json`, `?? .specs/features/column-foreign-key/design.md`, `?? .specs/features/column-foreign-key/validation.md` — all pre-existing, none feature-related).

---

## Spec-Anchored Acceptance Criteria (CFK-01..21)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| CFK-01: Add FK on existing column with matching types succeeds | 200, constraint created | `apps_column_foreign_key_foruser_test.go` `TestAddColumnForeignKeyForUser_Success` | ✅ |
| CFK-02: Reject if column already has a reference | `ErrColumnAlreadyHasReference` | `TestAddColumnForeignKeyForUser_AlreadyHasReferenceRejected` (`handler.go:1692`) | ✅ |
| CFK-03: Reject FK targeting nonexistent table/column | `*ValidationError`/400 | `TestAddColumnForeignKeyForUser_InvalidTargetRejected` | ✅ |
| CFK-04: Reject 400 if target is `_auth_users` and column type isn't `uuid` | `*ValidationError`/400 | `apps_column_foreign_key_foruser_test.go:163-186` (`TestAddColumnForeignKeyForUser_AuthUsersTargetTypeMismatchRejected`) — now provisions `_auth_users` via `AuthEmailEnabled: true` (line 168) and asserts `errors.As(err, &valErr)` against the genuine type-check path | ✅ **Fixed** — confirmed discriminating via targeted bypass above |
| CFK-05: Reject general (non-`_auth_users`) physical type mismatch | `*ValidationError`/400 | `TestAddColumnForeignKeyForUser_TypeMismatchRejected`, `TestCheckForeignKeyColumnTypesMatch_Mismatch` (`table.go:368`) | ✅ |
| CFK-06: Reject when existing rows violate the new constraint (orphans) | `*provisioner.ForeignKeyViolationError` | `TestAddColumnForeignKeyForUser_OrphanedRowsRejected` | ✅ |
| CFK-07: Viewer role forbidden from adding FK | 403 | `TestAddColumnForeignKeyForUser_ViewerForbidden` | ✅ |
| CFK-08: Add FK records audit log entry | audit row written | `TestAddColumnForeignKeyForUser_RecordsAuditLog` | ✅ |
| CFK-09..21 (Remove FK, `DropColumnForeignKey`, MCP tools, `UpdateAppTable` no-silent-no-op guard, config validation, error types) | per spec | unchanged from iteration 1 — no code affecting these ACs changed in `76b37a0..HEAD` beyond the two additive test files (`0164688`, `4b3af05`, `f187ea2`) already reviewed across iterations 1-3; see `internal/provisioner/table_test.go` (`TestDropColumnForeignKey_*`), `internal/mcpserver/tools_add_column_foreign_key_test.go`, `internal/mcpserver/tools_remove_column_foreign_key_test.go`, `internal/dashboard/apps_handler_test.go` (`TestUpdateAppTable_RejectsReferencesChangeOnExistingColumn`, `_AllowsReferencesOnBrandNewColumn`) | ✅ |

**Status**: 21/21 ACs fully spec-anchored and verified.

---

## Gate Check

- **Gate command**: `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable DASHBOARD_BOOTSTRAP_SECRET=ci-test-bootstrap-secret-not-for-prod go build ./... && go vet ./... && gofmt -l $(git diff --name-only 76b37a0 -- '*.go') && go test ./...` (mcpserver run separately with `-p 1`)
- **Build**: clean
- **Vet**: clean
- **gofmt**: clean — no files listed for the 17 changed `.go` files in `76b37a0..HEAD`
- **Full suite** (serial, `-count=1 -p 1`, excluding mcpserver): all 16 packages `ok` — `auth`, `config`, `crypto`, `dashboard`, `dashboard/ai`, `db`, `deploy/render`, `docs`, `github`, `policytemplates`, `provisioner`, `query`, `registry`, `server`, `sshkey`, `webhookengine`
- **mcpserver** (run separately with `-p 1` per its concurrency requirement): `ok` (22.8s)
- **Note**: one parallel run (`go test $(go list ./...)` without `-p 1`) showed a transient `internal/server` failure under concurrent package execution (port/resource contention across parallel test binaries); `internal/server` is untouched by this feature's diff (0 files in `76b37a0..HEAD`) and passed cleanly both standalone (`go test ./internal/server/...`) and in the full serial (`-p 1`) run. Not a regression from this feature.
- **Skipped tests**: none observed
- **Failures**: none (serial gate)

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — fix commit is additive, test-only (one field added to an existing call) |
| Surgical changes | ✅ |
| No scope creep | ✅ |
| Matches patterns | ✅ — mirrors `AuthEmailEnabled: true` usage already established elsewhere in the same test file |
| Spec-anchored outcome check (asserted values match spec) | ✅ — CFK-04's test now asserts the right error via the right code path |
| Every test maps to a spec requirement — no unclaimed tests | ✅ |
| Documented guidelines followed: `AGENTS.md` §3, §4 | ✅ |

---

## Requirement Traceability Update

| Requirement | Previous Status (iter 2) | Final Status (iter 3) |
| --- | --- | --- |
| CFK-01, 02, 03, 05, 06, 07, 08, 09-21 | ✅ Verified | ✅ Verified (unchanged) |
| CFK-04 | ⚠️ Open — root cause identified, fix task ready | ✅ Verified — fix confirmed discriminating |

---

## Summary

**Overall**: ✅ Ready — both iteration 1 and iteration 2 gaps are closed and independently re-confirmed. This is iteration 3 of 3 in the bounded fix→re-verify loop; the loop closes here with a clean PASS.

**Spec-anchored check**: 21/21 ACs matched spec outcome.
**Sensor**: 4/4 of the original mutations killed, plus the targeted CFK-04 bypass confirmed to fail the fixed test via the genuine code path (real Postgres SQLSTATE 42804 rejection, not "table not found").
**Gate**: all packages passed (serial run); build/vet/gofmt clean; mcpserver passed separately with `-p 1`.

**Lessons distilled**: none — per the skill's rule, a clean PASS records no new lessons. (The underlying lesson — "app-creation calls in FK/`_auth_users` tests must set `AuthEmailEnabled: true` or the target table never gets provisioned" — was already captured during iteration 2's investigation; no new grounded failure emerged in this iteration to distill.)

**Loop status**: closed. 3/3 iterations used; final verdict PASS. No further verification cycles needed for this feature.
