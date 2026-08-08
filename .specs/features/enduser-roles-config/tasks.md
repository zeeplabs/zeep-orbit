# End-User Roles Configuration Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/enduser-roles-config/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` (section 3 - gate commands; section 5 - i18n both-files rule). No JS/TS unit-test framework installed for React components (`internal/dashboard/ui/src` has zero `*.test.*`/`*.spec.*` files) - the only frontend test type in this repo is Playwright e2e (`internal/dashboard/ui/e2e/*.spec.ts`, 1-2 tests per file, broad end-to-end flows). Go layers follow the existing pattern in `internal/dashboard/*_store_test.go` / `*_handler_test.go` (integration tests against a real Postgres pool, no mocking).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Backend store (`AppRow.EnduserRolesConfig` decode, `UpdateAppEnduserRoles`, `CountAppUsersByRole`, `CountTablePoliciesByRole`) | integration | Every listed edge case: add role, remove role not in use, remove role blocked by end-user, remove role blocked by policy, decode round-trip on `GetApp`/`ListApps` - same depth as `app_users_store_test.go` | `internal/dashboard/apps_store_test.go` | `go test ./internal/dashboard/...` |
| Backend handler (`UpdateAppEnduserRoles` route) | integration | Happy path + every listed edge case (invalid format 400, duplicate role 400, in-use removal 409 with counts, success 200) | `internal/dashboard/apps_handler_test.go` | `go test ./internal/dashboard/...` |
| Frontend UI flows (Settings roles section, `RoleCell` → Drawer, `TablePolicies` chips) | e2e | One Playwright test per new flow, happy path + at least one blocking/edge case per flow - matches existing depth of `apps.spec.ts`/`users.spec.ts` (not exhaustive, matches repo norm) | `internal/dashboard/ui/e2e/enduser-roles.spec.ts` | `cd internal/dashboard/ui && npx playwright test enduser-roles` |
| Frontend types/hooks/i18n wiring (`App` interface, `useUpdateAppEnduserRoles`, locale JSON) | none | build gate only | `internal/dashboard/ui/src/lib/api.ts`, `internal/dashboard/ui/src/locales/*.json` | `npx tsc -b && npm run build` |
| Migration DDL (`provisioner.go` ALTER TABLE) | none | build gate only (exercised indirectly by store integration tests, which depend on the column existing) | `internal/dashboard/provisioner.go` | `go build ./...` |

## Gate Check Commands

> Generated from `Makefile`, `AGENTS.md` section 3, and `internal/dashboard/ui/package.json`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Backend-only task, no frontend touched | `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...` |
| Full | Task touches frontend types/hooks/UI, no new/changed e2e spec yet | `go build ./... && go test ./... && go vet ./... && cd internal/dashboard/ui && npx tsc -b && npm run build` |
| Build | Phase completion, or task adds/changes a Playwright e2e spec, or touches i18n JSON | Full gate + `cd internal/dashboard/ui && npx playwright test enduser-roles` + `gofmt -l <changed .go files>` + (if i18n touched) `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json')); json.load(open('internal/dashboard/ui/src/locales/pt-BR.json'))"` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend foundation (migration + store)

```
T1 → T2
T1 → T3
T1 → T4
```

### Phase 2: Backend handler + route

```
T2 → T5
T3 → T5
T4 → T5
T5 → T6
```

### Phase 3: Frontend foundation (types, hook, i18n)

```
T6 → T7
T7 → T8
```

### Phase 4: P1 - Settings roles section

```
T7 → T9
T8 → T9
T9 → T10
```

### Phase 5: P2 - RoleCell → drawer

```
T7 → T11
T11 → T12
T12 → T13
```

### Phase 6: P3 - TablePolicies chips

```
T7 → T14
T14 → T15
```

---

## Task Breakdown

### T1: Migração - coluna `enduser_roles_config`

