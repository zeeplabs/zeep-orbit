# Dashboard Redesign Specification

## Problem Statement

O dashboard admin do Zeep Orbit (React/Vite/TS embutido no binário Go) tem hoje uma camada visual antiga: tema **dark-only**, cores derivadas de um sistema de "brand-theme" (`azure`/`emerald`/`ruby` em `lib/themes.ts`), fonte Plus Jakarta Sans, ícones `lucide-react`, e ~9.5k linhas de páginas onde padrões visuais (tabelas, diálogos, estados vazio/erro/loading, cards de provider, badges de status) estão **replicados inline em cada tela**, não extraídos em componentes compartilhados.

O handoff de design (`handoff/README.md` + `handoff/Zeep Orbit Redesign.dc.html`) entrega um novo sistema visual high-fidelity, final: palette fixa dark+light, tipografia Manrope/Space Grotesk, ícones Material Symbols Rounded, e novos padrões de card/table/drawer/modal. É um **redesign da app existente**, não um rewrite — a mesma stack (React 18, Tailwind v4, Radix, TanStack Query, react-router 7, i18next, sonner) e a mesma lógica de negócio permanecem; só a camada de apresentação e alguns padrões de interação mudam.

Esta spec cobre **exclusivamente** dois eixos:

1. **Fundação visual + sistema de componentes compartilhados** — tokens dark/light, fontes, ícones, e uma biblioteca de componentes em 3 níveis para que cada tela **componha** em vez de **copiar** markup.
2. **Migração das telas carry-over** (Apps, App details, Data Browser, Logs, Users, Audit, Settings, SDKs, Changelog, Login) para o novo sistema, sem mudar dados/lógica de negócio.

As **features net-new** do handoff (2FA, licensing enterprise, observability, roles globais 4-níveis, per-app 2FA, integrações multi-provider, Create-with-AI, app-users management, mobile shell) **já têm specs próprias** em `.specs/features/` ou serão especificadas separadamente. Esta spec as trata como **consumidoras** do sistema de componentes aqui definido e como **dependências de sequenciamento**, não as re-especifica.

## Goals

- [ ] Sistema de design tokens dark+light baseado na palette do handoff (`handoff/README.md`, linhas 50-66), aplicado via classe `theme-dark`/`theme-light` no root, substituindo o sistema brand-theme atual.
- [ ] Sistema brand-theme (`THEMES`/`applyTheme` em `lib/themes.ts`, `azure`/`emerald`/`ruby`) **removido** — decisão explícita do usuário.
- [ ] Toggle dark/light no footer da sidebar, persistido por-usuário (não só sessão).
- [ ] Toggle de idioma (EN/PT) no footer da sidebar usando o **modelo i18next atual** (`i18n.changeLanguage()` + persistência), **sem** slug de idioma na URL — decisão explícita do usuário.
- [ ] Fontes Manrope (UI) + Space Grotesk (títulos) + monospace, self-hosted (CSP/offline, não CDN).
- [ ] Migração de ícones `lucide-react` → Material Symbols Rounded via wrapper `<Icon>`, feita de forma **incremental por tela** — decisão explícita do usuário.
- [ ] Biblioteca de componentes em 3 níveis (primitivos → padrões → telas): nenhum padrão visual (tabela, diálogo, estado, provider card, badge, secret mascarado) reimplementado inline em página.
- [ ] Shell/navegação reescritos: sidebar nova, footer, mobile shell (bottom tab bar + more sheet), consciente de role (omite itens sem acesso, não desabilita).
- [ ] Rota `/403` genérica para navegação direta a tela bloqueada.
- [ ] Todas as telas carry-over migradas para o novo sistema, pixel-fiel ao protótipo, com dados/lógica intactos.
- [ ] Zero string hardcoded: todo texto por `react-i18next`, en+pt-BR no mesmo commit.

## Out of Scope

