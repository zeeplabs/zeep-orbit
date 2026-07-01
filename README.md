<p align="center">
  <img src="docs/static/img/orbit-logo.png" alt="Zeep Orbit" width="200" />
  <p align="center"><strong>One backend for all your AI-generated frontends.</strong></p>

  <p>
    <a href="https://github.com/zeeplabs/zeep-orbit/actions"><img src="https://github.com/zeeplabs/zeep-orbit/actions/workflows/docker-publish.yml/badge.svg" alt="CI" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License" /></a>
    <a href="https://go.dev/doc/devel/release"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go" /></a>
    <a href="https://hub.docker.com/r/zeeplabs/zeep-orbit"><img src="https://img.shields.io/docker/pulls/zeeplabs/zeep-orbit" alt="Docker Pulls" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/releases"><img src="https://img.shields.io/github/v/release/zeeplabs/zeep-orbit" alt="Release" /></a>
  </p>
</div>

---

**Zeep Orbit** is an open-source, self-hosted BaaS (Backend-as-a-Service) platform. It turns a simple schema definition into instant REST APIs + PostgreSQL schemas — designed for AI-generated frontends (Claude Code, Cursor, Lovable, v0) that need a backend without building one from scratch.

<p align="center">
  <img src="docs/static/img/diagram.svg" alt="Architecture Diagram" width="800" />
</p>

```yaml
# apps.yaml → instant APIs
apps:
  - name: billing
    tables:
      - name: invoices
        columns:
          - { name: amount, type: decimal, required: true }
          - { name: status, type: text, default: "pending" }
```

```bash
docker compose up -d
curl -H "Authorization: Bearer $TOKEN" localhost:8080/billing/invoices
# → {"data":[],"count":0}
```

---

## ✨ Features

| Feature                | Description                                              |
| ---------------------- | -------------------------------------------------------- |
| **Schema → REST**      | Define tables in YAML or Dashboard UI → instant CRUD API |
| **Web Dashboard**      | Premium dark UI to manage apps, tables, data, users      |
| **Auth by Email**      | Built-in email/password register & login per app         |
| **Google OAuth**       | Sign in with Google — both dashboard and per-app         |
| **Row-Level Security** | Auto-filter data by owner (`rls: owner`)                 |
| **Per-App Health**     | `GET /{app}/health` for monitoring and readiness probes  |
| **Soft Delete**        | Configurable soft delete toggle (dashboard settings)     |
| **CORS**               | Cross-origin support for SPAs and mobile apps            |
| **OpenAPI Docs**       | Auto-generated Swagger UI per app                        |
| **Data Browser**       | GUI to browse, filter, edit, export CSV, delete rows     |
| **User Management**    | Manage dashboard admins and app users                    |
| **Audit Logs**         | Action history with filters (who did what, when, IP)     |
| **File Storage**       | Per-app S3-compatible storage (DO Spaces, AWS, MinIO)    |
| **Rate Limiting**      | Per-app, per-IP sliding window (configurable RPM)         |
| **White-label**        | Custom branding, themes, company name                    |
| **Prometheus Metrics** | `zeep_http_requests_total`, latency histograms           |
| **Multi-app**          | One service, N apps, isolated schemas & JWT secrets      |
| **CLI**                | `zeep serve`, `zeep apply`, `zeep list`, `zeep status`   |
| **Kubernetes**         | Production-grade Helm chart (HPA, PDB, ingress, IRSA)    |
| **SDK Clients**        | TypeScript, Go, Python, Rust, Java, PHP                  |
| **i18n**               | Dashboard in pt-BR / English, language switcher          |

---

## 🚀 Quick start

### Docker Compose

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest

docker run -d \
  --name zeep-orbit \
  -p 8080:8080 \
  -e DATABASE_URL=postgres://user:pass@host:5432/db \
  -e DASHBOARD_BOOTSTRAP_SECRET=my-secret \
  ghcr.io/zeeplabs/zeep-orbit:latest
```

> **Nota:** Se o PostgreSQL estiver rodando na máquina host (não em outro container), use `host.docker.internal` no lugar de `localhost` na `DATABASE_URL`.

Then visit **http://localhost:8080/dashboard** to complete the first-time setup.

### Docker Compose

Create a `docker-compose.yml`:

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

### Quick start with Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest

# Create a sample apps.yaml
cat > apps.yaml << 'EOF'
apps:
  - name: myapp
    auth:
      jwt_secret: my-secret-key
    tables:
      - name: items
        columns:
          - { name: title, type: text }
EOF

docker run -d \
  --name zeep-orbit \
  -p 8080:8080 \
  -v $(pwd)/apps.yaml:/app/apps.yaml:ro \
  -e DATABASE_URL=postgres://user:pass@host:5432/db \
  -e DASHBOARD_BOOTSTRAP_SECRET=my-secret \
  ghcr.io/zeeplabs/zeep-orbit:latest
```

### Binary

```bash
go install github.com/zeeplabs/zeep-orbit/cmd/zeep@latest
zeep serve --config ./apps.yaml
```

### Kubernetes (Helm)

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

## 🖥️ Dashboard

The web dashboard is embedded in the binary and accessible at `/dashboard`. Features:

- **Apps** — create, edit, delete apps with dynamic table/column management
- **Data Browser** — browse, filter, sort, edit inline, delete, and export CSV
- **Users** — manage dashboard admins (superadmin/admin roles)
- **App Users** — view users registered in each app, deactivate accounts, reset sessions
- **Logs** — real-time request log with metrics breakdown
- **Audit** — action history with user, action type, resource, IP, and pagination
- **Settings** — white-label branding (themes, company name), Google OAuth configuration
- **i18n** — dashboard available in pt-BR and English, language switcher in sidebar

---

## 📋 Configuration

