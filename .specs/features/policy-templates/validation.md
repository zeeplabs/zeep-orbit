# Policy Templates & Help Drawer Validation

**Date**: 2026-08-12
**Spec**: `.specs/features/policy-templates/spec.md`
**Diff range**: `fa608a3^..928c591` (10 commits, T1-T9 + follow-up fix commit)
**Verifier**: independent sub-agent (author ≠ verifier) — second pass, re-verifying the fix for the 2 gaps raised by the first pass

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `hasOwnerColumn` extracted to `internal/dashboard/ui/src/lib/rls.ts`, `TableCard.tsx:83` uses it |
| T2   | ✅ Done | Single-action builders in `policyTemplates.ts` |
| T3   | ✅ Done | Composite builder reuses T2's builders |
| T4   | ✅ Done | `RoleChipPicker.tsx` extracted, used by advanced form + templates |
| T5   | ✅ Done | `PolicyHelpContent.tsx` + i18n |
| T6   | ✅ Done | Single-action templates in `PolicyTemplatePicker.tsx` |
| T7   | ✅ Done | Composite template + partial-failure UI |
| T8   | ✅ Done | Mode/toggle/drawer wired into `TablePolicies.tsx`; deviation (also touched `TableCard.tsx:415`) verified reasonable in first pass |
| T9   | ✅ Done | `policy-templates.spec.ts` added |
| Fix (928c591) | ✅ Done | Closes both gaps flagged by the first Verifier pass — see AC8 and P3 AC2 rows below |

---

## Spec-Anchored Acceptance Criteria

### P1: Templates de ação única

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1-AC7, AC9 | (unchanged from first pass — see prior evidence, all still hold at 928c591; line numbers shifted slightly by the new stage inserted before AC5) | `e2e/policy-templates.spec.ts:122-124` (AC1/AC9), `:127-138` (AC3), `:142-145` (AC4), `:168-171` (AC5, was :149), `:177-180`/`:216-217` (AC6, was :161-182), `:138` (AC7 endpoint reuse) | ✅ PASS (all, re-verified against current file) |
| AC8: template creation failure → `toast.error`, no stuck state, no consistent-state break | error message shown verbatim, no infinite loading, no duplicate silently created | `e2e/policy-templates.spec.ts:152-164` — reapplies the `open_read` template with the same role, colliding on the previously-created `tpl_open_read_select` name (a genuine backend 409, not a client-side validation short-circuit: the button click at line 155 fires the real `POST`). `:158` asserts `toast.error`'s exact text `'a policy with this name already exists on this table'`, verified byte-for-byte against the real 409 payload in `internal/dashboard/handler.go:1419,1479` (`writeJSON(w, http.StatusConflict, map[string]string{"error": "a policy with this name already exists on this table"})`). `:162` asserts the Apply button is `toBeEnabled()` again (not stuck — `isApplying`'s `finally` at `PolicyTemplatePicker.tsx:127-129` cleared). `:163` asserts exactly 1 `tpl_open_read_select` policy exists via `getPolicies` — no duplicate silently created. | ✅ PASS — gap closed |
| AC9 | unchanged | see AC1 row | ✅ PASS |

### P2: Template composto

Unchanged from first pass — all 3 ACs still ✅ PASS, evidence at `e2e/policy-templates.spec.ts:243-249` (AC1), `:277-282` (AC2), `:287-294` (AC3) — line numbers shifted forward by the 21 lines the fix commit inserted earlier in the file, content and assertions unchanged.

