# Builder projects without a backend: design

## Shape of the change

Three layers, in this order, each shippable on its own:

1. **Session without a backend** (API, store, readers). No UI, no new template. Behind the existing contract: nothing creates a `none` session until layer 3.
2. **Template `site` and prompt** (template files, kit copy, prompt text, tools gating, skills).
3. **Entry points** (dropdown default, planner question, UI states). This is the layer that changes what users see; it ships last.

## Layer 1: session without a backend

- `builder_sessions.backend_app_id` becomes nullable in `provisioner.go` (`ALTER COLUMN ... DROP NOT NULL`, idempotent). `backend_origin` accepts `none`; `store.BackendOriginNone`.
- `store.CreateSessionParams.BackendAppID` empty is valid only with `BackendOrigin == none` (the existing check at `store.go:282` becomes: backend id required unless origin is `none`). `Session.BackendAppID` stays a string, empty for `none`; scanning uses `COALESCE(backend_app_id::text, '')`. The HTTP view returns `backend_app_id: null` for `none` (a `*string` in the view only), so a client cannot mistake an empty string for an id; the store keeps a plain string.
- `resolveBuilderBackend` gets `builderBackendModeNone = "none"`; same validation style as `new`.
- Readers, each with the empty case:
  - `builder_tools.go ownerAndApp` already returns the model-visible unavailable message for an empty id; the tool list for the turn drops backend tools instead of relying on the error.
  - `builder_proposals.go`: backend proposals on a `none` session answer 409 (English error).
  - `builder_runtime.go promptSource`: skip the `zeep_system.apps` name lookup.
  - `agent/prompt.go`: no "Backend app" line; adds the no-backend sentence.
  - `builder_publish.go`: pass NULL, not an empty string, to `FrontendAppInput.BackendAppID` (that field is a string today and the insert would store `''` into a UUID column; use `*string` or a NULLIF at the insert). Verify the deploy step reads no backend data.
  - `builder_common.go` view, `ProjectsPage`/`projects.ts` grouping and the builder header.

## Layer 2: template `site` and prompt

- `builder/templates/site/`: Vite + React + TypeScript + Tailwind, `orbit-ui` kit, tokens in `src/index.css`, routes limited to what a site needs (a single page plus an optional second one), `index.html` with title and description meta, `public/` for images. No `@zeep/*` SDK, no `orbit.config.ts`, no `BackendStatus`, no sign-in.
- Selection: today `builderDefaultTemplateDir = "default"` and `store.TemplateDefault`. Add `store.TemplateSite = "site"` (`template_kind` check accepts it) and a `templateDirForBackendMode` helper. The hidden `github_templates` row used by publish (`ensureBuilderDefaultTemplate`) gets a sibling for `site`.
- Kit single source: move `builder/templates/default/src/components/ui`, `src/lib/cn.ts` and the token block that the kit reads to `builder/templates/_kit/`, and copy them into both templates in `Dockerfile.builder`. In the repo the two copies stay checked in (so `npm run build` of each template works on its own) and a Go test compares them and fails on a difference. `[Incerto]`: copy-at-build alone would make local template builds fail; the checked-in copy plus a test is the simpler option and is the proposal.
- Prompt: `core.md` gets a short section for sessions with no backend. Same cap discipline as before: pin text in `prompt_test.go`.
- Skills: the index for a `none` session leaves out `orbit-crud-screens` and `orbit-forms-and-states` (they teach data screens against the SDK). If that filter is a bigger change than it looks, the fallback is one sentence in each skill's description saying "not for sites"; both are small, choose when implementing.

## Layer 3: entry points

- Dropdown order: No backend (default), New backend, Existing backend. The choice is not remembered between visits, so the default stays the default; revisit if people complain.
- Planner question (NB-05): a rule in `core.md` plus the planning prompt. It is a normal `ask_user` question with two options and "decide for me", so it costs one question and no new UI.
- Measure before release (see Risks in the spec): a fixed set of briefs, count of how many reach the data question, cost of a site run versus an app run.

## Phase 2: add a backend later (NB-08)

`attachBackend(sessionID)`: creates the app through the same operation as `new`, sets `backend_app_id` and `backend_origin = builder` in one update guarded by `backend_app_id IS NULL`, then writes the data layer into the workspace from `builder/templates/default` (SDK dependency, `orbit.config.ts`, auth files) in one commit and tells the agent in the next turn. Needs its own spec section on conflicts with files the agent already changed, so it is deliberately not in the first release.

## Open questions

1. Does anything in publish/deploy (`builder_publish.go`, `deploy-provider-integration`) read the session's backend? Verify in T1 before anything else.
2. Skill filtering versus wording (see Layer 2).
3. Should `site` ship one starter page or a small multi-section home? Decide with a blind review like builder-design-quality.
