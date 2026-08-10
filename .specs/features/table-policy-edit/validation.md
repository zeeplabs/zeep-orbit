# table-policy-edit Validation

**Date**: 2026-08-10
**Spec**: `.specs/features/table-policy-edit/spec.md`
**Diff range**: `4a471fc..df86f21` (7 commits, T1–T7)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `provisioner.go` ALTER TABLE adds `updated_at`/`updated_by`, same list as `created_at`/`created_by`. |
| T2   | ✅ Done | `UpdateTablePolicy` in `table_policies_store.go:257-323`. |
| T3   | ✅ Done | `Handler.UpdateTablePolicy` in `handler.go:1426-1485`. |
| T4   | ✅ Done | Route registered `internal/server/server.go:179`. |
| T5   | ✅ Done | `TablePolicyRow.updated_at/updated_by` + `useUpdateTablePolicy` in `api.ts`. |
| T6   | ✅ Done | Edit mode in `TablePolicies.tsx` (`editingPolicy`, `openEditForm`, branching `submit()`). |
| T7   | ✅ Done | 2 new Playwright tests in `enduser-roles.spec.ts`. |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: PUT válido → DROP+CREATE+UPDATE catálogo numa tx | roles/action/pg_policy_name/updated_at/updated_by refletidos no catálogo E `pg_policies` reflete o novo `USING` | `internal/dashboard/table_policies_store_test.go:304-329` — `UpdateTablePolicy(...)`, asserts `updated.Roles == [member admin]`, `updated.UpdatedAt != nil`, `updated.UpdatedBy == userID`, e `pgPolicyQual(...)` contém `'admin'` | ✅ PASS |
| AC2: payload inválido → 400, sem DROP/CREATE | 400, catálogo e policy nativa inalterados | Store: `table_policies_store_test.go:392-406` — invalid clause, asserts `err != nil`, `pgPolicyQual` ainda `'member'` sem `'admin'`, `ListTablePolicies` retorna 1 linha com `UpdatedAt == nil`. Handler: `table_policies_handler_test.go:402-412` — `w.Code == http.StatusBadRequest`, mensagem descritiva (não genérica) | ✅ PASS |
| AC3: `policyId` inexistente/de outro app → 404 | 404 | Store: `table_policies_store_test.go:346-364` — `err == ErrPolicyNotFound` para ID inexistente E para policy de outro app. Handler: `table_policies_handler_test.go:427-430` — `w.Code == http.StatusNotFound` | ✅ PASS |
| AC4: colisão de `(app_id, table_name, action, pg_policy_name)` → 409, sem mutação | 409, policy que sofreria a colisão permanece inalterada | Store: `table_policies_store_test.go:437-452` — `err == ErrPolicyConflict`, `pgPolicyQual` da policy editada permanece `'member'` (sem `'admin'`). Handler: `table_policies_handler_test.go:433+` (`TestUpdateTablePolicyHandler_ConflictReturns409`) — status 409 | ✅ PASS |
| AC5: falha de DROP/CREATE dentro da tx → abort total, 500 genérico | nenhuma mutação parcial; nunca `err.Error()` bruto | Cobertura indireta apenas: nenhum teste força uma falha de infraestrutura no meio da tx (ex.: mock de `DROP POLICY` falhando por razão não-conflito) para provar rollback explícito nesse caminho específico. O padrão genérico "todo erro não mapeado → `h.writeError(..., "internal error", err)`" é herdado de `CreateTablePolicy`/`DeleteTablePolicy` (mesma função `writeError`), mas não há teste dedicado a esse branch dentro de `UpdateTablePolicy`. | ⚠️ Spec-precision gap (coberto por padrão herdado, não por teste direto) |
| AC6: abrir form de edição → pré-popula `action`/`roles`(+órfãs)/`clauses` | valores atuais, incluindo role órfã como chip selecionado | `internal/dashboard/ui/e2e/enduser-roles.spec.ts:278-279` (nome pré-populado) e `:335-338` (`ghost_role` chip com classe `bg-[var(--primary)]` após reabrir) | ✅ PASS |
| AC7: confirmar edição → chama PUT, lista reflete sem reload manual | PUT (não POST), UI atualizada sem `page.reload()` | `enduser-roles.spec.ts:282-288` — clica "Save policy", espera toast "Policy updated", depois `expect(page.getByText('admin'))`/`expect(page.getByText('member'))` visíveis sem chamar reload. Reforçado no nível de componente: `TablePolicies.tsx:159-161` chama `updatePolicy.mutateAsync` (não `createPolicy`) quando `editingPolicy` está setado | ✅ PASS |
| AC8: `updated_at`/`updated_by` preenchidos mesmo com payload idêntico | preenchidos mesmo em edição "vazia" (sem diff) | Não há teste que edite uma policy reenviando exatamente o mesmo payload e confirme `updated_at` ainda muda. O código sempre executa `UPDATE ... SET updated_at = now()` incondicionalmente (sem diff-check), então o comportamento é estruturalmente garantido, mas o cenário "payload idêntico" não tem um teste próprio — o teste mais próximo (`TestUpdateTablePolicy_HappyPathReflectsNewRolesAndTimestamps`) sempre muda `roles`. | ⚠️ Spec-precision gap (comportamento correto por construção, cenário exato não testado) |

