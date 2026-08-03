# Tasks: RBAC Per-App

**Spec**: `.specs/features/rbac-per-app/spec.md`
**Design**: `.specs/features/rbac-per-app/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build`), mesmo critério das demais specs.

**Pré-requisito cruzado (não bloqueante para T-01 a T-05)**: a integração com `dashboard-global-roles` (chamar `CanReadAnyApp` dentro de `ResolveAppRole`) só faz efeito real se `dashboard-global-roles` já estiver implementada — mas o ponto de extensão dentro de `ResolveAppRole` é independente: se `CanReadAnyApp` retornar `false` para todo mundo (caso da função ainda não existir ou antes da migration de 4 papéis subir), `ResolveAppRole` cai no lookup normal em `app_members`, que é o comportamento seguro. Cross-spec é opt-in, não bloqueante.

---

## Execution Plan

```
Fase 1: Modelo de dados                Fase 2: Enforcement (backend apps)
┌──────────────────────────┐           ┌───────────────────────────┐
│ T-01 app_members table +    │──────────▶│ T-04 ListApps, GetApp,        │
│      ResolveAppRole        │           │      DeleteApp, GetAppSecret, │
│ T-02 migração dados         │           │      GetAppAuthProviders,     │
│      (owner_id → app_members)│           │      app_tables CRUD          │
│ T-03 invariante "≥1 admin"   │           └───────────────────────────┘
│      (lock transacional)     │
└──────────────────────────┘                      │
         │                                          ▼
         │                            Fase 3: Enforcement (frontend apps)
         │                            ┌───────────────────────────┐
         │                            │ T-05 frontend_apps handlers  │
         │                            │      (list, get, archive,    │
         │                            │       deploy, sync creds)    │
         │                            └───────────────────────────┘
         │                                          │
         ▼                                          ▼
Fase 4: API de gestão                 Fase 5: Limpeza
┌──────────────────────────┐           ┌───────────────────────────┐
│ T-06 POST/GET/PATCH/DELETE  │           │ T-08 DROP app_ownership +     │
│      /members endpoints    │           │      remover owner_id das    │
└──────────────────────────┘           │      queries (vira metadata) │
                                       └───────────────────────────┘
                                                  │
Fase 6: Frontend                              Fase 7: Docs
┌──────────────────────────┐           ┌───────────────────────────┐
│ T-09 página Members no       │           │ T-11 README + CHANGELOG     │
│      AppDetailsPage          │           │ T-12 integração final com   │
│ T-10 i18n en/pt-BR            │──────────▶│      dashboard-global-roles │
└──────────────────────────┘           │      T-06 (CanReadAnyApp)    │
                                       └───────────────────────────┘
```

---

### T-01: `app_members` table + `ResolveAppRole` ⭐ MVP

- **What**: Migration criando `zeep_system.app_members` (UNIQUE parcial por eixo, CHECK de "exactly one" FK, índice em user_id para lookup reverso). Função `ResolveAppRole` em `internal/dashboard/rbac.go` com `AppRole`/`AppRef` e a integração `CanReadAnyApp` (cross-spec com `dashboard-global-roles` T-06).
- **Where**: `internal/dashboard/provisioner.go` (adiciona statements no array `stmts`), `internal/dashboard/rbac.go` (novo), `internal/dashboard/rbac_test.go` (novo).
- **Depends on**: nenhuma
- **Reuses**: padrão de migration idempotente de `provisioner.go`; padrão de teste com `TEST_DATABASE_URL` skip de `provisioner_roles_test.go`; `errors.Is(err, pgx.ErrNoRows)` já usado em `apps_store.go` e outros.
- **Requirement**: RB-01 (tabela existe), RB-07 (audit_log registrado pelos callers, mas o hook de audit fica em T-06 — T-01 só provê a função). **T-01 sozinho NÃO cobre RB-10..16 (enforcement) nem RB-20..25 (migração de dados)** — esses vêm em T-02 em diante. T-01 é o "core" da feature, sem o qual nada funciona.
- **Tools**: nenhum
- **Done when**:
  - `app_members` criada pelo `ProvisionZeepSystem` (verificado via `pg_tables`)
  - Tabela aceita 4 roles: `admin`/`editor`/`viewer` e rejeita valores fora (CHECK)
  - UNIQUE parcial funciona: não dá pra ter 2 linhas para o mesmo `(app, user)` no mesmo eixo, mas dá pra ter 1 admin de backend e 1 viewer de frontend
  - `ResolveAppRole(superadmin, qualquer app) == AppRoleAdmin` (bypass)
  - `ResolveAppRole(admin global, qualquer app) == AppRoleViewer` (cross-spec extension)
  - `ResolveAppRole(auditor global, qualquer app) == AppRoleViewer`
  - `ResolveAppRole(member sem membership, app X) == ""`
  - `ResolveAppRole(member com row em app_members, app X) == role da row`
  - `ResolveAppRole(_, AppRef{})` ou `AppRef{BackendAppID: "x", FrontendAppID: "y"}` retorna `ErrInvalidAppRef`
