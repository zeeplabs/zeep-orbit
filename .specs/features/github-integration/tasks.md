# GitHub Integration Tasks

**Design**: `.specs/features/github-integration/design.md`
**Status**: Draft

**Nota sobre gates**: não existe `.specs/codebase/TESTING.md` neste repo. Gates abaixo foram inferidos dos comandos reais do `Makefile` (`go test ./...`, `go vet ./...`, `dashboard-build`) e do padrão já usado em `.specs/features/mvp-core/tasks.md` (gate por pacote: `go test ./internal/{pkg}/...`). Nada inventado — confirmado no `Makefile` do repo.

**Convenção de Gate**:
- `quick` = `go test ./internal/{pkg}/...` (pacote isolado)
- `full` = `go test ./...` + `go vet ./...`
- `build` = `go build ./...` (mudança estrutural sem lógica testável isoladamente)
- `ui-build` = `cd internal/dashboard/ui && npm run build`

---

## Execution Plan

### Phase 1 — Foundation (Parallel)

```
T-01 [P] ─┐
T-02 [P] ─┴─→ (Phase 2)
```

### Phase 2 — Client + Stores (Parallel, T-03 sequential after T-02)

```
T-01 ──→ T-05 [P] ─┐
T-01 ──→ T-06 [P] ─┤
T-02 ──→ T-03 ─────┴─→ (Phase 3)
```

### Phase 3 — Validation Spike (Sequential gate)

```
T-03 ──→ T-04
```

### Phase 4 — Handlers (Parallel)

```
T-05, T-02 ──→ T-07 [P] ─┐
T-06, T-03, T-04 ──→ T-08 [P] ─┴─→ (Phase 5)
```

### Phase 5 — Wiring (Sequential)

```
T-07, T-08 ──→ T-09
```

### Phase 6 — UI (Parallel)

```
T-09 ──→ T-10 [P] ─┐
T-09 ──→ T-11 [P] ─┴─→ (Phase 7)
```

### Phase 7 — Nav (Sequential)

```
T-10, T-11 ──→ T-12
```

---

## Task Breakdown

### T-01: Provisionar tabelas `github_app_config` e `github_templates`

**What**: Adicionar DDL das duas tabelas novas em `internal/dashboard/provisioner.go`, seguindo o bloco de migrações existente (mesmo padrão de `system_config`, incluindo unique index `((TRUE))` pro singleton).
**Where**: `internal/dashboard/provisioner.go`
**Depends on**: None
**Reuses**: Padrão de `CREATE TABLE IF NOT EXISTS zeep_system.system_config` (linha 114-121) para o singleton; padrão de tabela com UUID PK (`audit_log`, `app_tokens`) para `github_templates`
**Requirement**: GH-01, GH-10

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `github_app_config` criada com unique index singleton `((TRUE))`
- [ ] `github_templates` criada com PK UUID, `active BOOLEAN DEFAULT true`
- [ ] Gate check passa: `go build ./...`

**Tests**: none (schema puro; coberto indiretamente pelos testes de integração de T-07/T-08 que dependem das tabelas existirem)
**Gate**: build

---

### T-02: Installation token cache + assinatura JWT do App [P]

**What**: Implementar `internal/github/token.go` — gera JWT RS256 do App (App ID + private key), troca por installation token via `POST /app/installations/{id}/access_tokens`, cacheia até expirar (~1h) com mutex.
**Where**: `internal/github/token.go`
**Depends on**: None
**Reuses**: Padrão de cache TTL+mutex de `internal/tokencache` (cache de jti do App Tokens)
**Requirement**: GH-01

**Tools**: MCP: `context7` (resolver formato exato do JWT de App exigido pelo GitHub — `iss`/`iat`/`exp`, algoritmo RS256, endpoint de troca — antes de codar) · Skill: NONE

**Done when**:
- [ ] JWT RS256 gerado corretamente (`iss=app_id`, `exp` ~10min conforme spec do GitHub)
- [ ] Installation token obtido e cacheado, renovado só quando expira
- [ ] Unit tests: geração de JWT, cache hit, cache expirado força renovação (mock HTTP)
- [ ] Gate check passa: `go test ./internal/github/...`
- [ ] Test count: mínimo 4 testes passam (JWT válido, JWT com private key inválida, cache hit, cache miss/renovação)

