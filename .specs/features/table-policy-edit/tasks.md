# Table Policy Edit Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/table-policy-edit/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` (section 3 - gate commands). Backend follows the existing pattern in `table_policies_store_test.go`/`table_policies_handler_test.go` (integration tests against a real Postgres pool - e.g. `TestCreateTablePolicy_DuplicateNameReturnsErrPolicyAlreadyExists`, `TestDeleteTablePolicy_NotFound`). Frontend has no component-level unit framework (confirmed in the sibling feature `enduser-roles-config`) - only Playwright e2e (`internal/dashboard/ui/e2e/*.spec.ts`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration DDL (`provisioner.go` ALTER TABLE) | none | build gate only | `internal/dashboard/provisioner.go` | `go build ./...` |
| Backend store (`UpdateTablePolicy`) | integration | Every listed edge case: happy path, not-found, unique conflict, invalid clause (no DDL applied), concurrent delete-then-update race - same depth as `TestCreateTablePolicy_*`/`TestDeleteTablePolicy_*` | `internal/dashboard/table_policies_store_test.go` | `go test ./internal/dashboard/...` |
| Backend handler (`UpdateTablePolicy` route) | integration | Happy path + 400 (invalid payload) + 404 (unknown policyId) + 409 (unique conflict) - same depth as `TestCreateTablePolicyHandler_*` | `internal/dashboard/table_policies_handler_test.go` | `go test ./internal/dashboard/...` |
| Frontend types/hook wiring (`TablePolicyRow`, `useUpdateTablePolicy`) | none | build gate only | `internal/dashboard/ui/src/lib/api.ts` | `npm run build` |
| Frontend UI flow (edit button, pre-populated form, PUT on save) | e2e | One Playwright test: happy path (edit roles/clauses, confirm persisted) + one edge case (orphan role still shown as chip on reopen - `ROLECFG-16`) | `internal/dashboard/ui/e2e/enduser-roles.spec.ts` (estende o arquivo já criado pela feature `enduser-roles-config`) | `cd internal/dashboard/ui && npx playwright test enduser-roles` |

## Gate Check Commands

> Generated from `Makefile`, `AGENTS.md` section 3, and `internal/dashboard/ui/package.json`. Nota: `npx tsc -b` direto pode falhar neste ambiente por resolver um `tsc` global diferente da versão pinada do projeto (achado da feature `enduser-roles-config`) - `npm run build` roda `tsc -b` pelo binário local correto e é o check real.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Backend-only task, no frontend touched | `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...` |
| Full | Task touches frontend types/hooks/UI, no new/changed e2e spec yet | `go build ./... && go test ./... && go vet ./... && cd internal/dashboard/ui && npm run build` |
| Build | Phase completion, or task adds/changes a Playwright e2e spec | Full gate + `cd internal/dashboard/ui && npx playwright test enduser-roles` + `gofmt -l <changed .go files>` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend

```
T1 → T2 → T3 → T4
```

### Phase 2: Frontend

```
T4 → T5
T5 → T6 → T7
```

---

## Task Breakdown

### T1: Migração - colunas `updated_at`/`updated_by`

**What**: Adicionar `ALTER TABLE zeep_system.table_policies ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ` e `ADD COLUMN IF NOT EXISTS updated_by UUID REFERENCES zeep_system.dashboard_users(id)` à lista de migrações idempotentes.
**Where**: `internal/dashboard/provisioner.go` (~linha 324-334, mesma lista das colunas de `table_policies`)
**Depends on**: None
**Reuses**: Padrão exato de `created_at`/`created_by` já existentes na mesma tabela.
**Requirement**: TPEDIT-01

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [x] Duas colunas adicionadas na mesma lista de `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
- [x] `go build ./...` limpo
- [x] Idempotência confirmada (rodar a migração 2x não erra)

**Tests**: none
**Gate**: quick

**Commit**: `feat(dashboard): add updated_at/updated_by columns to table_policies`

---

### T2: Store `UpdateTablePolicy`

**What**: Função `UpdateTablePolicy(ctx, pool, appID, schemaName, tableName, policyID string, columns []ColumnDef, def PolicyDef, updatedBy string) (TablePolicyRow, error)` — dentro de uma tx: `SELECT pg_policy_name FROM zeep_system.table_policies WHERE id=$1 AND app_id=$2 FOR UPDATE` (zero linhas → `ErrPolicyNotFound`), `BuildPolicySQL(def)`, `DROP POLICY IF EXISTS` do nome antigo, `CREATE POLICY` novo, `UPDATE` do catálogo (`action`, `roles`, `clauses`, `pg_policy_name`, `updated_at=now()`, `updated_by`) - unique violation (`23505`) → `ErrPolicyConflict`.
**Where**: `internal/dashboard/table_policies_store.go`
**Depends on**: T1
**Reuses**: Estrutura de tx de `CreateTablePolicy` (`table_policies_store.go:94-167`) + sintaxe `DROP POLICY IF EXISTS` de `DeleteTablePolicy` (`:228-231`).
**Requirement**: TPEDIT-01, TPEDIT-02, TPEDIT-03, TPEDIT-04, TPEDIT-05, TPEDIT-08

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] Happy path: edita roles/clauses/action de uma policy existente, `pg_policies` reflete o novo `USING`, catálogo reflete `updated_at`/`updated_by` preenchidos
- [ ] `policyID` inexistente ou de outro app retorna `ErrPolicyNotFound`
- [ ] Clause/coluna/operador inválido rejeitado por `BuildPolicySQL` ANTES de qualquer `DROP`/`CREATE` (nenhuma mutação parcial)
- [ ] Conflito de unicidade (`action`+`pg_policy_name` já usado por outra policy) retorna `ErrPolicyConflict`, sem commit
- [ ] Editar uma policy renomeando (`name` diferente) dropa o nome ANTIGO e cria com o nome NOVO - nenhuma policy nativa órfã
- [ ] Teste de integração cobre os 5 cenários acima
- [ ] Gate check passa: `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...`

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add UpdateTablePolicy store function`

