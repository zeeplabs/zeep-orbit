# Observability Integrations Specification

## Problem Statement

Hoje o zeep-orbit já captura, por request, um `dashboard.LogEntry` (método, path, status, latência, app, corpo) num `RingBuffer` em memória (`internal/server/server.go`, `logMiddleware`), visível apenas dentro do próprio dashboard (`AuditLogPage`/logs). Não existe nenhum jeito de levar esse dado pra ferramentas de observabilidade externas que os clientes já operam (Datadog, New Relic) ou pra um coletor OpenTelemetry próprio.

Esta feature cobre exclusivamente a exportação, por app hospedado, do que o zeep-orbit **já gera internamente** (request logs existentes no `RingBuffer`) para providers externos configuráveis. Não introduz nenhum SDK ou contrato novo para o código do app cliente emitir eventos customizados — isso foi avaliado no brainstorm e descartado por não caber no modelo "exporta o que já existe" (ver Out of Scope).

Está relacionada, mas é distinta, de uma eventual segunda feature de observabilidade da própria plataforma zeep-orbit (logs/métricas/traces do BaaS em si, para quem opera a instância) — essa segunda feature é intencionalmente deixada fora deste spec, para ser brainstormada e especificada separadamente.

Gating: Datadog e New Relic são features **enterprise** (`.specs/features/enterprise-licensing/`) — primeira feature concreta a usar o mecanismo `enterprise.HasFeature` já desenhado nesse spec. OpenTelemetry é **core**, sem gate de licença, por ser um padrão aberto e vendor-neutral (o cliente decide o backend final apontando o próprio collector).

## Goals

- [ ] App hospedado pode configurar um ou mais providers de observabilidade (OTel, Datadog, New Relic) simultaneamente, sem conflito entre eles
- [ ] Exportação roda em lote periódico a partir do `RingBuffer` já existente, sem introduzir fila persistente nem SDK novo pro código do app cliente
- [ ] OpenTelemetry funciona sem licença enterprise; Datadog e New Relic exigem licença enterprise ativa
- [ ] Falha de rede/timeout/erro do provider externo nunca derruba o `Manager` nem perde requests em andamento (fail-open, best-effort)
- [ ] Mecanismo de registro de provider é extensível: adicionar um provider novo não exige alterar o `Manager` nem os providers já existentes
- [ ] Config de cada provider (API key, endpoint) segue o mesmo padrão de criptografia e auditoria já usado em `deploy_provider_config.go`

## Out of Scope

| Item | Motivo |
|---|---|
| SDK/endpoint para o app cliente emitir eventos customizados (ex: "signup", "purchase") | Avaliado e descartado no brainstorm: escopo desta feature é só exportar o que o zeep-orbit já gera (request logs), não introduzir um novo contrato de API para o código do app |
| Firebase Analytics (ou qualquer provider orientado a evento de produto) | Não se encaixa no modelo de dado exportado (request log de infra), que é diferente de evento de produto — ficaria incoerente sem o item anterior |
| Observabilidade do próprio zeep-orbit (plataforma/BaaS) | Feature distinta, decidida explicitamente no brainstorm para ser especificada em spec própria e separado |
| Retry com backoff / fila persistente de envio | Exportação é best-effort; entry não enviada a tempo dentro do ciclo é aceita como perdida, mesma postura do fail-open já usado em `enterprise-licensing` |
| Dashboard de visualização própria dos dados exportados | O produto delega essa responsabilidade ao provider externo (Datadog/New Relic/backend OTel escolhido pelo cliente) |
| Coordenação cross-replica do cursor de exportação | Cada pod exporta apenas o que ele mesmo processou no `RingBuffer` local; aceitável para observabilidade (não é audit trail com garantia de completude/ordem) |
| Rotação/gestão de credenciais dos providers externos | Fica a cargo do cliente gerenciar a API key gerada no Datadog/New Relic; o zeep-orbit só armazena o que for colado |

---

## User Stories

### P1: Config de provider por app ⭐ MVP

**User Story**: Como admin de um app hospedado, quero configurar um provider de observabilidade (OTel, Datadog ou New Relic) com suas credenciais/endpoint, para que os dados de request do meu app comecem a ser exportados.

