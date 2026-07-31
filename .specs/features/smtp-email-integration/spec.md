# SMTP/Email Integration Specification

## Problem Statement

zeep-orbit hoje não envia nenhum email — não há SMTP, não há convite de usuário, não há recuperação de senha. A única forma de criar um usuário do dashboard é o superadmin definir a senha diretamente via `POST /dashboard/api/users`, e não existe nenhum jeito de um usuário recuperar acesso se esquecer a senha (fora intervenção manual em banco).

Esta feature introduz capacidade de envio de email na plataforma, configurável pelo superadmin, com dois casos de uso concretos desde já: **convite de novo usuário do dashboard** e **recuperação de senha ("esqueci minha senha")**. Ambos os fluxos ficam condicionados à existência de um provider de email ativo — na ausência de configuração, o comportamento atual (superadmin define senha manualmente) permanece intacto, sem regressão.

Suporta tanto SMTP próprio quanto providers de API de email (SendGrid, Amazon SES, Resend) via um mecanismo de provider extensível, análogo ao já desenhado em `.specs/features/observability-integrations/`. Diferente de observability (múltiplos providers simultâneos, fan-out), email tem **um único provider ativo por vez** — não é canal de fan-out.

## Goals

- [ ] Superadmin configura um provider de email (SMTP, SendGrid, SES ou Resend) com credenciais próprias, com botão de teste de envio antes de ativar
- [ ] Convite de novo usuário (sem definição manual de senha) funciona automaticamente quando há provider ativo; sem provider ativo, fluxo atual (senha definida pelo superadmin na criação) continua funcionando sem alteração
- [ ] Recuperação de senha self-service funciona quando há provider ativo, restrita a contas com senha local (contas Google OAuth-only não geram token de reset, evitando abrir um segundo caminho de acesso à conta)
- [ ] Sem provider ativo, existe um fallback administrativo (superadmin define/reseta senha de qualquer usuário diretamente) cobrindo tanto criação quanto recuperação
- [ ] Templates de email (convite, reset) usam dados dinâmicos de marca já existentes (`BrandConfig`: nome da empresa, logo) sem exigir redeploy para refletir mudanças de branding
- [ ] Mecanismo de provider é extensível: adicionar um provider de email novo não exige alterar os endpoints de convite/reset já implementados
- [ ] Nenhum token de convite/reset depende de estado em memória por processo — segue o mesmo padrão stateless (HMAC-SHA256 assinado, com `exp`) já usado em `signState`/`verifyState` do Google OAuth, correto para múltiplas réplicas atrás de load balancer não-sticky

## Out of Scope

| Item | Motivo |
|---|---|
| Editor de template no dashboard (WYSIWYG/markdown, gerenciado pelo superadmin) | Decidido explicitamente no brainstorm: templates ficam como `html/template` versionado no código, com dados dinâmicos de marca já suportados; um editor completo (versionamento, preview, validação de merge field) merece spec próprio se for feito no futuro |
| Outros providers de API além de SendGrid, SES e Resend | Mecanismo é extensível (mesmo padrão de `providerRegistry` de observability); adicionar novo provider é decisão futura, não travada aqui |
| Emails além de convite e recuperação de senha (notificações de deploy, alertas de sistema, digest, etc.) | Escopo desta feature é só os dois casos de uso concretos definidos no brainstorm; outros gatilhos de email são features separadas, a especificar quando surgirem |
| Revogação individual de token antes da expiração natural | Token é stateless por design (sem tabela de "tokens pendentes"); mitigado com expiração curta (convite: 7 dias, reset: 1 hora), não com revogação ativa |
| Login híbrido (conta Google OAuth ganhando senha local via reset) | Avaliado e rejeitado explicitamente no brainstorm por risco de segurança: abriria um segundo caminho de acesso à conta que não passa pela segurança do Google (2FA, etc.) |
| Gestão de múltiplos providers de email ativos simultaneamente (fan-out) | Diferente de observability: email é canal único, só um provider pode estar `active` por vez |

---

## User Stories

### P1: Configuração de provider de email ⭐ MVP

**User Story**: Como superadmin, quero configurar um provider de email (SMTP próprio, SendGrid, SES ou Resend) com suas credenciais, testar o envio antes de ativar, para que a plataforma tenha um canal de email funcional e validado.

**Why P1**: Sem provider configurado, nenhum dos outros fluxos desta feature tem efeito.

**Acceptance Criteria**:

1. WHEN o superadmin salva credenciais de um provider (host/porta/usuário/senha para SMTP; API key para SendGrid/Resend; access key/secret/região para SES) THEN o sistema SHALL persistir os valores sensíveis criptografados (mesmo mecanismo de `crypto.Encrypt` já usado em outras integrações), nunca retornados em claro em nenhuma resposta subsequente.
2. WHEN o superadmin aciona "enviar teste" para um provider configurado THEN o sistema SHALL disparar um envio real para o próprio email do superadmin logado e retornar sucesso/erro explícito, sem persistir o provider como ativo automaticamente.
3. WHEN o superadmin ativa um provider THEN o sistema SHALL garantir que apenas um provider fique `active = true` por vez — ativar um desativa automaticamente qualquer outro anteriormente ativo.
4. WHEN nenhum provider está ativo THEN o sistema SHALL expor esse estado (`IsConfigured() == false`) para os demais fluxos (convite, reset) decidirem seu comportamento.
5. Toda mutação de configuração de provider SHALL gerar evento de auditoria (`InsertAuditLog`).
6. Toda string nova de UI relacionada a esta feature SHALL passar por `react-i18next`, adicionada em `en.json` e `pt-BR.json` na mesma mudança.

**Independent Test**: Configurar cada um dos 4 providers contra um `httptest.Server`/mock correspondente; confirmar que a API key/senha nunca retorna em claro em `GET`; confirmar que ativar um provider desativa o anteriormente ativo.

---

### P1: Convite de usuário condicionado a provider ativo ⭐ MVP

**User Story**: Como superadmin, quero convidar um novo usuário do dashboard por email (sem precisar definir a senha dele), para que o próprio usuário defina sua senha no primeiro acesso — mas, se não houver provider de email configurado, quero continuar podendo criar o usuário definindo a senha diretamente, como hoje.

**Why P1**: É o primeiro caso de uso concreto da capacidade de email, e precisa preservar o fluxo atual sem regressão.

**Acceptance Criteria**:

1. WHEN um provider de email está ativo E o superadmin cria um usuário (`POST /dashboard/api/users`) THEN o sistema SHALL aceitar a requisição sem o campo `password`, criar o usuário com `status: 'pending'` e `password_hash` vazio, gerar um token assinado (`purpose: "invite"`, validade 7 dias) e enviar email de convite com link de aceite.
2. WHEN nenhum provider de email está ativo E o superadmin cria um usuário THEN o sistema SHALL exigir o campo `password` e seguir o comportamento atual (sem alteração), sem enviar nenhum email.
3. WHEN um usuário com `status: 'pending'` tenta logar (senha ou Google) THEN o sistema SHALL negar o acesso até que o convite seja aceito.
4. WHEN o link de convite é acessado com um token válido (assinatura correta, não expirado, `purpose == "invite"`) E uma nova senha (≥8 caracteres) é enviada (`POST /dashboard/api/invite/accept`) THEN o sistema SHALL definir `password_hash` e mudar `status` para `active`.
5. WHEN um token de convite está expirado, tem propósito diferente do esperado, ou assinatura inválida THEN o sistema SHALL retornar erro explícito (400), nunca aceitar silenciosamente.
6. WHEN o superadmin aciona reenvio de convite para um usuário `pending` THEN o sistema SHALL gerar e enviar um novo token, sem invalidar tokens anteriores ainda não expirados (trade-off aceito do modelo stateless — inofensivo, pois usuário ainda não tem senha definida até o primeiro `accept` bem-sucedido).

**Independent Test**: Criar usuário com provider ativo e sem provider ativo, confirmando os dois contratos de request/response; simular aceite de convite com token válido, expirado e de propósito errado; confirmar que login falha para usuário `pending`.

---

### P1: Recuperação de senha condicionada a provider ativo ⭐ MVP

**User Story**: Como usuário do dashboard com senha local, quero poder recuperar minha senha por email se esquecê-la, para não depender de intervenção manual do superadmin — mas, se a plataforma não tiver email configurado, aceito que essa opção não esteja disponível.

**Why P1**: Segundo caso de uso concreto do email, com implicações de segurança específicas (anti-enumeration, restrição a contas com senha local).

**Acceptance Criteria**:

1. WHEN um provider de email está ativo E `POST /dashboard/api/password/forgot` é chamado com um email THEN o sistema SHALL retornar sempre a mesma mensagem genérica de sucesso, independente de o email existir, pertencer a uma conta Google-only, ou pertencer a uma conta com senha local — evitando enumeração de contas.
2. WHEN o email pertence a uma conta com `password_hash` definido E provider está ativo THEN o sistema SHALL, nos bastidores, gerar um token assinado (`purpose: "reset"`, validade 1 hora) e enviar email com link de reset.
3. WHEN o email pertence a uma conta Google OAuth-only (sem `password_hash`) THEN o sistema SHALL NOT gerar token de reset — a conta não ganha senha local por esse fluxo, e a resposta externa continua idêntica ao caso genérico (item 1).
4. WHEN `POST /dashboard/api/password/reset` é chamado com token válido (assinatura correta, não expirado, `purpose == "reset"`) e nova senha (≥8 caracteres) THEN o sistema SHALL atualizar `password_hash`.
5. WHEN nenhum provider de email está ativo THEN `POST /dashboard/api/password/forgot` SHALL retornar erro explícito (não disponível nesta instância), e a UI SHALL esconder a opção "esqueci minha senha" na tela de login.
6. WHEN nenhum provider de email está ativo (ou mesmo com provider ativo, para uso administrativo direto) THEN o sistema SHALL expor `PATCH /dashboard/api/users/{id}/password` (superadmin only) para definir/resetar a senha de qualquer usuário diretamente, auditado.