### Environment variables

| Variable                     | Required | Description                                         |
| ---------------------------- | -------- | --------------------------------------------------- |
| `DATABASE_URL`               | ✅       | PostgreSQL connection string                        |
| `DASHBOARD_BOOTSTRAP_SECRET` | ✅       | First-time admin setup secret                       |
| `GOOGLE_CLIENT_ID`           | ❌       | Google OAuth Client ID (for dashboard login)        |
| `GOOGLE_CLIENT_SECRET`       | ❌       | Google OAuth Client Secret                          |
| `GOOGLE_REDIRECT_URL`        | ❌       | Google OAuth redirect URL                           |
| `GOOGLE_ALLOWED_DOMAINS`     | ❌       | Comma-separated allowed email domains               |
| `BRAND_THEME`                | ❌       | Default theme (azure, emerald, ruby, amber, orange) |
| `BRAND_COMPANY_NAME`         | ❌       | Company name for white-label                        |
| `LOG_LEVEL`                  | ❌       | Set `debug` for development output                  |
| `DASHBOARD_LOG_BUFFER_SIZE`  | ❌       | Ring buffer size for log viewer (default: 2000)     |

### apps.yaml

```yaml
platform:
  database_url: ${DATABASE_URL}

apps:
  - name: myapp
    auth:
      jwt_secret: ${MYAPP_JWT_SECRET}
      providers:
        email: true # enable email/password auth
    tables:
      - name: items
        columns:
          - { name: title, type: text, required: true }
          - { name: score, type: decimal }
```

### Column types

`text`, `integer`, `bigint`, `decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`

Options: `required` (NOT NULL), `unique`, `default` (SQL expression).

Auto-generated columns: `id` (UUID), `created_at`, `updated_at`, `deleted_at` (nullable, used when soft delete is enabled).

---

## 🔐 Authentication

### App-level (for your end-users)

Each app supports configurable login providers:

| Provider | Endpoint                       | Description                    |
| -------- | ------------------------------ | ------------------------------ |
| Email    | `POST /{app}/auth/register`    | Register with email + password |
| Email    | `POST /{app}/auth/login`       | Login with email + password    |
| Google   | `GET /{app}/auth/google/login` | Sign in with Google            |
| All      | `GET /{app}/auth/providers`    | List enabled providers         |

After authentication, you receive a JWT token signed with the app's secret.

### Dashboard (admin access)

Dashboard has its own auth system (email/password or Google OAuth), separate from app auth. Two roles: `admin` and `superadmin`.

---

## 📡 REST API

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

Query params for list: `?limit=`, `?offset=`, `?field=eq.value`, `?order=field.asc`, `?deleted=true` (show soft-deleted records when soft delete is enabled)

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

Example:

```bash
zeep serve --config ./apps.yaml --port 8080
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

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit \
  --set secrets.databaseUrl=postgres://... \
  --set 'secrets.apps.myapp.jwtSecret=...'
```

The Helm chart includes: HPA, PDB, Ingress, ServiceMonitor, PodDisruptionBudget, IRSA-ready ServiceAccount, and configurable resource limits.

---

## 🗺️ Roadmap

| Milestone | Status | Features |
|---|---|---|
| **M1 — MVP Core** | 🟢 Done | Schema → REST, CLI, Docker Compose |
| **M2 — Dashboard** | 🟢 Done | App CRUD, Data Browser, Logs, Users, Auth, White-label |
| **M3 — Governance** | 🔵 In progress | ✅ Audit Log · ⬜ RBAC · ⬜ SSO |
| **M4 — Storage & Events** | 🔵 In progress | ✅ S3/File Storage · ⬜ Webhooks · ⬜ Event Bus |
| **M5 — i18n** | 🟢 Done | pt-BR / English, language switcher |
| **M6 — SDKs** | 🟢 Done | TS, Go, Python, Rust, Java, PHP clients |

### Deferred / Backlog

- Sign in with Apple (per-app)
- TypeScript SDK code generator (`@zeeptech/orbit-generate`)
- MCP server for zeep-orbit
- GraphQL auto-generation
- Realtime subscriptions (WebSockets)
- Edge functions
- Schema change approval workflow
- Marketplace of app templates

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
cmd/zeep/              CLI entrypoint
internal/
  auth/                Auth handlers (register, login, Google OAuth)
  config/              YAML config loader + validation
  crypto/              AES-256-GCM encryption
  dashboard/           Web dashboard backend + React UI
  db/                  pgxpool client
  docs/                OpenAPI spec generator
  provisioner/         Schema/table provisioning
  query/               SQL query builder (injection-safe)
  registry/            Thread-safe in-memory app registry
  server/              HTTP router, handlers, middleware
charts/                Helm chart
k8s/                   Kustomize manifests
clients/               SDK clients (TS, Go, Python, Rust, Java, PHP)
```

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All contributions welcome — bug fixes, features, docs, tests.

---

## 📄 License

MIT — see [LICENSE](LICENSE).

---

## 🏢 About Zeep Tecnologia

zeep-orbit was created by [Zeep Tecnologia](https://zeeptecnologia.com.br) to solve a real pain we saw everywhere: small businesses, entrepreneurs, and indie developers using AI tools (Claude Code, Cursor, Lovable, v0) to build frontends in minutes — but getting stuck when they need a backend.

Spin up a database, write migrations, deploy an API, manage auth, handle secrets — it kills the momentum. And the alternative (Supabase, Firebase) sends your data outside your infrastructure.

zeep-orbit is our answer: **one binary, your PostgreSQL, infinite apps.** Deploy inside your own infra, connect any frontend, move fast without the backend overhead.

We build open-source infrastructure for the AI era. [Join us](https://github.com/zeeplabs/zeep-orbit/discussions).
