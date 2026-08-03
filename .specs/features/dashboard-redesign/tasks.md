# Dashboard Redesign Tasks

**Spec**: `.specs/features/dashboard-redesign/spec.md`
**Design**: `.specs/features/dashboard-redesign/design.md`

Ordem de fases é dependência real: Fase 0 → 0.5 bloqueiam 1.5 → 2. Fase 1 (backend RBAC) roda em paralelo, mas o shell (1.5) espera `/me` expor role. Cada task de tela migrada só é "done" com build/test/vet/gofmt/tsc/vite limpos (AGENTS §3) e i18n en+pt-BR no mesmo commit.

Legenda status: ☐ pending · ◐ in progress · ☑ done

---

## Fase 0 — Fundação (bloqueia tudo)

- [x] **T0.1** — Criar `src/styles/tokens.css` com `.theme-dark`/`.theme-light` e todas as CSS custom properties da palette do handoff (README linhas 50-66). Importar em `index.css`. `[DRD-01, DRD-04]`
- [◐] **T0.2** — Fontes Manrope + Space Grotesk + mono ativas + stacks `--font-ui`/`--font-display`/`--font-mono`, Plus Jakarta removido. **PENDENTE:** self-host (hoje via Google Fonts CDN, TODO marcado no `index.html`). `[DRD-01]`
- [◐] **T0.3** — `<Icon>` wrapper criado (`src/components/ui/icon.tsx`), Material Symbols Rounded ativo. **PENDENTE:** self-host da fonte de ícones (hoje via CDN). `[DRD-40, DRD-41]`
- [x] **T0.4** — Remover `THEMES`/`applyTheme` de `lib/themes.ts` e todo consumo de `azure`/`emerald`/`ruby`; ajustar `App.tsx` para aplicar `theme-dark`/`theme-light` no root via `useTheme`. `grep azure|emerald|ruby|applyTheme|THEMES` = 0 no `src`. `[DRD-02]`
- [x] **T0.5** — `usePublicConfig`: parar de consumir `theme` (brand); preservar `company_name`. Sinalizar ao backend que o campo `theme` só pode ser dropado após confirmar contrato. `[DRD-03]`
- [x] **T0.6** — Hooks `useTheme()` / `useLanguage()` com persistência localStorage (`zeep.theme`/`zeep.lang`) e API estável para futura migração a endpoint de prefs. `[DRD-01]`

**Checkpoint Fase 0**: `npx tsc -b` + `npm run build` limpos; app renderiza em dark e light alternando a classe root; zero referência a brand-theme.

---

## Fase 0.5 — Biblioteca de componentes (bloqueia Fase 2)

### Nível 1 — Primitivos

- [x] **T05.1** — Restilizar `button`, `input`, `label`, `separator`, `badge`, `switch` para tokens/shape novos. `[DRD-10]`
- [x] **T05.2** — Restilizar `table`, `dialog`, `tabs`, `select` para tokens novos, Radix mantido. `[DRD-10]`
- [x] **T05.3** — Novos primitivos: `drawer` (Radix Dialog lateral), `accordion` (Radix Accordion), `tooltip` (Radix Tooltip), `skeleton`. Adicionar `@radix-ui/react-accordion` + `@radix-ui/react-tooltip` a `package.json`. `[DRD-10]`

### Nível 2 — Padrões

- [x] **T05.4** — `PageHeader` (title/subtitle/actions/breadcrumb). `[DRD-11]`
- [x] **T05.5** — `DataTable<T>` (columns/rows/sort/pagination/rowActions + empty/loading/error embutidos), absorvendo `TableCard`. `[DRD-11, DRD-13]`
- [x] **T05.6** — `StatusPill`, `EmptyState`, `ErrorState`, `LoadingState`. `[DRD-11, DRD-13]`
- [x] **T05.7** — `ConfirmDialog` generalizando `DeleteConfirmDialog` (delete + logout, título/mensagem adaptáveis, flag `destructive`). Reapontar usos existentes de `DeleteConfirmDialog`. `[DRD-11]`
- [x] **T05.8** — `ProviderCard` (accordion card: name/status/badge/disabled/children), `SettingRow` (label/description/control). `[DRD-11]`
- [x] **T05.9** — `EnterpriseBadge` + `UpgradeModal`, `MaskedSecretField`, `FormDrawer`. `[DRD-11]`
- [x] **T05.10** — `RoleGate` (`allow: Role[]`, `fallback`), `useCurrentRole()` derivando da sessão `/me` com degradação para modelo 2-papéis. `[DRD-11]`

### Sandbox

- [x] **T05.11** — Rota `/dev/components` (gated, fora de produção) renderizando todos primitivos + padrões em dark e light. `[DRD-14]`

**Checkpoint Fase 0.5**: `/dev/components` mostra todos os componentes corretos nos dois temas; `ConfirmDialog` cobre os casos que `DeleteConfirmDialog` cobria; `tsc`/build limpos.

---

## Fase 1 — Backend RBAC 4 papéis (paralelo; pré-requisito do shell)

- [ ] **T1.1** — Implementar/coordenar `dashboard-global-roles` (spec própria): migração 2→4 papéis, `/me` expõe role de 4 valores. **Não** re-especificar aqui; esta task é o ponto de sincronização/dependência. `[DRD-20 dep]`

**Nota**: enforcement, migração e matriz de permissão são escopo de `.specs/features/dashboard-global-roles/`. Se essa spec não estiver pronta quando o shell for feito, T1.5.x degrada para modelo 2-papéis (ver `useCurrentRole`).

---

## Fase 1.5 — Shell + navegação