- **Tests**:
  - `rbac_test.go::TestResolveAppRoleInvalidRef` — puro, sem DB
  - `rbac_test.go::TestResolveAppRole` — DB-dependent, matrix de 4 roles globais × (sem membership / admin / editor / viewer) × (backend app / frontend app)
  - `rbac_test.go::TestAppMembersSchemaConstraints` — UNIQUE parcial, CHECK, ON DELETE CASCADE
- **Gate**: `TEST_DATABASE_URL=postgres://... go test ./internal/dashboard/... -run TestResolveAppRole`
- **Commit**: `feat(rbac): add app_members table and ResolveAppRole function`

---

### T-02: Migração de dados existentes sem perda de acesso

- **What**: Migration que popula `app_members` a partir de `apps.owner_id`, `app_ownership` (co-donos) e `frontend_apps.created_by` (que resolve para `dashboard_users.email`). `created_by` que não resolve para usuário existente é deixado sem membership (app órfão, acessível só por superadmin). `apps.owner_id` e `app_ownership` **não são removidos** — só deixam de ser fonte de autorização. Tudo dentro da mesma transação de `ProvisionZeepSystem`.
- **Where**: `internal/dashboard/provisioner.go` (statements adicionais com `INSERT ... SELECT ... ON CONFLICT DO NOTHING`).
- **Depends on**: T-01
- **Reuses**: padrão de idempotência de `ProvisionZeepSystem` (`IF NOT EXISTS` + advisory lock).
- **Requirement**: RB-20 a RB-25
- **Tools**: nenhum
- **Done when**: rodar `ProvisionZeepSystem` num banco com apps/frontend_apps/app_ownership pré-existentes produz `app_members` com `admin` para cada dono/co-dono/criador resolvível, e não quebra com `created_by` órfão.
- **Tests**: `rbac_migration_test.go::TestOwnershipMigration` — fixture com 2 backend apps (1 com co-dono), 1 frontend app com `created_by` válido, 1 frontend app com `created_by` órfão; roda `ProvisionZeepSystem`; valida que `app_members` tem os 3 admin esperados e o órfão fica vazio.
- **Gate**: `go test ./internal/dashboard/... -run TestOwnershipMigration`
- **Commit**: `feat(rbac): migrate existing app ownership to app_members`

---

### T-03: Invariante "≥1 admin por app" (transacional, lock)

- **What**: Helper `CountAppAdmins(ctx, pool, app AppRef) (int, error)` + lógica de "rejeitar mudança se resultaria em zero admin". Lock via `SELECT ... FOR UPDATE` na transação que faz o `UPDATE`/`DELETE` em `app_members`. Mesmo padrão de T-05 de `dashboard-global-roles` (invariante "≥1 superadmin").
- **Where**: `internal/dashboard/app_members_store.go` (novo), onde os endpoints de T-06 vão usar.
- **Depends on**: T-01
- **Reuses**: padrão de invariante transacional de `users.go` (PATCH/DELETE user).
- **Requirement**: edge case "≥1 admin" da spec (linha 41-44, AC-3, AC-4, AC-5)
- **Tools**: nenhum
- **Done when**: count retorna 0 quando app tem admin, retorna N quando tem N admins; função é thread-safe sob chamada concorrente.
- **Tests**: `app_members_store_test.go::TestCountAppAdmins` + teste de race condition com `-race`.
- **Gate**: `go test -race ./internal/dashboard/... -run TestCountAppAdmins`
- **Commit**: não (agrupa com T-06)

