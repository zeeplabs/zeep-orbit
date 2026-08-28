# Responsive Layout Specification

## Problem Statement

The dashboard shell (`internal/dashboard/ui`) has exactly two navigation states: a fixed 264px sidebar (`≥768px`) or a mobile bottom bar (`<768px`). Tablets (768–1024px) inherit the full desktop sidebar with no intermediate state, several forms use `min-w-[420px]` that overflows below 420px viewports, the main content area has no `max-width` (stretches edge-to-edge on ultra-wide monitors), and no automated test coverage exists for any viewport except one ad-hoc 390×844 mobile check. This spec defines the work to make the shell and its pages behave correctly across phone, tablet, laptop, and ultra-wide/super-wide monitors, and to formalize the bottom-bar navigation the user asked for (5 slots max, one slot a drawer with the rest).

## Goals

- [x] Three nav states ship: mobile bottom bar (5 slots), tablet icon-only collapsed sidebar, desktop full sidebar — driven by CSS breakpoints, zero layout shift/flash on resize across the boundary.
- [x] No page produces horizontal viewport overflow at 375px width (iPhone SE class) or below, or at any width between 375px and 3840px (ultra-wide).
- [x] Main content area caps at a fixed max-width and centers on monitors wider than that cap, so text/table rows stop stretching indefinitely.
- [x] Every current desktop feature (all `NAV_SECTIONS` routes, role gating, active-route highlight) remains reachable and functionally identical at every breakpoint.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Visual redesign / rebrand (colors, spacing scale, typography) | Not requested; this is a responsiveness pass, not a redesign. |
| Changing `hasPlatformPermission`/`RequireRole` gating logic | Permission matrix is out of scope; nav only renders what it already renders today, at different breakpoints. |
| New pages or new nav destinations | The 4th fixed bottom-bar slot (SDKs) and all other items already exist in `NAV_SECTIONS`/`MOBILE_TABS`; no new routes are created. |
| Generalizing `ui/drawer.tsx` to a bottom-sheet variant | User decision: keep the existing custom bottom-sheet in `MobileNav.tsx` (already built and covered by an e2e test) instead of unifying overlay components. |
| Per-column responsive hiding in `DataTable` (e.g., hide low-priority columns under 768px) | `overflow-x-auto` already prevents layout breakage; column hiding is a UX polish, deferred to P3. |
| Native mobile app / PWA installability | Out of scope — this is the existing web dashboard only. |
| Print stylesheets | Not requested. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here — nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Nav breakpoints | Mobile `<768px` (bottom bar), Tablet `768–1023px` (icon-only sidebar), Desktop `≥1024px` (full sidebar) | Matches Tailwind's existing `md`/`lg` defaults already used elsewhere in the codebase (`max-md:`, no custom breakpoints exist) — avoids introducing a non-standard breakpoint token. | y (user picked the 3-state option) |
| Bottom-bar 4th fixed slot | SDKs | User picked this explicitly over MCP or leaving only 3 fixed items. | y |
| Ultra-wide content cap | `max-width: 1920px`, centered, equal side margins | User picked this over "no global cap, per-page decisions." 1920px covers the largest common single-monitor resolution; beyond it is treated as margin, not content stretch. | y |
| Drawer implementation for "Mais" | Reuse existing custom bottom-sheet in `MobileNav.tsx` unchanged in mechanism, extended in content | User picked this over generalizing `ui/drawer.tsx`. Lowest risk: the sheet already renders `NAV_SECTIONS` + `SidebarFooter` and is covered by `e2e/personal-access-tokens.spec.ts`. | y |
| Tablet icon-only sidebar width | `72px` fixed rail, icons only, no visible labels, tooltip (existing `ui/tooltip.tsx`) on hover/focus shows the label | Not asked to the user (implementation-level detail, not a product decision) — 72px is a standard icon-rail width that fits a 40px touch/click target plus padding; tooltip reuses an already-installed Radix primitive instead of adding a new dependency. | n — flagged as default, not user-confirmed |
| Tablet icon-only sidebar section grouping | Section title text (`nav.sectionGeneral` etc.) hidden in icon-only mode; a thin `Separator` (already used elsewhere) marks section boundaries instead | Implementation detail. Showing rotated/truncated section text at 72px width has no clean solution; a divider preserves the grouping signal without forcing width back up. | n |
| Active-route highlight in icon-only mode | Same mechanism as today (background tint + left bar), applied to the icon button itself since there is no label to underline | Keeps one indicator implementation instead of a second bespoke style; consistent with `NavRow`'s existing `isActive` callback pattern. | n |
| `DataBrowserPage` two-pane layout (`240px_1fr`) at tablet | Tables panel switches to the existing mobile collapsed/scrollable-strip behavior (`max-h-[220px]` horizontal list) starting at `<1024px` instead of only `<768px` | The current mobile-only collapse (`max-md:flex max-md:flex-col`) is the closest existing pattern; reusing it at the new tablet boundary avoids inventing a fourth layout for this one page. | n |
| `min-w-[420px]` fixed-width form fields (`BrandSettingsPage`, `GitHubIntegrationPage`) | Replace with `w-full` + `max-md:min-w-0` (or equivalent: drop the hard floor below the `md` breakpoint) | This is the one confirmed concrete overflow bug found during investigation (forces horizontal scroll on any viewport <420px, e.g. iPhone SE at 375px). Fix is mechanical: remove the floor so flex children can shrink. | n — technical fix, not a product decision, applying it directly |
| Fixed 2-column grids (`SdkPage`, `WebhookMappingEditor`, `MCPPage`: `grid-cols-1 sm:grid-cols-2`) | Left as-is for this feature; the 1920px global content cap already bounds how wide these cards can stretch on ultra-wide monitors | Changing every fixed grid to `auto-fit`/`auto-fill` is a larger visual-polish effort not required to meet "doesn't break" — the global cap (RESP-04) already prevents the worst case. Revisit only if visual QA flags it. | n |
| Test coverage for new breakpoints | Add Playwright projects for mobile (390×844), tablet (820×1180), and ultra-wide (2560×1440) viewports; extend the nav-reachability assertion pattern already in `e2e/personal-access-tokens.spec.ts` to all three | No test infrastructure exists today beyond a single desktop Chromium project + one ad-hoc mobile viewport call. Three states need three verified viewports minimum. | n — standard practice, applying by default |
| `NAV_SECTIONS`/`MOBILE_TABS` remain the single source of truth | No new nav-config file; the tablet icon-rail and the bottom-bar drawer both continue reading from `src/components/layout/nav.ts` | Matches AGENTS.md-adjacent repo convention (single source of truth already exists and is reused by both current nav surfaces) — do not fork nav config per breakpoint. | y (implicit — this is how the code already works, not a new decision) |

