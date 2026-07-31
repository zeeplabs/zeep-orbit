# Enterprise Licensing Specification

## Problem Statement

zeep-orbit hoje é 100% MIT, sem nenhum mecanismo de monetização embutido no código. A Zeep Labs quer viabilizar um modelo comercial no estilo GrowthBook/Sentry: núcleo do produto continua open-source (MIT), mas um subconjunto de features fica disponível apenas mediante compra de licença enterprise — código ainda no mesmo repositório, sob licença diferente do restante.

Esta feature cobre dois componentes:

1. **Mecanismo de licenciamento no zeep-orbit** — verificação de license key, gating de features enterprise, UX de upgrade.
2. **`license-server`** — serviço separado (repo próprio da Zeep Labs, fora do zeep-orbit) responsável por emitir, vender (Stripe) e revogar licenças. Descrito aqui em nível de design/contrato porque o zeep-orbit depende do formato de dados e do protocolo de revogação que ele expõe, mas sua implementação, deploy e testes pertencem a um spec próprio nesse outro repositório.

Modelo de negócio: licença **vitalícia**, pagamento único via Stripe (não assinatura recorrente). Sem cap numérico de seats/usuários/apps — o único eixo de restrição é *quais features* estão disponíveis por plano.

## Goals

- [ ] Núcleo do zeep-orbit permanece MIT, sem nenhuma feature existente hoje sendo removida ou travada retroativamente
- [ ] Código de features enterprise vive isolado em pastas próprias (`internal/enterprise/`, `internal/dashboard/ui/src/enterprise/`), sob licença própria, seguindo o padrão GrowthBook (pasta com `LICENSE` própria + `LICENSE` raiz vira disclosure multi-licença)
- [ ] License key é um artefato assinado (Ed25519), verificável 100% offline — cliente air-gapped continua funcionando com a licença que já tem, mesmo sem rede
- [ ] Mecanismo de gating (`HasFeature`) é extensível: adicionar uma feature enterprise nova não exige tocar em nenhum ponto de enforcement existente
- [ ] Revogação de licença (fraude, reembolso, chargeback) é possível via consulta periódica opcional ao `license-server`, com fallback seguro caso o servidor esteja inacessível
- [ ] Licença inválida/expirada/revogada nunca derruba o processo nem bloqueia acesso às features core (MIT) — só remove acesso às features enterprise
- [ ] `license-server` emite licença automaticamente após pagamento confirmado via Stripe Checkout (modo pagamento único, não subscription)

## Out of Scope

| Item | Motivo |
|---|---|
| Cap numérico de seats/usuários/apps no modo free | Decidido explicitamente no brainstorm: único eixo de restrição é feature, não volume de uso |
| Licença por assinatura recorrente | Modelo é vitalícia, pagamento único — sem cobrança periódica, sem "renovação" |
| Implementação/deploy/testes do `license-server` em si | Vive em repositório separado da Zeep Labs, terá spec próprio nesse repo; aqui só o contrato de dados/API que o zeep-orbit consome é especificado |
| Portal de auto-serviço completo (gestão de conta, faturas, histórico) | Cobre só o essencial (ver licença ativa, colar nova key, pedir trial) — funcionalidades de conta mais ricas ficam para iteração futura do `license-server` |
| Lista fixa de quais features específicas são enterprise (SSO/SAML, RBAC avançado, audit export, etc.) | Mecanismo é genérico e extensível por design; a classificação de cada feature futura como core ou enterprise é decisão de produto feita feature a feature, não travada neste spec |
| Rotação automática de chave pública/privada | Path de rotação é manual (nova chave pública embutida em release, chaves antigas continuam válidas até deprecação manual documentada) |

---

## User Stories

### P1: Verificação offline de license key ⭐ MVP

**User Story**: Como operador self-hosted do zeep-orbit, quero colar uma license key e ter as features enterprise contratadas liberadas, mesmo rodando num ambiente sem acesso à internet, para que a licença funcione independente de conectividade com a Zeep Labs.

**Why P1**: É a base de tudo — sem verificação confiável, não há gating nem modelo comercial.

**Acceptance Criteria**:

1. WHEN uma license key válida (assinatura Ed25519 correta, não expirada) é configurada via `LICENSE_KEY` THEN o sistema SHALL resolver o plano correspondente (`oss` ou `enterprise`) no boot do processo.
2. WHEN a assinatura da key não bate com a chave pública embutida no binário THEN o sistema SHALL tratar como sem licença (plano `oss`), logar warning server-side, e SHALL NOT falhar o boot do processo.
3. WHEN o payload da key está corrompido/não é JSON válido após decode THEN o sistema SHALL aplicar o mesmo tratamento do item 2.
4. WHEN a data `exp` da key já passou THEN o sistema SHALL tratar como sem licença (plano `oss`), sem falhar o boot.
5. WHEN nenhuma `LICENSE_KEY` é configurada THEN o sistema SHALL operar normalmente no plano `oss`, sem nenhum log de erro (ausência de licença não é uma condição de erro).
6. A verificação de assinatura SHALL ocorrer inteiramente offline, sem nenhuma chamada de rede — a chave pública Ed25519 SHALL estar embutida no binário compilado.

**Independent Test**: Gerar par de chaves de teste; assinar payloads válido/expirado/adulterado com a chave privada de teste; verificar que cada caso resolve para o plano esperado sem crash, com o binário rodando sem `LICENSE_SERVER_URL` configurado (simulando air-gapped).

---

### P1: Gating de features por plano ⭐ MVP

**User Story**: Como plataforma, quero que cada feature classificada como enterprise seja bloqueada para usuários sem licença correspondente, tanto no backend quanto na UI, para que o modelo de licenciamento tenha efeito real.

**Why P1**: Sem enforcement, o license-check existe mas não protege nada.

**Acceptance Criteria**:

1. WHEN um endpoint gated por uma `Feature` é chamado e o plano resolvido da licença ativa não inclui essa feature THEN o sistema SHALL retornar 403 com mensagem em inglês (ex: `"this feature requires an enterprise license"`).
2. WHEN um endpoint gated é chamado e o plano resolvido inclui a feature THEN o sistema SHALL permitir a requisição normalmente.
3. WHEN uma feature enterprise nova é adicionada ao código THEN o sistema SHALL permitir declará-la (uma constante `Feature` + uma entrada no mapa `plan → []Feature`) sem exigir alteração em nenhum enforcement point pré-existente.
4. WHEN o frontend consulta `GET /dashboard/api/license/status` THEN o sistema SHALL retornar o plano ativo e a lista de features habilitadas, para popular o hook `useFeature(name)`.
5. WHEN uma feature está indisponível no plano atual THEN a UI SHALL exibir a feature com indicação visual "Enterprise" e link de upgrade, em vez de escondê-la silenciosamente.
6. Toda string nova de UI relacionada a licenciamento SHALL passar por `react-i18next`, adicionada em `en.json` e `pt-BR.json` na mesma mudança.

**Independent Test**: Criar 2 endpoints de teste gated por features diferentes; bater com licença `oss`, licença `enterprise` válida, e sem licença nenhuma; confirmar 200/403 conforme a matriz plano×feature.

---

### P2: Revogação via consulta periódica ao license-server

**User Story**: Como Zeep Labs, quero poder revogar uma licença vendida (fraude, reembolso, chargeback) e ter essa revogação propagada para instâncias com acesso à internet, sem depender de uma nova versão do zeep-orbit.

**Why P2**: Importante para o modelo comercial ser sustentável, mas não bloqueia o MVP de licenciamento em si — licenças recém-emitidas funcionam corretamente mesmo sem essa camada.

**Acceptance Criteria**:

1. WHEN `LICENSE_SERVER_URL` está configurado THEN o sistema SHALL consultar periodicamente (intervalo configurável, default diário) se o `ref` da licença ativa consta como revogado.
2. WHEN a consulta confirma que a licença foi revogada THEN o sistema SHALL transicionar o plano ativo para `oss` a partir do próximo ciclo, sem reiniciar o processo e sem perda de dados do cliente.
3. WHEN o `license-server` está inacessível durante a consulta (timeout, erro de rede, 5xx) THEN o sistema SHALL manter o último estado de licença válido conhecido, sem penalizar o cliente pela indisponibilidade do servidor da Zeep Labs.
4. WHEN `LICENSE_SERVER_URL` não está configurado (vazio) THEN o sistema SHALL operar apenas com verificação offline (User Story 1), sem nenhuma tentativa de rede.
5. A consulta de revogação SHALL rodar em goroutine própria, sem bloquear requisições HTTP em andamento.

**Independent Test**: Simular `license-server` de teste que responde "revogado" para um `ref` específico; confirmar que a instância transiciona para `oss` no ciclo seguinte; simular servidor fora do ar e confirmar que o plano anterior é mantido.

---

### P2: Emissão automática pós-pagamento (contrato com license-server)

**User Story**: Como comprador, quero receber minha license key automaticamente assim que o pagamento é confirmado, sem intervenção manual da Zeep Labs.

**Why P2**: Fecha o ciclo comercial, mas a implementação real vive no `license-server` — aqui apenas o contrato de dados é fixado, para que o zeep-orbit saiba o formato exato que vai consumir.

