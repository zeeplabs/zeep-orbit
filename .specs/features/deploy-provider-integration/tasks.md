# Tasks: Deploy Provider Integration

**Spec**: `.specs/features/deploy-provider-integration/spec.md`
**Design**: `.specs/features/deploy-provider-integration/design.md`
**Status**: Verified — T-01..T-15 implementados e mergeados (verificado 2026-08-10: pacote `internal/deploy/` + `internal/deploy/render/`, `internal/dashboard/deploy_provider_config.go`, rotas `GET /api/deploy-provider/status`, `POST|PUT /api/deploy-provider/config`, `GET /api/deploy-provider/recent-deploys` em `internal/server/server.go:249-252`, `POST /api/frontend-apps/{id}/deploy/retry` em `:233`)

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `dashboard-build`), mesmo critério de `mvp-core/tasks.md`, `github-integration/tasks.md`, `frontend-app-entity/tasks.md` e `sync-local-repo/tasks.md`.

**Pré-requisito externo**: depende de `frontend-app-entity` (T-01..T-10) e `sync-local-repo` (T-01..T-13) estarem implementadas e mergeadas — esta sub-feature estende `POST /api/frontend-apps` e `DELETE /api/frontend-apps/{id}`, ambos já estendidos por essas duas. Depende também de `github-integration` (`internal/github.Client`, `github_templates` store) e da feature já implementada de App Tokens (`internal/dashboard/app_tokens_store.go`).

---

## Execution Plan

```
Fase 1: Dado (DDL)                          Fase 2: Interface e Render client
┌──────────────────────────────┐            ┌──────────────────────────────┐
│ T-01 deploy_provider_config    │           │ T-04 internal/deploy.DeployProvider│
│ T-02 ALTER github_templates     │──────────▶│ T-05 render.CreateService          │
│ T-03 ALTER frontend_apps        │           │ T-06 render.DeleteService/validate │
└──────────────────────────────┘            └──────────────────────────────┘
                                                             │
Fase 3: Stores                                               ▼
┌──────────────────────────────┐            Fase 4: Config de provider (superadmin)
│ T-07 deployProviderConfigStore  │◀───────────┐──────────────────────────────┐
└──────────────────────────────┘             │ T-08 GET/POST /api/deploy-provider │
                                              └──────────────────────────────┘

Fase 5: Config de deploy por template          Fase 6: Criação integrada
┌──────────────────────────────┐             ┌──────────────────────────────┐
│ T-09 estende templates store/handler│──────▶│ T-10 estende POST /frontend-apps   │
└──────────────────────────────┘             │       (cria service + env vars)     │
                                              └──────────────────────────────┘
                                                             │
Fase 7: Retry e delete                                       ▼
┌──────────────────────────────┐             Fase 8: Observabilidade
│ T-11 POST .../deploy/retry        │────────▶│ T-13 audit log                     │
│ T-12 estende DELETE (remove service)│        └──────────────────────────────┘
└──────────────────────────────┘                            │
                                                             ▼
                                              Fase 9: UI e verificação
                                              ┌──────────────────────────────┐
                                              │ T-14 testes integração            │
                                              │ T-15 UI dashboard                 │
                                              └──────────────────────────────┘
```

---

### T-01: Provisionar tabela `deploy_provider_config` [x]

- **What**: DDL da tabela singleton (`provider`, `api_key` criptografado, `connected_at`, `updated_at`) + unique index `((TRUE))`, no bloco de provisionamento existente.
- **Where**: `internal/dashboard/provisioner.go`
- **Depends on**: nenhuma
- **Reuses**: mesmo padrão exato de `github_app_config` (`internal/dashboard/provisioner.go`)
- **Requirement**: DP-01..DP-04 (pré-condição de dado)
- **Tools**: nenhum
- **Done when**: bootstrap cria a tabela num banco limpo; segunda tentativa de insert sobrescreve a linha singleton (via `ON CONFLICT` ou `UPDATE` explícito na store, testado em T-07)
- **Tests**: teste de provisionamento (subir banco de teste, checar `information_schema.tables` e a unique index)
- **Gate**: `go test ./internal/dashboard/... -run Provision` + `go vet ./...`
- **Commit**: não (agrupa com T-02/T-03)