**Tests**: unit
**Gate**: quick

---

### T-03: Cliente GitHub API — `VerifyTemplateRepo` + `CreateRepoFromTemplate`

**What**: Implementar `internal/github/client.go` com `NewClient(cfg AppConfig)`, `VerifyTemplateRepo(ctx, owner, repo) error` (`GET /repos/{owner}/{repo}`, checa `is_template`), `CreateRepoFromTemplate(ctx, templateOwner, templateRepo, newRepoSlug) (repoURL string, err error)` (`POST /repos/{owner}/{repo}/generate`, `private: true`), `Status(ctx) (StatusResult, error)`.
**Where**: `internal/github/client.go`
**Depends on**: T-02
**Reuses**: `internal/github.InstallationTokenCache` de T-02; `golang-jwt/v5` já presente no `go.mod`
**Requirement**: GH-11, GH-12, GH-20, GH-21, GH-22, GH-23

**Tools**: MCP: `context7` (confirmar payload/resposta exatos de `GET /repos/{owner}/{repo}` e `POST /repos/{owner}/{repo}/generate`, e headers de rate limit) · Skill: NONE

**Done when**:
- [ ] `VerifyTemplateRepo` retorna erro claro quando repo não existe (404) ou `is_template: false`
- [ ] `CreateRepoFromTemplate` retorna URL do repo criado; propaga erro 422 (slug já existe) sem retry silencioso
- [ ] `CreateRepoFromTemplate` propaga erro 403 (permissão insuficiente) com mensagem específica
- [ ] Rate limit (429/403 com header `X-RateLimit-Remaining: 0`) tratado como `ErrRateLimited` com tempo de espera
- [ ] Unit tests com mock HTTP cobrindo todos os casos acima
- [ ] Gate check passa: `go test ./internal/github/...`
- [ ] Test count: mínimo 6 testes passam (verify ok, verify not-template, verify 404, create ok, create conflict, create rate-limited)

**Tests**: unit
**Gate**: quick

---

### T-04: Spike de validação — installation ganha acesso automático ao repo gerado

**What**: Teste de integração contra org GitHub de sandbox: instalar App com `repository_selection: selected`, chamar `CreateRepoFromTemplate`, e imediatamente usar o MESMO installation token pra chamar `GET /repos/{org}/{new_repo}/contents` — confirmar 200 sem precisar de re-autorização manual. Documentar resultado (confirma ou refuta a suposição `[Provável]` do design.md).
**Where**: `internal/github/client_integration_test.go` (build tag `integration`, mesmo padrão de `internal/db/client_test.go` contra Postgres real)
**Depends on**: T-03
**Reuses**: Padrão de integration test com build tag já usado em `internal/provisioner/provisioner_test.go`
**Requirement**: (valida premissa de design, sem REQ próprio — gate de risco técnico)

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Teste roda contra org GitHub de sandbox real (não mock)
- [ ] Resultado documentado: instalação ganha acesso automático (confirma design) OU não ganha (design precisa de ajuste — reportar antes de prosseguir pra T-08)
- [ ] Gate check passa: `go test -tags=integration ./internal/github/...`

**Tests**: integration
**Gate**: full

**⚠️ Gate de bloqueio**: se o resultado for negativo (não ganha acesso automático), PARE — não prossiga para T-08 sem antes revisar o design.md (pode precisar de uma chamada extra de "add repo to installation" após criar).

---

### T-05: Store `github_app_config` (CRUD criptografado) [P]

**What**: Implementar `internal/dashboard/github_config_store.go` — `GetGitHubConfig(ctx, pool)`, `UpsertGitHubConfig(ctx, pool, input)` (criptografa `client_secret`/`private_key`/`webhook_secret` com `crypto.Encrypt`, preserva valor existente quando campo vem vazio no update).
**Where**: `internal/dashboard/github_config_store.go`
**Depends on**: T-01
**Reuses**: Estrutura de `auth_providers_store.go` (Get/Upsert com campo `_encrypted` e preservação de valor existente)
**Requirement**: GH-01, GH-02, GH-03

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `UpsertGitHubConfig` criptografa os 3 campos sensíveis antes de persistir
- [ ] Update parcial preserva `private_key` existente quando não reenviada
- [ ] Unit tests: insert, update parcial, decrypt round-trip
- [ ] Gate check passa: `go test ./internal/dashboard/...`
- [ ] Test count: mínimo 3 testes passam

