# Tasks: Enterprise Licensing

**Spec**: `.specs/features/enterprise-licensing/spec.md`
**Design**: `.specs/features/enterprise-licensing/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build`), mesmo critério de `frontend-app-entity/tasks.md`.

**Pré-requisito externo**: nenhum dentro do zeep-orbit. `license-server` (repo separado) não é dependência de bloqueio — as tasks T-05/T-06 assumem apenas o contrato de `GET /v1/status?ref=` fixado no `design.md`; um stub/mock local cobre os testes até o `license-server` real existir.

**Nota de estado real (2026-08-10) — UI mock já mergeada, backend ainda Draft**: o `Status: Draft` acima continua correto para o escopo desta spec (backend `internal/enterprise/` não existe: `ls internal/` não lista o pacote, não há `License`/`Verify`/`HasFeature`/`GET /dashboard/api/license/status`). Porém a **UI de preview já está no repo**, entregue por `dashboard-settings-consolidation` S3.2, não por esta spec:

- `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx:574-614+` — `LicensePreviewState`/`LICENSE_PREVIEW_STATES` e o componente `LicenseTab()`, com plan card, feature checklist, switcher de estado (Free/Enterprise/Trial/Expired), textarea de license key e demo de badge/upgrade. Comentário no próprio código: *"License tab: UI-only preview (no `internal/enterprise` backend exists yet)"*. Todos os dados são hardcoded e as ações chamam `previewOnly()` (`toast.info`), sem persistir nada.
- A tab está **travada atrás de um `disabled`**: `BrandSettingsPage.tsx:82-83` renderiza o `TabsTrigger` com `disabled={tb.value === "license"}` + `title={t("apps.soon")}`, então o preview é inalcançável por clique em produção.

Consequência para o planejamento desta spec: **T-09 (Página "Licença" + badge de upgrade) já tem a casca visual pronta e não deve ser reescrita do zero** — a task vira "ligar o `LicenseTab` existente ao backend real (T-06 `GET /dashboard/api/license/status` + T-08 `useFeature`), substituir os dados hardcoded e remover o `disabled` do `TabsTrigger`". T-10 (i18n) também já tem parte das chaves `settings.license*` criadas em `en.json`/`pt-BR.json`. Nenhuma outra task muda.

---

## Execution Plan

```
Fase 1: Núcleo de verificação            Fase 2: Boot e config
┌──────────────────────────┐            ┌───────────────────────────┐
│ T-01 License + Verify     │───────────▶│ T-04 wire no boot           │
│ T-02 Feature registry     │            │ (LICENSE_KEY, README)      │
│ T-03 State (in-memory)    │            └───────────────────────────┘
└──────────────────────────┘                        │
                                                     ▼
Fase 3: Revogação                         Fase 4: API do dashboard
┌──────────────────────────┐            ┌───────────────────────────┐
│ T-05 refresh + client HTTP│───────────▶│ T-06 GET /license/status    │
│ (license-server opcional) │            │ T-07 audit log events      │
└──────────────────────────┘            └───────────────────────────┘
                                                     │
                                                     ▼
Fase 5: Frontend                          Fase 6: Licença do repositório
┌──────────────────────────┐            ┌───────────────────────────┐
│ T-08 useFeature hook       │           │ T-11 LICENSE raiz +         │
│ T-09 página Licença        │──────────▶│ internal/enterprise/LICENSE │
│ T-10 i18n en/pt-BR         │           │ (placeholder, gate legal)   │
└──────────────────────────┘            └───────────────────────────┘
                                                     │
                                                     ▼
                                          Fase 7: Docs e changelog
                                          ┌───────────────────────────┐
                                          │ T-12 README + CHANGELOG     │
                                          └───────────────────────────┘
```

---

### T-01: `License` — tipos e `Verify` (Ed25519)

