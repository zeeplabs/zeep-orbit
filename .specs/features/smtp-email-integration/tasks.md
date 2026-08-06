# Tasks: SMTP/Email Integration

**Spec**: `.specs/features/smtp-email-integration/spec.md`
**Design**: `.specs/features/smtp-email-integration/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build`), mesmo critério das demais specs.

**Pré-requisito externo**: nenhum. Toda a feature é interna ao zeep-orbit — reusa `BrandConfig`, `signState`/`verifyState` e `dashboard_users` já existentes.

---

## Execution Plan

```
Fase 1: Config + providers                 Fase 2: Tokens + templates
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-01 email_config table    │             │ T-05 Token (HMAC stateless) │
│      + ConfigStore          │───────────▶│ T-06 Templates (html/       │
│ T-02 Sender interface +     │             │      template + BrandConfig)│
│      SMTP/SendGrid/SES/     │             └───────────────────────────┘
│      Resend exporters       │                        │
│ T-03 EmailConfigHandler     │                        ▼
│      (CRUD+activate+test)   │             Fase 3: dashboard_users
└──────────────────────────┘             ┌───────────────────────────┐
                                          │ T-07 coluna status +         │
                                          │      migration               │
                                          └───────────────────────────┘
                                                     │
                                                     ▼
Fase 4: Convite                           Fase 5: Reset de senha
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-08 CreateUser dual-path  │             │ T-10 forgot-password         │
│ T-09 invite/accept +       │───────────▶│      (anti-enumeration)      │
│      resend-invite         │             │ T-11 password/reset          │
└──────────────────────────┘             │ T-12 fallback admin           │
                                          │      (PATCH .../password)    │
                                          └───────────────────────────┘
                                                     │
                                                     ▼
Fase 6: Frontend                          Fase 7: Docs e changelog
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-13 página Email          │             │ T-16 README + CHANGELOG      │
│ T-14 telas públicas         │────────────▶└───────────────────────────┘
│      (invite/forgot/reset)  │
│ T-15 i18n en/pt-BR          │
└──────────────────────────┘
```

---

### T-01: Tabela `email_config` + `ConfigStore`

- **What**: Migration criando `zeep_system.email_config` (`UNIQUE(provider)`, `credentials jsonb`). `ConfigStore`: `UpsertConfig`, `Activate` (transação: desativa os demais antes de ativar o alvo), `ListConfigs`, `IsConfigured`. Valores sensíveis dentro de `credentials` sempre cifrados individualmente com `crypto.Encrypt` antes de compor o JSON.
- **Where**: migration nova, `internal/mail/config_store.go`
- **Depends on**: nenhuma
- **Reuses**: `crypto.Encrypt`/`Decrypt`, padrão de tabela de `deploy_provider_configs`
- **Requirement**: MAIL-01, MAIL-03, MAIL-04
- **Tools**: nenhum
- **Done when**: `Activate` garante exatamente 1 linha `active=true` mesmo sob chamadas concorrentes (teste com `-race`)
- **Tests**: `config_store_test.go` — upsert, ativação alternando entre providers, `IsConfigured` antes/depois
- **Gate**: `go test -race ./internal/mail/... -run TestConfigStore`
- **Commit**: não (agrupa com T-02/T-03)

---

### T-02: `Sender` interface + 4 exporters

- **What**: Interface `Sender.Send(ctx, to, subject, htmlBody) error`, `providerRegistry`, implementações `newSMTPSender` (`net/smtp`), `newSendGridSender`, `newSESSender` (SigV4), `newResendSender`. `ActiveSender(ctx, pool)` resolve o `Sender` do provider `active` atual.
- **Where**: `internal/mail/sender.go`, `smtp.go`, `sendgrid.go`, `ses.go`, `resend.go`
- **Depends on**: T-01
- **Reuses**: `net/http`/`net/smtp` diretos, sem lib externa pesada
- **Requirement**: MAIL-01, MAIL-02
- **Tools**: nenhum
- **Done when**: cada exporter valida contra `httptest.Server` (ou mock de socket SMTP), payload/header de auth corretos
- **Tests**: 1 arquivo de teste por exporter, matriz de sucesso/erro (401 do provider, timeout)
- **Gate**: `go test ./internal/mail/... -run TestSender`
- **Commit**: não (agrupa com T-01/T-03)

---

### T-03: `EmailConfigHandler` (CRUD + activate + test)

