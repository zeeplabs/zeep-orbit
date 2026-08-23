<div align="center">
  <img src="../docs/static/img/orbit-logo.png" alt="Zeep Orbit" width="200" />
  <p><strong>A plataforma completa para equipas de tecnologia.</strong></p>

  <p>
    <a href="https://github.com/zeeplabs/zeep-orbit/actions"><img src="https://github.com/zeeplabs/zeep-orbit/actions/workflows/docker-publish.yml/badge.svg" alt="CI" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License" /></a>
    <a href="https://go.dev/doc/devel/release"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/releases"><img src="https://img.shields.io/github/v/release/zeeplabs/zeep-orbit" alt="Release" /></a>
  </p>

  <p>
    <a href="../README.md">🇺🇸 English</a> ·
    <a href="README.pt-BR.md">🇧🇷 Português (Brasil)</a> ·
    <a href="README.pt-PT.md">🇵🇹 Português (Portugal)</a> ·
    <a href="README.es.md">🇪🇸 Español</a>
  </p>
</div>

---

O **Zeep Orbit** é uma plataforma open-source e self-hosted que dá à sua equipa tudo o que precisa para criar e publicar aplicações — APIs de backend, deployment de frontend, domínios personalizados e gestão de utilizadores — tudo a partir de um único dashboard. Sem serviços externos, sem lock-in. A sua infraestrutura, os seus dados.

<p align="center">
  <img src="../docs/static/img/diagram.svg" alt="Diagrama de Arquitetura" width="800" />
</p>

```bash
# Aplicações backend — defina tabelas, obtenha APIs REST instantâneas
docker compose up -d
curl -H "Authorization: Bearer $TOKEN" localhost:8080/aminhaapp/tarefas
# → {"data":[],"count":0}

# Aplicações frontend — escolha um template, obtenha um site online com domínio próprio
# Ligue o GitHub, escolha Vite + React, faça deploy no Render num clique
```

---

## 📑 Índice

