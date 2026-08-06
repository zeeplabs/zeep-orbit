# Dashboard Global Roles — Brief pra Design de Telas

Contexto: zeep-orbit é um backend-as-a-service self-hosted (dashboard React já existente). `dashboard_users.role` passa de 2 níveis (`admin`/`superadmin`) para 4: `superadmin` (irrestrito), `admin` (gestão de plataforma + apps próprios), `auditor` (leitura irrestrita de tudo, edita nada), `member` (cria/gerencia só os próprios apps — é o `admin` de hoje, renomeado). Ref técnica completa: `.specs/features/dashboard-global-roles/{spec,design}.md`.

Design system existente do dashboard deve ser seguido (não inventar estilo novo).

---

## Princípio central: omitir, não desabilitar

Regra não-negociável desta feature (ver spec.md, Edge Cases): nenhuma tela/botão/item de menu fora da permissão do usuário logado deve aparecer, nem desabilitado/esmaecido. Um `auditor` nunca deve ver um botão "Editar template" cinza — o botão simplesmente não existe na renderização dele. Isso é diferente do padrão usado em `enterprise-licensing` (onde features gated **aparecem** com badge, pra gerar conversão) — aqui é o oposto: é controle de acesso interno, não upselling, então esconder é o comportamento certo.

---

## Telas a ajustar/criar

### 1. Navegação lateral/menu (existente, ajustar visibilidade)

Cada item de menu já existente precisa ficar condicionado à role:

| Item | superadmin | admin | auditor | member |
|---|---|---|---|---|
| Apps (próprios) | ✅ | ✅ | ❌ (não gerencia apps próprios) | ✅ |
| Todos os apps (visão de supervisão) | ✅ | ✅ (read-only) | ✅ (read-only) | ❌ |
| Templates | ✅ | ✅ | ❌ | ❌ |
| Branding | ✅ | ✅ | ❌ | ❌ |
| Usuários do dashboard | ✅ | ✅ | ❌ | ❌ |
| Integrações (deploy, GitHub, observability, email) | ✅ | ❌ | ❌ | ❌ |
| Infra | ✅ | ❌ | ❌ | ❌ |
| Auditoria | ✅ | ❌ | ✅ (read-only) | ❌ |

`auditor` e `admin` que acessam "Todos os apps"/apps de terceiros veem a mesma tela de detalhe do app, mas **sem nenhum botão de edição** — mesma tela, modo read-only, não uma tela paralela.

---

### 2. Tela "Usuários do dashboard" (existente, ajustar formulário de criação/edição)

- Campo "Role" no formulário de criação/edição: dropdown com as opções que o usuário logado tem permissão de atribuir.
  - `superadmin` logado: vê as 4 opções.
  - `admin` logado: vê só `member`, `auditor`, `admin` — **opção `superadmin` nem aparece no dropdown** (não é só desabilitada).
- Badge de role na listagem de usuários: 4 cores/estilos distintos (ex: `superadmin` destaque forte, `admin` destaque médio, `auditor` neutro, `member` padrão) — seguir a paleta já usada em outros badges de status do dashboard.

---

### 3. Indicador de modo "leitura" em apps de terceiros

Quando `admin` ou `auditor` abre um app que não é seu (via a visão de supervisão), a tela de detalhe do app precisa de um indicador claro (banner ou badge no topo) tipo "Visualizando em modo leitura — você não é membro deste app" — evita confusão de "por que não consigo salvar".

---

## Interações específicas

- **Erro 403** (ex: `member` tentando acessar URL de integrações direto): nunca deveria acontecer via UI (item de menu omitido), mas se acontecer (URL direta), mostrar página de "acesso negado" genérica, não um crash ou tela em branco.
- **Mudança de role de um usuário** (tela de usuários): confirmação simples antes de aplicar (é reversível, não precisa de dupla confirmação tipo "digite o nome"), toast de sucesso/erro (`sonner`).
- **Tentativa de promover alguém a `superadmin` sendo `admin`**: como a opção nem aparece no dropdown, não há esse erro a tratar na UI — só reforça que o filtro é na fonte da lista de opções, não uma validação depois do fato.

---

## Coisas que o design PRECISA respeitar

- **i18n obrigatório**: toda string por `react-i18next`, en+pt-BR desde o dia 1, incluindo os 4 labels de role
- **Omitir, não desabilitar**: repetido de propósito — é o princípio mais fácil de errar (desabilitar é o instinto padrão de UI, aqui é errado)
- **Erro de mutação sempre visível**: toast, nunca silencioso
- **Modo leitura precisa ser óbvio**: usuário não pode ficar tentando editar algo que nunca vai salvar sem entender por quê

---

## Fora de escopo deste brief

- Telas de `rbac-per-app` (gestão de membros por app) — spec e brief separados, ainda não escritos
- Qualquer editor de permissão granular por ação — a granularidade é por tela inteira, não há matriz de checkbox fina pra desenhar
