# Frontend App Entity Specification

## Problem Statement

zeep-orbit hoje tem uma entidade "app" que é sempre backend (schema PostgreSQL + REST CRUD). Para o dashboard suportar deploy self-service de frontend, o sistema precisa de uma entidade nova, independente — "Frontend App" — que representa um app frontend criado por qualquer usuário autenticado, materializado como um repositório privado no GitHub (gerado a partir de um template cadastrado). Esta é a segunda de quatro sub-features do projeto "Deploy self-service de frontend" (GitHub Integration ✅ spec'd → **Frontend App Entity** → Sync Local↔Repo → Deploy Provider Integration), e consome diretamente a capacidade `CreateRepoFromTemplate` exposta pela primeira.

## Goals

- [x] Qualquer usuário autenticado do dashboard cria um frontend app (nome + template) sem precisar de acesso ao GitHub da empresa
- [x] Criação é síncrona: usuário recebe sucesso ou erro na mesma requisição, sem polling
- [x] Falha na criação não perde o registro — fica visível com erro e permite retry
- [x] Frontend app pode ser removido (soft delete) sem deixar repo órfão ativo na org GitHub

## Out of Scope

| Feature | Reason |
|---|---|
| Sync do projeto local do usuário com o repo | Sub-feature seguinte (Sync Local↔Repo) |
| Deploy no Render/Vercel | Sub-feature "Deploy Provider Integration" |
| RBAC / permissão granular por app | Depende de M3 (RBAC ainda não implementado no ROADMAP) — qualquer usuário autenticado pode criar, por ora |
| Rename de app / repo | GitHub não suporta rename trivial sem quebrar clone local existente |
| Retry automático sem ação do usuário | Usuário decide quando tentar de novo, não há job em background |
| Schema PostgreSQL / REST CRUD por frontend app | Frontend não tem dados persistidos no zeep-orbit — só repo + (futuro) deploy |

---

## User Stories

### P1: Usuário cria frontend app a partir de template ⭐ MVP

**User Story**: Como usuário autenticado do dashboard, quero criar um frontend app escolhendo um nome e um template, para que um repositório privado já configurado seja gerado pra mim sem eu precisar acessar o GitHub da empresa.

**Why P1**: É a capacidade central da sub-feature — sem ela não existe entidade "Frontend App" no dashboard.

**Acceptance Criteria**:

1. WHEN o usuário submete `{name, template_id}` com um template ativo THEN o sistema SHALL slugificar o `name`, validar unicidade do slug entre repos não-arquivados, chamar `CreateRepoFromTemplate`, e retornar o app criado com `status: ready` e a URL do repo.
2. WHEN o `template_id` não existe ou está inativo THEN o sistema SHALL rejeitar antes de qualquer chamada ao GitHub, com erro "Template inválido ou desativado".
3. WHEN o GitHub não está conectado (`github_app_config` sem instalação ativa) THEN o sistema SHALL rejeitar antes de tentar slugificar, com erro "GitHub não conectado — peça ao admin conectar em Integrações".
4. WHEN o slug gerado já existe como frontend app não-arquivado THEN o sistema SHALL rejeitar com erro "Nome já em uso — escolha outro", sem chamar o GitHub.
5. WHEN o app é criado com sucesso THEN o sistema SHALL registrar entrada em `audit_log` com slug, template usado e URL do repo.

**Independent Test**: Usuário autenticado (sem papel superadmin) cria frontend app via `POST /api/frontend-apps`; `GET /api/frontend-apps/{id}` retorna `status: ready` e `github_repo_url` preenchido; repo aparece na org GitHub, privado, com conteúdo do template.

---

### P1: Sistema preserva tentativa falha e permite retry ⭐ MVP

**User Story**: Como usuário, quero que uma falha temporária do GitHub (rate limit, etc) não me faça perder o que já preenchi, e poder tentar de novo sem recriar tudo.

**Why P1**: Chamada ao GitHub pode falhar por motivos transitórios; sem isso o usuário perde contexto e tenta adivinhar o que deu errado.

**Acceptance Criteria**:

1. WHEN `CreateRepoFromTemplate` falha por qualquer motivo (rate limit, erro de rede, 5xx do GitHub) THEN o sistema SHALL persistir o registro com `status: failed` e `error_message` preenchido, em vez de descartar a tentativa.
2. WHEN o usuário chama `POST /api/frontend-apps/{id}/retry` em um registro `failed` THEN o sistema SHALL refazer `CreateRepoFromTemplate` com o mesmo `name`/`slug`/`template_id`, atualizando `status` para `ready` (sucesso) ou mantendo `failed` com o novo erro.
3. WHEN a falha original foi "slug já existe" THEN o retry SHALL falhar novamente pelo mesmo motivo — o sistema não tenta gerar um slug alternativo automaticamente.
4. WHEN o retry é chamado em um registro que não está `failed` (já `ready` ou arquivado) THEN o sistema SHALL rejeitar com erro claro, sem reprocessar.

**Independent Test**: Forçar falha (ex: slug duplicado propositalmente), confirmar `status: failed` persistido; corrigir a causa e chamar `retry`; confirmar transição para `status: ready`.

---

### P1: Usuário lista e visualiza seus frontend apps ⭐ MVP

**User Story**: Como usuário, quero ver a lista dos frontend apps que existem (com status), para acompanhar o que foi criado e o que falhou.

**Why P1**: Sem listagem, criação e retry não têm superfície de uso no dashboard.

**Acceptance Criteria**:

1. WHEN o usuário acessa a listagem THEN o sistema SHALL retornar todos os frontend apps não-arquivados (`archived_at IS NULL`), com nome, slug, status, template, URL do repo (se `ready`) e erro (se `failed`).
2. WHEN o usuário acessa o detalhe de um frontend app THEN o sistema SHALL retornar os mesmos campos da listagem para aquele registro específico.

**Independent Test**: Criar 2 apps (1 sucesso, 1 forçado a falhar); `GET /api/frontend-apps` retorna os 2 com status correspondentes.

---

### P1: Usuário remove frontend app ⭐ MVP

**User Story**: Como usuário, quero remover um frontend app que não uso mais, para manter minha listagem limpa, sem deixar o repo GitHub ativo e esquecido na org.

**Why P1**: Sem remoção, apps de teste/erro se acumulam indefinidamente na listagem e na org GitHub.

**Acceptance Criteria**:

1. WHEN o usuário remove um frontend app THEN o sistema SHALL marcar `archived_at` (soft delete) e chamar a API do GitHub para arquivar o repo (`archived: true`).
2. WHEN o arquivamento remoto no GitHub falha (ex: já foi removido manualmente, ou API indisponível) THEN o sistema SHALL manter o soft delete local (não reverte a remoção no dashboard), registrando o erro apenas em log/audit — sem retry automático.
3. WHEN um frontend app é removido THEN o sistema SHALL registrar entrada em `audit_log`.
4. WHEN o slug de um app removido é reutilizado em uma nova criação THEN o sistema SHALL permitir (unicidade de slug vale só entre não-arquivados).

**Independent Test**: Remover um frontend app `ready`; confirmar que some da listagem (`GET /api/frontend-apps`); confirmar repo arquivado na org GitHub (`archived: true`).

---

## Edge Cases

- WHEN dois usuários tentam criar frontend apps com nomes que geram o mesmo slug simultaneamente THEN o sistema SHALL garantir que só um sucede, via unique index parcial em `slug` (constraint de banco, não checagem em memória).
- WHEN `template_id` referenciado por um frontend app é removido (soft delete `active: false`) depois de já usado THEN o sistema SHALL manter o frontend app existente intacto — a checagem de template ativo só vale na criação/retry, não retroativamente.
- WHEN o usuário chama `retry` em um app cujo template foi desativado nesse meio tempo THEN o sistema SHALL rejeitar como se fosse uma criação nova (mesma regra do AC P1-1.2).
- WHEN não há RBAC implementado (M3 pendente) THEN qualquer usuário autenticado vê a listagem completa de frontend apps de todos os usuários, não só os seus — comportamento aceito para este MVP, revisar quando RBAC existir.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| FA-01 | P1: Criar app | Execute | Verified |
| FA-02 | P1: Criar app | Execute | Verified |
| FA-03 | P1: Criar app | Execute | Verified |
| FA-04 | P1: Criar app | Execute | Verified |
| FA-05 | P1: Criar app | Execute | Verified |
| FA-10 | P1: Falha e retry | Execute | Verified |
| FA-11 | P1: Falha e retry | Execute | Verified |
| FA-12 | P1: Falha e retry | Execute | Verified |
| FA-13 | P1: Falha e retry | Execute | Verified |
| FA-20 | P1: Listagem | Execute | Verified |
| FA-21 | P1: Listagem | Execute | Verified |
| FA-30 | P1: Remoção | Execute | Verified |
| FA-31 | P1: Remoção | Execute | Verified |
| FA-32 | P1: Remoção | Execute | Verified |
| FA-33 | P1: Remoção | Execute | Verified |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 15 total, 15 mapped to tasks (T-01..T-10), 0 unmapped — todos Verified (verificado 2026-08-10 contra o código; ver `tasks.md`)

---

## Success Criteria

- [x] Usuário não-técnico cria frontend app (nome + template) em menos de 10 segundos, chamada síncrona
- [x] Falha de criação nunca perde o registro — sempre visível na listagem com erro claro
- [x] Retry funciona corretamente para erros transitórios (rate limit) e falha de forma previsível para erros permanentes (slug duplicado)
- [x] Delete arquiva o repo no GitHub, não deixa repo órfão ativo na org
- [x] Slug nunca colide com outro frontend app não-arquivado (garantido por constraint de banco, não só validação de aplicação)
