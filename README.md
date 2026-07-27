<p align="center">
  <img src="docs/static/img/orbit-logo.png" alt="Zeep Orbit" width="200" />
  <p align="center"><strong>Plataforma completa para times de tecnologia.</strong></p>

  <p>
    <a href="https://github.com/zeeplabs/zeep-orbit/actions"><img src="https://github.com/zeeplabs/zeep-orbit/actions/workflows/docker-publish.yml/badge.svg" alt="CI" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License" /></a>
    <a href="https://go.dev/doc/devel/release"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/releases"><img src="https://img.shields.io/github/v/release/zeeplabs/zeep-orbit" alt="Release" /></a>
  </p>
</div>

---

**Zeep Orbit** is an open-source, self-hosted platform that gives your team everything to build and ship apps — backend APIs, frontend deployment, custom domains, and user management — all from one dashboard. No external services, no lock-in. Your infrastructure, your data.

<p align="center">
  <img src="docs/static/img/diagram.svg" alt="Architecture Diagram" width="800" />
</p>

```bash
# Backend apps — define tables, get instant REST APIs
docker compose up -d
curl -H "Authorization: Bearer $TOKEN" localhost:8080/myapp/tasks
# → {"data":[],"count":0}

# Frontend apps — pick a template, get a live site with a custom domain
# Connect GitHub, choose Vite + React, deploy to Render in one click
```

---

## 📑 Index