| Item | Motivo |
|---|---|
| Implementação de 2FA, licensing, observability, roles 4-níveis, per-app 2FA, integrações multi-provider, Create-with-AI, app-users management | Cada uma tem spec própria (`two-factor-auth`, `enterprise-licensing`, `observability-integrations`, `dashboard-global-roles`, `rbac-per-app`, etc). Esta spec entrega os **componentes** que elas consomem, não as features. |
| Mudança de dados/lógica de negócio das telas carry-over | Redesign é camada de apresentação. Qualquer mudança de comportamento é escopo de outra spec. |
| Backend RBAC de 4 papéis | Escopo de `dashboard-global-roles`. Esta spec **depende** do papel vir da sessão (`/me`), mas não implementa o enforcement. |
| Endpoint de persistência de preferência tema/idioma | Sinalizado como dependência de backend (ver Edge Cases). O contrato é definido aqui; a implementação server-side é trabalho de backend a coordenar. |
| Slug de idioma na URL | Decisão explícita: usar modelo i18next atual, sem rota por idioma. |
| Manter brand-themes coexistindo | Decisão explícita: brand-themes saem por completo. |
| Rewrite de stack (trocar React/Tailwind/etc) | Handoff é explícito: recriar o design na stack existente. |

---

## User Stories

### P1: Fundação de tokens dark/light + remoção de brand-themes ⭐ MVP

**User Story**: Como plataforma, quero um sistema de tokens dark/light único baseado na palette final do handoff, para que toda tela migrada nasça consistente e o sistema brand-theme legado seja removido sem deixar cor órfã.

**Why P1**: Bloqueia tudo. Sem tokens, cada tela migrada nasceria fora do padrão e teria que ser refeita.

**Acceptance Criteria**:

1. WHEN o app renderiza THEN o sistema SHALL aplicar `theme-dark` ou `theme-light` no elemento root, com todas as CSS custom properties da palette do handoff definidas em cada classe.
2. WHEN o código legado é migrado THEN o sistema SHALL remover `THEMES`, `applyTheme`, e todo consumo de `azure`/`emerald`/`ruby` de `lib/themes.ts`, sem referência pendente que quebre o build.
3. WHEN o endpoint público de config (`usePublicConfig`) é ajustado THEN o campo `theme` (brand-theme) SHALL ser removido do contrato **somente após** confirmar que nenhum cliente depende dele; enquanto não confirmado, o campo é ignorado no frontend, não dropado no backend.
4. WHEN um token semântico é necessário (`--surface`, `--border`, `--text-primary`, `--primary`, `--accent`, `--success/warning/danger` + tints, overlay, shadows) THEN ele SHALL existir em ambos os temas com os valores exatos do handoff.

**Independent Test**: Alternar `theme-dark`/`theme-light` no root e confirmar, via inspeção, que backgrounds/borders/text/primary/accent batem com os hex do handoff; `grep` por `azure|emerald|ruby|applyTheme|THEMES` retorna zero ocorrências no `src` após a migração da fundação.

---

### P1: Sistema de componentes compartilhados em 3 níveis ⭐ MVP

**User Story**: Como time de frontend, quero uma biblioteca de componentes em 3 níveis (primitivos → padrões → telas) para que cada tela componha blocos prontos em vez de replicar tabela/diálogo/estados/cards em cada arquivo.

**Why P1**: É o pedido central do usuário. Sem isso, o redesign vira 12 páginas de markup duplicado, exatamente o problema que existe hoje.

**Acceptance Criteria**:

1. WHEN um primitivo de UI é usado (button, input, table, dialog, badge, switch, tabs, select, drawer, accordion, tooltip, skeleton) THEN ele SHALL vir de `components/ui/*`, restilizado para os tokens novos, com Radix mantido onde já existe.
2. WHEN um padrão compartilhado é necessário (`PageHeader`, `DataTable`, `StatusPill`, `EmptyState`, `ErrorState`, `LoadingState`, `ConfirmDialog`, `ProviderCard`, `SettingRow`, `EnterpriseBadge`/`UpgradeModal`, `MaskedSecretField`, `FormDrawer`, `RoleGate`) THEN ele SHALL existir em `components/patterns/*` e ser a **única** implementação daquele padrão.
3. WHEN uma tela precisa de tabela, diálogo de confirmação, ou estado vazio/erro/loading THEN ela SHALL compor o componente de nível 2 correspondente, e NÃO reimplementar o markup inline.
4. WHEN a biblioteca é revisada THEN SHALL existir uma página de sandbox (`/dev/components`, gated) que renderiza os primitivos e padrões isoladamente em ambos os temas.
5. WHEN um PR de migração de tela reintroduz um padrão inline que já existe em nível 2 THEN ele SHALL ser rejeitado no review.

