# Tasks: Two-Factor Authentication (2FA)

**Spec**: `.specs/features/two-factor-auth/spec.md`
**Design**: `.specs/features/two-factor-auth/design.md`
**Status**: Draft

> Convenção de Gate: sem `TESTING.md` no repo — inferido do `Makefile` (`go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build`), mesmo critério das demais specs.

**Pré-requisito externo**: nenhum. Toda a feature é interna. Depende de decidir em T-01 se o token de step-up reusa `internal/mail.SignToken` (introduzindo dependência de `smtp-email-integration`, hoje só especificada/commitada, não implementada) ou ganha um assinador HMAC próprio dentro de `internal/twofactor` — recomendação: assinador próprio, para não acoplar 2FA à implementação futura de email.

---

## Execution Plan

```
Fase 1: Núcleo criptográfico               Fase 2: Dashboard
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-01 TOTP + token pending  │────────────▶│ T-04 tabela +               │
│ T-02 Backup codes          │             │      TwoFactorHandler       │
│ T-03 (reservado)           │             │      (setup/confirm/disable)│
└──────────────────────────┘             │ T-05 login dashboard step-up│
                                          │ T-06 require_2fa global +   │
                                          │      reset admin            │
                                          └───────────────────────────┘
                                                     │
                                                     ▼
Fase 3: App (internal/auth)               Fase 4: Frontend
┌──────────────────────────┐             ┌───────────────────────────┐
│ T-07 tabela {schema}.      │             │ T-10 SecurityPage           │
│      users_2fa + handler   │────────────▶│      (dashboard)            │
│ T-08 login app step-up     │             │ T-11 config 2FA na página   │
│ T-09 require_2fa por app + │             │      de config do app        │
│      reset admin do app    │             │ T-12 i18n en/pt-BR          │
└──────────────────────────┘             └───────────────────────────┘
                                                     │
                                                     ▼
                                          Fase 5: Docs e changelog
                                          ┌───────────────────────────┐
                                          │ T-13 README + CHANGELOG      │
                                          └───────────────────────────┘
```

---

### T-01: `internal/twofactor` — TOTP + token de step-up

- **What**: `GenerateSecret`, `ProvisioningURI`, `VerifyTOTP` (RFC 6238, SHA1, janela ±1 step de 30s), `SignPendingToken`/`VerifyPendingToken` (HMAC-SHA256 próprio do pacote, payload `{user_id, scope, purpose: "2fa_pending", exp}`, TTL 5 min).
- **Where**: `internal/twofactor/totp.go`, `internal/twofactor/token.go`
- **Depends on**: nenhuma
- **Reuses**: `internal/crypto` (Encrypt/Decrypt do secret), formato de `signState` como referência (implementação própria, sem importar `internal/dashboard`)
- **Requirement**: TFA-01, TFA-10, TFA-11
- **Tools**: nenhum externo — gerar TOTP de teste manualmente no `_test.go` usando o mesmo secret
- **Done when**: código gerado a partir de um secret de teste é aceito por `VerifyTOTP`; código de janela adjacente também é aceito; código de janela distante é rejeitado
- **Tests**: tabela de casos (código correto, ±1 step, fora da janela, secret corrompido); token pending válido/expirado/adulterado
- **Gate**: `go test ./internal/twofactor/... -run TestTOTP`
- **Commit**: não (agrupa com T-02)

---

### T-02: Backup codes

- **What**: `GenerateBackupCodes` (8 códigos formato `XXXX-XXXX`, hash bcrypt), `VerifyBackupCode` (retorna lista de hashes restante sem o usado, ou erro se não encontrado).
- **Where**: `internal/twofactor/backup_codes.go`
- **Depends on**: nenhuma
- **Reuses**: `bcrypt` (mesmo já usado em `CreateUser`)
- **Requirement**: TFA-02, TFA-05, TFA-13
- **Tools**: nenhum
- **Done when**: code válido é consumido (removido da lista), reenvio do mesmo code falha
- **Tests**: geração produz 8 códigos únicos; verificação de uso único; regeneração descarta lista anterior
- **Gate**: `go test ./internal/twofactor/... -run TestBackupCodes`
- **Commit**: `feat(twofactor): add shared TOTP core, pending tokens and backup codes` (T-01+T-02)

---

### T-04: Tabela `dashboard_users_2fa` + `TwoFactorHandler` (setup/confirm/disable)