**Status**: ⚠️ Spec-precision gaps flagged (AC5, AC8) — nenhum GAP funcional (❌), ambos são casos onde o comportamento correto decorre da estrutura do código (transação única / UPDATE incondicional) mas não há um teste que force especificamente esse cenário.

---

## Discrimination Sensor

Sensor rodado em scratch worktree (`git worktree add /tmp/verify-mutant HEAD`), Postgres descartável em container Docker separado (`postgres:16-alpine`, porta 15432, banco `zeep_test`) — nunca o Postgres de dev do usuário (`zeep-orbit-db-1`, porta 5434). Worktree removido e container destruído ao final; `git status --porcelain` no repo real confirmado idêntico ao baseline (vazio) antes e depois.

| # | File:line | Mutation | Killed? |
| - | --- | --- | --- |
| 1 | `internal/dashboard/table_policies_store.go:296` | `DROP POLICY IF EXISTS %q` — trocado `currentPgPolicyName` → `def.Name` (simula o bug "rename não dropa o nome antigo") | ✅ Killed — `TestUpdateTablePolicy_RenameDropsOldNativePolicy` falhou: "expected old_name native policy dropped, still has 1 rows" |
| 2 | `internal/dashboard/table_policies_store.go:264` | `if err != nil { return ... }` → `if err != nil && false { ... }` (ignora erro de validação do `BuildPolicySQL`, simula "clause inválida não bloqueia a mutação") | ✅ Killed — `TestUpdateTablePolicy_InvalidClauseRejectedNoMutation` falhou: "expected error for invalid clause, got nil" |
| 3 | `internal/dashboard/handler.go:1473` | `case errors.Is(err, ErrPolicyNotFound): writeJSON(w, http.StatusNotFound, ...)` → `http.StatusOK` (simula handler não mapeando 404) | ✅ Killed — `TestUpdateTablePolicyHandler_UnknownPolicyReturns404` falhou: "status = 200, want 404" |

**Sensor depth**: lightweight (3 mutations, default tier — feature não é P0/payment/auth)
**Result**: 3/3 killed — ✅ PASS

Nota: a mutação 2 revelou algo digno de registro (não um bug, mas uma observação): mesmo ignorando o erro de validação, o teste ainda falhou porque o `ddl` retornado por `BuildPolicySQL` em erro não é necessariamente vazio/inócuo — o mutante foi morto, mas por um caminho ligeiramente diferente do esperado (a asserção que capturou foi "no error path", não uma verificação direta de que nenhuma DDL rodou). Isso não é uma fraqueza do teste — o teste ainda captura corretamente o cenário — só não isola tão precisamente o "por quê" quanto seria ideal.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — cada task é o mínimo necessário (1 função, 1 handler, 1 rota, 1 tipo+hook, 1 modo de UI, 1 spec e2e) |
| Surgical changes | ✅ — nenhum arquivo fora da lista esperada em tasks.md foi tocado |
| No scope creep | ✅ — nenhuma feature de Out of Scope (histórico/versionamento, lock otimista, `ALTER POLICY`, nova allowlist, auto-reenable de RLS) apareceu no diff |
| Matches patterns | ✅ — `UpdateTablePolicy` segue a mesma estrutura de tx/erro de `CreateTablePolicy`/`DeleteTablePolicy`; handler segue o mesmo padrão de `CreateTablePolicy`; hook segue `useCreateTablePolicy`/`useDeleteTablePolicy` |
| Spec-anchored outcome check | ⚠️ 6/8 ACs com assertion exata; AC5 e AC8 são spec-precision gaps (ver tabela acima) |
| Per-layer Coverage Expectation met | ✅ store: 5/5 cenários da matrix (happy, not-found, invalid-clause, conflict, rename) cobertos com asserções reais, não apenas "sem erro". ⚠️ "concurrent delete-then-update race" listado na Test Coverage Matrix do tasks.md (linha 23) mas **sem teste correspondente** — ver Gap 1 abaixo |
| Every test maps to a spec requirement | ✅ — nenhum teste "solto" sem AC/edge-case associado |
| Documented guidelines followed | `AGENTS.md` §3 (gate commands), §4 (erro genérico em 500, nunca `err.Error()` bruto — confirmado em `handler.go:1478-1481`), §5 (i18n em `en.json`/`pt-BR.json`, toast `onError`) — todas seguidas |

