# End-User Row Policies (Native Postgres RLS) Specification

## Problem Statement

Zeep Orbit hoje só sabe filtrar dados de usuário final por dono (`rls: owner`, filtro `WHERE owner_id = $N` montado em Go — `internal/query/builder.go`, `internal/server/handler.go:resolveOwner`). Não existe forma de expressar "usuário com papel X só pode fazer ação Y numa linha se condição Z sobre os dados da própria linha for verdadeira" — caso real: `asset-manager-web` precisa impedir que um colaborador aprove a própria requisição, uma regra que depende do conteúdo da linha (`requester_id` da requisição vs. usuário autenticado), não só de papel fixo. Hoje esse tipo de app é obrigado a manter um backend próprio (Express) só pra reimplementar essa checagem, porque o Orbit não oferece o mecanismo. A superfície de risco é maior do que "app sem essa feature": times já estão conectando o Orbit direto do browser guardando JWT em `localStorage` (`Starbem/starbem-interno-RH`) pra eliminar o backend próprio — sem esse mecanismo, a lacuna de autorização vai junto pro cliente.

## Goals

- [ ] Usuário final de um app tem um papel de negócio (`role`, string livre, definida por app) persistido em `_auth_users` e presente como claim no JWT emitido no login.
- [ ] Admin do app (via dashboard, papel `AppRoleAdmin` já existente em `ResolveAppRole`) define, por tabela e por ação (`select`/`insert`/`update`/`delete`), uma ou mais policies compostas por cláusulas estruturadas (`coluna operador valor`, onde `valor` é um claim do JWT ou um literal) — sem SQL livre exposto ao usuário do dashboard.
- [ ] Essas policies são traduzidas para `CREATE POLICY` nativo do Postgres e aplicadas com `ENABLE ROW LEVEL SECURITY` na tabela — enforcement dentro do banco, não em filtro Go. Fecha bypass por conexão direta ao Postgres (via psql, ferramenta externa, etc.), não só via REST.
- [ ] Tabelas sem nenhuma policy nova cadastrada mantêm o comportamento atual exatamente como está hoje (RLS-owner app-layer, se configurado, ou sem filtro) — nenhum app existente quebra ao subir a feature.
- [ ] Operações internas de confiança (Data Browser do dashboard, job de retention/purge, provisionador de schema) continuam lendo/escrevendo todas as linhas de todas as tabelas, sem serem bloqueadas pelas novas policies — porque continuam executando como o role owner das tabelas (isento de RLS por padrão no Postgres), nunca precisando de flag de bypass própria da aplicação.
- [ ] Admin do app configura as policies numa tela do dashboard (builder de cláusulas, não editor de SQL).
- [ ] Admin do app pode declarar um FK explícito de uma coluna própria (ex.: `requester_id`) para `_auth_users.id` — hoje só existe o FK automático e implícito de `owner_id`, sem forma de o dono do app vincular explicitamente uma coluna de negócio a um usuário final.

## Out of Scope

Explicitamente excluído desta spec. Documentado pra prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Migrar o RLS-owner atual (filtro `WHERE owner_id` em Go) para `CREATE POLICY` nativo | Mecanismo já funciona, sem bug reportado — migrar sem necessidade concreta é retrabalho fora do escopo motivador desta feature. Os dois eixos continuam coexistindo (Assumption abaixo). |
| Revogação ativa de JWT de usuário final ao trocar de papel | Mecanismo de `jti`+revogação hoje só existe para App Tokens (`internal/server/middleware.go`), não para o JWT de login de usuário final. Estender exigiria mudar `IssueJWT`/`ParseJWT` e o fluxo de login inteiro — escopo maior, não bloqueia o caso motivador. |
| Policies que referenciam colunas de outras tabelas (subquery/join entre tabelas) | Cláusulas operam só sobre colunas da própria tabela sendo protegida + claims do JWT. Regra motivadora (`requester_id != claim.user_id`) não precisa disso. |
| Papéis hierárquicos ou herança de papel (`admin` implica `editor`) | `role` é string livre plana, sem árvore — cada policy lista os papéis aos quais se aplica explicitamente. |
| Editor de SQL livre para policies no dashboard | Superfície de injeção/DDL arbitrário — todas as policies passam pelo builder estruturado (allowlist de colunas/operadores), sem exceção. |
| Preview/dry-run de policy ("quais linhas o papel X veria") | Ferramenta de diagnóstico útil, mas não bloqueia o caso motivador — candidato a P3/backlog. |