**Why P1**: Sem configuração persistida, não há o que o `Manager` exportar.

**Acceptance Criteria**:

1. WHEN um admin cria uma config de provider para um app (`provider`, `enabled`, `api_key` quando aplicável, `endpoint`, campos extras por provider) THEN o sistema SHALL persistir a `api_key` criptografada (mesmo mecanismo de `crypto.Encrypt` usado em `deploy_provider_config.go`) e nunca retorná-la em claro em nenhuma resposta subsequente.
2. WHEN um app já possui uma config para um provider e uma nova config do mesmo provider é enviada THEN o sistema SHALL fazer upsert (chave única `app_name + provider`), não criar duplicata.
3. WHEN um app configura múltiplos providers diferentes (ex: OTel e Datadog) THEN o sistema SHALL manter ambas as configs ativas de forma independente.
4. WHEN uma config é criada, atualizada ou removida THEN o sistema SHALL registrar um evento de auditoria (`InsertAuditLog`), incluindo usuário, ação e provider afetado.
5. Um update parcial que precise **limpar** um campo (ex: remover `endpoint`) SHALL enviar a chave explicitamente com valor vazio — omissão é interpretada como "não alterar", nunca como limpeza (mesma regra de `mergeProviderConfig`).
6. Toda string nova de UI relacionada a esta feature SHALL passar por `react-i18next`, adicionada em `en.json` e `pt-BR.json` na mesma mudança.

**Independent Test**: Criar config OTel + Datadog para o mesmo app; confirmar que ambas persistem, `GET` nunca expõe a key em claro, e um update parcial de um campo não apaga os demais.

---

### P1: Exportação em lote a partir do RingBuffer ⭐ MVP

**User Story**: Como plataforma, quero exportar periodicamente as requests já capturadas no `RingBuffer` para os providers configurados de cada app, para que o cliente veja os dados no seu Datadog/New Relic/coletor OTel sem esperar por uma requisição síncrona extra.

**Why P1**: É o mecanismo central da feature — sem ele, a configuração da User Story 1 não tem efeito nenhum.

**Acceptance Criteria**:

1. WHEN um app tem ao menos um provider `enabled` THEN o sistema SHALL, em um ciclo periódico (intervalo configurável, goroutine própria por app com `time.Ticker`), ler as entries novas desse app no `RingBuffer` local do processo e despachar para cada provider habilitado.
2. WHEN um app não tem nenhum provider configurado ou nenhum habilitado THEN o sistema SHALL NOT rodar nenhum ciclo de exportação para ele (sem overhead).
3. WHEN não há entries novas desde o último ciclo THEN o sistema SHALL NOT fazer nenhuma chamada HTTP ao provider.
4. WHEN o envio a um provider falha (timeout, erro de rede, resposta 4xx/5xx) THEN o sistema SHALL logar o erro server-side e seguir para o próximo ciclo, sem reintentar automaticamente e sem interromper a exportação para os demais providers/apps.
5. A leitura e exportação SHALL rodar em goroutine própria, sem bloquear o processamento de requisições HTTP em andamento.
6. Cada pod/réplica SHALL exportar apenas as entries que ele mesmo processou (sem coordenação cross-replica do cursor).

**Independent Test**: Popular o `RingBuffer` de teste com entries sintéticas; rodar um ciclo do `Manager` contra um `httptest.Server` fazendo o papel do provider; confirmar payload recebido, e confirmar que um provider retornando 500 não impede o envio aos demais providers configurados no mesmo app.

---

### P1: Gate de licença por provider ⭐ MVP

**User Story**: Como Zeep Labs, quero que Datadog e New Relic só funcionem com licença enterprise ativa, enquanto OpenTelemetry funciona sempre, para monetizar as integrações com provedores comerciais mantendo o padrão aberto disponível a todos.

**Why P1**: É a decisão comercial central desta feature — sem ela, não há diferenciação core/enterprise.

**Acceptance Criteria**:

1. WHEN o provider configurado é `otel` THEN o sistema SHALL exportar independentemente do estado da licença (nenhum `enterprise.HasFeature` check).
2. WHEN o provider configurado é `datadog` ou `newrelic` E a licença ativa não inclui a feature correspondente (`FeatureObservabilityDatadog`/`FeatureObservabilityNewRelic`) THEN o sistema SHALL pular o envio para esse provider nesse ciclo, sem erro, e SHALL registrar esse fato de forma distinguível de uma falha real de rede (para a UI poder explicar a diferença).
3. WHEN uma licença enterprise anteriormente ativa expira/é revogada THEN a exportação para Datadog/New Relic SHALL pausar automaticamente a partir do próximo ciclo, sem exigir reinício do processo e sem apagar a config salva.
4. WHEN a licença volta a cobrir a feature (nova key configurada) THEN a exportação para esse provider SHALL retomar automaticamente no próximo ciclo, usando a config já salva.
5. Adicionar um provider novo classificado como enterprise no futuro SHALL exigir apenas registrar sua `Feature` no `providerRegistry`, sem alterar o `Manager`.

**Independent Test**: Configurar Datadog para um app sem licença enterprise; confirmar que o envio é pulado (não é erro) enquanto OTel no mesmo app continua exportando normalmente; simular licença enterprise válida e confirmar que Datadog passa a exportar no ciclo seguinte.

---

## Edge Cases

- WHEN um provider é desabilitado (`enabled: false`) mas a config permanece salva THEN o sistema SHALL parar de exportar para ele imediatamente no próximo ciclo, sem deletar a config.
- WHEN o mesmo app tem OTel core + Datadog/New Relic enterprise configurados e a licença cai THEN apenas OTel SHALL continuar exportando — o app nunca perde 100% da observabilidade por perder a licença.
- WHEN o `RingBuffer` já sobrescreveu (rolling buffer) entries que ainda não tinham sido exportadas por um ciclo lento/atrasado THEN essas entries SHALL ser aceitas como perdidas — comportamento documentado, coerente com a postura best-effort desta feature.
- WHEN um app é removido/deletado THEN suas configs de observabilidade SHALL ser removidas junto (sem config órfã apontando pra um app inexistente).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| OBS-01 | P1: Config de provider por app | Design | Pending |
| OBS-02 | P1: Config de provider por app | Design | Pending |
| OBS-03 | P1: Config de provider por app | Design | Pending |
| OBS-04 | P1: Config de provider por app | Design | Pending |
| OBS-05 | P1: Config de provider por app | Design | Pending |
| OBS-06 | P1: Config de provider por app | Design | Pending |
| OBS-10 | P1: Exportação em lote | Design | Pending |
| OBS-11 | P1: Exportação em lote | Design | Pending |
| OBS-12 | P1: Exportação em lote | Design | Pending |
| OBS-13 | P1: Exportação em lote | Design | Pending |
| OBS-14 | P1: Exportação em lote | Design | Pending |
| OBS-15 | P1: Exportação em lote | Design | Pending |
| OBS-20 | P1: Gate de licença por provider | Design | Pending |
| OBS-21 | P1: Gate de licença por provider | Design | Pending |
| OBS-22 | P1: Gate de licença por provider | Design | Pending |
| OBS-23 | P1: Gate de licença por provider | Design | Pending |
| OBS-24 | P1: Gate de licença por provider | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 17 total, 0 mapeados a tasks, 17 não mapeados ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Config de qualquer provider persiste com API key sempre criptografada e nunca exposta em claro após salva
- [ ] OTel exporta sem depender de licença; Datadog/New Relic exportam apenas com licença enterprise ativa cobrindo a feature correspondente
- [ ] Falha de qualquer provider externo (rede, timeout, 4xx/5xx) nunca impede exportação para os demais providers do mesmo app nem derruba o processo
- [ ] Adicionar um provider novo é uma mudança isolada no `providerRegistry`, sem tocar no `Manager` nem nos providers existentes
- [ ] Licença enterprise expirando/revogada pausa automaticamente Datadog/New Relic sem apagar config e sem afetar OTel no mesmo app
- [ ] Toda string de UI nova está em `en.json` e `pt-BR.json` desde o início