---

### T-02: `ALTER TABLE github_templates` — colunas de deploy [x]

- **What**: Adicionar `render_service_type`, `build_command`, `publish_path`, `start_command` (conforme design.md), todas `NOT NULL DEFAULT ''`.
- **Where**: `internal/dashboard/provisioner.go`
- **Depends on**: nenhuma (tabela `github_templates` já existe de github-integration)
- **Reuses**: mesmo bloco de migração aditiva já usado no projeto (`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`)
- **Requirement**: DP-10, DP-11, DP-12
- **Tools**: nenhum
- **Done when**: `ALTER TABLE` roda idempotente (`IF NOT EXISTS`) num banco já provisionado pela github-integration, sem quebrar dados existentes
- **Tests**: teste de provisionamento confirmando as 4 colunas novas com defaults corretos
- **Gate**: `go test ./internal/dashboard/... -run Provision`
- **Commit**: não (agrupa com T-01/T-03)

---

### T-03: `ALTER TABLE frontend_apps` — colunas de deploy e vínculo com backend app [x]

- **What**: Adicionar `backend_app_id` (FK nullable pra `apps(id)`), `deploy_service_id`, `deploy_url`, `deploy_status` (default `'pending'`), `deploy_error_message`.
- **Where**: `internal/dashboard/provisioner.go`
- **Depends on**: nenhuma (tabela `frontend_apps` já existe de frontend-app-entity)
- **Reuses**: mesmo padrão de `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
- **Requirement**: DP-20..DP-25 (pré-condição de dado)
- **Tools**: nenhum
- **Done when**: `ALTER TABLE` idempotente; FK `backend_app_id` aceita `NULL` e referencia `apps(id)` sem `ON DELETE CASCADE`
- **Tests**: teste de provisionamento confirmando as 5 colunas novas e o comportamento da FK (delete de um `apps` linkado não derruba o `frontend_apps`)
- **Gate**: `go test ./internal/dashboard/... -run Provision` + `go vet ./...`
- **Commit**: `feat(deploy-provider-integration): add DDL for provider config, template deploy fields and frontend app deploy fields` (T-01+T-02+T-03)

---

### T-04: Pacote `internal/deploy` — interface `DeployProvider` [x]

- **What**: Definir `CreateServiceParams`, `ServiceInfo` e a interface `DeployProvider` exatamente conforme `design.md` (`CreateService`, `DeleteService`).
- **Where**: `internal/deploy/provider.go` (novo pacote)
- **Depends on**: nenhuma
- **Reuses**: nada — é o contrato novo
- **Requirement**: DP-20 (base do contrato consumido pelas demais ACs)
- **Tools**: nenhum
- **Done when**: pacote compila isoladamente, sem nenhuma implementação concreta ainda; usado só como tipo em testes com fake/mock
- **Tests**: nenhum teste de comportamento (é só definição de tipos) — `go build ./internal/deploy/...` como verificação
- **Gate**: `go build ./internal/deploy/...` + `go vet ./...`
- **Commit**: não (agrupa com T-05/T-06)

---

### T-05: `internal/deploy/render.RenderProvider` — `CreateService` [x]

- **What**: Implementar `CreateService(ctx, params) (deploy.ServiceInfo, error)` chamando `POST /v1/services` da API do Render, mapeando `params.ServiceType` pro payload correto (`static_site` vs `web_service`), incluindo `params.EnvVars`.
- **Where**: `internal/deploy/render/render.go`, `internal/deploy/render/client.go` (novo pacote)
- **Depends on**: T-04
- **Reuses**: padrão de client HTTP autenticado já usado em `internal/github/client.go` (adaptado pra `Authorization: Bearer <api_key>`)
- **Requirement**: DP-20, DP-21, DP-22, DP-25
- **Tools**: `MCP: context7` (confirmar payload exato de `POST /v1/services` do Render — campos de `serviceDetails` pra static site vs web service, formato de `envVars`)
- **Done when**: `RenderProvider` implementa `deploy.DeployProvider`; `CreateService` testado contra sandbox real do Render cria um static site e um web service funcionais
- **Tests**: teste de integração contra conta sandbox do Render (2 casos: `static_site`, `web_service`)
- **Gate**: `go test ./internal/deploy/render/... -run CreateService`
- **Commit**: não (agrupa com T-06)

---

### T-06: `internal/deploy/render.RenderProvider` — `DeleteService` e validação de API Key [x]

- **What**: Implementar `DeleteService(ctx, serviceID) error` (`DELETE /v1/services/{id}`) e uma função auxiliar `ValidateAPIKey(ctx, apiKey) error` (`GET /v1/owners`, usada por T-08 antes de persistir).
- **Where**: `internal/deploy/render/render.go`, `internal/deploy/render/client.go`
- **Depends on**: T-05
- **Reuses**: mesmo client HTTP de T-05
- **Requirement**: DP-02, DP-40, DP-41
- **Tools**: `MCP: context7` (confirmar `GET /v1/owners` como endpoint de validação de credencial mais barato disponível na API do Render)
- **Done when**: `DeleteService` remove service real em sandbox; `ValidateAPIKey` retorna erro claro pra key inválida e `nil` pra key válida
- **Tests**: teste de integração (delete de service criado em T-05; validação com key inválida e válida)
- **Gate**: `go test ./internal/deploy/render/... -run "DeleteService|ValidateAPIKey"`
- **Commit**: `feat(deploy-provider-integration): implement RenderProvider (create/delete service, API key validation)` (T-04+T-05+T-06)

---

### T-07: `deployProviderConfigStore` (CRUD singleton) [x]

- **What**: Implementar `Upsert(ctx, provider, apiKeyEncrypted string) error`, `Get(ctx) (*DeployProviderConfig, error)` contra `zeep_system.deploy_provider_config`.
- **Where**: `internal/dashboard/deploy_provider_config_store.go`
- **Depends on**: T-01
- **Reuses**: mesmo padrão de store singleton já usado por `github_app_config` (`UPDATE ... WHERE true` ou `INSERT ... ON CONFLICT`)
- **Requirement**: DP-01, DP-03
- **Tools**: nenhum
- **Done when**: `Upsert` chamado 2x sobrescreve a mesma linha (confirmado por `Get` retornando o valor mais recente); `Get` em tabela vazia retorna "não configurado" sem erro
- **Tests**: teste de integração com banco real (upsert duplo, get vazio)
- **Gate**: `go test ./internal/dashboard/... -run DeployProviderConfigStore`
- **Commit**: não (agrupa com T-08)

---

### T-08: `GET /api/deploy-provider/status` e `POST /api/deploy-provider/config` [x]

- **What**: Handler que valida a API Key via `render.ValidateAPIKey` antes de criptografar (`internal/crypto`) e persistir via `deployProviderConfigStore.Upsert`; `GET` retorna `{connected, provider}` baseado em `Get`.
- **Where**: `internal/dashboard/deploy_provider_config.go` (novo arquivo)
- **Depends on**: T-06, T-07
- **Reuses**: `middleware.RequireSuperadmin`, `internal/crypto`, `h.audit(...)`, mesmo padrão exato de `internal/dashboard/github_config.go`
- **Requirement**: DP-01, DP-02, DP-03, DP-04
- **Tools**: nenhum
- **Done when**: `POST` com key válida persiste e retorna 200; `POST` com key inválida retorna 400 sem persistir; `GET` reflete o estado corretamente; audit log gerado em toda alteração
- **Tests**: 3 casos acima com `render.ValidateAPIKey` mockado + 1 teste de integração real contra sandbox
- **Gate**: `go test ./internal/dashboard/... -run DeployProviderConfig` + `go vet ./...`
- **Commit**: `feat(deploy-provider-integration): add deploy provider config store and superadmin endpoints`

---

### T-09: Estender store e handler de `github_templates` — campos de deploy [x]

- **What**: Estender `githubTemplatesStore.Update` (ou método equivalente) e `PUT /api/github/templates/{id}` pra aceitar `render_service_type`, `build_command`, `publish_path`, `start_command`; validar conforme AC DP-10 (se `static_site`, exige `publish_path`; se `web_service`, exige `start_command`; ambos exigem `build_command`).
- **Where**: `internal/dashboard/github_templates.go`, `internal/dashboard/github_templates_store.go`
- **Depends on**: T-02
- **Reuses**: handler e store já existentes de github-integration
- **Requirement**: DP-10, DP-11
- **Tools**: nenhum
- **Done when**: `PUT` com combinação válida (`static_site` + `build_command` + `publish_path`) persiste; combinações incompletas rejeitam com erro claro; `GET` retorna os campos preenchidos
- **Tests**: 3 casos acima (válido static, válido web_service, inválido incompleto)
- **Gate**: `go test ./internal/dashboard/... -run GithubTemplatesDeploy`
- **Commit**: `feat(deploy-provider-integration): add deploy config fields to github templates`

---

### T-10: Estender `POST /api/frontend-apps` — criação do service na criação do frontend app [x]

- **What**: Após repo + deploy key resolvidos (sub-features anteriores), como último passo síncrono: (1) checar se template tem `render_service_type` preenchido, senão rejeitar (DP-11 na criação); (2) checar se provider está conectado, senão marcar `deploy_status: failed` sem bloquear a criação (DP-25); (3) se `backend_app_id` informado, resolver URL pública do app e gerar `APP_TOKEN` via `app_tokens_store`; (4) chamar `DeployProvider.CreateService`; (5) persistir `deploy_service_id`, `deploy_url`, `deploy_status`.
- **Where**: `internal/dashboard/frontend_apps.go` (handler existente de criação)
- **Depends on**: T-03, T-06, T-09
- **Reuses**: `app_tokens_store` (feature já implementada), fluxo de criação já existente
- **Requirement**: DP-20, DP-21, DP-22, DP-23, DP-24, DP-25
- **Tools**: nenhum
- **Done when**: criação com template configurado + provider conectado + `backend_app_id` resulta em `deploy_status: ready`, `deploy_url` preenchido, env vars corretas no service (confirmado no dashboard do Render); criação sem provider conectado resulta em `deploy_status: failed` sem impedir criação do frontend app; criação com template sem config de deploy é rejeitada antes de criar repo
- **Tests**: teste de integração cobrindo os 3 casos acima, com `DeployProvider` fake (não a implementação real do Render, pra isolar do sandbox nesta task)
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsCreateDeploy` + `go vet ./...`
- **Commit**: `feat(deploy-provider-integration): create deploy service on frontend app creation`

