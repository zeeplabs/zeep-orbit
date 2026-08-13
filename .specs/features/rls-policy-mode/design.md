# Modo RLS "policy" Design

**Spec**: `.specs/features/rls-policy-mode/spec.md`
**Status**: Draft

---

## Abordagens consideradas

**Recomendada: B — centralizar os dois predicados hoje conflados, sem criar tipo/enum novo.**

| # | Abordagem | Trade-off |
| - | --------- | --------- |
| A | Adicionar `"policy"` como terceiro literal em cada um dos ~6 pontos que hoje fazem `rls == "owner" \|\| rls == "enabled"` (`handler.go:30`, `table.go:123`, `table.go:241`, `dashboard/handler.go:132`, `docs/generator.go:264`, `dashboard/handler.go:1899`) | Menor diff, mas perpetua o problema raiz que o agente de exploração encontrou: a checagem "essa tabela tem owner_id" e "essa tabela filtra por owner_id" são hoje o mesmo `if`, e é exatamente essa fusão que quebra o INSERT em modo `policy` (RLSP-03) se só se adicionar mais um literal ao mesmo `if`. |
| **B** | Extrair dois predicados nomeados em `internal/config` — `HasOwnerColumn(rls)` (verdadeiro para `owner`/`enabled`/`policy`) e `AutoScopesByOwner(rls)` (verdadeiro só para `owner`/`enabled`) — e trocar os ~6 pontos por chamadas a eles | Resolve a causa raiz (a fusão de dois conceitos num único `if`) em vez de só estender o padrão problemático. Diff um pouco maior (6 arquivos, troca mecânica de condição), zero mudança de comportamento observável em `owner`/`enabled` (os dois predicados retornam exatamente o mesmo que o `if` atual para esses dois valores). |
| C | Enum/tipo dedicado (`type RLSMode int` com `String()`/`Parse()`) substituindo o `string` solto em `TableConfig.RLS` | Ceremonial demais pra 4 valores possíveis; exigiria tocar serialização JSON/YAML existente (`yaml:"rls"`, `json` no dashboard) e todo o banco (`app_tables.rls` já é `TEXT`) sem ganho real sobre B. |

Escolhida **B**: resolve o bug de conflação na raiz (owner_id-column vs owner_id-filter), sem introduzir tipo novo nem migração de schema.

---

## Architecture Overview

