# Tasks: Dashboard Global Roles

**Spec**: `.specs/features/dashboard-global-roles/spec.md`
**Design**: `.specs/features/dashboard-global-roles/design.md`
**Status**: Verified — T-01..T-09 implementados e mergeados (verificado 2026-08-10 contra o código)

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build`), mesmo critério das demais specs.

**Pré-requisito cruzado — resolvido**: T-06 (leitura irrestrita de apps para `admin`/`auditor`) dependia de `ResolveAppRole` existir (spec `rbac-per-app`). `rbac-per-app` foi implementada; `ResolveAppRole` vive em `internal/dashboard/rbac.go` e já chama `CanReadAnyApp` (linha 107). T-06 está entregue.

---

## Execution Plan

```
Fase 1: Migração                          Fase 2: Permissões de plataforma
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-01 migration role +      │───────────▶│ T-02 HasPlatformPermission  │
│      constraint            │             │ T-03 CanCreateUserWithRole  │
└──────────────────────────┘             │      + CreateUser edição    │
                                          └───────────────────────────┘
                                                     │
                                                     ▼
Fase 3: Gestão de usuários                Fase 4: Integração rbac-per-app
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-04 PATCH /users/{id}     │             │ T-06 CanReadAnyApp +         │
│      (mudar role)          │             │      extensão em            │
│ T-05 invariante ≥1          │────────────▶│      ResolveAppRole         │
│      superadmin             │             │      (bloqueado até         │
└──────────────────────────┘             │      rbac-per-app existir)  │
                                          └───────────────────────────┘
                                                     │
                                                     ▼