---

### T3: Handler `UpdateTablePolicy`

**What**: Handler que recebe `PUT /dashboard/api/apps/{id}/tables/{table}/policies/{policyId}`, valida o body (mesmo parsing/validação de `CreateTablePolicy`), chama o store de T2, traduz `ErrPolicyNotFound`→404, `ErrPolicyConflict`→409, erro de validação→400, qualquer outro erro→500 genérico (nunca `err.Error()` bruto).
**Where**: `internal/dashboard/handler.go`
**Depends on**: T2
**Reuses**: Parsing/validação de body de `CreateTablePolicy` (`handler.go:1368-1424`).
**Requirement**: TPEDIT-01, TPEDIT-02, TPEDIT-03, TPEDIT-04, TPEDIT-05

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] 200 com a `TablePolicyRow` atualizada no happy path
- [ ] 400 quando payload inválido, mesma mensagem de erro de `CreateTablePolicy`
- [ ] 404 quando `policyId` não existe/não pertence ao app
- [ ] 409 quando a edição colide com outra policy existente
- [ ] Teste de integração cobre os 4 cenários acima (mirror de `TestCreateTablePolicyHandler_*`)
- [ ] Gate check passa: `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...`

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add UpdateTablePolicy handler`

---

### T4: Rota `PUT /api/apps/{id}/tables/{table}/policies/{policyId}`

**What**: Registrar a rota do handler de T3 no mesmo grupo protegido de `CreateTablePolicy`/`DeleteTablePolicy`.
**Where**: `internal/server/server.go` (~linha 178-179)
**Depends on**: T3
**Reuses**: Grupo de rotas já existente, `RequireAuth` inalterado.
**Requirement**: TPEDIT-01

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] `r.With(dashboard.RequireAuth(pool)).Put(".../policies/{policyId}", dashH.UpdateTablePolicy)` registrado
- [ ] Teste de integração confirma a rota responde via HTTP real (pode estender o de T3)
- [ ] Gate check passa: `go build ./... && go test ./... && go vet ./...` (gate completo de backend, fecha a Phase 1)

**Tests**: integration
**Gate**: full

**Commit**: `feat(server): register PUT table policy edit route`

---

### T5: Tipo `TablePolicyRow.updated_at/updated_by` + hook `useUpdateTablePolicy`

**What**: Estender `TablePolicyRow` com `updated_at: string | null`/`updated_by: string | null`; criar `useUpdateTablePolicy(appId, table)` (mutation, `PUT /dashboard/api/apps/{id}/tables/{table}/policies/{policyId}`, body `{policyId, def}`, invalida `['table-policies', appId, table]`, `onError: toast.error`).
**Where**: `internal/dashboard/ui/src/lib/api.ts`
**Depends on**: T4
**Reuses**: Forma exata de `useCreateTablePolicy`/`useDeleteTablePolicy` (`api.ts:241-280`).
**Requirement**: TPEDIT-01, TPEDIT-08

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] `TablePolicyRow` tipado com as duas novas colunas
- [ ] `useUpdateTablePolicy` chama `onError: toast.error(error.message)` (regra do `AGENTS.md`)
- [ ] `npm run build` limpo
- [ ] Gate check passa: full gate

**Tests**: none
**Gate**: full

**Commit**: `feat(dashboard-ui): add updated_at/updated_by fields and update hook for table policies`

---

### T6: `TablePoliciesTab` - modo de edição

**What**: Estado `editingPolicy: TablePolicyRow | null`; `openEditForm(policy)` popula `name`/`action`/`selectedRoles`/`clauses` a partir da policy e abre o form; `submit()` ramifica pra `updatePolicy.mutate({policyId, def})` quando `editingPolicy` presente; botão "Editar" (ícone) ao lado do botão de delete já existente (`:374-382`).
**Where**: `internal/dashboard/ui/src/components/TablePolicies.tsx`
**Depends on**: T5
**Reuses**: Form/estado de criação já existente (`name`, `action`, `selectedRoles`, `clauses`, `chipRoles`, `toggleRole`, `addClause`/`removeClause`/`updateClause`) - nenhum campo/componente novo.
**Requirement**: TPEDIT-06, TPEDIT-07

**Tools**:
- MCP: NONE
- Skill: `react-composition-patterns`

**Done when**:
- [ ] Botão "Editar" aparece por policy, ao lado do delete
- [ ] Abrir edição pré-popula todos os campos com os dados atuais da policy, incluindo role órfã como chip selecionado
- [ ] Salvar chama `PUT` (não `POST`) e a lista reflete os dados novos sem reload manual
- [ ] Fechar/cancelar sem salvar não chama nenhuma mutation
- [ ] `npm run build` limpo
- [ ] Gate check passa: full gate

**Tests**: none (cobertura funcional no e2e de T7)
**Gate**: full

**Commit**: `feat(dashboard-ui): add edit mode to TablePoliciesTab`

---

### T7: E2E - edição de table policy

**What**: Estender `internal/dashboard/ui/e2e/enduser-roles.spec.ts` com um teste cobrindo: editar uma policy existente (trocar roles/clauses via chips, sem digitar SQL), salvar, confirmar persistência; e um segundo cenário confirmando que uma role órfã (atribuída antes de sair de `enduser_roles_config`) aparece selecionada ao reabrir o form de edição (`ROLECFG-16`, agora testável).
**Where**: `internal/dashboard/ui/e2e/enduser-roles.spec.ts` (modificar)
**Depends on**: T6
**Reuses**: Setup/fixtures já existentes no arquivo (login via `storageState`, criação de app de teste).
**Requirement**: TPEDIT-06, TPEDIT-07

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] Teste "edita policy existente via form pré-populado" passa
- [ ] Teste "role órfã aparece selecionada ao reabrir edição" passa
- [ ] Gate check passa: `cd internal/dashboard/ui && npx playwright test enduser-roles` (Build gate, fecha a feature)
- [ ] Test count: 2 testes novos, nenhum dos existentes removido/pulado

**Tests**: e2e
**Gate**: build

**Commit**: `test(dashboard-ui): cover table policy edit and orphan role reopen`

---

## Phase Execution Map

```
Phase 1 → Phase 2

