# Sync Local↔Repo Specification

## Problem Statement

Um frontend app criado (sub-feature 2) já existe como repositório privado no GitHub, mas o usuário não-técnico não tem como levar o projeto que constrói localmente (com Claude Code, Codex ou ferramenta similar) até esse repositório sem tocar diretamente no GitHub da empresa. Esta é a terceira de quatro sub-features do projeto "Deploy self-service de frontend" (GitHub Integration ✅ → Frontend App Entity ✅ → **Sync Local↔Repo** → Deploy Provider Integration), e resolve a peça historicamente mais nebulosa da iniciativa: como o código local vira commits no repo.

## Goals

- [ ] Todo frontend app criado já sai com credencial de sync gerada (deploy key SSH), sem passo manual extra do usuário
- [ ] Usuário recebe comandos git prontos (clone ou remote add) + chave privada + prompt copiável para seu agente de IA configurar tudo sozinho
- [ ] Falha na geração/registro da credencial não impede o uso do frontend app — fica marcada e permite retry
- [ ] Usuário pode regenerar a credencial a qualquer momento (perda, suspeita de vazamento) sem recriar o frontend app
- [ ] Remoção do frontend app revoga a credencial de sync, não deixa chave órfã ativa no GitHub

## Out of Scope

| Feature | Reason |
|---|---|
| CLI dedicada do zeep-orbit (`zeep-orbit push`) | Estudo futuro — custo de build multiplataforma + autenticação própria, avaliado e descartado nesta fase |
| Proxy git no backend (zeep-orbit como git remote HTTPS) | Estudo futuro — exige relay smart-HTTP próprio, engenharia não trivial pra este estágio |
| Fluxo "conectar pasta" com watch automático | Fora de escopo — push é ação explícita do usuário/agente, não automático |
| Deploy automático pós-push | Sub-feature seguinte (Deploy Provider Integration) |
| Detecção de SO / geração dinâmica do prompt do agente | YAGNI — template estático interpolado no frontend resolve o MVP |
| Histórico de rotação de chave (log completo de regenerações) | Não pedido — `audit_log` já cobre o essencial |
| RBAC sobre quem pode regenerar/revelar credencial | Depende de M3 (RBAC ainda não implementado) — mesma aceitação de gap já feita na sub-feature 2 |

---

## User Stories

### P1: Sistema gera credencial de sync na criação do frontend app ⭐ MVP

**User Story**: Como usuário, quero que meu frontend app já saia pronto pra sincronizar com meu projeto local, sem precisar de um passo manual extra depois de criar o app.

**Why P1**: É a base de toda a sub-feature — sem credencial gerada, não há o que revelar, copiar ou usar.

**Acceptance Criteria**:

1. WHEN um frontend app é criado com sucesso (`status: ready`) THEN o sistema SHALL gerar um par de chaves SSH e registrar a chave pública como deploy key no repositório GitHub correspondente, na mesma requisição síncrona.
2. WHEN a geração do par de chaves ou o registro no GitHub falha THEN o sistema SHALL persistir `sync_status: pending` com `error_message`, sem falhar a criação do frontend app em si.
3. WHEN a credencial é gerada com sucesso THEN o sistema SHALL persistir `sync_status: ready`, o `github_key_id` retornado pelo GitHub, a chave pública e a chave privada criptografada.

**Independent Test**: Criar frontend app; `GET /api/frontend-apps/{id}/sync` retorna `sync_status: ready` e `public_key` preenchido; deploy key aparece nas configurações do repo no GitHub.

---

### P1: Usuário revela credencial e recebe prompt pro agente de IA ⭐ MVP

**User Story**: Como usuário, quero copiar um prompt pronto e colar no meu agente de IA (Claude Code, Codex), para que ele configure sozinho o SSH e o git remote do meu projeto local.

**Why P1**: É a superfície de uso real da sub-feature — sem isso, a credencial gerada não chega a ser usada.

**Acceptance Criteria**:

1. WHEN o usuário chama `POST /api/frontend-apps/{id}/reveal-key` em um app com `sync_status: ready` THEN o sistema SHALL descriptografar e retornar a chave privada uma única vez por chamada, sem armazenar em cache.
2. WHEN a chave é revelada THEN o sistema SHALL registrar entrada em `audit_log` com o id do frontend app e o usuário que revelou.
3. WHEN o usuário acessa a tela de detalhe do frontend app THEN o sistema SHALL exibir os dois comandos git (clone e remote add) e um template de prompt copiável, com placeholders (slug, URL do repo, comando de setup SSH) prontos para interpolação no frontend.
4. WHEN o app tem `sync_status: pending` ou `failed` THEN o `reveal-key` SHALL ser rejeitado com erro claro, sem chave para revelar.

**Independent Test**: Revelar chave de um app `ready`; confirmar retorno da chave privada e entrada correspondente em `audit_log`; confirmar que revelar em um app `pending` retorna erro sem vazar dado.

---

### P1: Sistema preserva falha de setup de sync e permite retry ⭐ MVP

**User Story**: Como usuário, quero que uma falha temporária ao configurar meu sync (rate limit, rede) não me deixe sem opção — quero poder tentar de novo sem recriar o frontend app.

**Why P1**: Sem retry, uma falha transitória na etapa de credencial deixaria o frontend app permanentemente sem forma de sync.

**Acceptance Criteria**:

