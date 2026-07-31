# Tasks: Observability Integrations

**Spec**: `.specs/features/observability-integrations/spec.md`
**Design**: `.specs/features/observability-integrations/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build`), mesmo critério de `enterprise-licensing/tasks.md`.

**Pré-requisito interno (bloqueante)**: `internal/enterprise/license` (spec `enterprise-licensing`) precisa existir com `HasFeature`/`license.State` implementados **antes** de T-05 (gate por provider) — hoje só o spec/design/tasks dessa feature estão escritos, o código ainda não. T-01 a T-04 (config + exporters) não dependem disso e podem ser implementadas em paralelo/antes.

---

## Execution Plan

```
Fase 1: Config por app                    Fase 2: Exporters
┌──────────────────────────┐            ┌───────────────────────────┐
│ T-01 tabela + ConfigStore  │───────────▶│ T-03 Exporter interface     │
│ T-02 ObservabilityConfig   │            │       + providerRegistry    │
│      Handler (CRUD)        │            │ T-04 OTelExporter (core)   │
└──────────────────────────┘            │ T-05 DatadogExporter        │
                                          │ T-06 NewRelicExporter       │
                                          │      (T-05/T-06 = enterprise)│
                                          └───────────────────────────┘
                                                     │
                                                     ▼
Fase 3: Manager (batch + gate)            Fase 4: Frontend
┌──────────────────────────┐            ┌───────────────────────────┐
│ T-07 Manager (ticker,     │───────────▶│ T-09 página Observability   │
│      leitura RingBuffer)  │            │ T-10 EnterpriseBadge nos    │
│ T-08 gate por provider    │            │      providers gated        │
│      (HasFeature)         │            │ T-11 i18n en/pt-BR          │
└──────────────────────────┘            └───────────────────────────┘
                                                     │
                                                     ▼
                                          Fase 5: Docs e changelog
                                          ┌───────────────────────────┐
                                          │ T-12 README + CHANGELOG     │
                                          └───────────────────────────┘
```

---

### T-01: Tabela `observability_configs` + `ConfigStore`

- **What**: Migration criando `zeep_system.observability_configs` (colunas conforme `design.md`, `UNIQUE(app_name, provider)`). `ConfigStore` com `UpsertConfig`, `ListConfigs`, `DeleteConfig` — `api_key` sempre passa por `crypto.Encrypt` antes de persistir, nunca decodificada de volta em resposta de API.
- **Where**: migration em `internal/db/migrations/` (próximo número livre), `internal/observability/config_store.go`
- **Depends on**: nenhuma
- **Reuses**: `crypto.Encrypt`/`Decrypt`, padrão de tabela de `deploy_provider_configs`
- **Requirement**: OBS-01, OBS-02, OBS-03
- **Tools**: nenhum
- **Done when**: upsert do mesmo `app_name+provider` atualiza em vez de duplicar; múltiplos providers no mesmo app coexistem
- **Tests**: `config_store_test.go` — upsert idempotente, listagem por app, delete, unicidade violada retorna erro tratável
- **Gate**: `go test ./internal/observability/... -run TestConfigStore`
- **Commit**: não (agrupa com T-02)

---

### T-02: `ObservabilityConfigHandler` (CRUD + audit)

- **What**: `GET/POST/PATCH/DELETE /dashboard/api/apps/{app}/observability/configs`, nunca retorna `api_key` em claro (só provider/enabled/endpoint/extra). Toda mutação gera `InsertAuditLog`. `PATCH` segue merge-on-absent-key (chave explícita pra limpar campo).
- **Where**: `internal/dashboard/observability_config.go`
- **Depends on**: T-01
- **Reuses**: esqueleto de `DeployProviderConfigHandler`, `h.audit(...)` de `audit_store.go`
- **Requirement**: OBS-01, OBS-02, OBS-04, OBS-05
- **Tools**: nenhum
- **Done when**: resposta de `GET` nunca contém `api_key_encrypted`/key decodificada; update parcial que omite campo não apaga valor existente
- **Tests**: teste HTTP cobrindo create/list/update parcial/delete + verificação de audit log gerado em cada mutação
- **Gate**: `go test ./internal/dashboard/... -run TestObservabilityConfig`
- **Commit**: `feat(observability): add per-app provider config CRUD with encrypted keys` (T-01+T-02)

---

### T-03: `Exporter` interface + `providerRegistry`

