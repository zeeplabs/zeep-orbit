# Modo RLS "policy" (Row Level Security 100% via Table Policies) Specification

## Problem Statement

Hoje `rls: "enabled"` não é "RLS livre por policy" como o nome sugere — `resolveOwner` (`internal/server/handler.go:28-36`) trata `"enabled"` exatamente como `"owner"`: injeta `owner_id = $sub` como filtro obrigatório em toda operação (`internal/query/builder.go` — `BuildList:160`, `BuildGetByID:323`, `BuildUpdate:307`, `BuildDelete:339,350`), **independente de role e de qualquer table policy cadastrada**. Uma table policy nativa (`CREATE POLICY`, feature `end-user-row-policies`) só consegue *restringir mais* o que o próprio usuário vê dentro das próprias linhas — nunca *ampliar* a visibilidade para linhas de outro usuário. Padrões comuns de produto ("admin vê os posts de todo mundo", "post publicado é visível pra qualquer um", "RH vê todos os colaboradores mas colaborador só vê a si mesmo") são estruturalmente impossíveis via `/{app}/{table}` hoje, não importa quantas policies existam.

Achado, reproduzido e documentado em demo real (`blog-demo`, app id `98f097a8-afe7-437a-b2e6-5e6b2c791208`, banco de dev porta 5433): policy `posts_select_admin_all` (role `admin_org`, sem filtro de linha) criada e correta no Postgres — confirmada via simulação direta de RLS claims por `psql` retornando as linhas certas — mas o mesmo usuário via API pública (`GET /blog-demo/posts`) recebia 0 resultados, porque o filtro de `resolveOwner` roda antes de a query alcançar qualquer policy.

Este spec adiciona um terceiro modo de RLS, `rls: "policy"`, que desacopla o auto-scope de dono do enforcement por policy — sem alterar o comportamento de nenhuma tabela existente em `""`, `"owner"` ou `"enabled"`.

## Goals

- [ ] Tabela com `rls: "policy"` não recebe filtro automático `owner_id = $sub` em nenhuma operação (list, get, update, delete) — visibilidade e permissão de escrita ficam 100% a cargo das table policies nativas do Postgres.
- [ ] Tabela com `rls: "policy"` é fail-closed por padrão desde a criação: nega toda leitura/escrita para o role `zeep_app_enduser` até que ao menos uma policy correspondente exista — usando o próprio comportamento nativo do Postgres (RLS habilitado + zero policies = nega tudo), não uma checagem extra na aplicação.
- [ ] `owner_id` passa a ser referenciável em cláusulas de policy (hoje impossível — `translateClause`/`internal/provisioner/policy.go:220-223` só aceita colunas presentes em `table.Columns`, e `owner_id` é injetada só na DDL).
- [ ] `owner_id` continua sendo preenchida automaticamente com o `sub` do usuário autenticado em todo INSERT em tabela `rls: "policy"`, mesmo sem o filtro automático de leitura — hoje isso quebraria (`internal/query/builder.go:246`: `if ownerID != ""` nunca inclui `owner_id` no INSERT quando `ownerID` vem vazio), o que violaria a constraint `NOT NULL` da coluna.
- [ ] Dashboard permite mudar o modo de uma tabela existente entre `"enabled"` ↔ `"policy"`, com aviso explícito de que a troca altera quem vê quais linhas.
- [ ] Nenhuma tabela hoje configurada como `""`, `"owner"` ou `"enabled"` tem seu comportamento alterado por esta feature.

## Out of Scope

