# Table RLS Default Value Fix Specification

## Problem Statement

O Dashboard usa a string `"disabled"` como valor de `rls` em dois pontos do frontend — `internal/dashboard/ui/src/pages/AppDetailsPage.tsx:193` (`defaultRls = requireRls ? "enabled" : "disabled"`, aplicado a toda nova tabela quando "RLS obrigatório por padrão" está desligado) e `internal/dashboard/ui/src/components/TableCard.tsx:309` (`<SelectItem value="disabled">` pro rótulo "Public") — mas o backend (`internal/config/rls.go`'s `ValidRLS`) só aceita `""`, `"owner"`, `"enabled"`, `"policy"`; `"disabled"` é citado no próprio comentário do backend como exemplo de typo inválido a ser rejeitado (`internal/config/rls.go:5`). Toda criação/edição de tabela que use o valor default ou selecione "Public" explicitamente envia `rls: "disabled"` e `validateTableInput` (`internal/dashboard/handler.go:129`) rejeita com erro — quebrando o fluxo padrão de "Add table" sempre que "RLS obrigatório por padrão" está desligado, e quebrando a seleção explícita de "Public" em qualquer app. Descoberto durante o Verifier da feature `policy-templates` (batch T8-T9, 2026-08-12): 3 testes pré-existentes de `internal/dashboard/ui/e2e/enduser-roles.spec.ts` já reproduzem essa falha, e reproduz mesmo antes de `policy-templates` existir (bug pré-existente, não introduzido por ela).

## Goals

- [ ] `AppDetailsPage.tsx:193` usa `""` em vez de `"disabled"` como default de RLS quando "RLS obrigatório por padrão" está desligado.
- [ ] `TableCard.tsx:309` usa `value=""` em vez de `value="disabled"` no `SelectItem` do rótulo "Public"/`appForm.tablePublic`.
- [ ] Criar uma tabela nova com RLS "Public" (default ou selecionado explicitamente) é aceito pelo backend (`validateTableInput`) sem erro, em qualquer app.
- [ ] Os 3 testes de `enduser-roles.spec.ts` que hoje falham por esse bug voltam a passar, sem nenhuma outra alteração no comportamento que eles verificam.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Renomear a option "Public" pra outro rótulo | Fora do motivador — o bug é o *valor* enviado (`"disabled"` vs `""`), não o texto exibido ao usuário. |
| Adicionar `"disabled"` como alias aceito no backend (`ValidRLS`) | O backend já documenta `"disabled"` como o exemplo canônico de typo inválido (`internal/config/rls.go:5`) — aceitar esse valor iria contra essa decisão explícita, e mascararia o bug real (frontend enviando o valor errado) em vez de corrigi-lo. |
| Auditoria de outros valores hardcoded de `rls` no frontend além dos 2 já localizados | Uma busca por `"disabled"` como valor de rls no frontend (`grep -rn '"disabled"' internal/dashboard/ui/src`) encontrou exatamente essas 2 ocorrências — sem indício de um terceiro local a corrigir. |

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Valor correto pra "RLS desligado" / "Public" | `""` (string vazia) | É o valor que `config.ValidRLS`/`config.HasOwnerColumn` já tratam como "sem RLS" em todo o resto do código (backend e frontend, ex.: `autoColumnsFor("")` já retorna as colunas base sem `owner_id`) — `""` já é o "modo desligado" real, `"disabled"` nunca foi um valor válido, só um bug de digitação que passou os checks de tipo do TypeScript por ser uma string qualquer | y — decisão técnica, sem ambiguidade de produto a validar |

**Open questions:** none.

---

## User Stories

### P1: Criar tabela nova com RLS "Public" funciona ⭐ MVP

**User Story**: Como usuário do Dashboard, eu quero criar uma tabela com RLS desligado ("Public") — seja por ser o default ou por selecionar essa opção explicitamente — para que a tabela seja criada com sucesso, sem erro de validação.

**Why P1**: É o único bug desta spec — sem o fix, o fluxo padrão de criar tabela falha sempre que RLS obrigatório está desligado.

**Acceptance Criteria**:

1. WHEN o usuário abre "Add table" num app onde "RLS obrigatório por padrão" está desligado THEN o sistema SHALL propor `rls: ""` como valor default (não `"disabled"`).
2. WHEN o usuário seleciona explicitamente a opção "Public" no seletor de RLS de uma tabela (rascunho ou existente) THEN o sistema SHALL definir `rls: ""` (não `"disabled"`).
3. WHEN o usuário salva uma tabela com `rls: ""` THEN o backend (`validateTableInput`) SHALL aceitar a requisição sem erro de RLS inválido.
4. THE fix SHALL não alterar o comportamento de nenhum outro valor de `rls` (`"owner"`, `"enabled"`, `"policy"`) nem a lógica de exibição do badge Restricted/Public em `TableCard.tsx` (que já trata qualquer valor diferente de `"enabled"` como Public).

**Independent Test**: Num app com `require_rls_default` desligado, clicar "Add table", salvar sem tocar no seletor de RLS; confirmar que a tabela é criada com sucesso e `rls === ""` na resposta. Rodar `enduser-roles.spec.ts` e confirmar que os 3 testes previamente quebrados passam.

---

## Edge Cases

- IF uma tabela já existente no banco tiver `rls: "disabled"` persistido (não deveria existir, já que o backend sempre rejeitou esse valor — mas verificar) THEN esta spec não cobre migração de dados; nenhuma linha real deveria existir com esse valor, já que toda tentativa de persistir `"disabled"` sempre falhou no backend.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| RLSDEF-01 | P1: RLS "Public" funciona | Execute | Verified |