- [x] **T15.1** — `AppShell` (grid sidebar+conteúdo) + `Sidebar` nova, itens de nav envoltos em `RoleGate` (omite, não desabilita). `[DRD-20, DRD-21, DRD-24]`
- [x] **T15.2** — `SidebarFooter`: `ThemeToggle` + `LanguageToggle` + logout com `ConfirmDialog`. `[DRD-20]`
- [x] **T15.3** — Rota `/403` (acesso negado genérico) + guard de rota para navegação direta a tela bloqueada. `[DRD-22]`
- [x] **T15.4** — `MobileTabBar` (Apps/Data/Logs) + `MobileMoreSheet` (bottom-sheet com resto da nav + footer). Alternar shell por viewport. `[DRD-23]`
- [x] **T15.5** — Migrar `DashboardShell.tsx` legado para o novo `AppShell`, preservando leitura de role da sessão; remover switcher client-side de role da renderização de produção. `[DRD-24]`

**Checkpoint Fase 1.5**: cada papel omite os itens corretos; `/403` cobre URL bloqueada; mobile mostra tab bar + more sheet; toggles de tema/idioma/logout funcionam.

---

## Fase 2 — Telas carry-over (uma por PR, compondo Nível 2)

Ordem por risco/tráfego. Cada task: substituir markup por composição de nível 2 + tokens/ícones/fontes novos; preservar 100% de dados/API/ações; ícones da tela migram para `<Icon>` na mesma PR; strings en+pt-BR juntas; checks AGENTS §3 limpos. `[DRD-30..34, DRD-40..42]`

- [ ] **T2.1** — `AppsPage` (955 ln) — home de apps.
- [ ] **T2.2** — `AppDetailsPage` (924 ln) — tabs Database/Login/Storage/API/Members/Observability. Login/Storage → `ProviderCard` accordion.
- [ ] **T2.3** — `DataBrowserPage` (1385 ln, maior) — `DataTable`.
- [ ] **T2.4** — `LogsPage` (523 ln).
- [x] **T2.5** — `UsersPage` (575 ln) — tela de referência de `DataTable` + row-actions.
- [ ] **T2.6** — `AuditLogPage` (292 ln).
- [ ] **T2.7** — `BrandSettingsPage` (483 ln) — Settings, tabs Branding/Database/Auth/Storage; `SettingRow` + `ProviderCard`.
- [ ] **T2.8** — `SdkPage` (184 ln).
- [ ] **T2.9** — `ChangelogPage` (150 ln).
- [ ] **T2.10** — `LoginPage` (181 ln) + `OnboardingPage`/`GoogleSetupPage`/`AppOnboardingPage`.
- [ ] **T2.11** — `AppUsersPage` (449 ln) — re-skin (feature já existe hoje).
- [ ] **T2.12** — `FrontendAppsPage` (634 ln) + `GitHubIntegrationPage` (1050 ln) — re-skin visual; redesign multi-provider de Integrations é escopo de spec própria.

**Checkpoint Fase 2**: cada tela pixel-fiel ao protótipo, ações disparando as mesmas APIs de antes.

---

## Fase 3 — Encerramento

- [ ] **T3.1** — Remover `lucide-react` de `package.json` (só quando a última tela migrar). `[DRD-42]`
- [ ] **T3.2** — Auditoria final: `grep` por strings hardcoded, cores fora de token, markup de padrão duplicado inline. `[DRD-12]`
- [ ] **T3.3** — CHANGELOG `[Unreleased]` + README feature tables (+ 3 traduções, AGENTS §6) para o que for user-facing novo (toggle tema/idioma, mobile shell). `[DRD-30]`
- [ ] **T3.4** — Coordenar com features net-new que consomem nível 2 (licensing/observability/2FA) — confirmar que os componentes servem sem fork. `[DRD-11]`

---

## Requirement Coverage

| Requirement ID | Tasks | Status |
|---|---|---|
| DRD-01 | T0.1, T0.2, T0.6 | Pending |
| DRD-02 | T0.4 | Pending |
| DRD-03 | T0.5 | Pending |
| DRD-04 | T0.1 | Pending |
| DRD-10 | T05.1, T05.2, T05.3 | Pending |
| DRD-11 | T05.4-T05.10, T3.4 | Pending |
| DRD-12 | T3.2 | Pending |
| DRD-13 | T05.5, T05.6 | Pending |
| DRD-14 | T05.11 | Pending |
| DRD-20 | T15.1, T15.2 | Pending |
| DRD-21 | T15.1 | Pending |
| DRD-22 | T15.3 | Pending |
| DRD-23 | T15.4 | Pending |
| DRD-24 | T15.1, T15.5 | Pending |
| DRD-30 | T2.1-T2.12, T3.3 | Pending |
| DRD-31 | T2.1-T2.12 | Pending |
| DRD-32 | T2.1-T2.12 | Pending |
| DRD-33 | T2.1-T2.12 | Pending |
| DRD-34 | T2.1-T2.12 | Pending |
| DRD-40 | T0.3, T2.* | Pending |
| DRD-41 | T0.3 | Pending |
| DRD-42 | T3.1 | Pending |

**Coverage**: 22/22 requisitos mapeados a tasks.

---

## Dependências externas (sinalizar ao time)

- **Backend RBAC 4 papéis** (`dashboard-global-roles`) — pré-requisito do shell nascer role-aware. Sem ele, shell degrada para 2 papéis.
- **Endpoint de prefs tema/idioma** (`PATCH /me/prefs`, `theme_pref`/`lang_pref` em `/me`) — sem ele, fallback localStorage (não cross-device).
- **Confirmação de contrato do campo `theme`** (brand) em `usePublicConfig` antes de dropar no backend.
- **Features net-new** (2FA, licensing, observability, integrações multi-provider, Create-with-AI) — specs próprias; consomem nível 2 daqui.