**Independent Test**: Abrir `/dev/components` em dark e light; confirmar que cada primitivo e cada padrão renderiza corretamente nos dois temas. Migrar uma tela de referência (Users) e confirmar, por diff, que ela não contém markup de tabela/diálogo/estado próprio — só composição.

---

### P1: Shell, navegação role-aware e mobile shell ⭐ MVP

**User Story**: Como usuário do dashboard, quero a nova sidebar (com footer de tema/idioma/logout), a navegação que omite itens sem acesso, e uma experiência mobile de app real, para navegar o dashboard redesenhado em qualquer dispositivo.

**Why P1**: Todas as telas vivem dentro do shell; ele precisa existir antes das telas migradas.

**Acceptance Criteria**:

1. WHEN o shell renderiza THEN o sistema SHALL exibir a nova sidebar com footer contendo toggle de tema, toggle de idioma, e ícone de logout (com `ConfirmDialog` de logout).
2. WHEN o papel do usuário logado não tem acesso a um item de nav THEN o item SHALL ser **omitido** da renderização, não desabilitado (mesmo princípio de `dashboard-global-roles`).
3. WHEN o usuário navega direto (URL) para uma tela sem permissão THEN o sistema SHALL renderizar a rota `/403` (acesso negado genérico), não crash nem tela branca.
4. WHEN a app é aberta em viewport mobile THEN o sistema SHALL usar o mobile shell (bottom tab bar Apps/Data/Logs + botão "More" abrindo bottom-sheet com o resto da nav e o footer user/tema/logout), não um reflow responsivo da sidebar desktop.
5. WHEN o papel do usuário vem da sessão (`/me`) THEN a visibilidade de nav SHALL derivar dele, nunca de um switcher client-side (o "Viewing as" do protótipo é aid de review, não feature).

**Independent Test**: Com `/me` retornando cada um dos papéis, confirmar que a sidebar omite os itens corretos; navegar direto a uma URL bloqueada e confirmar `/403`; reduzir viewport e confirmar bottom tab bar + more sheet.

---

### P1: Migração das telas carry-over ⭐ MVP

**User Story**: Como usuário, quero todas as telas existentes (Apps, App details, Data Browser, Logs, Users, Audit, Settings, SDKs, Changelog, Login) no novo visual, sem perder nenhum dado ou ação que já existe, para usar o dashboard redesenhado com a mesma funcionalidade de hoje.

**Why P1**: É o corpo do redesign. Sem migrar as telas, a fundação não entrega valor ao usuário.

**Acceptance Criteria**:

1. WHEN uma tela carry-over é migrada THEN ela SHALL manter 100% dos dados, chamadas de API, e ações que já tinha, mudando apenas a apresentação.
2. WHEN uma tela é migrada THEN ela SHALL compor componentes de nível 2 e usar os tokens/ícones/fontes novos, sem markup de padrão duplicado.
3. WHEN uma tela é migrada THEN toda string nova/alterada SHALL estar em `en.json` e `pt-BR.json` no mesmo commit.
4. WHEN uma mutação de uma tela migrada falha THEN a tela SHALL exibir `toast.error(error.message)` (sonner), sem regressão de tratamento de erro.
5. WHEN cada tela é considerada pronta THEN `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt`, `npx tsc -b`, `npm run build` SHALL passar limpos (regra AGENTS.md §3).

**Independent Test**: Para cada tela migrada, comparar lado-a-lado com o protótipo (fidelidade) e exercer cada ação da tela confirmando que dispara a mesma API de antes (funcionalidade preservada).

---

### P2: Migração incremental de ícones lucide → Material Symbols

**User Story**: Como time, quero migrar ícones de `lucide-react` para Material Symbols Rounded de forma incremental por tela, para não fazer um big-bang arriscado e permitir revisar cada tela isolada.

**Why P2**: Suporta P1 mas não bloqueia — telas podem migrar visual antes de trocar todos os ícones, desde que o wrapper exista.

**Acceptance Criteria**:

1. WHEN uma tela é migrada visualmente THEN os ícones daquela tela SHALL passar a usar o wrapper `<Icon name>` (Material Symbols Rounded), na mesma PR.
2. WHEN o wrapper `<Icon>` é usado THEN ele SHALL renderizar `<span class="material-symbols-rounded">` com cor via `currentColor`, tamanho 15-20px.
3. WHEN todas as telas estiverem migradas THEN `lucide-react` SHALL ser removido de `package.json`; enquanto houver tela pendente, a dependência permanece.

