---
sidebar_position: 2
---

# Quick Start

## Docker (recommended)

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest

docker run -d \
  --name zeep-orbit \
  -p 8080:8080 \
  -e DATABASE_URL=postgres://user:pass@host:5432/db \
  -e DASHBOARD_BOOTSTRAP_SECRET=my-secret \
  ghcr.io/zeeplabs/zeep-orbit:latest
```

Then open **http://localhost:8080/dashboard** to access the management dashboard.

## Docker Compose

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

## Example App

A complete React + Vite todo example is available at `examples/todo-app/`:

```bash
cd examples/todo-app
npm install && npm run dev
```

It demonstrates authentication, CRUD operations, and SDK usage against any Orbit instance.

## First-time setup

1. Open the dashboard at `http://localhost:8080/dashboard`
2. You'll see the onboarding wizard
3. Enter the bootstrap secret (from `DASHBOARD_BOOTSTRAP_SECRET` env var)
4. Create your superadmin account
5. Start creating apps

## Binary (alternative)

```bash
go install github.com/zeeplabs/zeep-orbit/cmd/zeep@latest
zeep serve --config ./apps.yaml
```

## Kubernetes (Helm)

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit \
  --set secrets.databaseUrl=postgres://... \
  --set 'secrets.apps.myapp.jwtSecret=...'
```
