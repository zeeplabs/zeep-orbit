# Dashboard Settings Consolidation Tasks

**Spec**: `.specs/features/dashboard-settings-consolidation/spec.md`
**Design**: _pendente — escrever após brainstorm fechar as Open Questions da spec._

Legenda status: ☐ pending · ◐ in progress · ☑ done
Origem: T4.5 do `dashboard-redesign` (Fase 4 — fidelidade ao handoff). Cada task de tela só fecha com AGENTS §3 limpo (build/test/vet/gofmt/tsc/vite) + i18n en+pt-BR no mesmo commit.

---

## Fase 0 — Decisão (bloqueia todo o resto)

- [ ] **S0.1** — Brainstorm de produto/infra fechando as Open Questions da spec: defaults (retention/timeout/pool/CSV rows), se schema-change-approval entra aqui ou vira spec própria, se o release pode sair com tabs parciais, risco do pool cap. Saída: `design.md`.

## Fase 1 — Entregável sem backend novo (mergeável já)

- [ ] **S1.1** — Renomear o título da página para `Settings` (handoff) mantendo a tab "Branding". Ajustar `PageHeader` do `BrandSettingsPage` (novo `settings.pageTitle`/`settings.pageSubtitle` en+pt) sem tocar no conteúdo das tabs existentes.
- [ ] **S1.2** — Auditar as tabs que já existem (Branding/Auth Google/Storage) contra o inline-spec do handoff (casing/copy/spacing/ordem de campos) e alinhar o que for só apresentação.

## Fase 2 — Database tab: controles net-new (cada um depende de backend real)

> Regra dura: cada controle só entra na UI quando o backend correspondente persiste **e** faz efeito. Sem backend = não renderiza.

- [ ] **S2.1** — **Retention period + purga**: config em `system_config_store` + rotina de purga de soft-deleted após N dias. UI só depois do job existir. Default: ver S0.1.
- [ ] **S2.2** — **Require RLS by default**: default global lido em `CreateAppTable` (nova tabela nasce Restricted). Config + enforcement + UI.
- [ ] **S2.3** — **Max rows per CSV export**: trocar o cap fixo `10000` do `DataBrowserExport` (handler.go) por leitura da config. Amarra com T4.4 do `dashboard-redesign`. Config + wire + UI.
- [ ] **S2.4** — **Statement timeout**: config + enforcement real (`SET statement_timeout` por conexão/consulta das queries de app). Validar que aplica, não só persiste. UI.
- [ ] **S2.5** — **Max connections per app (pool cap)**: config + cap no pool Postgres por app (`registry`/`db`). **Higher-risk (runtime multi-app)** — validar infra (S0.1 Q4) antes. UI só após enforcement seguro.
- [ ] **S2.6** — **Require schema-change approval**: SOMENTE se S0.1 decidir que entra aqui. Caso contrário, abrir `.specs/features/schema-change-approval/` e deixar o toggle desligado/oculto até lá.

## Fase 3 — Slots cross-spec

- [ ] **S3.1** — **Require 2FA for all admins**: toggle que liga a policy quando `two-factor-auth` entregar o backend. Coordenar contrato com aquela spec; até lá, oculto ou desabilitado com nota.
- [ ] **S3.2** — **Tab License**: slot da 5ª tab lendo o estado de licença que `enterprise-licensing` expõe. Sem reimplementar licensing aqui.

## Fase 4 — Fechamento

- [ ] **S4.1** — CHANGELOG `[Unreleased]` + (se user-facing novo) README + 3 traduções (AGENTS §6). Novos env/config na tabela de configuração do README (AGENTS §8).
- [ ] **S4.2** — Validação end-to-end de cada controle no ar: persiste, aplica efeito, e o que não tem backend não aparece.

---

## Dependências externas

- `two-factor-auth` — backend do "Require 2FA" (S3.1).
- `enterprise-licensing` — estado de licença para a tab License (S3.2).
- Decisão de infra sobre pool cap por app (S2.5) e defaults de limites (S0.1).
- `dashboard-redesign` T4.4 — o cap de CSV export vira configurável em S2.3.