**Acceptance Criteria**:

1. O payload da license key SHALL conter, no mínimo: `org` (identificador do comprador), `plan`, `iat` (data de emissão), `exp` (data de expiração — ausente ou muito distante para licença vitalícia), `trial` (bool), `ref` (identificador único da licença, usado para revogação).
2. WHEN o `license-server` confirma um pagamento via webhook do Stripe (`checkout.session.completed`, modo `payment`, não `subscription`) THEN ele SHALL gerar e assinar a license key correspondente e disponibilizá-la ao comprador (e-mail e/ou portal).
3. WHEN um usuário solicita trial pelo dashboard do zeep-orbit THEN o `license-server` SHALL poder emitir uma key com `trial: true` e `exp` de curto prazo, sem exigir checkout.
4. A chave privada usada para assinar licenças SHALL existir apenas no `license-server`, nunca em nenhum artefato do zeep-orbit.

**Independent Test**: Este AC é verificado no spec do `license-server`, não neste repositório — aqui serve como contrato de interface (formato do payload) que os testes das User Stories 1 e 2 já exercitam do lado de consumo.

---

## Edge Cases

- WHEN uma license key assinada com uma chave pública já deprecada (rotação manual) é usada THEN o sistema SHALL reportar erro específico ("chave pública desconhecida"), distinto do erro genérico de assinatura inválida — facilita suporte a identificar o caso.
- WHEN duas license keys diferentes são coladas em sequência THEN a mais recente SHALL substituir a anterior integralmente, sem estado de "licenças múltiplas ativas".
- WHEN uma instância roda permanentemente air-gapped (sem `LICENSE_SERVER_URL`) THEN ela SHALL continuar operando normalmente com a última licença configurada, aceitando explicitamente que nunca receberá revogação — comportamento documentado, não um bug.
- WHEN o processo reinicia THEN a licença SHALL ser reavaliada do zero a partir da `LICENSE_KEY` configurada (sem cache de plano entre reinícios que sobreviva a uma revogação já conhecida antes do restart).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| LIC-01 | P1: Verificação offline | Design | Pending |
| LIC-02 | P1: Verificação offline | Design | Pending |
| LIC-03 | P1: Verificação offline | Design | Pending |
| LIC-04 | P1: Verificação offline | Design | Pending |
| LIC-05 | P1: Verificação offline | Design | Pending |
| LIC-06 | P1: Verificação offline | Design | Pending |
| LIC-10 | P1: Gating de features | Design | Pending |
| LIC-11 | P1: Gating de features | Design | Pending |
| LIC-12 | P1: Gating de features | Design | Pending |
| LIC-13 | P1: Gating de features | Design | Pending |
| LIC-14 | P1: Gating de features | Design | Pending |
| LIC-15 | P1: Gating de features | Design | Pending |
| LIC-20 | P2: Revogação | Design | Pending |
| LIC-21 | P2: Revogação | Design | Pending |
| LIC-22 | P2: Revogação | Design | Pending |
| LIC-23 | P2: Revogação | Design | Pending |
| LIC-24 | P2: Revogação | Design | Pending |
| LIC-30 | P2: Emissão pós-pagamento (contrato) | Design | Pending |
| LIC-31 | P2: Emissão pós-pagamento (contrato) | Design | Pending |
| LIC-32 | P2: Emissão pós-pagamento (contrato) | Design | Pending |
| LIC-33 | P2: Emissão pós-pagamento (contrato) | Design | Pending |

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 21 total, 0 mapeados a tasks, 21 não mapeados ⚠️ (mapeamento acontece na fase Tasks)

---

## Success Criteria

- [ ] Nenhuma feature MIT existente é removida ou travada retroativamente pela introdução do licenciamento
- [ ] Licença válida configurada offline (sem `LICENSE_SERVER_URL`) libera as features corretas sem nenhuma chamada de rede
- [ ] Licença inválida, expirada, corrompida ou revogada nunca derruba o processo — sempre degrada para `oss` de forma limpa
- [ ] Adicionar uma feature enterprise nova é uma mudança de poucas linhas (const + entrada de mapa), sem tocar em enforcement points existentes
- [ ] `LICENSE` raiz do repositório reflete corretamente o modelo multi-licença (MIT + Zeep Labs Enterprise License na pasta `internal/enterprise/`), texto final validado juridicamente antes da publicação
- [ ] Contrato de dados entre zeep-orbit e `license-server` (formato do payload, protocolo de revogação) está claro o suficiente para o spec do `license-server` ser escrito de forma independente
