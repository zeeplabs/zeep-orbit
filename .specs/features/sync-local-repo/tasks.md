# Tasks: Sync Local↔Repo

**Spec**: `.specs/features/sync-local-repo/spec.md`
**Design**: `.specs/features/sync-local-repo/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `dashboard-build`), mesmo critério de `mvp-core/tasks.md`, `github-integration/tasks.md` e `frontend-app-entity/tasks.md`.

**Pré-requisito externo**: depende de `frontend-app-entity` (T-01..T-10) estar implementada e mergeada — esta sub-feature estende `POST /api/frontend-apps` e `DELETE /api/frontend-apps/{id}`, ambos definidos lá. Depende também de `internal/github.Client` (github-integration T-01..T-12) já existir.

---

## Execution Plan

```
Fase 1: Dado                         Fase 2: SSH e GitHub client        Fase 3: Store
┌──────────────────────────┐         ┌──────────────────────────┐      ┌─────────────────────┐
│ T-01 DDL                  │────────▶│ T-02 internal/sshkey       │─────▶│ T-04 sync creds store │
│ sync_credentials table   │         │ T-03 AddDeployKey/         │      │                       │
└──────────────────────────┘         │       RevokeDeployKey     │      └─────────────────────┘
                                      └──────────────────────────┘                │
                                                                                    ▼
Fase 4: Criação integrada                    Fase 5: Reveal e leitura
┌──────────────────────────────┐             ┌──────────────────────────┐
│ T-05 estende POST /frontend-apps│───────────▶│ T-06 GET .../sync          │
│ (gera+registra credencial)      │             │ T-07 POST .../reveal-key   │
└──────────────────────────────┘             └──────────────────────────┘
              │
              ▼
Fase 6: Retry, regenerate e delete            Fase 7: Observabilidade
┌──────────────────────────────┐             ┌──────────────────────────┐
│ T-08 POST .../sync/retry        │────────────▶│ T-11 audit log             │
│ T-09 POST .../sync/regenerate   │             └──────────────────────────┘
│ T-10 estende DELETE (revoga)    │                        │
└──────────────────────────────┘                        ▼
                                              Fase 8: UI e verificação
                                              ┌──────────────────────────┐
                                              │ T-12 testes integração    │
                                              │ T-13 UI dashboard          │
                                              └──────────────────────────┘
