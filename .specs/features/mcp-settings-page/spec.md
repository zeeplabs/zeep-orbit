# MCP Settings Page Specification

## Problem Statement

The MCP server (`.specs/features/mcp-server/`) is implemented and reachable at `/dashboard/mcp`, but a user has no way to discover it in the dashboard. Personal Access Token (PAT) management — the credential external MCP clients need — exists only as a modal behind an unlabeled key icon in the sidebar footer (tooltip-only, no visible text), which is easy to miss even though it works correctly (`pats.*` and `common.copyToClipboard` are already present and translated in both `en.json` and `pt-BR.json` — a prior investigation's claim that they were missing was a false positive from a nested-object lookup against a flat-keyed locale file). There is no explanation anywhere in the product of the MCP endpoint URL, the two supported auth methods, or how to point an AI coding agent (Claude Code, Codex, Cursor, OpenCode) at it.

## Goals

- [ ] A user can find and open MCP setup from a labeled, top-level sidebar entry (no hunting for an unlabeled icon)
- [ ] The page explains the MCP endpoint URL, both auth methods (PAT and OAuth 2.1), and gives copy-pasteable client config for Claude Code, Codex, Cursor, and OpenCode
- [ ] PAT create/list/revoke management works from this page with zero loss of existing functionality (list, create with one-time reveal, revoke with confirm)
- [ ] All page strings exist in both `en.json` and `pt-BR.json` — no raw `pats.*`/`mcp.*` keys visible in the rendered UI

## Out of Scope

| Feature | Reason |
|---|---|
| Claude Desktop OAuth 2.1 interactive connect UI (a "Connect" button that runs the authorize redirect from the dashboard) | The OAuth flow is client-initiated (Claude Desktop calls `/.well-known/oauth-authorization-server` itself); the dashboard only needs to *document* it, not drive it. A future story can add a guided connect button if needed. |
| New backend endpoints or PAT data model changes | `usePATs`/`useCreatePAT`/`useRevokePAT` (`src/lib/api.ts`) and the underlying store already exist and are already tested (T14-T16, `internal/dashboard/pat_handler.go`) — this feature only relocates and completes the frontend. |
| Per-app or role-scoped MCP permissions | The MCP server today authorizes by dashboard user identity only (same as REST API); no new authorization model is introduced here. |
| Removing the `/dashboard/mcp` PAT/OAuth backend behavior | Backend is out of scope entirely — frontend-only feature. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
|---|---|---|---|
| Where the new "MCP" nav item lives | `NAV_SECTIONS`' `nav.sectionDeployment` group, alongside SDKs | Same audience (developers connecting external tooling to Orbit), same mental model as the SDKs page | n |
| Old sidebar-footer key icon + `PersonalAccessTokens` modal | Removed entirely from `SidebarFooter.tsx`/`MobileNav.tsx`/`DashboardShell.tsx`; PAT management only exists on the new `/mcp` page | User asked to "migrate," not duplicate, the PAT UI | n |
| Route path | `/mcp` | Short, matches the sidebar label, consistent with `/sdks`, `/logs` | n |
| Endpoint URL shown to the user | Computed from `window.location.origin + '/dashboard/mcp'` at render time, not a static placeholder | Matches the existing `Webhooks.tsx` pattern (`base = window.location.origin`) for showing a real, copy-pasteable URL instead of `<host>` | n |
| Access gating on the new page | None (`platformAction` omitted) — any authenticated dashboard user can reach it | PATs are personal (`ResolvePAT` scopes by dashboard user), matching current modal's ungated behavior; no platform-permission matrix entry exists for "mcp" today | n |
| Whether the page documents Claude Desktop / OAuth at all | Yes — one short explanatory block (no interactive flow, see Out of Scope), so the page is accurate about both auth methods Orbit supports | Documenting only what exists (PAT tutorial) but omitting OAuth entirely would misrepresent how Claude Desktop actually connects | n |
| Client config snippet content | Reuse the four snippets already drafted in `README.md`'s "🔌 MCP Server" section (Claude Code `.mcp.json`, Codex `config.toml`, Cursor `.cursor/mcp.json`, OpenCode `opencode.json`) | Single source of truth for the exact JSON/TOML shape; avoids drafting a second, potentially inconsistent version | n |

**Open questions:** none — all resolved above via defaults with stated rationale.

---

## User Stories

### P1: Discover and open MCP setup ⭐ MVP

**User Story**: As a dashboard user, I want a labeled "MCP" entry in the sidebar so that I can find MCP setup without hunting for an unlabeled icon.

**Why P1**: This is the reported bug — the entry point exists but is undiscoverable. Without this, nothing else in the feature is reachable.

**Acceptance Criteria**:

1. WHEN a dashboard user views the sidebar THEN the system SHALL render a nav item labeled with the translated string for `nav.mcp` (not a raw key) under the Deployment section, alongside SDKs.
2. WHEN the user clicks the "MCP" nav item THEN the system SHALL navigate to `/mcp` and render the MCP settings page.
3. The system SHALL NOT render the previous unlabeled key-icon button in `SidebarFooter` or `MobileNav`.
4. WHEN the user is on a narrow/mobile viewport THEN the system SHALL provide equivalent access to `/mcp` from the mobile navigation.

**Independent Test**: Log into the dashboard, look at the sidebar without prior knowledge, click "MCP", land on `/mcp`.

---

### P2: Understand how to connect an MCP client ⭐

**User Story**: As a developer connecting Claude Code, Codex, Cursor, or OpenCode to this Orbit instance, I want the MCP page to show the exact endpoint URL and a copy-pasteable config snippet for my client, so I don't have to guess the JSON/TOML shape or the URL.

**Why P2**: This is the actual value of the page beyond fixing discoverability — it's what makes MCP setup self-service.

**Acceptance Criteria**:

1. WHEN the page loads THEN the system SHALL display the MCP endpoint URL computed as `window.location.origin + '/dashboard/mcp'`.
2. WHEN the user clicks the copy icon next to the endpoint URL THEN the system SHALL copy the URL to the clipboard and show a success toast (matching the `Webhooks.tsx` copy pattern).
3. The system SHALL render one tab/section per client — Claude Code, Codex, Cursor, OpenCode — each with syntax-highlighted config content matching the corresponding snippet in `README.md`'s "🔌 MCP Server" section, with the placeholder `<host>` replaced by the live endpoint URL and the PAT reference kept as `${ZEEP_ORBIT_PAT}` (an env var name, not a real secret).
4. The system SHALL include a short explanatory block distinguishing PAT (bearer token, used by the four CLI-style clients above) from OAuth 2.1 + PKCE (used by Claude Desktop's interactive connect, discovery at `/.well-known/oauth-authorization-server`), without providing an interactive OAuth "connect" action (Out of Scope).
5. WHEN the user clicks the copy icon on a client's config snippet THEN the system SHALL copy that exact snippet text to the clipboard and show a success toast.

**Independent Test**: Open `/mcp`, read the endpoint URL, switch to the "Codex" snippet, copy it, paste into `~/.codex/config.toml`, confirm it matches the README's Codex block content (with the real URL substituted for `<host>`).

---

### P3: Manage personal access tokens from the same page

**User Story**: As a dashboard user, I want to create, view, and revoke my own MCP personal access tokens from the MCP page, so I don't need a separate screen for the credential the tutorials above tell me to generate.

**Why P3**: Functionally required (PAT management must live somewhere), but P3 because the underlying create/list/revoke behavior already exists and is already tested (T14-T16) — this story is a relocation + i18n completion, not new behavior.

**Acceptance Criteria**:

1. WHEN the page loads THEN the system SHALL list the user's non-revoked personal access tokens (same filter as today: `!pat.revoked_at`), each showing its name and last-used date or a "never used" state.
2. WHEN the user clicks "New token", enters a name, and submits THEN the system SHALL create the token, display the raw token value exactly once with a copy action and a warning that it won't be shown again, and SHALL NOT display the raw value again after the user dismisses it.
3. WHEN the user clicks revoke on a token THEN the system SHALL show a destructive confirm dialog (matching `ConfirmDialog` usage today) naming the token, and SHALL only revoke it after explicit confirmation.
4. IF the token list is empty THEN the system SHALL render an empty state explaining what a PAT is for, instead of a blank list.
5. IF create or revoke fails THEN the system SHALL show `toast.error(error.message)` and SHALL NOT close the create form or optimistically remove the token from the list.
6. The system SHALL render every string on this page (migrated `pats.*` strings, the existing `common.copyToClipboard` key, and new `mcp.*`/`nav.mcp` keys) from `en.json` and `pt-BR.json` — no key falls back to being displayed literally.

**Independent Test**: On `/mcp`, create a token, copy it, confirm it disappears from view after dismissal, then revoke it and confirm it no longer appears in the active list.

---

## Edge Cases

- IF `usePATs` is loading THEN the system SHALL show a loading state (reuse `LoadingState`) instead of an empty-state flash.
- IF the clipboard API is unavailable (permissions denied, insecure context) THEN the system SHALL leave the copied value visible/selectable on screen rather than failing silently (matches existing `PersonalAccessTokens.tsx` catch-and-ignore behavior).
- IF a token name is empty or whitespace-only THEN the system SHALL disable the create submit action (matches existing `!name.trim()` guard).
- WHEN the user navigates to `/mcp` directly via URL (not through the sidebar click) THEN the system SHALL render the same page — no state depends on modal-open props since it's no longer a modal.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| MCPUI-01 | P1: Discover and open MCP setup | Execute | Implementing |
| MCPUI-02 | P1: Discover and open MCP setup | Execute | Pending |
| MCPUI-03 | P1: Discover and open MCP setup | Execute | Pending |
| MCPUI-04 | P1: Discover and open MCP setup | Execute | Pending |
| MCPUI-05 | P2: Understand how to connect | Execute | Pending |
| MCPUI-06 | P2: Understand how to connect | Execute | Pending |
| MCPUI-07 | P2: Understand how to connect | Execute | Pending |
| MCPUI-08 | P2: Understand how to connect | Execute | Pending |
| MCPUI-09 | P2: Understand how to connect | Execute | Pending |
| MCPUI-10 | P3: Manage PATs | Execute | Pending |
| MCPUI-11 | P3: Manage PATs | Execute | Pending |
| MCPUI-12 | P3: Manage PATs | Execute | Pending |
| MCPUI-13 | P3: Manage PATs | Execute | Pending |
| MCPUI-14 | P3: Manage PATs | Execute | Pending |
| MCPUI-15 | P3: Manage PATs | Execute | Pending |

**Coverage:** 15 total, 0 mapped to tasks (Tasks phase skipped — Medium scope, ≤8 implicit steps in Execute), 15 unmapped until Execute's inline task list ⚠️

---

## Success Criteria

- [ ] Sidebar shows a labeled "MCP" entry; the old unlabeled key icon is gone
- [ ] `/mcp` renders endpoint URL, auth explanation, 4 client tutorials, and full PAT CRUD with zero raw i18n keys visible
- [ ] `npx tsc -b` and `npm run build` (internal/dashboard/ui) pass; existing PAT e2e test (T16, from commit `6349d64`) still passes after relocation, updated to target `/mcp` instead of the modal trigger