- **What**: Struct `License` (`Org string` — UUID interno do license-server, não nome legível; `Product string` — SHALL ser `"orbit"`, payload de outro produto é inválido; `Plan`, `Ref`, `Trial`, `IssuedAt`, `ExpiresAt time.Time`, `RenewalDueAt time.Time` — D-133/D-134: `ExpiresAt` já vem do `license-server` com os 7 dias de graça embutidos, verificado contra a implementação real em 2026-08-21, `RenewalDueAt` é só a data real de cobrança pra exibição, nunca usada em lógica de gating), constantes `PlanOSS`/`PlanEnterprise`, função `Verify(key string) *License` que faz split/decode base64url, `ed25519.Verify` contra chave pública embutida, parse do JSON (campos `iat`/`exp`/`renewal_due_at` são Unix timestamp `int64`, não string ISO), checagem de `exp` e de `product == "orbit"`. Nunca retorna erro fatal — qualquer falha de verificação (incluindo `product` errado/ausente) retorna `License{Plan: PlanOSS}` e loga warning.
- **Where**: `internal/enterprise/license/license.go`, `internal/enterprise/license/publickey.go`
- **Depends on**: nenhuma
- **Reuses**: nenhum (pacote novo)
- **Requirement**: LIC-01, LIC-02, LIC-03, LIC-04, LIC-06
- **Tools**: nenhum externo — gerar par de chaves de teste localmente com `ed25519.GenerateKey` num `_test.go`, nunca usar chave de produção em teste
- **Done when**: `Verify` cobre key válida, expirada, assinatura adulterada, JSON corrompido, sem crash em nenhum caso
- **Tests**: tabela de casos em `license_test.go` (válida vitalícia, válida com trial+exp curto, expirada, assinatura tampered, base64 inválido, JSON malformado) — todas usando par de chaves de teste gerado no próprio teste
- **Gate**: `go test ./internal/enterprise/... -run TestVerify` + `go vet ./...` + `gofmt -l internal/enterprise/license`
- **Commit**: não (agrupa com T-02/T-03)

---

### T-02: Registry de features (`HasFeature`)

- **What**: Tipo `Feature string`, mapa `planFeatures map[Plan][]Feature` (vazio de features concretas neste momento — só a estrutura, ver Out of Scope do spec), função `HasFeature(l *License, f Feature) bool` (licença `nil` ou plano sem a feature → `false`).
- **Where**: `internal/enterprise/license/features.go`
- **Depends on**: T-01
- **Reuses**: nenhum
- **Requirement**: LIC-10, LIC-11, LIC-12
- **Tools**: nenhum
- **Done when**: adicionar uma feature de teste ao mapa e verificar `HasFeature` para os 2 planos, sem tocar em nenhum outro arquivo
- **Tests**: matriz plano×feature com pelo menos 1 feature fictícia de teste (`featureTestOnly`) declarada só no `_test.go`
- **Gate**: `go test ./internal/enterprise/... -run TestHasFeature`
- **Commit**: não (agrupa com T-01/T-03)

---

### T-03: `State` — licença ativa em memória

- **What**: `Load(key string) *State` (chama `Verify` uma vez), `(*State).Current() *License` (leitura com `RLock`), estrutura preparada para `StartRefresh` (T-05) atualizar via `Lock`.
- **Where**: `internal/enterprise/license/state.go`
- **Depends on**: T-01
- **Reuses**: nenhum
- **Requirement**: LIC-01, LIC-05
- **Tools**: nenhum
- **Done when**: `Load("")` (sem key) resolve `PlanOSS` sem log de erro (AC LIC-05 — ausência de licença não é erro)
- **Tests**: `Load` com key vazia, key válida, key inválida; concorrência básica (`go test -race`) entre leitura e escrita simulada
- **Gate**: `go test -race ./internal/enterprise/... -run TestState`
- **Commit**: `feat(license): add offline verification core (License, HasFeature, State)` (T-01+T-02+T-03)

---

### T-04: Wire no boot + configuração

- **What**: `cmd/zeep` chama `license.Load(os.Getenv("LICENSE_KEY"))` no startup, guarda resultado acessível ao resto do processo (mesmo nível de outras configs globais de boot). Documentar `LICENSE_KEY`, `LICENSE_SERVER_URL`, `LICENSE_REFRESH_INTERVAL` na tabela de configuração do `README.md`.
- **Where**: `cmd/zeep/main.go` (ou equivalente já existente de boot), `README.md`
- **Depends on**: T-03
- **Reuses**: padrão de leitura de env vars já usado no boot atual
- **Requirement**: LIC-01, LIC-05, LIC-06
- **Tools**: nenhum
- **Done when**: processo sobe normalmente sem `LICENSE_KEY` setada, e com key válida/inválida — em todos os casos, sem crash
- **Tests**: teste de boot (subir processo/handler raiz e checar que não há panic) para os 3 cenários
- **Gate**: `go build ./...` + `go test ./cmd/...`
- **Commit**: `feat(license): wire license loading into boot, document env vars` (T-04 sozinho, já que é o ponto de integração visível)

---

### T-05: Refresh periódico de revogação (client do `license-server`)