```

---

### T-01: Provisionar tabela `frontend_app_sync_credentials`

- **What**: DDL da tabela (colunas conforme design.md, FK `UNIQUE` pra `frontend_apps`) no bloco de provisionamento existente.
- **Where**: `internal/dashboard/provisioner.go`
- **Depends on**: nenhuma nesta feature; externamente, `frontend_apps` já deve existir (frontend-app-entity T-01)
- **Reuses**: mesmo bloco `CREATE TABLE IF NOT EXISTS` das tabelas `zeep_system.*`
- **Requirement**: SY-01 (pré-condição de dado)
- **Tools**: nenhum
- **Done when**: bootstrap cria a tabela num banco limpo, FK `UNIQUE` presente
- **Tests**: teste de provisionamento (subir banco de teste, checar `information_schema.tables`)
- **Gate**: `go test ./internal/dashboard/... -run Provision` + `go vet ./...`
- **Commit**: não (agrupa com T-02/T-03/T-04)

---

### T-02: Pacote `internal/sshkey`

- **What**: Função `GenerateKeyPair() (publicKey, privateKey string, err error)` — gera par ed25519, serializa chave pública em formato OpenSSH (`authorized_keys`), chave privada em PEM.
- **Where**: `internal/sshkey/sshkey.go` (novo pacote)
- **Depends on**: nenhuma
- **Reuses**: `golang.org/x/crypto/ssh` — confirmar se já é dependência indireta do `go.mod`; se não, adicionar (dependência mínima, biblioteca padrão do ecossistema Go pra SSH)
- **Requirement**: SY-01
- **Tools**: `MCP: context7` (confirmar API correta de serialização de chave pública OpenSSH em `golang.org/x/crypto/ssh`, evitar formato incompatível com o parser de deploy key do GitHub)
- **Done when**: função pura, sem I/O, gera par válido (chave pública aceita pelo GitHub em teste manual/sandbox)
- **Tests**: teste unitário validando formato da chave pública gerada (prefixo `ssh-ed25519`) e que a privada decodifica como PEM válido
- **Gate**: `go test ./internal/sshkey/...`
- **Commit**: não (agrupa com T-01/T-03/T-04)

---

### T-03: `internal/github.Client` — `AddDeployKey` e `RevokeDeployKey`

- **What**: `(c *Client) AddDeployKey(ctx, owner, repo, title, publicKey string) (keyID int64, err error)` e `(c *Client) RevokeDeployKey(ctx, owner, repo string, keyID int64) error`.
- **Where**: `internal/github/client.go`
- **Depends on**: nenhuma nesta feature (client já existe de github-integration)
- **Reuses**: mesmo padrão de chamada autenticada já usado por `CreateRepoFromTemplate`/`ArchiveRepo`
- **Requirement**: SY-01, SY-30
- **Tools**: `MCP: context7` (confirmar payload exato de `POST /repos/{owner}/{repo}/keys` — campos `title`, `key`, `read_only: false` — e de `DELETE /repos/{owner}/{repo}/keys/{key_id}`)
- **Done when**: ambos os métodos testados contra sandbox real (chave criada aparece nas configurações do repo, revogação remove)
- **Tests**: teste de integração contra org GitHub de sandbox
- **Gate**: `go test ./internal/github/... -run DeployKey`
- **Commit**: não (agrupa com T-01/T-02/T-04)

---

### T-04: `frontendAppSyncCredentialsStore` (CRUD)

- **What**: Implementar `Create`, `Get`, `UpdateSuccess`, `UpdateFailure` contra `zeep_system.frontend_app_sync_credentials`.
- **Where**: `internal/dashboard/frontend_app_sync_credentials_store.go`
- **Depends on**: T-01
- **Reuses**: mesmo padrão de store `pgx` simples já usado por `frontend_apps_store`
- **Requirement**: SY-01, SY-20, SY-30
- **Tools**: nenhum
- **Done when**: as 4 operações implementadas e testadas contra banco de teste, incluindo violação do `UNIQUE` em `frontend_app_id`
- **Tests**: teste de integração com banco real
- **Gate**: `go test ./internal/dashboard/... -run SyncCredentialsStore`
- **Commit**: `feat(sync-local-repo): add DDL, sshkey package, GitHub deploy key methods and store CRUD` (T-01+T-02+T-03+T-04)

---

### T-05: Estender `POST /api/frontend-apps` — geração de credencial na criação

- **What**: Após `CreateRepoFromTemplate` bem-sucedido, chamar `sshkey.GenerateKeyPair`, `AddDeployKey`, persistir credencial (`sync_status: ready`) ou falha (`sync_status: pending` + `error_message`), sem afetar o `status` do frontend app.
- **Where**: `internal/dashboard/frontend_apps.go` (handler existente de criação, sub-feature 2)
- **Depends on**: T-03, T-04
- **Reuses**: fluxo de criação já existente (`frontend-app-entity` T-04)
- **Requirement**: SY-01, SY-02, SY-03
- **Tools**: nenhum externo — comportamento já validado em T-02/T-03
- **Done when**: criação bem-sucedida do frontend app sempre resulta em `sync_status: ready` ou `pending`, nunca falha a criação do app por causa da credencial
- **Tests**: teste de integração cobrindo sucesso e falha simulada na etapa de credencial (com `CreateRepoFromTemplate` já OK)
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsCreateSync` + `go vet ./...`
- **Commit**: `feat(sync-local-repo): generate and register deploy key on frontend app creation`

---

### T-06: `GET /api/frontend-apps/{id}/sync`

- **What**: Handler retornando `sync_status`, `public_key`, `error_message` (nunca a chave privada).
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-04
- **Reuses**: `middleware.RequireAuth`
- **Requirement**: SY-10
- **Tools**: nenhum
- **Done when**: retorna os 3 campos corretos pros 3 estados possíveis de `sync_status`
- **Tests**: teste unitário com store mockado, 3 casos (ready/pending/failed)
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsSyncGet`
- **Commit**: não (agrupa com T-07)

---

### T-07: `POST /api/frontend-apps/{id}/reveal-key`

- **What**: Handler que descriptografa e retorna a chave privada uma vez, rejeitando se `sync_status` ≠ `ready`; registra audit log.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-04
- **Reuses**: `internal/crypto` (descriptografia AES-256-GCM), `middleware.RequireAuth`, `h.audit(...)`
- **Requirement**: SY-11, SY-12, SY-13
- **Tools**: nenhum
- **Done when**: reveal em app `ready` retorna chave e gera 1 entrada de audit; reveal em `pending`/`failed` rejeita sem vazar dado
- **Tests**: 2 casos acima
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsRevealKey`
- **Commit**: `feat(sync-local-repo): add sync status and reveal-key endpoints`