**Tests**: unit
**Gate**: quick

---

### T-06: Store `github_templates` (CRUD) [P]

**What**: Implementar `internal/dashboard/github_templates_store.go` — `ListGitHubTemplates`, `CreateGitHubTemplate`, `UpdateGitHubTemplate`, `SetGitHubTemplateActive`.
**Where**: `internal/dashboard/github_templates_store.go`
**Depends on**: T-01
**Reuses**: Padrão de store simples de `apps_store.go` (Create/List/Update sem criptografia)
**Requirement**: GH-13, GH-14

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] CRUD completo contra `github_templates`, soft toggle via `active`
- [ ] Unit tests: create, list (só ativos vs todos), update, desativar
- [ ] Gate check passa: `go test ./internal/dashboard/...`
- [ ] Test count: mínimo 4 testes passam

**Tests**: unit
**Gate**: quick

---

### T-07: Handlers `github_config` (config, status, install callback, disconnect) [P]

**What**: Implementar `internal/dashboard/github_config.go` — `POST /api/github/config`, `GET /api/github/status`, `GET /api/github/install/callback`, `DELETE /api/github/config`. Valida credenciais chamando `internal/github.Client.Status` antes de persistir. Chama `h.audit(...)` em toda mutação.
**Where**: `internal/dashboard/github_config.go`
**Depends on**: T-05, T-02
**Reuses**: `middleware.RequireSuperadmin`; `h.audit(...)` de `audit_store.go`; padrão de handler de `system_config` handler existente
**Requirement**: GH-01, GH-02, GH-03, GH-04, GH-05, GH-06, GH-07

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `POST /api/github/config` rejeita credenciais inválidas (400) sem persistir
- [ ] `POST /api/github/config` válido persiste criptografado e retorna 200
- [ ] `GET /api/github/status` retorna `connected: false` quando installation revogada (401/404 do GitHub)
- [ ] `GET /api/github/install/callback` persiste `installation_id`, `org_login`, `installed_at`
- [ ] Todas as mutações geram entrada em `audit_log` (`github.config.update`, `github.install`)
- [ ] Integration tests contra PostgreSQL real cobrindo os 4 endpoints (padrão de `handler_test.go`)
- [ ] Gate check passa: `go test ./internal/dashboard/... && go vet ./...`
- [ ] Test count: mínimo 6 testes passam

**Tests**: integration
**Gate**: full

**Commit**: `feat(github): add GitHub App config and installation endpoints`

---

### T-08: Handlers `github_templates` (CRUD + validação de template) [P]

**What**: Implementar `internal/dashboard/github_templates.go` — `GET/POST/PUT/DELETE /api/github/templates`. `POST`/`PUT` chamam `internal/github.Client.VerifyTemplateRepo` antes de persistir.
**Where**: `internal/dashboard/github_templates.go`
**Depends on**: T-06, T-03, T-04
**Reuses**: `middleware.RequireSuperadmin`; `h.audit(...)`
**Requirement**: GH-10, GH-11, GH-12, GH-13, GH-14

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `POST /api/github/templates` rejeita repo inexistente ou não-template com mensagem clara
- [ ] `POST /api/github/templates` válido persiste com `active: true`
- [ ] `DELETE` (ou toggle) marca `active: false` sem apagar registro
- [ ] Todas as mutações geram entrada em `audit_log` (`github.template.create/update/delete`)
- [ ] Integration tests contra PostgreSQL real + mock/sandbox do GitHub
- [ ] Gate check passa: `go test ./internal/dashboard/... && go vet ./...`
- [ ] Test count: mínimo 5 testes passam

**Tests**: integration
**Gate**: full

**Commit**: `feat(github): add template repository CRUD with template validation`

---

### T-09: Wire rotas + middleware superadmin