---

## Assumptions & Open Questions

Toda ambiguidade está resolvida ou registrada aqui — nada fica silenciosamente indefinido.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Operadores suportados no builder de cláusula | `=`, `!=`, `IN`, `NOT IN`, `>`, `<`, `>=`, `<=`, `IS NULL`, `IS NOT NULL` sobre `(coluna, claim ou literal)` | Decisão explícita do usuário: cobrir os casos de comparação numérica/data (`valor <= claim.limite`) e checagem de nulidade (`aprovado_por IS NULL`, comum em workflow de aprovação) além do caso motivador de igualdade/desigualdade. `LIKE`/`ILIKE` e agrupamento arbitrário ficam fora — sem caso motivador concreto, custo desproporcional (risco de scan sem índice, ambiguidade de escaping) | y — confirmado pelo usuário em 2026-08-07 |
| Composição de cláusulas dentro de uma policy | `AND`/`OR` no mesmo nível, sem agrupamento (parênteses aninhados) — avaliação estritamente left-to-right, cada cláusula além da primeira carrega o conector (`AND`/`OR`) que a liga ao resultado acumulado até ali | Cobre a maioria dos casos reais de "papel + condição de linha" sem exigir builder de árvore de expressão (validação/tradução SQL muito mais simples e segura que agrupamento arbitrário) | y — confirmado pelo usuário em 2026-08-07 |
| Fonte de claims disponíveis nas cláusulas | `role` (novo), `sub`/user id (já existe em `RegisteredClaims.Subject`), `email` (já existe) | São os únicos dados de identidade que o JWT já carrega ou vai carregar nesta feature | y (decorre da spec, sem gray area real) |
| Mecanismo de isenção de RLS para rotinas internas | Segundo role Postgres, sem privilégio de owner/`BYPASSRLS`, usado só no caminho de request de usuário final via `SET LOCAL ROLE` dentro da transação; Data Browser/purge/provisionador continuam como o role owner (que o Postgres já isenta de RLS por padrão, sem `FORCE`) | Mecanismo de permissão real do Postgres (GRANT/membership), não uma flag de sessão forjável por bug futuro de código — validado com o usuário no início do Design | y |
| Janela de staleness quando papel do usuário muda em sessão ativa | Aceitar até 1h (TTL atual do JWT, `TokenTTL` em `internal/auth/jwt.go`) sem revogação ativa | Revogação ativa pra JWT de usuário final é Out of Scope (ver tabela acima); é o mesmo risco já aceito no RBAC per-app de dashboard | y |
| Granularidade de ação | 4 ações do Postgres RLS: `select`, `insert`, `update`, `delete` (mapeadas 1:1 pra `FOR SELECT/INSERT/UPDATE/DELETE` de `CREATE POLICY`) | Alinhado ao que o Postgres já oferece nativamente, sem inventar granularidade extra | y |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Usuário final de app tem papel de negócio refletido no JWT ⭐ MVP

**User Story**: Como app rodando no Orbit, quero que cada usuário final tenha um papel de negócio (definido por mim, não pelo Orbit) presente no JWT emitido no login, para poder usar esse papel em regras de autorização de dados.

**Why P1**: Sem o claim de papel no JWT, não existe informação disponível no banco (via GUC de sessão) pra nenhuma policy nativa referenciar — é a pré-condição de toda a feature.

**Acceptance Criteria**:

