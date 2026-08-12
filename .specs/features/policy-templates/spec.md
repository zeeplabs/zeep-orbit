# Policy Templates & Help Drawer Specification

## Problem Statement

O formulário de criação de table policy (`internal/dashboard/ui/src/components/TablePolicies.tsx`) expõe direto o modelo técnico de RLS do zeep-orbit: `Action` (select/insert/update/delete), `Roles`, e uma lista de `Clauses` onde cada cláusula exige escolher `Column` + `Operator` (allowlist de 10 operadores) + `ValueSource` (`claim` vs `literal`) + `Value` + `Logic` (`AND`/`OR` de composição). Esse modelo é fiel ao que `internal/provisioner/policy.go` (`BuildPolicySQL`) aceita, mas presume que quem está montando a policy entende RLS do Postgres, JWT claims e álgebra booleana de cláusulas — a maioria dos usuários do Dashboard não é tecnicamente essa pessoa. Hoje não existe caminho mais simples: criar qualquer policy, incluindo os casos mais comuns ("só o dono vê", "todo mundo autenticado vê"), passa pelo formulário técnico completo.

## Goals

- [ ] Tela de criação de policy oferece, por padrão, uma lista de **templates** nomeados em linguagem de produto (não técnica) que cobrem os casos mais comuns, gerando a `PolicyDef` (Action/Roles/Clauses) certa por trás sem expor Column/Operator/ValueSource/Logic ao usuário.
- [ ] Usuário sempre pode sair do modo template e usar o formulário técnico atual ("Modo avançado") para combinações que nenhum template cobre — o formulário atual não é removido nem alterado em capacidade.
- [ ] Um botão "Ajuda" na tela de Table Policies abre um drawer com tutorial explicando, com exemplos reais, como montar policies avançadas manualmente — cobrindo o que os templates não alcançam.
- [ ] Todo texto novo (nomes de template, descrições, conteúdo do tutorial) passa por `react-i18next`, adicionado em `src/locales/en.json` e `src/locales/pt-BR.json` na mesma mudança (`AGENTS.md` §5).

## Out of Scope

Explicitamente excluído desta spec. Documentado pra prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Edição de policy existente via template | `table-policy-edit` (spec já implementada) cobre edição — continua usando exclusivamente o formulário avançado, pré-populado como hoje. Reabrir uma policy existente para "reaplicar um template" teria que reverse-engenhar qual template (se algum) gerou aquela policy, o que não é rastreado e não tem caso motivador concreto. |
| Novo endpoint de criação em lote/transacional no backend | Escopo fechado como "só frontend" — qualquer template que precise de mais de 1 policy usa chamadas sequenciais ao endpoint `POST` já existente (ver Assumptions, template composto). |
| Novos operadores, claims ou capacidades de cláusula (ex.: `LIKE`, `now()`, claims customizados) | Nenhum template ou exemplo do tutorial usa capacidade que `internal/provisioner/policy.go` não aceita hoje — ensinar ou oferecer algo que o backend rejeita quebra a promessa dos dois recursos. |
| Templates para modos RLS `""` (sem RLS) ou tabelas sem `owner_id` | Templates que referenciam `owner_id` (1, 3, 6) só aparecem quando `config.HasOwnerColumn(rls)` é verdadeiro pra tabela — mesma condição já usada para exibir a coluna `owner_id` no restante do Dashboard (`internal/dashboard/handler.go`, Data Browser). Tabela com `rls == ""` não tem tela de Table Policies pra começo (fora de escopo mudar isso). |
| Personalização/edição de conteúdo do tutorial pelo usuário final (multi-tenant) | Conteúdo do drawer é estático, mantido pelo time do Orbit, igual a qualquer outra string de UI. |
| Analytics/telemetria de qual template é mais usado | Sem caso motivador concreto nesta spec; pode ser considerado depois se o time de produto pedir. |

---

## Assumptions & Open Questions