---

### T-04: Enforcement em handlers de apps backend

- **What**: Substituir filtros baseados em `apps.owner_id` e `app_ownership` (em `apps_store.go` linhas 60-61, 174-175, 298-300 e os handlers correspondentes em `handler.go`) por filtros via `app_members` + `ResolveAppRole`. Handlers de `app_tables` (table create/list/edit/delete) também passam a checar `role.CanWrite()` / `role.CanManage()`. `apps.owner_id` continua existindo como metadado (não removido).
- **Where**: `internal/dashboard/apps_store.go`, `internal/dashboard/handler.go` (handlers `ListApps`, `GetApp`, `DeleteApp`, `GetAppSecret`, `GetAppAuthProviders`, e os de `app_tables`).
- **Depends on**: T-01, T-02 (precisa dos dados migrados pra não quebrar acesso)
- **Reuses**: `ResolveAppRole` de T-01; padrão "handler consulta função, retorna 403/omite" já usado em `CreateUser` (dashboard-global-roles T-03).
- **Requirement**: RB-10 a RB-16
- **Tools**: nenhum
- **Done when**: 4 usuários (superadmin, admin global, admin do app, editor do app, viewer do app, não-membro) batem em cada endpoint e recebem 200/403 conforme a matriz.
- **Tests**: testes HTTP em `apps_handler_test.go` cobrindo a matriz de permissão (superadmin sempre 200; admin/auditor global só leitura; member com role X faz o que X permite).
- **Gate**: `go test ./internal/dashboard/... -run TestAppsRBAC`
- **Commit**: `feat(rbac): enforce per-app role on backend app endpoints`

---

### T-05: Enforcement em handlers de apps frontend

- **What**: Mesmo tratamento de T-04 mas para `frontend_apps.go`/`frontend_apps_store.go` e handlers relacionados (`GET /frontend-apps`, `GET /frontend-apps/{id}`, archive, deploy, sync credentials). Apps arquivados para não-membros (mesmo padrão de "não existe" já usado).
- **Where**: `internal/dashboard/frontend_apps.go`, `internal/dashboard/frontend_apps_store.go`, `internal/dashboard/handler.go` (handlers de frontend apps).
- **Depends on**: T-01, T-02
- **Reuses**: T-04, `ResolveAppRole`.
- **Requirement**: RB-10 a RB-16 (eixo frontend)
- **Tools**: nenhum
- **Done when**: matriz de permissão passa pra 4 roles × 3 roles per-app × 2 eixos.
- **Tests**: `frontend_apps_handler_test.go` espelhando os cenários de T-04.
- **Gate**: `go test ./internal/dashboard/... -run TestFrontendAppsRBAC`
- **Commit**: `feat(rbac): enforce per-app role on frontend app endpoints`

---

### T-06: API de gestão de membros (POST/GET/PATCH/DELETE)

- **What**: 4 endpoints × 2 eixos = 8 rotas (ou 4 se unificadas sob `/members` com switch no handler pelo tipo de app):
  - `POST /dashboard/api/apps/{id}/members` `{user_id, role}` → 201
  - `GET /dashboard/api/apps/{id}/members` → lista
  - `PATCH /dashboard/api/apps/{id}/members/{user_id}` `{role}` → 200
  - `DELETE /dashboard/api/apps/{id}/members/{user_id}` → 204
  - (mesma coisa para `frontend-apps/{id}/members`)
  - Todos auditados (`app_member.added` / `role_changed` / `removed`).
  - `POST` e `PATCH`/`DELETE` exigem `AppRoleAdmin` no app (ou `superadmin`).
  - `PATCH`/`DELETE` rejeitados com 400 se deixariam app sem admin (T-03).
  - UNIQUE constraint rejeita duplicata com erro claro.