**Independent Test**: `grep` por `lucide-react` mostra apenas telas ainda não migradas; wrapper `<Icon>` renderiza corretamente com `currentColor` em ambos os temas.

---

## Edge Cases

- WHEN o backend ainda não expõe endpoint de persistência de preferência tema/idioma THEN o frontend SHALL persistir localmente (localStorage) como fallback e o contrato de persistência por-usuário SHALL ser sinalizado ao time de backend como gap — nunca silenciosamente esquecido.
- WHEN o papel na sessão (`/me`) ainda for o modelo antigo de 2 papéis (`admin`/`superadmin`) porque `dashboard-global-roles` não foi implementado THEN o shell SHALL degradar para o mapeamento antigo (tratar `admin` como o papel mais permissivo não-super), sem crash, até os 4 papéis existirem.
- WHEN uma tela carry-over depende de um campo de config (`theme`, `company_name`) que estava atrelado ao brand-theme THEN a remoção do brand-theme SHALL preservar `company_name` (branding textual) e só remover a seleção de paleta por brand.
- WHEN o toggle de idioma troca `lng` THEN nenhuma rota SHALL mudar (sem slug de idioma), e a preferência SHALL persistir para a próxima sessão.
- WHEN um componente de nível 2 é usado por uma feature net-new (ex: `MaskedSecretField` em observability, `EnterpriseBadge` em licensing) THEN o componente SHALL ser genérico o suficiente para servir essa feature sem fork — dependências cruzadas documentadas, não duplicadas.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| DRD-01 | P1: Fundação tokens | Design | Pending |
| DRD-02 | P1: Fundação tokens | Design | Pending |
| DRD-03 | P1: Fundação tokens | Design | Pending |
| DRD-04 | P1: Fundação tokens | Design | Pending |
| DRD-10 | P1: Componentes 3 níveis | Design | Pending |
| DRD-11 | P1: Componentes 3 níveis | Design | Pending |
| DRD-12 | P1: Componentes 3 níveis | Design | Pending |
| DRD-13 | P1: Componentes 3 níveis | Design | Pending |
| DRD-14 | P1: Componentes 3 níveis | Design | Pending |
| DRD-20 | P1: Shell + nav role-aware | Design | Pending |
| DRD-21 | P1: Shell + nav role-aware | Design | Pending |
| DRD-22 | P1: Shell + nav role-aware | Design | Pending |
| DRD-23 | P1: Shell + nav role-aware | Design | Pending |
| DRD-24 | P1: Shell + nav role-aware | Design | Pending |
| DRD-30 | P1: Telas carry-over | Design | Pending |
| DRD-31 | P1: Telas carry-over | Design | Pending |
| DRD-32 | P1: Telas carry-over | Design | Pending |
| DRD-33 | P1: Telas carry-over | Design | Pending |
| DRD-34 | P1: Telas carry-over | Design | Pending |
| DRD-40 | P2: Ícones incremental | Design | Pending |
| DRD-41 | P2: Ícones incremental | Design | Pending |
| DRD-42 | P2: Ícones incremental | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 22 total, 0 mapeados a tasks, 22 não mapeados ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Todas as telas carry-over renderizam no novo visual, pixel-fiéis ao protótipo, com dados/lógica preservados.
- [ ] Zero markup de padrão (tabela/diálogo/estado/provider card/badge/secret) duplicado inline em página — tudo compõe nível 2.
- [ ] Sistema brand-theme removido sem cor órfã; `grep azure|emerald|ruby|applyTheme` zerado no `src`.
- [ ] Dark/light e idioma persistem por-usuário (ou localStorage como fallback documentado enquanto backend não existe).
- [ ] Sidebar omite (não desabilita) itens fora da role; `/403` cobre navegação direta bloqueada.
- [ ] Mobile shell é experiência de app (tab bar + more sheet), não reflow.
- [ ] Ícones migrados incrementalmente; `lucide-react` removido só quando a última tela migrar.
- [ ] Componentes de nível 2 servem as features net-new (licensing/observability/2FA) sem fork.
- [ ] Todo texto por i18n, en+pt-BR; build/test/vet/gofmt/tsc/vite limpos por PR.