---

### T-11: `POST /api/frontend-apps/{id}/deploy/retry` [x]

- **What**: Handler que rejeita se `deploy_status: ready`, senão refaz a etapa de criação de service (extrair função compartilhada `attemptDeployCreate` reusada por T-10).
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-10
- **Reuses**: `attemptDeployCreate` (extraída de T-10)
- **Requirement**: DP-30, DP-31
- **Tools**: nenhum
- **Done when**: retry em `failed`/`pending` transiciona pra `ready` em caso de sucesso simulado (`DeployProvider` fake); retry em `ready` é rejeitado sem chamar o provider
- **Tests**: 2 casos acima com provider fake
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsDeployRetry`
- **Commit**: não (agrupa com T-12)

---

### T-12: Estender `DELETE /api/frontend-apps/{id}` — remover service [x]

- **What**: Após soft delete + archive do repo (sub-features anteriores), chamar `DeployProvider.DeleteService` (best-effort, não bloqueia se falhar) quando `deploy_status: ready`.
- **Where**: `internal/dashboard/frontend_apps.go` (handler existente de delete)
- **Depends on**: T-06, T-10
- **Reuses**: handler de delete já existente
- **Requirement**: DP-40, DP-41
- **Tools**: nenhum
- **Done when**: delete de app `deploy_status: ready` tenta remover o service; falha simulada na remoção não impede a remoção do frontend app
- **Tests**: 2 casos (remoção sucesso e falha simulada) com provider fake
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsDeleteDeploy` + `go vet ./...`
- **Commit**: `feat(deploy-provider-integration): remove deploy service on frontend app deletion` (T-11+T-12)

