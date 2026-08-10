# Schema Relationships and Indexes Specification

## Problem Statement

Hoje o schema builder do app-backend (`internal/config.TableConfig`/`ColumnConfig` + `internal/provisioner`) só suporta colunas simples com `NOT NULL`/`DEFAULT`/`UNIQUE` (`internal/provisioner/table.go:45-61`). Não existe forma de declarar foreign keys entre tabelas de um mesmo app nem índices — nem simples (1 coluna) nem compostos. O único FK do sistema é fixo e interno (`owner_id → _auth_users.id`, gerado automaticamente pelo RLS, `internal/provisioner/table.go:92`), não é um recurso configurável pelo usuário. Sem relacionamento e índice, apps com dados relacionais (ex: `orders` → `customers`) não conseguem expressar integridade referencial nem performance de consulta via `apps.yaml` — a única alternativa hoje seria mexer direto no banco por fora do fluxo `zeep apply`, quebrando a garantia de "schema-in → REST-out" que é o princípio central do produto (`.specs/project/PROJECT.md`).

## Goals

- [x] Usuário declara FK de uma coluna para outra tabela do mesmo app via `apps.yaml`, com `on_delete` explícito
- [x] Usuário declara índice (simples ou composto, único ou não) em uma tabela via `apps.yaml`
- [x] `zeep apply` cria tabelas na ordem de dependência correta quando há FK entre elas (topological sort)
- [x] `zeep apply` cria/atualiza índices de forma idempotente, igual já acontece com colunas
- [x] Remover uma tabela referenciada por FK de outra falha com erro claro, em vez de quebrar a integridade do banco silenciosamente
- [x] Erros de validação de schema (referência inexistente, dependência circular) são pega antes de qualquer DDL rodar

## Out of Scope

| Feature | Reason |
|---|---|
| FK entre apps diferentes | Cada app tem schema PostgreSQL isolado (schema-per-app) — FK cross-schema quebra a garantia de isolamento que é decisão arquitetural existente (ver PROJECT.md) |
| Composite foreign keys (FK sobre múltiplas colunas) | Nenhum caso de uso identificado ainda; MVP cobre FK de 1 coluna |
| `CHECK` constraints arbitrárias | Fora do escopo desta spec — cuidar de constraint de valor é outra feature |
| Constraints `DEFERRABLE` | Complexidade extra sem necessidade demonstrada; FK sempre validada imediatamente |
| Editor visual (drag-and-drop) de relacionamento no dashboard | Esta spec cobre só `apps.yaml` + provisioner; UI visual é sub-feature futura, se houver demanda |
| Rollback automático de schema (versionamento) | Já listado como "Deferred Idea" em `.specs/project/STATE.md` — independente desta spec |
| Índices parciais (`WHERE` clause) ou por expressão | MVP cobre índice normal (B-tree) sobre coluna(s); parcial/expressão fica para iteração futura se pedido |

---

## User Stories

### P1: Usuário declara foreign key entre tabelas do mesmo app ⭐ MVP

**User Story**: Como usuário criando um app com dados relacionais, quero declarar que uma coluna referencia a linha de outra tabela do mesmo app, para que o banco garanta integridade referencial sem eu precisar escrever SQL manual.

**Why P1**: É a capacidade central da spec — sem FK configurável não existe "relacionamento" no schema builder.

**Acceptance Criteria**:

1. WHEN o usuário declara uma coluna com `references: {table: "customers", column: "id"}` THEN o sistema SHALL gerar `REFERENCES "customers"("id")` na `CREATE TABLE`/`ALTER TABLE` da coluna.
2. WHEN o usuário declara `on_delete` como `cascade`, `restrict`, `set_null` ou `no_action` THEN o sistema SHALL traduzir para `ON DELETE CASCADE`/`RESTRICT`/`SET NULL`/`NO ACTION` respectivamente; ausência do campo SHALL default para `NO ACTION` (comportamento padrão do PostgreSQL, explícito na validação).
3. WHEN a tabela referenciada em `references.table` não existe em nenhuma tabela do mesmo app THEN a validação SHALL rejeitar o `apply` inteiro antes de qualquer DDL, com erro citando a tabela e coluna de origem.
4. WHEN a coluna referenciada em `references.column` não é `id` (PK) nem tem `unique: true` na tabela alvo THEN a validação SHALL rejeitar — PostgreSQL exige que a coluna referenciada tenha constraint UNIQUE ou seja PK.
5. WHEN `on_delete: set_null` é declarado em uma coluna com `required: true` THEN a validação SHALL rejeitar — coluna NOT NULL não pode receber SET NULL.

**Independent Test**: `apps.yaml` com tabela `customers` (PK `id`) e tabela `orders` com coluna `customer_id` referenciando `customers.id` com `on_delete: cascade`; `zeep apply` cria ambas as tabelas; `DROP` de uma linha em `customers` remove as `orders` associadas via cascade no PostgreSQL (verificado por query direta, não pela API REST).

---

### P1: `zeep apply` resolve ordem de criação por dependência de FK ⭐ MVP

**User Story**: Como usuário, quero declarar tabelas em qualquer ordem no `apps.yaml`, e o sistema descobrir a ordem certa de criação sozinho, para eu não precisar saber de detalhe de implementação do provisioner.