**Open questions:** none — all resolved above or logged as assumptions.

---

## User Stories

### P1: Mobile bottom bar with 5 slots ⭐ MVP

**User Story**: As a user on a phone, I want a bottom navigation bar with the most-used sections plus a "More" menu, so that I can navigate the dashboard one-handed without a hidden sidebar.

**Why P1**: This is the explicit, named ask ("sidebar deve virar bottom bar com no máximo 5 itens... um deles deve ser um menu que abre um drawer"). Partially exists today (3 fixed tabs + More) — must be extended to the confirmed 4-fixed-plus-drawer shape.

**Acceptance Criteria**:

1. WHILE viewport width is `<768px` THEN the system SHALL render a fixed bottom bar with exactly 5 slots: Apps, Data Browser, Logs, SDKs, and a 5th "More" slot.
2. WHEN the user taps "More" THEN the system SHALL open a bottom-sheet drawer listing every remaining `NAV_SECTIONS` item the user's role can see (Users, Audit, Integrations, Settings, MCP), grouped exactly as the desktop sidebar groups them.
3. WHEN the user taps any of the 4 fixed bottom-bar items THEN the system SHALL navigate to that route and SHALL visually mark that slot as active using the same active-indicator convention as the desktop sidebar (fill/weight + tint).
4. IF the user's role does not have `platformAction` permission for an item shown inside the "More" drawer THEN the system SHALL omit that item, identically to how the desktop sidebar already omits it.
5. The system SHALL keep the bottom bar reachable above the iOS/Android safe-area inset (`env(safe-area-inset-bottom)`) at all supported mobile widths down to 375px.

**Independent Test**: Load `/apps` at a 390×844 viewport, confirm 5 bottom-bar slots render, tap "More", confirm the drawer lists Users/Audit/Integrations/Settings/MCP (superadmin role) or a role-filtered subset (non-superadmin role).

---

### P1: Tablet collapsed icon-only sidebar

**User Story**: As a user on a tablet (768–1023px), I want a compact icon-only sidebar instead of the full 264px sidebar or a mobile bottom bar, so that more horizontal space is available for content while navigation stays one tap away.