**What**: Adicionar `ALTER TABLE zeep_system.apps ADD COLUMN IF NOT EXISTS enduser_roles_config JSONB NOT NULL DEFAULT '["member"]'::jsonb` à lista de migrações idempotentes de `ProvisionZeepSystem`.
**Where**: `internal/dashboard/provisioner.go` (~linha 102, mesma lista de `auth_providers`/`storage_config`/`rate_limit_config`)
**Depends on**: None
**Reuses**: Padrão exato das 3 colunas JSONB já existentes na mesma lista.
**Requirement**: ROLECFG-07

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [x] Linha adicionada na mesma lista de `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
- [x] `go build ./...` limpo
- [x] Rodar servidor local (ou suite existente) confirma que a coluna existe em `zeep_system.apps` após provisionamento, sem erro de migração duplicada em execução repetida (idempotência)

**Tests**: none
**Gate**: quick

**Commit**: `feat(dashboard): add enduser_roles_config column to apps`

---

### T2: `AppRow.EnduserRolesConfig` - campo + decode

**What**: Adicionar campo `EnduserRolesConfig []string` (`json:"enduser_roles_config"`) à struct `AppRow` e decodificá-lo em `ListApps`/`GetApp`/`CreateApp`, seguindo o padrão de decode manual já usado para `StorageConfig`.
**Where**: `internal/dashboard/apps_store.go`
**Depends on**: T1
**Reuses**: Padrão de unmarshal manual de `StorageConfig` (`apps_store.go:79-97`, `:186-203`).
**Requirement**: ROLECFG-01

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] `AppRow` expõe `EnduserRolesConfig []string`
- [ ] `ListApps`, `GetApp` e `CreateApp` decodificam a coluna sem erro quando ela é `["member"]` (default) ou uma lista customizada
- [ ] Teste de integração cobre round-trip (cria app, lê de volta, lista igual à persistida)
- [ ] Gate check passa: `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...`

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): decode enduser_roles_config into AppRow`

---

### T3: Store - contagem de uso (`CountAppUsersByRole`, `CountTablePoliciesByRole`)

**What**: Duas funções de store: `CountAppUsersByRole(ctx, pool, schema, role string) (int, error)` (`SELECT COUNT(*) FROM %q."_auth_users" WHERE role = $1`, schema resolvido via `schemaNameForDB`) e `CountTablePoliciesByRole(ctx, pool, appID, role string) (int, error)` (`SELECT COUNT(*) FROM zeep_system.table_policies WHERE app_id = $1 AND roles ? $2`).
**Where**: `internal/dashboard/apps_store.go`
**Depends on**: T1
**Reuses**: `schemaNameForDB` (regra obrigatória do `AGENTS.md` - nunca hardcodear `"app_" + name`).
**Requirement**: ROLECFG-05

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] `CountAppUsersByRole` retorna a contagem correta (0 quando ninguém usa a role, N quando N end-users usam)
- [ ] `CountTablePoliciesByRole` retorna a contagem correta usando o operador jsonb `?` (existência de elemento top-level)
- [ ] Testes de integração cobrem: zero uso, uso por end-user, uso por policy, uso por ambos
- [ ] Gate check passa: `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...`

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add role usage count queries for enduser roles`

---

### T4: Store - `UpdateAppEnduserRoles`

**What**: Função `UpdateAppEnduserRoles(ctx, pool, appID string, roles []string) error` que persiste a lista completa (`UPDATE zeep_system.apps SET enduser_roles_config = $1 WHERE id = $2`) - só persistência, sem validação de negócio (validação e checagem de uso ficam no handler, T5).
**Where**: `internal/dashboard/apps_store.go`
**Depends on**: T1
**Reuses**: Padrão de `UpdateAppStorageConfig`/`UpdateAppRateLimitConfig` (marshal + `UPDATE`).
**Requirement**: ROLECFG-02

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] Persiste array vazio e array populado corretamente
- [ ] Retorna erro se `appID` não existir (mesmo padrão de erro dos updates existentes)
- [ ] Teste de integração cobre persistência e leitura de volta via `GetApp`
- [ ] Gate check passa: `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...`

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add UpdateAppEnduserRoles store function`

