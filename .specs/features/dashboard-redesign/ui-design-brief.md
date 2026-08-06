# Dashboard Redesign — Brief pra Design de Telas

Contexto: zeep-orbit é um BaaS self-hosted (dashboard React embutido no binário Go). Esta feature substitui a camada visual **inteira** do dashboard pelo redesign high-fidelity do handoff (`handoff/README.md` + `handoff/Zeep Orbit Redesign.dc.html` — protótipo clicável, fonte única de verdade). Ref técnica: `.specs/features/dashboard-redesign/{spec,design,tasks}.md`.

**Fidelidade: high.** Cores, tipo, espaçamento e estados de componente são finais. Copy é final (inglês). Recriar pixel-fiel na stack existente (React/Tailwind/Radix) — **não** portar o markup/runtime do protótipo.

---

## Princípio central: compor, não copiar

Regra não-negociável: nenhum padrão visual (tabela, diálogo, estado vazio/erro/loading, provider card, badge de status, secret mascarado) pode ser reimplementado inline numa tela. Tudo vem da biblioteca de nível 2 (`components/patterns/*`). Se uma tela precisa de tabela, ela usa `DataTable`; se precisa de confirmação, usa `ConfirmDialog`. PR que reintroduz markup inline de um padrão já existente é rejeitado no review. Este é o problema que o código tem hoje (~9.5k linhas de páginas com padrões duplicados) e o motivo desta feature.

---

## Design tokens (do handoff)

- **Tema**: dark + light. Toggle no footer da sidebar, persistido por-usuário. Definir tokens em `.theme-dark`/`.theme-light` (valores exatos: `handoff/README.md` linhas 50-66). **Brand-themes antigos (azure/emerald/ruby) saem.**
- **Tipografia**: Manrope (UI/body), Space Grotesk (títulos/display), mono (código/keys/IDs). UI 12.5-14px; títulos de página 22-26px.
- **Ícones**: Material Symbols Rounded (`<Icon name>`), 15-20px, `currentColor`. Migração incremental por tela (lucide sai por último).
- **Shape**: cards/painéis 12-16px radius; pills/badges/toggles full-round; icon-buttons ~8px.

---

## Telas

### Shell (novo)
- Sidebar nova + footer com **toggle de tema**, **toggle de idioma EN/PT** (troca i18next, sem mudar rota), **logout** (com `ConfirmDialog`).
- Nav **omite** itens que a role não acessa (não desabilita — mesmo princípio de `dashboard-global-roles`).
- **Mobile shell** é app de verdade: bottom tab bar (Apps/Data/Logs) + botão "More" abrindo bottom-sheet com o resto da nav e o footer. Não é reflow da sidebar desktop.
- Rota `/403` genérica para navegação direta a tela bloqueada.

### Telas carry-over (só nova pele, dados/lógica intactos)
Apps home · App details (Database/Login/Storage/API/Members/Observability) · Data Browser · Logs · Users (admins do dashboard) · Audit · Settings (Branding/Database/Auth/Storage) · SDKs · Changelog · Login/Onboarding.

- Login/Storage providers viram **accordion-of-cards** (`ProviderCard`) — um card por provider, expande pra configurar. Providers futuros mostram badge "SOON" + estado disabled.
- Todos os estados (vazio/erro/loading) vêm de `EmptyState`/`ErrorState`/`LoadingState`, nunca ad-hoc.

---

## Interações específicas

- **Toggle de idioma**: `i18n.changeLanguage()` + persist. Nenhuma rota muda. Todo texto por `react-i18next`, en+pt-BR desde o dia 1.
- **Toggle de tema**: aplica classe root, persiste por-usuário (localStorage como fallback enquanto backend não tem endpoint).
- **Logout / delete**: `ConfirmDialog` genérico, título/mensagem adaptáveis ao que está sendo confirmado.
- **Erro de mutação**: sempre `toast.error(error.message)` (sonner), nunca silencioso.
- **Modo leitura** (admin/auditor abrindo app de terceiro): banner claro "modo leitura" — detalhe em `dashboard-global-roles`, mas o componente de banner é compartilhado.

---

## Coisas que o design PRECISA respeitar

- **Compor, não copiar** — repetido de propósito, é o pilar da feature.
- **i18n obrigatório** — en+pt-BR no mesmo commit, zero string hardcoded.
- **Omitir, não desabilitar** — itens de nav fora da role somem, não ficam cinza.
- **Pixel-fiel ao protótipo** — cores/tipo/espaçamento finais, não improvisar.
- **Self-host de fontes e ícones** — sem CDN (CSP/offline, binário embutido).
- **Erro sempre visível** — toast, nunca engolido.

---

## Fora de escopo deste brief

- Telas das features net-new (2FA, License, Observability, Integrações multi-provider, Create-with-AI, App-users, RBAC) — cada uma tem spec/brief próprios. Aqui só se entrega os **componentes** que elas consomem (`ProviderCard`, `EnterpriseBadge`, `MaskedSecretField`, `FormDrawer`, `StatusPill`, `ConfirmDialog`, `RoleGate`).
- Backend RBAC de 4 papéis — `dashboard-global-roles`.
- Endpoint de persistência de preferência — gap de backend a coordenar.