1. WHEN a migração desta feature roda num app existente THEN o sistema SHALL adicionar a coluna `role TEXT NOT NULL DEFAULT 'member'` em `_auth_users`, de forma idempotente (mesmo padrão de `addMissingAuthUserColumns`).
2. WHEN um usuário final faz login com sucesso (`/{app}/auth/login` ou Google OAuth) THEN o sistema SHALL incluir o valor atual de `_auth_users.role` como claim `role` no JWT emitido.
3. The system SHALL manter `role` como string livre (sem enum fixo), definida e validada pelo dono do app via dashboard — o Orbit não impõe vocabulário de papéis.
4. IF um `_auth_users` existente (criado antes desta feature) não tem valor em `role` THEN o sistema SHALL tratá-lo como `'member'` (valor do `DEFAULT`), sem exigir migração manual de dado por app.

**Independent Test**: Criar usuário final com papel customizado via dashboard/API de gestão de usuário; fazer login; decodificar o JWT retornado e confirmar que o claim `role` está presente com o valor esperado.

---

### P1: App admin define policy de linha por papel + condição de dados, aplicada nativamente pelo Postgres ⭐ MVP

**User Story**: Como admin de um app, quero definir regras de acesso a uma tabela combinando papel do usuário e condição sobre os dados da própria linha (ex.: "papel `approver` só atualiza a linha se `requester_id` for diferente do próprio usuário"), para que o Postgres aplique essa regra em toda query, inclusive fora da API do Orbit.

**Why P1**: É a capacidade central da feature — sem isso, o caso motivador (`asset-manager-web`) continua sem solução no Orbit.

**Acceptance Criteria**:

1. WHEN um admin do app (`AppRoleAdmin` ou `superadmin` global) cria uma policy para uma tabela via `POST /dashboard/api/apps/{id}/tables/{table}/policies` com `{action, roles: [...], clauses: [{column, operator, value_source, value, logic}]}` válidos THEN o sistema SHALL persistir a policy e traduzi-la para `CREATE POLICY "<nome>" ON "<schema>"."<tabela>" FOR <ação> USING (<cláusulas dobradas left-to-right por `logic`, cada passo totalmente parenteseado>) WITH CHECK (<mesma expressão>)` (`WITH CHECK` só para `insert`/`update`); a primeira cláusula não carrega `logic` (não há acumulado anterior), toda cláusula seguinte SHALL informar `logic: "AND"|"OR"`.
2. WHEN a policy é criada pela primeira vez para uma tabela que ainda não tem RLS nativo habilitado THEN o sistema SHALL executar `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` e `GRANT` das permissões necessárias ao role de usuário final antes de criar a policy (sem `FORCE ROW LEVEL SECURITY` — o role owner, usado pelas rotinas internas, já é isento de RLS por padrão do Postgres).
3. IF uma cláusula referencia uma coluna que não existe na tabela, ou um nome de coluna fora do padrão `identRe` (`^[a-z][a-z0-9_]{0,62}$`, mesmo allowlist de `internal/dashboard/handler.go:85`), ou um operador fora de `{=, !=, IN, NOT IN, >, <, >=, <=, IS NULL, IS NOT NULL}`, ou (quando não for a primeira cláusula da policy) um `logic` fora de `{AND, OR}` THEN o sistema SHALL rejeitar a criação com 400 e mensagem em inglês descrevendo qual cláusula falhou, sem executar DDL algum.
4. IF uma cláusula usa operador `IS NULL` ou `IS NOT NULL` e o payload informa `value_source`/`value` não-vazios THEN o sistema SHALL rejeitar com 400 — são operadores unários, não recebem valor comparável.
5. WHEN uma cláusula usa `value_source: "claim"` THEN o sistema SHALL aceitar só `role`, `sub` ou `email` como `value`, traduzindo para `current_setting('app.jwt_<claim>', true)` (com cast explícito pro tipo da coluna via `::uuid`/`::text`/`::numeric`/etc. conforme o tipo declarado da coluna).
6. WHEN uma cláusula usa `value_source: "literal"` THEN o sistema SHALL embutir o valor via `quote_literal()`/formatação segura equivalente (nunca concatenação direta de string do usuário no DDL).
7. WHEN um usuário final autenticado por JWT executa uma ação sobre uma tabela com policy cadastrada para o papel dele e a ação THEN o Postgres SHALL permitir ou negar a linha conforme a policy, independente de o request ter vindo da API REST do Orbit ou de uma conexão direta ao Postgres autenticada como o role de usuário final (não o role owner).
8. WHEN um admin do app deleta uma policy via `DELETE .../policies/{id}` THEN o sistema SHALL executar `DROP POLICY` correspondente; SE for a última policy da tabela para toda ação, a tabela permanece com `ROW LEVEL SECURITY` habilitado (nenhuma linha visível ao role de usuário final até nova policy existir) — comportamento default-deny explícito, não implícito.
9. WHEN qualquer operação de criar/atualizar/deletar policy é aplicada com sucesso THEN o sistema SHALL registrar entrada em `audit_log` (mesmo padrão de `InsertAuditLog` usado em `rbac-per-app`).

