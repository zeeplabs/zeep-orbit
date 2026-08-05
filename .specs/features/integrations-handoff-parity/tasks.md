# Integrations Page — Handoff Parity Tasks

**Spec**: `.specs/features/integrations-handoff-parity/spec.md`
**Design**: `.specs/features/integrations-handoff-parity/design.md`

Legenda status: ☐ pending · ◐ in progress · ☑ done
Cada task fecha com AGENTS §3 limpo (go build/test/vet/gofmt para tasks de backend, tsc/vite build para tasks de frontend) + i18n en+pt-BR no mesmo commit, quando aplicável.

---

## Fase 1 — UI-only (sem backend novo, mergeável independente da Fase 2/3)

- [x] **T1.1** — Extrair `AboutPanel` de `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx` para `internal/dashboard/ui/src/components/patterns/AboutPanel.tsx`, exportar no barrel `src/components/patterns/index.ts`, atualizar import em `BrandSettingsPage.tsx`. Sem mudança de comportamento — puro move + reexport.
- [x] **T1.2** — Tab Configuration (`GitHubConfigTab`, `GitHubIntegrationPage.tsx:159-488`): adicionar cards `ProviderCard` desabilitados para "GitLab" (`icon="merge_type"`) e "Bitbucket" (`icon="account_tree"`), badge `t("apps.soon")` (chave já existente), ao lado do card GitHub ativo. Novas chaves i18n: `integrations.codeHostGitlabDesc`, `integrations.codeHostBitbucketDesc` (en + pt-BR).
- [x] **T1.3** — Tab Configuration: adicionar seção "Linked templates" reaproveitando os dados já buscados pela tab Templates (nome + repo, read-only, sem fetch novo). Chave i18n `integrations.linkedTemplatesTitle`.
- [x] **T1.4** — Tab Configuration: envolver o retorno em layout `flex flex-wrap items-start gap-6` (mesmo padrão usado em `BrandSettingsPage.tsx` nesta sessão) e adicionar `<AboutPanel title={t("integrations.aboutCodeHostingTitle")} lines={[...]} />`. Chaves: `integrations.aboutCodeHostingTitle`, `integrations.aboutCodeHostingLine1` (conteúdo: explicar que o code host ativo é usado pra criar repos a partir de templates).
- [x] **T1.5** — Tab Deploy providers (`DeployTab`, `GitHubIntegrationPage.tsx:877-1057`): adicionar cards `ProviderCard` desabilitados para "Cloudflare Pages" (`icon="cloud"`), "DigitalOcean" (`icon="water_drop"`), "AWS" (`icon="cloud_queue"`), "Azure" (`icon="web_stories"`), "Google Cloud" (`icon="cloud_circle"`), badge `t("apps.soon")`, ao lado do card Render ativo. Chaves i18n `integrations.deployProviderCloudflareDesc`/`DigitaloceanDesc`/`AwsDesc`/`AzureDesc`/`GcpDesc` (en + pt-BR).
- [x] **T1.6** — Tab Deploy providers: mesmo padrão de layout de T1.4, `<AboutPanel title={t("integrations.aboutDeployProvidersTitle")} lines={[...]} />`. Chaves: `integrations.aboutDeployProvidersTitle`, `...Line1` (conteúdo: provider ativo é usado pra todo novo deploy de frontend app; apps existentes continuam no provider com que foram criados; API keys criptografadas em repouso).
- [x] **T1.7** — Rodar `npx tsc -b` e `npm run build` (dir `internal/dashboard/ui`), validar JSON dos 2 locales (`python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json'))"`). Adicionar entrada em `CHANGELOG.md` `[Unreleased] > Changed`.

## Fase 2 — Backend: Recent Deploys (endpoint agregador)

