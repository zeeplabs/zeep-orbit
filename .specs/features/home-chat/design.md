# Home Chat Design

**Spec**: `.specs/features/home-chat/spec.md`
**Status**: Draft (2026-11-16). Frontend only unless R-items below say otherwise. Items marked **[verify]** were not checked against the code.

---

## Architecture Overview

No new backend capability is required if the data checks (H01) pass. The work is routing, two shells, three pages and one preference hook, all in `internal/dashboard/ui`.

```
/            ─ builder available && mode != advanced ─► HomeChatPage   (simple shell)
             ─ otherwise ───────────────────────────► redirect /apps
/projetos    ─► ProjectsPage                                        (simple shell)
/builder/:id ─► BuilderPage                                         (simple shell, collapsed sidebar)
/apps, /apps/:id, /data-browser, /logs, ... ─► unchanged            (advanced shell = today's DashboardShell)
```

Reused unchanged: `useBuilderStatus`, `useBuilderSessions`, `useApps`, `POST /builder/sessions`, `POST /builder/sessions/{id}/messages`, `DELETE /builder/sessions/{id}`, `BuilderPage`, `AppsPage`, the i18n and toast patterns, the theme tokens.

## Screens (durable copy of the canvas)

All four use the dark tokens of `styles/tokens.css` (`--bg-page`, `--surface`, `--border`, `--primary`, `--accent`, text tokens), Manrope for text and Space Grotesk for headings, as the app does today.

1. **Home (simple shell, `/`).** Sidebar 260 px: logo, "Novo projeto" button, "Recentes" list (latest sessions, title only), user chip at the bottom. Header: tablist **Chat | Meus projetos** on the left, **Modo avançado** switch on the right. Body, centered: heading "O que vamos construir hoje?", one-line subtitle, the composer card (textarea, attach button, "Backend novo" selector, round send button), four suggestion chips, then "Continue de onde parou" with the three latest projects and a "Ver todos os projetos" link.
2. **Creation mode (`/builder/:id`).** Same header, **sidebar collapsed to 68 px** (icons only: logo, "Novo projeto", one chat icon per recent project with the current highlighted, avatar). A project bar below the header: title, status pill ("Em construção · marco 2 de 4"), backend name, "Abrir backend" and "Publicar". Two panes: the conversation (plan card with the milestones, the backend confirmation card, the composer) and the preview. This screen is today's `BuilderPage` inside the new shell; its content is not redesigned here.
3. **Meus projetos (`/projetos`).** Title and subtitle, "Novo projeto" button, search box, filter chips, a responsive grid of project cards (title, status pill, backend slug in mono, preview thumbnail placeholder, last update, "Abrir conversa" and "Backend").
4. **Advanced mode (`/apps` and every existing route).** Today's `DashboardShell` and pages. The only addition is the **Modo avançado** switch (on) at the bottom of the sidebar, which returns to `/`.

The canvas is a mockup: card contents are fictional, the "Backend" thumbnails are placeholders, and the advanced screen was drawn from the code (`nav.ts`, `pt-BR.json`), not from a capture of the running page.

## Components

### C1 — Mode preference (HCH-01, 08, 10)
`src/lib/mode.ts`: `getMode(): 'simple' | 'advanced'` and `setMode`, backed by `localStorage` key `orbit.mode`, every access in `try/catch` (the pattern of `lib/theme.ts`); a `useMode()` hook that re-reads on navigation. No server state in v1.

### C2 — Landing and shells (HCH-09 to HCH-12, HCH-20)
`App.tsx`: replace `<Route index element={<Navigate to="/apps" replace />}/>` with a `HomeEntry` that waits for `useBuilderStatus()` (same loading gate as `LoadingScreen`, AGENTS §5), then renders `HomeChatPage` or `<Navigate to="/apps" replace/>`. Two layout routes: `SimpleShell` (new) for `/`, `/projetos`, `/builder/:sessionId`; the existing `DashboardShell` for the rest. `BuilderPage` moves under `SimpleShell`; **[verify]** that nothing in `BuilderPage` assumes the old shell (page padding, height, the mobile tab bar) and that `/builder/usage` stays in the advanced shell.

### C3 — SimpleShell and the collapsed sidebar (HCH-13 to HCH-15)
`components/layout/SimpleShell.tsx` with `SimpleSidebar` in two variants selected by route (`/builder/:id` collapsed, the others expanded) using the same nav primitives as `DashboardShell`. **[verify]** whether `DashboardShell` already has a collapsed or mobile drawer mode to reuse (it has a mobile tab bar, `nav.ts`). Icons are Material Symbols through the existing `Icon` component (not inline SVG as in the canvas). Collapsed items carry `aria-label` and `title`. The "Recentes" list is `useBuilderSessions()` sliced to the latest 20 by `updated_at`.