1. WHEN o usuário chama `POST /api/frontend-apps/{id}/sync/retry` em um app com `sync_status: pending` ou `failed` THEN o sistema SHALL tentar novamente gerar o par de chaves e registrar no GitHub, atualizando o mesmo registro de credencial.
2. WHEN o retry é chamado em um app com `sync_status: ready` THEN o sistema SHALL rejeitar com erro claro, sem reprocessar.
3. WHEN o retry é bem-sucedido THEN o sistema SHALL transicionar `sync_status` para `ready` e limpar `error_message`.

**Independent Test**: Forçar falha na geração de credencial; confirmar `sync_status: pending`; corrigir causa e chamar retry; confirmar transição para `ready`.

---

### P1: Usuário regenera credencial de sync ⭐ MVP

**User Story**: Como usuário, quero poder gerar uma nova credencial de sync a qualquer momento (perdi a chave privada, ou suspeito que vazou), sem precisar recriar o frontend app.

**Why P1**: Chave privada revelada uma única vez — sem regenerar, perda de acesso é irreversível.

**Acceptance Criteria**:

1. WHEN o usuário chama `POST /api/frontend-apps/{id}/sync/regenerate` em um app com `sync_status: ready` THEN o sistema SHALL tentar revogar a deploy key atual no GitHub, gerar um novo par de chaves, registrar a nova chave pública e atualizar o mesmo registro de credencial.
2. WHEN a revogação da chave antiga no GitHub falha THEN o sistema SHALL prosseguir mesmo assim com a geração e registro da nova chave, sem bloquear a regeneração (best-effort na revogação).
3. WHEN a regeneração é concluída THEN o sistema SHALL registrar entrada em `audit_log`.

**Independent Test**: Regenerar credencial de um app `ready`; confirmar nova `public_key` diferente da anterior; confirmar chamada de revogação da chave antiga registrada (mesmo que simulada como falha, regeneração completa com sucesso).

---

### P1: Remoção do frontend app revoga a credencial de sync ⭐ MVP

**User Story**: Como usuário, quero que remover um frontend app também invalide o acesso de sync associado, para não deixar uma chave ativa esquecida na org GitHub.

**Why P1**: Consistente com o archive do repo já feito na sub-feature 2 — sem isso, a chave sobrevive ao app que a criou.

**Acceptance Criteria**:

1. WHEN um frontend app é removido (`DELETE /api/frontend-apps/{id}`) THEN o sistema SHALL tentar revogar a deploy key associada no GitHub, além do archive do repo já existente.
2. WHEN a revogação da chave falha THEN o sistema SHALL manter o soft delete do frontend app (não reverte), registrando o erro apenas em log — sem retry automático, mesmo padrão já aceito para o archive do repo.

**Independent Test**: Remover frontend app com `sync_status: ready`; confirmar tentativa de revogação da deploy key no GitHub (sucesso ou falha logada) sem impedir a remoção do app.

---

## Edge Cases

- WHEN o usuário perde a chave privada revelada (não guardou) THEN não há como recuperar — única opção é regenerar (nova chave, chave antiga revogada).
- WHEN `regenerate` é chamado em um app com `sync_status: pending`/`failed` (nunca teve chave registrada com sucesso) THEN o sistema SHALL tratar como equivalente a um retry (não há chave antiga real para revogar).
- WHEN o repo do frontend app já foi arquivado manualmente no GitHub antes da chamada de revogação/regeneração THEN a chamada ao GitHub SHALL falhar de forma esperada (repo arquivado rejeita mutações) — tratado como falha best-effort, não bloqueia a ação local.
- WHEN não há RBAC implementado (M3 pendente) THEN qualquer usuário autenticado pode revelar/regenerar a credencial de qualquer frontend app, não só a própria — mesmo gap já aceito na sub-feature 2, revisar quando RBAC existir.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| SY-01 | P1: Geração na criação | Design | Pending |
| SY-02 | P1: Geração na criação | Design | Pending |
| SY-03 | P1: Geração na criação | Design | Pending |
| SY-10 | P1: Reveal + prompt | Design | Pending |
| SY-11 | P1: Reveal + prompt | Design | Pending |
| SY-12 | P1: Reveal + prompt | Design | Pending |
| SY-13 | P1: Reveal + prompt | Design | Pending |
| SY-20 | P1: Retry | Design | Pending |
| SY-21 | P1: Retry | Design | Pending |
| SY-22 | P1: Retry | Design | Pending |
| SY-30 | P1: Regenerate | Design | Pending |
| SY-31 | P1: Regenerate | Design | Pending |
| SY-32 | P1: Regenerate | Design | Pending |
| SY-40 | P1: Delete revoga | Design | Pending |
| SY-41 | P1: Delete revoga | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 15 total, 0 mapped to tasks, 15 unmapped ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Todo frontend app criado com sucesso já sai com deploy key gerada e registrada (ou `sync_status: pending` se falhar, nunca bloqueando a criação do app)
- [ ] Usuário revela a chave privada e copia o prompt pro agente sem nunca precisar acessar o GitHub diretamente
- [ ] Retry e regenerate funcionam sem exigir recriação do frontend app
- [ ] Regenerate segue best-effort na revogação da chave antiga — nunca bloqueia a emissão da nova
- [ ] Delete do frontend app tenta revogar a credencial de sync, best-effort, sem bloquear a remoção local
