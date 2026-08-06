# Settings Consolidation — S1 (front-only) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retitle the dashboard Settings page to "Settings", add the handoff's 5th tab (License) and the disabled+SOON roadmap slots (Require 2FA, Require schema-change approval, License body), and align the existing tabs' copy to the handoff — all front-only, no backend.

**Architecture:** Edit the single page `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx` (the current Settings page, 4 tabs). Add i18n keys to both locales. Reuse the existing `SettingRow` + `Switch` + `ProviderCard` patterns and the established disabled+SOON treatment (D-DT08, `apps.soon` badge). No API calls, no new deps.

**Tech Stack:** React + Vite + TypeScript, `react-i18next`, Tailwind v4 design tokens, Material Symbols via `<Icon>`, Radix-based `Tabs`/`Switch` primitives.

## Global Constraints

- Every user-facing string via `react-i18next`, added to **both** `src/locales/en.json` and `src/locales/pt-BR.json` in the same change (AGENTS §5). No hardcoded PT/EN in components.
- English label casing = handoff sentence-case (AGENTS / redesign rule). pt-BR translates content; casing is EN-only.
- Disabled+SOON slots follow the D-DT08 pattern: control `disabled` + `apps.soon` badge + explanatory tooltip. No dead/enabled control without backend.
- Gate per task (this repo has **no** React component test harness): `python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json'))"` + `npx tsc -b` + `npm run build`, all clean. Visual check in the audit env (`./bin/zeep serve --port=8099`, superadmin `admin@test.com`/`test1234`, route `/configuracoes`).
- **Do not commit** — Julio commits (AGENTS §2). No `git add`/`git commit` steps.
- Presentation-only slice: no backend files touched, no API contract change.

Base dir for all relative paths: `internal/dashboard/ui/`.

---

### Task 1: Retitle page to "Settings"

**Files:**
- Modify: `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx:32` (PageHeader)
- Modify: `internal/dashboard/ui/src/locales/en.json`
- Modify: `internal/dashboard/ui/src/locales/pt-BR.json`

**Interfaces:**
- Produces: i18n keys `settings.pageTitle`, `settings.pageSubtitle` consumed by Task 1 only.

- [ ] **Step 1: Add i18n keys (en.json)** — near the existing `settings.*` block:

```json
"settings.pageTitle": "Settings",
"settings.pageSubtitle": "Customize how the dashboard looks and behaves for your company.",
```

- [ ] **Step 2: Add i18n keys (pt-BR.json)** — same location:

```json
"settings.pageTitle": "Configurações",
"settings.pageSubtitle": "Personalize a aparência e o comportamento do dashboard para sua empresa.",
```

- [ ] **Step 3: Point the PageHeader at the new keys** — `BrandSettingsPage.tsx:32`:

```tsx
<PageHeader title={t("settings.pageTitle")} subtitle={t("settings.pageSubtitle")} />
```

(The "Branding" **tab** label stays `settings.tabBranding` — only the page title changes.)

- [ ] **Step 4: Gate** — from `internal/dashboard/ui/`:

```bash
python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json')); print('JSON OK')"
npx tsc -b
npm run build
```

Expected: JSON OK, tsc clean, build succeeds.

---

### Task 2: Add the License tab (disabled + SOON)

**Files:**
- Modify: `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx:14-19` (TABS array), `:56-58` (TabsContent block)
- Modify: `src/locales/en.json`, `src/locales/pt-BR.json`

**Interfaces:**
- Consumes: `apps.soon` (existing badge string).
- Produces: i18n keys `settings.tabLicense`, `settings.licenseSoonTitle`, `settings.licenseSoonDesc`; component `LicenseTab` used only in this file.

- [ ] **Step 1: i18n keys (en.json)**:

```json
"settings.tabLicense": "License",
"settings.licenseSoonTitle": "Enterprise licensing",
"settings.licenseSoonDesc": "Activate a lifetime enterprise license to unlock SSO, audit export and more. Coming soon.",
```

- [ ] **Step 2: i18n keys (pt-BR.json)**:

```json
"settings.tabLicense": "Licença",
"settings.licenseSoonTitle": "Licenciamento enterprise",
"settings.licenseSoonDesc": "Ative uma licença enterprise vitalícia para liberar SSO, exportação de auditoria e mais. Em breve.",
```

- [ ] **Step 3: Add `license` to the TABS array** — `BrandSettingsPage.tsx:14-19`:

```tsx
const TABS = [
  { value: "branding", icon: "palette", labelKey: "settings.tabBranding" },
  { value: "database", icon: "database", labelKey: "settings.tabDatabase" },
  { value: "auth", icon: "public", labelKey: "settings.tabAuth" },
  { value: "storage", icon: "hard_drive", labelKey: "settings.tabStorage" },
  { value: "license", icon: "workspace_premium", labelKey: "settings.tabLicense" },
] as const;
```

- [ ] **Step 4: Add the TabsContent block** — after the `storage` TabsContent (`:56-58`):

```tsx
<TabsContent value="license" className="mt-0">
  <LicenseTab />
</TabsContent>
```

- [ ] **Step 5: Add the `LicenseTab` component** — at the end of the file:

```tsx
function LicenseTab() {
  const { t } = useTranslation();
  return (
    <div className="flex items-start gap-3 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <div
        className="flex size-9 shrink-0 items-center justify-center rounded-[10px]"
        style={{ background: "var(--accent-tint)", color: "var(--accent)" }}
      >
        <Icon name="workspace_premium" size={18} />
      </div>
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[14px] font-semibold text-[var(--text-primary)]">
            {t("settings.licenseSoonTitle")}
          </span>
          <span
            className="rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider"
            style={{ background: "var(--accent-tint)", color: "var(--accent)" }}
          >
            {t("apps.soon")}
          </span>
        </div>
        <p className="mt-1 text-[13px] text-[var(--text-secondary)]">
          {t("settings.licenseSoonDesc")}
        </p>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Gate** (same three commands as Task 1 Step 4). Expected: clean. Visual: 5th tab "License" renders, shows the SOON card, no console error.

---

### Task 3: Disabled+SOON toggles — Require 2FA (Auth tab) and Require schema-change approval (Database tab)

**Files:**
- Modify: `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx` (`GoogleAuthProviderCard` return ~:229, `SoftDeleteCard` return ~:148, add a shared `SoonRow` helper)
- Modify: `src/locales/en.json`, `src/locales/pt-BR.json`

**Interfaces:**
- Consumes: `apps.soon`, `SettingRow`, `Switch` (all existing).
- Produces: component `SoonRow` (used by this file); i18n keys `settings.require2fa`, `settings.require2faDesc`, `settings.requireApproval`, `settings.requireApprovalDesc`.

- [ ] **Step 1: i18n keys (en.json)** — exact handoff copy:

```json
"settings.require2fa": "Require 2FA for all admins",
"settings.require2faDesc": "Dashboard users without 2FA are prompted to set it up before continuing.",
"settings.requireApproval": "Require schema-change approval",
"settings.requireApprovalDesc": "Table and column changes wait for another Admin to approve before applying.",
```

- [ ] **Step 2: i18n keys (pt-BR.json)**:

```json
"settings.require2fa": "Exigir 2FA para todos os admins",
"settings.require2faDesc": "Usuários do dashboard sem 2FA são solicitados a configurá-lo antes de continuar.",
"settings.requireApproval": "Exigir aprovação de mudança de schema",
"settings.requireApprovalDesc": "Mudanças de tabela e coluna aguardam aprovação de outro Admin antes de aplicar.",
```

- [ ] **Step 3: Add a shared `SoonRow` helper** — near the top of the component section (after the `TABS` const / before `BrandSettingsPage`):

```tsx
// Disabled roadmap control: handoff shows it, backend not built yet (D-DT08 pattern).
function SoonRow({ label, description }: { label: string; description: string }) {
  const { t } = useTranslation();
  return (
    <div className="opacity-60" title={t("apps.soon")}>
      <SettingRow
        label={
          <span className="flex items-center gap-2">
            {label}
            <span
              className="rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider"
              style={{ background: "var(--accent-tint)", color: "var(--accent)" }}
            >
              {t("apps.soon")}
            </span>
          </span>
        }
        description={description}
        control={<Switch checked={false} disabled />}
      />
    </div>
  );
}
```

> Verify `SettingRow`'s `label` prop accepts a `ReactNode` (not just `string`). If it's typed `string`, widen it to `ReactNode` in `src/components/patterns` (the SettingRow definition) as part of this step — `tsc -b` will flag it if so.

- [ ] **Step 4: Render `SoonRow` for 2FA in the Auth tab** — inside `GoogleAuthProviderCard`'s return, **above** the `<ProviderCard>` (so 2FA sits at the top of the tab per the handoff). Wrap the existing return in a fragment:

```tsx
return (
  <div className="flex flex-col gap-4">
    <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <SoonRow label={t("settings.require2fa")} description={t("settings.require2faDesc")} />
    </div>
    <ProviderCard
      /* ...existing props unchanged... */
    >
      {/* ...existing children unchanged... */}
    </ProviderCard>
  </div>
);
```

- [ ] **Step 5: Render `SoonRow` for approval in the Database tab** — inside `SoftDeleteCard`'s return, add below the existing soft-delete `SettingRow`, before the Save button divider:

```tsx
<div className="mt-4 border-t border-[var(--border)] pt-4">
  <SoonRow label={t("settings.requireApproval")} description={t("settings.requireApprovalDesc")} />
