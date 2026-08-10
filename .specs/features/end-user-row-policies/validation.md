# End-User Row Policies Validation

**Date**: 2026-08-07
**Spec**: `.specs/features/end-user-row-policies/spec.md`
**Diff range**: `f1bcf8b..57142f4` (docs commit adding design.md through T17's final commit; feature-relevant commits interleaved with two unrelated commits — `deec93e` ROADMAP update, merge `8516ee7` — excluded from scope)
**Verifier**: independent sub-agent (author ≠ verifier). Read spec/design/tasks in full; dispatched one read-only research sub-agent to build the AC→test evidence table (results re-checked by hand below for the two flagged gaps); ran the full gate and discrimination sensor myself.

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1–T17 | ✅ Done | All 17 tasks marked `[x]` in `tasks.md`; commits present on `develop` in dependency order matching the Execution Plan. |

---

## Spec-Anchored Acceptance Criteria

Evidence-or-zero: a criterion with no `file:line` citation counts as NOT covered, per the skill's rule, even when production code looks correct by inspection.

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| ROWPOL-01 | `role TEXT NOT NULL DEFAULT 'member'` added idempotently | `internal/provisioner/auth_test.go:16-72` — asserts `role == "member"` on 2nd run, no error | ✅ PASS |
| ROWPOL-02 | Successful login (password or Google) embeds current `_auth_users.role` as JWT claim `role` | Production wiring is correct: `internal/auth/handler.go:119,178,265`, `internal/auth/google.go:192` all pass the DB-read `role` into `IssueJWT`. **No test decodes a real login-issued JWT and asserts `claims.Role`.** `internal/auth/jwt_test.go:8-46` only calls `IssueJWT` directly with `role` as a literal parameter (unit-level, bypasses the login handler entirely). `internal/auth/handler_test.go:217` (`TestLoginReturnsTokenAndRefresh`) only checks `m["token"] != ""`, never parses it. No Google-OAuth-login test exists at all. | ❌ **GAP — NOT covered** (evidence-or-zero; verified by hand, not just by the research sub-agent) |
| ROWPOL-03 | `role` free string, no enum/CHECK | `internal/provisioner/auth_test.go:60-71` — sets `role='approver'` via UPDATE, reads it back unchanged | ✅ PASS |
| ROWPOL-04 | Pre-existing user without `role` reads back `'member'` | `internal/provisioner/auth_test.go:51-58` | ✅ PASS |
| ROWPOL-05 | `CREATE POLICY` built with `USING`/`WITH CHECK` (insert/update only), first clause has no `logic`, rest require `AND`/`OR` | `internal/provisioner/policy_test.go:20-79` — exact `USING`/`WITH CHECK` string comparisons | ✅ PASS |
| ROWPOL-06 | First policy on a table triggers `ENABLE ROW LEVEL SECURITY` + `GRANT` before `CREATE POLICY` | `internal/dashboard/table_policies_store_test.go:92-138` — RLS-enabled flag false→true→still-true across 1st/2nd policy | ✅ PASS |
| ROWPOL-07 | Bad column/operator/logic → 400, zero DDL | `internal/provisioner/policy_test.go:81-124,406-451`; `internal/dashboard/table_policies_store_test.go:140-166` — asserts RLS flag stays disabled after a failed validation | ✅ PASS |
| ROWPOL-08 | `IS NULL`/`IS NOT NULL` with a value → rejected | `internal/provisioner/policy_test.go:295-308` | ✅ PASS |
| ROWPOL-09 | Claim restricted to `role`/`sub`/`email`, cast to column type | `internal/provisioner/policy_test.go:126-159,206-255` — exact cast strings (`::UUID`, `::DECIMAL`, `::TIMESTAMPTZ`) | ✅ PASS |
| ROWPOL-10 | Literal embedded via safe escaping, never raw concatenation | `internal/provisioner/policy_test.go:161-204,535-549` — injection-shaped literal (`'; DROP TABLE...`) asserted to produce a syntactically safe, fully-escaped string | ✅ PASS |
| ROWPOL-11 | Postgres enforces the policy identically via REST or a direct connection as the end-user role | `internal/server/rls_policy_test.go:175-282` — REST denial (404), raw `pgx` connection with manually-set GUCs reproduces the same denial, REST allow-case succeeds | ✅ PASS (strongest single piece of evidence in the suite) |
| ROWPOL-12 | `DELETE` policy → `DROP POLICY`; last policy leaves RLS enabled (default-deny) | `internal/dashboard/table_policies_store_test.go:196-239` | ✅ PASS |
| ROWPOL-13 | Second Postgres role, no `BYPASSRLS`/ownership, with membership grant | `internal/dashboard/enduser_role_test.go:34-102` — `rolbypassrls=false`, `rolcanlogin=false`, `SET ROLE` succeeds, idempotent on 2nd run | ✅ PASS |
| ROWPOL-14 | `SET LOCAL ROLE` + GUCs before end-user query, reverted at tx end, `statement_timeout` preserved | `internal/db/client_test.go:114-265` — `current_user` check, cross-connection GUC-leak check, composed-timeout regression, explicit-error-if-role-missing case | ✅ PASS |
| ROWPOL-15 | Data Browser/purge/provisioner never call `SET ROLE`; end-user path switches role unconditionally | `internal/dashboard/purge_test.go:184-226`, `internal/server/rls_policy_test.go:260-281` | ⚠️ PASS with a scope caveat — see Finding 2 below (`storage_handler.go`) |
| ROWPOL-16–20 | Dashboard UI: list+create form, column/operator dropdowns (no free text), claim dropdown, toast success/error, full i18n | `internal/dashboard/ui/src/components/TablePolicies.tsx` (367 lines) + `api.ts` `onError: toast.error(error.message)` on both mutations + 23 matching keys in `en.json`/`pt-BR.json`. No unit-test framework exists for this UI (confirmed against `tasks.md`'s own Test Coverage Matrix — build-gate only, by design). | ✅ PASS (code-level; no automated behavioral test, as scoped) |
| ROWPOL-21–24 | FK to `_auth_users.id`: accepted for `uuid`, rejected for other type/column, real FK violation on insert | `internal/provisioner/relationships_test.go` (`TestForeignKeyToAuthUsersEnforced` and siblings), `internal/config/validate_test.go` | ✅ PASS |
| ROWPOL-25 | FK to `_auth_users` shows in dashboard relationships UI like any other FK | No `_auth_users`-specific code found in `internal/dashboard/ui/src` (expected — design/T17 predicted no UI change needed, since the UI renders whatever `references` metadata the API returns generically). Not independently exercised in this pass (no running dashboard to click through). | ⚠️ Spec-precision gap — plausible by code inspection, not directly observed |
| ROWPOL-28 | Extended operators `>`,`<`,`>=`,`<=`,`IS NULL`,`IS NOT NULL` | `internal/provisioner/policy_test.go:206-293` | ✅ PASS |
| ROWPOL-29 | AND/OR fold left-to-right, fully parenthesized each step | `internal/provisioner/policy_test.go:353-404` — exact string match for 3- and 4-clause mixed folds (`((c1 AND c2) OR c3)` form, not `(c1 AND (c2 OR c3))`) | ✅ PASS — this is the AC I scrutinized hardest per your instructions; the test compares the **exact** SQL string, and mutation 1 below independently confirms the test kills a wrong-parenthesization mutant |

**Status**: ❌ 1 real gap (ROWPOL-02, a P1/MVP AC — evidence-or-zero rule applies even though the code looks correct by inspection) + 1 scope caveat (ROWPOL-15) + 1 unverified UI claim (ROWPOL-25). All other 25 ACs have precise, non-vague evidence.

---

## Discrimination Sensor

Ran in an isolated `git worktree` (`git worktree add <scratch> HEAD`), never touching the real tree. Baseline `git status --porcelain | md5` captured before (`07eb9569ef8c520870cc7dc0d204c980`) and re-confirmed identical after `git worktree remove --force`.

| # | File:line | Mutation | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/provisioner/policy.go:197` | Removed the wrapping parens from the AND/OR fold step (`acc = fmt.Sprintf("(%s %s %s)", ...)` → `fmt.Sprintf("%s %s %s", ...)`) — targets ROWPOL-29, the exact requirement you asked me to scrutinize | ✅ Killed — `TestBuildPolicySQL_ThreeClauseFoldExactParenthesization` failed with a diff showing the missing parens |
| 2 | `internal/provisioner/policy.go:184` | Disabled the `logic` allowlist check (`else if !allowedLogic[...]` → `else if false && ...`) — targets the operator/logic allowlist gate | ✅ Killed — `TestBuildPolicySQL_NonFirstClauseMissingLogicIsRejected` and `_InvalidLogicIsRejected` both failed |
| 3 | `internal/dashboard/handler.go:1389` | Neutralized the `CanManage()` gate on `CreateTablePolicy` (`if !role.CanManage()` → effectively `if false`) — targets the authorization gate on policy-management endpoints | ✅ Killed — `TestCreateTablePolicyHandler_NonAdminForbidden` failed (non-admin got through) |
| 4 | `internal/db/client.go:130-132` | Disabled the `SET LOCAL ROLE <EnduserRole>` call inside `WithRLSContext` — targets the core enforcement swap | ✅ Killed — `TestWithRLSContext_RoleSwitch`, `_LacksMembershipFailsExplicitly`, `TestHandlerRunsAsEnduserRole`, and (decisively) `TestRLSPolicy_EndToEndMotivatingCase/REST_UserCannotApproveOwnRequest` all failed — the motivating-case denial silently became an allow |

**Sensor depth**: lightweight (4 targeted mutations, default tier — this is not a P0 payment path, though it is security-adjacent; 4 covers the fold, the allowlist gate, the authz gate, and the core role-switch, i.e. the four places a silent regression would be most dangerous and least visible).
**Result**: 4/4 killed — ✅ PASS. No surviving mutants; no fix tasks generated from this sensor pass.

---

## Gate Check

- **Gate command**: `go build ./...` && `go vet ./...` && `gofmt -l $(git diff --name-only f1bcf8b..HEAD -- '*.go')` && `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep_test go test ./...` && `cd internal/dashboard/ui && npx tsc -b && npm run build` && locale JSON validation
- **Result**: build clean, vet clean, gofmt clean (0 files listed), **298 subtests passed, 0 failed, 1 skipped**, `tsc -b`/`vite build` clean, both locale files parse as valid JSON.
- **Skipped test**: `TestInstallationAutoAccess` (`internal/github/client_integration_test.go:24`) — skips because `GITHUB_INTEGRATION_*` env vars aren't set. Pre-existing, unrelated to this feature (GitHub App integration test) — justified skip.
- **Postgres used**: local Docker container `zeep-orbit-db-1` (`postgres:16-alpine`, already running on the box), against a disposable `zeep_test` database created and dropped for this run — never touched the dev `zeep` database.
- **Failures**: none.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ — deviations in `tasks.md` (T8, T10, T11, T13, T14) are all disclosed, justified, and narrowly scoped; none looked like undisclosed scope creep on inspection |
| No scope creep | ✅ |
| Matches patterns | ✅ — `WithRLSContext` mirrors `WithTimeout`'s structure; `policy.Builder` reuses `identRe`/`pgType()` as designed |
| Spec-anchored outcome check (asserted values match spec) | ⚠️ 25/27 testable ACs matched exactly; ROWPOL-02 did not (see gap above) |
| Per-layer Coverage Expectation met | ✅ for provisioner/db/store/handler/server layers; UI has no unit-test infra by design (documented in `tasks.md`'s Test Coverage Matrix) |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — every test file inspected traces to a specific ROWPOL-ID or task |
| Documented guideline followed | `AGENTS.md` §3 (gate commands), §4 (English error strings — confirmed in `policy.go`/`handler.go` error messages), §5 (i18n + `toast.error` — confirmed) |

---

## Edge Cases (from spec.md)

- [x] Duplicate policy name/table/action → 409 (`internal/dashboard/table_policies_handler_test.go`, `TestCreateTablePolicyHandler_DuplicateReturns409`)
- [x] Table delete cascades policy delete (`table_policies_store_test.go` cascade test; `apps_store.go`'s `DeleteAppTable` amended per T8's disclosed deviation)
- [x] Claim outside `role`/`sub`/`email` rejected (`policy_test.go`)
- [x] Table with no policy keeps RLS disabled — never preemptive (`table_policies_store_test.go:92-138`, RLS flag false before first policy)
- [x] Role with no matching policy on an RLS-enabled table → default-deny (implicit in Postgres semantics, exercised by `TestRLSPolicy_EndToEndMotivatingCase`)
- [x] Single-clause policy with `logic` set → rejected (`TestBuildPolicySQL_FirstClauseWithLogicIsRejected`)
- [x] Mixed AND/OR 3+ clause fold — asserted exactly (see ROWPOL-29 above; also independently confirmed by sensor mutation 1)

---

## Findings (ranked by severity)

**1. [Major] ROWPOL-02 — no test proves the login flow actually embeds the DB-read `role` in the issued JWT.**
`internal/auth/jwt_test.go:8-46` only tests `IssueJWT` called directly with `role` as a literal argument — it never goes through `Login`/`Register`/Google OAuth. `internal/auth/handler_test.go:217` (`TestLoginReturnsTokenAndRefresh`) checks only that a token string is non-empty, never decodes it. No test exists for the Google OAuth login path's role claim at all. Production code (`internal/auth/handler.go:119,178,265`, `internal/auth/google.go:192`) reads `_auth_users.role` and passes it to `IssueJWT` correctly by inspection, but per the skill's evidence-or-zero rule, an AC with no direct `file:line` proof counts as not covered — and this is a P1/MVP AC, not a peripheral one. **Fix task**: add an integration test that registers/updates a user to a custom role, logs in via `/{app}/auth/login`, decodes the returned JWT with `ParseJWT`, and asserts `claims.Role` equals the DB value; repeat (or at minimum add a comment justifying the gap) for the Google OAuth path.

**2. [Minor] `internal/server/storage_handler.go` (`/{app}/files/*`) never calls `WithRLSContext` — it runs on `h.pool` (owner role) directly, at every call site (`HandleFileUpload`, `HandleFileList`, `HandleFileGet`, `HandleFileDownload`, `HandleFileDelete`, `HandleFileSignedURL`).**
This contradicts the literal wording of the spec's "Bypass interno" AC-04: *"nenhum handler do caminho `/{app}/...` executa código com o role owner — a troca de role... é incondicional, não uma opção configurável por request."* In practice this is low-risk today: the `_files` table these handlers touch is a system-managed table, not registered in `app_tables`, so an admin cannot attach a row policy to it through the dashboard's policy builder — there's no way to exploit this via the feature being shipped. But it is a real, undocumented deviation from the spec's absolute claim, pre-existing and untouched by this feature's commits. Neither `tasks.md`'s Deviations sections nor the spec's Out-of-Scope table mentions it. **Recommendation**: either scope the AC's wording explicitly to exclude system-managed tables (`_files`, `_auth_users` itself), or file a follow-up to route file-metadata queries through `WithRLSContext` too, before any future feature makes `_files` policy-addressable.

**3. [Minor / unverified] ROWPOL-25 (FK-to-`_auth_users` visible in the dashboard's relationships UI) was not independently observed.**
Code inspection supports the claim (the relationships UI is generic, driven by whatever `references` metadata the table-columns API returns, with no `_auth_users`-specific branching found — matching T17's own note that "no code change needed"), but this pass had no running dashboard to click through and visually confirm. Low risk given the generic rendering, but flagged rather than silently assumed.

No other gaps found. All 4 sensor mutations were killed cleanly — the fold logic, the operator/logic allowlist, the `CanManage()` authorization gate, and the core `SET LOCAL ROLE` enforcement swap are all genuinely defended by the test suite, not just superficially covered.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| ROWPOL-01, 03, 04, 05, 06, 07, 08, 09, 10, 11, 12, 13, 14, 16–24, 28, 29 | Implementing/Verified | ✅ Verified |
| ROWPOL-02 | Implementing | ❌ Needs Fix (test gap, not a functional bug) |
| ROWPOL-15 | Verified | ⚠️ Verified with scope caveat (Finding 2) |
| ROWPOL-25 | Pending | ⚠️ Plausible, not independently observed |
| ROWPOL-26, 27 | Pending | Pending (out of this pass's scope — P2 Audit UI / P3 Preview were never implemented, matching `tasks.md`'s own "12 unmapped" note) |

---

## Summary

**Overall**: ❌ **FAIL** (strict evidence-or-zero reading — one P1/MVP acceptance criterion, ROWPOL-02, has no direct test evidence) — but functionally the feature is sound: 25 of 27 testable ACs have precise, non-vague evidence; the gate is 100% green (298 passed, 0 failed, 1 justified skip); the discrimination sensor killed all 4 targeted mutations, including the exact AND/OR-fold parenthesization behavior you asked me to scrutinize and the core role-switch enforcement. This is a coverage gap on a security-relevant claim, not a broken feature — treat it accordingly when deciding whether to block or ship-with-followup.

**Spec-anchored check**: 25/27 testable ACs matched spec outcome exactly; 1 gap (ROWPOL-02); 1 unverified-but-plausible (ROWPOL-25)
**Sensor**: 4/4 mutations killed
**Gate**: 298 passed, 0 failed, 1 justified skip; build/vet/gofmt/tsc/vite all clean

**What works**: Native Postgres RLS enforcement is proven at the database level, not just the HTTP layer (`TestRLSPolicy_EndToEndMotivatingCase`'s raw-`pgx`-as-`zeep_app_enduser` subtest is the strongest evidence in the suite). Internal routines (Data Browser, purge, provisioner) are proven unaffected. The AND/OR fold parenthesization and extended operators are asserted by exact string, not loose semantic equivalence. Backward compatibility (tables without policies) is exercised.

**Issues found**: See ranked Findings above (1 Major test-coverage gap, 2 Minor/documentation-scope items).

**Next steps**: Route Finding 1 (ROWPOL-02) back as a fix task — add the missing login→JWT integration test — before calling this feature done. Findings 2 and 3 are lower-priority: worth a decision (explicit spec scoping for Finding 2; a manual UI click-through for Finding 3) but don't block, since neither represents a functional break in what shipped.

---

## Re-verificação (rodada 2)

**Date**: 2026-08-07
**Diff range desta rodada**: `57142f4..04ab798` (2 commits: `297c2e6` teste, `04ab798` doc)
**Verifier**: fresh sub-agent, independente do worker que aplicou o fix (author ≠ verifier). Leu o código do teste novo, não aceitou o resumo do commit como prova.

### Achado 1 (ROWPOL-02) — re-verificado

Lido `internal/auth/handler_test.go:245-306` (`TestLoginEmbedsDBRoleInJWTClaim`) linha a linha, não apenas a mensagem do commit:

- Faz login via handler HTTP real: `router.ServeHTTP` sobre `POST /{app}/auth/login` (não chama `IssueJWT` direto) — `handler_test.go:283-287`.
- Usa um `role` não-default lido do banco, não um literal hardcoded que coincidentemente bate: seta `role = 'approver'` via `UPDATE ..._auth_users`, depois faz um `SELECT` separado para `dbRole` (`handler_test.go:262-275`) — a asserção principal compara `claims.Role != dbRole` (variável lida do banco), não `claims.Role != "approver"`. Há uma segunda asserção de invariante de setup (`dbRole != "approver"`), mas essa não é a que prova o claim — a prova é contra `dbRole`.
- Decodifica o JWT retornado: `ParseJWT([]byte(testSecret), rawToken)` sobre o `token` devolvido pelo handler (`handler_test.go:296-299`), não reaproveita nenhum valor calculado fora da resposta HTTP.
- Falha se o claim não corresponder: `t.Fatalf(...)` em `claims.Role != dbRole` (`handler_test.go:300-302`).

Rodado isolado:
```
TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep go test ./internal/auth/... -run TestLoginEmbedsDBRoleInJWTClaim -v
--- PASS: TestLoginEmbedsDBRoleInJWTClaim (0.54s)
```
**Resultado**: ✅ ROWPOL-02 agora tem evidência direta, não apenas por inspeção de código. Gap fechado.

### Achado 2 (`storage_handler.go` fora de escopo) — re-verificado

`spec.md:102` (AC-04 da story "Bypass interno") agora nomeia explicitamente os handlers cobertos (`HandleList`/`HandleGet`/`HandleGetByID`/`HandleInsert`/`HandleUpdate`/`HandleDelete` em `internal/server/handler.go`) e declara, na mesma frase, que não se aplica a `internal/server/storage_handler.go`. `spec.md:29` (tabela Out of Scope) tem uma linha dedicada nomeando o arquivo e o motivo (S3/MinIO, sem tabela Postgres endereçável). Sem ambiguidade remanescente — texto e Out of Scope table concordam. `python3 <skill-dir>/scripts/validate_spec.py end-user-row-policies` → `0 error(s), 0 warning(s)`.

### Achado 3 (ROWPOL-25) — não coberto por este fix

Não fazia parte do escopo do fix desta rodada (worker tratou apenas ROWPOL-02 e o texto do AC-04). Continua como estava: plausível por inspeção de código, não observado interativamente. Não bloqueia — é Minor/unverified, não um gap de P1/MVP.

### Gate completo (rodada 2, do zero)

- `go build ./...` → limpo.
- `go vet ./...` → limpo.
- `gofmt -l internal/auth/handler_test.go` → sem saída (limpo).
- `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep go test ./... -p 1` → todos os pacotes `ok`; **299 subtestes passaram, 0 falharam, 1 skip** (`TestInstallationAutoAccess`, mesmo skip pré-existente e justificado da rodada 1 — `GITHUB_INTEGRATION_*` não configurado). Delta vs. rodada 1: +1 teste (o novo `TestLoginEmbedsDBRoleInJWTClaim`), consistente com o fix.
- `cd internal/dashboard/ui && npx tsc -b && npm run build` → limpo, build de produção gerado sem erros.

### Sensor de mutação — não repetido, escopo confirmado intocado

Não repetido, conforme instrução (nada nos pontos testados por ele mudou). Confirmado por diff restrito aos 4 arquivos mutados na rodada 1:

```
git diff 57142f4..HEAD -- internal/provisioner/policy.go internal/dashboard/handler.go internal/db/client.go
```
→ saída vazia. `policy.Builder`, o allowlist de `logic`, `CanManage()` e `SET LOCAL ROLE` (em `client.go`) não foram tocados pelo fix desta rodada — o resultado da rodada 1 (4/4 mutações mortas) permanece válido.

### Veredito da rodada 2

**Overall**: ✅ **PASS**

- Spec-anchored check: 26/27 ACs testáveis com evidência exata (ROWPOL-02 fechado nesta rodada); ROWPOL-25 permanece como spec-precision gap Minor/não-bloqueante (inspeção de código apoia a claim, sem observação interativa).
- Gate: build/vet/gofmt limpos; 299 passed, 0 failed, 1 skip justificado (pré-existente); tsc/vite limpos.
- Sensor: resultado da rodada 1 (4/4 mortas) confirmado ainda válido — arquivos mutados intocados por este fix.
- Achado Major da rodada 1 (ROWPOL-02) fechado com evidência direta e verificada por leitura de código + execução isolada, não apenas pelo resumo do commit.
- Achado Minor 2 (storage_handler.go) resolvido via escopo textual explícito no spec, sem ambiguidade.
- Achado 3 (ROWPOL-25) permanece aberto como item Minor/não-bloqueante — não impede o PASS.

Sem novos achados nesta rodada.