### C4 — Home page and creation flow (HCH-02 to HCH-07)
`pages/HomeChatPage.tsx`:
1. `useMutation`: `POST /builder/sessions {backend, title, locale}` then `POST /builder/sessions/{id}/messages {content}`; on success `navigate('/builder/'+id)` and invalidate the sessions query.
2. If the second call fails: `DELETE /builder/sessions/{id}` (ignore its failure), keep the text, `toast.error(error.message)`.
3. Title: first 60 characters of the prompt at a word boundary (a small pure function with tests).
4. Backend selector: "Backend novo" / "Usar backend existente" with a picker from `useApps()` filtered to backends.
5. Locale: the current i18n language, as `AppsPage` does today when it creates a session (**[verify]**).

### C5 — Meus projetos (HCH-16 to HCH-18)
`pages/ProjectsPage.tsx`, a client-side join: sessions (`useBuilderSessions`) by `backend_app_id` and `frontend_app_id` with `useApps()`; backends referenced by no session become "Só backend" cards. Status: published when `published_sha !== ''` or `frontend_app_id` is set; "Em construção · marco X de Y" needs the milestone counts, which `GET /builder/sessions/{id}` returns (`milestones_done`, `milestones_total`) and the list may not (**[verify]**). If the list lacks them: a single small endpoint change (add the two counts to `ListSessions`) is cheaper than N requests, and is the only possible Go change in this feature. Filters and search are client-side over the loaded list.

### C6 — Unavailable builder (HCH-20)
`HomeEntry` and `/projetos` redirect to `/apps` when `status.available` is false; the existing banner logic on `AppsPage` (`builderReason`) stays the place where a superadmin sees why.

### C7 — i18n and docs (HCH-21, HCH-22)
New keys under `home.*`, `projects.*`, `nav.simple.*` in `en.json` and `pt-BR.json`; docs and CHANGELOG as in the spec.

## Data Models

No DDL, no new table. The preference is `localStorage["orbit.mode"] = "simple" | "advanced"`. A "project" is a view model, not a stored entity:

```ts
interface ProjectCard {
  kind: 'session' | 'backend-only'
  sessionId?: string
  title: string
  backendAppId: string
  backendName: string
  status: 'building' | 'published' | 'idle' | 'backend-only'
  milestone?: { done: number; total: number }
  updatedAt: string
}
```

## Error Handling

API errors surface through `toast.error(error.message)` (AGENTS §5); server errors are already English and generic (AGENTS §4). The screens never show raw error objects. Loading uses skeletons, not a spinner on a blank page.

## Security and Privacy Notes

- The title carries up to 60 characters of the prompt into session lists. Prompts can contain personal or business data; the same text is already stored as the first message, so this widens where it is displayed, not what is stored. Called out in assumption 3.
- The preference holds no data about the user beyond the mode.
- Visibility and RBAC stay server-side: the list endpoints already return only what the user can see (**[verify]** `ListSessions` is owner-scoped, as the builder spec says sessions are owner-only).

## Tech Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Shell by route, not by preference | Simple shell for three routes only | No mixed screens; every deep link keeps working (assumption 2). |
| Preference in `localStorage` | v1 | No preferences table exists; the theme does the same. Follow-up if users want it across devices. |
| Client-side join for projects | Reuse list endpoints | No new API unless the list lacks milestone counts. |
| Icons through the existing `Icon` component | Material Symbols | Consistency; the canvas used inline SVG only because it could not load the font. |
| Collapsed sidebar not user-controlled | Route-driven | What the canvas drew; an expand control is a follow-up. |

## Risks and Concerns

| Risk | Impact | Mitigation |
| --- | --- | --- |
| `/` was the same as `/apps` for every user and bookmark | Existing users land on a different screen | The redirect for advanced mode and unavailable builder; the switch is the first control in the header; release notes. |
| `BuilderPage` moved under another shell | Layout regressions (height, padding, mobile tab bar) | H06 with a UAT at three widths and both themes. |
| First-prompt title in lists | Sensitive text shown in the sidebar | Assumption 3; the user can rename in a follow-up; trim at 60. |
| The mockup is a mockup | Real data may break the cards (long names, many projects) | Truncation rules and the 20-item recents limit; UAT with long titles. |
| Phone layout not designed | An inconsistent mobile experience | H09 designs and checks it before release; HCH-19 is the contract. |