**Independent Test**: Criar tabela `requests` com coluna `requester_id UUID`; criar policy `FOR UPDATE`, papel `approver`, cláusula `requester_id != claim:sub`; autenticar como usuário A (`role=approver`) e tentar `UPDATE` numa linha onde `requester_id = A.id` → negado pelo Postgres (não pela API); tentar `UPDATE` numa linha de outro requester → permitido. Repetir a mesma tentativa de `UPDATE` via conexão `psql` direta autenticada como o role de usuário final com os mesmos GUCs de sessão setados manualmente, confirmando que o bloqueio é do Postgres, não da camada HTTP.

---

### P1: Operações internas de confiança continuam ilesas às novas policies ⭐ MVP

**User Story**: Como plataforma, quero que Data Browser, job de retention/purge e provisionador de schema continuem lendo/escrevendo qualquer linha de qualquer tabela depois que `ROW LEVEL SECURITY` for habilitado, para que a feature não quebre operação administrativa já existente.

**Why P1**: `cmd/zeep/main.go` usa uma única DSN — o mesmo role Postgres provisiona schema e serve requests, e é o **owner** das tabelas que cria. Sem um caminho de execução separado para requests de usuário final, esse role continuaria isento de RLS em todo lugar (owner é isento por padrão), o que significaria "policy nunca é aplicada a ninguém" — é um requisito técnico obrigatório da feature, não um nice-to-have.

**Acceptance Criteria**:

1. WHEN a feature é habilitada num ambiente THEN o sistema SHALL provisionar um segundo role Postgres (sem `BYPASSRLS`, sem ownership das tabelas) dedicado a servir requests de usuário final, com `GRANT` das permissões necessárias (`SELECT`/`INSERT`/`UPDATE`/`DELETE` conforme a tabela) e membership do role principal nele (`GRANT <role_enduser> TO <role_principal>`, pré-condição de `SET ROLE`).
2. WHEN o servidor executa uma query originada de um request de usuário final autenticado por JWT de app (`internal/server/handler.go`) THEN o sistema SHALL abrir uma transação e executar `SET LOCAL ROLE <role_enduser>` antes da query, revertendo automaticamente ao fim da transação (mesmo padrão transacional de `SET LOCAL` já usado em `internal/db/client.go:69` para `statement_timeout`).
3. WHILE uma operação está sendo executada pelo Data Browser do dashboard, pelo job de retention/purge (`internal/dashboard/purge.go`), ou pelo provisionador de schema THEN o sistema SHALL executá-la com o role principal (owner), sem nunca chamar `SET ROLE` — nenhuma dessas rotinas SHALL ter suas queries filtradas por policies de usuário final.
4. The system SHALL garantir que nenhum handler do caminho `/{app}/...` (usuário final) executa código com o role owner — a troca de role para `<role_enduser>` é incondicional nesse caminho, não uma opção configurável por request.

**Independent Test**: Habilitar `ROW LEVEL SECURITY` numa tabela com policy que nega tudo a um papel específico; confirmar que o Data Browser do dashboard (executando como role owner) ainda lista 100% das linhas dessa tabela para um admin do app; confirmar que o job de purge ainda deleta linhas expiradas independente de papel/policy; confirmar que uma query rodada manualmente como `<role_enduser>` respeita a policy.

