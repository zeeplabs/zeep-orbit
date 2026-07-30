# Tasks: Dashboard UI for Foreign Keys and Indexes

**Spec**: `.specs/features/dashboard-relationships-ui/spec.md`
**Design**: `.specs/features/dashboard-relationships-ui/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo. Frontend não tem suíte de testes automatizados hoje (`internal/dashboard/ui`) — verificação é manual (rodar `npm run dev`, testar fluxo no browser), como já é o padrão do restante da UI.

---

## Execution Plan

```
Fase 1: Tipos              Fase 2: Formulário FK        Fase 3: Formulário índice   Fase 4: Verificação
┌──────────────────┐       ┌───────────────────┐        ┌──────────────────────┐   ┌────────────────────┐
│ T-01 ReferenceDef │──────▶│ T-03 UI referência │        │ T-05 estado indexes   │──▶│ T-07 leitura FK/idx │
│ T-02 IndexDef      │      │ T-04 payload save   │       │ T-06 UI índices        │   │ T-08 manual e2e     │
└──────────────────┘       └───────────────────┘        └──────────────────────┘   └────────────────────┘
```

---

## T-01: Adicionar `ReferenceDef` e campo `references` em `ColumnDef`

**Arquivo**: `internal/dashboard/ui/src/lib/api.ts`

- `export interface ReferenceDef { table: string; column: string; on_delete?: '' | 'cascade' | 'restrict' | 'set_null' | 'no_action' }`.
- `ColumnDef.references?: ReferenceDef | null`.

**Acceptance**: `npm run build` (dashboard UI) passa sem erro de tipo.

---

## T-02: Adicionar `IndexDef` e campo `indexes` em `TableDef`

**Arquivo**: `internal/dashboard/ui/src/lib/api.ts`

- `export interface IndexDef { name: string; columns: string[]; unique?: boolean }`.
- `TableDef.indexes?: IndexDef[]`.

**Acceptance**: `npm run build` passa. `Depende de`: nenhuma (paralelo a T-01).

---

## T-03: UI de referência por coluna em `TableCard.tsx`

**Arquivo**: `internal/dashboard/ui/src/components/TableCard.tsx`

- Nova prop `otherTables: TableDef[]` (tabelas do app já salvas, id truthy, excluindo a própria tabela em edição) — passada de `AppDetailsPage.tsx`.
- Por linha de coluna: toggle "Referenciar outra tabela"; quando ativo, mostra `Select` de tabela (`otherTables`), `Select` de coluna alvo (`"id"` + colunas `unique` da tabela escolhida), `Select` de `on_delete`.
- `updateColumn` já existente recebe o novo campo `references` via `Partial<ColumnDef>` — sem mudança de assinatura.
- Ao entrar em modo edição (`enterEdit`), `references` de cada coluna existente é copiado junto com o resto de `col` (já é `{ ...c }` — spread cobre o campo novo automaticamente).

**Acceptance Criteria** (spec.md, User Story P1 FK, AC1/AC2/AC4):
1. Toggle liga/desliga a seção de referência sem perder os outros campos da coluna.
2. Select de tabela só lista tabelas com `id` (não rascunhos).
3. Select de coluna alvo atualiza ao trocar a tabela escolhida.
4. Reabrir uma tabela com FK existente mostra a referência já preenchida.

**Depende de**: T-01.

---

## T-04: Enviar `references`/incluir no payload de save

**Arquivo**: `internal/dashboard/ui/src/components/TableCard.tsx`

- `save()` (linha 97-123) já serializa o objeto `columns`/tabela inteiro — confirmar que `references` (dentro de cada `ColumnDef`) e `indexes` (T-06) trafegam sem transformação extra pro `onCreate`/`onUpdate`.
- Nenhuma mudança em `lib/api.ts` nas funções `useCreateAppTable`/`useUpdateAppTable` — o body já é `JSON.stringify(table)`/`JSON.stringify(input)` (linhas 119/139), cobre campos novos automaticamente.

**Acceptance Criteria** (spec.md AC3): payload de `POST`/`PUT` inclui `references` de cada coluna no formato `{ table, column, on_delete }`.

**Depende de**: T-03.

---

## T-05: Estado `indexes` em `TableCard.tsx`

**Arquivo**: `internal/dashboard/ui/src/components/TableCard.tsx`

- `const [indexes, setIndexes] = useState<IndexDef[]>(table.indexes ?? [])`, mesmo padrão de `columns` (linha 68-70).
- `addIndex`/`removeIndex`/`updateIndex`, espelhando `addColumn`/`removeColumn`/`updateColumn`.
- `enterEdit`/`cancel` resetam `indexes` a partir de `table.indexes` igual fazem com `columns`.

**Acceptance**: estado de índices reseta corretamente ao cancelar edição, igual ao de colunas.

**Depende de**: T-02.

---

## T-06: UI de índices por tabela em `TableCard.tsx`

**Arquivo**: `internal/dashboard/ui/src/components/TableCard.tsx`

- Nova seção "Índices" abaixo da lista de colunas, antes do botão "Salvar tabela".
- Por índice: input de nome, seleção multi-coluna a partir do estado local `columns` (não `table.columns` — permite referenciar coluna adicionada na mesma sessão), toggle `unique`.
- Botão "Adicionar índice" (mesmo estilo de "Adicionar Coluna", linha 288-296).

**Acceptance Criteria** (spec.md, User Story P1 índices, AC1/AC2/AC3):
1. Adicionar/remover índice funciona sem afetar colunas.
2. Multi-select de colunas só oferece colunas já nomeadas na tabela.
3. Reabrir tabela com índice existente mostra índice pré-populado.

**Depende de**: T-05.

---

## T-07: Exibição somente-leitura de FK/índices

**Arquivo**: `internal/dashboard/ui/src/components/TableCard.tsx`

- No card colapsado (`!editing`, linha 138-174): adicionar lista compacta de FKs (`"{coluna} → {tabela}.{coluna_alvo} ({on_delete})"`) e índices (`"{nome} ({colunas}) UNIQUE?"`) abaixo do nome/badge RLS já existentes.

**Acceptance Criteria** (spec.md Goal 5): card fechado mostra FK/índices existentes sem precisar entrar em modo edição.

**Depende de**: T-03, T-06.

---

## T-08: Verificação manual end-to-end

Sem suíte de teste automatizado no frontend hoje — verificação via `npm run dev` (dashboard UI) contra API real:

1. Criar tabela `customers` (sem FK).
2. Criar tabela `orders` com coluna `customer_id` referenciando `customers.id`, `on_delete: cascade` — confirmar via `psql`/`pg_indexes`/`information_schema` que a constraint existe.
3. Adicionar índice único composto numa tabela — confirmar via `pg_indexes`.
4. Forçar erro (referenciar tabela inexistente via manipulação momentânea, ou nome de índice duplicado) — confirmar que a mensagem de erro do backend aparece legível no formulário (spec.md P2).
5. Reabrir as tabelas criadas — confirmar que FK e índices aparecem pré-populados no formulário e no card colapsado.

**Depende de**: T-01 a T-07 (fase final).
