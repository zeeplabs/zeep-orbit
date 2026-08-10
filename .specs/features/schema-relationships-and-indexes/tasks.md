# Tasks: Schema Relationships and Indexes

**Spec**: `.specs/features/schema-relationships-and-indexes/spec.md`
**Design**: `.specs/features/schema-relationships-and-indexes/design.md`
**Status**: Verified (T-01..T-11) — verificado 2026-08-10: `config.ReferenceConfig`/`IndexConfig` (`internal/config/types.go:46,62`), `validateReference`/`validateIndexes`/`detectReferenceCycle` (`internal/config/validate.go:135,191,218`), `topoSortTables` (`internal/provisioner/topsort.go:14`), `ensureIndexes` (`internal/provisioner/index.go:16`), FK emitida em `columnDDL` (`internal/provisioner/table.go:88-89`), `checkDependents` + guard em `DropTable` (`internal/provisioner/table.go:150,180-181`), testes em `internal/provisioner/relationships_test.go`/`topsort_test.go` e `internal/config/validate_test.go`. **T-12 obsoleta**: o fluxo `apps.yaml`/`apply` foi removido do produto (AGENTS §1) — não há exemplo de YAML para documentar; schema é gerenciado só pelo Dashboard.

> Convenção de Gate: não há `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`), mesmo critério usado em `mvp-core/tasks.md` e `frontend-app-entity/tasks.md`.

---

## Execution Plan

```
Fase 1: Config              Fase 2: Validação           Fase 3: Provisioner
┌──────────────────┐        ┌──────────────────┐        ┌───────────────────────┐
│ T-01 ReferenceConfig│─────▶│ T-03 ValidateSchema│──────▶│ T-06 topoSortTables    │
│ T-02 IndexConfig     │      │ T-04 detecção ciclo │      │ T-07 FK em columnDDL   │
│                      │      │ T-05 índice inválido│      │ T-08 ensureIndexes     │
└──────────────────┘        └──────────────────┘        │ T-09 checkDependents   │
                                                          └───────────────────────┘
                                                                    │
                                                                    ▼
                                                         Fase 4: Integração
                                                    ┌────────────────────────────┐
                                                    │ T-10 apply incremental FK   │
                                                    │ T-11 testes de integração   │
                                                    │ T-12 documentação apps.yaml │
                                                    └────────────────────────────┘
```

---

## T-01: Adicionar `ReferenceConfig` e campo `References` em `ColumnConfig` [x]

**Arquivo**: `internal/config/types.go`

- Adicionar struct `ReferenceConfig{Table, Column, OnDelete string}`.
- Adicionar campo `References *ReferenceConfig` em `ColumnConfig`, `omitempty` em JSON/YAML.

**Acceptance**: `go build ./...` passa; YAML de exemplo com `references: {table: x, column: id}` faz parse sem erro.

---

## T-02: Adicionar `IndexConfig` e campo `Indexes` em `TableConfig` [x]

**Arquivo**: `internal/config/types.go`

- Adicionar struct `IndexConfig{Name string, Columns []string, Unique bool}`.
- Adicionar campo `Indexes []IndexConfig` em `TableConfig`, `omitempty`.

**Acceptance**: `go build ./...` passa; YAML com `indexes: [{name: idx_x, columns: [a, b]}]` faz parse sem erro.

**Depende de**: nenhuma (paralelo a T-01).

---

## T-03: Criar `internal/config.ValidateSchema` — referência existe e coluna alvo é PK/unique [x]

**Arquivo**: novo `internal/config/validate.go`

- Para cada app, para cada coluna com `References != nil`: checar que `References.Table` existe entre as tabelas do app; checar que `References.Column` é `"id"` ou é coluna com `Unique: true` na tabela alvo.
- Checar `References.OnDelete` (se não vazio) é um de `cascade|restrict|set_null|no_action`.
- Checar `OnDelete == "set_null"` implica `Required == false` na coluna de origem.
- Agregar todos os erros encontrados (não retornar no primeiro).

**Acceptance Criteria** (do spec.md, P1 "FK entre tabelas"):
1. Referência a tabela inexistente → erro citando tabela/coluna de origem.
2. Referência a coluna não-PK e não-unique → erro.
3. `on_delete` inválido → erro.
4. `on_delete: set_null` em coluna `required: true` → erro.

**Depende de**: T-01.

---

## T-04: Estender `ValidateSchema` — detecção de ciclo entre tabelas [x]

**Arquivo**: `internal/config/validate.go`

- Montar grafo de dependência (tabela → tabelas que ela referencia) por app.
- DFS com detecção de back-edge; se ciclo encontrado, erro citando a cadeia de tabelas do ciclo.

**Acceptance**: `apps.yaml` com A→B→A (direto ou transitivo) falha o `apply` com erro citando o ciclo, antes de qualquer DDL.

**Depende de**: T-03 (mesma função, mesmo arquivo).

---

## T-05: Estender `ValidateSchema` — índice: coluna existe e nome único no app [x]

**Arquivo**: `internal/config/validate.go`

- Para cada `IndexConfig` de cada tabela: checar todas as `Columns` existem na tabela.
- Checar `Name` de índice é único entre todos os índices declarados no mesmo app (mesmo schema PostgreSQL).

**Acceptance Criteria** (do spec.md, P1 "índice"):
4. Coluna inexistente citada em índice → erro antes de DDL.
7 (design.md Components): nome de índice duplicado no mesmo app → erro.