**What**: Registrar as 8 rotas novas (`/api/github/config`, `/api/github/status`, `/api/github/install/callback`, `/api/github/templates*`) no router do dashboard, todas atrás de `middleware.RequireSuperadmin` exceto o callback de instalação (que vem do redirect do GitHub, sem sessão — validar via `state` param assinado, mesmo padrão do Google OAuth callback).
**Where**: `internal/dashboard/server.go` (ou arquivo de rotas equivalente)
**Depends on**: T-07, T-08
**Reuses**: Padrão de wiring de rotas do Google OAuth (`internal/dashboard/google.go` callback sem sessão + state assinado)
**Requirement**: GH-01 a GH-14 (wiring, sem lógica nova)

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Todas as 8 rotas respondem no smoke test manual
- [ ] Callback de instalação não exige sessão, mas valida `state` assinado (anti-CSRF)
- [ ] Demais rotas retornam 403 sem sessão superadmin
- [ ] Gate check passa: `go build ./... && go test ./... && go vet ./...`

**Tests**: none (wiring puro; lógica já testada em T-07/T-08)
**Gate**: full

---

### T-10: UI — página "Integrações → GitHub" [P]

**What**: Criar `GitHubIntegrationPage.tsx` — formulário de credenciais, botão "Instalar na Org", badge de status conectado/desconectado, botão desconectar.
**Where**: `internal/dashboard/ui/src/pages/GitHubIntegrationPage.tsx`
**Depends on**: T-09
**Reuses**: Padrão de página de config superadmin-only (`BrandSettingsPage.tsx` ou `AuthProvidersPage` equivalente)
**Requirement**: GH-01 a GH-07 (UI)

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Form salva credenciais via `POST /api/github/config`
- [ ] Botão "Instalar na Org" redireciona pro fluxo do GitHub
- [ ] Badge reflete `GET /api/github/status` em tempo real
- [ ] Gate check passa: `npm run build` (ver Makefile `dashboard-build`)

**Tests**: none (sem TESTING.md definindo e2e obrigatório pra esta página; `test-e2e`/Playwright existente cobre outras páginas — adicionar e2e é decisão de produto, não bloqueante aqui)
**Gate**: ui-build

---

### T-11: UI — página/seção "Templates" [P]

**What**: Criar seção de gestão de templates (lista, criar, editar, desativar) dentro da mesma tela de Integrações ou página própria `GitHubTemplatesPage.tsx`.
**Where**: `internal/dashboard/ui/src/pages/GitHubTemplatesPage.tsx`
**Depends on**: T-09
**Reuses**: Padrão de tabela+modal já usado em `UsersPage.tsx` / `AppsPage.tsx`
**Requirement**: GH-10 a GH-14 (UI)

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Lista templates com nome, framework, status ativo
- [ ] Modal de criação valida erro do backend (repo não é template) e exibe mensagem clara
- [ ] Toggle ativar/desativar funcional
- [ ] Gate check passa: `npm run build`

**Tests**: none (mesma justificativa de T-10)
**Gate**: ui-build

---

### T-12: Sidebar nav "Integrações" (superadmin only)

**What**: Adicionar item de navegação no sidebar apontando pras páginas de T-10/T-11, visível só para superadmin (mesmo padrão de "Auditoria"/"Usuários").
**Where**: `internal/dashboard/ui/src/components/DashboardShell.tsx` (ou `Sidebar.tsx` equivalente)
**Depends on**: T-10, T-11
**Reuses**: Guard `superadmin-only` já usado nos itens "Usuários"/"Auditoria"/"Aparência"
**Requirement**: (UX, sem REQ próprio)

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Item "Integrações" visível só pra superadmin
- [ ] Navega corretamente pras duas páginas novas
- [ ] Gate check passa: `npm run build`

**Tests**: none
**Gate**: ui-build

---

## Parallel Execution Map

```
Phase 1 (Parallel):
  T-01 [P]
  T-02 [P]

Phase 2 (T-01 done → parallel; T-02 done → sequential T-03):
  ├── T-05 [P] (needs T-01)
  ├── T-06 [P] (needs T-01)
  └── T-03      (needs T-02)

Phase 3 (Sequential gate):
  T-03 done → T-04

Phase 4 (Parallel):
  ├── T-07 [P] (needs T-05, T-02)
  └── T-08 [P] (needs T-06, T-03, T-04)

Phase 5 (Sequential):
  T-07, T-08 done → T-09

Phase 6 (Parallel):
  ├── T-10 [P] (needs T-09)
  └── T-11 [P] (needs T-09)

Phase 7 (Sequential):
  T-10, T-11 done → T-12
```

