# Deploy Provider Integration Specification

## Problem Statement

zeep-orbit já resolve criação de repo (GitHub Integration), entidade de frontend app (Frontend App Entity) e sincronização do código local do usuário com o repo (Sync Local↔Repo). Falta a última peça: fazer o código chegar de fato ao ar. Esta é a quarta e última sub-feature do projeto "Deploy self-service de frontend" (GitHub Integration ✅ → Frontend App Entity ✅ → Sync Local↔Repo ✅ → **Deploy Provider Integration**). Nenhuma das três anteriores está implementada ainda — todas em fase de spec.

O sistema precisa: (1) permitir que o superadmin conecte um provider de deploy (Render primeiro), (2) permitir que o superadmin configure, por template de repositório, como esse template deve ser deployado, e (3) criar automaticamente um service no provider quando um admin (usuário final) cria um frontend app, para que o deploy aconteça sem nenhuma ação manual no console do provider.

## Terminologia (dashboard zeep-orbit)

- **Superadmin**: configura a plataforma (GitHub Org, provider de deploy, templates). Não é usuário final.
- **Admin**: usuário final do dashboard, hoje só gerencia as próprias apps (backend e frontend). Não configura integrações de plataforma. Isso muda quando RBAC (M3 do ROADMAP) existir — por ora é assim.

## Goals

- [x] Superadmin conecta um provider de deploy (Render) via API Key, sem expor a chave a admins
- [x] Superadmin configura, por template de repositório já cadastrado (GitHub Integration), o tipo de service e comandos de build/start do provider
- [x] Ao admin criar um frontend app, o sistema cria automaticamente o service no provider conectado, na mesma chamada síncrona já usada pelas sub-features anteriores
- [x] Push subsequente do admin no repo (via Sync Local↔Repo) dispara auto-deploy no provider sem qualquer chamada do zeep-orbit
- [x] Admin pode, opcionalmente, linkar o frontend app a um backend app já existente, e o sistema injeta a URL da API e um App Token como env vars no service criado
- [x] Arquitetura de provider é uma interface (`DeployProvider`), não acoplada a detalhes do Render — Vercel e outros entram depois implementando a mesma interface

## Out of Scope

| Feature | Reason |
|---|---|
| Vercel ou outros providers implementados | Só a interface é desenhada agora; implementação fica pra quando houver 2º caso real (YAGNI) |
| zeep-orbit recebendo webhook de deploy/build status | Render expõe status via API e no próprio dashboard dele; zeep-orbit não precisa espelhar em tempo real nesta fase |
| RBAC granular (quem pode conectar provider, quem pode linkar backend app) | Depende de M3 do ROADMAP, ainda não implementado — por ora, ações de plataforma são superadmin-only, ações de app são qualquer admin autenticado |
| Múltiplos providers conectados simultaneamente | Um provider ativo por instância nesta fase — trocar de provider é reconfigurar, não coexistir |
| Rollback de deploy pelo zeep-orbit | Fora do dashboard nesta fase — usuário usa o console do Render se precisar |
| Custom domains gerenciados pelo zeep-orbit | Fora de escopo — configuração de domínio fica no console do provider |

---

## Pré-requisito operacional crítico

Render precisa da **própria GitHub App** (não a do zeep-orbit) instalada na org, com escopo **"All repositories"**, para enxergar repositórios privados criados dinamicamente e disparar auto-deploy nativo em cada push. Escopo "Only select repositories" exigiria o superadmin re-autorizar manualmente a cada frontend app novo, quebrando o self-service. Isso é documentado como pré-requisito de setup, não resolvido em código — o sistema apenas valida e reporta erro claro se a criação do service falhar por "repo not found".

---

## User Stories

### P1: Superadmin conecta provider de deploy ⭐ MVP

**User Story**: Como superadmin, quero conectar o Render via API Key, para que o zeep-orbit possa criar services de deploy em nome da empresa.

**Why P1**: Sem provider conectado, nenhuma criação de service funciona.

**Acceptance Criteria**:

1. WHEN o superadmin submete uma API Key do Render THEN o sistema SHALL validar contra a API do Render (ex.: `GET /v1/owners`) antes de persistir, criptografar (AES-256-GCM) e salvar em `zeep_system.deploy_provider_config`.
2. WHEN a API Key é inválida THEN o sistema SHALL rejeitar com erro claro e não persistir nada.
3. WHEN já existe um provider conectado e o superadmin salva uma nova API Key THEN o sistema SHALL sobrescrever a configuração existente (singleton, mesmo padrão de `github_app_config`).
4. WHEN qualquer alteração de configuração de provider ocorre THEN o sistema SHALL registrar entrada em `audit_log`.

**Independent Test**: Superadmin conecta uma API Key válida de sandbox Render; `GET /api/deploy-provider/status` retorna `{"connected": true, "provider": "render"}`.

---

### P1: Superadmin configura deploy por template ⭐ MVP

**User Story**: Como superadmin, quero definir, para cada template de repositório já cadastrado (GitHub Integration), como ele deve ser deployado no provider, para que cada stack (Vite React, Next.js) seja criada com as configurações corretas de build.

**Why P1**: Sem essa config, o sistema não sabe se deve criar um static site ou um web service, nem quais comandos rodar.

**Acceptance Criteria**:

1. WHEN o superadmin edita um template e define `render_service_type` (`static_site` ou `web_service`) THEN o sistema SHALL exigir `build_command` e, se `static_site`, `publish_path`, ou se `web_service`, `start_command`.
2. WHEN um template não tem configuração de deploy definida THEN o sistema SHALL impedir a criação de frontend apps a partir dele com erro "Template sem configuração de deploy — peça ao superadmin completar em Integrações → GitHub → Templates".
3. WHEN o superadmin atualiza a configuração de deploy de um template já usado por frontend apps existentes THEN o sistema SHALL aplicar a mudança apenas a criações futuras — não re-configura services já criados.

**Independent Test**: Superadmin configura um template como `static_site` com `build_command: "npm run build"` e `publish_path: "dist"`; `GET /api/github/templates/{id}` retorna os campos de deploy preenchidos.

---

### P1: Sistema cria service de deploy ao criar frontend app ⭐ MVP

**User Story**: Como admin, quero que, ao criar meu frontend app, o deploy já esteja configurado, para que eu só precise dar push no código pra ver ele no ar.

**Why P1**: É a capacidade central da sub-feature — conecta a entidade já criada (Frontend App Entity) ao provider.

**Acceptance Criteria**:

1. WHEN um frontend app é criado com sucesso (repo + deploy key já resolvidos pelas sub-features anteriores) THEN o sistema SHALL chamar o provider conectado pra criar um service apontando pro repo recém-criado, usando a config de deploy do template escolhido.
2. WHEN o admin informa um `backend_app_id` opcional na criação THEN o sistema SHALL injetar `API_URL` (URL pública do backend app) e `APP_TOKEN` (token gerado via feature de App Tokens) como env vars do service criado.
3. WHEN nenhum `backend_app_id` é informado THEN o sistema SHALL criar o service sem env vars de backend.
4. WHEN o service é criado com sucesso THEN o sistema SHALL persistir `deploy_service_id`, `deploy_url` e `deploy_status: ready` no frontend app, e registrar `audit_log`.
5. WHEN nenhum provider está conectado THEN o sistema SHALL ainda assim criar o frontend app (repo + sync), mas marcar `deploy_status: failed` com mensagem "Provider de deploy não conectado — peça ao superadmin conectar em Integrações".
6. WHEN a criação do service falha por qualquer motivo (rate limit, repo não encontrado, template sem config) THEN o sistema SHALL persistir `deploy_status: failed` com `deploy_error_message`, sem desfazer repo ou deploy key já criados.

**Independent Test**: Admin cria frontend app linkado a um backend app existente; `GET /api/frontend-apps/{id}` retorna `deploy_status: ready`, `deploy_url` preenchido; service aparece no dashboard do Render com env vars `API_URL`/`APP_TOKEN` setadas.

---

### P1: Admin tenta novamente deploy que falhou ⭐ MVP

**User Story**: Como admin, quero tentar de novo a criação do service de deploy sem recriar o frontend app inteiro, se a primeira tentativa falhou.

**Why P1**: Falha de deploy não pode obrigar o usuário a recriar repo e sync do zero.