---

### T-13: Audit log [x]

- **What**: Instrumentar `h.audit(...)` em `deploy_provider.config.update`, `frontend_app.deploy.create`, `.retry`, `.delete`.
- **Where**: `internal/dashboard/deploy_provider_config.go`, `internal/dashboard/frontend_apps.go`
- **Depends on**: T-08, T-10, T-11, T-12
- **Reuses**: `audit_store.InsertAuditLog` (`h.audit(...)`)
- **Requirement**: DP-04, DP-24
- **Tools**: nenhum
- **Done when**: cada uma das 4 ações gera exatamente 1 entrada de audit log
- **Tests**: teste de integração checando contagem/conteúdo de `audit_log` após cada ação
- **Gate**: `go test ./internal/dashboard/... -run DeployProviderAudit`
- **Commit**: `feat(deploy-provider-integration): wire audit log for provider config and deploy actions`

---

### T-14: Testes de integração end-to-end contra Render sandbox [x]

- **What**: Suite cobrindo fluxo completo contra conta sandbox real do Render: conectar provider → configurar template → criar frontend app linkado a um backend app → confirmar service criado com env vars corretas → forçar falha (desconectar) → retry → deletar → confirmar service removido.
- **Where**: `internal/dashboard/deploy_provider_integration_test.go`
- **Depends on**: T-01..T-13
- **Reuses**: mesma infraestrutura de teste de integração de github-integration/frontend-app-entity/sync-local-repo
- **Requirement**: todos os DP-* (cobertura completa)
- **Tools**: `verify` skill (rodar após esta task)
- **Done when**: suite passa de ponta a ponta contra sandbox real, sem mocks
- **Tests**: é a própria task
- **Gate**: `go test ./internal/dashboard/... -tags=integration`
- **Commit**: `test(deploy-provider-integration): add end-to-end integration test suite`

