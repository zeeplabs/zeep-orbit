# Tasks: Frontend App Entity

**Spec**: `.specs/features/frontend-app-entity/spec.md`
**Design**: `.specs/features/frontend-app-entity/design.md`
**Status**: Verified — T-01..T-10 implementados e mergeados (verificado 2026-08-10: `internal/dashboard/frontend_apps.go` + `frontend_apps_store.go`; rotas `GET/POST /api/frontend-apps`, `GET/DELETE /api/frontend-apps/{id}`, `POST /api/frontend-apps/{id}/retry` em `internal/server/server.go:224-228`; UI `src/pages/FrontendAppsPage.tsx`)

> Convenção de Gate: não há `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `dashboard-build`), mesmo critério usado em `mvp-core/tasks.md` e `github-integration/tasks.md`.

**Pré-requisito externo**: todas as tasks abaixo dependem de `internal/github.Client` (T-01..T-12 de `.specs/features/github-integration/tasks.md`) estarem implementadas e mergeadas. Nenhuma task aqui deve começar antes disso — sem `CreateRepoFromTemplate` funcional não há o que chamar.

---

## Execution Plan

```
Fase 1: Dado                    Fase 2: Store              Fase 3: Handlers de leitura
┌─────────────────────┐        ┌──────────────────┐        ┌──────────────────────┐
│ T-01 DDL             │───────▶│ T-02 slugify       │──────▶│ T-05 GET list/detail  │
│ frontend_apps table  │        │ T-03 store CRUD    │        │                       │
└─────────────────────┘        └──────────────────┘        └──────────────────────┘
                                         │
                                         ▼
Fase 4: Handler de escrita (core)                  Fase 5: Retry e Delete
┌────────────────────────────────┐                 ┌──────────────────────────┐
│ T-04 POST /frontend-apps        │────────────────▶│ T-06 POST .../retry       │
│ (slugify+valida+CreateRepo)     │                 │ T-07 DELETE (archive)     │
└────────────────────────────────┘                 └──────────────────────────┘
                                                              │
                                                              ▼
Fase 6: Observabilidade                            Fase 7: UI e verificação
┌──────────────────┐                               ┌──────────────────────────┐
│ T-08 audit log     │──────────────────────────────▶│ T-09 testes integração    │
└──────────────────┘                               │ T-10 UI dashboard         │
                                                    └──────────────────────────┘
```

---

### T-01: Provisionar tabela `frontend_apps` [x]

- **What**: Adicionar DDL de `zeep_system.frontend_apps` (colunas conforme design.md, FK pra `github_templates`, unique index parcial em `slug`) ao bloco de provisionamento existente.
- **Where**: `internal/dashboard/provisioner.go`
- **Depends on**: nenhuma (dentro desta feature); externamente, `github_templates` já deve existir (github-integration T-01)
- **Reuses**: mesmo bloco de `CREATE TABLE IF NOT EXISTS` das tabelas `zeep_system.*` existentes
- **Requirement**: FA-01 (pré-condição de dado)
- **Tools**: nenhum externo necessário
- **Done when**: `zeep apply`/bootstrap cria a tabela num banco limpo, FK e unique index presentes
- **Tests**: teste de provisionamento (subir banco de teste, rodar bootstrap, checar `information_schema.tables` e `pg_indexes`)
- **Gate**: `go test ./internal/dashboard/... -run Provision` + `go vet ./...`
- **Commit**: não (agrupa com T-02/T-03 no fim da Fase 2)

---

### T-02: Helper de slugify [x]

- **What**: Função `slugify(name string) string` — lowercase, espaço/underscore → hífen, remove caracteres fora de `[a-z0-9-]`, colapsa hífens repetidos, trim.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: nenhuma
- **Reuses**: nenhum helper existente identificado no repo (confirmado por busca — não há `slugify` em `internal/`)
- **Requirement**: FA-01
- **Tools**: nenhum
- **Done when**: função pura, sem I/O, cobre casos de acento/espaço/símbolo
- **Tests**: tabela de casos (`"Meu App Legal"` → `"meu-app-legal"`, `"  Café   "` → `"caf-e"` ou equivalente definido no teste, string vazia → erro de validação upstream)
- **Gate**: `go test ./internal/dashboard/... -run Slugify`
- **Commit**: não (agrupa com T-03)

---

### T-03: `frontendAppsStore` (CRUD) [x]

- **What**: Implementar `Create`, `Get`, `List`, `UpdateStatus`, `Archive`, `SlugExists` contra `zeep_system.frontend_apps`.
- **Where**: `internal/dashboard/frontend_apps_store.go`
- **Depends on**: T-01
- **Reuses**: padrão de store `pgx` simples já usado por `github_templates` store
- **Requirement**: FA-01, FA-20, FA-21, FA-30
- **Tools**: nenhum
- **Done when**: todas as 6 operações implementadas e testadas isoladamente contra banco de teste
- **Tests**: teste de integração com banco real (testcontainers ou banco de teste do repo, seguindo padrão existente) cobrindo insert, update de status, archive, e violação do unique index parcial
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsStore`
- **Commit**: `feat(frontend-apps): add DDL, slugify helper and store CRUD` (T-01+T-02+T-03)