**Acceptance Criteria**:

1. WHEN o admin chama `POST /api/frontend-apps/{id}/deploy/retry` em um registro com `deploy_status: failed` THEN o sistema SHALL tentar novamente a criação do service, atualizando `deploy_status` para `ready` ou mantendo `failed` com o novo erro.
2. WHEN o retry é chamado em um registro que já está `ready` THEN o sistema SHALL rejeitar com erro claro, sem recriar o service.

**Independent Test**: Forçar falha (provider desconectado), corrigir (conectar provider), chamar retry; confirmar transição para `deploy_status: ready`.

---

### P1: Sistema remove service ao remover frontend app ⭐ MVP

**User Story**: Como admin, quero que remover meu frontend app também derrube o deploy, para não pagar/manter um service órfão no provider.

**Why P1**: Sem isso, services acumulam no provider sem controle do zeep-orbit.

**Acceptance Criteria**:

1. WHEN um frontend app com `deploy_status: ready` é removido THEN o sistema SHALL chamar o provider pra deletar o service, best-effort (não bloqueia o soft delete se falhar).
2. WHEN a remoção do service no provider falha THEN o sistema SHALL manter o soft delete local, registrando o erro apenas em log/audit — sem retry automático (mesmo padrão já aceito pro archive de repo na Frontend App Entity).

**Independent Test**: Remover frontend app `ready`; confirmar que service some do provider (ou, se falhar, que o frontend app é removido do dashboard mesmo assim).

---

## Edge Cases

- WHEN o template usado por um frontend app é reconfigurado (deploy config alterada) depois que o service já foi criado THEN o sistema SHALL manter o service existente intacto — mudança vale só pra criações novas.
- WHEN o backend app linkado é removido depois que o frontend app já foi criado e deployado THEN o sistema SHALL manter o frontend app e o service intactos — env vars já injetadas não são revogadas retroativamente (fora de escopo; usuário reconfigura manualmente no Render se precisar).
- WHEN dois frontend apps são criados simultaneamente apontando pro mesmo template THEN cada um SHALL gerar um service independente — sem compartilhamento de config em runtime.
- WHEN a API Key do provider é rotacionada/invalidada externamente (revogada no console do Render) THEN chamadas subsequentes SHALL falhar com erro claro, e `GET /api/deploy-provider/status` SHALL refletir `connected: false` na próxima checagem.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| DP-01 | P1: Conectar provider | Execute | Verified |
| DP-02 | P1: Conectar provider | Execute | Verified |
| DP-03 | P1: Conectar provider | Execute | Verified |
| DP-04 | P1: Conectar provider | Execute | Verified |
| DP-10 | P1: Config por template | Execute | Verified |
| DP-11 | P1: Config por template | Execute | Verified |
| DP-12 | P1: Config por template | Execute | Verified |
| DP-20 | P1: Criação de service | Execute | Verified |
| DP-21 | P1: Criação de service | Execute | Verified |
| DP-22 | P1: Criação de service | Execute | Verified |
| DP-23 | P1: Criação de service | Execute | Verified |
| DP-24 | P1: Criação de service | Execute | Verified |
| DP-25 | P1: Criação de service | Execute | Verified |
| DP-30 | P1: Retry | Execute | Verified |
| DP-31 | P1: Retry | Execute | Verified |
| DP-40 | P1: Delete | Execute | Verified |
| DP-41 | P1: Delete | Execute | Verified |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 17 total, 17 mapped to tasks (T-01..T-15), 0 unmapped — todos Verified (verificado 2026-08-10 contra o código; ver `tasks.md`)

---

## Success Criteria

- [x] Superadmin conecta o Render em menos de 5 minutos, sem suporte técnico externo
- [x] Admin cria um frontend app linkado a um backend app e recebe uma URL de deploy funcional sem tocar no console do Render
- [x] Push subsequente no repo aparece automaticamente como novo deploy no Render, sem qualquer chamada do zeep-orbit
- [x] Falha em qualquer etapa da criação do service não derruba repo, deploy key ou o frontend app já criados
- [x] Nenhuma credencial do provider trafega ou é exibida em texto plano fora do momento de cadastro
