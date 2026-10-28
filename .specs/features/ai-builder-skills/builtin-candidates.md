# Candidatos a skills built-in (S12, SKL-07)

Status: **proposta para aprovação do mantenedor** (gate de S13). Pesquisa somente leitura, em 2026-10-28. Nada foi instalado, copiado ou vendorizado.

Legenda: **[Certo]** verificado numa fonte consultada nesta pesquisa; **[Provável]** inferência razoável; **[Incerto]** não verificado.

## 1. Método e limites

- Busca de candidatos: `npx -y skills find "<query>"` (apenas busca; nenhum `add`/`install` foi executado). Consultas: frontend design, accessibility, wcag accessibility, react best practices, react patterns hooks, responsive layout css, responsive mobile-first, forms validation ux, crud admin ui, loading empty error states. [Certo]
- Licenças: lidas do repositório upstream via API do GitHub (`gh api repos/<repo>/license`, que devolve o SPDX detectado, e o arquivo `LICENSE` bruto do repo). Usei `gh api`, e não WebFetch, por ser leitura direta da fonte upstream e devolver o texto bruto. O SPDX detectado vale para o repositório; o arquivo de cada skill foi lido quando citado. [Certo]
- Contagens de instalação do skills.sh **não** são usadas como critério de decisão e não são reproduzidas aqui.
- Skills locais revisadas: `.agents/skills/` do repo e `~/.agents/skills/` (`frontend-design`, `fixing-accessibility`, symlinks em `~/.claude/skills`). `skills-lock.json` do repo registra as fontes. [Certo]
- Conteúdo de candidatos **não foi lido por inteiro**; a avaliação de "o que muda" vem de frontmatter, cabeçalhos e buscas por padrões (`npx`, `curl`, `bash`, `http`, `allowed-tools`, Next.js, Tailwind). Revisão linha a linha é parte de S13.

Regra de decisão aplicada: **adapt with attribution** só com licença permissiva verificada (MIT/Apache-2.0/BSD/CC0) **e** bom encaixe na stack (React + Vite + TS + CSS puro + `HashRouter`, tools `read_file`/`write_file`/`edit_file`/`run_build`/`orbit_*`/`propose_backend_change`/`load_skill`). Caso contrário, **write from scratch**.

Flag **[SUPPLY-CHAIN]**: conteúdo que executa comandos, baixa conteúdo remoto ou pré-aprova ferramentas. Nunca entra, mesmo como trecho.

## 2. Candidatos por área

### 2.1 orbit-frontend-design

| Candidato | Fonte / URL | Licença | O que muda para nossa stack | Decisão |
| --- | --- | --- | --- | --- |
| `frontend-design` | anthropics/skills, `skills/frontend-design` · https://github.com/anthropics/skills/tree/main/skills/frontend-design | **Apache-2.0** [Certo]: `LICENSE.txt` da própria skill é o texto Apache 2.0; o README do repo diz que muitas skills são Apache 2.0 e que docx/pdf/pptx/xlsx são source-available (não é o caso desta). O repo não tem `LICENSE` raiz (a API de licença devolve 404), então vale o arquivo da skill. | ~67 linhas, sem `allowed-tools`, sem comandos. Cópia local em `~/.agents/skills/frontend-design` tem frontmatter idêntico ao upstream. Falta: tokens em variáveis CSS, regra "ceder ao design system da empresa", sem fontes externas/CDN (lint proíbe `http(s)://`), sem menção a screenshots (não temos tool). | **adapt with attribution** (Apache-2.0 exige manter o aviso de licença e indicar alterações; registrar em `NOTICE.md`) |
| `impeccable` (+ `frontend-design`) | pbakaus/impeccable · https://github.com/pbakaus/impeccable | **Apache-2.0** [Certo] (`LICENSE`; README: "Apache 2.0"). `NOTICE.md` cita material de terceiros (ehmo/platform-design-skills, MIT) em arquivos de referência iOS/Android. | **[SUPPLY-CHAIN]** frontmatter com `allowed-tools: Bash(npx impeccable *)` e scripts do skill (`scripts/impeccable`, modo "live" em navegador). Parte do conteúdo é para app nativo. | **write from scratch** (usar só como leitura de inspiração; não copiar) |
| `design-taste-frontend` e variantes | leonxlnx/taste-skill · https://github.com/leonxlnx/taste-skill | **MIT** [Certo, repo] | Não inspecionado [Incerto]; foco estético/opinativo, risco de impor estilo contra design system do cliente. | **write from scratch** (não avaliado a fundo) |
| `ui-ux-pro-max` (vendorizado) | nextlevelbuilder/ui-ux-pro-max-skill · https://github.com/nextlevelbuilder/ui-ux-pro-max-skill | **MIT** [Certo, repo] | **[SUPPLY-CHAIN]** executa scripts Python (`scripts/search.py`) e depende de CSVs locais (estilos, paletas, fontes); stacks fora da nossa (22 stacks). Não cabe em ≤24 KiB sem shell. | **write from scratch** |
| `frontend-ui-engineering` | addyosmani/agent-skills · https://github.com/addyosmani/agent-skills | **MIT** [Certo, repo; Copyright (c) 2025 Addy Osmani] | 340 linhas; mistura design, estado e a11y; exemplos Tailwind; menciona axe-core/DevTools. Seria dividido entre 3 built-ins. | referência; **write from scratch** |