- **What**: `GET/PUT /dashboard/api/email-config`, `POST .../activate`, `POST .../test` — todos superadmin-only, nunca retornam credenciais em claro, `test` nunca marca `active` automaticamente. Toda mutação via `InsertAuditLog`.
- **Where**: `internal/dashboard/email_config.go`
- **Depends on**: T-01, T-02
- **Reuses**: esqueleto de `DeployProviderConfigHandler`, `h.audit(...)`
- **Requirement**: MAIL-01, MAIL-02, MAIL-03, MAIL-05
- **Tools**: nenhum
- **Done when**: `GET` nunca expõe `credentials` em claro; `test` com credencial errada não altera `active`
- **Tests**: teste HTTP cobrindo os 4 endpoints, incluindo teste de credencial inválida
- **Gate**: `go test ./internal/dashboard/... -run TestEmailConfig`
- **Commit**: `feat(mail): add multi-provider email config (SMTP/SendGrid/SES/Resend)` (T-01+T-02+T-03)

---

### T-04: (reservado — sem task; numeração mantida alinhada ao design)

*(Task removida na consolidação — T-01/T-02/T-03 já cobrem toda a Fase 1. Numeração das tasks seguintes preservada para rastreabilidade com o design.)*

---

### T-05: Token stateless (`SignToken`/`VerifyToken`)

- **What**: `SignToken(userID, purpose, ttl) (string, error)`, `VerifyToken(token, want) (userID string, err error)` — mesmo formato HMAC-SHA256 de `signState`/`verifyState`, com `purpose` (`invite`/`reset`) no payload. Erro único e genérico pra qualquer falha de validação (expirado, adulterado, propósito errado) — não distinguir motivo exato na resposta pública.
- **Where**: `internal/mail/token.go`
- **Depends on**: nenhuma
- **Reuses**: formato de `signState`/`verifyState` (`internal/dashboard/google.go`)
- **Requirement**: MAIL-11, MAIL-14, MAIL-15, MAIL-22, MAIL-24
- **Tools**: nenhum
- **Done when**: token válido, expirado, propósito errado e assinatura adulterada resolvem corretamente, todos com o mesmo tipo de erro externo
- **Tests**: tabela de casos em `token_test.go`
- **Gate**: `go test ./internal/mail/... -run TestToken`
- **Commit**: não (agrupa com T-06)

---

### T-06: Templates (`html/template` + `BrandConfig`)

- **What**: `templates/invite.html`, `templates/reset.html` (embutidos via `embed.FS`), `RenderInvite`/`RenderPasswordReset` interpolando `TemplateData` (`CompanyName`, `LogoURL` de `GetBrandConfig`, com fallback default se `BrandConfig` ausente).
- **Where**: `internal/mail/templates/`, `internal/mail/render.go`
- **Depends on**: nenhuma
- **Reuses**: `internal/dashboard.GetBrandConfig` (existente, sem tabela nova)
- **Requirement**: MAIL-30, MAIL-31, MAIL-32
- **Tools**: nenhum
- **Done when**: renderização com `BrandConfig` preenchido e ausente produzem HTML válido em ambos os casos
- **Tests**: `render_test.go` — 2 cenários (com/sem brand config)
- **Gate**: `go test ./internal/mail/... -run TestRender`
- **Commit**: `feat(mail): add stateless invite/reset tokens and email templates` (T-05+T-06)

---

### T-07: Coluna `status` em `dashboard_users`

- **What**: Migration `ALTER TABLE ... ADD COLUMN status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','pending'))`. Login (senha e Google) passa a checar `status == 'active'` antes de validar credencial.
- **Where**: migration nova, `internal/dashboard/store.go` (struct `DashboardUser`), handler de login
- **Depends on**: nenhuma
- **Reuses**: tabela existente, sem tabela nova
- **Requirement**: MAIL-12
- **Tools**: nenhum
- **Done when**: usuário `pending` não consegue logar por nenhum dos 2 métodos; usuários existentes (default `active`) não são afetados
- **Tests**: teste de login com usuário `pending` e `active`
- **Gate**: `go test ./internal/dashboard/... -run TestLoginStatus`
- **Commit**: não (agrupa com T-08)

---

### T-08: `CreateUser` dual-path

