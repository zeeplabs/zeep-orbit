---
sidebar_position: 1
---

# zeep-orbit

**zeep-orbit** is an open-source, self-hosted BaaS (Backend-as-a-Service) platform. Define your data model — get instant REST APIs + PostgreSQL schemas. Built for AI-generated frontends that need a backend fast.

```yaml
apps:
  - name: billing
    tables:
      - name: invoices
        columns:
          - { name: amount, type: decimal, required: true }
          - { name: status, type: text, default: "pending" }
```

## How it works

1. **Define** your schema (YAML or Dashboard UI)
2. **Provision** — zeep-orbit creates PostgreSQL schemas and tables
3. **Serve** — instant CRUD REST API per table
4. **Auth** — built-in email/password + Google OAuth per app

## Features

- Web dashboard (embedded React UI)
- REST CRUD API with filtering, sorting, pagination
- Email/password auth + Google OAuth per app
- Row-Level Security (`rls: owner`)
- Auto-generated OpenAPI/Swagger docs
- Prometheus metrics + structured logging
- Audit log (who did what, when, IP)
- File storage per app (S3-compatible)
- Rate limiting per app (configurable RPM)
- Dashboard in pt-BR and English (i18n)
- SDK clients: TypeScript, Go, Python, Rust, Java, PHP
- Docker Compose / Kubernetes (Helm)
- Multi-app — one service, N isolated schemas
