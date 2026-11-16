# AI Builder Planning Design

**Spec**: `.specs/features/ai-builder-planning/spec.md`
**Status**: Approved by the maintainer on 2026-11-13, with the defaults of section 12. Items marked **[verify]** were not confirmed in the code or against a real provider yet and must be checked in the task that touches them.

---

## 1. What exists and is reused (read from the code)

| Piece | Where | Reuse |
| --- | --- | --- |
| Pause and resume of a turn | `loop.go` `pauseForProposal` (status `awaiting_confirmation`, `builder_turns.pending_proposal`), `store.ResolveProposal` (tool result + event + turn back to `pending` in one transaction) | Question rounds and the plan card pause the turn the same way. A paused turn holds no lease and no timer, and `RunTurn` rebuilds from the database, so ABP-06 comes from the existing mechanism. |
| Tool registry | `internal/agent/tools/registry.go` (`Kind`, `Specs`, `SpecsWithout`, `Dispatch`) | One new kind and one filtered `Specs` variant. |
| Per-turn gate that answers a call without dispatching it | `loop.go` `gateSkillCall` | Same pattern for "tool not available while planning" and for the question bound. |
| Turn state rebuilt from persisted messages | `history.go` `analyzeTurn`, `toolMeta` | Counts question rounds and reads the skip marker, so both survive a crash. |
| System prompt sections | `prompt.go` `BuildSystemPrompt` (core, company, template guide, skills index, session context) | One new section for the brief, before the session context. |
| Prompt source | `builder_runtime.go` `promptSource` (already scans the workspace at HEAD) | Also returns the brief, the turn directive and whether the brief file must be rewritten. |
| Settings | `builder_settings` single row with partial-update store | One new column, same merge-on-absent-key rule. |
| Pending proposal in the session view | `builder_proposals.go` `BuilderProposalView` (`name`, `operation`) | The UI picks the card by `name`. |
| Usage ledger | `ai_usage_ledger` (`session_id`, `turn_id`, `purpose`, `cost_micros`) | New purpose `planning`; the estimate reads per-turn totals. |

## 2. Model: session phase and turn mode

Two values, with different lifetimes:

- **`builder_sessions.phase`**: `briefing` or `building`. A session created while `planning_mode = required` starts in `briefing`. Everything else, including every session that exists today, is `building` (assumption 8).
- **`builder_turns.mode`**: `plan` or `build`. It is fixed when the turn is created and changes only when a plan is approved. `plan` when the session is in `briefing` or the user sent the message with "Plan first" (ABP-15); otherwise `build`.

The loop reads only `turn.mode`. That gives one rule for the tool set and makes "the build starts in the same turn" (ABP-10) fall out of the resume: approval sets `phase = building` and `mode = build` in the transaction that resumes the turn, and the next `RunTurn` invocation rebuilds with the full tool set.

| `planning_mode` | New session | Planning tools in a build turn | "Plan first" switch |
| --- | --- | --- | --- |
| `required` (default) | `briefing` | offered | shown |
| `suggested` | `building` | offered; the prompt tells the agent to brief when the request is new, large or ambiguous | shown |
| `off` | `building` | not offered | hidden |

`required` → `off` with sessions still in `briefing`: `CreateTurn` moves such a session to `building` and creates a `build` turn. **Spec deviation:** the spec says the answers are kept "as the brief"; no brief version is synthesized. The answers stay in the conversation history, which the agent reads. A turn already paused in plan mode finishes in plan mode.

## 3. Data model (DDL, higher-risk, AGENTS.md §8: show the diff and get confirmation before applying)

Idempotent, in the builder block of `provisioner.go`.

