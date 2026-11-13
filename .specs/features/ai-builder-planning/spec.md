# AI Builder Planning Specification

> Status: **Approved by the maintainer on 2026-11-13 together with `design.md` and `tasks.md`. The two deviations from this spec are marked in the design (§2 and §5).** Child of `.specs/features/ai-frontend-builder/`. Requested by the maintainer on 2026-11-13: before it builds anything, the agent must lead the user to give as much information as it needs, through a strong planning step.

## Problem Statement

Today the first message of a builder session goes straight to the agent loop with every tool available. The agent starts writing files from one sentence ("a CRM for my clinic"). What it does not know, it guesses: which screens, which data, who signs in, what it should look like. A wrong guess costs a full build cycle in tokens and time, and then another one to undo it. The user also has no moment to see what is about to be built, what the backend will hold, or what it will cost.

Planning is the cheapest place to be wrong. A question costs a few hundred tokens; a rebuilt app costs hundreds of thousands.

## Goals

- [ ] A new session starts with a **briefing**: the agent asks what it needs, in short structured questions, before it can change any file or the backend.
- [ ] The briefing ends in an **app brief** and a **build plan** the user reads and approves: screens, data and who can reach it, the order of work, and an estimated cost range.
- [ ] The approved brief stays with the project and is given to the agent on every later turn, so it does not have to be rediscovered or re-explained.
- [ ] The user is never trapped in questions: every question has suggested answers and a "you decide" option, the number of rounds is bounded, and "start building" is always available.
- [ ] After the first build, planning is available on demand and is suggested by the agent for large or ambiguous requests.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Free-form requirements documents uploaded by the user (PDF, DOCX) | Needs file parsing. Pasted text in the chat already works. Revisit with image attachments. |
| Wireframe or visual mockup generation during the briefing | Belongs to the experience spec. The plan is text. |
| Automatic execution of the whole plan without stopping | Each milestone ends in a build the user can see. Unattended multi-milestone runs need budget rules first (`ai-builder-costs`). |
| Editing the plan as a task board (drag, reorder, assign) | A numbered list the user can ask the agent to change is enough for v1. |
| Exact cost quotes | The estimate is a range from past usage. It is shown as a range and labeled an estimate. |

---

## Assumptions & Open Questions

| # | Decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | When the briefing is mandatory | On the first turn of every new session. Operator setting `planning_mode`: `required` (default), `suggested`, `off`. | Matches the maintainer's request and still lets an operator who wants one-shot builds turn it off. | No |
| 2 | How much the agent may ask | At most 3 rounds, at most 4 questions per round. The agent asks only what the first message did not answer. | "As much information as possible" without an interrogation. A detailed first message can need zero questions and go straight to the plan. | No |
| 3 | Skipping | "Decide for me" per question and "Start building now" for the whole briefing. Whatever was not answered is written in the brief as an **assumption**, visible to the user. | The user must be able to move on; the agent must not hide what it guessed. | No |
| 4 | What the agent can do during the briefing | Read the backend schema and auth providers (existing backends), load skills, ask questions, propose the plan. It cannot write files, run builds or propose backend changes. | A planning step that can already edit is not a planning step. Enforced by the tool registry, not by the prompt. | No |
| 5 | Where the brief lives | In the database, versioned per session, and written to the workspace as a file so it ships with the code. A compact form is placed in the stable part of the system prompt. | The brief is the project's memory. A stable prefix is also what provider prompt caches reward. | No |
| 6 | Which model plans | The same model as the build in v1. A cheaper model for the briefing arrives with model tiers in `ai-builder-economy`. | Keeps this spec independent of the tiers work. | No |
| 7 | Backend plan inside the build plan | The build plan lists the tables and their access in words. The actual backend change still goes through `propose_backend_plan` and its own confirmation when the milestone that needs it runs. | One approval path for the backend (BBE-07). The build plan is intent, not an applied change. | No |
| 8 | Sessions created before this feature | Treated as already in the building phase, with an empty brief the user can fill by asking for a plan. | No migration of old conversations. | No |

---

## User Stories

### P1: Briefing before the first build ⭐ MVP

**US-1**: As a builder user, I want the agent to ask me what it needs before it builds, so that the first version is close to what I want.

**Acceptance Criteria**

