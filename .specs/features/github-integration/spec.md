# GitHub Integration Specification

## Problem Statement

zeep-orbit quer permitir que pessoas não-técnicas criem e façam deploy de apps frontend pelo dashboard, sem nunca tocar diretamente no GitHub da empresa. Antes de qualquer criação de app frontend ou deploy, o sistema precisa de uma forma segura e auto-service de: (1) o admin conectar a GitHub Org da empresa ao zeep-orbit, e (2) o admin cadastrar os repositórios-template que serão usados pra gerar os repos de cada app frontend. Esta é a primeira de quatro sub-features do projeto maior "Deploy self-service de frontend" (GitHub Integration → Frontend App Entity → Sync Local↔Repo → Deploy Provider Integration).

## Goals

- [ ] Admin conecta a GitHub Org via GitHub App (não OAuth pessoal, não PAT) — credenciais nunca expostas a usuários não-técnicos
- [ ] Admin cadastra múltiplos templates de repositório, cada um selecionável depois na criação de app frontend (fora de escopo aqui)
- [ ] Sistema expõe uma capacidade interna, reutilizável pelas próximas sub-features, para gerar um repo privado a partir de um template escolhido

## Out of Scope

Documentado para não virar scope creep — cada um vira spec própria depois.

| Feature | Reason |
|---|---|
| Entidade "Frontend App" no dashboard | Sub-feature seguinte (Frontend App Entity) — consome a capacidade exposta aqui |
| Sync do projeto local do usuário com o repo | Sub-feature "Sync Local↔Repo", ainda não desenhada |
| Deploy no Render/Vercel | Sub-feature "Deploy Provider Integration", ainda não desenhada |
| Webhooks do GitHub (push, PR, etc.) | Não há consumidor desses eventos ainda; reavaliar quando Deploy Provider existir |
| Repos públicos | Fora do modelo de negócio atual — repo sempre privado |
| Múltiplas orgs GitHub por instância | Uma instância self-hosted = uma empresa = uma org |
| Admin escolher escopo "All repositories" no App | Decidido: sempre "Only select repositories", API cria o repo já dentro do escopo |

---

## User Stories

### P1: Admin conecta a GitHub Org ⭐ MVP

**User Story**: Como superadmin do dashboard, quero conectar a GitHub Org da empresa via GitHub App, para que o zeep-orbit possa criar repositórios em nome da empresa sem eu precisar compartilhar credenciais pessoais.

**Why P1**: Sem conexão ativa, nenhuma outra capacidade desta feature (nem das sub-features seguintes) funciona.

**Acceptance Criteria**:

1. WHEN o superadmin acessa "Integrações → GitHub" pela primeira vez THEN o dashboard SHALL mostrar estado "não conectado" e um formulário para colar App ID, Client Secret, Private Key (PEM) e Webhook Secret.
2. WHEN o superadmin salva credenciais válidas THEN o sistema SHALL criptografar (AES-256-GCM) e persistir em `zeep_system.github_app_config`, e exibir botão "Instalar na Org".
3. WHEN o superadmin salva credenciais inválidas (App ID inexistente, private key malformada) THEN o sistema SHALL rejeitar com erro claro e não persistir nada.
4. WHEN o superadmin clica "Instalar na Org" THEN o sistema SHALL redirecionar para o fluxo de instalação nativo do GitHub (`github.com/apps/{app_slug}/installations/new`).
5. WHEN o GitHub redireciona de volta com `installation_id` THEN o sistema SHALL persistir `installation_id`, `org_login` e `installed_at`, e marcar a integração como conectada.
6. WHEN uma chamada à API do GitHub retorna 401/404 por causa de instalação revogada externamente THEN o sistema SHALL marcar `connected: false` no status endpoint, sem derrubar o dashboard.
7. WHEN qualquer ação desta feature ocorre (config atualizada, instalação concluída) THEN o sistema SHALL registrar entrada em `audit_log`.

**Independent Test**: Superadmin completa o fluxo de conexão numa org GitHub de sandbox; `GET /api/github/status` retorna `{"connected": true, "org_login": "..."}`.

---

### P1: Admin cadastra templates de repositório ⭐ MVP

**User Story**: Como superadmin, quero cadastrar um ou mais repositórios-template (ex: "Vite React", "Next.js"), para que quem for criar um app frontend (sub-feature futura) possa escolher qual stack usar.

**Why P1**: Sem templates cadastrados, a capacidade de gerar repo não tem o que gerar.

**Acceptance Criteria**:

1. WHEN o superadmin cadastra um template com `owner/repo` que não existe ou a instalação não tem acesso THEN o sistema SHALL rejeitar com erro claro.
2. WHEN o superadmin cadastra um template cujo repo existe mas não está marcado como "Template repository" no GitHub (`is_template: false`) THEN o sistema SHALL rejeitar com a mensagem "Repositório não é um template — marque como Template Repository no GitHub".
3. WHEN o superadmin cadastra um template válido THEN o sistema SHALL persistir em `zeep_system.github_templates` com `active: true` por padrão.
4. WHEN o superadmin desativa um template (`active: false`) THEN o sistema SHALL manter o registro (soft state) e parar de oferecê-lo em seleções futuras.
5. WHEN o superadmin lista templates THEN o sistema SHALL retornar nome, descrição, framework, owner/repo e status ativo.

**Independent Test**: Superadmin cadastra 2 templates via `POST /api/github/templates`; `GET /api/github/templates` retorna os 2, um deles marcado inativo após `DELETE`/toggle.

---

### P1: Sistema gera repo privado a partir de template ⭐ MVP

**User Story**: Como sistema (consumido internamente pela sub-feature "Frontend App Entity"), quero gerar um novo repositório privado na org a partir de um template escolhido, para que cada app frontend tenha seu próprio repo versionado desde a criação.

**Why P1**: É a capacidade central que justifica toda a feature — sem ela as outras três sub-features não têm onde criar código.

**Acceptance Criteria**:

1. WHEN a capacidade `CreateRepoFromTemplate(ctx, templateID, repoSlug)` é chamada com um `templateID` ativo e válido THEN o sistema SHALL criar um repositório privado na org com nome igual a `repoSlug`, usando a API de "generate repository from template" do GitHub, e retornar a URL do repo criado.
2. WHEN `repoSlug` já existe como repo na org THEN o sistema SHALL retornar erro claro sem tentar sobrescrever.
3. WHEN o `templateID` está inativo ou não existe THEN o sistema SHALL retornar erro antes de chamar a API do GitHub.
4. WHEN a instalação não tem permissão `Administration` suficiente para criar o repo THEN o sistema SHALL retornar erro específico orientando o admin a corrigir a permissão do App no GitHub e reinstalar.
5. WHEN o repo é criado com sucesso THEN o sistema SHALL registrar entrada em `audit_log` com o slug e a URL do repo.

**Independent Test**: Teste de integração chama `CreateRepoFromTemplate` contra uma org GitHub de sandbox e confirma que o repo aparece na org, é privado, e tem o conteúdo do template.

---

## Edge Cases

- WHEN a private key salva está corrompida ou a criptografia falha ao decodificar THEN o sistema SHALL tratar como "não conectado" e pedir reconfiguração, nunca vazar erro bruto de criptografia na resposta.
- WHEN o rate limit da API do GitHub é atingido durante criação de repo THEN o sistema SHALL propagar erro claro ("GitHub rate limit atingido, tente novamente em alguns minutos") sem retry silencioso indefinido.
- WHEN dois cadastros de template usam o mesmo `owner/repo` THEN o sistema SHALL permitir sem bloquear (sem constraint de unicidade) — dois templates lógicos com metadados diferentes podem apontar pro mesmo repo fonte.
- WHEN o superadmin remove as credenciais do GitHub App (desconecta) enquanto há templates cadastrados THEN o sistema SHALL manter os templates persistidos, mas todas as chamadas de geração de repo devem falhar com "GitHub não conectado" até reconfigurar.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| GH-01 | P1: Conectar Org | Design | Pending |
| GH-02 | P1: Conectar Org | Design | Pending |
| GH-03 | P1: Conectar Org | Design | Pending |
| GH-04 | P1: Conectar Org | Design | Pending |
| GH-05 | P1: Conectar Org | Design | Pending |
| GH-06 | P1: Conectar Org | Design | Pending |
| GH-07 | P1: Conectar Org | Design | Pending |
| GH-10 | P1: Templates | Design | Pending |
| GH-11 | P1: Templates | Design | Pending |
| GH-12 | P1: Templates | Design | Pending |
| GH-13 | P1: Templates | Design | Pending |
| GH-14 | P1: Templates | Design | Pending |
| GH-20 | P1: Gerar repo | Design | Pending |
| GH-21 | P1: Gerar repo | Design | Pending |
| GH-22 | P1: Gerar repo | Design | Pending |
| GH-23 | P1: Gerar repo | Design | Pending |
| GH-24 | P1: Gerar repo | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 17 total, 0 mapped to tasks, 17 unmapped ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Superadmin conecta a Org GitHub em menos de 5 minutos seguindo o fluxo do dashboard, sem precisar de suporte técnico externo
- [ ] Superadmin cadastra um template e o sistema valida corretamente se é um template repo antes de aceitar
- [ ] `CreateRepoFromTemplate` cria um repo privado funcional, confirmado por teste de integração contra GitHub real
- [ ] Nenhuma credencial (private key, client secret, webhook secret) trafega ou é exibida em texto plano fora do momento de cadastro
