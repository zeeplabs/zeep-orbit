# Builder projects without a backend (sites, landing pages)

**Status**: Draft 2026-11-24, not implemented. Decisions 1 to 3 confirmed by the maintainer the same day; the rest is proposal.

## Problem

Every builder session creates or reuses an Orbit backend app. `builder_sessions.backend_app_id` is `NOT NULL`, `resolveBuilderBackend` only accepts `new` or `existing`, and the default template carries the SDK, a sign-in screen and a backend status panel. Someone who asks for a landing page or a one-page site gets an empty backend they never use, a template shaped for data screens, and a design pass written for CRUD apps.

## Decisions

1. **New backend mode `none`.** The chat-home dropdown gets "No backend" and it is the **default**. "New backend" and "Existing backend" stay.
2. **The planner asks whether the project stores data** when the brief does not settle it. It decides alone only for a clear site (landing page, one-pager, portfolio, menu, event page) and says so in the plan assumptions. This is the safety net for the new default: a prompt that describes an app ("manage appointments") must not silently become a site.
3. **A separate built-in template `site`**, not the default template with the data layer switched off. The design kit `orbit-ui` stays a single source in the repo and is copied into both templates when the image is built.

## Requirements

- **NB-01** `POST /api/builder/sessions` accepts `backend: {mode: "none"}`. A body without `backend` and without `backend_app_id` keeps meaning `existing` (legacy contract unchanged). `none` cannot be combined with an app id (English error). The session is created with no backend app and no backend app is created.
- **NB-02** `builder_sessions.backend_app_id` becomes nullable (additive migration, existing rows untouched). `backend_origin` gets a third value `none`. Store, session JSON (`backend_app_id` is `null`) and every reader handle the empty case; nothing assumes a non-empty id.
- **NB-03** A `none` session uses the template kind `default` row's sibling: built-in template `site` (`builder/templates/site`, `TEMPLATE_VERSION`, `orbit-template.json` with `ui_kit`). It has no SDK dependency, no `src/orbit.config.ts`, no sign-in or backend status. The workspace starts with the same kit (`orbit-ui`, Tailwind tokens, Base UI, lucide-react) so design quality work (builder-design-quality) applies unchanged.
- **NB-04** Backend tools are not offered when the session has no backend: `orbit_get_app_schema`, `propose_backend_plan`, backend edit proposals. If the model calls one anyway the tool answers with the model-visible `builderAppUnavailableMsg`, as it does today for a missing app. The system prompt omits the "Backend app" line and says the project has no backend and must not call or invent an API.
- **NB-05** Planner: the question "does this project store data (accounts, orders, bookings, form submissions)?" is asked when the brief is ambiguous. The plan states the choice under assumptions. Answering "yes" to a `none` session is handled by NB-08, not by silently continuing.
- **NB-06** Skills: `orbit-frontend-design`, `orbit-responsive-layout` and `orbit-accessibility` apply to `site`. `orbit-crud-screens` and `orbit-forms-and-states` are written for data screens; they are not listed in the prompt index of a `none` session (or their text says when to skip, `[to decide in design]`). One new built-in skill for sites (sections, hero, calls to action, meta tags, no layout shift) is a design question, not a requirement here.
- **NB-07** Publish works without a backend: the frontend app is created with `backend_app_id` NULL (that column is already nullable), no backend origin or CORS is configured, and the published site is static. `[to verify]` that nothing in the publish and deploy path reads the session's backend id.
- **NB-08** "Add a backend later": from a `none` session the user can attach a new backend. It creates the app, sets the session's `backend_app_id` and `backend_origin`, and adds the data layer (SDK dependency, `orbit.config.ts`, auth pages) to the workspace from an overlay owned by the default template. **Phase 2**: not part of the first release; until then the UI says a backend can only be chosen when the project is created.
- **NB-09** UI: the chat-home dropdown shows "No backend" first and selected; the builder header says "No backend" in place of "App de backend"; the project list and filters handle sessions with no backend app; the plan card shows no backend section. Every string in `en.json` and `pt-BR.json`.
- **NB-10** Audit: `builder.session.create` records `backend_origin: "none"` and an empty `backend_app_id`.

## Out of scope

Server-side form handling for sites (contact forms, newsletter): a site that needs one needs a backend, and NB-05 sends it there. A third-party form service. Converting an app session into a site. Custom domains beyond what publish already does.

## Risks

- **The default changes who gets what.** A user who types an app prompt and does not touch the dropdown gets a site unless the planner asks. Mitigation: NB-05, plus a measurement before release: run the prompts from the earlier validation (bike shop, gym, repairs, bakery, bookshop) with the new default and count how many reach the data question. `[Incerto]` until measured.
- **`backend_app_id` nullable touches many readers.** Builder code: `builder_tools.go`, `builder_proposals.go`, `builder_runtime.go`, `builder_publish.go`, `builder_common.go`, `agent/prompt.go`, `agent/loop.go`, `agent/store/store.go`. The `ON DELETE CASCADE` foreign key stays; a `none` session is never cascaded by a backend delete. Many other `backend_app_id` references in the repo (app members, RBAC, frontend apps) are other tables and not affected.
- **Two templates drift.** The kit is shared by a copy step at image build time; a test fails when the two copies differ.
- **Template guide cap.** The `ui_kit` block is prepended to the guide (builder-design-quality T9) and applies to `site` as is.
- **Migration is additive but not reversible by the app.** Making the column nullable is safe to roll forward; rolling back needs a manual `NOT NULL` once no `none` session exists.
