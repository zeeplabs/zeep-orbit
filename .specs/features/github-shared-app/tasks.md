# GitHub Shared App Tasks

**Design**: `.specs/features/github-shared-app/design.md`
**Status**: Abandoned

**Decisão (2026-07-30)**: revertida — ver `spec.md` para o rationale completo. Todo o código destas 8 tasks (T-01 a T-08) foi implementado, testado, e depois revertido do repo na mesma sessão. Mantido só como registro histórico.

**Nota sobre gates**: mesma convenção já usada em `.specs/features/github-integration/tasks.md` — inferida do `Makefile` real do repo, nada inventado.

**Convenção de Gate**:
- `quick` = `go test ./internal/{pkg}/...` (pacote isolado)
- `full` = `go test ./...` + `go vet ./...`
- `build` = `go build ./...` (mudança estrutural sem lógica testável isoladamente)
- `ui-build` = `cd internal/dashboard/ui && npm run build`

---

## Execution Plan

### Phase 1 — Config env (Sequential)

```
T-01
```

### Phase 2 — Seeder (Sequential, depende de T-01)

```
T-01 ──→ T-02
```

### Phase 3 — Wiring boot + handler (Parallel, ambos dependem de T-01/T-02)

```
T-02 ──→ T-03 [P] ─┐
T-01 ──→ T-04 [P] ─┴─→ (Phase 4)
```

### Phase 4 — UI (Sequential, depende de T-04)

```
T-04 ──→ T-05
```

### Phase 5 — Docs (Sequential, depende de tudo)

```
T-03, T-05 ──→ T-06
```

---

## Task Breakdown

### T-01: `SharedGitHubAppEnv` — leitura das env vars do App compartilhado

**What**: Struct + `LoadSharedGitHubAppEnv() SharedGitHubAppEnv` lendo `GITHUB_SHARED_APP_ID`, `GITHUB_SHARED_APP_SLUG`, `GITHUB_SHARED_APP_CLIENT_ID`, `GITHUB_SHARED_APP_CLIENT_SECRET`, `GITHUB_SHARED_APP_PRIVATE_KEY_B64` (decodificado de base64), `GITHUB_SHARED_APP_WEBHOOK_SECRET`. Método `(c SharedGitHubAppEnv) Configured() bool` retorna `true` só quando `AppID` e `PrivateKeyPEM` não estão vazios.
**Where**: `internal/config/types.go`
**Depends on**: None
**Reuses**: Padrão de leitura de env var já usado no mesmo arquivo para outras credenciais do produto
**Requirement**: GHS-01

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `LoadSharedGitHubAppEnv()` lê as 6 env vars, decodifica base64 da private key, retorna erro claro se a base64 for inválida (não crasha o boot silenciosamente)
- [ ] `Configured()` retorna `false` quando `AppID` ou `PrivateKeyPEM` ausentes, `true` caso contrário
- [ ] Unit tests: todas ausentes, todas presentes, base64 inválida, só `AppID` presente (ainda `false`)
- [ ] Gate check passa: `go test ./internal/config/...`
- [ ] Test count: mínimo 4 testes passam

**Tests**: unit
**Gate**: quick

---

### T-02: `SeedSharedGitHubAppConfig` — popular config no boot

**What**: `SeedSharedGitHubAppConfig(ctx, pool, cfg SharedGitHubAppEnv) error` — se `!cfg.Configured()`, no-op. Senão, chama `GetGitHubConfig`; se já existe linha, no-op (log informativo "using existing GitHub App config, shared app env ignored"). Senão, valida com `github.Client.VerifyAppCredentials` e, se ok, chama `UpsertGitHubConfig` com os dados do env.
**Where**: `internal/dashboard/github_shared_app_seed.go`
**Depends on**: T-01
**Reuses**: `GetGitHubConfig`, `UpsertGitHubConfig` (`github_config_store.go`), `github.NewClient` + `VerifyAppCredentials` (`internal/github/client.go`)
**Requirement**: GHS-01, GHS-03

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Env não configurado → no-op, nenhuma query ao banco além da leitura implícita não ocorre (early return antes de qualquer chamada)
- [ ] Env configurado + banco já tem linha → no-op, linha existente intacta (assert via `GetGitHubConfig` antes/depois no teste)
- [ ] Env configurado + banco vazio + credenciais válidas → linha criada, campos batem com o env
- [ ] Env configurado + banco vazio + credenciais inválidas (mock retorna 401 em `VerifyAppCredentials`) → nenhuma linha criada, erro retornado/logado
- [ ] Integration tests contra PostgreSQL real (mesmo padrão de `github_config_store_test.go` se existir, ou `handler_test.go`)
- [ ] Gate check passa: `go test ./internal/dashboard/... && go vet ./...`
- [ ] Test count: mínimo 4 testes passam

