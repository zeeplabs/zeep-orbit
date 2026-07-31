# Two-Factor Authentication (2FA) Specification

## Problem Statement

zeep-orbit hoje não tem nenhum segundo fator de autenticação — nem para os administradores do próprio dashboard (`dashboard_users`), nem para os usuários finais de cada app hospedado (`internal/auth`). Uma senha vazada ou fraca é o único obstáculo entre um atacante e o acesso total à conta.

Esta feature cobre dois escopos simétricos, na mesma spec, compartilhando o mesmo mecanismo criptográfico (`internal/twofactor`):

1. **2FA do dashboard**: qualquer `dashboard_users` (admin ou superadmin) pode ativar TOTP por conta própria; o superadmin pode exigir 2FA de todos os admins da plataforma.
2. **2FA por app hospedado**: o criador do app pode permitir que os usuários finais daquele app ativem 2FA por conta própria, e opcionalmente exigir de todos.

Método: **TOTP (RFC 6238) + códigos de backup**, sem SMS/email como alternativa (evita dependência de provider de SMS e não acopla a esta spec a `smtp-email-integration`, ainda não implementada). 2FA se aplica somente a contas com senha local — contas autenticadas via Google OAuth (dashboard ou app) já têm a segurança própria do Google e não passam por este fluxo, mesma lógica já usada em `smtp-email-integration` para reset de senha.

## Goals

- [ ] Qualquer usuário com senha local (dashboard ou app) pode ativar TOTP + gerar códigos de backup, com fluxo de setup que exige confirmação de um código válido antes de considerar a 2FA ativa
- [ ] Superadmin pode exigir 2FA de todos os admins do dashboard; criador de app pode exigir 2FA de todos os usuários finais daquele app — em ambos os casos, usuário sem 2FA configurada é direcionado a um fluxo de setup obrigatório no próximo login, não simplesmente bloqueado
- [ ] Login com 2FA habilitada é stateless (token de step-up assinado, TTL curto) — sem estado em memória por processo, correto sob múltiplas réplicas atrás de load balancer não-sticky (regra do `AGENTS.md`)
- [ ] Usuário não pode desativar a própria 2FA enquanto a exigência (`require_2fa`) estiver ativa, evitando se autoexcluir do próximo login
- [ ] Existe um fallback administrativo para resetar 2FA de um usuário travado (perdeu dispositivo e códigos de backup) — superadmin no dashboard, criador/admin do app no lado app
- [ ] Mecanismo criptográfico (`internal/twofactor`) é compartilhado entre os dois escopos, sem duplicar lógica de geração/verificação TOTP

## Out of Scope

| Item | Motivo |
|---|---|
| SMS ou email como método alternativo de 2FA | Decidido explicitamente no brainstorm: evita dependência de provider de SMS (custo, integração nova) e não acopla esta spec a `smtp-email-integration` (ainda não implementada) |
| 2FA para contas autenticadas via Google OAuth | Google já provê sua própria segurança (2FA, verificação em duas etapas); exigir TOTP local em cima disso seria redundante — mesma lógica já usada na decisão de reset de senha de `smtp-email-integration` |
| WebAuthn / chaves de segurança física (FIDO2) | Evolução possível futura, não travada nesta spec — TOTP + backup codes cobre o caso de uso atual sem essa complexidade adicional |
| Revogação individual do token de step-up antes da expiração | Token é stateless por design (mesmo trade-off já aceito em `smtp-email-integration`); mitigado com TTL curto (5 minutos) |
| Desativação automática de `require_2fa` quando o último admin/usuário perde acesso | Cenário de recuperação é coberto pelo fallback administrativo (reset de 2FA por superadmin/criador do app), não por desligar a exigência automaticamente |

---

## User Stories

### P1: Setup de 2FA (TOTP + backup codes) ⭐ MVP

**User Story**: Como usuário com senha local (admin do dashboard ou usuário final de um app), quero ativar 2FA via TOTP e receber códigos de backup, para proteger minha conta com um segundo fator além da senha.

**Why P1**: É a base funcional de toda a feature — sem setup, não há o que exigir nem o que verificar no login.

**Acceptance Criteria**:

1. WHEN o setup é iniciado (`POST .../2fa/setup`) THEN o sistema SHALL gerar um secret TOTP novo (ainda não persistido como ativo) e retornar o secret e a URI de provisionamento (`otpauth://`) para renderização de QR code.
2. WHEN o usuário confirma o setup com um código TOTP válido gerado a partir desse secret (`POST .../2fa/confirm`) THEN o sistema SHALL persistir o secret criptografado, marcar 2FA como habilitada, gerar 8 códigos de backup e retorná-los **uma única vez** na resposta — nunca mais recuperáveis em texto claro depois disso.
3. WHEN o código de confirmação enviado não corresponde ao secret pendente THEN o sistema SHALL rejeitar o setup, sem persistir nada como ativo.
4. WHEN o usuário desativa a 2FA (`POST .../2fa/disable`) E não há exigência (`require_2fa`) ativa para essa conta THEN o sistema SHALL remover o secret e os backup codes, revertendo ao login normal por senha.
5. WHEN o usuário regenera os códigos de backup THEN o sistema SHALL invalidar todos os códigos anteriores, substituindo-os integralmente pelos novos.
6. Toda string nova de UI relacionada a esta feature SHALL passar por `react-i18next`, adicionada em `en.json` e `pt-BR.json` na mesma mudança.

**Independent Test**: Simular setup completo (gerar secret de teste, calcular código TOTP válido, confirmar); simular confirmação com código errado (rejeitada, nada persistido); simular regeneração de backup codes e confirmar que os antigos param de funcionar.

---

### P1: Login com 2FA (step-up stateless) ⭐ MVP

**User Story**: Como plataforma, quero que o login de uma conta com 2FA habilitada exija o segundo fator antes de emitir a sessão, de forma stateless, para que o mecanismo funcione corretamente sob múltiplas réplicas sem depender de estado compartilhado em memória.

**Why P1**: Sem enforcement no login, o setup da User Story 1 não protege nada na prática.

**Acceptance Criteria**:

1. WHEN a senha está correta E a conta não tem 2FA habilitada THEN o sistema SHALL emitir a sessão/JWT normalmente, sem nenhuma etapa adicional (comportamento atual, sem regressão).
2. WHEN a senha está correta E a conta tem 2FA habilitada THEN o sistema SHALL NOT emitir a sessão/JWT diretamente — SHALL emitir um token assinado de step-up (`purpose: "2fa_pending"`, TTL de 5 minutos, sem persistência em banco ou memória).
3. WHEN o token de step-up e um código TOTP ou backup code válidos são enviados (`POST .../login/2fa`) THEN o sistema SHALL emitir a sessão/JWT real.
4. WHEN o código enviado é inválido THEN o sistema SHALL rejeitar sem emitir sessão, e SHALL aplicar rate limit dedicado a esse endpoint (reuso do mecanismo já existente em `internal/auth/ratelimit.go`), para mitigar força bruta sobre o espaço de 6 dígitos.
5. WHEN o token de step-up expira (>5 minutos) THEN o sistema SHALL exigir novo login (senha) do zero.
6. A conta autenticada via Google OAuth SHALL NOT passar por este fluxo em nenhuma hipótese.

**Independent Test**: Login de conta com 2FA habilitada gera token de step-up, não sessão; completar com código válido gera sessão; completar com código inválido é rejeitado e conta como tentativa de rate limit; token expirado exige novo login.

---

### P1: Exigência de 2FA (`require_2fa`) — dashboard e app ⭐ MVP

**User Story**: Como superadmin (dashboard) ou criador de app, quero poder exigir que todos os usuários daquele escopo tenham 2FA ativa, para elevar a postura de segurança padrão sem depender da adesão voluntária de cada usuário.

**Why P1**: É o mecanismo que torna a feature efetiva como política, não só como opção individual.

**Acceptance Criteria**:

1. WHEN `require_2fa` está ativo para o escopo (dashboard global ou um app específico) E um usuário desse escopo faz login com senha correta mas sem 2FA configurada THEN o sistema SHALL retornar um estado que sinaliza setup obrigatório (`must_setup_2fa: true`), sem emitir sessão/JWT, direcionando ao fluxo de setup da User Story 1.
2. WHEN `require_2fa` está ativo para o escopo THEN o usuário afetado SHALL NOT conseguir desativar a própria 2FA enquanto a exigência permanecer ligada.
3. WHEN `require_2fa` é desativado pelo superadmin/criador do app THEN as contas que já têm 2FA habilitada SHALL permanecer com ela habilitada (desligar a exigência não desativa 2FA já configurada em nenhuma conta).
4. `require_2fa` do dashboard SHALL ser configurável somente por `superadmin`; `require_2fa` de um app SHALL ser configurável somente pelo criador/admin daquele app.
5. Contas autenticadas via Google OAuth SHALL ser ignoradas pela exigência (ver Out of Scope) — não ficam bloqueadas nem recebem `must_setup_2fa`.