- **What**: Cliente HTTP simples (`net/http`, timeout curto, sem retry) para `GET {LICENSE_SERVER_URL}/v1/status?product=orbit&ref={ref}` (`product` obrigatório — contrato verificado contra a implementação real do `zeep-license-server` em 2026-08-21, ausente na versão original desta task); `StartRefresh(ctx, serverURL, interval)` roda em goroutine com `time.Ticker`, atualiza `State` só se `revoked: true`; resposta real também traz `expires_at` (ignorado por este MVP, ver design.md); qualquer erro de rede/timeout/5xx/404 mantém o estado atual (fail-open, AC LIC-23 — o servidor real não implementa fail-open nenhum, é responsabilidade só do client).
- **Where**: `internal/enterprise/license/refresh.go`
- **Depends on**: T-03, T-04 (para saber quando iniciar a goroutine)
- **Reuses**: nenhum cliente HTTP genérico identificado no repo para reusar — `net/http.Client` direto, sem lib externa
- **Requirement**: LIC-20, LIC-21, LIC-22, LIC-23, LIC-24, LIC-25
- **Tools**: nenhum — servidor de teste local (`httptest.Server`) simula `license-server` real e falso-fora-do-ar
- **Done when**: revogação confirmada transiciona `State` para `PlanOSS` no ciclo seguinte; servidor fora do ar não altera nada; `LICENSE_SERVER_URL` vazio nunca dispara request; request sempre inclui `product=orbit`
- **Tests**: `httptest.Server` respondendo `revoked: true`, `revoked: false`, 500, timeout, e um caso confirmando que a query string enviada inclui `product=orbit` — 5 casos mínimos
- **Gate**: `go test ./internal/enterprise/... -run TestRefresh`
- **Commit**: `feat(license): add optional revocation refresh against license-server` (T-05 sozinho)

---

### T-06: `GET /dashboard/api/license/status`

- **What**: Handler que retorna `{plan, features: []string, org, trial, expires_at, renewal_due_at}` a partir do `State.Current()` — nunca expõe a key crua nem o `ref` completo.
- **Where**: `internal/dashboard/license.go`
- **Depends on**: T-03
- **Reuses**: roteamento padrão do dashboard, mesmo padrão de handler leve de outros endpoints públicos de config
- **Requirement**: LIC-13
- **Tools**: nenhum
- **Done when**: endpoint responde 200 sem autenticação (mesmo nível de `usePublicConfig`), payload correto para `oss` e `enterprise`
- **Tests**: teste HTTP com `State` mockado nos 2 planos
- **Gate**: `go test ./internal/dashboard/... -run TestLicenseStatus`
- **Commit**: não (agrupa com T-07)

---

### T-07: Enforcement inline + audit log

- **What**: Padrão de checagem `if !enterprise.HasFeature(license, feature) { return apperror.Forbidden(...) }` documentado/exemplificado (sem feature concreta ainda a gatear — este spec não define nenhuma, ver Out of Scope). Eventos de audit log `license.loaded` (no boot, com plano resolvido) e `license.revoked` (na transição do refresh).
- **Where**: `internal/dashboard/license.go` (exemplo/helper), `internal/enterprise/license/state.go` (chamada ao audit no momento da transição)
- **Depends on**: T-04, T-05, T-06
- **Reuses**: `h.audit(...)` já existente em `internal/dashboard/audit_store.go`
- **Requirement**: LIC-10, LIC-11, LIC-12
- **Tools**: nenhum
- **Done when**: audit log recebe evento no boot e num teste simulado de revogação; mensagem de 403 confirmada em inglês
- **Tests**: teste de integração checando entrada no audit log para os 2 eventos
- **Gate**: `go test ./internal/dashboard/... -run TestLicenseAudit`
- **Commit**: `feat(license): add dashboard status endpoint, audit events and enforcement pattern` (T-06+T-07)

---

### T-08: Hook `useFeature`

- **What**: `useFeature(name: string): boolean`, lendo do payload já carregado por `usePublicConfig()` (licença entra como mais um campo desse config, sem fetch próprio).
- **Where**: `internal/dashboard/ui/src/enterprise/useFeature.ts`
- **Depends on**: T-06
- **Reuses**: `usePublicConfig()` existente em `src/lib/api.ts`
- **Requirement**: LIC-13
- **Tools**: nenhum
- **Done when**: hook retorna `false` por padrão (sem config carregada ainda) e o valor correto após load
- **Tests**: teste de hook (testing-library) com config mockado `enterprise`/`oss`
- **Gate**: `npx tsc -b` + teste do hook
- **Commit**: não (agrupa com T-09/T-10)

---

### T-09: Página "Licença" + badge de upgrade

