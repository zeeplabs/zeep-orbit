# Design: Data Browser (DASHBOARD-004)

## Status

Approved 2026-06-27. Read-only MVP. CRUD inline registrado como melhoria futura.

## Escopo

Dashboard admin visualiza dados das tabelas dos apps que possui. Superadmin vê todos os apps.

## UX — Navegação

Sidebar com árvore: sidebar principal mantida, dentro da página do Data Browser um painel esquerdo com lista expansível de apps → tabelas. Clica na tabela → carrega dados no painel direito.

```
┌─────────────────────────────────────────────────────────┐
│  │ Data Browser                                         │
│  │                                                      │
│  │  ▼ app_1                          ┌───┬───┬───┬───┐ │
│  │    ├─ users                       │id │name│...│   │ │
│  │    ├─ orders                      ├───┼───┼───┼───┤ │
│  │    └─ products                    │   │   │   │   │ │
│  │  ▼ app_2                          │   │   │   │   │ │
│  │    ├─ tasks                       │   │   │   │   │ │
│  │    └─ comments                    └───┴───┴───┴───┘ │
│  │                                    ← 1-50 de 230 →  │
│  │                                                      │
│  └──────────────────────────────────────────────────────┘
```

## Backend — Endpoints

### `GET /dashboard/api/data-browser/apps`

Retorna apps com tabelas que o usuário pode acessar (ownership filter igual ListApps).

**Response:**
```json
[
  {
    "name": "app_1",
    "tables": [
      {"name": "users", "columns": [{"name": "id", "type": "uuid"}, ...]},
      {"name": "orders", "columns": [...]}
    ]
  }
]
```

Implementação: reusa `ListApps` + registry para montar resposta. O registry já tem `Columns` com nome e tipo.

### `GET /dashboard/api/data-browser/query?app=X&table=Y&limit=50&offset=0&order=name.asc`

Executa SELECT paginado usando `query.BuildList`.

**Response:**
```json
{
  "data": [{"id": "...", "name": "...", ...}],
  "count": 230,
  "limit": 50,
  "offset": 0
}
```

**Parâmetros:**
- `app` (obrigatório): nome do app
- `table` (obrigatório): nome da tabela
- `limit` (opcional, default 50, max 200)
- `offset` (opcional, default 0)
- `order` (opcional): `{coluna}.asc` ou `{coluna}.desc`

### Segurança

- Ownership filter: admin só vê apps que é dono. Superadmin vê todos.
- Usa o `query.BuildList` que já sanitiza identificadores SQL e previne DDL injection.

## Frontend — DataBrowserPage.tsx

**Layout:**
- `<div style="display:grid; grid-template-columns: 240px 1fr;">` (split interno)
- Lado esquerdo: tree de apps (accordion via estado `expandedApps: Set<string>`)
- Lado direito: tabela com dados + paginação

**Tree (painel esquerdo):**
- Fetch: `GET /dashboard/api/data-browser/apps` via `useQuery`
- Cada app é um item clicável que expande/recolhe (ícone de chevron)
- Tabelas listadas abaixo com indentação
- Tabela ativa destacada (background highlight)
- Loading state com skeleton

**Table view (painel direito):**
- Estado vazio: "Selecione uma tabela" (quando nada selecionado)
- Fetch: `GET /dashboard/api/data-browser/query?app=X&table=Y&limit=50&offset=0` via `useQuery`
- Cabeçalhos: nomes das colunas do schema (do response do /apps)
- Sorting: clicar no cabeçalho alterna ASC/DESC, reinicia offset para 0
- Paginação: "1-50 de 230" com controles Previous/Next
- Loading state com shimmer/skeleton
- Error state com mensagem e botão de retry

**Auto-refresh:** Não. Botão de refresh manual no canto superior direito.

**Ownership:** O endpoint já filtra. Admin só vê apps dele.

### Routing

`/data-browser` → `DataBrowserPage` (substitui o Placeholder atual em App.tsx)

## Considerações Futuras (CRUD)

- Inline edit: clicar em célula → modo edição inline → PATCH via dashboard API
- Inline create: botão "Novo registro" → modal com campos → POST via dashboard API
- Inline delete: checkbox + botão "Deletar" → DELETE via dashboard API
- Filtros por coluna: dropdown de operadores (=, >, <, LIKE, etc.)
- Export CSV