- **What**: `POST /dashboard/api/users` consulta `mail.IsConfigured()`: se `true`, aceita sem `password`, cria com `status: 'pending'`, gera token `invite` (7 dias) e envia email; se `false`, comportamento atual inalterado (exige `password`).
- **Where**: `internal/dashboard/handler.go` (edição de `CreateUser`)
- **Depends on**: T-03, T-05, T-06, T-07
- **Reuses**: `bcrypt.GenerateFromPassword` já usado, `h.audit(...)`
- **Requirement**: MAIL-10, MAIL-11
- **Tools**: nenhum
- **Done when**: os 2 contratos de request (com/sem `password`) funcionam corretamente conforme `IsConfigured()`
- **Tests**: teste HTTP para os 2 cenários (provider ativo/inativo)
- **Gate**: `go test ./internal/dashboard/... -run TestCreateUser`
- **Commit**: não (agrupa com T-09)

---

### T-09: `invite/accept` + `resend-invite`

- **What**: `POST /dashboard/api/invite/accept` `{token, password}` (público) valida token (`purpose: invite`), define `password_hash`, muda `status` para `active`. `POST /dashboard/api/users/{id}/resend-invite` (superadmin only) gera novo token e reenvia, sem invalidar o anterior (trade-off documentado no spec).
- **Where**: `internal/dashboard/invite.go`
- **Depends on**: T-08
- **Reuses**: `internal/mail.VerifyToken`, `bcrypt`
- **Requirement**: MAIL-13, MAIL-14, MAIL-15
- **Tools**: nenhum
- **Done when**: aceite com token válido promove o usuário; token expirado/adulterado/propósito errado retorna 400
- **Tests**: matriz de token (válido/expirado/propósito errado) + teste de reenvio
- **Gate**: `go test ./internal/dashboard/... -run TestInviteAccept`
- **Commit**: `feat(mail): add email-based user invite flow with dual-path fallback` (T-07+T-08+T-09)

---

### T-10: `password/forgot` (anti-enumeration)

- **What**: `POST /dashboard/api/password/forgot` `{email}` (público) — resposta sempre idêntica (200, mensagem genérica) independente de: email inexistente, conta Google-only, conta com senha local. Só o último caso gera token real e dispara email.
- **Where**: `internal/dashboard/password_reset.go`
- **Depends on**: T-05, T-06
- **Reuses**: `internal/mail.SignToken`, `internal/mail.RenderPasswordReset`
- **Requirement**: MAIL-20, MAIL-21, MAIL-22, MAIL-23
- **Tools**: nenhum
- **Done when**: os 3 cenários (inexistente/Google-only/com senha) retornam resposta idêntica ao chamador, apenas o terceiro dispara envio real (verificável via mock do `Sender`)
- **Tests**: 3 casos, cada um verificando resposta E se `Sender.Send` foi chamado ou não
- **Gate**: `go test ./internal/dashboard/... -run TestForgotPassword`
- **Commit**: não (agrupa com T-11/T-12)

---

### T-11: `password/reset`

- **What**: `POST /dashboard/api/password/reset` `{token, password}` (público) — valida token (`purpose: reset`), atualiza `password_hash`. Nunca cria senha para conta que virou Google-only entre o pedido e o uso do token (rechecar `password_hash != ''` no momento do reset, não só no momento do forgot).
- **Where**: `internal/dashboard/password_reset.go`
- **Depends on**: T-10
- **Reuses**: `internal/mail.VerifyToken`, `bcrypt`
- **Requirement**: MAIL-24
- **Tools**: nenhum
- **Done when**: reset funciona com token válido; falha com token de propósito errado, expirado, ou se a conta não tem mais `password_hash` (edge case de mudança de estado entre forgot e reset)
- **Tests**: matriz de token + cenário de conta mudada para Google-only no meio do fluxo
- **Gate**: `go test ./internal/dashboard/... -run TestPasswordReset`
- **Commit**: não (agrupa com T-12)

---

### T-12: Fallback administrativo (`PATCH /users/{id}/password`)

- **What**: Endpoint superadmin-only para definir/resetar senha de qualquer usuário diretamente (≥8 caracteres, mesma validação de `CreateUser`), cobrindo tanto instâncias sem provider ativo quanto necessidade administrativa direta. Também usado para destravar usuário `pending` preso (define senha e promove `status` para `active` manualmente).
- **Where**: `internal/dashboard/handler.go` (novo handler)
- **Depends on**: T-07
- **Reuses**: `bcrypt.GenerateFromPassword`, `h.audit(...)` (`user.password_reset_by_admin`)
- **Requirement**: MAIL-25
- **Tools**: nenhum
- **Done when**: endpoint funciona independente do estado de `IsConfigured()`; promove `pending` → `active` quando aplicável
- **Tests**: teste HTTP cobrindo usuário `active` e `pending`
- **Gate**: `go test ./internal/dashboard/... -run TestAdminPasswordReset`
- **Commit**: `feat(mail): add self-service password reset with admin fallback` (T-10+T-11+T-12)