---

### T5: Handler `UpdateAppEnduserRoles`

**What**: Handler que recebe `PUT /dashboard/api/apps/{id}/roles`, valida cada role com `identRe`, rejeita duplicata no array submetido (400), calcula `removed := old \ new` via `GetApp` atual, bloqueia (409, com `endUserCount`/`policyCount`) se qualquer role removida estiver em uso, senão chama `UpdateAppEnduserRoles` e retorna `{"roles": [...]}`.
**Where**: `internal/dashboard/handler.go`
**Depends on**: T2, T3, T4
**Reuses**: `identRe` (`handler.go:85`), mesma cópia já usada em `UpdateAppUserRole` (`handler.go:2421`) - não criar uma terceira instância da regex.
**Requirement**: ROLECFG-02, ROLECFG-03, ROLECFG-04, ROLECFG-05, ROLECFG-06

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] 400 quando role não casa `identRe`, mensagem igual à de `UpdateAppUserRole`
- [ ] 400 `role already exists` quando o array submetido tem duplicata
- [ ] 409 com `endUserCount`/`policyCount` > 0 quando tenta remover role em uso (por end-user e/ou por policy, testado nos dois casos)
- [ ] 200 com lista persistida quando remoção não afeta role em uso, ou quando só adiciona role nova
- [ ] Teste de integração cobre os 4 cenários acima
- [ ] Gate check passa: `go build ./... && go test ./internal/dashboard/... && go vet ./internal/dashboard/...`

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add UpdateAppEnduserRoles handler with in-use guard`

---

### T6: Rota `PUT /api/apps/{id}/roles`

**What**: Registrar a rota do handler de T5 no grupo de rotas de apps do dashboard.
**Where**: `internal/server/server.go` (~linha 188-189, junto de `UpdateAppUserRole`)
**Depends on**: T5
**Reuses**: Grupo de rotas já existente (`/api/apps/{id}/...`), mesmo middleware de auth de dashboard.
**Requirement**: ROLECFG-02

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`

**Done when**:
- [ ] `r.Put("/api/apps/{id}/roles", dashH.UpdateAppEnduserRoles)` registrado no mesmo grupo protegido
- [ ] Teste de integração (pode estender o de T5) confirma a rota responde via HTTP real, não só chamando o handler direto
- [ ] Gate check passa: `go build ./... && go test ./... && go vet ./...` (gate completo de backend, fecha a Phase 2)

**Tests**: integration
**Gate**: full

**Commit**: `feat(server): register PUT /api/apps/{id}/roles route`

---

### T7: Tipo `App.enduserRolesConfig` + hook `useUpdateAppEnduserRoles`