### 2.2 orbit-accessibility

| Candidato | Fonte / URL | Licença | O que muda para nossa stack | Decisão |
| --- | --- | --- | --- | --- |
| `fixing-accessibility` (global em `~/.agents/skills`) | ibelick/ui-skills, `skills/fixing-accessibility` · https://github.com/ibelick/ui-skills | **MIT** [Certo, repo; Copyright (c) 2026 Julien Thibeaut]. O `SKILL.md` não traz campo `license` próprio. | ~135 linhas, sem comandos, sem `allowed-tools`. Remover o bloco "how to use" com slash commands (`/fixing-accessibility`) e a lógica de "report violations"; reescrever como regras para quem gera código. Sem URLs. | **adapt with attribution** (base principal) |
| `accessibility` | addyosmani/web-quality-skills · https://github.com/addyosmani/web-quality-skills | **MIT** [Certo]; `license: MIT` no frontmatter da skill; Copyright (c) 2026 Addy Osmani | 464 linhas (WCAG 2.2). **[SUPPLY-CHAIN]** passo de auditoria pede Lighthouse/`lighthouse_audit` e bloco `bash` com CLI. Exige tools que não temos. | apenas ideias de checklist; **write from scratch** para o texto |
| Regras "Accessibility/Focus/Forms/Touch" do web-design-guidelines (vendorizado) | vercel-labs/web-interface-guidelines · https://github.com/vercel-labs/web-interface-guidelines | **MIT** [Certo] (`LICENSE` do repo) | Original **[SUPPLY-CHAIN]**: o `SKILL.md` upstream manda baixar `command.md` de `main` a cada uso (já remendado localmente, com commit fixo). Regras incluem Hydration/Next e Safe Areas, parte fora do escopo. | fonte secundária de regras; **adapt with attribution** só se S13 quiser trechos |
| `better-accessibility` | jakubkrehel/skills · https://github.com/jakubkrehel/skills | **MIT** [Certo, repo] | Não inspecionado [Incerto]. | não usar |

### 2.3 orbit-react-patterns

| Candidato | Fonte / URL | Licença | O que muda para nossa stack | Decisão |
| --- | --- | --- | --- | --- |
| `vercel-react-best-practices` (vendorizado) | vercel-labs/agent-skills · https://github.com/vercel-labs/agent-skills | **UNVERIFIED** como arquivo: frontmatter diz `license: MIT` e o README termina com "## License / MIT", mas o repo **não tem `LICENSE`** (API devolve 404) e portanto não há titular de copyright. | 70 regras em 8 categorias; foco Next.js/servidor (`server-*`, `next/dynamic`, `bundle-*`, `rendering-*`); nada sobre `getOrbit()`. Só parte de `rerender-*`/`async-*` serve. | **write from scratch** |
| `react-patterns` / `frontend-patterns` | affaan-m/ecc · https://github.com/affaan-m/ecc | **MIT** [Certo, repo; Copyright (c) 2026 Affaan Mustafa]; `frontend-patterns` traz `license: MIT` | `frontend-patterns` com 662 linhas, centrado em Next.js; `react-patterns` cobre React 18/19 com server components e form actions (fora da nossa stack). | **write from scratch** |
| `react-dev` | softaworks/agent-toolkit | **MIT** [Certo, repo] | Não inspecionado [Incerto]. | não usar |