| Feature | Reason |
| ------- | ------ |
| Alterar o comportamento de `rls: "owner"` ou `rls: "enabled"` existentes | Fora do raio de impacto autorizado — mudança aditiva, não migração forçada |
| Editor de SQL livre para policies | Já fora de escopo desde `end-user-row-policies`; builder estruturado permanece único caminho |
| Tornar `owner_id` opcional/removível da DDL | Avaliado e descartado — manter coluna obrigatória evita migração de dado e mantém DDL idêntica entre `enabled` e `policy` |
| Bloquear troca de modo em tabela com dados existentes | Avaliado e descartado — dashboard permite a troca com aviso, não bloqueio |
| Novo mecanismo de bypass de RLS para operações internas (Data Browser, retention job) | Já resolvido pela feature `end-user-row-policies` (essas operações correm como owner da tabela, isentas de RLS nativamente) — nada muda aqui |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| Fail-closed de tabela `policy` sem select policy | RLS habilitado na criação da tabela (não só na 1ª policy, diferente do fluxo lazy de `table_policies_store.go:100-142`); zero policies = Postgres nega tudo nativamente, sem checagem própria na app | Reaproveita mecanismo já validado nesta base (lição F14 da feature de webhooks: RLS habilitado + zero policy = deny-all); menos código, uma fonte de verdade | y |
| `owner_id` continua `NOT NULL REFERENCES _auth_users` em modo `policy` | Sim, mesma DDL de hoje | Evita migração de esquema; fica disponível pra quem quiser referenciar em policy sem forçar redesenho de schema | y |
| Troca de modo em tabela existente com dados | Dashboard permite, mostra aviso forte antes de confirmar | Recurso só é útil pra apps em produção se puder evoluir tabelas já existentes; risco mitigado por aviso explícito, não por bloqueio | y |
| Superfícies que hoje checam `RLS == "owner"` exclusivamente (não `"enabled"`/`"policy"`) | `internal/dashboard/handler.go:1899` (Data Browser expõe coluna `owner_id`) e `internal/docs/generator.go:264` (OpenAPI schema expõe `owner_id`) devem passar a também reconhecer `"policy"` — mesma razão que hoje as tratam para `"owner"`: a coluna existe e é legítima de expor | Descoberto lendo o código, não é decisão de produto — corrigido no Design | n/a (fato de código) |
| Nome do terceiro valor de `rls` | `"policy"` | Nome já usado nas conversas anteriores desta análise; comunica claramente "controle 100% delegado à policy" | y |

**Open questions:** nenhuma — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Modo `rls: "policy"` sem auto-scope de dono ⭐ MVP

**User Story**: Como admin de um app, quero criar uma tabela em modo `rls: "policy"` para que a visibilidade e permissão de escrita dependam inteiramente das policies que eu cadastrar — incluindo policies que deem a um role acesso a linhas de outros usuários.

**Why P1**: É o problema central do spec — sem isso, nenhuma das outras stories tem efeito prático.

**Acceptance Criteria**:

1. WHEN uma tabela é criada com `rls: "policy"` THEN o provisionador SHALL habilitar `ENABLE ROW LEVEL SECURITY` nessa tabela no momento da criação, antes de qualquer policy existir.
2. WHILE uma tabela `rls: "policy"` não tiver nenhuma policy de `select` cadastrada para o role do usuário autenticado, o sistema SHALL retornar zero linhas em `GET /{app}/{table}` e `GET /{app}/{table}/{id}` para esse usuário — via enforcement nativo do Postgres, sem filtro `owner_id` adicional da aplicação.
3. WHEN uma tabela `rls: "policy"` tem uma policy de `select` sem cláusula restritiva de linha para o role de um usuário THEN esse usuário SHALL ver todas as linhas da tabela via `GET /{app}/{table}`, incluindo linhas criadas por outros usuários.
4. The system SHALL NOT aplicar `WHERE owner_id = $sub` (ou equivalente em UPDATE/DELETE) a nenhuma operação sobre tabela `rls: "policy"`.
5. IF uma tabela `rls: "policy"` recebe um INSERT válido segundo suas policies THEN o sistema SHALL preencher a coluna `owner_id` dessa linha com o `sub` do usuário autenticado, exatamente como em `rls: "enabled"` hoje.
6. The system SHALL manter o comportamento atual, byte a byte, de tabelas com `rls: ""`, `rls: "owner"` e `rls: "enabled"` — nenhuma dessas SHALL ter seu SQL gerado alterado por esta feature.
7. IF o valor de `rls` enviado não for um de `""`, `"owner"`, `"enabled"`, `"policy"` THEN a validação SHALL rejeitar a criação/atualização da tabela com erro claro — hoje não existe essa validação em nenhuma camada (`internal/config` não valida o campo; qualquer string não reconhecida cai silenciosamente no ramo "sem RLS" de `resolveOwner`, confirmado por `internal/dashboard/table_rls_test.go:16` aceitando `"disabled"` sem erro). Fechar esse gap junto com a introdução do terceiro valor evita que um typo (`"polcy"`, `"enable"`) vire silenciosamente uma tabela pública.