**What**: Estender o tipo `App` no cliente com `enduserRolesConfig: string[]` e criar `useUpdateAppEnduserRoles(appId: string)` (mutation React Query, `PUT /dashboard/api/apps/${appId}/roles`, invalida `['apps']`), copiando a forma de `useUpdateApp`.
**Where**: `internal/dashboard/ui/src/lib/api.ts`
**Depends on**: T6
**Reuses**: Padrão exato de `useUpdateApp` (`api.ts:135-148`) e `useUpdateAppUserRole` (`api.ts:425-444`).
**Requirement**: ROLECFG-01, ROLECFG-02

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] `App.enduserRolesConfig` tipado como `string[]`
- [ ] `useUpdateAppEnduserRoles` chama `onError` com `toast.error(error.message)` (regra do `AGENTS.md` seção 5 - mutation sem `onError` é hook incompleto)
- [ ] `npx tsc -b` limpo
- [ ] Gate check passa: `go build ./... && go test ./... && go vet ./... && cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none
**Gate**: full

**Commit**: `feat(dashboard-ui): add enduserRolesConfig type and update hook`

---

### T8: i18n - strings de roles de end-user

**What**: Adicionar todas as strings novas (seção de Settings, drawer de edição de role, chips de policy - labels, placeholders, mensagens de erro de duplicata/formato/em-uso) em `en.json` e `pt-BR.json`, na mesma mudança.
**Where**: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T7
**Reuses**: Namespace/estrutura já usada pelas strings de `role`/`tablePolicies` existentes (`tablePolicies.rolesPlaceholder`, strings de `RoleCell` da feature D-139).
**Requirement**: ROLECFG-01, ROLECFG-09, ROLECFG-12

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] Toda string nova tem chave em `en.json` E `pt-BR.json` (nenhuma órfã em só um idioma)
- [ ] `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json')); json.load(open('internal/dashboard/ui/src/locales/pt-BR.json'))"` sem erro
- [ ] Gate check passa: full gate (T7) + validação JSON acima

**Tests**: none
**Gate**: full

**Commit**: `feat(dashboard-ui): add i18n strings for enduser roles config`

---

### T9: Seção "Roles de usuário final" em Settings

**What**: Componente local `EnduserRolesSection` na página de detalhes do app: lista roles atuais como `Badge` removível, `Input` + botão pra adicionar role nova, chama `useUpdateAppEnduserRoles` com o array atualizado (adição = old + nova; remoção = old - clicada), `toast.error` mostrando a mensagem do 409 (contagem de usos) quando bloqueado. Gated por `authEmailEnabled` (oculta a seção inteira quando `false`).
**Where**: `internal/dashboard/ui/src/pages/AppDetailsPage.tsx`
**Depends on**: T7, T8
**Reuses**: `Input`, `Badge`, `Button`, padrão visual das seções de `storage_config`/`rate_limit` (`AppDetailsPage.tsx:409-436`, `:538-601`), gate `authEmailEnabled` já usado no fix `ROWPOL-25`.
**Requirement**: ROLECFG-01, ROLECFG-02, ROLECFG-03, ROLECFG-04, ROLECFG-05, ROLECFG-06, ROLECFG-08

**Tools**:
- MCP: NONE
- Skill: `react-composition-patterns`

**Done when**:
- [ ] Seção oculta quando `authEmailEnabled === false`
- [ ] Lista renderiza `enduserRolesConfig` atual (mínimo `["member"]`) como chips
- [ ] Adicionar role nova via Input+botão persiste e atualiza a lista sem reload manual (invalidação de query)
- [ ] Remover role sem uso persiste; remover role em uso mostra toast de erro com a mensagem do backend, sem remover o chip
- [ ] `npx tsc -b` e `npm run build` limpos
- [ ] Gate check passa: full gate

**Tests**: none (cobertura funcional fica no e2e de T10)
**Gate**: full

**Commit**: `feat(dashboard-ui): add enduser roles management section to app settings`

---

### T10: E2E - gestão da lista de roles (P1)

**What**: Novo arquivo `enduser-roles.spec.ts` com um teste cobrindo: abrir Settings de um app com `authEmailEnabled=true`, ver `member` pré-populado, adicionar `viewer`, e um segundo cenário (mesmo `test.describe` ou teste separado) confirmando que remover uma role em uso é bloqueado com mensagem de erro visível.
**Where**: `internal/dashboard/ui/e2e/enduser-roles.spec.ts` (novo arquivo)
**Depends on**: T9
**Reuses**: Padrão de setup de `apps.spec.ts`/`users.spec.ts` (fixtures de login/app existente).
**Requirement**: ROLECFG-01, ROLECFG-05, ROLECFG-06, ROLECFG-08

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] Teste "adiciona role nova via Settings" passa
- [ ] Teste "remoção de role em uso é bloqueada com mensagem de erro" passa
- [ ] Gate check passa: `cd internal/dashboard/ui && npx playwright test enduser-roles` (Build gate, fecha a Phase 4)
- [ ] Test count: 2 testes passam (sem exclusão silenciosa)

