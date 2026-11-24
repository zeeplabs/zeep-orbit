# Builder projects without a backend: tasks

Gate: Go `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 ./...`; UI `npx tsc -b`, `npm test`, `npm run build`, i18n JSON valid. Template: `npm ci && npx tsc -b && npm run build` in each template.

Nothing is implemented. Order matters: layers 1 and 2 are invisible to users; layer 3 changes the default and ships last. Everything stays on beta until the maintainer authorizes `latest`.

- **T1 (done 2026-11-24)** Verify the open question: read publish and deploy for any use of the session's backend id (`builder_publish.go`, `deploy-provider-integration`). Write the result into design.md. Done when: a list of every reader of `BackendAppID` in builder code with "needs empty case: yes/no".
- **T2** Store and migration: nullable `backend_app_id`, `BackendOriginNone`, create check, scan with COALESCE, tests (create with none, reject empty id with another origin, existing rows unchanged).
- **T3** API: `resolveBuilderBackend` mode `none`, create handler path, audit fields, `backend_app_id: null` in the HTTP view. Tests for NB-01 and NB-10, including "none + app id" rejected.
- **T4** Readers: tools gating, proposals 409, prompt without the backend line, runtime name lookup, publish with NULL. Tests per reader.
- **T5** Template `site`: files, kit copy and the equality test, `TEMPLATE_VERSION`, `orbit-template.json` with `ui_kit`, `ORBIT_AGENT.md`, `UI_KIT.md`; `store.TemplateSite`, directory selection, hidden template row for publish; `Dockerfile.builder` copies the new directory. Build the template on its own.
- **T6** Prompt and skills: `core.md` no-backend section, skill index filter or wording, `prompt_test.go` pins, `builtin.lock` if a skill text changes (`go test ./internal/agent/skills -run TestBuiltinLint -update-lock`).
- **T7** Planner: the data question rule and the assumptions line. Tests on the planning prompt and a case where a clear site skips the question.
- **T8** UI: dropdown with "No backend" first and default, builder header, project list, plan card, i18n en and pt-BR, tests.
- **T9** Measure on the local stack with the real model (cost confirmed first): the five earlier app briefs with the new default (how many reach the data question; any that become a site wrongly) and two site briefs (landing page, one-pager). Write a validation.md.
- **T10** Docs and CHANGELOG, README rows if user-facing, `ai-frontend-builder.md` section.
- **T11 (phase 2)** Add a backend later (NB-08): separate spec section first.

Dependencies: T1 before T2. T2 to T4 in order. T5 and T6 can run after T3. T7 after T6. T8 after T3. T9 after T5 to T8.