- [Features](#-features)
- [Quick start](#-quick-start)
  - [Docker Compose](#docker-compose)
  - [Kubernetes (Helm)](#kubernetes-helm)
  - [Binary](#binary)
- [Dashboard](#%EF%B8%8F-dashboard)
- [Backend Apps](#-backend-apps)
- [Frontend Apps](#-frontend-apps)
- [Authentication](#-authentication)
- [REST API](#-rest-api)
- [SDK Clients](#-sdk-clients)
- [CLI](#-cli)
- [Observability](#-observability)
- [Deployment](#-deployment)
  - [Docker](#docker)
  - [Kubernetes (Helm)](#kubernetes-helm-1)
- [Configuration](#-configuration)
- [Changelog](#-changelog)
- [Roadmap](#%EF%B8%8F-roadmap)
- [Development](#%EF%B8%8F-development)
- [Contributing](#-contributing)
- [License](#-license)

---

## ✨ Features

### Backend Apps

| Feature                | Description                                              |
| ---------------------- | -------------------------------------------------------- |
| **Schema → REST**      | Define tables in the dashboard → instant CRUD API        |
| **Auth by Email**      | Built-in email/password register & login per app         |
| **Google OAuth**       | Sign in with Google — both dashboard and per-app         |
| **Row-Level Security** | Auto-filter data by owner (`rls: owner`)                 |
| **App Tokens**         | JWT management for apps without email auth (create, revoke, refresh) |
| **Per-App Health**     | `GET /{app}/health` for monitoring and readiness probes  |
| **Soft Delete**        | Configurable soft delete toggle (dashboard settings)     |
| **Rate Limiting**      | Per-app, per-IP sliding window (configurable RPM)        |
| **File Storage**       | Per-app S3-compatible storage (DO Spaces, AWS, MinIO)    |

### Frontend Apps

| Feature                  | Description                                              |
| ------------------------ | -------------------------------------------------------- |
| **GitHub Integration**   | Connect a GitHub App, manage templates and deploy keys   |
| **Template System**      | Pre-configured templates (Vite + React + TypeScript)     |
| **One-Click Deploy**     | Create repo from template, deploy to Render automatically |
| **Custom Domains**       | Configure custom domain + DNS CNAME for each frontend    |
| **Sync Credentials**     | Per-app deploy keys for local↔repo sync                  |

### Platform

| Feature                 | Description                                              |
| ----------------------- | -------------------------------------------------------- |
| **Web Dashboard**       | Premium dark UI to manage everything                    |
| **Data Browser**        | GUI to browse, filter, edit, export CSV, delete rows     |
| **User Management**     | Manage dashboard admins and app users                    |
| **Audit Logs**          | Action history with filters (who did what, when, IP)     |
| **CORS**                | Cross-origin support for SPAs and mobile apps            |
| **OpenAPI Docs**        | Auto-generated Swagger UI per app                        |
| **White-label**         | Custom branding, themes, company name                    |
| **Prometheus Metrics**  | `zeep_http_requests_total`, latency histograms           |
| **Multi-app**           | One service, N apps, isolated schemas & JWT secrets      |
| **CLI**                 | `zeep serve`, `zeep apply`, `zeep list`, `zeep status`   |
| **Kubernetes**          | Production-grade Helm chart (HPA, PDB, ingress, IRSA)    |
| **SDK Clients**         | TypeScript, Go, Python, Rust, Java, PHP                  |
| **i18n**                | Dashboard in pt-BR / English, language switcher          |
| **Changelog**           | In-app release history, shipped with the binary          |

---

## 🚀 Quick start

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

Then visit **http://localhost:8080/dashboard** to complete the first-time setup.

> **Note:** If PostgreSQL is running on the host machine (not in another container), use `host.docker.internal` instead of `localhost` in `DATABASE_URL`.

### Binary

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

→ See the [Kubernetes (Helm)](#kubernetes-helm-1) section under **Deployment** for a full guide.

---

## 🖥️ Dashboard

The web dashboard is embedded in the binary and accessible at `/dashboard`:

- **Apps** — create backend apps (database + API) or frontend apps (GitHub repo + deploy)
- **Data Browser** — browse, filter, sort, edit inline, delete, and export CSV
- **Users** — manage dashboard admins (superadmin/admin roles)
- **App Users** — view users registered in each app, deactivate accounts, reset sessions
- **Integrations** — GitHub App config, deploy templates, Render deploy provider
- **Logs** — real-time request log with metrics breakdown
- **Audit** — action history with user, action type, resource, IP, and pagination
- **SDKs** — installation snippets for all 6 official SDKs
- **Settings** — white-label branding (themes, company name), Google OAuth configuration
- **Changelog** — release history shipped with the binary, updated on every release
- **i18n** — dashboard available in pt-BR and English, language switcher in sidebar

---

## 🗄️ Backend Apps

Backend apps give you a PostgreSQL schema + instant REST API from a table definition.

### Auth

Each app supports configurable login providers:

| Provider | Endpoint                       | Description                    |
| -------- | ------------------------------ | ------------------------------ |
| Email    | `POST /{app}/auth/register`    | Register with email + password |
| Email    | `POST /{app}/auth/login`       | Login with email + password    |
| Google   | `GET /{app}/auth/google/login` | Sign in with Google            |
| All      | `GET /{app}/auth/providers`    | List enabled providers         |

After authentication, you receive a JWT token signed with the app's secret.

### REST API

| Method    | Path                  | Description                        |
| --------- | --------------------- | ---------------------------------- |
| GET       | `/{app}/{table}`      | List (paginated, filtered, sorted) |
| POST      | `/{app}/{table}`      | Create                             |
| GET       | `/{app}/{table}/{id}` | Get by ID                          |
| PUT/PATCH | `/{app}/{table}/{id}` | Update (partial)                   |
| DELETE    | `/{app}/{table}/{id}` | Delete (soft-delete if enabled)    |
| GET       | `/{app}/health`       | Per-app health check (no auth)     |
| GET       | `/health`             | Global health check                |
| GET       | `/metrics`            | Prometheus metrics                 |
| GET       | `/docs/{app}`         | Swagger UI                         |
| GET       | `/{app}/auth/*`       | Auth endpoints                     |
| POST      | `/{app}/files`        | Upload file (multipart)            |
| GET       | `/{app}/files`        | List files                         |
| GET       | `/{app}/files/{id}`   | Get file metadata                  |
| GET       | `/{app}/files/{id}/download` | Download file (302 → signed URL) |
| GET       | `/{app}/files/{id}/url` | Get signed URL with TTL          |
| DELETE    | `/{app}/files/{id}`   | Delete file                        |

Query params for list: `?limit=`, `?offset=`, `?field=eq.value`, `?order=field.asc`, `?deleted=true` (soft-deleted records when enabled).

### Column types

`text`, `integer`, `bigint`, `decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`

Options: `required` (NOT NULL), `unique`, `default` (SQL expression).

Auto-generated columns: `id` (UUID), `created_at`, `updated_at`, `deleted_at` (nullable, used when soft delete is enabled).

### App Tokens

For apps without email/password auth, you can create API tokens with configurable expiration (7d, 30d, 365d, or never). Tokens use JWT with unique `jti` — revocable individually, with a refresh endpoint that extends the expiration. Token revocation is checked per-request via an in-memory cache with immediate invalidation on revoke.

---

## 🖥️ Frontend Apps

Frontend apps let you deploy websites and web apps with zero configuration:

1. **Connect GitHub** — install the Zeep Orbit GitHub App on your organization
2. **Add a template** — configure a starter repo (e.g. Vite + React + TypeScript)
3. **Create a frontend app** — pick the template, set a subdomain
4. **Sync** — receive a deploy key to clone the repo locally and push changes
5. **Deploy** — automatic deploy to Render with custom domain configured

### Deploy Provider

- **Render** — configure API key, project ID, and base domain. Each frontend app deploys as a Render static site with automatic custom domain setup.

### GitHub Integration

- GitHub App installation with "All repositories" access
- Template management with deploy configuration fields
- Per-app deploy keys for secure local↔repo sync
- Repo archival when deleting frontend apps

---

## 📦 SDK Clients

Official clients for all major languages. Same API across all:

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

| Language | Package | Path |
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
Commands:
  serve    Load config, provision database, start HTTP server
  apply    Provision schemas and tables, print report
  list     Print apps, tables, and their API URLs
  status   Check if the server is running
```

```bash
zeep serve --port 8080
zeep apply                   # idempotent provisioning
zeep list                    # inspect all apps and tables
```

---

## 📊 Observability

- **Prometheus metrics** at `/metrics`: request count, latency, active apps
- **Structured JSON logging** via `zap` (set `LOG_LEVEL=debug`)
- **Dashboard logs** with real-time ring buffer, metrics, and app-level filtering

---

## 🐳 Deployment

### Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest
docker run -e DATABASE_URL=... -p 8080:8080 ghcr.io/zeeplabs/zeep-orbit
```

### Kubernetes (Helm)

The Helm chart includes: HPA, PDB, Ingress, ServiceMonitor, IRSA-ready ServiceAccount, topology spread, and configurable resource limits.

> **Important:** The `zeep serve` command loads everything from the database. Apps are created and managed through the Dashboard at `/dashboard`. All you need is the database.

#### Minimum setup

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"
```

1. Install with Helm → `helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml`
2. Access `https://your-domain/dashboard`
3. Use the `dashboardBootstrapSecret` in the bootstrap form
4. Create admin users and apps through the interface

Apps created in the Dashboard persist in the database and are loaded on every restart.

#### Full example (dashboard + Google OAuth + storage)

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"

  google:
    clientId: "123.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxx"
    redirectUrl: "https://orbit.yoursite.com/dashboard/api/auth/google/callback"
    allowedDomains: "yoursite.com"

  storage:
    endpoint: "https://s3.amazonaws.com"
    bucket: "my-bucket"
    region: "us-east-1"
    accessKeyId: "AKIA..."
    secretAccessKey: "wJalrX..."

brand:
  theme: "azure"
  companyName: "My Company"
```

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml
```

#### Upgrading

```bash
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```

The `--atomic` flag rolls back automatically if the upgrade fails.

---

## 📋 Configuration

### Environment variables

| Variable                     | Required | Description                                         |
| ---------------------------- | -------- | --------------------------------------------------- |
| `DATABASE_URL`               | Yes      | PostgreSQL connection string                        |
| `DASHBOARD_BOOTSTRAP_SECRET` | Yes      | First-time admin setup secret                       |
| `GOOGLE_CLIENT_ID`           | No       | Google OAuth Client ID (for dashboard login)        |
| `GOOGLE_CLIENT_SECRET`       | No       | Google OAuth Client Secret                          |
| `GOOGLE_REDIRECT_URL`        | No       | Google OAuth redirect URL                           |
| `GOOGLE_ALLOWED_DOMAINS`     | No       | Comma-separated allowed email domains               |
| `BRAND_THEME`                | No       | Default theme (azure, emerald, ruby, amber, orange) |
| `BRAND_COMPANY_NAME`         | No       | Company name for white-label                        |
| `LOG_LEVEL`                  | No       | Set `debug` for development output                  |
| `DASHBOARD_LOG_BUFFER_SIZE`  | No       | Ring buffer size for log viewer (default: 2000)     |

---

## 📝 Changelog

Every release of Zeep Orbit ships with an embedded changelog — no external dependencies, no per-instance database. The [changelog](internal/dashboard/changelog.json) is a static JSON file in the repository, embedded in the binary at compile time. Users see the latest updates in the dashboard at `/changelog` automatically on every upgrade.

To add a new entry: edit `internal/dashboard/changelog.json`, add your release to the `entries` array (newest first), commit, and release. That's it.

---

## 🗺️ Roadmap

| Milestone | Status | Features |
|---|---|---|
| **M1 — MVP Core** | Done | Schema → REST, CLI, Docker Compose |
| **M2 — Dashboard** | Done | App CRUD, Data Browser, Logs, Users, Auth, White-label |
| **M3 — Governance** | Done | Audit Log, Soft Delete, Rate Limiting, App Tokens |
| **M4 — Storage & Events** | Done | S3 File Storage, Per-App Storage Config |
| **M5 — Frontend Deploy** | Done | GitHub Integration, Templates, Render Deploy, Custom Domains |
| **M6 — i18n** | Done | pt-BR / English, language switcher |
| **M7 — SDKs** | Done | TS, Go, Python, Rust, Java, PHP clients |
| **M8 — AI & Automation** | Planned | Natural language schema creation, MCP server |

### Deferred / Backlog

- Sign in with Apple (per-app)
- TypeScript SDK code generator (`@zeeptech/orbit-generate`)
- GraphQL auto-generation
- Realtime subscriptions (WebSockets)
- Edge functions
- Schema change approval workflow
- Marketplace of app templates
- Webhooks & Event Bus
- RBAC with granular permissions
- SSO / SAML integration

---

## 🛠️ Development

```bash
git clone https://github.com/zeeplabs/zeep-orbit
make build        # builds Go binary + dashboard UI
make test         # unit tests (no DB required)
make lint         # go vet
make run          # go run ./cmd/zeep
```

Integration tests require PostgreSQL:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test ./...
```

### Project structure

```
cmd/zeep/                  CLI entrypoint
internal/
  auth/                    Auth handlers (register, login, Google OAuth)
  config/                  YAML config loader + validation
  crypto/                  AES-256-GCM encryption
  dashboard/               Web dashboard backend + React UI + changelog
    changelog.json         Release history (embedded in binary)
  db/                      pgxpool client
  deploy/                  Deploy provider interface + Render implementation
  docs/                    OpenAPI spec generator
  github/                  GitHub App client (repos, deploy keys, templates)
  provisioner/             Schema/table provisioning
  query/                   SQL query builder (injection-safe)
  registry/                Thread-safe in-memory app registry
  server/                  HTTP router, handlers, middleware
  sshkey/                  ED25519 key pair generation (OpenSSH native)
charts/                    Helm chart
k8s/                       Kustomize manifests
clients/                   SDK clients (TS, Go, Python, Rust, Java, PHP)
examples/                  Example apps (Todo app)
```

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All contributions welcome — bug fixes, features, docs, tests.

---

## 📄 License

MIT — see [LICENSE](LICENSE).

---

## 🏢 About Zeep Tecnologia

Zeep Orbit was created by [Zeep Tecnologia](https://zeeptecnologia.com.br) to solve what we saw everywhere: teams using AI tools to build frontends in minutes — and getting stuck when they need a backend and deployment.

Spin up a database, write migrations, deploy an API, manage auth, handle secrets, configure domains, set up CI/CD — it kills momentum. And alternatives send your data and infrastructure outside your control.

Zeep Orbit is our answer: **one binary, your PostgreSQL, infinite apps.** Deploy in your own infrastructure, connect any frontend, move fast without the overhead.

We build open-source infrastructure for the AI era. [Join us](https://github.com/zeeplabs/zeep-orbit/discussions).
