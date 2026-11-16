# Home Chat Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** If the skill cannot be activated, stop and tell the user.

One commit per finished task with a green gate (authorized by the maintainer for the builder work); push only with the maintainer's OK; never add Claude/Anthropic attribution; never force push. Spec approved by the maintainer on 2026-11-16 (assumptions 1 to 7 confirmed).

---

**Spec**: `.specs/features/home-chat/spec.md`
**Design**: `.specs/features/home-chat/design.md`
**Status**: Spec approved 2026-11-16. H01 to H05 done. Next: H06.

---

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Frontend | Every UI task | `cd internal/dashboard/ui && node_modules/.bin/tsc -b && npm test && npm run build` plus JSON validation of `en.json` and `pt-BR.json` (not `npx tsc`) |
| Full | Only if H05 touches Go | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 30m ./...` (`DASHBOARD_BOOTSTRAP_SECRET` and `TEST_DATABASE_URL` set) |

## Dependency Graph

```
H01 [verify] ─► H02 mode + landing ─► H03 shells ─┬─► H04 home + creation ─┐
                                                  ├─► H05 projects ────────┼─► H08 unavailable fallback ─► H09 UAT ─► H10 docs
                                                  └─► H06 builder in shell + collapsed sidebar ┘
                                                                      H07 i18n runs with each page
```

---

## Tasks

### H01 — Verify the open data and shell questions
- **What**: answer, with file and line or a request: (a) does `GET /builder/sessions` return milestone counts, the backend name and enough to join (`backend_app_id`, `frontend_app_id`, `published_sha`), and is it owner-scoped; (b) what happens in the list when a backend is deleted; (c) does `DashboardShell` have a collapse or drawer mode to reuse; (d) what `BuilderPage` assumes about its shell; (e) how `AppsPage` picks the session locale; (f) whether any per-user preference storage exists server-side.
- **Where**: `internal/dashboard/builder_*.go`, `internal/dashboard/ui/src/pages/{DashboardShell,BuilderPage,AppsPage}.tsx`, `lib/api.ts`.
- **Done when**: each item answered in the spec's assumption table and edge cases; the only possible Go change (milestone counts in the list) is decided.
- **Gate**: none.
- **Result (2026-11-16)**:
  - (a) `GET /builder/sessions` is owner-scoped (`builder_handlers.go:599`). `BuilderSessionView` has `backend_app_id`, `frontend_app_id`, `published_sha`, `status`, `phase`, `title`, `usage`; no backend name and no milestone progress (`builderBriefProgress` exists only on the detail route, `builder_planning.go:350`). Backend name: join client-side with the apps list. Milestone progress for HCH-17: **Go change needed in H05**, add the current and total milestone to the list view, computed in one batched query (no per-session query), with a handler test. Full gate applies to H05.
  - (b) A backend deletion cascades to its sessions, so no orphan session exists; frontend deletion sets `frontend_app_id` NULL. Edge case in the spec rewritten.
  - (c) `DashboardShell` has no collapse or drawer mode (a padded `<Outlet/>`); `SimpleSidebar` is new, nothing to reuse.
  - (d) `BuilderPage` sizes itself with `h-[70vh]` / `lg:h-auto lg:min-h-0`; whether it depends on the advanced shell padding is still to be checked in H06.
  - (e) Locale: send `locale: i18n.language`, same as `NewBuilderSessionDialog.tsx:127`; the server validates and falls back to Accept-Language, then `en`.
  - (f) No server-side per-user preference storage; `localStorage` stays (assumption 1).

### H02 — Mode preference and landing
- **What**: `lib/mode.ts` and `useMode`; `HomeEntry` in `App.tsx` replacing the unconditional redirect (HCH-10); route table for the two shells.
- **Depends on**: H01.
- **Done when**: tests for the preference (absent, simple, advanced, storage throws) and for the redirect matrix (available/unavailable × simple/advanced × `/`); existing routes unchanged.
- **Gate**: Frontend.

### H03 — Simple shell
- **What**: `SimpleShell` and `SimpleSidebar` (expanded variant), header with the tablist and the mode switch (`role="switch"`), the switch added to the advanced sidebar (HCH-08, HCH-09).
- **Depends on**: H02.
- **Done when**: keyboard and screen-reader behavior of the tablist and switch checked; advanced shell diff is only the new switch; both themes.
- **Gate**: Frontend.

> **Login landing (2026-11-16, maintainer's OK)**: a successful login and `/login` for a signed-in user now go to `/dashboard/` instead of `/dashboard/apps`; `HomeEntry` sends advanced-mode users and users without the builder on to `/apps`. The e2e global setup and `login()` helper set `orbit.mode=advanced` so the suite keeps driving the advanced interface on a stack where the builder is available.

> **H05 note (2026-11-16)**: `GET /builder/sessions` rows now carry `brief_version`, `milestones_total` and `milestones_done` (zeros without a brief), from `Store.MilestoneSummaries` (two queries for the whole list). They live in a separate `BuilderSessionListItem` type: embedding both the view and `builderBriefProgress` in one struct would make Go drop the duplicate JSON keys (the detail route is covered by a test). `/projetos` is a layout route (`SimpleLayout`) that redirects to `/apps` when the builder is unavailable (HCH-20). The advanced sidebar shows the mode switch only when the builder is available. HCH-19 still says `role=\"tablist\"` for Chat / Meus projetos; the shell uses route links instead (see the H03 note), to be reconciled in the spec at H09.