- **What**: Migration criando `zeep_system.dashboard_users_2fa`. Handler com `POST /dashboard/api/2fa/setup`, `/confirm`, `/disable` — `disable` retorna 403 se `require_2fa` estiver ativo pra essa conta.
- **Where**: migration nova, `internal/dashboard/twofactor.go`
- **Depends on**: T-01, T-02
- **Reuses**: `crypto.Encrypt`, `InsertAuditLog`
- **Requirement**: TFA-01, TFA-02, TFA-03, TFA-04, TFA-22
- **Tools**: nenhum
- **Done when**: setup sem confirmação não persiste nada; confirmação com código errado não ativa 2FA; disable bloqueado corretamente quando `require_2fa` ativo
- **Tests**: teste HTTP cobrindo os 3 endpoints + cenário de disable bloqueado
- **Gate**: `go test ./internal/dashboard/... -run TestTwoFactorSetup`
- **Commit**: não (agrupa com T-05/T-06)

---

### T-05: Login do dashboard com step-up

- **What**: Handler de login existente do dashboard passa a checar `dashboard_users_2fa.enabled`: sem 2FA e sem `require_2fa` → JWT normal; sem 2FA e `require_2fa` ativo → `must_setup_2fa: true`; com 2FA → token pending. Novo endpoint `POST /dashboard/api/login/2fa` completa o fluxo, com rate limit.
- **Where**: `internal/dashboard/handler.go` (edição do handler de login), `internal/dashboard/twofactor.go`
- **Depends on**: T-04
- **Reuses**: rate limiter existente (adaptado/reusado do padrão de `internal/auth/ratelimit.go`)
- **Requirement**: TFA-10, TFA-11, TFA-12, TFA-13, TFA-14
- **Tools**: nenhum
- **Done when**: os 3 ramos de login (sem 2FA, `must_setup_2fa`, com 2FA) retornam o contrato certo; rate limit ativa após N tentativas erradas em `/login/2fa`
- **Tests**: teste de login cobrindo os 3 ramos + teste de rate limit
- **Gate**: `go test ./internal/dashboard/... -run TestLoginTwoFactor`
- **Commit**: não (agrupa com T-06)

---

### T-06: `require_2fa` global (dashboard) + reset administrativo

- **What**: `GET/PUT /dashboard/api/platform-config/require-2fa` (superadmin only); `POST /dashboard/api/users/{id}/2fa/reset` (superadmin only, remove secret/backup codes de outro admin, auditado).
- **Where**: `internal/dashboard/twofactor.go`, migration de config de plataforma (coluna `require_2fa`, tabela exata a confirmar — ver Data Models do design)
- **Depends on**: T-04, T-05
- **Reuses**: `InsertAuditLog`
- **Requirement**: TFA-20, TFA-21, TFA-23, TFA-24, TFA-30
- **Tools**: nenhum
- **Done when**: toggle liga/desliga corretamente; reset administrativo funciona independente do estado de `require_2fa`; desligar `require_2fa` não desativa 2FA já ativa em nenhuma conta
- **Tests**: teste HTTP cobrindo toggle e reset, mais o cenário de "desligar não desativa 2FA existente"
- **Gate**: `go test ./internal/dashboard/... -run TestRequire2FA`
- **Commit**: `feat(twofactor): add dashboard 2FA setup, step-up login and require_2fa enforcement` (T-04+T-05+T-06)

---

### T-07: Tabela `{schema}.users_2fa` + handler no `internal/auth`

- **What**: Migration por-app (`{schema}.users_2fa`, aplicada no provisionamento de app, mesmo padrão de outras tabelas de sistema por app). Handler espelhando T-04, escopado ao app da rota.
- **Where**: `internal/provisioner` (migration por-app), `internal/auth/twofactor.go`
- **Depends on**: T-01, T-02
- **Reuses**: mesmo código de `TwoFactorHandler` do dashboard, adaptado (idealmente uma função compartilhada parametrizada por schema/tabela, evitando duplicar a lógica de setup/confirm/disable — decidir estrutura exata na implementação)
- **Requirement**: TFA-01, TFA-02, TFA-03, TFA-04, TFA-22
- **Tests**: espelha T-04, escopado a um app de teste
- **Gate**: `go test ./internal/auth/... -run TestTwoFactorSetup`
- **Commit**: não (agrupa com T-08/T-09)

---

### T-08: Login de app com step-up

- **What**: Espelha T-05 no `internal/auth.Handler.Login` — mesmos 3 ramos, escopado ao app.
- **Where**: `internal/auth/handler.go` (edição de `Login`), `internal/auth/twofactor.go`
- **Depends on**: T-07
- **Reuses**: `internal/auth/ratelimit.go` (já existente, reuso direto)
- **Requirement**: TFA-10, TFA-11, TFA-12, TFA-13, TFA-14
- **Tests**: espelha T-05
- **Gate**: `go test ./internal/auth/... -run TestLoginTwoFactor`
- **Commit**: não (agrupa com T-09)

