# Handoff: Zeep Orbit Dashboard Redesign

## Overview
This package covers the redesign of the Zeep Orbit admin dashboard (BaaS platform for tech teams). It replaces the current dashboard UI with a new visual system and layout, and adds several features that don't exist yet in the current app. The single source of truth for this redesign is the file **`Zeep Orbit Redesign.dc.html`**, included in this bundle — it is a fully clickable, interactive prototype covering all screens and states (light/dark theme, empty/error/loading states, role variations, etc).

## About the design file
The bundled file is an **HTML/React design reference**, not production code to copy verbatim. It's built with inline styles and a lightweight templating runtime specific to the prototyping tool that made it — **do not port its markup or its component runtime directly**. Your job is to **recreate this design in Zeep Orbit's real codebase** (whatever stack that is — React/Vue/etc, with its existing component library, state management, and API layer), matching this prototype pixel-for-pixel in layout/spacing/color/type, but wiring it to real data and real backend calls instead of the mocked state in the prototype.

Open the HTML file in a browser to click through every screen and state. Use the "PREVIEW" bar at the top of the page — it's a screen switcher used only for this prototype (not part of the design) to jump between every screen/state without navigating the full flow.

## Fidelity
**High-fidelity.** Colors, type, spacing and component states are final. Copy is final (English). Recreate pixel-perfectly.

## Migration scope: replace current layout + add new features
This is not a from-scratch app — it's a redesign of the **existing** Zeep Orbit dashboard. Two kinds of work:

1. **Replace the current layout/visual system** on every existing screen (Apps home, App details, Data Browser, Logs, Users, Audit, Settings, SDKs, Changelog, Login) with the new one: new sidebar, new color system (dark + light theme), new typography, new card/table/drawer/modal patterns. The underlying data and business logic for these screens already exists in the current app — only the presentation layer and interaction patterns change, unless a specific behavior change is called out below.
2. **Build net-new features** that don't exist in the current app at all (list below). These need new UI *and* likely new backend endpoints/state — flag backend gaps to the team as you find them.

### New features to build (do not exist in current app)
- **Dark/light theme toggle** — persisted per user, switch in every sidebar footer.
- **Language toggle** (EN/PT flag switch in sidebar footer) — wire to whatever i18n exists or stub it if none does yet.
- **"Create with AI" chat drawer** — right-side drawer chat (not a modal) that lets the user describe an app conversationally; the AI asks clarifying questions, then on confirmation fires the same create-app API calls the manual form would. See the Apps home screen's "Create with AI" entry point.
- **Two-factor authentication (2FA)**, end to end:
  - Dashboard-user **Security** screen: enable/disable TOTP 2FA, QR + manual secret setup flow, 6-digit confirm step, one-time backup codes (shown once, with a "saved" checkbox gate before finishing).
  - **Login step-up**: after password, prompt for 6-digit code or backup code if 2FA is enabled.
  - Per-app **"Allow 2FA" / "Require 2FA"** toggles for that app's own end users (App details → Login providers tab).
  - Platform-wide **"Require 2FA for all admins"** toggle (Settings → Auth provider tab).
  - **Reset 2FA** action per user in the Users table (superadmin only).
  - Google-linked accounts show "managed by Google" instead of the toggle (2FA is out of scope for those).
