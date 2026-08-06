# Integrations Page — Handoff Parity Design

**Spec**: `.specs/features/integrations-handoff-parity/spec.md`

## Architecture

Duas frentes independentes, sem dependência uma da outra — podem ser feitas em qualquer ordem ou em paralelo:

1. **UI-only** (frontend, sem endpoint novo): cards "SOON", seção "Linked templates", painéis `AboutPanel`. Reusa componentes já existentes (`ProviderCard`, `AboutPanel`) do trabalho feito em Settings nesta mesma sessão.
2. **Recent Deploys** (backend novo + frontend): endpoint agregador que consulta a API do Render ao vivo, sem persistência.

## Backend — `GET /api/deploy-provider/recent-deploys`

**Handler**: novo método em `internal/dashboard/deploy_provider_config.go` (mesmo arquivo dos outros handlers de deploy provider), registrado em `internal/server/server.go` junto das rotas existentes (`:245-247`):

```
GET /api/deploy-provider/recent-deploys
```

Guard: mesma checagem `role == superadmin` já usada nas outras rotas deste arquivo.

**Fluxo**:

1. `GetDeployProviderConfig()` — se não há provider conectado, retorna `{"deploys": []}` imediatamente (sem chamar Render).
2. Query em `frontend_apps`: `WHERE deploy_service_id IS NOT NULL ORDER BY updated_at DESC LIMIT 15`. Novo método no store (`frontend_apps_store.go`), ex. `ListWithDeployService(ctx, limit int)`.
3. Para cada app (fan-out concorrente, `errgroup` com limite de concorrência ou goroutines simples + `WaitGroup`, dado N ≤ 15): chama `render.Client.ListDeploys(ctx, serviceID, limit=3, status=[live, build_failed, update_failed, canceled])` — novo método no client `internal/deploy/render/render.go`, mesmo padrão HTTP dos métodos existentes (`CreateService`, `DeleteService`).
4. Timeout global de 5s no `context` da requisição inteira (`context.WithTimeout`) — chamadas que não terminarem a tempo são abandonadas; o que já retornou é aproveitado.
5. Erro em uma chamada individual (404, 429, 5xx, timeout de conexão) é logado (`log.Printf` ou logger estruturado já usado no arquivo) e tratado como "sem deploys pra esse app" — não aborta as demais.
6. Agrega todos os `Deploy` retornados de todos os apps, junta com o nome do app (join em memória, já que veio da mesma query), ordena por `CreatedAt` desc, corta pros 10 primeiros.
7. Mapeia pro shape de resposta:
   ```json
   { "deploys": [
     { "appName": "marketing-site", "status": "Live", "time": "2h ago" },
     { "appName": "app-teste-dez", "status": "Failed", "time": "1d ago" }
   ]}
   ```
   `status`: `live` → `"Live"`; `build_failed`/`update_failed`/`canceled` → `"Failed"`. `time`: relative time formatado server-side (evita duplicar lógica de timezone no frontend) a partir de `CreatedAt`.

**Novo tipo no client Render** (`internal/deploy/render/render.go`):

```go
type Deploy struct {
    ID         string
    Status     string
    CreatedAt  time.Time
    FinishedAt *time.Time
}

func (c *Client) ListDeploys(ctx context.Context, serviceID string, limit int, statuses []string) ([]Deploy, error)
```

Segue exatamente o padrão HTTP já usado por `CreateService`/`DeleteService` (base URL, header `Authorization: Bearer`, tratamento de erro por status code) — sem introduzir dependência ou padrão novo.

## Frontend

**Novo hook**: `useRecentDeploys()` em `src/lib/api.ts` (ou arquivo de hooks de deploy provider já existente), `useQuery` simples contra o endpoint novo, sem polling automático (fetch ao montar a tab, `refetchOnWindowFocus` default do react-query já cobre "atualizar ao voltar pra aba").

**Componente**: dentro de `DeployTab` (`GitHubIntegrationPage.tsx:877`), nova seção "Recent Deploys" abaixo do form de config, renderizando a lista com ícone `rocket_launch` + nome do app + `StatusPill` (tone `success` pra Live, `danger` pra Failed) + tempo relativo. Empty state: mensagem simples tipo "No deploys yet" quando `deploys.length === 0`.

**Cards SOON**: reuso direto de `ProviderCard` com `disabled` + `badge={t("apps.soon")}` (mesma chave i18n já usada em Settings), um pra cada provider da lista do handoff (`codeHostProviders`/`deployProviders`, linhas 3430-3458 do handoff).

**Linked templates**: lista read-only reaproveitando os dados já carregados pela tab Templates (mesmo fetch, sem chamada nova) — nome + repo, sem ações.

**AboutPanel**: reuso do componente já existente em `BrandSettingsPage.tsx` — mover para um local compartilhado (`src/components/patterns/AboutPanel.tsx`) já que agora tem 2 consumidores (Settings e Integrations), em vez de duplicar a definição inline.

## i18n

Novas chaves em `en.json`/`pt-BR.json` (mesma dinâmica das rodadas anteriores desta sessão): `integrations.codeHostGitlabSoon`, `integrations.codeHostBitbucketSoon`, `integrations.deployProviderCloudflareSoon`, `...DigitaloceanSoon`, `...AwsSoon`, `...AzureSoon`, `...GcpSoon`, `integrations.linkedTemplatesTitle`, `integrations.aboutCodeHostingTitle`/`Line1`, `integrations.aboutDeployProvidersTitle`/`Line1`, `integrations.recentDeploysTitle`, `integrations.recentDeploysEmpty`.

## Error Handling

- Endpoint nunca retorna erro 5xx por causa de falha do Render — falha parcial vira lista menor, não erro. Só retorna erro se o próprio zeep-orbit falhar (DB indisponível, etc — já coberto pelo padrão de erro genérico do resto do backend, AGENTS §4: nunca `err.Error()` cru pro client).
- 403 se role errado — igual as outras rotas do arquivo.
- Frontend: `onError` do `useQuery` mostra toast (`sonner`), consistente com AGENTS §5.

## Testing

- Backend: teste unitário do agregador com um `render.Client` mockado (interface já existente, mesmo padrão de outros testes em `internal/dashboard/*_test.go`) cobrindo: (1) provider desconectado → lista vazia sem chamar Render; (2) 3 apps, 1 falha, 2 sucesso → resultado tem só os 2; (3) ordenação por `CreatedAt` desc entre apps diferentes; (4) mapeamento de status `build_failed`→`Failed`, `live`→`Live`.
- Backend: teste do novo `render.Client.ListDeploys` contra um `httptest.Server` mockando a resposta da API do Render (mesmo padrão dos testes existentes do client, se houver — senão, seguir o padrão de teste HTTP já usado no pacote `internal/deploy`).
- Frontend: `npx tsc -b` + `npm run build` limpos (AGENTS §3). Visual real (screenshot da tela rodando) precisa ser conferido manualmente pelo usuário — sem backend/DB rodando nesta sessão, não dá pra validar via browser aqui.