---

### T-09: `require_2fa` por app + reset administrativo

- **What**: Campo `require_2fa` em `config.AppConfig` (config já existente do app), editável pelo criador/admin do app. `POST /v1/{app}/admin/users/{id}/2fa/reset` equivalente ao T-06 do dashboard.
- **Where**: `internal/config` (struct `AppConfig`), `internal/auth/twofactor.go`
- **Depends on**: T-07, T-08
- **Reuses**: `InsertAuditLog`
- **Requirement**: TFA-20, TFA-21, TFA-23, TFA-24, TFA-31, TFA-32
- **Tests**: espelha T-06, escopado a um app de teste
- **Gate**: `go test ./internal/auth/... -run TestRequire2FA`
- **Commit**: `feat(twofactor): add per-app 2FA setup, step-up login and require_2fa` (T-07+T-08+T-09)

---

### T-10: `SecurityPage` (dashboard)

- **What**: Página de perfil com toggle "Ativar 2FA" (fluxo QR code via lib nova de QR — ver Tech Decisions do design — + confirmação + exibição única dos backup codes com aviso de "salve agora").
- **Where**: `internal/dashboard/ui/src/pages/SecurityPage.tsx`, `components/TwoFactorSetupModal.tsx`
- **Depends on**: T-06
- **Reuses**: componentes de formulário/modal existentes, `toast.error`
- **Requirement**: TFA-01, TFA-02, TFA-04, TFA-05
- **Tests**: teste de componente cobrindo fluxo de setup completo e exibição única dos backup codes
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-11/T-12)

---

### T-11: Config de 2FA na página do app (criador do app)

- **What**: Toggle "Permitir 2FA" + toggle "Exigir 2FA" (só habilitável se o primeiro estiver ligado) na página de configuração do app.
- **Where**: página de config de app já existente no dashboard (a localizar na implementação)
- **Depends on**: T-09
- **Reuses**: componentes de formulário existentes
- **Requirement**: TFA-20, TFA-21
- **Tests**: teste de componente cobrindo dependência entre os 2 toggles
- **Gate**: `npx tsc -b`
- **Commit**: não (agrupa com T-12)

---

### T-12: i18n das strings novas

- **What**: Todas as strings de T-10/T-11 (incluindo texto de aviso dos backup codes) em `en.json` e `pt-BR.json`.
- **Where**: `internal/dashboard/ui/src/locales/en.json`, `pt-BR.json`
- **Depends on**: T-10, T-11
- **Reuses**: `react-i18next` já configurado
- **Requirement**: TFA-06
- **Tools**: nenhum
- **Done when**: validação JSON dos 2 arquivos passa, nenhuma string hardcoded restante
- **Tests**: validação JSON
- **Gate**: validação JSON + `npx tsc -b` + `npm run build`
- **Commit**: `feat(twofactor): add Security page, per-app 2FA config and i18n` (T-10+T-11+T-12)

---

### T-13: README e CHANGELOG

- **What**: Documentar decisão de assinador HMAC próprio (T-01) e dependência de lib de QR code no `README.md`; entrada em `CHANGELOG.md` sob `## [Unreleased]`.
- **Where**: `README.md`, `CHANGELOG.md`
- **Depends on**: T-01 até T-12
- **Reuses**: convenção existente de `## [Unreleased]`
- **Requirement**: nenhum ID específico — item de processo (`AGENTS.md` seção 6)
- **Tests**: nenhum
- **Gate**: revisão visual do diff
- **Commit**: `docs: document two-factor authentication configuration and changelog entry`

---

## Notas de execução

- Fase 1 (T-01/T-02) é pré-requisito de tudo, mas não depende de nada — pode começar imediatamente.
- Fases 2 (dashboard) e 3 (app) são estruturalmente espelhadas — considerar extrair uma função/struct genérica parametrizada por schema/tabela na implementação, em vez de duplicar `TwoFactorHandler` integralmente entre `internal/dashboard` e `internal/auth`.
- T-01 decide se reusa formato de token de outro pacote ou implementa assinador HMAC próprio — recomendação do design é próprio, para não criar dependência de `smtp-email-integration` (ainda não implementada) só por causa do formato de token.
- Lib de QR code no frontend é a única exceção de dependência nova desta spec — documentar a escolha exata (ex: `qrcode.react`) no README ao implementar T-10.
