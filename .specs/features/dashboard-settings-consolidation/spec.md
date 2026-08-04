# Dashboard Settings Consolidation Specification

> **Status**: draft / scoping. `design.md` intencionalmente pendente — depende de um brainstorm de produto (defaults, ownership de backend, e se o approval-workflow entra nesta feature ou vira spec própria). Ver "Open Questions".

## Problem Statement

O handoff do redesign (`handoff/Zeep Orbit Redesign.dc.html`, região Settings ~linha 1683) desenha a tela **Settings** como uma página única titulada `Settings` / "Customize how the dashboard looks and behaves for your company.", com 5 tabs: **Branding · Database · Auth provider · Storage · License**, e um conteúdo bem mais rico que a tela atual.

A tela atual (`BrandSettingsPage`, ~483 ln, entregue em T2.7 do `dashboard-redesign`) já tem **4 das 5 tabs** (Branding / Database / Auth provider / Storage) com o shell visual do redesign, mas:

- o título da página ainda é `Branding` (`brand.title`), não `Settings`;
- a tab **Database** só expõe soft-delete — falta todo o resto do handoff (retention/purge, schema safety, limits);
- a tab **Auth provider** não tem o toggle "Require 2FA for all admins";
- **não existe** a tab **License**.

Vários desses controles **não têm backend** hoje. Recriar a UI "pixel-exata" ao handoff sem o backend de cada controle produziria UI morta/enganosa (switch que não persiste, campo que não faz nada). Esta spec existe para mapear cada controle ao seu backend/spec antes de qualquer código, e sequenciar o que é entregável já vs o que depende de feature net-new.

Meta de aceitação: a página Settings do release final **funciona exatamente como o handoff desenha** — cada controle visível persiste e tem efeito real, ou não é mostrado.

## Goals

- [ ] Página Settings titulada conforme handoff (`Settings` / subtítulo), 5 tabs na ordem do handoff.
- [ ] Cada controle do handoff mapeado a: (a) backend existente, (b) backend net-new nesta spec, ou (c) cross-spec (feature própria).
- [ ] Nenhum controle renderizado sem persistência real (regra AGENTS: não scaffoldar UI para backend inexistente).
- [ ] Config global persistida via a superfície existente (`internal/dashboard/system_config_store.go`), estendida — não uma tabela ad-hoc nova por controle sem necessidade.
- [ ] i18n en + pt-BR para todo texto novo, no mesmo change.
- [ ] Enforcement real dos limites que afetam runtime (statement timeout, pool cap, max CSV rows) validado, não só persistido.

## Out of Scope

| Item | Motivo |
|---|---|
| Tab **License** (implementação) | Pertence à spec `enterprise-licensing`. Aqui só o slot/entry-point da tab e o contrato de leitura do estado de licença. |
| **Require 2FA for all admins** (mecanismo) | Pertence à spec `two-factor-auth`. Aqui só o toggle que liga a policy quando o backend de 2FA existir. |
| **Require schema-change approval** (workflow completo) | Approval-gate multi-admin é feature grande (fila de mudanças pendentes, segundo aprovador, auditoria). Provável spec própria — decidir no brainstorm (Open Questions). |
| Migração de dados de settings legados | Não há settings global legado além do `system_config_store` atual; nada a migrar. |

## Control Inventory (handoff × estado atual)

Legenda: **EXISTE** (backend pronto, só UI) · **NET-NEW** (backend novo nesta spec) · **CROSS** (outra spec).

### Tab Branding — EXISTE
- Company name, Theme, Login logo (upload), App icon (upload), Save. Já implementado no `BrandSettingsPage`. Gap único: título da página (`brand.title` "Branding" → page title "Settings"; a tab continua "Branding").

### Tab Database
- **Soft delete** (toggle) — **EXISTE** (`system_config_store` `SoftDeleteEnabled`; enforcement em `query.BuildList` etc).
- **Retention period** (dias) + purga de soft-deleted — **NET-NEW**. Precisa: campo de config + job/rotina de purga. Sem job, o campo é decorativo. Decidir cadência (Open Questions).
- **Require RLS by default** ("novas tabelas nascem Restricted") — **NET-NEW**. Default global lido no fluxo de criação de tabela (`CreateAppTable`). Per-table Restricted já existe; falta o default global.
- **Require schema-change approval** `NEW` — **NET-NEW / possivelmente CROSS**. Workflow de aprovação; grande. Ver Out of Scope + Open Questions.
- **Max rows per CSV export** — **NET-NEW**. Backend do export já existe (`DataBrowserExport`, cap fixo 10 000). Este controle troca o cap fixo por config. Acoplado ao T4.4 do `dashboard-redesign`.
- **Statement timeout** (ms) — **NET-NEW**. Aplicar por conexão/consulta (`SET statement_timeout`). Enforcement real, não só persistir.
- **Max connections per app** (pool cap) — **NET-NEW**. Limita o pool Postgres por app. Toca a criação de pool no `registry`/`db`. Higher-risk (afeta runtime multi-app).

### Tab Auth provider
- **Google provider** (active/soon, Client ID / Client secret / Allowed domains) — **EXISTE** (`settings.tabAuth`, config Google global).
- **Require 2FA for all admins** (toggle) — **CROSS** (`two-factor-auth`).

### Tab Storage — EXISTE
- Global storage providers (Endpoint / Region / Bucket prefix / Access key / Secret key) — **EXISTE** (`settings.globalStorage`).

### Tab License — CROSS
- License key paste, Add/replace, Save license, Enterprise badge, About licensing — **CROSS** (`enterprise-licensing`). Aqui só o slot da tab.

## Open Questions (resolver no brainstorm antes do design.md)

1. **Defaults**: valores default de retention (dias), statement timeout (ms), max connections per app, max CSV rows? Precisam de decisão de produto/infra — não invento.
2. **Schema-change approval**: entra nesta spec ou vira `.specs/features/schema-change-approval/`? É o item mais pesado (workflow, fila, segundo aprovador). Recomendação: spec própria; aqui só reservar o toggle desligado até ela existir.
3. **Ordem de entrega**: tabs prontas (Branding título + Storage + Auth Google) podem mergear já; Database rica e License dependem de backend/cross-spec. Confirmar se o release "handoff-exact" pode sair com tabs parciais (mostrando só o que persiste) ou espera tudo.
4. **Pool cap por app**: risco de runtime (starvation entre apps). Validar com quem cuida de infra antes de expor como toggle self-service.
5. **`Max connections per app` vs rate-limit existente**: já existe rate-limit por app (req/min). O pool cap é eixo diferente (conexões concorrentes). Confirmar que são controles distintos e ambos desejados.

## Acceptance / Checkpoint

- Nenhuma tab/controle no ar sem backend real por trás.
- `design.md` escrito só após o brainstorm fechar as Open Questions.
- `tasks.md` re-sequenciado conforme as decisões (o skeleton atual assume approval-workflow fora).
