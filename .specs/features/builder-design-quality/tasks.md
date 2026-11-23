# AI Builder Design Quality Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** If the skill cannot be activated, STOP and tell the user.

Commits only when the user asks (AGENTS.md §2); never add Claude/Anthropic attribution; never force push. Tests run locally or on a ZeepLabs-controlled server, never on Starbem infra. Each task updates `CHANGELOG.md` under `[Unreleased]` in the same change when it ships user-visible behavior (AGENTS.md §6).

---

**Spec**: `.specs/features/builder-design-quality/spec.md`
**Design**: `.specs/features/builder-design-quality/design.md`
**Status**: Draft 2026-11-23. Nothing implemented. Branch `feat/builder-design-quality` (worktree `.claude/worktrees/builder-design-quality`) from `origin/develop`.

## Gate Check Commands

- Backend: `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 25m ./...` (needs `TEST_DATABASE_URL`, `DASHBOARD_BOOTSTRAP_SECRET`, `BOOTSTRAP_SECRET`; one test suite per database). Quick loop for P1: `go test ./internal/agent/...`.
- Frontend (`internal/dashboard/ui`): `npx tsc -b`, `npm test`, `npm run build`; validate `en.json` and `pt-BR.json` when touched.
- Template (T8+): in `builder/templates/default`: `npm ci && npm run lint && npm run build`.

## Dependency overview

```
P1:  T1 -> T2 -> T3
     T1 -> T4 (core rule) -> T5 (skill v2) -> T6 (skills eval) -> T7 (compare with baseline)
P2:  T8 (template + Tailwind + library) -> T9 (ui_kit scan + prompt block) -> T10 (kit eval) -> T11 (docs)
P3:  T12 (spike, independent) -> decision -> addendum -> T13..
```

T1 (baseline) must finish before T2, T4 and T5 change any behavior. T8 can start in parallel with P1 once T1 exists.

---

## P1: good by default

### T1: Baseline and rubric (DQ-20)
- **What**: Create `validation.md` with the scoring rubric (hierarchy, distinctiveness between project types, state coverage, contrast, kit usage, 1-5 each, blind-review instructions). Run the 12 eval briefs on the current build (beta.2 stack or `develop`) with one reference model, keep the produced `dist` and a screenshot per brief, score them.
- **Where**: `.specs/features/builder-design-quality/validation.md`; outputs outside the repo if large (design §6).
- **Done when**: baseline scores recorded; brief size of the 12 plans before the change recorded (design §2.1).
- **Tests**: none (evidence task). **Gate**: none.

### T2: `Look` fields and validation (DQ-01, DQ-02)
- **What**: Add `Direction`, `Type`, `Density`, `Motion`; `field()` validation; `Validate` rejects an empty direction+style; `lookText` renders them; tool schema (`planning/tools.go`) and description updated; planning prompt tells the planner to derive the direction from the project and not reuse one.
- **Where**: `internal/agent/planning/planning.go`, `render.go`, `tools.go`; tests next to them.
- **Depends on**: T1.
- **Done when**: unit tests cover accept, reject, worst-case brief size under `MaxBriefChars`, old plans still decode; planning shape test (12 briefs) still passes.
- **Tests**: unit. **Gate**: `go test ./internal/agent/...` then full Go gate.

### T3: Plan card shows and edits the direction (DQ-03)
- **What**: The plan card renders the new `Look` fields and lets the user change them like other parts; strings in `en.json` and `pt-BR.json`.
- **Where**: `internal/dashboard/ui` plan components (locate first; design §2.1).
- **Depends on**: T2.
- **Done when**: component test for rendering/editing; manual check on the local stack.
- **Tests**: unit (vitest). **Gate**: frontend gate.

### T4: Core rule (DQ-06)
- **What**: One paragraph in `core.md`; test pins the sentence; check `MaxSessionContextChars` and prompt totals unchanged in the worst-case test.
- **Where**: `internal/agent/prompts/core.md`, `internal/agent/prompt_test.go`.
- **Depends on**: T1. Coordinate with the language-rule branch (`fix/builder-reply-language`) since both edit `core.md` and `prompt_test.go`: merge order decided at PR time.
- **Done when**: tests pass; core growth under 600 characters.
- **Tests**: unit. **Gate**: `go test ./internal/agent/...`.

### T5: Skill v2 `orbit-frontend-design` (DQ-07..DQ-09)
- **What**: Rewrite per design §2.3; bump `version`; update `builtin.lock` hash; description within 200 characters.
- **Where**: `internal/agent/skills/builtin/orbit-frontend-design/SKILL.md`, `builtin.lock`.
- **Depends on**: T1, T4.
- **Done when**: skill lint tests pass; maintainer read-through of the text (same step the skills spec required for S13).
- **Tests**: existing lint tests. **Gate**: `go test ./internal/agent/skills/...`.

