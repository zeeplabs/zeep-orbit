# Table Policy Edit Specification

## Problem Statement

O dashboard só permite criar (`POST`) ou deletar (`DELETE`) uma table policy (RLS) — não existe `PUT`/edição. Qualquer ajuste (trocar uma cláusula, adicionar uma role, corrigir um operador) exige deletar a policy e recriar do zero, perdendo o histórico de quem criou e reabrindo uma janela sem enforcement entre o delete e o create. Esse gap também deixa `ROLECFG-16` (spec `enduser-roles-config`, comportamento de "role órfã" num chip de policy já persistida) sem cenário real pra testar — hoje uma policy nunca é editada, só recriada.

## Goals

- [ ] Admin edita uma table policy existente via `PUT /dashboard/api/apps/{id}/tables/{table}/policies/{policyId}`, reaproveitando a mesma validação e o mesmo `BuildPolicySQL` já usados em `CreateTablePolicy`.
- [ ] A edição substitui a policy nativa do Postgres inteira (`DROP POLICY` do nome atual + `CREATE POLICY` com os dados novos) e atualiza a linha do catálogo (`roles`, `clauses`, `action`, `pg_policy_name`) numa única transação — nunca um estado parcial (só DROP sem CREATE, ou só catálogo sem policy nativa).
- [ ] `zeep_system.table_policies` ganha `updated_at`/`updated_by`, preenchidos em toda edição bem-sucedida (mesma convenção de `created_at`/`created_by` já existente).
- [ ] `TablePolicies.tsx` ganha um botão "Editar" por policy (ao lado do botão de delete já existente), que abre o mesmo formulário de criação, pré-populado com `action`/`roles`/`clauses` da policy atual — incluindo qualquer role fora da lista `enduser_roles_config` do app, exibida como chip selecionado (torna `ROLECFG-16` finalmente testável).

## Out of Scope

Explicitamente excluído desta spec. Documentado pra prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Histórico/versionamento de edições (diff de cada mudança, quem mudou o quê campo a campo) | `updated_at`/`updated_by` cobre "quem editou por último, quando" — um log de auditoria completo é escopo maior, sem caso motivador concreto além do gap atual. |
| Lock otimista / edição concorrente | Mesma semântica já aceita no resto do projeto (last-write-wins) — nenhuma policy tem alto volume de edição simultânea esperado. |
| `ALTER POLICY` nativo do Postgres em vez de `DROP`+`CREATE` | `ALTER POLICY` não cobre troca de `FOR <action>`/`TO <role>` — só `USING`/`WITH CHECK`. Decisão do usuário: sempre `DROP`+`CREATE`, mesmo quando só a cláusula muda, pra não ter dois caminhos de código (simples/complexo) pra manter. |
| Editar a allowlist de colunas/operadores disponíveis no formulário | Mesma allowlist de `CreateTablePolicy`, inalterada — esta spec só adiciona o caminho de edição, não muda o que pode ser expresso numa policy. |
| Reabilitar RLS automaticamente se a última policy for editada para remover todas as roles | Fora do escopo motivador; `DeleteTablePolicy` já documenta que nunca desliga RLS automaticamente (mesma decisão se estende aqui). |

---

## Assumptions & Open Questions

Toda ambiguidade está resolvida ou registrada aqui — nada fica silenciosamente indefinido.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Estratégia de aplicação da edição no Postgres | Sempre `DROP POLICY` (nome atual) + `CREATE POLICY` (dados novos), na mesma transação da atualização do catálogo | Confirmado pelo usuário em 2026-08-08; evita matriz de casos (ALTER quando possível, DROP+CREATE quando não) — `ALTER POLICY` não cobre troca de `action`/`roles` de qualquer forma, então o caminho simples cobre 100% dos casos com uma implementação só | y — confirmado pelo usuário em 2026-08-08 |
| Auditoria de edição | Adicionar `updated_at TIMESTAMPTZ`/`updated_by UUID REFERENCES dashboard_users(id)` em `zeep_system.table_policies`, preenchidos em toda edição | Confirmado pelo usuário em 2026-08-08; sem isso uma policy editada 5 vezes é indistinguível de uma nunca editada — mesma convenção já usada por `created_at`/`created_by` | y — confirmado pelo usuário em 2026-08-08 |
| Renomear a policy (`name` → `pg_policy_name`) durante a edição | Permitido livremente; o handler dropa o nome ANTIGO (persistido no catálogo antes do update) e cria com o nome NOVO | Sem isso, editar o nome deixaria a policy antiga órfã no Postgres (nunca dropada) — o catálogo sempre sabe o nome atual antes da escrita, então dropar por esse valor é seguro e não exige campo adicional no payload | y — decisão técnica, sem ambiguidade de produto a validar |
| Conflito de unicidade (`app_id, table_name, action, pg_policy_name`) após a edição | Se a combinação nova colidir com outra policy existente (diferente da que está sendo editada), rejeitar com 409 antes de tocar no Postgres | Mesma constraint já existente no banco — decisão é só sobre como o handler traduz a violação em resposta HTTP, mesmo padrão de erro já usado em outros conflitos de unicidade do dashboard | y — decisão técnica, sem ambiguidade de produto a validar |
| Edição "vazia" (admin abre, não muda nada, salva) | Ainda executa `DROP`+`CREATE` e atualiza `updated_at`/`updated_by` — sem diff-check pra pular a operação | Fora do escopo motivador otimizar esse caso raro; manter um caminho de código único (sempre a mesma sequência) é mais simples e não tem custo perceptível (operação de admin, não de hot path de request de usuário final) | y — decisão técnica, sem ambiguidade de produto a validar |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Admin edita uma table policy existente ⭐ MVP