---

### T-15: UI do dashboard [x]

- **What**: (1) Seção "Integrações → Deploy" — conectar Render (campo API Key), status conectado/desconectado (superadmin only); (2) tela de edição de template — campos de config de deploy (tipo de service, build/publish/start command); (3) tela de detalhe do frontend app — seletor opcional de backend app na criação, status de deploy, URL do deploy, botão retry.
- **Where**: `internal/dashboard/ui/` (seguir estrutura de páginas existente)
- **Depends on**: T-08, T-09, T-10, T-11
- **Reuses**: componentes de card/botão/formulário já existentes no dashboard
- **Requirement**: DP-01..DP-04, DP-10..DP-12, DP-20..DP-25, DP-30
- **Tools**: nenhum
- **Done when**: fluxo completo utilizável manualmente no browser (conectar Render, configurar template, criar frontend app linkado, ver status/URL, retry)
- **Tests**: Playwright e2e básico (conectar provider, configurar template, criar app linkado, confirmar URL exibida) seguindo `test-e2e` do Makefile
- **Gate**: `dashboard-build` + `test-e2e` (Playwright)
- **Commit**: `feat(deploy-provider-integration): add dashboard UI for provider config, template deploy settings and deploy status`

---

## Parallel Execution Map

- T-01, T-02, T-03 podem rodar em paralelo (sem dependência entre si, todas DDL aditivo)
- T-04 não depende de nenhuma DDL — paraleliza com T-01/T-02/T-03
- T-05 depende de T-04 (usa os tipos da interface) — não paraleliza com T-04
- T-06 depende de T-05 (mesmo pacote/client) — não paraleliza
- T-07 depende de T-01 — paraleliza com T-04/T-05/T-06
- T-08 depende de T-06 e T-07 — não paraleliza com nenhum dos dois
- T-09 depende de T-02 — paraleliza com T-04..T-08
- T-10 depende de T-03, T-06, T-09 — não paraleliza com nenhum dos três
- T-11 depende estritamente de T-10 (reusa `attemptDeployCreate`) — não paraleliza
- T-12 depende de T-06 e T-10 — pode rodar em paralelo com T-11 (lógicas independentes) mas ambas travam no mesmo commit (T-12)
- T-13 só depende do output de T-08/T-10/T-11/T-12 — mantido isolado pra checkpoint de review dedicado
- T-14 e T-15 são os únicos que dependem de tudo — não paralelizam com nada anterior