**Independent Test**: Ativar `require_2fa` num app de teste; logar com usuário sem 2FA e confirmar `must_setup_2fa: true` sem sessão emitida; confirmar que usuário com 2FA já ativa não consegue chamar `disable` enquanto a exigência estiver ligada; desativar a exigência e confirmar que a 2FA já ativa permanece intacta.

---

### P2: Fallback administrativo (recuperação de conta travada)

**User Story**: Como superadmin (dashboard) ou criador/admin de app, quero poder resetar a 2FA de um usuário que perdeu o dispositivo e os códigos de backup, para que ele não fique permanentemente sem acesso.

**Why P2**: Importante para a operação real da feature, mas não bloqueia o MVP — o caso de perda de dispositivo é raro comparado ao fluxo principal de login.

**Acceptance Criteria**:

1. WHEN o superadmin aciona reset de 2FA de outro admin (`POST /dashboard/api/users/{id}/2fa/reset`) THEN o sistema SHALL remover o secret e os backup codes daquela conta, revertendo-a para login só por senha, e SHALL registrar evento de auditoria.
2. WHEN o criador/admin de um app aciona o equivalente para um usuário final daquele app THEN o mesmo comportamento SHALL se aplicar, escopado àquele app.
3. Reset administrativo SHALL funcionar independentemente do estado de `require_2fa` — a conta resetada volta a ser tratada como "sem 2FA configurada" (sujeita a `must_setup_2fa` no próximo login se a exigência estiver ativa).

**Independent Test**: Simular conta travada (2FA ativa, sem acesso a TOTP/backup); acionar reset administrativo; confirmar que o próximo login funciona só com senha (ou exige novo setup, se `require_2fa` estiver ativo).

---

## Edge Cases

- WHEN um código TOTP da janela adjacente (±1 step de 30s, tolerância de clock drift) é enviado THEN o sistema SHALL aceitá-lo como válido, evitando falsos negativos por pequena dessincronia de relógio.
- WHEN um backup code já usado é enviado novamente THEN o sistema SHALL rejeitá-lo — uso único, sem exceção.
- WHEN o token de step-up é usado mais de uma vez dentro do TTL (replay) THEN o sistema aceita esse risco como trade-off do modelo stateless (ver Out of Scope) — mitigado só pelo TTL curto, não por invalidação ativa.
- WHEN `require_2fa` é ativado num app com usuários finais já logados (sessão JWT ativa) THEN a exigência SHALL só se aplicar a partir do próximo login — sessões já emitidas não são revogadas retroativamente por esta feature.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| TFA-01 | P1: Setup de 2FA | Design | Pending |
| TFA-02 | P1: Setup de 2FA | Design | Pending |
| TFA-03 | P1: Setup de 2FA | Design | Pending |
| TFA-04 | P1: Setup de 2FA | Design | Pending |
| TFA-05 | P1: Setup de 2FA | Design | Pending |
| TFA-06 | P1: Setup de 2FA | Design | Pending |
| TFA-10 | P1: Login com 2FA | Design | Pending |
| TFA-11 | P1: Login com 2FA | Design | Pending |
| TFA-12 | P1: Login com 2FA | Design | Pending |
| TFA-13 | P1: Login com 2FA | Design | Pending |
| TFA-14 | P1: Login com 2FA | Design | Pending |
| TFA-15 | P1: Login com 2FA | Design | Pending |
| TFA-20 | P1: Exigência require_2fa | Design | Pending |
| TFA-21 | P1: Exigência require_2fa | Design | Pending |
| TFA-22 | P1: Exigência require_2fa | Design | Pending |
| TFA-23 | P1: Exigência require_2fa | Design | Pending |
| TFA-24 | P1: Exigência require_2fa | Design | Pending |
| TFA-30 | P2: Fallback administrativo | Design | Pending |
| TFA-31 | P2: Fallback administrativo | Design | Pending |
| TFA-32 | P2: Fallback administrativo | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 20 total, 0 mapeados a tasks, 20 não mapeados ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Setup de 2FA exige confirmação de código válido antes de qualquer persistência como "ativa" — impossível ativar com QR code mal escaneado e ficar travado
- [ ] Login com 2FA nunca depende de estado em memória do processo — verificado sob múltiplas réplicas
- [ ] Usuário não consegue se autoexcluir do próprio login desativando 2FA enquanto `require_2fa` está ativo
- [ ] Fallback administrativo cobre 100% dos casos de conta travada (dashboard e app)
- [ ] Contas Google OAuth nunca são afetadas por nenhum aspecto desta feature (nem setup, nem exigência, nem bloqueio)
- [ ] `internal/twofactor` é usado por ambos os escopos (dashboard e app) sem lógica criptográfica duplicada
