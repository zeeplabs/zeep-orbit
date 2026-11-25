# Builder projects without a backend: tasks

Gate: Go `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 ./...`; UI `npx tsc -b`, `npm test`, `npm run build`, i18n JSON valid. Template: `npm ci && npx tsc -b && npm run build` in each template.

Status 2026-11-25: T1 to T8 are done and merged into `develop` (#66 to #72). T9 (measurement with the real model), T10 (docs) and T11 (phase 2) are open. Order matters: layers 1 and 2 are invisible to users; layer 3 changes the default and ships last. Everything stays on beta until the maintainer authorizes `latest`.

- **T1 (done 2026-11-24)** Verify the open question: read publish and deploy for any use of the session's backend id (`builder_publish.go`, `deploy-provider-integration`). Write the result into design.md. Done when: a list of every reader of `BackendAppID` in builder code with "needs empty case: yes/no".
- **T2 (done, #66)** Store and migration: nullable `backend_app_id`, `BackendOriginNone`, create check, scan with COALESCE, tests (create with none, reject empty id with another origin, existing rows unchanged).
- **T3 (done, #67)** API: `resolveBuilderBackend` mode `none`, create handler path, audit fields, `backend_app_id: null` in the HTTP view. Tests for NB-01 and NB-10, including "none + app id" rejected.
- **T4 (done, #68)** Readers: tools gating (`NeedsBackend`), proposals 409, prompt without the backend line, runtime name lookup. Publish needed no change (T1). Tests per reader.
- **T5 (done, #69)** Template `site`: files, kit copy and the equality test, `TEMPLATE_VERSION`, `orbit-template.json` with `ui_kit`, `ORBIT_AGENT.md`, `UI_KIT.md`; `site_template_version` in the builder health; `Dockerfile.builder` copies the new directory. Deviation: no `store.TemplateSite` and no second hidden template row; the session keeps `template_kind = default` and `backend_origin = none` tells it apart. Built and checked in the real builder image.
- **T6 (done, #70)** Prompt and skills: `core.md` no-backend section, skill index filter or wording, `prompt_test.go` pins, `builtin.lock` if a skill text changes (`go test ./internal/agent/skills -run TestBuiltinLint -update-lock`).
- **T7 (done, #71)** Planner: the data question rule and the assumptions line. Tests on the planning prompt and a case where a clear site skips the question.
- **T8 (done, #72)** UI: dropdown with "No backend" first and default, builder header (`BackendLink`, with a `/?backend=new` link), project list, i18n en and pt-BR, CHANGELOG, tests. The plan card needed no change (no backend section in it). The advanced new-session dialog is unchanged.
- **T9** Measure on the local stack with the real model (cost confirmed first): the five earlier app briefs with the new default (how many reach the data question; any that become a site wrongly) and two site briefs (landing page, one-pager). Write a validation.md.
- **T10** Docs and CHANGELOG, README rows if user-facing, `ai-frontend-builder.md` section.
- **T11 (phase 2)** Add a backend later (NB-08): separate spec section first.

Dependencies: T1 before T2. T2 to T4 in order. T5 and T6 can run after T3. T7 after T6. T8 after T3. T9 after T5 to T8.