> **H03/H04 note (2026-11-16)**: `SimpleShell` renders `/` (via `HomeEntry`, with `children`); H05 mounts it as a layout route for `/projetos`, H06 for `/builder/:sessionId`. The header uses route links with `aria-current` instead of ARIA tabs (each item navigates to a page; the tabs pattern needs a tabpanel in the same view). Mobile layout of the simple shell (nav, footer) is H09 (HCH-19); today the sidebar is hidden below `md`. Logout, language and change-password flows are duplicated from `DashboardShell` to keep its diff to the switch only; extracting a shared hook is a follow-up.

### H04 — Home page and creation flow  `[P with H05, H06]`
- **What**: `HomeChatPage`, composer, suggestions, backend selector with the existing-backend picker, "Continue de onde parou", creation mutation with the failure path (HCH-02 to HCH-07), title function.
- **Depends on**: H03.
- **Done when**: unit tests for the title function (word boundary, 60 chars, whitespace, non-ASCII); tests for the mutation (success, message fails → session kept and reused by the retry, nothing deleted, 429 shown; see assumption 6); local UAT creates a session and lands on `/builder/:id`.
- **Gate**: Frontend.

### H05 — Meus projetos
- **What**: `ProjectsPage`, the view-model join, status mapping, filters, search, empty state, "Criar frontend com IA" for backends with no session (HCH-16 to HCH-18). If H01 says the list lacks milestone counts, add them to `ListSessions` with a test.
- **Depends on**: H03, H01.
- **Done when**: join tests (session with and without frontend, backend-only, deleted backend); filters and search tests; with Go change: handler test and Full gate.
- **Gate**: Frontend (Full if Go).

### H06 — Builder in the simple shell with the collapsed sidebar
- **What**: move `/builder/:sessionId` under `SimpleShell`; collapsed `SimpleSidebar` variant with tooltips and accessible names; keep `/builder/usage` in the advanced shell (HCH-13 to HCH-15).
- **Depends on**: H03, H01 (d).
- **Done when**: the builder page renders correctly at 1440, 1024 and 390 px in both themes (headless Chromium on the local stack, scripted model, no real tokens); no horizontal page scroll; focus order sane.
- **Gate**: Frontend.

### H07 — i18n (runs with H03 to H06)
- **What**: keys in `en.json` and `pt-BR.json` for every new string; validate both files each time.
- **Done when**: no hardcoded text in the new components; JSON validates (HCH-22).

### H08 — Unavailable builder fallback
- **What**: redirects for `/` and `/projetos`, hidden switch, banner reuse on `/apps` for superadmins (HCH-20).
- **Depends on**: H04, H05.
- **Done when**: tests for each `BuilderUnavailableReason` (no chat shown, redirect to `/apps`); UAT with the builder disabled.
- **Gate**: Frontend.

### H09 — Phone layout and UAT
- **What**: design and implement the phone behavior (HCH-19: mobile navigation instead of the sidebar, stacked panes in creation mode); run the full UAT at three widths and both themes with a scripted model; record screenshots paths and findings here.
- **Depends on**: H04 to H08.
- **Done when**: the success criteria of the spec hold; open findings listed.
- **Gate**: Frontend.

### H10 — Docs
- **What**: HCH-21: `docs/docs/ai-frontend-builder.md`, `CHANGELOG.md`, and the 4 READMEs' feature tables (`README.md`, `i18n/README.pt-BR.md`, `i18n/README.pt-PT.md`, `i18n/README.es.md`).
- **Depends on**: H09.
- **Done when**: all five files updated in the same change; relative paths of the translated READMEs kept.
- **Gate**: none beyond review.