```mermaid
graph TD
    A[Dashboard: criar/editar tabela] -->|rls="policy"| B[validateTableInput]
    B -->|config.ValidRLS| C[UpdateAppTable / CreateAppTable]
    C --> D[provisioner.createTable / addMissingColumns]
    D -->|config.HasOwnerColumn| E["owner_id UUID NOT NULL"]
    D -->|rls == policy| F["ALTER TABLE ... ENABLE ROW LEVEL SECURITY (na criação, não só na 1ª policy)"]
    G[REST /{app}/{table}] --> H[resolveOwner]
    H -->|config.AutoScopesByOwner| I["ownerID = user.ID (owner/enabled)"]
    H -->|rls == policy| J["ownerID = '' para filtro, mas usuário ainda exigido"]
    I --> K[query.BuildList/Update/Delete/GetByID: WHERE owner_id = $sub]
    J --> L[query.BuildList/... sem WHERE owner_id]
    G --> M[query.BuildInsert]
    M -->|config.HasOwnerColumn| N["owner_id sempre populado com user.ID, mesmo em policy"]
    O[Postgres] -->|RLS habilitado + policies nativas| L
    O -->|zero select policy| P["nega tudo nativamente — fail-closed sem código de app"]
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --------- | -------- | ---------- |
| `translateClause`/`colByName` | `internal/provisioner/policy.go:152-241` | Injetar `owner_id` (tipo `uuid`) no `colByName` antes da validação — reaproveita 100% da lógica de cast/operador existente (RLSP-05/06), zero código novo de validação |
| `WithRLSContext` / GUCs `app.jwt_role`/`app.jwt_sub`/`app.jwt_email` | `internal/db/client.go`, `internal/server/handler.go:44-50` | Já roda incondicionalmente em toda request — modo `policy` não precisa de nenhum canal novo de claims, só deixar de sobrepor com filtro de app |
| `CreateTablePolicy`'s `ENABLE ROW LEVEL SECURITY` idempotente | `internal/dashboard/table_policies_store.go:140-146` | `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` é idempotente no Postgres — rodar de novo ao criar a 1ª policy numa tabela `policy` (que já a habilitou na criação) não dá erro; nenhuma mudança necessária nesse arquivo |
| Padrão de teste ponta-a-ponta com pool RLS + pool owner | `internal/server/rls_policy_test.go` (`TestRLSPolicy_EndToEndMotivatingCase`, `setupRLSPolicyFixture`) | Reusar a mesma estrutura de fixture/sub-testes pros testes de `policy` mode, não criar convenção nova |
| `resolveTableRLS` (default de RLS quando campo vem vazio) | `internal/dashboard/handler.go:108-113` | Mantém default `"enabled"` — `"policy"` nunca é escolhido implicitamente, só explicitamente pelo admin |

### Integration Points

| System | Integration Method |
| ------ | ------------------- |
| Postgres (RLS nativo) | Único enforcement de leitura/escrita em tabelas `policy` — sem filtro Go concorrente |
| `zeep_system.app_tables.rls` (coluna `TEXT`) | Nenhuma migração de schema — `"policy"` é só mais um valor de string aceito |

---

## Components

### `internal/config` — predicados centrais de RLS (novo arquivo `internal/config/rls.go`)

- **Purpose**: única fonte de verdade pra "essa tabela tem owner_id" vs. "essa tabela filtra automaticamente por owner_id" vs. "esse valor de rls é válido"
- **Location**: `internal/config/rls.go`
- **Interfaces**:
  - `ValidRLS(rls string) bool` — `true` para `""`, `"owner"`, `"enabled"`, `"policy"`
  - `HasOwnerColumn(rls string) bool` — `true` para `"owner"`, `"enabled"`, `"policy"` (precisa da coluna `owner_id` e precisa populá-la no INSERT)
  - `AutoScopesByOwner(rls string) bool` — `true` só para `"owner"`, `"enabled"` (filtro automático `WHERE owner_id = $sub`)
- **Dependencies**: nenhuma (só `string`)
- **Reuses**: nada — é a extração do padrão hoje espalhado

### `internal/server` — `resolveOwner` (alteração, não novo componente)

- **Purpose**: separar "valor de owner_id a escrever" de "valor de owner_id a filtrar por"
- **Location**: `internal/server/handler.go:29-38`
- **Mudança de interface**: hoje retorna um único `(ownerID string, ok bool)` usado tanto pro INSERT quanto pro WHERE de list/get/update/delete. Passa a retornar `(ownerID string, ok bool)` onde `ownerID` é sempre o `user.ID` quando `config.HasOwnerColumn(table.RLS)` e há usuário autenticado (cobre `owner`/`enabled`/`policy`) — e os call sites de list/get/update/delete passam a decidir se usam esse valor como filtro checando `config.AutoScopesByOwner(table.RLS)` antes de passar `ownerID` pra `query.Build*`; quando `false`, passam `""` só pra essas quatro chamadas, mantendo a chamada de `query.BuildInsert` sempre com o `ownerID` real.
- **Dependencies**: `internal/config`
- **Reuses**: os 5 call sites existentes (`HandleList`, `HandleCreate`, `HandleGetByID`, `HandleUpdate`, `HandleDelete`), троca mínima de qual variável cada um passa pro builder

### `internal/provisioner` — `createTable`/`addMissingColumns` (alteração)

- **Purpose**: garantir fail-closed nativo desde a criação da tabela em modo `policy`
- **Location**: `internal/provisioner/table.go:96-144` (createTable), `:197-256` (addMissingColumns)
- **Mudança**: troca as duas checagens `rls == "owner" || rls == "enabled"` (linhas 123, 241) por `config.HasOwnerColumn(rls)`; em `createTable`, adiciona um passo pós-`CREATE TABLE` que roda `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` quando `rls == "policy"` (novo — hoje só `table_policies_store.go` faz isso, e só lazy na 1ª policy)
- **Dependencies**: `internal/config`
- **Reuses**: statement idempotente já usado em `table_policies_store.go:142`

### `internal/dashboard` — `UpdateAppTable` (alteração, cobre P3)

- **Purpose**: troca de modo em tabela existente habilita RLS nativo se a tabela for pra `policy` e ainda não tiver
- **Location**: `internal/dashboard/apps_store.go:491`
- **Mudança**: quando o novo `rls` é `"policy"` e o antigo não era, roda o mesmo `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` idempotente antes de retornar — mesma lógica de `createTable`, reaproveitada como helper compartilhado (`provisioner.EnsureRowLevelSecurity(ctx, pool, schema, table)` extraído de `table_policies_store.go:140-146` pros três call sites usarem)
- **Dependencies**: `internal/provisioner`

### `internal/dashboard` — `validateTableInput`, `resolveTableRLS` (alteração)

- **Purpose**: validação de enum (RLSP-09) e reconhecimento do modo novo nos dois pontos que hoje só checam `owner`/`enabled` (RLSP-10)
- **Location**: `internal/dashboard/handler.go:108-113` (resolveTableRLS), `:119-134` (validateTableInput)
- **Mudança**: `validateTableInput` chama `config.ValidRLS(t.RLS)` antes de qualquer outra checagem; troca `t.RLS == "enabled" || t.RLS == "owner"` (linha 132) por `config.HasOwnerColumn(t.RLS)`

### `internal/provisioner/policy.go` — `colByName` (alteração)

- **Purpose**: liberar `owner_id` como coluna referenciável em cláusula de policy (RLSP-05)
- **Location**: `internal/provisioner/policy.go:152-155` (`BuildPolicySQL`, onde `colByName` é montado a partir de `tableColumns`)
- **Mudança**: após o loop que popula `colByName` a partir de `tableColumns`, adicionar incondicionalmente `colByName["owner_id"] = config.ColumnConfig{Name: "owner_id", Type: "uuid"}` — disponível em qualquer tabela com RLS habilitado (owner/enabled/policy), já que a coluna sempre existe nessas três. `translateClause`/`translateOperand` não mudam — já lidam com qualquer entrada de `colByName`.

### `internal/docs/generator.go` — `buildResponseSchema` (alteração)

- **Purpose**: expor `owner_id` no schema OpenAPI também pra tabelas `policy` (RLSP-10 / edge case)
- **Location**: `internal/docs/generator.go:264`
- **Mudança**: troca `table.RLS == "owner"` por `config.HasOwnerColumn(table.RLS)`

### `internal/dashboard/handler.go:1899` — Data Browser (alteração)

- **Purpose**: mesma exposição de coluna, superfície interna
- **Mudança**: troca `t.RLS == "owner"` por `config.HasOwnerColumn(t.RLS)` — nota: hoje esse ponto **já está errado pra `"enabled"`** (só cobre `"owner"`), então esta troca também corrige um gap pré-existente não coberto pelo spec original de `end-user-row-policies`, fora do escopo desta feature mas corrigido de graça pela mesma troca de predicado.

---

## Data Models

Nenhum modelo novo. `zeep_system.app_tables.rls` continua `TEXT`; nenhuma migração de banco.

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
| --------------- | -------- | ------------ |
| `rls` com valor não reconhecido (`"disabled"`, typo) | `validateTableInput` rejeita antes de chegar no provisionador (`config.ValidRLS`) | 400 com mensagem clara listando os valores aceitos |
| Tabela `policy` sem nenhuma policy de select, usuário final consulta | Nenhum erro — Postgres nega nativamente, API retorna `200 []` (lista vazia), consistente com o comportamento de uma tabela com dados mas filtro que não bate nada | Lista vazia, sem 403 — documentado no spec (AC P1-2) como decisão consciente, não bug |
| Admin troca `enabled` → `policy` numa tabela sem nenhuma policy ainda | RLS já habilitado (era `enabled`, se já tinha ao menos 1 policy) ou passa a ser habilitado agora — de qualquer forma, fail-closed nativo garante que a troca nunca abre a tabela geral por acidente | Aviso no Dashboard antes de confirmar (RLSP-07); depois da troca, tabela sem policy vira "todos veem `[]`", não "todos veem tudo" |
| Policy clause referenciando `owner_id` com operador incompatível (`LIKE` num `uuid`) | Mesma validação de tipo já existente em `translateOperand`/`claimExpr`/`literalExpr` — nenhum código novo, `owner_id` só entra no mesmo `colByName` que já tem essa checagem pras outras colunas `uuid` | Erro 400 do builder de policy, igual a hoje pra outras colunas `uuid` |

---

## Risks & Concerns

| Concern | Location (file:line) | Impact | Mitigation |
| ------- | --------------------- | ------ | ---------- |
| Nenhuma validação de enum pra `rls` existe hoje em lugar nenhum — achado pré-existente do agente de exploração, não introduzido por esta feature | `internal/config/types.go:44`, confirmado por `internal/dashboard/table_rls_test.go:16` aceitando `"disabled"` sem erro | Typo num valor de `rls` cai silenciosamente no modo público (sem filtro nenhum) — risco de segurança real, independente desta feature | Corrigido como parte do RLSP-09/`config.ValidRLS`, chamado em `validateTableInput` antes de qualquer outra checagem |
| `resolveOwner` hoje conflaciona "coluna existe" com "filtro aplicado" — é a causa raiz do bug de INSERT quebrado se `"policy"` só fosse adicionado como mais um literal na mesma condição | `internal/server/handler.go:29-38` | Sem a separação em dois predicados, todo INSERT em tabela `policy` violaria a constraint `NOT NULL` de `owner_id` (`BuildInsert`, `internal/query/builder.go:246-250`, só popula quando `ownerID != ""`) | Endereçado pela Abordagem B — dois predicados distintos, call sites de INSERT sempre recebem o `ownerID` real |
| `ENABLE ROW LEVEL SECURITY` na criação da tabela, hoje só acontece lazy na 1ª policy (`table_policies_store.go:133-146`) | `internal/provisioner/table.go:96-144` (ausente hoje) | Sem essa mudança, tabela `policy` recém-criada ficaria com RLS desligado — sem filtro de app (por design) e sem enforcement do Postgres (bug) = totalmente aberta até a 1ª policy | `createTable` roda o `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` incondicionalmente pra `rls == "policy"`, dentro da mesma transação da criação da tabela |
| `internal/dashboard/handler.go:1899` (Data Browser) já está incompleto hoje — só reconhece `"owner"`, não `"enabled"` | `internal/dashboard/handler.go:1899` | Data Browser hoje não mostra `owner_id` pra tabelas `"enabled"`, gap pré-existente fora do escopo original desta feature | Corrigido de graça pela troca pra `config.HasOwnerColumn` (RLSP-10), sem tarefa extra dedicada |
| Nenhum teste automatizado hoje cobre "tabela com policy mas nenhuma select policy retorna vazio" de ponta a ponta contra Postgres real — só verificado manualmente na demo (`blog-demo`) | `internal/server/rls_policy_test.go` (ausente esse caso específico) | Regressão futura no fail-closed passaria sem ser pega | Task dedicada de teste de integração (Postgres real, `TEST_DATABASE_URL`) seguindo o padrão de `rls_policy_test.go`, coberta na fase Tasks |

---

## Tech Decisions

| Decision | Choice | Rationale |
| -------- | ------ | --------- |
| Onde centralizar os predicados de RLS | `internal/config` (novo arquivo `rls.go`), não `internal/registry` nem `internal/server` | `internal/config` já é importado por `provisioner`, `registry`, `dashboard`, `docs` sem risco de ciclo — é o único pacote comum a todos os 6 call sites |
| `ENABLE ROW LEVEL SECURITY` em `createTable` vs. só em `table_policies_store.go` | Ambos os pontos continuam existindo (idempotente) | `createTable` cobre o fail-closed desde o instante zero pra tabelas `policy`; `table_policies_store.go` continua sendo o ponto de entrada pra `owner`/`enabled` que optam por adicionar policies depois — nenhuma duplicação de lógica de negócio, só uma chamada extra idempotente |
| Helper compartilhado `EnsureRowLevelSecurity` | Extraído de `table_policies_store.go:140-146` pra `internal/provisioner`, usado por `createTable`, `UpdateAppTable` e `table_policies_store.go` | Três call sites da mesma operação DDL — evita triplicar o `fmt.Sprintf("ALTER TABLE ...")` |

> Nenhuma decisão aqui estabelece convenção de projeto nova que exija entrada em `.specs/project/STATE.md` — são decisões locais desta feature, extensão do padrão já estabelecido em `end-user-row-policies`.

---

## Tips (referência, não normativo)

Nenhuma nota adicional além das já registradas no spec.