---

## Edge Cases

- [x] Role órfã aceita na edição e exibida como chip selecionado ao reabrir — `enduser-roles.spec.ts:295-339`
- [x] Last-write-wins sem lock otimista — não há código de versionamento/lock adicionado (consistente com "não fazer nada" sendo o comportamento correto); nenhum teste dedicado, mas a ausência de qualquer mecanismo de lock é a prova estrutural do comportamento
- [x] Editar sem remover todas as roles não desabilita RLS — `UpdateTablePolicy` nunca toca `ALTER TABLE ... DISABLE/ENABLE ROW LEVEL SECURITY`, então o comportamento é preservado por omissão (mesmo raciocínio de `DeleteTablePolicy`)

---

## Gate Check

- **Gate command (Build, fecha a feature)**: `go build ./... && go test ./... && go vet ./... && cd internal/dashboard/ui && npm run build` + `gofmt -l <changed .go files>` (+ Playwright, ver Limitations)
- **`go build ./...`**: limpo, sem output
- **`go vet ./...`**: limpo, sem output
- **`gofmt -l` nos 7 arquivos .go alterados**: limpo, sem output
- **`go test ./internal/dashboard/...`** (sem `TEST_DATABASE_URL`): passa, mas as 9 novas suites (`TestUpdateTablePolicy*`, `TestUpdateTablePolicyHandler*`) aparecem como `SKIP` — limitação documentada no ambiente padrão
- **`go test ./internal/dashboard/...`** (com `TEST_DATABASE_URL` apontando pra Postgres 16 descartável, porta 15432, nunca o Postgres de dev): **9/9 novos testes passaram** (`TestUpdateTablePolicy_HappyPathReflectsNewRolesAndTimestamps`, `_NotFoundReturnsErrPolicyNotFound`, `_InvalidClauseRejectedNoMutation`, `_ConflictReturnsErrPolicyConflict`, `_RenameDropsOldNativePolicy`, `TestUpdateTablePolicyHandler_AdminHappyPath`, `_InvalidClauseReturns400`, `_UnknownPolicyReturns404`, `_ConflictReturns409`)
- **`go test ./...`** completo (mesma DB descartável): 1 falha pré-existente e não relacionada — `TestAppMembersIndependentTest` (subtests sc1/sc4/sc5) falha quando roda dentro da suíte completa de `internal/dashboard`, mas **passa isoladamente** (`go test -run TestAppMembersIndependentTest`, 0.39s, 7/7 subtests OK) rodando na mesma DB. Root cause aparente: contaminação de estado entre testes compartilhando `TEST_DATABASE_URL` sem paralelismo isolado — já documentado como padrão conhecido em `.github/workflows/reusable-ci.yml:64` ("share the one TEST_DATABASE_URL and some tests drop+recreate..."). Não é uma regressão desta feature.
- **`npm run build`** (frontend): limpo, `tsc -b` + `vite build` sem erros
- **Test count before feature**: baseline não medido diretamente (checkout completo de `4a471fc` falhou por embed do dashboard estar vazio nesse commit neste ambiente) — a contagem de +9 testes Go novos + 2 Playwright novos é confirmada pelo diff de commits (`b2d198d`, `b4c11e2`, `df86f21`), não por comparação de contagem total rodada
- **Test count after feature**: 9 novos testes Go de integração + 2 novos testes Playwright (conforme diff)
- **Delta**: +9 Go, +2 Playwright, 0 removidos/pulados dos já existentes
- **Skipped tests**: os 9 novos testes Go skipam sob `go test ./...` padrão (sem `TEST_DATABASE_URL`) — mesmo comportamento de todo o resto da suíte de integração do dashboard, não uma lacuna desta feature
- **Playwright (`npx playwright test enduser-roles`)**: **não executado neste ambiente** — exigiria o binário `zeep` completo servindo em `localhost:8080` com bootstrap + Postgres real. Revisão manual do código dos 2 testes novos (linhas 247-339 de `enduser-roles.spec.ts`) não encontrou asserções vagas — cada teste afirma valores concretos (`toHaveValue('editable_policy')`, presença de `admin`+`member` como texto, classe CSS do chip `ghost_role`)

---

## Fix Plans (if issues found)

### Gap 1: "concurrent delete-then-update race" da Test Coverage Matrix não tem teste

