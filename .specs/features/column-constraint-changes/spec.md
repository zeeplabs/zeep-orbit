# Column Constraint Changes Specification

## Problem Statement

O Dashboard deixa o usuário alternar `required`, `unique` e `default` de uma coluna **já existente** (`TableCard.tsx`), e `PUT /dashboard/api/apps/{id}/tables/{tableId}` (`UpdateAppTable`) persiste essa mudança no metadata (`zeep_system.app_tables.columns`). Mas `provisioner.Apply`, para uma tabela existente, só roda `applyColumnChanges` (rename + type change) e `addMissingColumns` (add-only) — nenhum caminho emite `ALTER COLUMN ... SET/DROP NOT NULL`, `SET/DROP DEFAULT` ou adiciona/remove a constraint `UNIQUE`. Resultado: metadata e Postgres divergem silenciosamente.

Incidente real (2026-10-13, Orbit prd, app `asset-manager`): `assets.serial` com `required:false` no metadata e `NOT NULL` no banco — todo insert sem `serial` falhava com `23502`, que a Data API devolvia como `500 {"error":"failed to insert row"}`, sem nenhuma pista pro cliente. Auditoria read-only no mesmo dia achou o mesmo desvio em `asset-manager.employees.cpf/email` e em `github-dev-activity.backfill_progress.last_synced_at` (default).

Segundo problema, no mesmo pipe: a Data API (`HandleCreate`/`HandleUpdate`/`HandleDelete`) só classifica `23505`/`23514`/`42P10` (e `42501` no upsert). Qualquer outra falha de escrita causada pela requisição do cliente — not-null, FK, RLS `WITH CHECK`, valor inválido pro tipo — vira 500 genérico; update/delete nem logam o erro real.

## Goals

- [ ] CCC-01: Alterar `required` de uma coluna existente aplica `SET NOT NULL` / `DROP NOT NULL` no Postgres.
- [ ] CCC-02: Alterar `default` (literal ou expressão) de uma coluna existente aplica `SET DEFAULT` / `DROP DEFAULT`.
- [ ] CCC-03: Alterar `unique` de uma coluna existente adiciona/remove a constraint `UNIQUE` de coluna única.
- [ ] CCC-04: Mudanças que os dados existentes impedem (NULLs ao tornar obrigatória, duplicados ao tornar única, default inválido pro tipo, coluna nova obrigatória sem default em tabela com linhas) são rejeitadas **antes de qualquer DDL** com erro tipado `*provisioner.ColumnConstraintError` → HTTP 400 nomeando coluna e contagem; metadata não é persistido.
- [ ] CCC-05: Todas as mudanças de constraint de uma requisição rodam numa única transação (tudo ou nada).
- [ ] CCC-06: As DDLs são idempotentes frente a desvio pré-existente (ex.: `SET NOT NULL` numa coluna já `NOT NULL`, `ADD UNIQUE` numa coluna já única não cria constraint duplicada).
- [ ] CCC-07: `ColumnConstraintError` é exposto como 400/mensagem segura em REST, MCP (`mapWriteError`) e AI chat — mesmo tratamento de `TypeChangeError`.
- [ ] ERR-01: Data API classifica falhas de escrita causadas pelo cliente: `23502` → 400 `column "x" is required`; `23503` → 400 (insert/update, referência inexistente) / 409 (delete, linha ainda referenciada); `42501` em insert/update → 403; classe `22` (data exception) → 400. Mensagens nunca ecoam o valor enviado (`Message`/`Detail` do Postgres não vão pro cliente).
- [ ] ERR-02: Todo 500 que sobra em create/update/delete loga o erro real server-side (hoje só create loga).

## Out of Scope

| Item | Motivo |
| --- | --- |
| Reconciliar automaticamente desvios já existentes em prd | DDL em produção sem revisão humana. O fluxo novo converge quando alguém salvar a coluna; desvios conhecidos são corrigidos pontualmente com aprovação. |
| `UNIQUE` composto / índices | Já tem caminho próprio (`indexes`, `AddTableIndexForUser`). Só a constraint de coluna única é tratada. |
| Webhook inbound (`webhook_active`) | 500 em falha de escrita é intencional (o emissor re-tenta) — coberto por `TestWebhookActive_InsertRLSDeniedReturns500...`. |
| `0A000 cached plan must not change result type` após `ALTER` | Transitório (pgx invalida o cache na 1ª falha por conexão); tratado separadamente se aparecer em produção. |
| Lock/tempo de `ADD UNIQUE`/`SET NOT NULL` em tabela grande | Postgres faz scan sob lock exclusivo; `statement_timeout` existente se aplica. Documentado no CHANGELOG. |

## Decisões

- Mudança só é aplicada quando o **metadata anterior** difere do novo (intenção explícita do usuário) — nunca por diff contra o banco, para que qualquer save de tabela não dispare DDL surpresa em colunas com desvio antigo.
- Erro de schema é 400 (input do usuário incompatível com os dados), não 409, mantendo o padrão de `TypeChangeError`/`EnumValueInUseError`.
- RLS `WITH CHECK` negado vira 403 (era 500 por decisão anterior registrada em `rls_test.go`; a decisão era "não é 409", não "precisa ser 500").