**Tests**: e2e
**Gate**: build

**Commit**: `test(dashboard-ui): cover enduser roles settings management`

---

### T11: Coluna "role" vira somente-leitura

**What**: Remover `RoleCell` como editor da coluna `role` na tabela de usuários - passa a renderizar `row.original.role` como texto simples.
**Where**: `internal/dashboard/ui/src/pages/AppUsersPage.tsx`
**Depends on**: T7
**Reuses**: N/A (remoção de código existente).
**Requirement**: ROLECFG-09

**Tools**:
- MCP: NONE
- Skill: `react-composition-patterns`

**Done when**:
- [ ] Coluna `role` não tem mais `Input`/estado de edição inline
- [ ] `RoleCell` (definição antiga) removido do arquivo, sem código morto deixado
- [ ] `npx tsc -b` limpo
- [ ] Gate check passa: full gate

**Tests**: none (cobertura funcional fica no e2e de T13)
**Gate**: full

**Commit**: `refactor(dashboard-ui): make app user role column read-only`

---

### T12: Botão "Editar" + `EditRoleDrawer`

**What**: Adicionar botão "Editar" na coluna Ações já existente; ao clicar, abre `EditRoleDrawer` (`Drawer` + `Select` populado por `enduserRolesConfig` do app, mais a role atual como opção extra se ela for órfã) com "Salvar" (chama `useUpdateAppUserRole` já existente, fecha o drawer) e "Cancelar" (fecha sem chamar mutation).
**Where**: `internal/dashboard/ui/src/pages/AppUsersPage.tsx`
**Depends on**: T11
**Reuses**: `Drawer`/`DrawerContent`/`DrawerFooter` (`ui/drawer.tsx`), `Select` (`ui/select.tsx`), `useUpdateAppUserRole` (inalterado, contrato preservado).
**Requirement**: ROLECFG-10, ROLECFG-11, ROLECFG-12, ROLECFG-13, ROLECFG-14

**Tools**:
- MCP: NONE
- Skill: `react-composition-patterns`

**Done when**:
- [ ] Botão "Editar" aparece por linha na coluna Ações
- [ ] Drawer abre com `Select` pré-selecionado na role atual do usuário
- [ ] Role órfã (fora de `enduserRolesConfig`) aparece selecionada no `Select` sem forçar troca
- [ ] "Salvar" chama `useUpdateAppUserRole` e fecha o drawer; "Cancelar"/fechar não chama mutation alguma
- [ ] `npx tsc -b` e `npm run build` limpos
- [ ] Gate check passa: full gate

**Tests**: none (cobertura funcional fica no e2e de T13)
**Gate**: full

**Commit**: `feat(dashboard-ui): add role edit drawer to app users actions column`

---

### T13: E2E - edição de role via drawer (P2)

**What**: Adicionar teste em `enduser-roles.spec.ts` cobrindo: coluna role não é clicável/editável direto, clicar "Editar" abre drawer, trocar role via `Select`, confirmar e ver a tabela atualizada.
**Where**: `internal/dashboard/ui/e2e/enduser-roles.spec.ts` (modificar)
**Depends on**: T12
**Reuses**: Setup já criado em T10 (mesmo arquivo, mesmas fixtures).
**Requirement**: ROLECFG-09, ROLECFG-10, ROLECFG-11

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] Teste "edita role de um end-user via drawer" passa
- [ ] Gate check passa: `cd internal/dashboard/ui && npx playwright test enduser-roles` (Build gate, fecha a Phase 5)
- [ ] Test count: 3 testes passam no arquivo (2 de T10 + 1 novo, sem exclusão silenciosa)