---

### P1: Admin do app configura policies via UI do dashboard (builder estruturado) ⭐ MVP

**User Story**: Como admin de app sem conhecimento de SQL, quero montar uma regra de acesso por linha escolhendo tabela, ação, papéis e cláusulas (coluna, operador, valor) numa tela do dashboard, para não precisar chamar a API de policy manualmente nem escrever SQL.

**Why P1**: Definido com o usuário como parte do MVP — sem UI, a feature só é usável por quem já sabe montar o JSON da API diretamente, o que não é aceitável como entrega completa desta spec.

**Acceptance Criteria**:

1. WHEN um admin do app abre a tela de policies de uma tabela (nova aba/seção dentro da página de detalhe da tabela no dashboard) THEN o sistema SHALL listar as policies existentes (ação, papéis, cláusulas) e um formulário pra criar uma nova.
2. WHEN o admin monta uma cláusula no formulário THEN o sistema SHALL oferecer só as colunas reais da tabela (vindas do schema já carregado, mesmo padrão de `filterCol` em `DataBrowserPage.tsx`), só os operadores suportados (`=`, `!=`, `IN`, `NOT IN`, `>`, `<`, `>=`, `<=`, `IS NULL`, `IS NOT NULL`) — sem campo de texto livre pra nome de coluna ou operador — e, a partir da segunda cláusula, um select `AND`/`OR` ligando-a à cláusula anterior; ao escolher `IS NULL`/`IS NOT NULL` o sistema SHALL ocultar o campo de valor (operador unário).
3. WHEN o admin escolhe `value_source: claim` numa cláusula THEN o sistema SHALL oferecer um select fixo com `role`, `sub`, `email` — sem texto livre.
4. WHEN o formulário é submetido com sucesso THEN o sistema SHALL mostrar toast de sucesso (`sonner`) e atualizar a lista de policies sem reload de página; IF a API retornar erro (400/403/500) THEN o sistema SHALL mostrar `toast.error(error.message)`.
5. The system SHALL ter todo texto da UI (labels, botões, mensagens) traduzido em `en.json` e `pt-BR.json` via `react-i18next`, sem string hardcoded.

**Independent Test**: Abrir a tela de policies de uma tabela existente no dashboard, criar uma policy pelo formulário (sem tocar em API/JSON manualmente), confirmar que ela aparece na lista e que o comportamento de acesso muda de fato para o papel configurado.

---

### P2: App admin declara FK explícito de coluna própria para `_auth_users`

**User Story**: Como admin de app, quero declarar que uma coluna minha (ex.: `requester_id` em `requests`) referencia `_auth_users.id`, para ter integridade referencial garantida pelo banco e ver esse relacionamento na UI de relacionamentos do dashboard — hoje só o `owner_id` automático tem esse vínculo, colunas de negócio custom não têm como apontar pra `_auth_users`.

**Why P2**: Descoberta durante a análise desta spec (`config.ValidateTables`/`validateReference` rejeita hoje qualquer `references.table` fora do conjunto de tabelas do próprio app, e `_auth_users` nunca entra nesse conjunto — `internal/dashboard/handler.go:148-155`). Não bloqueia as stories P1 desta spec: o caso motivador (`requester_id != claim:sub`) já funciona com uma coluna `UUID` simples, sem FK declarado — FK aqui é sobre integridade referencial e UI de relacionamentos, não sobre a policy em si funcionar.

**Acceptance Criteria**:

1. WHEN um admin declara uma coluna com `references: {table: "_auth_users", column: "id"}` THEN o sistema SHALL aceitar essa referência mesmo `_auth_users` não estando no conjunto de tabelas do próprio app (caso especial, sem precisar de `tablesByName["_auth_users"]`).
2. IF a coluna que referencia `_auth_users.id` não é do tipo `uuid` THEN o sistema SHALL rejeitar com 400 (mesma exigência de tipo já implícita no `owner_id` automático).
3. IF `references.table == "_auth_users"` e `references.column != "id"` THEN o sistema SHALL rejeitar — `_auth_users` só expõe `id` como alvo de referência nesta fase (colunas de negócio adicionais de `_auth_users`, como `role`, não são FK-referenciáveis).
4. WHEN a tabela é provisionada com essa referência THEN o sistema SHALL gerar `REFERENCES "<schema>"."_auth_users"("id")` na DDL da coluna, com o `ON DELETE` declarado pelo admin (mesmo caminho de `columnDDL` já usado pra qualquer outro FK).
5. WHEN essa referência existe THEN a UI de relacionamentos do dashboard (`schema-relationships-and-indexes`) SHALL exibi-la como qualquer outro relacionamento entre tabelas do app.

**Independent Test**: Declarar coluna `requester_id UUID` em `requests` com `references: {table: "_auth_users", column: "id"}`; confirmar que a tabela é criada com o FK real no Postgres; tentar inserir um `requester_id` que não existe em `_auth_users` → erro de FK do próprio banco; deletar o `_auth_users` referenciado com `on_delete: restrict` → bloqueado pelo banco.

---

### P2: Auditoria e rastreio de mudanças de policy

**User Story**: Como admin de app, quero ver o histórico de criação/alteração/remoção de policies de uma tabela, para auditar quem mudou o quê e quando.

**Why P2**: Consequência direta de já registrar em `audit_log` (AC-08 da story de policy) — expor isso numa tela de histórico é valor incremental, não bloqueia o funcionamento da feature.

**Acceptance Criteria**:

1. WHEN um admin abre a aba de auditoria de uma tabela THEN o sistema SHALL listar entradas de `audit_log` filtradas por `app_id` + `table` + ação relacionada a policy, mesmo padrão de listagem de auditoria já usado em outras specs.

---

### P3: Preview/dry-run de policy

**User Story**: Como admin de app, quero simular "quais linhas o papel X veria" antes de salvar uma policy, para validar a regra sem precisar logar como outro usuário.

**Why P3**: Ferramenta de diagnóstico útil, mas não bloqueia nenhum caso motivador — fica pra depois que o motor de enforcement estiver validado em produção.

**Acceptance Criteria**:

1. WHEN um admin usa a função de preview com um papel e um `sub` fictício THEN o sistema SHALL executar a query com a GUC de sessão setada para esses valores fictícios e mostrar o resultado, sem afetar dados reais.

---

## Edge Cases

