# Builder project title: tasks

Gate: Go `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 ./...`; UI `npx tsc -b`, `npm test`, `npm run build`, i18n JSON valid.

- **T1 (done)** Store: `title_locked` column, `Session.TitleLocked`, `RenameSession` (locks), `SetPlanTitle` (skips when locked). Tests.
- **T2 (done)** `PATCH /api/builder/sessions/{id}` handler, route, audit row. Tests.
- **T3 (done)** Title generator: `titlegen` (model call, strict JSON parse, sanitize, heuristic fallback) with a fake provider test; wiring in `BuilderHandlerConfig`/server; `CreateSession` uses `prompt`. Tests.
- **T4 (done)** Plan `title`: `planning.Plan.Title`, tool schema (required), `RequireTitle` in the handler, planner prompt (PT-06), `ApproveBuildPlan` sets the title. Tests.
- **T5 (done)** UI: inline rename in the builder header, title in the plan card, chat home sends `prompt`. i18n, tests.
- **T6 (done)** Verify on the local stack with the real model (cost confirmed first); CHANGELOG; validation note.
