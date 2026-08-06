# Dashboard Global Roles Specification

## Problem Statement

`dashboard_users.role` hoje tem só dois valores (`admin`, `superadmin`), sem granularidade. `admin` acumula tudo que não é exclusivo de `superadmin` (cria/gerencia próprios apps, mas também teria que ganhar qualquer privilégio de plataforma futuro no mesmo balde). Não há papel de leitura-only nem forma de delegar tarefas administrativas de plataforma (templates, branding, gestão de usuários) sem dar acesso total de infraestrutura/integrações/auditoria.

Esta feature reestrutura `dashboard_users.role` em 4 níveis: `superadmin`, `admin`, `auditor`, `member`. É um eixo **global** de permissão de plataforma, distinto e ortogonal ao eixo **por app** desenhado em `.specs/features/rbac-per-app/` (`app_members.role`: `admin`/`editor`/`viewer`, escopado a um app específico). As duas specs se cruzam em um ponto explícito: `admin` e `auditor` globais precisam de leitura irrestrita de todos os apps, o que exige uma extensão na função central `ResolveAppRole` desenhada em `rbac-per-app` — documentada aqui como dependência de integração, não duplicada.

**Nota de nomenclatura**: o papel de leitura irrestrita global originalmente seria chamado `viewer`, mas colide com `app_members.role = 'viewer'` (escopo de um app só). Renomeado para `auditor` para eliminar a ambiguidade entre os dois eixos.

## Goals

- [ ] `dashboard_users.role` suporta 4 valores: `superadmin` (acesso irrestrito, já existente), `admin` (gestão de plataforma + apps próprios), `auditor` (leitura irrestrita de todos os apps e do audit log, sem editar nada), `member` (cria/gerencia só os próprios apps — é o `admin` de hoje, renomeado)
- [ ] Migração de dados existentes preserva comportamento: todo `admin` atual vira `member`; `superadmin` não muda
- [ ] `admin` e `auditor` enxergam dados/schema/logs de **todos** os apps hospedados (read-only), via bypass na resolução de role por app — mesmo mecanismo de bypass que `superadmin` já tem em `rbac-per-app`, mas sem escrita
- [ ] `admin` não pode criar usuário com `role: superadmin` (só `superadmin` promove/cria outro `superadmin`)
- [ ] Plataforma nunca fica sem nenhum `superadmin` (invariante garantido no código, mesma lógica do invariante "≥1 admin por app" de `rbac-per-app`)
- [ ] Toda mudança de role é auditada (`user.role_changed`)

## Out of Scope

| Item | Motivo |
|---|---|
| Permissões granulares por ação dentro de cada tela de plataforma | Escopo é binário por tela/recurso (templates, branding, integrações, infra, auditoria, usuários), mesmo padrão de nível de granularidade escolhido em `rbac-per-app` para apps |
| UI de histórico de mudança de role além do `audit_log` padrão | Cobrido pelo mecanismo de auditoria já existente, sem tela nova |
| Fundir esta spec com `rbac-per-app` | Decidido explicitamente no brainstorm: são eixos diferentes (role global em `dashboard_users` vs. membership por app em `app_members`); specs separadas ficam mais fáceis de revisar/implementar de forma independente |
| Papel `auditor` editar qualquer coisa | Por definição, é leitura irrestrita — qualquer ação de escrita fica fora, inclusive nas próprias telas que ele consegue ver |

---

## User Stories

### P1: Reestruturação de `dashboard_users.role` + migração ⭐ MVP

**User Story**: Como plataforma, quero migrar o modelo de role global de 2 para 4 níveis sem quebrar o acesso de nenhum usuário existente, para que a introdução de `auditor` e a divisão `admin`/`member` não cause regressão.

**Why P1**: Base de dados de toda a feature — sem a migração correta, os demais enforcement points não têm o que checar de forma segura.

**Acceptance Criteria**:

1. WHEN a migração roda THEN o sistema SHALL atualizar todo `dashboard_users` com `role = 'admin'` para `role = 'member'`, antes de trocar a constraint de `CHECK`.
2. WHEN a migração roda THEN nenhum `dashboard_users` com `role = 'superadmin'` SHALL ser alterado.
3. WHEN a migração termina THEN a constraint `CHECK` da coluna `role` SHALL aceitar exatamente `superadmin`, `admin`, `auditor`, `member`.
4. A migração SHALL ser idempotente (segura de rodar mais de uma vez sem efeito colateral).

**Independent Test**: Fixture de banco com usuários `admin` e `superadmin` pré-existentes; rodar a migração; confirmar que todo `admin` virou `member`, `superadmin` intocado, e que inserir um novo `dashboard_users` com role fora dos 4 valores falha na constraint.

---

### P1: Matriz de permissões de plataforma ⭐ MVP

**User Story**: Como plataforma, quero que cada tela/ação de gestão de plataforma (templates, branding, usuários, integrações, infra, auditoria, apps próprios) exija o papel correto, para que a divisão de responsabilidades tenha efeito real.

**Why P1**: Sem enforcement, a nova coluna de 4 valores existe mas não protege nada.

**Acceptance Criteria**:

1. WHEN um `admin` acessa templates, branding, ou cria usuário com role diferente de `superadmin` THEN o sistema SHALL permitir.
2. WHEN um `admin` tenta criar ou promover um usuário para `role: superadmin` THEN o sistema SHALL retornar 403.
3. WHEN um `admin` acessa config de integrações, config de infra, ou o `audit_log` THEN o sistema SHALL retornar 403.
4. WHEN um `admin` cria/gerencia um app próprio (via `app_members`, como dono) THEN o sistema SHALL permitir, do mesmo jeito que `member` já faz hoje.
5. WHEN um `auditor` acessa dados/schema/logs de qualquer app, ou o `audit_log` THEN o sistema SHALL permitir (read-only).
6. WHEN um `auditor` tenta qualquer ação de escrita (em app, template, branding, usuário, integração, infra) THEN o sistema SHALL retornar 403.
7. WHEN um `member` tenta qualquer tela de gestão de plataforma (templates, branding, usuários, integrações, infra, auditoria) THEN o sistema SHALL retornar 403 — `member` só opera dentro dos próprios apps, via `app_members`.
8. WHEN a resolução de permissão de plataforma é necessária THEN o sistema SHALL usar uma função central única (`HasPlatformPermission(role, action)`), sem checagem duplicada por endpoint.

**Independent Test**: Matriz completa (4 roles × ações da tabela de permissões) batendo em cada endpoint correspondente, confirmando 200/403 conforme especificado.

---

### P1: Leitura irrestrita de apps para `admin` e `auditor` (dependência de `rbac-per-app`) ⭐ MVP

**User Story**: Como `admin` ou `auditor`, quero ver dados/schema/logs de qualquer app hospedado, mesmo sem ser membro explícito dele, para exercer supervisão de plataforma sem precisar ser adicionado manualmente em cada app.

**Why P1**: Sem isso, `admin`/`auditor` ficariam mais restritos que o `admin` global de hoje (que já vê tudo), uma regressão de visibilidade.

**Acceptance Criteria**:

1. WHEN a função `ResolveAppRole` (de `rbac-per-app`) é chamada para um usuário com `role` global `admin` ou `auditor` THEN o sistema SHALL conceder acesso de leitura ao app consultado, independente de existir vínculo em `app_members`.
2. WHEN esse mesmo usuário tenta qualquer ação de escrita no app (via `role` efetiva daquele app) THEN o sistema SHALL negar, a menos que ele também seja membro explícito com role suficiente em `app_members` (ex: um `admin` global que também é `admin` daquele app especificamente).
3. Esta integração SHALL ser implementada como extensão da função central já existente (ou a ser criada em `rbac-per-app`), nunca como uma segunda checagem paralela dentro de `dashboard-global-roles`.

**Independent Test**: Usuário com role global `admin` (sem nenhum vínculo em `app_members`) consegue `GET` de um app de outro usuário, mas recebe 403 em qualquer ação de escrita nesse mesmo app.

---

## Edge Cases

- WHEN uma operação de mudança de role ou remoção de usuário resultaria em **zero `superadmin`** na plataforma THEN o sistema SHALL rejeitar com 400 ("plataforma precisa de ao menos um superadmin"), mesmo padrão do invariante "≥1 admin por app" de `rbac-per-app`.
- WHEN um `admin` cria um usuário THEN o campo `role` do body SHALL ser validado contra a lista de roles que ele tem permissão de atribuir (`member`, `auditor`, `admin`, nunca `superadmin`).
- WHEN um usuário `auditor` acessa a UI THEN nenhuma tela/ação exclusiva de `admin` ou `superadmin` SHALL aparecer, mesmo desabilitada — omitida, não apenas bloqueada visualmente (evita vazar existência de funcionalidade que ele não pode usar).
- Toda mudança de role SHALL gerar evento de auditoria (`user.role_changed`), visível a `superadmin` e `auditor` (únicos com acesso a `audit_log`).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| DGR-01 | P1: Migração | Design | Pending |
| DGR-02 | P1: Migração | Design | Pending |
| DGR-03 | P1: Migração | Design | Pending |
| DGR-04 | P1: Migração | Design | Pending |
| DGR-10 | P1: Matriz de permissões | Design | Pending |
| DGR-11 | P1: Matriz de permissões | Design | Pending |
| DGR-12 | P1: Matriz de permissões | Design | Pending |
| DGR-13 | P1: Matriz de permissões | Design | Pending |
| DGR-14 | P1: Matriz de permissões | Design | Pending |
| DGR-15 | P1: Matriz de permissões | Design | Pending |
| DGR-16 | P1: Matriz de permissões | Design | Pending |
| DGR-17 | P1: Matriz de permissões | Design | Pending |
| DGR-20 | P1: Leitura irrestrita (dependência rbac-per-app) | Design | Pending |
| DGR-21 | P1: Leitura irrestrita (dependência rbac-per-app) | Design | Pending |
| DGR-22 | P1: Leitura irrestrita (dependência rbac-per-app) | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 15 total, 0 mapeados a tasks, 15 não mapeados ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Nenhum usuário existente perde acesso que já tinha (migração `admin → member` preserva 100% do comportamento atual)
- [ ] Plataforma nunca fica sem `superadmin` — invariante garantido no código, não só sugerido na UI
- [ ] `admin` não consegue criar/promover ninguém para `superadmin` em nenhum caminho de código
- [ ] `admin` e `auditor` têm leitura irrestrita de todos os apps sem precisar de vínculo manual em `app_members`, via extensão documentada de `ResolveAppRole` (`rbac-per-app`)
- [ ] Toda ação de plataforma passa por `HasPlatformPermission`, sem checagem duplicada por endpoint
- [ ] UI nunca expõe tela/ação que o usuário logado não tem permissão de usar, mesmo desabilitada