**Why P1**: Sem isso, declarar `orders` antes de `customers` no YAML (ordem natural de leitura, não de dependência) falha com erro de FK para tabela inexistente — hoje o loop em `internal/provisioner/provisioner.go:61` processa `app.Tables` na ordem literal do slice, sem qualquer resolução de dependência.

**Acceptance Criteria**:

1. WHEN o app tem múltiplas tabelas com FK entre si THEN o sistema SHALL ordenar a criação via topological sort, criando a tabela referenciada antes da que a referencia.
2. WHEN existe dependência circular entre tabelas (A referencia B e B referencia A, direta ou transitivamente) THEN a validação SHALL rejeitar o `apply` com erro citando o ciclo encontrado, antes de qualquer DDL.
3. WHEN uma tabela já existe (apply incremental) e uma nova coluna com FK é adicionada apontando para tabela também nova no mesmo apply THEN o sistema SHALL garantir que a tabela referenciada seja criada antes do `ALTER TABLE ADD COLUMN` que adiciona a FK.

**Independent Test**: `apps.yaml` com `orders` declarada **antes** de `customers` no slice YAML, `orders.customer_id` referenciando `customers.id`; `zeep apply` cria `customers` primeiro internamente (verificável via log ou timestamp de `information_schema`) e não falha.

---

### P1: Usuário declara índice em uma ou mais colunas ⭐ MVP

**User Story**: Como usuário, quero declarar um índice sobre uma ou mais colunas de uma tabela, para consultas filtrando por essas colunas serem rápidas sem eu precisar rodar SQL manual.

**Why P1**: Sem índice configurável, toda consulta filtrada por coluna não-PK faz sequential scan — problema de performance real em qualquer tabela com volume, e não há como declarar isso hoje via `apps.yaml`.

**Acceptance Criteria**:

1. WHEN o usuário declara `indexes: [{name: "idx_orders_customer", columns: ["customer_id"]}]` na tabela THEN o sistema SHALL gerar `CREATE INDEX IF NOT EXISTS "idx_orders_customer" ON "schema"."orders" ("customer_id")`.
2. WHEN o usuário declara `columns` com mais de uma coluna THEN o sistema SHALL gerar índice composto na ordem declarada.
3. WHEN o usuário declara `unique: true` no índice THEN o sistema SHALL gerar `CREATE UNIQUE INDEX` em vez de `CREATE INDEX`.
4. WHEN uma coluna citada em `indexes[].columns` não existe na tabela THEN a validação SHALL rejeitar o `apply` antes de qualquer DDL, citando o índice e a coluna inválida.
5. WHEN o índice já existe (nome já presente em `pg_indexes` para aquele schema/tabela) THEN o `apply` SHALL ser idempotente — não recria nem falha, mesmo comportamento hoje aplicado a `addMissingColumns`.
6. WHEN o usuário remove uma entrada de `indexes` de uma tabela já aplicada e roda `apply` de novo THEN o sistema SHALL **manter** o índice existente no banco (mesma política conservadora hoje aplicada a colunas removidas do YAML — não há `DROP` automático implícito; remoção explícita fica para uma capability futura de "drop index", fora do MVP desta spec).

**Independent Test**: `apps.yaml` com tabela `orders` e `indexes: [{name: "idx_orders_status", columns: ["status"]}]`; `zeep apply` roda 2x seguidas sem erro; `SELECT * FROM pg_indexes WHERE indexname = 'idx_orders_status'` retorna 1 linha.

---

### P2: Remover tabela referenciada por FK falha com erro claro

**User Story**: Como usuário, quero que remover do `apps.yaml` uma tabela que é referenciada por FK de outra tabela do mesmo app me avise antes de quebrar o banco, para eu não perder integridade referencial por engano.

**Why P2**: Prático mas não bloqueia o MVP de declarar FK/índice — é proteção contra o caso de borda de remoção, hoje `DropTable` (`internal/provisioner/table.go:116-122`) não faz nenhuma checagem de dependentes.

**Acceptance Criteria**:

1. WHEN o usuário remove do `apps.yaml` uma tabela que ainda é referenciada por FK de outra tabela presente no mesmo `apps.yaml` THEN a validação SHALL rejeitar o `apply` com erro citando a tabela dependente, sem chamar `DropTable`.
2. WHEN o usuário quer remover a tabela referenciada mesmo assim THEN o sistema SHALL exigir que a FK dependente seja removida primeiro (em um apply anterior ou no mesmo YAML, na coluna da tabela dependente) — sem flag de "force drop" no MVP.

**Independent Test**: `apps.yaml` com `orders.customer_id → customers.id`; remover `customers` do YAML mantendo `orders`; `zeep apply` rejeita com erro citando `orders.customer_id` como dependente, `customers` continua existindo no banco.

---

## Constraints Técnicas

- FK só entre tabelas do mesmo app (mesmo schema PostgreSQL) — schema-per-app isolation é decisão arquitetural existente, não revisitada aqui.
- Coluna referenciada por FK precisa ser PK (`id`) ou ter `unique: true` — requisito do PostgreSQL, não de negócio.
- Nomes de índice devem ser únicos por schema (regra do PostgreSQL) — validação deve pegar colisão de nome entre tabelas do mesmo app antes do DDL.
- Toda validação (referência inexistente, ciclo, coluna de índice inexistente, nome de índice duplicado) roda **antes** de qualquer `CREATE`/`ALTER` — mesma garantia "fail-fast" que já existe para o resto do schema builder.
