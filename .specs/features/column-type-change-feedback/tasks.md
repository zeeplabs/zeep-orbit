# Tasks: Column Type Change Feedback

**Spec**: `.specs/features/column-type-change-feedback/spec.md`
**Design**: `.specs/features/column-type-change-feedback/design.md`
**Status**: Draft

> Convenção de Gate: não há `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`), mesmo critério usado nas outras specs deste repo.

---

## Execution Plan

```
Fase 1: Erro tipado                Fase 2: Handler                 Fase 3: Verificação
┌───────────────────────┐         ┌──────────────────────┐        ┌────────────────────┐
│ T-01 TypeChangeError    │────────▶│ T-03 errors.As nos 4   │───────▶│ T-05 testes de erro │
│ T-02 migration.go usa   │         │      call sites        │        │ T-06 allow-list doc  │
│      TypeChangeError    │         │ T-04 mensagem genérica │        │      (P2)            │
└───────────────────────┘         │      sem err.Error()    │        └────────────────────┘
                                    └──────────────────────┘
```

---

## T-01: Criar `provisioner.TypeChangeError`

**Arquivo**: novo `internal/provisioner/errors.go`

- Struct `TypeChangeError{Column, CurrentType, DesiredType, Reason string, Cause error}`.
- Método `Error() string` retornando mensagem pública segura (nunca inclui `Cause`/SQLSTATE).
- Método `Unwrap() error` retornando `Cause`, pra `errors.As`/`errors.Is` funcionarem na cadeia e log server-side continuar completo.
- `Reason` como constantes tipadas (não string solta): `ReasonNoConversionsDefined`, `ReasonUnsafeNarrowing`, `ReasonRuntimeFailure`.

**Acceptance**: `go build ./...` passa; teste unitário confirma `Error()` de uma instância com `Cause` preenchido não contém a string do `Cause` no output.

---

## T-02: `applyTypeChange` retorna `*TypeChangeError` nos 3 pontos de erro

**Arquivo**: `internal/provisioner/migration.go` (linhas 132-136, 145-148, 154-156)

- Linha 134 (`source type has no defined conversions`): retornar `&TypeChangeError{..., Reason: ReasonNoConversionsDefined}`.
- Linha 146 (`unsafe conversion`): retornar `&TypeChangeError{..., Reason: ReasonUnsafeNarrowing}`.
- Linha 154-156 (falha do `p.pool.Exec`): retornar `&TypeChangeError{..., Reason: ReasonRuntimeFailure, Cause: err}`.
- Confirmar que `applyColumnChanges` (chamador, dentro do mesmo arquivo/pacote) e `provisioner.go:73` continuam usando `%w` ao propagar — não quebrar a cadeia `Unwrap`.

**Acceptance Criteria** (spec.md P1 "mensagem clara" + P1 "runtime não vaza"):
1. Type change de `text→integer`: erro retornado é `*TypeChangeError` com `Reason: ReasonUnsafeNarrowing` (via `errors.As`).
2. Type change com tipo de origem sem entrada na allow-list: `Reason: ReasonNoConversionsDefined`.
3. Falha simulada de `pool.Exec` (mock ou dado real incompatível dentro de conversão liberada): `Reason: ReasonRuntimeFailure`, `Cause` preenchido e recuperável via `errors.Unwrap`.

**Depende de**: T-01.

---

## T-03: Handler distingue `TypeChangeError` via `errors.As` nos 4 call sites

**Arquivo**: `internal/dashboard/handler.go` (linhas 813, 909, 997, 1073)

- Em cada um dos 4 pontos, após `h.prov.Apply(...)` falhar: `var typeErr *provisioner.TypeChangeError; if errors.As(err, &typeErr) { ... }`.
- Se `TypeChangeError`: `h.writeError(w, r, http.StatusBadRequest, typeErr.Error(), err)`.
- Adicionar import de `errors` e do pacote `provisioner` no arquivo, se ainda não importado (confirmar durante implementação — `internal/dashboard` provavelmente já importa `provisioner` para outras chamadas).

**Acceptance Criteria** (spec.md P1 "runtime não vaza"):
1. Resposta HTTP de um type-change rejeitado tem status 400 e corpo `{"error": "cannot change type of ...: ..."}` sem string de erro Go bruta.
2. Log server-side (`h.logger.Error`) continua recebendo o erro completo (`Cause` incluso via `Unwrap`), verificável nos logs de teste.

**Depende de**: T-01, T-02.

---

## T-04: Erro genérico (não-`TypeChangeError`) para de concatenar `err.Error()`

**Arquivo**: `internal/dashboard/handler.go` (mesmos 4 call sites, branch `else`)

- Trocar `"provisioning failed: "+err.Error()` por mensagem fixa `"provisioning failed — check server logs for details"`.
- `err` completo continua sendo passado como último argumento de `writeError` (log server-side inalterado).

**Acceptance Criteria** (spec.md, constraint "mensagem pública nunca deve conter SQLSTATE/SQL"):
1. Qualquer erro de `Apply` que não seja `*TypeChangeError` (ex.: erro de rename, create table) retorna mensagem fixa, sem `err.Error()` no corpo JSON.

**Depende de**: T-03 (mesmo bloco de código, mudança conjunta).

---

## T-05: Testes cobrindo os 3 `Reason` e o vazamento fechado

**Arquivo**: `internal/provisioner/migration_test.go` (se já existir; senão criar) + teste de handler em `internal/dashboard/*_test.go`

Cobrir:
- `applyTypeChange` retorna `*TypeChangeError` com `Reason` correto pros 3 casos (allow-list vazia, allow-list não cobre destino, falha de exec).
- Handler retorna 400 + mensagem tratada pra `TypeChangeError`, 500 + mensagem genérica fixa pra outros erros — nenhum dos dois casos deve conter texto de erro Go bruto (`%!w`, `pgx:`, SQLSTATE, `ERROR:`) no corpo JSON.
- `errors.Unwrap` no erro retornado por `Apply` ainda alcança o erro pgx original, pra log continuar completo.

**Depende de**: T-01 a T-04.

---

## T-06 (P2, opcional): Revisar e documentar `safeTypeConversions`

**Arquivo**: `internal/provisioner/migration.go` (linhas 13-22)

- Pra cada par tipo-origem/tipo-destino já presente, adicionar comentário justificando segurança (baseado em comportamento real do `USING col::type` do PostgreSQL).
- Avaliar pares ausentes de `pgTypeToUDT` (linha 24-46: `int4, int8, numeric, text, bool, uuid, timestamptz, jsonb`) e decidir explicitamente incluir ou justificar exclusão.

**Acceptance Criteria** (spec.md P2):
1. Todo par tipo-origem/destino da allow-list tem comentário de justificativa.
2. Todo par de `pgTypeToUDT` ausente da allow-list está documentado como avaliado (seguro mas não incluído / inseguro por definição).

**Depende de**: T-01 a T-05 (fase final, não bloqueia o MVP de P1).