```sql
ALTER TABLE zeep_system.builder_settings
    ADD COLUMN IF NOT EXISTS planning_mode TEXT NOT NULL DEFAULT 'required'
    CHECK (planning_mode IN ('required', 'suggested', 'off'));

ALTER TABLE zeep_system.builder_sessions
    ADD COLUMN IF NOT EXISTS phase TEXT NOT NULL DEFAULT 'building'
    CHECK (phase IN ('briefing', 'building'));

ALTER TABLE zeep_system.builder_turns
    ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'build'
        CHECK (mode IN ('plan', 'build')),
    ADD COLUMN IF NOT EXISTS brief_version INT,   -- brief version the turn works on
    ADD COLUMN IF NOT EXISTS milestone     INT;   -- 1-based milestone of that version

-- One row per approved plan. The newest version is the session's brief.
CREATE TABLE IF NOT EXISTS zeep_system.builder_briefs (
    session_id UUID        NOT NULL REFERENCES zeep_system.builder_sessions(id) ON DELETE CASCADE,
    version    INT         NOT NULL,
    plan       JSONB       NOT NULL,            -- the approved propose_build_plan arguments
    turn_id    UUID,                            -- the turn that proposed it
    created_by UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, version)
);
```

Notes:

- The `planning_mode` default changes behavior on upgrade: new sessions of an existing installation start with the briefing. This is the maintainer's request (D-295); it goes in the CHANGELOG as a behavior change.
- Milestone progress has no stored counter. A milestone is done when the plan marks it `done` or a turn with that `(brief_version, milestone)` ended `completed`. It is read with one query on `builder_turns`, so progress and turn history cannot disagree (same reasoning as costs C01).
- **[verify]** every `SELECT` on `builder_sessions`, `builder_turns` and `builder_settings` uses explicit column lists (`sessionColumns`, `turnColumns` do).

## 4. Tools

New kind in `tools/registry.go`:

```go
// KindPlanning tools ask the user or propose the build plan. They pause the
// turn like a proposal and are the only pausing tools offered in plan mode.
KindPlanning
```

New package `internal/agent/planning` (pure Go, no database): argument types, validation, the two renderers and the tool specs. The handlers only validate; the loop pauses on the kind, so nothing else runs before the user answers.

### 4.1 `ask_user`

```json
{ "questions": [ {
    "id": "auth",                      // ^[a-z][a-z0-9_]{0,31}$, unique in the call
    "title": "Sign-in",                // <= 40 chars
    "question": "Who signs in?",       // <= 300 chars
    "options": [ { "label": "Only my team", "description": "..." } ],   // 2 to 5; label <= 80, description <= 160
    "multiple": false
} ] }                                   // 1 to 4 questions
```

"Type your own answer" and "Decide for me" are not options the model sends; the card always adds them (ABP-02). Validation failures are `ToolError`s, so the model corrects the call instead of the user seeing a broken card.

### 4.2 `propose_build_plan`

```json
{
  "summary": "one paragraph",                                   // <= 600 chars
  "screens":     [ { "name": "Patients", "purpose": "..." } ],  // 1 to 20
  "data":        [ { "table": "patients", "holds": "...", "access": "signed_in", "access_note": "..." } ],  // 0 to 20
  "look":        { "style": "...", "colors": "...", "ui_language": "pt-BR", "reference": "..." },
  "assumptions": [ "..." ],                                     // 0 to 15, required key
  "out_of_scope":[ "..." ],                                     // 0 to 10
  "milestones":  [ { "title": "...", "deliverable": "...", "done": false } ]   // 1 to 8
}
```

- `access` is one of `public`, `signed_in`, `owner`, `role`. `public` is highlighted on the card like `rls_mode` off in the backend plan card (ABP-07).
- `done` is only for a revision of an approved plan: the agent marks what is already built, and the user sees it on the card before approving.
- Size limits (ABP-14): the arguments must fit `maxEventObjectBytes` (8 KiB, so the `proposal` event carries the whole plan) and the compact rendering must fit `MaxBriefChars = 6000`. Over either limit is a `ToolError` that names the excess, and the core prompt tells the agent to shorten. The brief in the prompt is therefore never truncated.
- The `data` list is intent in words (assumption 7). Tables are created later by `propose_backend_plan` with its own confirmation.

