# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

### Added

- **Render Environment ID field in Deploy Provider config** (superadmin, GitHub Integration page) — needed because Render assigns new services to an Environment, not a Project; the Project ID alone only works when that project has exactly one Environment (auto-resolved). Projects with multiple Environments now require this field explicitly, since the app has no way to guess which one to use.
- **Role-based access (4 platform roles)** — dashboard `admin`/`auditor`/`member` users now have permissions gated by a single matrix (`HasPlatformPermission`, mirrored in `permissions.ts` for the UI). New `useHasPlatformPermission` hook lets components omit — not just disable — items the current role can't access. Sidebar (desktop) and MobileNav (mobile bottom-sheet) now resolve visibility through this matrix; `RequireRole` guards stay in place for direct URL navigation. `superadmin` is unaffected. Part of the `dashboard-global-roles` spec (T-02 + T-07/T-08 of 9).
- **Per-app roles (admin / editor / viewer)** — every app (backend or frontend) now has a per-user membership row with one of three roles. The `ResolveAppRole` function is the single source of truth for "what role does this user have on this app?" and is called by every handler that needs to gate a per-app action. The two axes cross cleanly: a `superadmin` global is admin on every app without explicit membership; `admin`/`auditor` global get read-only access to any app via the cross-spec `CanReadAnyApp` extension (the `dashboard-global-roles` T-06 cross-spec link, now satisfied). Backend enforcement is wired into the existing handlers (list/read/update/delete + table CRUD + sync credentials + reveal key + archive); frontend apps have the same treatment. New "Members" tab in the app details page lets admins add, change role, and remove members. The "≥1 admin" invariant is enforced inside the store via transaction + `SELECT ... FOR UPDATE` on the admin rows, so two concurrent admins cannot both see "there's still another admin" and both proceed to demote themselves. Every mutation is audit-logged (`app_member.added` / `role_changed` / `removed`). Part of the `rbac-per-app` spec (T-01..T-09 of 12; T-07 reserved, T-10..T-12 follow-up).

### Changed