Phase 1:  T1 ------→ T2 ------→ T3 ------→ T4
Phase 2:  T4 --→ T5 ------→ T6 ------→ T7
```

Execution is strictly sequential - there is no intra-phase parallelism.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migração | 2 colunas DDL, mesma finalidade | ✅ Granular |
| T2: Store | 1 função | ✅ Granular |
| T3: Handler | 1 handler | ✅ Granular |
| T4: Rota | 1 linha de registro | ✅ Granular |
| T5: Tipo + hook | 1 tipo + 1 hook (mesmo par, padrão já usado assim) | ✅ Granular |
| T6: Modo de edição | 1 componente modificado, 1 conceito (edição) | ✅ Granular |
| T7: E2E | 1 arquivo modificado, 2 casos do mesmo flow | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 (Phase 2 consome o fim da Phase 1) | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migração | Migration DDL | none | none | ✅ OK |
| T2: Store | Backend store | integration | integration | ✅ OK |
| T3: Handler | Backend handler | integration | integration | ✅ OK |
| T4: Rota | Backend handler | integration | integration | ✅ OK |
| T5: Tipo + hook | Frontend types/hook | none | none | ✅ OK |
| T6: Modo de edição | Frontend UI flow | e2e | none (coberto no e2e de T7, mesma fase) | ✅ OK |
| T7: E2E | Frontend UI flow | e2e | e2e | ✅ OK |

Regra aplicada (merge forward dentro da própria fase): T6 não é testável em unidade (sem framework de teste de componente no repo) - o e2e que exercita o flow completo é a task imediatamente seguinte na mesma fase (T7), não uma fase futura desconectada.