**Tests**: integration
**Gate**: full

---

### T-03: Wire seed no boot do processo [P]

**What**: Chamar `config.LoadSharedGitHubAppEnv()` + `dashboard.SeedSharedGitHubAppConfig(ctx, pool, env)` em `cmd/zeep/main.go`, logo após a conexão com o banco estar pronta e antes do servidor HTTP começar a aceitar requests.
**Where**: `cmd/zeep/main.go`
**Depends on**: T-02
**Reuses**: Ponto de boot já existente onde outras validações de config rodam (mesmo estilo de fail-fast)
**Requirement**: GHS-01

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] Seed roda uma única vez no boot, antes do listener HTTP subir
- [ ] Falha de seed (credenciais inválidas) loga erro claro mas **não** derruba o processo — instância continua no ar com GitHub desconectado (consistente com "falta de config não é fatal" já usado para outras integrações opcionais)
- [ ] Gate check passa: `go build ./... && go test ./... && go vet ./...`

**Tests**: none (wiring puro; lógica já testada em T-02)
**Gate**: full

---

### T-04: `GetConfig` expõe `managed_by_env` [P]

**What**: `NewGitHubConfigHandler` passa a receber `SharedGitHubAppEnv` (ou só o booleano `Configured()`) como parâmetro extra. `redactGitHubConfig` ganha o campo `managed_by_env: bool`. `GetConfig` (`github_config.go:164`) retorna esse campo mesmo quando `cfg == nil` (`{"configured": false, "managed_by_env": ...}`).
**Where**: `internal/dashboard/github_config.go`
**Depends on**: T-01
**Reuses**: Struct/handler existentes, só adiciona campo e parâmetro de construção
**Requirement**: GHS-10

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `GET /api/github/config` retorna `managed_by_env: true` quando env do App compartilhado está configurado no processo, `false` caso contrário
- [ ] Comportamento independe de haver ou não linha no banco (reflete o ambiente do processo, não o estado da tabela)
- [ ] Testes de handler existentes continuam passando (assinatura de `NewGitHubConfigHandler` muda — atualizar todos os call sites, incluindo testes)
- [ ] Gate check passa: `go test ./internal/dashboard/... && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T-05: UI — ocultar form de credenciais quando `managed_by_env`

**What**: Em `GitHubIntegrationPage.tsx`, quando `GET /api/github/config` retornar `managed_by_env: true`, esconder o formulário de App ID/Client Secret/Private Key/Webhook Secret e mostrar só o botão "Instalar no GitHub" (mesmo botão que já existe hoje, chamando `install/start`). Quando `false`, comportamento idêntico ao de hoje (form completo).
**Where**: `internal/dashboard/ui/src/pages/GitHubIntegrationPage.tsx`
**Depends on**: T-04
**Reuses**: Componentes de form e botão já existentes na página — só um `if (config?.managed_by_env)` a mais em torno do JSX do form
**Requirement**: GHS-10, GHS-11

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] `managed_by_env: true` → form de credenciais não renderiza, só botão de instalação
- [ ] `managed_by_env: false` → tela idêntica à de hoje, sem regressão
- [ ] Strings novas (se houver, ex: texto explicando "App gerenciado pela ZeepLabs") passam por `react-i18next`, adicionadas em `en.json` e `pt-BR.json` na mesma mudança (regra `AGENTS.md` §5)
- [ ] Gate check passa: `npx tsc -b && npm run build`

**Tests**: none (mesma justificativa de UI já usada em `github-integration/tasks.md` T-10/T-11 — sem TESTING.md exigindo e2e aqui)
**Gate**: ui-build

---

### T-06: Documentação — env vars do App compartilhado

**What**: Adicionar as 6 novas env vars (`GITHUB_SHARED_APP_ID`, `GITHUB_SHARED_APP_SLUG`, `GITHUB_SHARED_APP_CLIENT_ID`, `GITHUB_SHARED_APP_CLIENT_SECRET`, `GITHUB_SHARED_APP_PRIVATE_KEY_B64`, `GITHUB_SHARED_APP_WEBHOOK_SECRET`) na tabela de configuração do `README.md` (e nas 3 traduções em `i18n/`, regra `AGENTS.md` §6), e uma nota em `RELEASE.md`/`CHANGELOG.md` explicando que são opcionais e, quando ausentes, o fluxo legado (form manual) continua funcionando.
**Where**: `README.md`, `i18n/README.pt-BR.md`, `i18n/README.pt-PT.md`, `i18n/README.es.md`, `CHANGELOG.md`
**Depends on**: T-03, T-05
**Reuses**: Tabela de configuração já existente no README
**Requirement**: GHS-01 (documentação)

**Tools**: MCP: NONE · Skill: NONE

**Done when**:
- [ ] 6 env vars documentadas nas 4 versões do README, com nota "opcional — só necessário se a ZeepLabs for distribuir um App compartilhado"
- [ ] `CHANGELOG.md` ganha entrada em `[Unreleased]`
- [ ] `python3 -c "import json; json.load(open('src/locales/en.json'))"` e `pt-BR.json` (se strings de UI da T-05 tocaram JSON de i18n) continuam válidos

**Tests**: none
**Gate**: none (validação manual de markdown/JSON)

---

## Parallel Execution Map

```
Phase 1 (Sequential):
  T-01