</div>
```

- [ ] **Step 6: Gate** (three commands). Expected clean. Visual: Auth tab shows a disabled "Require 2FA … SOON" row above the Google card; Database tab shows a disabled "Require schema-change approval … SOON" row; neither toggle is interactive.

---

### Task 4: Audit existing tabs against the handoff inline spec

The existing-tab **label casing** was already aligned in the 2026-08-04 copy sweep (Auth provider, Company name, Storage, Access key ID, etc.). This task is the remaining visual/structural pass: field order, spacing, and any leftover copy divergence vs the handoff Settings region.

**Files:**
- Reference (read-only): `handoff/Zeep Orbit Redesign.dc.html` — Branding ~L1690, Database ~L1780, Auth provider ~L1860, Storage ~L1900.
- Modify (only if a divergence is found): `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx`, locale files.

- [ ] **Step 1: Diff each existing tab against its handoff region.** For Branding, Database (soft-delete), Auth (Google), Storage — compare rendered fields (labels, order, hints, section grouping) to the handoff inline styles. Write the divergence list to the task notes (file:line + handoff line).

- [ ] **Step 2: Fix only confirmed copy/order/spacing divergences.** Casing is already done — do not re-touch. Any new user-facing string goes through i18n (both locales). If nothing diverges, record "no divergences" and skip to the gate.

- [ ] **Step 3: Gate** (three commands) + visual pass across all 5 tabs at `/configuracoes` (dark + light). Expected: page titled "Settings", 5 tabs, existing tabs match the handoff, SOON slots present and inert.

---

## Self-Review

- **Spec coverage:** S1 scope items — (1) retitle → Task 1; (2) audit existing tabs → Task 4; (3) disabled+SOON slots: License tab → Task 2, 2FA + approval → Task 3. All covered.
- **Placeholders:** none — every string is exact (handoff-verified), every code step shows the code. Task 4 Step 2 is conditional-by-nature (fix *if* found) but bounded (casing excluded, i18n required), not a placeholder.
- **Type consistency:** `SoonRow({label, description})` defined in Task 3 Step 3, used in Steps 4–5 with the same prop names. `LicenseTab` defined and used in Task 2. TABS `value: "license"` matches the `TabsContent value="license"`. The `SettingRow` `label: ReactNode` caveat is called out with a concrete fallback.
- **Out of scope (do not do in S1):** any net-new control with backend (retention, RLS default, statement timeout, max CSV rows, pool cap), and any refactor of the pre-existing ad-hoc `fetch()` calls in the tab components (tracked separately; not this slice).