---

### T-08: `POST /api/frontend-apps/{id}/sync/retry`

- **What**: Handler que rejeita se `sync_status: ready`, senão refaz geração de chave + registro no GitHub sobre o registro existente.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-05
- **Reuses**: mesma lógica de T-05 (extrair função compartilhada `attemptSyncSetup`)
- **Requirement**: SY-20, SY-21, SY-22
- **Tools**: nenhum
- **Done when**: retry em `pending`/`failed` transiciona pra `ready` em caso de sucesso simulado; retry em `ready` é rejeitado
- **Tests**: 2 casos acima com client mockado
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsSyncRetry`
- **Commit**: não (agrupa com T-09/T-10)

---

### T-09: `POST /api/frontend-apps/{id}/sync/regenerate`

- **What**: Handler que tenta `RevokeDeployKey` na chave atual (best-effort, ignora falha), gera novo par via `sshkey`, registra nova chave, atualiza mesmo registro; se não havia chave anterior (`pending`/`failed`), comporta-se como retry.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-08
- **Reuses**: `attemptSyncSetup` de T-08, `RevokeDeployKey` de T-03
- **Requirement**: SY-30, SY-31, SY-32
- **Tools**: nenhum
- **Done when**: regenerate em app `ready` produz `public_key` diferente da anterior mesmo com revogação da antiga falhando simuladamente; regenerate em `pending`/`failed` funciona como retry
- **Tests**: 3 casos acima (sucesso completo, revogação falha mas regenera, estado inicial pending)
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsSyncRegenerate`
- **Commit**: não (agrupa com T-10)

---

### T-10: Estender `DELETE /api/frontend-apps/{id}` — revogar credencial