- **What**: Interface `Exporter.Send(ctx, []dashboard.LogEntry) error`, tipo `Provider`, `providerRegistry` mapeando provider → `Feature` (vazio pra core) + construtor. Nenhuma implementação concreta ainda (isso é T-04/T-05/T-06).
- **Where**: `internal/observability/exporter.go`
- **Depends on**: nenhuma
- **Reuses**: nenhum
- **Requirement**: OBS-20, OBS-24
- **Tools**: nenhum
- **Done when**: registrar um provider de teste fictício no mapa não exige alterar `Manager`
- **Tests**: teste de registro (provider fictício core e gated, ambos resolvidos corretamente pelo registry)
- **Gate**: `go test ./internal/observability/... -run TestProviderRegistry`
- **Commit**: não (agrupa com T-04)

---

### T-04: `OTelExporter` (core, sem gate)

- **What**: Converte `[]dashboard.LogEntry` pra log records OTLP/HTTP (`POST {endpoint}/v1/logs`), atributos `http.method`/`http.path`/`http.status_code`/`duration_ms`/`app`. Header de auth opcional vindo de `Extra`. Registrado no `providerRegistry` com `Feature: ""`.
- **Where**: `internal/observability/otel.go`
- **Depends on**: T-03
- **Reuses**: `net/http.Client` simples, sem lib externa
- **Requirement**: OBS-10, OBS-20
- **Tools**: nenhum
- **Done when**: payload OTLP validado contra `httptest.Server`, funciona sem nenhuma checagem de licença
- **Tests**: `httptest.Server` recebendo o payload, checagem de campos obrigatórios
- **Gate**: `go test ./internal/observability/... -run TestOTelExporter`
- **Commit**: `feat(observability): add OpenTelemetry exporter (core, no license gate)` (T-03+T-04)

---

### T-05: `DatadogExporter` (enterprise)

- **What**: `POST https://http-intake.logs.{site}/api/v2/logs`, header `DD-API-KEY`, `site` de `Extra.site` (default `datadoghq.com`), `status: "error"` quando `http.status_code >= 500` senão `"info"`. Registrado com `Feature: license.FeatureObservabilityDatadog`.
- **Where**: `internal/observability/datadog.go`
- **Depends on**: T-03, constante `license.FeatureObservabilityDatadog` (bloqueado pelo pré-requisito interno de `enterprise-licensing`)
- **Reuses**: mesmo client HTTP simples de T-04
- **Requirement**: OBS-10
- **Tools**: nenhum
- **Done when**: payload validado contra `httptest.Server`, severidade mapeada corretamente por status
- **Tests**: `httptest.Server`, casos status 2xx/4xx/5xx mapeando pra `info`/`error`
- **Gate**: `go test ./internal/observability/... -run TestDatadogExporter`
- **Commit**: não (agrupa com T-06)

---

### T-06: `NewRelicExporter` (enterprise)

- **What**: `POST https://log-api.newrelic.com/log/v1`, header `Api-Key`, mesma lógica de severidade de T-05. Registrado com `Feature: license.FeatureObservabilityNewRelic`.
- **Where**: `internal/observability/newrelic.go`
- **Depends on**: T-03, constante `license.FeatureObservabilityNewRelic`
- **Reuses**: mesmo client HTTP simples de T-04/T-05
- **Requirement**: OBS-10
- **Tools**: nenhum
- **Done when**: payload validado contra `httptest.Server`
- **Tests**: `httptest.Server`, mesma matriz de status de T-05
- **Gate**: `go test ./internal/observability/... -run TestNewRelicExporter`
- **Commit**: `feat(observability): add Datadog and New Relic exporters (enterprise-gated)` (T-05+T-06)

---

### T-07: `Manager` — ciclo periódico + leitura do RingBuffer

- **What**: `NewManager(buf, store, licenseState, interval)`; `Start(ctx)` inicia goroutine por app com config habilitada; cursor por timestamp em memória, por app; sem provider habilitado = sem goroutine.
- **Where**: `internal/observability/manager.go`
- **Depends on**: T-01, T-03
- **Reuses**: padrão de goroutine+ticker de `license.State.StartRefresh`
- **Requirement**: OBS-10, OBS-11, OBS-12, OBS-15
- **Tools**: nenhum
- **Done when**: app sem config não gera goroutine; app com config só exporta entries novas desde o último ciclo
- **Tests**: `RingBuffer` de teste populado, ciclo do `Manager` verificado com `httptest.Server`, `go test -race`
- **Gate**: `go test -race ./internal/observability/... -run TestManager`
- **Commit**: não (agrupa com T-08)

---

### T-08: Gate de licença por provider no `Manager`