**Independent Test**: Criar tabela `posts` em `rls: "policy"` sem nenhuma policy → `GET /app/posts` de qualquer usuário retorna `[]`. Criar policy `select` para role `admin` sem cláusula de linha → usuário `admin` vê posts de todos os usuários; usuário sem role `admin` continua vendo `[]`.

---

### P2: `owner_id` referenciável em cláusula de policy

**User Story**: Como admin de um app, quero escrever uma policy do tipo "role = 'admin' OR owner_id = claim.sub" para que um usuário sempre veja as próprias linhas além do que sua role adicional permitir.

**Why P1** (na prática, MVP junto com P1 — sem isso `owner_id` fica inacessível ao builder de policy, mas isoladamente não desbloqueia visibilidade cross-user; mantido como story separada por ser uma mudança de validação isolada, testável à parte): P2.

**Acceptance Criteria**:

1. WHEN o admin cria uma policy referenciando a coluna `owner_id` em uma tabela `rls: "policy"` ou `rls: "owner"` ou `rls: "enabled"` THEN o validador de cláusulas (`translateClause`) SHALL aceitar `owner_id` como coluna válida, com tipo `uuid`.
2. IF uma cláusula referencia `owner_id` com operador incompatível com `uuid` (ex.: `LIKE`) THEN o sistema SHALL rejeitar a policy com erro claro, mesma validação de tipo já aplicada às demais colunas `uuid`.
3. The system SHALL continuar rejeitando qualquer coluna que não esteja em `table.Columns` nem seja `owner_id` (nenhuma outra coluna de sistema — `id`, `created_at`, `updated_at`, `deleted_at` — passa a ser referenciável por esta feature).

**Independent Test**: Criar policy `select` com cláusula `owner_id = claim.sub OR role = 'admin'` numa tabela `rls: "policy"` → usuário comum vê só as próprias linhas, usuário `admin` vê todas.

---

### P3: Troca de modo em tabela existente via Dashboard

**User Story**: Como admin de um app já em produção com tabelas `rls: "enabled"`, quero migrar uma tabela para `rls: "policy"` sem recriar a tabela, para adotar o novo modelo de visibilidade sem perder dados.

**Why P2**: Sem isso, o modo novo só serve para apps novos — reduz o valor da feature para quem já opera em produção, mas não bloqueia o MVP (P1/P2 já entregam o mecanismo).

**Acceptance Criteria**:

1. WHEN o admin altera o campo RLS de uma tabela existente de `"enabled"` para `"policy"` (ou vice-versa) no Dashboard THEN o sistema SHALL exibir um aviso explícito, antes de confirmar, de que a troca altera quais linhas cada role passa a ver.
2. WHEN a troca é confirmada THEN o sistema SHALL aplicar a mudança sem exigir recriação da tabela nem perda de dados existentes.
3. IF a tabela de destino ainda não tem `ENABLE ROW LEVEL SECURITY` habilitado (caso legado: tabela `"enabled"` sem nenhuma policy ainda criada) THEN a troca para `"policy"` SHALL habilitar RLS nesse momento, para preservar o fail-closed do AC P1-2.

**Independent Test**: Tabela `enabled` existente com dados de 3 usuários, sem policies → mudar para `policy` → sem policy nenhuma, todos os usuários passam a ver `[]` (fail-closed), não um vazamento total.

---

## Edge Cases