### T6: Skill trigger eval (DQ-09)
- **What**: Confirm the skills eval runner works on this branch; add positive tasks ("build a dashboard screen", "make it prettier") and negative ones ("add a column", "fix the login error"); record load rates per reference model.
- **Where**: `internal/agent/eval` (opt-in, tag `eval`).
- **Depends on**: T5.
- **Done when**: rates recorded in `validation.md`; no negative task loads the skill more often than the baseline.
- **Tests**: eval. **Gate**: `go test -tags eval ./internal/agent/eval/...`.

### T7: Compare with baseline (success criteria)
- **What**: Re-run T1's briefs on the new build; blind comparison against the baseline using the rubric; record results and whether two project types still share palette and type.
- **Depends on**: T2..T6.
- **Done when**: results in `validation.md`; thresholds for P1 success fixed from the baseline and met or the gap documented. A "not met" result is reported as such.
- **Tests**: manual/eval. **Gate**: none.

---

## P2: template UI kit and default library

### T8: Default template with Tailwind and the Orbit library (DQ-14..DQ-18)
- **What**: Add Tailwind (CSS-first theme) and the minimal package set; add components under `src/components/ui`; semantic tokens in `src/index.css`; rewrite `ORBIT_AGENT.md`; add `UI_KIT.md` listing components, props, tokens; add `ui_kit` to `orbit-template.json`; bump `TEMPLATE_VERSION`; pin exact versions and regenerate `package-lock.json` through the registry proxy.
- **Where**: `builder/templates/default/**`.
- **Depends on**: T1 (so install/build time can be compared). Dependency surface per design §3.2 (decide Tailwind major, headless primitives, icon package here).
- **Done when**: `npm ci && npm run lint && npm run build` green; install and build times recorded against the old template; a built session in the local stack still previews under the token path (relative base kept).
- **Tests**: template build check; existing builder tests that copy the default template still pass. **Gate**: template gate + Go gate.
- **Risk**: new runtime dependencies are supply-chain surface; list each added package in the PR (AGENTS.md §9 spirit).

### T9: Read `ui_kit` and add the prompt block (DQ-10..DQ-12)
- **What**: `scanWorkspaceFiles` captures `orbit-template.json` and the notes path; validate the declaration; `BuildSystemPromptWithBrief` adds the fixed kit block when present; injection attempts in manifest fields never reach the prompt as free text.
- **Where**: `internal/dashboard/builder_runtime.go`, `internal/agent/prompt.go` (+ `loop.go` input struct), tests.
- **Depends on**: T8 for a real manifest (can use fixtures earlier).
- **Done when**: unit tests per design §5 pass; admin (GitHub) template with and without `ui_kit` both build a valid prompt.
- **Tests**: unit. **Gate**: Go gate.

### T10: Kit eval (DQ-13, DQ-16)
- **What**: Fixture template with a kit; briefs whose screens need buttons, forms, dialogs, tables; check imports from the kit path and absence of re-implemented primitives. Two apps with different `Look` rendered from the default library compared for visible difference.
- **Depends on**: T8, T9.
- **Done when**: results recorded in `validation.md`.
- **Tests**: eval. **Gate**: `go test -tags eval ./internal/agent/eval/...`.

### T11: Docs (DQ-18)
- **What**: `docs/` page for template authors (`ui_kit`, `UI_KIT.md`, `ORBIT_AGENT.md`, token names, precedence); `CHANGELOG.md`; README feature table row if user-facing, mirrored in the three translated READMEs (AGENTS.md §6).
- **Depends on**: T9.
- **Done when**: docs build green; translations mirrored.

---

## P3: visual feedback

### T12: Feasibility spike for render-and-look (DQ-19)
- **What**: Sidecar prototype and measurements per design §4 questions 1-4. No change to the builder container hardening.
- **Where**: throwaway code outside the repo until the decision; report in `validation.md`.
- **Done when**: go/no-go with numbers (image support per model, cost and latency per capture, isolation settings, browser-sandbox decision). On "go", write design addendum §4.1+ and tasks T13+ (provider image blocks, catalog vision flag, sidecar service + compose/Helm, `look_at_preview` tool, loop budget, automated visual eval). Nothing else in P3 starts before that.
- **Tests**: none (spike).

---

## Traceability

| Requirement | Tasks |
| --- | --- |
| DQ-01, DQ-02, DQ-04 | T2 |
| DQ-03 | T3 |
| DQ-05, DQ-06 | T4, T5 |
| DQ-07, DQ-08 | T5 |
| DQ-09 | T5, T6 |
| DQ-10, DQ-11, DQ-12 | T9 |
| DQ-13 | T10 |
| DQ-14, DQ-15, DQ-16, DQ-17 | T8 (DQ-16 verified in T10) |
| DQ-18 | T8, T11 |
| DQ-19, DQ-21 | T12 (T13+ after the decision) |
| DQ-20 | T1, T7, T12 |
