# Builder projects without a backend (sites, landing pages)

**Status**: 2026-11-25: NB-01 to NB-07, NB-09 and NB-10 implemented and merged into `develop` (#66 to #72); NB-08 (add a backend later) is phase 2 and not started. Decisions 1 to 3 confirmed by the maintainer 2026-11-24. The risk of the new default is not measured yet (task T9).

## Problem

Every builder session creates or reuses an Orbit backend app. `builder_sessions.backend_app_id` is `NOT NULL`, `resolveBuilderBackend` only accepts `new` or `existing`, and the default template carries the SDK, a sign-in screen and a backend status panel. Someone who asks for a landing page or a one-page site gets an empty backend they never use, a template shaped for data screens, and a design pass written for CRUD apps.

## Decisions

1. **New backend mode `none`.** The chat-home dropdown gets "No backend" and it is the **default**. "New backend" and "Existing backend" stay.
2. **The planner asks whether the project stores data** when the brief does not settle it. It decides alone only for a clear site (landing page, one-pager, portfolio, menu, event page) and says so in the plan assumptions. This is the safety net for the new default: a prompt that describes an app ("manage appointments") must not silently become a site.
3. **A separate built-in template `site`**, not the default template with the data layer switched off. The design kit `orbit-ui` stays a single source in the repo and is copied into both templates when the image is built.

## Requirements

- **NB-01** `POST /api/builder/sessions` accepts `backend: {mode: "none"}`. A body without `backend` and without `backend_app_id` keeps meaning `existing` (legacy contract unchanged). `none` cannot be combined with an app id (English error). The session is created with no backend app and no backend app is created.
- **NB-02** `builder_sessions.backend_app_id` becomes nullable (additive migration, existing rows untouched). `backend_origin` gets a third value `none` (its CHECK constraint is widened). Store, session JSON (`backend_app_id` is `null`) and every reader handle the empty case; nothing assumes a non-empty id.
- **NB-03** A `none` session uses a new built-in template kind, `site` (`builder/templates/site`, `TEMPLATE_VERSION`, `orbit-template.json` with `ui_kit`). It has no SDK dependency, no `src/orbit.config.ts`, no sign-in or backend status. The workspace starts with the same kit (`orbit-ui`, Tailwind tokens, Base UI, lucide-react) so design quality work (builder-design-quality) applies unchanged.
- **NB-04** Backend tools are not offered when the session has no backend: `orbit_get_app_schema`, `propose_backend_plan`, backend edit proposals. If the model calls one anyway the tool answers with the model-visible `builderAppUnavailableMsg`, as it does today for a missing app. The system prompt omits the "Backend app" line and says the project has no backend and must not call or invent an API.
- **NB-05** Planner: the question "does this project store data (accounts, orders, bookings, form submissions)?" is asked when the brief is ambiguous. The plan states the choice under assumptions. Answering "yes" to a `none` session is handled by NB-08, not by silently continuing.
- **NB-06** Skills: `orbit-frontend-design`, `orbit-responsive-layout` and `orbit-accessibility` apply to `site`. `orbit-crud-screens` and `orbit-forms-and-states` are written for data screens; they are not listed in the prompt index of a `none` session (or their text says when to skip, `[to decide in design]`). One new built-in skill for sites (sections, hero, calls to action, meta tags, no layout shift) is a design question, not a requirement here.
- **NB-07** Publish works without a backend: the frontend app is created with `backend_app_id` NULL (that column is already nullable), no backend origin or CORS is configured, and the published site is static. Verified in T1: publish and deploy do not read the session's backend id.
- **NB-08** "Add a backend later": from a `none` session the user can attach a new backend. It creates the app, sets the session's `backend_app_id` and `backend_origin`, and adds the data layer (SDK dependency, `orbit.config.ts`, auth pages) to the workspace from an overlay owned by the default template. **Phase 2**: not part of the first release; until then the UI says a backend can only be chosen when the project is created. Detailed in "NB-08 in detail" below.
- **NB-09** UI: the chat-home dropdown shows "No backend" first and selected; the builder header says "No backend" in place of "App de backend"; the project list and filters handle sessions with no backend app; the plan card shows no backend section. Every string in `en.json` and `pt-BR.json`.
- **NB-10** Audit: `builder.session.create` records `backend_origin: "none"` and an empty `backend_app_id`.

## NB-08 in detail (phase 2): add a backend to a project that has none

Status: spec only, not started. Written 2026-11-27 after T9 (validation.md): app briefs sent to "No backend" end in a plan that does not build the app, so the user starts again; this section is what lets the default go back to "No backend".

### Behavior

A session with `backend_origin = none` can be given a backend once. After it, the session behaves exactly like one created with "New backend" (or "Existing backend"): backend tools, prompt line, skills and proposals all key on `backend_app_id` being set (T4), so they switch on by themselves.

- **NB-08a Endpoint.** `POST /dashboard/api/builder/sessions/{id}/backend` with `{"mode": "new"}` or `{"mode": "existing", "app_id": "<uuid>"}`. Owner only (404 for another user's session, as everywhere). `existing` needs at least viewer on the app, as at create. Not `none`. Answers `409` when the session already has a backend (`session already has a backend`), `409` when a turn is in progress (the one-active-turn rule), `404` for an app the caller cannot see, `202` with the new turn otherwise.
- **NB-08b It is a turn, not a request that edits files.** Like revert, it is a turn of its own kind (`attach_backend`) so the worker runs the workspace changes under the one-active-turn lock, commits them and stores the snapshot with the machinery that already exists. No model call, no tokens spent (as revert).
- **NB-08c What the overlay adds.** Only new files and one dependency, never an edit of a file the user or the agent wrote:
  `src/orbit.config.ts` (generated, as at create), `src/lib/orbit.ts`, `src/lib/storage.ts`, the `@zeeptech/orbit-client` dependency in `package.json` (merged as JSON, lockfile updated by the install step) and the template guide `ORBIT_AGENT.md` replaced by the default template's. The overlay is a directory in the default template (`builder/templates/default/overlay`? `[to decide in design]`), versioned with it, so the SDK version has one source. No router, no sign-in page, no `BackendStatus`: those are screens, and the agent builds them in the next turns with the backend tools. If a path the overlay writes already exists, the turn fails with a clear message and nothing is written (the agent may have created its own `src/lib/orbit.ts`).
- **NB-08d One commit, revertible.** The overlay is one commit. Reverting to a turn before it restores the files but not the database row; the revert must therefore be refused across an attach (`400 cannot revert across a backend change`), `[to confirm in design]` against the revert rules.
- **NB-08e Order and compensation.** Create the app (new mode) -> run the turn -> set `backend_app_id` and `backend_origin` with one conditional `UPDATE ... WHERE backend_origin = 'none' AND backend_app_id IS NULL`. Zero rows updated means someone else attached first: the created app is removed (as BBE-02) and the answer is 409. A failed overlay turn removes the app it created and leaves the session on `none`.
- **NB-08f Audit.** `builder.session.backend.attach` with `backend_origin`, `backend_app_id` and the turn seq. No file content.
- **NB-08g Planner path.** In a `none` session, when the user answers "yes, it stores data", the plan card (static part only, by T7) offers **Add a backend** next to Approve. Choosing it attaches a new backend and then continues planning for the data part, instead of telling the user to start a new project. The planner text "needs a new project with a backend" is replaced by "needs a backend; add one" once this ships. Attaching always needs the user's click: it creates an app, so the model never triggers it.
- **NB-08h UI.** The builder header for a `none` session shows **Add a backend** (menu: new, or an existing app) in place of the "Start a project with a backend" link, with a confirmation that says what is created (an empty Orbit app, the SDK files, no data yet). The progress is the turn's, as for revert. Every string in `en.json` and `pt-BR.json`.
- **NB-08i Default.** When NB-08a to NB-08h are merged and measured, the chat-home default returns to "No backend". The measurement repeats T9's five app briefs plus the "yes" answer, and counts how many end in an app. That change is its own PR.

### Decisions to confirm before design

1. **Existing backends too?** Recommended yes: it costs one `existing` branch and a user with a backend should not have to create another. Alternative: new only.
2. **Published sessions.** A session that was already published has a static frontend app. Attaching afterwards means the published site must be allowed by the backend's CORS and the frontend app row linked to it. `[Incerto]`: T1 found that publish does not read the session's backend id, so this may need nothing, or a CORS update; verify first (T11a). Recommended: allow, once verified.
3. **Revert across an attach.** Recommended: refuse (NB-08d). Alternative: allow and treat as an attach in reverse, which is a detach feature this spec does not include.

### Risks

- **A file the agent already wrote collides with the overlay.** Mitigation: fail before writing; the message names the paths. A site rarely has `src/lib/orbit.ts`, but the agent could have invented one.
- **The overlay and the default template drift** (SDK version). Mitigation: the overlay lives in the default template and a test checks that a fresh default template and `site + overlay` agree on the SDK version and on the files the overlay owns.
- **A half-attached session.** Mitigation: NB-08e (single conditional update, compensation on every failure) and a test per failure point.
- **Cost of a wrong default is only removed if the planner path works.** NB-08g is part of the feature, not a follow-up; without it the user still has to understand the dropdown.
- **Prompt and plan stale after attach.** The brief says "static site"; after attach the agent must plan the data part. `[Incerto]` how the existing plan/revise flow carries a new brief version; design reads the planning code first (T11b).

## Out of scope

Server-side form handling for sites (contact forms, newsletter): a site that needs one needs a backend, and NB-05 sends it there. A third-party form service. Converting an app session into a site. Custom domains beyond what publish already does.

## Risks

- **The default changes who gets what.** A user who types an app prompt and does not touch the dropdown gets a site unless the planner asks. Mitigation: NB-05, plus a measurement before release: run the prompts from the earlier validation (bike shop, gym, repairs, bakery, bookshop) with the new default and count how many reach the data question. `[Incerto]` until measured.
- **`backend_app_id` nullable touches many readers.** Builder code: `builder_tools.go`, `builder_proposals.go`, `builder_runtime.go`, `builder_publish.go`, `builder_common.go`, `agent/prompt.go`, `agent/loop.go`, `agent/store/store.go`. The `ON DELETE CASCADE` foreign key stays; a `none` session is never cascaded by a backend delete. Many other `backend_app_id` references in the repo (app members, RBAC, frontend apps) are other tables and not affected.
- **Two templates drift.** The kit is shared by a copy step at image build time; a test fails when the two copies differ.
- **Template guide cap.** The `ui_kit` block is prepended to the guide (builder-design-quality T9) and applies to `site` as is.
- **Migration is additive but not reversible by the app.** Making the column nullable is safe to roll forward; rolling back needs a manual `NOT NULL` once no `none` session exists.
