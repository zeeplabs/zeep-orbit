# GitHub Shared App Specification

## Problem Statement

Hoje, para usar a integração GitHub (`.specs/features/github-integration/`), cada empresa cliente precisa **criar seu próprio GitHub App** (na própria conta/org GitHub) e colar App ID, Client Secret, Private Key (PEM) e Webhook Secret no dashboard antes de conectar. Isso é atrito desnecessário: a Starbem já é dona do produto zeep-orbit, então pode ser dona de **um único GitHub App** (ex: "Zeep Orbit") e distribuir suas credenciais junto com o próprio deployment. Cada empresa cliente passaria só a **instalar** esse App já existente na própria org GitHub — sem criar nada do zero, sem gerar/colar private key.

**Modelo de negócio não muda**: continua self-hosted, uma instância = uma empresa = uma org GitHub conectada (`github-integration/spec.md`, Out of Scope: "Múltiplas orgs GitHub por instância"). Esta feature não introduz multi-tenancy — só move a *propriedade das credenciais do App* de "por cliente" para "do produto".

## Goals

- [ ] Credenciais do GitHub App (App ID, Private Key, Client Secret, Webhook Secret) deixam de ser inseridas pelo admin de cada empresa e passam a ser configuração do produto (shipped via env var/secret do Helm chart, mesmo padrão de outros secrets documentados no README)
- [ ] Admin de cada empresa só vê e clica em "Instalar no GitHub", apontando para o slug fixo do App da Starbem (`github.com/apps/zeep-orbit/installations/new` ou equivalente)
- [ ] `installation_id` / `org_login` continuam persistidos por instância (schema atual do `zeep_system.github_app_config` já resolve isso — sem mudança de multi-tenant)
- [ ] Compatibilidade retroativa: instância já conectada com App próprio do cliente continua funcionando sem migração forçada (ver Edge Cases)

## Out of Scope

| Feature | Reason |
|---|---|
| Multi-tenancy / múltiplas orgs por instância | Modelo de negócio segue self-hosted, 1 instância = 1 empresa — decisão reafirmada nesta sessão |
| Criar/gerenciar o GitHub App da Starbem em si (manifest, permissões, registro no GitHub) | Ação administrativa única, feita manualmente pela Starbem fora do dashboard — não é feature de código |
| Migração automática de instâncias já conectadas com App próprio para o App compartilhado | Cliente já conectado pode continuar como está; troca é opcional e manual (ver Edge Cases) |
| Rotação/gestão de secrets do App compartilhado | Segue o mesmo processo de qualquer secret de produto (fora do escopo desta feature) |

---

## User Stories

### P1: Produto distribui credenciais do GitHub App compartilhado ⭐ MVP

**User Story**: Como time Starbem, quero empacotar as credenciais do GitHub App "Zeep Orbit" (App ID, Private Key, Client Secret, Webhook Secret) como configuração do produto, para que nenhuma empresa cliente precise criar o próprio App.

**Why P1**: Sem isso, não há credencial válida para nenhuma instância usar — é pré-requisito de tudo mais.

**Acceptance Criteria**:

1. WHEN a instância sobe THEN o sistema SHALL ler App ID, Private Key, Client Secret e Webhook Secret de variáveis de ambiente/secret do Helm chart (nunca de input do usuário do dashboard).
2. WHEN as variáveis de ambiente não estão configuradas THEN o sistema SHALL expor `connected: false` com mensagem clara "GitHub App não configurado nesta instância" (erro operacional, não erro de usuário).
3. WHEN as credenciais são lidas do ambiente THEN o sistema SHALL seguir o mesmo padrão de tratamento de secret já usado no projeto (nunca logar em texto plano, nunca expor em resposta de API).
4. WHEN o README/RELEASE.md documentam variáveis de ambiente THEN as novas variáveis do GitHub App compartilhado SHALL ser adicionadas à tabela de configuração (regra já existente em `AGENTS.md` §8 — nunca inventar env var sem documentar).