Toda ambiguidade está resolvida ou registrada aqui — nada fica silenciosamente indefinido.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Como satisfazer `len(def.Clauses) == 0` → erro (`internal/provisioner/policy.go`) pro template "Todos autenticados podem ver" (sem condição de linha) | O template gera uma cláusula dummy sempre-verdadeira: `owner_id IS NOT NULL` (operador unário, sem `value`/`value_source`) | `owner_id` é `NOT NULL` em toda tabela onde `config.HasOwnerColumn(rls)` é verdadeiro (`internal/provisioner/table.go`), logo a cláusula nunca filtra nada na prática — satisfaz a validação do backend sem exigir mudança nele e sem expor a cláusula ao usuário (ela nunca aparece na UI de template, só no resumo técnico se o usuário abrir "ver como avançado") | y — decisão técnica, sem ambiguidade de produto a validar; não exige mudança de backend |
| Falha parcial no template composto ("Leitura liberada, escrita só do dono" — cria 3 policies via 3 `POST` sequenciais: SELECT aberta, UPDATE dono, DELETE dono) | Para a sequência no primeiro erro. Mantém as policies já criadas com sucesso (sem rollback automático), mostra claramente quais foram criadas e qual falhou (com o motivo). Usuário completa manualmente (modo avançado) ou repete o template — o template detecta policies já existentes pra mesma `(action)` e as pula, tentando só as que faltam | Confirmado pelo usuário; mesmo padrão de "sem rollback automático" já aceito na spec `table-policy-edit` (last-write-wins, sem lock/transação distribuída). Criar um endpoint transacional novo contrariaria o escopo "só frontend" já fechado | y — confirmado pelo usuário em 2026-08-12 |
| Nome (`Name`, campo `identRe`) das policies geradas por template | Gerado automaticamente pelo sistema a partir do template + action (ex.: `tpl_owner_select`, `tpl_public_read`), nunca digitado pelo usuário em modo template | Modelo de template existe justamente pra não expor conceitos técnicos — pedir um nome de policy (que seguidor de `identRe`) reintroduziria a fricção que o recurso resolve | y — decisão técnica, sem ambiguidade de produto a validar |
| Colisão de nome gerado com policy já existente na mesma `(app, table, action)` | Sistema tenta o nome padrão; se o backend rejeitar por unicidade (mesmo erro que o formulário avançado já trata), a UI mostra a mensagem de conflito já existente e sugere modo avançado (renomear manualmente) | Reaproveita o tratamento de erro de unicidade que já existe hoje no formulário de criação — sem novo código de resolução de conflito | y — decisão técnica, sem ambiguidade de produto a validar |
| Quais templates aparecem pra uma tabela específica | Templates 1, 3, 6 (que referenciam `owner_id`) só aparecem quando `config.HasOwnerColumn(rls)` é verdadeiro pra tabela; templates 2, 4, 5 e a affordance 7 aparecem sempre que a tela de Table Policies está visível (RLS habilitado) | `owner_id` não existe como coluna referenciável fora desse conjunto de modos (`internal/provisioner/policy.go:156-162`) — oferecer um template que gera cláusula inválida pra tabela quebraria na primeira tentativa | y — decisão técnica, sem ambiguidade de produto a validar |
| Onde o toggle "Modo avançado" mora na tela | Ao lado do botão "Ajuda", no cabeçalho do fluxo de criação de policy — visível tanto na visão de lista de templates quanto dentro do formulário técnico (permite alternar nos dois sentidos sem perder o que já foi preenchido, quando os campos preenchidos ainda são válidos no outro modo) | Decisão de produto já fechada com o usuário antes desta spec formal | y — confirmado pelo usuário |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Usuário aplica um template de policy de ação única ⭐ MVP

**User Story**: Como usuário não-técnico do Dashboard, eu quero escolher um template em linguagem simples ("Só o dono vê", "Todos autenticados podem ver", etc.) pra criar uma table policy, para que eu não precise entender Column/Operator/ValueSource/Logic pra configurar os casos mais comuns.

**Why P1**: É o valor central da feature — sem isso, nada muda pro usuário não-técnico.

**Acceptance Criteria**:

1. WHEN o usuário abre a tela de criar policy numa tabela com `config.HasOwnerColumn(rls)` verdadeiro THEN o sistema SHALL exibir, entre outras, as opções de template: "Só o dono vê/edita" (PTPL-01), "Todos autenticados (papel selecionado) podem ver" (PTPL-02), "Ninguém edita, só leitura" (PTPL-04), "Visível quando valor bate" (PTPL-05), e o item explicativo não-acionável "Bloqueado por padrão" (PTPL-07).
2. WHEN o usuário abre a tela de criar policy numa tabela com `config.HasOwnerColumn(rls)` falso (mas RLS habilitado — modo `policy` sem coluna de dono não existe hoje, então este caso é apenas defensivo) THEN o sistema SHALL ocultar os templates que dependem de `owner_id` (PTPL-01, PTPL-03, PTPL-06).
3. WHEN o usuário seleciona o template "Só o dono vê/edita" e escolhe uma ou mais ações (select/insert/update/delete) e ao menos um papel THEN o sistema SHALL criar, para cada ação escolhida, uma policy com `Clauses = [{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"}]` e os `Roles` selecionados — sem exibir Column/Operator/ValueSource ao usuário.
4. WHEN o usuário seleciona o template "Todos autenticados (papel selecionado) podem ver" e escolhe um papel THEN o sistema SHALL criar uma policy de `Action = "select"` com `Roles` = papel(is) selecionado(s) e `Clauses = [{Column: "owner_id", Operator: "IS NOT NULL"}]` (cláusula sempre-verdadeira, nunca exibida como tal na UI de template).
5. WHEN o usuário seleciona o template "Ninguém edita, só leitura" THEN o sistema SHALL criar exatamente uma policy com `Action = "select"`, sem oferecer seleção de outras ações nesse template.
6. WHEN o usuário seleciona o template "Visível quando valor bate" e informa uma coluna real da tabela (dropdown das colunas existentes) e um valor literal THEN o sistema SHALL criar uma policy de `Action = "select"` com `Clauses = [{Column: <coluna>, Operator: "=", ValueSource: "literal", Value: <valor>}]`.
7. WHEN qualquer template gera uma `PolicyDef` THEN o sistema SHALL enviá-la ao mesmo endpoint `POST /dashboard/api/apps/{id}/tables/{table}/policies` já usado pelo modo avançado, sem novo endpoint.
8. IF a criação de uma policy gerada por template falhar (validação do backend, conflito de nome/unicidade, erro de rede) THEN o sistema SHALL exibir a mensagem de erro já retornada pelo backend (via `toast.error`, `AGENTS.md` §5) e não deixar a tela em estado inconsistente (loading infinito, formulário travado).
9. WHEN o usuário clica no item "Bloqueado por padrão" (PTPL-07) THEN o sistema SHALL exibir um texto explicativo (não um formulário) informando que, sem nenhuma policy, a tabela nega todo acesso de usuário final por padrão — sem chamar o endpoint de criação.

**Independent Test**: Numa tabela `rls: "policy"` com coluna `owner_id`, aplicar o template "Só o dono vê/edita" pra `Action = select` e papel `member`; confirmar via `pg_policies` que a policy criada tem exatamente a cláusula `"owner_id" = current_setting('app.jwt_sub', true)::uuid` combinada com o check de role, e que a UI nunca mostrou os campos Column/Operator/ValueSource durante o fluxo.

---

### P2: Usuário aplica o template composto de leitura aberta + escrita restrita ao dono

**User Story**: Como usuário não-técnico, eu quero um único template que deixe a leitura aberta pro papel que eu escolher mas restrinja escrita (update/delete) só ao dono da linha, para que eu não precise entender que isso exige criar 3 policies separadas manualmente.

**Why P2**: Cobre um padrão comum (post público, só autor edita) que hoje exige 3 idas ao formulário avançado; menos frequente que os templates de ação única de P1, mas ainda um caso real do domínio (conteúdo do Orbit — blog-demo).

**Acceptance Criteria**:

1. WHEN o usuário seleciona o template "Leitura liberada, escrita só do dono" e escolhe o(s) papel(is) de leitura THEN o sistema SHALL criar, em sequência: (a) uma policy `Action = "select"` sem cláusula de dono (mesma cláusula dummy de PTPL-02) com os papéis de leitura escolhidos, (b) uma policy `Action = "update"` com `Clauses = [{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"}]`, (c) uma policy `Action = "delete"` com a mesma cláusula de dono — cada uma via uma chamada `POST` separada ao endpoint existente.
2. IF a chamada (a) tiver sucesso mas (b) falhar THEN o sistema SHALL parar a sequência, manter a policy (a) já criada, e informar ao usuário exatamente quais das 3 policies foram criadas e qual falhou, com o motivo do erro.
3. WHEN o usuário reaplica o mesmo template na mesma tabela após uma falha parcial anterior THEN o sistema SHALL tentar criar apenas as policies (select/update/delete) que ainda não existem para aquela combinação de action, sem re-tentar criar as que já foram confirmadas como existentes.