Fundamento Orbit-específico (sem equivalente externo): `getOrbit()`, tratamento de erro da API, sem segredos no cliente. Base interna: `.agents/skills/orbit-usage/references/frontend-integration.md` e `gotchas.md` (conteúdo ZeepLabs, sem questão de licença). **[Provável]** reaproveitável como ponto de partida, a validar com o mantenedor.

### 2.4 orbit-responsive-layout

| Candidato | Fonte / URL | Licença | O que muda para nossa stack | Decisão |
| --- | --- | --- | --- | --- |
| `responsive-design` | wshobson/agents, `plugins/ui-design/skills/responsive-design` · https://github.com/wshobson/agents | **MIT** [Certo, repo; Copyright (c) 2024 Seth Hobson] | Só 44 linhas, genérico (container queries, fluid type, Grid, mobile-first). Sem comandos. Pouco a adaptar; falta HashRouter/shell de app, tabelas largas, touch targets. | **write from scratch** (pode citar a ideia, sem copiar) |
| `tailwind-responsive-ui`, `tailwind-css-patterns` | pproenca/dot-skills; giuseppe-trisciuoglio/developer-kit | Licença não consultada: **UNVERIFIED** | Assumem Tailwind; nosso builder usa CSS puro. | **write from scratch** |

### 2.5 orbit-forms-and-states

| Candidato | Fonte / URL | Licença | O que muda para nossa stack | Decisão |
| --- | --- | --- | --- | --- |
| `building-forms` | ancoleman/ai-design-components · https://github.com/ancoleman/ai-design-components | **MIT** [Certo, repo; Copyright (c) 2025 Anton Coleman] | 429 linhas, "50+ tipos de input", wizards, exemplos Python/FastAPI. Escopo largo demais; o essencial (validação, erros, estados) precisa de reescrita para controlled inputs em React/TS. | **write from scratch** |
| `forms`, `better-react-web-ui` | aladicf/better-web-ui; aladicf/better-react-web-ui | **Licença própria, não OSI/MIT padrão** [Certo]: texto "based on MIT" mas com exigência de publicar o código-fonte de versões modificadas sob a mesma licença e proibição de uso proprietário/pago. API do GitHub: `NOASSERTION`. | Incompatível com redistribuir como parte do produto. | **write from scratch — não usar nem como cópia** |
| `harden`, `unhappy` (estados de erro/vazio) | educlopez/ui-craft · https://github.com/educlopez/ui-craft | **MIT** [Certo, repo] | Conteúdo não lido [Incerto]. | só leitura de inspiração; **write from scratch** |
| `ux-inputs-and-forms` | uxcel-lab/product-skills | **MIT** [Certo, repo] | Conteúdo/caminho não confirmado [Incerto]. | não usar |

### 2.6 orbit-crud-screens

Buscas "crud admin ui" e "loading empty error states" só devolveram skills de baixa adoção e atreladas a outros produtos (Wix, Databricks, Avue, Sylius etc.). **[Certo]** não há candidato adequado. Conteúdo é específico de Orbit (tabelas, RLS, "lista vazia pode ser a policy escondendo linhas", paginação). **write from scratch** (já previsto em design §7). Entradas internas: `orbit-usage/references/rest-api.md` e `gotchas.md`.

## 3. Outras skills vendorizadas no repo, revisadas

