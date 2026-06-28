## 🚀 Zeep Orbit v0.1.0 — Initial Release

**One backend for all your AI-generated frontends.** Zeep Orbit is an open-source, self-hosted BaaS platform that turns simple schema definitions into instant REST APIs + PostgreSQL schemas.

### ✨ Features

#### 🖥️ Web Dashboard
- Premium dark UI embedded in the binary (`go:embed`)
- App management: create, edit, delete apps with dynamic tables & columns
- Data Browser: browse, filter, sort, edit inline, delete rows, export CSV
- Real-time request logs with metrics breakdown
- User management (superadmin/admin roles)
- White-label branding (5 themes, company name, persisted to DB)

#### 🔐 Authentication
- Email/password auth per app (register, login, refresh, logout, profile)
- Google OAuth — both for dashboard login and per-app sign-in
- Row-Level Security (`rls: owner`) — auto-filter data by JWT subject
- Configurable auth providers per app via dashboard UI
- AES-256-GCM encryption for OAuth secrets at rest

#### 📡 REST API
- Full CRUD per table (GET, POST, PUT/PATCH, DELETE)
- Filtering (`eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `like`, `ilike`, `in`)
- Sorting (`?order=field.asc` / `.desc`)
- Pagination (`?limit=&offset=`)
- Auto-generated OpenAPI/Swagger UI per app

#### 🔧 CLI & Deployment
- CLI: `zeep serve`, `zeep apply`, `zeep list`, `zeep status`
- Multi-stage Docker image (< 20MB, multi-arch: amd64 + arm64)
- Docker Compose for local development
- Production-grade Helm chart (HPA, PDB, Ingress, ServiceMonitor, IRSA)
- K8s manifests (Kustomize)

#### 📊 Observability
- Prometheus metrics (`zeep_http_requests_total`, latency histograms)
- Structured JSON logging via `zap`
- Health check endpoint

### 🐛 Bug Fixes
- Login 500 error when `google_id` is NULL
- FK violation on Data Browser create (owner_id injection removed)
- Race condition on bootstrap endpoint (TOCTOU)
- DDL injection prevention on table/column names
- JWT secret exposure in API responses
- Cache not clearing on user login/logout

### 🔒 Security
- Rate limiting on public auth routes (10 req/min)
- Security headers (X-Content-Type-Options, X-Frame-Options, etc.)
- bcrypt cost 12 for password hashing
- CSV formula injection protection
- Encryption at rest for OAuth secrets

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.0
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

Built with ❤️ by [Zeep Tecnologia](https://zeeptech.com.br)