**Independent Test**: Aplicar o template composto numa tabela de teste; interromper a rede (ou forçar erro) entre a 1ª e a 2ª chamada; confirmar que a policy de `select` existe em `pg_policies`, que a UI relata "1 de 3 criadas, falhou em: update" com o motivo, e que reaplicar o template cria só `update` e `delete` (pula `select`).

---

### P3: Usuário consulta o drawer de ajuda pra montar uma policy avançada

**User Story**: Como usuário que precisa de uma combinação que nenhum template cobre, eu quero abrir um drawer de ajuda com exemplos reais de policies avançadas, para que eu consiga usar o formulário técnico sem precisar perguntar pra alguém do time técnico.

**Why P3**: Complementa P1/P2 — sem isso, qualquer caso fora dos templates volta a exigir conhecimento tácito de RLS que o usuário não-técnico não tem.

**Acceptance Criteria**:

1. WHEN o usuário clica no botão "Ajuda" na tela de Table Policies THEN o sistema SHALL abrir um drawer com conteúdo explicativo, sem fechar ou descartar o formulário/seleção de template em andamento por trás dele.
2. THE drawer SHALL conter ao menos 3 exemplos completos de policy avançada (coluna, operador, claim/literal, lógica AND/OR quando aplicável), cada um usando exclusivamente operadores da allowlist real (`=`, `!=`, `IN`, `NOT IN`, `>`, `<`, `>=`, `<=`, `IS NULL`, `IS NOT NULL`) e claims reais (`role`, `sub`, `email`) — nenhum exemplo referencia `LIKE`, funções SQL (`now()`, etc.) ou claim fora desse conjunto.
3. WHEN o usuário fecha o drawer THEN o sistema SHALL preservar o estado do formulário/template que estava em andamento antes de abrir o drawer.
4. Todo texto do drawer (títulos, explicações, exemplos) SHALL existir em `src/locales/en.json` e `src/locales/pt-BR.json` (`AGENTS.md` §5).

**Independent Test**: Abrir o drawer com um rascunho de cláusula já preenchido no formulário avançado; fechar o drawer; confirmar que o rascunho não foi perdido; confirmar que os 3+ exemplos do drawer usam apenas operadores/claims da allowlist real.

---

## Edge Cases

- IF a tabela não tem nenhuma policy ainda (estado inicial, RLS habilitado, zero policies) THEN a tela SHALL exibir o item "Bloqueado por padrão" (PTPL-07) de forma visível, não escondido atrás de um clique extra, já que é o estado de segurança real da tabela nesse momento.
- IF o usuário alterna de "modo template" pra "Modo avançado" com um template parcialmente preenchido THEN o sistema SHALL tentar pré-popular o formulário avançado com o que já foi escolhido (ex.: papéis selecionados), sem perder o trabalho já feito — quando a tradução direta não for possível (ex.: template ainda sem coluna escolhida), o campo correspondente fica vazio no modo avançado.
- IF a tabela não tem nenhuma coluna elegível pro template "Visível quando valor bate" (situação hipotética, toda tabela tem ao menos as colunas do usuário) THEN o dropdown de colunas SHALL mostrar todas as colunas reais da tabela, sem incluir `owner_id` nesse template específico (esse template não é sobre dono).
- IF o usuário usa o template "Só o dono vê/edita" pra uma ação que já tem uma policy do mesmo nome gerado automaticamente THEN o sistema SHALL tratar como o conflito de unicidade já tratado pelo formulário avançado (ver Assumptions) — não criar uma policy duplicada silenciosamente.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PTPL-01 | P1: Templates de ação única | Execute | Done (T2, T6, T8, T9) |
| PTPL-02 | P1: Templates de ação única | Execute | Done (T2, T6, T8, T9) |
| PTPL-03 | P1: Templates de ação única | — | Unused — referenced only in Assumptions, never in a template/builder. Not implemented; see T9's findings note in `tasks.md` |
| PTPL-04 | P1: Templates de ação única | Execute | Done (T2, T6, T8, T9) |
| PTPL-05 | P1: Templates de ação única | Execute | Done (T2, T6, T8, T9) |
| PTPL-06 | P2: Template composto | Execute | Done (T3, T7, T8, T9) |
| PTPL-07 | P1: Affordance bloqueado por padrão | Execute | Done (T2, T6, T8, T9) |
| PTPL-08 | P3: Drawer de ajuda | Execute | Done (T5, T8, T9) |