---

### T-04: `POST /api/frontend-apps` (criação) [x]

- **What**: Handler que recebe `{name, template_id}`, valida template ativo, checa GitHub conectado, slugifica, checa `SlugExists`, chama `CreateRepoFromTemplate`, persiste resultado (`ready` ou `failed`).
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-02, T-03, `github-integration` T-08 (`CreateRepoFromTemplate` implementado)
- **Reuses**: `middleware.RequireAuth`, leitura de `github_templates` store, `internal/github.Client`
- **Requirement**: FA-01, FA-02, FA-03, FA-04
- **Tools**: nenhum externo — comportamento já validado na spike do github-integration (T-04 lá)
- **Done when**: request válido cria repo real e persiste `status: ready`; cada rejeição (AC 2/3/4 do spec) retorna erro específico sem chamar o GitHub desnecessariamente
- **Tests**: teste de integração contra org GitHub de sandbox (repo real criado) + testes unitários dos 3 caminhos de rejeição com client mockado
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsCreate` + `go vet ./...`
- **Commit**: `feat(frontend-apps): add POST /api/frontend-apps creation endpoint`

---

### T-05: `GET /api/frontend-apps` e `GET /api/frontend-apps/{id}` [x]

- **What**: Handlers de listagem (não-arquivados) e detalhe.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-03
- **Reuses**: `middleware.RequireAuth`
- **Requirement**: FA-20, FA-21
- **Tools**: nenhum
- **Done when**: listagem reflete exatamente `archived_at IS NULL`; detalhe 404 em id inexistente ou arquivado
- **Tests**: teste unitário com store fake/mockado cobrindo lista vazia, lista com `ready`+`failed`, detalhe 404
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsList`
- **Commit**: `feat(frontend-apps): add list and detail endpoints`

---

### T-06: `POST /api/frontend-apps/{id}/retry` [x]

- **What**: Handler que rejeita se status ≠ `failed`, senão refaz o mesmo fluxo de criação (checagem de template ativo + `SlugExists` + `CreateRepoFromTemplate`) sobre o registro existente.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-04
- **Reuses**: mesma lógica de validação/chamada de T-04 (extrair função compartilhada `attemptCreate` pra evitar duplicação)
- **Requirement**: FA-10, FA-11, FA-12, FA-13
- **Tools**: nenhum
- **Done when**: retry em `failed` por rate limit simulado transiciona pra `ready`; retry em `failed` por slug duplicado falha de novo com a mesma mensagem; retry em `ready` é rejeitado
- **Tests**: 3 casos acima, com client mockado pros dois primeiros e store real pro terceiro
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsRetry`
- **Commit**: `feat(frontend-apps): add retry endpoint for failed creations`

---

### T-07: `DELETE /api/frontend-apps/{id}` [x]

- **What**: Handler que aplica soft delete (`Archive`) e então chama arquivamento do repo no GitHub (`archived: true`); se o arquivamento remoto falhar, loga erro mas não reverte o soft delete local.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Where (github client)**: adicionar `(c *Client) ArchiveRepo(ctx, owner, repo string) error` em `internal/github/client.go` se ainda não existir (checar se github-integration já previu isso — se não, é acréscimo mínimo desta feature)
- **Depends on**: T-03
- **Reuses**: `internal/github.Client`
- **Requirement**: FA-30, FA-31, FA-32, FA-33
- **Tools**: `MCP: context7` (confirmar payload exato de `PATCH /repos/{owner}/{repo}` com `archived: true`, já que não estava no escopo original do github-integration)
- **Done when**: delete bem-sucedido soft-deleta e arquiva; delete com falha remota simulada ainda soft-deleta local e loga
- **Tests**: 2 casos acima com client mockado (sucesso e falha na chamada remota)
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsDelete` + `go vet ./...`
- **Commit**: `feat(frontend-apps): add delete endpoint with GitHub repo archival`

---

### T-08: Audit log [x]

