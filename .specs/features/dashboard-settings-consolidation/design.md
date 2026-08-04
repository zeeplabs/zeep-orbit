# Dashboard Settings Consolidation — Design

**Spec**: `.specs/features/dashboard-settings-consolidation/spec.md`
**Origem**: T4.5 do `dashboard-redesign` (Fase 4 — fidelidade ao handoff).
**Status**: aprovado no brainstorm 2026-08-04. Decisões abaixo fecham as Open Questions da spec.

## Decisões (brainstorm)

1. **schema-change-approval** → **spec própria** (`.specs/features/schema-change-approval/`, a abrir). Aqui só um toggle reservado, renderizado disabled+SOON. É o item mais pesado e independente; não bloqueia o resto.
2. **Gating de release**: **incremental**. Cada controle entra quando seu backend fica pronto; releases saem no caminho mostrando só o que persiste. "Handoff-exact" é a meta final, não um gate único. Settings não segura release.
3. **Max connections per app (pool cap)**: **adiado**. Renderizado oculto até haver enforcement seguro + validação de infra (risco de starvation entre apps). Fora do primeiro corte.
4. **Defaults preservam comportamento atual**, exceto statement timeout:
   - `max_csv_export_rows` = **10000** (cap fixo de hoje)
   - `statement_timeout_ms` = **30000** (30s) — **default de segurança que MUDA comportamento** (queries longas passam a cortar em 30s). Exige callout no CHANGELOG.
   - `retention_days` / purga = **off** (purga é destrutiva; opt-in explícito)
   - `require_rls_default` = **off** (tabela nova nasce Public salvo escolha)
5. **Slots cross-spec** (toggle "Require 2FA", tab "License") → **disabled + SOON**, padrão D-DT08 (Create-with-AI). O handoff marca itens como SOON, então é fiel.

## Overview

Recriar a tela **Settings** do handoff (`handoff/Zeep Orbit Redesign.dc.html` ~linha 1683): página única titulada `Settings`, 5 tabs (**Branding · Database · Auth provider · Storage · License**). O `BrandSettingsPage` atual já tem 4 dessas tabs com o shell do redesign; esta feature retitula a página, enriquece a tab **Database** com controles de config global net-new, e adiciona slots (disabled+SOON) para 2FA e License.

Princípio de fidelidade: **nenhum controle no ar sem persistência real e efeito**. Controles sem backend ou renderizam disabled+SOON (quando o handoff os mostra como roadmap) ou ficam ocultos (pool cap).

## Escopo deste corte

| Tab / controle | Estado | Ação |
|---|---|---|
| Branding (company name, theme, logos) | existe | auditar contra handoff sob novo título |
| Storage global | existe | auditar |
| Auth provider — Google | existe | auditar |
| Auth provider — Require 2FA | cross-spec | slot disabled+SOON |
| Database — Soft delete | existe | manter |
| Database — Retention/purga | net-new | implementar (off default) |
| Database — Require RLS by default | net-new | implementar (off default) |
| Database — Max rows per CSV export | net-new | implementar (10000 default) |
| Database — Statement timeout | net-new | implementar (30s default, callout) |
| Database — Max connections per app | adiado | oculto |
| License (tab inteira) | cross-spec | tab disabled+SOON |
| Schema-change approval | spec própria | toggle reservado disabled+SOON |

## Backend

### Extensão do `SystemConfig`

`internal/dashboard/system_config_store.go` hoje: `SystemConfig{ SoftDeleteEnabled bool; StorageConfig }`, cacheado no registry via `reg.SystemConfig()`, lido em `query.BuildList`/`BuildDelete`. Estender — **não criar tabela nova por controle**:

```go
type SystemConfig struct {
    SoftDeleteEnabled  bool
    StorageConfig      *GlobalStorageConfig
    // net-new (este corte):
    RequireRLSDefault  bool  // default false
    StatementTimeoutMs int   // default 30000; 0 = desligado
    MaxCSVExportRows   int   // default 10000
    RetentionDays      int   // default 0 = purga desligada
    // net-new (adiados, colunas podem já nascer mas sem UI/enforcement):
    // MaxConnectionsPerApp int
}
```

- Migração idempotente na provisão (`ProvisionZeepSystem` / onde o schema de settings é criado): `ADD COLUMN IF NOT EXISTS` com os defaults acima. Backfill dos existentes com os defaults (statement_timeout 30000, max_csv 10000, resto 0/false).
- `GetSystemConfig` passa a escanear as colunas novas; `UpsertSystemConfig` ganha os campos novos (revisar assinatura — hoje recebe `softDeleteEnabled, storageConfig`; migrar para receber o struct inteiro para não crescer a lista de parâmetros a cada controle).
- Handler PATCH/PUT de settings: **merge-on-absent-key** (AGENTS §4) — form que limpa um campo envia o valor explícito (0/false), nunca omite.
- Erros: strings em inglês (AGENTS §4). 500 não vaza `err.Error()`.

### Enforcement por controle