- **What**: Antes de despachar pra um provider com `Feature != ""`, `Manager` chama `enterprise.HasFeature(licenseState.Current(), providerRegistry[p].Feature)`; se `false`, skip com log/evento `observability.export_skipped_no_license` (distinto de erro de rede), sem interromper os demais providers do mesmo app.
- **Where**: `internal/observability/manager.go`
- **Depends on**: T-07, `enterprise.HasFeature` (pré-requisito interno)
- **Reuses**: `enterprise.HasFeature`, `license.State.Current()`
- **Requirement**: OBS-20, OBS-21, OBS-22, OBS-23
- **Tools**: nenhum
- **Done when**: Datadog/New Relic pulados sem licença enquanto OTel no mesmo app continua exportando; licença cai/volta durante execução reflete no próximo ciclo sem restart
- **Tests**: cenário com 3 providers no mesmo app, licença oss/enterprise/revogada simulada via `State` mockado
- **Gate**: `go test -race ./internal/observability/... -run TestManagerLicenseGate`
- **Commit**: `feat(observability): add periodic batch manager with per-provider license gate` (T-07+T-08)

---

### T-09: Página "Observability"

- **What**: Página listando providers configuráveis (OTel/Datadog/New Relic) por app, formulário de config por provider, indicação de status (ativo/pausado por licença/erro).
- **Where**: `internal/dashboard/ui/src/pages/ObservabilityPage.tsx`
- **Depends on**: T-02
- **Reuses**: componentes de layout/formulário existentes, `toast.error` padrão em erro de mutação
- **Requirement**: OBS-01
- **Tools**: nenhum
- **Done when**: página renderiza os 3 providers, salva config, mostra erro via toast em falha de mutação
- **Tests**: teste de componente cobrindo os estados principais (sem config, configurado, erro de save)
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-10/T-11)

---

### T-10: `EnterpriseBadge` em Datadog/New Relic

- **What**: Aplicar `EnterpriseBadge`/`useFeature` (do spec `enterprise-licensing`, `ui-design-brief.md`) nos cards de Datadog e New Relic — se licença não cobre, badge + tooltip/modal de upgrade; salvar config continua permitido (pré-configurar antes de comprar), só o envio real fica pausado.
- **Where**: `internal/dashboard/ui/src/pages/ObservabilityPage.tsx`
- **Depends on**: T-09, `EnterpriseBadge`/`useFeature` (pré-requisito interno de `enterprise-licensing`)
- **Reuses**: `EnterpriseBadge`, `useFeature`
- **Requirement**: OBS-20
- **Tools**: nenhum
- **Done when**: card de OTel nunca mostra badge; cards de Datadog/New Relic mostram badge só quando licença não cobre
- **Tests**: teste de componente com `useFeature` mockado true/false
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-11)

---

### T-11: i18n das strings novas

- **What**: Todas as strings de T-09/T-10 adicionadas em `en.json` e `pt-BR.json` na mesma mudança.
- **Where**: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
- **Depends on**: T-09, T-10
- **Reuses**: `react-i18next` já configurado
- **Requirement**: OBS-06
- **Tools**: nenhum
- **Done when**: `python3 -c "import json; json.load(open(...))"` passa para os 2 arquivos, nenhuma string hardcoded restante
- **Tests**: validação JSON dos 2 locales
- **Gate**: validação JSON + `npx tsc -b` + `npm run build`
- **Commit**: `feat(observability): add Observability page with enterprise badges and i18n` (T-09+T-10+T-11)

---

### T-12: README e CHANGELOG

- **What**: Documentar a nova tabela/env vars (se houver, ex: intervalo do batch configurável) na tabela de configuração do `README.md`; entrada em `CHANGELOG.md` sob `## [Unreleased]`.
- **Where**: `README.md`, `CHANGELOG.md`
- **Depends on**: T-01 até T-11
- **Reuses**: convenção existente de `## [Unreleased]`
- **Requirement**: nenhum ID específico — item de processo (`AGENTS.md` seção 6)
- **Tools**: nenhum
- **Done when**: entrada no CHANGELOG presente na mesma mudança que fecha a feature
- **Tests**: nenhum
- **Gate**: revisão visual do diff de `README.md`/`CHANGELOG.md`
- **Commit**: `docs: document observability integrations configuration and changelog entry`

---

## Notas de execução

- T-01 a T-04 (config + OTelExporter) não dependem de `enterprise-licensing` estar implementado — podem começar imediatamente.
- T-05, T-06, T-08, T-10 dependem de `internal/enterprise/license` (constantes `Feature`, `HasFeature`, `State`) existir de fato — hoje só spec/design/tasks estão escritos nesse outro feature, sem código. Bloqueio real, não hipotético.
- Nenhuma feature de observabilidade da própria plataforma zeep-orbit é coberta aqui — está fora de escopo por decisão explícita (ver spec.md).