Phase 2 (Sequential):
  T-01 done → T-02

Phase 3 (Parallel):
  ├── T-03 [P] (needs T-02)
  └── T-04 [P] (needs T-01)

Phase 4 (Sequential):
  T-04 done → T-05

Phase 5 (Sequential):
  T-03, T-05 done → T-06
```

---

## Task Granularity Check

| Task | Scope | Status |
|---|---|---|
| T-01 | 1 componente (config env) | ✅ Granular |
| T-02 | 1 componente (seeder), orquestração fina | ✅ Granular |
| T-03 | 1 mudança de wiring (boot) | ✅ Granular |
| T-04 | 1 mudança de handler (campo + assinatura) | ✅ Granular |
| T-05 | 1 página, mudança condicional de render | ✅ Granular |
| T-06 | Documentação (5 arquivos, mesma mudança replicada) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
|---|---|---|---|
| T-01 | None | None | ✅ Match |
| T-02 | T-01 | T-01 | ✅ Match |
| T-03 | T-02 | T-02 | ✅ Match |
| T-04 | T-01 | T-01 | ✅ Match |
| T-05 | T-04 | T-04 | ✅ Match |
| T-06 | T-03, T-05 | T-03, T-05 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Inferred Requirement | Task Says | Status |
|---|---|---|---|---|
| T-01 | Config (internal package) | unit | unit | ✅ OK |
| T-02 | Orquestração de boot (dashboard) | integration (toca DB real) | integration | ✅ OK |
| T-03 | Wiring de boot | none (coberto por T-02) | none | ✅ OK |
| T-04 | Handler HTTP (dashboard) | integration (padrão `handler_test.go`) | integration | ✅ OK |
| T-05 | Página React | none (sem padrão de teste unitário de página) | none | ✅ OK |
| T-06 | Documentação | none | none | ✅ OK |

---

## Tools confirmados (Execute)

- Nenhum MCP necessário — toda a feature reusa APIs/stores já existentes e documentadas no código; nenhuma chamada nova à API do GitHub é introduzida (mesmos endpoints de `VerifyAppCredentials` já usados hoje).
- **`code-review` skill**: rodar ao final da Phase 3 (boot + handler prontos) e ao final da Phase 4 (UI pronta), antes de fechar a feature.
- **`verify` skill**: rodar após T-05 — fluxo completo manual: subir instância com env vars do App compartilhado configuradas, confirmar que a tela mostra só o botão de instalação e que a instalação funciona fim-a-fim.
