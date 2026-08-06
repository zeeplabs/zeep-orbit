# Integrations Page — Handoff Parity Specification

## Problem Statement

A tela real de Integrations (`GitHubIntegrationPage.tsx`, apesar do nome cobre as 3 tabs: Configuration/Templates/Deploy providers) diverge do handoff de redesign (`handoff/Zeep Orbit Redesign.dc.html`) em pontos visuais e em uma capacidade funcional real que ainda não existe.

Divergências visuais confirmadas (comparando código real vs handoff):
- Tab Configuration: sem seletor de outros code hosts (GitLab/Bitbucket) marcados "SOON"; sem seção "Linked templates"; sem painel lateral "About code hosting".
- Tab Deploy providers: sem seletor de outros deploy providers (Cloudflare Pages/DigitalOcean/AWS/Azure/Google Cloud) marcados "SOON"; sem painel lateral "About deploy providers"; sem lista "Recent Deploys".

Divergência funcional: a integração com Render já existe e funciona (`internal/deploy/render/render.go` chama `/owners`, `/environments`, cria/deleta service, adiciona custom domain), mas o zeep-orbit nunca consulta o histórico de deploys — não há visibilidade de "o que deployou recentemente e o status" em lugar nenhum do dashboard, apesar da API do Render expor isso (`GET /services/{serviceId}/deploys`, confirmado na doc oficial: suporta `status`, `createdAfter`/`finishedAfter`, `limit`).

Esta spec é continuação direta de `deploy-provider-integration` (que já cobre conectar provider e criar service) e `github-integration`/`github-shared-app` (que cobrem a tab Configuration) — aqui o foco é fechar o gap visual com o handoff nas 3 tabs e adicionar a capacidade real de "Recent Deploys".

## Goals

- [ ] Tab Configuration mostra GitLab e Bitbucket como cards desabilitados com badge "SOON" (mesmo padrão `ProviderCard disabled+badge` já usado em Settings)
- [ ] Tab Configuration mostra seção "Linked templates" (lista read-only dos templates GitHub ativos)
- [ ] Tab Configuration ganha painel lateral fixo "About code hosting" (componente `AboutPanel` já existente, reuso de Settings)
- [ ] Tab Deploy providers mostra Cloudflare Pages, DigitalOcean, AWS, Azure e Google Cloud como cards desabilitados "SOON"
- [ ] Tab Deploy providers ganha painel lateral fixo "About deploy providers"
- [ ] Tab Deploy providers mostra "Recent Deploys" com dados **reais**, buscados ao vivo via API do Render (sem persistência nova, sem job de polling)
- [ ] Campo "Environment ID" do form Render é mantido (não existe no handoff, mas é funcionalmente necessário — resolve o Environment dentro do Project Render; divergência intencional, mesmo padrão do "Redirect URL" em Auth provider)

## Out of Scope

| Feature | Reason |
|---|---|
| GitLab/Bitbucket/Cloudflare/DigitalOcean/AWS/Azure/GCP implementados de fato | Só os cards "SOON" (UI), sem provider real — YAGNI, mesmo racional do `deploy-provider-integration` original |
| Tabela de histórico de deploy (`deploy_events` ou similar) | "Recent Deploys" é fetch ao vivo na API do Render a cada carregamento da página — sem persistência nova nesta fase |
| Webhook de deploy/build status do Render | Já fora de escopo em `deploy-provider-integration/spec.md`; continua fora aqui |
| Paginação/histórico além dos 10 deploys mais recentes agregados | Widget de visibilidade rápida, não um log completo — usuário usa o console do Render pra histórico completo |
| Retry ou ação (redeploy/rollback) a partir da lista "Recent Deploys" | Lista é só leitura; ações de deploy continuam via fluxos já existentes (retry de `frontend_app`) |
| "Recent Deploys" para outros providers além do Render | Só Render está implementado hoje; interface pode ser generalizada depois quando houver 2º provider real |

---

## User Stories

### P1: Superadmin vê deploys recentes reais na tab Deploy providers ⭐ MVP

**User Story**: Como superadmin, quero ver os deploys mais recentes (app, status, quando) direto na tab Deploy providers, para não precisar abrir o console do Render pra saber se um deploy passou ou falhou.

**Why P1**: É a única lacuna funcional real desta spec — o resto é paridade visual.

**Acceptance Criteria**:

1. WHEN a tab Deploy providers carrega THEN o sistema SHALL chamar `GET /api/deploy-provider/recent-deploys` e renderizar até 10 itens, cada um com nome do frontend app, status (`Live` ou `Failed`) e tempo relativo (ex. "2h ago").
2. WHEN o backend processa a requisição THEN o sistema SHALL selecionar até 15 frontend apps com `deploy_service_id` preenchido (ordenados por `updated_at DESC`), chamar `GET /services/{id}/deploys?limit=3&status=live,build_failed,update_failed,canceled` da API do Render para cada um, agregar todos os deploys retornados, ordenar por `createdAt` desc e retornar os 10 mais recentes.
3. WHEN o status retornado pelo Render é `live` THEN o sistema SHALL mapear para `Live`; WHEN é `build_failed`, `update_failed` ou `canceled` THEN SHALL mapear para `Failed`.
4. WHEN nenhum provider está conectado OU nenhum frontend app tem `deploy_service_id` THEN o sistema SHALL retornar lista vazia (sem erro), e o frontend SHALL mostrar um empty state.
5. WHEN a chamada Render falha para um app específico (timeout, 4xx, 5xx) THEN o sistema SHALL logar o erro server-side e seguir agregando os demais apps, sem falhar a requisição inteira.
6. WHEN o tempo total de agregação excede um timeout global (5s) THEN o sistema SHALL retornar o que já foi agregado até ali, sem bloquear a resposta indefinidamente.
7. WHEN um usuário sem role `superadmin` chama o endpoint THEN o sistema SHALL responder 403, mesma guard das demais rotas de `/api/deploy-provider/*`.

**Independent Test**: Com Render conectado e ao menos 1 frontend app com `deploy_service_id` que teve deploy recente no Render (sandbox), `GET /api/deploy-provider/recent-deploys` retorna ao menos 1 item com `status` em `Live`/`Failed` e `time` coerente com o deploy real feito no console do Render.

---

### P2: Tabs Configuration e Deploy providers batem visualmente com o handoff

**User Story**: Como superadmin, quero ver os mesmos elementos visuais do handoff (providers "SOON", linked templates, painéis "About"), para a tela ficar consistente com o resto do dashboard redesenhado.

**Why P2**: Puramente de apresentação — não desbloqueia nenhuma capacidade nova, mas fecha o gap de paridade visual identificado.

**Acceptance Criteria**:

1. WHEN a tab Configuration renderiza THEN o sistema SHALL mostrar, além do GitHub, cards desabilitados "GitLab" e "Bitbucket" com badge "SOON".
2. WHEN a tab Configuration renderiza THEN o sistema SHALL mostrar uma seção "Linked templates" listando os templates GitHub ativos (nome + repo), read-only.
3. WHEN a tab Configuration renderiza THEN o sistema SHALL mostrar um painel lateral fixo "About code hosting" via componente `AboutPanel`.
4. WHEN a tab Deploy providers renderiza THEN o sistema SHALL mostrar, além do Render, cards desabilitados "Cloudflare Pages", "DigitalOcean", "AWS", "Azure" e "Google Cloud" com badge "SOON".
5. WHEN a tab Deploy providers renderiza THEN o sistema SHALL mostrar um painel lateral fixo "About deploy providers" via componente `AboutPanel`.

**Independent Test**: Rodar `npm run build`; inspecionar visualmente (screenshot real da tela rodando) as 2 tabs e confirmar presença de cada elemento acima.

---

## Edge Cases

- WHEN um frontend app tem `deploy_service_id` mas foi removido do Render externamente (fora do zeep-orbit) THEN a chamada Render pra aquele service SHALL falhar (404) e o sistema SHALL apenas pular esse app na agregação, sem expor erro ao usuário.
- WHEN há mais de 15 frontend apps com `deploy_service_id` THEN o sistema SHALL considerar só os 15 mais recentes por `updated_at` — apps mais antigos não aparecem mesmo que tenham deploy mais recente que os incluídos (trade-off aceito pra limitar chamadas Render por request).
- WHEN dois deploys do mesmo app aparecem entre os agregados (limit=3 por service) THEN a lista final SHALL mostrar cada deploy como item separado — sem dedupe por app, é uma lista de eventos, não de apps.
- WHEN o Render está com rate limit ativo (429) THEN o sistema SHALL tratar como falha daquele app específico (mesmo comportamento do edge case de 404), sem retry automático.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| IHP-01 | P1: Recent Deploys reais | Design | Pending |
| IHP-02 | P1: Recent Deploys reais | Design | Pending |
| IHP-03 | P1: Recent Deploys reais | Design | Pending |
| IHP-04 | P1: Recent Deploys reais | Design | Pending |
| IHP-05 | P1: Recent Deploys reais | Design | Pending |
| IHP-06 | P1: Recent Deploys reais | Design | Pending |
| IHP-07 | P1: Recent Deploys reais | Design | Pending |
| IHP-10 | P2: Paridade visual Configuration | Design | Pending |
| IHP-11 | P2: Paridade visual Configuration | Design | Pending |
| IHP-12 | P2: Paridade visual Configuration | Design | Pending |
| IHP-13 | P2: Paridade visual Deploy providers | Design | Pending |
| IHP-14 | P2: Paridade visual Deploy providers | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 12 total, 0 mapped to tasks, 12 unmapped ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Superadmin abre Deploy providers e vê, em até 3s, deploys reais dos últimos frontend apps deployados, sem abrir o console do Render
- [ ] Falha da API do Render para 1 app não impede visualização dos deploys dos demais apps
- [ ] Nenhuma chave/API key do Render trafega para o client em nenhum momento (mesma garantia já existente hoje)
- [ ] Tabs Configuration e Deploy providers batem visualmente com o handoff nos elementos listados nos Goals (screenshot real confirma)