**Why P1**: Explicitly requested ("todo o resto deve ficar perfeitamente funcional em dispositivos... tablets") and confirmed by the user as a 3rd distinct nav state, not a shared mobile/tablet bottom bar.

**Acceptance Criteria**:

1. WHILE viewport width is `≥768px` and `<1024px` THEN the system SHALL render a fixed-width (`72px`) icon-only sidebar rail showing every `NAV_SECTIONS` item the user's role can see, with no visible text labels.
2. WHEN the user hovers or focuses a rail icon THEN the system SHALL show a tooltip with that item's label.
3. WHEN the user activates a rail icon (click/Enter) THEN the system SHALL navigate to that route and SHALL apply the same active-route visual treatment used on desktop, adapted to the icon-only layout (no left-bar-plus-label; icon container itself carries the tint/fill state).
4. The system SHALL preserve section grouping visually (a thin separator between `sectionGeneral`/`sectionDeployment`/`sectionSuperadmin` groups) without rendering the section title text.
5. WHILE the tablet sidebar is shown THEN the bottom bar SHALL NOT also render (mutually exclusive with the mobile state).

**Independent Test**: Load `/apps` at 820×1180 (portrait tablet), confirm a 72px icon rail (no labels) renders instead of both the 264px sidebar and the bottom bar, confirm hovering an icon shows its label as a tooltip, confirm clicking navigates correctly.

---

### P1: Desktop full sidebar unchanged in behavior, ultra-wide content capped

**User Story**: As a user on a desktop or ultra-wide monitor, I want the existing full sidebar to keep working exactly as it does today, and the main content to stop stretching edge-to-edge past a reasonable width, so that pages remain readable on very large screens.

**Why P1**: Explicitly requested ("funcionar perfeitamente em monitores ultra wide e super wide sem quebrar o layout"). The full-sidebar desktop behavior is the regression-risk baseline every other change must not break.

**Acceptance Criteria**:

1. WHILE viewport width is `≥1024px` THEN the system SHALL render the existing 264px full sidebar with labels, exactly as implemented today (no visual regression).
2. WHILE viewport width exceeds `1920px` THEN the system SHALL cap the main content area (the `<main>` region inside `DashboardShell`) at `max-width: 1920px` and SHALL center it horizontally, with the sidebar remaining pinned to the left edge of the viewport (not centered with the content).
3. WHILE viewport width is `≤1920px` (and `≥1024px`) THEN the main content area SHALL continue to use the full available width, unchanged from current behavior.
4. The system SHALL NOT introduce horizontal scrolling on the `<body>`/page level at any width from 1024px to 3840px on any route listed in `NAV_SECTIONS`.

**Independent Test**: Load `/apps` and `/data-browser` at 3440×1440 (ultra-wide) and 1920×1080, confirm content is capped/centered at the former and full-width at the latter, confirm no page-level horizontal scrollbar appears.

---

### P1: Fix confirmed sub-420px overflow in settings forms

**User Story**: As a user on a narrow phone (375px class), I want settings/integration forms to fit the viewport without horizontal scrolling, so that I can fill them out without side-scrolling every field.

**Why P1**: Concrete, already-identified bug (`min-w-[420px]` on multiple fields in `BrandSettingsPage.tsx` and `GitHubIntegrationPage.tsx` forces overflow below 420px) — not a hypothetical risk.

**Acceptance Criteria**:

1. IF viewport width is `<420px` THEN every form field currently declaring `min-w-[420px]` in `BrandSettingsPage.tsx` and `GitHubIntegrationPage.tsx` SHALL render at full available width instead of forcing a 420px floor.
2. The system SHALL NOT produce horizontal overflow on `/configuracoes` or `/integracoes/github` at 375px viewport width.

**Independent Test**: Load `/configuracoes` and `/integracoes/github` at 375×667, confirm no horizontal scrollbar and every field is fully visible without side-scrolling.

---

### P2: Playwright coverage for the three nav states and ultra-wide cap

**User Story**: As the team maintaining this dashboard, I want automated tests pinning the mobile/tablet/desktop nav behavior and the ultra-wide content cap, so that a future change can't silently regress responsiveness.

**Why P2**: Important for durability of this feature, but the dashboard functions correctly without it shipping in the same PR as the UI change — test infrastructure (new Playwright projects) is additive.

**Acceptance Criteria**:

1. WHEN the mobile Playwright project (390×844) runs THEN it SHALL assert the bottom bar renders exactly 5 slots and the "More" drawer lists role-appropriate items (extending the existing pattern in `e2e/personal-access-tokens.spec.ts`).
2. WHEN the tablet Playwright project (820×1180) runs THEN it SHALL assert the icon-only rail renders (not the full sidebar, not the bottom bar) and that clicking an icon navigates correctly.
3. WHEN the ultra-wide Playwright project (2560×1440) runs THEN it SHALL assert the main content area's rendered width does not exceed 1920px.

**Independent Test**: `npx playwright test` runs all three new projects green in CI alongside the existing desktop Chromium project.

---

### P3: Data Browser tablet layout parity

**User Story**: As a user on a tablet, I want the Data Browser's two-pane layout (table list + table content) to behave like it does on mobile rather than cramming the desktop 240px+1fr split into a narrower viewport, so the table list stays usable.

**Why P3**: Real but lower-severity gap — the desktop layout at tablet width is cramped, not broken (no overflow), so it is not a blocking correctness issue.

**Acceptance Criteria**:

1. WHILE viewport width is `<1024px` THEN `DataBrowserPage` SHALL apply the existing mobile collapsed/scrollable table-list behavior (currently gated at `<768px`) instead of the desktop `240px_1fr` grid.

---

## Edge Cases

- IF viewport width crosses a nav breakpoint while a menu/drawer is open (e.g., user rotates a tablet from portrait 820px to landscape 1180px while the desktop-equivalent state should apply) THEN the system SHALL close any open mobile/tablet-specific overlay (bottom-sheet) rather than leave it rendered in the new state.
- IF a role has zero visible items in a `NAV_SECTIONS` group (all gated out) THEN the system SHALL omit that group's separator/heading entirely at every breakpoint (matches current desktop behavior — not a new requirement, stated here to confirm it must hold at the new tablet/mobile-drawer states too).
- WHEN viewport width is exactly at a breakpoint boundary (768px, 1024px, 1920px) THEN the system SHALL resolve to the state for the wider range (i.e., boundaries are inclusive-lower per Tailwind's `min-width` semantics: `768px` is already "tablet", `1024px` is already "desktop", `1920px` is the last uncapped width).
- IF the browser window is resized live (not just loaded at a fixed size) THEN the system SHALL re-render the correct nav state without requiring a page reload (this must work via CSS media queries reactively, consistent with how `max-md:hidden` already behaves today).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| RESP-01 | P1: Mobile bottom bar with 5 slots | Execute | Verified |
| RESP-02 | P1: Tablet collapsed icon-only sidebar | Execute | Verified |
| RESP-03 | P1: Desktop sidebar unchanged | Execute | Verified |
| RESP-04 | P1: Ultra-wide content cap (1920px) | Execute | Verified (substantively covered via RESP-03 AC2; no distinct AC block of its own — see `validation.md`) |
| RESP-05 | P1: Fix `min-w-[420px]` overflow (Brand Settings, GitHub Integration) | Execute | Verified |
| RESP-06 | P2: Playwright coverage (mobile/tablet/ultra-wide) | Execute | Verified |
| RESP-07 | P3: Data Browser tablet layout parity | Execute | Verified |

**ID format:** `RESP-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 7 total, 0 mapped to tasks, 7 unmapped ⚠️ (expected — Tasks phase not yet run)

---

## Success Criteria

- [x] Every route in `NAV_SECTIONS` renders without horizontal page-level overflow at 375px, 768px, 1024px, 1920px, and 3440px viewport widths. (Structurally true via the global 1920px cap + `min-w-0` fixes; e2e coverage is 2 of 9 routes at 375px/2560px — see `validation.md` round 2, RESP-03 AC4 precision gap.)
- [x] Bottom bar (mobile), icon rail (tablet), full sidebar (desktop) are mutually exclusive and each reachable/functional for every role that has at least one visible nav item.
- [x] `min-w-[420px]` no longer appears in `BrandSettingsPage.tsx` or `GitHubIntegrationPage.tsx` without a `max-md:`/responsive override.
- [x] Main content area measurably caps at 1920px and centers beyond that width (verified via Playwright bounding-box assertion).
- [ ] Three new Playwright projects (mobile/tablet/ultra-wide) pass in CI. **Not met as literally stated** — `.github/workflows/` has no job that runs `npx playwright test`; these projects pass locally only. Wiring e2e into CI is a pre-existing gap in this repo (not introduced by this feature) — flagging as an open follow-up, not silently checking it off.