| Skill | Fonte | Licença | Aproveitável? |
| --- | --- | --- | --- |
| `shadcn` | shadcn-ui/ui | **MIT** [Certo] | Não: depende de `npx shadcn@latest`, Tailwind e `components.json`. |
| `playwright-best-practices`, `golang-*` | currents-dev; samber/cc-skills-golang | `license: MIT` no frontmatter [Certo, declarado] | Não: fora do escopo do builder. |
| `orbit-usage` | zeeplabs/zeep-orbit-agent-skills | interna | Sim, como base de `orbit-react-patterns`/`orbit-crud-screens` (sem copiar trechos que usem shell/`npx`). |
| `web-design-guidelines`, `vercel-react-best-practices`, `ui-ux-pro-max` | ver 2.1 a 2.3 | ver acima | Ver acima. |

## 4. Observações para S13

- **Apache-2.0 (frontend-design):** manter o texto da licença, indicar que o arquivo foi modificado e registrar em `builtin/NOTICE.md`. A pasta da skill upstream contém só `LICENSE.txt` e `SKILL.md` (sem arquivo `NOTICE`). [Certo]
- **MIT (ibelick):** manter o aviso de copyright e a permissão no `NOTICE.md`. O `SKILL.md` de origem não tem copyright próprio; vale o `LICENSE` do repo. [Certo]
- O lint de design §7 proíbe `http(s)://` fora de `source`, `npx`, `bash`, nomes de tools estrangeiras: nenhum trecho dos candidatos deve entrar sem passar por esse filtro.
- Um repo pode ter skills com licenças diferentes (caso anthropics/skills). Reconfirmar o arquivo da skill no momento da adaptação e anotar o commit no `source`.
- Re-verificar licença antes de S13: dados desta pesquisa são de 2026-10-28.

## 5. Lista final proposta (6 built-ins)

Descrições em inglês (idioma do corpo do skill; lint exige "Use when ..." e ≤ 200 caracteres).

| # | Nome | Trigger (description) | Decisão | Fonte de inspiração |
| --- | --- | --- | --- | --- |
| 1 | `orbit-frontend-design` | Use when designing or restyling screens: visual hierarchy, spacing and type scale, CSS-variable tokens; yield to the company design system when one is provided. | **adapt with attribution** (Apache-2.0) | anthropics/skills `frontend-design` |
| 2 | `orbit-accessibility` | Use when building interactive UI, forms, dialogs or navigation: semantic HTML, labels, focus, keyboard support, contrast and error announcements. | **adapt with attribution** (MIT) | ibelick/ui-skills `fixing-accessibility` (+ ideias de addyosmani/web-quality-skills) |
| 3 | `orbit-react-patterns` | Use when writing React components, state and effects, or fetching data through getOrbit(): hook hygiene, error handling, no secrets in client code. | **write from scratch** | `orbit-usage` (interna); ecc/Vercel só como leitura |
| 4 | `orbit-responsive-layout` | Use when laying out pages for phone, tablet and desktop: mobile-first CSS, grid and flex, breakpoints, wide tables and touch targets. | **write from scratch** | wshobson/agents `responsive-design` (ideia) |
| 5 | `orbit-forms-and-states` | Use when building forms or any async view: validation, submit feedback, loading, empty and error states, and confirmation for destructive actions. | **write from scratch** | building-forms, ui-craft (leitura) |
| 6 | `orbit-crud-screens` | Use when building list, detail and edit screens over Orbit tables: pagination, filters, page-size limits, and empty lists that may be hidden by RLS policies. | **write from scratch** | `orbit-usage` (interna) |

Comprimento das descrições conferido por contagem: 133 a 160 caracteres (limite 200). Nenhuma descrição contém URL, comando ou nome de tool estrangeira.

Pendências de validação com o mantenedor:
1. Aceitar Apache-2.0 + MIT como únicos regimes de atribuição em `NOTICE.md`, ou reduzir para "tudo do zero" (6/6). Resultado de qualidade esperado é parecido; adaptar economiza texto, escrever do zero elimina obrigação de aviso.
2. Idioma do corpo dos built-ins (inglês recomendado por consistência com o lint e com as tools).
3. Se `vercel-react-best-practices` deve ser reavaliada caso a Vercel publique um `LICENSE`.