---

### T-13: Página "Email" (config de provider)

- **What**: Seletor de provider (SMTP/SendGrid/SES/Resend), formulário condicional por provider, indicação de ativo, botão "Enviar teste".
- **Where**: `internal/dashboard/ui/src/pages/EmailConfigPage.tsx`
- **Depends on**: T-03
- **Reuses**: componentes de formulário/layout existentes, `toast.error` padrão
- **Requirement**: MAIL-01, MAIL-02, MAIL-03
- **Tools**: nenhum
- **Done when**: os 4 providers renderizam formulário correto, teste de envio mostra sucesso/erro via toast
- **Tests**: teste de componente cobrindo troca de provider e estado ativo
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-14/T-15)

---

### T-14: Telas públicas (invite/forgot/reset)

- **What**: `/invite?token=...` (define senha), `/forgot-password` (pede email, só aparece se `IsConfigured()` via `usePublicConfig`), `/reset-password?token=...` (define nova senha). Campo de senha em criação de usuário some/aparece conforme `IsConfigured()`.
- **Where**: `internal/dashboard/ui/src/pages/InviteAcceptPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`, edição da página de criação de usuário existente
- **Depends on**: T-09, T-10, T-11
- **Reuses**: `usePublicConfig()`, `toast.error`
- **Requirement**: MAIL-11, MAIL-25 (UI correspondente)
- **Tools**: nenhum
- **Done when**: as 3 telas renderizam e chamam os endpoints corretos; "esqueci senha" só aparece com provider ativo
- **Tests**: teste de componente por tela (estado de sucesso/erro)
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-15)

---

### T-15: i18n das strings novas

- **What**: Todas as strings de T-13/T-14 (e mensagens de erro dos endpoints consumidas no frontend) em `en.json` e `pt-BR.json` na mesma mudança.
- **Where**: `internal/dashboard/ui/src/locales/en.json`, `pt-BR.json`
- **Depends on**: T-13, T-14
- **Reuses**: `react-i18next` já configurado
- **Requirement**: MAIL-06
- **Tools**: nenhum
- **Done when**: validação JSON dos 2 arquivos passa, nenhuma string hardcoded restante
- **Tests**: validação JSON (`python3 -c "import json; json.load(...)"`)
- **Gate**: validação JSON + `npx tsc -b` + `npm run build`
- **Commit**: `feat(mail): add Email config page, public invite/reset screens and i18n` (T-13+T-14+T-15)

---

### T-16: README e CHANGELOG

- **What**: Documentar as novas tabelas/env vars relevantes (ex: segredo HMAC do token, se for dedicado e não reusar o do Google OAuth — decidir no início da implementação) no `README.md`; entrada em `CHANGELOG.md` sob `## [Unreleased]`.
- **Where**: `README.md`, `CHANGELOG.md`
- **Depends on**: T-01 até T-15
- **Reuses**: convenção existente de `## [Unreleased]`
- **Requirement**: nenhum ID específico — item de processo (`AGENTS.md` seção 6)
- **Tools**: nenhum
- **Done when**: entrada no CHANGELOG presente na mesma mudança que fecha a feature
- **Tests**: nenhum
- **Gate**: revisão visual do diff de `README.md`/`CHANGELOG.md`
- **Commit**: `docs: document SMTP/email integration configuration and changelog entry`

---

## Notas de execução

- Fases 1 e 2 (T-01 a T-06) não dependem uma da outra e podem ser feitas em paralelo.
- T-05 decide se reusa o segredo HMAC de `signState` ou usa um dedicado só para `internal/mail` — qualquer escolha é válida, mas deve ficar documentada no README (T-16) e não pode ser hardcoded.
- Nenhum editor de template é implementado (fora de escopo, ver spec.md) — templates são arquivos versionados, alterá-los é mudança de código normal.
- Nenhuma feature de email além de convite/reset (notificações, alertas) é coberta aqui — mecanismo (`Sender`, templates) fica pronto para consumo futuro por outras specs.
