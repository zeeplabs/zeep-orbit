# Modo RLS "policy" Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/rls-policy-mode/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 ("Before considering any change done") — mandates `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l` for backend; `npx tsc -b`, `npm run build` for `internal/dashboard/ui`. No repo-wide coverage threshold beyond "run and confirm clean". Sampled 8 existing test files: `internal/config/validate_test.go`, `internal/dashboard/table_rls_test.go`, `internal/dashboard/handler_test.go`, `internal/server/rls_test.go`, `internal/server/rls_policy_test.go`, `internal/dashboard/table_policies_store_test.go`, `internal/dashboard/table_policies_handler_test.go`, `internal/provisioner/enduser_grant_test.go`. No test files exist under `internal/dashboard/ui/src` (no test runner wired for the frontend at all — `package.json` scripts are `dev`/`build`/`preview` only).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------- | --------------------- | ----------------- | ------------ |
| `internal/config` (predicados `ValidRLS`/`HasOwnerColumn`/`AutoScopesByOwner`) | unit | Todas as 4 entradas de enum × cada predicado (tabela de casos) | `internal/config/rls_test.go` | `go test ./internal/config/...` |
| `internal/server` (`resolveOwner`, 5 call sites) | unit + integration | 1:1 com RLSP-01/02/03/04; integração contra Postgres real segue padrão de `rls_policy_test.go` | `internal/server/handler_test.go` (unit), `internal/server/rls_policy_mode_test.go` (integration, `TEST_DATABASE_URL`) | `go test ./internal/server/...` (unit sem DB); `TEST_DATABASE_URL=... go test ./internal/server/...` (integration) |
| `internal/provisioner` (`createTable`, `addMissingColumns`, `EnsureRowLevelSecurity`, `BuildPolicySQL`/`colByName`) | unit | Todo branch de `rls` (`""`/`owner`/`enabled`/`policy`) coberto; RLSP-05/06 cobertos por caso de cláusula `owner_id` | `internal/provisioner/table_test.go`, `internal/provisioner/policy_test.go` | `go test ./internal/provisioner/...` |
| `internal/dashboard` (`validateTableInput`, `resolveTableRLS`, `UpdateAppTable`) | unit | RLSP-07/08/09/10, todo caso de `Assumptions`/`Edge Cases` do spec | `internal/dashboard/handler_test.go`, `internal/dashboard/apps_store_test.go` | `go test ./internal/dashboard/...` |
| `internal/docs/generator.go` | unit | Caso `rls: "policy"` expõe `owner_id` no schema OpenAPI | `internal/docs/generator_test.go` | `go test ./internal/docs/...` |
| Frontend (`TableCard.tsx`) | none | Sem runner de teste no frontend hoje — build gate cobre | — | `npx tsc -b && npm run build` (dentro de `internal/dashboard/ui`) |
| End-to-end cross-cutting (fail-closed nativo, visibilidade cross-user) | integration | Cenário motivador do spec (RLSP-01/02): tabela `policy` sem policy → `[]`; com policy sem cláusula de linha → role vê linhas de outro usuário; INSERT continua populando `owner_id` | `internal/server/rls_policy_mode_test.go` (segue estrutura de `rls_policy_test.go`: fixture + pool RLS + pool owner) | `TEST_DATABASE_URL=postgres://... go test ./internal/server/... -run TestRLSPolicyMode` |

## Gate Check Commands

> Gerado do `AGENTS.md` §3 (comandos exatos exigidos antes de considerar qualquer mudança pronta).

| Gate Level | When to Use | Command |
| ---------- | ------------ | ------- |
| Quick | Após tasks Go sem dependência de Postgres real | `go build ./... && go vet ./... && gofmt -l <arquivos alterados>` |
| Full | Após tasks que tocam `internal/server`/`internal/dashboard`/`internal/provisioner` com teste de integração | `go build ./... && go test ./... && go vet ./... && gofmt -l <arquivos alterados>` (suíte completa exige Postgres descartável via `TEST_DATABASE_URL`, mesmo padrão já usado nas sessões anteriores desta feature) |
| Build | Após task que toca frontend (`internal/dashboard/ui`) | `cd internal/dashboard/ui && npx tsc -b && npm run build` |

---

## Execution Plan

### Phase 1: Predicados centrais

```
T1
```