- **Where**: `internal/dashboard/app_members_store.go` (operações CRUD), `internal/dashboard/handler.go` (handlers), `internal/dashboard/app_members_handler_test.go` (testes).
- **Depends on**: T-01, T-02, T-03
- **Reuses**: `InsertAuditLog`, padrão de handler do `users.go` (PATCH/DELETE user).
- **Requirement**: RB-01 a RB-07 (P1: Gestão de membros)
- **Tools**: nenhum
- **Done when**: 7 cenários do "Independent Test" da spec (linha 46) passam.
- **Tests**: tabela cobrindo os 7 cenários + casos de borda (UNIQUE, "último admin", não-membro tentando gerenciar).
- **Gate**: `go test ./internal/dashboard/... -run TestAppMembersAPI`
- **Commit**: `feat(rbac): add member management API for apps` (T-03 + T-06)

---

### T-07: (reservado)

- Reservado para não pular numeração. Tasks T-01 a T-06 cobrem a Fase 1-4. T-07 absorveria o invariante "≥1 admin" se não tivesse sido puxado pra T-03.

---

### T-08: Remoção de `app_ownership` + cleanup de queries

- **What**: `DROP TABLE zeep_system.app_ownership` no provisioner. Remover todos os `LEFT JOIN app_ownership` em `apps_store.go` (linhas 60-61, 174-175, 298-300). Atualizar testes que usam `app_ownership` para usar `app_members`. `apps.owner_id` e `frontend_apps.created_by`/`owner_id` **não são removidos** — continuam como metadado.
- **Where**: `internal/dashboard/provisioner.go` (DROP TABLE), `internal/dashboard/apps_store.go` (remove JOINs), `internal/dashboard/apps_store_test.go`, `internal/dashboard/frontend_apps_store_test.go` (atualiza fixtures).
- **Depends on**: T-04 e T-05 (enforcement tem que estar 100% em `ResolveAppRole` antes de remover o fallback `app_ownership`)
- **Reuses**: nenhum
- **Requirement**: RB-25 (cleanup), edge case de remoção da spec
- **Tools**: nenhum
- **Done when**: `grep app_ownership` retorna 0 no `src/`. Testes que usavam `app_ownership` passam com `app_members`.
- **Tests**: `grep` + os mesmos testes de T-04/T-05 rodando sem a tabela.
- **Gate**: `grep -r app_ownership internal/ && echo FAIL || echo OK` (deve dar OK), `go test ./internal/dashboard/...`
- **Commit**: `refactor(rbac): drop app_ownership table in favor of app_members`

---

### T-09: Frontend — página Members no AppDetailsPage

- **What**: Nova aba "Members" em `AppDetailsPage` (backend e frontend) listando os membros com role, e botões "Add member" / "Change role" / "Remove" gated por `AppRoleAdmin` no app. Componente reusável (mesma página para backend/frontend, com prop `appType`).
- **Where**: `internal/dashboard/ui/src/pages/AppDetailsPage.tsx` (nova tab), `internal/dashboard/ui/src/components/patterns/AppMembersList.tsx` (novo), mutações via `useApi`.
- **Depends on**: T-06
- **Reuses**: `DataTable` (Fase 0.5 do `dashboard-redesign`), `ConfirmDialog` (idem), padrão de mutation hook com `toast.error`.
- **Requirement**: cobertura de UX da P1 "Gestão de membros" (a parte de UI).
- **Tools**: nenhum
- **Done when**: usuário `admin` do app consegue adicionar/remover/mudar role de membros; UI omite controles para `editor`/`viewer`; redirect/403 se não-membro.
- **Tests**: e2e com Playwright em `internal/dashboard/ui/e2e/app-members.spec.ts` (cobre o "Independent Test" da spec).
- **Gate**: `npx tsc -b`, `npm run build`
- **Commit**: `feat(rbac): add members management UI in app details`

