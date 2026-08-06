# Observability Integrations — Brief pra Design de Telas

Contexto: zeep-orbit é um backend-as-a-service self-hosted (dashboard React já existente). Cada app hospedado pode configurar exportação de request logs pra ferramentas de observabilidade externas: **OpenTelemetry** (core, sem licença), **Datadog** e **New Relic** (enterprise, gated pela licença descrita em `.specs/features/enterprise-licensing/`). Ref técnica completa: `.specs/features/observability-integrations/{spec,design}.md`.

Design system existente do dashboard deve ser seguido (não inventar estilo novo). Reusa também os componentes já brifados em `.specs/features/enterprise-licensing/ui-design-brief.md` — em especial `EnterpriseBadge` e o modal/tooltip de upgrade, que essa tela também usa.

---

## Tela a desenhar

### Página "Observability" (nova, dentro do dashboard, por app)

**Propósito**: configurar quais providers de observabilidade estão ativos para o app atual, ver status de cada um.

**Estrutura**: 3 cards, um por provider, sempre visíveis (nenhum é escondido):

| Card | Badge | Campos de config |
|---|---|---|
| **OpenTelemetry** | nenhum (sempre disponível, core) | `endpoint` (URL do collector, obrigatório), header de auth opcional |
| **Datadog** | "Enterprise" se licença não cobre (reusa `EnterpriseBadge`) | `api_key`, `site` (dropdown: `datadoghq.com` / `datadoghq.eu` / outros datacenters, default US) |
| **New Relic** | "Enterprise" se licença não cobre | `api_key`, `account_id` |

**Cada card tem 3 estados independentes**:

1. **Não configurado**: campos vazios, botão "Salvar e ativar" (ou equivalente), sem indicação de erro.
2. **Configurado e ativo**: toggle "Habilitado" ligado, indicação visual de status:
   - "Exportando" (verde) — provider core, ou provider enterprise com licença válida
   - "Pausado — requer licença Enterprise" (amarelo/neutro, **não vermelho** — não é erro do usuário, é estado esperado sem licença) — só acontece em Datadog/New Relic sem licença cobrindo
3. **Configurado mas desabilitado** (toggle desligado pelo próprio usuário): visualmente distinto do estado 2b, deixa claro que foi o usuário quem pausou, não a licença.

**Nunca exibir a `api_key` em claro depois de salva** — só um indicador tipo "•••• configurada" com opção de substituir.

**Dados** (vêm de `GET /dashboard/api/apps/{app}/observability/configs`): array de `{provider, enabled, endpoint, extra, has_key: bool}` por app. Estado de licença (pra saber se mostra badge/pausa) vem do mesmo `useFeature`/config público já usado na feature de licenciamento.

**Quem vê**: admin do app (mesma checagem de permissão de outras páginas de config administrativa).

---

## Interações específicas

- **Datadog/New Relic sem licença**: usuário ainda consegue preencher e salvar a config (permite pré-configurar antes de comprar a licença) — o toggle "Habilitado" fica funcional, mas o card mostra claramente que o envio real está pausado até ter licença. Clicar no badge "Enterprise" abre o mesmo modal de upgrade já brifado em `enterprise-licensing`.
- **Erro ao salvar** (validação de campo, API key inválida rejeitada pelo provider, etc.): toast (`sonner`), padrão do produto — nunca erro silencioso.
- **OpenTelemetry sem `endpoint` preenchido**: card não pode ser habilitado, mensagem inline explicando que o endpoint é obrigatório (não é erro de licença, é campo faltando).

---

## Coisas que o design PRECISA respeitar

- **i18n obrigatório**: toda string por `react-i18next`, en+pt-BR desde o dia 1 — texto do design deve vir com string-key sugerida, não hardcoded
- **Erro de mutação sempre visível**: toast, nunca estado silencioso
- **Tom neutro no estado "pausado por licença"**: nunca tratar como erro/falha do usuário — é uma feature comercial esperando compra, não um bug
- **Os 3 providers sempre aparecem**, mesmo os gated — gerar conversão, não esconder
- **Nunca expor `api_key` crua** depois de salva
- **Reusar `EnterpriseBadge` e modal de upgrade** já desenhados em `enterprise-licensing/ui-design-brief.md`, não recriar do zero

---

## Fora de escopo deste brief

- Visualização dos dados exportados (dashboards de métricas/logs) — isso vive no Datadog/New Relic/backend OTel do próprio cliente, não no zeep-orbit
- Tela de observabilidade da própria plataforma zeep-orbit (feature distinta, fora deste spec)
- Fluxo de compra da licença Enterprise em si — já coberto no brief de `enterprise-licensing`