- IF uma tabela `rls: "policy"` recebe DELETE ou UPDATE sem policy correspondente para o role do usuário THEN o sistema SHALL negar a operação (0 linhas afetadas / policy nativa nega), consistente com o comportamento de SELECT.
- IF o admin tenta criar `rls: "policy"` numa tabela sem autenticação por e-mail habilitada no app THEN o sistema SHALL rejeitar, mesma regra hoje aplicada a `"owner"`/`"enabled"` (`internal/dashboard/handler.go:132`) — `owner_id` continua exigindo FK para `_auth_users`.
- WHEN o Data Browser do Dashboard (superfície interna, roda como owner da tabela) lista uma tabela `rls: "policy"` THEN o sistema SHALL exibir todas as linhas normalmente, sem aplicar nem exigir nenhuma policy — mesma isenção nativa do Postgres já aplicada a `"owner"`/`"enabled"`.
- WHEN o gerador de documentação OpenAPI (`internal/docs/generator.go:264`) processa uma tabela `rls: "policy"` THEN o schema de resposta SHALL incluir a propriedade `owner_id` (tipo uuid, somente leitura), mesma regra hoje restrita a `RLS == "owner"`.
- WHEN `resolveTableRLS` (`internal/dashboard/handler.go:108-112`) e o gate de auth-email (`internal/dashboard/handler.go:132`) avaliam uma tabela `rls: "policy"` THEN ambos SHALL tratá-la como as regras hoje tratam `"owner"`/`"enabled"` (exige auth por e-mail; nunca é o default silencioso de `resolveTableRLS` quando `rls` vem vazio — esse default continua sendo `"enabled"`, não `"policy"`, pois trocar o default é fora de escopo).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --------------- | ----------- | ------ | ------- |
| RLSP-01 | P1: Modo policy sem auto-scope | Tasks | Implementing |
| RLSP-02 | P1: Fail-closed nativo desde criação | Tasks | Implementing |
| RLSP-03 | P1: owner_id continua preenchido no INSERT | Tasks | Implementing |
| RLSP-04 | P1: modos existentes intactos | Tasks | Implementing |
| RLSP-05 | P2: owner_id referenciável em clause | Design | Pending |
| RLSP-06 | P2: validação de tipo/coluna mantida | Design | Pending |
| RLSP-07 | P3: troca de modo via Dashboard, com aviso | Design | Pending |
| RLSP-08 | P3: troca preserva fail-closed em tabela legada | Design | Pending |
| RLSP-09 | P1: validação de enum `rls` (fecha gap pré-existente) | Tasks | Implementing |
| RLSP-10 | Edge: resolveTableRLS/auth-email gate reconhecem `"policy"` | Design | Pending |

**ID format:** `RLSP-NN`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 10 total, 0 mapped to tasks, 10 unmapped ⚠️ (aguardando fase Design/Tasks)

---

## Notas para o Design (não normativas, achados do agente de exploração)

- `internal/server/rls_policy_test.go` (`TestRLSPolicy_EndToEndMotivatingCase`) já é o precedente de teste ponta-a-ponta pra RLS nativa nesta base — inclui prova via REST e via conexão raw pgx como `zeep_app_enduser`, e um sub-teste confirmando que o pool "owner" (Data Browser) ignora as policies. Design deve seguir essa mesma estrutura para os testes de `"policy"` mode, em vez de criar convenção nova.
- Convenção de nome de teste nesta base: `Test<Verbo><Entidade>_<Cenário>` (`table_policies_store_test.go`, `table_policies_handler_test.go`).
- `internal/query/builder.go`'s `systemFields` (linha 64-70) já lista `owner_id: uuid` para fins de filtro/ordenação via query string — separado do allowlist de `policy.go` usado por cláusulas de policy (RLSP-05). Design deve decidir se isso já basta ou se precisa de ajuste para `"policy"` mode (provavelmente não, é ortogonal).
- `DropTable` não tem guarda própria pra policies — Postgres já cascateia `DROP POLICY` com `DROP TABLE`. Nenhuma mudança esperada aqui.

---

## Success Criteria

- [ ] Tabela `rls: "policy"` sem policy nenhuma retorna `[]` para qualquer usuário (fail-closed verificado por teste automatizado, não só manual).
- [ ] Tabela `rls: "policy"` com policy `select` sem cláusula de linha permite a um role ver linhas de outro usuário (teste automatizado cross-user, cenário que hoje é estruturalmente impossível).
- [ ] INSERT em tabela `rls: "policy"` continua populando `owner_id` corretamente (regressão coberta por teste).
- [ ] Nenhum teste existente de `rls: "owner"`/`"enabled"` muda de comportamento (suite completa verde sem alteração de asserts nesses testes).
- [ ] `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l` limpos; `tsc -b` e `npm run build` do dashboard limpos (troca de modo é UI).
