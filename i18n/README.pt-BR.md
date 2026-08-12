<div align="center">
  <img src="../docs/static/img/orbit-logo.png" alt="Zeep Orbit" width="200" />
  <p><strong>Plataforma completa para times de tecnologia.</strong></p>

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

**Zeep Orbit** é uma plataforma open-source e self-hosted que dá ao seu time tudo para criar e publicar apps — APIs backend, deploy de frontend, domínios customizados e gestão de usuários — tudo de um único dashboard. Sem serviços externos, sem lock-in. Sua infraestrutura, seus dados.

<p align="center">
  <img src="../docs/static/img/diagram.svg" alt="Diagrama de Arquitetura" width="800" />
</p>

```bash
# Apps backend — defina tabelas, tenha APIs REST instantâneas
docker compose up -d
curl -H "Authorization: Bearer $TOKEN" localhost:8080/meu-app/tarefas
# → {"data":[],"count":0}

# Apps frontend — escolha um template, tenha um site no ar com domínio customizado
# Conecte o GitHub, escolha Vite + React, deploy no Render em um clique
```

---

## 📑 Índice

- [Funcionalidades](#-funcionalidades)
- [Início rápido](#-início-rápido)
  - [Docker Compose](#docker-compose)
  - [Kubernetes (Helm)](#kubernetes-helm)
  - [Binário](#binário)
- [Dashboard](#%EF%B8%8F-dashboard)
- [Apps Backend](#%EF%B8%8F-apps-backend)
- [Apps Frontend](#%EF%B8%8F-apps-frontend)
- [SDK Clients](#-sdk-clients)
- [CLI](#-cli)
- [Observabilidade](#-observabilidade)
- [Deploy](#-deploy)
  - [Docker](#docker)
  - [Kubernetes (Helm)](#kubernetes-helm-1)
- [Configuração](#-configuração)
- [Changelog](#-changelog)
- [Roadmap](#%EF%B8%8F-roadmap)
- [Desenvolvimento](#%EF%B8%8F-desenvolvimento)
- [Contribuindo](#-contribuindo)
- [Licença](#-licença)

---

## ✨ Funcionalidades

### Apps Backend

| Funcionalidade          | Descrição                                                |
| ----------------------- | -------------------------------------------------------- |
| **Schema → REST**       | Defina tabelas no dashboard → API CRUD instantânea       |
| **Relacionamentos e Índices** | Foreign keys (`references` + `on_delete`) e índices no schema builder, validados e ordenados automaticamente |
| **Auth por Email**      | Registro e login com email/senha por app                 |
| **Google OAuth**        | Login com Google — tanto no dashboard quanto por app     |
| **Row-Level Security**  | Filtro automático por dono (`rls: owner`/`rls: enabled`), com opção de "exigir RLS por padrão" em tabelas novas; `rls: policy` remove o filtro automático de dono por completo, delegando visibilidade e permissão de escrita 100% às table policies nativas do Postgres — incluindo policies que dão a um role acesso a linhas de outros usuários |
| **Row Policies para Usuário Final** | Regras de acesso por linha, por tabela/ação, combinando o papel de negócio do usuário final com uma condição sobre os dados da própria linha, aplicadas por RLS nativo do Postgres (`CREATE POLICY`) — não um filtro na camada Go |
| **Papéis de Usuário Final Configuráveis** | Defina sua própria lista de papéis de negócio por app, usada pelas row policies e exibida na gestão de usuários do app |
| **App Tokens**          | Gestão de JWT para apps sem auth por email (criar, revogar, renovar) |
| **Health por App**      | `GET /{app}/health` para monitoramento e readiness       |
| **Soft Delete**         | Exclusão lógica configurável (toggle nas configurações)  |
| **Retenção e Purge**    | Job em background remove definitivamente registros soft-deleted fora da janela de retenção (desligado por padrão, auditado) |
| **Timeout de Query**    | `statement_timeout` global nas queries de dados dos apps (padrão 30s, `0` desliga) |
| **Rate Limiting**       | Limite por app, por IP com janela deslizante (RPM)       |
| **Armazenamento S3**    | Storage S3-compatível por app (DO Spaces, AWS, MinIO)    |

### Apps Frontend

| Funcionalidade            | Descrição                                                |
| ------------------------- | -------------------------------------------------------- |
| **Integração GitHub**     | Conecte um GitHub App, gerencie templates e deploy keys  |
| **Sistema de Templates**  | Templates pré-configurados (Vite + React + TypeScript)   |
| **Deploy em Um Clique**   | Crie repositório do template, deploy automático no Render |
| **Domínios Customizados** | Configure domínio + CNAME DNS para cada frontend         |
| **Credenciais de Sync**   | Deploy keys por app para sincronizar local↔repo          |
| **Deploys Recentes**      | Lista ao vivo dos últimos deploys no Render dos seus apps frontend |

### Plataforma

| Funcionalidade           | Descrição                                                |
| ------------------------ | -------------------------------------------------------- |
| **Web Dashboard**        | Interface dark premium para gerenciar tudo              |
| **Data Browser**         | Navegar, filtrar, ordenar, editar, excluir e exportar CSV (limite de linhas configurável) |
| **Gestão de Usuários**   | Gerencie usuários do dashboard e usuários dos apps      |
| **Acesso por papel**     | 4 papéis de plataforma (superadmin/admin/auditor/member) com matriz de permissão para UI e backend |
| **Papéis por app**       | 3 papéis por app (admin/editor/viewer) com UI de gestão de membros; invariante ≥1 admin enforced via transação |
| **Audit Log**            | Histórico de ações com filtros (quem fez o quê, quando)  |
| **CORS**                 | Suporte cross-origin para SPAs e apps mobile             |
| **Docs OpenAPI**         | Swagger UI gerado automaticamente por app                |
| **White-label**          | Branding customizado, temas, nome da empresa             |
| **Métricas Prometheus**  | `zeep_http_requests_total`, histogramas de latência      |
| **Multi-app**            | Um serviço, N apps, schemas e JWT isolados               |
| **CLI**                  | `zeep serve`, `zeep status`                              |
| **Kubernetes**           | Helm chart produção (HPA, PDB, ingress, IRSA)            |
| **SDK Clients**          | TypeScript, Go, Python, Rust, Java, PHP                  |
| **i18n**                 | Dashboard em pt-BR / English, seletor de idioma          |
| **Changelog**            | Histórico de releases no app, embarcado no binário       |
| **Aviso de Atualização** | Alerta no sidebar quando há novo release no GitHub       |

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

Depois acesse **http://localhost:8080/dashboard** para fazer a configuração inicial.

> **Nota:** Se o PostgreSQL estiver rodando na máquina host (não em outro container), use `host.docker.internal` no lugar de `localhost` no `DATABASE_URL`.

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

→ Veja a seção [Kubernetes (Helm)](#kubernetes-helm-1) em **Deploy** para o guia completo.

---

## 🖥️ Dashboard

O dashboard web é embarcado no binário e acessível em `/dashboard`:

- **Apps** — crie apps backend (banco + API) ou frontend (repositório GitHub + deploy)
- **Data Browser** — navegue, filtre, ordene, edite inline, exclua e exporte CSV
- **Usuários** — gerencie usuários do dashboard (papéis superadmin/admin/auditor/member)
- **App Users** — veja usuários cadastrados em cada app, desative contas, resete sessões
- **Membros** — gestão de membros por app (papéis admin/editor/viewer); a aba "Membros" na página de detalhes do app permite ao admin adicionar, mudar papel e remover membros; o invariante ≥1 admin impede remover o último admin
- **Integrações** — config do GitHub App, templates de deploy, provider Render
- **Logs** — log de requisições em tempo real com métricas
- **Auditoria** — histórico de ações com usuário, tipo, recurso, IP e paginação
- **SDKs** — snippets de instalação para os 6 SDKs oficiais
- **Configurações** — branding white-label (temas, nome da empresa), Google OAuth
- **Changelog** — histórico de releases embarcado no binário, atualizado a cada release
- **i18n** — dashboard disponível em pt-BR e English, seletor de idioma na sidebar

---

## 🗄️ Apps Backend

Apps backend fornecem um schema PostgreSQL + API REST instantânea a partir da definição de tabelas.

### Autenticação

Cada app suporta provedores de login configuráveis:

| Provedor | Endpoint                       | Descrição                       |
| -------- | ------------------------------ | ------------------------------- |
| Email    | `POST /{app}/auth/register`    | Registrar com email + senha     |
| Email    | `POST /{app}/auth/login`       | Login com email + senha         |
| Google   | `GET /{app}/auth/google/login` | Entrar com Google               |
| Todos    | `GET /{app}/auth/providers`    | Listar provedores ativos        |

Após autenticação, você recebe um token JWT assinado com o segredo do app.

### API REST

| Método    | Caminho                       | Descrição                             |
| --------- | ----------------------------- | ------------------------------------- |
| GET       | `/{app}/{tabela}`             | Listar (paginado, filtrado, ordenado) |
| POST      | `/{app}/{tabela}`             | Criar                                 |
| GET       | `/{app}/{tabela}/{id}`        | Buscar por ID                         |
| PUT/PATCH | `/{app}/{tabela}/{id}`        | Atualizar (parcial)                   |
| DELETE    | `/{app}/{tabela}/{id}`        | Excluir (soft-delete se ativo)        |
| GET       | `/{app}/health`               | Health check por app (sem auth)       |
| GET       | `/health`                     | Health check global                   |
| GET       | `/metrics`                    | Métricas Prometheus                   |
| GET       | `/docs/{app}`                 | Swagger UI                            |
| GET       | `/{app}/auth/*`               | Endpoints de autenticação             |
| POST      | `/{app}/files`                | Upload de arquivo (multipart)         |
| GET       | `/{app}/files`                | Listar arquivos                       |
| GET       | `/{app}/files/{id}`           | Metadados do arquivo                  |
| GET       | `/{app}/files/{id}/download` | Download (302 → URL assinada)         |
| GET       | `/{app}/files/{id}/url`      | URL assinada com TTL                  |
| DELETE    | `/{app}/files/{id}`           | Excluir arquivo                       |

Query params para listagem: `?limit=`, `?offset=`, `?campo=eq.valor`, `?order=campo.asc`, `?deleted=true` (registros com soft-delete ativo).

### Tipos de coluna

`text`, `integer`, `bigint`, `decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`

Opções: `required` (NOT NULL), `unique`, `default` (expressão SQL).

Colunas automáticas: `id` (UUID), `created_at`, `updated_at`, `deleted_at` (nullable, usado quando soft delete está ativo).

### Relacionamentos e índices

Uma tabela pode declarar foreign keys (`references`: tabela/coluna alvo + `on_delete`) e índices. O schema é validado antes de qualquer DDL rodar (tabela/coluna inexistente, `on_delete` inválido, nomes de índice duplicados, dependências circulares de FK) e as tabelas são criadas em ordem de dependência. O provisionamento de índices é idempotente — nada é removido implicitamente — e dropar uma tabela ainda referenciada pela foreign key de outra é recusado com um erro claro.

### App Tokens

Para apps sem autenticação por email/senha, você pode criar tokens de API com expiração configurável (7d, 30d, 365d ou sem expiração). Tokens usam JWT com `jti` único — revogáveis individualmente, com endpoint de refresh que estende a expiração. A revogação é verificada a cada requisição via cache em memória com invalidação imediata.

---

## 🖥️ Apps Frontend

Apps frontend permitem publicar sites e web apps sem configuração:

1. **Conecte o GitHub** — instale o Zeep Orbit GitHub App na sua organização
2. **Adicione um template** — configure um repositório inicial (ex: Vite + React + TypeScript)
3. **Crie um app frontend** — escolha o template, defina um subdomínio
4. **Sincronize** — receba uma deploy key para clonar o repositório e fazer push
5. **Deploy** — deploy automático no Render com domínio customizado configurado

### Provedor de Deploy

- **Render** — configure API key, project ID, environment ID e domínio base. Cada app frontend faz deploy como um site estático no Render com domínio customizado automático. O Render associa serviços a um *Environment*, não a um Project: o environment é resolvido automaticamente quando o project tem apenas um, e precisa ser informado explicitamente caso contrário. O dashboard também mostra os deploys mais recentes dos seus apps frontend.

### Integração GitHub

O Zeep Orbit conecta no GitHub via **GitHub App** — nunca OAuth ou personal access token, então nenhuma credencial pessoal fica armazenada. Cada instância self-hosted cria e conecta o próprio App na própria org do GitHub (é um produto self-hosted: uma instância, uma empresa, um App — não existe App compartilhado/central pra instalar).

**1. Crie o GitHub App** — `https://github.com/organizations/<sua-org>/settings/apps/new` (ou nas configurações de desenvolvedor da sua conta pessoal, se não usar org):

| Campo | Valor |
|---|---|
| GitHub App name | Qualquer nome único (precisa ser único em todo o GitHub), ex: `acme-zeep-orbit` |
| Homepage URL | URL da sua instância, ou este repo |
| Callback URL | Deixe vazio — não é usado, esse fluxo nunca faz OAuth de login de usuário |
| Setup URL | `https://<sua-instancia>/dashboard/api/github/install/callback` |
| Webhook → Active | Desmarcado — nenhum evento de webhook é consumido hoje |
| Repository permissions → Administration | Read and write |
| Where can this GitHub App be installed | Only on this account |

Gere uma **private key** na página de configurações do App (baixa um arquivo `.pem`) e anote **App ID**, **App slug**, **Client ID** e **Client Secret**.

**2. Configure no dashboard** — vá em **Integrações → Configuração** e cole App ID, App slug, Client ID, Client Secret, e o conteúdo completo do arquivo `.pem` da private key.

**3. Instale** — clique em **Instalar**, que roda o fluxo nativo de instalação do GitHub. Sempre escolha **"Only select repositories"** e marque os repos que essa instância deve gerenciar — a API cria repos novos e gerencia deploy keys só dentro desse escopo, nunca "All repositories".

**4. Adicione um repositório template** — na aba **Templates**, cadastre um repo marcado como **Template repository** no GitHub (`Settings → Template repository` no próprio repo). É esse repo que é clonado toda vez que alguém cria um novo app frontend.

Depois de conectado, deploy keys são gerenciadas automaticamente por app frontend, e repos são arquivados (não excluídos) quando um app frontend é removido.

---

## 📦 SDK Clients

Clients oficiais para todas as principais linguagens. Mesma API em todas:

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
  serve    Provisiona o schema interno, inicia servidor HTTP + Dashboard
  status   Verifica se o servidor está rodando
```

```bash
zeep serve --port 8080
```

Apps e tabelas são criados e gerenciados inteiramente pelo Dashboard (e, futuramente, pelo servidor MCP) — não há arquivo YAML pra escrever nem passo de `apply`.

---

## 📊 Observabilidade

- **Métricas Prometheus** em `/metrics`: contagem de requisições, latência, apps ativos
- **Logging JSON estruturado** via `zap` (use `LOG_LEVEL=debug`)
- **Logs do dashboard** com buffer circular em tempo real, métricas e filtro por app

---

## 🐳 Deploy

### Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest
docker run -e DATABASE_URL=... -p 8080:8080 ghcr.io/zeeplabs/zeep-orbit
```

### Kubernetes (Helm)

O Helm chart inclui: HPA, PDB, Ingress, ServiceMonitor, ServiceAccount pronto para IRSA, topology spread e resource limits configuráveis.

> **Importante:** O comando `zeep serve` carrega tudo do banco de dados. Apps são criados e gerenciados pelo Dashboard em `/dashboard`. Você só precisa do banco.

#### Configuração mínima

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"
```

1. Instale com Helm → `helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml`
2. Acesse `https://seu-dominio/dashboard`
3. Use o `dashboardBootstrapSecret` no formulário de bootstrap
4. Crie usuários admin e apps pela interface

Apps criados no Dashboard persistem no banco e são carregados a cada restart.

#### Exemplo completo (dashboard + Google OAuth + storage)

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"

  google:
    clientId: "123.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxx"
    redirectUrl: "https://orbit.seusite.com/dashboard/api/auth/google/callback"
    allowedDomains: "seusite.com"

  storage:
    endpoint: "https://s3.amazonaws.com"
    bucket: "meu-bucket"
    region: "us-east-1"
    accessKeyId: "AKIA..."
    secretAccessKey: "wJalrX..."

brand:
  theme: "azure"
  companyName: "Minha Empresa"
```

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml
```

#### Atualizando

```bash
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```

A flag `--atomic` faz rollback automático se a atualização falhar.

---

## 📋 Configuração

### Variáveis de ambiente

| Variável                     | Obrigatória | Descrição                                           |
| ---------------------------- | ----------- | --------------------------------------------------- |
| `DATABASE_URL`               | Sim         | String de conexão PostgreSQL                        |
| `DASHBOARD_BOOTSTRAP_SECRET` | Sim         | Segredo para setup inicial do admin                 |
| `GOOGLE_CLIENT_ID`           | Não         | Google OAuth Client ID (para login no dashboard)    |
| `GOOGLE_CLIENT_SECRET`       | Não         | Google OAuth Client Secret                          |
| `GOOGLE_REDIRECT_URL`        | Não         | URL de redirect do Google OAuth                     |
| `GOOGLE_ALLOWED_DOMAINS`     | Não         | Domínios de email permitidos (separados por vírgula) |
| `GOOGLE_OAUTH_ENCRYPTION_KEY` | Não        | Criptografa o Google OAuth client secret em repouso (padrão: `DASHBOARD_BOOTSTRAP_SECRET`) |
| `WEBHOOK_TOKEN_ENCRYPTION_KEY` | Não        | Criptografa os tokens de webhook inbound em repouso (padrão: `DASHBOARD_BOOTSTRAP_SECRET`; separada de propósito, para rotacionar uma sem invalidar a outra) |
| `BRAND_THEME`                | Não         | Tema padrão (azure, emerald, ruby, amber, orange)   |
| `BRAND_COMPANY_NAME`         | Não         | Nome da empresa para white-label                    |
| `LOG_LEVEL`                  | Não         | `debug` para logs de desenvolvimento                |
| `DASHBOARD_LOG_BUFFER_SIZE`  | Não         | Tamanho do buffer circular de logs (padrão: 2000)   |

---

## 📝 Changelog

Toda release do Zeep Orbit é publicada com um changelog embarcado — sem dependências externas, sem banco por instância. O [changelog](../internal/dashboard/changelog.json) é um arquivo JSON estático no repositório, embarcado no binário em tempo de compilação. Usuários veem as atualizações no dashboard em `/changelog` automaticamente a cada upgrade.

Para adicionar uma nova entrada: edite `internal/dashboard/changelog.json`, adicione sua release no array `entries` (mais recente primeiro), faça commit e publique a release. Só isso.

---

## 🗺️ Roadmap

Detalhe completo (checklists por milestone, specs vinculadas) vive em [`.specs/project/ROADMAP.md`](../.specs/project/ROADMAP.md) — esta tabela é um resumo, mantido sincronizado com ele.

| Milestone | Status | Funcionalidades |
|---|---|---|
| **M1 — MVP Core** | ✅ Concluído | Schema → REST, CLI, Docker Compose |
| **M2 — Developer Experience** | ✅ Concluído | Dashboard, SDKs, relacionamentos e índices, migrações, filtro/ordenação |
| **M3 — Apps Frontend** | ✅ Concluído | Integração GitHub, Templates, Deploy Render, Domínios Customizados |
| **M4 — Governança & Segurança** | 🔵 Em desenvolvimento | Audit Log, Soft Delete + retenção/purge, SSO, Rate Limiting, [RBAC por app](../.specs/features/rbac-per-app/) (admin/editor/viewer), [roles globais do dashboard](../.specs/features/dashboard-global-roles/) (superadmin/admin/auditor/member) · planejado: [2FA](../.specs/features/two-factor-auth/), fluxo de aprovação de mudanças de schema |
| **M5 — Storage & Eventos** | 🔵 Em desenvolvimento | Storage S3, [Webhooks de Entrada](../.specs/features/inbound-webhooks/) · planejado: webhooks de saída, event bus |
| **M6 — i18n** | ✅ Concluído | pt-BR / English, seletor de idioma |
| **M7 — SDKs** | ✅ Concluído | Clients TS, Go, Python, Rust, Java, PHP |
| **M8 — Platform Services** | 🔵 Em desenvolvimento | planejado: [integração SMTP/email](../.specs/features/smtp-email-integration/) (convites, recuperação de senha), [integrações de observabilidade](../.specs/features/observability-integrations/) (OpenTelemetry, Datadog, New Relic) |
| **M9 — Enterprise Licensing** | 🔵 Em desenvolvimento | planejado: [modelo de licenciamento dual](../.specs/features/enterprise-licensing/) (núcleo MIT + features enterprise travadas, assinatura anual) |
| **M10 — Autorização de linha (usuário final)** | ✅ Concluído | [Row policies para usuário final](../.specs/features/end-user-row-policies/) (claim de papel de negócio + RLS nativo do Postgres configurado pelo admin) e [papéis de usuário final configuráveis por app](../.specs/features/enduser-roles-config/) |

### Planejado — visível no dashboard, ainda não funcional

Alguns destes itens já aparecem no dashboard como controles desabilitados ou badge "Em breve", para deixar o roadmap visível onde ele vai aterrissar. Eles **não têm backend hoje** e não fazem nada ao serem clicados:

| Item | Onde aparece |
|---|---|
| Autenticação em dois fatores ([spec](../.specs/features/two-factor-auth/)) | Configurações → Provedor de auth ("Exigir 2FA para todos os admins"), Usuários do dashboard (ação "Resetar 2FA") |
| Aprovação de mudanças de schema | Configurações → Banco de dados ("Exigir aprovação de mudanças de schema") |
| Licenciamento enterprise ([spec](../.specs/features/enterprise-licensing/)) | Configurações → Licença (preview só de UI, aba travada) |
| Code hosting: GitLab, Bitbucket | Integrações → Configuração (seletor de provedor) |
| Provedores de deploy: Cloudflare Pages, DigitalOcean, AWS, Azure, Google Cloud | Integrações → Provedores de deploy (seletor de provedor) |
| Provedores de auth do dashboard: Microsoft Entra ID, Sign in with Apple, GitHub | Configurações → Provedor de auth |
| Provedores de storage: Azure Blob Storage, Google Cloud Storage | Configurações → Storage |
| Criação de app assistida por IA | Apps → "Criar com IA" |

### Backlog

- Sign in with Apple (por app)
- Gerador de código TypeScript SDK (`@zeeptech/orbit-generate`)
- Snippets oficiais de prompt para Claude Code / Cursor / Lovable
- Servidor MCP para operações do zeep-orbit
- Geração automática de GraphQL
- Assinaturas em tempo real (WebSockets)
- Edge functions
- Suporte multi-região
- Marketplace de templates de app
- RBAC com permissões granulares por ação (além dos níveis fixos admin/editor/viewer)
- SSO Microsoft Entra ID

---

## 🛠️ Desenvolvimento

```bash
git clone https://github.com/zeeplabs/zeep-orbit
make build        # compila binário Go + UI do dashboard
make test         # testes unitários (sem DB)
make lint         # go vet
make run          # go run ./cmd/zeep
```

Testes de integração precisam de PostgreSQL:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test ./...
```

### Estrutura do projeto

```
cmd/zeep/                  Entrada da CLI
internal/
  auth/                    Handlers de autenticação (registro, login, Google OAuth)
  config/                  Carregador de config YAML + validação
  crypto/                  Criptografia AES-256-GCM
  dashboard/               Backend do dashboard web + UI React + changelog
    changelog.json         Histórico de releases (embarcado no binário)
  db/                      Client pgxpool
  deploy/                  Interface de provedor de deploy + implementação Render
  docs/                    Gerador de spec OpenAPI
  github/                  Client GitHub App (repositórios, deploy keys, templates)
  provisioner/             Provisionamento de schema/tabelas
  query/                   Construtor de queries SQL (seguro contra injeção)
  registry/                Registro de apps em memória thread-safe
  server/                  Roteador HTTP, handlers, middleware
  sshkey/                  Geração de chaves ED25519 (OpenSSH nativo)
charts/                    Helm chart
k8s/                       Manifestos Kustomize
clients/                   SDK clients (TS, Go, Python, Rust, Java, PHP)
examples/                  Apps de exemplo (Todo app)
```

---

## 🤝 Contribuindo

Veja [CONTRIBUTING.md](../CONTRIBUTING.md). Toda contribuição é bem-vinda — correções, features, docs, testes.

---

## 📄 Licença

MIT — veja [LICENSE](../LICENSE).

---

## 🏢 Sobre a Zeep Tecnologia

Zeep Orbit foi criado pela [Zeep Tecnologia](https://zeeptecnologia.com.br) para resolver o que vemos em todo lugar: times usando ferramentas de IA para criar frontends em minutos — e travando quando precisam de backend e deploy.

Subir banco, escrever migrations, publicar API, gerenciar auth, lidar com secrets, configurar domínios, montar CI/CD — isso mata o ritmo. E as alternativas enviam seus dados e infraestrutura para fora do seu controle.

Zeep Orbit é a nossa resposta: **um binário, seu PostgreSQL, infinitos apps.** Faça deploy na sua própria infra, conecte qualquer frontend, mova-se rápido sem a sobrecarga.

Construímos infraestrutura open-source para a era da IA. [Junte-se a nós](https://github.com/zeeplabs/zeep-orbit/discussions).
