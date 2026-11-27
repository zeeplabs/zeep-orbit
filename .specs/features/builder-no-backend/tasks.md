# Builder projects without a backend: tasks

Gate: Go `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 ./...`; UI `npx tsc -b`, `npm test`, `npm run build`, i18n JSON valid. Template: `npm ci && npx tsc -b && npm run build` in each template.

Status 2026-11-25: T1 to T10 are done (T2 to T8 merged as #66 to #72). T9 found that the "No backend" default sends every app prompt to a static plan, so the chat home default went back to "New backend" (see validation.md). T11 (phase 2, now the most valuable part: attach a backend to the session) are open. Order matters: layers 1 and 2 are invisible to users; layer 3 changes the default and ships last. Everything stays on beta until the maintainer authorizes `latest`.

- **T1 (done 2026-11-24)** Verify the open question: read publish and deploy for any use of the session's backend id (`builder_publish.go`, `deploy-provider-integration`). Write the result into design.md. Done when: a list of every reader of `BackendAppID` in builder code with "needs empty case: yes/no".
- **T2 (done, #66)** Store and migration: nullable `backend_app_id`, `BackendOriginNone`, create check, scan with COALESCE, tests (create with none, reject empty id with another origin, existing rows unchanged).
- **T3 (done, #67)** API: `resolveBuilderBackend` mode `none`, create handler path, audit fields, `backend_app_id: null` in the HTTP view. Tests for NB-01 and NB-10, including "none + app id" rejected.
- **T4 (done, #68)** Readers: tools gating (`NeedsBackend`), proposals 409, prompt without the backend line, runtime name lookup. Publish needed no change (T1). Tests per reader.
- **T5 (done, #69)** Template `site`: files, kit copy and the equality test, `TEMPLATE_VERSION`, `orbit-template.json` with `ui_kit`, `ORBIT_AGENT.md`, `UI_KIT.md`; `site_template_version` in the builder health; `Dockerfile.builder` copies the new directory. Deviation: no `store.TemplateSite` and no second hidden template row; the session keeps `template_kind = default` and `backend_origin = none` tells it apart. Built and checked in the real builder image.
- **T6 (done, #70)** Prompt and skills: `core.md` no-backend section, skill index filter or wording, `prompt_test.go` pins, `builtin.lock` if a skill text changes (`go test ./internal/agent/skills -run TestBuiltinLint -update-lock`).
- **T7 (done, #71)** Planner: the data question rule and the assumptions line. Tests on the planning prompt and a case where a clear site skips the question.
- **T8 (done, #72)** UI: dropdown with "No backend" first and default, builder header (`BackendLink`, with a `/?backend=new` link), project list, i18n en and pt-BR, CHANGELOG, tests. The plan card needed no change (no backend section in it). The advanced new-session dialog is unchanged.
- **T9 (done 2026-11-25, see validation.md)** Measure on the local stack with the real model (cost confirmed first): the five earlier app briefs with the new default (how many reach the data question; any that become a site wrongly) and two site briefs (landing page, one-pager). Write a validation.md.
- **T10 (done 2026-11-25)** Docs and CHANGELOG, README bullet in the 4 languages, `ai-frontend-builder.md` section "Projects without a backend", CI job `templates` (default and site: install, typecheck, lint, build).
- **T11 (phase 2, NB-08, see "NB-08 in detail" in spec.md)** Spec approved 2026-11-27 (existing backends too; published sessions allowed once T11a verifies CORS and the link; revert across an attach refused). Tasks, in order:
  - **T11a (done 2026-11-27, see design.md "T11a result")** Verified: no CORS change (global `*`), one UPDATE on `frontend_apps.backend_app_id`; revert across an attach must be refused (`ErrRevertAcrossBackend`); no migration (turn `kind` has no CHECK); writes possible with `FileOp`; a pending plan card blocks the attach (one-active-turn), so T11b/T11c add `AbandonPendingPlan` and chain a `PlanFirst` turn.
  - **T11b** Design: overlay location and format, the `attach_backend` turn kind, the conditional update, planner card action. Needs T11a.
  - **T11c** Store and turn: `KindAttachBackend`, `AttachBackend` conditional update (and the `frontend_apps` link in the same transaction), `ErrRevertAcrossBackend`, `AbandonPendingPlan`; no migration. Tests per failure point (NB-08e).
  - **T11d** Overlay and worker: the overlay directory, the turn's file operations, collision check, one commit. Equality test with the default template.
  - **T11e** API: `POST .../backend`, audit, 409/404 cases. Tests.
  - **T11f** Planner: the "Add a backend" path for a "yes" answer (NB-08g). Real-model check on the "yes" path.
  - **T11g** UI: header menu, plan card action, i18n, CHANGELOG, docs.
  - **T11h** Measure and flip the default back to "No backend" (NB-08i), as its own PR.

Dependencies: T1 before T2. T2 to T4 in order. T5 and T6 can run after T3. T7 after T6. T8 after T3. T9 after T5 to T8.