**Depende de**: T-02.

---

## T-06: Criar `internal/provisioner.topoSortTables` [x]

**Arquivo**: novo `internal/provisioner/topsort.go`

- Kahn's algorithm sobre `[]config.TableConfig` de um app, usando `ColumnConfig.References.Table` como aresta de dependência.
- Retorna slice reordenado (referenciada antes de quem referencia).
- Retorna erro defensivo se detectar ciclo (segunda camada de defesa — `ValidateSchema` já deveria ter barrado antes).

**Acceptance**: tabela `orders` declarada antes de `customers` no slice de entrada; `orders.customer_id → customers.id`; saída tem `customers` antes de `orders`.

**Depende de**: T-01.

---

## T-07: Estender `columnDDL`/`createTable`/`addMissingColumns` para emitir `REFERENCES ... ON DELETE ...` [x]

**Arquivo**: `internal/provisioner/table.go`

- `columnDDL`: se `col.References != nil`, apender `REFERENCES %q(%q) ON DELETE %s` traduzindo `OnDelete` (`cascade→CASCADE`, `restrict→RESTRICT`, `set_null→SET NULL`, `no_action`/vazio→`NO ACTION`).
- `createTable`: usa `topoSortTables` (T-06) antes de iterar `app.Tables` — mudança feita em `provisioner.go:61`, não em `table.go` diretamente.
- `addMissingColumns`: mesma emissão de `REFERENCES` quando FK é adicionada em apply incremental (coluna nova com `References` em tabela já existente).

**Acceptance Criteria** (do spec.md, P1 "FK"):
1. `references` gera `REFERENCES "customers"("id")`.
2. `on_delete` traduzido corretamente para os 4 valores.
Teste de integração: `DELETE` em `customers` propaga `CASCADE` pra `orders` (query direta no Postgres de teste).

**Depende de**: T-03, T-06.

---

## T-08: Criar `internal/provisioner.ensureIndexes` [x]

**Arquivo**: novo `internal/provisioner/index.go`

- Para cada `IndexConfig` de uma tabela: gerar `CREATE [UNIQUE] INDEX IF NOT EXISTS %q ON %q.%q (%s)` com colunas na ordem declarada.
- Chamado logo após `createTable`/`addMissingColumns` no loop principal do provisioner.

**Acceptance Criteria** (do spec.md, P1 "índice"):
1. Índice simples gerado corretamente.
2. Índice composto gerado na ordem declarada.
3. `unique: true` gera `CREATE UNIQUE INDEX`.
5. `apply` rodado 2x não falha nem duplica (idempotência via `IF NOT EXISTS`).
6. Remover índice do YAML e reaplicar mantém o índice existente no banco (nenhum `DROP` implícito).

**Depende de**: T-02, T-05.

---

## T-09: Criar `internal/provisioner.checkDependents` e integrar em `DropTable` [x]

**Arquivo**: `internal/provisioner/table.go` (`DropTable`, linha atual 116-122)

- Antes do `DROP TABLE IF EXISTS`: consultar dependentes via `information_schema` (ou grafo de FK já montado a partir do `Config`, se disponível no fluxo de chamada) — outras tabelas do mesmo schema que referenciam a que está sendo removida.
- Se houver dependente, retornar erro citando tabela/coluna dependente, sem executar o `DROP`.

**Acceptance Criteria** (do spec.md, P2 "remover tabela referenciada"):
1. `customers` referenciada por `orders.customer_id`; remover `customers` do YAML falha citando `orders.customer_id`, sem rodar `DropTable`.
2. `customers` só é removível depois que a FK dependente for removida primeiro (apply anterior ou mesmo YAML sem a referência).

**Depende de**: T-06 (reusa grafo, se prático) ou implementação independente via `information_schema`.

---

## T-10: Apply incremental — ordem entre criar tabela nova e adicionar FK em tabela existente [x]

**Arquivo**: `internal/provisioner/provisioner.go`

- Garantir que quando uma coluna com FK nova é adicionada (via `addMissingColumns`) apontando para uma tabela que também é criada no mesmo apply, a tabela referenciada é criada **antes** do `ALTER TABLE ADD COLUMN` que traz a FK — risco identificado no design.md ("Riscos e Perguntas em Aberto").

**Acceptance**: teste de integração cobrindo esse caso específico (tabela existente ganha FK nova para tabela recém-criada no mesmo `apply`).

**Depende de**: T-06, T-07.

---

## T-11: Testes de integração (Postgres real, não mock) [x]

**Arquivo**: `internal/provisioner/*_test.go` (seguir padrão de teste com banco real já usado no pacote, se existir — confirmar durante implementação)

Cobrir todos os "Independent Test" do spec.md:
- FK simples + `ON DELETE CASCADE` funcional.
- Ordem de criação resolvida mesmo com tabelas fora de ordem no YAML.
- Ciclo rejeitado antes de DDL.
- Índice simples e composto, único e não-único.
- Idempotência de `apply` rodado 2x.
- Remoção de tabela referenciada rejeitada com erro claro.

**Depende de**: T-01 a T-10 (fase final).

---

## T-12: Documentar `references`/`indexes` no exemplo de `apps.yaml` do repo

**Arquivo**: README ou exemplo de config citado no ROADMAP/PROJECT (confirmar localização exata durante implementação — não assumir caminho sem checar)

- Adicionar exemplo com FK e índice, igual ao usado em design.md.

**Depende de**: T-11 (documentar só depois de validado por teste).