---

## Task Granularity Check

| Task | Scope | Status |
|---|---|---|
| T-01 | 1 migration (2 tabelas relacionadas, cohesivas) | ✅ Granular |
| T-02 | 1 componente (token cache) | ✅ Granular |
| T-03 | 1 componente (client), 2 métodos cohesivos | ✅ Granular |
| T-04 | 1 teste de validação | ✅ Granular |
| T-05 | 1 store | ✅ Granular |
| T-06 | 1 store | ✅ Granular |
| T-07 | 1 arquivo, 4 endpoints cohesivos do mesmo domínio (config) | ✅ Granular |
| T-08 | 1 arquivo, 4 endpoints cohesivos do mesmo domínio (templates) | ✅ Granular |
| T-09 | 1 mudança de wiring | ✅ Granular |
| T-10 | 1 página | ✅ Granular |
| T-11 | 1 página | ✅ Granular |
| T-12 | 1 componente (nav item) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
|---|---|---|---|
| T-01 | None | None | ✅ Match |
| T-02 | None | None | ✅ Match |
| T-03 | T-02 | T-02 | ✅ Match |
| T-04 | T-03 | T-03 | ✅ Match |
| T-05 | T-01 | T-01 | ✅ Match |
| T-06 | T-01 | T-01 | ✅ Match |
| T-07 | T-05, T-02 | T-05, T-02 | ✅ Match |
| T-08 | T-06, T-03, T-04 | T-06, T-03, T-04 | ✅ Match |
| T-09 | T-07, T-08 | T-07, T-08 | ✅ Match |
| T-10 | T-09 | T-09 | ✅ Match |
| T-11 | T-09 | T-09 | ✅ Match |
| T-12 | T-10, T-11 | T-10, T-11 | ✅ Match |

---

## Test Co-location Validation

Sem `TESTING.md` formal; matriz inferida do padrão real do repo (`internal/provisioner/provisioner_test.go`, `internal/server/handler_test.go` = integration contra Postgres real; `internal/query/builder_test.go` = unit; UI sem teste unitário, só Playwright e2e opcional em `internal/dashboard/ui/e2e/`).

| Task | Code Layer Created/Modified | Inferred Requirement | Task Says | Status |
|---|---|---|---|---|
| T-01 | Migração de schema | none (padrão do repo: migrações não têm teste próprio) | none | ✅ OK |
| T-02 | Lógica de auth/cache (internal package) | unit | unit | ✅ OK |
| T-03 | Client HTTP (internal package) | unit | unit | ✅ OK |
| T-04 | Validação de integração real | integration | integration | ✅ OK |
| T-05 | Store (dashboard) | unit | unit | ✅ OK |
| T-06 | Store (dashboard) | unit | unit | ✅ OK |
| T-07 | Handler HTTP (dashboard) | integration (padrão `handler_test.go`) | integration | ✅ OK |
| T-08 | Handler HTTP (dashboard) | integration | integration | ✅ OK |
| T-09 | Wiring de rotas | none (coberto pelos testes de T-07/T-08) | none | ✅ OK |
| T-10 | Página React | none (sem padrão de teste unitário de página no repo) | none | ✅ OK |
| T-11 | Página React | none | none | ✅ OK |
| T-12 | Nav item | none | none | ✅ OK |

---

## Tools confirmados (Execute)

- **Context7 MCP**: atribuído a T-02 e T-03 — validar formato do JWT de App e payload/resposta dos endpoints do GitHub antes de codar (evita fabricar API).
- **`code-review` skill**: rodar ao final de CADA fase (não task a task), antes de avançar pra próxima. Checkpoints: após Phase 1, Phase 2, Phase 4, Phase 6. (Phase 3 = só o spike T-04, Phase 5/7 = wiring puro — code-review dispensável ali, mas rodar se o diff acumulado ainda não foi revisado.)
- **`verify` skill**: rodar após T-09 (fim da Phase 5 — wiring completo, backend fim-a-fim testável via curl/Postman) e após T-12 (fim da Phase 7 — fluxo completo na UI: conectar GitHub, cadastrar template, ver status).