**Independent Test**: Simular forgot-password para conta com senha, conta Google-only e conta inexistente, confirmando resposta idêntica nos 3 casos e que só o primeiro gera token real; confirmar que sem provider ativo o endpoint retorna erro e o fallback administrativo funciona.

---

### P2: Templates com dados dinâmicos de marca

**User Story**: Como superadmin, quero que os emails de convite e reset reflitam o nome da empresa e logo configurados na plataforma (`BrandConfig`), para que a comunicação pareça consistente com o resto do produto, sem precisar de um novo deploy a cada mudança de marca.

**Why P2**: Refinamento sobre os fluxos P1 — a feature funciona sem isso (com valores default), mas a experiência fica mais coerente com ele.

**Acceptance Criteria**:

1. WHEN um email de convite ou reset é renderizado THEN o sistema SHALL interpolar `CompanyName` e `LogoURL` a partir do `BrandConfig` já existente (`internal/dashboard/brand_store.go`), lido no momento do envio (não cacheado).
2. WHEN `BrandConfig` não retorna nenhuma linha (nunca configurado) THEN o sistema SHALL usar valores default sensatos (ex: "Zeep Orbit"), sem falhar o envio.
3. Templates SHALL ser arquivos `html/template` versionados no repositório (`internal/mail/templates/`), não editáveis via dashboard.

**Independent Test**: Renderizar os 2 templates com um `BrandConfig` preenchido e com `BrandConfig` ausente, confirmando fallback correto em ambos os casos.

---

## Edge Cases

- WHEN o provider ativo é trocado (ex: SMTP → SendGrid) enquanto um convite está pendente THEN o aceite do convite SHALL continuar funcionando normalmente — o token não depende de qual provider enviou o email original.
- WHEN um usuário fica `pending` e o provider de email é desativado antes do aceite THEN o superadmin SHALL poder destravar a conta via `PATCH /dashboard/api/users/{id}/password` (define senha e promove `status` para `active` manualmente).
- WHEN o teste de envio de um provider falha (credenciais inválidas) THEN o sistema SHALL NOT marcar esse provider como `active`, retornando erro antes de qualquer persistência de estado "ativo".
- WHEN um token de propósito errado é usado no endpoint contrário (ex: token de convite em `/password/reset`) THEN o sistema SHALL rejeitar com 400, nunca aceitar silenciosamente.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| MAIL-01 | P1: Configuração de provider | Design | Pending |
| MAIL-02 | P1: Configuração de provider | Design | Pending |
| MAIL-03 | P1: Configuração de provider | Design | Pending |
| MAIL-04 | P1: Configuração de provider | Design | Pending |
| MAIL-05 | P1: Configuração de provider | Design | Pending |
| MAIL-06 | P1: Configuração de provider | Design | Pending |
| MAIL-10 | P1: Convite de usuário | Design | Pending |
| MAIL-11 | P1: Convite de usuário | Design | Pending |
| MAIL-12 | P1: Convite de usuário | Design | Pending |
| MAIL-13 | P1: Convite de usuário | Design | Pending |
| MAIL-14 | P1: Convite de usuário | Design | Pending |
| MAIL-15 | P1: Convite de usuário | Design | Pending |
| MAIL-20 | P1: Recuperação de senha | Design | Pending |
| MAIL-21 | P1: Recuperação de senha | Design | Pending |
| MAIL-22 | P1: Recuperação de senha | Design | Pending |
| MAIL-23 | P1: Recuperação de senha | Design | Pending |
| MAIL-24 | P1: Recuperação de senha | Design | Pending |
| MAIL-25 | P1: Recuperação de senha | Design | Pending |
| MAIL-30 | P2: Templates com dados de marca | Design | Pending |
| MAIL-31 | P2: Templates com dados de marca | Design | Pending |
| MAIL-32 | P2: Templates com dados de marca | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 21 total, 0 mapeados a tasks, 21 não mapeados ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Nenhuma credencial de provider de email é exposta em claro em nenhuma resposta de API após salva
- [ ] Criação de usuário funciona corretamente nos dois modos (com/sem provider ativo), sem regressão no fluxo atual
- [ ] Conta Google OAuth-only nunca ganha senha local via reset — restrição verificada e testada explicitamente
- [ ] Resposta de `forgot-password` é indistinguível entre conta existente, inexistente e Google-only (anti-enumeration)
- [ ] Fallback administrativo (`PATCH /users/{id}/password`) cobre 100% dos casos em que o self-service não está disponível
- [ ] Adicionar um provider de email novo é uma mudança isolada no `providerRegistry`, sem alterar os endpoints de convite/reset
- [ ] Templates refletem `BrandConfig` sem exigir redeploy