- [x] **T2.1** — `internal/deploy/render/render.go`: adicionar tipo `Deploy` e método `ListDeploys(ctx context.Context, serviceID string, limit int, statuses []string) ([]Deploy, error)`, chamando `GET /v1/services/{serviceID}/deploys?limit={limit}&status={status1}&status={status2}...`, seguindo o mesmo padrão de auth/erro dos métodos existentes (`CreateService`/`DeleteService`, mesmo arquivo). Teste unitário com `httptest.Server` mockando resposta 200 (lista de deploys) e resposta de erro (404/429), cobrindo parse de `status`/`createdAt`/`finishedAt`.
- [x] **T2.2** — `internal/dashboard/frontend_apps_store.go`: adicionar método `ListWithDeployService(ctx context.Context, limit int) ([]FrontendApp, error)` — query `WHERE deploy_service_id IS NOT NULL ORDER BY updated_at DESC LIMIT $1`. Teste unitário/integração seguindo o padrão já usado nos testes existentes do arquivo (se houver suíte de store tests no pacote; caso contrário, cobrir via teste do handler em T2.3).
- [x] **T2.3** — `internal/dashboard/deploy_provider_config.go`: novo handler `RecentDeploys` — guard `role == superadmin`; se `GetDeployProviderConfig()` indica não conectado, responde `{"deploys": []}`; senão chama `ListWithDeployService(ctx, 15)`, para cada app faz fan-out (goroutines + `sync.WaitGroup` + mutex, ou `errgroup` se já for dependência do módulo) chamando `render.Client.ListDeploys(ctx, app.DeployServiceID, 3, []string{"live","build_failed","update_failed","canceled"})` sob `context.WithTimeout(ctx, 5*time.Second)`; erro individual só é logado (não aborta os demais); agrega, ordena por `CreatedAt` desc, corta pros 10 primeiros, mapeia `status` (`live`→`"Live"`, demais→`"Failed"`) e `time` (relative, ex. helper `humanize`/similar já usado no repo, ou implementação simples `time.Since`). Registrar rota `GET /api/deploy-provider/recent-deploys` em `internal/server/server.go` junto das rotas existentes (`:245-247`).
- [x] **T2.4** — Teste unitário do handler/agregador com `render.Client` mockado via interface (extrair interface mínima `renderDeployLister` se o client concreto não for facilmente mockável) cobrindo os 4 cenários do design: (a) provider desconectado → lista vazia sem chamar Render; (b) 3 apps, 1 falha → resultado só com os 2 que funcionaram; (c) ordenação correta por `CreatedAt` entre apps diferentes; (d) mapeamento de status.
- [x] **T2.5** — Rodar `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l` nos arquivos tocados. Adicionar entrada em `CHANGELOG.md` `[Unreleased] > Added` descrevendo o endpoint novo.

## Fase 3 — Frontend: consumir Recent Deploys

- [x] **T3.1** — Novo hook `useRecentDeploys()` (mesmo arquivo/padrão dos demais hooks de deploy provider em `src/lib/api.ts` ou equivalente), `useQuery` contra `GET /api/deploy-provider/recent-deploys`, sem polling automático.
- [x] **T3.2** — `DeployTab` (`GitHubIntegrationPage.tsx:877-1057`): nova seção "Recent Deploys" abaixo do form Render, lista com ícone `rocket_launch`, nome do app, `StatusPill` (`tone="success"` pra `"Live"`, `tone="danger"` pra `"Failed"`), tempo relativo. Empty state (`deploys.length === 0`): mensagem `t("integrations.recentDeploysEmpty")`. `onError` do `useQuery` com `toast.error` (sonner, AGENTS §5). Chaves i18n: `integrations.recentDeploysTitle`, `integrations.recentDeploysEmpty` (en + pt-BR).
- [x] **T3.3** — Rodar `npx tsc -b`, `npm run build`, validar JSON dos locales. Adicionar entrada em `CHANGELOG.md` `[Unreleased] > Added` (Recent Deploys visível na UI).

## Fase 4 — Fechamento

- [x] **T4.1** — Atualizar `.specs/features/deploy-provider-integration/tasks.md` (se existir referência cruzada) apontando pra esta spec como extensão que adiciona visibilidade de deploy — não reabrir escopo daquela spec.
- [ ] **T4.2** — Validação end-to-end manual: com backend + DB rodando e Render conectado (sandbox ou real), confirmar visualmente as 3 tabs batendo com o handoff (screenshot real), e que Recent Deploys mostra dados reais coerentes com o console do Render.

---

## Dependências entre fases

- Fase 1 é totalmente independente — pode mergear sozinha.
- Fase 3 depende da Fase 2 (endpoint precisa existir antes do hook consumir).
- Fase 4 (T4.2) depende de Fase 2 e 3 completas.
