# End-User Roles Configuration Specification

## Problem Statement

O `role` de negócio do end-user (claim JWT usado por `_auth_users.role` e por `table_policies.roles` no enforcement de RLS — feature `end-user-row-policies`, D-137/D-138) é hoje texto livre em dois pontos do dashboard: `RoleCell` (`AppUsersPage.tsx:32-70`) e o campo CSV de roles em `TablePolicies.tsx:69,98-101,169-174`. Backend só valida formato (`identRe: ^[a-z][a-z0-9_]{0,62}$`), sem whitelist de conteúdo. Um erro de digitação (`admni` em vez de `admin`) não gera erro visível — a policy simplesmente nunca casa (`current_setting('app.jwt_role') = ANY(roles)`), e o usuário fica silenciosamente sem acesso a nenhuma linha. Não existe hoje nenhum registro de "quais roles esse app usa" — cada admin decora de memória.

## Goals

- [ ] Admin do app cadastra, por app, a lista de roles de negócio válidas (`enduser_roles_config`, array de strings), persistida em `zeep_system.apps`.
- [ ] `RoleCell` (edição de role do end-user) passa de `Input` livre para `Select` populado por essa lista.
- [ ] Campo de roles em `TablePolicies` passa de `Input` CSV livre para multi-select (chips) populado pela mesma lista.
- [ ] Apps novos e apps existentes (via migração) nunca ficam com lista vazia — seed automático `["member"]`, mesmo default hoje de `_auth_users.role`.
- [ ] Remoção de uma role da lista é bloqueada pelo backend se ela estiver em uso (por ao menos um `_auth_users.role` ou por ao menos uma `table_policies.roles`).

## Out of Scope

Explicitamente excluído desta spec. Documentado pra prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Hierarquia/herança de roles (`admin` implica `member`) | `role` continua string plana, sem árvore — decisão já registrada na spec de `end-user-row-policies` e mantida aqui. |
| Migração/correção automática de policies ou end-users com role fora da lista atual | Esta feature valida daqui pra frente; dados já existentes com role "órfã" (fora da lista configurada) continuam funcionando como estão, só não aparecem mais como opção pra novas atribuições — ver AC de "role órfã" abaixo. |
| Lista de roles por tabela (em vez de por app) | `role` já é um conceito por-app no JWT/claim atual (`_auth_users.role` é uma coluna por app, não por tabela) — manter o mesmo nível. |
| Limite máximo de quantidade de roles por app | Sem caso motivador concreto para um teto artificial; lista pequena esperada na prática (dezenas no máximo). |
| Gestão de roles de **dashboard/plataforma** (`superadmin`/`admin`/`auditor`/`member` em `platform_roles.go`) | Conceito diferente e já resolvido (enum fixo, `ResolveAppRole`) — esta spec trata só do business role do end-user do app. |
| Admin criar um end-user diretamente pela tela "Usuários do App" (sem passar por `/auth/register`) | Feature seguinte, fora do escopo desta spec. O botão de ação "Editar" (P2) introduz o padrão de drawer de ações por linha que essa próxima feature vai reaproveitar — mencionado aqui só pra registrar a intenção, sem requisito algum definido. |

---

## Assumptions & Open Questions

Toda ambiguidade está resolvida ou registrada aqui — nada fica silenciosamente indefinido.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Fallback quando a lista de roles está vazia | Seed automático `["member"]` em apps novos e via migração para apps existentes — a lista nunca fica vazia | Confirmado pelo usuário em 2026-08-08; consistente com o default atual de `_auth_users.role DEFAULT 'member'`, evita um segundo modo de UI (Select vs Input livre coexistindo) | y — confirmado pelo usuário em 2026-08-08 |
| Remoção de role em uso | Bloquear no backend (erro explícito) se `_auth_users.role` ou `table_policies.roles` referenciar a role sendo removida | Confirmado pelo usuário em 2026-08-08; evita "role órfã" invisível e policy que silenciosamente nunca casa | y — confirmado pelo usuário em 2026-08-08 |
| Local no dashboard onde a lista é gerenciada | Nova seção na tela de Settings do app, ao lado das seções existentes de `auth_providers`/`storage_config`/`rate_limit_config` | Confirmado pelo usuário em 2026-08-08; mesmo padrão de config por-app já estabelecido, evita fragmentar gestão de config em múltiplas telas | y — confirmado pelo usuário em 2026-08-08 |
| Seção de gestão de roles visível apenas quando `auth_email_enabled = true` | Sim — oculta/desabilitada quando `auth_email_enabled = false` | Role de negócio de end-user só existe quando há autenticação de end-user; mesmo padrão de gate usado no fix `ROWPOL-25` (`authEmailEnabled && col.type === "uuid"`) para a FK de `_auth_users` | y — decisão de design, sem ambiguidade de produto a validar com o usuário |
| Validação de formato de cada role individual | Reaproveita a regex já existente `identRe: ^[a-z][a-z0-9_]{0,62}$`, sem mudança de regra | Regra já validada em produção (`handler.go:2421`, `policy.go:140`); introduzir uma segunda regra de formato só para a lista configurável criaria inconsistência com o texto livre que continua chegando via API direta | y — decisão técnica, sem ambiguidade de produto |
| Comportamento de uma role atualmente atribuída a um end-user ou usada numa policy, mas que não está (ou não está mais) na lista `enduser_roles_config` ("role órfã") | O valor persistido continua funcionando como está (claim JWT, enforcement de RLS inalterados); a UI (`RoleCell`, multi-select de `TablePolicies`) exibe essa role como opção adicional presente/selecionada mesmo fora da lista, sem removê-la ou forçar troca | Impedir "sumir" um valor real do banco só porque saiu da lista de opções seria destrutivo e fora do escopo (esta feature é sobre prevenir erro de digitação em atribuições *novas*, não sobre normalizar dados existentes) | y — decisão técnica derivada diretamente da decisão de remoção bloqueada acima |
| Endpoint de leitura da lista de roles é exposto também na API pública do app (fora do dashboard) | Não — só dashboard (`/dashboard/api/apps/{id}/roles`), mesmo perímetro de auth dos demais endpoints de config de app | Nenhum caso motivador de app externo precisar ler a lista; API REST de end-user (`/{app}/...`) não referencia a lista, só o claim `role` já presente no JWT | y — sem ambiguidade de produto a validar, decorre do próprio escopo (config administrativa) |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Admin cadastra a lista de roles válidas do app ⭐ MVP