**Tests**: e2e
**Gate**: build

**Commit**: `test(dashboard-ui): cover app user role edit via drawer`

---

### T14: Chips de roles em `TablePolicies`

**What**: Substituir o estado `rolesInput`/parse CSV/`Input` livre por `selectedRoles: string[]` e chips clicáveis (`Badge`, toggle) populados por `enduserRolesConfig` do app; qualquer role já persistida na policy que não esteja na lista atual (órfã) aparece como chip extra, sempre visível, removível manualmente.
**Where**: `internal/dashboard/ui/src/components/TablePolicies.tsx`
**Depends on**: T7
**Reuses**: `Badge` (`ui/badge.tsx`). Nenhuma dependência nova (decisão registrada no design - sem combobox de terceiros).
**Requirement**: ROLECFG-15, ROLECFG-16, ROLECFG-17

**Tools**:
- MCP: NONE
- Skill: `react-composition-patterns`

**Done when**:
- [ ] `Input` CSV de roles removido, substituído por chips toggle
- [ ] Role órfã de uma policy já existente aparece como chip selecionado, sem ser removida automaticamente
- [ ] Salvar a policy persiste exatamente o array `selectedRoles` (sem parse de string)
- [ ] Backend (`BuildPolicySQL`, inalterado) continua validando cada role via `identRe` - nenhuma regressão introduzida
- [ ] `npx tsc -b` e `npm run build` limpos
- [ ] Gate check passa: full gate

**Tests**: none (cobertura funcional fica no e2e de T15)
**Gate**: full

**Commit**: `feat(dashboard-ui): replace free-text CSV roles input with chip multi-select in TablePolicies`

---

### T15: E2E - seleção de roles via chips numa policy (P3)

**What**: Adicionar teste em `enduser-roles.spec.ts` cobrindo: criar/editar uma table policy selecionando roles via chips (sem digitar texto), salvar, e confirmar que a policy persistida reflete exatamente as roles selecionadas.
**Where**: `internal/dashboard/ui/e2e/enduser-roles.spec.ts` (modificar)
**Depends on**: T14
**Reuses**: Setup já criado em T10/T13 (mesmo arquivo, mesmas fixtures).
**Requirement**: ROLECFG-15, ROLECFG-17

**Tools**:
- MCP: NONE
- Skill: `react-best-practices`

**Done when**:
- [ ] Teste "cria policy selecionando roles via chips" passa
- [ ] Gate check passa: `cd internal/dashboard/ui && npx playwright test enduser-roles` (Build gate, fecha a Phase 6 e a feature)
- [ ] Test count: 4 testes passam no arquivo (3 anteriores + 1 novo, sem exclusão silenciosa)

**Tests**: e2e
**Gate**: build

**Commit**: `test(dashboard-ui): cover table policy roles chip selection`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

Phase 1:  T1 --→ T2 --→ T5
          T1 --→ T3 --→ T5
          T1 --→ T4 --→ T5
Phase 2:  T5 ------→ T6
Phase 3:  T6 ------→ T7 ------→ T8
Phase 4:  T7 --→ T9 --→ T10
          T8 --→ T9
