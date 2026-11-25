# Builder projects without a backend: design

## Shape of the change

Three layers, in this order, each shippable on its own:

1. **Session without a backend** (API, store, readers). No UI, no new template. Behind the existing contract: nothing creates a `none` session until layer 3.
2. **Template `site` and prompt** (template files, kit copy, prompt text, tools gating, skills).
3. **Entry points** (dropdown default, planner question, UI states). This is the layer that changes what users see; it ships last.

## Layer 1: session without a backend

- `builder_sessions.backend_app_id` becomes nullable in `provisioner.go` (`ALTER COLUMN ... DROP NOT NULL`, idempotent). `backend_origin` accepts `none`; `store.BackendOriginNone`. The column has an inline `CHECK (backend_origin IN ('existing', 'builder'))` (`provisioner.go`), so the migration also drops that constraint (auto-named `builder_sessions_backend_origin_check`) and adds one that includes `none`; both statements idempotent.
- `store.CreateSessionParams.BackendAppID` empty is valid only with `BackendOrigin == none` (the existing check at `store.go:282` becomes: backend id required unless origin is `none`). `Session.BackendAppID` stays a string, empty for `none`; scanning uses `COALESCE(backend_app_id::text, '')`. The HTTP view returns `backend_app_id: null` for `none` (a `*string` in the view only), so a client cannot mistake an empty string for an id; the store keeps a plain string.
- `resolveBuilderBackend` gets `builderBackendModeNone = "none"`; same validation style as `new`.
- Readers, each with the empty case:
  - `builder_tools.go ownerAndApp` already returns the model-visible unavailable message for an empty id; the tool list for the turn drops backend tools instead of relying on the error.
  - `builder_proposals.go`: backend proposals on a `none` session answer 409 (English error).
  - `builder_runtime.go promptSource`: skip the `zeep_system.apps` name lookup.
  - `agent/prompt.go`: no "Backend app" line; adds the no-backend sentence.
  - `builder_publish.go`: no change needed. `CreateFrontendApp` already stores an empty id as NULL (`NULLIF($9, '')::uuid`), and the publish role check is on the frontend app (`AppRef{FrontendAppID}`), not on the backend.
  - `builder_common.go` view, `ProjectsPage`/`projects.ts` grouping and the builder header.

## Layer 2: template `site` and prompt

- `builder/templates/site/`: Vite + React + TypeScript + Tailwind, `orbit-ui` kit, tokens in `src/index.css`, routes limited to what a site needs (a single page plus an optional second one), `index.html` with title and description meta, `public/` for images. No `@zeep/*` SDK, no `orbit.config.ts`, no `BackendStatus`, no sign-in.
- Selection (as built): `builderSiteTemplateDir = "site"` next to `builderDefaultTemplateDir`; the create handler Inits the workspace straight from `site` when the mode is `none`. No new `template_kind` and no second hidden template row: `orbit-template.json` of the two templates is the same (static site, `dist`), so publish treats both as the built-in default. The session pins the site template's own version, reported by the builder health (`site_template_version`).
- Kit single source: move `builder/templates/default/src/components/ui`, `src/lib/cn.ts` and the token block that the kit reads to `builder/templates/_kit/`, and copy them into both templates in `Dockerfile.builder`. In the repo the two copies stay checked in (so `npm run build` of each template works on its own) and a Go test compares them and fails on a difference. `[Incerto]`: copy-at-build alone would make local template builds fail; the checked-in copy plus a test is the simpler option and is the proposal.
- Prompt: `core.md` gets a short section for sessions with no backend. Same cap discipline as before: pin text in `prompt_test.go`.
- Skills: the index for a `none` session leaves out `orbit-crud-screens` and `orbit-forms-and-states` (they teach data screens against the SDK). If that filter is a bigger change than it looks, the fallback is one sentence in each skill's description saying "not for sites"; both are small, choose when implementing.

## Layer 3: entry points

- Dropdown order: No backend (default), New backend, Existing backend. The choice is not remembered between visits, so the default stays the default; revisit if people complain.
- Planner question (NB-05): a rule in `core.md` plus the planning prompt. It is a normal `ask_user` question with two options and "decide for me", so it costs one question and no new UI.
- Measure before release (see Risks in the spec): a fixed set of briefs, count of how many reach the data question, cost of a site run versus an app run.

## Phase 2: add a backend later (NB-08)

`attachBackend(sessionID)`: creates the app through the same operation as `new`, sets `backend_app_id` and `backend_origin = builder` in one update guarded by `backend_app_id IS NULL`, then writes the data layer into the workspace from `builder/templates/default` (SDK dependency, `orbit.config.ts`, auth files) in one commit and tells the agent in the next turn. Needs its own spec section on conflicts with files the agent already changed, so it is deliberately not in the first release.

## As built (2026-11-25)

- Deviation: `template_kind` stays `default` (see Layer 2). `backend_origin = none` is the only marker.
- The planner's "yes, it stores data" cannot refuse: a planning turn must end in `propose_build_plan`. So the plan covers only the static part and says the saving part needs a project with a backend; the builder header offers `/?backend=new`. This stands until phase 2.
- Skill filtering was done in the index (`skills.ForNoBackend`), not by rewording the skills. `orbit-react-patterns` stays listed.
- The advanced new-session dialog does not offer "No backend"; only the chat home does.

## Open questions

1. ~~Does anything in publish/deploy read the session's backend?~~ Answered in T1: no (see below).
2. Skill filtering versus wording (see Layer 2).
3. Should `site` ship one starter page or a small multi-section home? Decide with a blind review like builder-design-quality.

## T1 result (2026-11-24, read-only)

- Publish and deploy do not read the session's backend. `builder_publish.go` uses `sess.BackendAppID` once, to fill the frontend app record; `frontend_apps.backend_app_id` is already nullable and `CreateFrontendApp` stores `''` as NULL. No CORS or allowed-origin setup appears in publish, deploy or the frontend app code.
- Role checks on the publish path are on the frontend app. Backend role checks exist only in `builder_proposals.go` (line 198) and `builder_tools.go` (line 183), both backend-only paths that a `none` session never reaches once tools are gated.
- New finding: the `backend_origin` CHECK constraint must be widened (see Layer 1). Missing from the first draft.
- Correction to the first draft: it said publish must pass NULL instead of an empty string. It already does.
- Other `BackendAppID` references (`rbac.go`, `app_members*.go`, `apps_store.go`, `frontend_apps*.go`) belong to other tables and need no change.