- IF a criação de uma policy resultaria em `CREATE POLICY` com nome duplicado na mesma tabela/ação THEN o sistema SHALL rejeitar com 409 antes de tentar o DDL.
- IF uma tabela é deletada (`DeleteAppTable`) THEN o sistema SHALL deletar as policies associadas no mesmo fluxo (o `DROP TABLE` já remove as policies nativas do Postgres; a limpeza é do metadado próprio do Orbit).
- IF o admin tenta usar `value_source: claim` com um claim que não seja `role`/`sub`/`email` THEN o sistema SHALL rejeitar com 400 (ver AC-04 da story principal).
- WHEN uma tabela nunca teve nenhuma policy cadastrada THEN o sistema SHALL manter `ROW LEVEL SECURITY` desabilitado nela (comportamento idêntico ao atual) — só é habilitado no momento em que a primeira policy é criada (AC-02 da story principal), nunca preventivamente.
- IF o `role` claim do JWT de um usuário não corresponde a nenhum papel referenciado em nenhuma policy da tabela, mas a tabela tem `ROW LEVEL SECURITY` habilitado (por outra policy de outro papel) THEN o sistema SHALL negar todo acesso a esse usuário nessa tabela (default-deny do próprio Postgres, sem policy = sem linha visível para o role de usuário final).
- IF a policy tem só 1 cláusula THEN o campo `logic` SHALL ser omitido/ignorado (não há cláusula anterior pra ligar) — presença de `logic` numa policy de 1 cláusula é erro de validação, não é silenciosamente descartada.
- WHEN a policy mistura `AND` e `OR` entre mais de 2 cláusulas THEN a tradução SQL SHALL dobrar left-to-right, parenteseando cada passo (`((c1 AND c2) OR c3)`, nunca `(c1 AND (c2 OR c3))`) — ordem de avaliação determinística, sem depender de precedência implícita de operador.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| ROWPOL-01 | P1: Papel no JWT | Tasks | Implementing |
| ROWPOL-02 | P1: Papel no JWT | Tasks | Implementing |
| ROWPOL-03 | P1: Papel no JWT | Tasks | Implementing |
| ROWPOL-04 | P1: Papel no JWT | Tasks | Implementing |
| ROWPOL-05 | P1: Policy nativa | Tasks | Implementing |
| ROWPOL-06 | P1: Policy nativa | Tasks | Verified |
| ROWPOL-07 | P1: Policy nativa | Tasks | Implementing |
| ROWPOL-08 | P1: Policy nativa | Tasks | Implementing |
| ROWPOL-09 | P1: Policy nativa | Tasks | Implementing |
| ROWPOL-10 | P1: Policy nativa | Tasks | Verified |
| ROWPOL-11 | P1: Policy nativa | Tasks | Verified |
| ROWPOL-12 | P1: Policy nativa | Tasks | Implementing |
| ROWPOL-13 | P1: Bypass interno | Tasks | Verified |
| ROWPOL-14 | P1: Bypass interno | Tasks | Verified |
| ROWPOL-15 | P1: Bypass interno | Tasks | Verified |
| ROWPOL-16 | P1: UI dashboard | Tasks | Implementing |
| ROWPOL-17 | P1: UI dashboard | Design | Pending |
| ROWPOL-18 | P1: UI dashboard | Design | Pending |
| ROWPOL-19 | P1: UI dashboard | Tasks | Implementing |
| ROWPOL-20 | P1: UI dashboard | Design | Pending |
| ROWPOL-21 | P2: FK explícito para `_auth_users` | Design | Pending |
| ROWPOL-22 | P2: FK explícito para `_auth_users` | Design | Pending |
| ROWPOL-23 | P2: FK explícito para `_auth_users` | Design | Pending |
| ROWPOL-24 | P2: FK explícito para `_auth_users` | Design | Pending |
| ROWPOL-25 | P2: FK explícito para `_auth_users` | Design | Pending |
| ROWPOL-26 | P2: Auditoria | - | Pending |
| ROWPOL-27 | P3: Preview | - | Pending |
| ROWPOL-28 | P1: Policy nativa — operadores estendidos (`>`,`<`,`>=`,`<=`,`IS NULL`,`IS NOT NULL`) | Tasks | Implementing |
| ROWPOL-29 | P1: Policy nativa — composição `AND`/`OR` flat (fold left-to-right) | Tasks | Implementing |

**ID format:** `ROWPOL-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 29 total, 17 mapped to tasks (T1–T11 done: ROWPOL-01/02/03/04/05/06/07/08/09/10/11/12/13/14/15/28/29), 12 unmapped (ROWPOL-16 through ROWPOL-27 — Phase 5 UI, Phase 6 docs, and the standalone T17 phase, none of which have run yet)

---

## Success Criteria

- [ ] `asset-manager-web` consegue expressar a regra "colaborador não aprova a própria requisição" só com policy nativa do Orbit, sem lógica de autorização própria no Express.
- [ ] Uma tentativa de acesso via conexão direta ao Postgres autenticada como o role de usuário final (sem passar pela API REST) respeita a policy — prova de que o enforcement é do banco, não da aplicação.
- [ ] Nenhum app existente perde acesso a dados após o deploy (tabelas sem policy nova mantêm comportamento atual, 100% retrocompatível).
- [ ] Data Browser, purge job e provisionador continuam operando sobre 100% das linhas após `ROW LEVEL SECURITY` ser habilitado em qualquer tabela, porque nunca executam como o role de usuário final.
- [ ] `asset-manager-web` consegue declarar `requester_id` com FK real pra `_auth_users.id`, com integridade garantida pelo Postgres, visível na UI de relacionamentos do dashboard.