- **`max_csv_export_rows`** — `DataBrowserExport` (handler.go:1748) troca `const exportLimit = 10000` por `h.reg.SystemConfig().MaxCSVExportRows` (fallback 10000 se 0/inválido). Header `X-Truncated` mantido.
- **`require_rls_default`** — `CreateAppTable` (handler.go): quando a criação não especifica acesso, default = Restricted em vez de Public. **Edge**: Restricted exige email auth (handler.go:119). Regra: aplicar o default Restricted **apenas quando o app tem email auth**; sem auth, cair para Public (senão a criação quebraria). Documentar esse acoplamento.
- **`statement_timeout_ms`** — aplicar nas queries de dados de app (`internal/server/handler.go` `h.pool.Query(...)`). **Nuance**: `h.pool` é pgxpool compartilhado; `SET statement_timeout` numa conexão pooled vazaria para o próximo request. Enforcement correto: envolver a query de app numa transação curta com `SET LOCAL statement_timeout = <ms>` quando `> 0`, ou usar a config de conexão do pgx por-query. Escolha: helper que, quando `StatementTimeoutMs > 0`, roda a query dentro de tx com `SET LOCAL`. Aplicar no data-plane de app (server/handler.go), não no dashboard.
- **`retention_days` + purga** — rotina background (goroutine com ticker no boot do server, ou reaproveitar scheduler existente se houver) que, quando `RetentionDays > 0` **e** soft-delete ligado, apaga em hard-delete linhas com `deleted_at < now() - RetentionDays` por schema de app (`schemaNameForDB`). Off por default. **Destrutivo**: cada execução audit-logged; ao ligar na UI, confirm dialog explicando que é irreversível.

### Migração / risco

- Colunas novas com `ADD COLUMN IF NOT EXISTS` + backfill — idempotente, roda no boot. Baixo risco (schema de settings, tabela pequena).
- Statement timeout 30s default **muda comportamento** de instalações existentes → CHANGELOG explícito + entry na tabela de config do README (AGENTS §8) se virar env/config documentável.

## Frontend

`BrandSettingsPage` (o Settings atual):

- **Título** → `t('settings.pageTitle')` = "Settings" / subtítulo do handoff (keys novas en+pt). Tab "Branding" continua "Branding".
- **Tab Database** — adicionar seções do handoff via `SettingRow` + `Switch` + inputs numéricos:
  - Data retention: soft delete (existe) + retention period (dias, input) + hint de purga.
  - Schema safety: Require RLS by default (switch). (approval → toggle disabled+SOON.)
  - Limits: Max rows per CSV export (input), Statement timeout (ms, input). (Max connections per app → **não renderiza**.)
- **Tab Auth provider** — Require 2FA: `Switch` disabled + badge SOON + tooltip (padrão D-DT08).
- **Tab License** — 5ª tab, conteúdo disabled+SOON (não reimplementar licensing).
- **Regra de render**: controle sem backend correspondente não aparece (pool cap) ou aparece disabled+SOON (2FA/License/approval). Só o que persiste é editável.
- Mutations: `onError` com `toast.error(error.message)` (AGENTS §5). Config lida via o hook de settings existente; sem fetch ad-hoc duplicado.
- i18n en+pt para todo texto novo, mesmo change.

## Sequência de entrega (incremental — cada item mergeável sozinho)

1. **S1** — Título "Settings" + auditoria das tabs existentes (Branding/Storage/Auth-Google) contra handoff. **Front puro.** Sai primeiro.
2. **S2.3** — Max rows per CSV export (backend trivial: config + trocar constante) + UI.
3. **S2.2** — Require RLS by default (config + enforcement em CreateAppTable, com o edge de email-auth) + UI.
4. **S2.4** — Statement timeout (config + helper SET LOCAL no data-plane; **CHANGELOG callout**) + UI.
5. **S2.1** — Retention/purga (config + job background + confirm dialog destrutivo + audit) + UI.
6. **Adiados/cross-spec**: pool cap (infra), 2FA (two-factor-auth), License (enterprise-licensing), approval (schema-change-approval). Slots SOON entram junto do S1.

## Riscos / callouts

- **Statement timeout 30s** = mudança de comportamento → CHANGELOG + README config.
- **Purga** = destrutiva → off default, confirm dialog, audit-log, hard-delete só de já-soft-deleted.
- **Pool cap** = risco de starvation multi-app → adiado até validar infra (fora deste design).
- **`require_rls_default`** acoplado à regra "Restricted exige email auth" — não pode forçar Restricted em app sem auth.
- **`UpsertSystemConfig`** cresce em parâmetros a cada controle → migrar assinatura para receber o struct.

## Cross-references

- `dashboard-redesign` T4.4 — cap de CSV export vira configurável aqui (S2.3).
- `two-factor-auth` — backend do "Require 2FA" (slot S3.1 da spec).
- `enterprise-licensing` — estado de licença para a tab License (S3.2).
- `schema-change-approval` (a abrir) — o toggle reservado aqui.