- **Global dashboard roles / RBAC**: four roles — **Superadmin** (full access), **Admin** (own apps + platform users/settings, no Integrations), **Auditor** (read-only: All apps, Audit, no Users/Settings/Integrations), **Member** (own apps only, no superadmin section at all). The sidebar nav must *omit* items the current role can't access (not just disable them). A generic **403 Access Denied** page/route is needed for direct navigation to a blocked screen. New "All apps" read-only supervision view for Admin/Auditor (App details opens in a visibly read-only banner state when reached this way).
- **Enterprise licensing**: Settings → new **License** tab — shows current plan (Free / Enterprise), status badges (Active / Trial with countdown / Revoked), a feature list per plan, a "paste license key" form (key is write-only, never re-displayed), and a reusable "Enterprise" badge + upgrade modal pattern used to gate specific features elsewhere in the app.
- **Observability integrations**: App details → new **Observability** tab — OpenTelemetry (any collector, free), Datadog and New Relic (both gated behind the Enterprise license: visible and configurable even without a license, but show "Paused — requires Enterprise" and don't actually export until licensed). Each provider: enable toggle, status pill, masked API key with "Replace," provider-specific config fields.
- **App users management**: new screen (reached via a "Users" button on each backend app card) listing that specific app's *end users* (not dashboard admins) — search, provider badges (email/Google), active/inactive status, Deactivate/Reactivate/Reset sessions icon actions, pagination.
- **Delete confirmation dialog** — generic reusable modal, wired to every destructive delete action (backend app, frontend app, etc). Title/message adapt to what's being deleted.
- **Logout confirmation dialog** — wired to the logout icon in every sidebar footer.
- **Expanded Login/Storage provider config**: App details and Settings both move Login providers and Storage to an **accordion-of-cards** pattern (one card per provider, expand to configure) so future providers (Microsoft Entra, Azure Blob, GCS, etc.) slot in without redesign. Only one provider can be globally/app-level active at a time; inactive/future providers show a "SOON" badge and a disabled state.
- **Integrations page redesign**: generalized from a GitHub-only screen into a multi-provider **Integrations** page — Code tab (GitHub, Bitbucket, GitLab), Deploy providers tab (Render live today; Cloudflare, DigitalOcean, AWS, Azure, GCP listed as future).
- **Database behavior settings tab**: beyond the existing soft-delete toggle, add whatever additional DB behavior settings were scoped in chat (check the "Database" tab in Settings/App details for the final set implemented in the prototype).
- **Mobile app shell**: a true mobile-app-like experience (not just a responsive reflow) — bottom tab bar (Apps / Data / Logs) + a "More" hamburger opening a bottom-sheet drawer with the rest of the sidebar's nav items and the user/theme/logout footer. Prototype demonstrates this on Login, Apps home, and App details; extend the same shell pattern to the remaining screens.

### Screens carried over with new visual layer only
Apps home, App details (Database/Login/Storage/API/Members/Observability tabs), Data Browser, Logs, Users (dashboard admins), Audit, Settings (Branding/Database/Auth/Storage/License tabs), SDKs, Changelog, Login/Onboarding.

## Design tokens
Defined as CSS custom properties on `.theme-dark` / `.theme-light` classes at the top of the prototype file — copy these into your design-token system.

**Typography:** Manrope (body/UI), Space Grotesk (headings/wordmark-adjacent display text), monospace (code/keys/IDs). Base UI text 12.5–14px; page titles 22–26px.

**Dark theme:**
- Backgrounds: page `#0A0E1A`, surface `#111726`, surface-raised `#161D2E`, sunken `#0D1220`
- Borders: `#212A3D` / strong `#2C3650`
- Text: primary `#F2F4FA`, secondary `#9BA4BE`, tertiary `#6B7590`
- Primary (brand/action): `#5B6EF5`, hover `#4756DE`, tint `#1A2043`
- Accent: `#FF6B4A`, hover `#E85A3B`, tint `#3A2118`
- Success `#34D399` / tint `#0F2B22`; Warning `#FBBF24` / tint `#332708`; Danger `#F87171` / tint `#391818`
- Overlay `rgba(6,9,16,0.7)`; shadow-sm `0 1px 2px rgba(0,0,0,0.4)`; shadow-md `0 8px 28px rgba(0,0,0,0.45)`

**Light theme:**
- Backgrounds: page `#F5F6FA`, surface `#FFFFFF`, sunken `#EEF0F6`
- Borders: `#E3E6F0` / strong `#CDD2E4`
- Text: primary `#13172A`, secondary `#555F7C`, tertiary `#8A92AC`
- Primary: `#4753E0`, hover `#3A44C4`, tint `#ECEDFF`
- Accent: `#F0532F`, hover `#D6431F`, tint `#FEEAE3`
- Success `#16A34A` / tint `#E7F7ED`; Warning `#D97706` / tint `#FDF3E2`; Danger `#DC2626` / tint `#FCE9E9`
- Overlay `rgba(20,24,40,0.45)`; shadow-sm `0 1px 2px rgba(20,24,45,0.06)`; shadow-md `0 8px 28px rgba(20,24,45,0.10)`

**Shape:** cards/panels 12–16px radius; pills/badges/toggles fully rounded; icon buttons ~8px radius.

**Icons:** Google "Material Symbols Rounded" font (`<span class="material-symbols-rounded">icon_name</span>`), 15–20px, colored via `currentColor`.

## Assets
- `assets/orbit-icon.png` — Zeep Orbit icon mark (transparent PNG)
- `assets/orbit-wordmark.png` — Zeep Orbit wordmark (transparent PNG)
Both are used at small fixed heights (15–34px) throughout every sidebar and the login screen — get final/vector versions from the brand owner if higher-res is needed for large placements.

## State management notes
The prototype fakes all state with local component state (per-field toggles, a "demo state" switcher for License/2FA to preview all statuses, etc). In production:
- Theme (dark/light) and language should persist per-user (not just session).
- Current dashboard role should come from the authenticated session, not a client-side switcher (the prototype's "Viewing as" switcher in the preview bar is a design-review aid only, not a feature to ship).
- License status, 2FA status, and Observability provider status should be fetched from real backend state, not the prototype's mock arrays.

## Files
- `Zeep Orbit Redesign.dc.html` — the full interactive design reference (open in any browser)