- **Copy aligned to the redesign handoff (visual-validation pass — Apps, App details, Login, Logs, Dashboard users, Audit, Integrations, Settings, App users)** — the handoff's README declares its copy final, so the audited screens now use the handoff strings and its sentence-case label casing (English only; casing is not a pt-BR rule). English label casing: `Your apps`, `Create app`, `Backend apps`/`Frontend apps`, `Back to apps`, `Login providers`, `Add table`, `App users`, `Audit log`, `Auth provider`, `Company name`, `Login logo`/`App icon`/`Upload logo`/`Upload icon`, `Client secret`/`Allowed domains`/`Access key ID` (Settings), and `App slug`/`Client secret`/`Private key (PEM)`/`Install on org`/`New template`/`Edit template`/`Delete template`/`Create template` (Integrations). Content strings updated in both locales to avoid drift: `apps.subtitle`, the login hero (`login.title`/`login.subtitle`), the `(S3)` removal on every storage label (`Storage`/`Global Storage`), `logs.title`/`subtitle` ("Request logs" / "Live traffic and metrics across every app."), `users.title`/`subtitle`/`create`/`createTitle`/`createDesc` (the dashboard-users screen is renamed from "Manage Users" to "Dashboard users"), `audit.subtitle` ("Every mutating action taken across the platform."), and `github.tabDeploy` ("Deploy" → "Deploy providers"). No key additions/removals, no API changes — string values only. Part of the `dashboard-redesign` spec (visual-validation). Structural handoff divergences found but deliberately **not** applied here (they are architecture/feature changes, not copy): the handoff's Dashboard-users table has a `SIGN-IN` column, the Data Browser toolbar has an `Export CSV` action, the App-users subtitle interpolates the app name, the Integrations page is titled "Integrations" (not "GitHub"), and the handoff Settings consolidates 5 tabs (Branding/Database/Auth provider/Storage/License) the current app splits differently — tracked separately, not in this change.
- **Dashboard-users list now reports each account's sign-in method** (handoff-fidelity, Fase 4 T4.3). `GET /dashboard/api/users` returns a derived `sign_in` field (`"google"` when the account is linked to a Google identity, `"email"` otherwise) — the underlying `google_id` stays server-side (`json:"-"`); `ListUsers` selects it and derives the value via a `signInMethod` helper. The Dashboard-users table gained the handoff's `SIGN-IN` column (between Role and Created) rendering `account_circle`/`mail` + label. New i18n keys `users.colSignIn`/`users.signInEmail`/`users.signInGoogle` (en + pt-BR). Additive response field; no breaking change.
- **Data Browser "Export CSV" button label is now translated** (handoff-fidelity, Fase 4 T4.4). The CSV export (backend `DataBrowserExport` + button) already existed; only the button's hardcoded `CSV` label was replaced with the i18n key `dataBrowser.exportCsv` ("Export CSV" / "Exportar CSV"), matching the handoff and satisfying the no-hardcoded-strings rule. No API or behavior change. (The export's 10 000-row cap will later read the "Max rows per CSV export" Settings control once that lands — tracked with the Settings consolidation.)
- **Integrations page renamed from "GitHub" to "Integrations", and the App-users subtitle now names the app** (handoff-fidelity, Fase 4 T4.1/T4.2). The 3-tab integrations page header now reads "Integrations" / "Connect code hosting and deploy providers for your apps." (was "GitHub" / "Connect your company's GitHub Org…"), matching the handoff and the sidebar nav label. The App-users screen subtitle interpolates the app name — "End users registered inside {{app}}." — resolved via the existing `useApp(id)` hook (was the static "…inside this app."). Both locales updated. Presentation-only; no API changes. The remaining Fase 4 gap — the full Settings consolidation (5-tab page with net-new Database/Limits/2FA/License controls) — is tracked in `dashboard-redesign/tasks.md` (T4.5) and needs its own spec + backend, not in this change.
- **Sidebar footer + update banner aligned to the redesign handoff** (visual-validation pass). The update-available banner is now the handoff's compact single-line pill (`arrow_circle_up` + `{version} available`, `primary-tint` background, no border) instead of the previous two-line bordered card; the user block gained the handoff's gradient avatar circle wrapped in a `--sunken` rounded card (was centered text with no avatar); the footer icon buttons are now equal-width (`flex-1`) with `--border-strong` borders per the handoff, and the GitHub link renders the real GitHub logo SVG (it was mistakenly showing the `code_blocks` glyph). The change-password (`lock`) button is kept — it's a real feature the prototype's footer didn't depict. Sidebar width corrected from 240px to the handoff's 264px. New i18n key `nav.updateShort` (en + pt-BR). Presentation-only; no API changes.
- **Dashboard redesign — Apps home, App details, and Data Browser migrated to the new component library** (dashboard-redesign spec, T2.1/T2.2/T2.3). The three highest-traffic screens were re-skinned onto the design tokens (`.theme-dark`/`.theme-light`), Material Symbols icons (`<Icon>`), and the level-2 pattern components — no more inline table/dialog/empty-state/badge markup. Apps home now composes `PageHeader` + `EmptyState`/`ErrorState`/`LoadingState` + `StatusPill` + `ConfirmDialog` (replacing `DeleteConfirmDialog`); App details' Login and Storage tabs became `ProviderCard` accordions, API/rate-limit rows use `SettingRow`, and token modals use `Dialog`/`ConfirmDialog`; Data Browser's table is now the shared `DataTable` (controlled sort + pagination + row-actions), with `ProviderCard`-free tree, `Dialog` edit form, and `ConfirmDialog` delete. All backend contracts, API calls, and actions are preserved exactly — presentation-only change. `lucide-react` and `framer-motion` removed from all three files (incremental icon migration, `lucide-react` drops from `package.json` once the last screen migrates). New i18n keys added to `en.json` + `pt-BR.json` in the same change.
- **Border-radius normalized to the handoff scale across shared primitives, level-2 patterns, and the migrated screens** — the dashboard's Tailwind v4 `@theme` redefines `--radius-lg: 16px` (upstream default is 8px), so every `rounded-lg`/`rounded-xl` rendered far rounder than the handoff (which uses 8px actions, 10px CTAs/inputs/boxes, 12px icon squares, 14px cards, 18px modals). Swept the offenders to explicit `rounded-[Npx]` values immune to the token override: primitives (`button`, `input`, `select`, `dialog` close + 18px modal container, `tooltip`, `accordion`, `drawer`), patterns (`DataTable`, `ProviderCard`, `ConfirmDialog`, `Enterprise`, `states`, `MaskedSecretField`, `AppMembersList`), and `AppsPage`/`AppDetailsPage`/`DataBrowserPage`/`TableCard`. Non-migrated pages still carry the old radii and will be normalized as they migrate. Presentation-only.
- **Apps home gained a blocked "Create with AI" header button** — the handoff's second header CTA is now present (accent-tinted, `auto_awesome` icon) but disabled with an "Em breve"/"Soon" badge and an explanatory tooltip, since no AI-assisted app-creation flow exists in the backend yet. It's a visible placeholder for the roadmap feature, not a functional entry point. New i18n keys `apps.createWithAi` / `apps.soon` / `apps.createWithAiSoon` (en + pt-BR).
- **Apps home cards aligned pixel-for-pixel with the redesign handoff** — backend app cards now always show an `Owned by …` line ("you" when the current user owns the app, resolved via `owner_id` vs `/me`), a colored auth pill (`success` tint + `mail` for email auth, neutral + `vpn_key` for token auth), and an outlined footer with labeled `Users`/`API docs` buttons plus icon-only edit/delete (no created-at line). Frontend app cards drop the date, use neutral status pills with a colored status dot (repo / deploy `Live`·`Deploy failed`·`Deploying`) + a primary-tinted template badge, inline the deploy error with an in-box `Retry deploy` action, and keep a labeled `Sync` + rocket + delete footer. Section headers (`Backend apps N` / `Frontend apps N`) now render whenever a section has apps. Presentation-only; all handlers and API calls unchanged.
- **Sidebar footer matches the handoff layout** — `Sidebar` and `MobileNav` now show the Changelog link as part of `SidebarFooter` (above the user card), not as a stray nav item, and the footer ends with `Zeep Orbit · v{version}` (sourced from `package.json` via the existing `import pkg from "../../package.json"` in `DashboardShell`). The dead `CHANGELOG_ITEM` export in `nav.ts` is removed. Mobile bottom-sheet closes on footer navigation via the new `onNavigate` prop. Presentation-only.
- **SDKs page re-skinned to the redesign handoff** — `framer-motion` and `lucide-react` removed from `SdkPage` (incremental icon migration; T3.1 drops the `lucide-react` dep once the last screen migrates). The page now uses `<Icon>` (Material Symbols Rounded) for `code`, `content_copy`, and `check`; the ad-hoc header becomes `<PageHeader>`; cards compose a 2-col grid on desktop (1-col on mobile) per handoff, with the `code` icon in a `primary-tint` square, the package name in mono, the install command in a `bg-sunken` strip with its own copy button, and the code snippet in a `bg-page` `pre` with another copy button. Presentation-only; no API changes. New i18n keys `sdk.title` / `sdk.subtitle` / `sdk.copyInstall` / `sdk.copySnippet` (en + pt-BR). Part of the `dashboard-redesign` spec (T2.8).
- **Changelog page re-skinned to the redesign handoff** — `lucide-react` removed from `ChangelogPage` (the empty-state `Megaphone` icon is now the shared `<EmptyState icon="campaign">` pattern). The page uses the handoff's timeline layout: a fixed 90px column on the left with the version (mono, `var(--primary)`) and release date (`var(--text-tertiary)`), and the content on the right with `border-left`. Section type colors are now design tokens — `features`→success, `improvements`→primary, `fixes`→warning, `security`→danger, `breaking`→accent — rendered as fully-rounded `xx-tint` background + matching foreground pills. The hardcoded `pt-BR` date formatter is replaced with `Intl.DateTimeFormat(i18n.language, …)`. `<PageHeader>` for title+subtitle, `<LoadingState>`/`<EmptyState>` for states. New i18n key `changelog.subtitle` (en + pt-BR). Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.9).
- **Login page re-skinned to the redesign handoff** — `framer-motion` and `lucide-react` removed from `LoginPage`. Layout is now the handoff's split: a `flex: 1.1` hero on the left (`bg-surface` + `border-right` + decorative `primary-tint`/`accent-tint` gradient orbs, logo + logotype at the top, the existing `login.title`/`login.subtitle` strings re-rendered as the 38px Space Grotesk headline and 15px secondary subtitle, `Zeep Orbit · v{version}` in the footer) and a `flex: 1` form on the right with the new `login.formTitle` / `login.formSubtitle` ("Welcome back" / "Sign in to manage your apps."), Label + Input primitives, the show/hide password eye as `<Icon name="visibility"|"visibility_off">` (with i18n `aria-label`s), the danger error pill on `var(--danger-tint)`, and the "or" + Google button below when `config.google_oauth_enabled` is true. No `email_password_enabled` flag yet, so the "Google only" case (F4-10) is left for the 2FA work. New i18n keys `login.formTitle` / `login.formSubtitle` / `login.showPassword` / `login.hidePassword` (en + pt-BR). Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.10).
- **Onboarding page re-skinned to the redesign handoff** — `framer-motion` removed from `OnboardingPage` (the AnimatePresence + stepVariants slide transitions are replaced with plain conditional rendering — the design no longer calls for animation between the 3 steps). The step indicator is now the handoff's numbered-circle row (3 circles with `check` for done, primary bg for current, sunken bg + border for pending, 10px primary/separator connector line between them). Each step lives in a single 460px card with `bg-surface`, `border`, `shadow-md`, `rounded-2xl`, padding 40px: welcome has a `rocket_launch` icon in a 14px `primary-tint` square + 22px Space Grotesk title; create-superadmin has the form with Label+Input primitives and a 2-col grid for password/confirm per the handoff; done has a `check` icon in a `success-tint` circle + 22px Space Grotesk title. All three buttons are full-width. No new i18n keys — the 18 existing `onboarding.*` keys cover all three steps. Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.10).
- **Google setup page re-skinned to the redesign handoff** — `framer-motion` and `lucide-react` removed from `GoogleSetupPage` (Eye/EyeOff become `<Icon name="visibility"|"visibility_off">` reusing the `login.showPassword`/`login.hidePassword` `aria-label`s; `Loader2` becomes `<Icon name="progress_activity" className="animate-spin">` — Tailwind v4 built-in, matching the pattern used in `AppsPage` and `DataBrowserPage`; `Check` becomes `<Icon name="check">`). The card container matches `OnboardingPage` (`bg-surface` + `border` + `shadow-md` + `rounded-2xl`, max-width 460px, padding 40px) with logo + title + description, three Label+Input primitive fields, show/hide password, the danger error pill, and a full-width primary submit. The "done" state uses the same `<Icon name="check">` in a `success-tint` circle pattern as `OnboardingPage`'s done step, with the 1.5s redirect unchanged. No new i18n keys — the 9 existing `googleSetup.*` keys cover all texts. Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.10).
- **App onboarding page polished to the redesign handoff** — `AppOnboardingPage` was already 80% re-skinned (it was using `<PageHeader>`, `<SettingRow>`, `<Icon>`, `<Switch>`, `<Input>`, `<Label>`, `<Button>`, and design tokens from the start), so the T2.10 work is just polish: the form card uses the explicit `rounded-[14px]` radius (handoff card size, immune to the `--radius-lg: 16px` token override) and `bg-[var(--bg-surface)]` (canonical, not the legacy `--surface` alias); the danger error box uses `rounded-[10px]` (input radius) with a solid `var(--danger)` border (the previous `border-[var(--danger)]/20` was a no-op because `--danger` doesn't have an RGB variant). No new i18n keys, no deps to drop. Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.10).
- **Login page right side now has an explicit `var(--bg-page)` background** — the handoff has the form side (right) darker than the hero side (left): hero = `var(--bg-surface)` (#111726), form = `var(--bg-page)` (#0A0E1A) via inheritance from the page container. The initial T2.10 Login commit relied on that inheritance, which works today but is fragile against CSS resets or future wrappers. The form side now sets `background: var(--bg-page)` explicitly so the visual difference is robust and the intent is clear in the code. Presentation-only. Part of the `dashboard-redesign` spec (T2.10 polish).
- **App users page re-skinned to the redesign handoff** — `framer-motion`, `lucide-react`, and the `Badge` primitive all removed from `AppUsersPage`. The screen is now built from the shared F0.5 patterns: `<DataTable>` for the users table (sort + pagination + loading/empty/error states built in), `<StatusPill tone="success"|"danger" label={...}>` for the Active/Inactive status, `<PageHeader>` for title + subtitle, and `<EmptyState>` for the empty/emptySearch cases. The 9 hardcoded Portuguese column headers ("Nome", "Email", "Telefone", "Provider", "Status", "Último acesso", "Criado em", "Ações") and the "Buscar"/"Anterior"/"Próximo" pagination labels are now in `en.json` + `pt-BR.json` (9 new keys: `appUsers.subtitle`, `appUsers.searchBtn`, `appUsers.clearSearch`, `appUsers.table.{name,phone,actions}`, `appUsers.pagination.{prev,next,range}`). The avatar falls back to a `linear-gradient(135deg, var(--primary), var(--accent))` circle with the user's initial when `avatar_url` is missing (matching the AppsPage/UsersPage pattern). The hardcoded pt-BR date formatter is replaced with `Intl.DateTimeFormat(i18n.language, …)`. The "back" link now points to `/apps/${id}` (the parent app) instead of `/apps` (the apps list), matching the handoff. Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.11).
- **Frontend apps page re-skinned to the redesign handoff** — `framer-motion` and 10 `lucide-react` icons (`Globe`, `Plus`, `RotateCcw`, `Trash2`, `Loader2`, `CheckCircle2`, `XCircle`, `Key`, `Copy`, `AlertTriangle`) all removed from `FrontendAppsPage`. The screen is now built from the shared F0.5 patterns: `<DataTable>` for the apps table (with a custom "name" column that shows the owner as a secondary line, matching the handoff), `<StatusPill tone="success"|"danger" dot label={...}>` for Ready/Failed, `<PageHeader>` for title+subtitle, and the inline loader + empty state replaced by `DataTable`'s built-in states. The create + sync dialogs keep the existing `<Dialog>` primitive but lose their hardcoded `bg-[#0F0F17]`/`border-white/[0.08]` (now default to the restyled Dialog tokens); the 6 inline `bg-[#3B82F6]` blue buttons are replaced with the default primary `<Button>`; the "Sync" / "Retry" / "Delete" ghost buttons use `var(--primary)`, `var(--warning)`, `var(--danger)` respectively. The 3 `bg-[#22C55E]/[#EF4444]/[#F59E0B]` colored status bits are now the `success`/`danger`/`warning` tones of the shared `StatusPill`. The `<CopyButton>` helper uses `<Icon name="content_copy">` and shows `toast.success` on click. All hardcoded Portuguese error strings ("Failed to load frontend apps", "Network error", etc.) now route through `t('frontendApps.*')` / `t('common.*')`. No new i18n keys — the 78 existing `frontendApps.*` keys already cover every text in both locales. Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.12).
- **GitHub integration page re-skinned to the redesign handoff** — `framer-motion` and 10 `lucide-react` icons (`Save`, `Eye`, `EyeOff`, `Loader2`, `Link2`, `Trash2`, `Plus`, `Pencil`, `Rocket`, `ShieldAlert`) all removed from `GitHubIntegrationPage`. The screen — the 3-tab superadmin-only Integrations page (Config / Templates / Deploy) — is now built from the shared F0.5 patterns: `<PageHeader>` for title+subtitle, `<Tabs>` for the 3 sub-pages, `<DataTable>` for the templates list, `<StatusPill tone="success"|"warning">` for Connected/NotConnected + Active/Inactive, `<EmptyState>`/`<LoadingState>` for the templates states, and `<Icon>` for all glyphs. The superadmin role guard (formerly `motion.div` with `ShieldAlert`) is now a static card with `<Icon name="shield">` in `var(--hover-surface)`. The 3 dialogs (disconnect, add/edit template) lose their hardcoded `bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0` overrides and use the restyled `<Dialog>` primitive with default tokens; the icon squares inside dialogs (red for disconnect, neutral for add/edit) are replaced with `var(--danger-tint)`/`var(--hover-surface)` + matching `<Icon>` + border. The hardcoded pt-BR error strings ("Failed", "Connection error") now route through `t('common.*')` / `t('github.*')`. A new `<PageFooter>` helper consolidates the success/error message pattern (rounded-[10px] with `success-tint`/`danger-tint` background + matching foreground + border) used in Config, Templates form, and Deploy tabs. The native `<select>` for Render service type is replaced with the shared `<Select>` primitive. The `inputClass` constant with hardcoded `border-white/[0.10] bg-white/[0.06]` is gone — the restyled `<Input>` already has the right defaults. No new i18n keys — the 46 `github.*` + 16 `deploy.*` + 1 `integrations.*` + 8 `brand.*` keys already cover every text. Presentation-only; no API or backend changes. Part of the `dashboard-redesign` spec (T2.12).
- **`lucide-react` dependency dropped from `package.json`** — the last 4 non-redesign files that still imported `lucide-react` (`ChangePasswordModal`, `DeleteConfirmDialog`, `TableCard`, and the Radix `Select` primitive wrapper) were migrated to the shared `<Icon>` wrapper using the same icon-name mapping as the rest of the redesign (`Lock`→`lock`, `Loader2`→`progress_activity`+`animate-spin`, `Eye`/`EyeOff`→`visibility`/`visibility_off`, `CheckCircle`→`check_circle`, `AlertTriangle`→`warning`, `Plus`→`add`, `Trash2`→`delete`, `Table2`→`table_chart`, `Link2`→`link`, `AlertCircle`→`error`, `Check`→`check`, `ChevronDown`/`ChevronUp`→`keyboard_arrow_down`/`keyboard_arrow_up`). The dep is then removed via `npm uninstall lucide-react` (updates `package.json` and `package-lock.json` atomically). 10 lines dropped from the lockfile, 0 references in `src/`. Material Symbols Rounded via the existing self-hosted `<Icon>` is now the single icon source for the dashboard. Part of the `dashboard-redesign` spec (T3.1).
- **`framer-motion` dependency dropped from `package.json`** — the last 2 files that still imported it were migrated to the design system's canonical animation approach: `ChangePasswordModal` now composes the F0.5 `<Dialog>` primitives (Radix overlay + content, `animate-in`/`fade-in-0`/`zoom-in-95` classes per the shared `ui/dialog.tsx`) instead of hand-rolled `AnimatePresence`/`motion.div`; the `TableCard` entrance animation is a plain `div` with `animate-in fade-in-0 slide-in-from-bottom-2` classes. `npm uninstall framer-motion` updates `package.json` + `package-lock.json` atomically; 0 references in `src/` (only historical comments remain). Completes T3.1 — the redesign's dependency removal (lucide + framer-motion) is now fully done. Note: `tailwindcss-animate` is not installed, so the `animate-in`/`fade-in-0` utility classes (already used by `Dialog`/`Drawer`/`Select`/`Tooltip` F0.5 primitives) are currently inert no-ops; installing the plugin would activate entrance/exit animations app-wide and is a T3.2 audit follow-up, not part of this change.
- **Design audit sweep (T3.2)** — the final audit found and fixed the leftover token violations the earlier sweeps missed: (1) **bug: `--bg-surface` token never existed** — 6 screens used `var(--bg-surface)` (`LoginPage` hero, `AppOnboardingPage`, `GitHubIntegrationPage`, `SdkPage`, `OnboardingPage`, `GoogleSetupPage`) which resolves to nothing, so those surfaces rendered transparent; corrected to the canonical `var(--surface)` (handoff token, #111726 dark / #FFFFFF light). (2) **dead code removed**: `DeleteConfirmDialog.tsx` (superseded by `ConfirmDialog`, 0 usages) and the entire `magicui/` folder (`animated-gradient-text`, `border-beam`, `shine-border` — 0 usages anywhere). (3) **radius sweep completed**: the remaining `rounded-lg`/`rounded-xl`/`rounded-2xl` (which render 16/20/24px because the Tailwind v4 `@theme` redefines `--radius-lg: 16px` and `--radius-xl: 20px`) were normalized to the handoff scale's explicit `rounded-[Npx]` across the shell and migrated screens — `Sidebar`, `SidebarFooter`, `LogsPage`, `LoginPage`, `AppUsersPage`, `TableCard`, `DashboardShell`, `UsersPage`, `AccessDenied`, `DataBrowserPage`, `AppDetailsPage`, `GitHubIntegrationPage`, `SdkPage`, `BrandSettingsPage`, `GoogleSetupPage`, the dev sandbox, and `ChangePasswordModal`. (4) **`ChangePasswordModal` tokenized**: hardcoded colors (`#F8FAFC`, `#64748B`, `#94A3B8`, `#ef4444`, `rgba(239,68,68,…)`, `white/[0.06]`) swapped for design tokens, the legacy `brand-*` gradient replaced with canonical `var(--primary)`/`var(--accent)`. The 460px cards on `OnboardingPage`/`GoogleSetupPage` keep `rounded-2xl` deliberately (documented design in the T2.10 entries). Presentation-only; no behavior or API changes. Completes T3.2 of the `dashboard-redesign` spec.
- **Dashboard user roles restructured from 2 to 4 tiers** — `zeep_system.dashboard_users.role` now accepts `superadmin`, `admin`, `auditor`, `member` (was `superadmin`, `admin`). Existing `admin` users are reclassified to `member`; `superadmin` is untouched. `auditor` is a new read-only platform role (read access to all apps and the audit log, no writes). `admin` is now a platform-management role distinct from the per-app "owner" pattern that `member` replaces. Migration is idempotent (DROP IF EXISTS + UPDATE + ADD inside the same transaction in `ProvisionZeepSystem`) and runs automatically on every server boot. Part of the `dashboard-global-roles` spec (T-01 of 9) — the platform permission matrix and `HasPlatformPermission` gate are now wired into both backend enforcement and the dashboard UI (see Added above). The cross-spec link to `rbac-per-app` (`admin`/`auditor` read access across all apps) is still pending T-06.
- **`app_ownership` table dropped** — the pre-rbac co-ownership table is no longer needed. `app_members` is now the single source of truth for per-app membership; the co-owner rows it carried were migrated to `app_members` as `admin` in T-02 (idempotent `ON CONFLICT DO NOTHING`). `ProvisionZeepSystem` now `DROP TABLE IF EXISTS app_ownership` on every boot, so existing databases with the old table are cleaned up automatically. `CreateApp` now adds the new app's owner to `app_members` directly (not to the dropped `app_ownership`). Safe to ship because T-04 + T-05 enforcement is 100% on `ResolveAppRole`. Part of the `rbac-per-app` spec (T-08).

### Fixed

- **GitHub Integration page (config, templates, deploy provider) had no role guard on the frontend** — every action there is `superadmin`-only server-side, but an `admin` navigating to the page directly saw the forms render and silently 403 on every request. The page now checks the current user's role and shows a clear "superadmin access required" message instead.
- **Admin users couldn't see GitHub templates in the frontend-app creation modal** — `GET /dashboard/api/github/templates` was restricted to `superadmin`, but the modal (used by any authenticated role) calls that same endpoint and silently swallowed the resulting 403, leaving the template select empty. Listing is now open to any authenticated role; managing templates (create/update/delete/set-active) stays `superadmin`-only.
- **Frontend app deploy could leave an orphaned Render service after a failure**, permanently blocking that name — Render service name is always the app's slug with no suffix, and `deploy_service_id` was only persisted after every deploy step (custom domain, etc.) succeeded. If Render created the service but a later step failed, the ID was never saved; deleting or archiving the app then couldn't clean up the Render service (nothing to delete), so any retry or new app with the same slug hit `already in use`. The service ID is now persisted right after Render confirms creation, before any further step.
- **Frontend app creation intermittently failed to deploy** with transient `404`/`500` errors from Render right after creating the service — Render's GitHub App integration can take a few seconds to notice a repo created moments earlier via the GitHub API, and the single deploy attempt had no retry. `CreateService` now retries those two statuses with backoff (up to 4 attempts) before failing; other errors (name conflict, rate limit) still fail immediately.
- **Deployed Render services were never actually placed in the configured Project** — they were created loose in the workspace instead. Render's create-service API has no `projectId` field (that field is silently ignored); services are assigned via `environmentId`, and the follow-up call meant to fix this up (`PATCH /services/{id}` with `projectId`) isn't a real Render endpoint either, and its failure was swallowed. The configured Project ID is now resolved to its Environment at startup and sent as `environmentId` on creation. Only supports a single Environment per configured Project for now — deploy fails with a clear error instead of guessing if the project has more than one.

## [0.5.0] — 2026-07-30

### Added

- **Update notifications** — sidebar banner (above Changelog) alerts when a new Zeep Orbit release is available on GitHub, showing the version and linking to the release page. Backend proxies GitHub's releases API with a 1-hour cache to avoid rate-limit exposure.
- **Phone field in per-app auth Swagger docs** — `/register` and `PATCH /me` request bodies, and the user response schema, now include `phone` (already accepted and persisted by the handlers, just missing from the hand-written OpenAPI generator).
- **Column default value in the backend app schema builder** — set a default for a table column when creating or editing it in the dashboard. `integer`/`bigint`/`numeric` accept a literal value; `boolean` picks `true`/`false`; `uuid`/`timestamptz` can auto-generate (`gen_random_uuid()`/`now()`) via a strict, type-scoped allowlist of SQL expressions (not free-form — the value is validated server-side before it ever reaches the generated DDL). Not available for `text`/`jsonb` columns.

### Fixed

- **App token refresh endpoint missing from Swagger** — `POST /{app}/auth/token/refresh` existed in the router but was never registered in `internal/docs/generator.go`, so it didn't show up in the per-app OpenAPI docs. Now documented, gated to apps without email/password auth (same scope as the endpoint itself); apps with email auth keep using the separate, already-documented `/auth/refresh` (refresh_token grant).
- **Dashboard Google login failing behind multiple replicas** — OAuth `state` (CSRF protection) was kept in an in-memory map per process; if `/login` and `/callback` landed on different pods behind a non-sticky load balancer, the callback always failed with "session expired or invalid." Now signed stateless (HMAC-SHA256), matching the existing per-app Google OAuth flow.
- **GitHub App installation callback had the same in-memory `state` issue** as the Google login above — fixed the same way (signed stateless HMAC-SHA256, keyed on the App's private key).
- **`/apps/{id}/users` always returned an empty list** when email auth was enabled — 4 handlers built the app schema name incorrectly (`"app_" + name` instead of the actual naming rule), and the resulting Postgres error was silently swallowed.
- **`PATCH /{app}/auth/me` returned 405** despite being documented — route was registered as `PUT`.
- **Auth provider "allowed domains" field couldn't be cleared** — emptying it in Settings and saving reported success but the old value came back on reload; the frontend omitted the field entirely instead of sending an empty list.
- **Google login button and brand theme flashed incorrectly on first paint of the login screen** — two redundant, uncached fetches of the same config endpoint resolved after the first render. Unified into a single shared query, gated behind the app's loading screen.
- User registration (bootstrap, dashboard user creation, app signup) now validates email format and normalizes email (lowercase) and name (Title Case) before persisting.

---

## [0.4.0] — 2026-07-26

### Added

- **Changelog page** — new `/changelog` page in the dashboard showing full release history (all 12 releases from v0.1.0 to v0.3.0), organized by version with categorized sections (Features, Improvements, Fixes, Security, Breaking Changes). Backed by a static `internal/dashboard/changelog.json` embedded into the Go binary via `//go:embed` — no per-instance database. Sidebar link (Megaphone icon) above the user section, also on mobile bottom bar.
- **Custom domains for frontend apps** — configure a custom domain directly from the dashboard. Domain modal with live preview of the full domain (subdomain + base domain) and DNS CNAME instructions pointing to the Render service URL. Base domain configured in Integrations → Deploy. Backend integration with Render's custom domain API (`POST /v1/services/{id}/custom-domains`).
- Inline domain editing on the frontend app card.
- Deploy retry button on frontend app cards when deploy fails.
- New `README.pt-BR.md` — GitHub auto-detects user locale and shows the Portuguese version.

### Changed

- **Brand repositioning** — platform identity shifted from "Database-first backend platform" to "The complete platform for tech teams." Login page, onboarding page, and dashboard subtitle updated across pt-BR and English. README rewritten for the full platform scope (backend apps, frontend deploy, SDKs, changelog), roadmap updated (M3-M7 marked done, M8 added).
- **Card redesign** — both backend and frontend app cards restructured with header/content/footer layout; status rows organized per category; creation date moved to the footer.
- Loading states and toast notifications added to every async operation.
- Unified delete confirmation dialog (glass-morphism design) used consistently across all delete actions.
- Global `cursor-pointer` on all button elements via the shadcn/ui Button base class.
- Sync modal now shows a description of what the deploy key is for and a "View usage instructions" button, instead of raw key reveal.

### Fixed

- **Login page** — subtitle no longer duplicates the title; both title and subtitle centered.
- **Render API** — custom domain field corrected to `name` instead of `domain`.
- **Render API** — PATCH `/v1/services/{id}` used to assign `projectId` after creation.
- **Render API** — owner object properly unwrapped from `GET /v1/owners` response.
- **Render API** — request body logged on create service error for debugging.
- **Frontend** — broken JSX in `AppsPage` causing build failure.
- **Domains** — subdomain and base domain whitespace trimmed before constructing the full domain URL.

---

## [0.3.0] — 2026-07-25

### Added

- **App Tokens** — JWT-based token management for apps without email/password auth. Dashboard exposes 5 endpoints (list, create, revoke, revoke-all via secret regeneration, view secret) and a new "Tokens" tab on the app details page (create with configurable expiration — 7d/30d/365d/never —, one-time JWT reveal with copy, status badges, 2-step confirm to regenerate the app secret).
- `POST /{app}/auth/token/refresh` — reissues an app token JWT (same `jti`), extending `expires_at` by the token's original duration.
- `zeep_system.app_tokens` table (`id`, `app_id` FK cascade, `name`, `jti` unique, `expires_at`, `revoked_at`, `last_used_at`, `created_at`), indexed on `app_id` and `jti`.
- `internal/tokencache` — shared in-memory jti activity cache (30s TTL) used by the request-path middleware and invalidated immediately on revoke/regenerate-secret.

### Fixed

- `JWTMiddleware` validated app-token `jti`/revocation using a second JWT parse with a `nil` keyFunc, which `golang-jwt/v5` always rejects — the revocation check was dead code; a revoked token kept authenticating until its own `exp`. Now reuses the claims from the already-verified parse.
- `RegenerateAppSecret` was missing the app-ownership check present in every sibling token endpoint, letting any authenticated dashboard user rotate the JWT secret (and revoke all tokens) of an app they don't own.
- `CreateAppToken` could nil-pointer-dereference on a registry cache miss; now reads the JWT secret from the already-loaded app row instead.
- jti activity cache was never invalidated on revoke/regenerate-secret, giving a revoked token up to 30s of extra validity.
- `randomJTI` fell back to a fixed, predictable id when `crypto/rand.Read` failed instead of returning an error.

### Security

- Rate limiting added to `POST /{app}/auth/token/refresh`, matching `/register` and `/login`.

---

## [0.2.0] — 2026-07-13

### Fixed

- **Company Name / Theme resetting on restart** — the server reseeded `zeep_system.brand_config` on every boot via an upsert that always applies the env var (or hardcoded default) as the new value, overwriting anything saved through the dashboard Settings page. Startup seeding is now insert-only (`ON CONFLICT DO NOTHING`) and never touches an existing row — saved settings survive restarts.

### Removed

- **Custom logo upload** — removed the ability to upload a custom login logo / app icon from Settings. The dashboard and login page now always use the default Zeep Orbit logo.

---

## [0.1.11] — 2026-07-02

### Fixed

- **Google OAuth "session expired" on callback** — OAuth `state` was stored in an in-memory map per pod. With multiple replicas and no sticky session, `/login` and `/callback` could land on different pods, so the state was never found and login always failed with "Login expired, please try again". State is now a stateless, HMAC-signed token (embeds `redirect` + expiry, signed with the app's JWT secret) — any replica can validate it without shared storage.
- **API docs branding** — Generated OpenAPI spec title and docs index page now show "API Documentation" instead of the hardcoded "zeep-orbit" name.

### Changed

- **Dashboard copy** — Login page and app subtitle updated to "Database-first backend platform" / "Create backends from your database", with new supporting subtitle text.

## [0.1.10] — 2026-07-02

### Added

- **Global S3 Configuration** — Superadmin configures global S3 (endpoint, region, keys, bucket). Admins creating apps only need a folder name (auto-filled with app name). Files stored as subfolders within the global bucket.
- **Brand Logos** — Superadmin uploads login logo and sidebar icon to global S3 bucket. Instant sidebar update. Fallback to built-in SVGs when not configured.
- **Google Setup page** — First-time Google login users are prompted to set name + password as backup auth method.
- **Settings tabs** — Settings page reorganized into 5 tabs: Branding, Logo, Database, Auth Provider, Storage.
- **Toast notifications** — `sonner` toasts replace native `alert()` throughout the dashboard.
- **Public brand config endpoint** — `GET /dashboard/api/brand/config` (unauthenticated) returns logo URLs for login page.
- **`needs_setup` on `/api/me`** — Frontend detects Google users who need to complete account setup.
- **`storage_configured` on `/api/config`** — Frontend knows if global S3 is active.
- **`Folder` field in StorageConfig** — Files in global S3 are prefixed with the app's folder name.

### Fixed

- **Helm chart 404** — Chart now published alongside Docusaurus docs at `https://zeeplabs.github.io/zeep-orbit/helm/` (was on orphan `gh-pages` branch ignored by Pages).
- **Helm `--config` flag** — Removed invalid `--config /app/apps.yaml` from deployment args (`zeep serve` doesn't use config files).
- **Race condition on startup** — `ProvisionZeepSystem` wrapped in transaction with `pg_advisory_xact_lock` to serialize across concurrent pods.
- **Logo upload overwrite** — Uploading one logo no longer clears the other.
- **Global S3 save resets soft delete** — `GlobalStorageCard` now preserves existing `soft_delete_enabled`.
- **Hardcoded texts** — All user-facing strings in BrandSettingsPage and AppFormPage storage section moved to i18n keys.

### Changed

- **Settings page** — Now uses tabs routed via `?tab=` (branding, logo, database, auth, storage).
- **App form storage** — When global S3 is active, bucket field is read-only and automatically set to the app name.
- **Dashboard sidebar** — Icon fetched dynamically from `GET /dashboard/api/brand/config` with query cache invalidation on upload.
- **`storage_config` column** — App storage config now supports `folder` field for global S3 subfolder prefix.
- **`brand_config`** — Added `icon_url` column for sidebar icon.
- **`system_config`** — Added `storage_config` JSONB column for global S3 settings.

## [0.1.5] — 2026-06-29

### Added

- **Audit Log** — action history tracked in `zeep_system.audit_log` (who, what, when, IP). Dashboard UI with filters by action/user, pagination. Superadmin only.
- **File Storage per App** — S3-compatible buckets (DO Spaces, Magalu, AWS, MinIO). Config per app via dashboard. Endpoints: upload, list, get, download (signed URL), delete. Uses `aws-sdk-go-v2`.
- **Rate Limiting per App** — configurable RPM via `rate_limit_config` JSONB. Sliding-window middleware per IP. Config in dashboard "API" tab. Returns 429 when exceeded.
- **i18n — pt-BR + English** — `i18next` + `react-i18next` in all 13 pages. Language switcher in sidebar. ~250 translation keys.
- **Language per User** — `language` column in `dashboard_users`. `PUT /api/me/language`. Auto-applied on login.
- **SDK Clients** — 6 official clients, same API design:
  - TypeScript: `@zeeptech/orbit-client` (npm)
  - Go: `github.com/zeeplabs/orbit-go` (git tag)
  - Python: `zeeplabs-orbit-client` (PyPI)
  - Rust: `zeep-orbit-client` (crates.io)
  - Java: `com.zeeplabs:orbit-client` (Maven Central)
  - PHP: `zeeplabs/orbit-client` (Packagist)
- **Dashboard — tabs no form** — AppFormPage reorganizado em 3 tabs + roteadas via `?tab=`
- **Dashboard — owner no card** — superadmin vê nome/email do dono do app no card
- **Dashboard — nome do usuário** — coluna `name` em `dashboard_users`. Campos no onboarding e criar usuário
- **Dashboard — sidebar com nome** — exibe nome do usuário (fallback email)
- **Dashboard — favicon** — SVG logotype como favicon
- **Dashboard — English default** — idioma padrão alterado para `en`

### Changed

- Dashboard tabs roteadas via `useSearchParams` (preserva tab ao recarregar)
- Backend: todas as queries de app agora incluem `rate_limit_config`, `storage_config`, `owner_email`, `owner_name`
- Backend: `ListApps` faz JOIN com `dashboard_users` para dados do dono

### Docs

- README atualizado com File Storage, Rate Limiting, SDK Clients, i18n
- Docusaurus: novas páginas `api/files.md`, `api/rate-limiting.md`, `clients.md`
- RELEASE.md: instruções de publicação para todos os 6 SDKs
- CHANGELOG.md: esta entrada

## [0.1.0] — 2026-06-28

### Added

- Web dashboard (React + Vite + TypeScript, embedded via `go:embed`)
- Dashboard auth: email/password login + session cookies
- Dashboard auth: Google OAuth sign-in (config via DB or env vars, encrypted secrets)
- Dashboard onboarding wizard (first-time superadmin setup)
- Dashboard app CRUD (create/edit/delete apps with dynamic tables & columns)
- Dashboard user management (superadmin creates/manages dashboard admins)
- Dashboard data browser (browse, filter, sort, edit inline, delete rows, export CSV)
- Dashboard real-time request logs with metrics (ring buffer, app-level filter)
- Dashboard white-label branding (5 themes, company name, persisted to DB)
- Dashboard change password (own + superadmin for any user)
- Dashboard app users management (list, search, deactivate/reactivate, reset sessions)
- Dashboard auth providers configuration (Google OAuth setup via UI)
- Per-app auth providers (email + Google OAuth configurable per app)
- Native email/password auth per app (register, login, refresh, logout, me)
- Google OAuth per app (/{app}/auth/google/login + callback)
- Row-Level Security (`rls: owner` — auto-filter by JWT `sub`)
- OpenAPI/Swagger docs auto-generated per app
- Helm chart (production-grade: HPA, PDB, ingress, ServiceMonitor, IRSA)
- K8s manifests (Kustomize)
- GitHub Actions CI/CD (multi-platform Docker build, Helm chart release)
- AES-256-GCM encryption for sensitive data at rest
- Auth providers config table (`zeep_system.auth_providers`)
- `go mod tidy` — dependency cleanup

### Fixed

- Login 500 error when `google_id` is NULL (COALESCE fix for pgx v5)
- FK violation on DataBrowserCreate (owner_id injection removed)
- Race condition TOCTOU on bootstrap endpoint
- DDL injection prevention on table/column names
- JWT secret exposure in API responses
- React Query cache not cleared on login/logout (user switching)

### Security

- Rate limiting on public auth routes (10 req/min)
- Security headers (X-Content-Type-Options, X-Frame-Options, etc.)
- bcrypt cost 12 for password hashing
- CSV formula injection protection
- Encryption at rest for OAuth client secrets (AES-256-GCM)