1. ABP-01 WHEN a session is created and `planning_mode` is `required` THEN the session SHALL start in the `briefing` phase, and the agent SHALL NOT be able to call a tool that writes files, runs a build or proposes a backend change until a plan is approved or the user skips the briefing. The restriction SHALL be enforced by the set of tools offered to the model.
2. ABP-02 The agent SHALL ask through a structured tool, not free text: each question has a short title, the question, 2 to 5 suggested answers, whether several can be chosen, and always accepts a typed answer and "decide for me".
3. ABP-03 The agent SHALL cover, by asking or by reading the user's message: what the app is for and who uses it; the main things it stores; who signs in and what each kind of user may do; the main screens and flows; the look (style, reference, colors, language of the interface); and anything that must be left out. It SHALL NOT ask what the user already said.
4. ABP-04 The briefing SHALL be bounded: at most 3 rounds of at most 4 questions. WHEN the bound is reached THEN the agent SHALL produce the plan with what it has.
5. ABP-05 The user SHALL be able to end the briefing at any time ("Start building now"). The agent SHALL then produce the plan, marking every open point as an assumption.
6. ABP-06 A pending question round SHALL survive a page reload and a server restart, and SHALL pause the turn without consuming the turn's time limit, like a pending backend proposal.

### P1: Brief and build plan ⭐ MVP

**US-2**: As a builder user, I want to read and approve what will be built before it is built.

**Acceptance Criteria**

1. ABP-07 The briefing SHALL end in a plan card with: a one-paragraph summary; the list of screens; the data (tables in words, and who can reach each one, with open-to-everyone highlighted as in the backend plan card); the assumptions the agent made; and the ordered milestones, each small enough to end in one visible build.
2. ABP-08 The plan card SHALL show an estimated token and cost range for the first milestone and for the whole plan WHEN the installation has usage history and prices to estimate from; otherwise it SHALL say that no estimate is available. It SHALL be labeled an estimate.
3. ABP-09 The user SHALL be able to approve the plan, ask for changes in words (the agent revises and shows the plan again), or discard it and go back to questions.
4. ABP-10 WHEN the plan is approved THEN the session SHALL move to the `building` phase, the brief SHALL be stored as a new version, written to the workspace, and the agent SHALL start the first milestone in the same turn.
5. ABP-11 The agent SHALL build one milestone per turn and stop, reporting what was done and what the next milestone is; the user continues with one click or redirects in words.

### P1: The brief as project memory ⭐ MVP

**US-3**: As a builder user, I do not want to repeat myself in later turns.

**Acceptance Criteria**

1. ABP-12 On every turn of a session with a brief, a compact form of the current brief and the plan's progress (done and pending milestones) SHALL be part of the system prompt, placed in its stable part.
2. ABP-13 The user SHALL be able to open the brief, see its versions, and ask the agent to change it. A superadmin's persona and rules still take precedence over the brief.
3. ABP-14 The brief SHALL have a size limit, and the agent SHALL be told to keep it within the limit when revising it.

### P2: Planning on demand

**US-4**: As a builder user, I want to plan a large change before it is made.

**Acceptance Criteria**

1. ABP-15 The composer SHALL have a "Plan first" switch. With it on, the turn runs with the briefing tool set (no writes) and ends in a plan card for that change.
2. ABP-16 WHEN a request in the building phase is large or ambiguous THEN the agent SHALL be instructed to ask one round of questions or propose a plan instead of editing; the user can answer "just do it".
3. ABP-17 A superadmin SHALL be able to set `planning_mode` to `required`, `suggested` (the agent decides whether to brief on the first turn) or `off`.

### P3

1. ABP-18 Briefing templates per kind of app (CRM, booking, internal tool, landing page) that pre-fill questions and suggested answers.
2. ABP-19 Reuse of a brief from another session as a starting point.

---

## Edge Cases

- The first message already answers everything: zero question rounds, straight to the plan card.
- The user answers a question round with free text that changes the scope: the agent may use a remaining round; it does not exceed the bound.
- The user leaves during the briefing and returns days later: the pending round is still there (ABP-06).
- Existing backend: the agent reads the schema first and asks about screens and look, not about data that already exists.
- The user rejects the plan three times: there is no limit on plan revisions, but each revision is a model call and counts toward the budget; the budget warning applies.
- Budget exhausted during the briefing: same outcome as any turn (`budget_exceeded`); answers given so far are kept.
- `planning_mode` changed from `required` to `off` while sessions are in the briefing phase: those sessions move to building on their next turn, keeping any answers as the brief.

---

## Success Criteria

- In the eval set, the share of sessions whose first build is accepted without a corrective turn is higher with the briefing than without it, at a lower total token cost per accepted app. Both numbers are recorded per model (depends on the eval runner, parent T42, and on `ai-builder-costs`).
- A user can go from an empty session to an approved plan without typing more than the first message, using only suggested answers.
- No file change and no backend change is recorded before the plan approval or the explicit skip, in any session created with `planning_mode = required` (asserted by a test on the tool set, not by reading prompts).
