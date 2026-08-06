# Enterprise Licensing — Brief pra Design de Telas

Contexto: zeep-orbit é um backend-as-a-service self-hosted (dashboard React já existente). Vamos introduzir licenciamento estilo GrowthBook: core continua grátis (MIT), um conjunto de features futuras fica trancado atrás de licença paga (Ed25519, vitalícia, sem limite de seats). Ref técnica completa: `.specs/features/enterprise-licensing/{spec,design}.md`.

Design system existente do dashboard deve ser seguido (não inventar estilo novo) — layout de página, tipografia, cores, componentes de formulário já estabelecidos no resto do produto.

---

## Telas/componentes a desenhar

### 1. Página "Licença" (nova, dentro do dashboard, área de configurações/admin)

**Propósito**: ver status da licença atual, trocar a key.

**Estados possíveis** (cada um precisa de layout próprio, não é só texto mudando):

| Estado | O que mostra |
|---|---|
| **Sem licença (free/core)** | "Plano atual: Free/Core", lista do que é grátis, CTA "Adicionar licença" (abre campo de colar key ou leva a link de compra externo) |
| **Licença válida ativa** | "Plano atual: Enterprise", nome da organização licenciada, badge de status verde "Válida", lista de features desbloqueadas, sem data de expiração visível (é vitalícia — não mostrar "expira em" quando não há exp) |
| **Licença trial ativa** | Igual acima, mas com badge amarelo "Trial" + contagem regressiva de dias restantes ("Expira em 9 dias") |
| **Licença expirada/revogada** | Badge vermelho "Inválida"/"Revogada", mensagem explicando que features enterprise foram desativadas, CTA pra renovar/contatar suporte — sem alarmismo (o resto do produto continua funcionando normal) |
| **Campo de colar key** | Textarea/input pra colar a license key, botão "Salvar", validação inline (sucesso/erro), sem exibir a key depois de salva (só o resumo resolvido: org/plano/status) |

**Dados exibidos** (vêm de `GET /dashboard/api/license/status`): `plan` (`oss`/`enterprise`), `features: string[]`, `org`, `trial: bool`, `expires_at: string | null`.

**Quem vê**: qualquer admin (mesma checagem de permissão já usada em outras páginas de config administrativa do dashboard).

---

### 2. Badge/Selo "Enterprise" (componente reutilizável, aparece em várias telas futuras)

**Propósito**: marcar qualquer feature/botão/seção que existe na UI mas está bloqueada por licença — a feature **aparece**, não some, pra gerar conversão.

**Precisa de 2 variações visuais**:
- Badge pequeno inline (ex: ao lado de um item de menu ou título de seção) — "Enterprise" com ícone de cadeado ou coroa
- Estado de bloqueio de uma ação (ex: botão desabilitado ou com overlay) quando o usuário tenta clicar numa feature trancada — idealmente abre um tooltip/modal leve: "Esta funcionalidade requer licença Enterprise" + link "Saiba mais"

**Onde vai aparecer** (nenhuma feature concreta existe ainda, mas o componente precisa ser genérico o suficiente pra qualquer contexto futuro): item de menu lateral, seção dentro de uma página existente, botão de ação específico.

---

### 3. Modal/tooltip de upgrade

**Propósito**: quando o usuário esbarra numa feature bloqueada, explicar o que ela faz e como conseguir a licença.

**Conteúdo mínimo**: nome da feature, 1-2 linhas do que ela resolve, CTA (link externo pra página de vendas/portal — URL ainda não existe, usar placeholder), botão "Fechar".

---

## Coisas que o design PRECISA respeitar (não são só sugestão)

- **i18n obrigatório**: toda string vai por `react-i18next`, existe em en+pt-BR desde o dia 1 — texto do design deve vir junto com string-key sugerida (não hardcoded), design não pode assumir só português
- **Erro de mutação sempre visível**: qualquer ação que falhar (ex: colar key inválida) precisa de um estado de erro visível — o padrão do produto é toast (`sonner`), não silencioso
- **Sem alarmismo em licença expirada**: o app nunca "quebra" — o tom da tela de licença expirada deve deixar claro que só as features extras saíram do ar, não o produto inteiro
- **Nenhuma feature concreta pra desenhar ainda**: este brief é só a infraestrutura de licenciamento (página de status + badge genérico + modal). A primeira feature real gated (SSO, RBAC avançado etc.) é decisão de produto separada, ainda não especificada — não inventar essas telas agora
- **Não expor a license key crua** em nenhuma tela depois de salva (só o resultado resolvido: org/plano/status)

---

## Fora de escopo deste brief

- Portal externo de compra/checkout (Stripe) — isso vive num serviço/site separado da Zeep Labs, fora do dashboard do zeep-orbit
- Qualquer tela de gestão de seats/usuários por licença — não existe esse conceito neste modelo (licença é vitalícia, sem cap de seats)