**User Story**: Como admin de um app, eu quero editar uma table policy já criada (trocar roles, cláusulas ou action) sem precisar deletar e recriar do zero, para que eu não perca o rastro de quando a policy foi originalmente criada e não abra uma janela sem enforcement entre delete e create.

**Why P1**: É a única story da feature — sem ela, a edição continua não existindo.

**Acceptance Criteria**:

1. WHEN admin submete `PUT /dashboard/api/apps/{id}/tables/{table}/policies/{policyId}` com `{name, action, roles, clauses}` válidos THEN o sistema SHALL, numa única transação: `DROP POLICY` do `pg_policy_name` atual (lido do catálogo antes do update), executar o `CREATE POLICY` gerado por `BuildPolicySQL` com os dados novos, e `UPDATE` a linha do catálogo (`roles`, `clauses`, `action`, `pg_policy_name`, `updated_at`, `updated_by`).
2. IF o payload falha a mesma validação já usada em `CreateTablePolicy` (formato de role via `identRe`, coluna/operador fora da allowlist, `action` fora do enum) THEN o sistema SHALL retornar 400 sem executar `DROP`/`CREATE` no Postgres (falha antes de qualquer mutação).
3. IF `policyId` não existe ou não pertence ao `app_id` da URL THEN o sistema SHALL retornar 404.
4. IF a combinação `(app_id, table_name, action, pg_policy_name)` resultante colide com outra policy já existente (diferente da que está sendo editada) THEN o sistema SHALL retornar 409, sem aplicar a mutação.
5. IF o `DROP POLICY` ou o `CREATE POLICY` falhar dentro da transação (ex.: dessincronia entre catálogo e Postgres) THEN o sistema SHALL abortar a transação inteira (nenhuma mutação parcial no catálogo nem no Postgres) e retornar 500 com mensagem genérica (nunca `err.Error()` bruto, regra do `AGENTS.md`).
6. WHEN admin abre o formulário de edição de uma policy existente na UI THEN o sistema SHALL pré-popular `action`, `roles` (incluindo roles fora de `enduser_roles_config`, exibidas como chip selecionado) e todas as `clauses` com os valores atuais da policy.
7. WHEN admin confirma a edição no formulário THEN o sistema SHALL chamar o novo endpoint `PUT` (em vez de `POST`) e a lista de policies na tela SHALL refletir os dados atualizados sem reload manual da página.
8. The system SHALL preencher `updated_at`/`updated_by` em toda edição bem-sucedida, mesmo quando o payload submetido é idêntico ao estado anterior da policy.

**Independent Test**: Criar uma policy com `roles=["member"]`, editá-la via UI trocando pra `roles=["member","admin"]`, confirmar que a policy nativa do Postgres (`pg_policies`) reflete o novo `USING`, que o catálogo mostra `updated_at` preenchido, e que uma tentativa de editar um `policyId` inexistente retorna 404.

---

## Edge Cases

- IF admin edita uma policy pra usar uma role que não está em `enduser_roles_config` (role "órfã") THEN o sistema SHALL aceitar (mesma regra de defesa em profundidade da spec `enduser-roles-config` — UI não bloqueia, backend só valida formato via `identRe`) e a UI SHALL exibir essa role como chip selecionado ao reabrir a policy pra editar de novo.
- IF dois admins editam a mesma policy concorrentemente THEN o sistema SHALL aplicar last-write-wins (sem lock otimista, ver Assumptions) — a segunda escrita vence.
- IF a policy sendo editada é a única da tabela e a edição não altera `roles`/`action` de forma que remova todas as roles THEN RLS permanece habilitado, sem mudança de comportamento em relação ao estado atual.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| TPEDIT-01 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-02 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-03 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-04 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-05 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-06 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-07 | P1: Admin edita policy existente | Execute | Verified |
| TPEDIT-08 | P1: Admin edita policy existente | Execute | Verified |

**ID format:** `TPEDIT-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 8 total, 8 mapped to tasks (T1–T7 em `tasks.md`), 0 unmapped. Mapeamento: TPEDIT-01 → T1/T2/T3/T4/T5; TPEDIT-02..05 → T2/T3; TPEDIT-06/07 → T6/T7; TPEDIT-08 → T2/T5.

**Implementação: concluída e verificada (2026-08-10)** — T1–T7 implementados (commits `16650bf`..`df86f21`), gate independente rodado contra Postgres real (não só `go build`/skip): `go test ./internal/dashboard/...` com `TEST_DATABASE_URL` apontando pra um Postgres 16 descartável passou as 9 novas suites de `UpdateTablePolicy`. Ver `.specs/features/table-policy-edit/validation.md` para o relatório completo.

---

## Success Criteria

- [ ] Editar uma policy nunca deixa o Postgres e o catálogo dessincronizados (ou os dois mudam, ou nenhum muda).
- [ ] `ROLECFG-16` (spec `enduser-roles-config`) passa a ter um cenário real pra testar: editar uma policy pra ter uma role órfã, reabrir o form, confirmar que ela aparece como chip selecionado.
- [ ] Nenhuma regressão em `CreateTablePolicy`/`DeleteTablePolicy` — endpoints e comportamento existentes inalterados.