**Independent Test**: Subir instância com as 4 env vars configuradas; `GET /api/github/status` não retorna erro de "credenciais ausentes".

---

### P1: Admin da empresa instala o App compartilhado ⭐ MVP

**User Story**: Como superadmin do dashboard de uma empresa cliente, quero clicar em "Instalar no GitHub" sem precisar criar/colar nenhuma credencial, para conectar minha org em poucos cliques.

**Why P1**: É a mudança de UX que motiva toda a feature — remove o formulário de credenciais do fluxo do cliente.

**Acceptance Criteria**:

1. WHEN o superadmin acessa "Integrações → GitHub" pela primeira vez e a instância tem credenciais de produto configuradas THEN o dashboard SHALL mostrar estado "não conectado" com apenas um botão "Instalar no GitHub" (sem formulário de App ID/Private Key/Client Secret/Webhook Secret).
2. WHEN o superadmin clica "Instalar no GitHub" THEN o sistema SHALL redirecionar para o fluxo nativo de instalação do App compartilhado (slug fixo do App da Starbem).
3. WHEN o GitHub redireciona de volta com `installation_id` THEN o sistema SHALL persistir `installation_id`, `org_login`, `installed_at` em `zeep_system.github_app_config` — mesma tabela/linha singleton de hoje, sem mudança de schema.
4. WHEN qualquer ação desta feature ocorre (instalação concluída, desconexão) THEN o sistema SHALL registrar entrada em `audit_log` (mesmo padrão já existente).

**Independent Test**: Superadmin completa instalação numa org GitHub de sandbox sem preencher nenhum campo de credencial; `GET /api/github/status` retorna `{"connected": true, "org_login": "..."}`.

---

## Edge Cases

- WHEN uma instância já está conectada usando um GitHub App próprio do cliente (fluxo antigo) THEN o sistema SHALL continuar funcionando sem forçar reconexão — a troca para o App compartilhado é opcional, feita manualmente pelo cliente se quiser (desconectar e reinstalar com o App novo).
- WHEN a instância não tem as env vars do App compartilhado configuradas E o cliente ainda tem credenciais próprias salvas em `zeep_system.github_app_config` (fluxo antigo) THEN o sistema SHALL priorizar as credenciais já salvas no banco, para não quebrar quem já está migrado/configurado.
- WHEN o Webhook Secret do App compartilhado precisa validar payloads de múltiplas instâncias (todas usando o mesmo App) THEN o sistema SHALL validar a assinatura HMAC normalmente — o segredo é o mesmo em todas as instâncias, isso não introduz risco adicional (mesmo modelo de App único usado por Vercel/Netlify).
- WHEN o App compartilhado é desinstalado da org do cliente diretamente pelo GitHub (fora do dashboard) THEN o comportamento SHALL seguir a regra já existente (`github-integration/spec.md` GH-06): marcar `connected: false` sem derrubar o dashboard.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| GHS-01 | P1: Distribuir credenciais do produto | Design | Pending |
| GHS-02 | P1: Distribuir credenciais do produto | Design | Pending |
| GHS-03 | P1: Distribuir credenciais do produto | Design | Pending |
| GHS-04 | P1: Distribuir credenciais do produto | Design | Pending |
| GHS-10 | P1: Admin instala App compartilhado | Design | Pending |
| GHS-11 | P1: Admin instala App compartilhado | Design | Pending |
| GHS-12 | P1: Admin instala App compartilhado | Design | Pending |
| GHS-13 | P1: Admin instala App compartilhado | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 8 total, 0 mapped to tasks, 8 unmapped ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Superadmin de empresa cliente conecta a org GitHub clicando em um único botão, sem preencher nenhuma credencial
- [ ] Instâncias já conectadas com App próprio do cliente continuam funcionando sem quebra
- [ ] Nenhuma credencial do App compartilhado (private key, client secret, webhook secret) é exposta em texto plano fora do ambiente de deployment
- [ ] README e RELEASE.md documentam as novas variáveis de ambiente do App compartilhado