---

## Task Granularity Check

| Task | Escopo em 1 sessão? | Testável isoladamente? |
|---|---|---|
| T-01 | ✅ (1 tabela, DDL) | ✅ |
| T-02 | ✅ (ALTER pontual) | ✅ |
| T-03 | ✅ (ALTER pontual) | ✅ |
| T-04 | ✅ (definição de tipos) | ✅ |
| T-05 | ✅ (1 método, novo pacote) | ✅ |
| T-06 | ✅ (2 métodos, mesmo pacote) | ✅ |
| T-07 | ✅ (1 arquivo novo) | ✅ |
| T-08 | ✅ (2 handlers) | ✅ |
| T-09 | ✅ (extensão de handler/store existentes) | ✅ |
| T-10 | ✅ (extensão de handler existente) | ✅ |
| T-11 | ✅ (reusa T-10, handler fino) | ✅ |
| T-12 | ✅ (extensão de handler existente) | ✅ |
| T-13 | ✅ (instrumentação pontual) | ✅ |
| T-14 | ✅ (suite única, escopo fechado) | ✅ |
| T-15 | ✅ (3 seções de tela, componentes reusados) | ✅ |

## Diagram-Definition Cross-Check

| Fase no diagrama | Tasks correspondentes | Consistente? |
|---|---|---|
| Fase 1: Dado (DDL) | T-01, T-02, T-03 | ✅ |
| Fase 2: Interface e Render client | T-04, T-05, T-06 | ✅ |
| Fase 3: Stores | T-07 | ✅ |
| Fase 4: Config de provider | T-08 | ✅ |
| Fase 5: Config de deploy por template | T-09 | ✅ |
| Fase 6: Criação integrada | T-10 | ✅ |
| Fase 7: Retry e delete | T-11, T-12 | ✅ |
| Fase 8: Observabilidade | T-13 | ✅ |
| Fase 9: UI e verificação | T-14, T-15 | ✅ |

## Test Co-location Validation

| Task | Teste no mesmo commit? | Nota |
|---|---|---|
| T-01 | ✅ | teste de provisionamento junto do DDL |
| T-02 | ✅ | teste de provisionamento junto do ALTER |
| T-03 | ✅ | teste de provisionamento junto do ALTER |
| T-04 | — | sem comportamento a testar, só `go build` |
| T-05 | ✅ | integração contra sandbox junto do método |
| T-06 | ✅ | integração contra sandbox junto dos métodos |
| T-07 | ✅ | teste de store junto do CRUD |
| T-08 | ✅ | casos mockados + 1 integração real junto dos handlers |
| T-09 | ✅ | 3 casos junto da extensão |
| T-10 | ✅ | integração junto da extensão do handler |
| T-11 | ✅ | 2 casos junto do handler |
| T-12 | ✅ | 2 casos junto do handler |
| T-13 | ✅ | teste de contagem/conteúdo de audit_log |
| T-14 | ✅ | é a própria suite |
| T-15 | ✅ | Playwright junto das telas |

---

## Tools confirmados (Execute)

- `MCP: context7` — em T-05 (payload exato de `POST /v1/services` do Render) e T-06 (endpoint de validação de credencial mais barato)
- `code-review` skill — checkpoint ao final das Fases 3, 6 e 9
- `verify` skill — após T-14 (suite de integração) e após T-15 (UI final)

---

## Extensão relacionada (2026-08-05)

Visibilidade de deploy (histórico "Recent Deploys" na tab Deploy providers, via `GET /services/{id}/deploys` do Render) foi adicionada em `.specs/features/integrations-handoff-parity/`, não aqui — esta spec permanece só conectar provider + criar/deletar service. Não reabrir escopo desta spec pra isso.
