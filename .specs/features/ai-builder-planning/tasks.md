# AI Builder Planning Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. If the skill cannot be activated, stop and tell the user.

Commits only when the user asks (AGENTS.md §2); one commit per task when authorized; never add Claude/Anthropic attribution; never force push. **P01 adds DDL to `provisioner.go` (higher-risk, AGENTS.md §8): show the DDL diff and get confirmation before applying.** Tests and measurements run locally or on ZeepLabs-controlled infra, never on Starbem infra. Provider API keys are typed by the owner in the settings screen or exported in their own shell; they are never written to the repository, to a spec, to a log or to a memory note.

---

**Spec**: `.specs/features/ai-builder-planning/spec.md`
**Design**: `.specs/features/ai-builder-planning/design.md`
**Parent**: `.specs/features/ai-frontend-builder/`
**Status**: Approved by the maintainer on 2026-11-13 with the defaults of design §12. In execution on `feat/ai-frontend-builder`. Done: P01. The DDL of P01 is still shown and confirmed before it is applied.

---

## Test Coverage Matrix

| Code Layer | Required Test Type | Location Pattern | Run Command | Parallel-Safe |
| --- | --- | --- | --- | --- |
| Plan and question validation, renderers | unit | `internal/agent/planning/*_test.go` | `go test ./internal/agent/planning/...` | Yes |
| Loop with fakes (tool set, gates, pause, brief file) | unit | `internal/agent/*_test.go` | `go test ./internal/agent/...` | Yes |
| Stores and handlers (real Postgres) | integration | `internal/agent/store/*_test.go`, `internal/dashboard/builder_*_test.go` | `go test -p 1 -parallel 4 -timeout 25m ./...` | No (shared DB) |
| DDL in `provisioner.go` | none | exercised by P01/P02 tests | build gate | — |
| Provider adapters (history with tools that are not offered) | unit | `internal/agent/provider/contract_test.go` | `go test ./internal/agent/provider/...` | Yes |
| Frontend | none (no framework) | `src/**` | frontend gate, plus the local run of P12 | — |

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 25m ./...` |
| Frontend | UI tasks | `cd internal/dashboard/ui && npx tsc -b && npm run build` plus i18n JSON validation for `en.json` and `pt-BR.json` |

---

## Dependency Graph

```
P01 DDL + planning_mode ──► P02 store (phase, mode, briefs, resolutions) ──► P05 runtime wiring ──► P06 HTTP ──► P08 question card ──► P09 plan card ──► P10 brief, continue, plan first
P03 planning package + tools [P] ──► P04 loop (tool set, gates, prompt, brief file) ──► P05          └──► P07 brief API + estimate ──► P09
P11 docs + CHANGELOG (last)        P12 local end-to-end run        P13 real-model run (owner, with costs C13)
```

---

## Tasks

### P01 — DDL and the `planning_mode` setting  `[higher-risk: confirm DDL first]`
- **What**: the DDL of design §3; `planning_mode` in the settings store (partial update), the settings API (validated against the three values, superadmin only, audited) and the builder status the UI already reads.
- **Where**: `internal/dashboard/provisioner.go`, `builder_settings_store.go`, `builder_settings.go`, `builder_common.go`.
- **Done when**: DDL diff shown and confirmed; provisioning twice on a database with the old tables works; old sessions read `phase = building` and old turns `mode = build`; **[verify]** no `SELECT *` on the altered tables; settings tests cover default, each value, an invalid value (400, English message) and absence of the key (unchanged).
- **Requirements**: ABP-01, ABP-17. **Tests**: integration. **Gate**: Full.
- **Status**: Done 2026-11-13. DDL confirmed by the maintainer and applied by the test suite on the local test database, which already had the old tables (provisioning runs once per test pool, so it ran many times). `planning_mode` is in the settings store and API and in `GET /api/builder/status` (`planning_mode`), so any user's UI can read it. No `SELECT *` on the altered tables (`grep` on the builder files and the agent store). Nothing reads the setting yet: session creation is wired in P05. Full gate green (27 packages).

### P02 — Store: phase, turn mode, briefs and the planning resolutions
- **What**: `Session.Phase`; `Turn.Mode`, `BriefVersion`, `Milestone`; `CreateSession` takes the phase; `CreateTurn` takes options (`PlanFirst`, `Milestone`) and decides the mode inside its transaction, including the `off` transition of design §2; briefs store (insert version, current, by version, list, milestone progress query); `ResolveProposal` split so an extra step runs in its transaction; `AnswerQuestions`, `ApproveBuildPlan`, `ReviseBuildPlan` (design §5); `PurposePlanning`.
- **Where**: `internal/agent/store/store.go`, `proposals.go`, new `planning.go`, `usage.go`.
- **Done when**: integration tests: mode per phase and per `PlanFirst`; milestone accepted only when it is the next pending one; approval is atomic (brief row, phase, turn mode, tool result, event, turn `pending`), verified by a failing extra step that leaves nothing behind; each resolution refuses the wrong pending tool with `ErrProposalNotPending`; two concurrent approvals yield one version; progress query for done-by-plan and done-by-turn; existing proposal tests unchanged.
- **Requirements**: ABP-01, ABP-06, ABP-09, ABP-10, ABP-11. **Depends**: P01. **Tests**: integration. **Gate**: Full.

### P03 — Planning package and the two tools  [P]
- **What**: `internal/agent/planning`: argument types, `ParseQuestions`, `ParsePlan` with every bound of design §4, `RenderBrief`, `RenderBriefFile`, `ValidateAnswers` (answers against the asked questions), answer and resolution texts, tool specs and JSON Schemas. `tools.KindPlanning`, `Registry.SpecsWhere`.
- **Where**: new `internal/agent/planning/`, `internal/agent/tools/registry.go`.
- **Done when**: table tests for every bound (question count, option count, lengths, duplicate ids, plan lists, `access` values, argument bytes, rendered size) with messages the model can act on; `RenderBrief` is byte-stable for the same plan (golden file) and never exceeds `MaxBriefChars` for a valid plan; registry tests for the new kind and filter; user text in answers cannot break the result format (newlines, quotes, very long text).
- **Requirements**: ABP-02, ABP-04, ABP-07, ABP-14. **Tests**: unit. **Gate**: Quick.

### P04 — Loop: tool set, gates, prompt and the brief file
- **What**: `toolSpecs()` and the `runCalls` gate by mode (design §4.3); pause on `KindPlanning`; question bounds, skip and reopen from `analyzeTurn` (§4.4); `purpose = planning` for plan-mode calls; brief section and directive in `BuildSystemPrompt` (§7); brief file write (§6); the "Planning" block in `prompts/core.md`. Resolve the two **[verify]** items of §4.3 and §6 and record the result in the design.
- **Where**: `internal/agent/loop.go`, `history.go`, `prompt.go`, `prompts/core.md`, `fakes_test.go`, `internal/agent/provider/contract_test.go`.
- **Done when**: loop tests with fakes: (1) in plan mode the offered specs contain no workspace or backend-proposal tool, and a call to each of them is refused without reaching the registry or the builder — **this is the test of spec success criterion 3**; (2) `ask_user` and `propose_build_plan` pause and resume, including a crash between the pause and the resume; (3) fourth round refused in plan mode, second in build mode, one more after a reopen, none after a skip, all after a simulated resume; (4) planning tools absent in build mode when planning is disabled; (5) brief file written once, replayed on resume, not written in plan mode; (6) usage purpose per mode. Prompt tests: section order, header text, brief never truncated, sections 1 to 5 byte-identical across two turns with different directives. Contract table case for a history with calls to tools that are not offered, for both adapters.
- **Requirements**: ABP-01, ABP-03, ABP-04, ABP-05, ABP-12, ABP-13, ABP-16. **Depends**: P02, P03. **Tests**: unit. **Gate**: Full (touches the store fakes).

### P05 — Runtime wiring
- **What**: register the two tools; `prepareDeps` sets `PlanningEnabled`; `promptSource` returns the brief, the directive (mode, progress, milestone of the turn) and the brief file content when the workspace copy differs; `CreateSession` picks the phase from the settings.
- **Where**: `internal/dashboard/builder_runtime.go`, `builder_tools.go`, `builder_handlers.go`.
- **Done when**: integration tests: session created under each `planning_mode`; prompt of a briefing session has no brief and has the planning directive; prompt after approval has the brief and the milestone directive; brief file content returned only when the archive copy differs; an existing-backend session's directive tells the agent to read the schema first.
- **Requirements**: ABP-01, ABP-10, ABP-12, ABP-17. **Depends**: P04. **Tests**: integration. **Gate**: Full.

### P06 — HTTP: answers, plan approval and revision
- **What**: the three `POST` routes of design §8; `messages` accepts `plan_first` and `milestone`; `confirm`/`reject` and the new routes refuse each other's proposals with 409 (resolve the **[verify]** on `ConfirmProposal` first); events whitelisted (`questions_answered`, `plan_approved`, `plan_revision_requested`); session view fields; audit `builder.plan.approve`.
- **Where**: new `internal/dashboard/builder_planning.go`, `builder_messages.go`, `builder_proposals.go`, `builder_common.go`, `internal/server/server.go`.
- **Done when**: integration tests: full path over HTTP with the mock model (message → questions → answers → plan → approve → build turn runs with the full tool set in the same turn); validation errors of the answers (English); skip; revise with and without `more_questions`; another user's session is 404; archived session; wrong pending tool is 409 both ways; double approve (second is 409 or the idempotent answer the proposal routes already give — match them); event payloads contain only whitelisted keys and capped text; `milestone` mismatch is 400.
- **Requirements**: ABP-02, ABP-05, ABP-06, ABP-09, ABP-10, ABP-11, ABP-15. **Depends**: P05. **Tests**: integration. **Gate**: Full.

### P07 — Brief API and the estimate
- **What**: `GET brief`, `GET brief/versions/{v}`, `GET plan/estimate` (design §8, §9).
- **Where**: `internal/dashboard/builder_planning.go`, new store query in `internal/agent/store/usage.go`, `server.go`.
- **Done when**: integration tests: no brief (`version: 0`); versions listed newest first; estimate with fewer than 10 turns (`available: false`); with 10 priced turns (range equals the percentiles of the seeded values); with unpriced calls in the sample (tokens only); turns before a currency change excluded from the cost; plan-mode turns and other models excluded; whole-plan range counts only milestones not done; the query plan on the 300,000-row seeded ledger of costs C01 is recorded in the design.
- **Requirements**: ABP-08, ABP-13. **Depends**: P06. **Tests**: integration. **Gate**: Full.

### P08 — UI: question card
- **What**: `QuestionCard`, card selection by proposal name, answered rounds in the history, briefing placeholder in the composer, `useAnswerQuestions`.
- **Where**: `components/patterns/builder/`, `ChatPanel.tsx`, `builderModel.ts`, `api.ts`, both locale files.
- **Done when**: frontend gate green; on the local stack: single and multiple choice, typed answer, "Decide for me", "Start building now", reload with a pending round, keyboard-only use, both languages; a failed request shows the toast and keeps the typed answers.
- **Requirements**: ABP-02, ABP-05, ABP-06. **Depends**: P06. **Gate**: Frontend.

### P09 — UI: plan card
- **What**: `PlanCard` with the estimate, approve / ask for changes / back to questions, approved plans in the history, `useApprovePlan`, `useRevisePlan`, `usePlanEstimate`.
- **Where**: `components/patterns/builder/`, `ChatPanel.tsx`, `builderModel.ts`, `api.ts`, both locale files.
- **Done when**: frontend gate green; on the local stack: card with and without an estimate, with tokens only, `public` data highlighted, assumptions visible, a revision shows the new card, approval starts the build without another message; no figure is computed in the browser.
- **Requirements**: ABP-07, ABP-08, ABP-09, ABP-10. **Depends**: P07, P08. **Gate**: Frontend.

### P10 — UI: brief, continue, plan first and the setting
- **What**: `BriefDrawer` and its header button, the "Continue: <title>" button, the "Plan first" switch, the `planning_mode` select in the builder settings, `useBuilderBrief`.
- **Where**: `components/patterns/builder/`, `BuilderPage.tsx`, `ChatPanel.tsx`, `pages/settings/`, `api.ts`, both locale files.
- **Done when**: frontend gate green; on the local stack: brief with versions; continue runs the next milestone and the progress advances; plan-first turn ends in a plan card and writes nothing; switch and planning UI absent with `off`; an old session shows no brief and still works.
- **Requirements**: ABP-11, ABP-13, ABP-15, ABP-17. **Depends**: P09. **Gate**: Frontend.

### P11 — Docs and CHANGELOG
- **What**: `docs/docs/ai-frontend-builder.md` (briefing, plan, brief, milestones, the setting, the estimate and its limits, limitations of design §11); README feature line and the three translations if the feature table changes; CHANGELOG entries are added by each task, this task reviews them and states the behavior change of the default.
- **Done when**: Docusaurus build green; translations mirrored.
- **Requirements**: all. **Depends**: P10.

### P12 — Local end-to-end run
- **What**: scripted mock model on the local stack: zero-question path, three rounds then the bound, skip, revise, back to questions, approve and build, continue to the last milestone, plan first, `off`, `suggested`, restart of the server with a pending round, budget ending during the briefing. Screenshots of the cards in both languages.
- **Done when**: each path recorded as passed or failed with what was seen; failures fixed or listed as open in this file.
- **Depends**: P10.

### P13 — Real-model run (owner)
- **What**: with the owner's key, together with costs C13: whether a real model asks useful questions and stays inside the bounds, whether it respects one milestone per turn, the size of real plans against the limits, and the provider behavior of the §4.3 **[verify]**. Feeds the eval set of the parent spec (T42) for success criterion 1.
- **Depends**: P12, a key typed by the owner.