**User Story**: Como admin de um app, eu quero cadastrar a lista de roles de negócio válidas do meu app, para que eu (e qualquer outro admin) sempre atribua/referencie roles através de uma lista fixa, sem depender de digitar o valor certo de memória.

**Why P1**: É a base de dados que as outras duas stories (P2, P3) consomem — sem a lista persistida, não há o que popular nos Selects.

**Acceptance Criteria**:

1. WHEN admin abre a seção de roles na tela de Settings do app THEN o sistema SHALL exibir a lista atual de `enduser_roles_config` (mínimo `["member"]`, nunca vazia).
2. WHEN admin submete uma nova role via `PUT /dashboard/api/apps/{id}/roles` THEN o sistema SHALL validá-la contra `identRe` e persistir a lista atualizada em `zeep_system.apps.enduser_roles_config`.
3. IF a role submetida já existe na lista (comparação exata, case-sensitive) THEN o sistema SHALL retornar erro `role already exists` sem duplicar a entrada.
4. IF a role submetida não casa com `identRe` THEN o sistema SHALL retornar o mesmo erro de formato já usado hoje em `UpdateAppUserRole` (`role must match ^[a-z][a-z0-9_]{0,62}$`).
5. IF admin solicita remoção de uma role que está presente em ao menos um `_auth_users.role` do schema do app OU em ao menos uma `table_policies.roles` do app THEN o sistema SHALL bloquear a remoção e retornar erro indicando quantos end-users e/ou quantas policies referenciam a role.
6. WHEN admin remove uma role sem nenhum uso (nem end-user, nem policy) THEN o sistema SHALL persistir a lista sem essa entrada.
7. The system SHALL seedar `enduser_roles_config = ["member"]` para todo app já existente na migração que introduz a coluna, e para todo app novo criado a partir dela.
8. WHERE `auth_email_enabled = false` no app THEN a seção de gestão de roles SHALL ficar oculta na tela de Settings (mesmo padrão de gate de `authEmailEnabled` já usado no dropdown de FK pra `_auth_users`).

**Independent Test**: Criar/abrir um app com `auth_email_enabled = true`, abrir Settings, ver `["member"]` pré-populado, adicionar `"viewer"`, remover `"member"` (deve bloquear se algum end-user ainda tiver essa role — testável isoladamente sem tocar em RoleCell/TablePolicies).

---

### P2: Edição de role sai da coluna da tabela e vai para um drawer de ações

**User Story**: Como admin, eu quero que a coluna "role" da tabela de usuários seja só leitura (texto descritivo) e que a edição aconteça por um botão "Editar" nas Ações, abrindo um drawer com `Select`, para que eu nunca digite um valor com erro de digitação e para que a tela já siga o padrão de drawer de ações que a próxima feature (criar usuário do app pela tela) vai reaproveitar.

**Why P2**: Consome diretamente a lista da P1; é o primeiro dos dois pontos de UI que hoje são texto livre, e decide o padrão de interação (drawer de ações por linha) usado pela feature seguinte.

**Acceptance Criteria**:

1. WHEN a tabela de usuários do app é renderizada THEN a coluna "role" SHALL exibir a role atual como texto simples, somente leitura — sem `Select` nem `Input` inline na célula.
2. WHEN a tabela de usuários do app é renderizada THEN a coluna de Ações SHALL exibir um botão "Editar" por linha.
3. WHEN admin clica em "Editar" THEN o sistema SHALL abrir um drawer com um `Select` de role, populado pelas opções de `enduser_roles_config` do app, pré-selecionado com a role atual do usuário.
4. IF a role atualmente persistida do end-user não está na lista `enduser_roles_config` (role órfã) THEN o `Select` do drawer SHALL exibir essa role como a opção selecionada na abertura, mesmo estando fora da lista configurada, sem forçar troca automática para outro valor.
5. WHEN admin seleciona uma nova role no `Select` do drawer e confirma THEN o sistema SHALL chamar `useUpdateAppUserRole` exatamente como hoje (endpoint/contrato inalterados — só a UI de escolha e o local de edição mudam) e fechar o drawer.
6. WHEN admin fecha o drawer sem confirmar (cancelar/clicar fora) THEN o sistema SHALL descartar a seleção sem chamar `useUpdateAppUserRole`.

**Independent Test**: Com `enduser_roles_config = ["member", "viewer"]`, abrir "Usuários do App", confirmar que a coluna role não é editável diretamente, clicar em "Editar" de uma linha, trocar `member` para `viewer` no drawer, confirmar, e ver a tabela refletir o novo valor sem ter digitado nada.

---

### P3: TablePolicies usa multi-select em vez de Input CSV livre

**User Story**: Como admin, eu quero selecionar as roles de uma table policy numa lista fechada (multi-select), para que a policy nunca fique associada a uma role com erro de digitação que nunca vai casar no `USING` clause.

**Why P3**: Segundo e último ponto de texto livre a fechar; mesmo racional de P2, aplicado ao formulário de policy.

**Acceptance Criteria**:

1. WHEN admin cria ou edita uma table policy na tela de RLS THEN o sistema SHALL exibir o campo "roles" como multi-select (chips) populado por `enduser_roles_config`, substituindo o `Input` CSV livre atual.
2. IF a policy já persistida referencia uma role fora da lista atual (role órfã) THEN o multi-select SHALL exibir essa role como chip selecionado, sem removê-la automaticamente.
3. The system SHALL continuar validando cada role selecionada contra `identRe` no backend (`BuildPolicySQL`) como camada de defesa, independente da restrição já imposta pela UI.

**Independent Test**: Com `enduser_roles_config = ["member", "admin"]`, criar uma policy selecionando `["admin"]` via chips (sem digitar texto), salvar e confirmar que `table_policies.roles` persiste `["admin"]`.

---

## Edge Cases

- IF `enduser_roles_config` ainda não existe pra um app criado antes desta feature (coluna recém-adicionada) THEN a migração SHALL preencher `["member"]` antes de qualquer leitura pela UI — nunca undefined/null.
- IF dois admins do mesmo app tentam adicionar/remover roles concorrentemente THEN o sistema SHALL aplicar a última escrita vencendo (mesma semântica de concorrência já usada nas demais colunas JSONB de `apps` — sem lock otimista introduzido por esta feature, fora do escopo motivador).
- IF a lista de roles cresce a um tamanho grande (sem limite definido) THEN o `Select`/multi-select SHALL permanecer funcional (scroll), sem paginação especial.
- IF admin tenta remover a última role restante da lista (deixando-a vazia) THEN o sistema SHALL bloquear com o mesmo erro de "role já usada" se ela estiver em uso; se não estiver em uso, o sistema SHALL permitir a remoção mesmo que a lista fique vazia — o Goal de "nunca vazia" cobre o estado inicial/migração, não impede o admin de esvaziar deliberadamente depois (ele reconfigura antes de precisar atribuir a próxima role).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| ROLECFG-01 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-02 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-03 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-04 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-05 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-06 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-07 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-08 | P1: Admin cadastra lista de roles | Design | Pending |
| ROLECFG-09 | P2: Edição de role via drawer de ações | Design | Pending |
| ROLECFG-10 | P2: Edição de role via drawer de ações | Design | Pending |
| ROLECFG-11 | P2: Edição de role via drawer de ações | Design | Pending |
| ROLECFG-12 | P2: Edição de role via drawer de ações | Design | Pending |
| ROLECFG-13 | P2: Edição de role via drawer de ações | Design | Pending |
| ROLECFG-14 | P2: Edição de role via drawer de ações | Design | Pending |
| ROLECFG-15 | P3: TablePolicies usa multi-select | Design | Pending |
| ROLECFG-16 | P3: TablePolicies usa multi-select | Design | Pending |
| ROLECFG-17 | P3: TablePolicies usa multi-select | Design | Pending |

**ID format:** `ROLECFG-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 17 total, 0 mapped to tasks, 17 unmapped ⚠️ (mapeamento ocorre na fase Design/Tasks)

---

## Success Criteria

- [ ] Nenhum dos dois pontos de UI (`RoleCell`, `TablePolicies`) aceita mais texto livre pra role quando `enduser_roles_config` não está vazio.
- [ ] Nenhum app existente perde acesso/funcionalidade após a migração (roles órfãs continuam funcionando, seed automático garante lista não-vazia).
- [ ] Tentativa de remover role em uso retorna erro claro (contagem de usos), nunca falha silenciosa.