- [Funcionalidades](#-funcionalidades)
- [Início rápido](#-início-rápido)
  - [Docker Compose](#docker-compose)
  - [Kubernetes (Helm)](#kubernetes-helm)
  - [Binário](#binário)
- [Dashboard](#%EF%B8%8F-dashboard)
- [Aplicações Backend](#%EF%B8%8F-aplicações-backend)
- [Aplicações Frontend](#%EF%B8%8F-aplicações-frontend)
- [SDK Clients](#-sdk-clients)
- [CLI](#-cli)
- [Servidor MCP](#-servidor-mcp)
- [Observabilidade](#-observabilidade)
- [Deployment](#-deployment)
  - [Docker](#docker)
  - [Kubernetes (Helm)](#kubernetes-helm-1)
- [Configuração](#-configuração)
- [Changelog](#-changelog)
- [Roadmap](#%EF%B8%8F-roadmap)
- [Desenvolvimento](#%EF%B8%8F-desenvolvimento)
- [Contribuir](#-contribuir)
- [Licença](#-licença)

---

## ✨ Funcionalidades

### Aplicações Backend

| Funcionalidade         | Descrição                                                |
| ----------------------- | -------------------------------------------------------- |
| **Schema → REST**      | Defina tabelas no dashboard → API CRUD instantânea       |
| **Relações e Índices** | Foreign keys (`references` + `on_delete`) e índices no schema builder, validados e ordenados automaticamente |
| **Autenticação por Email** | Registo e login com email/palavra-passe por aplicação |
| **Google OAuth**       | Início de sessão com Google — dashboard e por aplicação  |
| **Row-Level Security** | Filtragem automática de dados por proprietário (`rls: owner`/`rls: enabled`), com opção de "exigir RLS por omissão" em tabelas novas; `rls: policy` remove por completo o filtro automático de proprietário, delegando a visibilidade e a permissão de escrita a 100% às table policies nativas do Postgres — incluindo policies que dão a um role acesso a linhas de outros utilizadores |
| **Policies de Linha para Utilizador Final** | Regras de acesso por linha, por tabela/ação, combinando o papel de negócio do utilizador final com uma condição sobre os dados da própria linha, aplicadas por RLS nativo do Postgres (`CREATE POLICY`) — não um filtro na camada Go |
| **Papéis de Utilizador Final Configuráveis** | Defina a sua própria lista de papéis de negócio por app, usada pelas policies de linha e mostrada na gestão de utilizadores do app |
| **App Tokens**         | Gestão de JWT para aplicações sem auth por email (criar, revogar, renovar) |
| **Health por Aplicação** | `GET /{app}/health` para monitorização e readiness probes |
| **Soft Delete**        | Toggle configurável de eliminação lógica (definições do dashboard) |
| **Retenção e Purga**   | Job em background elimina definitivamente registos soft-deleted fora da janela de retenção (desligado por omissão, auditado) |
| **Timeout de Query**   | `statement_timeout` global nas queries de dados das aplicações (por omissão 30s, `0` desliga) |
| **Rate Limiting**      | Sliding window por aplicação e por IP (RPM configurável) |
| **Armazenamento de Ficheiros** | Armazenamento S3-compatível por aplicação (DO Spaces, AWS, MinIO) |

### Aplicações Frontend

| Funcionalidade           | Descrição                                                |
| ------------------------- | -------------------------------------------------------- |
| **Integração com GitHub** | Ligue uma GitHub App, faça a gestão de templates e deploy keys |
| **Sistema de Templates**  | Templates pré-configurados (Vite + React + TypeScript)   |
| **Deploy num Clique**    | Cria o repositório a partir do template, faz deploy no Render automaticamente |
| **Domínios Personalizados** | Configure um domínio próprio + DNS CNAME para cada frontend |
| **Credenciais de Sincronização** | Deploy keys por aplicação para sincronizar local↔repo |
| **Deploys Recentes**     | Lista em tempo real dos últimos deploys no Render das suas aplicações frontend |

### Plataforma

| Funcionalidade          | Descrição                                                |
| ------------------------ | -------------------------------------------------------- |
| **Dashboard Web**       | Interface escura premium para gerir tudo                 |
| **Build with AI**       | Descreva uma app em linguagem natural num painel de chat, reveja o plano proposto (tabelas, auth), confirme para criar; depois de criada, use "Edit with AI" na app para adicionar tabelas/colunas/índices/relações ou alternar RLS/auth uma alteração de cada vez — usa uma chave OpenAI configurada pelo superadmin (Gemini/Claude brevemente) |
| **Data Browser**        | GUI para navegar, filtrar, editar, eliminar registos e exportar CSV (limite de linhas configurável) |
| **Gestão de Utilizadores** | Gerir utilizadores do dashboard e utilizadores de cada app |
| **Acesso por função**     | 4 funções de plataforma (superadmin/admin/auditor/member) com matriz de permissões para UI e backend |
| **Funções por aplicação** | 3 funções por aplicação (admin/editor/viewer) com UI de gestão de membros; invariante ≥1 admin aplicado via transação |
| **Audit Logs**          | Histórico de ações com filtros (quem fez o quê, quando, IP) |
| **CORS**                | Suporte cross-origin para SPAs e apps móveis             |
| **Documentação OpenAPI** | Swagger UI gerado automaticamente por aplicação          |
| **White-label**         | Marca personalizada, temas, nome da empresa               |
| **Métricas Prometheus** | `zeep_http_requests_total`, histogramas de latência       |
| **Multi-app**           | Um serviço, N aplicações, schemas e JWT secrets isolados  |
| **CLI**                 | `zeep serve`, `zeep status`                               |
| **Kubernetes**          | Helm chart pronto para produção (HPA, PDB, ingress, IRSA) |
| **SDK Clients**         | TypeScript, Go, Python, Rust, Java, PHP                   |
| **i18n**                | Dashboard em pt-BR / inglês, seletor de idioma            |
| **Changelog**           | Histórico de releases embutido no binário                 |
| **Notificações de Atualização** | Aviso na sidebar quando há um novo release no GitHub |
| **Servidor MCP**        | Servidor Model Context Protocol — cria e inspeciona apps, tabelas, políticas de linha, membros, tokens e webhooks a partir do Claude Code, Codex, Cursor, OpenCode (PAT) ou Claude Desktop (OAuth 2.1) |

---

## 🚀 Início rápido

### Docker Compose

```yaml
services:
  zeep:
    image: ghcr.io/zeeplabs/zeep-orbit:latest
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://zeep:zeep@db:5432/zeep?sslmode=disable
      DASHBOARD_BOOTSTRAP_SECRET: change-me
    depends_on:
      db:
        condition: service_healthy

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: zeep
      POSTGRES_PASSWORD: zeep
      POSTGRES_DB: zeep
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zeep"]
      interval: 5s
      timeout: 5s
      retries: 5
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

```bash
docker compose up -d
```

De seguida, aceda a **http://localhost:8080/dashboard** para concluir a configuração inicial.

> **Nota:** Se o PostgreSQL estiver a correr na máquina anfitriã (e não noutro contentor), utilize `host.docker.internal` em vez de `localhost` na `DATABASE_URL`.

### Binário

```bash
go install github.com/zeeplabs/zeep-orbit/cmd/zeep@latest
zeep serve
```

### Kubernetes (Helm)

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit \
  --values values.yaml
```

→ Consulte a secção [Kubernetes (Helm)](#kubernetes-helm-1) em **Deployment** para o guia completo.

---

## 🖥️ Dashboard

O dashboard web está embutido no binário e é acessível em `/dashboard`:

- **Aplicações** — crie aplicações backend (base de dados + API) ou frontend (repositório GitHub + deploy)
- **Data Browser** — navegue, filtre, ordene, edite em linha, elimine e exporte para CSV
- **Utilizadores** — gestão de utilizadores do dashboard (funções superadmin/admin/auditor/member)
- **Utilizadores da Aplicação** — visualize utilizadores registados em cada aplicação, desative contas, reinicie sessões
- **Membros** — gestão de membros por aplicação (funções admin/editor/viewer); o separador "Membros" na página de detalhes da aplicação permite ao admin adicionar, alterar função e remover membros; o invariante ≥1 admin impede remover o último admin
- **Integrações** — configuração da GitHub App, templates de deploy, fornecedor de deploy Render
- **Logs** — registo de pedidos em tempo real com detalhe de métricas
- **Auditoria** — histórico de ações com utilizador, tipo de ação, recurso, IP e paginação
- **SDKs** — snippets de instalação para os 6 SDKs oficiais
- **Definições** — marca white-label (temas, nome da empresa), configuração do Google OAuth
- **Changelog** — histórico de releases embutido no binário, atualizado a cada release
- **i18n** — dashboard disponível em pt-BR e inglês, seletor de idioma na sidebar

---

## 🗄️ Aplicações Backend

As aplicações backend dão-lhe um schema PostgreSQL + API REST instantânea a partir da definição de uma tabela.

### Autenticação

Cada aplicação suporta fornecedores de login configuráveis:

| Fornecedor | Endpoint                       | Descrição                          |
| ---------- | ------------------------------- | ------------------------------------ |
| Email      | `POST /{app}/auth/register`    | Registo com email + palavra-passe   |
| Email      | `POST /{app}/auth/login`       | Login com email + palavra-passe     |
| Google     | `GET /{app}/auth/google/login` | Início de sessão com Google         |
| Todos      | `GET /{app}/auth/providers`    | Lista de fornecedores ativos        |

Após a autenticação, recebe um token JWT assinado com o secret da aplicação.

### API REST

| Método    | Rota                  | Descrição                              |
| --------- | --------------------- | ---------------------------------------- |
| GET       | `/{app}/{table}`      | Listar (paginado, filtrado, ordenado)   |
| POST      | `/{app}/{table}`      | Criar                                    |
| GET       | `/{app}/{table}/{id}` | Obter por ID                            |
| PUT/PATCH | `/{app}/{table}/{id}` | Atualizar (parcial)                     |
| DELETE    | `/{app}/{table}/{id}` | Eliminar (soft-delete se ativo)         |
| GET       | `/{app}/health`       | Health check por aplicação (sem auth)   |
| GET       | `/health`             | Health check global                     |
| GET       | `/metrics`            | Métricas Prometheus                     |
| GET       | `/docs/{app}`         | Swagger UI                              |
| GET       | `/{app}/auth/*`       | Endpoints de autenticação               |
| POST      | `/{app}/files`        | Carregar ficheiro (multipart)           |
| GET       | `/{app}/files`        | Listar ficheiros                        |
| GET       | `/{app}/files/{id}`   | Obter metadados do ficheiro             |
| GET       | `/{app}/files/{id}/download` | Transferir ficheiro (302 → URL assinado) |
| GET       | `/{app}/files/{id}/url` | Obter URL assinado com TTL            |
| DELETE    | `/{app}/files/{id}`   | Eliminar ficheiro                        |

Parâmetros de query para listagem: `?limit=`, `?offset=`, `?field=eq.value`, `?order=field.asc`, `?deleted=true` (registos com soft-delete, quando ativo).

### Tipos de coluna

`text`, `integer`, `bigint`, `decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`

Opções: `required` (NOT NULL), `unique`, `default` (expressão SQL).

Colunas geradas automaticamente: `id` (UUID), `created_at`, `updated_at`, `deleted_at` (anulável, usada quando o soft delete está ativo).

### Relações e índices

Uma tabela pode declarar foreign keys (`references`: tabela/coluna alvo + `on_delete`) e índices. O schema é validado antes de qualquer DDL ser executado (tabela/coluna inexistente, `on_delete` inválido, nomes de índice duplicados, dependências circulares de FK) e as tabelas são criadas por ordem de dependência. O provisionamento de índices é idempotente — nada é removido implicitamente — e eliminar uma tabela ainda referenciada pela foreign key de outra é recusado com um erro claro.

### App Tokens

Para aplicações sem autenticação por email/palavra-passe, pode criar tokens de API com expiração configurável (7d, 30d, 365d, ou nunca). Os tokens usam JWT com `jti` único — revogáveis individualmente, com um endpoint de renovação que estende a expiração. A revogação é verificada a cada pedido através de uma cache em memória com invalidação imediata ao revogar.

---

## 🖥️ Aplicações Frontend

As aplicações frontend permitem-lhe fazer deploy de sites e aplicações web sem qualquer configuração:

1. **Ligue o GitHub** — instale a GitHub App do Zeep Orbit na sua organização
2. **Adicione um template** — configure um repositório inicial (ex.: Vite + React + TypeScript)
3. **Crie uma aplicação frontend** — escolha o template, defina um subdomínio
4. **Sincronize** — receba uma deploy key para clonar o repositório localmente e enviar alterações
5. **Faça deploy** — deploy automático no Render com domínio personalizado configurado

### Fornecedor de Deploy

- **Render** — configure a API key, o project ID, o environment ID e o domínio base. Cada aplicação frontend é publicada como um static site do Render, com configuração automática de domínio personalizado. O Render associa serviços a um *Environment*, não a um Project: o environment é resolvido automaticamente quando o project só tem um, e tem de ser indicado explicitamente caso contrário. O dashboard mostra também os deploys mais recentes das suas aplicações frontend.

### Integração com GitHub

O Zeep Orbit liga-se ao GitHub através de uma **GitHub App** — nunca OAuth ou personal access token, pelo que nenhuma credencial pessoal fica armazenada. Cada instância self-hosted cria e liga a própria App à própria org do GitHub (é um produto self-hosted: uma instância, uma empresa, uma App — não existe App partilhada/central para instalar).

**1. Crie a GitHub App** — `https://github.com/organizations/<sua-org>/settings/apps/new` (ou nas definições de programador da sua conta pessoal, se não utilizar uma org):

| Campo | Valor |
|---|---|
| GitHub App name | Qualquer nome único (tem de ser único em todo o GitHub), ex: `acme-zeep-orbit` |
| Homepage URL | URL da sua instância, ou este repositório |
| Callback URL | Deixe vazio — não é utilizado, este fluxo nunca faz OAuth de login de utilizador |
| Setup URL | `https://<sua-instancia>/dashboard/api/github/install/callback` |
| Webhook → Active | Desmarcado — nenhum evento de webhook é consumido hoje |
| Repository permissions → Administration | Read and write |
| Where can this GitHub App be installed | Only on this account |

Gere uma **private key** na página de definições da App (transfere um ficheiro `.pem`) e anote **App ID**, **App slug**, **Client ID** e **Client Secret**.

**2. Configure no dashboard** — vá a **Integrações → Configuração** e cole o App ID, App slug, Client ID, Client Secret, e o conteúdo completo do ficheiro `.pem` da private key.

**3. Instale** — clique em **Instalar**, que executa o fluxo nativo de instalação do GitHub. Escolha sempre **"Only select repositories"** e marque os repositórios que esta instância deve gerir — a API cria repositórios novos e gere deploy keys só dentro desse âmbito, nunca "All repositories".

**4. Adicione um repositório template** — no separador **Templates**, registe um repositório marcado como **Template repository** no GitHub (`Settings → Template repository` no próprio repositório). É este repositório que é clonado sempre que alguém cria uma nova aplicação frontend.

Depois de ligado, as deploy keys são geridas automaticamente por aplicação frontend, e os repositórios são arquivados (não eliminados) quando uma aplicação frontend é removida.

---

## 📦 SDK Clients

Clientes oficiais para todas as principais linguagens. A mesma API em todos:

```typescript
// TypeScript
import { OrbitClient } from '@zeeptech/orbit-client'
const orbit = new OrbitClient({ baseURL, app: 'myapp', jwt })
const rows = await orbit.table('invoices').findMany({ limit: 10 })
```

```go
// Go
import "github.com/zeeplabs/orbit-go"
client := orbit.New(orbit.ClientConfig{BaseURL, "myapp", jwt})
rows, err := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{Limit: 10})
```

```python
# Python
from zeeplabs_orbit_client import OrbitClient, ClientConfig
orbit = OrbitClient(ClientConfig(baseURL, "myapp", jwt))
rows = orbit.table("invoices").find_many(limit=10)
```

```rust
// Rust
use orbit_client::OrbitClient;
let orbit = OrbitClient::new(cfg);
let rows = orbit.table("invoices").find_many(Some(10), None, None, None).await?;
```

```java
// Java
OrbitClient orbit = new OrbitClient(new ClientConfig(baseURL, "myapp", jwt));
ListResponse resp = orbit.table("invoices").findMany(10, 0, null, null);
```

```php
// PHP
$orbit = new Zeeplabs\Orbit\OrbitClient($baseURL, 'myapp', $jwt);
$rows = $orbit->table('invoices')->findMany(limit: 10);
```

| Linguagem | Pacote | Caminho |
|---|---|---|
| TypeScript | `@zeeptech/orbit-client` | `clients/typescript/` |
| Go | `github.com/zeeplabs/orbit-go` | `clients/go/` |
| Python | `zeeplabs-orbit-client` | `clients/python/` |
| Rust | `orbit-client` | `clients/rust/` |
| Java | `com.zeeplabs:orbit-client` | `clients/java/` |
| PHP | `zeeplabs/orbit-client` | `clients/php/` |

---

## 🔧 CLI

```
Comandos:
  serve    Provisiona o schema interno, inicia o servidor HTTP + Dashboard
  status   Verifica se o servidor está em execução
```

```bash
zeep serve --port 8080
```

As aplicações e tabelas são criadas e geridas inteiramente através do Dashboard (ou, futuramente, do servidor MCP) — não existe nenhum ficheiro YAML para escrever nem qualquer passo de `apply`.

---

## 🔌 Servidor MCP

O Zeep Orbit inclui um servidor [Model Context Protocol](https://modelcontextprotocol.io) para que assistentes de código com IA criem e inspecionem aplicações, tabelas, políticas de linha, membros, tokens e webhooks diretamente — sem ser preciso clicar no dashboard.

- **Endpoint:** `https://<host>/dashboard/mcp` — Streamable HTTP, stateless (seguro atrás de load balancer não-sticky / múltiplas réplicas)
- **Autenticação:** dois métodos, ambos resolvidos contra o mesmo store de Personal Access Token:
  - **Personal Access Token (PAT)** — gere um em **Dashboard → MCP**, depois envie como bearer token. É o que Claude Code, Codex, Cursor e OpenCode usam.
  - **OAuth 2.1 + PKCE** — registo dinâmico de client, fluxo authorization code, rotação de refresh token, discovery em `/.well-known/oauth-authorization-server`. É o que o Claude Desktop usa no fluxo interativo de ligação.
- **Tools expostas:** `orbit_list_apps`, `orbit_get_app_schema`, `orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`, `orbit_list_policy_templates`, `orbit_create_policy_from_template` — mesmo caminho de validação, provisionamento e auditoria da REST API e do dashboard, sem atalhos.

### Configuração do cliente

Gere um PAT primeiro (**Dashboard → MCP**), exporte como variável de ambiente e configure o seu cliente:

**Claude Code** — `.mcp.json`:
```json
{
  "mcpServers": {
    "zeep-orbit": {
      "type": "http",
      "url": "https://<host>/dashboard/mcp",
      "headers": {
        "Authorization": "Bearer ${ZEEP_ORBIT_PAT}"
      }
    }
  }
}
```

**Codex** — `~/.codex/config.toml`:
```toml
[mcp_servers.zeep-orbit]
url = "https://<host>/dashboard/mcp"
bearer_token_env_var = "ZEEP_ORBIT_PAT"
```

**Cursor** — `.cursor/mcp.json`:
```json
{
  "mcpServers": {
    "zeep-orbit": {
      "url": "https://<host>/dashboard/mcp",
      "headers": {
        "Authorization": "Bearer ${ZEEP_ORBIT_PAT}"
      }
    }
  }
}
```

**OpenCode** — `opencode.json`:
```json
{
  "mcp": {
    "zeep-orbit": {
      "type": "remote",
      "url": "https://<host>/dashboard/mcp",
      "headers": {
        "Authorization": "Bearer ${ZEEP_ORBIT_PAT}"
      },
      "enabled": true
    }
  }
}
```

> Trate o PAT como uma palavra-passe — nunca o faça commit. Se o seu cliente não suportar interpolação de variável `${VAR}` no ficheiro de configuração, mantenha esse ficheiro fora do controlo de versão.

---

## 📊 Observabilidade

- **Métricas Prometheus** em `/metrics`: número de pedidos, latência, aplicações ativas
- **Logging JSON estruturado** via `zap` (defina `LOG_LEVEL=debug`)
- **Logs do dashboard** com ring buffer em tempo real, métricas e filtragem por aplicação

---

## 🐳 Deployment

### Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest
docker run -e DATABASE_URL=... -p 8080:8080 ghcr.io/zeeplabs/zeep-orbit
```

### Kubernetes (Helm)

O Helm chart inclui: HPA, PDB, Ingress, ServiceMonitor, ServiceAccount pronto para IRSA, topology spread e limites de recursos configuráveis.

> **Importante:** O comando `zeep serve` carrega tudo a partir da base de dados. As aplicações são criadas e geridas através do Dashboard em `/dashboard`. Só precisa da base de dados.

#### Configuração mínima

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"
```

1. Instale com Helm → `helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml`
2. Aceda a `https://o-seu-dominio/dashboard`
3. Utilize o `dashboardBootstrapSecret` no formulário de bootstrap
4. Crie utilizadores administradores e aplicações através da interface

As aplicações criadas no Dashboard persistem na base de dados e são carregadas a cada reinício.

#### Exemplo completo (dashboard + Google OAuth + storage)

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"

  google:
    clientId: "123.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxx"
    redirectUrl: "https://orbit.osite.com/dashboard/api/auth/google/callback"
    allowedDomains: "osite.com"

  storage:
    endpoint: "https://s3.amazonaws.com"
    bucket: "my-bucket"
    region: "us-east-1"
    accessKeyId: "AKIA..."
    secretAccessKey: "wJalrX..."

brand:
  theme: "azure"
  companyName: "A Minha Empresa"
```

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml
```

#### Atualizar

```bash
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit -n <namespace> --reuse-values --atomic
```

`--reuse-values` mantém os values já configurados no release (secrets, configuração) — não é
preciso ter um `values.yaml` local à mão; use `--values values.yaml` em vez disso se for alterar
alguma configuração. `-n <namespace>` tem de corresponder ao namespace onde o release foi
instalado (verificar com `helm list -A`). A flag `--atomic` reverte automaticamente caso a
atualização falhe.

O `image.tag` predefinido é `latest`, pelo que só o `helm upgrade` não recria os pods se a
string da tag não mudou — o Kubernetes só faz rollout quando o pod spec muda. Force o pull da
nova imagem com:

```bash
kubectl rollout restart deploy/zeep-orbit -n <namespace>
kubectl rollout status deploy/zeep-orbit -n <namespace>
```

Em produção, fixe `image.tag` ou `image.digest` no `values.yaml` em vez de depender do
`latest` — assim o `helm upgrade` sozinho já dispara o rollout.

---

## 📋 Configuração

### Variáveis de ambiente

| Variável                      | Obrigatória | Descrição                                             |
| ------------------------------ | ----------- | ------------------------------------------------------ |
| `DATABASE_URL`                 | Sim         | String de ligação ao PostgreSQL                        |
| `DASHBOARD_BOOTSTRAP_SECRET`   | Sim         | Secret para a configuração inicial do administrador    |
| `GOOGLE_CLIENT_ID`             | Não         | Client ID do Google OAuth (login do dashboard)         |
| `GOOGLE_CLIENT_SECRET`         | Não         | Client Secret do Google OAuth                          |
| `GOOGLE_REDIRECT_URL`          | Não         | URL de redirecionamento do Google OAuth                |
| `GOOGLE_ALLOWED_DOMAINS`       | Não         | Domínios de email permitidos, separados por vírgula    |
| `GOOGLE_OAUTH_ENCRYPTION_KEY`  | Não         | Cifra o client secret do Google OAuth em repouso (por defeito: `DASHBOARD_BOOTSTRAP_SECRET`) |
| `WEBHOOK_TOKEN_ENCRYPTION_KEY` | Não         | Cifra os tokens de webhook inbound em repouso (por defeito: `DASHBOARD_BOOTSTRAP_SECRET`; separada de propósito, para rodar uma sem invalidar a outra) |
| `BRAND_THEME`                  | Não         | Tema por defeito (azure, emerald, ruby, amber, orange) |
| `BRAND_COMPANY_NAME`           | Não         | Nome da empresa para white-label                        |
| `LOG_LEVEL`                    | Não         | Defina `debug` para output de desenvolvimento           |
| `DASHBOARD_LOG_BUFFER_SIZE`    | Não         | Tamanho do ring buffer do visualizador de logs (padrão: 2000) |
| `ORBIT_PUBLIC_URL`             | Não         | URL base visível externamente (ex: `https://orbit.example.com`) para o documento de metadados OAuth 2.1 (`/.well-known/oauth-authorization-server`). Sem ela, a URL é derivada dos cabeçalhos `Host`/`X-Forwarded-Proto` do pedido — defina quando o Orbit não estiver atrás de um proxy que valide esses cabeçalhos, para que um cliente MCP não possa ser apontado para um endpoint de token falsificado. |

---

## 📝 Changelog

Cada release do Zeep Orbit é publicado com um changelog embutido — sem dependências externas, sem base de dados por instância. O [changelog](../internal/dashboard/changelog.json) é um ficheiro JSON estático no repositório, embutido no binário em tempo de compilação. Os utilizadores veem as novidades no dashboard em `/changelog` automaticamente a cada atualização.

Para adicionar uma nova entrada: edite `internal/dashboard/changelog.json`, adicione o seu release ao array `entries` (mais recente primeiro), faça commit e publique o release. É só isso.

---

## 🗺️ Roadmap

Detalhe completo (checklists por milestone, specs ligadas) vive em [`.specs/project/ROADMAP.md`](../.specs/project/ROADMAP.md) — esta tabela é um resumo, mantido sincronizado com ele.

| Milestone | Estado | Funcionalidades |
|---|---|---|
| **M1 — MVP Core** | ✅ Concluído | Schema → REST, CLI, Docker Compose |
| **M2 — Developer Experience** | ✅ Concluído | Dashboard, SDKs, relações e índices, migrações, filtragem/ordenação |
| **M3 — Aplicações Frontend** | ✅ Concluído | Integração com GitHub, Templates, Deploy no Render, Domínios personalizados |
| **M4 — Governação & Segurança** | 🔵 Em desenvolvimento | Audit Log, Soft Delete + retenção/purga, SSO, Rate Limiting, [RBAC por aplicação](../.specs/features/rbac-per-app/) (admin/editor/viewer), [papéis globais do dashboard](../.specs/features/dashboard-global-roles/) (superadmin/admin/auditor/member) · planeado: [2FA](../.specs/features/two-factor-auth/), fluxo de aprovação de alterações de schema |
| **M5 — Storage & Events** | 🔵 Em desenvolvimento | Armazenamento S3, [Webhooks de Entrada](../.specs/features/inbound-webhooks/) · planeado: webhooks de saída, event bus |
| **M6 — i18n** | ✅ Concluído | pt-BR / inglês, seletor de idioma |
| **M7 — SDKs** | ✅ Concluído | Clientes TS, Go, Python, Rust, Java, PHP |
| **M8 — Platform Services** | 🔵 Em desenvolvimento | planeado: [integração SMTP/email](../.specs/features/smtp-email-integration/) (convites, recuperação de senha), [integrações de observabilidade](../.specs/features/observability-integrations/) (OpenTelemetry, Datadog, New Relic) |
| **M9 — Enterprise Licensing** | 🔵 Em desenvolvimento | planeado: [modelo de licenciamento dual](../.specs/features/enterprise-licensing/) (núcleo MIT + funcionalidades enterprise trancadas, subscrição anual) |
| **M10 — Autorização de linha (utilizador final)** | ✅ Concluído | [Policies de linha por utilizador final](../.specs/features/end-user-row-policies/) (claim de papel de negócio + RLS nativo do Postgres configurado pelo admin) e [papéis de utilizador final configuráveis por app](../.specs/features/enduser-roles-config/) |

### Planeado — visível no dashboard, ainda não funcional

Alguns destes itens já aparecem no dashboard como controlos desativados ou badge "Em breve", para tornar o roadmap visível onde ele vai aterrar. **Não têm backend hoje** e não fazem nada quando clicados:

| Item | Onde aparece |
|---|---|
| Autenticação de dois fatores ([spec](../.specs/features/two-factor-auth/)) | Definições → Fornecedor de auth ("Exigir 2FA para todos os admins"), Utilizadores do dashboard (ação "Repor 2FA") |
| Aprovação de alterações de schema | Definições → Base de dados ("Exigir aprovação de alterações de schema") |
| Licenciamento enterprise ([spec](../.specs/features/enterprise-licensing/)) | Definições → Licença (pré-visualização só de UI, separador bloqueado) |
| Code hosting: GitLab, Bitbucket | Integrações → Configuração (seletor de fornecedor) |
| Fornecedores de deploy: Cloudflare Pages, DigitalOcean, AWS, Azure, Google Cloud | Integrações → Fornecedores de deploy (seletor de fornecedor) |
| Fornecedores de auth do dashboard: Microsoft Entra ID, Sign in with Apple, GitHub | Definições → Fornecedor de auth |
| Fornecedores de storage: Azure Blob Storage, Google Cloud Storage | Definições → Storage |
| Criação de aplicação assistida por IA | Aplicações → "Criar com IA" |

### Adiado / Backlog

- Início de sessão com Apple (por aplicação)
- Gerador de código para o SDK de TypeScript (`@zeeptech/orbit-generate`)
- Snippets oficiais de prompt para Claude Code / Cursor / Lovable
- Geração automática de GraphQL
- Subscrições em tempo real (WebSockets)
- Edge functions
- Suporte multi-região
- Marketplace de templates de aplicações
- RBAC com permissões granulares por ação (além dos níveis fixos admin/editor/viewer)
- SSO Microsoft Entra ID

---

## 🛠️ Desenvolvimento

```bash
git clone https://github.com/zeeplabs/zeep-orbit
make build        # compila o binário Go + a UI do dashboard
make test         # testes unitários (sem necessidade de base de dados)
make lint         # go vet
make run          # go run ./cmd/zeep
```

Os testes de integração requerem PostgreSQL:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test ./...
```

### Estrutura do projeto

```
cmd/zeep/                  Entrypoint do CLI
internal/
  auth/                    Handlers de autenticação (registo, login, Google OAuth)
  config/                  Carregador de configuração YAML + validação
  crypto/                  Encriptação AES-256-GCM
  dashboard/               Backend do dashboard + UI React + changelog
    changelog.json         Histórico de releases (embutido no binário)
  db/                      Cliente pgxpool
  deploy/                  Interface de fornecedor de deploy + implementação Render
  docs/                    Gerador de especificação OpenAPI
  github/                  Cliente da GitHub App (repositórios, deploy keys, templates)
  provisioner/             Provisionamento de schemas/tabelas
  query/                   Query builder SQL (seguro contra injeção)
  registry/                Registo de aplicações em memória, thread-safe
  server/                  Router HTTP, handlers, middleware
  sshkey/                  Geração de par de chaves ED25519 (OpenSSH nativo)
charts/                    Helm chart
k8s/                       Manifestos Kustomize
clients/                   SDK clients (TS, Go, Python, Rust, Java, PHP)
examples/                  Aplicações de exemplo (Todo app)
```

---

## 🤝 Contribuir

Consulte [CONTRIBUTING.md](../CONTRIBUTING.md). Todas as contribuições são bem-vindas — correções, funcionalidades, documentação, testes.

---

## 📄 Licença

O Zeep Orbit usa um modelo de licença dupla. O núcleo (todo o repositório,
exceto os diretórios enterprise abaixo) é **MIT** — ver [LICENSE](../LICENSE).

O código em `internal/enterprise/` e o seu espelho de frontend
`internal/dashboard/ui/src/enterprise/` é source-available sob a
[Zeep Orbit Enterprise Source License](../internal/enterprise/LICENSE):
livre para ler, estudar e modificar, mas a utilização em produção exige uma
Chave de Licença Enterprise ativa. Ver [LICENSING.md](../LICENSING.md) para
o resumo do modelo, [docs/docs/enterprise-licensing.md](../docs/docs/enterprise-licensing.md)
para uma explicação orientada ao produto, e [COMMERCIAL_TERMS.md](../COMMERCIAL_TERMS.md)
para os termos de assinatura.

Nenhuma funcionalidade enterprise existe ainda — isto apenas estabelece o
limite de licença antes do próprio mecanismo de licenciamento enterprise
([spec](../.specs/features/enterprise-licensing/)).

---

## 🏢 Sobre a Zeep Tecnologia

O Zeep Orbit foi criado pela [Zeep Tecnologia](https://zeeptecnologia.com.br) para resolver algo que víamos por todo o lado: equipas a usar ferramentas de IA para criar frontends em minutos — e a ficarem bloqueadas quando precisam de um backend e de deployment.

Configurar uma base de dados, escrever migrações, publicar uma API, gerir autenticação, tratar de secrets, configurar domínios, montar CI/CD — tudo isto mata o ritmo. E as alternativas enviam os seus dados e infraestrutura para fora do seu controlo.

O Zeep Orbit é a nossa resposta: **um único binário, o seu PostgreSQL, aplicações infinitas.** Faça deploy na sua própria infraestrutura, ligue qualquer frontend, avance rapidamente sem o overhead.

Construímos infraestrutura open-source para a era da IA. [Junte-se a nós](https://github.com/zeeplabs/zeep-orbit/discussions).
