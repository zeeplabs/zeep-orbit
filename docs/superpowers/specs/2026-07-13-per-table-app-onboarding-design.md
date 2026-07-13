# Onboarding de app + salvar tabela por tabela

## Contexto e motivação

Hoje, criar um app (`/apps/new`) e editar um app (`/apps/:id/edit`) usam a mesma
tela (`AppFormPage.tsx`), que junta nome, autenticação por e-mail, provedores de
login, storage, rate limit e a lista inteira de tabelas/colunas num único form.
O submit final (`CreateApp`/`UpdateApp`) troca **todas** as tabelas do app de
uma vez (`UpdateApp` faz `DELETE FROM app_tables WHERE app_id=$1` e reinsere
tudo). Se o usuário preencher várias tabelas com várias colunas e a página
recarregar, fechar aba, cair conexão, ou o backend rejeitar por causa de uma
tabela problemática, tudo é perdido — incluindo tabelas que estavam corretas.

Objetivo: quebrar isso em passos pequenos e auto-explicativos, salvando sob
demanda, para que o usuário nunca arrisque perder trabalho já validado.

## Arquitetura

O provisioner (`internal/provisioner/table.go`, `migration.go`) já é puramente
aditivo: `CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, sem nenhum
`DROP` de tabela ou coluna ausente da config. Isso significa que é seguro
chamar `Provisioner.Apply` com um `config.Config` contendo **só uma tabela**
por vez — as demais tabelas do app não são tocadas. Essa propriedade já
existente é o que destrava o modelo de salvar-por-tabela sem precisar reescrever
o provisioner.

## Backend — novos endpoints

Seguindo o padrão já usado em `/api/apps/{id}/auth/providers`:

- `POST /api/apps/{id}/tables` — cria uma tabela nova.
  - Body: `{name, rls, columns}` (mesmo shape de `AppTableRow` menos o `id`).
  - Valida: nome de tabela único **contra as tabelas já existentes no app**
    (carregadas do banco via `GetApp`/`loadAppTables`), nomes de coluna
    únicos dentro da tabela, tipos de coluna permitidos, e a regra já existente
    de RLS×auth (`rls in {"enabled","owner"}` exige `auth_email_enabled=true`
    no app).
  - Insere metadado (1 linha em `app_tables`) e chama `provisioner.Apply` só
    com essa tabela.
- `PUT /api/apps/{id}/tables/{tableId}` — atualiza colunas/rls de uma tabela
  existente. Mesmas validações. Reusa `applyColumnChanges`/`addMissingColumns`
  do provisioner (já lidam com adicionar coluna, renomear, mudar tipo).
- `DELETE /api/apps/{id}/tables/{tableId}` — remove a tabela: apaga a linha de
  metadado e executa `DROP TABLE` físico no schema do app. **Gap corrigido
  aqui**: hoje remover uma tabela da lista no bulk-update nunca dropa a tabela
  física no Postgres — isso fica explícito e resolvido no novo endpoint.

`CreateApp`/`UpdateApp` bulk continuam existindo, mas o campo `tables` sai do
payload deles — passam a cuidar só de nome/auth email/storage/rate limit.
`validateAppInput` perde a parte de tabelas (duplicidade de nome, RLS×auth,
tipos); essa lógica migra para os novos handlers de tabela, comparando contra
o que já está persistido para aquele app.

## Frontend — rotas e telas

- `/apps/new` — mini-onboarding: campo nome + toggle "Autenticação por
  e-mail". Submit = `POST /api/apps` sem tabelas → app criado → navega para
  `/apps/:id`.
- `/apps/:id` — substitui `/apps/:id/edit`. Única tela de "detalhes do app",
  usada tanto logo após o onboarding quanto para editar depois. Mesmas abas de
  hoje (Banco de Dados, Login, Storage, Rate Limit, API), exceto:
  - Aba "Banco de Dados": tabela-por-tabela (máquina de estado abaixo).
  - Abas Login/Storage/Rate Limit: continuam com botão "Salvar" próprio por
    aba, igual hoje (config simples, baixo risco de perda, não precisa virar
    granular).
- `/apps/:id/edit` deixa de existir como rota própria — vira redirect para
  `/apps/:id` (evita duas rotas fazendo a mesma coisa).

### Máquina de estado por tabela

- Cada tabela tem 2 estados: **salva** (card colapsado: nome + badge de
  RLS + ícones editar/excluir) ou **em edição** (nome + colunas abertos,
  botões "Salvar tabela" e "Cancelar").
- Só uma tabela pode estar em edição por vez. O botão "Adicionar Tabela" fica
  desabilitado enquanto qualquer tabela — nova ou existente — estiver em
  edição. Clicar "Editar" numa tabela salva conta como abrir uma edição e
  bloqueia as outras da mesma forma.
- **Tabela nova (draft, sem `id`)**: "Salvar tabela" → `POST /tables`.
  - Sucesso: card recebe o `id` do servidor e vira **salva**; "Adicionar
    Tabela" libera de novo.
  - Erro: mensagem inline no próprio card, tabela continua em edição, nada se
    perde (é só estado local em memória).
- **Tabela existente editada**: "Salvar tabela" → `PUT /tables/{id}`. Mesmo
  tratamento de erro inline.
- **Cancelar**: tabela nova → descarta o card (nunca existiu no servidor).
  Tabela existente → reverte para os valores salvos (do estado em memória
  carregado no load da página, sem novo fetch).
- **Excluir tabela**: confirmação → `DELETE /tables/{id}` direto, sem precisar
  entrar em modo edição primeiro.
- RLS "Restrito" fica desabilitado no select se o app não tiver autenticação
  por e-mail ligada — evita em tela o erro de RLS×auth já corrigido no
  backend (commit `987474a`).

## Erros

Todo erro de tabela (nome duplicado, RLS sem auth, tipo inválido, falha de
provisionamento) fica preso ao card daquela tabela específica — nunca mais
aparece solto no rodapé da página como hoje. Reusa `h.writeError` (log rico
via zap, já implementado) nos novos handlers. Como o save é sempre de 1
tabela por vez, o raio de explosão de qualquer erro é aquela tabela, nunca o
formulário inteiro.

## Migração de dados existentes

Apps antigos com N tabelas continuam funcionando sem migração de dados: a
tela `/apps/:id` carrega todas as tabelas via `GetApp` (já existente) e cada
uma nasce em estado **salva**. `insertAppTables` pode continuar existindo
internamente (usado por um endpoint bulk legado, se mantido, ou removido se
nada mais chamar) — decisão de implementação, sem impacto em dado já gravado.

## Testes

- **Backend**: testes unitários para os 3 handlers novos (create/update/delete
  table), cobrindo nome de tabela duplicado, RLS sem auth, tipo de coluna
  inválido, tabela não encontrada — mesmo padrão de
  `internal/dashboard/handler_test.go`.
- **Frontend e2e**: `internal/dashboard/ui/e2e/apps.spec.ts` já existe e
  cobre o fluxo antigo (`/apps/new` → preencher tabela inline → "Criar" →
  volta para `/apps`). Esse teste **quebra** com o redesign (rota, placeholders
  e passos mudam) e precisa ser reescrito para o novo fluxo: criar app →
  redirecionar para `/apps/:id` → adicionar tabela → salvar tabela → conferir
  que ela aparece salva → editar → salvar → excluir.
- **Verificação manual**: repetir o mesmo repro ponta a ponta feito na sessão
  anterior (backend local + Postgres via `docker compose`, frontend via Vite
  dev, Playwright dirigindo o browser) antes de fechar a implementação.

## Fora de escopo

- Auto-save granular em Login/Storage/Rate Limit (decidido: continuam com
  botão "Salvar" por aba).
- Reordenar tabelas ou colunas.
- Editar o nome do app depois de criado (comportamento atual mantido, fora do
  escopo desta mudança).