### 4.3 Tool set per mode (ABP-01)

| Mode | Offered | Refused |
| --- | --- | --- |
| `plan` | every `KindReadOnly` tool (`list_dir`, `read_file`, `get_build_logs`, `orbit_get_app_schema`, `orbit_list_auth_providers`, `load_skill` when skills exist) and the two `KindPlanning` tools | `KindWorkspace`, `KindProposal`, `KindExternalMutating` |
| `build` | everything; `KindPlanning` only when `planning_mode` is not `off` | — |

Enforced twice, both in the loop and both tested:

1. `toolSpecs()` filters by kind (new `Registry.SpecsWhere(func(Tool) bool)`).
2. `runCalls` refuses a call of a kind the mode does not allow, without dispatching it, with an error result ("not available while planning: ask with ask_user or call propose_build_plan"). This is the real guarantee: a plan-first turn in an old session has `write_file` calls in its history, and a model can repeat a tool it was not offered.

**Partly resolved in P04:** the shared contract table has the case `history_with_unoffered_tool` (a history with a call to a tool that is not in the request's tool list). Both adapters send the history unchanged and the case passes against both fakes; the fakes do not check tool names against the list, so the answer of the real OpenAI and Anthropic endpoints is still open and goes to P13. If a provider refuses, the fallback is to keep offering the specs of tools present in the history while the loop still refuses to run them.

**Resolved for OpenAI in P13:** the Chat Completions endpoint (`gpt-4.1`) accepted a plan-first turn whose history has `write_file`, `edit_file` and `run_build` calls while those tools were not offered. Anthropic is still unverified (no key in the run).

### 4.4 Bounds (ABP-04, ABP-05, ABP-16)

Counted by `analyzeTurn` from the turn's persisted messages (answered `ask_user` calls), so a resume cannot reset them:

- plan mode: 3 rounds per turn; build mode: 1 round per turn.
- Each "back to questions" on the plan card grants one more round.
- After "Start building now", `ask_user` is refused for the rest of the turn.

Over the bound, the gate answers with an error result telling the agent to call `propose_build_plan` with what it has and list the open points as assumptions. Known gap: if a briefing turn ends early (budget, failure) the next turn gets a fresh allowance. It needs a new user message each time, so it is bounded by the user.

## 5. Resolutions

`store.ResolveProposal` is split into an internal helper that also accepts an extra step run in the same transaction; the public function keeps its signature. New file `internal/agent/store/planning.go`:

| Store method | User action | Tool result the agent reads | In the same transaction |
| --- | --- | --- | --- |
| `AnswerQuestions` | answers a round | each question id with the chosen labels, the typed text, or "decide for me" | — |
| `AnswerQuestions` with skip | "Start building now" | "The user ended the questions. Do not ask again. Call propose_build_plan now and list every open point under assumptions." (tool meta `questions_closed`) | — |
| `ApproveBuildPlan` | approves the plan | "Plan approved as brief vN. Build milestone K now: <title>. Stop when it builds." | insert `builder_briefs`; `phase = building`; turn `mode = build`, `brief_version = N`, `milestone = K` (first not done) |
| `ReviseBuildPlan` | asks for changes in words | the user's text (capped at 2,000 chars) and "revise and call propose_build_plan again" | — |
| `ReviseBuildPlan` with `more_questions` | "back to questions" | "ask one more round with ask_user, then propose again" (tool meta `questions_reopened`) | — |

Each method checks that the pending proposal is the expected tool; otherwise `ErrProposalNotPending`. `toolMeta` gains `QuestionsClosed` and `QuestionsReopened`.

**Spec deviation (decision 1 in §12):** "Start building now" ends the questions but the plan card is still shown and approved with one click. ABP-01 allows writing after "the plan is approved or the user skips"; this design keeps a single unlock (approval), so the user always sees the assumptions and the estimate before tokens are spent on a build.

Events (whitelisted in `builderEventFields`; user-provided text is capped):

| Event | Payload |
| --- | --- |
| `proposal` (existing) | `name` = `ask_user` or `propose_build_plan`, `operation` = the arguments |
| `questions_answered` | `skipped` bool, `answers` object `{ "<id>": "<answer, <= 200 chars>" }` |
| `plan_approved` | `version`, `milestones` (count), `milestone` (the one that starts) |
| `plan_revision_requested` | `more_questions` bool |

The chat rebuilds an answered round from the `proposal` event (questions) plus `questions_answered` (answers), and an approved plan from `proposal` plus `plan_approved`.

## 6. Loop changes (`internal/agent`)

- `store.Session.Phase`, `store.Turn.Mode`, `BriefVersion`, `Milestone`; `Deps.PlanningEnabled` (from `planning_mode != off`, set in `prepareDeps`).
- `runCalls` pauses on `KindPlanning` as on `KindProposal`.
- Model calls of a plan-mode turn are recorded with `purpose = planning` (`store.PurposePlanning`, validated in Go; no DDL, as foreseen in costs design §2). Reports that sum all purposes are unchanged; the per-purpose breakdown (`group_by=purpose`) was added by `ai-builder-economy`.
- `PromptInput` gains `Brief *BriefInput` (`Version`, `Plan`, `Done`, `WorkspaceFile`): data, not rendered text. **Changed in P04:** the prompt source only knows the session, while the mode and the milestone belong to the turn, so the loop renders the brief (`planning.RenderBrief`), builds the directive (`SessionContext.Directive`) and decides whether the file is stale.
- In a build turn, when `planning.RenderBriefFile(plan, version)` differs from `WorkspaceFile` (the content of `ORBIT_BRIEF.md` at HEAD), the loop writes the file through `Builder.Op` (`FileOp{Kind: write_file, Path, Content}`) before the first model call and marks the turn as having written, so it is committed with the next checkpoint. **Resolved in P04:** the builder remembers op ids for the whole session, so the id is `<turn id>:brief-v<version>`: a resumed turn replays the write, and a later turn that must write the same version again (after a revert) is not swallowed as a replay. A builder failure on this write is logged and the turn goes on.

## 7. Prompt (ABP-03, ABP-12, ABP-13)

Section order after this change:

1. core, 2. company, 3. template guide, 4. skills index, 5. **app brief** (new), 6. session context.

- Header: `# APP BRIEF (what the user approved; lower priority than CORE RULES and company standards)`. This states the precedence ABP-13 asks for.
- The brief changes only when a plan is approved, so sections 1 to 5 are a stable prefix across the turns of a session. Milestone progress and the mode line change per turn and go in the session context (`Directive`), after the prefix.
- `planning.RenderBrief(plan)` is deterministic (same plan, same bytes) and is used for both the prompt and `ORBIT_BRIEF.md` (the file adds a title and the version).
- `prompts/core.md` gains a "Planning" block: ask only what the message and the schema do not answer; cover purpose and users, data, sign-in and permissions, screens and flows, look and interface language, exclusions; questions in the reply language, in the user's terms, never about implementation; with an existing backend read the schema first; one milestone per turn, then stop and say what is next; in build mode ask or propose a plan before a large or ambiguous change; keep the plan within the size limit.
- The directive of a plan-mode turn says the agent cannot change files, build, or propose backend changes in this turn. The tool gate enforces it; the text only saves the model a refused call.

## 8. API

All under `/dashboard/api/builder`, owner-scoped like the existing session routes, rate-limited like `confirm`/`reject`, error strings in English.

| Route | Body | Result |
| --- | --- | --- |
| `POST /sessions/{id}/turns/{turn}/answers` | `{ "answers": [ { "id", "choices": ["label"], "text": "", "decide": false } ], "skip": false }` | 202 with the turn. 400 for an unknown id, a choice that is not an option, several choices on a single-choice question, or text over 500 chars. |
| `POST /sessions/{id}/turns/{turn}/plan/approve` | — | 202 with the turn. |
| `POST /sessions/{id}/turns/{turn}/plan/revise` | `{ "feedback": "...", "more_questions": false }` | 202 with the turn. `feedback` required unless `more_questions`. |
| `GET /sessions/{id}/turns/{turn}/plan/estimate` | — | §9 |
| `GET /sessions/{id}/brief` | — | `{ version, plan, created_at, milestones: [ { index, title, done } ], versions: [ { version, created_at } ] }`, or `version: 0` when there is none |
| `GET /sessions/{id}/brief/versions/{v}` | — | one stored version |

Changes to existing routes:

- `POST /sessions/{id}/messages` accepts `plan_first` (bool) and `milestone` (int). `milestone` must be the next pending milestone of the current brief, otherwise 400; the store sets `brief_version` and `milestone` on the turn. The content is still sent by the client (localized text of the "Continue" button), so the conversation reads naturally.
- `confirm` and `reject` answer 409 when the pending proposal is a planning tool, and the new routes answer 409 for a backend proposal. **[verify]** what `ConfirmProposal` does today with an unknown proposal name.
- Session view: `phase`, `brief_version`, `milestones_total`, `milestones_done`, `next_milestone` (`{ index, title }` or null).
- Settings: `planning_mode` in `GET/PUT /builder/settings` (superadmin; audited like the other settings).
- Audit: `builder.plan.approve` with the version and the milestone count. Answers and plan text are chat content and are not audited, like messages.

**Changed in P06:**

- The three `POST` routes require `tool_call_id`: the id of the card the user acted on (`pending_proposal.tool_call_id`). A turn pauses several times, so without it a click on a card that was already replaced would answer the next round, or approve a revised plan the user has not seen. A different id is a 409 ("this card was replaced"). A repeated request for a call already resolved by the same route is a 200 with `already_resolved`, found by `store.ResolvedToolCall` on the event's `tool_call_id` (the three planning events carry it, including `plan_approved`). Any other request on a turn that is no longer paused is a 409.
- The **[verify]** is answered: `ConfirmProposal` and `RejectProposal` never see a planning tool, because `store.ResolveProposal` refuses it with `ErrProposalKind`, now mapped to 409. The new routes map the same error to 409 for a backend proposal. `ConfirmProposal` still checks the backend role first, so a viewer gets 403 before the 409.
- The brief fields (`brief_version`, `milestones_total`, `milestones_done`, `next_milestone`) are in the session detail (`GET /sessions/{id}`) only, not in the list, which would cost one progress query per session. The turn view gains `mode`, `brief_version` and `milestone`.
- `messages` answers 400 for `plan_first` with `milestone`, and for `plan_first` when `planning_mode` is `off`.
- Answering a round and revising a plan are audited too (`builder.questions.answer` with `skipped`, `builder.plan.revise` with `more_questions`), with no text, like `confirm` and `reject`.
- A skip may carry answers: they are sent to the agent before the "do not ask again" text.

## 9. Estimate (ABP-08)

Source: completed build-mode turns of the installation in the last 90 days, for the provider kind and model configured now. Per turn, the ledger gives total tokens and total cost.

```
tokens_low, tokens_high = percentile_cont(0.25), percentile_cont(0.75) of per-turn tokens
cost_low,   cost_high   = the same over per-turn cost, using only turns in which every call
                          has a cost and that ran after cost_currency_changed_at
```

- Fewer than 10 turns in the sample: `available: false` ("no estimate available"). Fewer than 10 fully priced turns: tokens only, cost null.
- First milestone = the range above. Whole plan = the range times the number of milestones not done.
- Response: `{ available, sample_turns, currency, milestone: { tokens_low, tokens_high, cost_micros_low, cost_micros_high }, plan: { ... } }`. The frontend formats; it computes nothing.
- The card labels it an estimate and says it comes from past builds on this installation.
- It uses turns of every user, as percentiles with a minimum sample. A member can therefore infer the typical cost of a build on the installation (decision 4 in §12).
- Known limit: the first build of an app usually costs more than a later milestone, and this v1 uses one distribution. Refined by `ai-builder-economy`: with at least 10 turns of milestone 1 and 10 of later milestones the estimate is split (`basis: split`); otherwise this single distribution still applies.

**Changed in P07:**

- The sample is defined per turn: a completed `build`-mode turn created in the window. Its total adds only the calls whose `purpose` is not `planning`: a turn whose plan was approved keeps going and becomes `build`, and its planning calls are not the cost of building a milestone. A turn is left out when any of those calls used another provider or model, or has `usage_reported = false` (its tokens would read as zero and pull the range down). A turn is priced when every one of those calls has a cost and its first call is on or after `cost_currency_changed_at`.
- "The model configured now" is the model of the enabled `ai_providers` row named by `builder_settings.provider`, read without the key. No such row: `available: false`, `sample_turns: 0`.
- The route answers only for a turn paused on `propose_build_plan` (409 otherwise, with the messages of the other planning routes; 400 when the stored plan no longer parses). The whole-plan factor is the number of milestones of that pending plan not marked `done`; the response carries it as `milestones`.
- Query plan, measured on the local Postgres 16.15 with 300,000 ledger rows (300 sessions, 30,000 turns over 180 days, half of them in the window): 44 ms, parallel sequential scan of the ledger and hash join with the turns. A sequential scan is the right plan while the window holds a large share of the ledger; the cost grows with the ledger volume of the last 90 days and is the same for every session, so a cached value is the obvious step if it shows up in `ai-builder-economy`. No index was added.
- `GET brief` and `GET brief/versions/{v}` are owner-scoped like `GET /sessions/{id}` (another user's or an archived session is 404); a version that is not a positive integer or does not exist is 404 `brief version not found`. The three GET routes are not rate-limited, like the other session reads.

## 10. Frontend

| Piece | Where | Notes |
| --- | --- | --- |
| `QuestionCard` | `components/patterns/builder/` | One block per question: option buttons (radio or checkbox semantics by `multiple`), a text field, "Decide for me". Footer: "Send answers" and "Start building now". Answers are kept in component state until sent. Keyboard and screen-reader behavior follows the existing `ProposalCard`. |
| `PlanCard` | same | Summary, screens, data with the `public` highlight, look, assumptions (visually distinct), out of scope, numbered milestones with done marks, estimate line. Actions: "Build this" (approve), "Ask for changes" (opens a text field), "Back to questions". |
| Card selection | `ChatPanel.tsx` | By `pending_proposal.name`; the backend `ProposalCard` stays the default. |
| History | `builderModel.ts`, `ActivityTimeline.tsx` | Answered rounds and approved plans rendered read-only from the events of §5. |
| Composer | `ChatPanel.tsx` | "Plan first" switch (hidden when `planning_mode = off`); in the briefing phase the placeholder says the agent will ask before building. |
| Continue | `ChatPanel.tsx` | After a finished turn with `next_milestone`, a button "Continue: <title>" sends the message with `milestone`. |
| Brief | new `BriefDrawer`, button in the session header | Current brief, progress, version list (read-only). Changing it is done by asking the agent. |
| Settings | `settings/` builder section | `planning_mode` select with a one-line explanation of each value. |
| `api.ts` | | Types and hooks: `useAnswerQuestions`, `useApprovePlan`, `useRevisePlan`, `usePlanEstimate`, `useBuilderBrief`; every mutation with `onError` toast. Session and settings types gain the new fields. |

Every string in `en.json` and `pt-BR.json`.

**Changed in P08:**

- The hook is `useAnswerBuilderQuestions`, named like the other builder hooks of `api.ts`.
- The answered round in the history shows a blank answer as "Left to the assistant", on a skip too: `questions_answered` stores `""` for both "decide for me" and a blank, and the agent decides both.
- The composer's blocked reason depends on the pending card; the screen-reader live text and the publish hint no longer name a backend change.

**Changed in P09:**

- The hooks are `useApproveBuilderPlan`, `useReviseBuilderPlan` and `useBuilderPlanEstimate`. The estimate query is keyed by the tool call, so a revised plan gets its own figures; it does not retry and shows no toast: the card says "no estimate available".
- A plan the page cannot read (no milestone left after dropping items without a name) is not approvable from the card: it offers "Ask for changes" and "Back to questions" only.
- A table whose access is not one of the four values is drawn as a warning, like `public`.
- A plan sent back is shown in the history as a note on the user's side (changes asked, or more questions), not as a timeline row: the timeline of a finished turn is collapsed, and it is the user's action. The user's words are chat content and are not in the event, so the note does not show them.

**Changed in P10:**

- The brief hooks are `useBuilderBrief` (fetched only while the drawer is open, refetched when the session's brief version or progress moves) and `useBuilderBriefVersion` (a stored version never changes). `useSendBuilderMessage` takes `{content, plan_first?, milestone?}`.
- The Brief button sits in the header row with Publish and Download and shows the milestones built ("Brief · 1/3"). The drawer draws the current plan with the done marks of the server's progress, not the plan's own `done`, and shows an older version with a notice that it is not the current brief.
- With `off`, only the "Plan first" switch and the briefing placeholder are hidden. A session that already has a brief keeps the Brief button and "Continue": the server accepts `milestone` in every mode, and the brief is still in the prompt.
- "Continue" is offered while the session is building, has a `next_milestone` and the composer is not blocked. "Plan first" is offered while the session is building and the mode is `required` or `suggested`; it is off again after each message.

## 11. Edge cases against the design

| Case | Behavior |
| --- | --- |
| First message answers everything | The agent calls `propose_build_plan` directly; zero rounds. |
| User leaves during the briefing | The turn stays `awaiting_confirmation`; the card is rebuilt from the session view on return. |
| Budget ends during the briefing | Turn ends `budget_exceeded`; answered rounds are persisted tool messages. |
| Plan rejected many times | No limit on revisions; each is a model call and the 80% budget warning applies. |
| Revert to a turn before the approval | Brief versions are not reverted. `ORBIT_BRIEF.md` is, and the next build turn writes it again. Documented as a limitation. |
| Message sent with `milestone` while the brief changed | 400; the client refetches the session and shows the new next milestone. |
| Turn of a milestone ends `limit_reached` or `failed` | The milestone stays pending; "Continue" offers it again. |
| Session without a brief (old sessions, `off`) | No brief section, no file, no Continue button. Asking for a plan creates version 1. |

## 12. Decisions (accepted as written by the maintainer on 2026-11-13)

1. **Skip still shows the plan card** (§5). Alternative: after "Start building now" the plan is approved automatically and the build starts with no further click. It needs an auto-approval path in the loop and removes the last look at the assumptions and the estimate.
2. **`required` is the default, also for existing installations** (§3).
3. **File name and place of the brief in the workspace**: `ORBIT_BRIEF.md` at the root, next to `ORBIT_AGENT.md`. It ships in the downloaded code and is not part of the built site.
4. **The estimate uses the usage of all users of the installation**, aggregated (§9). Alternative: only the caller's own history, which leaves most users with no estimate.
5. **Milestone progress is decided by the server** (a turn that ends `completed`), not by a tool the agent calls. Cheaper and it cannot be claimed without a finished turn; the cost is that a turn the agent ends early still counts as done, and the user corrects it in words.