---

### T-10: i18n en/pt-BR das strings novas

- **What**: Labels de role (`Admin`/`Editor`/`Viewer`), mensagens de erro (já vêm do backend em inglês, AGENTS §4), e strings da UI de Members em `en.json` e `pt-BR.json`.
- **Where**: `internal/dashboard/ui/src/locales/en.json`, `pt-BR.json`.
- **Depends on**: T-09
- **Reuses**: `react-i18next` já configurado.
- **Requirement**: regra geral do `AGENTS.md` §5.
- **Tools**: nenhum
- **Done when**: validação JSON dos 2 arquivos passa; UI renderiza em ambos idiomas.
- **Tests**: validação JSON.
- **Gate**: validação JSON + `npx tsc -b` + `npm run build`.
- **Commit**: não (agrupa com T-09).

---

### T-11: README + CHANGELOG

- **What**: Documentar os 3 níveis de role per-app (`admin`/`editor`/`viewer`) e a função `ResolveAppRole` no `README.md` (tabela Platform + seção Dashboard) e entrada em `CHANGELOG.md` sob `## [Unreleased]`. Mirror nas 3 traduções (AGENTS §6).
- **Where**: `README.md`, `i18n/README.{pt-BR,pt-PT,es}.md`, `CHANGELOG.md`.
- **Depends on**: T-01 a T-10
- **Reuses**: convenção de `## [Unreleased]`.
- **Requirement**: AGENTS §6.
- **Tests**: nenhum.
- **Gate**: revisão visual do diff.
- **Commit**: `docs: document RBAC per-app and changelog entry`

---

### T-12: Integração final com `dashboard-global-roles` T-06

- **What**: Verificar que `CanReadAnyApp` é realmente chamado dentro de `ResolveAppRole` (já está em T-01, mas garantir que o teste cobre o caminho). Adicionar teste explícito do "cross-spec": `ResolveAppRole(admin global, app de terceiro) == AppRoleViewer`. Adicionar teste de regressão: se `CanReadAnyApp` retornar `false` (caso `dashboard-global-roles` ainda não esteja implementada), `ResolveAppRole` cai no lookup normal em `app_members` sem crash.
- **Where**: `internal/dashboard/rbac_test.go` (testes adicionais), comentário em `rbac.go` referenciando `dashboard-global-roles/design.md`.
- **Depends on**: T-01
- **Reuses**: testes existentes.
- **Requirement**: cumpre o contrato cross-spec documentado em `dashboard-global-roles/design.md` Seção Tech Decisions.
- **Tests**: adições ao `rbac_test.go`.
- **Gate**: `go test ./internal/dashboard/... -run TestResolveAppRole`
- **Commit**: não (merge com T-01 ou ticket à parte se preferir separar)

---

## Notas de execução

- T-01 sozinho é **unitariamente entregável** (cobre o core: schema + função central + cross-spec extension) e pode ser mergeado independente de T-02+. T-02 em diante dependem de T-01 mas não entre si para a maior parte (T-03 e T-04 podem rodar em paralelo com T-02).
- T-02 (migração de dados) é **pré-requisito não-bloqueante** para T-04/T-05 (enforcement), mas pode ser mergeado antes deles sem quebrar nada — só popula a tabela. Se enforcement subir antes da migração, donos atuais de apps perdem acesso até T-02 rodar.
- T-03 (invariante) é tecnicamente parte de T-06 (gestão), mas é separado porque a função `CountAppAdmins` + lock tem o seu próprio escopo de teste (race condition).
- T-08 (cleanup de `app_ownership`) é a **última** task porque depende do enforcement estar 100% verde. Antes dela, `app_ownership` é fallback de segurança.
- T-12 é ticket de "validação cross-spec" — pode ser mergeado junto com T-01 ou separado. Recomendado junto para evitar PR órfão.
- A remoção física de `apps.owner_id` / `frontend_apps.created_by` / `frontend_apps.owner_id` está **fora de escopo** — eles viram metadado, não fonte de autorização, mas não são dropados.