### Phase 2: Enforcement na camada de request

```
T1 → T2
```

### Phase 3: Provisionador — DDL e fail-closed nativo

```
T3 → T4
T1 → T4
```

### Phase 4: Policy clauses e validação de enum

```
T5
T1 → T6
```

(T5 e T6 não dependem uma da outra — ambas dependem só de T1/nada; executam em sequência dentro da fase por convenção, sem relação de dependência entre si.)

### Phase 5: Dashboard — troca de modo e correções de exibição

```
T1 → T7
T3 → T7
T1 → T8
T1 → T9
```

(T7, T8, T9 não dependem umas das outras — todas dependem só de T1/T3; executam em sequência dentro da fase por convenção.)

### Phase 6: Frontend — aviso de troca de modo

```
T7 → T10
```

### Phase 7: Teste de integração ponta-a-ponta

```
T2 → T11
T4 → T11
T5 → T11
```

---

## Task Breakdown

### T1: Predicados centrais de RLS em `internal/config`

**What**: Criar `internal/config/rls.go` com `ValidRLS(rls string) bool`, `HasOwnerColumn(rls string) bool`, `AutoScopesByOwner(rls string) bool`, cobrindo `""`/`"owner"`/`"enabled"`/`"policy"`.
**Where**: `internal/config/rls.go`
**Depends on**: None
**Reuses**: Nenhum código existente — extração do padrão hoje espalhado em `handler.go:30`, `table.go:123,241`
**Requirement**: RLSP-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `ValidRLS` retorna `true` só para `""`, `"owner"`, `"enabled"`, `"policy"`
- [x] `HasOwnerColumn` retorna `true` para `"owner"`, `"enabled"`, `"policy"`; `false` para `""` e qualquer outro valor
- [x] `AutoScopesByOwner` retorna `true` só para `"owner"`, `"enabled"`
- [x] Gate: `go build ./... && go vet ./... && gofmt -l internal/config/rls.go internal/config/rls_test.go`
- [x] Test count: tabela de casos cobrindo as 4 entradas × 3 predicados (mínimo 12 asserts) passa

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add rls mode predicates for policy mode`

---

### T2: `resolveOwner` separa filtro de coluna

**What**: Alterar `resolveOwner` e os 5 call sites (`HandleList`, `HandleCreate`, `HandleGetByID`, `HandleUpdate`, `HandleDelete`) em `internal/server/handler.go` para usar `config.HasOwnerColumn` (decide se popula `ownerID` a partir do usuário autenticado) e `config.AutoScopesByOwner` (decide se esse `ownerID` é passado como filtro pros builders de list/get/update/delete) — `HandleCreate` sempre recebe o `ownerID` real.
**Where**: `internal/server/handler.go`
**Depends on**: T1
**Reuses**: `resolveOwner` existente (linhas 29-38), `rlsClaimsFromContext` inalterado
**Requirement**: RLSP-01, RLSP-03, RLSP-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Tabela `rls: "policy"` — `HandleList`/`HandleGetByID`/`HandleUpdate`/`HandleDelete` não recebem `ownerID` como filtro (SQL gerado sem `owner_id = $N`)
- [x] Tabela `rls: "policy"` — `HandleCreate` continua populando `owner_id` com o `sub` do usuário autenticado
- [x] Tabela `rls: "owner"`/`"enabled"` — nenhuma mudança de SQL gerado (teste de regressão comparando query antes/depois)
- [x] Gate: `go build ./... && go vet ./... && gofmt -l internal/server/handler.go internal/server/handler_test.go`
- [x] Test count: casos novos para `"policy"` (list/get/update/delete sem filtro, insert com filtro) + suíte existente de `internal/server` sem regressão

**Tests**: unit
**Gate**: quick

**Commit**: `fix(server): decouple owner_id filter from owner_id column in resolveOwner`

---

### T3: `EnsureRowLevelSecurity` extraído e reusado

**What**: Extrair o `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` de `internal/dashboard/table_policies_store.go:140-146` para uma função exportada `provisioner.EnsureRowLevelSecurity(ctx, pool, schemaName, tableName)` (idempotente), e trocar a chamada inline em `table_policies_store.go` para usá-la.
**Where**: `internal/provisioner/table.go` (nova função), `internal/dashboard/table_policies_store.go` (troca de chamada)
**Depends on**: None
**Reuses**: Statement SQL já existente em `table_policies_store.go:142`
**Requirement**: RLSP-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `EnsureRowLevelSecurity` executa `ALTER TABLE %q.%q ENABLE ROW LEVEL SECURITY` e é idempotente (chamar duas vezes não erra)
- [ ] `table_policies_store.go` usa a função extraída, comportamento de `TestCreateTablePolicy_EnablesRowLevelSecurityOnlyOnce` inalterado
- [ ] Gate: `go build ./... && go vet ./... && gofmt -l internal/provisioner/table.go internal/dashboard/table_policies_store.go`
- [ ] Test count: suíte de `internal/dashboard/table_policies_store_test.go` sem regressão + novo teste unitário de `EnsureRowLevelSecurity`

**Tests**: unit
**Gate**: quick

**Commit**: `refactor(provisioner): extract EnsureRowLevelSecurity helper`

---

### T4: `createTable`/`addMissingColumns` reconhecem `"policy"`

**What**: Trocar as duas checagens `rls == "owner" || rls == "enabled"` em `internal/provisioner/table.go` (linhas 123 e 241) por `config.HasOwnerColumn(rls)`; em `createTable`, chamar `EnsureRowLevelSecurity` (T3) quando `rls == "policy"`, dentro da mesma transação de criação da tabela.
**Where**: `internal/provisioner/table.go`
**Depends on**: T1, T3
**Reuses**: `EnsureRowLevelSecurity` (T3), predicados de T1
**Requirement**: RLSP-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Tabela criada com `rls: "policy"` sai do provisionamento com `ENABLE ROW LEVEL SECURITY` já ativo, sem nenhuma policy ainda cadastrada
- [ ] `SELECT`/`UPDATE`/`DELETE` como `zeep_app_enduser` nessa tabela recém-criada retornam zero linhas/zero linhas afetadas (fail-closed nativo, sem código de app)
- [ ] Colunas `owner_id` continuam sendo criadas para `"owner"`, `"enabled"` e agora também `"policy"` (mesma DDL)
- [ ] Gate: `go build ./... && go test ./... && go vet ./... && gofmt -l internal/provisioner/table.go internal/provisioner/table_test.go`
- [ ] Test count: casos novos para `rls: "policy"` em ambas as funções + suíte existente sem regressão

**Tests**: unit
**Gate**: full

**Commit**: `feat(provisioner): enable native RLS at table creation for policy mode`

---

### T5: `owner_id` referenciável em cláusula de policy

**What**: Em `internal/provisioner/policy.go`, após o loop que popula `colByName` a partir de `tableColumns` (linhas 152-155), adicionar incondicionalmente `colByName["owner_id"] = config.ColumnConfig{Name: "owner_id", Type: "uuid"}`.
**Where**: `internal/provisioner/policy.go`
**Depends on**: None
**Reuses**: `translateClause`/`translateOperand`/`claimExpr`/`literalExpr` sem alteração
**Requirement**: RLSP-05, RLSP-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Policy com cláusula `{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"}` traduz para `"owner_id" = current_setting('app.jwt_sub', true)::UUID`
- [ ] Cláusula com `owner_id` e operador incompatível com `uuid` (ex.: `LIKE`) continua rejeitada pela mesma validação de tipo já existente
- [ ] Coluna fora de `tableColumns` e diferente de `owner_id` continua rejeitada como "unknown column"
- [ ] Gate: `go build ./... && go vet ./... && gofmt -l internal/provisioner/policy.go internal/provisioner/policy_test.go`
- [ ] Test count: 3 casos novos (clause válida com owner_id, operador incompatível, coluna de sistema diferente ainda rejeitada) + suíte existente sem regressão

**Tests**: unit
**Gate**: quick

**Commit**: `feat(provisioner): allow owner_id in policy clauses`

---

### T6: Validação de enum `rls` + reconhecimento de `"policy"` no Dashboard

**What**: Em `internal/dashboard/handler.go`, `validateTableInput` chama `config.ValidRLS(t.RLS)` antes de qualquer outra checagem (erro claro se inválido); trocar `t.RLS == "enabled" || t.RLS == "owner"` (linha 132, gate de auth-email) por `config.HasOwnerColumn(t.RLS)`.
**Where**: `internal/dashboard/handler.go`
**Depends on**: T1
**Reuses**: `validateTableInput` existente, mensagens de erro no mesmo estilo das já usadas ali
**Requirement**: RLSP-09, RLSP-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `rls: "disabled"` (ou qualquer valor não reconhecido) é rejeitado com erro claro na criação/atualização de tabela
- [ ] `rls: "policy"` sem auth por e-mail habilitada no app é rejeitado, mesma regra hoje aplicada a `"owner"`/`"enabled"`
- [ ] `rls: "policy"` com auth por e-mail habilitada é aceito
- [ ] Gate: `go build ./... && go vet ./... && gofmt -l internal/dashboard/handler.go internal/dashboard/handler_test.go`
- [ ] Test count: `TestValidateTableInputRejectsUnknownRLS` (novo) + `TestValidateTableInputAcceptsPolicyWithEmailAuth` (novo) + suíte existente sem regressão

**Tests**: unit
**Gate**: quick

**Commit**: `feat(dashboard): validate rls enum and recognize policy mode`

---

### T7: `UpdateAppTable` habilita RLS nativo na troca de modo

**What**: Em `internal/dashboard/apps_store.go:491` (`UpdateAppTable`), quando o novo `rls` é `"policy"` e o valor anterior não garantia RLS habilitado, chamar `provisioner.EnsureRowLevelSecurity` (T3) antes de retornar — sem exigir recriação da tabela nem perda de dados.
**Where**: `internal/dashboard/apps_store.go`
**Depends on**: T1, T3
**Reuses**: `EnsureRowLevelSecurity` (T3)
**Requirement**: RLSP-07, RLSP-08

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Tabela `"enabled"` sem nenhuma policy, com dados de múltiplos usuários, trocada para `"policy"` → RLS fica habilitado e, sem policy nenhuma, todos os usuários passam a ver `[]` (não vazamento)
- [ ] Troca não recria a tabela nem apaga dados existentes
- [ ] Troca `"policy"` → `"enabled"` continua funcionando (RLS habilitado não é desligado, comportamento de app volta a filtrar por dono)
- [ ] Gate: `go build ./... && go test ./... && go vet ./... && gofmt -l internal/dashboard/apps_store.go internal/dashboard/apps_store_test.go`
- [ ] Test count: 3 casos novos de troca de modo + suíte existente sem regressão

**Tests**: unit
**Gate**: full

**Commit**: `feat(dashboard): enable native RLS when switching table to policy mode`

---

### T8: `docs/generator.go` expõe `owner_id` pra `"policy"`

**What**: Em `internal/docs/generator.go:264`, trocar `table.RLS == "owner"` por `config.HasOwnerColumn(table.RLS)`.
**Where**: `internal/docs/generator.go`
**Depends on**: T1
**Reuses**: `buildResponseSchema` existente
**Requirement**: RLSP-10 (edge case)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Schema OpenAPI de tabela `rls: "policy"` inclui `owner_id` (uuid, readOnly)
- [ ] Schema de tabela `rls: "owner"` continua idêntico (regressão)
- [ ] Schema de tabela `rls: "enabled"` agora também inclui `owner_id` — correção de gap pré-existente, fora do escopo original mas coberta de graça pela troca de predicado
- [ ] Gate: `go build ./... && go vet ./... && gofmt -l internal/docs/generator.go internal/docs/generator_test.go`
- [ ] Test count: 1 caso novo (`policy`) + 1 caso de regressão (`enabled` agora expõe `owner_id`) + suíte existente

**Tests**: unit
**Gate**: quick

**Commit**: `fix(docs): expose owner_id in OpenAPI schema for enabled and policy rls modes`

---

### T9: Data Browser reconhece `"enabled"`/`"policy"`

**What**: Em `internal/dashboard/handler.go:1899`, trocar `t.RLS == "owner"` por `config.HasOwnerColumn(t.RLS)`.
**Where**: `internal/dashboard/handler.go`
**Depends on**: T1
**Reuses**: Handler do Data Browser existente

**Requirement**: RLSP-10 (edge case)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Data Browser lista `owner_id` como coluna pra tabelas `"owner"`, `"enabled"` e `"policy"`
- [ ] Gate: `go build ./... && go vet ./... && gofmt -l internal/dashboard/handler.go`
- [ ] Test count: caso novo cobrindo `"enabled"`/`"policy"` no Data Browser (se já existir teste da rota, estender; senão, teste unitário isolado da montagem de `cols`)

**Tests**: unit
**Gate**: quick

**Commit**: `fix(dashboard): show owner_id column in data browser for enabled and policy tables`

---

### T10: Frontend — opção `"policy"` e aviso de troca de modo

**What**: Em `internal/dashboard/ui/src/components/TableCard.tsx`: (1) corrigir `autoColumnsFor` (linha 82-84) pra incluir `"policy"`; (2) adicionar `"policy"` como opção no `Select` de RLS (linha 286) com rótulo próprio (i18n en/pt-BR); (3) ao trocar o valor entre `"enabled"`/`"owner"` e `"policy"` numa tabela já existente (não numa criação nova), exibir um aviso explícito (mesmo padrão de confirmação já usado em outras ações destrutivas do dashboard) de que a troca altera quais linhas cada role passa a ver, antes de confirmar o `onUpdate`.
**Where**: `internal/dashboard/ui/src/components/TableCard.tsx`, `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T7
**Reuses**: Padrão de confirmação já existente no dashboard para ações que alteram comportamento de dados (mesmo componente de diálogo usado em exclusões)
**Requirement**: RLSP-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `Select` de RLS mostra `"policy"` como opção, com string traduzida em `en.json` e `pt-BR.json`
- [ ] `autoColumnsFor("policy")` inclui a coluna `owner_id` (mesma lista de `"owner"`/`"enabled"`)
- [ ] Trocar o valor numa tabela existente entre grupos (`""/"owner"/"enabled"` ↔ `"policy"`) dispara o aviso antes de chamar `onUpdate`; trocar dentro do mesmo grupo (ex.: `"owner"` → `"enabled"`) ou numa tabela ainda não salva (criação) não dispara
- [ ] Gate: `cd internal/dashboard/ui && npx tsc -b && npm run build`
- [ ] `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json'))"` e o mesmo pra `pt-BR.json` sem erro

**Tests**: none (sem runner de teste no frontend — build gate cobre)
**Gate**: build

**Commit**: `feat(dashboard-ui): add policy rls mode option with mode-switch warning`

---

### T11: Teste de integração ponta-a-ponta do modo `"policy"`

**What**: Criar `internal/server/rls_policy_mode_test.go` seguindo a estrutura de `internal/server/rls_policy_test.go` (fixture com pool RLS `zeep_app_enduser` + pool owner, skip se `TEST_DATABASE_URL` vazio), cobrindo: (a) tabela `policy` sem nenhuma policy → qualquer usuário vê `[]`; (b) policy `select` sem cláusula de linha pra um role → esse role vê linhas de outro usuário; (c) INSERT em tabela `policy` continua populando `owner_id`; (d) Data Browser (pool owner) vê todas as linhas independente de policy.
**Where**: `internal/server/rls_policy_mode_test.go`
**Depends on**: T2, T4, T5
**Reuses**: `setupRLSPolicyFixture` e estrutura de sub-testes de `rls_policy_test.go`
**Requirement**: RLSP-01, RLSP-02, RLSP-03, RLSP-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Os 4 cenários (a-d) do "What" passam contra Postgres real
- [ ] Cenário (a) prova fail-closed sem depender de nenhum filtro de app (testável desligando temporariamente a asserção de app e confirmando que o Postgres já nega — ou, mais simples, testando direto com conexão raw pgx como `zeep_app_enduser`, mesmo padrão de `RawPgxConnectionAsEnduserRoleReproducesTheSameDenial` em `rls_policy_test.go`)
- [ ] Gate: `TEST_DATABASE_URL=postgres://... go test ./internal/server/... -run TestRLSPolicyMode -v`
- [ ] Test count: mínimo 4 sub-testes (um por cenário), todos verdes

**Tests**: integration
**Gate**: full

**Commit**: `test(server): add end-to-end coverage for rls policy mode`

---

## Phase Execution Map

Fases executam em ordem (1 a 7); dentro de cada fase, as tasks executam na ordem listada. As setas de dependência real de cada task já estão nos diagramas por fase, na seção **Execution Plan** acima — esta seção não repete arrows pra evitar divergência entre os dois blocos; ela só resume o agrupamento:

- Fase 1: T1
- Fase 2: T2
- Fase 3: T3, T4
- Fase 4: T5, T6
- Fase 5: T7, T8, T9
- Fase 6: T10
- Fase 7: T11

Execução estritamente sequencial — sem paralelismo intra-fase.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Predicados centrais | 1 arquivo novo | ✅ Granular |
| T2: `resolveOwner` + call sites | 1 arquivo (`handler.go`), 1 conceito coeso | ✅ Granular |
| T3: `EnsureRowLevelSecurity` | 1 função extraída + 1 troca de chamada, 2 arquivos diretamente relacionados | ✅ Granular (extração + uso, indissociáveis) |
| T4: `createTable`/`addMissingColumns` | 1 arquivo (`table.go`), 2 funções do mesmo conceito | ✅ Granular |
| T5: `owner_id` em policy clause | 1 arquivo (`policy.go`) | ✅ Granular |
| T6: Validação de enum + auth-email gate | 1 arquivo (`handler.go`), mesma função lógica (`validateTableInput`) | ✅ Granular |
| T7: `UpdateAppTable` troca de modo | 1 arquivo (`apps_store.go`) | ✅ Granular |
| T8: `docs/generator.go` | 1 arquivo | ✅ Granular |
| T9: Data Browser | 1 arquivo (mesma função de T6, task separada por commit/teste distintos) | ✅ Granular |
| T10: Frontend | 1 componente + 2 arquivos de i18n (mesmo padrão de todas as outras features que tocam string nova) | ✅ Granular |
| T11: Teste de integração | 1 arquivo novo | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ----------------------- | --------------- | ------ |
| T1 | None | Nenhuma seta entrante | ✅ Match |
| T2 | T1 | `T1 → T2` (Fase 2) | ✅ Match |
| T3 | None | Nenhuma seta entrante | ✅ Match |
| T4 | T1, T3 | `T3 → T4`, `T1 → T4` (Fase 3) | ✅ Match |
| T5 | None | Nenhuma seta entrante (Fase 4) | ✅ Match |
| T6 | T1 | `T1 → T6` (Fase 4) | ✅ Match |
| T7 | T1, T3 | `T1 → T7`, `T3 → T7` (Fase 5) | ✅ Match |
| T8 | T1 | `T1 → T8` (Fase 5) | ✅ Match |
| T9 | T1 | `T1 → T9` (Fase 5) | ✅ Match |
| T10 | T7 | `T7 → T10` (Fase 6) | ✅ Match |
| T11 | T2, T4, T5 | `T2 → T11`, `T4 → T11`, `T5 → T11` (Fase 7) | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ----------------------------- | ----------------- | ----------- | ------ |
| T1: Predicados | `internal/config` | unit | unit | ✅ OK |
| T2: `resolveOwner` | `internal/server` | unit + integration (integration na T11, não deferida — cobertura unit já embutida na própria task) | unit | ✅ OK (integração fica em T11, que é a task dedicada de integração da matriz — não é deferral de teste unitário, é camada distinta) |
| T3: `EnsureRowLevelSecurity` | `internal/provisioner` | unit | unit | ✅ OK |
| T4: `createTable`/`addMissingColumns` | `internal/provisioner` | unit | unit | ✅ OK |
| T5: `owner_id` em clause | `internal/provisioner` | unit | unit | ✅ OK |
| T6: Validação de enum | `internal/dashboard` | unit | unit | ✅ OK |
| T7: `UpdateAppTable` | `internal/dashboard` | unit | unit | ✅ OK |
| T8: `docs/generator.go` | `internal/docs` | unit | unit | ✅ OK |
| T9: Data Browser | `internal/dashboard` | unit | unit | ✅ OK |
| T10: Frontend | Frontend (`TableCard.tsx`) | none | none | ✅ OK |
| T11: Integração ponta-a-ponta | Cross-cutting (server+provisioner+dashboard via Postgres real) | integration | integration | ✅ OK |

Nenhuma violação — nenhuma task usa "testado em outra task" como justificativa pra `Tests: none`; T10 usa `none` só porque a matriz já define `none` pra essa camada (sem runner de teste no frontend).

---

## Tools por task

Todas as tasks usam ferramentas padrão de leitura/edição de arquivo e `go`/`npm` via shell — nenhuma precisa de MCP externo ou skill adicional além do próprio `tlc-spec-driven` que orquestra a execução.