- **What**: Após soft delete + archive do repo (sub-feature 2), chamar `RevokeDeployKey` (best-effort, não bloqueia se falhar).
- **Where**: `internal/dashboard/frontend_apps.go` (handler existente de delete)
- **Depends on**: T-03, T-04
- **Reuses**: handler de delete já existente (`frontend-app-entity` T-07)
- **Requirement**: SY-40, SY-41
- **Tools**: nenhum
- **Done when**: delete bem-sucedido tenta revogar a chave; falha na revogação não impede a remoção do app (já garantido pelo padrão existente do archive)
- **Tests**: 2 casos (revogação sucesso e falha simulada) com client mockado
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsDeleteSync` + `go vet ./...`
- **Commit**: `feat(sync-local-repo): add sync retry, regenerate and revoke-on-delete`

---

### T-11: Audit log

- **What**: Instrumentar `h.audit(...)` em `frontend_app.sync.reveal`, `.retry`, `.regenerate`.
- **Where**: `internal/dashboard/frontend_apps.go`
- **Depends on**: T-07, T-08, T-09
- **Reuses**: `audit_store.InsertAuditLog` (`h.audit(...)`)
- **Requirement**: SY-12
- **Tools**: nenhum
- **Done when**: cada uma das 3 ações gera exatamente 1 entrada de audit log
- **Tests**: teste de integração checando contagem/conteúdo de `audit_log` após cada ação
- **Gate**: `go test ./internal/dashboard/... -run FrontendAppsSyncAudit`
- **Commit**: `feat(sync-local-repo): wire audit log for reveal, retry and regenerate`

---

### T-12: Testes de integração end-to-end

- **What**: Suite cobrindo fluxo completo contra org GitHub de sandbox: criar frontend app → confirmar deploy key registrada → revelar → forçar falha → retry → regenerar → deletar → confirmar revogação.
- **Where**: `internal/dashboard/frontend_apps_sync_integration_test.go`
- **Depends on**: T-01..T-11
- **Reuses**: mesma infraestrutura de teste de integração de github-integration/frontend-app-entity
- **Requirement**: todos os SY-* (cobertura completa)
- **Tools**: `verify` skill (rodar após esta task)
- **Done when**: suite passa de ponta a ponta contra sandbox real, sem mocks
- **Tests**: é a própria task
- **Gate**: `go test ./internal/dashboard/... -tags=integration`
- **Commit**: `test(sync-local-repo): add end-to-end integration test suite`

---

### T-13: UI do dashboard

- **What**: Seção "Sync" na tela de detalhe do frontend app — comandos git (clone/remote add), botão revelar chave, prompt copiável pro agente de IA (template estático com placeholders), botões retry/regenerate.
- **Where**: `internal/dashboard/ui/` (seguir estrutura de páginas existente, mesma tela de detalhe da sub-feature 2)
- **Depends on**: T-06, T-07, T-08, T-09
- **Reuses**: componentes de card/botão já existentes no dashboard
- **Requirement**: SY-10, SY-11, SY-13, SY-20, SY-30
- **Tools**: nenhum
- **Done when**: fluxo completo utilizável manualmente no browser (ver status, revelar, copiar prompt, retry, regenerate)
- **Tests**: Playwright e2e básico (criar app, revelar chave, confirmar prompt renderizado) seguindo `test-e2e` do Makefile
- **Gate**: `dashboard-build` + `test-e2e` (Playwright)
- **Commit**: `feat(sync-local-repo): add dashboard UI for sync setup and credential management`

---

## Parallel Execution Map

- T-01, T-02 podem rodar em paralelo (sem dependência entre si)
- T-03 não depende de T-01/T-02 — paraleliza com ambos
- T-04 depende de T-01 (precisa da tabela) — não paraleliza com T-01
- T-06 pode começar assim que T-04 terminar, em paralelo com T-05
- T-08 depende estritamente de T-05 (reusa lógica), não paraleliza
- T-09 depende estritamente de T-08 (reusa `attemptSyncSetup`), não paraleliza
- T-11 só depende do output de T-07/T-08/T-09 — mantido isolado pra checkpoint de review dedicado
- T-12 e T-13 são os únicos que dependem de tudo — não paralelizam com nada anterior

---

## Task Granularity Check

| Task | Escopo em 1 sessão? | Testável isoladamente? |
|---|---|---|
| T-01 | ✅ (1 arquivo, DDL) | ✅ |
| T-02 | ✅ (pacote novo, função pura) | ✅ |
| T-03 | ✅ (2 métodos no client existente) | ✅ |
| T-04 | ✅ (1 arquivo novo) | ✅ |
| T-05 | ✅ (extensão de handler existente) | ✅ |
| T-06 | ✅ (1 handler simples) | ✅ |
| T-07 | ✅ (1 handler, lógica linear) | ✅ |
| T-08 | ✅ (reusa T-05, handler fino) | ✅ |
| T-09 | ✅ (reusa T-08, handler fino) | ✅ |
| T-10 | ✅ (extensão de handler existente) | ✅ |
| T-11 | ✅ (instrumentação pontual) | ✅ |
| T-12 | ✅ (suite única, escopo fechado) | ✅ |
| T-13 | ✅ (1 seção de tela, componentes reusados) | ✅ |

## Diagram-Definition Cross-Check

| Fase no diagrama | Tasks correspondentes | Consistente? |
|---|---|---|
| Fase 1: Dado | T-01 | ✅ |
| Fase 2: SSH e GitHub client | T-02, T-03 | ✅ |
| Fase 3: Store | T-04 | ✅ |
| Fase 4: Criação integrada | T-05 | ✅ |
| Fase 5: Reveal e leitura | T-06, T-07 | ✅ |
| Fase 6: Retry, regenerate e delete | T-08, T-09, T-10 | ✅ |
| Fase 7: Observabilidade | T-11 | ✅ |
| Fase 8: UI e verificação | T-12, T-13 | ✅ |

## Test Co-location Validation

| Task | Teste no mesmo commit? | Nota |
|---|---|---|
| T-01 | ✅ | teste de provisionamento junto do DDL |
| T-02 | ✅ | teste unitário de formato de chave junto do pacote |
| T-03 | ✅ | integração contra sandbox junto dos métodos |
| T-04 | ✅ | teste de store junto do CRUD |
| T-05 | ✅ | integração junto da extensão do handler |
| T-06 | ✅ | unitário com store mockado |
| T-07 | ✅ | 2 casos junto do handler |
| T-08 | ✅ | 2 casos junto do handler |
| T-09 | ✅ | 3 casos junto do handler |
| T-10 | ✅ | 2 casos junto do handler |
| T-11 | ✅ | teste de contagem/conteúdo de audit_log |
| T-12 | ✅ | é a própria suite |
| T-13 | ✅ | Playwright junto da tela |

---

## Tools confirmados (Execute)

- `MCP: context7` — em T-02 (serialização de chave pública OpenSSH em `golang.org/x/crypto/ssh`) e T-03 (payload exato de `POST`/`DELETE .../keys`)
- `code-review` skill — checkpoint ao final das Fases 3, 6 e 8
- `verify` skill — após T-12 (suite de integração) e após T-13 (UI final)