Fase 5: Frontend                          Fase 6: Docs e changelog
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-07 UI condicional por     │             │ T-09 README + CHANGELOG      │
│      role (omitir, não      │────────────▶└───────────────────────────┘
│      só desabilitar)        │
│ T-08 i18n en/pt-BR          │
└──────────────────────────┘
```

---

### T-01: Migration — 4 roles [x]

- **What**: `DROP CONSTRAINT IF EXISTS dashboard_users_role_check` (libera o UPDATE), `UPDATE dashboard_users SET role = 'member' WHERE role = 'admin'`, depois `ADD CONSTRAINT dashboard_users_role_check CHECK (role IN ('superadmin','admin','auditor','member'))`. **Ordem importa e é a inversa do que estava escrito antes**: drop → update → add. O UPDATE só passa se a OLD 2-value CHECK já tiver sido removida (a OLD constraint rejeita `member` mid-flight). As três statements rodam dentro da mesma transação do `ProvisionZeepSystem` (janela sem CHECK é atômica e invisível pra outras sessões).
- **Where**: `internal/dashboard/provisioner.go` (não `internal/provisioner/` — `ProvisionZeepSystem` é o orquestrador das migrations de `zeep_system`)
- **Depends on**: nenhuma
- **Reuses**: padrão de migration idempotente já usado no `provisioner.go` (advisory lock + IF NOT EXISTS + statements em lista)
- **Requirement**: DGR-01, DGR-02, DGR-03, DGR-04
- **Tools**: nenhum
- **Done when**: fixture com `admin`/`superadmin` pré-existentes migra corretamente; inserir role fora dos 4 valores falha na constraint; rodar a migration 2x não quebra nada
- **Tests**: `internal/dashboard/provisioner_roles_test.go::TestRoleMigration` — recria `dashboard_users` com a OLD constraint, semeia admin+superadmin, chama `ProvisionZeepSystem`, valida os 5 cenários (admin→member, superadmin intacto, constraint rejeita valor inválido, aceita os 4 valores, idempotente)
- **Gate**: `go test ./internal/dashboard/... -run TestRoleMigration` (exercitado contra PostgreSQL real local em 2026-08-03; spec originalmente apontava `./internal/provisioner/...` mas o local correto é `./internal/dashboard/...`)
- **Commit**: `feat(roles): migrate dashboard_users to 4-tier role model (superadmin/admin/auditor/member)` (T-01 sozinho, mudança de schema visível)

---

### T-02: `HasPlatformPermission` [x]

- **What**: Função central mapeando `(role, action) → bool` conforme a matriz de permissões do spec (templates/branding/usuários/integrações/infra/auditoria/apps próprios).
- **Where**: `internal/dashboard/platform_roles.go`
- **Depends on**: T-01
- **Reuses**: nenhum
- **Requirement**: DGR-10, DGR-11, DGR-13, DGR-14, DGR-15, DGR-16, DGR-17
- **Tools**: nenhum
- **Done when**: matriz completa (4 roles × 7 ações) testada, sem exceção não coberta
- **Tests**: tabela de casos cobrindo toda a matriz da Seção B do design
- **Gate**: `go test ./internal/dashboard/... -run TestHasPlatformPermission`
- **Commit**: não (agrupa com T-03)

---

### T-03: `CanCreateUserWithRole` + edição de `CreateUser` [x]

- **What**: `CanCreateUserWithRole(actorRole, targetRole) bool` (bloqueia qualquer não-`superadmin` tentando criar `role: superadmin`). Aplicado em `CreateUser` existente antes de persistir.
- **Where**: `internal/dashboard/platform_roles.go`, edição de `internal/dashboard/handler.go` (`CreateUser`)
- **Depends on**: T-02
- **Reuses**: `CreateUser` já existente (edição, não reescrita)
- **Requirement**: DGR-12
- **Tools**: nenhum
- **Done when**: `admin` tentando criar `superadmin` recebe 403; `admin` criando `member`/`auditor`/`admin` funciona normalmente
- **Tests**: teste HTTP cobrindo os 2 cenários
- **Gate**: `go test ./internal/dashboard/... -run TestCreateUserRoleGate`
- **Commit**: `feat(roles): add platform permission matrix and role-creation guard` (T-02+T-03)

---

### T-04: `PATCH /dashboard/api/users/{id}` (mudar role) [x]

- **What**: Endpoint novo para promover/rebaixar role de um usuário existente — não existe hoje (só `CreateUser`/`DeleteUser`). Aplica `CanCreateUserWithRole` (mesma regra vale pra mudança de role, não só criação) e gera `user.role_changed` no audit log.
- **Where**: `internal/dashboard/users.go` (novo)
- **Depends on**: T-03
- **Reuses**: `InsertAuditLog`
- **Requirement**: DGR-12 (extensão), edge case de auditoria da spec
- **Tools**: nenhum
- **Done when**: mudança de role funciona e é auditada; `admin` não consegue promover ninguém pra `superadmin` por esse endpoint também
- **Tests**: teste HTTP cobrindo promoção/rebaixamento + verificação de audit log
- **Gate**: `go test ./internal/dashboard/... -run TestUpdateUserRole`
- **Commit**: não (agrupa com T-05)

---

### T-05: Invariante "≥1 superadmin" [x]

- **What**: `PATCH .../users/{id}` (mudança de role) e `DELETE .../users/{id}` (já existente) passam a checar, antes de aplicar, se a operação resultaria em zero `dashboard_users` com `role = 'superadmin'` — se sim, 400.
- **Where**: `internal/dashboard/users.go`, edição de `DeleteUser` existente
- **Depends on**: T-04
- **Reuses**: mesma técnica de invariante de `rbac-per-app` ("≥1 admin por app"), aplicada ao nível de plataforma
- **Requirement**: Edge case "zero superadmin" da spec
- **Tools**: nenhum
- **Done when**: rebaixar ou deletar o último `superadmin` é rejeitado; com 2+ superadmins, a operação funciona normalmente
- **Tests**: teste HTTP com 1 e 2 superadmins, cobrindo os 2 cenários pra `PATCH` e `DELETE`
- **Gate**: `go test ./internal/dashboard/... -run TestLastSuperadminInvariant`
- **Commit**: `feat(roles): add self-service user role management with last-superadmin invariant` (T-04+T-05)

---

### T-06: `CanReadAnyApp` + extensão em `ResolveAppRole` (dependência de `rbac-per-app`) [x]

- **What**: `CanReadAnyApp(role string) bool` (true para `superadmin`/`admin`/`auditor`). Extensão em `ResolveAppRole` (de `rbac-per-app`, implementada lá) para retornar acesso de leitura quando `CanReadAnyApp(user.Role)` é verdadeiro, antes do lookup normal em `app_members`.
- **Where**: `internal/dashboard/platform_roles.go` (`CanReadAnyApp`), arquivo de `ResolveAppRole` (a localizar quando `rbac-per-app` for implementada)
- **Depends on**: T-02, e **bloqueado** até `ResolveAppRole` existir no código (spec `rbac-per-app`, hoje só especificada)
- **Reuses**: `ResolveAppRole` (função central de `rbac-per-app`)
- **Requirement**: DGR-20, DGR-21, DGR-22
- **Tools**: nenhum
- **Done when**: `admin`/`auditor` sem vínculo em `app_members` conseguem `GET` de qualquer app; qualquer ação de escrita nesse mesmo app continua 403 pra eles
- **Tests**: teste cruzado com `app_members` fixture — usuário `admin` global lê app de terceiro, tenta escrever, recebe 403
- **Gate**: `go test ./internal/dashboard/... -run TestCanReadAnyApp`
- **Commit**: `feat(roles): grant admin/auditor read-only access to all apps via ResolveAppRole extension` (T-06 sozinho, depende de código externo a esta spec)

---

### T-07: UI condicional por role (omitir, não desabilitar) [x]

- **What**: Navegação/telas do dashboard consultam a role do usuário logado e **omitem** (não apenas desabilitam) qualquer tela/ação fora da matriz de permissão dele — ex: `auditor` nunca vê o botão de "criar template" nem esmaecido.
- **Where**: componente de navegação/layout do dashboard (a localizar na implementação), hook novo tipo `useHasPlatformPermission(action)`
- **Depends on**: T-02 (via endpoint que expõe a role/permissões resolvidas ao frontend)
- **Reuses**: `usePublicConfig()`/padrão de config já carregado antes do primeiro paint
- **Requirement**: Edge case "UI nunca expõe tela sem permissão" da spec
- **Tests**: teste de componente com as 4 roles, confirmando itens de menu ausentes (não só desabilitados)
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-08)

---

### T-08: i18n das strings novas [x]

- **What**: Strings novas de T-07 (mensagens de erro 403 traduzidas no frontend, labels de role na UI de usuários) em `en.json`/`pt-BR.json`.
- **Where**: `internal/dashboard/ui/src/locales/en.json`, `pt-BR.json`
- **Depends on**: T-07
- **Reuses**: `react-i18next` já configurado
- **Requirement**: nenhum ID específico — regra geral do `AGENTS.md`
- **Tools**: nenhum
- **Done when**: validação JSON dos 2 arquivos passa
- **Tests**: validação JSON
- **Gate**: validação JSON + `npx tsc -b` + `npm run build`
- **Commit**: `feat(roles): add role-aware UI navigation and i18n` (T-07+T-08)

---

### T-09: README e CHANGELOG [x]

- **What**: Documentar os 4 níveis de role e a matriz de permissões no `README.md`; entrada em `CHANGELOG.md` sob `## [Unreleased]`.
- **Where**: `README.md`, `CHANGELOG.md`
- **Depends on**: T-01 até T-08
- **Reuses**: convenção existente de `## [Unreleased]`
- **Requirement**: nenhum ID específico — item de processo (`AGENTS.md` seção 6)
- **Tests**: nenhum
- **Gate**: revisão visual do diff
- **Commit**: `docs: document dashboard global roles (superadmin/admin/auditor/member) and changelog entry`

---

## Notas de execução

- T-01 a T-05 e T-07/T-08 não dependem de `rbac-per-app` — podem ser implementadas e mergeadas independentemente, antes ou depois daquela spec.
- T-06 é a única task com dependência real de código externo (`ResolveAppRole` de `rbac-per-app`) — se `dashboard-global-roles` for implementada primeiro, T-06 fica pendente até `rbac-per-app` existir; se a ordem for invertida, quem implementar `rbac-per-app` precisa saber que este spec depende dessa função (linkar esta spec no PR/commit de `ResolveAppRole` quando implementada).
- Nenhuma tela de permissão granular por ação é criada — a granularidade é por tela/recurso inteiro, mesmo nível de `rbac-per-app`.
