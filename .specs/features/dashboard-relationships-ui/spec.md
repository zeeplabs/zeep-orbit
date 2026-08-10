# Dashboard UI for Foreign Keys and Indexes Specification

## Problem Statement

O backend já suporta `references` (foreign key) e `indexes` de ponta a ponta: `internal/config.ColumnConfig.References`/`config.TableConfig.Indexes`, validação (`config.ValidateTables`, chamada por `validateTableInput` em `internal/dashboard/handler.go`), persistência (`zeep_system.app_tables.indexes`, coluna JSONB) e provisionamento real no Postgres (`internal/provisioner`: `columnDDL` emite `REFERENCES ... ON DELETE ...`, `ensureIndexes`, `topoSortTables`). Spec de origem: `.specs/features/schema-relationships-and-indexes/`.

Nada disso é acessível pela UI. O único formulário real de criar/editar tabela é `internal/dashboard/ui/src/components/TableCard.tsx`, que só expõe por coluna: nome, tipo (`COLUMN_TYPES`, linha 16-25), `required`, `unique`. Os tipos TypeScript que definem o contrato com a API (`internal/dashboard/ui/src/lib/api.ts`, `ColumnDef`/`TableDef`, linhas 10-23) não têm campo `references` nem `indexes` — mesmo que o backend aceite esses campos no JSON, o formulário nunca os envia nem os lê de volta.

Resultado: um usuário só consegue declarar FK ou índice via chamada HTTP direta (`curl`/Postman), nunca pelo Dashboard, que é o único caminho de uso do produto hoje (`.specs/features/schema-relationships-and-indexes/` e o histórico desta conversa confirmam que o fluxo YAML foi removido — Dashboard + futuro MCP são as únicas vias).

## Goals

- [x] `ColumnDef`/`TableDef` (TypeScript) ganham os campos `references`/`indexes`, espelhando `config.ReferenceConfig`/`config.IndexConfig` do backend
- [x] `TableCard.tsx` permite declarar, por coluna, uma referência (tabela alvo, coluna alvo, `on_delete`) usando as tabelas já existentes do mesmo app
- [x] `TableCard.tsx` permite declarar, por tabela, uma lista de índices (nome, colunas, `unique`)
- [x] Erros de validação retornados pelo backend (referência a tabela/coluna inexistente, ciclo, índice em coluna inexistente, nome de índice duplicado) aparecem de forma legível no formulário, reaproveitando o padrão de erro já existente (`setError`, linha 73/172/298 de `TableCard.tsx`)
- [x] Visualização somente-leitura (tabela não em edição) mostra as FKs e índices existentes, não só nome/RLS

## Out of Scope

| Feature | Reason |
|---|---|
| Editor visual de diagrama ER (arrastar linhas entre tabelas) | Complexidade de UI desproporcional ao pedido atual; formulário simples (selects) resolve o caso de uso |
| Autocomplete/preview de dados ao escolher tabela/coluna referenciada | Requer nova chamada de API; fora do escopo desta spec, que é só expor o que o backend já aceita |
| Edição de FK/índice em lote (múltiplas tabelas de uma vez) | `TableCard.tsx` já opera uma tabela por vez; manter o padrão existente |
| Drop de índice removido do formulário refletir no banco | Já é decisão de produto do backend (`schema-relationships-and-indexes/spec.md`): remover do formulário não deve apagar o índice existente — fora de escopo mudar isso agora |
| Suporte a `on_delete` diferente por dashboard vs API direta | Mesmo contrato para os dois — não há distinção de comportamento |

---

## User Stories

### P1: Declarar foreign key ao criar/editar coluna ⭐ MVP

**User Story**: Como usuário criando ou editando uma tabela no Dashboard, quero marcar uma coluna como referência a outra tabela do mesmo app (e escolher o comportamento de `on_delete`), para não precisar chamar a API manualmente pra ter uma FK real no banco.

**Why P1**: É o núcleo do problema — sem isso, a feature de FK do backend é inacessível pelo único caminho de uso do produto.

**Acceptance Criteria**:

1. WHEN o usuário está editando uma coluna THEN o formulário SHALL oferecer uma opção "Referenciar outra tabela" que, quando ativada, mostra: select de tabela (lista das outras tabelas do mesmo app, via `app.tables` já carregado por `useApp`), select de coluna alvo (default `id`, ou outra coluna marcada `unique` na tabela escolhida), select de `on_delete` (`cascade`, `restrict`, `set_null`, `no_action`, default `no_action`).
2. WHEN a tabela referenciada ainda não existe no app (por exemplo, ambas estão sendo criadas na mesma sessão de edição) THEN o select de tabela SHALL listar só tabelas já salvas (com `id`), não rascunhos — referência a uma tabela ainda não persistida não é suportada pelo backend nesta iteração.
3. WHEN o usuário salva a tabela THEN o `references` de cada coluna SHALL ser enviado no payload de `POST`/`PUT /dashboard/api/apps/{id}/tables` exatamente no formato que `config.ReferenceConfig` espera (`table`, `column`, `on_delete`).
4. WHEN a tabela é carregada para edição e já tem uma coluna com FK THEN o formulário SHALL pré-popular a referência existente (não perder o dado ao reabrir o formulário).

**Independent Test**: Criar tabela `orders` com coluna `customer_id` referenciando `customers.id` com `on_delete: cascade` só pelo Dashboard; confirmar via `psql` que a constraint `REFERENCES ... ON DELETE CASCADE` existe.

---

### P1: Declarar índices ao criar/editar tabela ⭐ MVP

**User Story**: Como usuário, quero adicionar um ou mais índices (simples ou compostos, únicos ou não) a uma tabela pelo Dashboard, sem precisar da API direta.

**Why P1**: Mesma motivação da FK — feature pronta no backend, inacessível na prática.

**Acceptance Criteria**:

1. WHEN o usuário está editando uma tabela THEN o formulário SHALL ter uma seção "Índices" com botão "Adicionar índice", cada índice com: campo nome, seleção multi-coluna (checkboxes ou multi-select das colunas já declaradas na tabela), toggle `unique`.
2. WHEN o usuário salva a tabela THEN `indexes` SHALL ser enviado no payload no formato que `config.IndexConfig` espera (`name`, `columns`, `unique`).
3. WHEN a tabela é carregada para edição THEN os índices já existentes SHALL aparecer pré-populados.

**Independent Test**: Criar tabela `users` com índice único composto em `(org_id, email)` só pelo Dashboard; confirmar via `pg_indexes` que o índice existe com `UNIQUE`.

---

### P2: Erros de validação do backend aparecem de forma legível

**User Story**: Como usuário, quando minha FK ou índice é inválido (tabela/coluna inexistente, ciclo, nome duplicado), quero ver a mensagem de erro específica no formulário, não um erro genérico.

**Why P2**: Sem isso a feature funciona no caminho feliz, mas falha silenciosamente/confusamente no caminho de erro — `config.ValidateTables` já produz mensagens específicas (`internal/config/validate.go`), só falta a UI não abafar essas mensagens.

**Acceptance Criteria**:

1. WHEN a API retorna 400 com uma mensagem de `config.ValidateTables` (ex.: "references unknown table") THEN o formulário SHALL mostrar essa mensagem tal como veio (já é o comportamento de `apiFetch`, que usa `body.error` — confirmar que nada trunca ou substitui essa string no caminho de FK/índice).

**Independent Test**: Tentar salvar uma FK apontando pra tabela inexistente; erro exibido no formulário é o texto vindo da API, não um "Erro ao salvar tabela" genérico.

---

## Constraints Técnicas

- Não alterar contrato de API (`ColumnConfig`/`IndexConfig` no Go já existem e estão corretos) — esta spec é só frontend.
- Seguir o estilo visual e de estado já usado em `TableCard.tsx` (Tailwind inline, `useState` local, sem introduzir gerenciador de estado novo).
- Achado incidental, fora de escopo desta spec, mas registrado para não se perder: `allowedTypes` em `handler.go:65-68` aceita `"numeric"` como tipo válido (é o que `COLUMN_TYPES` do front, linha 16-25 de `TableCard.tsx`, oferece), mas `provisioner.pgType()` (`internal/provisioner/table.go:20-42`) só trata `"decimal"` — `"numeric"` cai no `default` e vira `TEXT` silenciosamente. Ou seja, hoje uma coluna criada como `numeric` pelo Dashboard é fisicamente `TEXT` no Postgres, sem erro nem aviso. Não corrigir aqui (não é FK/índice); reportar como bug separado.