### P3: Drawer de ajuda

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1, AC3 | drawer opens without discarding form/template; closing preserves state | `e2e/policy-templates.spec.ts:188-190` (open, unchanged), `:210-212` (close via Escape, `getByPlaceholder('Value')` still `'published'`) | ✅ PASS |
| AC2: ≥3 examples, real allowlist only (no `LIKE`, no SQL function, no invented claim) | 3+ examples; every operator/claim used belongs to the allowlist (`=`,`!=`,`IN`,`NOT IN`,`>`,`<`,`>=`,`<=`,`IS NULL`,`IS NOT NULL` / `role`,`sub`,`email`) | `e2e/policy-templates.spec.ts:198-209` — now asserts against `helpDialog.innerText()` (the drawer's actual rendered DOM text), not a source comment: `:199-200` counts `p.font-semibold` example-title nodes (`>= 3`); `:201-202` asserts the full rendered text does not contain `'like '` (case-insensitive) or `'now()'`; `:205-208` extracts every `claim:X` token via regex and asserts each one is in `['role','sub','email']`, with `:206` guarding the loop isn't vacuous (`claimsUsed.length > 0`, currently 2 - `email`, `sub`). Cross-checked the assertion is non-shallow via the discrimination sensor below (mutating one example's operator to `LIKE` is caught specifically by this assertion, not by any pre-existing check). | ✅ PASS — gap closed |
| AC4: drawer text in both locale files | `en.json`/`pt-BR.json` both have `tablePolicies.help.*` | unchanged from first pass — `b81950c` diffed +38/+38 lines, JSON-parse gate green | ✅ PASS |

**Status**: ✅ All ACs covered — both gaps flagged by the first pass are closed with direct `file:line` evidence; 0 real gaps remain. (The one first-pass ⚠️ spec-precision gap on P1 AC2's untestable defensive branch is unrelated to this fix and still stands as a spec-acknowledged non-issue — spec.md itself calls that branch "apenas defensivo... não existe hoje".)

---

## Discrimination Sensor

Isolated scratch: `git worktree add <scratch-path> HEAD` (never `git stash`) at commit `928c591`. Baseline `git status --porcelain` on the real tree was `?? .specs/features/policy-templates/validation.md` (this report, mine to write) before sensor work and confirmed byte-identical after cleanup (`git worktree remove --force`).

Process per mutation: edited the scratch copy of `PolicyHelpContent.tsx` → `npm run build` (scratch's `ui/`) → `go build ./cmd/zeep` (scratch, so the mutated frontend is embedded) → ran the mutant binary against a fresh disposable Postgres database on an unused port → ran the **real, unmodified** `policy-templates.spec.ts` from the working tree against it via `BASE_URL`.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ------------ | ------- |
| 1 (attempt, superseded) | `PolicyHelpContent.tsx:39` (scratch) | Flipped example 3's `owner_id` clause operator from `"="` to `"LIKE"` | ✅ Killed, but by the pre-existing spot-check at `:193` (`owner_id = claim:sub` text no longer matches), not by the new AC2 allowlist assertion — inconclusive for proving the new assertion specifically. Reverted, re-targeted a different example (below). |
| 2 (targeted at the new AC2 assertion) | `PolicyHelpContent.tsx:24` (scratch) | Flipped example 1's `created_by_email` clause operator from `"="` to `"LIKE"` (leaves example 3's `owner_id = claim:sub` text — the pre-existing spot-check's target — untouched) | ✅ Killed — failed exactly at `e2e/policy-templates.spec.ts:201` (`expect(helpText.toLowerCase()).not.toContain('like ')`), the new AC2 assertion, with the rendered text showing `"created_by_email like claim:email"`. Confirms the new assertion is the one doing the discriminating, not a side effect of an older check. |

**Sensor depth**: lightweight (1 clean targeted mutation after 1 superseded attempt — standard feature, coverage-gap re-verification, no P0/critical path)
**Result**: 1/1 (targeted) killed by the intended assertion - PASS ✅
**Isolation check**: `git status --porcelain` on the real tree identical before and after (`?? .specs/features/policy-templates/validation.md` only, both times); scratch worktree removed; disposable Postgres container and binaries removed.

Gap 1 (AC8) was not independently mutation-tested in this pass — it reuses the exact same `useCreateTablePolicy`/`toast.error`/`isApplying.finally` code path already mutation-tested for the composite template's failure handling in the first pass (mutations 1-2 in that pass's sensor table, both killed). No new production code was introduced by the fix commit (it is test-only), so no new mutation surface exists for AC8.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ - fix commit is test-only, 37 insertions/2 deletions, no production code touched |
| Surgical changes | ✅ - only `policy-templates.spec.ts` changed, exactly as the fix plan specified |
| No scope creep | ✅ - both new stages map 1:1 to the two gaps raised by the first pass, nothing extra added |
| Matches patterns | ✅ - reuses the suite's existing helpers (`getPolicies`, `useTemplateButton`) and commenting convention (AC-tagged comments) |
| Spec-anchored outcome check (asserted values match spec) | ✅ - AC8's `toast.error` text verified against `handler.go`'s actual 409 payload, not just plausible-looking; AC2's allowlist check verified against rendered DOM text |
| Per-layer Coverage Expectation met | ✅ - single-action template's error path (AC8) and drawer-content compliance (AC2) are now both e2e-covered, closing the two previously-uncovered surfaces |
| Every test maps to a spec requirement - no unclaimed tests | ✅ - both new stages carry explicit AC-tag comments |
| Documented guidelines followed | ✅ - `AGENTS.md` §3 gate commands re-run clean (see Gate Check below) |

---

## Edge Cases

Unchanged from first pass — all still hold; the fix commit does not touch any edge-case-relevant code or assertion.

---

## Gate Check

- **Gate command (Build)**: `cd internal/dashboard/ui && npx tsc -b && npm run build`, then a real `zeep serve` binary against a fresh disposable Postgres (Docker `postgres:16-alpine`), then `BASE_URL=... npx playwright test e2e/policy-templates.spec.ts`, then from repo root `go build ./... && go vet ./...`, then locale JSON-parse check on both `en.json`/`pt-BR.json`.
- **Result**:
  - `npx tsc -b`: 0 errors
  - `npm run build`: succeeds (1,050 kB main-chunk warning, pre-existing, unrelated)
  - `npx playwright test e2e/policy-templates.spec.ts` (real backend, disposable Postgres on port 5439, `zeep serve --port 8099`): **1 passed** (single run needed two setup corrections unrelated to the code under test — the app's bootstrap endpoint reads `DASHBOARD_BOOTSTRAP_SECRET`, not `BOOTSTRAP_SECRET`; a stale prior server process on the same port had to be killed before the corrected env var took effect)
  - `go build ./...`: OK
  - `go vet ./...`: OK
  - Locale JSON parse (`en.json`, `pt-BR.json`): OK
- **Test count before fix commit**: 1 test, 1 `describe`, ~15 AC-tagged stages (at `2f971ab`)
- **Test count after fix commit**: 1 test, 1 `describe`, ~17 AC-tagged stages (at `928c591`) — +2 stages (AC8 failure-path, AC2 allowlist-compliance), 0 removed, 0 weakened
- **Delta**: +37/-2 lines, test-only
- **Skipped tests**: none
- **Failures**: none (baseline run against the real, unmodified tree)

---

## Fix Plans

None — both gaps from the first pass are closed. No new gaps found in this pass.

---

## Requirement Traceability Update

No change from first pass — `spec.md`'s existing table is accurate as written.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 16/16 AC rows matched spec outcome with direct file:line evidence (up from 14/16 + 1 real gap in the first pass); the 1 pre-existing ⚠️ spec-precision gap (P1 AC2's untestable defensive branch) is unrelated to this fix and remains spec-acknowledged, not a fix target.
**Sensor**: 1/1 targeted mutation killed by the intended new assertion (verified the new AC2 check is the one discriminating, not a pre-existing check catching it by accident)
**Gate**: all passed (tsc, vite build, real e2e against real backend, go build, go vet, locale JSON)

**What works**: Both gaps the first Verifier pass raised are genuinely closed. AC8's single-action failure path now forces a real backend 409 (not a client-side short-circuit) and the asserted `toast.error` text is byte-verified against `handler.go`'s actual response. AC2's allowlist-compliance check now runs against the drawer's real rendered DOM text and was proven non-shallow: a targeted mutant (swapping one example's operator to `LIKE`) is caught specifically by the new assertion, confirmed by first observing a mutation attempt that was killed by the *wrong* (pre-existing) assertion and re-targeting until the new assertion was isolated as the one doing the work.

**Issues found**: none.

**Next steps**: Feature is verified complete. No further fix→re-verify iterations needed (this closes iteration 2 of the bounded 3-iteration loop, with 0 gaps remaining).