Phase 5:  T7 ------→ T11 ------→ T12 ------→ T13
Phase 6:  T7 ------→ T14 ------→ T15
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migração coluna | 1 linha DDL | ✅ Granular |
| T2: `AppRow` decode | 1 struct + 3 pontos de decode do mesmo campo | ✅ Granular (mesmo conceito, 1 campo) |
| T3: Contagem de uso | 2 funções irmãs (mesma finalidade: checar uso) | ✅ Granular |
| T4: `UpdateAppEnduserRoles` store | 1 função | ✅ Granular |
| T5: Handler | 1 handler (múltiplas validações internas, mas 1 endpoint) | ✅ Granular |
| T6: Rota | 1 linha de registro | ✅ Granular |
| T7: Tipo + hook | 1 tipo + 1 hook (mesmo par type/mutation, padrão já usado assim no repo) | ✅ Granular |
| T8: i18n | 2 arquivos (par obrigatório en/pt-BR, regra explícita do `AGENTS.md`) | ✅ Granular |
| T9: Seção Settings | 1 componente | ✅ Granular |
| T10: E2E P1 | 1 arquivo de teste, 2 casos do mesmo flow | ✅ Granular |
| T11: Coluna somente-leitura | 1 mudança de coluna | ✅ Granular |
| T12: Drawer de edição | 1 componente + wiring do botão que o abre | ✅ Granular |
| T13: E2E P2 | 1 caso de teste | ✅ Granular |
| T14: Chips de roles | 1 componente/estado | ✅ Granular |
| T15: E2E P3 | 1 caso de teste | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T1 | T1 → T3 (mesma cadeia da fase) | ✅ Match |
| T4 | T1 | T1 → T4 (mesma cadeia da fase) | ✅ Match |
| T5 | T2, T3, T4 | T4 → T5 (fim da Phase 1 alimenta início da Phase 2) | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T7, T8 | T8 → T9 (fim da Phase 3 alimenta início da Phase 4) | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T7 | T7 → T11 (Phase 5 também consome o fim da Phase 3) | ✅ Match |
| T12 | T11 | T11 → T12 | ✅ Match |
| T13 | T12 | T12 → T13 | ✅ Match |
| T14 | T7 | T7 → T14 (Phase 6 também consome o fim da Phase 3) | ✅ Match |
| T15 | T14 | T14 → T15 | ✅ Match |

Nota: T3 e T4 dependem só de T1 (não de T2) - ambas fases criam funções independentes sobre a mesma coluna, sem depender do decode de leitura feito em T2. T11 e T14 dependem de T7 (não de T9/T10) - Phase 5 e Phase 6 só precisam do tipo/hook da Phase 3, não da seção de Settings da Phase 4; a ordem sequencial das fases é de execução (Phases rodam em sequência), não uma dependência de dado adicional.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migração coluna | Migration DDL | none | none | ✅ OK |
| T2: `AppRow` decode | Backend store | integration | integration | ✅ OK |
| T3: Contagem de uso | Backend store | integration | integration | ✅ OK |
| T4: `UpdateAppEnduserRoles` store | Backend store | integration | integration | ✅ OK |
| T5: Handler | Backend handler | integration | integration | ✅ OK |
| T6: Rota | Backend handler | integration | integration | ✅ OK |
| T7: Tipo + hook | Frontend types/hooks | none | none | ✅ OK |
| T8: i18n | Frontend i18n | none | none | ✅ OK |
| T9: Seção Settings | Frontend UI flow | e2e | none (coberto no e2e de T10, mesmo flow) | ✅ OK |
| T10: E2E P1 | Frontend UI flow | e2e | e2e | ✅ OK |
| T11: Coluna somente-leitura | Frontend UI flow | e2e | none (coberto no e2e de T13, mesmo flow) | ✅ OK |
| T12: Drawer de edição | Frontend UI flow | e2e | none (coberto no e2e de T13, mesmo flow) | ✅ OK |
| T13: E2E P2 | Frontend UI flow | e2e | e2e | ✅ OK |
| T14: Chips de roles | Frontend UI flow | e2e | none (coberto no e2e de T15, mesmo flow) | ✅ OK |
| T15: E2E P3 | Frontend UI flow | e2e | e2e | ✅ OK |

Regra aplicada (ver "Resolving compilation dependencies" do processo): componentes de UI (T9, T11, T12, T14) não são testáveis isoladamente em unidade (sem framework de teste de componente no repo) - o teste e2e que exercita o flow completo fica na task seguinte da mesma fase (T10, T13, T15), nunca numa fase futura desconectada. Isso é "merge forward" dentro da própria fase, não deferral cross-fase.