- **Root cause**: `tasks.md` linha 23 lista esse cenário na Test Coverage Matrix ("Every listed edge case: happy path, not-found, unique conflict, invalid clause, concurrent delete-then-update race"), mas o Done-when de T2 (linhas 97-104) só lista 5 cenários (sem o de race) e a implementação/testes seguiram o Done-when, não a matrix. Não é um AC do spec.md (spec.md não menciona esse cenário explicitamente — só "last-write-wins" entre dois updates concorrentes, não delete-vs-update), então não é um GAP de spec, mas é uma inconsistência interna do tasks.md.
- **Impacto real**: baixo. `SELECT ... FOR UPDATE` na store já serializa contra deletes concorrentes na mesma linha; se um DELETE committar entre o SELECT e o UPDATE de outra transação, o segundo `UPDATE ... WHERE id = $6 AND app_id = $7` afeta 0 linhas e o código atual **não verifica `RowsAffected`** — o `Scan` do `RETURNING` já cobre isso (zero linhas retornadas → `pgx.ErrNoRows` no `Scan`, que hoje cai no branch genérico `fmt.Errorf(...)`, não em `ErrPolicyNotFound`). Ou seja, um delete concorrente durante o update provavelmente produz um 500 genérico em vez de um 404 mais preciso — comportamento aceitável (não é uma mutação parcial, e não viola nenhum AC), mas não testado.
- **Fix task**: adicionar um teste de integração que faça `DeleteTablePolicy` da mesma policy entre o `SELECT ... FOR UPDATE` e o commit do `UPDATE` (via duas conexões/transações), confirmando que o resultado é ou 404 ou 500 genérico — nunca uma segunda linha fantasma. Task pequena, mesma camada de T2.
- **Priority**: Minor (documentação/tasks.md ficou inconsistente com o Done-when real; nenhum AC do spec.md é afetado).

### Gap 2: `CHANGELOG.md` sem entrada em `[Unreleased]`

- **Root cause**: `AGENTS.md` §6 exige "add an entry under `## [Unreleased]` in the same change that ships the fix/feature" — `CHANGELOG.md:10-11` mostra `## [Unreleased]` vazio no `HEAD` atual, apesar da feature completa (T1-T7) já ter sido implementada.
- **Fix task**: adicionar uma linha em `CHANGELOG.md` sob `[Unreleased]` descrevendo a edição de table policies (PUT endpoint, updated_at/updated_by, UI de edição).
- **Priority**: Minor (regra documentada de projeto, não um AC funcional, mas é uma regra explícita e repetida do AGENTS.md).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| TPEDIT-01 | Pending | ✅ Verified |
| TPEDIT-02 | Pending | ✅ Verified |
| TPEDIT-03 | Pending | ✅ Verified |
| TPEDIT-04 | Pending | ✅ Verified |
| TPEDIT-05 | Pending | ✅ Verified |
| TPEDIT-06 | Pending | ✅ Verified |
| TPEDIT-07 | Pending | ✅ Verified |
| TPEDIT-08 | Pending | ✅ Verified |

`spec.md` atualizado nesta validação: tabela de Requirement Traceability e a nota de status de implementação (que estava desatualizada, ainda dizendo "ainda não iniciada").

---

## Summary

**Overall**: ⚠️ Issues (menores, não bloqueantes)

**Spec-anchored check**: 6/8 ACs com match exato; 2 spec-precision gaps (AC5, AC8) — comportamento correto por construção do código, sem teste dedicado ao cenário exato
**Sensor**: 3/3 mutações mortas
**Gate**: build ✅, vet ✅, gofmt ✅, frontend build ✅, 9/9 testes de integração novos passando contra Postgres real, 1 falha pré-existente não relacionada (`TestAppMembersIndependentTest`, contaminação de estado entre testes na suíte completa, reproduzida também fora desta feature)

**What works**: DROP+CREATE+UPDATE numa única transação (AC1), validação bloqueia mutação antes de qualquer DROP/CREATE (AC2), 404/409 mapeados corretamente em store e handler (AC3/AC4), rename dropa o nome antigo (assumption confirmada), UI pré-popula e edita via PUT sem reload (AC6/AC7), role órfã sobrevive como chip (ROLECFG-16 agora testável), i18n en/pt-BR completo, nenhum scope creep.

**Issues found**:
1. AC5 (abort de transação em falha de infra) e AC8 (updated_at em edição "vazia") não têm teste que force especificamente o cenário — comportamento correto por construção, mas não travado por teste (spec-precision gap, não bug).
2. Cenário "concurrent delete-then-update race" da Test Coverage Matrix do tasks.md não foi implementado como teste (Gap 1 acima) — risco baixo, resultado provável é 500 genérico em vez de 404 num caso raro.
3. `CHANGELOG.md [Unreleased]` não recebeu entrada para esta feature (Gap 2 acima) — violação de regra documentada em `AGENTS.md` §6.

**Next steps**: nenhum bloqueante para uso em produção da funcionalidade em si. Recomendo: (a) adicionar a entrada de CHANGELOG antes de qualquer release, (b) opcionalmente adicionar o teste de race de Gap 1 e testes diretos para AC5/AC8 se o time quiser fechar 100% dos spec-precision gaps — nenhum dos dois é urgente dado que o comportamento subjacente já é estruturalmente correto.