- **What**: Página no dashboard mostrando plano atual, `org`, status (válida/expirada/revogada), campo para colar nova key (some/reaparece a depender de permissão — reusa checagem de admin já existente, não RBAC per-app que ainda não existe). Componente de badge "Enterprise" + link de upgrade para uso em qualquer feature futura gated. Novo (D-134/LIC-16): quando `renewal_due_at` estiver a ≤14 dias, exibir aviso de renovação próxima — visualmente distinto de "licença expirada/travada" (graça de 7 dias após `exp` nunca é mostrada como problema até o `exp` real passar).
- **Where**: `internal/dashboard/ui/src/enterprise/LicensePage.tsx`, `internal/dashboard/ui/src/enterprise/EnterpriseBadge.tsx`
- **Depends on**: T-08
- **Reuses**: componentes de layout/formulário já existentes no dashboard, `toast.error` padrão em mutação de erro
- **Requirement**: LIC-14, LIC-16
- **Tools**: nenhum
- **Done when**: página renderiza os 4 estados de licença (sem key, válida em dia, válida com renovação próxima, expirada/travada) sem erro de console
- **Tests**: teste de componente para os 4 estados
- **Gate**: `npx tsc -b` + `npm run build`
- **Commit**: não (agrupa com T-10)

---

### T-10: i18n das strings novas

- **What**: Todas as strings de T-09 (e mensagens de erro do `useFeature`/badge) adicionadas em `en.json` e `pt-BR.json` na mesma mudança.
- **Where**: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
- **Depends on**: T-09
- **Reuses**: `react-i18next` já configurado no projeto
- **Requirement**: LIC-15
- **Tools**: nenhum
- **Done when**: `python3 -c "import json; json.load(open(...))"` passa para os 2 arquivos, nenhuma string hardcoded restante em T-09
- **Tests**: validação JSON (comando acima) + revisão manual de que nenhum texto ficou fora do `t()`
- **Gate**: validação JSON dos 2 locales + `npx tsc -b`
- **Commit**: `feat(license): add license page, upgrade badge and useFeature hook` (T-08+T-09+T-10)

---

### T-11: `LICENSE` raiz e `internal/enterprise/LICENSE` (placeholder)

- **What**: Reescrever `LICENSE` raiz como disclosure multi-licença (MIT geral + aponta para `internal/enterprise/LICENSE`), criar `internal/enterprise/LICENSE` com texto **placeholder** claramente marcado como não-final.
- **Where**: `LICENSE`, `internal/enterprise/LICENSE`
- **Depends on**: nenhuma (pode rodar em paralelo com as demais)
- **Reuses**: nenhum
- **Requirement**: Success Criteria do spec (disclosure de licença)
- **Tools**: nenhum
- **Done when**: arquivo existe, mas **este PR não deve ser publicado/mergeado em `main` com o texto final sem revisão jurídica** — gate explícito abaixo
- **Tests**: nenhum (arquivo de texto, não código)
- **Gate**: **bloqueio manual — obter validação jurídica do texto antes de qualquer release que inclua este arquivo como definitivo**; até lá, o arquivo deve conter aviso visível de "DRAFT — pending legal review" no topo
- **Commit**: `docs(license): add multi-license disclosure (draft, pending legal review)`

---

### T-12: README e CHANGELOG

- **What**: Tabela de configuração do README com as 3 env vars (T-04 já adicionou, aqui é revisão final), entrada em `CHANGELOG.md` sob `## [Unreleased]` descrevendo a feature.
- **Where**: `README.md`, `CHANGELOG.md`
- **Depends on**: T-04 até T-10 (documenta o conjunto já implementado)
- **Reuses**: convenção existente de `## [Unreleased]`
- **Requirement**: nenhum ID específico — item de processo (`AGENTS.md` seção 6)
- **Tools**: nenhum
- **Done when**: entrada no CHANGELOG presente na mesma mudança que fecha a feature, não deixada para o dia do release
- **Tests**: nenhum
- **Gate**: revisão visual do diff de `README.md`/`CHANGELOG.md`
- **Commit**: `docs: document enterprise licensing configuration and changelog entry`

---

## Notas de execução

- T-01/T-02/T-03 não têm nenhuma feature enterprise concreta pra gatear — o mecanismo nasce vazio de propósito (spec explicitamente fora de escopo). A primeira feature real a usar `HasFeature` vem de um spec futuro separado.
- T-05 depende só do contrato (`GET /v1/status?ref=`), não do `license-server` real existir — testável inteiramente com `httptest.Server` local.
- T-11 é o único item com gate não-técnico (jurídico) — não deve bloquear o merge das demais tasks, mas bloqueia especificamente a publicação do texto de licença como definitivo.
