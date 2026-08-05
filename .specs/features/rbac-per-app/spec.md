# RBAC Per-App Specification

## Problem Statement

zeep-orbit hoje tem só dois níveis de acesso globais (`admin`, `superadmin` em `dashboard_users.role`), sem noção de app. Isso já causa gaps documentados em 3 specs anteriores: qualquer `admin` autenticado vê e edita **todos** os backend apps de outros usuários (mitigado parcialmente por `apps.owner_id`/`app_ownership`, mas sem granularidade de role) e **todos** os frontend apps de qualquer um (sem ownership nenhum — `frontend_apps.created_by` é só um texto, não uma FK). O ROADMAP (M3 — Governance) lista "Role-based access per app (admin, editor, viewer)" como item pendente. Esta feature implementa RBAC granular por app, unificando o modelo de ownership divergente entre backend apps e frontend apps numa única tabela de membership.

## Goals

- [x] Cada app (backend ou frontend) tem membros individuais, cada um com uma role: `admin`, `editor` ou `viewer` — `app_members` (T-01).
- [x] `superadmin` global continua com acesso irrestrito a qualquer app, sem precisar de membership explícita (camada separada, não substitui o role por app) — bypass em `ResolveAppRole` (T-01).
- [x] ~~Usuário `admin` global só acessa apps onde é membro explícito~~ — **superado por decisão cross-spec** (`dashboard-global-roles` T-06, formalizada em T-01/T-12 aqui): `admin`/`auditor` global recebem acesso **viewer** (leitura) a qualquer app via `CanReadAnyApp`, não acesso zero. Decisão documentada em `dashboard-global-roles/design.md` e no `CHANGELOG.md`; escritas continuam bloqueadas por `CanWrite()`.
- [x] Modelo de ownership unificado entre backend apps e frontend apps (hoje são dois mecanismos diferentes: `owner_id`+`app_ownership` vs. nenhum) — `app_ownership` dropado (T-08), `app_members` é a única fonte de autorização para os dois eixos.
- [x] Dados existentes migram sem perda: donos/co-donos atuais de backend apps e criadores de frontend apps (quando resolvíveis) tornam-se `admin` do respectivo app — migração idempotente em `ProvisionZeepSystem` (T-02).

## Out of Scope

| Feature | Reason |
|---|---|
| Permissões granulares por ação (ex: pode editar schema mas não deletar tabela) | 3 níveis fixos (`admin`/`editor`/`viewer`) decididos no brainstorm — escopo amplo por nível, não por ação individual |
| Convite por e-mail de usuário que ainda não existe no dashboard | Usuário precisa já existir em `dashboard_users` (fluxo atual de criação por superadmin) — adicionar membro é só vincular usuário existente + role |
| SSO/SAML corporativo | Item separado do ROADMAP M3, sem relação com RBAC por app |
| Workflow de aprovação de mudança de schema | Item separado do ROADMAP M3 |
| UI dedicada de histórico de mudança de role | Cobrido pelo `audit_log` padrão já existente, sem tela nova |
| Transferência de "criador" (`owner_id`/`created_by`) para outro usuário | Esses campos passam a ser só metadado histórico ("criado por"), não fonte de autorização — não precisam de fluxo de transferência |

---

## User Stories

### P1: App admin gerencia membros do próprio app ⭐ MVP

**User Story**: Como admin de um app (backend ou frontend), quero adicionar, remover e mudar a role de outros usuários no meu app, para controlar quem tem acesso e com que nível.

**Why P1**: É a capacidade central da feature — sem gestão de membros não há RBAC, só um modelo de dados vazio.

**Acceptance Criteria**:

1. WHEN um usuário com role `admin` no app (ou `superadmin` global) chama `POST /dashboard/api/{apps|frontend-apps}/{id}/members` com `{user_id, role}` válidos THEN o sistema SHALL criar o vínculo em `app_members` e retornar 201.
2. WHEN o `user_id` informado já é membro do app THEN o sistema SHALL rejeitar com erro claro ("usuário já é membro"), sem criar duplicata — `UNIQUE (backend_app_id, user_id)` / `UNIQUE (frontend_app_id, user_id)` garante isso no banco, não só na aplicação.
3. WHEN um usuário com role `admin` no app chama `PATCH .../members/{user_id}` com `{role}` novo THEN o sistema SHALL atualizar a role, exceto quando isso deixaria o app sem nenhum `admin` (ver AC 5).
4. WHEN um usuário com role `admin` no app chama `DELETE .../members/{user_id}` THEN o sistema SHALL remover o vínculo, exceto quando isso deixaria o app sem nenhum `admin` (ver AC 5).
5. WHEN a operação de `PATCH`/`DELETE` resultaria em zero membros com role `admin` no app THEN o sistema SHALL rejeitar com 400 e mensagem clara ("app precisa de ao menos um admin"), sem aplicar a mudança.
6. WHEN um usuário com role `editor` ou `viewer` (ou não-membro, não-superadmin) chama qualquer endpoint de `/members` THEN o sistema SHALL rejeitar com 403.
7. WHEN qualquer operação de `/members` é aplicada com sucesso THEN o sistema SHALL registrar entrada em `audit_log` (ação, app, usuário afetado, role).

**Independent Test**: Usuário A (`admin` do app X) adiciona usuário B como `editor`; usuário B consegue `GET` do app mas recebe 403 em ações `admin`-only; usuário A tenta remover a si mesmo sendo o único `admin` → 400; usuário A adiciona usuário C como `admin`, depois remove a si mesmo → sucesso.

---

### P1: Sistema aplica autorização por role nas ações existentes ⭐ MVP

**User Story**: Como plataforma, quero que toda ação em um app (leitura, escrita de dados/schema, gestão) exija o nível mínimo de role correspondente, para que RBAC tenha efeito real e não seja só uma tabela sem consequência.

**Why P1**: Sem enforcement, a tabela de membros existe mas não protege nada — é a diferença entre "ter RBAC" e "ter uma tabela chamada app_members".

**Acceptance Criteria**:

1. WHEN um usuário tenta visualizar um app (detalhe, schema, data browser, logs) e sua role efetiva no app é `viewer`, `editor` ou `admin` (ou é `superadmin` global) THEN o sistema SHALL permitir.
2. WHEN um usuário tenta visualizar um app e não é membro (e não é `superadmin`) THEN o sistema SHALL retornar 403 (backend apps) ou simplesmente omitir o app da listagem/detalhe (frontend apps, mesmo padrão de "não existe" já usado pra apps arquivados).
3. WHEN um usuário com role `viewer` tenta uma ação de escrita (CRUD de dados, criar/editar/deletar tabela ou coluna) THEN o sistema SHALL retornar 403.
4. WHEN um usuário com role `editor` ou superior tenta uma ação de escrita (CRUD de dados, criar/editar/deletar tabela ou coluna) THEN o sistema SHALL permitir.
5. WHEN um usuário com role `editor` (não `admin`) tenta uma ação de gestão (config de auth/storage/rate limit, tokens, deploy config, sync credentials, arquivar/deletar app) THEN o sistema SHALL retornar 403.
6. WHEN um usuário com role `admin` no app (ou `superadmin` global) tenta uma ação de gestão THEN o sistema SHALL permitir.
7. WHEN a role efetiva do usuário precisa ser resolvida THEN o sistema SHALL usar uma função central única (não checagens duplicadas por endpoint) que retorna `admin` implícito para `superadmin` global sem consultar `app_members`, e a role de `app_members` para os demais.

**Independent Test**: Criar 1 app com 3 membros (`admin`/`editor`/`viewer`) e 1 não-membro; bater em cada endpoint da matriz de permissão (config, escrita de dados, leitura) com cada um dos 4 usuários; confirmar 200/403 conforme a matriz.

---

### P1: Migração de dados existentes sem perda de acesso ⭐ MVP

**User Story**: Como usuário que já tinha acesso a um app antes do RBAC existir, quero continuar tendo acesso administrativo ao meu app depois da migração, sem precisar que alguém me adicione manualmente.

**Why P1**: Sem isso, o deploy da feature quebra acesso de todo mundo no mesmo instante em que sobe — inaceitável pra uma migração de autorização.

**Acceptance Criteria**:

1. WHEN a migração roda THEN o sistema SHALL criar `app_members` com role `admin` para cada `(apps.owner_id, apps.id)`.
2. WHEN a migração roda THEN o sistema SHALL criar `app_members` com role `admin` para cada linha existente em `app_ownership` (co-donos, que hoje não têm granularidade — todos promovidos a `admin`).
3. WHEN a migração roda THEN o sistema SHALL, para cada `frontend_apps.created_by` que resolve para um `dashboard_users.email` existente, criar `app_members` com role `admin`.
4. WHEN `frontend_apps.created_by` não resolve para nenhum usuário existente (ex: usuário deletado) THEN o sistema SHALL deixar o app sem membro explícito — acessível só por `superadmin` até alguém ser adicionado manualmente, sem erro na migração.
5. WHEN a migração termina THEN o sistema SHALL remover a tabela `app_ownership` e todo o código que a referencia, já superada por `app_members`.
6. A migração SHALL ser idempotente (`ON CONFLICT DO NOTHING` / `IF NOT EXISTS`), seguindo o padrão já usado no restante de `provisioner.go`.

**Independent Test**: Rodar a migração num banco com apps/frontend_apps/app_ownership pré-existentes (fixture de teste); confirmar que todo dono/co-dono atual aparece em `app_members` com role `admin`; confirmar que `app_ownership` não existe mais depois.

---

## Edge Cases

- WHEN dois usuários tentam adicionar o mesmo `user_id` como membro do mesmo app simultaneamente THEN o sistema SHALL garantir que só um sucede, via `UNIQUE (app_id, user_id)` no banco — não checagem em memória.
- WHEN um usuário é removido de `dashboard_users` (deletado) THEN o sistema SHALL remover seus vínculos em `app_members` via `ON DELETE CASCADE`, sem deixar registro órfão.
- WHEN dois `admin`s do mesmo app tentam se remover/rebaixar quase simultaneamente (cada um veria "ainda tem outro admin" antes da outra operação committar) THEN o sistema SHALL prevenir a condição de corrida (checagem do invariante "≥1 admin" e a escrita SHALL ocorrer na mesma transação/lock, não em passos separados).
- WHEN um app fica sem nenhum membro `admin` (caso do created_by não resolvido) THEN `superadmin` SHALL continuar acessando normalmente e SHALL poder adicionar o primeiro membro `admin` do app.
- WHEN um usuário `admin` de um app também é `admin` de outro app diferente THEN as roles SHALL ser completamente independentes entre apps — não há role "global de admin de apps", só role global (`dashboard_users.role`) e role por app.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| RB-01 | P1: Gestão de membros | Design | Pending |
| RB-02 | P1: Gestão de membros | Design | Pending |
| RB-03 | P1: Gestão de membros | Design | Pending |
| RB-04 | P1: Gestão de membros | Design | Pending |
| RB-05 | P1: Gestão de membros | Design | Pending |
| RB-06 | P1: Gestão de membros | Design | Pending |
| RB-07 | P1: Gestão de membros | Design | Pending |
| RB-10 | P1: Enforcement | Design | Pending |
| RB-11 | P1: Enforcement | Design | Pending |
| RB-12 | P1: Enforcement | Design | Pending |
| RB-13 | P1: Enforcement | Design | Pending |
| RB-14 | P1: Enforcement | Design | Pending |
| RB-15 | P1: Enforcement | Design | Pending |
| RB-16 | P1: Enforcement | Design | Pending |
| RB-20 | P1: Migração | Design | Pending |
| RB-21 | P1: Migração | Design | Pending |
| RB-22 | P1: Migração | Design | Pending |
| RB-23 | P1: Migração | Design | Pending |
| RB-24 | P1: Migração | Design | Pending |
| RB-25 | P1: Migração | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 20 total, 0 mapped to tasks, 20 unmapped ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Nenhum usuário perde acesso administrativo ao próprio app na migração (donos/co-donos atuais de backend apps, criadores resolvíveis de frontend apps)
- [ ] `admin` global deixa de ver apps de outros usuários onde não é membro — gap de visibilidade documentado em 3 specs anteriores fechado
- [ ] Toda ação de escrita/gestão em qualquer app passa pela função central de resolução de role, sem checagem duplicada por endpoint
- [ ] Nenhum app pode ficar sem `admin` por ação de outro membro (invariante garantido, não só sugerido na UI)
- [ ] `app_ownership` removida do schema sem nenhum código remanescente que a referencie