- **What**: Instrumentar `h.audit(...)` nos 3 fluxos mutantes: `frontend_app.create`, `frontend_app.retry`, `frontend_app.delete`.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-04, T-06, T-07
- **Reuses**: `audit_store.InsertAuditLog` (`h.audit(...)`)
- **Requirement**: FA-05, FA-33
- **Tools**: nenhum
- **Done when**: cada uma das 3 ações gera exatamente 1 entrada de audit log com slug/id relevante
- **Tests**: teste de integração checando contagem e conteúdo de `audit_log` após cada ação
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsAudit`
- **Commit**: `feat(frontend-apps): wire audit log for create, retry and delete`

---

### T-09: Testes de integração end-to-end [x]

- **What**: Suite cobrindo o fluxo completo contra org GitHub de sandbox: criar → listar → forçar falha → retry → deletar → confirmar arquivamento.
- **Where**: `internal/dashboard/frontend_apps_integration_test.go` (ou equivalente, seguindo convenção de testes de integração já usada em github-integration)
- **Depends on**: T-01..T-08
- **Reuses**: mesma infraestrutura de teste de integração da spike/testes do github-integration (T-04/T-09 lá)
- **Requirement**: todos os FA-* (cobertura completa)
- **Tools**: `verify` skill (rodar após esta task, conforme confirmado)
- **Done when**: suite passa de ponta a ponta contra sandbox real, sem mocks
- **Tests**: é a própria task
- **Gate**: `go test ./internal/dashboard/... -tags=integration`
- **Commit**: `test(frontend-apps): add end-to-end integration test suite`

---

### T-10: UI do dashboard [x]

- **What**: Tela "Frontend Apps" — formulário de criação (nome + select de template ativo), listagem com status/badge (ready/failed), botão retry em falhos, botão delete.
- **Where**: `internal/dashboard/ui/` (seguir estrutura de páginas existente do dashboard React)
- **Depends on**: T-04, T-05, T-06, T-07
- **Reuses**: componentes de tabela/form já existentes no dashboard (mesmo padrão da tela de Integrações → GitHub)
- **Requirement**: FA-01, FA-20, FA-21, FA-11, FA-30
- **Tools**: nenhum
- **Done when**: fluxo completo utilizável manualmente no browser (criar, ver erro, retry, deletar)
- **Tests**: Playwright e2e básico (criar 1 app, confirmar na lista) seguindo `test-e2e` do Makefile
- **Gate**: `dashboard-build` + `test-e2e` (Playwright)
- **Commit**: `feat(frontend-apps): add dashboard UI for frontend app management`

---

## Parallel Execution Map

- T-01, T-02 podem rodar em paralelo (sem dependência entre si)
- T-03 depende de T-01 (precisa da tabela) — não paraleliza com T-01
- T-05 pode começar assim que T-03 terminar, em paralelo com T-04 (ambos só dependem de T-03, não um do outro)
- T-06 depende estritamente de T-04 (reusa lógica), não paraleliza
- T-08 só depende do output de T-04/T-06/T-07 — pode ser feito incrementalmente junto de cada um em vez de task separada, mas mantido isolado aqui pra checkpoint de review dedicado
- T-09 e T-10 são os únicos que dependem de tudo — não paralelizam com nada anterior

---

## Task Granularity Check

| Task | Escopo em 1 sessão? | Testável isoladamente? |
|---|---|---|
| T-01 | ✅ (1 arquivo, DDL) | ✅ |
| T-02 | ✅ (função pura) | ✅ |
| T-03 | ✅ (1 arquivo novo) | ✅ |
| T-04 | ✅ (1 handler, lógica linear) | ✅ |
| T-05 | ✅ (2 handlers simples) | ✅ |
| T-06 | ✅ (reusa T-04, handler fino) | ✅ |
| T-07 | ✅ (1 handler + 1 método novo no client) | ✅ |
| T-08 | ✅ (instrumentação pontual) | ✅ |
| T-09 | ✅ (suite única, escopo fechado) | ✅ |
| T-10 | ✅ (1 tela, componentes reusados) | ✅ |

## Diagram-Definition Cross-Check

| Fase no diagrama | Tasks correspondentes | Consistente? |
|---|---|---|
| Fase 1: Dado | T-01 | ✅ |
| Fase 2: Store | T-02, T-03 | ✅ |
| Fase 3: Handlers de leitura | T-05 | ✅ |
| Fase 4: Handler de escrita | T-04 | ✅ |
| Fase 5: Retry e Delete | T-06, T-07 | ✅ |
| Fase 6: Observabilidade | T-08 | ✅ |
| Fase 7: UI e verificação | T-09, T-10 | ✅ |

## Test Co-location Validation

| Task | Teste no mesmo commit? | Nota |
|---|---|---|
| T-01 | ✅ | teste de provisionamento junto do DDL |
| T-02 | ✅ | tabela de casos junto da função |
| T-03 | ✅ | teste de store junto do CRUD |
| T-04 | ✅ | integração + unitários junto do handler |
| T-05 | ✅ | unitários com store mockado |
| T-06 | ✅ | 3 casos junto do handler |
| T-07 | ✅ | 2 casos junto do handler |
| T-08 | ✅ | teste de contagem/conteúdo de audit_log |
| T-09 | ✅ | é a própria suite |
| T-10 | ✅ | Playwright junto da tela |

---

## Tools confirmados (Execute)

- `MCP: context7` — em T-07, pra confirmar payload exato de `PATCH /repos/{owner}/{repo}` (`archived: true`), não coberto pela pesquisa já feita em github-integration
- `code-review` skill — checkpoint ao final das Fases 2, 4, 5 e 7
- `verify` skill — após T-09 (suite de integração) e após T-10 (UI final)
