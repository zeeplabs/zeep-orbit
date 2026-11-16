# Home Chat Specification

> Status: **Draft for review (2026-11-16).** Nothing is implemented. Child of `.specs/features/ai-frontend-builder/` and `ai-builder-backend/`. Design reference: the canvas "Orbit Home Chat" (private artifact, <https://claude.ai/artifact/WB885VgnYJXzREoeSRT4h4>, version 5, four screens). The screen descriptions in `design.md` are the durable copy; the canvas may not outlive this conversation.

## Problem Statement

The Orbit dashboard opens on `/apps`, a list of backend and frontend apps, and the AI Frontend Builder sits behind a "Criar com IA" button and a section of the same page. A non-technical user who just wants "an app for my clinic" has to understand backends, frontends and sessions before the first prompt. The builder already creates the backend together with the session (`backend.mode = "new"`), so the first step in the UI can be just a prompt.

This feature makes the AI chat the default landing screen, in the style of a general chat product: describe the app, get a project. It adds a **Meus projetos** tab that lists what the user has built, and an **advanced mode** that is exactly today's interface. It adds no new builder capability.

## Goals

- [ ] The default landing screen is a chat prompt that always starts a new project (a builder session with a new backend).
- [ ] A **Meus projetos** tab lists the user's projects (builder sessions, with their backend and frontend) and backends that have no session.
- [ ] **Modo avançado** shows the current interface unchanged (same pages, routes and navigation); the choice is remembered for the user.
- [ ] While a project is being built (creation mode), the sidebar is collapsed to icons so the conversation and the preview get the width.
- [ ] A user who cannot use the builder (disabled, not configured, no provider) lands on today's interface and is never shown a chat that cannot work.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Any change to what the builder can build or to its permissions | Separate specs (`ai-builder-*`). |
| Removing or changing the advanced interface | It must stay exactly as today. |
| Renaming or deleting a project from the new screens | Deletion exists on `DELETE /builder/sessions/{id}`; a UI for it is a follow-up. |
| Frontend apps that were not created by the builder (ZIP import, manual) in "Meus projetos" | They have no conversation; they stay in advanced mode. Open question 4. |
| Syncing the mode preference across devices | v1 stores it in the browser, like the theme (`lib/theme.ts`). Server-side preference is a follow-up. |
| A phone layout design | The canvas is desktop only. The requirement below (HCH-19) fixes the behavior; the visual design needs its own pass. |
| Chat history search | Not in the canvas. |

---

## Assumptions & Open Questions

| # | Decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | Where the preference lives | `localStorage`, per browser, key `orbit.mode`; absent means simple mode when the builder is available. | `lib/theme.ts` already does this; there is no per-user preferences table (**[verify]**). | No |
| 2 | Which shell a route uses | By route, not by preference: `/`, `/projetos`, `/builder/:id` use the simple shell; every other route uses today's shell. The preference only chooses where `/` lands and what the toggle shows. | Avoids a half-simple, half-advanced screen and keeps every existing deep link working. | No |
| 3 | Project title from the first prompt | The first 60 characters of the prompt, trimmed at a word boundary. | The server default for a new backend is "Untitled app"; a list of identical titles is unusable. The prompt is already stored as the first message, so the title adds no new data, but it shows in more places. | No |
| 4 | Frontends outside the builder in "Meus projetos" | Not shown in v1. | They have no session to open. | No |
| 5 | Who sees the chat landing | Every authenticated user for whom `GET /builder/status` says `available`. Anyone else lands on `/apps`. | Same gate as "Criar com IA" in `AppsPage`. | Fact for the gate; behavior is my proposal. |
| 6 | Session created, first message failed | Keep the user on the home screen with the text intact, delete the empty session best-effort, and show the error. | A session with no message is clutter, and the user must not lose a long prompt. | No |
| 7 | Backend choice on the composer | "Backend novo" by default; "Usar backend existente" opens a picker of the apps the user can see. | `CreateBuilderSessionInput.backend` supports both. | Fact for the API. |

---

## User Stories

### P1: Start a project from the chat ⭐ MVP

**US-1**: As a user, I want to describe an app in a chat box and land in a working project so that I do not have to learn backends, frontends and sessions first.

**Acceptance Criteria**

1. HCH-01 The route `/` SHALL render the home chat when the builder is available and the saved mode is simple (or unset); the heading, subtitle, composer, suggestions and "Continue de onde parou" section follow `design.md`.
2. HCH-02 WHEN the user submits a non-empty prompt THEN the screen SHALL create a builder session with the chosen backend (`{mode:"new"}` by default) and a title per assumption 3, post the prompt as the first message, and navigate to `/builder/:sessionId`.
3. HCH-03 WHEN the session is created but the message is not accepted THEN the screen SHALL behave as assumption 6: error shown, text kept, empty session deleted best-effort (a failed delete is logged, not shown).
4. HCH-04 The submit control SHALL be disabled while a request is in flight and when the prompt is empty or whitespace; the composer SHALL be a real labelled `<textarea>`; Enter sends and Shift+Enter inserts a newline.
5. HCH-05 The suggestion chips SHALL fill the composer (not send); they are static, translated text and make no model call.
6. HCH-06 The "Backend novo" control SHALL offer "Usar backend existente" with a picker of the apps the user can see; picking one sends `{mode:"existing", app_id}`.
7. HCH-07 Rate limits and errors of `POST /builder/sessions` and `.../messages` (429, 4xx) SHALL be shown with the message the API returns, through the existing toast pattern, and never crash the screen.

### P1: Modes and landing

**US-2**: As a user, I want to switch between the simple and the advanced interface so that I can work the way I prefer.

**Acceptance Criteria**

1. HCH-08 A switch labelled "Modo avançado" (a `button` with `role="switch"` and `aria-checked`) SHALL be present in the header of the simple screens and at the bottom of the advanced sidebar; toggling it saves the preference and navigates to `/` (turning advanced off) or `/apps` (turning it on).
2. HCH-09 The advanced interface SHALL be unchanged except for the new switch in its sidebar: same routes, pages, navigation sections, permissions and behavior.
3. HCH-10 `/` SHALL redirect to `/apps` when the saved mode is advanced or the builder is not available (replacing today's unconditional redirect).
4. HCH-11 Existing deep links (`/apps/:id`, `/data-browser`, `/logs`, and so on) SHALL keep working and SHALL render in the advanced shell regardless of the saved mode.
5. HCH-12 Any "open backend" action of the simple screens SHALL navigate to `/apps/:id` (advanced shell) without changing the saved mode.

### P1: Creation mode with a collapsed sidebar

**US-3**: As a user building a project, I want the sidebar out of the way so that the conversation and the preview have the width.

**Acceptance Criteria**

1. HCH-13 On `/builder/:sessionId` in the simple shell the sidebar SHALL be collapsed to icons only (68 px): logo, "Novo projeto", one icon per recent project with the current one highlighted, and the user avatar.
2. HCH-14 Every icon SHALL have an accessible name and a tooltip (the project title, "Novo projeto"); keyboard focus and Enter work as on the expanded sidebar.
3. HCH-15 The collapsed state SHALL NOT depend on a user setting in v1. (A control to expand it is a follow-up the canvas did not draw.)

### P2: Meus projetos

**US-4**: As a user, I want to see everything I have built so that I can continue or open any of it.

**Acceptance Criteria**

1. HCH-16 `/projetos` SHALL list one card per builder session the user owns (title, status, backend app name, last update) with "Abrir conversa" (`/builder/:id`) and "Backend" (`/apps/:backend_app_id`) actions, plus one card per backend app the user can see that no session uses, with "Abrir backend" and "Criar frontend com IA" (a session with `{mode:"existing"}`).
2. HCH-17 The status SHALL be "Publicado" when the session has a published frontend, "Em construção" with the current milestone when a brief exists (for example "marco 2 de 4"), and a neutral label otherwise; for a backend with no session it SHALL be "Só backend".
3. HCH-18 The filters (Todos, Em construção, Publicados, Só backend) and the search box SHALL filter the loaded list on the client; an empty result shows an empty state with the "Novo projeto" action.

### P2: Layout and fallback

**US-5**: As a user on any device or theme, I want the new screens to behave like the rest of the dashboard.

**Acceptance Criteria**

1. HCH-19 The new screens SHALL work in the light and dark themes (tokens only, no literal colors), at phone width (the sidebar becomes the existing mobile navigation, the two-pane builder stacks), and with the keyboard (visible focus, `role="tablist"` for Chat / Meus projetos with arrow-key support).
2. HCH-20 WHEN the builder is not available THEN the simple screens SHALL NOT render; a user who types `/` or `/projetos` is redirected to `/apps`, and the switch is hidden. A superadmin SHALL see the reason ("not_configured", "disabled", "provider_not_configured", ...) as a banner on `/apps` with a link to the settings, as `AppsPage` already does for "Criar com IA".

### P3: Docs

**US-6**: As an operator I want the new default described.

**Acceptance Criteria**

1. HCH-21 `docs/docs/ai-frontend-builder.md` SHALL describe the home chat, "Meus projetos", the advanced mode and where the preference is stored; `CHANGELOG.md` `[Unreleased]` and the 4 READMEs (feature table) SHALL be updated in the same change.
2. HCH-22 All user-facing strings SHALL be in both `en.json` and `pt-BR.json` (AGENTS §5), and every mutation SHALL have `onError`.

---

## Edge Cases

- The user has no sessions and no apps: the home shows the composer and hides "Continue de onde parou"; `/projetos` shows the empty state.
- A session whose backend was deleted: the card shows "Backend removido" and only "Abrir conversa" (**[verify]** the list payload carries the app name or a deleted flag).
- A very long prompt: the title is cut at 60 characters; the full text goes as the message (the API's own limit applies and its error is shown).
- Two tabs: the preference is read at navigation, not live-synced.
- `localStorage` unavailable (private mode): the preference falls back to simple mode for the session without an error, as `theme.ts` does.
- A viewer-only user on a backend: the builder requires at least viewer access; "Criar frontend com IA" on that backend works, and a confirmed backend change later needs editor or admin (existing rule).

## Success Criteria

- [ ] A new user can type one prompt on `/` and reach a working preview without opening any other page.
- [ ] Switching to advanced mode and back loses nothing and changes no existing page.
- [ ] A user without builder access never sees the chat.
